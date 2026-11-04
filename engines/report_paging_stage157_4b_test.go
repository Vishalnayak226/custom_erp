package engines

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"custom_erp/db"
)

// Stage 157.4b: every report pages on screen, its totals cover every page, and
// a run the server cannot finish is refused rather than cut short.

func reg1574b(t *testing.T, def ReportDefinition) {
	t.Helper()
	RegisterReport(def)
	t.Cleanup(func() {
		delete(reportRegistry, def.ID)
		for i, id := range reportRegistryOrder {
			if id == def.ID {
				reportRegistryOrder = append(reportRegistryOrder[:i], reportRegistryOrder[i+1:]...)
				break
			}
		}
	})
}

func rows1574b(n int) []map[string]interface{} {
	rows := make([]map[string]interface{}, n)
	for i := range rows {
		rows[i] = map[string]interface{}{"n": i, "amount": 1.1, "cost": 2.0}
	}
	return rows
}

func def1574b(id string, n int) ReportDefinition {
	return ReportDefinition{ID: id, Label: id, Category: "Inventory", StoreKey: "-",
		Columns: []ReportColumn{{Key: "n", Label: "N"}, {Key: "amount", Label: "Amount", Total: true},
			{Key: "cost", Label: "Cost", Total: true, Sensitive: true}},
		Run: func(string, map[string]string) ([]map[string]interface{}, error) { return rows1574b(n), nil }}
}

func code1574b(err error) (string, string) {
	var v *ValidationError
	if errors.As(err, &v) {
		return v.Code, v.Message
	}
	return "", ""
}

func TestStage1574bAnyReportPagesWithTrueTotals(t *testing.T) {
	tenantID, _ := cancellationTestTenant(t)
	if err := SetSetting(tenantID, "platform.max_sync_report_rows", "100", "test"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	const id = "test-1574b-slice"
	reg1574b(t, def1574b(id, 150))

	_, _, _, err := RunReportContext(context.Background(), tenantID, id, RoleSuperAdmin, "", nil)
	if code, msg := code1574b(err); code != "REPORT-0161" || !strings.Contains(msg, "Export") {
		t.Fatalf("unpaged over the setting: want REPORT-0161 pointing at Export, got %q %q", code, msg)
	}

	page := &ReportPage{Limit: 50, Offset: 100}
	_, rows, _, err := RunReportContext(WithReportPage(context.Background(), page), tenantID, id, RoleSuperAdmin, "", nil)
	if err != nil || len(rows) != 50 || page.Total != 150 {
		t.Fatalf("last page: rows=%d total=%d err=%v", len(rows), page.Total, err)
	}
	if rows[0]["n"] != 100 {
		t.Fatalf("last page starts at row %v, want 100", rows[0]["n"])
	}
	if page.Totals["amount"] != 165 || page.Totals["cost"] != 300 {
		t.Fatalf("totals must cover every page, got %v", page.Totals)
	}

	// A role that sees the cost masked must not read it back from the total.
	masked := &ReportPage{Limit: 50}
	_, rows, _, err = RunReportContext(WithReportPage(context.Background(), masked), tenantID, id, "Accountant", "", nil)
	if err != nil {
		t.Fatalf("masked run: %v", err)
	}
	if _, leaked := masked.Totals["cost"]; leaked || masked.Totals["amount"] != 165 {
		t.Fatalf("masked totals: %v", masked.Totals)
	}
	if rows[0]["cost"] != redactedReportValue {
		t.Fatalf("masked cell: %v", rows[0]["cost"])
	}

	// Past the end: an empty page that still knows the result is not empty.
	past := &ReportPage{Limit: 50, Offset: 200}
	_, rows, _, err = RunReportContext(WithReportPage(context.Background(), past), tenantID, id, RoleSuperAdmin, "", nil)
	if err != nil || len(rows) != 0 || past.Total != 150 {
		t.Fatalf("past the end: rows=%d total=%d err=%v", len(rows), past.Total, err)
	}
}

func TestStage1574bCeilingRefusesWithoutACircularMessage(t *testing.T) {
	tenantID, _ := cancellationTestTenant(t)
	const atCeiling, over = "test-1574b-at", "test-1574b-over"
	reg1574b(t, def1574b(atCeiling, maxReportRunRows))
	reg1574b(t, def1574b(over, maxReportRunRows+1))

	page := &ReportPage{Limit: 10}
	_, rows, _, err := RunReportContext(WithReportPage(context.Background(), page), tenantID, atCeiling, RoleSuperAdmin, "", nil)
	if err != nil || len(rows) != 10 || page.Total != maxReportRunRows {
		t.Fatalf("at the ceiling: rows=%d total=%d err=%v", len(rows), page.Total, err)
	}

	_, _, _, err = RunReportContext(WithReportPage(context.Background(), &ReportPage{Limit: 10}), tenantID, over, RoleSuperAdmin, "", nil)
	code, msg := code1574b(err)
	if code != "REPORT-0161" {
		t.Fatalf("over the ceiling: want REPORT-0161, got %v", err)
	}
	if strings.Contains(msg, "Export") {
		t.Fatalf("an export stops at the same ceiling, so it must not be offered: %q", msg)
	}
}

func TestStage1574bExportsReachTheCeiling(t *testing.T) {
	tenantID, schema := cancellationTestTenant(t)
	if err := SetSetting(tenantID, "platform.max_sync_report_rows", "100", "test"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	const fits, tooBig = "test-1574b-export-fits", "test-1574b-export-big"
	for id, n := range map[string]int{fits: 101, tooBig: maxReportRunRows + 1} {
		d := def1574b(id, n)
		d.Category = "Admin"
		reg1574b(t, d)
	}
	run := func(id string) (string, string) {
		jobID, err := CreateReportExportJob(tenantID, id, "HR/Admin", nil, "system")
		if err != nil {
			t.Fatalf("CreateReportExportJob: %v", err)
		}
		processReportExportJobs(context.Background(), schema)
		status, _, code, err := GetReportExportJob(tenantID, jobID)
		if err != nil {
			t.Fatalf("GetReportExportJob: %v", err)
		}
		return status, code
	}
	if status, code := run(fits); status != "Completed" {
		t.Fatalf("101 rows at a setting of 100 must export: %s %s", status, code)
	}
	if status, code := run(tooBig); status != "Failed" || code != "REPORT-0285" {
		t.Fatalf("over the ceiling: %s %s", status, code)
	}
	// The list loads the jobs, not every stored file.
	jobs, err := ListMyReportExportJobs(tenantID, "system", 20)
	if err != nil || len(jobs) != 2 {
		t.Fatalf("list: %d jobs, %v", len(jobs), err)
	}
	var withFile int
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.documents WHERE doctype = 'ReportExportJob' AND data ? 'csv'`, schema)).Scan(&withFile); err != nil {
		t.Fatalf("count files: %v", err)
	}
	if withFile == 0 {
		t.Fatal("the completed job should have stored its file")
	}
}

func seedCart1574b(t *testing.T, schema, by, id, location string, price float64) {
	t.Helper()
	if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by)
		VALUES ($1, 'POSCart', jsonb_build_object('location', $2::text, 'items', jsonb_build_array(jsonb_build_object('qty', 1, 'sale_price', $3::numeric))), 'Paid', $4)`, schema),
		id, location, price, by); err != nil {
		t.Fatalf("seed cart: %v", err)
	}
}

// A paged run totals every row: the control panel must not compare one page
// with the whole ledger and raise a false "does not tie".
func TestStage1574bControlTotalsCoverEveryPage(t *testing.T) {
	tenantID, schema := cancellationTestTenant(t)
	for i := 0; i < 30; i++ {
		seedCart1574b(t, schema, "test", fmt.Sprintf("CART-1574B-%d", i), "HO", 100)
	}
	page := &ReportPage{Limit: 10}
	_, rows, _, controls, err := RunReportWithControlTotals(WithReportPage(context.Background(), page), tenantID, "sales-register", RoleSuperAdmin, "", nil)
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	if len(rows) != 10 || page.Total != 30 {
		t.Fatalf("first page: rows=%d total=%d", len(rows), page.Total)
	}
	if controls == nil || controls.DetailTotal != 3000 || controls.DetailVsSummary != 0 {
		t.Fatalf("control totals must cover all 30 sales, got %+v", controls)
	}
	var flagged int
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.audit_logs WHERE details LIKE '%%control_totals:sales-register%%' AND details LIKE '%%detail 1000.00%%'`, schema)).Scan(&flagged); err == nil && flagged != 0 {
		t.Fatalf("a page must not raise the invariant event: %d", flagged)
	}

	later := &ReportPage{Limit: 10, Offset: 10}
	_, rows, _, controls, err = RunReportWithControlTotals(WithReportPage(context.Background(), later), tenantID, "sales-register", RoleSuperAdmin, "", nil)
	if err != nil || len(rows) != 10 || later.Total != 30 || controls != nil {
		t.Fatalf("later page: rows=%d total=%d controls=%+v err=%v", len(rows), later.Total, controls, err)
	}
}

// A store-limited user's page is cut in SQL, so the count and the pages hold.
func TestStage1574bStoreLimitedUserPagesInSQL(t *testing.T) {
	db.InitDB(testConnStr())
	const tenantID, user, role = "default", "manager1", "Store Manager"
	before, err := GetUserStoreAccess(tenantID, user)
	if err != nil {
		t.Skipf("no %s user in this database: %v", user, err)
	}
	if err := SetUserStoreAccess(tenantID, user, StoreAccessHome, nil, "test"); err != nil {
		t.Fatalf("pin %s to the home store: %v", user, err)
	}
	defer SetUserStoreAccess(tenantID, user, before.Access, before.Stores, "test")
	access, limited := reportStoreScope(tenantID, role, user)
	if !limited || access.Home == "" {
		t.Fatalf("want a store-limited user, got %+v", access)
	}
	home := access.Home
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	const sku, other = "SKU-1574B-SCOPE", "OTHER-1574B"
	cleanup := func() {
		db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.documents WHERE id LIKE 'SLE-1574B-%%' OR id LIKE 'CART-1574B-SCOPE-%%'`, schema))
		db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.inventory_availability WHERE sku = $1`, schema), sku)
	}
	cleanup()
	t.Cleanup(cleanup)
	for _, wh := range []string{home, other} {
		if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by)
			SELECT 'SLE-1574B-'||$1::text||'-'||g, 'StockLedgerEntry',
			       jsonb_build_object('item_id', $2::text, 'warehouse_id', $1::text, 'qty', 1, 'voucher_type', 'GRN', 'voucher_id', 'G'||g),
			       'Active', 'admin' FROM generate_series(1, 30) g`, schema), wh, sku); err != nil {
			t.Fatalf("seed ledger: %v", err)
		}
		if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, 5, 5)`, schema), sku, wh); err != nil {
			t.Fatalf("seed stock: %v", err)
		}
		seedCart1574b(t, schema, "admin", "CART-1574B-SCOPE-"+wh, wh, 10)
	}

	page := &ReportPage{Limit: 10, Offset: 20}
	_, rows, _, err := RunReportContext(WithReportPage(context.Background(), page), tenantID, "stock-ledger", role, user, map[string]string{"sku": sku})
	if err != nil || len(rows) != 10 || page.Total != 30 {
		t.Fatalf("stock ledger: rows=%d total=%d err=%v", len(rows), page.Total, err)
	}
	for _, r := range rows {
		if r["warehouse_id"] != home {
			t.Fatalf("stock ledger leaked %v", r["warehouse_id"])
		}
	}

	countIn := func(sql string) int {
		var n int
		if err := db.DB.QueryRow(fmt.Sprintf(sql, schema), home).Scan(&n); err != nil {
			t.Fatalf("count: %v", err)
		}
		return n
	}
	for _, c := range []struct {
		report, key, count string
	}{
		{"current-stock", "location_code", `SELECT COUNT(*) FROM %s.inventory_availability WHERE location_code = $1`},
		{"sales-register", "location", `SELECT COUNT(*) FROM %s.documents WHERE doctype = 'POSCart' AND status IN ('Paid','Settled') AND data->>'location' = $1`},
	} {
		page := &ReportPage{Limit: 50}
		_, rows, _, _, err := RunReportWithControlTotals(WithReportPage(context.Background(), page), tenantID, c.report, role, user, nil)
		if err != nil {
			t.Fatalf("%s: %v", c.report, err)
		}
		if want := countIn(c.count); page.Total != want {
			t.Fatalf("%s: total %d, want %d (the store's own count)", c.report, page.Total, want)
		}
		for _, r := range rows {
			if r[c.key] != home {
				t.Fatalf("%s leaked %v", c.report, r[c.key])
			}
		}
	}
}

func TestStage1574bVendorLedgerPagesInSQL(t *testing.T) {
	tenantID, schema := cancellationTestTenant(t)
	if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by)
		SELECT 'PO-1574B-'||g, 'PurchaseOrder', jsonb_build_object('vendor', 'V-1574B', 'po_number', 'P'||g, 'total_amount', 10), 'Approved', 'test'
		FROM generate_series(1, 12) g`, schema)); err != nil {
		t.Fatalf("seed: %v", err)
	}
	seen := map[interface{}]bool{}
	for off := 0; off < 12; off += 5 {
		page := &ReportPage{Limit: 5, Offset: off}
		_, rows, _, err := RunReportContext(WithReportPage(context.Background(), page), tenantID, "vendor-ledger", RoleSuperAdmin, "", map[string]string{"vendor_id": "V-1574B"})
		if err != nil || page.Total != 12 {
			t.Fatalf("offset %d: total=%d err=%v", off, page.Total, err)
		}
		for _, r := range rows {
			seen[r["id"]] = true
		}
	}
	if len(seen) != 12 {
		t.Fatalf("pages must show each PO once: %d distinct of 12", len(seen))
	}
}

// A read that stopped at its own limit is refused, not folded into a wrong sum.
func TestStage1574bStoppedReadsAreRefused(t *testing.T) {
	tenantID, schema := cancellationTestTenant(t)
	seed := func(doctype string, n int) {
		t.Helper()
		if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by)
			SELECT $1::text||'-1574B-'||g, $1::text, '{}'::jsonb, 'Active', 'test' FROM generate_series(1, $2::int) g`, schema), doctype, n); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	// Through the runner: a helper's refusal keeps its code, not REPORT-0162.
	seed("TransferOrder", 5000)
	_, _, _, err := RunReportContext(context.Background(), tenantID, "transfer-shortage-by-reason", RoleSuperAdmin, "", map[string]string{"from": "2000-01-01", "to": "2100-01-01"})
	if code, msg := code1574b(err); code != "REPORT-0161" || strings.Contains(msg, "Export") {
		t.Fatalf("a stopped read: want REPORT-0161 without Export, got %v", err)
	}

	// A catalog source reading documents.
	seed("Test1574bDoc", registerRowCap)
	src := &catSource{Doctype: "Test1574bDoc", StoreField: "-"}
	if _, err := src.docRows(schema, map[string]string{}, true); err == nil {
		t.Fatal("a catalog read at the row cap must be refused")
	} else if code, _ := code1574b(err); code != "REPORT-0161" {
		t.Fatalf("catalog read: %v", err)
	}
}

func TestStage1574bContactColumnsAreMaskedOnEveryPath(t *testing.T) {
	tenantID, _ := cancellationTestTenant(t)
	const id = "test-1574b-contact"
	reg1574b(t, ReportDefinition{ID: id, Label: id, Category: "Inventory", StoreKey: "-",
		Columns: []ReportColumn{{Key: "customer_mobile", Label: "Customer Mobile"}},
		Run: func(string, map[string]string) ([]map[string]interface{}, error) {
			return []map[string]interface{}{{"customer_mobile": "9876543210"}}, nil
		}})
	check := func(label string, rows []map[string]interface{}, err error) {
		t.Helper()
		if err != nil || len(rows) != 1 {
			t.Fatalf("%s: %d rows, %v", label, len(rows), err)
		}
		if rows[0]["customer_mobile"] == "9876543210" {
			t.Fatalf("%s: a role without contact access read the full number", label)
		}
	}
	_, rows, _, err := RunReportContext(context.Background(), tenantID, id, "Accountant", "", nil)
	check("plain run", rows, err)

	RegisterReportControlTotals(id, func(string, map[string]string, []map[string]interface{}) (*ReportControlTotals, error) {
		return &ReportControlTotals{Tied: true}, nil
	})
	t.Cleanup(func() { delete(reportControlTotals, id) })
	_, rows, _, _, err = RunReportWithControlTotals(context.Background(), tenantID, id, "Accountant", "", nil)
	check("control totals", rows, err)
	_, rows, _, _, err = RunReportWithControlTotals(WithReportPage(context.Background(), &ReportPage{Limit: 10}), tenantID, id, "Accountant", "", nil)
	check("control totals, paged", rows, err)
}

func TestStage1574bEveryDuplicateRowNamesItsKeptRecord(t *testing.T) {
	rows := masterDuplicateReportRows([]MasterDuplicateRow{
		{Group: 1, ID: "V1", Golden: true}, {Group: 1, ID: "V2"}, {Group: 2, ID: "V3"}, {Group: 2, ID: "V4", Golden: true},
	})
	want := map[string]string{"V1": "V1", "V2": "V1", "V3": "V4", "V4": "V4"}
	for _, r := range rows {
		if r["keep_id"] != want[r["id"].(string)] {
			t.Fatalf("row %v kept record: %v", r["id"], r["keep_id"])
		}
	}
}

// Pages are cut from separate runs, so a report must give the same rows in the
// same order each time, or pages would repeat some rows and lose others. Reports
// built by ranging over a Go map do not, unless they sort; this catches any that
// has data here. Only a report whose rows are identical in another order fails:
// rows that changed between runs (a clock, a sequence) say nothing about order.
func TestStage1574bReportRowOrderIsStable(t *testing.T) {
	db.InitDB(testConnStr())
	a, b, c := map[string]interface{}{"x": 1}, map[string]interface{}{"x": 2}, map[string]interface{}{"x": 3}
	if !orderMoved1574b([]map[string]interface{}{a, b}, []map[string]interface{}{b, a}) ||
		orderMoved1574b([]map[string]interface{}{a, b}, []map[string]interface{}{a, c}) ||
		orderMoved1574b([]map[string]interface{}{a, b}, []map[string]interface{}{a, b}) {
		t.Fatal("the order check itself is broken")
	}
	checked := 0
	for _, id := range reportRegistryOrder {
		def, ok := reportRegistry[id]
		if !ok || def.Run == nil || strings.HasPrefix(id, "test-") {
			continue
		}
		params, needsInput := map[string]string{}, false
		for _, p := range def.Params {
			if p.Required && p.Type != "date" {
				needsInput = true
			}
			if p.Type == "date" {
				params[p.Key] = "2026-01-01"
				switch p.Key {
				case "end", "to", "to_date", "end_date", "as_of", "until":
					params[p.Key] = "2026-12-31"
				}
			}
		}
		if needsInput {
			continue
		}
		first, err := def.Run("default", params)
		if err != nil || len(first) < 2 {
			continue
		}
		checked++
		for run := 0; run < 3; run++ {
			again, err := def.Run("default", params)
			if err != nil || len(again) != len(first) {
				break
			}
			if orderMoved1574b(first, again) {
				t.Errorf("%s gave the same rows in another order - sort them", id)
				break
			}
		}
	}
	t.Logf("%d of %d reports had rows to compare", checked, len(reportRegistryOrder))
}

func orderMoved1574b(first, again []map[string]interface{}) bool {
	sameOrder, count := true, map[string]int{}
	for i := range first {
		a, b := fmt.Sprint(first[i]), fmt.Sprint(again[i])
		sameOrder = sameOrder && a == b
		count[a]++
		count[b]--
	}
	for _, n := range count {
		if n != 0 {
			return false
		}
	}
	return !sameOrder
}

// The menu's Current Stock, Sales Register and Vendor Ledger are the standard
// report page now: nothing their old screens showed may be lost to the roles
// those screens served, and nobody else gains the amounts.
func TestStage1574bMovedReportsKeepWhatTheirScreensShowed(t *testing.T) {
	db.InitDB(testConnStr())
	for id, key := range map[string]string{"sales-register": "sale_total", "vendor-ledger": "total_amount"} {
		def := reportRegistry[id]
		for role, wantMasked := range map[string]bool{"Accountant": false, "Auditor": false, "Admin": false, "Cashier": true, "Picker": true} {
			rows := maskSensitiveColumns(def.Columns, []map[string]interface{}{{key: 5.0}}, role)
			if masked := rows[0][key] == redactedReportValue; masked != wantMasked {
				t.Errorf("%s: %s reads %v", id, role, rows[0][key])
			}
			if got := reportMasked(def.Columns, role); got != wantMasked {
				t.Errorf("%s: reportMasked(%s) = %v", id, role, got)
			}
		}
	}

	// Every column Current Stock declares comes back from both its paths.
	stock := reportRegistry["current-stock"]
	declared := map[string]bool{}
	for _, c := range stock.Columns {
		declared[c.Key] = true
	}
	for _, key := range []string{"available_to_sell", "hold_qty", "blocked", "qc_hold", "damaged", "channel_buffer"} {
		if !declared[key] {
			t.Errorf("Current Stock lost %s", key)
		}
	}
	rows, _, err := GetCurrentStockPage("default", CurrentStockFilter{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		for key := range declared {
			if _, ok := row[key]; !ok {
				t.Errorf("Current Stock declares %s but the row has none", key)
			}
		}
	}
}
