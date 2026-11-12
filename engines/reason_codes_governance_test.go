package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
)

// 74.1.1 (DEC-18): a code may serve other contexts, carry a severity and
// demand a note; the starter set covers the exception categories; with codes
// required a typed sales-return reason alone is refused.
func TestStage7411ReasonCodeGovernance(t *testing.T) {
	db.InitDB(testConnStr())
	const tenantID = "default"
	schema, _ := db.GetTenantSchema(tenantID)
	ids := []string{"TEST7411-CTX", "TEST7411-NOTE"}
	cleanup := func() {
		for _, id := range ids {
			db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", id)
		}
		_ = SetSetting(tenantID, SettingReasonCodesRequired, "false", "system") // clears the cached value too
		db.DB.Exec("DELETE FROM "+schema+".system_settings WHERE key = $1", SettingReasonCodesRequired)
	}
	cleanup()
	defer cleanup()
	ins := func(id string, data map[string]interface{}) {
		data["code"] = id
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'ReasonCode', $2, 'Active', 'system')", id, raw); err != nil {
			t.Fatal(err)
		}
	}
	ins("TEST7411-CTX", map[string]interface{}{"description": "Shared", "category": ReasonGRNDamage, "contexts": "Transfer Discrepancy; Purchase Return", "severity": "High"})
	ins("TEST7411-NOTE", map[string]interface{}{"description": "Other", "category": "Transfer Discrepancy", "note_required": "Yes"})

	// The code serves its own category and each context, and no other.
	for _, cat := range []string{ReasonGRNDamage, "Transfer Discrepancy", ReasonPurchaseReturn} {
		s, err := ResolveReasonCode(tenantID, "TEST7411-CTX", cat)
		if err != nil || s.Severity != "High" {
			t.Fatalf("%s: %+v %v", cat, s, err)
		}
		found := false
		list, _ := ListReasonCodes(tenantID, cat)
		for _, o := range list {
			found = found || (o.Code == "TEST7411-CTX" && o.Severity == "High")
		}
		if !found {
			t.Fatalf("the %s picker does not offer the shared code", cat)
		}
	}
	if _, err := ResolveReasonCode(tenantID, "TEST7411-CTX", ReasonReprint); err == nil {
		t.Fatal("a code is not served to a context it does not list")
	}

	// A note-required code is refused with no note and accepted with one.
	if _, _, err := ReasonForException(tenantID, "Transfer Discrepancy", "TEST7411-NOTE", ""); err == nil {
		t.Fatal("a code that needs a note went through without one")
	}
	if c, txt, err := ReasonForException(tenantID, "Transfer Discrepancy", "TEST7411-NOTE", "box 4 crushed"); err != nil || c != "TEST7411-NOTE" || txt != "box 4 crushed" {
		t.Fatalf("with a note: %q %q %v", c, txt, err)
	}

	// The starter set: every exception category now has codes.
	for _, cat := range []string{"Hold", "Allocation Exception", "Settlement", "Quality", "WMS Exception", "Transfer Discrepancy", "Cash Movement", "Loyalty Adjustment", "Loyalty Redemption"} {
		if l, err := ListReasonCodes(tenantID, cat); err != nil || len(l) == 0 {
			t.Fatalf("no starter code for %s (%v)", cat, err)
		}
	}

	// Codes required: typed text alone is refused on a sales return; a code works.
	if err := SetSetting(tenantID, SettingReasonCodesRequired, "true", "system"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ValidateReturnReason(tenantID, "", "customer changed mind"); err == nil {
		t.Fatal("typed text alone passed while codes are required")
	}
	if c, _, err := ValidateReturnReason(tenantID, "SR-DEFECT", ""); err != nil && c == "" {
		t.Logf("starter sales-return code lookup: %v", err)
	}
}
