package engines

import (
	"custom_erp/db"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// Stage 47.14.3 (live confidence): open reconciliation mismatches in a
// widget's domain make its contract say so instead of the static basis.
func TestStage47143LiveConfidenceNamesOpenMismatches(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	insert := func(id, doctype, status string, data map[string]interface{}) {
		t.Helper()
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system')", id, doctype, raw, status); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	items := func(qty int) string {
		b, _ := json.Marshal([]map[string]interface{}{{"sku": "SKU-1", "qty": qty}})
		return string(b)
	}
	salesDS, _ := DashboardDatasetByKey("sales_lines")
	stockDS, _ := DashboardDatasetByKey("stock_on_hand")
	contract := func() (KPIContract, KPIContract, KPIContract) {
		sales := WithLiveConfidence(tenantID, buildKPIContract(salesDS, DatasetQuery{}, false, nil, time.Now()), DatasetConfidenceDomain("sales_lines"), "")
		stock := WithLiveConfidence(tenantID, buildKPIContract(stockDS, DatasetQuery{}, false, nil, time.Now()), DatasetConfidenceDomain("stock_on_hand"), "")
		rep := reportRegistry["order-to-cash"]
		report := WithLiveConfidence(tenantID, BuildReportContract(rep, nil, false, nil, time.Now()), ReportConfidenceDomain(rep), "")
		return sales, stock, report
	}
	// nothing open: the static basis stays
	sales, _, report := contract()
	if sales.Confidence != "" || report.Confidence != "" || sales.OpenMismatches != 0 {
		t.Fatalf("clean book: confidence = %q / %q, want the static basis", sales.Confidence, report.Confidence)
	}

	// one order shipped 5, invoiced 3
	insert("SO-A", "SalesOrder", "Confirmed", map[string]interface{}{"customer": "C1"})
	insert("SP-A", "ShippingPackage", "Shipped", map[string]interface{}{"order_id": "SO-A", "items": items(5)})
	insert("SI-A", "SalesInvoice", "Approved", map[string]interface{}{"sales_order_id": "SO-A", "items": items(3), "total_amount": 300.0, "customer": "C1"})
	sales, stock, report := contract()
	if sales.OpenMismatches != 1 || !strings.Contains(sales.Confidence, "1 open mismatch") {
		t.Errorf("sales widget: %+v, want 1 open mismatch", sales)
	}
	if report.OpenMismatches != 1 {
		t.Errorf("order-to-cash report: %+v, want 1 open mismatch", report)
	}
	if stock.OpenMismatches != 0 || stock.Confidence != "" {
		t.Errorf("stock widget touched by a cash mismatch: %+v", stock)
	}
	if sales.Basis == "" || sales.BasisNote == "" {
		t.Errorf("the static basis fields must stay: %+v", sales)
	}

	// a dismissed mismatch is no longer open
	if err := SetReconciliationException(tenantID, "Shipped not invoiced", "SO-A", "Dismissed", "second invoice follows", "", "asha"); err != nil {
		t.Fatal(err)
	}
	sales, _, _ = contract()
	if sales.OpenMismatches != 0 || sales.Confidence != "" {
		t.Errorf("after dismissing: %+v, want the static basis", sales)
	}

	// stock ownership touches the stock widget, and a store scope narrows it
	for _, q := range []string{
		"INSERT INTO " + schema + ".bin_stock_owner (bin_code, sku, owner_id, location_code, qty) VALUES ('B1','A','OWN-A','LOC-1',4),('B2','A','OWN-B','LOC-1',6)",
	} {
		if _, err := db.DB.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	stock = WithLiveConfidence(tenantID, buildKPIContract(stockDS, DatasetQuery{}, false, nil, time.Now()), DatasetConfidenceDomain("stock_on_hand"), "")
	if stock.OpenMismatches != 1 || !strings.Contains(stock.Confidence, "1 open mismatch") {
		t.Errorf("stock widget: %+v, want 1 open mismatch", stock)
	}
	other := WithLiveConfidence(tenantID, buildKPIContract(stockDS, DatasetQuery{}, false, nil, time.Now()), DatasetConfidenceDomain("stock_on_hand"), "LOC-9")
	if other.OpenMismatches != 0 {
		t.Errorf("a widget scoped to another store counted LOC-1: %+v", other)
	}
}
