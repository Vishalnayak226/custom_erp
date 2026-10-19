package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
	"time"
)

// TestStage74_1_1ReasonCodes: one check for every reason code (status,
// action, dates), the starter codes, a code beside the typed reason on a GRN
// line and a purchase return, the reprint reason, and the client setting that
// makes codes mandatory.
func TestStage74_1_1ReasonCodes(t *testing.T) {
	db.InitDB(testConnStr())
	const tenantID = "default"
	schema, _ := db.GetTenantSchema(tenantID)
	ids := []string{"TEST741-OLD", "TEST741-FUTURE", "TEST741-OFF"}
	cleanup := func() {
		for _, id := range ids {
			db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", id)
		}
		_ = SetSetting(tenantID, SettingReasonCodesRequired, "false", "system")
		db.DB.Exec("DELETE FROM "+schema+".system_settings WHERE key = $1", SettingReasonCodesRequired)
	}
	cleanup()
	defer cleanup()
	ins := func(id, status string, data map[string]interface{}) {
		data["code"] = id
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'ReasonCode', $2, $3, 'system')", id, raw, status); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	tomorrow := time.Now().AddDate(0, 0, 1).Format("2006-01-02")
	ins("TEST741-OLD", "Active", map[string]interface{}{"description": "Retired", "category": ReasonGRNRejection, "effective_to": yesterday})
	ins("TEST741-FUTURE", "Active", map[string]interface{}{"description": "Not yet", "category": ReasonGRNRejection, "effective_from": tomorrow})
	ins("TEST741-OFF", "Inactive", map[string]interface{}{"description": "Off", "category": ReasonGRNRejection})

	if s, err := ResolveReasonCode(tenantID, "GRN-REJ-QUALITY", ReasonGRNRejection); err != nil || s.Label != "Quality not as ordered" {
		t.Fatalf("a starter code: %+v %v", s, err)
	}
	for _, bad := range []string{"TEST741-OLD", "TEST741-FUTURE", "TEST741-OFF", "NOPE"} {
		if _, err := ResolveReasonCode(tenantID, bad, ReasonGRNRejection); err == nil {
			t.Fatalf("%s must be refused", bad)
		}
	}
	if _, err := ResolveReasonCode(tenantID, "GRN-REJ-QUALITY", ReasonPurchaseReturn); err == nil {
		t.Fatalf("a code of another action must be refused")
	}
	if requireActiveReasonCode(tenantID, "TEST741-OLD", ReasonGRNRejection) == nil {
		t.Fatalf("the old guard goes through the same check (dates)")
	}
	list, err := ListReasonCodes(tenantID, ReasonGRNRejection)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, o := range list {
		if o.Code == "TEST741-OLD" || o.Code == "TEST741-FUTURE" || o.Code == "TEST741-OFF" {
			t.Fatalf("the picker must offer only usable codes, got %s", o.Code)
		}
	}

	// A GRN line: the code fills a blank reason; typed text alone still works
	// while codes are optional.
	lines, _ := json.Marshal([]map[string]interface{}{
		{"sku": "X1", "qty": 5, "rejected_qty": 1, "rejection_reason_code": "GRN-REJ-WRONG"},
		{"sku": "X2", "qty": 5, "damaged_qty": 1, "damage_reason": "crushed box"},
	})
	payload := map[string]interface{}{"received_items": string(lines)}
	if err := PrepareGRNReasons(tenantID, true, payload); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	got := grnLineMaps(payload)
	if got[0]["rejection_reason"] != "Wrong item, size or colour" || got[1]["damage_reason"] != "crushed box" {
		t.Fatalf("reasons %v / %v", got[0]["rejection_reason"], got[1]["damage_reason"])
	}
	if r, err := normalizeReprintReason(tenantID, "RPT-LOST"); err != nil || r != "RPT-LOST: Original lost" {
		t.Fatalf("reprint code: %q %v", r, err)
	}

	// The client makes codes mandatory.
	if err := SetSetting(tenantID, SettingReasonCodesRequired, "true", "system"); err != nil {
		t.Fatalf("setting: %v", err)
	}
	payload = map[string]interface{}{"received_items": `[{"sku":"X2","qty":5,"damaged_qty":1,"damage_reason":"crushed box"}]`}
	if err := PrepareGRNReasons(tenantID, true, payload); err == nil {
		t.Fatalf("a typed reason alone must be refused when codes are required")
	}
	if _, err := normalizeReprintReason(tenantID, "just again"); err == nil {
		t.Fatalf("a typed reprint reason must be refused when codes are required")
	}
	pr := map[string]interface{}{"grn_id": "NOPE", "reason": "bad"}
	if err := validatePurchaseReturnRules(tenantID, "", "", nil, pr); err == nil {
		t.Fatalf("a purchase return without a code must be refused when codes are required")
	}
}
