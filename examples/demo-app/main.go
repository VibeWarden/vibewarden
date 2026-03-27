// Package main implements the VibeWarden demo API server.
//
// This is a deliberately simple Go HTTP server that showcases how VibeWarden
// protects a real application.  Each endpoint demonstrates a different
// VibeWarden feature: auth header forwarding, rate limiting, security headers,
// and public vs protected routes.
//
// The server listens on port 3000 (or $PORT).  All protected functionality
// relies on headers injected by the VibeWarden sidecar — the app itself
// performs no authentication.
//
// Static HTML pages live in the static/ directory and are embedded into the
// binary at compile time via go:embed.  They are served under the /static/
// path prefix.  GET / serves the index page for browser requests (Accept:
// text/html) and the JSON greeting for API clients.
package main

import (
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	// Import the modernc SQLite driver for its side-effect of registering the
	// "sqlite" database/sql driver.  Pure Go — no CGO required.
	_ "modernc.org/sqlite"
)

//go:embed static
var staticFiles embed.FS

// spamCounter counts POST /spam requests to demonstrate rate limiting.
var spamCounter atomic.Int64

// guestbook holds submitted messages for the stored XSS demo.
// Access is protected by guestbookMu.
//
// INTENTIONALLY VULNERABLE — messages are stored and rendered without sanitisation.
var (
	guestbook   []string
	guestbookMu sync.Mutex
)

// notesDB is the in-memory SQLite database used by the SQL injection demo.
// It is initialised once at startup and shared across requests.
//
// INTENTIONALLY VULNERABLE — the /vuln/sqli endpoint queries this database
// using string concatenation instead of parameterised queries.
var notesDB *sql.DB

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}

	// Initialise the in-memory SQLite database for the SQL injection demo.
	// Errors here are unrecoverable startup failures — panic is acceptable in main.
	var err error
	notesDB, err = sql.Open("sqlite", ":memory:")
	if err != nil {
		fmt.Fprintf(os.Stderr, "open sqlite: %v\n", err)
		os.Exit(1)
	}
	if err := initNotesDB(notesDB); err != nil {
		fmt.Fprintf(os.Stderr, "init notes db: %v\n", err)
		os.Exit(1)
	}

	// Expose the embedded static/ tree as an http.FileSystem rooted at
	// "static" so requests to /static/index.html map to static/index.html
	// inside the embed.
	staticFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		fmt.Fprintf(os.Stderr, "static fs: %v\n", err)
		os.Exit(1)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /", handleRoot)
	mux.HandleFunc("GET /public", handlePublic)
	mux.HandleFunc("GET /me", handleMe)
	mux.HandleFunc("GET /headers", handleHeaders)
	mux.HandleFunc("POST /spam", handleSpam)
	mux.HandleFunc("GET /health", handleHealth)
	mux.HandleFunc("GET /auth/login", handleAuthPage(staticFS, "login.html"))
	mux.HandleFunc("GET /auth/registration", handleAuthPage(staticFS, "register.html"))
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))
	mux.HandleFunc("GET /vuln/xss-reflected", handleXSSReflected)
	mux.HandleFunc("GET /vuln/xss-stored", handleXSSStoredGet)
	mux.HandleFunc("POST /vuln/xss-stored", handleXSSStoredPost)
	mux.HandleFunc("GET /vuln/sqli", handleSQLi)
	mux.HandleFunc("GET /vuln/", handleVulnLab)

	addr := ":" + port
	slog.Info("demo-app starting", "addr", addr)

	srv := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	if err := srv.ListenAndServe(); err != nil {
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
		os.Exit(1)
	}
}

// handleRoot returns a personalised greeting when VibeWarden forwards an
// authenticated user's identity via the X-User-Id / X-User-Email headers,
// or a generic welcome message for unauthenticated requests.
//
// For browser requests (Accept: text/html) it redirects to /static/index.html
// so the demo UI is immediately visible.  API clients (curl, fetch) receive
// the JSON response as before.
//
// Demonstrates: VibeWarden auth header forwarding.
func handleRoot(w http.ResponseWriter, r *http.Request) {
	// Only handle the exact root path; let the mux 404 on anything else.
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	// Browser redirect — serve the HTML frontend.
	if strings.Contains(r.Header.Get("Accept"), "text/html") {
		http.Redirect(w, r, "/static/index.html", http.StatusFound)
		return
	}

	userID := r.Header.Get("X-User-Id")
	email := r.Header.Get("X-User-Email")

	var resp map[string]any
	if userID != "" {
		resp = map[string]any{
			"message":       "Welcome, " + email + "!",
			"authenticated": true,
		}
	} else {
		resp = map[string]any{
			"message":       "Welcome! Please log in.",
			"authenticated": false,
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

// handlePublic always returns a public response regardless of auth status.
//
// Demonstrates: VibeWarden public path bypass (no auth required).
func handlePublic(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"message":   "This is a public endpoint",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})
}

// handleMe returns the authenticated user's identity extracted from the
// headers injected by VibeWarden.  Returns 401 when the headers are absent,
// which only happens if the request bypasses VibeWarden.
//
// Demonstrates: VibeWarden protected route — app trusts sidecar-injected headers.
func handleMe(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-Id")
	if userID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"error": "not authenticated",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"user_id":  userID,
		"email":    r.Header.Get("X-User-Email"),
		"verified": r.Header.Get("X-User-Verified"),
	})
}

// handleHeaders echoes all incoming request headers as a JSON object.
//
// Demonstrates: the full set of headers VibeWarden adds (X-User-*, security
// headers stripped/forwarded, X-Request-Id, etc.).
func handleHeaders(w http.ResponseWriter, r *http.Request) {
	headers := make(map[string]string, len(r.Header))
	for key, values := range r.Header {
		if len(values) > 0 {
			headers[key] = values[0]
		}
	}
	writeJSON(w, http.StatusOK, headers)
}

// handleSpam increments an in-memory counter and returns it.  Hitting this
// endpoint rapidly will trigger VibeWarden's rate limiter (5 req/s per IP).
//
// Demonstrates: VibeWarden rate limiting — try:
//
//	for i in $(seq 1 20); do curl -s -o /dev/null -w "%{http_code}\n" -X POST http://localhost:8080/spam; done
func handleSpam(w http.ResponseWriter, r *http.Request) {
	n := spamCounter.Add(1)
	writeJSON(w, http.StatusOK, map[string]any{
		"message":        "ok",
		"request_number": n,
	})
}

// handleHealth returns a simple liveness response.
//
// Demonstrates: a health endpoint excluded from auth and rate limiting.
func handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

// handleVulnLab handles GET /vuln/* requests for the Vulnerability Lab.
//
// The individual vulnerability demo pages under /vuln/ are placeholder routes
// that return a JSON response describing the vulnerability until the full demo
// pages are implemented.  The route is listed in public_paths so it requires
// no authentication.
//
// Demonstrates: VibeWarden public path bypass, CSP, X-Frame-Options, and
// X-Content-Type-Options mitigations visible on these pages.
func handleVulnLab(w http.ResponseWriter, r *http.Request) {
	// Strip the /vuln/ prefix to get the vulnerability slug.
	slug := strings.TrimPrefix(r.URL.Path, "/vuln/")
	slug = strings.TrimSuffix(slug, "/")

	if slug == "" {
		http.Redirect(w, r, "/static/vulnlab.html", http.StatusFound)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"vulnerability": slug,
		"status":        "demo page coming soon",
		"lab":           "/static/vulnlab.html",
	})
}

// handleXSSReflected is the INTENTIONALLY VULNERABLE reflected XSS endpoint.
//
// It renders the value of the "q" query parameter directly into the HTML
// response without any escaping.  This lets an attacker craft a URL that
// executes arbitrary JavaScript in the victim's browser.
//
// VibeWarden mitigation: the CSP header "default-src 'self'" set by the
// sidecar prevents inline scripts from executing, so the injected payload
// is blocked even though the app itself does nothing to stop it.
//
// INTENTIONALLY VULNERABLE — do not sanitise this endpoint.
func handleXSSReflected(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <title>Search Results — Reflected XSS Demo</title>
  <link rel="stylesheet" href="/static/water.css">
</head>
<body>
  <nav>
    <strong>VibeWarden Demo</strong> &nbsp;|&nbsp;
    <a href="/">Home</a>
    <a href="/static/vulnlab.html">Vulnerability Lab</a>
    <a href="/static/xss-reflected.html">Reflected XSS Explained</a>
  </nav>
  <h1>Search Results</h1>
  <!-- INTENTIONALLY VULNERABLE: q is rendered without escaping -->
  <p>Search results for: %s</p>
  <p><a href="/static/xss-reflected.html">Back to explanation</a></p>
</body>
</html>`, q)
}

// handleXSSStoredGet renders the guestbook WITHOUT escaping stored messages.
//
// INTENTIONALLY VULNERABLE — messages are rendered raw so that any HTML/JS
// that was stored via POST /vuln/xss-stored is executed in the browser.
//
// VibeWarden mitigation: CSP blocks inline event handlers (e.g. onerror=),
// reducing the blast radius, but the app still must sanitise input at write
// time to be fully safe.
func handleXSSStoredGet(w http.ResponseWriter, r *http.Request) {
	guestbookMu.Lock()
	entries := make([]string, len(guestbook))
	copy(entries, guestbook)
	guestbookMu.Unlock()

	var rows strings.Builder
	if len(entries) == 0 {
		rows.WriteString("<li><em>No messages yet. Be the first!</em></li>")
	}
	for _, msg := range entries {
		// INTENTIONALLY VULNERABLE: msg is written without escaping.
		fmt.Fprintf(&rows, "<li>%s</li>\n", msg)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <title>Guestbook — Stored XSS Demo</title>
  <link rel="stylesheet" href="/static/water.css">
</head>
<body>
  <nav>
    <strong>VibeWarden Demo</strong> &nbsp;|&nbsp;
    <a href="/">Home</a>
    <a href="/static/vulnlab.html">Vulnerability Lab</a>
    <a href="/static/xss-stored.html">Stored XSS Explained</a>
  </nav>
  <h1>Guestbook</h1>
  <!-- INTENTIONALLY VULNERABLE: messages are rendered without escaping -->
  <ul>
    %s
  </ul>
  <p><a href="/static/xss-stored.html">Back to explanation</a></p>
</body>
</html>`, rows.String())
}

// handleXSSStoredPost stores a guestbook message without sanitisation.
//
// INTENTIONALLY VULNERABLE — the message from the form body is appended to
// the in-memory guestbook as-is and later rendered raw by handleXSSStoredGet.
func handleXSSStoredPost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	msg := r.FormValue("message")
	if msg == "" {
		http.Redirect(w, r, "/static/xss-stored.html", http.StatusSeeOther)
		return
	}

	guestbookMu.Lock()
	guestbook = append(guestbook, msg)
	guestbookMu.Unlock()

	http.Redirect(w, r, "/static/xss-stored.html", http.StatusSeeOther)
}

// initNotesDB seeds the in-memory SQLite database with sample notes.
//
// The notes table is intentionally populated with "sensitive" data so that
// the SQL injection demo can show realistic data exfiltration.
func initNotesDB(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE notes (
		id      INTEGER PRIMARY KEY,
		user_id TEXT,
		title   TEXT,
		content TEXT
	)`); err != nil {
		return fmt.Errorf("create notes table: %w", err)
	}
	seed := []struct {
		id                     int
		userID, title, content string
	}{
		{1, "admin", "Secret Note", "This is the admin secret"},
		{2, "admin", "Credentials", "password: hunter2"},
		{3, "user1", "Public Note", "Hello world"},
	}
	for _, row := range seed {
		if _, err := db.Exec(
			`INSERT INTO notes VALUES (?, ?, ?, ?)`,
			row.id, row.userID, row.title, row.content,
		); err != nil {
			return fmt.Errorf("seed note %d: %w", row.id, err)
		}
	}
	return nil
}

// note is the data transfer object for a row in the notes table.
type note struct {
	ID      int    `json:"id"`
	UserID  string `json:"user_id"`
	Title   string `json:"title"`
	Content string `json:"content"`
}

// handleSQLi is the INTENTIONALLY VULNERABLE SQL injection endpoint.
//
// It builds the SQL query by concatenating the raw value of the "user" query
// parameter directly into the SQL string without any escaping or parameterisation.
// An attacker can craft input such as:
//
//	/vuln/sqli?user=admin'+OR+'1'='1
//
// to dump all rows regardless of user_id.
//
// VibeWarden partial mitigations:
//   - Auth (Kratos) prevents unauthenticated access to protected routes.
//   - Rate limiting slows automated scanning and exfiltration.
//
// Neither mitigation prevents SQL injection itself.  The real fix is to use
// parameterised queries: db.Query("... WHERE user_id = ?", userID).
//
// INTENTIONALLY VULNERABLE — do not change to parameterised queries.
func handleSQLi(w http.ResponseWriter, r *http.Request) {
	userParam := r.URL.Query().Get("user")

	// INTENTIONALLY VULNERABLE: string concatenation builds the SQL query.
	// This allows input like: admin' OR '1'='1
	//nolint:gosec // intentional SQL injection for demonstration purposes
	query := "SELECT id, user_id, title, content FROM notes WHERE user_id = '" + userParam + "'"

	rows, err := notesDB.Query(query) //nolint:gosec
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": err.Error(),
			"query": query,
		})
		return
	}
	defer rows.Close()

	notes := make([]note, 0)
	for rows.Next() {
		var n note
		if err := rows.Scan(&n.ID, &n.UserID, &n.Title, &n.Content); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{
				"error": fmt.Sprintf("scan row: %v", err),
			})
			return
		}
		notes = append(notes, n)
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": fmt.Sprintf("rows: %v", err),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"query": query,
		"notes": notes,
		"count": len(notes),
	})
}

// handleAuthPage returns an http.HandlerFunc that serves a named HTML file
// from the embedded static filesystem.  It is used for the Kratos self-service
// UI pages (/auth/login, /auth/registration) so that VibeWarden can route
// Kratos's ui_url redirects directly to the demo app.
func handleAuthPage(staticFS fs.FS, filename string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		content, err := fs.ReadFile(staticFS, filename)
		if err != nil {
			http.Error(w, "page not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if _, err := w.Write(content); err != nil {
			slog.Error("failed to write auth page response", "file", filename, "error", err)
		}
	}
}

// writeJSON serialises v as JSON and writes it to w with the given status code.
// It sets Content-Type to application/json.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("failed to write JSON response", "error", err)
	}
}
