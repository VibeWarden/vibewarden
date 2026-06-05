package config

import "testing"

func TestIsReleaseVersion(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"clean release", "0.20.0", true},
		{"minor version", "1.2.3", true},
		{"large numbers", "10.200.300", true},
		{"pre-release dash suffix", "0.20.0-rc.1", true},
		{"pre-release dot suffix", "0.20.0-beta.2", true},
		{"pre-release alpha", "1.0.0-alpha", true},
		{"leading v — not a release version", "v0.20.0", false},
		{"git-describe with leading v", "v0.20.0-5-gabc1234", false},
		{"dev", "dev", false},
		{"empty string", "", false},
		{"dirty suffix", "0.20.0-dirty", true}, // the regexp allows dash suffixes; dirty is treated as pre-release
		{"two-part version", "0.20", false},
		{"one-part version", "20", false},
		{"git describe no leading v", "0.20.0-5-gabc1234", true}, // only a leading v is the fallback trigger
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isReleaseVersion(tt.input)
			if got != tt.want {
				t.Errorf("isReleaseVersion(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestSidecarImageRef(t *testing.T) {
	tests := []struct {
		name           string
		version        string
		wantImage      string
		wantPullPolicy string
	}{
		{
			name:           "release version pins image tag",
			version:        "0.20.0",
			wantImage:      "ghcr.io/vibewarden/vibewarden:0.20.0",
			wantPullPolicy: "",
		},
		{
			name:           "pre-release version pins image tag",
			version:        "0.20.0-rc.1",
			wantImage:      "ghcr.io/vibewarden/vibewarden:0.20.0-rc.1",
			wantPullPolicy: "",
		},
		{
			name:           "dev falls back to latest with always",
			version:        "dev",
			wantImage:      "ghcr.io/vibewarden/vibewarden:latest",
			wantPullPolicy: "always",
		},
		{
			name:           "git-describe with leading v falls back",
			version:        "v0.20.0-5-gabc1234",
			wantImage:      "ghcr.io/vibewarden/vibewarden:latest",
			wantPullPolicy: "always",
		},
		{
			name:           "leading v on clean tag falls back (defensive)",
			version:        "v0.20.0",
			wantImage:      "ghcr.io/vibewarden/vibewarden:latest",
			wantPullPolicy: "always",
		},
		{
			name:           "empty string falls back",
			version:        "",
			wantImage:      "ghcr.io/vibewarden/vibewarden:latest",
			wantPullPolicy: "always",
		},
		{
			name:           "release 1.0.0 pins image tag",
			version:        "1.0.0",
			wantImage:      "ghcr.io/vibewarden/vibewarden:1.0.0",
			wantPullPolicy: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotImage, gotPullPolicy := SidecarImageRef(tt.version)
			if gotImage != tt.wantImage {
				t.Errorf("SidecarImageRef(%q) image = %q, want %q", tt.version, gotImage, tt.wantImage)
			}
			if gotPullPolicy != tt.wantPullPolicy {
				t.Errorf("SidecarImageRef(%q) pullPolicy = %q, want %q", tt.version, gotPullPolicy, tt.wantPullPolicy)
			}
		})
	}
}
