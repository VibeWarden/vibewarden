package ops

import (
	"net"
	"net/url"
	"strings"
)

// hostKratosAdminURL maps a configured Kratos admin URL onto the URL that
// "vibew status" should actually probe from the host.
//
// The bundled config that ships inside the sidecar container addresses Kratos
// by its Docker Compose service name (e.g. "http://kratos:4434"). That host is
// resolvable only from inside the compose network, so probing it from the host
// always fails — a false FAIL even when the stack is healthy (#1337). The
// generated compose file publishes the Kratos admin port on the host, so the
// equivalent host-side address is the same scheme and port on 127.0.0.1.
//
// It returns the base URL to probe and, when the configured host was
// container-internal, that original host so callers can explain the rewrite.
// A non-container-internal URL (loopback, IP literal, or dotted hostname) is
// returned unchanged with an empty rewrittenFrom.
func hostKratosAdminURL(rawURL string) (probeBase, rewrittenFrom string) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return rawURL, ""
	}
	if !isContainerInternalHost(u.Hostname()) {
		return rawURL, ""
	}

	host := "127.0.0.1"
	if port := u.Port(); port != "" {
		host = net.JoinHostPort(host, port)
	}
	return u.Scheme + "://" + host + strings.TrimSuffix(u.Path, "/"), u.Host
}

// isContainerInternalHost reports whether host is a bare Docker Compose
// service name — a single-label hostname that is neither "localhost" nor an
// IP literal. Such names resolve only inside the container network.
//
// Dotted hostnames (FQDNs) are assumed to be resolvable from the host and are
// therefore probed as configured; this keeps external Kratos deployments
// (kratos.external: true) reported accurately.
func isContainerInternalHost(host string) bool {
	switch {
	case host == "":
		return false
	case strings.EqualFold(host, "localhost"):
		return false
	case net.ParseIP(host) != nil:
		return false
	case strings.Contains(host, "."):
		return false
	default:
		return true
	}
}
