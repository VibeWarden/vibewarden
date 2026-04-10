package webhook

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/vibewarden/vibewarden/internal/domain/events"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// defaultTimeout is the HTTP request timeout applied when no endpoint-specific
// timeout is configured.
const defaultTimeout = 10 * time.Second

// retryDelays defines the exponential backoff delays between delivery attempts.
// Three attempts total: immediate, then 1 s, then 5 s, then 30 s dead-letter.
var retryDelays = [3]time.Duration{1 * time.Second, 5 * time.Second, 30 * time.Second}

// maxAttempts is the total number of delivery attempts (initial + retries).
const maxAttempts = 3

// endpoint holds a parsed, ready-to-use webhook endpoint configuration.
type endpoint struct {
	url       string
	events    []string // "*" means all events
	allEvents bool
	formatter ports.WebhookFormatter
	timeout   time.Duration
}

// matches returns true when the endpoint is subscribed to eventType.
func (e *endpoint) matches(eventType string) bool {
	if e.allEvents {
		return true
	}
	for _, et := range e.events {
		if et == eventType {
			return true
		}
	}
	return false
}

// Dispatcher implements ports.WebhookDispatcher. It sends events to all
// matching HTTP endpoints asynchronously using a background goroutine per
// delivery. Failed deliveries are retried with exponential backoff up to
// maxAttempts times before being dead-letter logged.
type Dispatcher struct {
	endpoints  []endpoint
	httpClient *http.Client
	logger     *slog.Logger
}

// DispatcherConfig holds the parsed configuration for a single webhook endpoint.
// It is used to construct a Dispatcher via NewDispatcher.
type DispatcherConfig struct {
	// URL is the HTTP(S) endpoint to POST events to.
	URL string

	// Events is the list of event type strings to subscribe to.
	// A single-element slice containing "*" subscribes to all events.
	Events []string

	// Format selects the payload format: "raw", "slack", or "discord".
	// Defaults to "raw" when empty.
	Format ports.WebhookFormat

	// Timeout is the per-request HTTP timeout. Defaults to defaultTimeout.
	Timeout time.Duration
}

// NewDispatcher creates a Dispatcher from the given endpoint configs and logger.
// All HTTP requests share a single *http.Client for connection pooling.
func NewDispatcher(cfgs []DispatcherConfig, logger *slog.Logger) (*Dispatcher, error) {
	eps := make([]endpoint, 0, len(cfgs))
	for i, cfg := range cfgs {
		if cfg.URL == "" {
			return nil, fmt.Errorf("webhook endpoint[%d]: url is required", i)
		}
		if len(cfg.Events) == 0 {
			return nil, fmt.Errorf("webhook endpoint[%d]: events must not be empty", i)
		}

		f, err := newFormatter(cfg.Format)
		if err != nil {
			return nil, fmt.Errorf("webhook endpoint[%d]: %w", i, err)
		}

		timeout := cfg.Timeout
		if timeout <= 0 {
			timeout = defaultTimeout
		}

		allEvents := false
		for _, ev := range cfg.Events {
			if ev == "*" {
				allEvents = true
				break
			}
		}

		eps = append(eps, endpoint{
			url:       cfg.URL,
			events:    cfg.Events,
			allEvents: allEvents,
			formatter: f,
			timeout:   timeout,
		})
	}

	return &Dispatcher{
		endpoints:  eps,
		httpClient: &http.Client{},
		logger:     logger,
	}, nil
}

// newFormatter returns the WebhookFormatter for the given format string.
func newFormatter(format ports.WebhookFormat) (ports.WebhookFormatter, error) {
	switch format {
	case ports.WebhookFormatSlack:
		return &SlackFormatter{}, nil
	case ports.WebhookFormatDiscord:
		return &DiscordFormatter{}, nil
	case ports.WebhookFormatRaw, "":
		return &RawFormatter{}, nil
	default:
		return nil, fmt.Errorf("unknown webhook format %q; accepted: raw, slack, discord", format)
	}
}

// Dispatch sends event to all configured endpoints whose event filter matches.
// Each matching endpoint delivery runs in a separate goroutine so that slow or
// failing webhooks never block the event logging pipeline.
// Dispatch always returns nil — webhook errors are logged, not propagated.
func (d *Dispatcher) Dispatch(ctx context.Context, event events.Event) error {
	for i := range d.endpoints {
		ep := d.endpoints[i] // copy to avoid loop-variable capture
		if !ep.matches(event.EventType) {
			continue
		}
		go d.deliverWithRetry(ctx, ep, event)
	}
	return nil
}

// deliverWithRetry attempts to deliver event to ep up to maxAttempts times
// with exponential backoff. After the final failure it logs a dead-letter entry.
func (d *Dispatcher) deliverWithRetry(ctx context.Context, ep endpoint, event events.Event) {
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			delay := retryDelays[attempt-1]
			select {
			case <-ctx.Done():
				d.logger.WarnContext(ctx, "webhook delivery cancelled during backoff",
					slog.String("url", ep.url),
					slog.String("event_type", event.EventType),
					slog.Int("attempt", attempt+1),
				)
				return
			case <-time.After(delay):
			}
		}

		err := d.deliver(ctx, ep, event)
		if err == nil {
			if attempt > 0 {
				d.logger.InfoContext(ctx, "webhook delivery succeeded after retry",
					slog.String("url", ep.url),
					slog.String("event_type", event.EventType),
					slog.Int("attempt", attempt+1),
				)
			}
			return
		}
		lastErr = err
		d.logger.WarnContext(ctx, "webhook delivery attempt failed",
			slog.String("url", ep.url),
			slog.String("event_type", event.EventType),
			slog.Int("attempt", attempt+1),
			slog.Int("max_attempts", maxAttempts),
			slog.String("error", err.Error()),
		)
	}

	// All attempts exhausted — emit dead-letter log entry.
	d.logger.ErrorContext(ctx, "webhook delivery dead-lettered: all attempts exhausted",
		slog.String("url", ep.url),
		slog.String("event_type", event.EventType),
		slog.String("ai_summary", event.AISummary),
		slog.String("final_error", lastErr.Error()),
	)
}

// deliver performs a single HTTP POST of event to ep.
func (d *Dispatcher) deliver(ctx context.Context, ep endpoint, event events.Event) error {
	body, err := ep.formatter.Format(event)
	if err != nil {
		return fmt.Errorf("formatting event: %w", err)
	}

	reqCtx, cancel := context.WithTimeout(ctx, ep.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, ep.url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "VibeWarden/1")

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("HTTP POST: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("unexpected HTTP status: %d", resp.StatusCode)
	}
	return nil
}

// Interface guard.
var _ ports.WebhookDispatcher = (*Dispatcher)(nil)
