package engines

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"custom_erp/db"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"
)

// Stage 47.7.6 - the archive half of A-07/A-30's audit evidence work.
//
// 47.7.1-47.7.5/47.7.7/47.7.8 (engines/audit_evidence.go) built signed rows and
// checkpoints; the retention/legal-hold SCHEMA shipped in
// migrations_stage47_7_audit_evidence.sql (audit_retention_policy,
// audit_legal_hold) with auto-deletion deliberately OFF by default. What was
// missing was the archive itself: an integrity-checked encrypted/compressed
// archive, manifest-then-delete, and a tested query/export/restore drill.
//
// THE DESIGN CONSTRAINT THAT SHAPES ALL OF THIS: a checkpoint's verification
// (verifyCheckpoints in audit_evidence.go) proves a sealed [from_seq, to_seq]
// window has not been tampered with or had rows deleted from it, by re-reading
// audit_logs and recomputing the digest. If archiving simply deleted the rows
// after copying them out, every future verification of that checkpoint would
// find the window "missing" and report a legitimate archive as tampering -
// training an operator to ignore the one alarm that matters. So an archived
// window's verification is redirected (not weakened) to the archive file
// itself: the file's own SHA-256 on disk, a signature over its metadata, and a
// recomputed digest over its decrypted content must all agree with what the
// checkpoint sealed. Deleting or corrupting the archive file is exactly as
// detectable as deleting or corrupting the original rows would have been.
//
// ONE ARCHIVE == ONE CHECKPOINT WINDOW, atomically. A single held row (legal
// hold) or a single mismatched row blocks archiving the WHOLE window rather
// than fragmenting it - see RunAuditArchive.

// --- encryption --------------------------------------------------------

// auditArchiveKey follows engines/channel_credentials.go's
// loadOrGenerateChannelCredentialKey pattern exactly (itself mirroring
// engines/auth.go's JWT secret loading): AUDIT_ARCHIVE_KEY env var wins in
// production, else a random key is generated once and persisted outside the
// repo under the OS per-user config dir. Deliberately a SEPARATE key from the
// channel-credential one - two independent secrets, so rotating or
// compromising one never touches the other.
var auditArchiveKey = loadOrGenerateAuditArchiveKey()

func loadOrGenerateAuditArchiveKey() []byte {
	if v := os.Getenv("AUDIT_ARCHIVE_KEY"); v != "" {
		key := []byte(v)
		if len(key) != 32 {
			log.Fatalf("AUDIT_ARCHIVE_KEY must be exactly 32 bytes for AES-256, got %d", len(key))
		}
		return key
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		log.Fatalf("cannot determine user config dir for audit archive key persistence: %v", err)
	}
	keyPath := filepath.Join(configDir, "custom_erp", "audit_archive_key.local")
	if data, err := os.ReadFile(keyPath); err == nil && len(data) == 32 {
		return data
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		log.Fatalf("failed to generate audit archive key: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(keyPath), 0700); err != nil {
		log.Fatalf("failed to create config dir for audit archive key: %v", err)
	}
	if err := os.WriteFile(keyPath, raw, 0600); err != nil {
		log.Fatalf("failed to persist audit archive key: %v", err)
	}
	log.Printf("Generated new local audit archive encryption key at %s - set AUDIT_ARCHIVE_KEY env var explicitly for production deployments", keyPath)
	return raw
}

func encryptArchiveBytes(plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(auditArchiveKey)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

func decryptArchiveBytes(ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(auditArchiveKey)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("archive ciphertext is too short")
	}
	nonce, encrypted := ciphertext[:nonceSize], ciphertext[nonceSize:]
	return gcm.Open(nil, nonce, encrypted, nil)
}

// archiveDir mirrors engines/alerting.go's backupDir() - an
// AUDIT_ARCHIVE_DIR-or-default env var, unset (and therefore a no-op) on
// every dev box and CI runner.
func archiveDir() string {
	if dir := os.Getenv("AUDIT_ARCHIVE_DIR"); dir != "" {
		return dir
	}
	return "/opt/erp/audit-archives"
}

// ensureArchiveDirReady validates archiveDir() before RunAuditArchive writes
// anything: absolute, existing (or creatable), and actually writable.
//
// Found in review after the 2026-09-11 incident: the archive directory was
// never checked, only used - a test pointed AUDIT_ARCHIVE_DIR at
// t.TempDir(), which Go deletes at process exit, and nothing refused that.
// An absolute-path requirement additionally rules out a relative path
// silently resolving against whatever the process's working directory
// happens to be, which is not a promise about where evidence persists.
func ensureArchiveDirReady() (string, error) {
	dir := archiveDir()
	if !filepath.IsAbs(dir) {
		return "", fmt.Errorf("AUDIT_ARCHIVE_DIR %q is not an absolute path - refusing rather than silently resolving it against the process's current working directory", dir)
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("archive directory %q does not exist and could not be created: %w", dir, err)
	}
	probe := filepath.Join(dir, ".write_probe")
	if err := os.WriteFile(probe, []byte("ok"), 0600); err != nil {
		return "", fmt.Errorf("archive directory %q is not writable: %w", dir, err)
	}
	os.Remove(probe)
	return dir, nil
}

// --- shapes --------------------------------------------------------------

// AuditArchiveRow is one archived audit_logs row, preserved losslessly
// (including the legacy `checksum` chain field) so a restore can put it back
// exactly as it was.
type AuditArchiveRow struct {
	ID            string    `json:"id"`
	Seq           int64     `json:"seq"`
	UserID        string    `json:"user_id"`
	Action        string    `json:"action"`
	Status        string    `json:"status"`
	Details       string    `json:"details,omitempty"`
	EntityType    string    `json:"entity_type,omitempty"`
	EntityID      string    `json:"entity_id,omitempty"`
	CorrelationID string    `json:"correlation_id,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	Signature     string    `json:"signature,omitempty"`
	SigVersion    string    `json:"sig_version,omitempty"`
	Checksum      string    `json:"checksum,omitempty"`
}

// AuditArchiveManifest is the decrypted content of one archive file: the
// checkpoint metadata it was built from, plus every row.
type AuditArchiveManifest struct {
	TenantSchema  string             `json:"tenant_schema"`
	CheckpointID  string             `json:"checkpoint_id"`
	FromSeq       int64              `json:"from_seq"`
	ToSeq         int64              `json:"to_seq"`
	RowCount      int                `json:"row_count"`
	UnsignedCount int                `json:"unsigned_count"`
	RowDigest     string             `json:"row_digest"`
	SigVersion    string             `json:"sig_version"`
	CreatedAt     time.Time          `json:"created_at"`
	Rows          []AuditArchiveRow  `json:"rows"`
}

// AuditArchiveSummary is one archive's metadata, for the list endpoint -
// deliberately without the row content, which can be large.
//
// No RestoredAt/RestoredBy here: RestoreAuditArchive DELETES the archive row
// (see its comment for why a mutable flag is not safe), so any row this
// lists is, by construction, currently archived - a restored one simply does
// not appear. The restore event itself is recorded as a signed audit_logs
// entry (action "audit_archive_restored"), which is where that history now
// lives.
type AuditArchiveSummary struct {
	ID                string    `json:"id"`
	CheckpointID      string    `json:"checkpoint_id"`
	FromSeq           int64     `json:"from_seq"`
	ToSeq             int64     `json:"to_seq"`
	RowCount          int       `json:"row_count"`
	UnsignedCount     int       `json:"unsigned_count"`
	CreatedAt         time.Time `json:"created_at"`
	CurrentlyArchived bool      `json:"currently_archived"`
}

// AuditArchiveWindowResult reports what happened to one candidate checkpoint
// window during a RunAuditArchive pass, including WHY a window was skipped -
// silently skipping is how an operator loses confidence that retention is
// actually happening.
type AuditArchiveWindowResult struct {
	CheckpointID string `json:"checkpoint_id"`
	ArchiveID    string `json:"archive_id,omitempty"`
	FromSeq      int64  `json:"from_seq"`
	ToSeq        int64  `json:"to_seq"`
	RowCount     int    `json:"row_count"`
	Archived     bool   `json:"archived"`
	Reason       string `json:"reason,omitempty"`
}

// AuditArchiveRunResult is one tenant's RunAuditArchive pass.
type AuditArchiveRunResult struct {
	Windows []AuditArchiveWindowResult `json:"windows"`
}

// AuditArchiveRestoreResult is the outcome of RestoreAuditArchive.
type AuditArchiveRestoreResult struct {
	ArchiveID    string `json:"archive_id"`
	CheckpointID string `json:"checkpoint_id"`
	RowsRestored int    `json:"rows_restored"`
}

// --- retention policy and legal holds -------------------------------------

type retentionPolicy struct {
	Version          int
	HotWindowDays    int
	ArchiveAfterDays int
	DeleteAfterDays  sql.NullInt64
	EffectiveFrom    time.Time
}

// currentRetentionPolicy returns the highest-versioned policy already in
// effect - never a future-dated one - so "the rule in force right now"
// answers correctly even if a future policy has been scheduled ahead of time.
func currentRetentionPolicy(schema string) (*retentionPolicy, error) {
	var p retentionPolicy
	err := db.DB.QueryRow(fmt.Sprintf(`
		SELECT version, hot_window_days, archive_after_days, delete_after_days, effective_from
		  FROM %s.audit_retention_policy
		 WHERE effective_from <= CURRENT_TIMESTAMP
		 ORDER BY effective_from DESC, version DESC LIMIT 1`, schema)).
		Scan(&p.Version, &p.HotWindowDays, &p.ArchiveAfterDays, &p.DeleteAfterDays, &p.EffectiveFrom)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

type legalHold struct {
	EntityType, EntityID, Actor sql.NullString
}

func activeLegalHolds(schema string) ([]legalHold, error) {
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT scope_entity_type, scope_entity_id, scope_actor
		  FROM %s.audit_legal_hold WHERE released_at IS NULL`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []legalHold
	for rows.Next() {
		var h legalHold
		if err := rows.Scan(&h.EntityType, &h.EntityID, &h.Actor); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// rowIsHeld reports whether row falls inside an active legal hold's scope.
// Every scope field a hold specifies must match; a field the hold leaves
// unset is a wildcard. A hold with every field unset is a blanket hold that
// matches everything - a deliberate tenant-wide freeze, not a bug.
func rowIsHeld(holds []legalHold, row AuditArchiveRow) bool {
	for _, h := range holds {
		if h.EntityType.Valid && h.EntityType.String != row.EntityType {
			continue
		}
		if h.EntityID.Valid && h.EntityID.String != row.EntityID {
			continue
		}
		if h.Actor.Valid && h.Actor.String != row.UserID {
			continue
		}
		return true
	}
	return false
}

// --- selecting what to archive --------------------------------------------

type archiveCandidate struct {
	id                      string
	fromSeq, toSeq          int64
	rowCount, unsignedCount int
	digest                  string
}

// archiveEligibleCheckpoints returns every not-yet-archived checkpoint whose
// window is entirely older than cutoff, oldest first. A window whose rows
// have all already been deleted by something else (MAX(created_at) is NULL)
// is excluded rather than archived empty - digest verification against the
// checkpoint will already flag that as a discrepancy for the normal verifier
// to surface.
func archiveEligibleCheckpoints(schema string, cutoff time.Time) ([]archiveCandidate, error) {
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT c.id::text, c.from_seq, c.to_seq, c.row_count, c.unsigned_count, c.row_digest
		  FROM %s.audit_checkpoints c
		 WHERE c.archive_id IS NULL
		   AND c.kind IN ('Periodic', 'Sealed')
		   AND (SELECT MAX(a.created_at) FROM %s.audit_logs a
		         WHERE a.seq >= c.from_seq AND a.seq <= c.to_seq) < $1
		 ORDER BY c.to_seq ASC`, schema, schema), cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []archiveCandidate
	for rows.Next() {
		var c archiveCandidate
		if err := rows.Scan(&c.id, &c.fromSeq, &c.toSeq, &c.rowCount, &c.unsignedCount, &c.digest); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func loadAuditRowsInRange(schema string, fromSeq, toSeq int64) ([]AuditArchiveRow, error) {
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id::text, seq, user_id, action, status, COALESCE(details,''),
		       COALESCE(entity_type,''), COALESCE(entity_id,''), COALESCE(correlation_id,''),
		       created_at, COALESCE(signature,''), COALESCE(sig_version,''), COALESCE(checksum,'')
		  FROM %s.audit_logs WHERE seq >= $1 AND seq <= $2 ORDER BY seq ASC`, schema), fromSeq, toSeq)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditArchiveRow
	for rows.Next() {
		var r AuditArchiveRow
		if err := rows.Scan(&r.ID, &r.Seq, &r.UserID, &r.Action, &r.Status, &r.Details,
			&r.EntityType, &r.EntityID, &r.CorrelationID, &r.CreatedAt, &r.Signature, &r.SigVersion, &r.Checksum); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// --- signing and file-level verification ----------------------------------

func signArchiveManifest(tenantID, checkpointID string, fromSeq, toSeq int64, rowCount, unsignedCount int, rowDigest, fileSHA string) string {
	mac := hmac.New(sha256.New, jwtSigningKey.secret)
	fmt.Fprintf(mac, "%s|%s|%s|%d|%d|%d|%d|%s|%s",
		AuditSigVersion, tenantID, checkpointID, fromSeq, toSeq, rowCount, unsignedCount, rowDigest, fileSHA)
	return hex.EncodeToString(mac.Sum(nil))
}

// verifyArchiveFile is the ONE place an archive's content is trusted: called
// right after a fresh write (before any DB row references it) and again
// every time an archive is read back for checkpoint verification, query,
// export or restore - so a corrupted or substituted file is caught
// identically in every path rather than only in some of them.
//
// Four independent checks, in order, each catching a different failure: the
// file is readable at all; its bytes on disk match what was recorded at
// write time (catches substitution/corruption of the ciphertext itself); its
// metadata signature holds (catches a forged audit_archives row pointing at
// an unrelated file); and its decrypted content's digest/counts match what
// the checkpoint sealed (catches tampering with the plaintext that a
// same-key re-encryption could otherwise hide).
func verifyArchiveFile(filePath, fileSHA, sig, sigVersion, tenantID, checkpointID string,
	fromSeq, toSeq int64, rowCount, unsignedCount int, rowDigest string) (*AuditArchiveManifest, error) {

	raw, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("archive file unreadable: %w", err)
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != fileSHA {
		return nil, fmt.Errorf("archive file checksum mismatch - the file on disk does not match what was recorded when it was written")
	}
	if sigVersion == AuditSigVersion {
		wantSig := signArchiveManifest(tenantID, checkpointID, fromSeq, toSeq, rowCount, unsignedCount, rowDigest, fileSHA)
		if !hmac.Equal([]byte(wantSig), []byte(sig)) {
			return nil, fmt.Errorf("archive manifest signature invalid")
		}
	}

	plain, err := decryptArchiveBytes(raw)
	if err != nil {
		return nil, fmt.Errorf("archive decryption failed: %w", err)
	}
	zr, err := gzip.NewReader(bytes.NewReader(plain))
	if err != nil {
		return nil, fmt.Errorf("archive decompression failed: %w", err)
	}
	defer zr.Close()
	jsonBytes, err := io.ReadAll(zr)
	if err != nil {
		return nil, fmt.Errorf("archive decompression failed: %w", err)
	}
	var manifest AuditArchiveManifest
	if err := json.Unmarshal(jsonBytes, &manifest); err != nil {
		return nil, fmt.Errorf("archive manifest is not valid JSON: %w", err)
	}

	var pairs [][2]string
	for _, r := range manifest.Rows {
		pairs = append(pairs, [2]string{r.Signature, r.ID})
	}
	gotDigest := digestSigIDPairs(pairs)
	gotUnsigned := countUnsigned(pairs)
	if gotDigest != rowDigest || len(manifest.Rows) != rowCount || gotUnsigned != unsignedCount {
		return nil, fmt.Errorf("archived content does not match what was sealed (%d rows now, %d sealed)", len(manifest.Rows), rowCount)
	}
	return &manifest, nil
}

type archiveMeta struct {
	filePath, fileSHA, sig, sigVer, rowDigest, checkpointID string
	fromSeq, toSeq                                          int64
	rowCount, unsignedCount                                 int
}

func loadArchiveMeta(schema, archiveID string) (archiveMeta, error) {
	var m archiveMeta
	err := db.DB.QueryRow(fmt.Sprintf(`
		SELECT file_path, file_sha256, manifest_signature, sig_version, row_digest,
		       checkpoint_id::text, from_seq, to_seq, row_count, unsigned_count
		  FROM %s.audit_archives WHERE id = $1::uuid`, schema), archiveID).Scan(
		&m.filePath, &m.fileSHA, &m.sig, &m.sigVer, &m.rowDigest, &m.checkpointID,
		&m.fromSeq, &m.toSeq, &m.rowCount, &m.unsignedCount)
	if err != nil {
		return archiveMeta{}, fmt.Errorf("no such archive: %w", err)
	}
	return m, nil
}

// loadAndVerifyArchive loads one archive's metadata and its full, verified
// content. Used by query/export/restore, which are keyed directly by
// archiveID from an admin action.
func loadAndVerifyArchive(tenantID, schema, archiveID string) (*AuditArchiveManifest, error) {
	m, err := loadArchiveMeta(schema, archiveID)
	if err != nil {
		return nil, err
	}
	return verifyArchiveFile(m.filePath, m.fileSHA, m.sig, m.sigVer, tenantID, m.checkpointID,
		m.fromSeq, m.toSeq, m.rowCount, m.unsignedCount, m.rowDigest)
}

// verifyArchivedWindow is what verifyCheckpoints (engines/audit_evidence.go)
// calls in place of digestRange once a checkpoint's window has been
// archived: same three return values, so the caller's comparison against the
// checkpoint's sealed digest/count/unsigned-count is unchanged, just sourced
// from the archive instead of the live table.
//
// SECURITY: audit_checkpoints.archive_id is a plain foreign key, not itself
// covered by the checkpoint's own signCheckpoint signature (which predates
// archiving and cannot be re-signed without invalidating every existing
// checkpoint). So this function does NOT trust archive_id merely because it
// is set - an attacker with direct DB write access could otherwise point a
// checkpoint whose rows they just deleted at an unrelated archive record.
// The explicit check below closes that: the archive's OWN checkpoint_id must
// equal the checkpoint that is asking, rather than leaving it to the hope
// that two different windows happen to produce the same SHA-256 digest.
//
// A restored archive needs no separate check here: RestoreAuditArchive
// DELETES the archive row rather than flagging it (a flag would be an
// unsigned column an attacker could simply reset), so loadArchiveMeta below
// already returns "no such archive" for one, and repointing a checkpoint at
// a deleted archive's id is refused by the foreign key itself before this
// function is ever reached.
func verifyArchivedWindow(tenantID, schema, checkpointID, archiveID string) (string, int, int, error) {
	m, err := loadArchiveMeta(schema, archiveID)
	if err != nil {
		return "", 0, 0, err
	}
	if m.checkpointID != checkpointID {
		return "", 0, 0, fmt.Errorf("archive %s belongs to checkpoint %s, not %s", archiveID, m.checkpointID, checkpointID)
	}
	if _, err := verifyArchiveFile(m.filePath, m.fileSHA, m.sig, m.sigVer, tenantID, m.checkpointID,
		m.fromSeq, m.toSeq, m.rowCount, m.unsignedCount, m.rowDigest); err != nil {
		return "", 0, 0, err
	}
	return m.rowDigest, m.rowCount, m.unsignedCount, nil
}

// --- the archive pass itself -----------------------------------------------

// minArchiveAfterDays is a floor RunAuditArchive refuses to operate below,
// independent of whatever audit_retention_policy says.
//
// Found in review (2026-09-11): a test that set archive_after_days to 0/-1
// to make its own just-written rows "old enough" made EVERY checkpoint in
// the tenant eligible in the same pass - including one covering the entire
// pre-existing history, which was then archived into a temp directory that
// got deleted, permanently losing ~389,000 rows from a shared dev database.
// Isolating the test fixed that occurrence, but the underlying hole is in
// the engine, not the test: nothing stops audit_retention_policy.archive_after_days
// from being set to 0 (or negative) in production by a fat-fingered admin
// action, and RunAuditArchive would honor it exactly as literally as the
// test did. This floor makes that class of misconfiguration a refused,
// reported error instead of a silent mass-archive.
const minArchiveAfterDays = 30

// maxRowsPerArchiveWindow caps how large a single checkpoint window
// RunAuditArchive will archive in one pass unless allowLargeWindows is true.
//
// archiveEligibleCheckpoints has no LIMIT, and the one-time legacy Sealed
// checkpoint (47.7.4's boundary over everything that predates Stage 47.7)
// can be hundreds of thousands of rows on a real production database - doing
// that inside one transaction, unattended, on an hourly/daily schedule, is
// not something an automatic pass should ever do silently. The scheduled job
// (runAuditArchiveJob) always passes false; a deliberate operator-triggered
// run (the manual HTTP trigger) can pass true to process it anyway.
//
// A var, not a const, so audit_archive_test.go can lower it to exercise this
// boundary without writing 50,000 real rows.
var maxRowsPerArchiveWindow = 50000

// RunAuditArchive archives every eligible checkpoint window for a tenant:
// old enough per the current retention policy, not already archived, and
// free of any actively held row. Manifest-then-delete: the file is written
// and self-verified, THEN the audit_archives row and the audit_logs deletion
// commit together in one transaction - never the deletion without a verified
// record of where the data went.
func RunAuditArchive(tenantID string, allowLargeWindows bool) (*AuditArchiveRunResult, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}

	policy, err := currentRetentionPolicy(schema)
	if err != nil {
		return nil, err
	}
	if policy == nil {
		return &AuditArchiveRunResult{}, nil
	}
	if policy.ArchiveAfterDays < minArchiveAfterDays {
		return nil, fmt.Errorf("retention policy version %d sets archive_after_days=%d, below the %d-day floor - "+
			"refusing to archive rather than risk sweeping up recent or entire history under a misconfigured policy",
			policy.Version, policy.ArchiveAfterDays, minArchiveAfterDays)
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -policy.ArchiveAfterDays)

	// Validated once per pass, before any candidate is touched: a directory
	// that turns out unwritable partway through a pass would leave earlier
	// windows archived and later ones not, for a reason that has nothing to
	// do with the data itself.
	archiveDirPath, err := ensureArchiveDirReady()
	if err != nil {
		return nil, err
	}

	candidates, err := archiveEligibleCheckpoints(schema, cutoff)
	if err != nil {
		return nil, err
	}
	holds, err := activeLegalHolds(schema)
	if err != nil {
		return nil, err
	}

	out := &AuditArchiveRunResult{}
	for _, cand := range candidates {
		res := AuditArchiveWindowResult{CheckpointID: cand.id, FromSeq: cand.fromSeq, ToSeq: cand.toSeq}

		if cand.rowCount > maxRowsPerArchiveWindow && !allowLargeWindows {
			res.Reason = fmt.Sprintf("window has %d rows, exceeding the %d-row automatic ceiling - needs an operator-run archive with an explicit override",
				cand.rowCount, maxRowsPerArchiveWindow)
			out.Windows = append(out.Windows, res)
			continue
		}

		// Every failure below degrades to a skip-with-reason rather than
		// aborting the whole pass: one problem window (a race, a transient
		// read error, a discrepancy) must not stop every OTHER eligible
		// window in this tenant from being archived on schedule.
		rows, err := loadAuditRowsInRange(schema, cand.fromSeq, cand.toSeq)
		if err != nil {
			res.Reason = fmt.Sprintf("could not read rows: %v", err)
			out.Windows = append(out.Windows, res)
			continue
		}
		if len(rows) != cand.rowCount {
			res.Reason = fmt.Sprintf("live row count (%d) no longer matches the checkpoint (%d) - skipped rather than archiving over a discrepancy the verifier should investigate", len(rows), cand.rowCount)
			out.Windows = append(out.Windows, res)
			continue
		}
		var pairs [][2]string
		for _, r := range rows {
			pairs = append(pairs, [2]string{r.Signature, r.ID})
		}
		if digestSigIDPairs(pairs) != cand.digest {
			res.Reason = "recomputed digest does not match the checkpoint's sealed digest - skipped, this is a tamper signal the verifier should surface, not something archiving should paper over"
			out.Windows = append(out.Windows, res)
			continue
		}

		heldCount := 0
		for _, r := range rows {
			if rowIsHeld(holds, r) {
				heldCount++
			}
		}
		if heldCount > 0 {
			res.Reason = fmt.Sprintf("%d row(s) in this window are under an active legal hold - the whole window is skipped rather than archived partially", heldCount)
			out.Windows = append(out.Windows, res)
			continue
		}

		archiveID, err := writeArchiveWindow(tenantID, schema, cand, rows, archiveDirPath)
		if err != nil {
			res.Reason = err.Error()
			out.Windows = append(out.Windows, res)
			continue
		}
		res.RowCount = len(rows)
		res.ArchiveID = archiveID
		res.Archived = true
		out.Windows = append(out.Windows, res)
	}
	return out, nil
}

func writeArchiveWindow(tenantID, schema string, cand archiveCandidate, rows []AuditArchiveRow, dir string) (string, error) {
	manifest := AuditArchiveManifest{
		TenantSchema: schema, CheckpointID: cand.id, FromSeq: cand.fromSeq, ToSeq: cand.toSeq,
		RowCount: cand.rowCount, UnsignedCount: cand.unsignedCount, RowDigest: cand.digest,
		SigVersion: AuditSigVersion, CreatedAt: time.Now().UTC(), Rows: rows,
	}
	plain, err := json.Marshal(manifest)
	if err != nil {
		return "", err
	}

	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	if _, err := zw.Write(plain); err != nil {
		return "", err
	}
	if err := zw.Close(); err != nil {
		return "", err
	}

	encrypted, err := encryptArchiveBytes(gz.Bytes())
	if err != nil {
		return "", err
	}

	fileName := fmt.Sprintf("audit_archive_%s_%d-%d_%s.json.gz.enc",
		schema, cand.fromSeq, cand.toSeq, time.Now().UTC().Format("20060102T150405Z"))
	filePath := filepath.Join(dir, fileName)
	if err := os.WriteFile(filePath, encrypted, 0600); err != nil {
		return "", fmt.Errorf("cannot write archive file: %w", err)
	}

	sum := sha256.Sum256(encrypted)
	fileSHA := hex.EncodeToString(sum[:])
	sig := signArchiveManifest(tenantID, cand.id, cand.fromSeq, cand.toSeq, cand.rowCount, cand.unsignedCount, cand.digest, fileSHA)

	// Manifest-then-delete, step one: prove the file can be read back and
	// decodes to exactly what was just written BEFORE this archive is
	// recorded or a single row is deleted.
	if _, err := verifyArchiveFile(filePath, fileSHA, sig, AuditSigVersion, tenantID, cand.id,
		cand.fromSeq, cand.toSeq, cand.rowCount, cand.unsignedCount, cand.digest); err != nil {
		os.Remove(filePath)
		return "", fmt.Errorf("archive self-check failed, nothing was deleted or recorded: %w", err)
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	var archiveID string
	if err := tx.QueryRow(fmt.Sprintf(`
		INSERT INTO %s.audit_archives
			(checkpoint_id, from_seq, to_seq, row_count, unsigned_count, row_digest,
			 file_path, file_sha256, manifest_signature, sig_version, compressed_bytes)
		VALUES ($1::uuid,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING id::text`, schema),
		cand.id, cand.fromSeq, cand.toSeq, cand.rowCount, cand.unsignedCount, cand.digest,
		filePath, fileSHA, sig, AuditSigVersion, len(encrypted)).Scan(&archiveID); err != nil {
		os.Remove(filePath)
		return "", fmt.Errorf("could not record archive: %w", err)
	}
	// Manifest-then-delete, step two: the archive row and the deletion commit
	// together. A checkpoint is never marked archived without the archive
	// record existing, and rows are never deleted without the checkpoint
	// pointing at where they went.
	//
	// "AND archive_id IS NULL" guards against a concurrent archive run (a
	// manual trigger racing the scheduler, or two scheduler ticks) picking
	// the same eligible checkpoint before either commits: archiveEligibleCheckpoints
	// is a plain SELECT with no locking, so both could reach this point. The
	// loser's UPDATE affects zero rows; that is treated as losing the race,
	// not as a delete-worthy success, so this transaction rolls back and
	// nothing is lost - the winner's archive (identical content, since both
	// read the same not-yet-deleted rows) is the one left standing.
	res, err := tx.Exec(fmt.Sprintf(
		`UPDATE %s.audit_checkpoints SET archive_id = $1::uuid WHERE id = $2::uuid AND archive_id IS NULL`, schema),
		archiveID, cand.id)
	if err != nil {
		os.Remove(filePath)
		return "", fmt.Errorf("could not mark checkpoint archived: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		os.Remove(filePath)
		return "", fmt.Errorf("checkpoint %s was archived by a concurrent run - skipping this one", cand.id)
	}
	if _, err := tx.Exec(fmt.Sprintf(
		`DELETE FROM %s.audit_logs WHERE seq >= $1 AND seq <= $2`, schema),
		cand.fromSeq, cand.toSeq); err != nil {
		os.Remove(filePath)
		return "", fmt.Errorf("could not delete archived rows: %w", err)
	}
	if err := tx.Commit(); err != nil {
		os.Remove(filePath)
		return "", err
	}
	return archiveID, nil
}

// --- query, export, restore -------------------------------------------------

// ListAuditArchives lists every archive's metadata (not its row content, which
// can be large) for a tenant, newest window first.
func ListAuditArchives(tenantID string) ([]AuditArchiveSummary, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT a.id::text, a.checkpoint_id::text, a.from_seq, a.to_seq, a.row_count, a.unsigned_count,
		       a.created_at,
		       EXISTS(SELECT 1 FROM %s.audit_checkpoints c WHERE c.id = a.checkpoint_id AND c.archive_id = a.id)
		  FROM %s.audit_archives a
		 ORDER BY a.to_seq DESC`, schema, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditArchiveSummary
	for rows.Next() {
		var s AuditArchiveSummary
		if err := rows.Scan(&s.ID, &s.CheckpointID, &s.FromSeq, &s.ToSeq, &s.RowCount, &s.UnsignedCount,
			&s.CreatedAt, &s.CurrentlyArchived); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ReadAuditArchive decrypts, decompresses and fully verifies one archive,
// returning every row it holds - the shared read path for both the query and
// export endpoints. A caller that only wants a subset filters the returned
// rows itself; the trust boundary (does this content match what was sealed)
// is the same either way and must not be skipped for a "just export it" path.
func ReadAuditArchive(tenantID, archiveID string) (*AuditArchiveManifest, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	return loadAndVerifyArchive(tenantID, schema, archiveID)
}

// RestoreAuditArchive is the drill (and the real legal-recovery path): verify
// the archive, re-insert every row into audit_logs with its original id and
// seq, and point the checkpoint back at the live table. Refuses to run
// against an archive that is not the checkpoint's CURRENT archive (already
// restored, or superseded) - re-inserting would otherwise collide with the
// seq unique index.
func RestoreAuditArchive(tenantID, archiveID, restoredBy string) (*AuditArchiveRestoreResult, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	manifest, err := loadAndVerifyArchive(tenantID, schema, archiveID)
	if err != nil {
		return nil, fmt.Errorf("refusing to restore: %w", err)
	}

	var currentlyArchived bool
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT archive_id = $1::uuid FROM %s.audit_checkpoints WHERE id = $2::uuid`, schema),
		archiveID, manifest.CheckpointID).Scan(&currentlyArchived); err != nil {
		return nil, fmt.Errorf("could not read checkpoint state: %w", err)
	}
	if !currentlyArchived {
		return nil, fmt.Errorf("checkpoint %s is not currently pointing at archive %s - already restored, or this archive was superseded", manifest.CheckpointID, archiveID)
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	insertStmt := fmt.Sprintf(`
		INSERT INTO %s.audit_logs
			(id, seq, user_id, action, status, details, entity_type, entity_id,
			 correlation_id, created_at, signature, sig_version, checksum)
		VALUES ($1::uuid,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, schema)
	for _, r := range manifest.Rows {
		if _, err := tx.Exec(insertStmt, r.ID, r.Seq, r.UserID, r.Action, r.Status, nullIfEmpty(r.Details),
			nullIfEmpty(r.EntityType), nullIfEmpty(r.EntityID), nullIfEmpty(r.CorrelationID),
			r.CreatedAt, nullIfEmpty(r.Signature), nullIfEmpty(r.SigVersion), nullIfEmpty(r.Checksum)); err != nil {
			return nil, fmt.Errorf("could not restore row %s (seq %d): %w", r.ID, r.Seq, err)
		}
	}
	// The restore itself becomes a signed audit event, inside the same
	// transaction as the rows it restores - the same commit-together
	// guarantee WriteAuditEvidenceTx gives every other mutation this system
	// audits. This is also what makes the fail-closed design below safe:
	// the historical fact "this window was archived and restored" now lives
	// in the tamper-evident audit_logs stream itself, not in a plain mutable
	// column, so deleting the audit_archives row loses no evidence.
	if err := WriteAuditEvidenceTx(tx, schema, tenantID, AuditEvidence{
		UserID: restoredBy, Action: "audit_archive_restored", Status: "Success",
		Details: fmt.Sprintf("restored %d row(s) (seq %d-%d) from archive %s back into audit_logs",
			len(manifest.Rows), manifest.FromSeq, manifest.ToSeq, archiveID),
		EntityType: "AuditArchive", EntityID: archiveID, CorrelationID: manifest.CheckpointID,
	}); err != nil {
		return nil, fmt.Errorf("could not record the restore as an audit event: %w", err)
	}
	if _, err := tx.Exec(fmt.Sprintf(
		`UPDATE %s.audit_checkpoints SET archive_id = NULL WHERE id = $1::uuid`, schema),
		manifest.CheckpointID); err != nil {
		return nil, err
	}
	// SECURITY (found in review, 2026-09-11): the archive row is DELETED
	// here rather than flagged with a restored_at timestamp. A flag is a
	// plain unsigned column - a DB-write attacker (the exact threat model
	// 47.7.1 names) could reset restored_at to NULL and reuse the archive as
	// a fake verification target after deleting the restored rows. Deleting
	// the row instead means loadArchiveMeta fails closed on "no such
	// archive" by construction, and audit_checkpoints.archive_id's own
	// foreign key refuses to let anyone repoint a checkpoint at an archive
	// that no longer exists - a constraint the database enforces, not a
	// check that can be raced or reset.
	if _, err := tx.Exec(fmt.Sprintf(
		`DELETE FROM %s.audit_archives WHERE id = $1::uuid`, schema), archiveID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &AuditArchiveRestoreResult{ArchiveID: archiveID, CheckpointID: manifest.CheckpointID, RowsRestored: len(manifest.Rows)}, nil
}

// --- scheduled archiving -----------------------------------------------------

const AuditArchiveJobType = "audit_archive"

func init() {
	RegisterJobHandler(AuditArchiveJobType, runAuditArchiveJob)
}

func runAuditArchiveJob(schema string, job Job) (map[string]interface{}, error) {
	tenantID := schemaToTenantID(schema)
	// false: the scheduled pass never overrides maxRowsPerArchiveWindow -
	// see that constant's comment for why an unattended run must not process
	// an unbounded window silently.
	result, err := RunAuditArchive(tenantID, false)
	if err != nil {
		return nil, err
	}
	archived := 0
	for _, w := range result.Windows {
		if w.Archived {
			archived++
		}
	}
	return map[string]interface{}{"windows_considered": len(result.Windows), "windows_archived": archived}, nil
}

// StartAuditArchiveScheduler enqueues a daily archive pass per tenant onto
// Stage 38.6's runner, the same enqueue-only split
// StartAuditEvidenceScheduler uses. Daily rather than hourly: archiving acts
// on day-granularity retention windows, so there is nothing to gain from a
// tighter cadence and every run scans every tenant's checkpoints.
func StartAuditArchiveScheduler(ctx context.Context, interval time.Duration) {
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
					log.Printf("[AUDIT-ARCHIVE] cannot list tenant schemas: %v", err)
					continue
				}
				stamp := time.Now().UTC().Format("2006-01-02")
				for _, schema := range schemas {
					var ready bool
					if err := db.DB.QueryRow(`SELECT to_regclass($1) IS NOT NULL`,
						schema+".audit_archives").Scan(&ready); err != nil || !ready {
						continue
					}
					tenantID := schemaToTenantID(schema)
					if _, err := EnqueueJob(tenantID, AuditArchiveJobType, nil,
						fmt.Sprintf("audit-archive:%s:%s", tenantID, stamp)); err != nil {
						log.Printf("[AUDIT-ARCHIVE] %s: could not enqueue archive: %v", schema, err)
					}
				}
			}
		}
	}()
}
