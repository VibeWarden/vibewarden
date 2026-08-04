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
// ErrorHandler collapses that stream: the first occurrence of a given error
// message is logged at warn level so the misconfiguration is still visible,
// and every consecutive repeat of the same message is demoted to debug with a
// repeat counter. The dedup state resets whenever the message changes, so a
// different failure is always surfaced at warn.
//
// ErrorHandler is safe for concurrent use.
type ErrorHandler struct {
	logger *slog.Logger

	mu      sync.Mutex
	lastMsg string
	repeats int
}

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
// message is logged at warn level; consecutive repeats of the same message are
// logged at debug level with a repeat count. Nil errors are ignored.
func (h *ErrorHandler) Handle(err error) {
	if err == nil {
		return
	}

	msg := err.Error()

	h.mu.Lock()
	first := msg != h.lastMsg
	if first {
		h.lastMsg = msg
		h.repeats = 0
	} else {
		h.repeats++
	}
	repeats := h.repeats
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
