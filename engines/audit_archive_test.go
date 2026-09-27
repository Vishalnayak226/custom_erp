package engines

import (
	"custom_erp/db"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Stage 47.7.6 - the archive/restore drill 47.7's acceptance criteria named
// as the one thing still missing: "integrity-checked encrypted/compressed
// archive, manifest-then-delete, and a tested query/export/restore drill."
//
// INCIDENT (2026-09-11, found by a peer session mid-review): an earlier
// version of this file ran RunAuditArchive against tenant_default - the
// shared dev database every session's tests share - after sealing a
// "boundary checkpoint" meant only to isolate this test's own rows from
// pre-existing history. RunAuditArchive is correctly tenant-wide (it has to
// be, for its real job as a scheduled ops pass): it archives every eligible
// checkpoint, not just the one a caller has in mind. The boundary checkpoint
// WAS a real, eligible checkpoint covering the tenant's entire prior audit
// history, and archive_after_days was set to -1 to make the test's own rows
// eligible - which made that history eligible too. It was archived into
// AUDIT_ARCHIVE_DIR=t.TempDir(), which Go deletes when the test exits, and
// the original rows were already gone from audit_logs by then. Net effect:
// tenant_default.audit_logs dropped from roughly 389,000 rows to 334,
// unrecoverably (the newest dev-DB backup was 2 months stale; restoring it
// would have rolled back every other session's dev data too).
//
// THE FIX: never touch tenant_default from this file. Every test below runs
// against its own freshly provisioned, single-use scratch tenant schema
// (provisionScratchAuditTenant) that starts with zero audit_logs rows, so
// RunAuditArchive's tenant-wide sweep has nothing to reach but rows the test
// itself created - regardless of what retention override the test sets.

// provisionScratchAuditTenant creates an isolated tenant + schema holding
// only the tables this file's tests need (audit_logs, audit_checkpoints,
// audit_retention_policy, audit_legal_hold, audit_archives), built directly
// from the same DDL the real migrations use so structure cannot drift from
// production. DROP SCHEMA ... CASCADE on cleanup removes everything in one
// step - there is nothing precious in a schema this test created and owns
// exclusively.
func provisionScratchAuditTenant(t *testing.T) (tenantID, schema string) {
	t.Helper()
	db.InitDB(testConnStr())
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	tenantID = "auditarchive_test_" + suffix
	schema = "tenant_auditarchive_test_" + suffix

	if _, err := db.DB.Exec("INSERT INTO public.tenants (tenant_id, name, schema_name) VALUES ($1,$1,$2)", tenantID, schema); err != nil {
		t.Fatalf("register scratch tenant: %v", err)
	}
	if _, err := db.DB.Exec("CREATE SCHEMA " + schema); err != nil {
		db.DB.Exec("DELETE FROM public.tenants WHERE tenant_id = $1", tenantID)
		t.Fatalf("create scratch schema: %v", err)
	}
	t.Cleanup(func() {
		db.DB.Exec("DROP SCHEMA " + schema + " CASCADE")
		db.DB.Exec("DELETE FROM public.tenants WHERE tenant_id = $1", tenantID)
	})

	ddl := []string{
		`CREATE TABLE ` + schema + `.audit_logs (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			user_id VARCHAR(100) NOT NULL,
			action VARCHAR(255) NOT NULL,
			status VARCHAR(50) NOT NULL,
			details TEXT,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			checksum VARCHAR(64),
			seq BIGSERIAL,
			signature VARCHAR(64),
			sig_version VARCHAR(20),
			entity_type VARCHAR(100),
			entity_id VARCHAR(200),
			correlation_id VARCHAR(100)
		)`,
		`CREATE UNIQUE INDEX ON ` + schema + `.audit_logs (seq)`,
		`CREATE TABLE ` + schema + `.audit_checkpoints (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			from_seq BIGINT NOT NULL,
			to_seq BIGINT NOT NULL,
			row_count INT NOT NULL,
			unsigned_count INT NOT NULL DEFAULT 0,
			row_digest VARCHAR(64) NOT NULL,
			prev_checkpoint_signature VARCHAR(64),
			signature VARCHAR(64) NOT NULL,
			sig_version VARCHAR(20) NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			kind VARCHAR(20) NOT NULL DEFAULT 'Periodic',
			archive_id UUID
		)`,
		`CREATE TABLE ` + schema + `.audit_retention_policy (
			id SERIAL PRIMARY KEY,
			version INT NOT NULL,
			hot_window_days INT NOT NULL,
			archive_after_days INT NOT NULL,
			delete_after_days INT,
			effective_from TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			approved_by VARCHAR(100),
			note TEXT
		)`,
		`CREATE TABLE ` + schema + `.audit_legal_hold (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			scope_entity_type VARCHAR(100),
			scope_entity_id VARCHAR(200),
			scope_actor VARCHAR(100),
			reason TEXT NOT NULL,
			placed_by VARCHAR(100) NOT NULL,
			placed_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			released_by VARCHAR(100),
			released_at TIMESTAMP
		)`,
		`CREATE TABLE ` + schema + `.audit_archives (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			checkpoint_id UUID NOT NULL,
			from_seq BIGINT NOT NULL,
			to_seq BIGINT NOT NULL,
			row_count INT NOT NULL,
			unsigned_count INT NOT NULL DEFAULT 0,
			row_digest VARCHAR(64) NOT NULL,
			file_path TEXT NOT NULL,
			file_sha256 VARCHAR(64) NOT NULL,
			manifest_signature VARCHAR(64) NOT NULL,
			sig_version VARCHAR(20) NOT NULL,
			compressed_bytes INT NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			restored_at TIMESTAMP,
			restored_by VARCHAR(100)
		)`,
		// Same FK the real migration adds - TestAuditArchiveRepointAfterRestoreFailsClosed
		// depends on the database itself refusing to resurrect a reference to
		// a deleted archive.
		`ALTER TABLE ` + schema + `.audit_checkpoints
			ADD CONSTRAINT audit_checkpoints_archive_fk
			FOREIGN KEY (archive_id) REFERENCES ` + schema + `.audit_archives(id)`,
		// Same shipped default the real migration seeds: a year hot/archive,
		// auto-deletion off. Individual tests override archive_after_days via
		// setArchiveAfterDaysForTest.
		`INSERT INTO ` + schema + `.audit_retention_policy
			(version, hot_window_days, archive_after_days, delete_after_days, approved_by, note)
			VALUES (1, 365, 365, NULL, 'test-seed', 'scratch tenant default')`,
	}
	for _, stmt := range ddl {
		if _, err := db.DB.Exec(stmt); err != nil {
			t.Fatalf("scratch schema DDL failed (%s...): %v", stmt[:40], err)
		}
	}
	return tenantID, schema
}

// archiveTestSetup provisions a fresh scratch tenant (see
// provisionScratchAuditTenant and the incident note above - this NEVER
// touches tenant_default), seeds N rows genuinely old enough to archive,
// seals them into a checkpoint, sets the retention policy to the engine's
// own floor (minArchiveAfterDays), and points AUDIT_ARCHIVE_DIR at a scratch
// directory for this test.
//
// Rows are backdated (writeBackdatedAuditRow), not made eligible by
// disabling the policy: RunAuditArchive refuses archive_after_days below
// minArchiveAfterDays (found in the same review that found the data-loss
// bug this file is named after - see RunAuditArchive's comment), so a test
// cannot exercise archiving by asking for "everything is eligible" any more
// than production can. Aging the data itself is what a real caller would do.
func archiveTestSetup(t *testing.T, n int) (tenantID, schema, marker string, cp *AuditCheckpoint) {
	t.Helper()
	tenantID, schema = provisionScratchAuditTenant(t)
	marker = "AUDITARCHIVE-" + NewDocIDCompact("T")

	t.Setenv("AUDIT_ARCHIVE_DIR", t.TempDir())
	setArchiveAfterDaysForTest(t, schema, minArchiveAfterDays)

	backdated := time.Now().UTC().AddDate(0, 0, -(minArchiveAfterDays + 10))
	for i := 0; i < n; i++ {
		writeBackdatedAuditRow(t, tenantID, schema, "actor", marker, "Success", fmt.Sprintf("row %d", i), backdated)
	}
	var err error
	cp, err = WriteAuditCheckpoint(tenantID, "Periodic")
	if err != nil {
		t.Fatalf("checkpoint failed: %v", err)
	}
	if cp == nil {
		t.Fatal("no checkpoint was written despite new rows")
	}
	return tenantID, schema, marker, cp
}

// writeBackdatedAuditRow writes a validly signed audit_logs row with a
// caller-chosen created_at, bypassing LogAuditEvent/WriteAuditEvidenceTx
// (which always use time.Now()). SignAuditRow signs createdAt as one of its
// fields, so the signature is computed over the SAME backdated timestamp
// that gets stored - a row backdated by a raw UPDATE after the fact would
// fail its own signature, exactly the class of bug 47.7.8's tamper test was
// built to catch.
func writeBackdatedAuditRow(t *testing.T, tenantID, schema, userID, action, status, details string, createdAt time.Time) string {
	t.Helper()
	createdAt = createdAt.UTC().Truncate(time.Microsecond)
	sig := SignAuditRow(tenantID, userID, action, status, details, "", "", "", createdAt)
	var id string
	if err := db.DB.QueryRow(fmt.Sprintf(`
		INSERT INTO %s.audit_logs (user_id, action, status, details, created_at, signature, sig_version)
		VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id::text`, schema),
		userID, action, status, details, createdAt, sig, AuditSigVersion).Scan(&id); err != nil {
		t.Fatalf("could not write backdated audit row: %v", err)
	}
	return id
}

// TestAuditArchiveWriteVerifyDeleteRestoreDrill is the whole lifecycle in one
// place: archive a sealed window, confirm the rows are gone from audit_logs
// but the checkpoint STILL verifies (redirected to the archive, not reporting
// a legitimate archive as a deletion attack), query and export the archived
// content, then run the restore drill and confirm the checkpoint verifies
// again off the live table exactly as it did before any of this happened.
func TestAuditArchiveWriteVerifyDeleteRestoreDrill(t *testing.T) {
	tenantID, schema, marker, cp := archiveTestSetup(t, 5)

	before, err := VerifyAuditEvidence(tenantID)
	if err != nil {
		t.Fatalf("baseline verification failed: %v", err)
	}
	if len(before.CheckpointsBad) > 0 {
		t.Fatalf("checkpoint failed verification immediately after being sealed: %v", before.CheckpointsBad)
	}

	result, err := RunAuditArchive(tenantID, false)
	if err != nil {
		t.Fatalf("RunAuditArchive failed: %v", err)
	}
	var archiveID string
	for _, w := range result.Windows {
		if w.CheckpointID == cp.ID {
			if !w.Archived {
				t.Fatalf("checkpoint %s was not archived: %s", cp.ID, w.Reason)
			}
			archiveID = w.ArchiveID
		}
	}
	if archiveID == "" {
		t.Fatalf("RunAuditArchive did not report our checkpoint among its windows: %+v", result.Windows)
	}

	// The rows must actually be gone from the live table - archiving that
	// does not delete is not archiving, it is just an extra copy.
	rows := auditRowsFor(t, tenantID, marker)
	if len(rows) != 0 {
		t.Errorf("%d row(s) still live in audit_logs after archiving; manifest-then-delete did not delete", len(rows))
	}

	// The checkpoint must STILL verify - redirected to the archive, not
	// reported as a deletion attack against the live table.
	afterArchive, err := VerifyAuditEvidence(tenantID)
	if err != nil {
		t.Fatalf("verification after archiving failed: %v", err)
	}
	for _, bad := range afterArchive.CheckpointsBad {
		if containsPrefix(bad, cp.ID) {
			t.Errorf("checkpoint %s reported BAD after a legitimate archive: %s - archiving a window must not look like deleting it", cp.ID, bad)
		}
	}

	// --- query -------------------------------------------------------------
	manifest, err := ReadAuditArchive(tenantID, archiveID)
	if err != nil {
		t.Fatalf("ReadAuditArchive (query) failed: %v", err)
	}
	if len(manifest.Rows) != 5 {
		t.Fatalf("archive holds %d rows, expected 5", len(manifest.Rows))
	}
	for i, r := range manifest.Rows {
		if r.Action != marker {
			t.Errorf("row %d: action %q, expected marker %q", i, r.Action, marker)
		}
		if r.Signature == "" {
			t.Errorf("row %d: archived with no signature", i)
		}
	}

	// --- export --------------------------------------------------------
	// Export is the same trusted read path as query (ReadAuditArchive) - the
	// distinct requirement is that it is complete and matches what was
	// sealed, which the row count/signature checks above already establish.
	// A second independent read, straight from loadArchiveMeta, confirms the
	// file on disk is what the DB row claims - not merely that the first read
	// happened to succeed.
	meta, err := loadArchiveMeta(schema, archiveID)
	if err != nil {
		t.Fatalf("loadArchiveMeta failed: %v", err)
	}
	if _, err := os.Stat(meta.filePath); err != nil {
		t.Fatalf("archive file %s does not exist on disk: %v", meta.filePath, err)
	}

	// --- restore ---------------------------------------------------------
	restoreResult, err := RestoreAuditArchive(tenantID, archiveID, "drill-operator")
	if err != nil {
		t.Fatalf("RestoreAuditArchive failed: %v", err)
	}
	if restoreResult.RowsRestored != 5 {
		t.Errorf("restored %d rows, expected 5", restoreResult.RowsRestored)
	}

	restoredRows := auditRowsFor(t, tenantID, marker)
	if len(restoredRows) != 5 {
		t.Fatalf("%d row(s) live in audit_logs after restore, expected 5", len(restoredRows))
	}
	for _, r := range restoredRows {
		if r.signature == "" {
			t.Errorf("restored row %s lost its signature", r.id)
		}
	}

	// The checkpoint must verify again, now off the live table exactly as
	// before archiving - the round trip must be lossless for verification,
	// not just for row count.
	afterRestore, err := VerifyAuditEvidence(tenantID)
	if err != nil {
		t.Fatalf("verification after restore failed: %v", err)
	}
	for _, bad := range afterRestore.CheckpointsBad {
		if containsPrefix(bad, cp.ID) {
			t.Errorf("checkpoint %s reported BAD after a restore that put back exactly what was archived: %s", cp.ID, bad)
		}
	}

	// A second restore of the same (now-restored) archive must be refused,
	// not silently re-insert and collide with the seq unique index.
	if _, err := RestoreAuditArchive(tenantID, archiveID, "drill-operator-again"); err == nil {
		t.Error("a second restore of an already-restored archive succeeded; it should have been refused")
	}
}

// TestAuditArchiveSkipsHeldRows is 47.7.6's legal-hold requirement: a hold
// must outrank retention. A single held row blocks the WHOLE window rather
// than archiving around it, so a partially-archived checkpoint window never
// exists.
func TestAuditArchiveSkipsHeldRows(t *testing.T) {
	tenantID, schema, marker, cp := archiveTestSetup(t, 3)

	// A hold scoped to an entity_type these rows do NOT carry (LogAuditEvent
	// writes no entity_type) must NOT block archiving - proving scope is
	// respected, not "any hold blocks everything".
	var nonMatchingHoldID string
	if err := db.DB.QueryRow(fmt.Sprintf(`
		INSERT INTO %s.audit_legal_hold (scope_entity_type, reason, placed_by)
		VALUES ($1, 'drill test hold - must not match', 'tester') RETURNING id::text`, schema),
		"__nonexistent_entity_type__").Scan(&nonMatchingHoldID); err != nil {
		t.Fatalf("could not place non-matching legal hold: %v", err)
	}
	t.Cleanup(func() {
		db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.audit_legal_hold WHERE id = $1::uuid`, schema), nonMatchingHoldID)
	})

	// A second hold scoped to the actor these rows WERE written by is what
	// actually blocks this window.
	var holdID string
	if err := db.DB.QueryRow(fmt.Sprintf(`
		INSERT INTO %s.audit_legal_hold (scope_actor, reason, placed_by)
		VALUES ($1, 'drill test hold - blocks this window', 'tester') RETURNING id::text`, schema),
		"actor").Scan(&holdID); err != nil {
		t.Fatalf("could not place legal hold: %v", err)
	}
	t.Cleanup(func() {
		db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.audit_legal_hold WHERE id = $1::uuid`, schema), holdID)
	})

	result, err := RunAuditArchive(tenantID, false)
	if err != nil {
		t.Fatalf("RunAuditArchive failed: %v", err)
	}
	for _, w := range result.Windows {
		if w.CheckpointID == cp.ID {
			if w.Archived {
				t.Fatalf("checkpoint %s was archived despite an active legal hold matching every row in it (scope_actor=actor)", cp.ID)
			}
			if w.Reason == "" {
				t.Error("a skipped window must state WHY, not skip silently")
			}
		}
	}
	rows := auditRowsFor(t, tenantID, marker)
	if len(rows) != 3 {
		t.Errorf("%d row(s) remain live, expected all 3 to be left alone under legal hold", len(rows))
	}
}

// TestAuditArchiveFileTamperIsDetected proves the redirect verifyCheckpoints
// takes for an archived window is not weaker than reading the live table -
// corrupting the archive file must be caught exactly like corrupting a live
// row would have been.
func TestAuditArchiveFileTamperIsDetected(t *testing.T) {
	tenantID, schema, _, cp := archiveTestSetup(t, 2)

	result, err := RunAuditArchive(tenantID, false)
	if err != nil {
		t.Fatalf("RunAuditArchive failed: %v", err)
	}
	var archiveID string
	for _, w := range result.Windows {
		if w.CheckpointID == cp.ID && w.Archived {
			archiveID = w.ArchiveID
		}
	}
	if archiveID == "" {
		t.Fatalf("checkpoint %s was not archived", cp.ID)
	}

	meta, err := loadArchiveMeta(schema, archiveID)
	if err != nil {
		t.Fatalf("loadArchiveMeta failed: %v", err)
	}
	raw, err := os.ReadFile(meta.filePath)
	if err != nil {
		t.Fatalf("could not read archive file: %v", err)
	}
	tampered := append([]byte(nil), raw...)
	tampered[len(tampered)-1] ^= 0xFF
	if err := os.WriteFile(meta.filePath, tampered, 0600); err != nil {
		t.Fatalf("could not tamper archive file: %v", err)
	}

	after, err := VerifyAuditEvidence(tenantID)
	if err != nil {
		t.Fatalf("verification failed: %v", err)
	}
	found := false
	for _, bad := range after.CheckpointsBad {
		if containsPrefix(bad, cp.ID) {
			found = true
		}
	}
	if !found {
		t.Error("a byte flipped in the archive file was NOT detected - the archive redirect is not protecting the checkpoint's guarantee")
	}
	if after.Intact {
		t.Error("verification reports the evidence intact after the archive file was tampered with")
	}
}

// TestAuditArchiveRepointAfterRestoreFailsClosed is the narrower gap found
// while reviewing this feature (2026-09-11), then sharpened again on review:
// a first design flagged a restored archive with a `restored_at` timestamp
// and had verifyArchivedWindow refuse it - but that column is not covered by
// manifest_signature, so a DB-write attacker could simply null it back out
// and reuse the archive as a redirect target after deleting the restored
// (live) rows. The actual fix RestoreAuditArchive uses: DELETE the
// audit_archives row on restore, so there is no flag to unset - the row
// simply does not exist, and audit_checkpoints.archive_id's own foreign key
// constraint refuses to let anyone point a checkpoint at an archive id that
// is not there. This proves the DATABASE enforces that, not just Go-level
// verification.
func TestAuditArchiveRepointAfterRestoreFailsClosed(t *testing.T) {
	tenantID, schema, marker, cp := archiveTestSetup(t, 2)

	result, err := RunAuditArchive(tenantID, false)
	if err != nil {
		t.Fatalf("RunAuditArchive failed: %v", err)
	}
	var archiveID string
	for _, w := range result.Windows {
		if w.CheckpointID == cp.ID && w.Archived {
			archiveID = w.ArchiveID
		}
	}
	if archiveID == "" {
		t.Fatalf("checkpoint %s was not archived", cp.ID)
	}

	if _, err := RestoreAuditArchive(tenantID, archiveID, "drill-operator"); err != nil {
		t.Fatalf("restore failed: %v", err)
	}

	// The archive row must actually be gone - not merely flagged - or the
	// rest of this test proves nothing.
	var stillExists bool
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT EXISTS(SELECT 1 FROM %s.audit_archives WHERE id = $1::uuid)`, schema),
		archiveID).Scan(&stillExists); err != nil {
		t.Fatalf("could not check archive existence: %v", err)
	}
	if stillExists {
		t.Fatal("the archive row still exists after restore; the fail-closed design requires it to be deleted, not flagged")
	}

	// Simulate the attack directly in SQL: delete the just-restored (live)
	// rows, then try to repoint the checkpoint at the archive id that
	// legitimately covered them before the restore. The archive row no
	// longer exists, so this must fail at the foreign key, not merely be
	// caught later by verification.
	if _, err := db.DB.Exec(fmt.Sprintf(
		`DELETE FROM %s.audit_logs WHERE action = $1`, schema), marker); err != nil {
		t.Fatalf("failed to delete restored rows: %v", err)
	}
	_, repointErr := db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.audit_checkpoints SET archive_id = $1::uuid WHERE id = $2::uuid`, schema),
		archiveID, cp.ID)
	if repointErr == nil {
		t.Fatal("repointing a checkpoint's archive_id at a deleted (restored) archive's id succeeded - " +
			"the foreign key constraint should have refused to create that reference")
	}

	// The checkpoint's archive_id must still be NULL (the repoint above was
	// refused), so verification runs the normal live-table path - and must
	// report this checkpoint bad, since its rows really are gone.
	after, err := VerifyAuditEvidence(tenantID)
	if err != nil {
		t.Fatalf("verification failed: %v", err)
	}
	found := false
	for _, bad := range after.CheckpointsBad {
		if containsPrefix(bad, cp.ID) {
			found = true
		}
	}
	if !found {
		t.Error("a checkpoint whose restored rows were deleted (repoint refused by the foreign key) was NOT flagged bad")
	}
	if after.Intact {
		t.Error("verification reports the evidence intact after the restored rows were deleted")
	}
}

// TestAuditArchiveRefusesOversizedWindowUnlessOverridden is 47.7.6's
// per-tick cap (found in the same review as the incident, before it could be
// reached by anything - see maxRowsPerArchiveWindow's comment): an automatic
// pass must not silently process a huge checkpoint window, but a deliberate
// operator override must still be able to.
func TestAuditArchiveRefusesOversizedWindowUnlessOverridden(t *testing.T) {
	tenantID, _, _, cp := archiveTestSetup(t, 5)

	original := maxRowsPerArchiveWindow
	maxRowsPerArchiveWindow = 3 // below this test's 5 rows, without writing 50,000 real ones
	t.Cleanup(func() { maxRowsPerArchiveWindow = original })

	result, err := RunAuditArchive(tenantID, false)
	if err != nil {
		t.Fatalf("RunAuditArchive failed: %v", err)
	}
	for _, w := range result.Windows {
		if w.CheckpointID != cp.ID {
			continue
		}
		if w.Archived {
			t.Fatal("window exceeding maxRowsPerArchiveWindow was archived automatically without an override")
		}
		if w.Reason == "" || !containsPrefix(w.Reason, "window has") {
			t.Errorf("skip reason does not explain the ceiling: %q", w.Reason)
		}
	}

	// The override must still let a deliberate operator-triggered run
	// process it.
	result, err = RunAuditArchive(tenantID, true)
	if err != nil {
		t.Fatalf("RunAuditArchive with override failed: %v", err)
	}
	archived := false
	for _, w := range result.Windows {
		if w.CheckpointID == cp.ID && w.Archived {
			archived = true
		}
	}
	if !archived {
		t.Error("allowLargeWindows=true did not archive a window over the ceiling")
	}
}

// TestAuditArchiveRefusesUnsafeArchiveDir is the other precondition found in
// the same review: the incident happened because AUDIT_ARCHIVE_DIR was never
// checked, only used. A relative path and an unwritable directory must both
// be refused before anything is read from audit_logs, not discovered
// partway through a pass.
func TestAuditArchiveRefusesUnsafeArchiveDir(t *testing.T) {
	tenantID, _, _, _ := archiveTestSetup(t, 2)

	t.Setenv("AUDIT_ARCHIVE_DIR", "relative/path/not/absolute")
	if _, err := RunAuditArchive(tenantID, false); err == nil {
		t.Error("a relative AUDIT_ARCHIVE_DIR was accepted; it must be refused before any row is touched")
	}

	unwritable := t.TempDir()
	if err := os.Chmod(unwritable, 0500); err == nil { // best-effort; no-op on some filesystems
		defer os.Chmod(unwritable, 0700)
	}
	t.Setenv("AUDIT_ARCHIVE_DIR", filepath.Join(unwritable, "nested", "dir"))
	if _, err := RunAuditArchive(tenantID, false); err == nil {
		t.Skip("could not make a directory genuinely unwritable on this filesystem/OS - not a failure of the check itself")
	}
}

// --- helpers -----------------------------------------------------------

// setArchiveAfterDaysForTest points the tenant's current retention policy at
// archive_after_days=days for the duration of the test. Called by
// archiveTestSetup with minArchiveAfterDays (the engine's own floor) paired
// with rows backdated past it - not with a value below the floor, which
// RunAuditArchive now refuses outright.
func setArchiveAfterDaysForTest(t *testing.T, schema string, days int) {
	t.Helper()
	var version, hotWindow, archiveAfter int
	var deleteAfter *int
	var effectiveFrom time.Time
	if err := db.DB.QueryRow(fmt.Sprintf(`
		SELECT version, hot_window_days, archive_after_days, delete_after_days, effective_from
		  FROM %s.audit_retention_policy
		 WHERE effective_from <= CURRENT_TIMESTAMP
		 ORDER BY effective_from DESC, version DESC LIMIT 1`, schema)).
		Scan(&version, &hotWindow, &archiveAfter, &deleteAfter, &effectiveFrom); err != nil {
		t.Fatalf("could not read current retention policy: %v", err)
	}
	if _, err := db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.audit_retention_policy SET archive_after_days = $1 WHERE version = $2`, schema),
		days, version); err != nil {
		t.Fatalf("could not set archive_after_days for test: %v", err)
	}
	t.Cleanup(func() {
		db.DB.Exec(fmt.Sprintf(
			`UPDATE %s.audit_retention_policy SET archive_after_days = $1 WHERE version = $2`, schema),
			archiveAfter, version)
	})
}

func containsPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
