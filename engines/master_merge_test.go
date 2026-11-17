package engines

import (
	"custom_erp/db"
	"encoding/json"
	"strings"
	"testing"
)

// Stage 47.13.5: merge a duplicate supplier / item into the one kept. Every
// Link reference moves (found from the metadata), a posted document keeps its
// snapshot fields, the duplicate is marked Merged and never deleted, audit
// rows exist, and unmerge puts the references back - skipping one changed
// again since.
func TestStage47135MergeAndUnmergeMasters(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	insert := func(id, doctype, status string, data map[string]interface{}) {
		t.Helper()
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system')", id, doctype, raw, status); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	field := func(id, key string) string {
		t.Helper()
		var v string
		if err := db.DB.QueryRow("SELECT COALESCE(data->>$2, '') FROM "+schema+".documents WHERE id = $1", id, key).Scan(&v); err != nil {
			t.Fatalf("read %s: %v", id, err)
		}
		return v
	}
	insert("V-KEEP", "Vendor", "Active", map[string]interface{}{"code": "V-KEEP", "name": "Sharma Textiles"})
	insert("V-DUP", "Vendor", "Active", map[string]interface{}{"code": "V-DUP", "name": "Sharma Textiles Pvt Ltd"})
	// five references on three record types; the invoice is posted and keeps its snapshot
	insert("PO-1", "PurchaseOrder", "Draft", map[string]interface{}{"vendor": "V-DUP"})
	insert("PO-2", "PurchaseOrder", "Approved", map[string]interface{}{"vendor": "V-DUP"})
	insert("GRN-1", "GRN", "Posted", map[string]interface{}{"vendor": "V-DUP"})
	insert("VI-1", "VendorInvoice", "Posted", map[string]interface{}{"vendor_id": "V-DUP", "vendor_name": "Sharma Textiles Pvt Ltd", "total": "1180.00"})
	insert("VI-2", "VendorInvoice", "Posted", map[string]interface{}{"vendor_id": "V-DUP", "vendor_name": "Sharma Textiles Pvt Ltd"})
	insert("PO-OTHER", "PurchaseOrder", "Draft", map[string]interface{}{"vendor": "V-KEEP"})

	res, err := MergeMasters(tenantID, "Vendor", "V-KEEP", "V-DUP", "same GSTIN", "admin1")
	if err != nil {
		t.Fatalf("MergeMasters: %v", err)
	}
	if res.Moved != 5 || res.ByType["PurchaseOrder"] != 2 || res.ByType["VendorInvoice"] != 2 || res.ByType["GRN"] != 1 {
		t.Fatalf("moved = %d %v, want 5 over PurchaseOrder 2 / VendorInvoice 2 / GRN 1", res.Moved, res.ByType)
	}
	for _, id := range []string{"PO-1", "PO-2"} {
		if got := field(id, "vendor"); got != "V-KEEP" {
			t.Errorf("%s vendor = %q after merge, want V-KEEP", id, got)
		}
	}
	if got := field("VI-1", "vendor_id"); got != "V-KEEP" {
		t.Errorf("posted invoice link = %q, want V-KEEP", got)
	}
	if field("VI-1", "vendor_name") != "Sharma Textiles Pvt Ltd" || field("VI-1", "total") != "1180.00" {
		t.Errorf("posted invoice snapshot changed: name %q total %q", field("VI-1", "vendor_name"), field("VI-1", "total"))
	}
	var dupStatus string
	var dupRows int
	db.DB.QueryRow("SELECT status FROM " + schema + ".documents WHERE id = 'V-DUP'").Scan(&dupStatus)
	db.DB.QueryRow("SELECT COUNT(*) FROM " + schema + ".documents WHERE id = 'V-DUP' AND deleted_at IS NULL").Scan(&dupRows)
	if dupStatus != "Merged" || dupRows != 1 || field("V-DUP", "merged_into") != "V-KEEP" {
		t.Errorf("duplicate: status %q rows %d merged_into %q, want Merged, kept, V-KEEP", dupStatus, dupRows, field("V-DUP", "merged_into"))
	}
	// a merged record cannot be merged again
	if _, err := MergeMasters(tenantID, "Vendor", "V-KEEP", "V-DUP", "", "admin1"); err == nil || !strings.Contains(err.Error(), "merged") {
		t.Errorf("merging an already merged record: err = %v, want a refusal", err)
	}
	var audits int
	db.DB.QueryRow("SELECT COUNT(*) FROM " + schema + ".audit_logs WHERE action = 'MASTER_MERGE'").Scan(&audits)
	if audits != 1 {
		t.Errorf("MASTER_MERGE audit rows = %d, want 1", audits)
	}

	// somebody re-points GRN-1 again after the merge: unmerge must not overwrite it
	if _, err := db.DB.Exec("UPDATE " + schema + ".documents SET data = jsonb_set(data, '{vendor}', '\"V-ELSE\"') WHERE id = 'GRN-1'"); err != nil {
		t.Fatal(err)
	}
	un, err := UnmergeMasters(tenantID, res.ID, "admin2")
	if err != nil {
		t.Fatalf("UnmergeMasters: %v", err)
	}
	if un.Moved != 4 || un.Skipped != 1 {
		t.Errorf("unmerge restored %d skipped %d, want 4 and 1", un.Moved, un.Skipped)
	}
	for _, id := range []string{"PO-1", "PO-2"} {
		if got := field(id, "vendor"); got != "V-DUP" {
			t.Errorf("%s vendor = %q after unmerge, want V-DUP", id, got)
		}
	}
	if field("VI-1", "vendor_id") != "V-DUP" || field("VI-2", "vendor_id") != "V-DUP" || field("PO-OTHER", "vendor") != "V-KEEP" {
		t.Errorf("unmerge did not restore the invoices or touched an unrelated order")
	}
	if field("GRN-1", "vendor") != "V-ELSE" {
		t.Errorf("a reference changed since the merge was overwritten: %q", field("GRN-1", "vendor"))
	}
	db.DB.QueryRow("SELECT status FROM " + schema + ".documents WHERE id = 'V-DUP'").Scan(&dupStatus)
	if dupStatus != "Active" || field("V-DUP", "merged_into") != "" {
		t.Errorf("duplicate after unmerge: status %q merged_into %q, want Active and none", dupStatus, field("V-DUP", "merged_into"))
	}
	if _, err := UnmergeMasters(tenantID, res.ID, "admin2"); err == nil {
		t.Errorf("a second unmerge should be refused")
	}
	db.DB.QueryRow("SELECT COUNT(*) FROM " + schema + ".audit_logs WHERE action = 'MASTER_UNMERGE'").Scan(&audits)
	if audits != 1 {
		t.Errorf("MASTER_UNMERGE audit rows = %d, want 1", audits)
	}
	hist, err := ListMasterMerges(tenantID)
	if err != nil || len(hist) != 1 || hist[0].Status != "Unmerged" || hist[0].Moved != 5 {
		t.Errorf("history = %+v err %v, want one Unmerged row of 5", hist, err)
	}

	// items: stock blocks the merge, zero stock allows it and ProductMedia follows
	insert("I-KEEP", "Item", "Active", map[string]interface{}{"code": "I-KEEP", "name": "Blue Shirt"})
	insert("I-DUP", "Item", "Active", map[string]interface{}{"code": "I-DUP", "name": "Blue Shirt"})
	insert("PM-1", "ProductMedia", "Active", map[string]interface{}{"item": "I-DUP"})
	if _, err := db.DB.Exec("INSERT INTO " + schema + ".inventory_availability (sku, location_code, on_hand, available) VALUES ('I-DUP', 'STORE-A', 4, 4)"); err != nil {
		t.Fatalf("stock row: %v", err)
	}
	if _, err := MergeMasters(tenantID, "Item", "I-KEEP", "I-DUP", "", "admin1"); err == nil || !strings.Contains(err.Error(), "stock") {
		t.Errorf("item with stock: err = %v, want a stock refusal", err)
	}
	if _, err := db.DB.Exec("UPDATE " + schema + ".inventory_availability SET on_hand = 0, available = 0 WHERE sku = 'I-DUP'"); err != nil {
		t.Fatal(err)
	}
	ir, err := MergeMasters(tenantID, "Item", "I-KEEP", "I-DUP", "", "admin1")
	if err != nil || ir.Moved < 1 || field("PM-1", "item") != "I-KEEP" {
		t.Fatalf("item merge: %+v err %v media item %q", ir, err, field("PM-1", "item"))
	}
	if _, err := MergeMasters(tenantID, "Customer", "a", "b", "", "admin1"); err == nil {
		t.Errorf("customers merge elsewhere; MergeMasters should refuse them")
	}
}

// Stage 47.13.5 (ask 179): a merge request waits in Approvals where a
// MasterMerge rule exists; nothing moves until a second admin approves it,
// a rejection moves nothing, a merge that can no longer be done when approved
// is recorded on the request, and with no rule the merge is done at once.
func TestStage47135MergeGoesThroughApproval(t *testing.T) {
	db.InitDB(testConnStr())
	var registered bool
	if err := db.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM tenant_default.doctype_meta WHERE name = 'MasterMerge')`).Scan(&registered); err != nil || !registered {
		t.Skip("migrations_stage47_13_5b_master_merge_approval.sql is not applied to this database")
	}
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	insert := func(id, doctype, status string, data map[string]interface{}) {
		t.Helper()
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system')", id, doctype, raw, status); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	read := func(id, key string) string {
		t.Helper()
		var v string
		_ = db.DB.QueryRow("SELECT COALESCE(CASE WHEN $2 = 'status' THEN status ELSE data->>$2 END, '') FROM "+schema+".documents WHERE id = $1", id, key).Scan(&v)
		return v
	}
	if _, err := db.DB.Exec("INSERT INTO " + schema + ".approval_rules (doctype, min_amount, max_amount, required_role) SELECT 'MasterMerge', 0, NULL, 'HR/Admin' WHERE NOT EXISTS (SELECT 1 FROM " + schema + ".approval_rules WHERE doctype = 'MasterMerge')"); err != nil {
		t.Fatal(err)
	}
	insert("V-KEEP", "Vendor", "Active", map[string]interface{}{"code": "V-KEEP", "name": "Sharma Textiles"})
	insert("V-DUP", "Vendor", "Active", map[string]interface{}{"code": "V-DUP", "name": "Sharma Textiles Pvt Ltd"})
	insert("PO-1", "PurchaseOrder", "Draft", map[string]interface{}{"vendor": "V-DUP"})

	req, err := RequestMasterMerge(tenantID, "Vendor", "V-KEEP", "V-DUP", "same GSTIN", "admin1", "HR/Admin")
	if err != nil || req.Status != "Pending Approval" || req.Merge != nil {
		t.Fatalf("request: %v %+v, want Pending Approval and nothing merged", err, req)
	}
	if read("PO-1", "vendor") != "V-DUP" || read("V-DUP", "status") != "Active" {
		t.Fatalf("a merge waiting for approval already moved: PO-1 %q, duplicate %q", read("PO-1", "vendor"), read("V-DUP", "status"))
	}
	if _, err := RequestMasterMerge(tenantID, "Vendor", "V-KEEP", "V-DUP", "again", "admin2", "HR/Admin"); err == nil || !strings.Contains(err.Error(), "already waiting") {
		t.Errorf("a second request for the same duplicate: %v, want a refusal", err)
	}
	if _, err := RequestMasterMerge(tenantID, "Vendor", "V-DUP", "V-KEEP", "", "admin2", "HR/Admin"); err == nil {
		t.Errorf("the reverse merge of a pair already waiting was accepted")
	}
	// a new client starts with nobody deciding their own request
	if err := DecideApproval(tenantID, MasterMergeDoctype, req.ID, "admin1", "HR/Admin", "", "Approved", ""); err == nil {
		t.Errorf("the requester approved their own merge")
	}
	if err := DecideApproval(tenantID, MasterMergeDoctype, req.ID, "admin2", "HR/Admin", "", "Approved", "checked"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if read(req.ID, "status") != "Merged" || read(req.ID, "merge_record") == "" || read("PO-1", "vendor") != "V-KEEP" || read("V-DUP", "status") != "Merged" {
		t.Fatalf("after approval: request %q record %q, PO-1 %q, duplicate %q - want Merged, a record, V-KEEP, Merged",
			read(req.ID, "status"), read(req.ID, "merge_record"), read("PO-1", "vendor"), read("V-DUP", "status"))
	}
	var by string
	_ = db.DB.QueryRow("SELECT merged_by FROM "+schema+".master_merges WHERE id = $1", read(req.ID, "merge_record")).Scan(&by)
	if by != "admin2" {
		t.Errorf("merge record merged_by = %q, want the approver admin2", by)
	}

	// rejected: nothing moves
	insert("V-DUP2", "Vendor", "Active", map[string]interface{}{"code": "V-DUP2", "name": "Sharma Tex"})
	insert("PO-2", "PurchaseOrder", "Draft", map[string]interface{}{"vendor": "V-DUP2"})
	rej, err := RequestMasterMerge(tenantID, "Vendor", "V-KEEP", "V-DUP2", "", "admin1", "HR/Admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := DecideApproval(tenantID, MasterMergeDoctype, rej.ID, "admin2", "HR/Admin", "", "Rejected", "different branch"); err != nil {
		t.Fatal(err)
	}
	if read(rej.ID, "status") != "Rejected" || read("PO-2", "vendor") != "V-DUP2" || read("V-DUP2", "status") != "Active" {
		t.Errorf("after rejection: request %q, PO-2 %q, duplicate %q", read(rej.ID, "status"), read("PO-2", "vendor"), read("V-DUP2", "status"))
	}

	// stock taken while the request waited: the approval records why it was not merged
	insert("STORE-MM", "Location", "Active", map[string]interface{}{"code": "STORE-MM", "name": "Store MM", "type": "Store", "sellable": "Yes"})
	insert("ITEM-KEEP", "Item", "Active", map[string]interface{}{"code": "ITEM-KEEP", "name": "Kurta"})
	insert("ITEM-DUP", "Item", "Active", map[string]interface{}{"code": "ITEM-DUP", "name": "Kurta "})
	late, err := RequestMasterMerge(tenantID, "Item", "ITEM-KEEP", "ITEM-DUP", "", "admin1", "HR/Admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := PostInventoryLedger(tenantID, "STORE-MM", []interface{}{map[string]interface{}{"sku": "ITEM-DUP", "qty": 2.0}}, false); err != nil {
		t.Fatal(err)
	}
	if err := DecideApproval(tenantID, MasterMergeDoctype, late.ID, "admin2", "HR/Admin", "", "Approved", ""); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if read(late.ID, "status") != "Approved" || !strings.Contains(read(late.ID, "merge_error"), "stock") || read("ITEM-DUP", "status") != "Active" {
		t.Errorf("approved after the item took stock: request %q error %q item %q, want Approved, a stock error, Active",
			read(late.ID, "status"), read(late.ID, "merge_error"), read("ITEM-DUP", "status"))
	}

	// no rule: done at once, as before
	if _, err := db.DB.Exec("DELETE FROM " + schema + ".approval_rules WHERE doctype = 'MasterMerge'"); err != nil {
		t.Fatal(err)
	}
	insert("V-DUP3", "Vendor", "Active", map[string]interface{}{"code": "V-DUP3", "name": "Sharma T"})
	insert("PO-3", "PurchaseOrder", "Draft", map[string]interface{}{"vendor": "V-DUP3"})
	now, err := RequestMasterMerge(tenantID, "Vendor", "V-KEEP", "V-DUP3", "", "admin1", "HR/Admin")
	if err != nil || now.Status != "Merged" || now.Merge == nil || now.Merge.Moved != 1 || read("PO-3", "vendor") != "V-KEEP" {
		t.Fatalf("with no rule: %v %+v, PO-3 %q - want Merged at once, 1 moved", err, now, read("PO-3", "vendor"))
	}
	if _, err := RequestMasterMerge(tenantID, "Customer", "C1", "C2", "", "admin1", "HR/Admin"); err == nil {
		t.Errorf("a customer merge request was accepted here")
	}
}

// Ask 223 (47.13.5): ids inside line lists move with the merge - a PO's items
// stored as a JSON string, a BOM's components stored as an array, an RFQ's
// invited vendors - only in the linked columns, and an undo puts each list
// back exactly as it was unless it changed again since.
func TestMasterMergeMovesLineLists(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	var specs int
	db.DB.QueryRow("SELECT COUNT(*) FROM " + schema + ".doctype_fields WHERE fieldtype = 'JSONTable' AND (doctype_name, fieldname) IN (('PurchaseOrder','items'),('BOM','components'),('RFQ','vendor_invitations'))").Scan(&specs)
	if specs != 3 {
		t.Skipf("line-list specs not seeded in this schema (%d of 3)", specs)
	}
	insertRaw := func(id, doctype, raw string) {
		t.Helper()
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3::jsonb, 'Draft', 'system')", id, doctype, raw); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	stored := func(id, field string) string {
		t.Helper()
		var v string
		if err := db.DB.QueryRow("SELECT COALESCE((data->$2)::text, '') FROM "+schema+".documents WHERE id = $1", id, field).Scan(&v); err != nil {
			t.Fatalf("read %s: %v", id, err)
		}
		return v
	}
	insertRaw("LI-KEEP", "Item", `{"code":"LI-KEEP","name":"Kada"}`)
	insertRaw("LI-DUP", "Item", `{"code":"LI-DUP","name":"Kada"}`)
	insertRaw("LV-KEEP", "Vendor", `{"code":"LV-KEEP","name":"Om Gold"}`)
	insertRaw("LV-DUP", "Vendor", `{"code":"LV-DUP","name":"Om Gold Pvt Ltd"}`)
	// a description that merely contains the id must not change
	insertRaw("LPO-1", "PurchaseOrder", `{"items":"[{\"sku\":\"LI-DUP\",\"qty\":2,\"rate\":10.50},{\"sku\":\"OTHER\",\"qty\":1,\"note\":\"was LI-DUP\"},{\"sku\":\"LI-DUP\",\"qty\":1}]"}`)
	insertRaw("LBOM-1", "BOM", `{"components":[{"sku":"LI-DUP","qty":3},{"sku":"LI-KEEP","qty":1}]}`)
	insertRaw("LBOM-2", "BOM", `{"components":[{"sku":"OTHER","qty":3}]}`)
	insertRaw("LRFQ-1", "RFQ", `{"vendor_invitations":"[{\"vendor\":\"LV-DUP\",\"status\":\"Invited\"}]"}`)
	poBefore, bomBefore, bom2Before := stored("LPO-1", "items"), stored("LBOM-1", "components"), stored("LBOM-2", "components")

	ir, err := MergeMasters(tenantID, "Item", "LI-KEEP", "LI-DUP", "same design", "admin1")
	if err != nil {
		t.Fatalf("item merge: %v", err)
	}
	if ir.ByType["PurchaseOrder"] != 1 || ir.ByType["BOM"] != 1 {
		t.Fatalf("item merge moved %v, want one PurchaseOrder and one BOM", ir.ByType)
	}
	var po []map[string]interface{}
	var poText string
	db.DB.QueryRow("SELECT data->>'items' FROM " + schema + ".documents WHERE id = 'LPO-1'").Scan(&poText)
	if err := json.Unmarshal([]byte(poText), &po); err != nil {
		t.Fatalf("PO items no longer a JSON string of lines: %v (%s)", err, stored("LPO-1", "items"))
	}
	if po[0]["sku"] != "LI-KEEP" || po[2]["sku"] != "LI-KEEP" || po[1]["sku"] != "OTHER" || po[1]["note"] != "was LI-DUP" {
		t.Errorf("PO lines after merge = %v", po)
	}
	if !strings.Contains(poText, `"rate":10.50`) {
		t.Errorf("a line's number lost its exact text: %s", poText)
	}
	if got := stored("LBOM-1", "components"); !strings.HasPrefix(got, "[") || !strings.Contains(got, `"sku": "LI-KEEP", "qty": 3`) && !strings.Contains(got, `"qty": 3, "sku": "LI-KEEP"`) {
		t.Errorf("BOM components after merge = %s, want an array naming LI-KEEP", got)
	}
	if stored("LBOM-2", "components") != bom2Before {
		t.Errorf("a BOM without the duplicate changed")
	}
	vr, err := MergeMasters(tenantID, "Vendor", "LV-KEEP", "LV-DUP", "", "admin1")
	if err != nil || vr.ByType["RFQ"] != 1 || !strings.Contains(stored("LRFQ-1", "vendor_invitations"), `LV-KEEP`) {
		t.Fatalf("vendor merge: %+v err %v rfq %s", vr, err, stored("LRFQ-1", "vendor_invitations"))
	}

	// the BOM is edited again after the merge: undo leaves it, restores the PO exactly
	if _, err := db.DB.Exec("UPDATE " + schema + ".documents SET data = jsonb_set(data, '{components}', '[{\"sku\":\"LI-KEEP\",\"qty\":5}]') WHERE id = 'LBOM-1'"); err != nil {
		t.Fatal(err)
	}
	un, err := UnmergeMasters(tenantID, ir.ID, "admin2")
	if err != nil || un.Moved != 1 || un.Skipped != 1 {
		t.Fatalf("unmerge: %+v err %v, want 1 restored and 1 skipped", un, err)
	}
	if got := stored("LPO-1", "items"); got != poBefore {
		t.Errorf("PO items after undo = %s, want exactly %s", got, poBefore)
	}
	if got := stored("LBOM-1", "components"); got == bomBefore || !strings.Contains(got, `"qty": 5`) {
		t.Errorf("a list changed since the merge was overwritten: %s", got)
	}
}

func TestRepointLinesKeepsShapeAndOtherColumns(t *testing.T) {
	out, ok := repointLines([]byte(`[{"sku":"A","alt":"A","qty":1.0}]`), []string{"sku"}, "A", "B")
	if !ok || string(out) != `[{"alt":"A","qty":1.0,"sku":"B"}]` {
		t.Errorf("array: %s %v", out, ok)
	}
	out, ok = repointLines([]byte(`"[{\"sku\":\"A<1>\"}]"`), []string{"sku"}, "A<1>", "B&2")
	if !ok || string(out) != `"[{\"sku\":\"B&2\"}]"` {
		t.Errorf("string: %s %v", out, ok)
	}
	if _, ok := repointLines([]byte(`"not json"`), []string{"sku"}, "A", "B"); ok {
		t.Errorf("a value that is not a list changed")
	}
	if _, ok := repointLines([]byte(`[{"sku":"AB"}]`), []string{"sku"}, "A", "B"); ok {
		t.Errorf("a partial match changed")
	}
}
