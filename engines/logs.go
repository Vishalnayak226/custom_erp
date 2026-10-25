package engines

import (
	"context"
	"crypto/sha256"
	"custom_erp/db"
	"encoding/hex"
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

var (
	secretAssignmentPattern = regexp.MustCompile(`(?i)\b(password|token|secret|api[_-]?key|authorization)\b(\s*[:=]\s*)(?:"[^"]*"|'[^']*'|[^\s,;]+)`)
	bearerSecretPattern     = regexp.MustCompile(`(?i)(\bbearer\s+)[A-Za-z0-9._~+/-]+=*`)
)

const (
	maxSystemErrorMessageBytes = 4 * 1024
	maxSystemErrorStackBytes   = 32 * 1024
)

func redactAndBoundLogField(value string, maxBytes int) string {
	value = bearerSecretPattern.ReplaceAllString(value, "${1}[REDACTED]")
	value = secretAssignmentPattern.ReplaceAllString(value, "$1$2[REDACTED]")
	if len(value) <= maxBytes {
		return value
	}
	// Keep the stored string valid UTF-8 when a multibyte character falls on
	// the byte boundary. Log fields are diagnostic excerpts, not payloads.
	const marker = "…[truncated]"
	limit := maxBytes - len(marker)
	if limit < 0 {
		limit = 0
	}
	value = value[:limit]
	for len(value) > 0 && !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value + marker
}

// auditChecksum (24.24) hashes one row's content together with the
// immediately preceding row's checksum, so altering or deleting any row
// breaks the chain for every row inserted after it - checkable
// independently of whether the DB's own audit-log triggers are still
// intact (the scenario this closes: a database superuser, not reachable by
// any app-level user or code path, disabling those triggers).
func auditChecksum(prevChecksum, userID, action, status, details string) string {
	h := sha256.Sum256([]byte(prevChecksum + "|" + userID + "|" + action + "|" + status + "|" + details))
	return hex.EncodeToString(h[:])
}

// maxAuditDetailBytes bounds one audit row's details (Stage 49.11.2). Generous
// - readers match on its prefix - but a whole request body or document can
// no longer land in the security log.
const maxAuditDetailBytes = 16 * 1024

// LogAuditEvent writes an audit log entry for the specified tenant
func LogAuditEvent(tenantID, userID, action, status, details string) {
	logAuditEvent(tenantID, userID, action, status, details, "")
}

// LogAuditEventCtx is LogAuditEvent plus the request's correlation id from
// ctx (Stage 49.11.2), so a security event links to the request, its system
// log rows and the jobs it queued (InvestigationTrail) instead of copying them.
func LogAuditEventCtx(ctx context.Context, tenantID, userID, action, status, details string) {
	logAuditEvent(tenantID, userID, action, status, details, CorrelationIDFromContext(ctx))
}

func logAuditEvent(tenantID, userID, action, status, details, correlationID string) {
	// Stage 49.11.2: never a secret or a whole payload in the evidence. The
	// same redaction system_error_logs gets, applied before signing so the
	// signature covers what is stored.
	details = redactAndBoundLogField(details, maxAuditDetailBytes)
	correlationID = SafeCorrelationID(correlationID)
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		noteAuditWriteFailure(tenantID, action, "cannot get tenant schema", err)
		return
	}

	tx, err := db.DB.Begin()
	if err != nil {
		noteAuditWriteFailure(tenantID, action, "cannot begin transaction", err)
		return
	}
	defer tx.Rollback()

	if err := db.SetSearchPath(tx, schema); err != nil {
		noteAuditWriteFailure(tenantID, action, "cannot set search path", err)
		return
	}

	// Stage 47.7.2: an INDEPENDENT signature, not a chain link.
	//
	// This used to read the previous row's checksum and hash into it - chain
	// semantics - while deliberately not locking, on the reasoning that "worst
	// case under real concurrency is two rows briefly chaining from the same
	// parent, not a broken tamper-evidence guarantee". The second half of that
	// was wrong: sibling rows are exactly what made the old verifier report a
	// break on perfectly clean data, which is the failure 47.7.2 names. Taking
	// the lock instead would have serialized every audit write per tenant, on a
	// table that takes a row on nearly every request.
	//
	// Signing each row over its own content removes the ordering dependency
	// entirely, so there is nothing to read first and nothing to serialize.
	// Deletion - the one thing per-row signatures cannot catch - is covered by
	// the periodic checkpoints in engines/audit_evidence.go.
	//
	// The legacy `checksum` column is still written so a database mid-upgrade
	// (new binary, migration not yet applied) keeps its old behaviour rather
	// than losing the column's value entirely.
	// Microsecond truncation: Postgres TIMESTAMP stores no finer, so signing
	// the nanosecond value would sign something the database never held.
	now := time.Now().UTC().Truncate(time.Microsecond)
	signature := SignAuditRow(tenantID, userID, action, status, details, "", "", correlationID, now)
	var prevChecksum string
	_ = tx.QueryRow(`SELECT checksum FROM audit_logs ORDER BY created_at DESC, id DESC LIMIT 1`).Scan(&prevChecksum)
	checksum := auditChecksum(prevChecksum, userID, action, status, details)

	query := `INSERT INTO audit_logs (user_id, action, status, details, checksum, created_at, signature, sig_version, correlation_id)
	          VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`
	_, err = tx.Exec(query, userID, action, status, details, checksum, now, signature, AuditSigVersion, nullIfEmpty(correlationID))
	if err != nil {
		noteAuditWriteFailure(tenantID, action, "cannot insert entry", err)
		return
	}

	if err := tx.Commit(); err != nil {
		noteAuditWriteFailure(tenantID, action, "cannot commit", err)
	}
}

// Stage 49.13.7: a lost audit event must never be silent. The business call
// that wanted it has already happened, so the write is not retried blindly;
// instead every loss is counted (Activity Log -> Security shows the count
// and the last reason) and pages through SendOpsAlert at most once per
// auditLossAlertEvery, so a database outage does not become an alert storm.
const auditLossAlertEvery = 10 * time.Minute

var auditWriteLoss struct {
	sync.Mutex
	count     int64
	lastErr   string
	lastAt    time.Time
	lastAlert time.Time
}

func noteAuditWriteFailure(tenantID, action, stage string, err error) {
	reason := redactAndBoundLogField(fmt.Sprintf("%s: %v", stage, err), 300)
	log.Printf("Audit logging failed (%s, %s): %s", tenantID, action, reason)
	now := time.Now()
	auditWriteLoss.Lock()
	auditWriteLoss.count++
	auditWriteLoss.lastErr, auditWriteLoss.lastAt = reason, now
	alert := now.Sub(auditWriteLoss.lastAlert) >= auditLossAlertEvery
	if alert {
		auditWriteLoss.lastAlert = now
	}
	auditWriteLoss.Unlock()
	if alert {
		SendOpsAlert("AUDIT_WRITE_FAILED", tenantID, "an audit event ("+action+") could not be written - security evidence is being lost: "+reason)
	}
}

// AuditWriteLoss reports how many audit events this server failed to write
// since it started, and the last reason.
func AuditWriteLoss() (count int64, lastErr string, lastAt time.Time) {
	auditWriteLoss.Lock()
	defer auditWriteLoss.Unlock()
	return auditWriteLoss.count, auditWriteLoss.lastErr, auditWriteLoss.lastAt
}

// AuditChainBreak describes the first row (in insertion order) whose stored
// checksum doesn't match what VerifyAuditLogChain recomputes from its
// content and the previous row's checksum.
type AuditChainBreak struct {
	ID        string `json:"id"`
	CreatedAt string `json:"created_at"`
}

// VerifyAuditLogChain (24.24) walks a tenant's audit_logs in insertion
// order and recomputes each row's checksum, reporting the first row whose
// stored checksum doesn't match - meant to be run periodically/on-demand by
// an operator (GET /api/v1/admin/audit-logs/verify), not on a fixed
// schedule this codebase would have to own.
func VerifyAuditLogChain(tenantID string) (intact bool, brokenAt *AuditChainBreak, err error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return false, nil, err
	}
	// Stage 30.2.3: every text column is COALESCE'd. Found live - this
	// endpoint 500'd on a row with a NULL checksum (only `checksum` has a
	// pre-migration NULL case today, but user_id/details are equally nullable
	// in the schema, and the whole point of a tamper-evidence check is that it
	// still runs when the data is unusual). An empty string is also exactly
	// how the loop below already treats a pre-checksum row, so nothing about
	// the verification semantics changes.
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, COALESCE(user_id, ''), COALESCE(action, ''), COALESCE(status, ''),
		       COALESCE(details, ''), COALESCE(checksum, ''), created_at
		FROM %s.audit_logs ORDER BY created_at ASC, id ASC`, schema))
	if err != nil {
		return false, nil, err
	}
	defer rows.Close()

	prevChecksum := ""
	for rows.Next() {
		var id, userID, action, status, details, storedChecksum, createdAt string
		if err := rows.Scan(&id, &userID, &action, &status, &details, &storedChecksum, &createdAt); err != nil {
			return false, nil, err
		}
		expected := auditChecksum(prevChecksum, userID, action, status, details)
		if storedChecksum != "" && storedChecksum != expected {
			return false, &AuditChainBreak{ID: id, CreatedAt: createdAt}, nil
		}
		// A row written before this migration has an empty stored checksum -
		// skip verifying it, and chain the next row off that same empty
		// value, matching exactly what the real LogAuditEvent call read as
		// "the previous row's checksum" when that next row was inserted.
		prevChecksum = storedChecksum
	}
	return true, nil, nil
}

// ClientErrorCodes (Stage 104.2) lists the error-catalog codes that answer a
// request the user can correct - HTTP 2xx/4xx: a missing field, an expired
// session, a refused self-approval. The API error writers log every catalog
// code marked "log required" as "[CODE] message", so most rows in
// system_error_logs are those rejections, not failures of this server.
// internal/server sets this from its catalog (engines cannot import it).
var ClientErrorCodes func() []string

// systemFaultSQL is a WHERE condition on system_error_logs keeping only
// faults - background-worker errors, panics, 5xx answers - for the two counts
// that mean "something is wrong": the Control Panel's Errors (24h) and the
// sustained-error-rate alert. The rejections stay in the log for the Activity
// Log and a correlation-id lookup. arg is the placeholder bound to
// clientErrorCodesArg().
func systemFaultSQL(arg string) string {
	return `COALESCE(substring(error_message from '^\[([A-Z]+-[0-9]+)\]'), '') <> ALL(` + arg + `::text[])`
}

// clientErrorCodesArg is ClientErrorCodes as a Postgres array literal - "{}"
// (count every row, as before) when it is unset, e.g. in engine-only tests.
func clientErrorCodesArg() string {
	if ClientErrorCodes == nil {
		return "{}"
	}
	return "{" + strings.Join(ClientErrorCodes(), ",") + "}"
}

// LogSystemError writes a system exception/panic trace for the specified tenant
func LogSystemError(tenantID string, correlationID string, severity, moduleSource, message, stackTrace string) {
	severity = redactAndBoundLogField(severity, 32)
	moduleSource = redactAndBoundLogField(moduleSource, 200)
	message = redactAndBoundLogField(message, maxSystemErrorMessageBytes)
	stackTrace = redactAndBoundLogField(stackTrace, maxSystemErrorStackBytes)
	correlationID = redactAndBoundLogField(correlationID, 100)
	log.Printf("[%s] System Error in module %s: %s", severity, moduleSource, message)

	// Panics alert immediately, ahead of/independent from the DB insert below
	// (a panic during a DB outage is exactly when the alert matters most).
	// Non-panic failures are covered by the sustained-error-rate monitor
	// (engines/alerting.go's StartAlertMonitor) instead of alerting on each
	// individual occurrence.
	if severity == "PANIC" {
		SendOpsAlert(severity, moduleSource, message)
	}

	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		log.Printf("Error logging failed: cannot get tenant schema: %v", err)
		return
	}

	tx, err := db.DB.Begin()
	if err != nil {
		log.Printf("Error logging failed: cannot begin transaction: %v", err)
		return
	}
	defer tx.Rollback()

	if err := db.SetSearchPath(tx, schema); err != nil {
		log.Printf("Error logging failed: cannot set search path: %v", err)
		return
	}

	query := `INSERT INTO system_error_logs (correlation_id, severity, module_source, error_message, stack_trace)
	          VALUES (CASE WHEN $1 = '' THEN NULL ELSE $1::uuid END, $2, $3, $4, $5)`
	_, err = tx.Exec(query, correlationID, severity, moduleSource, message, stackTrace)
	if err != nil {
		log.Printf("Error logging failed: cannot insert error entry: %v", err)
		return
	}

	tx.Commit()
}

// Stage 64.2: a background worker's failure for one client used to reach only
// the server log, so a data problem it hit (on production: tenant_minn's
// missing pinelabs_transactions table, 286 times a day) never showed in that
// client's system_error_logs, where the Data Console looks. Each distinct
// failure is now recorded there too - at most once an hour, so a worker that
// retries every few minutes does not flood the table.
const backgroundErrorRecordEvery = time.Hour

var (
	backgroundErrorMu   sync.Mutex
	backgroundErrorLast = map[string]time.Time{}
)

// LogBackgroundJobError logs "[MODULE] <what> failed for <schema>: <err>" to
// the server log every time, and to that client's system_error_logs once an
// hour per distinct failure.
func LogBackgroundJobError(module, what, schema string, err error) {
	log.Printf("[%s] %s failed for %s: %v", module, what, schema, err)
	key := module + "|" + schema + "|" + err.Error()
	now := time.Now()
	backgroundErrorMu.Lock()
	if last, ok := backgroundErrorLast[key]; ok && now.Sub(last) < backgroundErrorRecordEvery {
		backgroundErrorMu.Unlock()
		return
	}
	backgroundErrorLast[key] = now
	if len(backgroundErrorLast) > 1000 {
		for k, t := range backgroundErrorLast {
			if now.Sub(t) >= backgroundErrorRecordEvery {
				delete(backgroundErrorLast, k)
			}
		}
	}
	backgroundErrorMu.Unlock()

	tenantID, idErr := tenantIDForSchema(schema)
	if idErr != nil || tenantID == "" {
		return
	}
	LogSystemError(tenantID, "", "ERROR", module, fmt.Sprintf("%s failed: %v", what, err), "")
}
