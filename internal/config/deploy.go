package config

// DeployConfig holds settings consumed by `vibew bundle` when producing the
// deploy artifact. Fields here describe the deploy *target* — they have no
// effect on the running sidecar (vibewarden serve never reads them).
type DeployConfig struct {
	// TargetPlatform is the expected deployment platform, in the standard
	// Docker Buildx format "<os>/<arch>" (e.g. "linux/amd64", "linux/arm64").
	// `vibew bundle` compares this against the bundled image's architecture
	// and aborts on mismatch with an actionable rebuild command.
	//
	// Default: "linux/amd64" (Hetzner and most cloud VMs).
	TargetPlatform string `mapstructure:"target_platform"`

	// Host is the SSH target used in the "Next: deploy" block printed by
	// `vibew bundle` and in the bundle README fenced deploy block. When set,
	// the literal value (e.g. "alice@host.example" or a ~/.ssh/config alias)
	// is substituted verbatim into all three ssh lines. When empty (the
	// default), the bracketed placeholder "<your-ssh-user>@<your-ssh-host>"
	// is used and a hint paragraph is appended.
	//
	// VibeWarden does not validate the shape of this value — any string the
	// user wrote is passed through. SSH will surface auth or DNS failure with
	// its own clear message. This also means ~/.ssh/config aliases (which
	// contain no "@") are accepted without modification.
	Host string `mapstructure:"host"`
}
