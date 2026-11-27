package engines

import (
	"custom_erp/db"
	"fmt"
	"testing"
)

// Item 273.1: sales velocity is one sum in the database. The single-SKU read,
// the one-store map and the every-store map must agree with each other and
// with what the old per-cart decode counted: paid and settled carts of the
// window, at that store, that SKU.
func TestSalesVelocityReadsOnceAndAgrees(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	seed := func(id, status, data, age string) {
		t.Helper()
		if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by, created_at)
			VALUES ($1, 'POSCart', $2::jsonb, $3, 'system', NOW() - $4::interval)`, schema), id, data, status, age); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
	seed("C1", "Paid", `{"location":"S1","items":[{"sku":"A","qty":6},{"sku":"B","qty":3}]}`, "1 day")
	seed("C2", "Settled", `{"location":"S1","items":[{"sku":"A","qty":9}]}`, "5 days")
	seed("C3", "Paid", `{"location":"S2","items":[{"sku":"A","qty":30}]}`, "2 days")
	seed("C4", "Open", `{"location":"S1","items":[{"sku":"A","qty":100}]}`, "1 day")                     // not sold yet
	seed("C5", "Paid", `{"location":"S1","items":[{"sku":"A","qty":100}]}`, "45 days")                   // outside the window
	seed("C6", "Paid", `{"location":"S1","items":[{"sku":"B","qty":"x"},{"sku":"A","qty":3}]}`, "1 day") // a bad qty skips only its line
	seed("C7", "Paid", `{"location":"S1","items":"none"}`, "1 day")                                      // no item list

	want := map[string]map[string]float64{
		"S1": {"A": 18.0 / 30, "B": 3.0 / 30},
		"S2": {"A": 30.0 / 30},
	}
	all, err := SalesVelocityAllLocations(tenantID, 30)
	if err != nil {
		t.Fatalf("SalesVelocityAllLocations: %v", err)
	}
	near := func(a, b float64) bool { d := a - b; return d < 1e-9 && d > -1e-9 }
	for loc, skus := range want {
		at, err := SalesVelocityAt(tenantID, loc, 30)
		if err != nil {
			t.Fatalf("SalesVelocityAt %s: %v", loc, err)
		}
		for sku, v := range skus {
			one, err := CalculateSalesVelocity(tenantID, loc, sku, 30)
			if err != nil {
				t.Fatalf("CalculateSalesVelocity %s/%s: %v", loc, sku, err)
			}
			if !near(one, v) || !near(at[sku], v) || !near(all[loc][sku], v) {
				t.Errorf("%s/%s: want %.4f, single %.4f, store map %.4f, all map %.4f", loc, sku, v, one, at[sku], all[loc][sku])
			}
		}
	}
	if v, _ := CalculateSalesVelocity(tenantID, "S2", "B", 30); v != 0 {
		t.Errorf("S2/B never sold, got %v", v)
	}
	if at, _ := SalesVelocityAt(tenantID, "S9", 30); len(at) != 0 {
		t.Errorf("a store with no sales reads empty, got %v", at)
	}
}

// Item 273.5 (ask 274): a client that bills by Sales Invoice has velocity too.
// Posted invoices count at their dispatch store, less returns against them;
// drafts, cancelled, deleted and old invoices do not, and till returns are not
// netted. Returns never push a store's figure below zero.
func TestSalesVelocityCountsInvoicesLessReturns(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	seed := func(id, doctype, status, data, age string) {
		t.Helper()
		if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by, created_at)
			VALUES ($1, $2, $3::jsonb, $4, 'system', NOW() - $5::interval)`, schema), id, doctype, data, status, age); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
	seed("SV-C1", "POSCart", "Paid", `{"location":"S1","items":[{"sku":"A","qty":6}]}`, "1 day")
	seed("SV-I1", "SalesInvoice", "Approved", `{"location":"S1","items":[{"sku":"A","qty":10},{"sku":"B","qty":4}]}`, "2 days")
	seed("SV-I2", "SalesInvoice", "Paid", `{"location":"S2","sales_order_id":"SV-SO1","items":"[{\"sku\":\"A\",\"qty\":5}]"}`, "3 days") // lines as a string
	seed("SV-I3", "SalesInvoice", "Draft", `{"location":"S1","items":[{"sku":"A","qty":100}]}`, "1 day")
	seed("SV-I4", "SalesInvoice", "Cancelled", `{"location":"S1","items":[{"sku":"A","qty":100}]}`, "1 day")
	seed("SV-I5", "SalesInvoice", "Approved", `{"location":"S1","items":[{"sku":"A","qty":100}]}`, "45 days")
	seed("SV-I6", "SalesInvoice", "Approved", `{"location":"S1","items":[{"sku":"A","qty":100}]}`, "1 day")
	seed("SV-I7", "SalesInvoice", "Approved", `{"location":"S3","items":[{"sku":"A","qty":1}]}`, "1 day")
	if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET deleted_at = NOW() WHERE id = 'SV-I6'`, schema)); err != nil {
		t.Fatalf("delete SV-I6: %v", err)
	}
	seed("SV-R1", "ReturnRequest", "Closed", `{"status":"Closed","original_order_id":"SV-I1","items":[{"sku":"A","qty":3}]}`, "1 day")
	seed("SV-R2", "ReturnRequest", "Rejected", `{"status":"Rejected","original_order_id":"SV-I1","items":[{"sku":"A","qty":4}]}`, "1 day")
	seed("SV-R3", "SalesReturn", "Returned", `{"original_order_id":"SV-I1","items":[{"sku":"B","qty":4}]}`, "1 day")
	seed("SV-R4", "ReturnRequest", "Requested", `{"status":"Requested","original_order_id":"SV-SO1","items":"[{\"sku\":\"A\",\"qty\":2}]"}`, "1 day") // an RTO names the order
	seed("SV-R5", "ReturnRequest", "Closed", `{"status":"Closed","original_order_id":"SV-C1","items":[{"sku":"A","qty":6}]}`, "1 day")           // a till return
	seed("SV-R6", "ReturnRequest", "Closed", `{"status":"Closed","original_order_id":"SV-I5","items":[{"sku":"A","qty":50}]}`, "1 day")          // its invoice is outside the window
	seed("SV-R7", "ReturnRequest", "Closed", `{"status":"Closed","original_order_id":"SV-I7","items":[{"sku":"A","qty":5}]}`, "1 day")           // more back than billed

	want := map[string]map[string]float64{
		"S1": {"A": (6 + 10 - 3) / 30.0, "B": 0},
		"S2": {"A": (5 - 2) / 30.0},
		"S3": {"A": 0},
	}
	all, err := SalesVelocityAllLocations(tenantID, 30)
	if err != nil {
		t.Fatalf("SalesVelocityAllLocations: %v", err)
	}
	near := func(a, b float64) bool { d := a - b; return d < 1e-9 && d > -1e-9 }
	for loc, skus := range want {
		at, err := SalesVelocityAt(tenantID, loc, 30)
		if err != nil {
			t.Fatalf("SalesVelocityAt %s: %v", loc, err)
		}
		for sku, v := range skus {
			one, err := CalculateSalesVelocity(tenantID, loc, sku, 30)
			if err != nil {
				t.Fatalf("CalculateSalesVelocity %s/%s: %v", loc, sku, err)
			}
			if !near(one, v) || !near(at[sku], v) || !near(all[loc][sku], v) {
				t.Errorf("%s/%s: want %.4f, single %.4f, store map %.4f, all map %.4f", loc, sku, v, one, at[sku], all[loc][sku])
			}
		}
	}
}
