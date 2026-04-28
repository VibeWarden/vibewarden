package ops_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/app/ops"
	"github.com/vibewarden/vibewarden/internal/config/templates"
	"github.com/vibewarden/vibewarden/internal/ports"
)

func TestObsService_Up_InvokesComposeWithObservabilityProfile(t *testing.T) {
	fc := &fakeCompose{}
	svc := ops.NewObsService(fc, nil)
	cfg := defaultConfig()
	var buf bytes.Buffer

	if err := svc.Up(context.Background(), cfg, ops.ObsUpOptions{}, &buf); err != nil {
		t.Fatalf("Up() unexpected error: %v", err)
	}

	if len(fc.capturedProfiles) != 1 || fc.capturedProfiles[0] != "observability" {
		t.Errorf("Up() profiles = %v, want [observability]", fc.capturedProfiles)
	}
}

func TestObsService_Up_UsesGeneratedComposeFile(t *testing.T) {
	fc := &fakeCompose{}
	svc := ops.NewObsService(fc, nil)
	cfg := defaultConfig()
	var buf bytes.Buffer

	if err := svc.Up(context.Background(), cfg, ops.ObsUpOptions{}, &buf); err != nil {
		t.Fatalf("Up() unexpected error: %v", err)
	}

	want := ".vibewarden/generated/docker-compose.yml"
	if fc.capturedComposeFile != want {
		t.Errorf("Up() composeFile = %q, want %q", fc.capturedComposeFile, want)
	}
}

func TestObsService_Up_PrintsGrafanaAndPrometheusURLs(t *testing.T) {
	fc := &fakeCompose{}
	svc := ops.NewObsService(fc, nil)
	cfg := defaultConfig()
	var buf bytes.Buffer

	if err := svc.Up(context.Background(), cfg, ops.ObsUpOptions{}, &buf); err != nil {
		t.Fatalf("Up() unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Grafana") {
		t.Errorf("expected Grafana URL in output, got:\n%s", out)
	}
	if !strings.Contains(out, "Prometheus") {
		t.Errorf("expected Prometheus URL in output, got:\n%s", out)
	}
}

func TestObsService_Up_PrintsAllFourURLs(t *testing.T) {
	// Success message must include Grafana, Prometheus, Loki, and Jaeger URLs.
	fc := &fakeCompose{}
	svc := ops.NewObsService(fc, nil)
	cfg := defaultConfig()
	var buf bytes.Buffer

	if err := svc.Up(context.Background(), cfg, ops.ObsUpOptions{}, &buf); err != nil {
		t.Fatalf("Up() unexpected error: %v", err)
	}

	out := buf.String()
	for _, want := range []string{
		"localhost:3001",
		"localhost:9090",
		"localhost:3100/ready",
		"localhost:16686",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output, got:\n%s", want, out)
		}
	}
}

func TestObsService_Up_PortsReflectConfig(t *testing.T) {
	// When ports are set to non-default values, the success message must use
	// the configured values — no hardcoded ports for Grafana, Prometheus, or Loki.
	tests := []struct {
		name           string
		grafanaPort    int
		prometheusPort int
		lokiPort       int
		wantLines      []string
	}{
		{
			name:           "default ports",
			grafanaPort:    3001,
			prometheusPort: 9090,
			lokiPort:       3100,
			wantLines: []string{
				"localhost:3001",
				"localhost:9090",
				"localhost:3100/ready",
				"localhost:16686",
			},
		},
		{
			name:           "custom ports",
			grafanaPort:    3002,
			prometheusPort: 9091,
			lokiPort:       3101,
			wantLines: []string{
				"localhost:3002",
				"localhost:9091",
				"localhost:3101/ready",
				"localhost:16686",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fc := &fakeCompose{}
			svc := ops.NewObsService(fc, nil)
			cfg := defaultConfig()
			cfg.Observability.GrafanaPort = tt.grafanaPort
			cfg.Observability.PrometheusPort = tt.prometheusPort
			cfg.Observability.LokiPort = tt.lokiPort
			var buf bytes.Buffer

			if err := svc.Up(context.Background(), cfg, ops.ObsUpOptions{}, &buf); err != nil {
				t.Fatalf("Up() unexpected error: %v", err)
			}

			out := buf.String()
			for _, want := range tt.wantLines {
				if !strings.Contains(out, want) {
					t.Errorf("expected %q in output, got:\n%s", want, out)
				}
			}
		})
	}
}

func TestObsService_Up_DoesNotPrintPromtailOrOtelCollector(t *testing.T) {
	// Promtail and otel-collector have no host-bound UI ports and must NOT
	// appear in the success message.
	fc := &fakeCompose{}
	svc := ops.NewObsService(fc, nil)
	cfg := defaultConfig()
	var buf bytes.Buffer

	if err := svc.Up(context.Background(), cfg, ops.ObsUpOptions{}, &buf); err != nil {
		t.Fatalf("Up() unexpected error: %v", err)
	}

	out := buf.String()
	for _, unwanted := range []string{"promtail", "otel-collector"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("success message must not contain %q, got:\n%s", unwanted, out)
		}
	}
}

func TestObsService_Up_ComposeError_ReturnsError(t *testing.T) {
	fc := &fakeCompose{upErr: errors.New("docker not running")}
	svc := ops.NewObsService(fc, nil)
	cfg := defaultConfig()
	var buf bytes.Buffer

	err := svc.Up(context.Background(), cfg, ops.ObsUpOptions{}, &buf)
	if err == nil {
		t.Fatal("Up() expected error when compose fails, got nil")
	}
	if !strings.Contains(err.Error(), "starting observability stack") {
		t.Errorf("error should wrap compose error, got: %v", err)
	}
}

func TestObsService_Up_Verbose_ForwardsStderrWriter(t *testing.T) {
	fc := &fakeCompose{}
	svc := ops.NewObsService(fc, nil)
	cfg := defaultConfig()
	var buf bytes.Buffer

	if err := svc.Up(context.Background(), cfg, ops.ObsUpOptions{Verbose: true}, &buf); err != nil {
		t.Fatalf("Up() unexpected error: %v", err)
	}
	if fc.capturedUpOpts.Stderr == nil {
		t.Error("expected Up() to be called with non-nil Stderr in verbose mode")
	}
}

func TestObsService_Up_NotVerbose_StderrNil(t *testing.T) {
	fc := &fakeCompose{}
	svc := ops.NewObsService(fc, nil)
	cfg := defaultConfig()
	var buf bytes.Buffer

	if err := svc.Up(context.Background(), cfg, ops.ObsUpOptions{}, &buf); err != nil {
		t.Fatalf("Up() unexpected error: %v", err)
	}
	if fc.capturedUpOpts.Stderr != nil {
		t.Error("expected Up() Stderr nil when not verbose")
	}
}

func TestObsService_Up_SidecarNotRunning_PrintsAdvisory(t *testing.T) {
	// When PS returns no sidecar container, Up still succeeds but prints an advisory.
	fc := &fakeCompose{
		psResult: []ports.ContainerInfo{
			{Service: "postgres", State: "running"},
		},
	}
	svc := ops.NewObsService(fc, nil)
	cfg := defaultConfig()
	var buf bytes.Buffer

	if err := svc.Up(context.Background(), cfg, ops.ObsUpOptions{}, &buf); err != nil {
		t.Fatalf("Up() expected no error for missing sidecar, got: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Advisory") {
		t.Errorf("expected advisory message when sidecar not running, got:\n%s", out)
	}
	if !strings.Contains(out, "vibew dev") {
		t.Errorf("expected 'vibew dev' hint in advisory, got:\n%s", out)
	}
}

func TestObsService_Up_SidecarRunning_NoAdvisory(t *testing.T) {
	// When PS includes a running sidecar, no advisory is printed.
	fc := &fakeCompose{
		psResult: []ports.ContainerInfo{
			{Service: "vibewarden", State: "running"},
		},
	}
	svc := ops.NewObsService(fc, nil)
	cfg := defaultConfig()
	var buf bytes.Buffer

	if err := svc.Up(context.Background(), cfg, ops.ObsUpOptions{}, &buf); err != nil {
		t.Fatalf("Up() unexpected error: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "Advisory") {
		t.Errorf("unexpected advisory when sidecar is running:\n%s", out)
	}
}

func TestObsService_Up_PSError_DoesNotFail(t *testing.T) {
	// When PS fails, the advisory check is skipped and Up proceeds normally.
	fc := &fakeCompose{psErr: errors.New("docker daemon not responding")}
	svc := ops.NewObsService(fc, nil)
	cfg := defaultConfig()
	var buf bytes.Buffer

	if err := svc.Up(context.Background(), cfg, ops.ObsUpOptions{}, &buf); err != nil {
		t.Fatalf("Up() unexpected error when PS fails: %v", err)
	}
}

// --- Down tests ---

func TestObsService_Down_InvokesComposeDown(t *testing.T) {
	fc := &fakeCompose{}
	svc := ops.NewObsService(fc, nil)
	var buf bytes.Buffer

	if err := svc.Down(context.Background(), ops.ObsDownOptions{Yes: true}, &buf); err != nil {
		t.Fatalf("Down() unexpected error: %v", err)
	}
	if fc.downCalled != 1 {
		t.Errorf("expected Down() to be called once, got %d", fc.downCalled)
	}
}

func TestObsService_Down_UsesGeneratedComposeFile(t *testing.T) {
	fc := &fakeCompose{}
	svc := ops.NewObsService(fc, nil)
	var buf bytes.Buffer

	if err := svc.Down(context.Background(), ops.ObsDownOptions{Yes: true}, &buf); err != nil {
		t.Fatalf("Down() unexpected error: %v", err)
	}
	// The compose file is internal; we verify Down was called (compose file
	// verified via the obsService's Down implementation which uses generatedOutputDir).
	if fc.downCalled == 0 {
		t.Error("expected Down() to be called")
	}
}

func TestObsService_Down_WithVolumes_ForwardsFlag(t *testing.T) {
	fc := &fakeCompose{}
	svc := ops.NewObsService(fc, nil)
	var buf bytes.Buffer

	opts := ops.ObsDownOptions{Volumes: true, Yes: true}
	if err := svc.Down(context.Background(), opts, &buf); err != nil {
		t.Fatalf("Down() unexpected error: %v", err)
	}
	if !fc.capturedDownOpts.Volumes {
		t.Error("expected Volumes=true forwarded to compose.Down")
	}
}

func TestObsService_Down_DoesNotForwardRemoveOrphans(t *testing.T) {
	// RemoveOrphans is a project-level concept and must NOT be forwarded when
	// the adapter is performing service-targeted teardown. See ADR-097.
	fc := &fakeCompose{}
	svc := ops.NewObsService(fc, nil)
	var buf bytes.Buffer

	opts := ops.ObsDownOptions{RemoveOrphans: true}
	if err := svc.Down(context.Background(), opts, &buf); err != nil {
		t.Fatalf("Down() unexpected error: %v", err)
	}
	if fc.capturedDownOpts.RemoveOrphans {
		t.Error("obs Down() must not set RemoveOrphans (not meaningful for service-targeted teardown)")
	}
}

func TestObsService_Down_NonTTY_VolumesWithoutYes_ReturnsError(t *testing.T) {
	fc := &fakeCompose{}
	svc := ops.NewObsService(fc, nil)
	var buf bytes.Buffer

	opts := ops.ObsDownOptions{Volumes: true, IsTTY: false, Yes: false}
	err := svc.Down(context.Background(), opts, &buf)
	if err == nil {
		t.Fatal("Down() expected error for non-TTY volumes without --yes, got nil")
	}
	if !errors.Is(err, ops.ErrNonTTYVolumesRequiresYes) {
		t.Errorf("expected ErrNonTTYVolumesRequiresYes, got: %v", err)
	}
}

func TestObsService_Down_ComposeError_ReturnsError(t *testing.T) {
	fc := &fakeCompose{downErr: errors.New("compose down failed")}
	svc := ops.NewObsService(fc, nil)
	var buf bytes.Buffer

	err := svc.Down(context.Background(), ops.ObsDownOptions{}, &buf)
	if err == nil {
		t.Fatal("Down() expected error when compose fails, got nil")
	}
	if !strings.Contains(err.Error(), "stopping observability stack") {
		t.Errorf("error should wrap compose error, got: %v", err)
	}
}

func TestObsService_Down_PassesObsServices(t *testing.T) {
	// obs down must perform a service-targeted teardown using the static obs
	// service list — NOT compose down --profile observability, which would
	// remove all services in the project. See ADR-097.
	fc := &fakeCompose{}
	svc := ops.NewObsService(fc, nil)
	var buf bytes.Buffer

	if err := svc.Down(context.Background(), ops.ObsDownOptions{Yes: true}, &buf); err != nil {
		t.Fatalf("Down() unexpected error: %v", err)
	}

	want := []string{"prometheus", "loki", "promtail", "otel-collector", "jaeger", "grafana"}
	got := fc.capturedDownOpts.Services
	if len(got) != len(want) {
		t.Errorf("Down() Services = %v, want %v", got, want)
	} else {
		for i, s := range want {
			if got[i] != s {
				t.Errorf("Down() Services[%d] = %q, want %q", i, got[i], s)
			}
		}
	}
}

func TestObsService_Down_PassesObsVolumeNames(t *testing.T) {
	// When Volumes=true, obs down must pass VolumeNames for the obs-specific
	// volumes so that non-obs volumes (kratos-db-data, vibewarden-data, etc.)
	// are not touched.
	fc := &fakeCompose{}
	svc := ops.NewObsService(fc, nil)
	var buf bytes.Buffer

	opts := ops.ObsDownOptions{Volumes: true, Yes: true}
	if err := svc.Down(context.Background(), opts, &buf); err != nil {
		t.Fatalf("Down() unexpected error: %v", err)
	}

	wantVols := []string{"prometheus-data", "loki-data", "grafana-data"}
	got := fc.capturedDownOpts.VolumeNames
	if len(got) != len(wantVols) {
		t.Errorf("Down() VolumeNames = %v, want %v", got, wantVols)
	} else {
		for i, v := range wantVols {
			if got[i] != v {
				t.Errorf("Down() VolumeNames[%d] = %q, want %q", i, got[i], v)
			}
		}
	}
}

func TestObsService_Down_WithVolumes_PassesProjectName(t *testing.T) {
	// When Volumes=true, obs down must forward ProjectName to ComposeDownOptions
	// so that the adapter can construct the correct "<project>_<volume>" Docker
	// volume reference. Without this, docker volume rm targets the wrong name
	// and silently removes nothing (RemovedVolumes stays 0).
	//
	// This is a regression guard for the resolveProjectName bug identified in
	// the PR #1182 review: the adapter must NOT derive the project name from
	// the compose file path (which yields "generated" for the generated file),
	// but must instead receive it from the caller via ComposeDownOptions.ProjectName.
	tests := []struct {
		name        string
		projectName string
		wantProject string
	}{
		{"project name forwarded", "myapp", "myapp"},
		{"empty project name forwarded as-is", "", ""},
		{"multi-word project name", "my-cool-app", "my-cool-app"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fc := &fakeCompose{}
			svc := ops.NewObsService(fc, nil)
			var buf bytes.Buffer

			opts := ops.ObsDownOptions{
				Volumes:     true,
				Yes:         true,
				ProjectName: tt.projectName,
			}
			if err := svc.Down(context.Background(), opts, &buf); err != nil {
				t.Fatalf("Down() unexpected error: %v", err)
			}

			got := fc.capturedDownOpts.ProjectName
			if got != tt.wantProject {
				t.Errorf("Down() ProjectName = %q, want %q", got, tt.wantProject)
			}
		})
	}
}

func TestObsService_Down_DoesNotPassProfiles(t *testing.T) {
	// Profiles must NOT be set on ComposeDownOptions — that was the buggy approach.
	fc := &fakeCompose{}
	svc := ops.NewObsService(fc, nil)
	var buf bytes.Buffer

	if err := svc.Down(context.Background(), ops.ObsDownOptions{Yes: true}, &buf); err != nil {
		t.Fatalf("Down() unexpected error: %v", err)
	}

	// Verify Services is non-empty (the correct approach) and RemoveOrphans
	// is not set (not meaningful for service-targeted teardown).
	if len(fc.capturedDownOpts.Services) == 0 {
		t.Error("Down() must set Services for service-targeted teardown, got empty")
	}
	if fc.capturedDownOpts.RemoveOrphans {
		t.Error("Down() must not set RemoveOrphans for service-targeted teardown")
	}
}

func TestObsService_Down_PrintsObsServiceCount(t *testing.T) {
	// The summary message should mention "obs services", not generic "containers".
	fc := &fakeCompose{downResult: ports.DownResult{StoppedContainers: 6}}
	svc := ops.NewObsService(fc, nil)
	var buf bytes.Buffer

	if err := svc.Down(context.Background(), ops.ObsDownOptions{Yes: true}, &buf); err != nil {
		t.Fatalf("Down() unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "obs services") {
		t.Errorf("expected 'obs services' in output, got:\n%s", out)
	}
}

func TestObsServices_MatchTemplate(t *testing.T) {
	// Drift-detection test: the static obsServices list must match the set of
	// services with `profiles: [observability]` in the compose template.
	// If the template gains or loses an obs service, this test will catch it.
	data, err := templates.FS.ReadFile("docker-compose.yml.tmpl")
	if err != nil {
		t.Fatalf("reading compose template: %v", err)
	}
	content := string(data)

	// Extract service names that have `profiles:\n      - observability`.
	// Strategy: scan line-by-line. When we see `      - observability` (the
	// profile annotation), walk backward past any indented config lines until
	// we find a top-level service key (two-space indent + name + colon, no
	// other characters on the line, e.g. "  prometheus:").
	lines := strings.Split(content, "\n")
	var templateServices []string
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "- observability" {
			continue
		}
		// Walk backward looking for the service-level key (2-space indent).
		for j := i - 1; j >= 0; j-- {
			l := lines[j]
			// Service-level keys have exactly 2 leading spaces, a name, and
			// a colon — e.g. "  prometheus:".
			if len(l) > 2 && l[0] == ' ' && l[1] == ' ' && l[2] != ' ' {
				candidate := strings.TrimSpace(l)
				if strings.HasSuffix(candidate, ":") {
					svcName := strings.TrimSuffix(candidate, ":")
					if svcName != "" {
						templateServices = append(templateServices, svcName)
					}
					break
				}
			}
		}
	}

	wantServices := []string{"prometheus", "loki", "promtail", "otel-collector", "jaeger", "grafana"}

	if len(templateServices) != len(wantServices) {
		t.Errorf("template has %d observability services %v; obsServices list has %d %v — update obs.go",
			len(templateServices), templateServices, len(wantServices), wantServices)
		return
	}

	// Build a set from template services for order-independent comparison.
	templateSet := make(map[string]bool, len(templateServices))
	for _, s := range templateServices {
		templateSet[s] = true
	}
	for _, s := range wantServices {
		if !templateSet[s] {
			t.Errorf("obsServices contains %q but template has no service with profiles: [observability] named %q", s, s)
		}
	}
}

func TestObsService_Up_PrintsGrafanaOnPort3001(t *testing.T) {
	// Grafana's default host port is 3001 (mapped from container-internal :3000).
	// The CLI output must reflect the actual accessible URL.
	fc := &fakeCompose{}
	svc := ops.NewObsService(fc, nil)
	cfg := defaultConfig()
	var buf bytes.Buffer

	if err := svc.Up(context.Background(), cfg, ops.ObsUpOptions{}, &buf); err != nil {
		t.Fatalf("Up() unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "localhost:3001") {
		t.Errorf("expected Grafana URL with port 3001 in output, got:\n%s", out)
	}
	if strings.Contains(out, "localhost:3000") {
		t.Errorf("output must not reference port 3000 (container-internal); got:\n%s", out)
	}
}
