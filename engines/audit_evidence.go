package engines

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"custom_erp/db"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log"
	"strings"
	"time"
)

// Stage 47.7 - audit evidence: independently signed events + checkpoints
// (audit findings A-07/A-30).
//
// WHY THIS MODEL, decided by the user on 2026-09-09 from the two 47.7.2
// offered. What existed was the worst of both worlds: engines/logs.go computed
// each row's checksum from the PREVIOUS row's checksum - chain semantics - but
// deliberately did not lock, and said so:
//
//	"Worst case under real concurrency is two rows briefly chaining from the
//	 same parent, not a broken tamper-evidence guarantee for either of them"
//
// The second half of that sentence is wrong, and 47.7.2 names why: two writers
// producing siblings is exactly what makes VerifyAuditLogChain report a break
// on clean data. A verifier that cries wolf under normal load is worse than no
// verifier, because it trains an operator to ignore it.
//
// The alternative - take the lock - would serialize every audit write per
// tenant. Audit logging runs on nearly every request, and Stage 47.3 had just
// made checkout deliberately concurrent (deterministic lock ordering so two
// tills can sell at once). A per-tenant audit chain would put a global
// serialization point back on every sale.
//
// So: each row is signed over its OWN content, with no dependency on any other
// row. Concurrency stops being a correctness question. What a per-row signature
// cannot catch on its own is DELETION - remove a row and every survivor still
// verifies - and that is what checkpoints are for: a periodic digest over a
// definite seq range, so a missing row changes both the count and the digest.
// The same shape as AWS QLDB digests and Certificate Transparency's signed tree
// heads.

// AuditSigVersion names the current signing scheme. It is stored on every row
// so a key rotation or a format change becomes a new version rather than a
// silent reinterpretation of old evidence.
const AuditSigVersion = "v1-hmac-sha256"

// SignAuditRow produces a row's independent signature.
//
// Canonical field order is fixed and every field is length-prefixed, so no
// combination of contents can be re-arranged into a different row with the
// same signature - "a|bc" and "ab|c" must not collide, which plain
// concatenation would allow.
func SignAuditRow(tenantID, userID, action, status, details, entityType, entityID, correlationID string, createdAt time.Time) string {
	var b strings.Builder
	for _, f := range []string{
		AuditSigVersion, tenantID, userID, action, status, details,
		entityType, entityID, correlationID,
		createdAt.UTC().Format(time.RFC3339Nano),
	} {
		fmt.Fprintf(&b, "%d:%s|", len(f), f)
	}
	mac := hmac.New(sha256.New, jwtSigningKey.secret)
	mac.Write([]byte(b.String()))
	return hex.EncodeToString(mac.Sum(nil))
}

// AuditEvidence is one protected business mutation, in the shape 47.7.1
// requires: who, what command, which entity, and the correlation that ties it
// back to the request and to Stage 47.3's command-idempotency record.
type AuditEvidence struct {
	UserID        string
	Action        string
	Status        string
	Details       string
	EntityType    string
	EntityID      string
	CorrelationID string
}

// WriteAuditEvidenceTx writes signed evidence INSIDE the caller's transaction
// (47.7.3), so evidence and the business mutation commit together or not at
// all.
//
// This is the half that could not be done under the chain model: writing
// inside the business transaction is exactly when a chain read of "the last
// row" would deadlock or serialize against other in-flight sales. With
// independent signatures there is nothing to read first, so the write is a
// plain INSERT that costs the enclosing transaction almost nothing.
func WriteAuditEvidenceTx(tx *sql.Tx, schema, tenantID string, ev AuditEvidence) error {
	// Truncated to microseconds because that is the resolution Postgres
	// TIMESTAMP actually stores. Signing the nanosecond value and storing the
	// microsecond one made every freshly written row fail its own signature -
	// found by the 47.7.8 tamper test reporting a row as tampered before
	// anything had touched it.
	now := time.Now().UTC().Truncate(time.Microsecond)
	sig := SignAuditRow(tenantID, ev.UserID, ev.Action, ev.Status, ev.Details,
		ev.EntityType, ev.EntityID, ev.CorrelationID, now)
	_, err := tx.Exec(fmt.Sprintf(`
		INSERT INTO %s.audit_logs
			(user_id, action, status, details, entity_type, entity_id, correlation_id,
			 created_at, signature, sig_version)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`, schema),
		ev.UserID, ev.Action, ev.Status, ev.Details, nullIfEmpty(ev.EntityType),
		nullIfEmpty(ev.EntityID), nullIfEmpty(ev.CorrelationID), now, sig, AuditSigVersion)
	return err
}

// --- verification ----------------------------------------------------------

// AuditVerification is the honest answer to "does this tenant's audit evidence
// hold up", including what it could NOT check.
type AuditVerification struct {
	TotalRows int `json:"total_rows"`
	// Signed/Unsigned is the coverage boundary 47.7.4 requires stated rather
	// than papered over: the legacy rows are reported as out of coverage, not
	// as passing.
	SignedRows        int      `json:"signed_rows"`
	UnsignedLegacy    int      `json:"unsigned_legacy_rows"`
	CoverageFromSeq   int64    `json:"coverage_from_seq"`
	Verified          int      `json:"verified_rows"`
	TamperedRows      []string `json:"tampered_row_ids,omitempty"`
	CheckpointsOK     int      `json:"checkpoints_verified"`
	CheckpointsBad    []string `json:"checkpoints_failed,omitempty"`
	Intact            bool     `json:"intact"`
	CoverageStatement string   `json:"coverage_statement"`
}

// VerifyAuditEvidence re-signs every signed row and compares, then verifies
// every checkpoint's digest and signature.
//
// Unlike the chain verifier this replaces, a concurrent write during
// verification cannot produce a false break: each row stands alone, so the
// only way a row fails is if its own content changed.
func VerifyAuditEvidence(tenantID string) (*AuditVerification, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	out := &AuditVerification{Intact: true}

	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, COALESCE(user_id,''), COALESCE(action,''), COALESCE(status,''),
		       COALESCE(details,''), COALESCE(entity_type,''), COALESCE(entity_id,''),
		       COALESCE(correlation_id,''), created_at, COALESCE(signature,''), COALESCE(sig_version,'')
		  FROM %s.audit_logs ORDER BY seq ASC`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var id, userID, action, status, details, entType, entID, corr, sig, sigVer string
		var createdAt time.Time
		if err := rows.Scan(&id, &userID, &action, &status, &details, &entType, &entID, &corr, &createdAt, &sig, &sigVer); err != nil {
			return nil, err
		}
		out.TotalRows++
		if sig == "" {
			// Legacy: written before this stage existed. Counted, never
			// claimed as verified - see the coverage statement below.
			out.UnsignedLegacy++
			continue
		}
		out.SignedRows++
		if sigVer != AuditSigVersion {
			// A row signed under a retired scheme is out of THIS verifier's
			// coverage, not evidence of tampering. Reporting it as tampering
			// is how a key rotation gets mistaken for an attack.
			continue
		}
		want := SignAuditRow(tenantID, userID, action, status, details, entType, entID, corr, createdAt)
		if hmac.Equal([]byte(want), []byte(sig)) {
			out.Verified++
		} else {
			out.Intact = false
			if len(out.TamperedRows) < 50 {
				out.TamperedRows = append(out.TamperedRows, id)
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if err := verifyCheckpoints(tenantID, schema, out); err != nil {
		return nil, err
	}

	out.CoverageStatement = fmt.Sprintf(
		"%d of %d rows carry independent signatures and are covered by this verification. "+
			"%d rows predate Stage 47.7 and carry no signature; they are sealed by a boundary checkpoint "+
			"(their content is fixed at the seal, but their authorship cannot be proven retrospectively) "+
			"and are deliberately NOT counted as verified.",
		out.SignedRows, out.TotalRows, out.UnsignedLegacy)
	return out, nil
}

func verifyCheckpoints(tenantID, schema string, out *AuditVerification) error {
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, from_seq, to_seq, row_count, unsigned_count, row_digest,
		       COALESCE(prev_checkpoint_signature,''), signature, sig_version
		  FROM %s.audit_checkpoints ORDER BY to_seq ASC`, schema))
	if err != nil {
		return err
	}
	defer rows.Close()

	type cp struct {
		id                           string
		fromSeq, toSeq               int64
		rowCount, unsignedCount      int
		digest, prevSig, sig, sigVer string
	}
	var checkpoints []cp
	for rows.Next() {
		var c cp
		if err := rows.Scan(&c.id, &c.fromSeq, &c.toSeq, &c.rowCount, &c.unsignedCount,
			&c.digest, &c.prevSig, &c.sig, &c.sigVer); err != nil {
			return err
		}
		checkpoints = append(checkpoints, c)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, c := range checkpoints {
		if c.sigVer != AuditSigVersion {
			continue
		}
		// Does the checkpoint's own signature hold?
		want := signCheckpoint(tenantID, c.fromSeq, c.toSeq, c.rowCount, c.unsignedCount, c.digest, c.prevSig)
		if !hmac.Equal([]byte(want), []byte(c.sig)) {
			out.Intact = false
			out.CheckpointsBad = append(out.CheckpointsBad, c.id+" (signature)")
			continue
		}
		// ...and does the window still contain what it said it did? This is
		// the deletion check a per-row signature cannot make on its own.
		digest, count, unsigned, err := digestRange(schema, c.fromSeq, c.toSeq)
		if err != nil {
			return err
		}
		if digest != c.digest || count != c.rowCount || unsigned != c.unsignedCount {
			out.Intact = false
			out.CheckpointsBad = append(out.CheckpointsBad,
				fmt.Sprintf("%s (window changed: %d rows now, %d at checkpoint)", c.id, count, c.rowCount))
			continue
		}
		out.CheckpointsOK++
	}
	return nil
}

// digestRange hashes the ordered signatures of every row in a seq window.
//
// Ordered by seq, which is assigned by the database, so the digest is
// deterministic no matter what order concurrent writers actually committed in.
// Unsigned legacy rows contribute their id instead, so they are still covered
// for DELETION even though their authorship cannot be proven.
func digestRange(schema string, fromSeq, toSeq int64) (digest string, rowCount, unsignedCount int, err error) {
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT COALESCE(signature, ''), id::text FROM %s.audit_logs
		 WHERE seq >= $1 AND seq <= $2 ORDER BY seq ASC`, schema), fromSeq, toSeq)
	if err != nil {
		return "", 0, 0, err
	}
	defer rows.Close()

	var pairs [][2]string
	for rows.Next() {
		var sig, id string
		if err := rows.Scan(&sig, &id); err != nil {
			return "", 0, 0, err
		}
		pairs = append(pairs, [2]string{sig, id})
	}
	if err := rows.Err(); err != nil {
		return "", 0, 0, err
	}
	return digestSigIDPairs(pairs), len(pairs), countUnsigned(pairs), nil
}

// digestSigIDPairs hashes the ordered (signature, id) pairs of a seq window.
// Extracted (47.7.6) so this exact algorithm - length-prefixed, "unsigned:"+id
// for an unsigned row, in seq order - runs identically whether the pairs came
// from a live query over audit_logs (digestRange, above) or from a decrypted
// archive's rows (engines/audit_archive.go's verifyArchiveFile). Any drift
// between two copies of this logic would make an archived window fail
// verification against a checkpoint the live table would have passed, purely
// from an algorithmic mismatch rather than a real integrity problem - so this
// is the one place the hash is computed.
func digestSigIDPairs(pairs [][2]string) string {
	h := sha256.New()
	for _, p := range pairs {
		sig, id := p[0], p[1]
		if sig == "" {
			sig = "unsigned:" + id
		}
		fmt.Fprintf(h, "%d:%s|", len(sig), sig)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func countUnsigned(pairs [][2]string) int {
	n := 0
	for _, p := range pairs {
		if p[0] == "" {
			n++
		}
	}
	return n
}

func signCheckpoint(tenantID string, fromSeq, toSeq int64, rowCount, unsignedCount int, digest, prevSig string) string {
	mac := hmac.New(sha256.New, jwtSigningKey.secret)
	fmt.Fprintf(mac, "%s|%s|%d|%d|%d|%d|%s|%s",
		AuditSigVersion, tenantID, fromSeq, toSeq, rowCount, unsignedCount, digest, prevSig)
	return hex.EncodeToString(mac.Sum(nil))
}

// --- checkpointing ---------------------------------------------------------

// WriteAuditCheckpoint seals every row written since the last checkpoint.
//
// Returns nil (no error, no checkpoint) when there is nothing new, so the
// scheduled job is a cheap no-op on a quiet tenant rather than accumulating
// empty checkpoints.
func WriteAuditCheckpoint(tenantID, kind string) (*AuditCheckpoint, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}

	var lastTo sql.NullInt64
	var prevSig sql.NullString
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT to_seq, signature FROM %s.audit_checkpoints ORDER BY to_seq DESC LIMIT 1`, schema)).
		Scan(&lastTo, &prevSig); err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	fromSeq := int64(1)
	if lastTo.Valid {
		fromSeq = lastTo.Int64 + 1
	}

	var maxSeq sql.NullInt64
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT MAX(seq) FROM %s.audit_logs`, schema)).Scan(&maxSeq); err != nil {
		return nil, err
	}
	if !maxSeq.Valid || maxSeq.Int64 < fromSeq {
		return nil, nil // nothing new to seal
	}

	digest, rowCount, unsignedCount, err := digestRange(schema, fromSeq, maxSeq.Int64)
	if err != nil {
		return nil, err
	}
	sig := signCheckpoint(tenantID, fromSeq, maxSeq.Int64, rowCount, unsignedCount, digest, prevSig.String)

	cp := &AuditCheckpoint{
		FromSeq: fromSeq, ToSeq: maxSeq.Int64, RowCount: rowCount,
		UnsignedCount: unsignedCount, RowDigest: digest, Signature: sig, Kind: kind,
	}
	if err := db.DB.QueryRow(fmt.Sprintf(`
		INSERT INTO %s.audit_checkpoints
			(from_seq, to_seq, row_count, unsigned_count, row_digest,
			 prev_checkpoint_signature, signature, sig_version, kind)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id::text`, schema),
		fromSeq, maxSeq.Int64, rowCount, unsignedCount, digest,
		nullIfEmpty(prevSig.String), sig, AuditSigVersion, kind).Scan(&cp.ID); err != nil {
		return nil, err
	}
	return cp, nil
}

// AuditCheckpoint is one sealed window.
type AuditCheckpoint struct {
	ID            string `json:"id"`
	FromSeq       int64  `json:"from_seq"`
	ToSeq         int64  `json:"to_seq"`
	RowCount      int    `json:"row_count"`
	UnsignedCount int    `json:"unsigned_count"`
	RowDigest     string `json:"row_digest"`
	Signature     string `json:"signature"`
	Kind          string `json:"kind"`
}

// SealLegacyAuditRows is 47.7.4, and the whole point is what it does NOT do.
//
// 345,808 of the development database's 383,810 audit rows (90.1%) have no
// checksum at all. The tempting move is to "backfill" them - compute a
// signature now and store it - which would make the verifier report 100%
// coverage. That would be fabricated evidence: a signature computed today
// proves only that the row exists today, and asserting otherwise is worse than
// admitting the gap, because it converts a known limitation into a false
// assurance an auditor would rely on.
//
// So instead: one boundary checkpoint over the legacy range. From the seal
// forward, any change to those rows is detectable (the digest moves). Before
// it, nothing is claimed about who wrote them. The coverage statement says so
// in words, every time anyone verifies.
func SealLegacyAuditRows(tenantID string) (*AuditCheckpoint, error) {
	return WriteAuditCheckpoint(tenantID, "Sealed")
}

// --- 47.7.7: scheduled verification and checkpointing ----------------------

// Job types, run on Stage 38.6's durable runner rather than a bespoke ticker -
// that runner already has retries, a DLQ and a visibility screen, and
// micro_checklist's own 47.11.4 names every bespoke ticker in this codebase as
// something to migrate ONTO it. Adding a new one here would be moving the
// wrong way.
const (
	AuditCheckpointJobType = "audit_checkpoint"
	AuditVerifyJobType     = "audit_verify"
)

func init() {
	RegisterJobHandler(AuditCheckpointJobType, runAuditCheckpointJob)
	RegisterJobHandler(AuditVerifyJobType, runAuditVerifyJob)
}

func runAuditCheckpointJob(schema string, job Job) (map[string]interface{}, error) {
	tenantID := schemaToTenantID(schema)
	cp, err := WriteAuditCheckpoint(tenantID, "Periodic")
	if err != nil {
		return nil, err
	}
	if cp == nil {
		return map[string]interface{}{"checkpointed": false, "reason": "no new audit rows"}, nil
	}
	return map[string]interface{}{
		"checkpointed": true, "checkpoint_id": cp.ID,
		"from_seq": cp.FromSeq, "to_seq": cp.ToSeq, "rows": cp.RowCount,
	}, nil
}

// runAuditVerifyJob is the scheduled verifier. 47.7.7's requirement is subtle
// and worth stating: "alert on MISSING or failed verification, not just
// explicit failure." A verifier that only shouts when it finds tampering is
// silent in exactly the scenario an attacker wants - the verifier not running
// at all. So a failure to verify is escalated as loudly as a failed
// verification, and AuditVerificationOverdue reports the silence itself.
func runAuditVerifyJob(schema string, job Job) (map[string]interface{}, error) {
	tenantID := schemaToTenantID(schema)
	result, err := VerifyAuditEvidence(tenantID)
	if err != nil {
		LogSystemError(tenantID, "", "CRITICAL", "audit.verify",
			fmt.Sprintf("audit verification could not run: %v - treat this as unverified, not as passing", err), "")
		return nil, err
	}
	if !result.Intact {
		LogSystemError(tenantID, "", "CRITICAL", "audit.verify",
			fmt.Sprintf("audit evidence FAILED verification: %d tampered row(s) %v, %d bad checkpoint(s) %v",
				len(result.TamperedRows), result.TamperedRows, len(result.CheckpointsBad), result.CheckpointsBad), "")
	}
	return map[string]interface{}{
		"intact": result.Intact, "verified": result.Verified,
		"unsigned_legacy": result.UnsignedLegacy, "checkpoints_ok": result.CheckpointsOK,
	}, nil
}

// AuditVerificationOverdue reports how long it has been since the last
// checkpoint, and whether that exceeds the tolerance.
//
// This is the "alert on missing verification" half. Absence of a failure is
// not evidence of success: if the job stopped running a week ago, every
// dashboard still shows the last green result, and that is precisely the
// state an administrator-level attacker would engineer.
func AuditVerificationOverdue(tenantID string, tolerance time.Duration) (overdue bool, since time.Duration, err error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return false, 0, err
	}
	var last sql.NullTime
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT MAX(created_at) FROM %s.audit_checkpoints`, schema)).Scan(&last); err != nil {
		return false, 0, err
	}
	if !last.Valid {
		// Never checkpointed at all. Overdue by definition - there is no
		// evidence of verification to be reassured by.
		return true, 0, nil
	}
	since = time.Since(last.Time)
	return since > tolerance, since, nil
}

// StartAuditEvidenceScheduler enqueues the checkpoint and verification jobs on
// Stage 38.6's durable runner, per tenant, on an interval.
//
// The scheduler is a thin ticker that ENQUEUES; the runner does the work. That
// split is deliberate and is what 47.11.4 asks for generally: the runner
// already owns retries, the DLQ, lease handling and a visibility screen, so a
// bespoke ticker that did the work itself would be a second mechanism with
// none of that. The idempotency key is the tenant plus the hour, so a restart
// mid-hour re-enqueues nothing.
func StartAuditEvidenceScheduler(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if db.DB == nil {
					continue
				}
				schemas, err := listTenantSchemas()
				if err != nil {
					log.Printf("[AUDIT-EVIDENCE] cannot list tenant schemas: %v", err)
					continue
				}
				stamp := time.Now().UTC().Format("2006-01-02T15")
				for _, schema := range schemas {
					// A database that has not had the 47.7 migration applied has
					// no audit_checkpoints table; skip quietly rather than
					// erroring every tick, the same guard the reservation
					// sweeper uses for its own Stage 35.3.7 column.
					var ready bool
					if err := db.DB.QueryRow(`SELECT to_regclass($1) IS NOT NULL`,
						schema+".audit_checkpoints").Scan(&ready); err != nil || !ready {
						continue
					}
					tenantID := schemaToTenantID(schema)
					if _, err := EnqueueJob(tenantID, AuditCheckpointJobType, nil,
						fmt.Sprintf("audit-checkpoint:%s:%s", tenantID, stamp)); err != nil {
						log.Printf("[AUDIT-EVIDENCE] %s: could not enqueue checkpoint: %v", schema, err)
					}
					if _, err := EnqueueJob(tenantID, AuditVerifyJobType, nil,
						fmt.Sprintf("audit-verify:%s:%s", tenantID, stamp)); err != nil {
						log.Printf("[AUDIT-EVIDENCE] %s: could not enqueue verification: %v", schema, err)
					}
				}
			}
		}
	}()
}
