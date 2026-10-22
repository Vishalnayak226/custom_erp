package engines

import (
	"custom_erp/db"
	"encoding/json"
	"errors"
	"testing"
)

// Stage 100.1: every field of every dataset runs against the real schema
// (grouped, totalled, filtered, by date); a store-limited viewer sees only
// their stores; money is hidden from a role that may not see it; nothing
// outside the list is accepted.
func TestStage1001DashboardDatasets(t *testing.T) {
	db.InitDB(testConnStr())
	const tenantID = "default"
	admin := DatasetViewer{Role: "Admin", UserID: "admin"}

	// Every field, through the SQL it builds.
	for _, ds := range dashboardDatasets {
		var measures []DatasetMeasure
		for _, fl := range ds.Fields {
			if fl.Type == "number" {
				measures = append(measures, DatasetMeasure{Field: fl.Key, Agg: "sum"})
			}
		}
		if len(measures) > 6 {
			measures = measures[:6]
		}
		measures = append([]DatasetMeasure{{Agg: "count"}}, measures...)
		if len(measures) > 6 {
			measures = measures[:6]
		}
		for _, fl := range ds.Fields {
			if fl.Type == "number" {
				continue
			}
			q := DatasetQuery{Dataset: ds.Key, GroupBy: []string{fl.Key}, Grain: "month", Measures: measures, Limit: 5,
				Filters: []DatasetFilter{{Field: fl.Key, Op: "contains", Value: "x"}}}
			if ds.DateField != "" {
				q.DateFrom, q.DateTo = "2020-01-01", "2030-12-31"
			}
			if _, err := RunDatasetQuery(tenantID, admin, q); err != nil {
				t.Errorf("%s by %s: %v", ds.Key, fl.Key, err)
			}
		}
	}

	code := func(err error) string {
		var v *ValidationError
		if errors.As(err, &v) {
			return v.Code
		}
		return ""
	}
	for _, q := range []DatasetQuery{
		{Dataset: "no_such"},
		{Dataset: "pos_bills", GroupBy: []string{"value"}},
		{Dataset: "pos_bills", Measures: []DatasetMeasure{{Field: "store", Agg: "sum"}}},
		{Dataset: "pos_bills", Measures: []DatasetMeasure{{Agg: "drop table"}}},
		{Dataset: "pos_bills", Filters: []DatasetFilter{{Field: "store; --", Op: "eq", Value: "x"}}},
		{Dataset: "pos_bills", Filters: []DatasetFilter{{Field: "store", Op: "regex", Value: "x"}}},
	} {
		if _, err := RunDatasetQuery(tenantID, admin, q); code(err) != "REPORT-0988" {
			t.Errorf("%+v: want REPORT-0988, got %v", q, err)
		}
	}

	// Two paid bills at two stores; a value filter keeps them apart from any
	// other test data.
	schema, _ := db.GetTenantSchema(tenantID)
	const other = "OTHER-1001"
	before, err := GetUserStoreAccess(tenantID, "manager1")
	if err != nil {
		t.Skipf("no manager1 user in this database: %v", err)
	}
	if err := SetUserStoreAccess(tenantID, "manager1", StoreAccessHome, nil, "test"); err != nil {
		t.Fatal(err)
	}
	defer SetUserStoreAccess(tenantID, "manager1", before.Access, before.Stores, "test")
	access, limited := reportStoreScope(tenantID, "Store Manager", "manager1")
	if !limited {
		t.Fatalf("manager1 on the home store is store-limited, got %+v", access)
	}
	for id, store := range map[string]string{"CART-1001-A": access.Home, "CART-1001-B": other} {
		raw, _ := json.Marshal(map[string]interface{}{"location": store, "amount_paid": 1001.5, "payment_mode": "T1001",
			"items": []map[string]interface{}{{"sku": "SKU-1001", "qty": 2, "sale_price": 500.75}}})
		db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", id)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'POSCart', $2, 'Paid', 'system')", id, raw); err != nil {
			t.Fatal(err)
		}
		defer db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", id)
	}
	q := DatasetQuery{Dataset: "pos_bills", GroupBy: []string{"store"}, Measures: []DatasetMeasure{{Agg: "count"}, {Field: "value", Agg: "sum"}},
		Filters: []DatasetFilter{{Field: "payment_mode", Op: "eq", Value: "T1001"}}}
	all, err := RunDatasetQuery(tenantID, admin, q)
	if err != nil || len(all.Rows) != 2 {
		t.Fatalf("an admin sees both stores: %+v %v", all.Rows, err)
	}
	mine, err := RunDatasetQuery(tenantID, DatasetViewer{Role: "Store Manager", UserID: "manager1"}, q)
	if err != nil || len(mine.Rows) != 1 || mine.Rows[0]["sum_value"] != 1001.5 {
		t.Fatalf("a store-limited manager sees only the home store's bill, with its value: %+v %v", mine.Rows, err)
	}
	q.Store = other
	if _, err := RunDatasetQuery(tenantID, DatasetViewer{Role: "Store Manager", UserID: "manager1"}, q); code(err) != "REPORT-0987" {
		t.Errorf("asking for another store: want REPORT-0987, got %v", err)
	}
	q.Store = ""
	cashier, err := RunDatasetQuery(tenantID, DatasetViewer{Role: "Cashier", UserID: ""}, q)
	if err != nil || len(cashier.Rows) == 0 || cashier.Rows[0]["sum_value"] != nil || cashier.Rows[0]["count"] == nil {
		t.Errorf("a cashier gets the count but no money: %+v %v", cashier.Rows, err)
	}
	lines, err := RunDatasetQuery(tenantID, admin, DatasetQuery{Dataset: "sales_lines", Measures: []DatasetMeasure{{Field: "qty", Agg: "sum"}, {Field: "value", Agg: "sum"}},
		Filters: []DatasetFilter{{Field: "sku", Op: "eq", Value: "SKU-1001"}}})
	if err != nil || len(lines.Rows) != 1 || lines.Rows[0]["sum_qty"] != 4.0 || lines.Rows[0]["sum_value"] != 2003.0 {
		t.Errorf("sales lines total the bills' lines (4 units, 2003): %+v %v", lines.Rows, err)
	}
	if ds := ListDashboardDatasets("Cashier", nil); len(ds) == 0 {
		t.Errorf("the list is offered")
	} else {
		for _, d := range ds {
			for _, fl := range d.Fields {
				if fl.Money {
					t.Errorf("%s.%s is money and offered to a cashier", d.Key, fl.Key)
				}
			}
		}
	}
}
