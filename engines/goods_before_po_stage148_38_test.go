package engines

import (
	"custom_erp/db"
	"fmt"
	"strings"
	"testing"
)

func gbpCode(err error) string {
	if v, ok := err.(*ValidationError); ok {
		return v.Code
	}
	return ""
}

func mentionsPO(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "po_id") || strings.Contains(err.Error(), "PO Reference"))
}

// 148.38: the mandatory PO is waived only when the setting is on and the
// receipt carries a real reason code; a forged flag alone waives nothing, in
// the editor's validator or the import's (both call ValidateDocument).
func TestStage14838ForgedFlagNeverWaivesThePO(t *testing.T) {
	tenantID, _, _ := poRulesTenant(t)
	doc := func(extra map[string]interface{}) map[string]interface{} {
		d := map[string]interface{}{"code": "GRN-V1", "vendor": "V-LOCAL", "location": "HO", "received_items": `[{"sku":"I-MISC","qty":1}]`}
		for k, v := range extra {
			d[k] = v
		}
		return d
	}
	flagged := map[string]interface{}{"received_before_po": "Yes", "before_po_reason": "GBP-URGENT"}
	// Off (the default): refused exactly as before, flag or no flag.
	if err := ValidateDocument(tenantID, "GRN", doc(flagged)); !mentionsPO(err) {
		t.Fatalf("off: %v, want the PO refusal", err)
	}
	if err := ValidateDocument(tenantID, "GRN", doc(nil)); !mentionsPO(err) {
		t.Fatalf("off, plain: %v, want the PO refusal", err)
	}
	if err := SetSetting(tenantID, SettingGoodsBeforePO, "true", "test"); err != nil {
		t.Fatal(err)
	}
	// On but the flag is missing, the reason is of another kind, or it does not exist.
	for name, extra := range map[string]map[string]interface{}{
		"no flag":      {"before_po_reason": "GBP-URGENT"},
		"wrong kind":   {"received_before_po": "Yes", "before_po_reason": "GRN-REJ-QUALITY"},
		"unknown code": {"received_before_po": "Yes", "before_po_reason": "NOPE"},
		"no reason":    {"received_before_po": "Yes"},
	} {
		if err := ValidateDocument(tenantID, "GRN", doc(extra)); !mentionsPO(err) {
			t.Errorf("%s: %v, want the PO refusal", name, err)
		}
	}
	if err := ValidateDocument(tenantID, "GRN", doc(flagged)); mentionsPO(err) {
		t.Fatalf("on, flagged, valid reason: still refused for the PO: %v", err)
	}
	// The create path: no reason is its own code, a good one stamps the due date.
	p := map[string]interface{}{"vendor": "V-LOCAL"}
	if err := prepareGRNBeforePO(tenantID, p); gbpCode(err) != "PURCHA-5030" {
		t.Fatalf("no reason: %v, want PURCHA-5030", err)
	}
	p = map[string]interface{}{"before_po_reason": "GBP-URGENT"}
	if err := prepareGRNBeforePO(tenantID, p); gbpCode(err) != "PURCHA-5031" {
		t.Fatalf("no vendor: %v, want PURCHA-5031", err)
	}
	p = map[string]interface{}{"vendor": "V-LOCAL", "before_po_reason": "GBP-URGENT"}
	if err := prepareGRNBeforePO(tenantID, p); err != nil || p["received_before_po"] != "Yes" || strField(p, "before_po_due") <= BusinessToday() {
		t.Fatalf("on: %v %v", err, p)
	}
}

// 148.38: no bill for goods received before a PO is paid until the retro PO is
// approved - by any of the ways a bill is paid - and approval lifts it and
// links the receipt.
func TestStage14838BillBlockedUntilRetroPOApproved(t *testing.T) {
	tenantID, schema, insert := poRulesTenant(t)
	insert("GRN-B1", "GRN", "Approved", map[string]interface{}{"code": "GRN-B1", "vendor": "V-LOCAL", "received_before_po": "Yes", "before_po_reason": "GBP-URGENT",
		"before_po_due": BusinessToday(), "received_items": `[{"sku":"I-MISC","qty":1}]`})
	for _, id := range []string{"VI-B1", "VI-B2", "VI-B3", "VI-B4"} {
		st := "Matched"
		if id == "VI-B4" {
			st = "Approved" // an override approved, waiting to be paid
		}
		insert(id, "VendorInvoice", st, map[string]interface{}{"code": id, "vendor": "V-LOCAL", "grn_id": "GRN-B1", "invoice_amount": 1000.0})
	}
	blocked := func(name string, err error) {
		t.Helper()
		if gbpCode(err) != "PURCHA-5032" {
			t.Errorf("%s: %v, want PURCHA-5032", name, err)
		}
	}
	_, _, err := PayVendorInvoice(tenantID, "VI-B1", "pr_acct", "Accountant", "")
	blocked("PayVendorInvoice", err)
	_, _, err = PayVendorInvoice(tenantID, "VI-B1", "pr_acct", "Accountant", "urgent")
	blocked("PayVendorInvoice with an override reason", err)
	_, err = PayVendorInvoiceAmount(tenantID, "VI-B2", "pr_acct", 100, "ref")
	blocked("PayVendorInvoiceAmount", err)
	_, _, err = PayVendorInvoiceWithTDS(tenantID, "VI-B3", "194C", "pr_acct")
	blocked("PayVendorInvoiceWithTDS", err)
	_, err = FinalizeVendorInvoiceOverridePayment(tenantID, "VI-B4", "pr_acct")
	blocked("FinalizeVendorInvoiceOverridePayment", err)

	// A retro PO for another vendor, or for a receipt that was not before a PO, is refused.
	if err := ValidatePORetroLink(tenantID, map[string]interface{}{"vendor": "V-FAR", "retro_grn_id": "GRN-B1"}); gbpCode(err) != "PURCHA-5034" {
		t.Fatalf("wrong vendor: %v, want PURCHA-5034", err)
	}
	insert("GRN-PLAIN", "GRN", "Approved", map[string]interface{}{"code": "GRN-PLAIN", "vendor": "V-LOCAL", "po_id": "PO-X"})
	if err := ValidatePORetroLink(tenantID, map[string]interface{}{"vendor": "V-LOCAL", "retro_grn_id": "GRN-PLAIN"}); gbpCode(err) != "PURCHA-5033" {
		t.Fatalf("plain receipt: %v, want PURCHA-5033", err)
	}
	// A forged mark on a PO that names no receipt is dropped.
	forged := map[string]interface{}{"vendor": "V-LOCAL", "received_before_po": "Yes"}
	if err := ValidatePORetroLink(tenantID, forged); err != nil || forged["received_before_po"] != nil {
		t.Fatalf("forged mark kept: %v %v", err, forged)
	}

	// The retro PO: it carries the receipt's id and goes through the usual approval.
	po := map[string]interface{}{"vendor": "V-LOCAL", "retro_grn_id": "GRN-B1", "total_amount": 1000.0, "base_total_amount": 1000.0, "items": `[{"sku":"I-MISC","qty":1,"unit_price":1000}]`}
	if err := ValidatePORetroLink(tenantID, po); err != nil || po["received_before_po"] != "Yes" {
		t.Fatalf("retro PO: %v %v", err, po)
	}
	po["code"] = "PO-RETRO1"
	insert("PO-RETRO1", "PurchaseOrder", "Draft", po)
	if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET created_by = 'pr_maker' WHERE id = 'PO-RETRO1'`, schema)); err != nil {
		t.Fatal(err)
	}
	// A second live retro PO for the same receipt is refused.
	if err := ValidatePORetroLink(tenantID, map[string]interface{}{"vendor": "V-LOCAL", "retro_grn_id": "GRN-B1", "code": "PO-OTHER"}); gbpCode(err) != "PURCHA-5035" {
		t.Fatalf("second PO: %v, want PURCHA-5035", err)
	}
	if err := SubmitForApproval(tenantID, "PurchaseOrder", "PO-RETRO1", "pr_maker", RoleCashier); err != nil {
		t.Fatal(err)
	}
	// Still waiting: the bill stays blocked, and the report shows the receipt.
	_, _, err = PayVendorInvoice(tenantID, "VI-B1", "pr_acct", "Accountant", "")
	blocked("pending retro PO", err)
	if err := DecideApproval(tenantID, "PurchaseOrder", "PO-RETRO1", "pr_acct", "Accountant", "HO", "Approved", ""); err != nil {
		t.Fatal(err)
	}
	grn, _, _ := fetchDocData(tenantID, "GRN", "GRN-B1")
	if strField(grn, "retro_po_id") != "PO-RETRO1" || strField(grn, "po_id") != "PO-RETRO1" {
		t.Fatalf("receipt not linked on approval: %v", grn)
	}
	if err := retroPOBlocksBill(tenantID, map[string]interface{}{"grn_id": "GRN-B1", "po_id": "PO-RETRO1"}); err != nil {
		t.Fatalf("after approval: %v", err)
	}
	// Off with a flagged receipt on file is not a free pass, and nothing else is blocked.
	if err := retroPOBlocksBill(tenantID, map[string]interface{}{"grn_id": "GRN-PLAIN"}); err != nil {
		t.Fatalf("an ordinary bill: %v", err)
	}
}

// 148.38: a receipt past its due date with no approved PO is listed as overdue.
func TestStage14838ReportListsOverdueReceipts(t *testing.T) {
	tenantID, _, insert := poRulesTenant(t)
	insert("GRN-R1", "GRN", "Approved", map[string]interface{}{"code": "GRN-R1", "vendor": "V-LOCAL", "received_before_po": "Yes", "before_po_reason": "GBP-URGENT", "before_po_due": "2020-01-01"})
	insert("GRN-R2", "GRN", "Approved", map[string]interface{}{"code": "GRN-R2", "vendor": "V-LOCAL", "received_before_po": "Yes", "before_po_reason": "GBP-URGENT", "before_po_due": "2099-01-01"})
	insert("GRN-R3", "GRN", "Approved", map[string]interface{}{"code": "GRN-R3", "vendor": "V-LOCAL", "po_id": "PO-1"})
	rows, err := goodsBeforePOReport(tenantID, nil)
	if err != nil {
		t.Fatal(err)
	}
	states := map[string]string{}
	for _, r := range rows {
		states[r["grn"].(string)] = r["state"].(string)
	}
	if len(states) != 2 || states["GRN-R1"] != "Overdue" || states["GRN-R2"] != "Waiting for PO" {
		t.Fatalf("report: %v", states)
	}
}

// TestStage14838EditKeepsTheStoredMarks: an edit cannot lift the bill block or
// flag a receipt by hand; a save cannot escalate a PO to the caller's own role.
func TestStage14838EditKeepsTheStoredMarks(t *testing.T) {
	tenantID, _, insert := poRulesTenant(t)
	insert("GRN-M1", "GRN", "Approved", map[string]interface{}{"code": "GRN-M1", "vendor": "V-LOCAL", "received_before_po": "Yes",
		"before_po_reason": "GBP-URGENT", "before_po_due": "2026-11-11"})
	insert("GRN-M2", "GRN", "Approved", map[string]interface{}{"code": "GRN-M2", "vendor": "V-LOCAL", "po_id": "PO-X"})

	cleared := map[string]interface{}{"received_before_po": "", "retro_po_id": "PO-FAKE", "before_po_due": "2099-01-01"}
	KeepGRNBeforePOMarks(tenantID, "GRN-M1", cleared)
	if cleared["received_before_po"] != "Yes" || cleared["retro_po_id"] != nil || cleared["before_po_due"] != "2026-11-11" || cleared["before_po_reason"] != "GBP-URGENT" {
		t.Fatalf("an edit changed the stored marks: %v", cleared)
	}
	forged := map[string]interface{}{"received_before_po": "Yes", "before_po_reason": "GBP-URGENT", "retro_po_id": "PO-FAKE"}
	KeepGRNBeforePOMarks(tenantID, "GRN-M2", forged)
	if forged["received_before_po"] != nil || forged["before_po_reason"] != nil || forged["retro_po_id"] != nil {
		t.Fatalf("an edit flagged a plain receipt: %v", forged)
	}

	po := map[string]interface{}{"vendor": "V-LOCAL", "escalated_to_role": RoleCashier, "escalated_at": "2026-11-01T00:00:00Z"}
	if err := ValidatePORetroLink(tenantID, po); err != nil {
		t.Fatal(err)
	}
	if po["escalated_to_role"] != nil || po["escalated_at"] != nil {
		t.Fatalf("a save escalated a PO: %v", po)
	}
}

// 148.38: the Goods Receipt and PO screens show their boxes only when the
// doctype's meta lists these fields, so the setting decides what a user sees.
func TestStage14838ScreenFieldsFollowTheSetting(t *testing.T) {
	tenantID, _, _ := poRulesTenant(t)
	listed := func(doctype, field string) bool {
		meta, err := GetDocTypeMeta(tenantID, doctype)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range meta {
			if m.Fieldname == field {
				return true
			}
		}
		return false
	}
	if listed("GRN", "before_po_reason") || listed("PurchaseOrder", "retro_grn_id") {
		t.Fatalf("off: the no-PO fields must not be listed")
	}
	if err := SetSetting(tenantID, SettingGoodsBeforePO, "true", "test"); err != nil {
		t.Fatal(err)
	}
	if !listed("GRN", "before_po_reason") || !listed("PurchaseOrder", "retro_grn_id") {
		t.Fatalf("on: the no-PO fields must be listed")
	}
}

// 148.38 in steps: with no PO the lines take the item's GST and nothing counts
// as beyond a PO; posting makes the bill, which waits as a Draft and cannot be
// paid; the retro PO's approval links and matches it, and then it can be paid.
func TestStage14838StepReceiptBillWaitsForTheRetroPO(t *testing.T) {
	m := newMC1481(t)
	preserveSeries(t, m.schema, "DN", "VCN", "GRNBatchSeq", "VendorInvoice", "GRN")
	if err := SetSetting(m.tenant, SettingGoodsBeforePO, "true", "test"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = SetSetting(m.tenant, SettingGoodsBeforePO, "false", "test") })
	reason, sku, vend, grn, po := m.id("GBP"), m.id("I"), m.id("VEND"), m.id("GRN"), m.id("PO")
	m.doc(reason, "ReasonCode", "Active", map[string]interface{}{"code": reason, "description": "Urgent", "category": ReasonReceivedBeforePO, "status": "Active"})
	m.doc(sku, "Item", "Active", map[string]interface{}{"code": sku, "name": sku, "hsn_code": "6204", "gst_rate": 18.0, "mrp": 999.0})
	m.skus = append(m.skus, sku)
	m.doc(vend, "Vendor", "Active", map[string]interface{}{"code": vend, "name": vend})
	lines := fmt.Sprintf(`[{"sku":"%s","qty":10,"invoice_rate":100}]`, sku)
	payload := map[string]interface{}{"flow": "steps", "vendor": vend, "before_po_reason": reason, "location": m.loc, "vendor_invoice_no": m.id("INV"),
		"vendor_invoice_date": "2026-10-18", "vendor_invoice_amount": 1180.0, "received_items": lines}
	for _, step := range []func() error{
		func() error { return PrepareGRNPricing(m.tenant, true, payload) },
		func() error { return PrepareGRNSteps(m.tenant, "system", true, payload) },
		func() error { return PrepareGRNBatchNumbers(m.tenant, true, payload) },
		func() error { return validateGRNRules(m.tenant, "", payload) },
	} {
		if err := step(); err != nil {
			t.Fatalf("receive with no PO: %v", err)
		}
	}
	if got := numFromInterface(grnLineMaps(payload)[0]["gst_rate"]); got != 18 {
		t.Fatalf("line GST %v, want the item's 18", got)
	}
	payload["code"] = grn
	m.doc(grn, "GRN", strField(payload, "status"), payload)
	SyncGRNLineRows(m.tenant, grn)
	t.Cleanup(func() {
		db.DB.Exec("DELETE FROM "+m.schema+".grn_line WHERE grn_id = $1", grn)
		db.DB.Exec("DELETE FROM "+m.schema+".grn_line_change WHERE grn_id = $1", grn)
	})
	price := 100.0
	verified, err := VerifyGRNStep(m.tenant, "system", "Super Admin", grn, GRNVerifyInput{Lines: []GRNVerifyLine{{LineNo: 1, GRNPrice: &price}}, Reason: "checked"})
	if err != nil {
		t.Fatalf("verify: %v (a receipt with no PO has nothing to be beyond)", err)
	}
	if !verified.Lines[0].BeforePO || verified.Lines[0].ExcessQty != 0 {
		t.Fatalf("line view: before PO %v, beyond %v", verified.Lines[0].BeforePO, verified.Lines[0].ExcessQty)
	}
	s := mc1481Step{sku: sku, grn: grn, po: po}
	v := m.postStep(s)
	if v.VendorBillID == "" || v.VendorBillStatus != "Draft" {
		t.Fatalf("after posting: bill %q status %q, want a Draft", v.VendorBillID, v.VendorBillStatus)
	}
	stored, _, _ := fetchDocData(m.tenant, "GRN", grn)
	if w := strField(stored, "posting_warning"); w != "" {
		t.Fatalf("posting warned: %s", w)
	}
	if _, err := PayGRNStep(m.tenant, "system", grn, GRNPayInput{Mode: "UPI", Reference: "UTR" + m.run}); gbpCode(err) != "PURCHA-5032" {
		t.Fatalf("pay before the PO: %v, want PURCHA-5032", err)
	}
	m.doc(po, "PurchaseOrder", "Approved", map[string]interface{}{"code": po, "vendor": vend, "gst_mode": GSTModeExclusive, "retro_grn_id": grn, "received_before_po": "Yes",
		"total_amount": 1000.0, "grand_total": 1180.0, "items": fmt.Sprintf(`[{"sku":"%s","qty":10,"rate":100}]`, sku)})
	runApprovalDecisionHooks(m.tenant, "PurchaseOrder", po, "Approved", "system")
	bill := m.billData(v.VendorBillID)
	if strField(bill, "po_id") != po {
		t.Fatalf("the bill did not take the PO: %v", bill["po_id"])
	}
	if _, status, _ := fetchDocData(m.tenant, "VendorInvoice", v.VendorBillID); status != "Matched" {
		t.Fatalf("bill status %q after approval, want Matched", status)
	}
	m.payStep(s)
	m.wantStockEqualsGL(sku, grn, v.VendorBillID)
	if net := m.glAll("2100", grn, v.VendorBillID); net < -1 || net > 1 {
		t.Errorf("2100 is left at %d paise", net)
	}
}
