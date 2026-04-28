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
}
