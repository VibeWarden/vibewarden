package ops

import (
	"bytes"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// ParseInspectOutputForTest exposes the unexported parseInspectJSON helper so
// that tests in the _test package can exercise the JSON→ImageInfo parsing path
// directly — specifically the Config.Labels → ImageInfo.Labels wiring — without
// shelling out to the Docker daemon (ADR-100 test-strategy requirement).
func ParseInspectOutputForTest(jsonBlob []byte) (ports.ImageInfo, error) {
	return parseInspectJSON(jsonBlob)
}

// BuildDockerArgsForTest exposes the unexported buildDockerArgs helper so that
// tests in the _test package can assert the exact argument slice produced by
// the BuildAdapter for various DockerBuildOptions without shelling out to Docker.
func BuildDockerArgsForTest(tag, contextDir string, opts ports.DockerBuildOptions) []string {
	return buildDockerArgs(tag, contextDir, opts)
}

// ParseDownOutputForTest exposes the unexported parseDownOutput helper so that
// tests in the _test package can exercise the parser directly.
func ParseDownOutputForTest(stderr string) ports.DownResult {
	return parseDownOutput(stderr)
}

// NewComposeAdapterForTest creates a ComposeAdapter whose stderrSink is a
// fresh bytes.Buffer. The buffer is returned so tests can assert on captured
// stderr without touching the real os.Stderr file descriptor.
func NewComposeAdapterForTest() (*ComposeAdapter, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	return &ComposeAdapter{stderrSink: buf}, buf
}

// IsNoOpErrorForTest exposes the unexported isNoOpError helper so that
// tests in the _test package can verify the no-op error classification logic.
func IsNoOpErrorForTest(lower string) bool {
	return isNoOpError(lower)
}

// FullVolumeNameForTest returns the fully-qualified Docker volume name that
// downServices would pass to "docker volume rm" for the given project name and
// relative volume name. This mirrors the construction in downServices so that
// tests can assert the correct argv without shelling out to Docker.
func FullVolumeNameForTest(projectName, volumeName string) string {
	return projectName + "_" + volumeName
}

// BuildImageRmArgsForTest exposes the unexported buildImageRmArgs helper so
// that tests in the _test package can assert the exact argument slice produced
// by ImageRemoveAdapter without shelling out to Docker.
func BuildImageRmArgsForTest(tag string) []string {
	return buildImageRmArgs(tag)
}
