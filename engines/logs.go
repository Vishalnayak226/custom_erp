package engines

import (
	"crypto/sha256"
	"custom_erp/db"
	"encoding/hex"
	"fmt"
	"log"
	"time"
)

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

// LogAuditEvent writes an audit log entry for the specified tenant
func LogAuditEvent(tenantID, userID, action, status, details string) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		log.Printf("Audit logging failed: cannot get tenant schema: %v", err)
		return
	}

	tx, err := db.DB.Begin()
	if err != nil {
		log.Printf("Audit logging failed: cannot begin transaction: %v", err)
		return
	}
	defer tx.Rollback()

	if err := db.SetSearchPath(tx, schema); err != nil {
		log.Printf("Audit logging failed: cannot set search path: %v", err)
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
	signature := SignAuditRow(tenantID, userID, action, status, details, "", "", "", now)
	var prevChecksum string
	_ = tx.QueryRow(`SELECT checksum FROM audit_logs ORDER BY created_at DESC, id DESC LIMIT 1`).Scan(&prevChecksum)
	checksum := auditChecksum(prevChecksum, userID, action, status, details)

	query := `INSERT INTO audit_logs (user_id, action, status, details, checksum, created_at, signature, sig_version)
	          VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`
	_, err = tx.Exec(query, userID, action, status, details, checksum, now, signature, AuditSigVersion)
	if err != nil {
		log.Printf("Audit logging failed: cannot insert entry: %v", err)
		return
	}

	tx.Commit()
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

// LogSystemError writes a system exception/panic trace for the specified tenant
func LogSystemError(tenantID string, correlationID string, severity, moduleSource, message, stackTrace string) {
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
