package engines

import (
	"context"
	"custom_erp/db"
	"math"
	"strings"
	"testing"
	"time"
)

func runWithControls(t *testing.T, tenantID, reportID, role string, params map[string]string) ([]map[string]interface{}, *ReportControlTotals) {
	t.Helper()
	_, rows, _, ct, err := RunReportWithControlTotals(context.Background(), tenantID, reportID, role, "system", params)
	if err != nil {
		t.Fatalf("%s: %v", reportID, err)
	}
	if ct == nil {
		t.Fatalf("%s returned no control totals", reportID)
	}
	return rows, ct
}

func near(a, b float64) bool { return math.Abs(a-b) < 0.005 }

// Stage 73.2.19: detail = summary = ledger with a stated cut-off, and a
// moved ledger shows up as a difference rather than being absorbed.
func TestReportControlTotalsTieDetailSummaryAndLedger(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	cleanup := func() {
		db.DB.Exec("DELETE FROM " + schema + ".gl_postings WHERE document_type = 'TestControl7319'")
		db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id LIKE 'T7319-%'")
	}
	cleanup()
	defer cleanup()

	// Inventory valuation: a GL-only movement on 1200 moves the ledger, not
	// the stock figures.
	_, before := runWithControls(t, tenantID, "inventory-valuation", RoleSuperAdmin, nil)
	if !near(before.DetailTotal, before.SummaryTotal) || before.LedgerTotal == nil || before.CutOff == "" || len(before.LedgerAccounts) == 0 {
		t.Fatalf("inventory valuation controls: %+v", before)
	}
	if err := PostDoubleEntry(tenantID, "TestControl7319", "INV", map[string]int64{"1200": 10000}, map[string]int64{"2100": 10000}, "", ""); err != nil {
		t.Fatalf("post: %v", err)
	}
	_, after := runWithControls(t, tenantID, "inventory-valuation", RoleSuperAdmin, nil)
	if !near(*after.LedgerTotal-*before.LedgerTotal, 100) || !near(*after.SummaryVsLedger-*before.SummaryVsLedger, -100) || after.Tied {
		t.Fatalf("ledger move not surfaced: before %+v after %+v", before, after)
	}

	// Receivables: an approved invoice with no GL behind it is a difference
	// of exactly its amount; posting it closes that difference.
	_, arBefore := runWithControls(t, tenantID, "receivables-ageing", RoleSuperAdmin, nil)
	insertTestDoc(t, schema, "T7319-SI", "SalesInvoice", map[string]interface{}{"total_amount": 1000, "due_date": time.Now().AddDate(0, 0, 10).Format("2006-01-02")})
	db.DB.Exec("UPDATE " + schema + ".documents SET status = 'Approved', created_at = CURRENT_TIMESTAMP - INTERVAL '200 days' WHERE id = 'T7319-SI'")
	_, arMid := runWithControls(t, tenantID, "receivables-ageing", RoleSuperAdmin, nil)
	if !near(arMid.SummaryTotal-arBefore.SummaryTotal, 1000) || !near(*arMid.SummaryVsLedger-*arBefore.SummaryVsLedger, 1000) || !near(arMid.DetailTotal, arMid.SummaryTotal) {
		t.Fatalf("receivable without GL: before %+v mid %+v", arBefore, arMid)
	}
	if err := PostDoubleEntry(tenantID, "TestControl7319", "AR", map[string]int64{"1300": 100000}, map[string]int64{"4100": 100000}, "", ""); err != nil {
		t.Fatalf("post AR: %v", err)
	}
	_, arAfter := runWithControls(t, tenantID, "receivables-ageing", RoleSuperAdmin, nil)
	if !near(*arAfter.SummaryVsLedger, *arBefore.SummaryVsLedger) {
		t.Fatalf("posting the receivable did not close the difference: before %+v after %+v", arBefore, arAfter)
	}
	// The drill-down ages like the report: raised 200 days ago but due in 10
	// days, the invoice is in 0-30 days on both.
	drill, err := ageingBucketDrillDown(tenantID, "SalesInvoice", "Approved", "total_amount", "0-30 days")
	if err != nil {
		t.Fatalf("drill: %v", err)
	}
	found := false
	for _, r := range drill {
		if r["id"] == "T7319-SI" {
			found = true
		}
	}
	if !found {
		t.Fatalf("a not-yet-due invoice is missing from its own bucket's drill-down")
	}

	// GST: drill lines = summary = ledger for the window.
	if err := PostDoubleEntry(tenantID, "TestControl7319", "GST", map[string]int64{"1100": 1800}, map[string]int64{"2200": 900, "2201": 900}, "", ""); err != nil {
		t.Fatalf("post GST: %v", err)
	}
	today := time.Now().Format("2006-01-02")
	_, gst := runWithControls(t, tenantID, "gst-return-summary", RoleSuperAdmin, map[string]string{"start": today, "end": today})
	if !gst.Tied || !near(gst.DetailTotal, gst.SummaryTotal) || !strings.Contains(gst.CutOff, today) {
		t.Fatalf("GST controls: %+v", gst)
	}

	// Sales register: detail = summary over the same carts.
	_, sales := runWithControls(t, tenantID, "sales-register", RoleSuperAdmin, nil)
	if !near(sales.DetailTotal, sales.SummaryTotal) || sales.LedgerTotal == nil {
		t.Fatalf("sales controls: %+v", sales)
	}

	// Payables (Stage 73 / 55.10): Payables Ageing is the received-not-paid
	// sub-ledger and ties to 2100; a new 2100 credit moves report and ledger
	// together.
	_, apBefore := runWithControls(t, tenantID, "payables-ageing", RoleSuperAdmin, nil)
	if apBefore.LedgerTotal == nil || !apBefore.Tied || len(apBefore.LedgerAccounts) == 0 || !strings.Contains(apBefore.LedgerAccounts[0], "2100") {
		t.Fatalf("payables controls do not tie to 2100: %+v", apBefore)
	}
	if err := PostDoubleEntry(tenantID, "TestControl7319", "AP", map[string]int64{"1200": 25000}, map[string]int64{"2100": 25000}, "", ""); err != nil {
		t.Fatalf("post AP: %v", err)
	}
	_, apAfter := runWithControls(t, tenantID, "payables-ageing", RoleSuperAdmin, nil)
	if !apAfter.Tied || !near(apAfter.SummaryTotal-apBefore.SummaryTotal, 250) || !near(*apAfter.LedgerTotal-*apBefore.LedgerTotal, 250) {
		t.Fatalf("a 2100 posting did not move payables and ledger together: before %+v after %+v", apBefore, apAfter)
	}
	// The purchase-order view lives on as its own report, with no ledger.
	_, po := runWithControls(t, tenantID, "po-commitments", RoleSuperAdmin, nil)
	if po.LedgerTotal != nil || len(po.Notes) == 0 || !strings.Contains(po.Notes[0], "2100") {
		t.Fatalf("po-commitments controls: %+v", po)
	}

	// A role that cannot see the money sees the verdict, not the amounts.
	_, masked := runWithControls(t, tenantID, "inventory-valuation", RoleCashier, nil)
	if !masked.AmountsHidden || masked.DetailTotal != 0 || masked.SummaryTotal != 0 {
		t.Fatalf("masked controls leak amounts: %+v", masked)
	}
	// 140.12.10: a finance report is refused outright to a role without a finance role.
	if _, _, _, _, err := RunReportWithControlTotals(context.Background(), tenantID, "receivables-ageing", RoleCashier, "system", nil); err == nil {
		t.Fatal("a Cashier ran Receivables Ageing")
	}
	// Reports without control totals keep the old shape.
	if _, _, _, ct, err := RunReportWithControlTotals(context.Background(), tenantID, "grn-register", RoleSuperAdmin, "system", nil); err != nil || ct != nil {
		t.Fatalf("grn-register: %v %v", ct, err)
	}
}

// Stage 73.2.19 (loyalty part, after 73.2.12): with "liability when earned"
// the Points Liability report ties to 2630, and a credit of points moves the
// report and the ledger together; with the default there is no ledger tie and
// the strip says why.
func TestPointsLiabilityControlTotalsTieTo2630WhenAccruing(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	prev := GetSettingStringForSchema(schema, "loyalty.liability_accounting")
	const cust = "T7319-LOYAL"
	cleanup := func() {
		db.DB.Exec("DELETE FROM " + schema + ".gl_postings WHERE document_id IN (SELECT id FROM " + schema + ".documents WHERE doctype = 'LoyaltyPointAdjustment' AND data->>'customer_id' = '" + cust + "')")
		db.DB.Exec("DELETE FROM " + schema + ".loyalty_point_ledger WHERE customer_id = '" + cust + "'")
		db.DB.Exec("DELETE FROM " + schema + ".documents WHERE (doctype = 'LoyaltyPointAdjustment' AND data->>'customer_id' = '" + cust + "') OR id IN ('" + cust + "', 'T7319-LOYAL-RC')")
	}
	cleanup()
	defer func() {
		cleanup()
		_ = SetSetting(tenantID, "loyalty.liability_accounting", prev, "tester")
	}()
	insertTestDoc(t, schema, cust, "Customer", map[string]interface{}{"code": cust, "name": "Control Total"})
	insertTestDoc(t, schema, "T7319-LOYAL-RC", "ReasonCode", map[string]interface{}{"code": "T7319-LOYAL-RC", "category": "Loyalty Adjustment", "status": "Active"})

	if err := SetSetting(tenantID, "loyalty.liability_accounting", "redemption", "tester"); err != nil {
		t.Fatalf("setting: %v", err)
	}
	_, expensed := runWithControls(t, tenantID, "points-liability", RoleSuperAdmin, nil)
	if expensed.LedgerTotal != nil || !near(expensed.DetailTotal, expensed.SummaryTotal) {
		t.Fatalf("expense mode controls: %+v", expensed)
	}
	found := false
	for _, n := range expensed.Notes {
		if strings.Contains(n, "no ledger tie") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expense mode does not say why there is no ledger: %v", expensed.Notes)
	}

	if err := SetSetting(tenantID, "loyalty.liability_accounting", "accrue", "tester"); err != nil {
		t.Fatalf("setting: %v", err)
	}
	_, before := runWithControls(t, tenantID, "points-liability", RoleSuperAdmin, nil)
	if before.LedgerTotal == nil || len(before.LedgerAccounts) == 0 || !near(before.DetailTotal, before.SummaryTotal) {
		t.Fatalf("accrual controls: %+v", before)
	}
	adj, err := RequestLoyaltyAdjustment(tenantID, cust, 40, "T7319-LOYAL-RC", "control total test", "system", RoleSuperAdmin)
	if err != nil || adj.Status != "Posted" {
		t.Fatalf("adjustment: %v %+v", err, adj)
	}
	_, after := runWithControls(t, tenantID, "points-liability", RoleSuperAdmin, nil)
	value := PaiseToRupees(40 * loyaltyPointValuePaise(tenantID))
	if !near(after.SummaryTotal-before.SummaryTotal, value) || !near(*after.LedgerTotal-*before.LedgerTotal, value) ||
		!near(*after.SummaryVsLedger, *before.SummaryVsLedger) {
		t.Fatalf("a 40-point credit did not move report and ledger together: before %+v after %+v", before, after)
	}
}
