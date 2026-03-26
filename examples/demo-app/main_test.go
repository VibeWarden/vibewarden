package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHandleRoot(t *testing.T) {
	tests := []struct {
		name            string
		userIDHeader    string
		userEmailHeader string
		wantAuth        bool
		wantMsgContains string
	}{
		{
			name:            "unauthenticated — no X-User-Id header",
			wantAuth:        false,
			wantMsgContains: "Please log in",
		},
		{
			name:            "authenticated — X-User-Id present",
			userIDHeader:    "usr_123",
			userEmailHeader: "alice@example.com",
			wantAuth:        true,
			wantMsgContains: "alice@example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.userIDHeader != "" {
				req.Header.Set("X-User-Id", tt.userIDHeader)
				req.Header.Set("X-User-Email", tt.userEmailHeader)
			}
			rr := httptest.NewRecorder()

			handleRoot(rr, req)

			if rr.Code != http.StatusOK {
				t.Fatalf("want status 200, got %d", rr.Code)
			}

			var body map[string]any
			if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
				t.Fatalf("decode response: %v", err)
			}

			gotAuth, _ := body["authenticated"].(bool)
			if gotAuth != tt.wantAuth {
				t.Errorf("authenticated: want %v, got %v", tt.wantAuth, gotAuth)
			}

			msg, _ := body["message"].(string)
			if !strings.Contains(msg, tt.wantMsgContains) {
				t.Errorf("message %q does not contain %q", msg, tt.wantMsgContains)
			}
		})
	}
}

func TestHandlePublic(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/public", nil)
	rr := httptest.NewRecorder()

	handlePublic(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("want status 200, got %d", rr.Code)
	}

	var body map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	msg, ok := body["message"].(string)
	if !ok || msg == "" {
		t.Error("want non-empty message field")
	}

	tsRaw, ok := body["timestamp"].(string)
	if !ok || tsRaw == "" {
		t.Error("want non-empty timestamp field")
	}

	if _, err := time.Parse(time.RFC3339, tsRaw); err != nil {
		t.Errorf("timestamp %q is not RFC3339: %v", tsRaw, err)
	}
}

func TestHandleMe(t *testing.T) {
	tests := []struct {
		name           string
		userID         string
		userEmail      string
		userVerified   string
		wantStatus     int
		wantUserID     string
		wantEmail      string
		wantVerified   string
	}{
		{
			name:       "no X-User-Id returns 401",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:         "authenticated returns user fields",
			userID:       "usr_abc",
			userEmail:    "bob@example.com",
			userVerified: "true",
			wantStatus:   http.StatusOK,
			wantUserID:   "usr_abc",
			wantEmail:    "bob@example.com",
			wantVerified: "true",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/me", nil)
			if tt.userID != "" {
				req.Header.Set("X-User-Id", tt.userID)
				req.Header.Set("X-User-Email", tt.userEmail)
				req.Header.Set("X-User-Verified", tt.userVerified)
			}
			rr := httptest.NewRecorder()

			handleMe(rr, req)

			if rr.Code != tt.wantStatus {
				t.Fatalf("want status %d, got %d", tt.wantStatus, rr.Code)
			}

			if tt.wantStatus != http.StatusOK {
				return
			}

			var body map[string]any
			if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
				t.Fatalf("decode response: %v", err)
			}

			if got := body["user_id"]; got != tt.wantUserID {
				t.Errorf("user_id: want %q, got %v", tt.wantUserID, got)
			}
			if got := body["email"]; got != tt.wantEmail {
				t.Errorf("email: want %q, got %v", tt.wantEmail, got)
			}
			if got := body["verified"]; got != tt.wantVerified {
				t.Errorf("verified: want %q, got %v", tt.wantVerified, got)
			}
		})
	}
}

func TestHandleHeaders(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/headers", nil)
	req.Header.Set("X-Custom-Header", "demo-value")
	rr := httptest.NewRecorder()

	handleHeaders(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("want status 200, got %d", rr.Code)
	}

	var body map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if got := body["X-Custom-Header"]; got != "demo-value" {
		t.Errorf("X-Custom-Header: want %q, got %q", "demo-value", got)
	}
}

func TestHandleSpam(t *testing.T) {
	// Reset the counter before the test to keep it deterministic.
	spamCounter.Store(0)

	for i := int64(1); i <= 3; i++ {
		req := httptest.NewRequest(http.MethodPost, "/spam", nil)
		rr := httptest.NewRecorder()

		handleSpam(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("request %d: want status 200, got %d", i, rr.Code)
		}

		var body map[string]any
		if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
			t.Fatalf("request %d: decode response: %v", i, err)
		}

		gotNum, ok := body["request_number"].(float64)
		if !ok {
			t.Fatalf("request %d: request_number not a number: %v", i, body["request_number"])
		}
		if int64(gotNum) != i {
			t.Errorf("request %d: request_number: want %d, got %g", i, i, gotNum)
		}
	}
}

func TestHandleHealth(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()

	handleHealth(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("want status 200, got %d", rr.Code)
	}

	var body map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if got := body["status"]; got != "ok" {
		t.Errorf("status: want %q, got %v", "ok", got)
	}
}

func TestWriteJSONContentType(t *testing.T) {
	rr := httptest.NewRecorder()
	writeJSON(rr, http.StatusCreated, map[string]string{"key": "val"})

	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type: want %q, got %q", "application/json", ct)
	}
	if rr.Code != http.StatusCreated {
		t.Errorf("status: want 201, got %d", rr.Code)
	}
}
