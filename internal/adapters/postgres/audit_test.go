// Package postgres contains unit tests for the PostgreSQL audit adapter.
// These tests do not require a real database — they cover compile-time
// interface satisfaction and pure-logic helpers.
package postgres

import (
	"testing"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// Compile-time check: AuditAdapter must satisfy ports.AuditLogger.
var _ ports.AuditLogger = (*AuditAdapter)(nil)

func TestNullableJSON_NilInput(t *testing.T) {
	result := nullableJSON(nil)
	if result.Valid {
		t.Error("nullableJSON(nil) should return Valid=false")
	}
	if result.String != "" {
		t.Errorf("nullableJSON(nil).String = %q, want empty", result.String)
	}
}

func TestNullableJSON_EmptySlice(t *testing.T) {
	result := nullableJSON([]byte{})
	if result.Valid {
		t.Error("nullableJSON(empty) should return Valid=false")
	}
	if result.String != "" {
		t.Errorf("nullableJSON(empty).String = %q, want empty", result.String)
	}
}

func TestNullableJSON_ValidJSON(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
		want  string
	}{
		{
			name:  "simple object",
			input: []byte(`{"key":"value"}`),
			want:  `{"key":"value"}`,
		},
		{
			name:  "array",
			input: []byte(`[1,2,3]`),
			want:  `[1,2,3]`,
		},
		{
			name:  "null json value",
			input: []byte(`null`),
			want:  `null`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := nullableJSON(tt.input)
			if !result.Valid {
				t.Errorf("nullableJSON(%q) should return Valid=true", tt.input)
			}
			if result.String != tt.want {
				t.Errorf("nullableJSON(%q).String = %q, want %q", tt.input, result.String, tt.want)
			}
		})
	}
}
