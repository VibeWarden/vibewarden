package cmd

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// TestSanitizeTerminalText covers the escape-sequence and control-character
// classes that untrusted subprocess stderr can carry.
func TestSanitizeTerminalText(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "plain ascii passes through",
			input: "Cannot connect to the Docker daemon at unix:///var/run/docker.sock",
			want:  "Cannot connect to the Docker daemon at unix:///var/run/docker.sock",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "csi colour sequence removed",
			input: "\x1b[31mred\x1b[0m text",
			want:  "red text",
		},
		{
			name:  "csi cursor move with multiple parameters removed",
			input: "before\x1b[2;10Hafter",
			want:  "beforeafter",
		},
		{
			name:  "csi erase display removed",
			input: "\x1b[2Jcleared",
			want:  "cleared",
		},
		{
			name:  "osc window title terminated by bel removed",
			input: "a\x1b]0;pwned\x07b",
			want:  "ab",
		},
		{
			name:  "osc terminated by st removed",
			input: "a\x1b]0;pwned\x1b\\b",
			want:  "ab",
		},
		{
			name:  "osc containing esc that is not st stays consumed",
			input: "a\x1b]0;pw\x1bXned\x07b",
			want:  "ab",
		},
		{
			name:  "unterminated osc stops at newline",
			input: "a\x1b]0;pwned\nsecond line",
			want:  "a\nsecond line",
		},
		{
			name:  "unterminated csi stops at newline",
			input: "a\x1b[38;5\nsecond line",
			want:  "a\nsecond line",
		},
		{
			name:  "dcs sequence removed",
			input: "a\x1bPq;stuff\x1b\\b",
			want:  "ab",
		},
		{
			name:  "apc sequence removed",
			input: "a\x1b_payload\x1b\\b",
			want:  "ab",
		},
		{
			name:  "two byte escape removed",
			input: "a\x1bcb",
			want:  "ab",
		},
		{
			name:  "lone trailing escape removed",
			input: "tail\x1b",
			want:  "tail",
		},
		{
			name:  "bare carriage return removed",
			input: "overwrite\rme",
			want:  "overwriteme",
		},
		{
			name:  "bare bel removed",
			input: "ding\x07dong",
			want:  "dingdong",
		},
		{
			name:  "backspace removed",
			input: "abc\x08d",
			want:  "abcd",
		},
		{
			name:  "del removed",
			input: "a\x7fb",
			want:  "ab",
		},
		{
			name:  "c1 csi removed",
			input: "a\u009b31mred",
			want:  "a31mred",
		},
		{
			name: "c1 osc and st removed",
			// U+009D is the single-byte OSC and U+009C the single-byte ST.
			// Both are C1 controls, so both are dropped on sight; the payload
			// between them is plain text and survives.
			input: "a\u009d0;title\u009cb",
			want:  "a0;titleb",
		},
		{
			name:  "newline and tab preserved",
			input: "line one\n\tindented",
			want:  "line one\n\tindented",
		},
		{
			name:  "non-ascii printable text preserved",
			input: "café naïve 日本語",
			want:  "café naïve 日本語",
		},
		{
			name:  "non-breaking space preserved",
			input: "a\u00a0b",
			want:  "a\u00a0b",
		},
		{
			name:  "escape only input collapses to empty",
			input: "\x1b[31m\x1b[0m\x1b]0;t\x07",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeTerminalText(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeTerminalText(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestSanitizeTerminalText_C1Controls verifies that every C1 control rune,
// including the single-byte CSI (U+009B) and OSC (U+009D) forms that many
// terminals honour, is dropped.
func TestSanitizeTerminalText_C1Controls(t *testing.T) {
	for r := rune(0x80); r <= 0x9F; r++ {
		got := sanitizeTerminalText("a" + string(r) + "b")
		if got != "ab" {
			t.Errorf("C1 control U+%04X not stripped: got %q", r, got)
		}
	}
}

// TestSanitizeTerminalText_C0Controls verifies that all C0 controls except
// newline, tab and ESC (which has dedicated table cases) are dropped.
func TestSanitizeTerminalText_C0Controls(t *testing.T) {
	for r := rune(0x00); r < 0x20; r++ {
		if r == '\n' || r == '\t' || r == 0x1B {
			continue
		}
		got := sanitizeTerminalText("a" + string(r) + "b")
		if got != "ab" {
			t.Errorf("C0 control U+%04X not stripped: got %q", r, got)
		}
	}
}

// TestSanitizeTerminalText_InvalidUTF8 verifies that arbitrary bytes (Docker
// stderr is not guaranteed to be UTF-8) produce valid UTF-8 output. Ranging
// over the string decodes invalid bytes to U+FFFD, which is printable and
// therefore kept, while escape sequences around them are still stripped.
func TestSanitizeTerminalText_InvalidUTF8(t *testing.T) {
	input := "ok" + string([]byte{0xff, 0xfe}) + "\x1b[31mend"
	got := sanitizeTerminalText(input)

	if !utf8.ValidString(got) {
		t.Errorf("output is not valid UTF-8: %q", got)
	}
	if !strings.HasPrefix(got, "ok") || !strings.HasSuffix(got, "end") {
		t.Errorf("printable content lost: got %q", got)
	}
	if strings.ContainsRune(got, 0x1B) {
		t.Errorf("escape byte survived: %q", got)
	}
}

// TestSanitizeTerminalText_NoControlBytesSurvive is a broad property check:
// for every adversarial input, the output must not contain ESC or any other
// stripped control byte.
func TestSanitizeTerminalText_NoControlBytesSurvive(t *testing.T) {
	inputs := []string{
		"\x1b[2J\x1b[H\x1b]0;evil\x07you are now root",
		"\x1b[?1049h",
		"\x1b(B\x1b[m",
		"\u009b31m",
		"a\rb\x08c\x07d\x7fe",
		string([]byte{0x1b, 0x5d, 0x30, 0x3b, 0xff, 0x07}),
	}
	for _, in := range inputs {
		got := sanitizeTerminalText(in)
		for _, bad := range []rune{0x1B, 0x07, 0x08, '\r', 0x7F, 0x9B} {
			if strings.ContainsRune(got, bad) {
				t.Errorf("sanitizeTerminalText(%q) leaked control U+%04X: %q", in, bad, got)
			}
		}
	}
}
