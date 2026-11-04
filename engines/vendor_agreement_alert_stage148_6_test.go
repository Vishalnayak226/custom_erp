package engines

import (
	"custom_erp/db"
	"testing"
	"time"
)

// Stage 148.6: the commitment meter counts the vendor's other open POs, and the
// expiry alert fires once per agreement end date.
func TestStage1486CommitmentMeterAndExpiryAlert(t *testing.T) {
	f := newAgreementFixture(t)
	defer f.cleanup()
	const (
		sku, v, ag = "TEST1486C-I", "TEST1486C-V", "TEST1486C-AG"
		po1        = "TEST1486C-PO1"
	)
	f.item(sku, "TEST1486C-D", "TEST1486C-C", 5)
	f.insert(v, "Vendor", "Active", map[string]interface{}{"code": v, "name": v})
	end := time.Now().AddDate(0, 0, 5).Format("2006-01-02")
	if d, err := time.Parse("2006-01-02", BusinessToday()); err == nil {
		end = d.AddDate(0, 0, 5).Format("2006-01-02")
	}
	f.agreement(ag, v, "2026-01-01", end, map[string]interface{}{"sku": sku, "price": 10, "commit_qty": 100})

	p := map[string]interface{}{"code": po1, "vendor": v, "gst_mode": GSTModeExclusive, "items": itemsJSON(map[string]interface{}{"sku": sku, "qty": 30})}
	if err := ApplyPurchaseOrderTotals("default", p); err != nil {
		t.Fatal(err)
	}
	f.insert(po1, "PurchaseOrder", "Draft", p)
	p2 := map[string]interface{}{"code": "TEST1486C-PO2", "vendor": v, "gst_mode": GSTModeExclusive, "items": itemsJSON(map[string]interface{}{"sku": sku, "qty": 20})}
	if err := ApplyPurchaseOrderTotals("default", p2); err != nil {
		t.Fatal(err)
	}
	l := poLinesOf(t, p2)[0]
	if numFromInterface(l["commit_qty"]) != 100 || numFromInterface(l["commit_used_qty"]) != 50 {
		t.Fatalf("meter = %v of %v, want 50 of 100", l["commit_used_qty"], l["commit_qty"])
	}

	for i := 0; i < 2; i++ {
		if err := RunVendorAgreementExpiryAlerts("default", f.schema); err != nil {
			t.Fatal(err)
		}
	}
	var stamped string
	db.DB.QueryRow("SELECT COALESCE(data->>'expiry_alerted_for','') FROM "+f.schema+".documents WHERE id = $1", ag).Scan(&stamped)
	if stamped != end {
		t.Fatalf("expiry_alerted_for = %q, want %q", stamped, end)
	}
}
