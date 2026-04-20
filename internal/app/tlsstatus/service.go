package tlsstatus

import (
	"context"
	"fmt"
	"regexp"

	"github.com/vibewarden/vibewarden/internal/domain/tls"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// Service is the application service that inspects remote TLS certificates by
// running openssl commands over SSH. It orchestrates the remote executor and the
// openssl output parser.
type Service struct {
	executor ports.RemoteExecutor
}

// NewService creates a new TLS status service using the provided remote executor
// for running commands on the remote host.
func NewService(executor ports.RemoteExecutor) *Service {
	return &Service{executor: executor}
}

// opensslCmd returns the shell command to inspect a TLS certificate for the
// given domain and port.
func opensslCmd(domain string, port int) string {
	return fmt.Sprintf(
		"echo | openssl s_client -connect %s:%d -servername %s 2>/dev/null | openssl x509 -noout -subject -issuer -dates -serial -ext subjectAltName",
		domain, port, domain,
	)
}

// validDomain matches safe domain names and wildcards (no shell metacharacters).
var validDomain = regexp.MustCompile(`^[a-zA-Z0-9*][a-zA-Z0-9.*-]*$`)

// Inspect runs openssl on the remote host to retrieve the TLS certificate for
// the given domain and port, parses the output, and returns the certificate
// information as a domain value object.
func (s *Service) Inspect(ctx context.Context, domain string, port int) (tls.CertInfo, error) {
	if domain == "" {
		return tls.CertInfo{}, fmt.Errorf("domain cannot be empty")
	}
	if !validDomain.MatchString(domain) {
		return tls.CertInfo{}, fmt.Errorf("invalid domain %q: contains unsafe characters", domain)
	}
	if port < 1 || port > 65535 {
		return tls.CertInfo{}, fmt.Errorf("port %d is out of range (1-65535)", port)
	}

	cmd := opensslCmd(domain, port)
	output, err := s.executor.Run(ctx, cmd)
	if err != nil {
		return tls.CertInfo{}, fmt.Errorf("running openssl on remote host: %w", err)
	}

	certInfo, err := ParseOpenSSLOutput(output)
	if err != nil {
		return tls.CertInfo{}, fmt.Errorf("parsing certificate output for %s:%d: %w", domain, port, err)
	}

	return certInfo, nil
}
