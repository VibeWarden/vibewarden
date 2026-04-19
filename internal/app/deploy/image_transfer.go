package deploy

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
)

// isLocalImage returns true when imageName is a bare Docker image name without
// a registry prefix. A registry prefix is identified by the presence of a "/"
// before the optional ":" tag separator.
//
// Examples:
//
//	"myapp:latest"                  -> true  (local)
//	"myapp"                         -> true  (local, no tag)
//	"ghcr.io/org/myapp:latest"     -> false (registry)
//	"docker.io/library/nginx:latest" -> false (registry)
//	"localhost:5000/myapp:latest"   -> false (registry — contains "/")
func isLocalImage(imageName string) bool {
	if imageName == "" {
		return false
	}

	// Strip the tag (":latest", ":v1.0", etc.) to isolate the repository part.
	repo := imageName
	if idx := strings.LastIndex(imageName, ":"); idx != -1 {
		repo = imageName[:idx]
	}

	// A registry prefix always introduces at least one "/" in the repo part
	// (e.g. "ghcr.io/org/app", "docker.io/library/nginx", "localhost:5000/app").
	// A bare name like "myapp" has no "/".
	return !strings.Contains(repo, "/")
}

// transferLocalImage saves the Docker image from the local daemon, rsyncs the
// tar archive to the remote host, loads it into the remote Docker daemon, and
// cleans up the temporary files on both sides.
func (s *Service) transferLocalImage(ctx context.Context, imageName, remoteDir string, out io.Writer) error {
	if s.imageExporter == nil {
		return fmt.Errorf("local image %q requires an image exporter; this is a bug — please report it", imageName)
	}

	fmt.Fprintf(out, "Transferring local image %s to remote...\n", imageName)

	// Step 1: save the image to a local temp file.
	tmpFile, err := os.CreateTemp("", "vibewarden-image-*.tar")
	if err != nil {
		return fmt.Errorf("creating temp file for image export: %w", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close() //nolint:errcheck // we only need the path; docker save writes to it

	// Ensure local cleanup.
	defer os.Remove(tmpPath) //nolint:errcheck

	fmt.Fprintf(out, "  Saving image %s to local temp file...\n", imageName)
	if err := s.imageExporter.Save(ctx, imageName, tmpPath); err != nil {
		return fmt.Errorf("saving image %s locally: %w", imageName, err)
	}

	// Step 2: rsync the tar to the remote host.
	remoteTmpPath := remoteDir + "image-transfer.tar"
	fmt.Fprintln(out, "  Uploading image archive to remote...")
	if err := s.executor.TransferFile(ctx, tmpPath, remoteTmpPath); err != nil {
		return fmt.Errorf("transferring image archive to remote: %w", err)
	}

	// Step 3: load the image on the remote and clean up the remote file.
	fmt.Fprintln(out, "  Loading image on remote...")
	loadCmd := fmt.Sprintf("docker load < %s && rm -f %s", remoteTmpPath, remoteTmpPath)
	if _, err := s.executor.Run(ctx, loadCmd); err != nil {
		return fmt.Errorf("loading image on remote: %w", err)
	}

	fmt.Fprintf(out, "  Image %s transferred successfully.\n", imageName)
	return nil
}
