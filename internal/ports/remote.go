package ports

import "context"

// RemoteExecutor runs commands and transfers files on a remote server reachable
// via SSH. Implementations shell out to the system ssh and rsync binaries so
// that the user's SSH agent and ~/.ssh/config are honoured automatically.
type RemoteExecutor interface {
	// Run executes cmd on the remote host and returns the combined stdout+stderr
	// output. A non-zero exit code is returned as an error.
	Run(ctx context.Context, cmd string) (output string, err error)

	// Transfer syncs localDir to remoteDir on the remote host using rsync.
	// localDir must be a path on the local filesystem. remoteDir is a path on
	// the remote host (e.g. "~/vibewarden/myproject/"). When deleteExtra is
	// true, files in remoteDir that are not present in localDir are removed
	// (rsync --delete).
	Transfer(ctx context.Context, localDir, remoteDir string, deleteExtra bool) error
}
