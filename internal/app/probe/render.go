package probe

import (
	"errors"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// Render writes the probe result to w. The output format depends on the
// result and error:
//
//   - nil error, upstream=="ok"         → dev-ok or env-ok block + "OK" summary
//   - ErrBootGapExhausted               → degraded block + "DEGRADED" summary
//   - ErrProbeRefused                   → "ERROR:" URL + stack-not-running hint
//   - *ProbeNon200Error                 → URL block + "ERROR: non-2xx" summary
//   - ErrProbeMalformed                 → "ERROR:" URL + malformed hint
//   - nil error, upstream!="ok"         → degraded block (failing/unknown) + "DEGRADED" summary
//
// All output — including errors — is written to w. The caller controls whether
// w is stdout or stderr.
func Render(w io.Writer, result Result, err error) {
	switch {
	case errors.Is(err, ports.ErrProbeRefused):
		renderRefused(w, result)
	case errors.Is(err, ports.ErrProbeMalformed):
		renderMalformed(w, result)
	case isNon200(err):
		renderNon200(w, result, err)
	case errors.Is(err, ErrBootGapExhausted):
		renderHealthBlock(w, result)
		fmt.Fprintf(w, "\nDEGRADED — upstream probe has not converged within 10s. Check `vibew logs vibewarden`.\n")
	case err != nil:
		// Generic / TLS / network error.
		renderGenericError(w, result, err)
	default:
		// No error: render the health block and pick the summary based on state.
		renderHealthBlock(w, result)
		if result.Doc.Components["upstream"] == "ok" {
			renderOKSummary(w, result)
		} else {
			// Upstream is "failing" or some other non-ok state.
			renderDegradedSummary(w, result)
		}
	}
}

// renderHealthBlock writes the URL + status table common to the ok, degraded,
// and boot-gap-exhausted paths.
func renderHealthBlock(w io.Writer, result Result) {
	fmt.Fprintf(w, "%s\n", result.URL)

	tw := tabwriter.NewWriter(w, 0, 0, 1, ' ', 0)
	fmt.Fprintf(tw, "  status:\t %s\n", result.Doc.Status)
	fmt.Fprintf(tw, "  version:\t %s\n", result.Doc.Version)
	fmt.Fprintf(tw, "  components.sidecar:\t %s\n", result.Doc.Components["sidecar"])
	fmt.Fprintf(tw, "  components.upstream:\t %s\n", result.Doc.Components["upstream"])
	_ = tw.Flush()
}

// renderOKSummary writes the trailing OK line, customised for the env.
func renderOKSummary(w io.Writer, result Result) {
	if result.EnvName == "" {
		fmt.Fprintf(w, "\nOK — dev stack healthy.\n")
	} else {
		fmt.Fprintf(w, "\nOK — %s stack healthy.\n", result.EnvName)
	}
}

// renderDegradedSummary writes the trailing DEGRADED line for a non-ok,
// non-exhausted upstream state (e.g. "failing").
func renderDegradedSummary(w io.Writer, _ Result) {
	fmt.Fprintf(w, "\nDEGRADED — upstream is not healthy. Check `vibew logs vibewarden`.\n")
}

// renderRefused writes the connection-refused error block.
func renderRefused(w io.Writer, result Result) {
	fmt.Fprintf(w, "ERROR: %s\n", result.URL)
	fmt.Fprintf(w, "Stack is not running. Start with: vibew dev\n")
}

// renderMalformed writes the malformed-body error block.
func renderMalformed(w io.Writer, result Result) {
	fmt.Fprintf(w, "ERROR: %s\n", result.URL)
	fmt.Fprintf(w, "Malformed response body — not the VibeWarden health wire format.\n")
}

// renderNon200 writes the non-2xx error block.
func renderNon200(w io.Writer, result Result, err error) {
	var ne *ports.ProbeNon200Error
	if !errors.As(err, &ne) {
		renderGenericError(w, result, err)
		return
	}
	fmt.Fprintf(w, "%s\n", result.URL)

	tw := tabwriter.NewWriter(w, 0, 0, 1, ' ', 0)
	fmt.Fprintf(tw, "  http_status:\t %d\n", ne.StatusCode)
	fmt.Fprintf(tw, "  body:\t %s\n", ne.Body)
	_ = tw.Flush()

	fmt.Fprintf(w, "\nERROR: non-2xx response from health endpoint.\n")
}

// renderGenericError writes a generic error block for TLS / network errors.
func renderGenericError(w io.Writer, result Result, err error) {
	fmt.Fprintf(w, "ERROR: %s\n", result.URL)
	fmt.Fprintf(w, "%v\n", err)
}

// isNon200 reports whether err (or any error in its chain) is a *ProbeNon200Error.
func isNon200(err error) bool {
	if err == nil {
		return false
	}
	var ne *ports.ProbeNon200Error
	return errors.As(err, &ne)
}
