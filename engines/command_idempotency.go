package engines

import (
	"crypto/sha256"
	"custom_erp/db"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Stage 47.3.1 - "Insert/lock one tenant-scoped command-idempotency record
// before any mutation; a repeated key returns the original outcome or safely
// resumes a defined durable state, never repeats a mutation." (audit A-03)
//
// What this replaces: handleCheckout's cart-number claim, which guarded the
// POSCart ROW. That stopped one cart number being processed twice, but it
// guarded the wrong thing - a retry that generated a new cart number (which is
// exactly what public/app.js used to do on every attempt) sailed straight past
// it and decremented stock a second time. The audit's phrasing for this is
// precise: "Idempotency protects availability itself, not only the ledger row."
//
// The claim is a real row, not an in-memory lock, so it survives a process
// kill; and CompleteCommand runs inside the SAME transaction as the business
// mutation, so a committed sale always has a committed Completed record and
// vice versa - there is no window in which one exists without the other.
//
// Deliberately not built on the generic doctype engine: this is infrastructure
// with its own status taxonomy and lease semantics, the same reasoning Stage
// 38.6 recorded for async_jobs and Stage 30.2.2 for integration_event_outbox.

// Claim outcomes.
type ClaimOutcome string

const (
	// ClaimAcquired - this caller owns the command and must run it.
	ClaimAcquired ClaimOutcome = "acquired"
	// ClaimReplay - the command already completed; Response holds exactly what
	// the original call returned and must be returned verbatim.
	ClaimReplay ClaimOutcome = "replay"
	// ClaimInProgress - another request holds an unexpired claim on this key.
	// The correct answer is "wait and retry", never "do it again".
	ClaimInProgress ClaimOutcome = "in_progress"
	// ClaimPayloadMismatch - the same key arrived with a different request
	// body. That is not a retry of anything; returning the first call's
	// outcome would answer a question nobody asked.
	ClaimPayloadMismatch ClaimOutcome = "payload_mismatch"
	// ClaimFailedEarlier - a previous attempt on this key ended Failed. The
	// caller may retry: the claim is re-acquired rather than replayed, because
	// a Failed command by definition committed no mutation.
	ClaimFailedEarlier ClaimOutcome = "retry_after_failure"
)

// commandLease is how long a claim is honoured before another request may take
// it over. Long enough that no honest checkout is stolen mid-flight, short
// enough that a killed process does not strand a till for a shift.
const commandLease = 2 * time.Minute

// CommandClaim is what ClaimCommand resolved.
type CommandClaim struct {
	Outcome  ClaimOutcome
	Key      string
	Response map[string]interface{}
	// DocumentID is the business document the original attempt produced, when
	// it recorded one - a replayed checkout names the cart it completed.
	DocumentID string
}

// CommandDigest is the canonical hash of a request body, used to tell a
// genuine retry (identical payload) from a key collision (different payload).
// Marshalling through a map sorts keys, so field order in the wire JSON does
// not change the digest - a retry from a different client build still matches.
func CommandDigest(payload interface{}) string {
	raw, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	var normalized interface{}
	if err := json.Unmarshal(raw, &normalized); err == nil {
		if canonical, err := json.Marshal(normalized); err == nil {
			raw = canonical
		}
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// ClaimCommand takes (or refuses) ownership of one command execution.
//
// The whole decision happens in one statement's worth of atomicity: the INSERT
// is the lock. Two concurrent requests with the same key cannot both get
// ClaimAcquired, because the second one's INSERT conflicts on the primary key
// and falls through to the inspection below - which is a SELECT ... FOR UPDATE
// on the row the first one just wrote.
func ClaimCommand(tenantID, command, key, requestDigest, correlationID string) (*CommandClaim, error) {
	if strings.TrimSpace(key) == "" {
		return nil, errors.New("an idempotency key is required")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	namespaced := command + ":" + key

	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var inserted string
	err = tx.QueryRow(fmt.Sprintf(`
		INSERT INTO %s.command_idempotency (idempotency_key, command, request_digest, status, correlation_id, lease_expires_at)
		VALUES ($1, $2, $3, 'InProgress', $4, CURRENT_TIMESTAMP + $5::interval)
		ON CONFLICT (idempotency_key) DO NOTHING
		RETURNING idempotency_key`, schema),
		namespaced, command, requestDigest, correlationID, fmt.Sprintf("%d seconds", int(commandLease.Seconds()))).Scan(&inserted)
	if err == nil {
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return &CommandClaim{Outcome: ClaimAcquired, Key: namespaced}, nil
	}
	if err != sql.ErrNoRows {
		return nil, err
	}

	// The key exists. Lock it so two duplicates cannot both decide to take
	// over an expired lease.
	var status, storedDigest string
	var responseRaw, documentID sql.NullString
	var leaseExpired bool
	err = tx.QueryRow(fmt.Sprintf(`
		SELECT status, request_digest, response::text, document_id, lease_expires_at < CURRENT_TIMESTAMP
		FROM %s.command_idempotency WHERE idempotency_key = $1 FOR UPDATE`, schema),
		namespaced).Scan(&status, &storedDigest, &responseRaw, &documentID, &leaseExpired)
	if err != nil {
		return nil, err
	}

	claim := &CommandClaim{Key: namespaced, DocumentID: documentID.String}

	// A different payload under the same key is never a retry.
	if storedDigest != requestDigest {
		claim.Outcome = ClaimPayloadMismatch
		return claim, tx.Commit()
	}

	switch status {
	case "Completed":
		claim.Outcome = ClaimReplay
		if responseRaw.Valid && responseRaw.String != "" {
			_ = json.Unmarshal([]byte(responseRaw.String), &claim.Response)
		}
		return claim, tx.Commit()
	case "Failed":
		// A Failed command committed nothing, so retrying it is safe and is
		// what the operator expects when they press the button again.
		if _, err := tx.Exec(fmt.Sprintf(`
			UPDATE %s.command_idempotency
			SET status = 'InProgress', claimed_at = CURRENT_TIMESTAMP, completed_at = NULL,
			    correlation_id = $2, lease_expires_at = CURRENT_TIMESTAMP + $3::interval
			WHERE idempotency_key = $1`, schema),
			namespaced, correlationID, fmt.Sprintf("%d seconds", int(commandLease.Seconds()))); err != nil {
			return nil, err
		}
		claim.Outcome = ClaimFailedEarlier
		return claim, tx.Commit()
	default: // InProgress
		if !leaseExpired {
			claim.Outcome = ClaimInProgress
			return claim, tx.Commit()
		}
		// The lease lapsed: whoever held it died without completing or
		// failing. Taking it over is safe precisely because the business
		// mutation and the Completed record commit together - an abandoned
		// InProgress row therefore proves nothing was committed.
		if _, err := tx.Exec(fmt.Sprintf(`
			UPDATE %s.command_idempotency
			SET claimed_at = CURRENT_TIMESTAMP, correlation_id = $2,
			    lease_expires_at = CURRENT_TIMESTAMP + $3::interval
			WHERE idempotency_key = $1`, schema),
			namespaced, correlationID, fmt.Sprintf("%d seconds", int(commandLease.Seconds()))); err != nil {
			return nil, err
		}
		claim.Outcome = ClaimAcquired
		return claim, tx.Commit()
	}
}

// CompleteCommandTx records the command's outcome INSIDE the caller's own
// transaction - the whole point of the mechanism. If the sale commits, so does
// this; if the sale rolls back, so does this, and the key is free to retry.
func CompleteCommandTx(tx *sql.Tx, schema, key, documentID string, response map[string]interface{}) error {
	payload, err := json.Marshal(response)
	if err != nil {
		return err
	}
	_, err = tx.Exec(fmt.Sprintf(`
		UPDATE %s.command_idempotency
		SET status = 'Completed', response = $2::jsonb, document_id = $3, completed_at = CURRENT_TIMESTAMP
		WHERE idempotency_key = $1`, schema), key, string(payload), documentID)
	return err
}

// FailCommand marks a claim Failed so the operator can retry it. Called on the
// error path, OUTSIDE any transaction - by then the business transaction has
// rolled back, so this must be its own committed statement or the failure
// would vanish with it and the key would sit InProgress until its lease ran
// out.
func FailCommand(tenantID, key, reason string) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return
	}
	if _, err := db.DB.Exec(fmt.Sprintf(`
		UPDATE %s.command_idempotency
		SET status = 'Failed', completed_at = CURRENT_TIMESTAMP,
		    response = jsonb_build_object('error', $2::text)
		WHERE idempotency_key = $1 AND status = 'InProgress'`, schema), key, reason); err != nil {
		LogSystemError(tenantID, "", "ERROR", "FailCommand",
			fmt.Sprintf("could not mark command %s failed: %v", key, err), "")
	}
}

// commandClaimRetention is how long a settled claim is remembered.
//
// It has to outlive every retry a real client could still make - a till that
// lost its connection mid-shift, an offline queue that syncs the next morning -
// and it must NOT be indefinite, or the table grows without bound and a cart
// number reused months later collides with an ancient outcome. A week covers
// the longest offline window this product supports (one shift, per Stage
// 20.13) with a large margin.
const commandClaimRetentionDays = 7

// purgeSettledCommandClaims deletes Completed/Failed claims past retention.
// Called from the reservation sweeper's tick (engines/reservation_sweeper.go).
// InProgress rows are never purged here regardless of age - one of those is
// either live or evidence of a crashed request, and ClaimCommand's lease
// takeover is what deals with it.
func purgeSettledCommandClaims(schema string) {
	var exists bool
	if err := db.DB.QueryRow(`SELECT to_regclass($1) IS NOT NULL`, schema+".command_idempotency").Scan(&exists); err != nil || !exists {
		return
	}
	if _, err := db.DB.Exec(fmt.Sprintf(`
		DELETE FROM %s.command_idempotency
		WHERE status IN ('Completed', 'Failed')
		  AND completed_at < CURRENT_TIMESTAMP - $1::interval`, schema),
		fmt.Sprintf("%d days", commandClaimRetentionDays)); err != nil {
		LogSystemError(schemaToTenantID(schema), "", "WARN", "purgeSettledCommandClaims",
			fmt.Sprintf("%s: %v", schema, err), "")
	}
}

// ReleaseCommand deletes a claim outright. Used only where the command was
// refused before doing anything at all (a validation rejection after the
// claim), so the key is immediately reusable rather than being remembered as a
// failure the operator has to think about.
func ReleaseCommand(tenantID, key string) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return
	}
	_, _ = db.DB.Exec(fmt.Sprintf(
		`DELETE FROM %s.command_idempotency WHERE idempotency_key = $1 AND status = 'InProgress'`, schema), key)
}
