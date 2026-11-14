package engines

import (
	"custom_erp/db"
	"strings"
	"testing"
)

// Stage 73.4.2.4: the order risk check holds a new order with FRAUD_RISK, a
// person's release clears it once, and an order held for something else first
// stops at the risk hold when that other hold is released.
func TestStage73424RiskHold(t *testing.T) {
	f := newCreditFixture(t)
	set := func(k, v string) {
		t.Helper()
		if err := SetSetting(f.tenantID, k, v, "admin"); err != nil {
			t.Fatalf("SetSetting %s: %v", k, err)
		}
	}
	place := func(qty int, phone, pay string) string {
		t.Helper()
		id, err := CreateSalesOrder(f.tenantID, SalesOrderInput{Channel: "Manual", CustomerName: "Walk In", CustomerPhone: phone,
			ShippingAddress: "12 MG Road, Bengaluru, Karnataka 560001", PaymentStatus: pay,
			Lines: []SalesOrderLineInput{{SKU: "ITEM-CC", Qty: qty, UnitPrice: 500}}})
		if err != nil {
			t.Fatalf("CreateSalesOrder: %v", err)
		}
		return id
	}
	state := func(id string) string {
		return f.field("SalesOrder", id, "order_status") + "/" + f.field("SalesOrder", id, "hold_reason")
	}

	// Switch off: rules set, nothing held.
	set(SettingRiskCODAbove, "2000")
	if o := place(10, "", "COD"); state(o) != "Reserved/" {
		t.Fatalf("switch off: got %s", state(o))
	}

	set(SettingRiskHold, "true")
	big := place(10, "", "COD")
	if state(big) != "On Hold/"+HoldFraudRisk || !strings.Contains(f.field("SalesOrder", big, "risk_reasons"), "COD above 2000") {
		t.Fatalf("COD rule: got %s %q", state(big), f.field("SalesOrder", big, "risk_reasons"))
	}
	if o := place(2, "", "COD"); state(o) != "Reserved/" {
		t.Fatalf("under the limit: got %s", state(o))
	}
	if err := ReleaseOrderHoldAs(f.tenantID, big, "mgr1"); err != nil {
		t.Fatal(err)
	}
	if state(big) != "Reserved/" || f.field("SalesOrder", big, "risk_cleared_by") != "mgr1" {
		t.Fatalf("release: got %s by %q", state(big), f.field("SalesOrder", big, "risk_cleared_by"))
	}

	// Repeat orders from one phone: the third in 24 hours is held.
	set(SettingRiskOrdersPerPhone, "2")
	place(1, "98765 43210", "COD")
	place(1, "+91 9876543210", "COD")
	third := place(1, "9876543210", "COD")
	if state(third) != "On Hold/"+HoldFraudRisk || !strings.Contains(f.field("SalesOrder", third, "risk_reasons"), "3 orders from this phone") {
		t.Fatalf("phone rule: got %s %q", state(third), f.field("SalesOrder", third, "risk_reasons"))
	}
	set(SettingRiskOrdersPerPhone, "0")

	// Block list: a PIN code. Payment pending comes first; once paid, the
	// release stops at the risk hold, and the next release reserves.
	set(SettingRiskBlocked, "560001")
	pend := place(1, "", "Pending")
	if state(pend) != "On Hold/"+HoldPaymentPending || f.field("SalesOrder", pend, "risk_reasons") != "blocked PIN code" {
		t.Fatalf("pending + blocked: got %s %q", state(pend), f.field("SalesOrder", pend, "risk_reasons"))
	}
	if _, err := db.DB.Exec(`UPDATE `+f.schema+`.documents SET data = jsonb_set(data, '{payment_status}', '"Confirmed"') WHERE id = $1`, pend); err != nil {
		t.Fatal(err)
	}
	if err := ReleaseOrderHold(f.tenantID, pend); err != nil {
		t.Fatal(err)
	}
	if state(pend) != "On Hold/"+HoldFraudRisk {
		t.Fatalf("paid: want the risk hold, got %s", state(pend))
	}
	if err := ReleaseOrderHoldAs(f.tenantID, pend, "mgr1"); err != nil {
		t.Fatal(err)
	}
	if state(pend) != "Reserved/" {
		t.Fatalf("risk released: got %s", state(pend))
	}
}
