// Package ssh provides a RemoteExecutor that shells out to the system ssh and
// rsync binaries. This means the user's SSH agent, ~/.ssh/config, and any
// ProxyJump rules are honoured automatically, with no Go SSH library required.
package ssh

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
)

// Target holds the parsed components of an ssh:// URL.
type Target struct {
	// User is the remote username (e.g. "ubuntu").
	User string
	// Host is the remote hostname or IP address (e.g. "203.0.113.10").
	Host string
	// Port is the SSH port. When zero the default port 22 is used.
	Port int
}

// ParseTarget parses a target string in ssh://user@host[:port] format.
// The scheme must be "ssh". User and host are required.
func ParseTarget(raw string) (Target, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return Target{}, fmt.Errorf("parsing target URL: %w", err)
	}
	if u.Scheme != "ssh" {
		return Target{}, fmt.Errorf("target URL scheme must be ssh, got %q", u.Scheme)
	}
	if u.User == nil || u.User.Username() == "" {
		return Target{}, fmt.Errorf("target URL must include a username (e.g. ssh://user@host)")
	}
	host := u.Hostname()
	if host == "" {
		return Target{}, fmt.Errorf("target URL must include a host (e.g. ssh://user@host)")
	}

	var port int
	if portStr := u.Port(); portStr != "" {
		port, err = strconv.Atoi(portStr)
		if err != nil {
			return Target{}, fmt.Errorf("target URL port is not a number: %w", err)
		}
		if port < 1 || port > 65535 {
			return Target{}, fmt.Errorf("target URL port %d is out of range", port)
		}
	}

	return Target{
		User: u.User.Username(),
		Host: host,
		Port: port,
	}, nil
}

// Destination returns user@host as expected by ssh/rsync.
func (t Target) Destination() string {
	return t.User + "@" + t.Host
}

// Executor implements ports.RemoteExecutor by shelling out to the system ssh
// and rsync binaries.
type Executor struct {
	target Target
	// keyPath is the optional path to a private key file. When non-empty it is
	// passed as -i <keyPath> to ssh and rsync commands. When empty the system
	// SSH agent and ~/.ssh/config are relied upon.
	keyPath string
}

// NewExecutor creates an Executor for the given Target.
func NewExecutor(target Target) *Executor {
	return &Executor{target: target}
}

// NewExecutorWithKey creates an Executor that uses the specified private key
// file for authentication. keyPath is passed as -i <keyPath> to ssh and rsync.
// Use NewExecutor when you want to rely on the SSH agent / ~/.ssh/config
// instead.
func NewExecutorWithKey(target Target, keyPath string) *Executor {
	return &Executor{target: target, keyPath: keyPath}
}

// Run executes cmd on the remote host via ssh and returns the combined
// stdout+stderr output. A non-zero exit code is wrapped and returned as an
// error that includes the captured output for diagnosis.
func (e *Executor) Run(ctx context.Context, cmd string) (string, error) {
	args := e.sshArgs(cmd)
	//nolint:gosec // cmd is caller-supplied; callers in this codebase use only
	// fixed shell commands (e.g. "which docker"). The linter flag is acceptable
	// here because the alternative (a Go SSH library) is worse for usability.
	c := exec.CommandContext(ctx, "ssh", args...)

	var buf bytes.Buffer
	c.Stdout = &buf
	c.Stderr = &buf

	if err := c.Run(); err != nil {
		return buf.String(), fmt.Errorf("ssh %s: %w\noutput: %s", cmd, err, strings.TrimSpace(buf.String()))
	}
	return strings.TrimSpace(buf.String()), nil
}

// RunStream executes cmd on the remote host via ssh, writing stdout and stderr
// directly to the provided writers without buffering. It is intended for
// long-running commands (e.g. "docker compose logs -f") where output must be
// streamed to the caller in real-time. Cancel ctx to terminate the remote
// process.
func (e *Executor) RunStream(ctx context.Context, cmd string, stdout, stderr io.Writer) error {
	args := e.sshArgs(cmd)
	//nolint:gosec // cmd is caller-supplied; see the Run method for the same
	// rationale.
	c := exec.CommandContext(ctx, "ssh", args...)
	c.Stdout = stdout
	c.Stderr = stderr

	if err := c.Run(); err != nil {
		// When the context is cancelled (user pressed Ctrl-C) the ssh process is
		// killed and Run returns a non-nil error. Treat context cancellation as a
		// clean exit so the caller does not print a spurious error message.
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("ssh %s: %w", cmd, err)
	}
	return nil
}

// Transfer syncs localDir to remoteDir on the remote host using rsync over SSH.
// When deleteExtra is true, extraneous files in remoteDir are removed.
func (e *Executor) Transfer(ctx context.Context, localDir, remoteDir string, deleteExtra bool) error {
	args := e.rsyncArgs(localDir, remoteDir, deleteExtra)
	//nolint:gosec // localDir is constructed internally from config paths; remoteDir
	// is a fixed pattern (~/vibewarden/<project>/). Safe in this context.
	c := exec.CommandContext(ctx, "rsync", args...)

	var buf bytes.Buffer
	c.Stdout = &buf
	c.Stderr = &buf

	if err := c.Run(); err != nil {
		return fmt.Errorf("rsync %s → %s:%s: %w\noutput: %s",
			localDir, e.target.Destination(), remoteDir, err, strings.TrimSpace(buf.String()))
	}
	return nil
}

// TransferExcluding syncs localDir to remoteDir on the remote host using rsync
// over SSH, excluding files or directories matching the given patterns. Each
// pattern is passed as an --exclude flag to rsync. When deleteExtra is true,
// extraneous files in remoteDir are removed (but only those not matched by an
// exclude pattern are considered).
func (e *Executor) TransferExcluding(ctx context.Context, localDir, remoteDir string, deleteExtra bool, excludes []string) error {
	args := e.rsyncExcludeArgs(localDir, remoteDir, deleteExtra, excludes)
	//nolint:gosec // localDir is constructed internally from config paths; remoteDir
	// is a fixed pattern (~/vibewarden/<project>/). Safe in this context.
	c := exec.CommandContext(ctx, "rsync", args...)

	var buf bytes.Buffer
	c.Stdout = &buf
	c.Stderr = &buf

	if err := c.Run(); err != nil {
		return fmt.Errorf("rsync %s → %s:%s: %w\noutput: %s",
			localDir, e.target.Destination(), remoteDir, err, strings.TrimSpace(buf.String()))
	}
	return nil
}

// TransferFile copies a single local file to remotePath on the remote host
// using rsync over SSH. Unlike Transfer, the source path is used as-is (no
// trailing slash) so rsync treats it as a file, not a directory.
func (e *Executor) TransferFile(ctx context.Context, localFile, remotePath string) error {
	args := e.rsyncFileArgs(localFile, remotePath)
	//nolint:gosec // localFile is constructed internally from config paths; remotePath
	// is a fixed pattern (~/vibewarden/<project>/vibewarden.yaml). Safe in this context.
	c := exec.CommandContext(ctx, "rsync", args...)

	var buf bytes.Buffer
	c.Stdout = &buf
	c.Stderr = &buf

	if err := c.Run(); err != nil {
		return fmt.Errorf("rsync %s → %s:%s: %w\noutput: %s",
			localFile, e.target.Destination(), remotePath, err, strings.TrimSpace(buf.String()))
	}
	return nil
}

// DryRunTransfer performs a dry-run rsync between localDir and remoteDir with
// --delete --itemize-changes to detect what would change without modifying any
// files. It returns a list of human-readable change descriptions (one per
// affected file). An empty slice means the directories are in sync.
func (e *Executor) DryRunTransfer(ctx context.Context, localDir, remoteDir string) ([]string, error) {
	args := e.rsyncDryRunArgs(localDir, remoteDir)
	//nolint:gosec // localDir is constructed internally from config paths; remoteDir
	// is a fixed pattern (~/vibewarden/<project>/). Safe in this context.
	c := exec.CommandContext(ctx, "rsync", args...)

	var buf bytes.Buffer
	c.Stdout = &buf
	c.Stderr = &buf

	if err := c.Run(); err != nil {
		return nil, fmt.Errorf("rsync dry-run %s → %s:%s: %w\noutput: %s",
			localDir, e.target.Destination(), remoteDir, err, strings.TrimSpace(buf.String()))
	}

	return parseDryRunOutput(buf.String()), nil
}

// parseDryRunOutput extracts meaningful change lines from rsync --itemize-changes
// dry-run output. Each non-empty line that starts with an itemize code (e.g.
// ">f..T......", "*deleting") is included. Directory-only metadata changes,
// new-file creation entries, and blank lines are filtered out.
//
// New-file entries (e.g. ">f+++++++++ filename") are expected on first deploy
// to an empty remote directory and are not considered drift. Only actual
// modifications (changed timestamps, sizes, permissions, or deletions) are
// reported as changes. See issue #962.
func parseDryRunOutput(output string) []string {
	var changes []string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// rsync --itemize-changes prefixes each change with a code like:
		//   >f..T...... filename   (file will be transferred — modification)
		//   >f+++++++++ filename   (file will be created — new, not drift)
		//   cd+++++++++ dirname/   (directory will be created — new, not drift)
		//   *deleting   filename   (file will be deleted)
		//   .d..t...... dirname/   (directory metadata change — skip)
		//
		// We skip:
		//   - directory-only metadata changes (starting with ".d")
		//   - new-file/new-directory creation entries where all attribute
		//     positions after the type character are "+" (e.g. ">f+++++++++")

		// Skip directory metadata changes.
		if strings.HasPrefix(line, ".d") {
			continue
		}

		// Skip new-item creation entries. The itemize code is the first
		// space-delimited field. When all characters after position 1 (the
		// type character) are "+", the item is being created for the first
		// time — this is not drift.
		if isNewItemEntry(line) {
			continue
		}

		changes = append(changes, line)
	}
	return changes
}

// isNewItemEntry returns true when the rsync itemize line represents a
// brand-new item (file or directory) being created on the remote. The itemize
// code format is YXcstpoguax where Y is the update type, X is the file type,
// and positions 2+ are attribute flags. When all attribute flags are "+", the
// item is new. Examples: ">f+++++++++", "cd+++++++++", "<f++++++++++".
func isNewItemEntry(line string) bool {
	// The itemize code is the first space-delimited field.
	code := line
	if idx := strings.IndexByte(line, ' '); idx != -1 {
		code = line[:idx]
	}

	// Need at least 3 characters: direction + type + at least one attribute.
	if len(code) < 3 {
		return false
	}

	// Check that all characters from position 2 onward are "+".
	for i := 2; i < len(code); i++ {
		if code[i] != '+' {
			return false
		}
	}
	return true
}

// sshArgs builds the ssh argument list for the given command.
func (e *Executor) sshArgs(cmd string) []string {
	args := []string{
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "BatchMode=yes",
	}
	if e.keyPath != "" {
		args = append(args, "-i", e.keyPath)
	}
	if e.target.Port != 0 {
		args = append(args, "-p", strconv.Itoa(e.target.Port))
	}
	args = append(args, e.target.Destination(), cmd)
	return args
}

// rsyncArgs builds the rsync argument list for a directory transfer.
func (e *Executor) rsyncArgs(localDir, remoteDir string, deleteExtra bool) []string {
	// Build the ssh command string used as rsync's transport.
	sshCmd := "ssh -o StrictHostKeyChecking=accept-new -o BatchMode=yes"
	if e.keyPath != "" {
		sshCmd += " -i " + e.keyPath
	}
	if e.target.Port != 0 {
		sshCmd += " -p " + strconv.Itoa(e.target.Port)
	}

	args := []string{
		"-az",
		"--progress",
		"-e", sshCmd,
	}
	if deleteExtra {
		args = append(args, "--delete")
	}
	// Ensure localDir ends with "/" so rsync syncs the contents, not the
	// directory name itself.
	src := strings.TrimSuffix(localDir, "/") + "/"
	dst := e.target.Destination() + ":" + remoteDir
	args = append(args, src, dst)
	return args
}

// rsyncExcludeArgs builds the rsync argument list for a directory transfer
// with --exclude patterns.
func (e *Executor) rsyncExcludeArgs(localDir, remoteDir string, deleteExtra bool, excludes []string) []string {
	sshCmd := "ssh -o StrictHostKeyChecking=accept-new -o BatchMode=yes"
	if e.keyPath != "" {
		sshCmd += " -i " + e.keyPath
	}
	if e.target.Port != 0 {
		sshCmd += " -p " + strconv.Itoa(e.target.Port)
	}

	args := []string{
		"-az",
		"--progress",
		"-e", sshCmd,
	}
	if deleteExtra {
		args = append(args, "--delete")
	}
	for _, pattern := range excludes {
		args = append(args, "--exclude", pattern)
	}
	src := strings.TrimSuffix(localDir, "/") + "/"
	dst := e.target.Destination() + ":" + remoteDir
	args = append(args, src, dst)
	return args
}

// rsyncDryRunArgs builds the rsync argument list for a dry-run transfer with
// --delete, --itemize-changes, and --checksum. The --checksum flag ensures
// rsync compares file content (via checksums) rather than mtime+size, which
// eliminates false-positive drift reports caused by timestamp differences on
// files whose content has not actually changed (e.g. regenerated deploy
// bundle files). See issue #1031.
func (e *Executor) rsyncDryRunArgs(localDir, remoteDir string) []string {
	sshCmd := "ssh -o StrictHostKeyChecking=accept-new -o BatchMode=yes"
	if e.keyPath != "" {
		sshCmd += " -i " + e.keyPath
	}
	if e.target.Port != 0 {
		sshCmd += " -p " + strconv.Itoa(e.target.Port)
	}

	src := strings.TrimSuffix(localDir, "/") + "/"
	dst := e.target.Destination() + ":" + remoteDir

	return []string{
		"-az",
		"--dry-run",
		"--delete",
		"--itemize-changes",
		"--checksum",
		"-e", sshCmd,
		src,
		dst,
	}
}

// rsyncFileArgs builds the rsync argument list for a single-file transfer.
// The source path is used as-is — no trailing slash — so rsync treats it as
// a regular file rather than a directory.
func (e *Executor) rsyncFileArgs(localFile, remotePath string) []string {
	sshCmd := "ssh -o StrictHostKeyChecking=accept-new -o BatchMode=yes"
	if e.keyPath != "" {
		sshCmd += " -i " + e.keyPath
	}
	if e.target.Port != 0 {
		sshCmd += " -p " + strconv.Itoa(e.target.Port)
	}

	dst := e.target.Destination() + ":" + remotePath
	return []string{
		"-az",
		"--progress",
		"-e", sshCmd,
		localFile,
		dst,
	}
}
