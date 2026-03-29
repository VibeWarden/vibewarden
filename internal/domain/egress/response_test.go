package egress_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/vibewarden/vibewarden/internal/domain/egress"
)

func TestNewEgressResponse(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		header     http.Header
		bodyRef    interface{}
		duration   time.Duration
		wantErr    bool
	}{
		{
			name:       "valid 200 response",
			statusCode: 200,
			header:     http.Header{"Content-Type": []string{"application/json"}},
			duration:   42 * time.Millisecond,
			wantErr:    false,
		},
		{
			name:       "valid 100 (lower boundary)",
			statusCode: 100,
			wantErr:    false,
		},
		{
			name:       "valid 599 (upper boundary)",
			statusCode: 599,
			wantErr:    false,
		},
		{
			name:       "invalid status 99",
			statusCode: 99,
			wantErr:    true,
		},
		{
			name:       "invalid status 600",
			statusCode: 600,
			wantErr:    true,
		},
		{
			name:       "invalid status 0",
			statusCode: 0,
			wantErr:    true,
		},
		{
			name:       "nil header is replaced with empty header",
			statusCode: 204,
			header:     nil,
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := egress.NewEgressResponse(tt.statusCode, tt.header, tt.bodyRef, tt.duration)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewEgressResponse() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if resp.StatusCode != tt.statusCode {
					t.Errorf("StatusCode = %d, want %d", resp.StatusCode, tt.statusCode)
				}
				if resp.Header == nil {
					t.Error("Header must not be nil after construction")
				}
				if resp.Duration != tt.duration {
					t.Errorf("Duration = %v, want %v", resp.Duration, tt.duration)
				}
			}
		})
	}
}
