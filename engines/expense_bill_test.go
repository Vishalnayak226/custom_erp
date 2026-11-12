package engines

import (
	"custom_erp/db"
	"fmt"
	"testing"
)

// Stage 148.19: a vendor's bill with no PO.

type exp19 struct {
	*mc1481
	vendor string
}

func newExp19(t *testing.T) *exp19 {
	m := newMC1481(t)
	if err := SetSetting("default", SettingExpenseBills, "true", "t14819"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = SetSetting("default", SettingExpenseBills, "false", "t14819") })
	// A shared test database can hold a leftover VendorInvoice approval rule; park it so
	// each scenario decides for itself, and put it back after.
	if rs, err := db.DB.Query("SELECT id, min_amount, max_amount, required_role FROM " + m.schema + ".approval_rules WHERE doctype = 'VendorInvoice'"); err == nil {
		type rule struct {
			id   int
			min  float64
			max  *float64
			role string
		}
		var parked []rule
		for rs.Next() {
			var r rule
			if rs.Scan(&r.id, &r.min, &r.max, &r.role) == nil {
				parked = append(parked, r)
			}
		}
		rs.Close()
		db.DB.Exec("DELETE FROM " + m.schema + ".approval_rules WHERE doctype = 'VendorInvoice'")
		t.Cleanup(func() {
			db.DB.Exec("DELETE FROM " + m.schema + ".approval_rules WHERE doctype = 'VendorInvoice'")
			for _, r := range parked {
				db.DB.Exec("INSERT INTO "+m.schema+".approval_rules (id, doctype, min_amount, max_amount, required_role) VALUES ($1,'VendorInvoice',$2,$3,$4)", r.id, r.min, r.max, r.role)
			}
		})
	}
	e := &exp19{mc1481: m}
	e.vendor = m.id("V")
	m.doc(e.vendor, "Vendor", "Active", map[string]interface{}{"code": e.vendor, "name": "Landlord " + m.run})
	return e
}

func (e *exp19) costCentre(name string) string {
	id := e.id("CC" + name)
	e.doc(id, "CostCenter", "Active", map[string]interface{}{"code": id, "name": id})
	return id
}

func (e *exp19) input(number string, lines ...ExpenseBillLine) ExpenseBillInput {
	return ExpenseBillInput{VendorID: e.vendor, InvoiceNumber: number + "-" + e.run, Lines: lines}
}

func (e *exp19) create(in ExpenseBillInput) string {
	e.t.Helper()
	res, err := CreateExpenseBill(e.tenant, "system", in)
	if err != nil {
		e.t.Fatalf("create: %v", err)
	}
	e.docs = append(e.docs, res.InvoiceID)
	return res.InvoiceID
}

func exp19Code(err error) string { return t5510Code(err) }

func TestStage14819OffRefusesTheBill(t *testing.T) {
	e := newExp19(t)
	if err := SetSetting("default", SettingExpenseBills, "false", "t14819"); err != nil {
		t.Fatal(err)
	}
	_, err := CreateExpenseBill(e.tenant, "system", e.input("OFF", ExpenseBillLine{Account: "5100", Amount: 100}))
	if exp19Code(err) != "FIN-0930" {
		t.Fatalf("got %v, want FIN-0930", err)
	}
}

func TestStage14819PostsByAccountAndCostCentreWithBlockedGSTInCost(t *testing.T) {
	e := newExp19(t)
	a, b := e.costCentre("A"), e.costCentre("B")
	bill := e.create(e.input("RENT",
		ExpenseBillLine{Account: "5100", Amount: 1000, GSTRate: 18, ITC: "Eligible", CostCenter: a},
		ExpenseBillLine{Account: "5200", Amount: 500, GSTRate: 18, ITC: "Blocked", CostCenter: b}))
	d := e.billData(bill)
	if numFromInterface(d["invoice_amount"]) != 1770 || numFromInterface(d["gst_amount"]) != 180 || numFromInterface(d["itc_blocked_amount"]) != 90 {
		t.Fatalf("bill %v / gst %v / blocked %v, want 1770 / 180 / 90", d["invoice_amount"], d["gst_amount"], d["itc_blocked_amount"])
	}
	if got := e.gl(bill, "5100"); got != 100000 {
		t.Errorf("5100 %d, want 100000", got)
	}
	if got := e.gl(bill, "5200"); got != 59000 { // 500 plus its blocked 90 of GST
		t.Errorf("5200 %d, want 59000", got)
	}
	if got := e.gl(bill, "1500"); got != 18000 {
		t.Errorf("1500 %d, want 18000 (eligible GST only)", got)
	}
	if got := e.gl(bill, "2100"); got != -177000 {
		t.Errorf("2100 %d, want -177000", got)
	}
	e.wantBalanced(bill)
	var ccA int64
	if err := db.DB.QueryRow("SELECT COALESCE(SUM(debit),0) FROM "+e.schema+".gl_postings WHERE document_id = $1 AND cost_center = $2", bill, a).Scan(&ccA); err != nil || ccA != 118000 {
		t.Errorf("cost centre A carries %d (%v), want 118000", ccA, err)
	}
}

func TestStage14819RefusesWhatCannotBeBooked(t *testing.T) {
	e := newExp19(t)
	cases := []struct {
		name string
		in   ExpenseBillInput
		code string
	}{
		{"no vendor", ExpenseBillInput{InvoiceNumber: "x", Lines: []ExpenseBillLine{{Account: "5100", Amount: 1}}}, "FIN-0931"},
		{"unknown vendor", ExpenseBillInput{VendorID: "NOPE-" + e.run, InvoiceNumber: "x", Lines: []ExpenseBillLine{{Account: "5100", Amount: 1}}}, "FIN-0931"},
		{"no number", ExpenseBillInput{VendorID: e.vendor, Lines: []ExpenseBillLine{{Account: "5100", Amount: 1}}}, "FIN-0931"},
		{"no lines", e.input("N"), "FIN-0931"},
		{"zero amount", e.input("Z", ExpenseBillLine{Account: "5100"}), "FIN-0932"},
		{"bad rate", e.input("R", ExpenseBillLine{Account: "5100", Amount: 1, GSTRate: 130}), "FIN-0932"},
		{"bad credit", e.input("C", ExpenseBillLine{Account: "5100", Amount: 1, ITC: "Maybe"}), "FIN-0932"},
		{"not an expense account", e.input("A", ExpenseBillLine{Account: "2100", Amount: 1}), "FIN-0932"},
		{"missing account", e.input("M", ExpenseBillLine{Account: "5999X", Amount: 1}), "FIN-0932"},
		{"no account anywhere", e.input("B", ExpenseBillLine{Amount: 1}), "FIN-0932"},
		{"cost centre not set up", e.input("K", ExpenseBillLine{Account: "5100", Amount: 1, CostCenter: "NOPE-" + e.run}), "FIN-0932"},
	}
	for _, c := range cases {
		if _, err := CreateExpenseBill(e.tenant, "system", c.in); exp19Code(err) != c.code {
			t.Errorf("%s: got %v, want %s", c.name, err, c.code)
		}
	}
}

func TestStage14819SameBillAgainIsOneBill(t *testing.T) {
	e := newExp19(t)
	in := e.input("DUP", ExpenseBillLine{Account: "5100", Amount: 100})
	bill := e.create(in)
	res, err := CreateExpenseBill(e.tenant, "system", in)
	if err != nil || !res.Replayed || res.InvoiceID != bill {
		t.Fatalf("again: %v %+v", err, res)
	}
	if got := e.gl(bill, "2100"); got != -10000 {
		t.Errorf("2100 %d after a second entry, want -10000", got)
	}
	in.InvoiceNumber = in.InvoiceNumber + "   "
	if _, err := CreateExpenseBill(e.tenant, "system", in); err != nil {
		t.Errorf("same number with spaces: %v", err)
	}
}

func TestStage14819PaysLikeAnyBillAndClearsThePayable(t *testing.T) {
	e := newExp19(t)
	bill := e.create(e.input("PAY", ExpenseBillLine{Account: "5100", Amount: 1000, GSTRate: 18}))
	e.mustPay(bill)
	if e.gl(bill, "2100") != 0 || e.gl(bill, "1100") != -118000 || e.gl(bill, "1500") != 18000 || e.gl(bill, "5100") != 100000 {
		t.Errorf("2100 %d 1100 %d 1500 %d 5100 %d, want 0 / -118000 / 18000 / 100000",
			e.gl(bill, "2100"), e.gl(bill, "1100"), e.gl(bill, "1500"), e.gl(bill, "5100"))
	}
	e.wantBalanced(bill)
}

func TestStage14819PartPaymentKeepsTheBasis(t *testing.T) {
	e := newExp19(t)
	bill := e.create(e.input("PART", ExpenseBillLine{Account: "5100", Amount: 1000, GSTRate: 18}))
	if _, err := PayVendorInvoiceAmount(e.tenant, bill, "system", 400, ""); err != nil {
		t.Fatalf("part pay: %v", err)
	}
	if got := e.gl(bill, "2100"); got != -118000+40000 {
		t.Errorf("2100 %d after Rs 400 of 1,180, want -78000", got)
	}
	if got := e.gl(bill, "1500"); got != 18000 {
		t.Errorf("1500 %d, want 18000 (a part payment does not touch it)", got)
	}
}

func TestStage14819TDSIsOnTheValueWithoutGST(t *testing.T) {
	e := newExp19(t)
	sec := e.id("TDS")
	e.doc(sec, "TDSSection", "Active", map[string]interface{}{"code": sec, "threshold_amount": 0, "rate_percent": 10})
	bill := e.create(e.input("TDS", ExpenseBillLine{Account: "5100", Amount: 1000, GSTRate: 18}))
	net, tds, err := PayVendorInvoiceWithTDS(e.tenant, bill, sec, "system")
	if err != nil || tds != 100 || net != 1080 {
		t.Fatalf("tds pay: net %d tds %d err %v, want 1080 / 100", net, tds, err)
	}
	if e.gl(bill, "2300") != -10000 || e.gl(bill, "1100") != -108000 || e.gl(bill, "2100") != 0 || e.gl(bill, "1500") != 18000 {
		t.Errorf("2300 %d 1100 %d 2100 %d 1500 %d, want -10000 / -108000 / 0 / 18000",
			e.gl(bill, "2300"), e.gl(bill, "1100"), e.gl(bill, "2100"), e.gl(bill, "1500"))
	}
	e.wantBalanced(bill)
}

func TestStage14819ABillOverTheSlabWaitsForAnApprover(t *testing.T) {
	e := newExp19(t)
	rule, err := UpsertApprovalRule(e.tenant, "VendorInvoice", 50000, nil, "Finance Manager", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = DeleteApprovalRule(e.tenant, rule) })
	small := e.create(e.input("SMALL", ExpenseBillLine{Account: "5100", Amount: 1000}))
	if _, st, _ := fetchDocData(e.tenant, "VendorInvoice", small); st != "Matched" {
		t.Errorf("a small bill is %s, want Matched", st)
	}
	big := e.create(e.input("BIG", ExpenseBillLine{Account: "5100", Amount: 60000}))
	if _, st, _ := fetchDocData(e.tenant, "VendorInvoice", big); st != "Pending Approval" {
		t.Fatalf("a big bill is %s, want Pending Approval", st)
	}
	if e.gl(big, "5100") != 0 || e.gl(big, "2100") != 0 {
		t.Fatal("a bill awaiting approval has booked")
	}
	if _, _, err := PayVendorInvoice(e.tenant, big, "system", "Super Admin", ""); err == nil {
		t.Error("paid a bill that is not approved")
	}
	if _, err := db.DB.Exec("UPDATE "+e.schema+".documents SET status = 'Approved' WHERE id = $1", big); err != nil {
		t.Fatal(err)
	}
	if n, err := FinalizeVendorInvoiceOverridePayment(e.tenant, big, "checker"); err != nil || n != 0 {
		t.Fatalf("approve: %d %v", n, err)
	}
	if _, st, _ := fetchDocData(e.tenant, "VendorInvoice", big); st != "Matched" {
		t.Errorf("after approval %s, want Matched", st)
	}
	if e.gl(big, "5100") != 6000000 || e.gl(big, "2100") != -6000000 || e.gl(big, "1100") != 0 {
		t.Errorf("after approval 5100 %d 2100 %d 1100 %d, want 6000000 / -6000000 / 0", e.gl(big, "5100"), e.gl(big, "2100"), e.gl(big, "1100"))
	}
	e.mustPay(big)
	if e.gl(big, "2100") != 0 {
		t.Errorf("2100 %d after paying, want 0", e.gl(big, "2100"))
	}
}

func TestStage14819LinkAPOLaterChangesNoBooks(t *testing.T) {
	e := newExp19(t)
	bill := e.create(e.input("LINK", ExpenseBillLine{Account: "5100", Amount: 1000, GSTRate: 18}))
	own, other := e.id("PO"), e.id("PO")
	e.doc(own, "PurchaseOrder", "Approved", map[string]interface{}{"vendor": e.vendor, "vendor_id": e.vendor})
	e.doc(other, "PurchaseOrder", "Approved", map[string]interface{}{"vendor": "someone-else", "vendor_id": "someone-else"})
	if err := LinkExpenseBillToPO(e.tenant, "system", bill, other); exp19Code(err) != "FIN-0934" {
		t.Errorf("another vendor's PO: %v, want FIN-0934", err)
	}
	if err := LinkExpenseBillToPO(e.tenant, "system", bill, "NOPE-"+e.run); exp19Code(err) != "FIN-0934" {
		t.Errorf("missing PO: %v, want FIN-0934", err)
	}
	if err := LinkExpenseBillToPO(e.tenant, "system", bill, own); err != nil {
		t.Fatalf("link: %v", err)
	}
	if got := strField(e.billData(bill), "linked_po_id"); got != own {
		t.Errorf("linked_po_id %q, want %q", got, own)
	}
	if e.gl(bill, "2100") != -118000 {
		t.Errorf("2100 moved on link: %d", e.gl(bill, "2100"))
	}
	if err := LinkExpenseBillToPO(e.tenant, "system", "VI-NOPE-"+e.run, own); exp19Code(err) != "FIN-0934" {
		t.Errorf("not an expense bill: %v, want FIN-0934", err)
	}
}

func TestStage14819APlainSaveCannotMakeOrChangeOne(t *testing.T) {
	e := newExp19(t)
	if err := KeepExpenseBillMarks(e.tenant, "", map[string]interface{}{"bill_kind": "Expense"}); exp19Code(err) != "FIN-0933" {
		t.Errorf("typed-in bill: %v, want FIN-0933", err)
	}
	if err := KeepExpenseBillMarks(e.tenant, "", map[string]interface{}{"expense_lines": "[]"}); exp19Code(err) != "FIN-0933" {
		t.Errorf("typed-in lines: %v, want FIN-0933", err)
	}
	if err := KeepExpenseBillMarks(e.tenant, "", map[string]interface{}{"invoice_amount": 5}); err != nil {
		t.Errorf("an ordinary bill: %v", err)
	}
	bill := e.create(e.input("HAND", ExpenseBillLine{Account: "5100", Amount: 1000}))
	edit := map[string]interface{}{"invoice_amount": 1, "bill_kind": "Purchase", "expense_posted": "No", "narration": "kept"}
	if err := KeepExpenseBillMarks(e.tenant, bill, edit); err != nil {
		t.Fatal(err)
	}
	if edit["invoice_amount"] != e.billData(bill)["invoice_amount"] || edit["bill_kind"] != "Expense" || edit["expense_posted"] != "Yes" || edit["narration"] != "kept" {
		t.Errorf("edit kept %+v", edit)
	}
	other := e.id("VI")
	e.doc(other, "VendorInvoice", "Draft", map[string]interface{}{"vendor_id": e.vendor})
	if err := KeepExpenseBillMarks(e.tenant, other, map[string]interface{}{"bill_kind": "Expense"}); exp19Code(err) != "FIN-0933" {
		t.Errorf("turning a bill into one: %v, want FIN-0933", err)
	}
	if err := KeepExpenseBillMarks(e.tenant, bill, map[string]interface{}{"status": "Cancelled"}); err != nil {
		t.Errorf("cancelling an unpaid bill: %v", err)
	}
}

func TestStage14819CancelTakesTheBooksBackUnlessPaid(t *testing.T) {
	e := newExp19(t)
	a := e.costCentre("C")
	bill := e.create(e.input("CAN", ExpenseBillLine{Account: "5100", Amount: 1000, GSTRate: 18, CostCenter: a}))
	if err := ReverseVendorBillDifference(e.tenant, bill, "system"); err != nil {
		t.Fatalf("reverse: %v", err)
	}
	for _, acct := range []string{"5100", "1500", "2100"} {
		if got := e.gl(bill, acct); got != 0 {
			t.Errorf("%s %d after cancel, want 0", acct, got)
		}
	}
	e.wantBalanced(bill)
	if err := ReverseVendorBillDifference(e.tenant, bill, "system"); err != nil {
		t.Errorf("second cancel: %v", err)
	}
	paid := e.create(e.input("CANP", ExpenseBillLine{Account: "5100", Amount: 1000}))
	e.mustPay(paid)
	if err := ReverseVendorBillDifference(e.tenant, paid, "system"); exp19Code(err) != "FIN-0934" {
		t.Errorf("cancel of a paid bill: %v, want FIN-0934", err)
	}
	if err := KeepExpenseBillMarks(e.tenant, paid, map[string]interface{}{"status": "Cancelled"}); exp19Code(err) != "FIN-0934" {
		t.Errorf("save-cancel of a paid bill: %v, want FIN-0934", err)
	}
}

func TestStage14819TheVendorGroupRuleNamesTheAccount(t *testing.T) {
	e := newExp19(t)
	group := "G19-" + e.run
	if _, err := db.DB.Exec("UPDATE "+e.schema+".documents SET data = data || $2::jsonb WHERE id = $1", e.vendor, fmt.Sprintf(`{"vendor_group":%q}`, group)); err != nil {
		t.Fatal(err)
	}
	profile := e.id("PP")
	e.doc(profile, "PostingProfile", "Approved", map[string]interface{}{"code": profile, "event": PostingEventExpenseBill,
		"vendor_group": group, "debit_account": "5300", "status": "Approved"})
	bill := e.create(e.input("PROF", ExpenseBillLine{Amount: 200}))
	if got := e.gl(bill, "5300"); got != 20000 {
		t.Errorf("5300 %d, want 20000 from the posting rule", got)
	}
}

func TestStage14819ListShowsTheVendorNameAndOnlyExpenseBills(t *testing.T) {
	e := newExp19(t)
	bill := e.create(e.input("LIST", ExpenseBillLine{Account: "5100", Amount: 100, GSTRate: 18, ITC: "Blocked"}))
	plain := e.id("VI")
	e.doc(plain, "VendorInvoice", "Matched", map[string]interface{}{"vendor_id": e.vendor, "invoice_amount": 5})
	rows, err := ListExpenseBills(e.tenant)
	if err != nil {
		t.Fatal(err)
	}
	seen := false
	for _, r := range rows {
		if r.ID == plain {
			t.Error("a PO bill is on the expense list")
		}
		if r.ID == bill {
			seen = true
			if r.Vendor != "Landlord "+e.run || r.Amount != 118 || r.GSTBlocked != 18 || r.Outstanding != 118 {
				t.Errorf("row %+v", r)
			}
		}
	}
	if !seen {
		t.Error("the bill is not listed")
	}
}

func TestStage14819ATypedInExpenseBillCannotClearAPayable(t *testing.T) {
	e := newExp19(t)
	forged := e.id("VI")
	e.doc(forged, "VendorInvoice", "Matched", map[string]interface{}{"id": forged, "vendor_id": e.vendor, "invoice_amount": 500,
		"bill_kind": "Expense", "expense_posted": "Yes", "gst_total": 0})
	if _, _, err := PayVendorInvoice(e.tenant, forged, "system", "Super Admin", ""); exp19Code(err) != "FIN-0935" {
		t.Errorf("pay of a typed-in expense bill: %v, want FIN-0935", err)
	}
	if e.gl(forged, "2100") != 0 || e.gl(forged, "1100") != 0 {
		t.Error("a typed-in expense bill posted money")
	}
}
