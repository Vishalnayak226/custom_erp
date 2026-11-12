package engines

import (
	"strings"
	"testing"
	"time"

	"custom_erp/db"
)

// Stage 148.9 - vendor master basics: hold (no new PO / no payment / both) at
// the shared choke points, terms (vendor default, end of month, MSME 45-day
// cap) with blank = today's due dates, and the payment run's early-payment
// discount.

func (f *creditFixture) vendor(id string, extra map[string]interface{}) {
	d := map[string]interface{}{"code": id, "name": "Vendor " + id, "status": "Active", "state": "Karnataka"}
	for k, v := range extra {
		d[k] = v
	}
	f.seed(id, "Vendor", "Active", d)
}

func (f *creditFixture) bill(id, vendor string, amount float64, invoiceDate string) {
	f.seed(id, "VendorInvoice", "Matched", map[string]interface{}{
		"code": id, "invoice_number": "N-" + id, "vendor_id": vendor, "invoice_amount": amount,
		"invoice_date": invoiceDate, "financial_year": "2026-27", "status": "Matched",
	})
}

func (f *creditFixture) glSum(doc, account, col string) int64 {
	var n int64
	_ = db.DB.QueryRow("SELECT COALESCE(SUM("+col+"), 0)::bigint FROM "+f.schema+".gl_postings WHERE document_id = $1 AND account_code = $2", doc, account).Scan(&n)
	return n
}

func TestStage1489HoldStopsPaymentOnEveryPath(t *testing.T) {
	f := newCreditFixture(t)
	f.vendor("V-HOLD", map[string]interface{}{"hold": "No payment"})
	f.vendor("V-BOTH", map[string]interface{}{"hold": "Both"})
	f.vendor("V-POONLY", map[string]interface{}{"hold": "No new PO"})
	f.bill("B-1", "V-HOLD", 1000, BusinessToday())
	f.bill("B-2", "V-HOLD", 1000, BusinessToday())
	f.bill("B-3", "V-BOTH", 1000, BusinessToday())
	f.bill("B-4", "V-POONLY", 1000, BusinessToday())

	// path 1: the manual pay
	if _, _, err := PayVendorInvoice(f.tenantID, "B-1", "u1", "Accountant", ""); err == nil || !strings.Contains(err.Error(), "on hold for payment") {
		t.Fatalf("manual pay of a held vendor's bill: want the hold refusal, got %v", err)
	}
	// path 2: a part payment
	if _, err := PayVendorInvoiceAmount(f.tenantID, "B-1", "u1", 400, "REF-1"); err == nil || !strings.Contains(err.Error(), "on hold for payment") {
		t.Fatalf("part payment of a held vendor's bill: want the hold refusal, got %v", err)
	}
	// path 3: a payment run
	pid, _, err := CreatePaymentProposal(f.tenantID, []string{"B-2", "B-3"}, "u1")
	if err != nil {
		t.Fatalf("CreatePaymentProposal: %v", err)
	}
	lines, err := ExecutePaymentProposal(f.tenantID, pid, "u1")
	if err != nil {
		t.Fatalf("ExecutePaymentProposal: %v", err)
	}
	for _, l := range lines {
		if l.Paid || !strings.Contains(l.Error, "on hold for payment") {
			t.Fatalf("payment run line %s: want the hold refusal, got paid=%v err=%q", l.InvoiceID, l.Paid, l.Error)
		}
	}
	for _, id := range []string{"B-1", "B-2", "B-3"} {
		if st := f.field("VendorInvoice", id, "status"); st != "Matched" {
			t.Fatalf("%s: a refused payment left it %s", id, st)
		}
		if n := f.glSum(id, "1100", "credit"); n != 0 {
			t.Fatalf("%s: a refused payment still posted %d to the bank", id, n)
		}
	}
	// a PO-only hold does not stop payment
	if _, _, err := PayVendorInvoice(f.tenantID, "B-4", "u1", "Accountant", ""); err != nil {
		t.Fatalf("a No-new-PO hold must not stop payment: %v", err)
	}
	// lifting the hold lets the bill through
	if _, err := db.DB.Exec("UPDATE " + f.schema + ".documents SET data = data - 'hold' WHERE id = 'V-HOLD'"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := PayVendorInvoice(f.tenantID, "B-1", "u1", "Accountant", ""); err != nil {
		t.Fatalf("pay after the hold is lifted: %v", err)
	}
}

func TestStage1489HoldStopsNewPurchaseOrders(t *testing.T) {
	f := newCreditFixture(t)
	f.vendor("V-NOPO", map[string]interface{}{"hold": "No new PO"})
	f.vendor("V-BOTH", map[string]interface{}{"hold": "Both"})
	f.vendor("V-PAYONLY", map[string]interface{}{"hold": "No payment"})
	f.vendor("V-FREE", nil)

	po := func(v, status string) map[string]interface{} {
		return map[string]interface{}{"vendor": v, "vendor_id": v, "status": status}
	}
	for _, v := range []string{"V-NOPO", "V-BOTH"} {
		err := RequirePOStates(f.tenantID, po(v, "Draft"))
		if codeOf(err) != VendorPOHoldCode {
			t.Fatalf("%s: a new PO must be refused with %s, got %v", v, VendorPOHoldCode, err)
		}
		// the submit-for-approval path (a draft saved before the hold)
		if err := runApprovalSubmitChecks(f.tenantID, "PurchaseOrder", "PO-X", po(v, "Draft")); codeOf(err) != VendorPOHoldCode {
			t.Fatalf("%s: submitting a draft must be refused with %s, got %v", v, VendorPOHoldCode, err)
		}
		// the record-save / import path
		if err := validateSupplyStateDocument(f.tenantID, "PurchaseOrder", po(v, "")); codeOf(err) != VendorPOHoldCode {
			t.Fatalf("%s: save / import must be refused with %s, got %v", v, VendorPOHoldCode, err)
		}
	}
	for _, v := range []string{"V-PAYONLY", "V-FREE"} {
		if err := RequirePOStates(f.tenantID, po(v, "Draft")); err != nil {
			t.Fatalf("%s: must still take new POs: %v", v, err)
		}
	}
	// an order already approved is not touched, so it can still be received and closed
	if err := RequirePOStates(f.tenantID, po("V-NOPO", "Approved")); err != nil {
		t.Fatalf("an Approved PO of a held vendor must not be refused: %v", err)
	}
}

func TestStage1489TermsDueDates(t *testing.T) {
	f := newCreditFixture(t)
	f.seed("NET30", "PaymentTerm", "Active", map[string]interface{}{"code": "NET30", "name": "Net 30", "net_days": 30.0})
	f.seed("EOM45", "PaymentTerm", "Active", map[string]interface{}{"code": "EOM45", "name": "EOM 45", "net_days": 45.0, "end_of_month": "Yes"})
	f.seed("NET60", "PaymentTerm", "Active", map[string]interface{}{"code": "NET60", "name": "Net 60", "net_days": 60.0})
	f.vendor("V-BLANK", nil)
	f.vendor("V-N30", map[string]interface{}{"payment_term": "NET30"})
	f.vendor("V-EOM", map[string]interface{}{"payment_term": "EOM45"})
	f.vendor("V-MSME", map[string]interface{}{"msme": "Yes", "payment_term": "NET60"})
	f.vendor("V-MSME0", map[string]interface{}{"msme": "Yes"})
	f.seed("PO-N30", "PurchaseOrder", "Approved", map[string]interface{}{"code": "PO-N30", "payment_term": "NET30", "vendor": "V-EOM"})
	f.seed("PO-NONE", "PurchaseOrder", "Approved", map[string]interface{}{"code": "PO-NONE", "vendor": "V-BLANK"})

	cases := []struct{ name, po, vendor, inv, accepted, want string }{
		{"blank terms: no date, as today", "PO-NONE", "V-BLANK", "2026-01-15", "", ""},
		{"blank terms with no PO: no date, as today", "", "V-BLANK", "2026-01-15", "", ""},
		{"PO term as today", "PO-N30", "V-BLANK", "2026-01-15", "", "2026-02-14"},
		{"PO term wins over the vendor's", "PO-N30", "V-EOM", "2026-01-15", "", "2026-02-14"},
		{"vendor default term when the PO has none", "PO-NONE", "V-N30", "2026-01-15", "", "2026-02-14"},
		{"end of month: month end plus net days", "", "V-EOM", "2026-01-15", "", "2026-03-17"},
		{"MSME caps a long term at 45 days from acceptance", "", "V-MSME", "2026-01-15", "2026-01-10", "2026-02-24"},
		{"MSME with no term is due at the cap", "", "V-MSME0", "2026-01-15", "2026-01-10", "2026-02-24"},
		{"MSME leaves a shorter term alone", "PO-N30", "V-MSME", "2026-01-15", "2026-01-10", "2026-02-14"},
	}
	for _, c := range cases {
		if got := VendorBillDueDate(f.tenantID, c.po, c.vendor, c.inv, c.accepted); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
	// the customer side shares the helper and keeps its meaning
	if got := paymentTermDueDate(f.tenantID, "NET30", "2026-01-15"); got != "2026-02-14" {
		t.Errorf("customer net term: got %q", got)
	}
	if got := POPaymentDueDate(f.tenantID, "PO-N30", "2026-01-15"); got != "2026-02-14" {
		t.Errorf("POPaymentDueDate: got %q", got)
	}
}

func TestStage1489EarlyPaymentDiscountInThePaymentRun(t *testing.T) {
	f := newCreditFixture(t)
	f.seed("2-10-N30", "PaymentTerm", "Active", map[string]interface{}{"code": "2-10-N30", "name": "2/10 net 30", "net_days": 30.0, "discount_days": 10.0, "discount_pct": 2.0})
	f.vendor("V-DISC", map[string]interface{}{"payment_term": "2-10-N30"})
	today := BusinessToday()
	old := time.Now().AddDate(0, 0, -20).Format("2006-01-02")
	f.bill("D-IN", "V-DISC", 1000, today)
	f.bill("D-LATE", "V-DISC", 1000, old)

	pid, _, err := CreatePaymentProposal(f.tenantID, []string{"D-IN", "D-LATE"}, "u1")
	if err != nil {
		t.Fatal(err)
	}
	lines, err := ExecutePaymentProposal(f.tenantID, pid, "u1")
	if err != nil {
		t.Fatal(err)
	}
	paid := map[string]int64{}
	for _, l := range lines {
		if !l.Paid {
			t.Fatalf("%s not paid: %s", l.InvoiceID, l.Error)
		}
		paid[l.InvoiceID] = l.AmountPaise
	}
	if paid["D-IN"] != 98000 {
		t.Fatalf("inside the window the run pays 980.00 (98000 paise), got %d", paid["D-IN"])
	}
	if paid["D-LATE"] != 100000 {
		t.Fatalf("outside the window the run pays the full 1000.00, got %d", paid["D-LATE"])
	}
	if n := f.glSum("D-IN", "5150", "credit"); n != 2000 {
		t.Fatalf("the discount is credited to 5150: got %d paise, want 2000", n)
	}
	if n := f.glSum("D-LATE", "5150", "credit"); n != 0 {
		t.Fatalf("a late payment takes no discount, 5150 got %d", n)
	}
	for _, id := range []string{"D-IN", "D-LATE"} {
		if st := f.field("VendorInvoice", id, "status"); st != "Paid" {
			t.Fatalf("%s should be Paid, is %s", id, st)
		}
	}
}
