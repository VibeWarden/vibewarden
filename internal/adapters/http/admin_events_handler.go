package http

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/vibewarden/vibewarden/internal/ports"
)

const (
	// defaultEventsLimit is the number of events returned per query when the
	// caller does not supply a limit query parameter.
	defaultEventsLimit = 50

	// maxEventsLimit caps the limit query parameter to prevent excessively large
	// in-memory responses.
	maxEventsLimit = 500
)

// eventsResponse is the JSON body returned by GET /_vibewarden/admin/events.
type eventsResponse struct {
	Events []eventItem `json:"events"`
	Cursor uint64      `json:"cursor"`
}

// eventItem is the JSON representation of a single stored event.
type eventItem struct {
	Cursor    uint64 `json:"cursor"`
	EventType string `json:"event_type"`
	Timestamp string `json:"timestamp,omitempty"`
	AISummary string `json:"ai_summary,omitempty"`
	// Payload is included as-is from the domain event.
	Payload map[string]any `json:"payload,omitempty"`
}

// WithEventRingBuffer returns a shallow copy of h with the EventRingBuffer set.
// It is called from serve.go after the ring buffer has been constructed.
//
// When ringBuf is nil, the events endpoint returns 503 Service Unavailable.
func (h *AdminHandlers) WithEventRingBuffer(ringBuf ports.EventRingBuffer) *AdminHandlers {
	return &AdminHandlers{
		svc:       h.svc,
		reloader:  h.reloader,
		ringBuf:   ringBuf,
		proposals: h.proposals,
		logger:    h.logger,
	}
}

// listEvents handles GET /_vibewarden/admin/events.
//
// Query parameters:
//   - since  (optional uint64) — return only events with cursor > since; omit or 0 for all
//   - type   (optional comma-separated string) — filter by event type(s)
//   - limit  (optional int, default 50, max 500)
func (h *AdminHandlers) listEvents(w http.ResponseWriter, r *http.Request) {
	if h.ringBuf == nil {
		h.logger.Error("events query requested but no ring buffer is configured")
		writeError(w, http.StatusServiceUnavailable, "service_unavailable", "event ring buffer is not available")
		return
	}

	since := parseUint64Query(r, "since", 0)

	limit := parseIntQuery(r, "limit", defaultEventsLimit)
	if limit < 1 {
		limit = defaultEventsLimit
	}
	if limit > maxEventsLimit {
		limit = maxEventsLimit
	}

	var types []string
	if raw := r.URL.Query().Get("type"); raw != "" {
		for _, t := range strings.Split(raw, ",") {
			t = strings.TrimSpace(t)
			if t != "" {
				types = append(types, t)
			}
		}
	}

	stored, newCursor := h.ringBuf.Query(since, types, limit)

	items := make([]eventItem, 0, len(stored))
	for _, se := range stored {
		item := eventItem{
			Cursor:    se.Cursor,
			EventType: se.Event.EventType,
			AISummary: se.Event.AISummary,
		}
		if !se.Event.Timestamp.IsZero() {
			item.Timestamp = se.Event.Timestamp.UTC().Format("2006-01-02T15:04:05Z07:00")
		}
		if len(se.Event.Payload) > 0 {
			item.Payload = se.Event.Payload
		}
		items = append(items, item)
	}

	writeJSON(w, http.StatusOK, eventsResponse{
		Events: items,
		Cursor: newCursor,
	})
}

// parseUint64Query reads a named query parameter as a uint64. Returns def if
// the parameter is missing or cannot be parsed.
func parseUint64Query(r *http.Request, name string, def uint64) uint64 {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return def
	}
	v, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return def
	}
	return v
}
