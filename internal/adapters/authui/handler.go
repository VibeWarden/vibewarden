// Package authui serves the built-in authentication UI pages for VibeWarden.
//
// It provides a Handler that renders four HTML pages — login, registration,
// recovery, and verification — at the /_vibewarden/{login,registration,recovery,verification}
// paths. All templates are embedded in the binary via embed.FS so that no
// external assets are required at runtime.
//
// Each page communicates with the Ory Kratos public API (proxied through
// VibeWarden at /self-service/*) using plain HTML and vanilla JavaScript with
// no external framework dependencies.
//
// Theming is achieved via CSS custom properties injected at render time from
// the AuthUIConfig. The values are safely HTML-escaped before insertion.
package authui

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"html/template"
	"log/slog"
	"net"
	"net/http"
)

//go:embed templates/*.html
var templateFS embed.FS

// AuthUIConfig holds the theming and behavioural configuration for the
// built-in auth UI pages.
type AuthUIConfig struct {
	// Mode selects the UI serving strategy.
	// "built-in" (default) — VibeWarden serves its own pages.
	// "custom"   — the operator provides their own pages; this handler is not mounted.
	Mode string

	// PrimaryColor is the CSS value for the --vw-primary custom property.
	// Defaults to "#7C3AED" (VibeWarden purple) when empty.
	PrimaryColor string

	// BackgroundColor is the CSS value for the --vw-bg custom property.
	// Defaults to "#F3F4F6" when empty.
	BackgroundColor string

	// TextColor is the CSS value for the --vw-text custom property.
	// Defaults to "#111827" when empty.
	TextColor string

	// ErrorColor is the CSS value for the --vw-error custom property.
	// Defaults to "#DC2626" when empty.
	ErrorColor string
}

// templateData is passed to every HTML template at render time.
type templateData struct {
	// PrimaryColor is the CSS color string for --vw-primary.
	PrimaryColor string
	// BackgroundColor is the CSS color string for --vw-bg.
	BackgroundColor string
	// TextColor is the CSS color string for --vw-text.
	TextColor string
	// ErrorColor is the CSS color string for --vw-error.
	ErrorColor string
	// ReturnToQuery is the optional ?return_to=... query string fragment
	// (including the leading "?") to append to inter-page links so the
	// original destination is preserved. Empty string when not set.
	ReturnToQuery string
}

// Handler serves the built-in auth UI pages and implements
// ports.InternalServerPlugin (via Addr) so the registry can reverse-proxy
// the /_vibewarden/login|registration|recovery|verification routes to it.
type Handler struct {
	cfg      AuthUIConfig
	tmpls    *template.Template
	listener net.Listener
	server   *http.Server
	logger   *slog.Logger
}

// NewHandler constructs an auth UI Handler. cfg controls theming; logger is
// used for startup and serve errors. Call Start to bind the listener.
func NewHandler(cfg AuthUIConfig, logger *slog.Logger) (*Handler, error) {
	if logger == nil {
		logger = slog.Default()
	}

	applyDefaults(&cfg)

	tmpls, err := loadTemplates()
	if err != nil {
		return nil, fmt.Errorf("authui: loading templates: %w", err)
	}

	return &Handler{
		cfg:    cfg,
		tmpls:  tmpls,
		logger: logger,
	}, nil
}

// Start binds a random localhost TCP port, registers the auth UI routes, and
// begins serving. It returns immediately; the server runs until Stop is called.
// Start must be called before Addr.
func (h *Handler) Start() error {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("authui: binding listener: %w", err)
	}
	h.listener = ln

	mux := http.NewServeMux()
	h.registerRoutes(mux)

	h.server = &http.Server{Handler: mux} //nolint:gosec // internal-only localhost listener
	go func() {
		if serveErr := h.server.Serve(ln); serveErr != nil && serveErr != http.ErrServerClosed {
			h.logger.Error("authui server stopped unexpectedly", "err", serveErr)
		}
	}()

	h.logger.Info("authui server started", slog.String("addr", ln.Addr().String()))
	return nil
}

// Stop gracefully shuts down the auth UI HTTP server.
func (h *Handler) Stop(ctx context.Context) error {
	if h.server == nil {
		return nil
	}
	if err := h.server.Shutdown(ctx); err != nil {
		return fmt.Errorf("authui: shutting down server: %w", err)
	}
	return nil
}

// Addr returns the host:port the internal HTTP server is listening on.
// Addr must only be called after a successful Start.
func (h *Handler) Addr() string {
	return h.listener.Addr().String()
}

// registerRoutes mounts all auth UI page handlers onto mux.
func (h *Handler) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/_vibewarden/login", h.handleLogin)
	mux.HandleFunc("/_vibewarden/registration", h.handleRegistration)
	mux.HandleFunc("/_vibewarden/recovery", h.handleRecovery)
	mux.HandleFunc("/_vibewarden/verification", h.handleVerification)
}

// handleLogin renders the login page.
func (h *Handler) handleLogin(w http.ResponseWriter, r *http.Request) {
	h.renderPage(w, r, "login.html")
}

// handleRegistration renders the registration page.
func (h *Handler) handleRegistration(w http.ResponseWriter, r *http.Request) {
	h.renderPage(w, r, "registration.html")
}

// handleRecovery renders the account recovery page.
func (h *Handler) handleRecovery(w http.ResponseWriter, r *http.Request) {
	h.renderPage(w, r, "recovery.html")
}

// handleVerification renders the email verification page.
func (h *Handler) handleVerification(w http.ResponseWriter, r *http.Request) {
	h.renderPage(w, r, "verification.html")
}

// renderPage executes the named template with theme data derived from cfg
// and writes the result to w. On template error a 500 is returned.
func (h *Handler) renderPage(w http.ResponseWriter, r *http.Request, tmplName string) {
	data := templateData{
		PrimaryColor:    h.cfg.PrimaryColor,
		BackgroundColor: h.cfg.BackgroundColor,
		TextColor:       h.cfg.TextColor,
		ErrorColor:      h.cfg.ErrorColor,
		ReturnToQuery:   returnToQuery(r),
	}

	var buf bytes.Buffer
	if err := h.tmpls.ExecuteTemplate(&buf, tmplName, data); err != nil {
		h.logger.Error("authui: executing template", slog.String("template", tmplName), "err", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
}

// loadTemplates parses all embedded HTML templates from templateFS.
// The templates are named by their base filename (e.g. "login.html").
func loadTemplates() (*template.Template, error) {
	entries, err := templateFS.ReadDir("templates")
	if err != nil {
		return nil, fmt.Errorf("reading templates dir: %w", err)
	}

	root := template.New("")
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		src, err := templateFS.ReadFile("templates/" + name)
		if err != nil {
			return nil, fmt.Errorf("reading template %q: %w", name, err)
		}
		if _, err := root.New(name).Parse(string(src)); err != nil {
			return nil, fmt.Errorf("parsing template %q: %w", name, err)
		}
	}

	return root, nil
}

// applyDefaults fills in zero-value fields in cfg with sensible defaults.
func applyDefaults(cfg *AuthUIConfig) {
	if cfg.Mode == "" {
		cfg.Mode = "built-in"
	}
	if cfg.PrimaryColor == "" {
		cfg.PrimaryColor = "#7C3AED"
	}
	if cfg.BackgroundColor == "" {
		cfg.BackgroundColor = "#F3F4F6"
	}
	if cfg.TextColor == "" {
		cfg.TextColor = "#111827"
	}
	if cfg.ErrorColor == "" {
		cfg.ErrorColor = "#DC2626"
	}
}

// returnToQuery extracts the return_to query parameter from r and returns
// a ready-to-append query string fragment like "?return_to=%2Fdashboard",
// or an empty string when the parameter is absent or empty.
func returnToQuery(r *http.Request) string {
	v := r.URL.Query().Get("return_to")
	if v == "" {
		return ""
	}
	return "?return_to=" + v
}
