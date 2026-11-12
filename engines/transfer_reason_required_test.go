package engines

import (
	"custom_erp/db"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
)

// 74.1.1: with reasons.codes_required on, a transfer receive that closes a
// shortfall with free text only is refused (STOCKT-0484); a Transfer
// Discrepancy code passes; with the setting off the typed reason still works.
func TestStage7411TransferShortfallNeedsCodeWhenRequired(t *testing.T) {
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
	const from, to, sku = "LOC-RR-A", "LOC-RR-B", "RR-SKU"
	for _, loc := range []string{from, to} {
		exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Location', $2, 'Active', 'system')",
			loc, fmt.Sprintf(`{"code":%q,"name":%q}`, loc, loc))
	}
	exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ('RC-RR-LOST', 'ReasonCode', $1, 'Active', 'system')",
		fmt.Sprintf(`{"code":"RC-RR-LOST","description":"Lost","category":%q}`, TransferDiscrepancyReasonCategory))
	item, _ := json.Marshal(map[string]interface{}{"code": sku, "name": sku, "tracking_mode": TrackingNone, "standard_cost": 10})
	exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ('ITEM-RR-SKU', 'Item', $1, 'Active', 'system')", item)
	exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, 100, 100)", sku, from)
	order := func(id string) {
		items, _ := json.Marshal([]map[string]interface{}{{"sku": sku, "qty": 5}})
		data, _ := json.Marshal(map[string]interface{}{"transfer_number": id, "from_warehouse": from, "to_warehouse": to, "status": "Approved", "items": string(items)})
		exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'TransferOrder', $2, 'Approved', 'maker')", id, data)
		if err := DispatchTransferOrder(tenantID, id, "system"); err != nil {
			t.Fatalf("dispatch %s: %v", id, err)
		}
	}
	code := func(err error) string {
		var v *ValidationError
		if errors.As(err, &v) {
			return v.Code
		}
		return ""
	}
	receive := func(id string, l TransferReceiptLine) error {
		l.Sku, l.Qty = sku, 3 // 2 of 5 are not accounted for
		_, err := ReceiveTransferOrderLines(tenantID, id, "system", TransferReceiptRequest{Lines: []TransferReceiptLine{l}})
		return err
	}
	order("TO-RR-1")
	order("TO-RR-2")
	order("TO-RR-3")

	if err := SetSetting(tenantID, SettingReasonCodesRequired, "true", "system"); err != nil {
		t.Fatal(err)
	}
	if err := receive("TO-RR-1", TransferReceiptLine{Reason: "box was short"}); code(err) != "STOCKT-0484" {
		t.Fatalf("free text with codes required: want STOCKT-0484, got %v", err)
	}
	if err := receive("TO-RR-1", TransferReceiptLine{ReasonCode: "RC-RR-LOST"}); err != nil {
		t.Fatalf("a Transfer Discrepancy code with codes required: %v", err)
	}

	if err := SetSetting(tenantID, SettingReasonCodesRequired, "false", "system"); err != nil {
		t.Fatal(err)
	}
	if err := receive("TO-RR-2", TransferReceiptLine{Reason: "box was short"}); err != nil {
		t.Fatalf("free text with codes not required: %v", err)
	}
}

// 74.1.1: a sales-return reason typed without a code is refused when codes are
// required and accepted when they are not.
func TestStage7411ReturnReasonTextNeedsCodeWhenRequired(t *testing.T) {
	db.InitDB(testConnStr())
	const tenantID = "default"
	schema, _ := db.GetTenantSchema(tenantID)
	cleanup := func() {
		_ = SetSetting(tenantID, SettingReasonCodesRequired, "false", "system") // clears the cached value too
		db.DB.Exec("DELETE FROM "+schema+".system_settings WHERE key = $1", SettingReasonCodesRequired)
	}
	cleanup()
	defer cleanup()

	if err := SetSetting(tenantID, SettingReasonCodesRequired, "false", "system"); err != nil {
		t.Fatal(err)
	}
	if c, txt, err := ValidateReturnReason(tenantID, "", "customer changed mind"); err != nil || c != "" || txt != "customer changed mind" {
		t.Fatalf("text only, codes not required: %q %q %v", c, txt, err)
	}
	if err := SetSetting(tenantID, SettingReasonCodesRequired, "true", "system"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ValidateReturnReason(tenantID, "", "customer changed mind"); err == nil {
		t.Fatal("text only passed while codes are required")
	}
	if c, _, err := ValidateReturnReason(tenantID, "SR-DEFECT", ""); err != nil || c != "SR-DEFECT" {
		t.Fatalf("a Sales Return code with codes required: %q %v", c, err)
	}
}
