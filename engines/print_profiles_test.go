package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
)

// Stage 127.4: the most specific Active print profile wins; the store block
// carries the Legal Entity's trade name, bank and terms.
func TestStage127_4PrintProfiles(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schemaName := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schemaName)
	if _, err := ProvisionTenantSchema(tenantID, schemaName, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	insert := func(id, doctype, status string, data map[string]interface{}) {
		t.Helper()
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schemaName+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system')", id, doctype, raw, status); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	insert("LE-P", "LegalEntity", "Active", map[string]interface{}{"code": "LE-P", "name": "Pixel Retail Pvt Ltd", "gstin": "29AAPFU0939F1ZV",
		"trade_name": "Pixel Stores", "bank_name": "HDFC Bank", "bank_account": "50100012345678", "bank_ifsc": "HDFC0000123", "print_terms": "Goods once sold are not taken back."})
	insert("ST-P", "Location", "Active", map[string]interface{}{"code": "ST-P", "name": "Pixel MG Road", "type": "Store", "legal_entity": "LE-P"})
	insert("RFQ-P", "RFQ", "Draft", map[string]interface{}{"code": "RFQ-P", "location": "ST-P"})

	d, _ := BuildPrintDocument(tenantID, "RFQ", "RFQ-P", "ST-P", false)
	if _, ok := d.Fields["print_profile"]; ok {
		t.Errorf("no profile: the print is as it always was")
	}
	if d.Store.TradeName != "Pixel Stores" || d.Store.Bank != "HDFC Bank · A/c 50100012345678 · IFSC HDFC0000123" || d.Store.Terms == "" {
		t.Errorf("store identity: %+v", d.Store)
	}

	insert("PP-ALL", "PrintProfile", "Active", map[string]interface{}{"code": "PP-ALL", "name": "Every document", "copy_labels": "Original", "status": "Active"})
	insert("PP-RFQ", "PrintProfile", "Active", map[string]interface{}{"code": "PP-RFQ", "name": "RFQ", "transaction": "RFQ",
		"copy_labels": "Original for Recipient, Duplicate for Transporter", "footer_text": "Thank you", "status": "Active"})
	insert("PP-OTHER", "PrintProfile", "Active", map[string]interface{}{"code": "PP-OTHER", "name": "Other store", "transaction": "RFQ", "store": "ST-ELSE", "copy_labels": "X", "status": "Active"})
	insert("PP-OFF", "PrintProfile", "Inactive", map[string]interface{}{"code": "PP-OFF", "name": "Off", "transaction": "RFQ", "store": "ST-P", "copy_labels": "Y", "status": "Inactive"})

	p := ResolvePrintProfile(tenantID, "RFQ", "RFQ", "ST-P")
	if p == nil || p.Code != "PP-RFQ" || len(p.CopyLabels) != 2 || p.CopyLabels[1] != "Duplicate for Transporter" {
		t.Fatalf("this document beats all documents; another store's and an Inactive one never apply: %+v", p)
	}
	if p := ResolvePrintProfile(tenantID, "GRN", "GRN", "ST-P"); p == nil || p.Code != "PP-ALL" {
		t.Errorf("a document with no profile of its own takes the all-documents one: %+v", p)
	}
	d, _ = BuildPrintDocument(tenantID, "RFQ", "RFQ-P", "ST-P", false)
	if pp, ok := d.Fields["print_profile"].(*PrintProfile); !ok || pp.Code != "PP-RFQ" || d.Fields["profile_footer"] != "Thank you" {
		t.Errorf("the print carries its profile: %+v %v", d.Fields["print_profile"], d.Fields["profile_footer"])
	}
}
