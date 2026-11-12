package engines

import (
	"custom_erp/db"
	"strings"
	"testing"
)

// Stage 148.50 - the advance refusals name the bill, vendor and advance, not raw ids.
func TestStage14850AdvanceRefusalsShowNamesNotIds(t *testing.T) {
	f := newCreditFixture(t)
	f.vendor("V-RAW1", map[string]interface{}{"name": "Sharma Traders"})
	f.vendor("V-RAW2", map[string]interface{}{"name": "Other Co"})
	f.advBill("BILL-RAW-9", "V-RAW2", 400)
	if _, err := db.DB.Exec("UPDATE " + f.schema + ".documents SET data = jsonb_set(data, '{invoice_number}', '\"INV/77\"') WHERE id = 'BILL-RAW-9'"); err != nil {
		t.Fatal(err)
	}
	adv, err := GiveVendorAdvance(f.tenantID, "u1", VendorAdvanceRequest{VendorID: "V-RAW1", Amount: 250})
	if err != nil {
		t.Fatal(err)
	}
	_, err = ApplyVendorAdvance(f.tenantID, "u1", adv.ID, "BILL-RAW-9", 0, "")
	if t5510Code(err) != "FIN-0923" {
		t.Fatalf("want FIN-0923, got %v", err)
	}
	msg := err.Error()
	for _, raw := range []string{adv.ID, "BILL-RAW-9"} {
		if strings.Contains(msg, raw) {
			t.Fatalf("message shows raw id %q: %s", raw, msg)
		}
	}
	for _, want := range []string{"Sharma Traders", "INV/77", "250.00"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("message lacks %q: %s", want, msg)
		}
	}
	f.advBill("BILL-RAW-1", "V-RAW1", 250)
	if _, err = ApplyVendorAdvance(f.tenantID, "u1", adv.ID, "BILL-RAW-1", 0, ""); err != nil {
		t.Fatal(err)
	}
	_, err = CancelVendorAdvance(f.tenantID, "u1", adv.ID)
	if t5510Code(err) != "FIN-0924" || strings.Contains(err.Error(), adv.ID) || !strings.Contains(err.Error(), "Sharma Traders") {
		t.Fatalf("cancel message: %v", err)
	}
}
