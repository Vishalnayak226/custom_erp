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

// 213.2: a profile may name B2B / B2C and one user. Each is a filter (a
// profile for B2B never applies to a B2C bill, one for another user never
// applies to this one) and a rank: document, then B2B / B2C, then user,
// then store - so the most specific one wins.
func TestProfileScorePartyAndUser(t *testing.T) {
	p := func(kv ...string) map[string]interface{} {
		m := map[string]interface{}{}
		for i := 0; i+1 < len(kv); i += 2 {
			m[kv[i]] = kv[i+1]
		}
		return m
	}
	score := func(data map[string]interface{}, party, user string) int {
		return profileScore(data, "POSTaxInvoice", "POSCart", "ST-1", "", "POS", party, user)
	}
	if s := score(p("transaction", "POSTaxInvoice", "party_type", "B2B"), "B2C", "ravi"); s != -1 {
		t.Errorf("a B2B profile applied to a B2C bill: %d", s)
	}
	if s := score(p("transaction", "POSTaxInvoice", "user", "asha"), "B2B", "ravi"); s != -1 {
		t.Errorf("another user's profile applied: %d", s)
	}
	if s := score(p("transaction", "POSTaxInvoice", "user", "asha"), "B2B", ""); s != -1 {
		t.Errorf("a user's profile applied when no one is printing: %d", s)
	}
	all := score(p("transaction", "POSTaxInvoice", "party_type", "All"), "B2B", "ravi")
	b2b := score(p("transaction", "POSTaxInvoice", "party_type", "b2b"), "B2B", "ravi")
	mine := score(p("transaction", "POSTaxInvoice", "user", "Ravi"), "B2B", "ravi")
	store := score(p("transaction", "POSTaxInvoice", "store", "ST-1"), "B2B", "ravi")
	both := score(p("transaction", "POSTaxInvoice", "party_type", "B2B", "user", "ravi"), "B2B", "ravi")
	anyDoc := score(p("party_type", "B2B", "user", "ravi", "store", "ST-1", "channel", "POS"), "B2B", "ravi")
	if !(both > b2b && b2b > mine && mine > store && store > all && all >= 0) {
		t.Errorf("rank: both %d > B2B %d > user %d > store %d > all %d", both, b2b, mine, store, all)
	}
	if anyDoc >= all {
		t.Errorf("a profile for this document beats one for every document however specific: %d vs %d", anyDoc, all)
	}
}
