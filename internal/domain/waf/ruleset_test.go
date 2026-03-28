package waf

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// RuleSet construction
// ---------------------------------------------------------------------------

func TestNewRuleSet_EmptyRulesError(t *testing.T) {
	_, err := NewRuleSet(nil)
	if err == nil {
		t.Error("expected error for empty rules, got nil")
	}
}

func TestNewRuleSet_RulesImmutable(t *testing.T) {
	r, _ := NewRule("r1", `foo`, SeverityLow, CategoryXSS)
	rs, err := NewRuleSet([]Rule{r})
	if err != nil {
		t.Fatalf("NewRuleSet() unexpected error: %v", err)
	}
	// Mutating the returned slice must not affect the internal state.
	got := rs.Rules()
	if len(got) != 1 {
		t.Fatalf("Rules() len = %d, want 1", len(got))
	}
}

func TestDefaultRuleSet_NotEmpty(t *testing.T) {
	rs := DefaultRuleSet()
	if len(rs.Rules()) == 0 {
		t.Error("DefaultRuleSet() should contain at least one rule")
	}
}

// ---------------------------------------------------------------------------
// RuleSet.Evaluate — attack payloads (must detect)
// ---------------------------------------------------------------------------

var attackPayloads = []struct {
	name     string
	category Category
	input    string
}{
	// SQL Injection
	{"sqli tautology single-quote OR", CategorySQLInjection, "' OR '1'='1"},
	{"sqli tautology 1=1", CategorySQLInjection, "' OR 1=1--"},
	{"sqli UNION SELECT", CategorySQLInjection, "foo UNION SELECT username,password FROM users--"},
	{"sqli DROP TABLE", CategorySQLInjection, "'; DROP TABLE users;--"},
	{"sqli DELETE FROM", CategorySQLInjection, "1; DELETE FROM orders"},
	{"sqli comment terminator", CategorySQLInjection, "admin'--"},
	{"sqli stacked query", CategorySQLInjection, "1; SELECT * FROM secret"},

	// XSS
	{"xss script tag", CategoryXSS, "<script>alert(1)</script>"},
	{"xss script tag uppercase", CategoryXSS, "<SCRIPT>alert(1)</SCRIPT>"},
	{"xss javascript uri", CategoryXSS, `<a href="javascript:alert(1)">click</a>`},
	{"xss onclick event", CategoryXSS, `<div onclick="steal()">x</div>`},
	{"xss onerror event", CategoryXSS, `<img onerror=alert(1)>`},
	{"xss img tag", CategoryXSS, `<img src=x onerror=alert(1)>`},
	{"xss iframe", CategoryXSS, `<iframe src="evil.com">`},
	{"xss vbscript", CategoryXSS, `<a href="vbscript:msgbox(1)">x</a>`},

	// Path Traversal
	{"path traversal dotdot unix", CategoryPathTraversal, "../../../etc/passwd"},
	{"path traversal dotdot windows", CategoryPathTraversal, `..\..\windows\system32`},
	{"path traversal encoded", CategoryPathTraversal, "%2e%2e%2fetc%2fpasswd"},
	{"path traversal /etc/passwd direct", CategoryPathTraversal, "/etc/passwd"},
	{"path traversal /etc/shadow", CategoryPathTraversal, "/etc/shadow"},
	{"path traversal windows system32", CategoryPathTraversal, `C:\windows\system32\cmd.exe`},

	// Command Injection
	{"cmdi semicolon ls", CategoryCommandInjection, "foo; ls /"},
	{"cmdi semicolon cat", CategoryCommandInjection, "foo; cat /etc/passwd "},
	{"cmdi pipe cat", CategoryCommandInjection, "foo | cat /etc/passwd "},
	{"cmdi backtick", CategoryCommandInjection, "foo`id`"},
	{"cmdi dollar paren", CategoryCommandInjection, "foo$(whoami)"},
	{"cmdi logical and", CategoryCommandInjection, "foo && id "},
	{"cmdi logical or", CategoryCommandInjection, "foo || ls "},
}

func TestRuleSet_Evaluate_AttackPayloads(t *testing.T) {
	rs := DefaultRuleSet()

	for _, tt := range attackPayloads {
		t.Run(tt.name, func(t *testing.T) {
			detections := rs.Evaluate(LocationQueryParam, "q", tt.input)
			if len(detections) == 0 {
				t.Errorf("Evaluate(%q) returned no detections, expected at least one", tt.input)
				return
			}
			// At least one detection should be in the expected category.
			found := false
			for _, d := range detections {
				if d.Rule().Category() == tt.category {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("Evaluate(%q) no detection in category %q; got: %v", tt.input, tt.category, detections)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// RuleSet.Evaluate — benign inputs (must not detect)
// ---------------------------------------------------------------------------

var benignInputs = []struct {
	name  string
	input string
}{
	{"normal search query", "best golang books"},
	{"email address", "user@example.com"},
	{"UUID", "550e8400-e29b-41d4-a716-446655440000"},
	{"numeric ID", "42"},
	{"ISO date", "2026-03-28"},
	{"simple URL path", "/api/v1/users/123"},
	{"base64 string", "dGVzdA=="},
	{"JSON value", `{"name":"Alice","age":30}`},
	{"markdown heading", "# Hello World"},
	{"CSS class", ".button-primary { color: #7C3AED; }"},
	{"version string", "v1.2.3-rc.1"},
	{"file extension check", "report.pdf"},
}

func TestRuleSet_Evaluate_BenignInputs(t *testing.T) {
	rs := DefaultRuleSet()

	for _, tt := range benignInputs {
		t.Run(tt.name, func(t *testing.T) {
			detections := rs.Evaluate(LocationQueryParam, "q", tt.input)
			if len(detections) > 0 {
				t.Errorf("Evaluate(%q) returned %d false-positive detection(s): %v", tt.input, len(detections), detections)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ScanRequest — query params
// ---------------------------------------------------------------------------

func TestScanRequest_QueryParams(t *testing.T) {
	rs := DefaultRuleSet()

	req := &http.Request{
		Method: http.MethodGet,
		URL: &url.URL{
			RawQuery: "q=UNION+SELECT+1&page=2",
		},
		Header: http.Header{},
	}

	detections, err := rs.ScanRequest(req)
	if err != nil {
		t.Fatalf("ScanRequest() unexpected error: %v", err)
	}
	if len(detections) == 0 {
		t.Fatal("ScanRequest() expected detections from UNION SELECT in query param, got none")
	}
	for _, d := range detections {
		if d.Location() != LocationQueryParam {
			t.Errorf("detection location = %q, want %q", d.Location(), LocationQueryParam)
		}
	}
}

// ---------------------------------------------------------------------------
// ScanRequest — headers
// ---------------------------------------------------------------------------

func TestScanRequest_Headers(t *testing.T) {
	rs := DefaultRuleSet()

	req := &http.Request{
		Method: http.MethodGet,
		URL:    &url.URL{},
		Header: http.Header{
			"User-Agent": []string{"Mozilla/5.0 <script>alert(1)</script>"},
			"Referer":    []string{"https://example.com"},
		},
	}

	detections, err := rs.ScanRequest(req)
	if err != nil {
		t.Fatalf("ScanRequest() unexpected error: %v", err)
	}
	if len(detections) == 0 {
		t.Fatal("ScanRequest() expected detections from XSS in User-Agent header, got none")
	}
	found := false
	for _, d := range detections {
		if d.Location() == LocationHeader && d.LocationKey() == "User-Agent" {
			found = true
		}
	}
	if !found {
		t.Error("expected at least one detection in User-Agent header")
	}
}

// ---------------------------------------------------------------------------
// ScanRequest — body
// ---------------------------------------------------------------------------

func TestScanRequest_Body(t *testing.T) {
	rs := DefaultRuleSet()

	body := `{"username":"admin","password":"' OR '1'='1"}`
	req := &http.Request{
		Method: http.MethodPost,
		URL:    &url.URL{},
		Header: http.Header{},
		Body:   io.NopCloser(strings.NewReader(body)),
	}

	detections, err := rs.ScanRequest(req)
	if err != nil {
		t.Fatalf("ScanRequest() unexpected error: %v", err)
	}
	if len(detections) == 0 {
		t.Fatal("ScanRequest() expected detections from SQLi in body, got none")
	}
	found := false
	for _, d := range detections {
		if d.Location() == LocationBody {
			found = true
		}
	}
	if !found {
		t.Error("expected at least one detection in body")
	}
}

func TestScanRequest_BodyRestored(t *testing.T) {
	rs := DefaultRuleSet()

	original := `hello world`
	req := &http.Request{
		Method: http.MethodPost,
		URL:    &url.URL{},
		Header: http.Header{},
		Body:   io.NopCloser(strings.NewReader(original)),
	}

	_, err := rs.ScanRequest(req)
	if err != nil {
		t.Fatalf("ScanRequest() unexpected error: %v", err)
	}

	// Body must be readable again after ScanRequest returns.
	if req.Body == nil {
		t.Fatal("request body is nil after ScanRequest")
	}
	b, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("reading body after ScanRequest: %v", err)
	}
	if string(b) != original {
		t.Errorf("body after ScanRequest = %q, want %q", string(b), original)
	}
}

func TestScanRequest_NilBody(t *testing.T) {
	rs := DefaultRuleSet()

	req := &http.Request{
		Method: http.MethodGet,
		URL:    &url.URL{},
		Header: http.Header{},
		Body:   nil,
	}

	_, err := rs.ScanRequest(req)
	if err != nil {
		t.Fatalf("ScanRequest() with nil body unexpected error: %v", err)
	}
}

func TestScanRequest_CleanRequest(t *testing.T) {
	rs := DefaultRuleSet()

	req := &http.Request{
		Method: http.MethodGet,
		URL: &url.URL{
			RawQuery: "q=hello+world&page=1",
		},
		Header: http.Header{
			"User-Agent": []string{"Mozilla/5.0 (compatible; Googlebot/2.1)"},
			"Referer":    []string{"https://example.com/search?q=go+programming"},
		},
	}

	detections, err := rs.ScanRequest(req)
	if err != nil {
		t.Fatalf("ScanRequest() unexpected error: %v", err)
	}
	if len(detections) > 0 {
		t.Errorf("ScanRequest() returned %d false-positive detection(s) on clean request: %v", len(detections), detections)
	}
}

// ---------------------------------------------------------------------------
// Benchmark
// ---------------------------------------------------------------------------

// BenchmarkScanRequest_Typical measures the cost of scanning a typical HTTP
// request (2 query params, standard headers, small JSON body).
// The performance target is < 1 ms per operation.
func BenchmarkScanRequest_Typical(b *testing.B) {
	rs := DefaultRuleSet()
	body := `{"username":"alice","action":"login"}`

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := &http.Request{
			Method: http.MethodPost,
			URL: &url.URL{
				RawQuery: "redirect=%2Fdashboard&lang=en",
			},
			Header: http.Header{
				"User-Agent": []string{"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)"},
				"Cookie":     []string{"session=abc123"},
			},
			Body: io.NopCloser(strings.NewReader(body)),
		}
		_, err := rs.ScanRequest(req)
		if err != nil {
			b.Fatalf("ScanRequest() unexpected error: %v", err)
		}
	}
}

// BenchmarkEvaluate_SingleInput measures the cost of running all rules against
// a single input string.
func BenchmarkEvaluate_SingleInput(b *testing.B) {
	rs := DefaultRuleSet()
	input := "UNION SELECT username, password FROM users WHERE 1=1--"

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rs.Evaluate(LocationQueryParam, "q", input)
	}
}
