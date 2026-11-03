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
