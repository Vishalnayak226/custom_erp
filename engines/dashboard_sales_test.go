package engines

import (
	"custom_erp/db"
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"
)

// 298.4: the all_sales dataset counts each channel once - a paid till bill
// at what was paid after offers, an online order's lines, a direct B2B
// invoice (not one raised from an order) - with a return as minus lines under
// the channel it came from; the % and per totals; a role sees only the
// channels it may read; and an admin can limit a ready-made dashboard to some
// roles.
func TestSalesDashboards(t *testing.T) {
	db.InitDB(testConnStr())
	const tenantID = "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Skipf("no default tenant: %v", err)
	}
	admin := DatasetViewer{Role: "Admin", UserID: "admin"}

	put := func(id, doctype, status string, data map[string]interface{}) {
		t.Helper()
		raw, _ := json.Marshal(data)
		db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", id)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system')", id, doctype, raw, status); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", id) })
	}
	item := func(sku string, qty, price float64) map[string]interface{} {
		return map[string]interface{}{"sku": sku, "qty": qty, "sale_price": price}
	}

	// An item at 5% GST, so margin backs the GST out.
	put("SKU-298T-A", "Item", "Active", map[string]interface{}{"name": "Test 298 shirt", "gst_rate": 5, "tax_treatment": "Taxable"})
	// Store: 2 at 500 listed, 900 paid after an offer, cost 400.
	put("CART-298T-1", "POSCart", "Paid", map[string]interface{}{"location": "ST-298T", "items": []interface{}{item("SKU-298T-A", 2, 500)},
		"allocation": []interface{}{map[string]interface{}{"sku": "SKU-298T-A", "qty": 2, "net_paise": 90000}},
		"sale_costs": []interface{}{map[string]interface{}{"sku": "SKU-298T-A", "qty": 2, "value_paise": 40000}}})
	// A bill not paid is not a sale.
	put("CART-298T-2", "POSCart", "Draft", map[string]interface{}{"location": "ST-298T", "items": []interface{}{item("SKU-298T-A", 9, 500)}})
	// Online: 3 at 100 on Amazon; a cancelled order does not count.
	put("SO-298T-1", "SalesOrder", "Confirmed", map[string]interface{}{"channel": "AMZ", "order_status": "Confirmed", "customer_name": "Asha"})
	put("SOL-298T-1", "SalesOrderLine", "Active", map[string]interface{}{"order_id": "SO-298T-1", "sku": "SKU-298T-B", "qty": 3, "unit_price": 100, "location_code": "ST-298T"})
	put("SO-298T-2", "SalesOrder", "Cancelled", map[string]interface{}{"channel": "AMZ", "order_status": "Cancelled"})
	put("SOL-298T-2", "SalesOrderLine", "Active", map[string]interface{}{"order_id": "SO-298T-2", "sku": "SKU-298T-B", "qty": 5, "unit_price": 100})
	// B2B: a direct invoice of 1000; one raised from the order is that
	// order's sale, already counted; a draft is not a sale.
	put("SINV-298T-1", "SalesInvoice", "Submitted", map[string]interface{}{"customer": "CUST-298T", "location": "ST-298T",
		"items": []interface{}{map[string]interface{}{"sku": "SKU-298T-C", "qty": 1, "rate": 1000}}})
	put("SINV-298T-2", "SalesInvoice", "Submitted", map[string]interface{}{"sales_order_id": "SO-298T-1", "location": "ST-298T",
		"items": []interface{}{map[string]interface{}{"sku": "SKU-298T-B", "qty": 3, "rate": 100}}})
	put("SINV-298T-3", "SalesInvoice", "Draft", map[string]interface{}{"location": "ST-298T",
		"items": []interface{}{map[string]interface{}{"sku": "SKU-298T-C", "qty": 7, "rate": 1000}}})
	// One of the two store pieces comes back: 450 refunded, cost 200.
	put("RR-298T-1", "ReturnRequest", "Approved", map[string]interface{}{"original_order_id": "CART-298T-1", "return_location": "ST-298T",
		"items": []interface{}{map[string]interface{}{"sku": "SKU-298T-A", "qty": 1, "allocated": true, "net_paise": 45000, "original_cost_price": 200}}})

	mine := []DatasetFilter{{Field: "sku", Op: "contains", Value: "SKU-298T-"}}
	num := func(v interface{}) float64 {
		switch x := v.(type) {
		case float64:
			return x
		case int64:
			return float64(x)
		}
		return math.NaN()
	}
	near := func(a, b float64) bool { return math.Abs(a-b) < 0.01 }

	res, err := RunDatasetQuery(tenantID, admin, DatasetQuery{Dataset: "all_sales", GroupBy: []string{"channel"}, Sort: "group", Filters: mine,
		Measures: []DatasetMeasure{dsSum("value"), dsSum("gross"), dsSum("returned"), dsSum("qty"), dsSum("returned_qty")}})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string][5]float64{}
	for _, r := range res.Rows {
		got[r["channel"].(string)] = [5]float64{num(r["sum_value"]), num(r["sum_gross"]), num(r["sum_returned"]), num(r["sum_qty"]), num(r["sum_returned_qty"])}
	}
	want := map[string][5]float64{
		"Store":  {450, 900, 450, 2, 1},
		"Online": {300, 300, 0, 3, 0},
		"B2B":    {1000, 1000, 0, 1, 0},
	}
	if len(got) != len(want) {
		t.Fatalf("one row per channel: %+v", res.Rows)
	}
	for ch, w := range want {
		for i := range w {
			if !near(got[ch][i], w[i]) {
				t.Errorf("%s: net, sales, returns, qty, returned qty = %v, want %v", ch, got[ch], w)
				break
			}
		}
	}

	// Returns %, margin % (ex GST: 857.14 - 428.57 sold net of the return,
	// less 400 - 200 cost) and the average bill (2200 over 3 bills).
	res, err = RunDatasetQuery(tenantID, admin, DatasetQuery{Dataset: "all_sales", Filters: mine, Measures: []DatasetMeasure{
		{Field: "returned", Agg: "pct", Of: "gross"}, {Field: "margin", Agg: "pct", Of: "costed_value"}, {Field: "gross", Agg: "per", Of: "bill"}}})
	if err != nil || len(res.Rows) != 1 {
		t.Fatalf("totals: %+v %v", res.Rows, err)
	}
	r := res.Rows[0]
	if v := num(r["pct_returned_gross"]); !near(v, 450.0/2200*100) {
		t.Errorf("returns %% = %v, want %.2f", v, 450.0/2200*100)
	}
	if v := num(r["pct_margin_costed_value"]); !near(v, 53.33) {
		t.Errorf("margin %% = %v, want 53.33", v)
	}
	if v := num(r["per_gross_bill"]); !near(v, 2200.0/3) {
		t.Errorf("average bill = %v, want %.2f", v, 2200.0/3)
	}
	if c := res.Columns[0]; c.Unit != "%" || c.Label != "Returns as % of sales" {
		t.Errorf("a share is labelled and marked %%: %+v", c)
	}
	if res.Columns[2].Money != true {
		t.Errorf("sales per bill is money: %+v", res.Columns[2])
	}

	// A role that may not see money gets the share hidden, not computed
	// from money it cannot see. Units per bill is not money; it counts the
	// channels the role may read (store 2 units, online 3, B2B 1; one bill each).
	cashier, err := RunDatasetQuery(tenantID, DatasetViewer{Role: "Cashier"}, DatasetQuery{Dataset: "all_sales", Filters: mine,
		Measures: []DatasetMeasure{{Field: "returned", Agg: "pct", Of: "gross"}, {Field: "qty", Agg: "per", Of: "bill"}}})
	if err == nil && len(cashier.Rows) == 1 {
		if cashier.Rows[0]["pct_returned_gross"] != nil || !cashier.Columns[0].Money {
			t.Errorf("a share of money is hidden from a cashier: %+v %+v", cashier.Rows, cashier.Columns)
		}
		units, bills := 0.0, 0.0
		for dt, n := range map[string]float64{"POSCart": 2, "SalesOrder": 3, "SalesInvoice": 1} {
			if ok, _ := RoleMayOnDoctype(tenantID, "Cashier", dt, "read"); ok {
				units, bills = units+n, bills+1
			}
		}
		if bills > 0 && !near(num(cashier.Rows[0]["per_qty_bill"]), units/bills) {
			t.Errorf("units per bill = %v, want %.2f over the channels a cashier reads", cashier.Rows[0]["per_qty_bill"], units/bills)
		}
	}

	// Wrong second fields are refused.
	for _, m := range []DatasetMeasure{
		{Field: "returned", Agg: "pct"},
		{Field: "returned", Agg: "pct", Of: "channel"},
		{Field: "gross", Agg: "per", Of: "qty"},
		{Field: "gross", Agg: "per", Of: "date"},
		{Field: "channel", Agg: "per", Of: "bill"},
	} {
		if _, err := RunDatasetQuery(tenantID, admin, DatasetQuery{Dataset: "all_sales", Measures: []DatasetMeasure{m}}); err == nil {
			t.Errorf("%+v is refused", m)
		}
	}

	// Each part shows only to a role that may read its records.
	ds, _ := DashboardDatasetByKey("all_sales")
	if ds.ReadableBy(func(string) bool { return false }) {
		t.Errorf("no readable part, no dataset")
	}
	if !ds.ReadableBy(func(dt string) bool { return dt == "POSCart" }) {
		t.Errorf("till bills alone are enough to open it")
	}
	for _, role := range []string{"Cashier", "Salesman", "Store Manager", "Warehouse", "Accountant"} {
		pos, _ := RoleMayOnDoctype(tenantID, role, "POSCart", "read")
		inv, _ := RoleMayOnDoctype(tenantID, role, "SalesInvoice", "read")
		if !pos || inv {
			continue
		}
		res, err := RunDatasetQuery(tenantID, DatasetViewer{Role: role}, DatasetQuery{Dataset: "all_sales", GroupBy: []string{"channel"}, Filters: mine, Measures: []DatasetMeasure{{Agg: "count"}}})
		if err != nil {
			t.Fatalf("%s: %v", role, err)
		}
		for _, r := range res.Rows {
			if r["channel"] == "B2B" {
				t.Errorf("%s may not read invoices but sees B2B lines: %+v", role, res.Rows)
			}
		}
		break
	}

	// 307.2: the query itself refuses a dataset the role may not read, so a
	// digest run as that role says so instead of holding the rows.
	refused := false
	for _, d := range dashboardDatasets {
		if len(d.parts) > 0 || refused {
			continue
		}
		for _, role := range []string{"Cashier", "Salesman", "Warehouse"} {
			if ok, _ := RoleMayOnDoctype(tenantID, role, d.Doctype, "read"); ok {
				continue
			}
			q := DatasetQuery{Dataset: d.Key, Measures: []DatasetMeasure{{Agg: "count"}}}
			if _, err := RunDatasetQuery(tenantID, DatasetViewer{Role: role}, q); err == nil {
				t.Errorf("%s may not read %s, yet dataset %s ran", role, d.Doctype, d.Key)
			}
			cd := CustomDashboard{Name: "t", Widgets: []DashboardWidget{{Title: "W", Source: "dataset", Kind: "number", Query: &q}}}
			if body, _, n := CustomDashboardDigestContent(tenantID, cd, DatasetViewer{Role: role}, time.Now()); n != 0 || !strings.Contains(body, "may not read") {
				t.Errorf("a digest run as %s holds dataset %s: %d rows, %.200s", role, d.Key, n, body)
			}
			refused = true
			break
		}
	}
	if !refused {
		t.Log("every role here reads every dataset; the refusal was not exercised")
	}

	// 307.3: a share reads as % in the digest and changes by points.
	share := DatasetColumn{Key: "pct_returned_gross", Label: "Returns as % of sales", Type: "number", Unit: "%"}
	mail := digestWidgetHTML(DashboardWidget{Kind: "number"}, DatasetResult{Columns: []DatasetColumn{share},
		Rows: []map[string]interface{}{{share.Key: 28.6}}, Previous: map[string]interface{}{share.Key: 17.4}})
	if !strings.Contains(mail, "28.6%") || !strings.Contains(mail, "11.2 points on the period before") {
		t.Errorf("digest share: %s", mail)
	}

	// Ready-made: blank shows to everyone; a role list limits it, and an
	// admin always sees it.
	key := SettingReadyDashboardRolesPrefix + "b2b_sales"
	before := GetSettingString(tenantID, key)
	t.Cleanup(func() { SetSetting(tenantID, key, before, "test") })
	if err := SetSetting(tenantID, key, "", "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := GetCustomDashboardFor(tenantID, "u9", "other", "Cashier", "ready:b2b_sales"); err != nil {
		t.Errorf("blank: a cashier opens B2B sales: %v", err)
	}
	if err := SetSetting(tenantID, key, "Accountant", "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := GetCustomDashboardFor(tenantID, "u9", "other", "Cashier", "ready:b2b_sales"); err == nil {
		t.Errorf("limited to Accountant: a cashier may not open B2B sales")
	}
	if _, err := GetCustomDashboardFor(tenantID, "u9", "other", "accountant", "ready:b2b_sales"); err != nil {
		t.Errorf("limited to Accountant: an accountant opens it: %v", err)
	}
	if _, err := GetCustomDashboardFor(tenantID, "admin", "admin", "Admin", "ready:b2b_sales"); err != nil {
		t.Errorf("an admin always opens it: %v", err)
	}
	list, err := ListCustomDashboards(tenantID, "u9", "other", "Cashier")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, d := range list {
		seen[d.ID] = true
	}
	if seen["ready:b2b_sales"] || !seen["ready:all_sales"] || !seen["ready:sales"] {
		t.Errorf("a cashier's list leaves out only B2B sales: %v", seen)
	}
}
