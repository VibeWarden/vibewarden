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
	"runtime"
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

// lastTransfer stores the most recent CSRF transfer attempt.
// Access is protected by lastTransferMu.
//
// INTENTIONALLY VULNERABLE — no CSRF token is checked; this demonstrates
// that the app relies on VibeWarden's SameSite cookie configuration.
var (
	lastTransfer   csrfTransfer
	lastTransferMu sync.Mutex
)

// csrfTransfer holds the parameters of the last transfer request.
type csrfTransfer struct {
	To     string `json:"to"`
	Amount string `json:"amount"`
	Time   string `json:"time"`
}

// startTime records when the process started, used by the info-leak endpoint.
var startTime = time.Now()

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
	mux.HandleFunc("GET /profile", handleProfile)
	mux.HandleFunc("GET /auth/login", handleAuthPage(staticFS, "login.html"))
	mux.HandleFunc("GET /auth/registration", handleAuthPage(staticFS, "register.html"))
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))
	mux.HandleFunc("GET /vuln/xss-reflected", handleXSSReflected)
	mux.HandleFunc("GET /vuln/xss-stored", handleXSSStoredGet)
	mux.HandleFunc("POST /vuln/xss-stored", handleXSSStoredPost)
	mux.HandleFunc("GET /vuln/sqli", handleSQLi)
	mux.HandleFunc("POST /vuln/csrf-transfer", handleCSRFTransfer)
	mux.HandleFunc("GET /vuln/redirect", handleOpenRedirect)
	mux.HandleFunc("GET /vuln/debug/vars", handleInfoLeakVars)
	mux.HandleFunc("GET /vuln/debug/config", handleInfoLeakConfig)
	mux.HandleFunc("GET /vuln/mime-sniff", handleMIMESniff)
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

// handleProfile returns the active demo profile and which optional feature sets
// are available.  The profile is read from the VIBEWARDEN_PROFILE environment
// variable (set in docker-compose.yml).  The landing page JavaScript calls this
// endpoint to decide which sections to show or hide.
//
// This endpoint is public (no auth required).
func handleProfile(w http.ResponseWriter, r *http.Request) {
	profile := os.Getenv("VIBEWARDEN_PROFILE")
	if profile == "" {
		profile = "dev"
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"profile": profile,
		// Feature flags derived from the profile name so the landing page
		// can conditionally show observability links.
		"tls_enabled":           profile == "tls" || profile == "prod",
		"observability_enabled": profile == "full" || profile == "observability",
	})
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

// handleCSRFTransfer is the INTENTIONALLY VULNERABLE CSRF endpoint.
//
// It accepts a POST with form parameters "to" and "amount" and stores the
// transfer in memory.  There is no CSRF token — the protection comes entirely
// from VibeWarden configuring Ory Kratos session cookies with SameSite=Strict,
// which prevents cross-origin POST requests from including the session cookie.
//
// INTENTIONALLY VULNERABLE — do not add CSRF token validation here.
func handleCSRFTransfer(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "bad request"})
		return
	}
	to := r.FormValue("to")
	amount := r.FormValue("amount")
	if to == "" || amount == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "to and amount are required"})
		return
	}

	t := csrfTransfer{
		To:     to,
		Amount: amount,
		Time:   time.Now().UTC().Format(time.RFC3339),
	}
	lastTransferMu.Lock()
	lastTransfer = t
	lastTransferMu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"message":  "Transfer recorded (no CSRF token checked — intentionally vulnerable)",
		"transfer": t,
	})
}

// handleOpenRedirect is the INTENTIONALLY VULNERABLE open redirect endpoint.
//
// It redirects to whatever URL is supplied in the "url" query parameter without
// any validation.  An attacker can craft a link to this endpoint that redirects
// victims to a phishing site while the URL still shows the trusted domain.
//
// VibeWarden CANNOT prevent this — it is an application-level vulnerability.
// The correct fix is to validate redirect targets against an allowlist.
//
// INTENTIONALLY VULNERABLE — do not add URL validation here.
func handleOpenRedirect(w http.ResponseWriter, r *http.Request) {
	target := r.URL.Query().Get("url")
	if target == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "url parameter required"})
		return
	}
	http.Redirect(w, r, target, http.StatusFound)
}

// handleInfoLeakVars is an INTENTIONALLY VULNERABLE debug endpoint that
// exposes application internals: Go version, goroutine count, memory stats,
// uptime, and environment variables.
//
// This endpoint is NOT listed in public_paths — VibeWarden's default
// auth-on-all-paths configuration blocks unauthenticated access, demonstrating
// defence-in-depth even when the developer forgot to protect the endpoint.
//
// INTENTIONALLY VULNERABLE — do not restrict the data returned here.
func handleInfoLeakVars(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-Id")
	if userID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"error": "not authenticated — VibeWarden blocked this request",
		})
		return
	}

	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	writeJSON(w, http.StatusOK, map[string]any{
		"go_version":  runtime.Version(),
		"goroutines":  runtime.NumGoroutine(),
		"uptime":      time.Since(startTime).String(),
		"alloc_bytes": mem.Alloc,
		"num_gc":      mem.NumGC,
		"env_vars":    os.Environ(),
	})
}

// handleInfoLeakConfig is an INTENTIONALLY VULNERABLE debug endpoint that
// exposes fake application configuration including database credentials and
// API keys.
//
// This endpoint is NOT listed in public_paths — VibeWarden's auth-on-all-paths
// blocks unauthenticated access.
//
// INTENTIONALLY VULNERABLE — the fake credentials are intentionally visible
// to demonstrate the disclosure risk.
func handleInfoLeakConfig(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-Id")
	if userID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"error": "not authenticated — VibeWarden blocked this request",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"database_url":      "postgres://admin:password123@internal-db:5432/production",
		"stripe_secret_key": "sk_live_FAKE_KEY_FOR_DEMO_PURPOSES_ONLY",
		"internal_api_url":  "http://internal-payments-service:8080",
		"smtp_password":     "smtp_FAKE_PASSWORD_DEMO",
		"session_secret":    "FAKE_SESSION_SECRET_DO_NOT_USE_IN_PROD",
		"note":              "These are FAKE credentials for demonstration purposes only.",
	})
}

// handleMIMESniff is the INTENTIONALLY VULNERABLE MIME sniffing endpoint.
//
// It serves JavaScript content with Content-Type: text/plain.  Without the
// X-Content-Type-Options: nosniff header, older browsers might sniff the
// content and execute it as a script.
//
// VibeWarden mitigation: X-Content-Type-Options: nosniff on every response
// instructs browsers to trust the declared Content-Type and never sniff.
//
// INTENTIONALLY VULNERABLE — do not fix the Content-Type mismatch here.
func handleMIMESniff(w http.ResponseWriter, r *http.Request) {
	// Serve JavaScript content with the wrong Content-Type.
	// Without nosniff, some browsers would execute this as JS.
	w.Header().Set("Content-Type", "text/plain")
	fmt.Fprint(w, "alert('MIME sniffing attack')")
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
