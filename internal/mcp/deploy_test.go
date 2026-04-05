package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Deploy spec generators
// ---------------------------------------------------------------------------

func TestPrepareDockerSpec(t *testing.T) {
	spec := PrepareDockerSpec(3000)

	if spec.Platform != string(PlatformDocker) {
		t.Errorf("Platform = %q, want %q", spec.Platform, PlatformDocker)
	}
	if spec.AppPort != 3000 {
		t.Errorf("AppPort = %d, want 3000", spec.AppPort)
	}
	if spec.Sidecar.Image != SidecarImage {
		t.Errorf("Sidecar.Image = %q, want %q", spec.Sidecar.Image, SidecarImage)
	}
	if !strings.Contains(spec.ConfigFileContent, "port: 3000") {
		t.Error("ConfigFileContent does not contain upstream port 3000")
	}
	if !strings.Contains(spec.PlatformSpec, "vibewarden:") {
		t.Error("PlatformSpec does not reference the vibewarden service")
	}
	if spec.HealthCheckURL == "" {
		t.Error("HealthCheckURL must not be empty")
	}
	if len(spec.Notes) == 0 {
		t.Error("Notes must not be empty")
	}
	if len(spec.Sidecar.Volumes) == 0 {
		t.Error("Sidecar.Volumes must not be empty for docker platform")
	}
}

func TestPrepareRailwaySpec(t *testing.T) {
	spec := PrepareRailwaySpec(4000)

	if spec.Platform != string(PlatformRailway) {
		t.Errorf("Platform = %q, want %q", spec.Platform, PlatformRailway)
	}
	if spec.AppPort != 4000 {
		t.Errorf("AppPort = %d, want 4000", spec.AppPort)
	}
	if spec.Sidecar.Image != SidecarImage {
		t.Errorf("Sidecar.Image = %q, want %q", spec.Sidecar.Image, SidecarImage)
	}
	if !strings.Contains(spec.ConfigFileContent, "port: 4000") {
		t.Error("ConfigFileContent does not contain upstream port 4000")
	}
	if !strings.Contains(spec.PlatformSpec, "railway.internal") {
		t.Error("PlatformSpec does not mention Railway internal networking")
	}
	// Railway spec should mention internal networking in notes too.
	found := false
	for _, note := range spec.Notes {
		if strings.Contains(note, "railway.internal") {
			found = true
			break
		}
	}
	if !found {
		t.Error("Notes should mention railway.internal private networking")
	}
}

func TestPrepareFlyioSpec(t *testing.T) {
	spec := PrepareFlyioSpec(8080)

	if spec.Platform != string(PlatformFlyio) {
		t.Errorf("Platform = %q, want %q", spec.Platform, PlatformFlyio)
	}
	if spec.AppPort != 8080 {
		t.Errorf("AppPort = %d, want 8080", spec.AppPort)
	}
	if spec.Sidecar.Image != SidecarImage {
		t.Errorf("Sidecar.Image = %q, want %q", spec.Sidecar.Image, SidecarImage)
	}
	if !strings.Contains(spec.PlatformSpec, "[processes]") {
		t.Error("fly.toml spec must use processes syntax")
	}
	if !strings.Contains(spec.PlatformSpec, "vibewarden") {
		t.Error("fly.toml spec must reference the vibewarden process")
	}
	if len(spec.Sidecar.Command) == 0 {
		t.Error("Fly.io spec must set Sidecar.Command")
	}
}

func TestBuildVibewardenYAML(t *testing.T) {
	tests := []struct {
		name     string
		appPort  int
		contains string
	}{
		{"port 3000", 3000, "port: 3000"},
		{"port 8080", 8080, "port: 8080"},
		{"tls enabled", 3000, "tls:"},
		{"security headers enabled", 3000, "security_headers:"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildVibewardenYAML(tt.appPort)
			if !strings.Contains(got, tt.contains) {
				t.Errorf("buildVibewardenYAML(%d) does not contain %q", tt.appPort, tt.contains)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// parsePort
// ---------------------------------------------------------------------------

func TestParsePort(t *testing.T) {
	tests := []struct {
		name    string
		input   any
		want    int
		wantErr bool
	}{
		{"float64 from JSON", float64(3000), 3000, false},
		{"string numeric", "8080", 8080, false},
		{"string non-numeric", "abc", 0, true},
		{"nil", nil, 0, true},
		{"unexpected type", true, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parsePort(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parsePort(%v) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("parsePort(%v) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// handlePrepareDeploy
// ---------------------------------------------------------------------------

func TestHandlePrepareDeploy(t *testing.T) {
	tests := []struct {
		name         string
		params       map[string]any
		wantPlatform string
		wantErr      bool
	}{
		{
			name:         "docker platform string port",
			params:       map[string]any{"platform": "docker", "app_port": "3000"},
			wantPlatform: "docker",
		},
		{
			name:         "docker platform numeric port",
			params:       map[string]any{"platform": "docker", "app_port": float64(3000)},
			wantPlatform: "docker",
		},
		{
			name:         "railway platform",
			params:       map[string]any{"platform": "railway", "app_port": "4000"},
			wantPlatform: "railway",
		},
		{
			name:         "flyio platform",
			params:       map[string]any{"platform": "flyio", "app_port": "8080"},
			wantPlatform: "flyio",
		},
		{
			name:    "unsupported platform",
			params:  map[string]any{"platform": "heroku", "app_port": "3000"},
			wantErr: true,
		},
		{
			name:    "invalid port",
			params:  map[string]any{"platform": "docker", "app_port": "notaport"},
			wantErr: true,
		},
		{
			name:    "port out of range",
			params:  map[string]any{"platform": "docker", "app_port": "99999"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, _ := json.Marshal(tt.params)
			items, err := handlePrepareDeploy(context.Background(), raw)
			if (err != nil) != tt.wantErr {
				t.Errorf("handlePrepareDeploy() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}
			if len(items) == 0 {
				t.Fatal("expected at least one content item")
			}
			// Decode the JSON spec returned in the text item.
			var spec DeploySpec
			if err := json.Unmarshal([]byte(items[0].Text), &spec); err != nil {
				t.Fatalf("cannot unmarshal spec JSON: %v\nraw: %s", err, items[0].Text)
			}
			if spec.Platform != tt.wantPlatform {
				t.Errorf("spec.Platform = %q, want %q", spec.Platform, tt.wantPlatform)
			}
			if spec.Sidecar.Image != SidecarImage {
				t.Errorf("spec.Sidecar.Image = %q, want %q", spec.Sidecar.Image, SidecarImage)
			}
			if spec.HealthCheckURL == "" {
				t.Error("spec.HealthCheckURL must not be empty")
			}
			if spec.ConfigFileContent == "" {
				t.Error("spec.ConfigFileContent must not be empty")
			}
		})
	}
}

func TestHandlePrepareDeploy_InvalidArgs(t *testing.T) {
	_, err := handlePrepareDeploy(context.Background(), json.RawMessage(`{invalid`))
	if err == nil {
		t.Error("expected an error for invalid JSON arguments")
	}
}

// ---------------------------------------------------------------------------
// handleVerifyDeploy
// ---------------------------------------------------------------------------

func TestHandleVerifyDeploy_Healthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/_vibewarden/health" {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	params, _ := json.Marshal(map[string]string{"url": srv.URL})
	items, err := handleVerifyDeploy(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("expected at least one content item")
	}
	if !strings.Contains(items[0].Text, "healthy") {
		t.Errorf("expected 'healthy' in output, got: %s", items[0].Text)
	}
}

func TestHandleVerifyDeploy_Unhealthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	params, _ := json.Marshal(map[string]string{"url": srv.URL})
	items, err := handleVerifyDeploy(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("expected at least one content item")
	}
	if !strings.Contains(items[0].Text, "not healthy") {
		t.Errorf("expected 'not healthy' in output, got: %s", items[0].Text)
	}
}

func TestHandleVerifyDeploy_Unreachable(t *testing.T) {
	// Use an address that is guaranteed to be unreachable.
	params, _ := json.Marshal(map[string]string{"url": "http://127.0.0.1:19999"})
	items, err := handleVerifyDeploy(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("expected at least one content item")
	}
	if !strings.Contains(items[0].Text, "unreachable") {
		t.Errorf("expected 'unreachable' in output, got: %s", items[0].Text)
	}
}

func TestHandleVerifyDeploy_MissingURL(t *testing.T) {
	params, _ := json.Marshal(map[string]string{})
	_, err := handleVerifyDeploy(context.Background(), params)
	if err == nil {
		t.Error("expected an error when url is missing")
	}
}

func TestHandleVerifyDeploy_InvalidArgs(t *testing.T) {
	_, err := handleVerifyDeploy(context.Background(), json.RawMessage(`{invalid`))
	if err == nil {
		t.Error("expected an error for invalid JSON arguments")
	}
}

func TestHandleVerifyDeploy_TrailingSlash(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/_vibewarden/health" {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	// Trailing slash in URL should be stripped before appending health path.
	params, _ := json.Marshal(map[string]string{"url": srv.URL + "/"})
	items, err := handleVerifyDeploy(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(items[0].Text, "healthy") {
		t.Errorf("expected 'healthy' in output, got: %s", items[0].Text)
	}
}

// ---------------------------------------------------------------------------
// handleGetDeployLogs
// ---------------------------------------------------------------------------

func TestHandleGetDeployLogs(t *testing.T) {
	tests := []struct {
		name     string
		params   map[string]string
		contains []string
	}{
		{
			name:   "no params",
			params: map[string]string{},
			contains: []string{
				"vibew deploy logs",
				"docker compose logs",
			},
		},
		{
			name:   "with url and lines",
			params: map[string]string{"url": "https://my-app.fly.dev", "lines": "100"},
			contains: []string{
				"vibew deploy logs",
				"100",
				"https://my-app.fly.dev",
			},
		},
		{
			name:   "fly.io hint included",
			params: map[string]string{"url": "https://my-app.fly.dev"},
			contains: []string{
				"fly logs",
			},
		},
		{
			name:   "railway hint included",
			params: map[string]string{},
			contains: []string{
				"railway logs",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, _ := json.Marshal(tt.params)
			items, err := handleGetDeployLogs(context.Background(), raw)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(items) == 0 {
				t.Fatal("expected at least one content item")
			}
			for _, want := range tt.contains {
				if !strings.Contains(items[0].Text, want) {
					t.Errorf("output does not contain %q\ngot:\n%s", want, items[0].Text)
				}
			}
		})
	}
}

func TestHandleGetDeployLogs_InvalidArgs(t *testing.T) {
	_, err := handleGetDeployLogs(context.Background(), json.RawMessage(`{invalid`))
	if err == nil {
		t.Error("expected an error for invalid JSON arguments")
	}
}

// ---------------------------------------------------------------------------
// RegisterDefaultTools — ensure deploy tools are included
// ---------------------------------------------------------------------------

func TestRegisterDefaultTools_IncludesDeployTools(t *testing.T) {
	srv := newTestServer()
	RegisterDefaultTools(srv)

	deployTools := []string{
		"vibewarden_prepare_deploy",
		"vibewarden_verify_deploy",
		"vibewarden_get_deploy_logs",
	}

	for _, name := range deployTools {
		if _, ok := srv.handlers[name]; !ok {
			t.Errorf("deploy tool %q not registered", name)
		}
	}
}
