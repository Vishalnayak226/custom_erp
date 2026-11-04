package engines

import (
	"testing"

	"custom_erp/db"
)

// Stage 148.17 - volume rebates from vendors: off by default and invisible
// while off; a target with two tiers on the vendor agreement; one claim per
// vendor and period, calculated by a button, approved (accrual) and settled
// (debit note); a re-run never touches an approved claim; strict status moves.

func (f *creditFixture) rebateBill(id, vendor string, invoice, gst float64, date string) {
	f.t.Helper()
	f.seed(id, "VendorInvoice", "Matched", map[string]interface{}{
		"code": id, "invoice_number": "N-" + id, "vendor_id": vendor, "invoice_amount": invoice, "gst_amount": gst,
		"invoice_date": date, "financial_year": "2026-27", "status": "Matched",
	})
}

func (f *creditFixture) claimCount(vendor string) int {
	var n int
	_ = db.DB.QueryRow("SELECT COUNT(*) FROM "+f.schema+".documents WHERE doctype = 'VendorRebateClaim' AND data->>'vendor_id' = $1", vendor).Scan(&n)
	return n
}

func TestStage14817OffByDefaultAndInvisibleWhileOff(t *testing.T) {
	f := newCreditFixture(t)
	if GetSettingBool(f.tenantID, SettingVendorRebates) {
		t.Fatalf("procurement.vendor_rebates must default to off")
	}
	if _, err := CalculateVendorRebates(f.tenantID, "u1", "2026-04-01", "2026-06-30", ""); t5510Code(err) != "PURCHA-5020" {
		t.Fatalf("calculate while off: want PURCHA-5020, got %v", err)
	}
	if err := SubmitVendorRebateClaim(f.tenantID, "u1", "Accountant", "X"); t5510Code(err) != "PURCHA-5020" {
		t.Fatalf("submit while off: want PURCHA-5020, got %v", err)
	}
	if _, err := SettleVendorRebateClaim(f.tenantID, "u1", "X"); t5510Code(err) != "PURCHA-5020" {
		t.Fatalf("settle while off: want PURCHA-5020, got %v", err)
	}
	types, _ := GetDocTypes(f.tenantID)
	for _, d := range types {
		if d["name"] == VendorRebateClaimDoctype {
			t.Fatalf("VendorRebateClaim must not be listed while the setting is off")
		}
	}
	meta, _ := GetDocTypeMeta(f.tenantID, "PriceListVersion")
	for _, m := range meta {
		if m.Fieldname == "rebate_basis" || m.Fieldname == "rebate_tier1_target" {
			t.Fatalf("the rebate fields must be hidden while the setting is off: saw %s", m.Fieldname)
		}
	}
	if err := SetSetting(f.tenantID, SettingVendorRebates, "true", "u1"); err != nil {
		t.Fatal(err)
	}
	types, _ = GetDocTypes(f.tenantID)
	seen := false
	for _, d := range types {
		seen = seen || d["name"] == VendorRebateClaimDoctype
	}
	meta, _ = GetDocTypeMeta(f.tenantID, "PriceListVersion")
	fields := 0
	for _, m := range meta {
		if m.Fieldname == "rebate_basis" || m.Fieldname == "rebate_tier2_pct" {
			fields++
		}
	}
	if !seen || fields != 2 {
		t.Fatalf("with the setting on, the doctype (%v) and the agreement fields (%d of 2) show", seen, fields)
	}
}

func TestStage14817ClaimLifecycleAccrualSettleAndRerun(t *testing.T) {
	f := newCreditFixture(t)
	if err := SetSetting(f.tenantID, SettingVendorRebates, "true", "u1"); err != nil {
		t.Fatal(err)
	}
	f.vendor("V-RB", nil)
	f.seed("AGR-RB", "PriceListVersion", "Approved", map[string]interface{}{
		"code": "AGR-RB", "vendor": "V-RB", "status": "Approved", "rebate_basis": "Gross",
		"rebate_tier1_target": 1000.0, "rebate_tier1_pct": 5.0, "rebate_tier2_target": 2000.0, "rebate_tier2_pct": 8.0,
	})
	f.rebateBill("RB-1", "V-RB", 1180, 180, "2026-05-10") // taxable 1000
	f.rebateBill("RB-2", "V-RB", 600, 0, "2026-05-11")
	f.rebateBill("RB-3", "V-RB", 500, 0, "2026-06-01")
	f.rebateBill("RB-OUT", "V-RB", 9999, 0, "2026-08-01") // outside the period
	f.seed("DN-RB", "DebitNote", "Posted", map[string]interface{}{"code": "DN-RB", "vendor_id": "V-RB", "amount": 300.0, "status": "Posted"})
	if _, err := db.DB.Exec("UPDATE " + f.schema + ".documents SET created_at = '2026-05-20' WHERE id = 'DN-RB'"); err != nil {
		t.Fatal(err)
	}

	if _, err := CalculateVendorRebates(f.tenantID, "u1", "2026-06-30", "2026-04-01", ""); t5510Code(err) != "PURCHA-5021" {
		t.Fatalf("a backwards period: want PURCHA-5021, got %v", err)
	}
	// Gross: 2100 reaches tier 2 (2000): 8% = 168.
	res, err := CalculateVendorRebates(f.tenantID, "u1", "2026-04-01", "2026-06-30", "")
	if err != nil || len(res) != 1 || res[0].Action != "created" || res[0].Tier != 2 || res[0].Rebate != 168 || res[0].Qualifying != 2100 || res[0].Status != "Calculated" {
		t.Fatalf("gross calculation: %+v %v", res, err)
	}
	id := res[0].ID
	again, err := CalculateVendorRebates(f.tenantID, "u1", "2026-04-01", "2026-06-30", "")
	if err != nil || len(again) != 1 || again[0].ID != id || again[0].Action != "unchanged" || f.claimCount("V-RB") != 1 {
		t.Fatalf("a re-run must find the same claim: %+v %v (claims %d)", again, err, f.claimCount("V-RB"))
	}
	// Net of the 300 debit note: 1800 is tier 1 (5%) = 90. A Calculated claim is updated.
	if _, err := db.DB.Exec("UPDATE " + f.schema + ".documents SET data = jsonb_set(data, '{rebate_basis}', '\"Net\"') WHERE id = 'AGR-RB'"); err != nil {
		t.Fatal(err)
	}
	up, err := CalculateVendorRebates(f.tenantID, "u1", "2026-04-01", "2026-06-30", "V-RB")
	if err != nil || len(up) != 1 || up[0].Action != "updated" || up[0].ID != id || up[0].Rebate != 90 || up[0].Tier != 1 || up[0].Qualifying != 1800 {
		t.Fatalf("net re-run: %+v %v", up, err)
	}
	// Settle and submit are refused out of order.
	if _, err := SettleVendorRebateClaim(f.tenantID, "u1", id); t5510Code(err) != "PURCHA-5022" {
		t.Fatalf("settle before approval: want PURCHA-5022, got %v", err)
	}
	if err := SubmitVendorRebateClaim(f.tenantID, "maker", "Accountant", id); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if st := f.field("VendorRebateClaim", id, "status"); st != "Pending Approval" {
		t.Fatalf("submitted claim is %s", st)
	}
	if err := SubmitVendorRebateClaim(f.tenantID, "maker", "Accountant", id); t5510Code(err) != "PURCHA-5022" {
		t.Fatalf("a second submit: want PURCHA-5022, got %v", err)
	}
	if sk, _ := CalculateVendorRebates(f.tenantID, "u1", "2026-04-01", "2026-06-30", ""); len(sk) != 1 || sk[0].Action != "skipped" {
		t.Fatalf("a claim waiting for approval is not recalculated: %+v", sk)
	}
	if err := DecideApproval(f.tenantID, VendorRebateClaimDoctype, id, "checker", "Accountant", "", "Approved", "ok"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if st := f.field("VendorRebateClaim", id, "status"); st != "Approved" {
		t.Fatalf("approved claim is %s", st)
	}
	if f.net(id, "1820") != 9000 || f.net(id, "5150") != -9000 {
		t.Fatalf("accrual: 1820 %d, 5150 %d (want 9000 / -9000)", f.net(id, "1820"), f.net(id, "5150"))
	}
	// An approved claim is never touched by a re-run, even with other terms.
	if _, err := db.DB.Exec("UPDATE " + f.schema + ".documents SET data = jsonb_set(data, '{rebate_tier1_pct}', '7') WHERE id = 'AGR-RB'"); err != nil {
		t.Fatal(err)
	}
	if sk, _ := CalculateVendorRebates(f.tenantID, "u1", "2026-04-01", "2026-06-30", ""); len(sk) != 1 || sk[0].Action != "skipped" || sk[0].Status != "Approved" || f.field("VendorRebateClaim", id, "rebate_amount") != "90" {
		t.Fatalf("an approved claim must not change: %+v amount %s", sk, f.field("VendorRebateClaim", id, "rebate_amount"))
	}
	// Settle: a Posted debit note, 2100 debited and the receivable cleared.
	note, err := SettleVendorRebateClaim(f.tenantID, "u1", id)
	if err != nil || note == "" {
		t.Fatalf("settle: %q %v", note, err)
	}
	if st := f.field("VendorRebateClaim", id, "status"); st != "Settled" || f.field("DebitNote", note, "basis") != "vendor_rebate" || f.field("DebitNote", note, "amount") != "90" {
		t.Fatalf("settled claim %s, note basis %s amount %s", st, f.field("DebitNote", note, "basis"), f.field("DebitNote", note, "amount"))
	}
	if f.net(note, "2100") != 9000 || f.net(note, "1820")+f.net(id, "1820") != 0 || f.accountNet("1820") != 0 {
		t.Fatalf("settle posting: 2100 %d, 1820 total %d", f.net(note, "2100"), f.accountNet("1820"))
	}
	if n2, err := SettleVendorRebateClaim(f.tenantID, "u1", id); err != nil || n2 != note {
		t.Fatalf("a retried settle must return the same note: %q %v", n2, err)
	}
	var notes int
	_ = db.DB.QueryRow("SELECT COUNT(*) FROM "+f.schema+".documents WHERE doctype = 'DebitNote' AND data->>'source_doc_id' = $1", id).Scan(&notes)
	if notes != 1 {
		t.Fatalf("a retried settle raised %d notes", notes)
	}
	if sk, _ := CalculateVendorRebates(f.tenantID, "u1", "2026-04-01", "2026-06-30", ""); len(sk) != 1 || sk[0].Action != "skipped" || sk[0].Status != "Settled" {
		t.Fatalf("a settled claim is not recalculated: %+v", sk)
	}
}

func TestStage14817RejectedClaimZeroRebateAndStrictTransitions(t *testing.T) {
	f := newCreditFixture(t)
	if err := SetSetting(f.tenantID, SettingVendorRebates, "true", "u1"); err != nil {
		t.Fatal(err)
	}
	f.vendor("V-RB2", nil)
	f.vendor("V-NOTARGET", nil)
	f.seed("AGR-RB2", "PriceListVersion", "Approved", map[string]interface{}{
		"code": "AGR-RB2", "vendor": "V-RB2", "status": "Approved", "rebate_basis": "Gross",
		"rebate_tier1_target": 1000.0, "rebate_tier1_pct": 5.0,
	})
	f.seed("AGR-NT", "PriceListVersion", "Approved", map[string]interface{}{"code": "AGR-NT", "vendor": "V-NOTARGET", "status": "Approved"})
	f.rebateBill("RB-4", "V-RB2", 2500, 0, "2026-07-10")
	f.rebateBill("RB-5", "V-NOTARGET", 5000, 0, "2026-07-10")
	res, err := CalculateVendorRebates(f.tenantID, "u1", "2026-07-01", "2026-07-31", "")
	if err != nil || len(res) != 1 || res[0].Vendor != "V-RB2" || res[0].Rebate != 125 {
		t.Fatalf("only the vendor with a target gets a claim: %+v %v", res, err)
	}
	id := res[0].ID
	if err := SubmitVendorRebateClaim(f.tenantID, "maker", "Accountant", id); err != nil {
		t.Fatal(err)
	}
	if err := DecideApproval(f.tenantID, VendorRebateClaimDoctype, id, "checker", "Accountant", "", "Rejected", "wrong period"); err != nil {
		t.Fatal(err)
	}
	if f.accountNet("1820") != 0 {
		t.Fatalf("a rejected claim must not accrue: 1820 %d", f.accountNet("1820"))
	}
	// A Rejected claim is recalculated, back to Calculated; below the first tier its rebate is nil and it cannot be submitted.
	if _, err := db.DB.Exec("UPDATE " + f.schema + ".documents SET data = jsonb_set(data, '{rebate_tier1_target}', '99999') WHERE id = 'AGR-RB2'"); err != nil {
		t.Fatal(err)
	}
	up, err := CalculateVendorRebates(f.tenantID, "u1", "2026-07-01", "2026-07-31", "V-RB2")
	if err != nil || len(up) != 1 || up[0].Action != "updated" || up[0].Status != "Calculated" || up[0].Rebate != 0 {
		t.Fatalf("a rejected claim is recalculated: %+v %v", up, err)
	}
	if err := SubmitVendorRebateClaim(f.tenantID, "maker", "Accountant", id); t5510Code(err) != "PURCHA-5023" {
		t.Fatalf("a zero claim: want PURCHA-5023, got %v", err)
	}
	if st := f.field("VendorRebateClaim", id, "status"); st != "Calculated" {
		t.Fatalf("a refused submit left the claim %s", st)
	}
	// A tier-2 target at or under tier 1 is refused with its own code.
	if _, err := db.DB.Exec("UPDATE " + f.schema + ".documents SET data = data || '{\"rebate_tier1_target\":1000,\"rebate_tier2_target\":900,\"rebate_tier2_pct\":9}'::jsonb WHERE id = 'AGR-RB2'"); err != nil {
		t.Fatal(err)
	}
	if _, err := CalculateVendorRebates(f.tenantID, "u1", "2026-07-01", "2026-07-31", "V-RB2"); t5510Code(err) != "PURCHA-5024" {
		t.Fatalf("a tier 2 under tier 1: want PURCHA-5024, got %v", err)
	}
	// Strict status transitions: the doctype is strict and no generic move is allowed.
	var strict bool
	_ = db.DB.QueryRow("SELECT strict_status_transitions FROM " + f.schema + ".doctype_meta WHERE name = 'VendorRebateClaim'").Scan(&strict)
	if !strict {
		t.Fatalf("VendorRebateClaim must be flagged strict")
	}
	for _, mv := range [][2]string{{"Approved", "Calculated"}, {"Approved", "Settled"}, {"Calculated", "Approved"}, {"Settled", "Approved"}} {
		if err := ValidateStatusTransition(f.tenantID, "VendorRebateClaim", mv[0], mv[1], map[string]interface{}{}); err == nil {
			t.Fatalf("a generic move %s -> %s must be refused", mv[0], mv[1])
		}
	}
	var writable int
	_ = db.DB.QueryRow("SELECT COUNT(*) FROM " + f.schema + ".role_permissions WHERE doctype_name = 'VendorRebateClaim' AND (allow_create OR allow_update OR allow_delete)").Scan(&writable)
	var acct int
	_ = db.DB.QueryRow("SELECT COUNT(*) FROM " + f.schema + ".gl_accounts WHERE account_code = '1820' AND account_type = 'Asset'").Scan(&acct)
	if writable != 0 || acct != 1 {
		t.Fatalf("claim must be read-only to generic writes (%d) and 1820 seeded (%d)", writable, acct)
	}
}
