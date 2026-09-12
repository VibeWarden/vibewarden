package middleware

import "net/http"

// FlushResponseWriter flushes any buffered bytes in w through to the client.
//
// It is the single implementation behind the Flush method of every
// http.ResponseWriter wrapper in VibeWarden. Streaming responses
// (text/event-stream, chunked LLM token streams) only reach the client when a
// flush call survives the whole wrapper chain: a wrapper that embeds
// http.ResponseWriter without re-exporting Flush hides the underlying
// http.Flusher, and the response is buffered until the connection closes.
//
// http.NewResponseController walks the Unwrap chain, so passing the wrapped
// writer (not the wrapper itself) reaches the first writer that can flush. The
// error is deliberately discarded: http.ErrNotSupported means nothing further
// down the chain can flush, which is not a request failure.
//
// Every wrapper must also implement Unwrap() http.ResponseWriter so that
// callers using http.ResponseController directly — Caddy's reverse proxy does,
// for both flushing and Upgrade hijacking — can reach past the wrapper.
func FlushResponseWriter(w http.ResponseWriter) {
	_ = http.NewResponseController(w).Flush()
}
