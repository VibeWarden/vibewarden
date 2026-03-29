package egress_test

import (
	"net/http"
	"testing"

	"github.com/vibewarden/vibewarden/internal/domain/egress"
)

func TestNewEgressRequest(t *testing.T) {
	tests := []struct {
		name    string
		method  string
		url     string
		header  http.Header
		bodyRef interface{}
		wantErr bool
	}{
		{
			name:    "valid request",
			method:  "GET",
			url:     "https://api.stripe.com/v1/charges",
			header:  http.Header{"Authorization": []string{"Bearer tok"}},
			bodyRef: nil,
			wantErr: false,
		},
		{
			name:    "empty method",
			method:  "",
			url:     "https://api.stripe.com/v1/charges",
			wantErr: true,
		},
		{
			name:    "empty URL",
			method:  "GET",
			url:     "",
			wantErr: true,
		},
		{
			name:    "nil header is replaced with empty header",
			method:  "POST",
			url:     "https://api.example.com/",
			header:  nil,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := egress.NewEgressRequest(tt.method, tt.url, tt.header, tt.bodyRef)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewEgressRequest() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if req.Method != tt.method {
					t.Errorf("Method = %q, want %q", req.Method, tt.method)
				}
				if req.URL != tt.url {
					t.Errorf("URL = %q, want %q", req.URL, tt.url)
				}
				if req.Header == nil {
					t.Error("Header must not be nil after construction")
				}
			}
		})
	}
}
