package funcs_test

import (
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/config/templates/funcs"
)

func TestFuncMap_mul(t *testing.T) {
	fm := funcs.FuncMap()
	mulFn, ok := fm["mul"]
	if !ok {
		t.Fatal("FuncMap() does not contain 'mul'")
	}
	fn, ok := mulFn.(func(int, int) int)
	if !ok {
		t.Fatalf("FuncMap()['mul'] has unexpected type %T", mulFn)
	}

	tests := []struct {
		name string
		a, b int
		want int
	}{
		{"positive values", 3, 4, 12},
		{"zero times anything", 0, 5, 0},
		{"negative multiplier", -2, 6, -12},
		{"identity", 1, 7, 7},
		{"retention days to hours (24 days)", 24, 24, 576},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fn(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("mul(%d, %d) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestFuncMap_healthcheckCmd(t *testing.T) {
	fm := funcs.FuncMap()
	hcFn, ok := fm["healthcheckCmd"]
	if !ok {
		t.Fatal("FuncMap() does not contain 'healthcheckCmd'")
	}
	fn, ok := hcFn.(func(string, int) string)
	if !ok {
		t.Fatalf("FuncMap()['healthcheckCmd'] has unexpected type %T", hcFn)
	}

	const port = 8080

	tests := []struct {
		name    string
		lang    string
		port    int
		wantSub string
		// wantSingleQuotedCode asserts that any code argument in the command
		// uses single quotes (not outer double quotes) so it is safe to embed
		// inside a YAML double-quoted ["CMD-SHELL", "..."] string.
		wantSingleQuotedCode bool
	}{
		{
			name:                 "python uses urllib",
			lang:                 "python",
			port:                 port,
			wantSub:              "urllib.request",
			wantSingleQuotedCode: true,
		},
		{
			name:                 "typescript uses node",
			lang:                 "typescript",
			port:                 port,
			wantSub:              "node -e",
			wantSingleQuotedCode: true,
		},
		{
			name:                 "javascript uses node",
			lang:                 "javascript",
			port:                 port,
			wantSub:              "node -e",
			wantSingleQuotedCode: true,
		},
		{
			name:    "go uses wget",
			lang:    "go",
			port:    port,
			wantSub: "wget",
		},
		{
			name:    "kotlin uses wget",
			lang:    "kotlin",
			port:    port,
			wantSub: "wget",
		},
		{
			name:    "unknown lang falls back to wget",
			lang:    "rust",
			port:    port,
			wantSub: "wget",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fn(tt.lang, tt.port)
			if !strings.Contains(got, tt.wantSub) {
				t.Errorf("healthcheckCmd(%q, %d) = %q, want it to contain %q",
					tt.lang, tt.port, got, tt.wantSub)
			}
			// For Python/Node the code argument must be wrapped in single quotes
			// so the command is safe inside a YAML double-quoted value.
			if tt.wantSingleQuotedCode && !strings.Contains(got, `'`) {
				t.Errorf("healthcheckCmd(%q, %d) = %q, expected single-quoted code argument for YAML safety",
					tt.lang, tt.port, got)
			}
		})
	}
}

func TestFuncMap_ReturnsNewMapEachCall(t *testing.T) {
	m1 := funcs.FuncMap()
	m2 := funcs.FuncMap()
	m1["extra"] = func() {}
	if _, found := m2["extra"]; found {
		t.Error("FuncMap() must return a new map each call; mutation of one affected another")
	}
}
