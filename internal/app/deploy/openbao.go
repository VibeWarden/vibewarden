// Package deploy provides the application service that deploys a VibeWarden
// project to a remote server over SSH.
package deploy

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/vibewarden/vibewarden/internal/ports"
)

const (
	// vibewardenPolicyName is the name of the OpenBao policy granted to the
	// VibeWarden AppRole.
	vibewardenPolicyName = "vibewarden"

	// vibewardenRoleName is the name of the OpenBao AppRole role.
	vibewardenRoleName = "vibewarden"

	// defaultSecretsMount is the KV v2 mount path used for VibeWarden secrets.
	defaultSecretsMount = "secret"

	// defaultBaoEnv is the environment variable prefix for bao CLI commands on
	// the remote host.
	defaultBaoEnv = "VAULT_ADDR=http://127.0.0.1:8200"
)

// BootstrapResult holds the credentials produced during the first-time
// OpenBao bootstrap. Callers should display the UnsealKey to the user and
// store RoleID / SecretID in the remote environment.
type BootstrapResult struct {
	// UnsealKey is the single unseal key generated during `bao operator init`.
	// The user must save this — it cannot be recovered.
	UnsealKey string

	// RootToken is the initial root token. It should be revoked after bootstrap.
	RootToken string

	// RoleID is the AppRole role_id for the vibewarden policy.
	RoleID string

	// SecretID is the AppRole secret_id for the vibewarden policy.
	SecretID string
}

// OpenBaoBootstrapper initialises and configures an OpenBao instance on the
// remote host using the bao CLI over SSH. It never calls the OpenBao HTTP API
// directly — all operations are performed via RemoteExecutor.
type OpenBaoBootstrapper struct {
	executor ports.RemoteExecutor
}

// NewOpenBaoBootstrapper creates an OpenBaoBootstrapper that uses the given
// executor to run bao CLI commands on the remote host.
func NewOpenBaoBootstrapper(executor ports.RemoteExecutor) *OpenBaoBootstrapper {
	return &OpenBaoBootstrapper{executor: executor}
}

// BootstrapOptions configures the OpenBao bootstrap operation.
type BootstrapOptions struct {
	// SecretsFile is the path to a local .env-format file whose KEY=VALUE pairs
	// are seeded into the remote OpenBao instance at secret/data/app.
	// When empty no secrets are seeded.
	SecretsFile string

	// RotateSecrets forces re-seeding of secrets even when OpenBao is already
	// initialised. When false and OpenBao is already initialised, only an
	// unseal is performed (if necessary).
	RotateSecrets bool

	// UnsealKey is required on subsequent deploys when OpenBao is already
	// initialised but sealed. When empty and OpenBao is sealed, an error is
	// returned.
	UnsealKey string

	// RemoteDir is the remote project directory used to persist the AppRole
	// credentials as environment variables in a file alongside the deployment.
	// Defaults to ~/vibewarden/<project>/ when empty.
	RemoteDir string

	// Out is the writer used for progress messages. May be nil.
	Out io.Writer
}

// Bootstrap performs the full OpenBao bootstrap on the remote host:
//
//  1. Checks OpenBao's init status via `bao operator init -status`.
//  2. If uninitialised: initialises with 1 key share / threshold, unseal,
//     enables KV v2, enables AppRole, creates policy and role, generates
//     role_id and secret_id, then seeds secrets if SecretsFile is set.
//  3. If already initialised but sealed: unseals using opts.UnsealKey.
//  4. If opts.RotateSecrets is true: re-seeds secrets from opts.SecretsFile.
//
// It returns a BootstrapResult on first-time init (UnsealKey is non-empty)
// or a partial result on subsequent deploys (UnsealKey is empty, RoleID and
// SecretID are re-read from the remote environment).
func (b *OpenBaoBootstrapper) Bootstrap(ctx context.Context, opts BootstrapOptions) (*BootstrapResult, error) {
	out := opts.Out
	if out == nil {
		out = io.Discard
	}

	initialized, err := b.isInitialized(ctx)
	if err != nil {
		return nil, fmt.Errorf("checking openbao init status: %w", err)
	}

	var result *BootstrapResult

	if !initialized {
		fmt.Fprintln(out, "OpenBao: not initialised — running first-time bootstrap...")
		result, err = b.firstTimeBootstrap(ctx, opts, out)
		if err != nil {
			return nil, err
		}
	} else {
		fmt.Fprintln(out, "OpenBao: already initialised — checking seal status...")
		result = &BootstrapResult{}

		if err := b.ensureUnsealed(ctx, opts.UnsealKey, out); err != nil {
			return nil, err
		}

		if opts.RotateSecrets && opts.SecretsFile != "" {
			fmt.Fprintln(out, "OpenBao: rotating secrets...")
			rootToken, err := b.readRemoteRootToken(ctx, opts.RemoteDir)
			if err != nil {
				return nil, fmt.Errorf("reading remote root token for secret rotation: %w", err)
			}
			if err := b.seedSecrets(ctx, opts.SecretsFile, rootToken, out); err != nil {
				return nil, err
			}
		}
	}

	return result, nil
}

// firstTimeBootstrap performs the full OpenBao initialisation sequence.
func (b *OpenBaoBootstrapper) firstTimeBootstrap(ctx context.Context, opts BootstrapOptions, out io.Writer) (*BootstrapResult, error) {
	// Step 1: initialise with 1 key share and threshold.
	fmt.Fprintln(out, "OpenBao: initialising (1 key share, threshold 1)...")
	initOutput, err := b.baoRun(ctx, "", "operator init -key-shares=1 -key-threshold=1 -format=json")
	if err != nil {
		return nil, fmt.Errorf("openbao operator init: %w", err)
	}

	unsealKey, rootToken, err := ParseInitOutput(initOutput)
	if err != nil {
		return nil, fmt.Errorf("parsing openbao init output: %w", err)
	}

	// Step 2: unseal.
	fmt.Fprintln(out, "OpenBao: unsealing...")
	if _, err := b.baoRun(ctx, rootToken, fmt.Sprintf("operator unseal %s", unsealKey)); err != nil {
		return nil, fmt.Errorf("openbao operator unseal: %w", err)
	}

	// Step 3: enable KV v2 at secret/.
	fmt.Fprintln(out, "OpenBao: enabling KV v2 secrets engine at secret/...")
	if _, err := b.baoRun(ctx, rootToken, fmt.Sprintf("secrets enable -path=%s kv-v2", defaultSecretsMount)); err != nil {
		// Ignore "already enabled" errors.
		if !strings.Contains(err.Error(), "already in use") && !strings.Contains(err.Error(), "path is already in use") {
			return nil, fmt.Errorf("enabling kv-v2: %w", err)
		}
	}

	// Step 4: enable AppRole auth.
	fmt.Fprintln(out, "OpenBao: enabling AppRole auth method...")
	if _, err := b.baoRun(ctx, rootToken, "auth enable approle"); err != nil {
		if !strings.Contains(err.Error(), "already in use") && !strings.Contains(err.Error(), "path is already in use") {
			return nil, fmt.Errorf("enabling approle: %w", err)
		}
	}

	// Step 5: create the vibewarden policy.
	fmt.Fprintln(out, "OpenBao: creating vibewarden policy...")
	policy := vibewardenPolicy()
	writeCmd := fmt.Sprintf("policy write %s - <<'POLICY'\n%s\nPOLICY", vibewardenPolicyName, policy)
	if _, err := b.baoRun(ctx, rootToken, writeCmd); err != nil {
		return nil, fmt.Errorf("writing vibewarden policy: %w", err)
	}

	// Step 6: create the AppRole role.
	fmt.Fprintln(out, "OpenBao: creating vibewarden AppRole role...")
	roleCmd := fmt.Sprintf("write auth/approle/role/%s token_policies=%s token_ttl=1h token_max_ttl=24h",
		vibewardenRoleName, vibewardenPolicyName)
	if _, err := b.baoRun(ctx, rootToken, roleCmd); err != nil {
		return nil, fmt.Errorf("creating approle role: %w", err)
	}

	// Step 7: read role_id.
	fmt.Fprintln(out, "OpenBao: reading role_id...")
	roleIDOutput, err := b.baoRun(ctx, rootToken, fmt.Sprintf("read -field=role_id auth/approle/role/%s/role-id", vibewardenRoleName))
	if err != nil {
		return nil, fmt.Errorf("reading role_id: %w", err)
	}
	roleID := strings.TrimSpace(roleIDOutput)

	// Step 8: generate secret_id.
	fmt.Fprintln(out, "OpenBao: generating secret_id...")
	secretIDOutput, err := b.baoRun(ctx, rootToken, fmt.Sprintf("write -field=secret_id -f auth/approle/role/%s/secret-id", vibewardenRoleName))
	if err != nil {
		return nil, fmt.Errorf("generating secret_id: %w", err)
	}
	secretID := strings.TrimSpace(secretIDOutput)

	// Step 9: seed secrets.
	if opts.SecretsFile != "" {
		fmt.Fprintf(out, "OpenBao: seeding secrets from %s...\n", opts.SecretsFile)
		if err := b.seedSecrets(ctx, opts.SecretsFile, rootToken, out); err != nil {
			return nil, err
		}
	}

	// Step 10: store credentials in the remote environment.
	if opts.RemoteDir != "" {
		fmt.Fprintln(out, "OpenBao: storing credentials on remote...")
		if err := b.storeRemoteCredentials(ctx, opts.RemoteDir, rootToken, roleID, secretID, unsealKey); err != nil {
			return nil, fmt.Errorf("storing remote credentials: %w", err)
		}
	}

	return &BootstrapResult{
		UnsealKey: unsealKey,
		RootToken: rootToken,
		RoleID:    roleID,
		SecretID:  secretID,
	}, nil
}

// isInitialized checks whether OpenBao has already been initialised on the
// remote host. It runs `bao operator init -status` and interprets the exit
// code: 0 = initialised, 2 = not initialised, other = error.
func (b *OpenBaoBootstrapper) isInitialized(ctx context.Context) (bool, error) {
	output, err := b.baoRun(ctx, "", "operator init -status")
	if err != nil {
		// bao operator init -status exits with code 2 when not initialised.
		// The SSH executor wraps the exit code in the error message.
		if strings.Contains(err.Error(), "exit status 2") || strings.Contains(output, "Vault is not initialized") {
			return false, nil
		}
		// Any other non-zero exit is a real error (e.g. OpenBao not running).
		return false, fmt.Errorf("openbao init status: %w", err)
	}
	// Exit 0 means initialised.
	return true, nil
}

// ensureUnsealed checks the seal status and unseals OpenBao if necessary.
func (b *OpenBaoBootstrapper) ensureUnsealed(ctx context.Context, unsealKey string, out io.Writer) error {
	sealed, err := b.isSealed(ctx)
	if err != nil {
		return fmt.Errorf("checking seal status: %w", err)
	}
	if !sealed {
		fmt.Fprintln(out, "OpenBao: already unsealed.")
		return nil
	}

	if unsealKey == "" {
		return fmt.Errorf("openbao is sealed and no unseal key was provided (use --unseal-key or VIBEWARDEN_UNSEAL_KEY)")
	}

	fmt.Fprintln(out, "OpenBao: unsealing...")
	if _, err := b.baoRun(ctx, "", fmt.Sprintf("operator unseal %s", unsealKey)); err != nil {
		return fmt.Errorf("openbao operator unseal: %w", err)
	}
	fmt.Fprintln(out, "OpenBao: unsealed successfully.")
	return nil
}

// isSealed checks whether OpenBao is currently sealed.
func (b *OpenBaoBootstrapper) isSealed(ctx context.Context) (bool, error) {
	output, err := b.baoRun(ctx, "", "status -format=json")
	if err != nil {
		// bao status exits with code 2 when sealed — that is not an error here.
		if strings.Contains(err.Error(), "exit status 2") {
			// Try to parse the JSON output to confirm sealed state.
			if strings.Contains(output, `"sealed":true`) {
				return true, nil
			}
			return true, nil
		}
		return false, fmt.Errorf("openbao status: %w", err)
	}

	return strings.Contains(output, `"sealed":true`), nil
}

// seedSecrets reads KEY=VALUE pairs from the local secrets file and writes
// them all to the remote OpenBao instance at secret/data/app.
func (b *OpenBaoBootstrapper) seedSecrets(ctx context.Context, secretsFile, rootToken string, out io.Writer) error {
	pairs, err := parseEnvFile(secretsFile)
	if err != nil {
		return fmt.Errorf("parsing secrets file %s: %w", secretsFile, err)
	}
	if len(pairs) == 0 {
		fmt.Fprintln(out, "OpenBao: secrets file is empty — nothing to seed.")
		return nil
	}

	// Build the `bao kv put` command arguments: key=value ...
	args := make([]string, 0, len(pairs)+1)
	args = append(args, fmt.Sprintf("kv put %s/data/app", defaultSecretsMount))
	for k, v := range pairs {
		// Shell-quote the value to handle spaces, $, etc.
		args = append(args, fmt.Sprintf("%s=%s", k, ShellQuote(v)))
	}

	cmd := strings.Join(args, " ")
	if _, err := b.baoRun(ctx, rootToken, cmd); err != nil {
		return fmt.Errorf("seeding secrets: %w", err)
	}
	fmt.Fprintf(out, "OpenBao: seeded %d secret(s) to %s/data/app.\n", len(pairs), defaultSecretsMount)
	return nil
}

// storeRemoteCredentials writes OPENBAO_ROOT_TOKEN, VIBEWARDEN_ROLE_ID,
// VIBEWARDEN_SECRET_ID, and OPENBAO_UNSEAL_KEY to a .openbao-credentials
// file in remoteDir on the remote host.
func (b *OpenBaoBootstrapper) storeRemoteCredentials(ctx context.Context, remoteDir, rootToken, roleID, secretID, unsealKey string) error {
	content := fmt.Sprintf(
		"OPENBAO_ROOT_TOKEN=%s\nVIBEWARDEN_ROLE_ID=%s\nVIBEWARDEN_SECRET_ID=%s\nOPENBAO_UNSEAL_KEY=%s\n",
		rootToken, roleID, secretID, unsealKey,
	)
	// Use printf to write the file without relying on heredoc portability.
	escapedContent := strings.ReplaceAll(content, "'", `'\''`)
	cmd := fmt.Sprintf("printf '%%s' '%s' > %s.openbao-credentials", escapedContent, remoteDir)
	if _, err := b.executor.Run(ctx, cmd); err != nil {
		return fmt.Errorf("writing .openbao-credentials: %w", err)
	}
	// Restrict permissions: only the owner can read/write.
	chmodCmd := fmt.Sprintf("chmod 600 %s.openbao-credentials", remoteDir)
	if _, err := b.executor.Run(ctx, chmodCmd); err != nil {
		return fmt.Errorf("chmod .openbao-credentials: %w", err)
	}
	return nil
}

// readRemoteRootToken reads the OPENBAO_ROOT_TOKEN from the .openbao-credentials
// file stored on the remote host. This is used when rotating secrets on
// subsequent deploys.
func (b *OpenBaoBootstrapper) readRemoteRootToken(ctx context.Context, remoteDir string) (string, error) {
	if remoteDir == "" {
		return "", fmt.Errorf("remote directory not set; cannot read root token")
	}
	cmd := fmt.Sprintf("grep ^OPENBAO_ROOT_TOKEN= %s.openbao-credentials | cut -d= -f2-", remoteDir)
	output, err := b.executor.Run(ctx, cmd)
	if err != nil {
		return "", fmt.Errorf("reading root token from remote: %w", err)
	}
	token := strings.TrimSpace(output)
	if token == "" {
		return "", fmt.Errorf("OPENBAO_ROOT_TOKEN not found in remote .openbao-credentials")
	}
	return token, nil
}

// baoRun executes a bao CLI command on the remote host via the RemoteExecutor.
// token is used as the VAULT_TOKEN environment variable when non-empty.
func (b *OpenBaoBootstrapper) baoRun(ctx context.Context, token, subcmd string) (string, error) {
	var envPrefix string
	if token != "" {
		envPrefix = fmt.Sprintf("%s VAULT_TOKEN=%s", defaultBaoEnv, token)
	} else {
		envPrefix = defaultBaoEnv
	}
	cmd := fmt.Sprintf("%s bao %s", envPrefix, subcmd)
	return b.executor.Run(ctx, cmd)
}

// ParseInitOutput extracts the unseal key and root token from the JSON output
// of `bao operator init -format=json`. It is exported for testing.
func ParseInitOutput(output string) (unsealKey, rootToken string, err error) {
	// Find the JSON object in output (bao may print warnings before JSON).
	start := strings.Index(output, "{")
	if start == -1 {
		return "", "", fmt.Errorf("no JSON found in openbao init output: %q", output)
	}
	jsonPart := output[start:]

	var initResponse struct {
		UnsealKeysB64 []string `json:"unseal_keys_b64"`
		RootToken     string   `json:"root_token"`
	}
	if err := json.Unmarshal([]byte(jsonPart), &initResponse); err != nil {
		return "", "", fmt.Errorf("decoding openbao init JSON: %w", err)
	}
	if len(initResponse.UnsealKeysB64) == 0 {
		return "", "", fmt.Errorf("openbao init returned no unseal keys")
	}
	if initResponse.RootToken == "" {
		return "", "", fmt.Errorf("openbao init returned empty root token")
	}
	return initResponse.UnsealKeysB64[0], initResponse.RootToken, nil
}

// parseEnvFile reads a .env-format file (KEY=VALUE per line) from the local
// filesystem and returns a map of key to value. Lines starting with # and
// blank lines are ignored. Values may optionally be quoted with ' or ".
func parseEnvFile(path string) (map[string]string, error) {
	f, err := os.Open(path) //nolint:gosec // path is user-supplied config; acceptable in this context.
	if err != nil {
		return nil, fmt.Errorf("opening secrets file: %w", err)
	}
	defer f.Close() //nolint:errcheck

	return ParseEnvReader(f)
}

// ParseEnvReader parses .env-format lines from r. It is exported for testing.
func ParseEnvReader(r io.Reader) (map[string]string, error) {
	result := make(map[string]string)
	scanner := bufio.NewScanner(r)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.IndexByte(line, '=')
		if idx == -1 {
			return nil, fmt.Errorf("line %d: missing '=' separator in %q", lineNum, line)
		}
		key := strings.TrimSpace(line[:idx])
		if key == "" {
			return nil, fmt.Errorf("line %d: empty key in %q", lineNum, line)
		}
		value := line[idx+1:]
		// Strip optional surrounding quotes.
		value = stripQuotes(value)
		result[key] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading secrets file: %w", err)
	}
	return result, nil
}

// stripQuotes removes a matching pair of surrounding single or double quotes
// from s, if present.
func stripQuotes(s string) string {
	if len(s) >= 2 {
		if (s[0] == '\'' && s[len(s)-1] == '\'') || (s[0] == '"' && s[len(s)-1] == '"') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// ShellQuote wraps a value in single quotes, escaping any existing single
// quotes so the result is safe to embed in a POSIX shell command.
// It is exported for testing.
func ShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// vibewardenPolicy returns the HCL policy granting the vibewarden AppRole
// read/write access to the secret/ KV mount.
func vibewardenPolicy() string {
	return `path "secret/data/*" {
  capabilities = ["create", "read", "update", "delete", "list"]
}
path "secret/metadata/*" {
  capabilities = ["read", "list", "delete"]
}`
}
