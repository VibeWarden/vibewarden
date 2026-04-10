package middleware

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

// newAccessLogTestLogger returns a slog.Logger that writes JSON to buf.
func newAccessLogTestLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func TestAccessLogMiddleware_Disabled(t *testing.T) {
	// When disabled the middleware must be a transparent pass-through; the
	// logger is never called so we can pass nil safely.
	called := false
	handler := AccessLogMiddleware(nil, false, false)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			w.WriteHeader(http.StatusNoContent)
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if !called {
		t.Error("downstream handler was not called when access log is disabled")
	}
	if rr.Code != http.StatusNoContent {
		t.Errorf("response status = %d, want %d", rr.Code, http.StatusNoContent)
	}
}

func TestAccessLogMiddleware_LogsAfterResponse(t *testing.T) {
	tests := []struct {
		name         string
		method       string
		path         string
		handlerFn    func(http.ResponseWriter, *http.Request)
		wantStatus   int
		wantBytesMin int
	}{
		{
			name:   "GET 200 with body",
			method: http.MethodGet,
			path:   "/hello",
			handlerFn: func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte("hello world"))
			},
			wantStatus:   http.StatusOK,
			wantBytesMin: 11,
		},
		{
			name:   "POST 201 no body",
			method: http.MethodPost,
			path:   "/users",
			handlerFn: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusCreated)
			},
			wantStatus:   http.StatusCreated,
			wantBytesMin: 0,
		},
		{
			name:   "DELETE 404",
			method: http.MethodDelete,
			path:   "/items/42",
			handlerFn: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			wantStatus:   http.StatusNotFound,
			wantBytesMin: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger := newAccessLogTestLogger(&buf)

			handler := AccessLogMiddleware(logger, true, false)(
				http.HandlerFunc(tt.handlerFn),
			)

			req := httptest.NewRequest(tt.method, tt.path, nil)
			req.Header.Set("User-Agent", "test-agent/1.0")
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			// Parse the JSON log record.
			var record map[string]any
			if err := json.NewDecoder(&buf).Decode(&record); err != nil {
				t.Fatalf("failed to decode log record: %v", err)
			}

			if got := record["msg"]; got != "access" {
				t.Errorf("msg = %q, want %q", got, "access")
			}
			if got := record["method"]; got != tt.method {
				t.Errorf("method = %q, want %q", got, tt.method)
			}
			if got := record["path"]; got != tt.path {
				t.Errorf("path = %q, want %q", got, tt.path)
			}
			gotStatus, _ := record["status"].(float64)
			if int(gotStatus) != tt.wantStatus {
				t.Errorf("status = %v, want %d", gotStatus, tt.wantStatus)
			}
			if _, ok := record["duration_ms"]; !ok {
				t.Error("duration_ms field missing from log record")
			}
			if _, ok := record["client_ip"]; !ok {
				t.Error("client_ip field missing from log record")
			}
			if _, ok := record["request_id"]; !ok {
				t.Error("request_id field missing from log record")
			}
			if got := record["user_agent"]; got != "test-agent/1.0" {
				t.Errorf("user_agent = %q, want %q", got, "test-agent/1.0")
			}
			gotBytes, _ := record["bytes"].(float64)
			if int(gotBytes) < tt.wantBytesMin {
				t.Errorf("bytes = %v, want >= %d", gotBytes, tt.wantBytesMin)
			}
		})
	}
}

func TestAccessLogMiddleware_CapturesBytesWritten(t *testing.T) {
	const body = "response body text"
	var buf bytes.Buffer
	logger := newAccessLogTestLogger(&buf)

	handler := AccessLogMiddleware(logger, true, false)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(body))
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/data", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	var record map[string]any
	if err := json.NewDecoder(&buf).Decode(&record); err != nil {
		t.Fatalf("failed to decode log record: %v", err)
	}

	gotBytes, _ := record["bytes"].(float64)
	if int(gotBytes) != len(body) {
		t.Errorf("bytes = %v, want %d", gotBytes, len(body))
	}
}

func TestAccessLogMiddleware_CapturesBytesFromMultipleWrites(t *testing.T) {
	var buf bytes.Buffer
	logger := newAccessLogTestLogger(&buf)

	handler := AccessLogMiddleware(logger, true, false)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("abc"))
			_, _ = w.Write([]byte("de"))
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/multi", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	var record map[string]any
	if err := json.NewDecoder(&buf).Decode(&record); err != nil {
		t.Fatalf("failed to decode log record: %v", err)
	}

	gotBytes, _ := record["bytes"].(float64)
	if int(gotBytes) != 5 {
		t.Errorf("bytes = %v, want 5", gotBytes)
	}
}

func TestAccessLogMiddleware_DefaultStatusOK_WhenNoWriteHeader(t *testing.T) {
	var buf bytes.Buffer
	logger := newAccessLogTestLogger(&buf)

	handler := AccessLogMiddleware(logger, true, false)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Handler writes body but never calls WriteHeader — Go defaults to 200.
			_, _ = w.Write([]byte("ok"))
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/ok", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	var record map[string]any
	if err := json.NewDecoder(&buf).Decode(&record); err != nil {
		t.Fatalf("failed to decode log record: %v", err)
	}

	gotStatus, _ := record["status"].(float64)
	if int(gotStatus) != http.StatusOK {
		t.Errorf("status = %v, want %d", gotStatus, http.StatusOK)
	}
}

func TestAccessLogMiddleware_ClientIP_TrustProxy(t *testing.T) {
	tests := []struct {
		name       string
		trustProxy bool
		xff        string
		remoteAddr string
		wantIP     string
	}{
		{
			name:       "trust proxy uses XFF",
			trustProxy: true,
			xff:        "203.0.113.5, 10.0.0.1",
			remoteAddr: "10.0.0.1:12345",
			wantIP:     "203.0.113.5",
		},
		{
			name:       "no trust proxy uses RemoteAddr",
			trustProxy: false,
			xff:        "203.0.113.5",
			remoteAddr: "10.0.0.1:12345",
			wantIP:     "10.0.0.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger := newAccessLogTestLogger(&buf)

			handler := AccessLogMiddleware(logger, true, tt.trustProxy)(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
				}),
			)

			req := httptest.NewRequest(http.MethodGet, "/ip", nil)
			req.RemoteAddr = tt.remoteAddr
			if tt.xff != "" {
				req.Header.Set("X-Forwarded-For", tt.xff)
			}
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			var record map[string]any
			if err := json.NewDecoder(&buf).Decode(&record); err != nil {
				t.Fatalf("failed to decode log record: %v", err)
			}

			if got := record["client_ip"]; got != tt.wantIP {
				t.Errorf("client_ip = %q, want %q", got, tt.wantIP)
			}
		})
	}
}

func TestAccessLogMiddleware_RequestID_FromContext(t *testing.T) {
	var buf bytes.Buffer
	logger := newAccessLogTestLogger(&buf)

	const wantID = "req_TESTID000001"

	handler := AccessLogMiddleware(logger, true, false)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/rid", nil)
	// Inject a known request ID into the context.
	ctx := ContextWithRequestID(req.Context(), wantID)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	var record map[string]any
	if err := json.NewDecoder(&buf).Decode(&record); err != nil {
		t.Fatalf("failed to decode log record: %v", err)
	}

	if got := record["request_id"]; got != wantID {
		t.Errorf("request_id = %q, want %q", got, wantID)
	}
}

func TestAccessLogMiddleware_DurationIsPositive(t *testing.T) {
	var buf bytes.Buffer
	logger := newAccessLogTestLogger(&buf)

	handler := AccessLogMiddleware(logger, true, false)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/dur", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	var record map[string]any
	if err := json.NewDecoder(&buf).Decode(&record); err != nil {
		t.Fatalf("failed to decode log record: %v", err)
	}

	durMS, _ := record["duration_ms"].(float64)
	if durMS < 0 {
		t.Errorf("duration_ms = %v, want >= 0", durMS)
	}
}
