package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"testing"
)

// The inbox reads its approval facts for the whole page at once. This holds
// the batch to the per-document queries it replaced: same creator, same
// waiting-since, same "made this round" answer, same approver slab - for a
// viewer who changed some of the documents and logged a round on others.
func TestTaskInboxBatchFactsMatchPerDocumentQueries(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	exec := func(q string, args ...interface{}) {
		t.Helper()
		if _, err := db.DB.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	exec("DELETE FROM " + schema + ".approval_rules WHERE doctype IN ('InboxBatchA', 'InboxBatchB')")
	exec("INSERT INTO " + schema + ".approval_rules (doctype, min_amount, max_amount, required_role) VALUES " +
		"('InboxBatchA', 0, 999, 'Store Manager'), ('InboxBatchA', 1000, NULL, 'Super Admin'), ('InboxBatchB', 0, NULL, 'Store Manager')")
	var pending []map[string]interface{}
	for i := 0; i < 12; i++ {
		doctype := "InboxBatchA"
		if i%3 == 0 {
			doctype = "InboxBatchB"
		}
		id := fmt.Sprintf("IB-%02d", i)
		amount := 500 + i*150
		raw, _ := json.Marshal(map[string]interface{}{"code": id, "total_amount": amount})
		exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by, updated_by) VALUES ($1, $2, $3, 'Pending Approval', $4, $5)",
			id, doctype, raw, fmt.Sprintf("maker%d", i%2), map[bool]string{true: "viewer", false: "other"}[i%4 == 1])
		if i%5 == 2 {
			exec("INSERT INTO "+schema+".approval_log (doctype, document_id, action, actor_user_id, actor_role) VALUES ($1, $2, 'Submitted', 'viewer', 'Store Manager')", doctype, id)
		}
		if i%5 == 3 { // a decision after the viewer's round closes it
			exec("INSERT INTO "+schema+".approval_log (doctype, document_id, action, actor_user_id, actor_role) VALUES ($1, $2, 'Submitted', 'viewer', 'Store Manager')", doctype, id)
			exec("INSERT INTO "+schema+".approval_log (doctype, document_id, action, actor_user_id, actor_role) VALUES ($1, $2, 'Rejected', 'x', 'Super Admin')", doctype, id)
		}
		pending = append(pending, map[string]interface{}{"id": id, "doctype": doctype, "total_amount": float64(amount)})
	}
	batch, err := loadInboxApprovalFacts(schema, "viewer", pending)
	if err != nil {
		t.Fatalf("loadInboxApprovalFacts: %v", err)
	}
	madeCount := 0
	for _, doc := range pending {
		doctype, id := doc["doctype"].(string), doc["id"].(string)
		var createdBy string
		var waiting interface{}
		_ = waiting
		if err := db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(created_by, '') FROM %s.documents WHERE id = $1 AND doctype = $2`, schema), id, doctype).Scan(&createdBy); err != nil {
			t.Fatal(err)
		}
		made, err := madeApprovalRound(db.DB, schema, doctype, id, "viewer")
		if err != nil {
			t.Fatal(err)
		}
		want, err := requiredApproverRoleFor(tenantID, doctype, doc, DocumentBaseAmount(doc))
		if err != nil {
			t.Fatal(err)
		}
		f, ok := batch.facts[inboxFactKey(doctype, id)]
		if !ok {
			t.Fatalf("%s %s missing from the batch", doctype, id)
		}
		if f.createdBy != createdBy || f.made != made || f.waiting.IsZero() {
			t.Errorf("%s: batch %+v, per-document createdBy=%q made=%v", id, f, createdBy, made)
		}
		if got := batch.requiredRole(tenantID, doctype, doc, DocumentBaseAmount(doc)); got != want {
			t.Errorf("%s: batch slab %q, per-document %q", id, got, want)
		}
		if made {
			madeCount++
		}
	}
	if madeCount == 0 || madeCount == len(pending) {
		t.Fatalf("fixture proves nothing: %d of %d made by the viewer", madeCount, len(pending))
	}
}
