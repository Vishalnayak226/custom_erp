package engines

import (
	"context"
	"fmt"
	"testing"
	"time"

	"custom_erp/db"
)

// Stage 49.3.6: a role change is previewed, recorded with its before/after
// evidence, ends the sessions a high-risk reduction must not leave open, and
// can be rolled back without restoring obsolete access.
func TestStage4936RoleGrantChangeEvidenceSessionsAndRollback(t *testing.T) {
	db.InitDB(testConnStr())
	ctx := context.Background()
	role := fmt.Sprintf("QA Priv %d", time.Now().UnixNano()%1_000_000_000)
	users := []string{"__qa4936a_" + role[8:] + "__", "__qa4936b_" + role[8:] + "__"}
	cleanup := func() {
		db.DB.Exec(`DELETE FROM tenant_default.role_permissions WHERE role = $1`, role)
		db.DB.Exec(`DELETE FROM tenant_default.role_grant_changes WHERE role = $1`, role)
		for _, u := range users {
			db.DB.Exec(`DELETE FROM tenant_default.users WHERE id = $1`, u)
		}
	}
	cleanup()
	t.Cleanup(cleanup)
	for _, u := range users {
		if _, err := db.DB.Exec(`INSERT INTO tenant_default.users (id, username, password_hash, email, role, status, location_code)
			VALUES ($1, $1, 'x', $3, $2, 'Active', 'HO')`, u, role, u+"@qa.invalid"); err != nil {
			t.Fatalf("seed user: %v", err)
		}
	}
	credVersion := func(u string) int {
		var v int
		if err := db.DB.QueryRow(`SELECT credential_version FROM tenant_default.users WHERE id = $1`, u).Scan(&v); err != nil {
			t.Fatalf("credential_version: %v", err)
		}
		return v
	}
	grants := func() map[string]string {
		rows, err := db.DB.Query(`SELECT doctype_name, allow_read, allow_create, allow_update, allow_delete FROM tenant_default.role_permissions WHERE role = $1`, role)
		if err != nil {
			t.Fatalf("grants: %v", err)
		}
		defer rows.Close()
		out := map[string]string{}
		for rows.Next() {
			var r storedRolePermissionRow
			if err := rows.Scan(&r.Doctype, &r.Read, &r.Create, &r.Update, &r.Delete); err != nil {
				t.Fatal(err)
			}
			out[r.Doctype] = rowRights(r)
		}
		return out
	}

	first := RoleGrantChangeRequest{Role: role, Actor: "qa-admin", Reason: "month-end cover", Grants: []RoleGrantInput{
		{Doctype: "Item", Read: true, Create: true},
		{Doctype: "Payslip", Read: true}, // self-scoped for a custom role: not dangerous
		{Doctype: "JournalVoucher", Read: true, Create: true, Update: true},
	}}

	// Preview answers and writes nothing.
	preview, err := PreviewRoleGrantChange("default", first)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if len(grants()) != 0 {
		t.Fatal("a preview wrote grants")
	}
	if !preview.HighRisk || len(preview.DangerousGained) != 1 || preview.DangerousGained[0].Doctype != "JournalVoucher" ||
		preview.DangerousGained[0].Category != "finance posting" {
		t.Fatalf("preview did not flag the journal grant alone: %+v", preview.DangerousGained)
	}
	if preview.UsersWithRole != 2 || preview.SessionsWillEnd || len(preview.Widened) != 3 {
		t.Fatalf("preview users=%d sessionsWillEnd=%v widened=%v", preview.UsersWithRole, preview.SessionsWillEnd, preview.Widened)
	}

	// Saved: evidence, no sessions ended (nothing was taken away).
	r1, err := ChangeRoleGrants(ctx, "default", first)
	if err != nil {
		t.Fatalf("first change: %v", err)
	}
	if r1.ChangeID == "" || r1.SessionsEnded != 0 || credVersion(users[0]) != 1 {
		t.Fatalf("first change: id=%q sessionsEnded=%d", r1.ChangeID, r1.SessionsEnded)
	}
	if err := VerifyRoleGrantChangeEvidence("default", r1.ChangeID); err != nil {
		t.Fatalf("fresh evidence does not verify: %v", err)
	}
	// Saving the same thing again changes nothing and records nothing.
	if again, err := ChangeRoleGrants(ctx, "default", first); err != nil || again.ChangeID != "" {
		t.Fatalf("a no-op save recorded a change: %+v %v", again, err)
	}

	// Taking the journal grant away is high-risk: every holder is signed out.
	second := RoleGrantChangeRequest{Role: role, Actor: "qa-admin", Reason: "cover over", Grants: []RoleGrantInput{
		{Doctype: "JournalVoucher", Read: true},
	}}
	r2, err := ChangeRoleGrants(ctx, "default", second)
	if err != nil {
		t.Fatalf("second change: %v", err)
	}
	if r2.SessionsEnded != 2 || credVersion(users[0]) != 2 || credVersion(users[1]) != 2 {
		t.Fatalf("a high-risk reduction ended %d sessions, credential versions %d/%d", r2.SessionsEnded, credVersion(users[0]), credVersion(users[1]))
	}
	if len(r2.Preview.DangerousRemoved) != 1 || r2.Preview.DangerousRemoved[0].Doctype != "JournalVoucher" {
		t.Fatalf("second change did not record the dangerous removal: %+v", r2.Preview.DangerousRemoved)
	}
	var audited int
	db.DB.QueryRow(`SELECT count(*) FROM tenant_default.audit_logs WHERE action = 'ROLE_PERMISSIONS'
		AND details LIKE $1`, "%LOSES JournalVoucher (finance posting)%Reason: cover over. Change "+r2.ChangeID+"%").Scan(&audited)
	if audited != 1 {
		t.Fatalf("expected one audit line naming the dangerous removal and the reason, found %d", audited)
	}

	// Rolling the reduction back would hand the journal grant back: withheld.
	if _, _, err := RollbackRoleGrantChange(ctx, "default", r2.ChangeID, "qa-admin", "", "x"); err == nil {
		t.Fatal("a rollback without a reason was accepted")
	}
	_, outcome, err := RollbackRoleGrantChange(ctx, "default", r2.ChangeID, "qa-admin", "", "undo the cover end")
	if err == nil {
		t.Fatal("a rollback that could only restore a dangerous grant went through")
	}
	if len(outcome.Withheld) != 1 || outcome.Withheld[0].Doctype != "JournalVoucher" {
		t.Fatalf("withheld = %+v, want JournalVoucher", outcome.Withheld)
	}
	if grants()["JournalVoucher"] != "read" {
		t.Fatalf("the journal grant moved: %v", grants())
	}

	// Rolling the first change back: Item and Payslip go back to nothing;
	// JournalVoucher was changed again since, so the later decision stands.
	r3, outcome, err := RollbackRoleGrantChange(ctx, "default", r1.ChangeID, "qa-admin", "", "granted to the wrong role")
	if err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if len(outcome.Superseded) != 1 || outcome.Superseded[0] != "JournalVoucher" || len(outcome.Restored) != 2 {
		t.Fatalf("rollback outcome %+v", outcome)
	}
	got := grants()
	if _, ok := got["Item"]; ok || got["JournalVoucher"] != "read" || len(got) != 1 {
		t.Fatalf("after rollback the role holds %v, want only JournalVoucher read", got)
	}
	var rollbackOf, rolledBackBy string
	db.DB.QueryRow(`SELECT COALESCE(rollback_of,'') FROM tenant_default.role_grant_changes WHERE id = $1`, r3.ChangeID).Scan(&rollbackOf)
	db.DB.QueryRow(`SELECT COALESCE(rolled_back_by,'') FROM tenant_default.role_grant_changes WHERE id = $1`, r1.ChangeID).Scan(&rolledBackBy)
	if rollbackOf != r1.ChangeID || rolledBackBy != r3.ChangeID {
		t.Fatalf("rollback not linked: rollback_of=%q rolled_back_by=%q", rollbackOf, rolledBackBy)
	}
	if _, _, err := RollbackRoleGrantChange(ctx, "default", r1.ChangeID, "qa-admin", "", "second time round"); err == nil {
		t.Fatal("a change was rolled back twice")
	}
	if _, _, err := RollbackRoleGrantChange(ctx, "default", r3.ChangeID, "qa-admin", "", "undo the undo please"); err == nil {
		t.Fatal("a rollback was itself rolled back")
	}
	if err := VerifyRoleGrantChangeEvidence("default", r3.ChangeID); err != nil {
		t.Fatalf("rollback evidence does not verify: %v", err)
	}

	// Evidence edited after the fact - even by a database superuser - no
	// longer matches the digest in the signed audit chain.
	if _, err := db.DB.Exec(`UPDATE tenant_default.role_grant_changes SET before_state = '[]' WHERE id = $1`, r2.ChangeID); err != nil {
		t.Fatal(err)
	}
	if err := VerifyRoleGrantChangeEvidence("default", r2.ChangeID); err == nil {
		t.Fatal("tampered evidence still verified")
	}
}

// The rollback planner on its own: superseded, withheld and gone, without a
// database.
func TestStage4936RollbackNeverRestoresObsoleteAccess(t *testing.T) {
	row := func(doctype string, read, create, update, del bool) storedRolePermissionRow {
		return storedRolePermissionRow{Role: "Night Lead", Doctype: doctype, Read: read, Create: create, Update: update, Delete: del}
	}
	previous := []storedRolePermissionRow{
		row("Item", true, true, true, false),
		row("PaymentProposal", true, true, true, false), // dangerous at operate
		row("Retired", true, false, false, false),
		row("Customer", true, false, false, false),
	}
	applied := []storedRolePermissionRow{
		row("Item", true, false, false, false),
		row("PaymentProposal", true, false, false, false),
		row("Retired", false, false, false, false),
		row("Customer", true, true, true, false),
		row("Lead", true, false, false, false), // created by the change
	}
	current := map[string]storedRolePermissionRow{}
	for _, r := range applied {
		current[grantKey(r.Role, r.Doctype)] = r
	}
	current[grantKey("Night Lead", "Customer")] = row("Customer", true, false, true, false) // changed again since
	exists := map[string]bool{"Item": true, "PaymentProposal": true, "Customer": true, "Lead": true}

	writes, deletes, out := planGrantRollback(current, previous, applied, exists)
	restored := map[string]bool{}
	for _, w := range writes {
		restored[w.Doctype] = true
	}
	if !restored["Item"] || !restored["Lead"] || len(writes) != 2 {
		t.Fatalf("writes = %+v", writes)
	}
	if !deletes[grantKey("Night Lead", "Lead")] || deletes[grantKey("Night Lead", "Item")] {
		t.Fatalf("deletes = %v: the row the change created goes, Item is rewritten", deletes)
	}
	if len(out.Withheld) != 1 || out.Withheld[0].Doctype != "PaymentProposal" {
		t.Fatalf("withheld = %+v", out.Withheld)
	}
	if len(out.Superseded) != 1 || out.Superseded[0] != "Customer" {
		t.Fatalf("superseded = %v", out.Superseded)
	}
	if len(out.Gone) != 1 || out.Gone[0] != "Retired" {
		t.Fatalf("gone = %v", out.Gone)
	}
}
