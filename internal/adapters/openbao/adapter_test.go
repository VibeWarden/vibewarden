package openbao_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/vibewarden/vibewarden/internal/adapters/openbao"
)

// newTestAdapter creates an Adapter pointed at a test HTTP server.
func newTestAdapter(address string, method openbao.AuthMethod, token string) *openbao.Adapter {
	cfg := openbao.Config{
		Address: address,
		Auth: openbao.AuthConfig{
			Method: method,
			Token:  token,
		},
		MountPath:       "secret",
		TokenRenewGrace: time.Second,
	}
	return openbao.New(cfg, slog.Default())
}

func TestAdapter_Health(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantErr    bool
	}{
		{"healthy unsealed", http.StatusOK, false},
		{"standby node", http.StatusTooManyRequests, false},
		{"sealed", http.StatusServiceUnavailable, true},
		{"not initialized", http.StatusNotImplemented, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/sys/health" {
					t.Errorf("unexpected path %q", r.URL.Path)
				}
				w.WriteHeader(tt.statusCode)
			}))
			defer srv.Close()

			a := newTestAdapter(srv.URL, openbao.AuthMethodToken, "test-token")
			err := a.Health(context.Background())
			if (err != nil) != tt.wantErr {
				t.Errorf("Health() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestAdapter_Authenticate_Token(t *testing.T) {
	// Token auth should not make any HTTP calls.
	a := newTestAdapter("http://unused", openbao.AuthMethodToken, "my-token")
	if err := a.Authenticate(context.Background()); err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
}

func TestAdapter_Authenticate_AppRole(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		wantErr    bool
	}{
		{
			name:       "successful login",
			statusCode: http.StatusOK,
			body:       `{"auth":{"client_token":"s.abc123","lease_duration":3600}}`,
			wantErr:    false,
		},
		{
			name:       "bad credentials",
			statusCode: http.StatusForbidden,
			body:       `{"errors":["permission denied"]}`,
			wantErr:    true,
		},
		{
			name:       "empty token in response",
			statusCode: http.StatusOK,
			body:       `{"auth":{"client_token":"","lease_duration":3600}}`,
			wantErr:    true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/auth/approle/login" {
					t.Errorf("unexpected path %q", r.URL.Path)
				}
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()

			cfg := openbao.Config{
				Address: srv.URL,
				Auth: openbao.AuthConfig{
					Method:   openbao.AuthMethodAppRole,
					RoleID:   "role-abc",
					SecretID: "secret-xyz",
				},
				MountPath: "secret",
			}
			a := openbao.New(cfg, slog.Default())
			err := a.Authenticate(context.Background())
			if (err != nil) != tt.wantErr {
				t.Errorf("Authenticate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestAdapter_Get(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		statusCode int
		body       string
		wantData   map[string]string
		wantErr    bool
	}{
		{
			name:       "secret found",
			path:       "app/database",
			statusCode: http.StatusOK,
			body:       `{"data":{"data":{"password":"s3cr3t","username":"app"}}}`,
			wantData:   map[string]string{"password": "s3cr3t", "username": "app"},
			wantErr:    false,
		},
		{
			name:       "secret not found",
			path:       "app/missing",
			statusCode: http.StatusNotFound,
			body:       `{"errors":[]}`,
			wantErr:    true,
		},
		{
			name:       "server error",
			path:       "app/error",
			statusCode: http.StatusInternalServerError,
			body:       `{"errors":["internal server error"]}`,
			wantErr:    true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				expectedPath := "/v1/secret/data/" + tt.path
				if r.URL.Path != expectedPath {
					t.Errorf("unexpected path %q, want %q", r.URL.Path, expectedPath)
				}
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()

			a := newTestAdapter(srv.URL, openbao.AuthMethodToken, "test-token")
			// Pre-authenticate with token method (no HTTP call needed).
			if err := a.Authenticate(context.Background()); err != nil {
				t.Fatalf("Authenticate() error = %v", err)
			}
			got, err := a.Get(context.Background(), tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("Get() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr {
				for k, v := range tt.wantData {
					if got[k] != v {
						t.Errorf("Get()[%q] = %q, want %q", k, got[k], v)
					}
				}
			}
		})
	}
}

func TestAdapter_Put(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		data       map[string]string
		statusCode int
		wantErr    bool
	}{
		{
			name:       "successful write",
			path:       "app/stripe",
			data:       map[string]string{"api_key": "sk_test_abc"},
			statusCode: http.StatusOK,
			wantErr:    false,
		},
		{
			name:       "permission denied",
			path:       "app/readonly",
			data:       map[string]string{"key": "val"},
			statusCode: http.StatusForbidden,
			wantErr:    true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Errorf("unexpected method %q", r.Method)
				}
				var body struct {
					Data map[string]any `json:"data"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Errorf("decode request body: %v", err)
				}
				w.WriteHeader(tt.statusCode)
			}))
			defer srv.Close()

			a := newTestAdapter(srv.URL, openbao.AuthMethodToken, "test-token")
			_ = a.Authenticate(context.Background())
			err := a.Put(context.Background(), tt.path, tt.data)
			if (err != nil) != tt.wantErr {
				t.Errorf("Put() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestAdapter_Delete(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantErr    bool
	}{
		{"deleted", http.StatusNoContent, false},
		{"not found is ok", http.StatusOK, false},
		{"permission denied", http.StatusForbidden, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodDelete {
					t.Errorf("unexpected method %q", r.Method)
				}
				w.WriteHeader(tt.statusCode)
			}))
			defer srv.Close()

			a := newTestAdapter(srv.URL, openbao.AuthMethodToken, "test-token")
			_ = a.Authenticate(context.Background())
			err := a.Delete(context.Background(), "app/test")
			if (err != nil) != tt.wantErr {
				t.Errorf("Delete() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestAdapter_List(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		wantKeys   []string
		wantErr    bool
	}{
		{
			name:       "keys found",
			statusCode: http.StatusOK,
			body:       `{"data":{"keys":["database","stripe","internal/"]}}`,
			wantKeys:   []string{"database", "stripe", "internal/"},
			wantErr:    false,
		},
		{
			name:       "empty path",
			statusCode: http.StatusNotFound,
			body:       `{"errors":[]}`,
			wantKeys:   nil,
			wantErr:    false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "LIST" {
					t.Errorf("unexpected method %q, want LIST", r.Method)
				}
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()

			a := newTestAdapter(srv.URL, openbao.AuthMethodToken, "test-token")
			_ = a.Authenticate(context.Background())
			got, err := a.List(context.Background(), "app")
			if (err != nil) != tt.wantErr {
				t.Errorf("List() error = %v, wantErr %v", err, tt.wantErr)
			}
			if len(got) != len(tt.wantKeys) {
				t.Errorf("List() len = %d, want %d", len(got), len(tt.wantKeys))
			}
		})
	}
}

func TestAdapter_RequestDynamicCredentials(t *testing.T) {
	tests := []struct {
		name       string
		role       string
		statusCode int
		body       string
		wantErr    bool
	}{
		{
			name:       "credentials issued",
			role:       "app-readwrite",
			statusCode: http.StatusOK,
			body:       `{"lease_id":"database/creds/app-readwrite/abc123","lease_duration":3600,"data":{"username":"v-app-Abc","password":"A1B2C3D4E5F6"}}`,
			wantErr:    false,
		},
		{
			name:       "role not found",
			role:       "missing-role",
			statusCode: http.StatusBadRequest,
			body:       `{"errors":["unknown role"]}`,
			wantErr:    true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()

			a := newTestAdapter(srv.URL, openbao.AuthMethodToken, "test-token")
			_ = a.Authenticate(context.Background())
			creds, err := a.RequestDynamicCredentials(context.Background(), tt.role)
			if (err != nil) != tt.wantErr {
				t.Errorf("RequestDynamicCredentials() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && creds == nil {
				t.Error("RequestDynamicCredentials() returned nil creds without error")
			}
		})
	}
}

func TestDynamicCredentials_ExpiresAt(t *testing.T) {
	now := time.Now()
	creds := &openbao.DynamicCredentials{
		IssuedAt: now,
		TTL:      time.Hour,
	}
	expected := now.Add(time.Hour)
	got := creds.ExpiresAt()
	// Allow 1 second tolerance.
	if got.Before(expected.Add(-time.Second)) || got.After(expected.Add(time.Second)) {
		t.Errorf("ExpiresAt() = %v, want ~%v", got, expected)
	}
}
