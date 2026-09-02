package ops_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/app/ops"
	"github.com/vibewarden/vibewarden/internal/config"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// decodeDoctorResults unmarshals the JSON doctor report produced by
// DoctorService.Run with DoctorOptions.JSON set.
func decodeDoctorResults(t *testing.T, raw []byte) []ops.CheckResult {
	t.Helper()
	var results []ops.CheckResult
	if err := json.Unmarshal(raw, &results); err != nil {
		t.Fatalf("unmarshal doctor JSON: %v\nraw:\n%s", err, raw)
	}
	return results
}

// kratosDevConfig returns a config describing a project with a vibew-managed
// kratos-migrate service: auth.mode kratos, embedded Kratos, embedded Postgres.
func kratosDevConfig() *config.Config {
	cfg := defaultConfig()
	cfg.Auth.Mode = config.AuthModeKratos
	cfg.Kratos.External = false
	cfg.Database.ExternalURL = ""
	return cfg
}

// pgAuthFailureLogs is a realistic kratos-migrate log tail for the stale-volume
// credential mismatch.
const pgAuthFailureLogs = `kratos-migrate-1  | ERROR: Could not open the database connection
kratos-migrate-1  | failed to connect to ` + "`host=kratos-db user=kratos database=kratos`" + `:
kratos-migrate-1  | server error (FATAL: password authentication failed for user "kratos" (SQLSTATE 28P01))
`

func TestHasKratosDBCredentialMismatch(t *testing.T) {
	tests := []struct {
		name string
		logs string
		want bool
	}{
		{"pgx wrapped form", pgAuthFailureLogs, true},
		{"lib/pq form", `pq: password authentication failed for user "kratos"`, true},
		{"sqlstate only", "server error (SQLSTATE 28P01)", true},
		{"upper case", `PASSWORD AUTHENTICATION FAILED FOR USER "kratos"`, true},
		{"mixed case sqlstate", "SQLState 28p01 returned by driver", true},
		{"unrelated migration failure", "ERROR: relation \"identities\" already exists", false},
		{"connection refused", "dial tcp 172.18.0.2:5432: connect: connection refused", false},
		{"empty logs", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ops.HasKratosDBCredentialMismatchForTest(tt.logs); got != tt.want {
				t.Errorf("HasKratosDBCredentialMismatch() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLocalKratosMigrateService(t *testing.T) {
	tests := []struct {
		name     string
		mutate   func(*config.Config)
		wantTrue bool
	}{
		{"kratos mode, embedded kratos and db", func(*config.Config) {}, true},
		{"auth mode none", func(c *config.Config) { c.Auth.Mode = config.AuthModeNone }, false},
		{"auth mode unset", func(c *config.Config) { c.Auth.Mode = "" }, false},
		{"auth mode jwt", func(c *config.Config) { c.Auth.Mode = config.AuthModeJWT }, false},
		{"external kratos", func(c *config.Config) { c.Kratos.External = true }, false},
		{"external database", func(c *config.Config) {
			c.Database.ExternalURL = "postgres://u:p@db.example.com:5432/kratos"
		}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := kratosDevConfig()
			tt.mutate(cfg)
			if got := ops.LocalKratosMigrateServiceForTest(cfg); got != tt.wantTrue {
				t.Errorf("LocalKratosMigrateService() = %v, want %v", got, tt.wantTrue)
			}
		})
	}
}

func TestLocalKratosMigrateService_NilConfig(t *testing.T) {
	if ops.LocalKratosMigrateServiceForTest(nil) {
		t.Error("LocalKratosMigrateService(nil) = true, want false")
	}
}

func TestKratosCredentialMismatchRenderings(t *testing.T) {
	block := ops.KratosCredentialMismatchBlockForTest()
	detail := ops.KratosCredentialMismatchDetailForTest()

	for _, want := range []string{
		ops.KratosRecoveryCommandForTest,
		ops.KratosDataLossWarningForTest,
		"kratos-db",
	} {
		if !strings.Contains(block, want) {
			t.Errorf("dev block missing %q\ngot:\n%s", want, block)
		}
		if !strings.Contains(detail, want) {
			t.Errorf("doctor detail missing %q\ngot:\n%s", want, detail)
		}
	}

	// AC2: the dev block must be textually distinct from the generic message.
	if strings.Contains(block, "Sidecar failed to start") {
		t.Errorf("dev block must not reuse the generic sidecar message:\n%s", block)
	}
	if !strings.Contains(block, "vibew dev --rebuild --volumes") {
		t.Errorf("dev block missing the alternative recovery command:\n%s", block)
	}

	// T6: the doctor detail is rendered in a fixed-width column; it must stay
	// on a single line.
	if strings.ContainsAny(detail, "\n\r") {
		t.Errorf("doctor detail must be single-line, got:\n%q", detail)
	}
}

// AC8a + AC3: the crash-loop signature produces the specific message and a
// non-zero exit (a non-nil error from Run).
func TestDevService_Run_KratosCredentialMismatch(t *testing.T) {
	fc := &fakeCompose{
		upErr:         errors.New("dependency failed to start: container kratos-migrate-1 exited (1)"),
		logsByService: map[string]string{ops.KratosMigrateServiceNameForTest: pgAuthFailureLogs},
	}
	svc := ops.NewDevService(fc)
	var buf bytes.Buffer

	err := svc.Run(context.Background(), kratosDevConfig(), ops.DevOptions{}, &buf)
	if err == nil {
		t.Fatal("Run() = nil, want credential-mismatch error")
	}
	if !strings.Contains(err.Error(), ops.KratosRecoveryCommandForTest) {
		t.Errorf("error missing recovery command %q:\n%s", ops.KratosRecoveryCommandForTest, err)
	}
	if !strings.Contains(err.Error(), ops.KratosDataLossWarningForTest) {
		t.Errorf("error missing data-loss warning %q:\n%s", ops.KratosDataLossWarningForTest, err)
	}
	if strings.Contains(err.Error(), "starting dev environment") {
		t.Errorf("expected specific message, got generic one:\n%s", err)
	}
	if err.Error() != ops.KratosCredentialMismatchBlockForTest() {
		t.Errorf("error text drifted from the pinned block:\ngot:\n%s\nwant:\n%s",
			err, ops.KratosCredentialMismatchBlockForTest())
	}
}

// AC4 + AC8c: a kratos-migrate failure without the known signature falls back
// to the generic error.
func TestDevService_Run_KratosMigrateOtherFailure_FallsBackToGeneric(t *testing.T) {
	fc := &fakeCompose{
		upErr:         errors.New("dependency failed to start"),
		logsByService: map[string]string{ops.KratosMigrateServiceNameForTest: "ERROR: disk full while applying migration 0042"},
	}
	svc := ops.NewDevService(fc)
	var buf bytes.Buffer

	err := svc.Run(context.Background(), kratosDevConfig(), ops.DevOptions{}, &buf)
	if err == nil {
		t.Fatal("Run() = nil, want generic error")
	}
	if !strings.Contains(err.Error(), "starting dev environment") {
		t.Errorf("expected generic error, got:\n%s", err)
	}
}

// AC4: an unavailable kratos-migrate log stream also falls back to the generic
// error rather than inventing a diagnosis.
func TestDevService_Run_KratosMigrateLogsUnavailable_FallsBackToGeneric(t *testing.T) {
	fc := &fakeCompose{
		upErr:   errors.New("dependency failed to start"),
		logsErr: errors.New("no such service: kratos-migrate"),
	}
	svc := ops.NewDevService(fc)
	var buf bytes.Buffer

	err := svc.Run(context.Background(), kratosDevConfig(), ops.DevOptions{}, &buf)
	if err == nil {
		t.Fatal("Run() = nil, want generic error")
	}
	if !strings.Contains(err.Error(), "starting dev environment") {
		t.Errorf("expected generic error, got:\n%s", err)
	}
}

// AC5 + AC8d: projects without a vibew-managed kratos-migrate service make no
// extra log calls for this check.
func TestDevService_Run_NoLocalKratos_MakesNoLogsCalls(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*config.Config)
	}{
		{"auth mode none", func(c *config.Config) { c.Auth.Mode = config.AuthModeNone }},
		{"external kratos", func(c *config.Config) { c.Kratos.External = true }},
		{"external database", func(c *config.Config) {
			c.Database.ExternalURL = "postgres://u:p@db.example.com:5432/kratos"
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := kratosDevConfig()
			tt.mutate(cfg)

			fc := &fakeCompose{upErr: errors.New("dependency failed to start")}
			svc := ops.NewDevService(fc)
			var buf bytes.Buffer

			err := svc.Run(context.Background(), cfg, ops.DevOptions{}, &buf)
			if err == nil {
				t.Fatal("Run() = nil, want generic error")
			}
			if !strings.Contains(err.Error(), "starting dev environment") {
				t.Errorf("expected generic error, got:\n%s", err)
			}
			if len(fc.logsCalls) != 0 {
				t.Errorf("expected zero Logs calls, got %v", fc.logsCalls)
			}
		})
	}
}

// AC8b: a healthy kratos stack behaves exactly as before — no diagnostic, no
// kratos-migrate log fetch, normal startup summary.
func TestDevService_Run_HealthyKratosStack_Unchanged(t *testing.T) {
	fc := &fakeCompose{
		psResult: []ports.ContainerInfo{
			{Name: "vibewarden-1", Service: "vibewarden", State: "running", Health: "healthy"},
		},
	}
	svc := ops.NewDevService(fc)
	var buf bytes.Buffer

	if err := svc.Run(context.Background(), kratosDevConfig(), ops.DevOptions{}, &buf); err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	for _, svcName := range fc.logsCalls {
		if svcName == ops.KratosMigrateServiceNameForTest {
			t.Errorf("healthy stack must not fetch kratos-migrate logs, got %v", fc.logsCalls)
		}
	}
	if !strings.Contains(buf.String(), "Started. https://localhost:8443") {
		t.Errorf("expected normal startup summary, got:\n%s", buf.String())
	}
}

// Defensive secondary hook: when compose up succeeds but the sidecar never
// reaches running, the credential mismatch still replaces the generic message.
func TestDevService_VerifySidecar_KratosCredentialMismatch(t *testing.T) {
	fc := &fakeCompose{
		psResult: []ports.ContainerInfo{
			{Name: "vibewarden-1", Service: "vibewarden", State: "created"},
		},
		logsByService: map[string]string{ops.KratosMigrateServiceNameForTest: pgAuthFailureLogs},
	}
	svc := ops.NewDevService(fc)
	var buf bytes.Buffer

	err := svc.Run(context.Background(), kratosDevConfig(), ops.DevOptions{}, &buf)
	if err == nil {
		t.Fatal("Run() = nil, want credential-mismatch error")
	}
	if err.Error() != ops.KratosCredentialMismatchBlockForTest() {
		t.Errorf("expected pinned credential-mismatch block, got:\n%s", err)
	}
	if strings.Contains(buf.String(), "Sidecar failed to start") {
		t.Errorf("generic sidecar line must be replaced, got:\n%s", buf.String())
	}
}

// AC8e: doctor reports a FAIL row with the pinned detail on a match.
func TestDoctorService_KratosDBCredentials_Fail(t *testing.T) {
	fc := noContainersCompose()
	fc.logsByService = map[string]string{ops.KratosMigrateServiceNameForTest: pgAuthFailureLogs}
	pc := &fakePortChecker{}
	svc := ops.NewDoctorService(fc, pc)

	cfg := doctorConfig()
	cfg.Auth.Mode = config.AuthModeKratos

	var buf bytes.Buffer
	opts := defaultOpts(t)
	opts.JSON = true

	allOK, err := svc.Run(context.Background(), cfg, opts, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if allOK {
		t.Error("expected allOK = false when the kratos-db credentials conflict")
	}

	results := decodeDoctorResults(t, buf.Bytes())
	found := 0
	for _, r := range results {
		if r.Name != "Kratos DB credentials" {
			continue
		}
		found++
		if r.Severity != ops.SeverityFail {
			t.Errorf("severity = %q, want FAIL", r.Severity)
		}
		if r.Detail != ops.KratosCredentialMismatchDetailForTest() {
			t.Errorf("detail = %q, want %q", r.Detail, ops.KratosCredentialMismatchDetailForTest())
		}
		if r.Section != "Local Runtime" {
			t.Errorf("section = %q, want %q", r.Section, "Local Runtime")
		}
	}
	if found != 1 {
		t.Errorf("expected exactly 1 'Kratos DB credentials' row, got %d", found)
	}
}

// AC8e: doctor reports OK (not FAIL) when the logs carry no signature.
func TestDoctorService_KratosDBCredentials_OK(t *testing.T) {
	fc := noContainersCompose()
	fc.logsByService = map[string]string{
		ops.KratosMigrateServiceNameForTest: "Successfully applied migrations.",
	}
	pc := &fakePortChecker{}
	svc := ops.NewDoctorService(fc, pc)

	cfg := doctorConfig()
	cfg.Auth.Mode = config.AuthModeKratos

	var buf bytes.Buffer
	opts := defaultOpts(t)
	opts.JSON = true

	if _, err := svc.Run(context.Background(), cfg, opts, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, r := range decodeDoctorResults(t, buf.Bytes()) {
		if r.Name == "Kratos DB credentials" && r.Severity != ops.SeverityOK {
			t.Errorf("severity = %q, want OK", r.Severity)
		}
	}
}

// The doctor row is omitted entirely for projects without a vibew-managed
// kratos-migrate service, and no log call is made.
func TestDoctorService_KratosDBCredentials_OmittedWhenNotApplicable(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*config.Config)
		logsErr error
	}{
		{"auth mode not kratos", func(*config.Config) {}, nil},
		{"external kratos", func(c *config.Config) {
			c.Auth.Mode = config.AuthModeKratos
			c.Kratos.External = true
		}, nil},
		{"external database", func(c *config.Config) {
			c.Auth.Mode = config.AuthModeKratos
			c.Database.ExternalURL = "postgres://u:p@db.example.com:5432/kratos"
		}, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fc := noContainersCompose()
			fc.logsErr = tt.logsErr
			pc := &fakePortChecker{}
			svc := ops.NewDoctorService(fc, pc)

			cfg := doctorConfig()
			tt.mutate(cfg)

			var buf bytes.Buffer
			opts := defaultOpts(t)
			opts.JSON = true

			if _, err := svc.Run(context.Background(), cfg, opts, &buf); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			for _, r := range decodeDoctorResults(t, buf.Bytes()) {
				if r.Name == "Kratos DB credentials" {
					t.Errorf("unexpected 'Kratos DB credentials' row for %s", tt.name)
				}
			}
			for _, svcName := range fc.logsCalls {
				if svcName == ops.KratosMigrateServiceNameForTest {
					t.Errorf("expected no kratos-migrate log call, got %v", fc.logsCalls)
				}
			}
		})
	}
}

// The doctor row is omitted when kratos-migrate logs cannot be read (stack was
// never created, service absent).
func TestDoctorService_KratosDBCredentials_OmittedWhenLogsUnavailable(t *testing.T) {
	fc := noContainersCompose()
	fc.logsErr = errors.New("no such service: kratos-migrate")
	pc := &fakePortChecker{}
	svc := ops.NewDoctorService(fc, pc)

	cfg := doctorConfig()
	cfg.Auth.Mode = config.AuthModeKratos

	var buf bytes.Buffer
	opts := defaultOpts(t)
	opts.JSON = true

	if _, err := svc.Run(context.Background(), cfg, opts, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, r := range decodeDoctorResults(t, buf.Bytes()) {
		if r.Name == "Kratos DB credentials" {
			t.Error("expected no 'Kratos DB credentials' row when logs are unavailable")
		}
	}
}
