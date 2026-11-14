package engines

import (
	"strings"
	"testing"
)

// Ask 169, 73.4.2.6: a claim is raised once per booking and moves
// Open -> Filed -> Settled only with its reference and an amount within the
// claim; a rejected claim needs a note.
func TestStage73426CarrierClaims(t *testing.T) {
	f := newCreditFixture(t)
	bk := NewDocID("BK")
	f.seed(bk, "LogisticsBooking", "Shipped", map[string]interface{}{"code": bk, "order_id": "SO-1", "carrier": "BlueDart",
		"tracking_number": "TRK-1", "awb_number": "AWB-1", "shipping_charge": 80.0, "status": "Shipped"})

	if _, err := RaiseCarrierClaim(f.tenantID, bk, "Stolen", 500, "", "mgr1"); codeOf(err) != "ORDERM-0471" {
		t.Fatalf("bad type: want ORDERM-0471, got %v", err)
	}
	claim, err := RaiseCarrierClaim(f.tenantID, bk, "Damaged", 500, "box crushed", "mgr1")
	if err != nil {
		t.Fatalf("raise: %v", err)
	}
	if c, a := f.field("CarrierClaim", claim, "carrier"), f.field("CarrierClaim", claim, "awb"); c != "BlueDart" || a != "AWB-1" {
		t.Fatalf("claim copied %q / %q from the booking", c, a)
	}
	if _, err := RaiseCarrierClaim(f.tenantID, bk, "Lost", 100, "", "mgr1"); codeOf(err) != "ORDERM-0471" {
		t.Fatalf("second open claim: want ORDERM-0471, got %v", err)
	}
	if err := MoveCarrierClaim(f.tenantID, claim, CarrierClaimMove{Action: "settle", Amount: 100}, "mgr1"); codeOf(err) != "ORDERM-0471" {
		t.Fatalf("settle before filing: want ORDERM-0471, got %v", err)
	}
	if err := MoveCarrierClaim(f.tenantID, claim, CarrierClaimMove{Action: "file"}, "mgr1"); codeOf(err) != "ORDERM-0471" {
		t.Fatalf("file with no reference: want ORDERM-0471, got %v", err)
	}
	if err := MoveCarrierClaim(f.tenantID, claim, CarrierClaimMove{Action: "file", ClaimRef: "BD-CL-77"}, "mgr1"); err != nil {
		t.Fatalf("file: %v", err)
	}
	if err := MoveCarrierClaim(f.tenantID, claim, CarrierClaimMove{Action: "settle", Amount: 600}, "mgr1"); codeOf(err) != "ORDERM-0471" {
		t.Fatalf("settle above the claim: want ORDERM-0471, got %v", err)
	}
	if err := MoveCarrierClaim(f.tenantID, claim, CarrierClaimMove{Action: "settle", Amount: 350, SettledOn: "2026-11-10"}, "mgr1"); err != nil {
		t.Fatalf("settle: %v", err)
	}
	if s, a, ref := f.field("CarrierClaim", claim, "status"), f.field("CarrierClaim", claim, "amount_settled"), f.field("CarrierClaim", claim, "claim_ref"); s != "Settled" || a != "350" || ref != "BD-CL-77" {
		t.Fatalf("settled claim: %s / %s / %s", s, a, ref)
	}
	if err := MoveCarrierClaim(f.tenantID, claim, CarrierClaimMove{Action: "reject", Note: "late"}, "mgr1"); codeOf(err) != "ORDERM-0471" {
		t.Fatalf("reject a settled claim: want ORDERM-0471, got %v", err)
	}

	// Once settled, a new claim on the booking may be raised, here by its AWB;
	// a reject needs a note.
	if _, err := RaiseCarrierClaim(f.tenantID, "AWB-NONE", "Short", 50, "", "mgr1"); codeOf(err) != "GLOBAL-0004" {
		t.Fatalf("unknown AWB: want GLOBAL-0004, got %v", err)
	}
	second, err := RaiseCarrierClaim(f.tenantID, "AWB-1", "Short", 50, "", "mgr1")
	if err != nil {
		t.Fatalf("raise after settle: %v", err)
	}
	if got := f.field("CarrierClaim", second, "booking_id"); got != bk {
		t.Fatalf("claim by AWB booked to %q, want %s", got, bk)
	}
	if err := MoveCarrierClaim(f.tenantID, second, CarrierClaimMove{Action: "reject"}, "mgr1"); codeOf(err) != "ORDERM-0471" {
		t.Fatalf("reject with no note: want ORDERM-0471, got %v", err)
	}
	if err := MoveCarrierClaim(f.tenantID, second, CarrierClaimMove{Action: "reject", Note: "count was right"}, "mgr1"); err != nil {
		t.Fatalf("reject: %v", err)
	}

	// A claim typed in or imported would skip these checks: refused.
	res, err := BulkImportCSV(f.tenantID, "CarrierClaim", strings.NewReader("booking_id,claim_type,amount_claimed,status\n"+bk+",Lost,10,Settled\n"), "admin", RoleSuperAdmin, true)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.SuccessRows != 0 || len(res.Errors) != 1 {
		t.Fatalf("an imported claim was not refused: %+v", res)
	}
}

// Ask 169, 73.4.2.7: each invoice line is matched to its booking by any AWB
// field; the expected charge is the provider's quote, else the shipping
// charge; outside the tolerance it is over or under; an AWB billed twice is a
// duplicate; an unknown AWB waits as No booking and matches once its booking
// exists.
func TestStage73427ReconcileFreightInvoices(t *testing.T) {
	f := newCreditFixture(t)
	if err := SetSetting(f.tenantID, SettingFreightTolerance, "2", "admin"); err != nil {
		t.Fatal(err)
	}
	quoted, plain := NewDocID("BK"), NewDocID("BK")
	f.seed(quoted, "LogisticsBooking", "Shipped", map[string]interface{}{"code": quoted, "order_id": "SO-1", "carrier": "BlueDart",
		"tracking_number": "TRK-Q", "provider_awb": "PAWB-Q", "shipping_charge": 80.0, "provider_charge_paise": 9500.0})
	f.seed(plain, "LogisticsBooking", "Shipped", map[string]interface{}{"code": plain, "order_id": "SO-2", "carrier": "BlueDart",
		"tracking_number": "TRK-P", "shipping_charge": 60.0})
	line := func(awb string, charged float64) string {
		id := NewDocID("FIL")
		f.seed(id, "FreightInvoiceLine", "Active", map[string]interface{}{"invoice_no": "INV-9", "carrier": "BlueDart", "awb": awb, "charged_amount": charged})
		return id
	}
	matched := line("PAWB-Q", 96.5) // 95 quoted, within 2
	over := line("TRK-P", 75)       // 60 charge, 15 over
	dup := line("PAWB-Q", 95)       // same booking billed again
	unknown := line("AWB-NEW", 40)

	res, err := ReconcileFreightInvoices(f.tenantID, "mgr1")
	if err != nil {
		t.Fatal(err)
	}
	if res.Checked != 4 || res.Matched != 1 || res.Overcharged != 1 || res.Duplicate != 1 || res.NoBooking != 1 {
		t.Fatalf("first run: %+v", res)
	}
	for id, want := range map[string]string{matched: "Matched", over: "Overcharged", dup: "Duplicate", unknown: "No booking"} {
		if got := f.field("FreightInvoiceLine", id, "status"); got != want {
			t.Fatalf("%s: want %s, got %s", id, want, got)
		}
	}
	if e, d := f.field("FreightInvoiceLine", over, "expected_amount"), f.field("FreightInvoiceLine", over, "difference"); e != "60" || d != "15" {
		t.Fatalf("over: expected %s difference %s", e, d)
	}

	// Its booking comes in: the waiting line matches on the next run; the
	// decided lines are left alone.
	late := NewDocID("BK")
	f.seed(late, "LogisticsBooking", "Shipped", map[string]interface{}{"code": late, "order_id": "SO-3", "carrier": "BlueDart",
		"tracking_number": "AWB-NEW", "shipping_charge": 40.0})
	res, err = ReconcileFreightInvoices(f.tenantID, "mgr1")
	if err != nil {
		t.Fatal(err)
	}
	if res.Checked != 1 || res.Matched != 1 {
		t.Fatalf("second run: %+v", res)
	}
	if got := f.field("FreightInvoiceLine", unknown, "booking_id"); got != late {
		t.Fatalf("late line booked to %q, want %s", got, late)
	}
}
