package crtsh_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/vibewarden/vibewarden/internal/adapters/crtsh"
	"github.com/vibewarden/vibewarden/internal/domain/tlspreflight"
)

// newTestClient builds a Client pointing at the given httptest server and
// using a 5-second timeout.
func newTestClient(t *testing.T, srv *httptest.Server) *crtsh.Client {
	t.Helper()
	return crtsh.NewClientWithBase(&http.Client{Timeout: 5 * time.Second}, srv.URL)
}

// serve returns an httptest.Server that responds with the given status, body,
// and Content-Type on every request.
func serve(status int, body, ct string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if ct != "" {
			w.Header().Set("Content-Type", ct)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
}

const realisticJSON = `[
  {"not_before":"2026-04-20T10:00:00","issuer_name":"C=US, O=Let's Encrypt, CN=R3","common_name":"example.com","name_value":"example.com"},
  {"not_before":"2026-04-18T08:30:00","issuer_name":"C=US, O=Let's Encrypt, CN=E1","common_name":"example.com","name_value":"example.com"},
  {"not_before":"2026-03-01T00:00:00","issuer_name":"C=US, O=Sectigo Limited, CN=Sectigo RSA","common_name":"example.com","name_value":"example.com"}
]`

func TestClient_Query_200_RealisticJSON(t *testing.T) {
	srv := serve(200, realisticJSON, "application/json")
	defer srv.Close()

	c := newTestClient(t, srv)
	recs, err := c.Query(context.Background(), "example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(recs) != 3 {
		t.Errorf("len(recs) = %d, want 3", len(recs))
	}
	// First record's IssuerName should contain Let's Encrypt.
	if !strings.Contains(recs[0].IssuerName, "Let's Encrypt") {
		t.Errorf("recs[0].IssuerName = %q, want Let's Encrypt", recs[0].IssuerName)
	}
}

func TestClient_Query_200_EmptyArray(t *testing.T) {
	srv := serve(200, "[]", "application/json")
	defer srv.Close()

	c := newTestClient(t, srv)
	recs, err := c.Query(context.Background(), "unknown.example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(recs) != 0 {
		t.Errorf("len(recs) = %d, want 0", len(recs))
	}
}

func TestClient_Query_200_EmptyBody(t *testing.T) {
	srv := serve(200, "", "application/json")
	defer srv.Close()

	c := newTestClient(t, srv)
	recs, err := c.Query(context.Background(), "example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if recs != nil {
		t.Errorf("expected nil recs for empty body, got %v", recs)
	}
}

func TestClient_Query_200_MalformedJSON(t *testing.T) {
	srv := serve(200, `{"not_an_array": true}`, "application/json")
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.Query(context.Background(), "example.com")
	if !errors.Is(err, tlspreflight.ErrCTResponseMalformed) {
		t.Errorf("error = %v, want ErrCTResponseMalformed", err)
	}
}

func TestClient_Query_200_HTMLContentType(t *testing.T) {
	srv := serve(200, "<html>error</html>", "text/html; charset=utf-8")
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.Query(context.Background(), "example.com")
	if !errors.Is(err, tlspreflight.ErrCTResponseMalformed) {
		t.Errorf("error = %v, want ErrCTResponseMalformed", err)
	}
}

func TestClient_Query_429_Throttled(t *testing.T) {
	srv := serve(429, "Too Many Requests", "text/plain")
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.Query(context.Background(), "example.com")
	if !errors.Is(err, tlspreflight.ErrCTThrottled) {
		t.Errorf("error = %v, want ErrCTThrottled", err)
	}
}

func TestClient_Query_500_Unavailable(t *testing.T) {
	srv := serve(500, "Internal Server Error", "text/plain")
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.Query(context.Background(), "example.com")
	if !errors.Is(err, tlspreflight.ErrCTUnavailable) {
		t.Errorf("error = %v, want ErrCTUnavailable", err)
	}
}

func TestClient_Query_ContextCancel_Unavailable(t *testing.T) {
	// A server that hangs until the client gives up.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	c := crtsh.NewClientWithBase(&http.Client{Timeout: 5 * time.Second}, srv.URL)
	_, err := c.Query(ctx, "example.com")
	if !errors.Is(err, tlspreflight.ErrCTUnavailable) {
		t.Errorf("error = %v, want ErrCTUnavailable", err)
	}
}

func TestClient_Query_URLEscaping(t *testing.T) {
	var gotURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte("[]"))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.Query(context.Background(), "*.example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(gotURL, "Identity") {
		t.Errorf("query string %q does not contain Identity param", gotURL)
	}
}

func TestClient_Query_UserAgentSent(t *testing.T) {
	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte("[]"))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, _ = c.Query(context.Background(), "example.com")
	if !strings.Contains(gotUA, "vibew-doctor") {
		t.Errorf("User-Agent = %q, want to contain 'vibew-doctor'", gotUA)
	}
}

func TestClient_Query_MalformedNotBeforeRowSkipped(t *testing.T) {
	// One record has a valid not_before, one has a malformed value.
	body := `[
  {"not_before":"2026-04-20T10:00:00","issuer_name":"C=US, O=Let's Encrypt, CN=R3","common_name":"example.com","name_value":"example.com"},
  {"not_before":"not-a-date","issuer_name":"C=US, O=Let's Encrypt, CN=R3","common_name":"example.com","name_value":"example.com"}
]`
	srv := serve(200, body, "application/json")
	defer srv.Close()

	c := newTestClient(t, srv)
	recs, err := c.Query(context.Background(), "example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Only the valid row should be returned.
	if len(recs) != 1 {
		t.Errorf("len(recs) = %d, want 1 (malformed row should be skipped)", len(recs))
	}
}
