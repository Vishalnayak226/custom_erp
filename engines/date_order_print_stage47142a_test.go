package engines

import (
	"custom_erp/db"
	"encoding/json"
	"strings"
	"testing"
)

// TestStage47142aPrintsFollowTheClientsDateOrder: a till receipt's bill
// date-time prints 03-02-2026 for a day-first client (the default) and
// 02-03-2026 once the client is month first; year first is 2026-02-03.
func TestStage47142aPrintsFollowTheClientsDateOrder(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	ids := []string{"T47142A-STORE", "T47142A-ITEM", "T47142A-CART"}
	cleanup := func() {
		for _, id := range ids {
			db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", id)
		}
	}
	cleanup()
	defer cleanup()
	_, _ = db.DB.Exec("DELETE FROM "+schema+".system_settings WHERE key = $1", SettingKeyDateOrder)
	defer func() {
		_ = SetSetting(tenantID, SettingKeyDateOrder, DateOrderDMY, "system")
		_, _ = db.DB.Exec("DELETE FROM "+schema+".system_settings WHERE key = $1", SettingKeyDateOrder)
	}()
	put := func(id, doctype, status string, data map[string]interface{}) {
		raw, _ := json.Marshal(data)
		// Midday, so no time zone moves the day.
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by, created_at) VALUES ($1, $2, $3, $4, 'system', '2026-02-03 12:15:00')", id, doctype, raw, status); err != nil {
			t.Fatalf("%s: %v", id, err)
		}
	}
	put("T47142A-STORE", "Location", "Active", map[string]interface{}{"code": "T47142A-STORE", "name": "T47142A Store", "type": "Store"})
	put("T47142A-ITEM", "Item", "Active", map[string]interface{}{"code": "T47142A-ITEM", "name": "Shirt", "hsn_code": "6205", "gst_rate": 12})
	put("T47142A-CART", "POSCart", "Paid", map[string]interface{}{"location": "T47142A-STORE", "payment_mode": "Cash", "bill_number": "BT47142A0001",
		"items": []map[string]interface{}{{"sku": "T47142A-ITEM", "qty": 1, "sale_price": 1120, "net_total": 1120}}})

	billDate := func() string {
		d, err := BuildPrintDocument(tenantID, PrintTxnPOSTaxInvoice, "T47142A-CART", "", false)
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		s, _ := d.Fields["bill_datetime"].(string)
		return s
	}
	if got := billDate(); !strings.HasPrefix(got, "03-02-2026 ") {
		t.Errorf("day first bill_datetime = %q, want 03-02-2026 hh:mm", got)
	}
	for order, want := range map[string]string{DateOrderMDY: "02-03-2026 ", DateOrderYMD: "2026-02-03 "} {
		if err := SetSetting(tenantID, SettingKeyDateOrder, order, "system"); err != nil {
			t.Fatal(err)
		}
		if got := billDate(); !strings.HasPrefix(got, want) {
			t.Errorf("%s bill_datetime = %q, want %shh:mm", order, got, want)
		}
	}
}
