package cmd

import "strings"

// termState is the parser state of sanitizeTerminalText's rune state machine.
type termState int

const (
	// termNormal copies printable runes through and watches for ESC.
	termNormal termState = iota
	// termEscape has just consumed ESC and is deciding what follows.
	termEscape
	// termCSI is inside a CSI sequence, discarding until a final byte.
	termCSI
	// termString is inside a string sequence (OSC/DCS/SOS/PM/APC),
	// discarding until BEL or ST.
	termString
)

// sanitizeTerminalText strips terminal escape sequences and non-printable
// control characters from s, so untrusted subprocess output (for example
// Docker daemon stderr) cannot reposition the cursor, recolour, set the window
// title, or otherwise manipulate the user's terminal when it is echoed back.
//
// What is removed:
//   - ESC-introduced sequences: CSI (ESC [ … final 0x40–0x7E), string
//     sequences (OSC/DCS/SOS/PM/APC, terminated by BEL or ST), and short
//     two-byte forms such as ESC c.
//   - C0 control characters below 0x20, plus DEL (0x7F).
//   - C1 control characters 0x80–0x9F, which include the single-byte CSI
//     (0x9B) and OSC (0x9D) forms that many terminals honour.
//
// What is preserved: newline and tab (the rendering layer relies on them for
// line splitting and alignment), and all other printable runes including
// non-ASCII text. A newline also terminates any in-progress escape sequence,
// which bounds the damage of an unterminated OSC: without that, a single
// truncated sequence would swallow all remaining output.
//
// The function iterates runes rather than applying a regular expression: a
// regexp would pass invalid UTF-8 bytes through untouched, whereas ranging
// over the string decodes them to U+FFFD, so the result is always valid UTF-8.
func sanitizeTerminalText(s string) string {
	var b strings.Builder
	b.Grow(len(s))

	state := termNormal
	// escPending tracks an ESC seen inside a string sequence, which may be the
	// first half of ST (ESC \).
	escPending := false

	for _, r := range s {
		// A newline always ends any pending sequence and is emitted, so a
		// malformed sequence cannot consume the rest of the input.
		if r == '\n' {
			state = termNormal
			escPending = false
			b.WriteRune('\n')
			continue
		}

		switch state {
		case termNormal:
			switch {
			case r == 0x1B: // ESC
				state = termEscape
			case r == '\t':
				b.WriteRune(r)
			case r < 0x20 || r == 0x7F: // C0 controls and DEL
			case r >= 0x80 && r <= 0x9F: // C1 controls (incl. CSI 0x9B, OSC 0x9D)
			default:
				b.WriteRune(r)
			}

		case termEscape:
			switch r {
			case '[':
				state = termCSI
			case ']', 'P', 'X', '^', '_': // OSC, DCS, SOS, PM, APC
				state = termString
				escPending = false
			default:
				// Two-byte escape (ESC c, ESC 7, …) or an intermediate byte;
				// drop it and resume normal copying.
				state = termNormal
			}

		case termCSI:
			// Parameter and intermediate bytes are 0x20–0x3F; the sequence
			// ends at the first final byte in 0x40–0x7E.
			if r >= 0x40 && r <= 0x7E {
				state = termNormal
			}

		case termString:
			switch {
			case escPending:
				// ESC \ is ST; any other ESC-prefixed pair stays inside the
				// string sequence.
				escPending = false
				if r == '\\' {
					state = termNormal
				}
			case r == 0x07: // BEL
				state = termNormal
			case r == 0x1B: // ESC — possible start of ST
				escPending = true
			}
		}
	}

	return b.String()
}
