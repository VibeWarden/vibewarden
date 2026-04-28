package ops

import (
	"context"
	"fmt"
	"io"
	"path/filepath"

	generateapp "github.com/vibewarden/vibewarden/internal/app/generate"
	"github.com/vibewarden/vibewarden/internal/config"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// obsProfile is the Docker Compose profile name for the observability stack.
const obsProfile = "observability"

// obsServices is the canonical list of service names in the "observability"
// Docker Compose profile. It must stay in sync with the services that carry
// `profiles: [observability]` in internal/config/templates/docker-compose.yml.tmpl.
// A drift-detection unit test in obs_test.go enforces this.
var obsServices = []string{
	"prometheus",
	"loki",
	"promtail",
	"otel-collector",
	"jaeger",
	"grafana",
}

// obsVolumeNames is the list of named volumes that belong exclusively to the
// observability stack. These are removed when `vibew obs down --volumes` is run.
var obsVolumeNames = []string{
	"prometheus-data",
	"loki-data",
	"grafana-data",
}

// ObsService orchestrates the "vibew obs up" and "vibew obs down" use cases.
// It runs the observability Compose profile (Prometheus + Grafana + Loki + Promtail)
// against the same compose project as the main dev stack.
type ObsService struct {
	compose   ports.ComposeRunner
	generator *generateapp.Service // optional; nil disables regeneration on up
}

// NewObsService creates an ObsService that uses a pre-existing generated compose
// file. Pass a non-nil generator to have Up regenerate config before starting.
func NewObsService(compose ports.ComposeRunner, generator *generateapp.Service) *ObsService {
	return &ObsService{compose: compose, generator: generator}
}

// ObsUpOptions holds options for the "vibew obs up" command.
type ObsUpOptions struct {
	// ConfigPath is the path to vibewarden.yaml used by the generator.
	// When empty the default "./vibewarden.yaml" is used.
	ConfigPath string
	// Verbose streams compose stderr to out during startup.
	Verbose bool
}

// ObsDownOptions holds options for the "vibew obs down" command.
type ObsDownOptions struct {
	// Volumes controls whether named volumes (Grafana dashboards, etc.) are
	// also removed. Destructive — callers should confirm with the user.
	Volumes bool
	// RemoveOrphans passes --remove-orphans to docker compose down.
	RemoveOrphans bool
	// Yes skips the interactive confirmation prompt when Volumes is true.
	Yes bool
	// In is the reader used for the y/N confirmation prompt. When nil, volume
	// deletion with IsTTY=false requires Yes=true.
	In io.Reader
	// IsTTY indicates whether the process is attached to an interactive terminal.
	IsTTY bool
	// ProjectName is the Docker Compose project name (e.g. "myapp"). It must
	// match the `name:` field in the generated compose file so that volume
	// removal constructs the correct "<project>_<volume>" reference. Obtain
	// this from cfg.ComposeProjectName(). When empty, volumes are not removed
	// even if Volumes is true.
	ProjectName string
}

// Up starts the observability Compose profile against the generated compose file.
// It optionally regenerates config first when a generator is wired. It prints a
// friendly advisory when the main sidecar does not appear to be running (Prometheus
// scrape targets will be empty), but does NOT return an error in that case.
func (s *ObsService) Up(ctx context.Context, cfg *config.Config, opts ObsUpOptions, out io.Writer) error {
	composeFile := filepath.Join(generatedOutputDir, "docker-compose.yml")

	// Optional regeneration step.
	if s.generator != nil {
		fmt.Fprintln(out, "Generating runtime configuration files...")
		if err := s.generator.Generate(ctx, cfg.ToGeneratorInput(), generatedOutputDir); err != nil {
			return fmt.Errorf("generating config: %w", err)
		}
		fmt.Fprintf(out, "Generated files written to %s\n", generatedOutputDir)
	}

	fmt.Fprintln(out, "Starting observability stack (Prometheus + Grafana)...")

	// Advisory: check whether the sidecar is running; if not, scrape targets
	// will be empty — this is not a hard failure.
	s.printSidecarAdvisory(ctx, composeFile, out)

	upOpts := ports.ComposeUpOptions{}
	if opts.Verbose {
		upOpts.Stderr = out
	}

	if err := s.compose.Up(ctx, composeFile, []string{obsProfile}, upOpts); err != nil {
		return fmt.Errorf("starting observability stack: %w", err)
	}

	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Observability stack started.")
	fmt.Fprintln(out, "Grafana:    http://localhost:3001")
	fmt.Fprintln(out, "Prometheus: http://localhost:9090")
	fmt.Fprintln(out, "Stop:       vibew obs down")

	return nil
}

// Down stops the observability Compose profile services.
func (s *ObsService) Down(ctx context.Context, opts ObsDownOptions, out io.Writer) error {
	composeFile := filepath.Join(generatedOutputDir, "docker-compose.yml")

	if opts.Volumes && !opts.Yes {
		if !opts.IsTTY {
			return ErrNonTTYVolumesRequiresYes
		}
		confirmed, err := confirmVolumeDeletion(opts.In, out)
		if err != nil {
			return fmt.Errorf("reading confirmation: %w", err)
		}
		if !confirmed {
			fmt.Fprintln(out, "Aborted. Volumes preserved.")
			return nil
		}
	}

	// Use service-targeted stop+rm instead of `compose down --profile observability`.
	// docker compose's --profile flag is an activation mechanism for `up`, not a
	// scope limiter for `down` — passing --profile to `down` stops ALL services
	// in the project regardless of profile. See ADR-097.
	result, err := s.compose.Down(ctx, composeFile, ports.ComposeDownOptions{
		Volumes:     opts.Volumes,
		Services:    obsServices,
		VolumeNames: obsVolumeNames,
		ProjectName: opts.ProjectName,
	})
	if err != nil {
		return fmt.Errorf("stopping observability stack: %w", err)
	}

	printObsDownSummary(result, opts.Volumes, out)
	return nil
}

// printObsDownSummary writes a one-line status summary for `vibew obs down`.
// It reports how many obs services were stopped rather than the generic
// "containers" wording, because the user stopped a named subset not the
// whole project.
func printObsDownSummary(result ports.DownResult, volumes bool, out io.Writer) {
	n := result.StoppedContainers
	if n == 0 && result.RemovedVolumes == 0 {
		fmt.Fprintln(out, "No observability services were running. Nothing to do.")
		return
	}
	if volumes {
		fmt.Fprintf(out, "Stopped %d obs services and removed %d volumes.\n", n, result.RemovedVolumes)
		return
	}
	fmt.Fprintf(out, "Stopped %d obs services. Volumes preserved — run 'vibew obs down -v' to also remove data.\n", n)
}

// printSidecarAdvisory checks whether the sidecar container is running and
// prints a helpful notice when it is not found. It never returns an error —
// missing sidecar is advisory only (Prometheus scrape targets will be empty).
func (s *ObsService) printSidecarAdvisory(ctx context.Context, composeFile string, out io.Writer) {
	containers, err := s.compose.PS(ctx, composeFile)
	if err != nil {
		// PS failure is non-fatal; skip the advisory.
		return
	}

	for _, c := range containers {
		if c.Service == sidecarServiceName {
			return // sidecar found — no advisory needed
		}
	}

	// Sidecar not found in running containers.
	fmt.Fprintln(out, "Advisory: sidecar not detected — Prometheus scrape targets will be empty")
	fmt.Fprintln(out, "until you run `vibew dev`. The observability services will still start.")
}
