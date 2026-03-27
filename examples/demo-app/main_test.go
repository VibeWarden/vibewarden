package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
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
		name         string
		userID       string
		userEmail    string
		userVerified string
		wantStatus   int
		wantUserID   string
		wantEmail    string
		wantVerified string
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

func TestHandleRootBrowserRedirect(t *testing.T) {
	tests := []struct {
		name         string
		acceptHeader string
		wantStatus   int
		wantLocation string
	}{
		{
			name:         "browser Accept header redirects to static index",
			acceptHeader: "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
			wantStatus:   http.StatusFound,
			wantLocation: "/static/index.html",
		},
		{
			name:         "API client without Accept text/html receives JSON",
			acceptHeader: "application/json",
			wantStatus:   http.StatusOK,
			wantLocation: "",
		},
		{
			name:         "no Accept header receives JSON",
			acceptHeader: "",
			wantStatus:   http.StatusOK,
			wantLocation: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.acceptHeader != "" {
				req.Header.Set("Accept", tt.acceptHeader)
			}
			rr := httptest.NewRecorder()

			handleRoot(rr, req)

			if rr.Code != tt.wantStatus {
				t.Fatalf("want status %d, got %d", tt.wantStatus, rr.Code)
			}
			if tt.wantLocation != "" {
				loc := rr.Header().Get("Location")
				if loc != tt.wantLocation {
					t.Errorf("Location: want %q, got %q", tt.wantLocation, loc)
				}
			}
		})
	}
}

func TestHandleRootUnknownPath(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/unknown-path", nil)
	rr := httptest.NewRecorder()

	handleRoot(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("want status 404 for unknown path, got %d", rr.Code)
	}
}

func TestHandleVulnLab(t *testing.T) {
	tests := []struct {
		name         string
		path         string
		wantStatus   int
		wantLocation string
		wantSlug     string
	}{
		{
			name:         "bare /vuln/ redirects to vulnlab.html",
			path:         "/vuln/",
			wantStatus:   http.StatusFound,
			wantLocation: "/static/vulnlab.html",
		},
		{
			name:       "xss-reflected returns JSON with slug",
			path:       "/vuln/xss-reflected",
			wantStatus: http.StatusOK,
			wantSlug:   "xss-reflected",
		},
		{
			name:       "sqli returns JSON with slug",
			path:       "/vuln/sqli",
			wantStatus: http.StatusOK,
			wantSlug:   "sqli",
		},
		{
			name:       "clickjacking returns JSON with slug",
			path:       "/vuln/clickjacking",
			wantStatus: http.StatusOK,
			wantSlug:   "clickjacking",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rr := httptest.NewRecorder()

			handleVulnLab(rr, req)

			if rr.Code != tt.wantStatus {
				t.Fatalf("want status %d, got %d", tt.wantStatus, rr.Code)
			}

			if tt.wantLocation != "" {
				loc := rr.Header().Get("Location")
				if loc != tt.wantLocation {
					t.Errorf("Location: want %q, got %q", tt.wantLocation, loc)
				}
				return
			}

			var body map[string]any
			if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
				t.Fatalf("decode response: %v", err)
			}

			if got := body["vulnerability"]; got != tt.wantSlug {
				t.Errorf("vulnerability: want %q, got %v", tt.wantSlug, got)
			}
			if got, ok := body["status"].(string); !ok || got == "" {
				t.Error("want non-empty status field")
			}
		})
	}
}

func TestStaticFilesEmbedded(t *testing.T) {
	// Verify that the expected HTML files are present in the embedded FS.
	wantFiles := []string{
		"static/index.html",
		"static/me.html",
		"static/headers.html",
		"static/ratelimit.html",
		"static/vulnlab.html",
		"static/xss-reflected.html",
		"static/xss-stored.html",
		"static/sqli.html",
	}
	for _, path := range wantFiles {
		t.Run(path, func(t *testing.T) {
			f, err := staticFiles.Open(path)
			if err != nil {
				t.Fatalf("expected embedded file %q to exist: %v", path, err)
			}
			f.Close()
		})
	}
}

// newTestDB opens a fresh in-memory SQLite database seeded with notes for testing.
func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := initNotesDB(db); err != nil {
		t.Fatalf("initNotesDB: %v", err)
	}
	return db
}

func TestInitNotesDB(t *testing.T) {
	db := newTestDB(t)

	// Verify that the seed data was inserted correctly.
	tests := []struct {
		name      string
		userID    string
		wantCount int
	}{
		{"admin has two notes", "admin", 2},
		{"user1 has one note", "user1", 1},
		{"unknown user has no notes", "nobody", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows, err := db.Query(
				"SELECT COUNT(*) FROM notes WHERE user_id = ?", tt.userID,
			)
			if err != nil {
				t.Fatalf("query: %v", err)
			}
			defer rows.Close()
			var count int
			if rows.Next() {
				if err := rows.Scan(&count); err != nil {
					t.Fatalf("scan: %v", err)
				}
			}
			if count != tt.wantCount {
				t.Errorf("user %q: want %d rows, got %d", tt.userID, tt.wantCount, count)
			}
		})
	}
}

func TestHandleSQLi(t *testing.T) {
	// Swap in a fresh test database for this test, then restore original.
	origDB := notesDB
	notesDB = newTestDB(t)
	t.Cleanup(func() { notesDB = origDB })

	tests := []struct {
		name       string
		userParam  string
		wantStatus int
		wantCount  int
		wantQuery  string // substring that must appear in the returned "query" field
	}{
		{
			name:       "normal query returns admin notes only",
			userParam:  "admin",
			wantStatus: http.StatusOK,
			wantCount:  2,
			wantQuery:  "WHERE user_id = 'admin'",
		},
		{
			name:       "injection payload dumps all rows",
			userParam:  "admin' OR '1'='1",
			wantStatus: http.StatusOK,
			wantCount:  3, // all three seed rows
			wantQuery:  "OR '1'='1'",
		},
		{
			name:       "unknown user returns zero rows",
			userParam:  "nobody",
			wantStatus: http.StatusOK,
			wantCount:  0,
			wantQuery:  "WHERE user_id = 'nobody'",
		},
		{
			name:       "empty user returns zero rows",
			userParam:  "",
			wantStatus: http.StatusOK,
			wantCount:  0,
			wantQuery:  "WHERE user_id = ''",
		},
		{
			name:       "syntax error returns 400 with error field",
			userParam:  "' INVALID SQL '''",
			wantStatus: http.StatusBadRequest,
			wantCount:  -1, // not checked
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/vuln/sqli?user="+url.QueryEscape(tt.userParam), nil)
			rr := httptest.NewRecorder()

			handleSQLi(rr, req)

			if rr.Code != tt.wantStatus {
				t.Fatalf("want status %d, got %d; body: %s", tt.wantStatus, rr.Code, rr.Body.String())
			}

			var body map[string]any
			if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
				t.Fatalf("decode response: %v", err)
			}

			// For error cases just verify the error field is present.
			if tt.wantStatus != http.StatusOK {
				if errMsg, ok := body["error"].(string); !ok || errMsg == "" {
					t.Error("want non-empty error field in error response")
				}
				return
			}

			// Verify the query field contains the expected substring.
			if tt.wantQuery != "" {
				q, _ := body["query"].(string)
				if !strings.Contains(q, tt.wantQuery) {
					t.Errorf("query field %q does not contain %q", q, tt.wantQuery)
				}
			}

			// Verify the count field.
			countRaw, ok := body["count"].(float64)
			if !ok {
				t.Fatalf("count field missing or wrong type: %v", body["count"])
			}
			if int(countRaw) != tt.wantCount {
				t.Errorf("count: want %d, got %d", tt.wantCount, int(countRaw))
			}

			// Verify the notes array has matching length.
			notes, _ := body["notes"].([]any)
			if len(notes) != tt.wantCount {
				t.Errorf("notes array length: want %d, got %d", tt.wantCount, len(notes))
			}
		})
	}
}

func TestHandleXSSReflected(t *testing.T) {
	tests := []struct {
		name       string
		query      string
		wantInBody string
		wantStatus int
		wantCT     string
	}{
		{
			name:       "plain text query is reflected verbatim",
			query:      "hello",
			wantInBody: "Search results for: hello",
			wantStatus: http.StatusOK,
			wantCT:     "text/html; charset=utf-8",
		},
		{
			name:       "XSS payload is reflected unescaped",
			query:      `<script>alert('XSS')</script>`,
			wantInBody: `<script>alert('XSS')</script>`,
			wantStatus: http.StatusOK,
			wantCT:     "text/html; charset=utf-8",
		},
		{
			name:       "empty query is handled without error",
			query:      "",
			wantInBody: "Search results for: ",
			wantStatus: http.StatusOK,
			wantCT:     "text/html; charset=utf-8",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/vuln/xss-reflected?q="+tt.query, nil)
			rr := httptest.NewRecorder()

			handleXSSReflected(rr, req)

			if rr.Code != tt.wantStatus {
				t.Fatalf("want status %d, got %d", tt.wantStatus, rr.Code)
			}
			if ct := rr.Header().Get("Content-Type"); ct != tt.wantCT {
				t.Errorf("Content-Type: want %q, got %q", tt.wantCT, ct)
			}
			if body := rr.Body.String(); !strings.Contains(body, tt.wantInBody) {
				t.Errorf("response body does not contain %q; body: %s", tt.wantInBody, body)
			}
		})
	}
}

func TestHandleXSSStoredGetPost(t *testing.T) {
	// Reset the global guestbook before this test so it is isolated.
	guestbookMu.Lock()
	guestbook = nil
	guestbookMu.Unlock()

	t.Run("GET empty guestbook shows no-messages placeholder", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/vuln/xss-stored", nil)
		rr := httptest.NewRecorder()

		handleXSSStoredGet(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("want status 200, got %d", rr.Code)
		}
		if ct := rr.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
			t.Errorf("Content-Type: want text/html, got %q", ct)
		}
		if body := rr.Body.String(); !strings.Contains(body, "No messages yet") {
			t.Errorf("expected empty guestbook message; body: %s", body)
		}
	})

	t.Run("POST stores message and redirects", func(t *testing.T) {
		form := strings.NewReader("message=hello+world")
		req := httptest.NewRequest(http.MethodPost, "/vuln/xss-stored", form)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rr := httptest.NewRecorder()

		handleXSSStoredPost(rr, req)

		if rr.Code != http.StatusSeeOther {
			t.Fatalf("want status 303, got %d", rr.Code)
		}
	})

	t.Run("GET after POST shows stored message unescaped", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/vuln/xss-stored", nil)
		rr := httptest.NewRecorder()

		handleXSSStoredGet(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("want status 200, got %d", rr.Code)
		}
		if body := rr.Body.String(); !strings.Contains(body, "hello world") {
			t.Errorf("expected stored message in body; body: %s", body)
		}
	})

	t.Run("POST stores XSS payload unescaped", func(t *testing.T) {
		// Reset guestbook first.
		guestbookMu.Lock()
		guestbook = nil
		guestbookMu.Unlock()

		payload := `<img src=x onerror=alert('XSS')>`
		form := strings.NewReader("message=" + payload)
		req := httptest.NewRequest(http.MethodPost, "/vuln/xss-stored", form)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rr := httptest.NewRecorder()
		handleXSSStoredPost(rr, req)

		// Now GET should reflect the payload verbatim.
		req2 := httptest.NewRequest(http.MethodGet, "/vuln/xss-stored", nil)
		rr2 := httptest.NewRecorder()
		handleXSSStoredGet(rr2, req2)

		if body := rr2.Body.String(); !strings.Contains(body, payload) {
			t.Errorf("expected XSS payload in body (unescaped); body: %s", body)
		}
	})

	t.Run("POST with empty message redirects without storing", func(t *testing.T) {
		guestbookMu.Lock()
		before := len(guestbook)
		guestbookMu.Unlock()

		form := strings.NewReader("message=")
		req := httptest.NewRequest(http.MethodPost, "/vuln/xss-stored", form)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rr := httptest.NewRecorder()

		handleXSSStoredPost(rr, req)

		if rr.Code != http.StatusSeeOther {
			t.Fatalf("want status 303, got %d", rr.Code)
		}
		guestbookMu.Lock()
		after := len(guestbook)
		guestbookMu.Unlock()
		if after != before {
			t.Errorf("empty message should not be stored; before=%d after=%d", before, after)
		}
	})
}
