package engines

import (
	"custom_erp/db"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// Stage 47.7.8 - "Test concurrent writers, direct tamper, skipped row,
// restore, partition/archive boundary, legal hold, key rotation, verifier
// outage and evidence export at representative scale." (A-07/A-30)
//
// The case that drove the whole model choice is the concurrency one below: the
// previous chain design produced sibling rows under normal load that the
// verifier reported as tampering, on perfectly clean data. A verifier that
// cries wolf under normal load is worse than no verifier, because it trains an
// operator to ignore it.

func auditTestTenant(t *testing.T) string {
	t.Helper()
	db.InitDB(testConnStr())
	return "default"
}

// TestAuditConcurrentWritersDoNotBreakVerification is the reason independent
// signatures were chosen over a serialized chain.
func TestAuditConcurrentWritersDoNotBreakVerification(t *testing.T) {
	tenantID := auditTestTenant(t)
	marker := "AUDITCONC-" + NewDocIDCompact("T")
	t.Cleanup(func() { cleanupAuditMarker(t, tenantID, marker) })

	const writers = 24
	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			LogAuditEvent(tenantID, fmt.Sprintf("user-%d", i), marker, "Success",
				fmt.Sprintf("concurrent write %d", i))
		}(i)
	}
	wg.Wait()

	rows := auditRowsFor(t, tenantID, marker)
	if len(rows) != writers {
		t.Fatalf("%d of %d concurrent audit writes landed", len(rows), writers)
	}
	for _, r := range rows {
		if r.signature == "" {
			t.Fatalf("row %s was written with no signature", r.id)
		}
	}

	// Every one of them must verify. Under the old chain design, rows written
	// concurrently chained from the same parent and the verifier called them
	// broken - clean data reported as tampering.
	result, err := VerifyAuditEvidence(tenantID)
	if err != nil {
		t.Fatalf("verification failed: %v", err)
	}
	for _, r := range rows {
		if containsID(result.TamperedRows, r.id) {
			t.Errorf("row %s was written cleanly by a concurrent writer but is reported as tampered - "+
				"exactly the sibling-row false positive the independent-signature model exists to remove", r.id)
		}
	}
}

// TestAuditDirectTamperIsDetected covers the administrator threat 47.7.1 names
// explicitly: someone with SQL access editing a row in place.
func TestAuditDirectTamperIsDetected(t *testing.T) {
	tenantID := auditTestTenant(t)
	schema, _ := db.GetTenantSchema(tenantID)
	marker := "AUDITTAMPER-" + NewDocIDCompact("T")
	t.Cleanup(func() { cleanupAuditMarker(t, tenantID, marker) })

	LogAuditEvent(tenantID, "victim", marker, "Success", "original details")
	rows := auditRowsFor(t, tenantID, marker)
	if len(rows) != 1 {
		t.Fatalf("expected 1 seeded row, got %d", len(rows))
	}

	before, err := VerifyAuditEvidence(tenantID)
	if err != nil {
		t.Fatalf("baseline verification failed: %v", err)
	}
	if containsID(before.TamperedRows, rows[0].id) {
		t.Fatalf("the row was reported tampered before anything touched it")
	}

	if _, err := db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.audit_logs SET details = 'quietly altered' WHERE id = $1::uuid`, schema), rows[0].id); err != nil {
		t.Fatalf("failed to tamper: %v", err)
	}

	after, err := VerifyAuditEvidence(tenantID)
	if err != nil {
		t.Fatalf("verification failed: %v", err)
	}
	if !containsID(after.TamperedRows, rows[0].id) {
		t.Error("a row edited directly in the database was NOT detected; the signature is not protecting the content")
	}
	if after.Intact {
		t.Error("verification reports the evidence intact after a direct edit")
	}
}

// TestAuditDeletedRowIsDetectedByCheckpoint is the case a per-row signature
// cannot catch on its own - remove a row and every survivor still verifies.
// This is the entire reason checkpoints exist alongside signatures.
func TestAuditDeletedRowIsDetectedByCheckpoint(t *testing.T) {
	tenantID := auditTestTenant(t)
	schema, _ := db.GetTenantSchema(tenantID)
	marker := "AUDITDEL-" + NewDocIDCompact("T")
	t.Cleanup(func() { cleanupAuditMarker(t, tenantID, marker) })

	for i := 0; i < 3; i++ {
		LogAuditEvent(tenantID, "actor", marker, "Success", fmt.Sprintf("row %d", i))
	}

	cp, err := WriteAuditCheckpoint(tenantID, "Periodic")
	if err != nil {
		t.Fatalf("checkpoint failed: %v", err)
	}
	if cp == nil {
		t.Fatal("no checkpoint was written despite new rows")
	}
	t.Cleanup(func() {
		db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.audit_checkpoints WHERE id = $1::uuid`, schema), cp.ID)
	})

	beforeV, err := VerifyAuditEvidence(tenantID)
	if err != nil {
		t.Fatalf("verification failed: %v", err)
	}
	if len(beforeV.CheckpointsBad) > 0 {
		t.Fatalf("the checkpoint failed verification immediately after being written: %v", beforeV.CheckpointsBad)
	}

	// Delete a row from inside the sealed window. Every remaining row still
	// carries a valid signature - only the checkpoint can notice.
	rows := auditRowsFor(t, tenantID, marker)
	if _, err := db.DB.Exec(fmt.Sprintf(
		`DELETE FROM %s.audit_logs WHERE id = $1::uuid`, schema), rows[0].id); err != nil {
		t.Fatalf("failed to delete: %v", err)
	}

	after, err := VerifyAuditEvidence(tenantID)
	if err != nil {
		t.Fatalf("verification failed: %v", err)
	}
	if len(after.CheckpointsBad) == 0 {
		t.Error("a row deleted from inside a sealed window was NOT detected - the checkpoint is not covering deletion, " +
			"which is the one failure mode per-row signatures cannot catch")
	}
	if after.Intact {
		t.Error("verification reports the evidence intact after a row was deleted from a sealed window")
	}
}

// TestAuditLegacyRowsAreSealedNotFabricated is 47.7.4, and the requirement is
// a NEGATIVE one: the system must not invent evidence for rows that never had
// any. So this asserts the absence of a false claim.
func TestAuditLegacyRowsAreSealedNotFabricated(t *testing.T) {
	tenantID := auditTestTenant(t)
	result, err := VerifyAuditEvidence(tenantID)
	if err != nil {
		t.Fatalf("verification failed: %v", err)
	}
	if result.UnsignedLegacy == 0 {
		t.Skip("this database has no legacy unsigned rows to reason about")
	}
	if result.Verified > result.SignedRows {
		t.Errorf("verified (%d) exceeds signed (%d) - unsigned legacy rows are being counted as verified, which is fabricated coverage",
			result.Verified, result.SignedRows)
	}
	if result.CoverageStatement == "" {
		t.Fatal("no coverage statement; the limitation has to be stated in words, not merely implied by two numbers")
	}
	for _, must := range []string{"predate", "NOT counted as verified"} {
		if !strings.Contains(result.CoverageStatement, must) {
			t.Errorf("the coverage statement does not say %q - an auditor reading it could mistake the gap for coverage: %s",
				must, result.CoverageStatement)
		}
	}
}

// TestAuditKeyRotationIsNotMistakenForTampering: a row signed under a retired
// scheme is out of coverage, not evidence of an attack. Confusing the two is
// how a routine key rotation triggers a security incident.
func TestAuditKeyRotationIsNotMistakenForTampering(t *testing.T) {
	tenantID := auditTestTenant(t)
	schema, _ := db.GetTenantSchema(tenantID)
	marker := "AUDITROT-" + NewDocIDCompact("T")
	t.Cleanup(func() { cleanupAuditMarker(t, tenantID, marker) })

	LogAuditEvent(tenantID, "actor", marker, "Success", "signed under the current scheme")
	rows := auditRowsFor(t, tenantID, marker)
	if len(rows) != 1 {
		t.Fatalf("expected 1 seeded row, got %d", len(rows))
	}

	if _, err := db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.audit_logs SET sig_version = 'v0-retired' WHERE id = $1::uuid`, schema), rows[0].id); err != nil {
		t.Fatalf("failed to restamp: %v", err)
	}

	result, err := VerifyAuditEvidence(tenantID)
	if err != nil {
		t.Fatalf("verification failed: %v", err)
	}
	if containsID(result.TamperedRows, rows[0].id) {
		t.Error("a row signed under a retired scheme is reported as TAMPERED; a key rotation must not look like an attack")
	}
}

// TestAuditVerificationOverdueDetectsSilence is 47.7.7's subtle half: alert on
// verification that did not HAPPEN, not only on verification that failed.
// Absence of a failure is not evidence of success.
func TestAuditVerificationOverdueDetectsSilence(t *testing.T) {
	tenantID := auditTestTenant(t)

	// A zero tolerance makes any existing checkpoint overdue, which proves the
	// age is genuinely measured rather than hardcoded to "fine".
	overdue, since, err := AuditVerificationOverdue(tenantID, 0)
	if err != nil {
		t.Fatalf("overdue check failed: %v", err)
	}
	if !overdue {
		t.Errorf("nothing is reported overdue at a zero tolerance (last seal %v ago); the silence check is not measuring anything", since)
	}

	fresh, err := WriteAuditCheckpoint(tenantID, "Periodic")
	if err != nil {
		t.Fatalf("checkpoint failed: %v", err)
	}
	if fresh != nil {
		// Removed again on the way out. A checkpoint seals a seq window, so
		// leaving one behind means any LATER test whose cleanup deletes audit
		// rows inside that window breaks checkpoint verification for every
		// subsequent run - a cross-test hazard on this shared database (see
		// CLAUDE.md), not a product defect.
		schema, _ := db.GetTenantSchema(tenantID)
		t.Cleanup(func() {
			db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.audit_checkpoints WHERE id = $1::uuid`, schema), fresh.ID)
		})
	}
	overdue, _, err = AuditVerificationOverdue(tenantID, 24*time.Hour)
	if err != nil {
		t.Fatalf("overdue check failed: %v", err)
	}
	if overdue {
		t.Error("a freshly checkpointed tenant is reported overdue within a 24h tolerance")
	}
}

// TestAuditEvidenceWritesInsideBusinessTransaction is 47.7.3: evidence and the
// mutation it describes commit together, or neither does.
func TestAuditEvidenceWritesInsideBusinessTransaction(t *testing.T) {
	tenantID := auditTestTenant(t)
	schema, _ := db.GetTenantSchema(tenantID)
	marker := "AUDITTX-" + NewDocIDCompact("T")
	t.Cleanup(func() { cleanupAuditMarker(t, tenantID, marker) })

	// A transaction that rolls back must leave NO evidence behind - otherwise
	// the audit trail records mutations that never happened.
	tx, err := db.DB.Begin()
	if err != nil {
		t.Fatalf("begin failed: %v", err)
	}
	if err := WriteAuditEvidenceTx(tx, schema, tenantID, AuditEvidence{
		UserID: "actor", Action: marker, Status: "Success",
		Details:    "this transaction is going to roll back",
		EntityType: "POSCart", EntityID: "CART-ROLLBACK",
	}); err != nil {
		t.Fatalf("evidence write failed: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("rollback failed: %v", err)
	}
	if rows := auditRowsFor(t, tenantID, marker); len(rows) != 0 {
		t.Errorf("a rolled-back transaction left %d audit row(s) behind; evidence must not outlive the mutation it describes", len(rows))
	}

	// ...and a committed one must leave exactly one, signed and verifying.
	tx, err = db.DB.Begin()
	if err != nil {
		t.Fatalf("begin failed: %v", err)
	}
	if err := WriteAuditEvidenceTx(tx, schema, tenantID, AuditEvidence{
		UserID: "actor", Action: marker, Status: "Success", Details: "committed",
		EntityType: "POSCart", EntityID: "CART-COMMIT", CorrelationID: "corr-123",
	}); err != nil {
		t.Fatalf("evidence write failed: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit failed: %v", err)
	}
	rows := auditRowsFor(t, tenantID, marker)
	if len(rows) != 1 {
		t.Fatalf("expected exactly 1 committed audit row, got %d", len(rows))
	}
	if rows[0].signature == "" {
		t.Error("evidence written inside a business transaction carries no signature")
	}
	result, err := VerifyAuditEvidence(tenantID)
	if err != nil {
		t.Fatalf("verification failed: %v", err)
	}
	if containsID(result.TamperedRows, rows[0].id) {
		t.Error("evidence written inside a business transaction does not verify")
	}
}

// --- helpers ---------------------------------------------------------------

type auditRow struct{ id, signature string }

func auditRowsFor(t *testing.T, tenantID, action string) []auditRow {
	t.Helper()
	schema, _ := db.GetTenantSchema(tenantID)
	rows, err := db.DB.Query(fmt.Sprintf(
		`SELECT id::text, COALESCE(signature,'') FROM %s.audit_logs WHERE action = $1 ORDER BY seq ASC`, schema), action)
	if err != nil {
		t.Fatalf("failed to read audit rows: %v", err)
	}
	defer rows.Close()
	var out []auditRow
	for rows.Next() {
		var r auditRow
		if err := rows.Scan(&r.id, &r.signature); err != nil {
			t.Fatalf("scan failed: %v", err)
		}
		out = append(out, r)
	}
	return out
}

func cleanupAuditMarker(t *testing.T, tenantID, action string) {
	schema, _ := db.GetTenantSchema(tenantID)
	db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.audit_logs WHERE action = $1`, schema), action)
}

func containsID(list []string, id string) bool {
	for _, v := range list {
		if v == id {
			return true
		}
	}
	return false
}
