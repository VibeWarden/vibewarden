package cmd

import (
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/mcp"
)

// TestMCPLongHelp_ListsEveryRegisteredTool is the drift guard. If a new tool
// gets added to internal/mcp.RegisterDefaultTools but is missing from the
// Long help output, this test fails. It is the reason the Long string is
// generated at runtime instead of hard-coded — before #813 the hard-coded
// list had drifted to show 4 tools while 13+ were actually registered.
func TestMCPLongHelp_ListsEveryRegisteredTool(t *testing.T) {
	long := buildMCPLongHelp()

	// Build a reference server the same way the CLI does, then enumerate.
	ref := mcp.NewServer("test", "test", slog.New(slog.NewTextHandler(io.Discard, nil)))
	mcp.RegisterDefaultTools(ref)

	if got := len(ref.Tools()); got == 0 {
		t.Fatal("RegisterDefaultTools registered zero tools — guard test would pass vacuously")
	}

	for _, td := range ref.Tools() {
		if !strings.Contains(long, td.Name) {
			t.Errorf("Long help is missing registered tool %q", td.Name)
		}
	}
}

// TestMCPFirstSentence exercises the description-trimming used in the
// compact tool listing, covering the single-sentence, multi-sentence, and
// no-delimiter cases.
func TestMCPFirstSentence(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"single sentence with period", "Check the sidecar.", "Check the sidecar."},
		{"two sentences splits at first", "Check the sidecar. Returns HTTP status.", "Check the sidecar."},
		{"no sentence delimiter", "Check the sidecar", "Check the sidecar"},
		{"leading whitespace trimmed", "  Check the sidecar.  ", "Check the sidecar."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mcpFirstSentence(tt.in); got != tt.want {
				t.Errorf("mcpFirstSentence(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
