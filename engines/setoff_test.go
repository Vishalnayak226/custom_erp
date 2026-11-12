package engines

import (
	"encoding/json"
	"testing"

	"custom_erp/db"
)

// Stage 148.23: customer-vendor set-off.

type setOffFixture struct {
	*creditFixture
}

func (f setOffFixture) invoice(id, customer string, amount float64, date string) {
	f.t.Helper()
	items, _ := json.Marshal([]map[string]interface{}{{"sku": "ITEM-CC", "qty": 1, "rate": amount, "amount": amount}})
	f.seed(id, "SalesInvoice", "Draft", map[string]interface{}{"customer": customer, "location": "HO", "items": string(items), "total_amount": amount, "invoice_date": date})
	if _, err := PostSalesInvoice(f.tenantID, id, "manager1"); err != nil {
		f.t.Fatalf("post %s: %v", id, err)
	}
}

func (f setOffFixture) net(doc, account string) int64 {
	return f.glSum(doc, account, "debit") - f.glSum(doc, account, "credit")
}

func (f setOffFixture) accountNet(account string) int64 {
	var n int64
	_ = db.DB.QueryRow("SELECT COALESCE(SUM(debit),0)::bigint - COALESCE(SUM(credit),0)::bigint FROM "+f.schema+".gl_postings WHERE account_code = $1", account).Scan(&n)
	return n
}

func (f setOffFixture) outstanding(invoice string) string {
	return f.field("SalesInvoice", invoice, "outstanding_amount")
}

// newSetOffFixture: a customer and a vendor sharing a GSTIN, two open invoices
// (1000 old, 500 newer), two open bills (700 old, 400 newer) and a clearing
// account row.
func newSetOffFixture(t *testing.T) setOffFixture {
	f := setOffFixture{newCreditFixture(t)}
	gstin := "29ABCDE1234F1Z5"
	f.customer("C-SO", "Acme Retail", map[string]interface{}{"gstin": gstin})
	f.vendor("V-SO", map[string]interface{}{"gstin": gstin})
	f.vendor("V-OTHER", map[string]interface{}{"gstin": "29ZZZZZ9999Z1Z1"})
	f.invoice("SI-SO-1", "C-SO", 1000, "2026-09-01")
	f.invoice("SI-SO-2", "C-SO", 500, "2026-09-15")
	f.seed("B-SO-1", "VendorInvoice", "Matched", map[string]interface{}{"code": "B-SO-1", "vendor_id": "V-SO", "invoice_amount": 700.0, "invoice_date": "2026-09-02", "status": "Matched"})
	f.seed("B-SO-2", "VendorInvoice", "Matched", map[string]interface{}{"code": "B-SO-2", "vendor_id": "V-SO", "invoice_amount": 400.0, "invoice_date": "2026-09-20", "status": "Matched"})
	for id, p := range map[string]int64{"B-SO-1": 70000, "B-SO-2": 40000} {
		if err := PostDoubleEntry(f.tenantID, "VendorInvoice", id, map[string]int64{"1200": p}, map[string]int64{"2100": p}, BusinessToday(), "VendorInvoice:"+id+":BOOK"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.DB.Exec("INSERT INTO " + f.schema + ".gl_accounts (account_code, account_name, account_type) VALUES ('2950', 'Set-off Clearing', 'Liability') ON CONFLICT DO NOTHING"); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f setOffFixture) pair(id string) {
	f.t.Helper()
	p := map[string]interface{}{"customer_id": "C-SO", "vendor_id": "V-SO"}
	if err := validatePartyPair(f.tenantID, id, nil, p); err != nil {
		f.t.Fatalf("pair: %v", err)
	}
	p["code"] = id
	f.seed(id, PartyPairDoctype, "Active", p)
}

func (f setOffFixture) voucher(id string, amount float64) error {
	f.t.Helper()
	p := map[string]interface{}{"pair_id": "PAIR-SO", "amount": amount, "voucher_date": BusinessToday(), "status": "Draft"}
	if err := validateSetOffVoucher(f.tenantID, id, "", nil, p); err != nil {
		return err
	}
	p["code"] = id
	raw, _ := json.Marshal(p)
	if _, err := db.DB.Exec("INSERT INTO "+f.schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, 'Draft', 'accountant1')", id, SetOffVoucherDoctype, raw); err != nil {
		f.t.Fatal(err)
	}
	return nil
}

func (f setOffFixture) approve(id string) {
	f.t.Helper()
	if err := SubmitForApproval(f.tenantID, SetOffVoucherDoctype, id, "accountant1", "Accountant"); err != nil {
		f.t.Fatalf("submit: %v", err)
	}
	if err := DecideApproval(f.tenantID, SetOffVoucherDoctype, id, "admin", "HR/Admin", "HO", "Approved", "ok"); err != nil {
		f.t.Fatalf("approve: %v", err)
	}
}

// A suggestion is only a suggestion: it appears when the GSTIN matches, makes
// no pair, and disappears once a person makes the pair.
func TestStage14823PairSuggestedNeverMadeSilently(t *testing.T) {
	f := newSetOffFixture(t)
	got, err := PartyPairSuggestions(f.tenantID)
	if err != nil || len(got) != 1 || got[0].CustomerID != "C-SO" || got[0].VendorID != "V-SO" || got[0].Basis != "GSTIN" {
		t.Fatalf("suggestions: %+v %v", got, err)
	}
	var n int
	_ = db.DB.QueryRow("SELECT COUNT(*) FROM "+f.schema+".documents WHERE doctype = $1", PartyPairDoctype).Scan(&n)
	if n != 0 {
		t.Fatalf("a suggestion must not create a pair, found %d", n)
	}
	f.pair("PAIR-SO")
	if got, _ := PartyPairSuggestions(f.tenantID); len(got) != 0 {
		t.Fatalf("a paired couple is not suggested again: %+v", got)
	}
	// The same customer cannot be in a second active pair.
	p := map[string]interface{}{"customer_id": "C-SO", "vendor_id": "V-OTHER"}
	if err := validatePartyPair(f.tenantID, "PAIR-2", nil, p); codeOf(err) != setOffPairCode {
		t.Fatalf("second pair for the customer: want %s, got %v", setOffPairCode, err)
	}
	if err := validatePartyPair(f.tenantID, "PAIR-3", nil, map[string]interface{}{"customer_id": "NOPE", "vendor_id": "V-SO"}); codeOf(err) != setOffPairCode {
		t.Fatalf("unknown customer: %v", err)
	}
}

// The whole path: caps, approval, posting through the clearing account, both
// balances down, both ledgers, statement, reversal.
func TestStage14823SetOffPostsReducesBothAndReverses(t *testing.T) {
	f := newSetOffFixture(t)
	f.pair("PAIR-SO")

	// No clearing account yet: refused.
	if err := f.voucher("SOV-0", 100); codeOf(err) != setOffClearingCode {
		t.Fatalf("no clearing account: want %s, got %v", setOffClearingCode, err)
	}
	if err := SetSetting(f.tenantID, SettingSetOffClearingAccount, "2950", "admin"); err != nil {
		t.Fatal(err)
	}
	bal, err := GetSetOffBalances(f.tenantID, "PAIR-SO")
	if err != nil || bal.CustomerOwes != 1500 || bal.WeOweVendor != 1100 || bal.MaxSetOff != 1100 {
		t.Fatalf("balances: %+v %v", bal, err)
	}
	// Capped by both balances.
	if err := f.voucher("SOV-0", 1200); codeOf(err) != setOffCapCode {
		t.Fatalf("over the vendor balance: want %s, got %v", setOffCapCode, err)
	}
	// A typed-in approved voucher is refused.
	if err := validateSetOffVoucher(f.tenantID, "SOV-X", "", nil, map[string]interface{}{"pair_id": "PAIR-SO", "amount": 10.0, "status": "Approved"}); codeOf(err) != setOffForgedCode {
		t.Fatalf("typed-in Approved: want %s, got %v", setOffForgedCode, err)
	}

	if err := f.voucher("SOV-1", 900); err != nil {
		t.Fatalf("voucher: %v", err)
	}
	// Nothing posts before approval.
	if f.accountNet("2950") != 0 || f.outstanding("SI-SO-1") != "" {
		t.Fatalf("nothing may post before approval")
	}
	// The maker cannot approve their own set-off (dual control).
	if err := SubmitForApproval(f.tenantID, SetOffVoucherDoctype, "SOV-1", "accountant1", "Accountant"); err != nil {
		t.Fatal(err)
	}
	if err := DecideApproval(f.tenantID, SetOffVoucherDoctype, "SOV-1", "accountant1", "Accountant", "HO", "Approved", "ok"); err == nil {
		t.Fatal("the maker must not approve their own set-off")
	}
	if err := DecideApproval(f.tenantID, SetOffVoucherDoctype, "SOV-1", "admin", "HR/Admin", "HO", "Approved", "ok"); err != nil {
		t.Fatalf("approve: %v", err)
	}

	// Customer side: the oldest invoice takes all 900. Vendor side: the oldest
	// bill is cleared and the rest goes on the next.
	if f.outstanding("SI-SO-1") != "100" || f.outstanding("SI-SO-2") != "" {
		t.Fatalf("customer balances: SI-1 %q SI-2 %q", f.outstanding("SI-SO-1"), f.outstanding("SI-SO-2"))
	}
	if f.net("SI-SO-1", "1300") != 100000-90000 || f.glSum("SI-SO-1", "2950", "debit") != 90000 {
		t.Fatalf("customer legs: 1300 net %d, clearing debit %d", f.net("SI-SO-1", "1300"), f.glSum("SI-SO-1", "2950", "debit"))
	}
	if f.field("VendorInvoice", "B-SO-1", "status") != "Paid" || f.field("VendorInvoice", "B-SO-2", "outstanding_amount") != "200" {
		t.Fatalf("vendor balances: B-1 %q B-2 outstanding %q", f.field("VendorInvoice", "B-SO-1", "status"), f.field("VendorInvoice", "B-SO-2", "outstanding_amount"))
	}
	if f.net("B-SO-1", "2100") != 0 || f.glSum("B-SO-1", "2950", "credit") != 70000 || f.glSum("B-SO-2", "2950", "credit") != 20000 {
		t.Fatalf("vendor legs: B-1 2100 net %d, clearing credits %d/%d", f.net("B-SO-1", "2100"), f.glSum("B-SO-1", "2950", "credit"), f.glSum("B-SO-2", "2950", "credit"))
	}
	if f.accountNet("2950") != 0 {
		t.Fatalf("the clearing account must net to zero, got %d", f.accountNet("2950"))
	}
	after, _ := GetSetOffBalances(f.tenantID, "PAIR-SO")
	if after.CustomerOwes != 600 || after.WeOweVendor != 200 {
		t.Fatalf("both open balances must fall by 900: %+v", after)
	}

	// Posting again changes nothing.
	if err := postSetOff(f.tenantID, "SOV-1", "admin"); err != nil || f.glSum("SI-SO-1", "2950", "debit") != 90000 {
		t.Fatalf("a second post must be a no-op: %v", err)
	}

	st, err := GetSetOffStatement(f.tenantID, "SOV-1")
	if err != nil || st.Amount != 900 || st.CustomerBefore != 1500 || st.CustomerAfter != 600 || st.VendorBefore != 1100 || st.VendorAfter != 200 ||
		len(st.Invoices) != 1 || len(st.Bills) != 2 || st.ClearingAccount != "2950" || st.CustomerName != "Acme Retail" {
		t.Fatalf("statement: %+v %v", st, err)
	}

	// Reverse: balances come back, the clearing account still nets to zero, once only.
	if err := ReverseSetOff(f.tenantID, "admin", "SOV-1", "2000-01-01"); codeOf(err) != setOffReverseCode {
		t.Fatalf("reversal before the set-off: want %s, got %v", setOffReverseCode, err)
	}
	if err := ReverseSetOff(f.tenantID, "admin", "SOV-1", "2999-01-01"); codeOf(err) != setOffReverseCode {
		t.Fatalf("reversal dated ahead: want %s, got %v", setOffReverseCode, err)
	}
	if err := ReverseSetOff(f.tenantID, "admin", "SOV-1", BusinessToday()); err != nil {
		t.Fatalf("reverse: %v", err)
	}
	back, _ := GetSetOffBalances(f.tenantID, "PAIR-SO")
	if back.CustomerOwes != 1500 || back.WeOweVendor != 1100 {
		t.Fatalf("reversal must give both balances back: %+v", back)
	}
	if f.field("VendorInvoice", "B-SO-1", "status") != "Matched" || f.accountNet("2950") != 0 || f.net("SI-SO-1", "1300") != 100000 || f.net("B-SO-1", "2100") != -70000 {
		t.Fatalf("after reversal: B-1 %q, clearing %d, 1300 %d, 2100 %d", f.field("VendorInvoice", "B-SO-1", "status"), f.accountNet("2950"), f.net("SI-SO-1", "1300"), f.net("B-SO-1", "2100"))
	}
	if err := ReverseSetOff(f.tenantID, "admin", "SOV-1", ""); codeOf(err) != setOffReverseCode {
		t.Fatalf("a second reversal: want %s, got %v", setOffReverseCode, err)
	}
	if f.field(SetOffVoucherDoctype, "SOV-1", "status") != "Reversed" {
		t.Fatalf("voucher must read Reversed")
	}
	// A voucher past Draft cannot be edited through a generic save.
	if err := validateSetOffVoucher(f.tenantID, "SOV-1", "Reversed", map[string]interface{}{"pair_id": "PAIR-SO"}, map[string]interface{}{"pair_id": "PAIR-SO", "amount": 1.0}); codeOf(err) != setOffForgedCode {
		t.Fatalf("edit after posting: want %s, got %v", setOffForgedCode, err)
	}
}

// A voucher approved after a balance fell below it does not post, and is not
// left looking posted.
func TestStage14823ApprovalAfterBalanceFellDoesNotPost(t *testing.T) {
	f := newSetOffFixture(t)
	f.pair("PAIR-SO")
	if err := SetSetting(f.tenantID, SettingSetOffClearingAccount, "2950", "admin"); err != nil {
		t.Fatal(err)
	}
	if err := f.voucher("SOV-2", 1000); err != nil {
		t.Fatal(err)
	}
	if err := SubmitForApproval(f.tenantID, SetOffVoucherDoctype, "SOV-2", "accountant1", "Accountant"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := PayVendorInvoice(f.tenantID, "B-SO-1", "u1", "Accountant", ""); err != nil { // vendor balance 1100 -> 400
		t.Fatalf("pay: %v", err)
	}
	if err := DecideApproval(f.tenantID, SetOffVoucherDoctype, "SOV-2", "admin", "HR/Admin", "HO", "Approved", "ok"); err != nil {
		t.Fatal(err)
	}
	if f.field(SetOffVoucherDoctype, "SOV-2", "status") != "Rejected" || f.field(SetOffVoucherDoctype, "SOV-2", "post_error") == "" || f.accountNet("2950") != 0 || f.outstanding("SI-SO-1") != "" {
		t.Fatalf("a voucher that cannot post must read Rejected with a reason and post nothing: %q %q",
			f.field(SetOffVoucherDoctype, "SOV-2", "status"), f.field(SetOffVoucherDoctype, "SOV-2", "post_error"))
	}
}

// A set-off in a closed period is stopped by the same period lock as any posting.
func TestStage14823SetOffHonoursClosedPeriod(t *testing.T) {
	f := newSetOffFixture(t)
	f.pair("PAIR-SO")
	if err := SetSetting(f.tenantID, SettingSetOffClearingAccount, "2950", "admin"); err != nil {
		t.Fatal(err)
	}
	if err := f.voucher("SOV-3", 100); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec("INSERT INTO "+f.schema+".accounting_periods (period_name, start_date, end_date, status, created_by) VALUES ('P-SO', $1::date - 1, $1::date + 1, 'Closed', 'admin')", BusinessToday()); err != nil {
		t.Fatal(err)
	}
	f.approve("SOV-3")
	if f.field(SetOffVoucherDoctype, "SOV-3", "posted_at") != "" || f.accountNet("2950") != 0 {
		t.Fatalf("a closed period must stop the set-off")
	}
}
