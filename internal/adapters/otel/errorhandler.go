package otel

import (
	"log/slog"
	"sync"

	"go.opentelemetry.io/otel"
)

// ErrorHandler implements otel.ErrorHandler by routing OTel SDK background
// errors (metric, trace and log export failures) into slog instead of the
// stdlib logger.
//
// The OTel SDK default handler writes to the stdlib log package. Caddy
// redirects the stdlib logger into its own zap logger at info level, so a
// permanently unreachable OTLP collector — the normal case in dev mode, where
// the observability compose profile is not running — produces an info-level
// log line on every export interval.
//
// ErrorHandler collapses that stream: the first occurrence of each distinct
// error message is logged at warn level so the misconfiguration is still
// visible, and every later occurrence of that same message is demoted to debug
// with a repeat counter. Messages are tracked independently, because the SDK
// runs several export loops concurrently — a stack with observability enabled
// pushes metrics and logs to the same collector on different intervals — and
// their failures interleave. Remembering only the previous message would make
// every alternation look new and warn forever.
//
// ErrorHandler is safe for concurrent use.
type ErrorHandler struct {
	logger *slog.Logger

	mu   sync.Mutex
	seen map[string]int
}

// maxTrackedMessages bounds the dedup table. The realistic ceiling is a handful
// of distinct SDK export errors; the bound keeps memory flat if an exporter
// ever produces high-cardinality messages. On overflow the table is cleared
// wholesale, which at worst re-warns once per message afterwards.
const maxTrackedMessages = 64

// NewErrorHandler creates an ErrorHandler that logs through the given logger.
// A nil logger falls back to slog.Default at log time.
func NewErrorHandler(logger *slog.Logger) *ErrorHandler {
	return &ErrorHandler{logger: logger}
}

// InstallErrorHandler registers a slog-backed ErrorHandler as the global OTel
// error handler. It must be called before the OTel SDK starts exporting so
// that background export failures never reach the stdlib logger.
func InstallErrorHandler(logger *slog.Logger) {
	otel.SetErrorHandler(NewErrorHandler(logger))
}

// Handle implements otel.ErrorHandler. The first occurrence of an error
// message is logged at warn level; every later occurrence of that same message
// is logged at debug level with a repeat count, regardless of which other
// messages were handled in between. Nil errors are ignored.
func (h *ErrorHandler) Handle(err error) {
	if err == nil {
		return
	}

	msg := err.Error()

	h.mu.Lock()
	if h.seen == nil {
		h.seen = make(map[string]int)
	}
	repeats, known := h.seen[msg]
	if known {
		repeats++
	} else if len(h.seen) >= maxTrackedMessages {
		clear(h.seen)
	}
	h.seen[msg] = repeats
	first := !known
	h.mu.Unlock()

	logger := h.logger
	if logger == nil {
		logger = slog.Default()
	}

	if first {
		logger.Warn("otel sdk background error", slog.String("error", msg))
		return
	}
	logger.Debug("otel sdk background error repeated",
		slog.String("error", msg),
		slog.Int("repeat_count", repeats),
	)
}
