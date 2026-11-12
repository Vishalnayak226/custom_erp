package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// Stage 49.3.5 - segregation of duties. When a client's approvals.self_approval
// says the submitter may not decide (admins / nobody), "the submitter" is
// everyone who made the document's current approval round: who created it,
// who last changed it, and who submitted it or changed it after approval. The
// default "anyone" (Stage 92) is untouched. A save, a bulk edit or an import
// can never write the decided statuses (Approved / Rejected) of an
// approval-gated record type: only DecideApproval does.

// sodTenant provisions a scratch client with an approval rule on
// ProductContent and three users, so the self-approval setting this test
// changes is never the shared default client's.
func sodTenant(t *testing.T) (tenantID, schema string) {
	t.Helper()
	db.InitDB(testConnStr())
	tenantID, schema = uniqueLifecycleTenant(t)
	t.Cleanup(func() { dropLifecycleTenant(tenantID, schema) })
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	for _, u := range []string{"sod_maker", "sod_editor", "sod_checker"} {
		if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.users (id, username, password_hash, role, status, location_code)
			VALUES ($1, $1, 'x', $2, 'Active', 'HO') ON CONFLICT (id) DO NOTHING`, schema), u, RoleSuperAdmin); err != nil {
			t.Fatalf("seed user %s: %v", u, err)
		}
	}
	if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.approval_rules (doctype, min_amount, required_role)
		VALUES ('ProductContent', 0, 'HR/Admin') ON CONFLICT (doctype, min_amount) DO NOTHING`, schema)); err != nil {
		t.Fatalf("approval rule: %v", err)
	}
	return tenantID, schema
}

// sodDraft creates a Draft ProductContent made by createdBy and returns its id.
func sodDraft(t *testing.T, schema, createdBy string) string {
	t.Helper()
	id := fmt.Sprintf("SOD-%d", time.Now().UnixNano())
	data, _ := json.Marshal(map[string]interface{}{"code": id, "product_id": "SOD-ITEM", "language": "en", "title": "SoD fixture", "status": "Draft"})
	if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'ProductContent', $2, 'Draft', $3)`, schema), id, data, createdBy); err != nil {
		t.Fatalf("seed draft: %v", err)
	}
	return id
}

func sodCode(err error) string {
	if verr, ok := err.(*ValidationError); ok {
		return verr.Code
	}
	return ""
}

func TestStage4935SubmitterMayNotDecideAnyRoundTheyMade(t *testing.T) {
	tenantID, schema := sodTenant(t)
	withSelfApproval(t, tenantID, SelfApprovalNobody)

	// The creator - Stage 92's case, unchanged.
	own := sodDraft(t, schema, "sod_maker")
	if err := SubmitForApproval(tenantID, "ProductContent", own, "sod_maker", RoleSuperAdmin); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if code := sodCode(DecideApproval(tenantID, "ProductContent", own, "sod_maker", RoleSuperAdmin, "HO", "Rejected", "own")); code != "APPROV-0840" {
		t.Fatalf("the creator decided their own document: %q, want APPROV-0840", code)
	}

	// Someone else's draft, sent for approval by the person who decides it:
	// before 49.3.5 only created_by was compared, so this went through.
	submitted := sodDraft(t, schema, "sod_maker")
	if err := SubmitForApproval(tenantID, "ProductContent", submitted, "sod_editor", RoleSuperAdmin); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if code := sodCode(DecideApproval(tenantID, "ProductContent", submitted, "sod_editor", RoleSuperAdmin, "HO", "Approved", "")); code != "APPROV-0840" {
		t.Fatalf("the submitter approved a document they sent for approval: %q, want APPROV-0840", code)
	}
	inbox, err := ListTaskInbox(tenantID, TaskInboxScope{UserID: "sod_editor", Role: RoleSuperAdmin})
	if err != nil {
		t.Fatalf("ListTaskInbox: %v", err)
	}
	for _, item := range inbox.Items {
		if item.Source == "Approval" && item.DocID == submitted {
			t.Fatal("My work lists a document to the person who submitted it, who may not decide it")
		}
	}

	// The bulk path runs the same check.
	_, failed, err := BulkDecideApproval(tenantID, "ProductContent", []string{submitted}, "sod_editor", RoleSuperAdmin, "HO", "Approved", "bulk")
	if err != nil {
		t.Fatalf("BulkDecideApproval: %v", err)
	}
	if !strings.Contains(failed[submitted], "maker-checker") {
		t.Fatalf("bulk approval by the submitter: failed=%v, want a maker-checker refusal", failed)
	}

	// An approved document changed afterwards goes back for approval
	// (ResetToPendingOnEdit); the person who changed it may not approve the
	// change.
	edited := sodDraft(t, schema, "sod_maker")
	if err := SubmitForApproval(tenantID, "ProductContent", edited, "sod_maker", RoleSuperAdmin); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if err := DecideApproval(tenantID, "ProductContent", edited, "sod_checker", RoleSuperAdmin, "HO", "Approved", ""); err != nil {
		t.Fatalf("checker approval: %v", err)
	}
	if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || '{"title":"changed after approval"}', updated_by = 'sod_editor' WHERE id = $1`, schema), edited); err != nil {
		t.Fatalf("edit: %v", err)
	}
	if err := ResetToPendingOnEdit(tenantID, "ProductContent", edited, "sod_editor", RoleSuperAdmin, map[string]interface{}{"code": edited, "title": "changed after approval"}); err != nil {
		t.Fatalf("ResetToPendingOnEdit: %v", err)
	}
	if code := sodCode(DecideApproval(tenantID, "ProductContent", edited, "sod_editor", RoleSuperAdmin, "HO", "Approved", "")); code != "APPROV-0840" {
		t.Fatalf("the editor approved their own change: %q, want APPROV-0840", code)
	}
	// The checker of the earlier round, who has not touched it since, still may.
	if err := DecideApproval(tenantID, "ProductContent", edited, "sod_checker", RoleSuperAdmin, "HO", "Rejected", "please explain the change"); err != nil {
		t.Fatalf("an independent checker was refused: %v", err)
	}
	// A checker who did nothing to the submitted one decides it.
	if err := DecideApproval(tenantID, "ProductContent", submitted, "sod_checker", RoleSuperAdmin, "HO", "Rejected", "independent"); err != nil {
		t.Fatalf("an independent checker was refused: %v", err)
	}
}

// Under the default "anyone" the submitter still decides, exactly as Stage 92
// left it: allowed, and marked only when they created it.
func TestStage4935AnyoneStillLetsTheSubmitterDecide(t *testing.T) {
	tenantID, schema := sodTenant(t)
	withSelfApproval(t, tenantID, SelfApprovalAnyone)

	id := sodDraft(t, schema, "sod_maker")
	if err := SubmitForApproval(tenantID, "ProductContent", id, "sod_editor", RoleSuperAdmin); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if err := DecideApproval(tenantID, "ProductContent", id, "sod_editor", RoleSuperAdmin, "HO", "Rejected", "not yet"); err != nil {
		t.Fatalf("anyone: the submitter was refused: %v", err)
	}
	var comment string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(comment, '') FROM %s.approval_log WHERE document_id = $1 AND action = 'Rejected'`, schema), id).Scan(&comment); err != nil {
		t.Fatalf("approval_log: %v", err)
	}
	if comment != "not yet" {
		t.Fatalf("comment = %q, want it unchanged under anyone", comment)
	}

	// admins: an administrator who submitted still decides.
	withSelfApproval(t, tenantID, SelfApprovalAdmins)
	other := sodDraft(t, schema, "sod_maker")
	if err := SubmitForApproval(tenantID, "ProductContent", other, "sod_editor", RoleSuperAdmin); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if err := DecideApproval(tenantID, "ProductContent", other, "sod_editor", RoleSuperAdmin, "HO", "Rejected", "admin"); err != nil {
		t.Fatalf("admins: an administrator who submitted was refused: %v", err)
	}
}

// A bulk edit of an approval-gated record type cannot set a decided status:
// before 49.3.5, field=status value=Approved approved PIM content with no
// checker at all.
func TestStage4935BulkEditCannotWriteADecidedStatus(t *testing.T) {
	db.InitDB(testConnStr())
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatal(err)
	}
	gated, err := IsApprovalGated("default", "ProductContent")
	if err != nil || !gated {
		t.Skipf("ProductContent is not approval-gated here (%v)", err)
	}
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	itemID, contentID := "SOD-BULK-ITEM-"+suffix, "SOD-BULK-ITEM-"+suffix+"::en"
	defer db.DB.Exec("DELETE FROM "+schema+".documents WHERE id IN ($1, $2)", itemID, contentID)
	item, _ := json.Marshal(map[string]interface{}{"code": itemID, "name": "SoD bulk fixture"})
	content, _ := json.Marshal(map[string]interface{}{"code": contentID, "product_id": itemID, "language": "en", "title": "Draft", "status": "Draft"})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Item', $2, 'Active', 'system'), ($3, 'ProductContent', $4, 'Draft', 'system')", itemID, item, contentID, content); err != nil {
		t.Fatalf("seed: %v", err)
	}
	for _, status := range []string{"Approved", "Rejected"} {
		_, err := BulkUpdateDocuments("default", "ProductContent", []string{contentID}, "status", status, "system", "Store Manager")
		if sodCode(err) != "GLOBAL-0019" {
			t.Fatalf("bulk edit to %s: %v, want GLOBAL-0019", status, err)
		}
	}
	var stored string
	db.DB.QueryRow("SELECT status FROM "+schema+".documents WHERE id = $1", contentID).Scan(&stored)
	if stored != "Draft" {
		t.Fatalf("status after the refused bulk edits = %q, want Draft", stored)
	}
	// Any other field still bulk-edits.
	if _, err := BulkUpdateDocuments("default", "ProductContent", []string{contentID}, "title", "Bulk title", "system", "HR/Admin"); err != nil {
		t.Fatalf("an ordinary bulk edit: %v", err)
	}
}

// An import cannot plant a decided status, nor change a record that is
// waiting for or past its approval.
func TestStage4935ImportCannotEvadeApproval(t *testing.T) {
	db.InitDB(testConnStr())
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatal(err)
	}
	if gated, err := IsApprovalGated("default", "ProductContent"); err != nil || !gated {
		t.Skipf("ProductContent is not approval-gated here (%v)", err)
	}
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	itemID := "SOD-IMP-ITEM-" + suffix
	planted, approved, draft := itemID+"::en", itemID+"::hi", itemID+"::ta"
	defer db.DB.Exec("DELETE FROM "+schema+".documents WHERE id IN ($1, $2, $3, $4)", itemID, planted, approved, draft)
	item, _ := json.Marshal(map[string]interface{}{"code": itemID, "name": "SoD import fixture"})
	appr, _ := json.Marshal(map[string]interface{}{"code": approved, "product_id": itemID, "language": "hi", "title": "Approved copy", "status": "Approved"})
	drf, _ := json.Marshal(map[string]interface{}{"code": draft, "product_id": itemID, "language": "ta", "title": "Draft copy", "status": "Draft"})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Item', $2, 'Active', 'system'), ($3, 'ProductContent', $4, 'Approved', 'system'), ($5, 'ProductContent', $6, 'Draft', 'system')",
		itemID, item, approved, appr, draft, drf); err != nil {
		t.Fatalf("seed: %v", err)
	}
	csv := "id,code,product_id,language,title,status\n" +
		planted + "," + planted + "," + itemID + ",en,Planted,Approved\n" +
		approved + "," + approved + "," + itemID + ",hi,Rewritten after approval,\n" +
		draft + "," + draft + "," + itemID + ",ta,Draft edited,\n"
	res, err := BulkImportCSV("default", "ProductContent", strings.NewReader(csv), "system", RoleSuperAdmin, false)
	if err != nil {
		t.Fatalf("BulkImportCSV: %v", err)
	}
	if res.SuccessRows != 1 || res.FailedRows != 2 {
		t.Fatalf("want only the draft row written, got %d ok / %d failed: %+v", res.SuccessRows, res.FailedRows, res.Errors)
	}
	var n int
	db.DB.QueryRow("SELECT COUNT(*) FROM "+schema+".documents WHERE id = $1", planted).Scan(&n)
	if n != 0 {
		t.Fatal("an import created a record claiming status Approved")
	}
	var title string
	db.DB.QueryRow("SELECT data->>'title' FROM "+schema+".documents WHERE id = $1", approved).Scan(&title)
	if title != "Approved copy" {
		t.Fatalf("an import rewrote an approved record: title %q", title)
	}
	db.DB.QueryRow("SELECT data->>'title' FROM "+schema+".documents WHERE id = $1", draft).Scan(&title)
	if title != "Draft edited" {
		t.Fatalf("the draft row was not imported: title %q", title)
	}
}
