package config_test

import (
	"testing"

	"github.com/vibewarden/vibewarden/internal/config"
)

func TestParseMemLimit(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    int64
		wantErr bool
	}{
		{"vibewarden spelling MB", "512MB", 536870912, false},
		{"docker shorthand M", "512M", 536870912, false},
		{"docker shorthand lowercase m", "512m", 536870912, false},
		{"vibewarden spelling GB", "1GB", 1073741824, false},
		{"docker shorthand g", "1g", 1073741824, false},
		{"docker shorthand K", "512K", 524288, false},
		{"plain byte count", "536870912", 536870912, false},
		{"explicit zero", "0", 0, false},
		{"empty string", "", 0, false},
		{"whitespace padded", "  512M  ", 536870912, false},
		{"negative", "-1MB", 0, true},
		{"unknown unit", "512X", 0, true},
		{"non-numeric", "abc", 0, true},
		{"unit only", "M", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := config.ParseMemLimit(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseMemLimit(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if err == nil && got != tt.want {
				t.Errorf("ParseMemLimit(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestServerConfig_ResourceLimits(t *testing.T) {
	tests := []struct {
		name string
		srv  config.ServerConfig
		want config.ComposeResourceLimits
	}{
		{
			name: "documented defaults",
			srv:  config.ServerConfig{MemLimit: "512MB", CPULimit: 1.0, PidsLimit: 200},
			want: config.ComposeResourceLimits{
				MemLimitBytes:   "536870912",
				MemLimitDisplay: "512MB",
				GoMemLimit:      "483183820B",
				CPULimit:        "1",
				PidsLimit:       "200",
			},
		},
		{
			name: "docker shorthand is normalised to bytes",
			srv:  config.ServerConfig{MemLimit: "512M", CPULimit: 0.5, PidsLimit: 64},
			want: config.ComposeResourceLimits{
				MemLimitBytes:   "536870912",
				MemLimitDisplay: "512M",
				GoMemLimit:      "483183820B",
				CPULimit:        "0.5",
				PidsLimit:       "64",
			},
		},
		{
			name: "memory cap disabled drops GOMEMLIMIT too",
			srv:  config.ServerConfig{MemLimit: "0", CPULimit: 1.0, PidsLimit: 200},
			want: config.ComposeResourceLimits{CPULimit: "1", PidsLimit: "200"},
		},
		{
			name: "empty mem limit disables the cap",
			srv:  config.ServerConfig{MemLimit: "", CPULimit: 1.0, PidsLimit: 200},
			want: config.ComposeResourceLimits{CPULimit: "1", PidsLimit: "200"},
		},
		{
			name: "cpu cap disabled",
			srv:  config.ServerConfig{MemLimit: "512MB", CPULimit: 0, PidsLimit: 200},
			want: config.ComposeResourceLimits{
				MemLimitBytes:   "536870912",
				MemLimitDisplay: "512MB",
				GoMemLimit:      "483183820B",
				PidsLimit:       "200",
			},
		},
		{
			name: "pids cap disabled",
			srv:  config.ServerConfig{MemLimit: "512MB", CPULimit: 1.0, PidsLimit: 0},
			want: config.ComposeResourceLimits{
				MemLimitBytes:   "536870912",
				MemLimitDisplay: "512MB",
				GoMemLimit:      "483183820B",
				CPULimit:        "1",
			},
		},
		{
			name: "all caps disabled yields an all-empty struct",
			srv:  config.ServerConfig{MemLimit: "0", CPULimit: 0, PidsLimit: 0},
			want: config.ComposeResourceLimits{},
		},
		{
			name: "zero-value ServerConfig yields an all-empty struct",
			srv:  config.ServerConfig{},
			want: config.ComposeResourceLimits{},
		},
		{
			name: "malformed mem limit blanks the memory fields",
			srv:  config.ServerConfig{MemLimit: "512X", CPULimit: 1.0, PidsLimit: 200},
			want: config.ComposeResourceLimits{CPULimit: "1", PidsLimit: "200"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.srv.ResourceLimits()
			if got != tt.want {
				t.Errorf("ResourceLimits() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// TestServerConfig_ResourceLimits_GoMemLimitHeadroom pins the GOMEMLIMIT
// derivation: it must be strictly below the container cap, since the whole
// point is to let the GC react before the kernel OOM-kills the sidecar.
func TestServerConfig_ResourceLimits_GoMemLimitHeadroom(t *testing.T) {
	for _, memLimit := range []string{"128MB", "512MB", "1GB", "4GB"} {
		t.Run(memLimit, func(t *testing.T) {
			srv := config.ServerConfig{MemLimit: memLimit}
			limits := srv.ResourceLimits()

			capBytes, err := config.ParseMemLimit(memLimit)
			if err != nil {
				t.Fatalf("ParseMemLimit(%q): %v", memLimit, err)
			}
			goBytes, err := config.ParseMemLimit(limits.GoMemLimit)
			if err != nil {
				t.Fatalf("ParseMemLimit(%q): %v", limits.GoMemLimit, err)
			}
			if goBytes >= capBytes {
				t.Errorf("GOMEMLIMIT %d must be below the container cap %d", goBytes, capBytes)
			}
			if goBytes < capBytes/2 {
				t.Errorf("GOMEMLIMIT %d wastes more than half of the %d byte cap", goBytes, capBytes)
			}
		})
	}
}
