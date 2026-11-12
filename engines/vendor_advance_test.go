package engines

import (
	"strings"
	"testing"

	"custom_erp/db"
)

// Stage 148.15 - a vendor advance against a purchase order: its own asset
// account (1810), the bill kept at its gross amount, a settle line that applies
// the advance, a refund for what is unused, cancel refused once used, and the
// period lock and the 148.9 payment hold on the shared posting choke point.

func (f *creditFixture) advBill(id, vendor string, amount float64) {
	f.t.Helper()
	f.bill(id, vendor, amount, BusinessToday())
	p := RupeesToPaise(amount)
	if err := PostDoubleEntry(f.tenantID, "VendorInvoice", id, map[string]int64{"1200": p}, map[string]int64{"2100": p}, BusinessToday(), "VendorInvoice:"+id+":BOOK"); err != nil {
		f.t.Fatalf("book %s: %v", id, err)
	}
}

func (f *creditFixture) net(doc, account string) int64 {
	return f.glSum(doc, account, "debit") - f.glSum(doc, account, "credit")
}

func (f *creditFixture) accountNet(account string) int64 {
	var n int64
	_ = db.DB.QueryRow("SELECT COALESCE(SUM(debit),0)::bigint - COALESCE(SUM(credit),0)::bigint FROM "+f.schema+".gl_postings WHERE account_code = $1", account).Scan(&n)
	return n
}

func TestStage14815AdvanceGivenAppliedPartlyAndRefunded(t *testing.T) {
	f := newCreditFixture(t)
	f.vendor("V-ADV", nil)
	f.advBill("B-A1", "V-ADV", 1000)
	f.advBill("B-A2", "V-ADV", 600)

	if _, err := GiveVendorAdvance(f.tenantID, "u1", VendorAdvanceRequest{VendorID: "V-ADV", Amount: 0}); t5510Code(err) != "FIN-0920" {
		t.Fatalf("zero advance: want FIN-0920, got %v", err)
	}
	if _, err := GiveVendorAdvance(f.tenantID, "u1", VendorAdvanceRequest{VendorID: "NOPE", Amount: 100}); t5510Code(err) != "FIN-0920" {
		t.Fatalf("unknown vendor: want FIN-0920, got %v", err)
	}
	adv, err := GiveVendorAdvance(f.tenantID, "u1", VendorAdvanceRequest{VendorID: "V-ADV", Amount: 1500, IdempotencyKey: "give-1"})
	if err != nil || adv.Status != "Open" || adv.OpenAmount != 1500 {
		t.Fatalf("give: %+v %v", adv, err)
	}
	if f.net(adv.ID, "1810") != 150000 || f.net(adv.ID, "1100") != -150000 {
		t.Fatalf("give posting: 1810 %d, 1100 %d", f.net(adv.ID, "1810"), f.net(adv.ID, "1100"))
	}
	again, err := GiveVendorAdvance(f.tenantID, "u1", VendorAdvanceRequest{VendorID: "V-ADV", Amount: 1500, IdempotencyKey: "give-1"})
	if err != nil || !again.Replayed || again.ID != adv.ID || f.glSum(adv.ID, "1810", "debit") != 150000 {
		t.Fatalf("a retried give must return the same advance and post nothing twice: %+v %v", again, err)
	}

	// An advance larger than the bill: it clears the bill and stays open for the rest.
	r1, err := ApplyVendorAdvance(f.tenantID, "u1", adv.ID, "B-A1", 0, "")
	if err != nil || r1.InvoiceStatus != "Paid" || r1.OpenAmount != 500 || r1.Status != "Part Applied" {
		t.Fatalf("apply to the first bill: %+v %v", r1, err)
	}
	if f.glSum("B-A1", "2100", "debit") != 100000 || f.net("B-A1", "2100") != 0 {
		t.Fatalf("2100 debit on the bill after apply: %d", f.net("B-A1", "2100"))
	}
	if f.field("VendorInvoice", "B-A1", "advance_applied") != "1000" {
		t.Fatalf("the bill must show what advance was applied: %q", f.field("VendorInvoice", "B-A1", "advance_applied"))
	}
	// A partial settle of the second bill, then a replay of the same reference.
	r2, err := ApplyVendorAdvance(f.tenantID, "u1", adv.ID, "B-A2", 200, "AP-2")
	if err != nil || r2.InvoiceStatus != "Matched" || r2.InvoiceOutstand != 400 || r2.OpenAmount != 300 {
		t.Fatalf("partial apply: %+v %v", r2, err)
	}
	dup, err := ApplyVendorAdvance(f.tenantID, "u1", adv.ID, "B-A2", 200, "AP-2")
	if err != nil || !dup.Replayed || f.glSum("B-A2", "1810", "credit") != 20000 {
		t.Fatalf("a retried apply must post nothing twice: %+v %v", dup, err)
	}
	// More than the advance holds, and a Paid bill, are refused.
	if _, err := ApplyVendorAdvance(f.tenantID, "u1", adv.ID, "B-A2", 400, "AP-3"); t5510Code(err) != "FIN-0922" {
		t.Fatalf("over the open advance: want FIN-0922, got %v", err)
	}
	if _, err := ApplyVendorAdvance(f.tenantID, "u1", adv.ID, "B-A1", 10, "AP-4"); t5510Code(err) != "FIN-0923" {
		t.Fatalf("a Paid bill: want FIN-0923, got %v", err)
	}
	// The rest of the second bill is paid in cash: only what is left.
	if _, _, err := PayVendorInvoice(f.tenantID, "B-A2", "u1", "Accountant", ""); err != nil {
		t.Fatalf("final pay: %v", err)
	}
	if f.glSum("B-A2", "1100", "credit") != 40000 || f.accountNet("2100") != 0 {
		t.Fatalf("cash paid on B-A2 = %d (want 40000); 2100 net = %d (want 0)", f.glSum("B-A2", "1100", "credit"), f.accountNet("2100"))
	}
	// Refund of the unused 300 clears 1810; a refund over the open amount is refused.
	if _, err := RefundVendorAdvance(f.tenantID, "u1", adv.ID, 400, "RF-0", ""); t5510Code(err) != "FIN-0922" {
		t.Fatalf("refund over the open amount: want FIN-0922, got %v", err)
	}
	rf, err := RefundVendorAdvance(f.tenantID, "u1", adv.ID, 0, "RF-1", "")
	if err != nil || rf.Status != "Applied" || rf.OpenAmount != 0 {
		t.Fatalf("refund: %+v %v", rf, err)
	}
	if f.accountNet("1810") != 0 {
		t.Fatalf("1810 must be clear after the advance is applied and refunded: %d", f.accountNet("1810"))
	}
	if _, err := CancelVendorAdvance(f.tenantID, "u1", adv.ID); t5510Code(err) != "FIN-0924" {
		t.Fatalf("cancel after apply: want FIN-0924, got %v", err)
	}
	// A TDS pay of a bill that took an advance is refused like any part-paid bill.
	f.advBill("B-A3", "V-ADV", 500)
	adv2, _ := GiveVendorAdvance(f.tenantID, "u1", VendorAdvanceRequest{VendorID: "V-ADV", Amount: 100})
	if _, err := ApplyVendorAdvance(f.tenantID, "u1", adv2.ID, "B-A3", 0, ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := PayVendorInvoiceWithTDS(f.tenantID, "B-A3", "194C", "u1"); err == nil || t5510Code(err) != "FIN-0521" {
		t.Fatalf("TDS pay after an advance: want FIN-0521, got %v", err)
	}
}

func TestStage14815AdvanceRefundCancelAndGuards(t *testing.T) {
	f := newCreditFixture(t)
	f.vendor("V-ONE", nil)
	f.vendor("V-TWO", nil)
	f.advBill("B-G1", "V-TWO", 300)
	adv, err := GiveVendorAdvance(f.tenantID, "u1", VendorAdvanceRequest{VendorID: "V-ONE", Amount: 400})
	if err != nil {
		t.Fatal(err)
	}
	// Another vendor's bill cannot take it.
	if _, err := ApplyVendorAdvance(f.tenantID, "u1", adv.ID, "B-G1", 0, ""); t5510Code(err) != "FIN-0923" {
		t.Fatalf("another vendor's bill: want FIN-0923, got %v", err)
	}
	// Refund of an unused advance clears it; a second refund is refused.
	rf, err := RefundVendorAdvance(f.tenantID, "u1", adv.ID, 0, "RF-A", "")
	if err != nil || rf.Status != "Refunded" {
		t.Fatalf("refund of an unused advance: %+v %v", rf, err)
	}
	if rep, err := RefundVendorAdvance(f.tenantID, "u1", adv.ID, 0, "RF-A", ""); err != nil || !rep.Replayed {
		t.Fatalf("a retried refund: %+v %v", rep, err)
	}
	if _, err := RefundVendorAdvance(f.tenantID, "u1", adv.ID, 0, "RF-B", ""); t5510Code(err) != "FIN-0925" {
		t.Fatalf("refund with nothing open: want FIN-0925, got %v", err)
	}
	// Cancel of an untouched advance reverses it; a repeat is a no-op.
	adv2, _ := GiveVendorAdvance(f.tenantID, "u1", VendorAdvanceRequest{VendorID: "V-ONE", Amount: 250})
	c, err := CancelVendorAdvance(f.tenantID, "u1", adv2.ID)
	if err != nil || c.Status != "Cancelled" || f.net(adv2.ID, "1810") != 0 || f.net(adv2.ID, "1100") != 0 {
		t.Fatalf("cancel: %+v %v (1810 %d)", c, err, f.net(adv2.ID, "1810"))
	}
	if rep, err := CancelVendorAdvance(f.tenantID, "u1", adv2.ID); err != nil || !rep.Replayed {
		t.Fatalf("a repeated cancel: %+v %v", rep, err)
	}
	if _, err := ApplyVendorAdvance(f.tenantID, "u1", adv2.ID, "B-G1", 0, ""); t5510Code(err) != "FIN-0925" {
		t.Fatalf("apply of a cancelled advance: want FIN-0925, got %v", err)
	}
	// An advance against a PO names the vendor's own, live PO only.
	f.seed("PO-G1", "PurchaseOrder", "Approved", map[string]interface{}{"code": "PO-G1", "vendor_id": "V-TWO", "status": "Approved"})
	if _, err := GiveVendorAdvance(f.tenantID, "u1", VendorAdvanceRequest{VendorID: "V-ONE", POID: "PO-G1", Amount: 50}); t5510Code(err) != "FIN-0921" {
		t.Fatalf("another vendor's PO: want FIN-0921, got %v", err)
	}
	if _, err := GiveVendorAdvance(f.tenantID, "u1", VendorAdvanceRequest{VendorID: "V-TWO", POID: "NO-PO", Amount: 50}); t5510Code(err) != "FIN-0921" {
		t.Fatalf("unknown PO: want FIN-0921, got %v", err)
	}
	pa, err := GiveVendorAdvance(f.tenantID, "u1", VendorAdvanceRequest{VendorID: "V-TWO", POID: "PO-G1", Amount: 100})
	if err != nil {
		t.Fatal(err)
	}
	// An advance against PO-G1 does not apply to a bill for another PO.
	if _, err := ApplyVendorAdvance(f.tenantID, "u1", pa.ID, "B-G1", 0, ""); t5510Code(err) != "FIN-0923" {
		t.Fatalf("bill without that PO: want FIN-0923, got %v", err)
	}
}

func TestStage14815PeriodLockAndPaymentHoldGuardTheAdvance(t *testing.T) {
	f := newCreditFixture(t)
	f.vendor("V-HOLDADV", map[string]interface{}{"hold": "No payment"})
	f.vendor("V-OKADV", nil)
	// 148.9 hold: no advance to a vendor on payment hold.
	_, err := GiveVendorAdvance(f.tenantID, "u1", VendorAdvanceRequest{VendorID: "V-HOLDADV", Amount: 100})
	if err == nil || !strings.Contains(err.Error(), "on hold for payment") {
		t.Fatalf("an advance to a vendor on payment hold: want the hold refusal, got %v", err)
	}
	var n int
	_ = db.DB.QueryRow("SELECT COUNT(*) FROM " + f.schema + ".documents WHERE doctype = 'VendorAdvance'").Scan(&n)
	if n != 0 {
		t.Fatalf("a refused advance left %d documents", n)
	}
	adv, err := GiveVendorAdvance(f.tenantID, "u1", VendorAdvanceRequest{VendorID: "V-OKADV", Amount: 100})
	if err != nil {
		t.Fatal(err)
	}
	// Period lock: a closed period refuses every posting the advance makes.
	if _, err := db.DB.Exec("INSERT INTO " + f.schema + ".accounting_periods (period_name, start_date, end_date, status, created_by) VALUES ('Closed adv', CURRENT_DATE - 1, CURRENT_DATE + 1, 'Closed', 'tester')"); err != nil {
		t.Fatal(err)
	}
	if _, err := GiveVendorAdvance(f.tenantID, "u1", VendorAdvanceRequest{VendorID: "V-OKADV", Amount: 70}); err == nil {
		t.Fatalf("an advance in a closed period must be refused")
	}
	if _, err := RefundVendorAdvance(f.tenantID, "u1", adv.ID, 0, "RF", ""); err == nil {
		t.Fatalf("a refund in a closed period must be refused")
	}
	if _, err := CancelVendorAdvance(f.tenantID, "u1", adv.ID); err == nil {
		t.Fatalf("a cancel in a closed period must be refused")
	}
	if f.accountNet("1810") != 10000 || f.field("VendorAdvance", adv.ID, "status") != "Open" {
		t.Fatalf("refused postings changed state: 1810 %d, status %s", f.accountNet("1810"), f.field("VendorAdvance", adv.ID, "status"))
	}
}

func TestStage14815VendorAdvanceMigrationSeedsAccountAndDoctype(t *testing.T) {
	f := newCreditFixture(t)
	var n int
	_ = db.DB.QueryRow("SELECT COUNT(*) FROM " + f.schema + ".gl_accounts WHERE account_code = '1810' AND account_type = 'Asset'").Scan(&n)
	if n != 1 {
		t.Fatalf("1810 Vendor Advances missing from a new tenant's chart")
	}
	_ = db.DB.QueryRow("SELECT COUNT(*) FROM " + f.schema + ".doctype_meta WHERE name = 'VendorAdvance'").Scan(&n)
	if n != 1 {
		t.Fatalf("VendorAdvance doctype missing")
	}
	_ = db.DB.QueryRow("SELECT COUNT(*) FROM " + f.schema + ".role_permissions WHERE doctype_name = 'VendorAdvance' AND (allow_create OR allow_update OR allow_delete)").Scan(&n)
	if n != 0 {
		t.Fatalf("VendorAdvance must be read-only to generic writes")
	}
}

// TestStage14838AdvanceWaitsForTheRetroPO: applying an advance settles a bill,
// so a bill for goods received before a PO waits for that PO here as well.
func TestStage14838AdvanceWaitsForTheRetroPO(t *testing.T) {
	f := newCreditFixture(t)
	f.vendor("V-RETRO", nil)
	f.seed("GRN-RETRO", "GRN", "Approved", map[string]interface{}{"code": "GRN-RETRO", "vendor": "V-RETRO", "received_before_po": "Yes"})
	f.advBill("B-RETRO", "V-RETRO", 1000)
	link := func(doc, field, value string) {
		t.Helper()
		if _, err := db.DB.Exec("UPDATE "+f.schema+".documents SET data = data || jsonb_build_object($2::text, $3::text) WHERE id = $1", doc, field, value); err != nil {
			t.Fatal(err)
		}
	}
	link("B-RETRO", "grn_id", "GRN-RETRO")
	adv, err := GiveVendorAdvance(f.tenantID, "u1", VendorAdvanceRequest{VendorID: "V-RETRO", Amount: 1000, IdempotencyKey: "give-retro"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyVendorAdvance(f.tenantID, "u1", adv.ID, "B-RETRO", 0, ""); t5510Code(err) != "PURCHA-5032" {
		t.Fatalf("waiting for the PO: want PURCHA-5032, got %v", err)
	}
	if f.net(adv.ID, "1810") != 100000 {
		t.Fatalf("a refused apply moved the advance: 1810 %d", f.net(adv.ID, "1810"))
	}
	link("GRN-RETRO", "retro_po_id", "PO-RETRO")
	if r, err := ApplyVendorAdvance(f.tenantID, "u1", adv.ID, "B-RETRO", 0, ""); err != nil || r.InvoiceStatus != "Paid" {
		t.Fatalf("after the PO: %+v %v", r, err)
	}
}
