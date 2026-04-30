package probe

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"text/tabwriter"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// Render writes the probe result to w. The output format depends on the
// result and error:
//
//   - nil error, upstream=="ok"         → dev-ok or env-ok block + "OK" summary
//   - ErrBootGapExhausted               → degraded block + "DEGRADED" summary
//   - ErrTLSRetryExhausted              → "ERROR:" TLS exhausted block with ACME hint
//   - ErrProbeRefused (EnvName=="")     → "ERROR:" URL + "Stack is not running" hint
//   - ErrProbeRefused (EnvName!="")     → "ERROR:" URL + per-env causes list
//   - ErrDNSFailure (EnvName=="")       → "ERROR:" URL + unexpected localhost hint
//   - ErrDNSFailure (EnvName!="")       → "ERROR:" URL + tls.domain/DNS hint
//   - *ProbeNon200Error                 → URL block + "ERROR: non-2xx" summary
//   - ErrProbeMalformed                 → "ERROR:" URL + malformed hint
//   - nil error, upstream!="ok"         → degraded block (failing/unknown) + "DEGRADED" summary
//
// All output — including errors — is written to w. The caller controls whether
// w is stdout or stderr.
func Render(w io.Writer, result Result, err error) {
	switch {
	case errors.Is(err, ErrTLSRetryExhausted):
		renderTLSRetryExhausted(w, result)
	case errors.Is(err, ports.ErrProbeRefused):
		renderRefused(w, result)
	case errors.Is(err, ports.ErrDNSFailure):
		renderDNSFailure(w, result)
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

// renderRefused writes the connection-refused error block. When EnvName is
// set (--env path) the message lists production-specific causes. For the
// default local-dev path it keeps the familiar "vibew dev" hint.
func renderRefused(w io.Writer, result Result) {
	fmt.Fprintf(w, "ERROR: %s\n", result.URL)
	if result.EnvName == "" {
		fmt.Fprintf(w, "Stack is not running. Start with: vibew dev\n")
		return
	}
	domain := hostnameFromURL(result.URL)
	fmt.Fprintf(w, "  connection refused\n")
	fmt.Fprintf(w, "\nThe %s endpoint is not reachable. Possible causes:\n", result.EnvName)
	fmt.Fprintf(w, "  - the bundle hasn't been deployed yet (see `vibew bundle` Next: block)\n")
	fmt.Fprintf(w, "  - the remote host is down\n")
	fmt.Fprintf(w, "  - DNS for %s doesn't resolve to the deploy host\n", domain)
	fmt.Fprintf(w, "  - the sidecar container exited (ssh in and check `docker compose ps`)\n")
}

// renderDNSFailure writes the DNS-resolution-failure error block. When
// EnvName is set it points the user at tls.domain in the env YAML file.
// For the default local-dev path it flags an unexpected /etc/hosts problem.
func renderDNSFailure(w io.Writer, result Result) {
	domain := hostnameFromURL(result.URL)
	fmt.Fprintf(w, "ERROR: %s\n", result.URL)
	fmt.Fprintf(w, "  DNS does not resolve\n")
	if result.EnvName == "" {
		fmt.Fprintf(w, "\nUnexpected — localhost should always resolve. Check /etc/hosts.\n")
		return
	}
	fmt.Fprintf(w, "\nCheck tls.domain in vibewarden.%s.yaml and the DNS A/AAAA records\nfor %s.\n", result.EnvName, domain)
}

// hostnameFromURL extracts the bare hostname (without port) from rawURL.
// Falls back to rawURL itself if parsing fails.
func hostnameFromURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	return u.Hostname()
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

// renderTLSRetryExhausted writes the TLS-retry-exhausted error block. The
// budget duration is read from result.TLSRetryBudget (set by Run from
// Options.TLSRetryWait) so the output is parameterised rather than hardcoded.
func renderTLSRetryExhausted(w io.Writer, result Result) {
	envName := result.EnvName
	if envName == "" {
		envName = "<env>"
	}
	budgetSec := int(result.TLSRetryBudget.Seconds())
	fmt.Fprintf(w, "ERROR: TLS handshake failed for %ds.\n", budgetSec)
	fmt.Fprintf(w, "\nLikely ACME (Let's Encrypt) issuance still in progress. Check:\n")
	fmt.Fprintf(w, "  ssh <host> docker compose logs vibewarden | grep -i acme\n")
	fmt.Fprintf(w, "If the cert hasn't been issued yet, retry `vibew probe --env %s`\n", envName)
	fmt.Fprintf(w, "in another minute.\n")
}

// isNon200 reports whether err (or any error in its chain) is a *ProbeNon200Error.
func isNon200(err error) bool {
	if err == nil {
		return false
	}
	var ne *ports.ProbeNon200Error
	return errors.As(err, &ne)
}
