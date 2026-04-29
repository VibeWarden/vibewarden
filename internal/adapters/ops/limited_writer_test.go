package ops

import (
	"bytes"
	"strings"
	"testing"
)

// TestLimitedWriter_CapEnforced verifies that limitedWriter stops accepting
// bytes once the underlying buffer reaches cap, never growing beyond it.
func TestLimitedWriter_CapEnforced(t *testing.T) {
	const cap = 64 * 1024 // 64 KiB — matches stderrCapCap

	var buf bytes.Buffer
	w := &limitedWriter{buf: &buf, cap: cap}

	// Write cap bytes in one shot.
	first := bytes.Repeat([]byte("a"), cap)
	n, err := w.Write(first)
	if err != nil {
		t.Fatalf("Write() returned unexpected error: %v", err)
	}
	if n != cap {
		t.Fatalf("Write() reported n=%d, want %d", n, cap)
	}
	if buf.Len() != cap {
		t.Fatalf("buf.Len()=%d, want %d after first write", buf.Len(), cap)
	}

	// Write additional bytes — they must be silently discarded.
	extra := bytes.Repeat([]byte("b"), 1024)
	n, err = w.Write(extra)
	if err != nil {
		t.Fatalf("Write() returned unexpected error on overflow: %v", err)
	}
	if n != len(extra) {
		t.Fatalf("Write() reported n=%d, want %d (must claim full write)", n, len(extra))
	}
	if buf.Len() != cap {
		t.Fatalf("buf.Len()=%d, want %d after overflow write (no growth)", buf.Len(), cap)
	}
}

// TestLimitedWriter_DaemonSignalDetectable verifies that when the
// daemon-unavailable message arrives in the first chunk (the realistic case),
// it is still present in the buffer after subsequent overflow writes.
func TestLimitedWriter_DaemonSignalDetectable(t *testing.T) {
	const cap = 64 * 1024

	var buf bytes.Buffer
	w := &limitedWriter{buf: &buf, cap: cap}

	// Write the daemon-unavailable signature as the first chunk.
	signal := "Cannot connect to the Docker daemon"
	n, err := w.Write([]byte(signal))
	if err != nil || n != len(signal) {
		t.Fatalf("Write(signal) n=%d err=%v", n, err)
	}

	// Flood with more than cap bytes of noise.
	noise := bytes.Repeat([]byte("x"), cap+512)
	_, _ = w.Write(noise)

	// Buffer must not exceed cap.
	if buf.Len() > cap {
		t.Fatalf("buf.Len()=%d exceeds cap=%d", buf.Len(), cap)
	}

	// Daemon-unavailable signature must still be detectable.
	if !strings.Contains(strings.ToLower(buf.String()), strings.ToLower(signal)) {
		t.Error("daemon-unavailable signal not detectable in buffer after overflow writes")
	}
}

// TestLimitedWriter_PartialWriteAtBoundary verifies that when a write would
// cross the cap boundary, only the bytes that fit are stored and the full
// input length is still reported as written.
func TestLimitedWriter_PartialWriteAtBoundary(t *testing.T) {
	const cap = 10

	var buf bytes.Buffer
	w := &limitedWriter{buf: &buf, cap: cap}

	// Pre-fill to within 4 bytes of the cap.
	_, _ = w.Write([]byte("aaaaaa")) // 6 bytes

	// Write 8 bytes — only 4 should be stored.
	payload := []byte("12345678")
	n, err := w.Write(payload)
	if err != nil {
		t.Fatalf("Write() returned unexpected error: %v", err)
	}
	if n != len(payload) {
		t.Fatalf("Write() n=%d, want %d (must report full len)", n, len(payload))
	}
	if buf.Len() != cap {
		t.Fatalf("buf.Len()=%d, want %d (exactly at cap)", buf.Len(), cap)
	}
	if !strings.HasSuffix(buf.String(), "1234") {
		t.Errorf("buf tail = %q, want suffix \"1234\"", buf.String())
	}
}
