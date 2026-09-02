package ops

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/vibewarden/vibewarden/internal/config"
)

// kratosMigrateServiceName is the Compose service name of the one-shot Kratos
// migration container in the generated docker-compose.yml.
const kratosMigrateServiceName = "kratos-migrate"

// kratosMigrateLogTailLines is how many log lines are fetched from
// kratos-migrate when diagnosing a failed startup.
const kratosMigrateLogTailLines = 50

// kratosRecoveryCommand is the single recovery command referenced by both the
// dev error block and the doctor detail. Shared so the two cannot drift.
const kratosRecoveryCommand = "vibew down -v --yes"

// kratosDataLossWarning is the shared plain-language warning about what the
// recovery command destroys. Shared so the two renderings cannot drift.
const kratosDataLossWarning = "destroys local auth data: users, sessions"

// kratosMigrateDiagTimeout bounds the log fetch used by the doctor check so a
// hung Docker daemon cannot stall the report.
const kratosMigrateDiagTimeout = 5 * time.Second

// kratosDBAuthFailureSignatures are the lower-cased substrings that identify a
// Postgres authentication failure in kratos-migrate's output. Kratos wraps the
// driver error and the exact prefix/casing varies between driver paths, so the
// match is case-insensitive and covers the SQLSTATE form as well.
var kratosDBAuthFailureSignatures = []string{
	"password authentication failed for user",
	"sqlstate 28p01",
}

// localKratosMigrateService reports whether the generated compose stack
// contains a vibew-managed kratos-migrate service, i.e. whether this project
// owns the kratos-db volume whose credentials can go stale.
//
// The condition mirrors the compose template guard at
// internal/config/templates/docker-compose.yml.tmpl (the
// `auth.mode == kratos && !kratos.external` block plus its nested
// `database.external_url == ""` condition). If the two drift, this diagnostic
// silently stops firing — keep them in sync.
func localKratosMigrateService(cfg *config.Config) bool {
	if cfg == nil {
		return false
	}
	return cfg.Auth.Mode == config.AuthModeKratos &&
		!cfg.Kratos.External &&
		cfg.Database.ExternalURL == ""
}

// hasKratosDBCredentialMismatch reports whether the supplied kratos-migrate
// logs carry the Postgres authentication-failure signature that indicates the
// kratos-db volume was initialised with a different password than the current
// .credentials/.env. The match is case-insensitive.
func hasKratosDBCredentialMismatch(logs string) bool {
	if logs == "" {
		return false
	}
	lower := strings.ToLower(logs)
	for _, sig := range kratosDBAuthFailureSignatures {
		if strings.Contains(lower, sig) {
			return true
		}
	}
	return false
}

// kratosCredentialMismatchBlock renders the multi-line, user-facing message
// shown by `vibew dev` when the credential mismatch is detected. The wording is
// pinned by golden tests in kratos_migrate_diag_test.go.
func kratosCredentialMismatchBlock() string {
	return "Kratos database migration failed -- credential mismatch.\n" +
		"\n" +
		"  The kratos-db volume was created by an earlier run and still holds that run's\n" +
		"  Postgres password. vibew regenerates .credentials/.env on every `vibew dev`, so\n" +
		"  the new password no longer matches the data already in the volume.\n" +
		"\n" +
		"Recovery (this " + kratosDataLossWarning + "):\n" +
		"\n" +
		"  " + kratosRecoveryCommand + " && vibew dev\n" +
		"\n" +
		"  or: vibew dev --rebuild --volumes\n" +
		"\n" +
		"See the raw failure with: vibew logs " + kratosMigrateServiceName
}

// kratosCredentialMismatchDetail renders the single-line doctor detail for the
// same condition. It must stay on one line — printDoctorReport lays results out
// with a fixed-width column format.
func kratosCredentialMismatchDetail() string {
	return "kratos-db volume holds credentials from an earlier run -- recover with '" +
		kratosRecoveryCommand + "' then 'vibew dev' (" + kratosDataLossWarning + ")"
}

// kratosCredentialMismatchError returns the actionable credential-mismatch
// error when kratos-migrate's logs carry the known signature, and nil in every
// other case: when the project has no vibew-managed kratos-migrate service,
// when the logs cannot be fetched, or when no signature matches. Callers fall
// back to their existing generic error when nil is returned.
func (s *DevService) kratosCredentialMismatchError(ctx context.Context, cfg *config.Config, composeFile string) error {
	if !localKratosMigrateService(cfg) {
		return nil
	}

	logs, err := s.compose.Logs(ctx, composeFile, kratosMigrateServiceName, kratosMigrateLogTailLines)
	if err != nil {
		slog.Debug("could not fetch kratos-migrate logs", "error", err)
		return nil
	}
	if !hasKratosDBCredentialMismatch(logs) {
		return nil
	}
	return fmt.Errorf("%s", kratosCredentialMismatchBlock()) //nolint:err113 // static user-facing message block
}

// checkKratosDBCredentials inspects kratos-migrate's logs for the stale-volume
// credential mismatch. The second return value is false when no row should be
// emitted at all: the project has no vibew-managed kratos-migrate service, or
// its logs are unavailable (stack never created, service absent).
//
// It deliberately does not depend on ComposeRunner.PS: `docker compose ps`
// without --all hides both the exited kratos-migrate container and the created
// sidecar, so a stuck stack looks empty. Logs work on exited containers.
func (s *DoctorService) checkKratosDBCredentials(ctx context.Context, cfg *config.Config, composeFile string) (CheckResult, bool) {
	if !localKratosMigrateService(cfg) {
		return CheckResult{}, false
	}

	logsCtx, cancel := context.WithTimeout(ctx, kratosMigrateDiagTimeout)
	defer cancel()

	logs, err := s.compose.Logs(logsCtx, composeFile, kratosMigrateServiceName, kratosMigrateLogTailLines)
	if err != nil {
		slog.Debug("could not fetch kratos-migrate logs", "error", err)
		return CheckResult{}, false
	}

	if hasKratosDBCredentialMismatch(logs) {
		return CheckResult{
			Name:     "Kratos DB credentials",
			Severity: SeverityFail,
			Detail:   kratosCredentialMismatchDetail(),
		}, true
	}
	return CheckResult{
		Name:     "Kratos DB credentials",
		Severity: SeverityOK,
		Detail:   "no credential mismatch detected",
	}, true
}
