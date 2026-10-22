package engines

import (
	"custom_erp/db"
	"errors"
	"strings"
	"testing"
	"time"
)

// Stage 100.2-100.3: a custom dashboard saves and lists for its owner, is
// shared by user / role / store, opens as a role's home, copies into one the
// copier owns; only its maker changes it; every ready-made widget runs; the
// digest renders charts and a CSV; the dashboard's filters reach the widgets.
func TestStage1003CustomDashboards(t *testing.T) {
	db.InitDB(testConnStr())
	const tenantID = "default"
	code := func(err error) string {
		var v *ValidationError
		if errors.As(err, &v) {
			return v.Code
		}
		return ""
	}
	schema, _ := db.GetTenantSchema(tenantID)
	var made []string
	defer func() {
		for _, id := range made {
			db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", id)
		}
	}()

	if _, err := SaveCustomDashboard(tenantID, "u1003", "owner1003", "Store Manager", CustomDashboard{Name: " "}); code(err) != "REPORT-0989" {
		t.Errorf("a nameless dashboard: want REPORT-0989, got %v", err)
	}
	bad := CustomDashboard{Name: "Bad", Widgets: []DashboardWidget{{Title: "x", Kind: "pie3d", Source: "dataset", Query: &DatasetQuery{Dataset: "pos_bills"}}}}
	if _, err := SaveCustomDashboard(tenantID, "u1003", "owner1003", "Store Manager", bad); code(err) != "REPORT-0989" {
		t.Errorf("an unknown widget type: want REPORT-0989, got %v", err)
	}

	cd := CustomDashboard{Name: "Test 1003", Filters: DashboardFilters{Range: "last30"}, ShareRoles: []string{"Cashier"}, HomeRoles: []string{"Accountant"},
		Widgets: []DashboardWidget{{Title: "Bills", Kind: "number", Source: "dataset", Query: &DatasetQuery{Dataset: "pos_bills", Measures: []DatasetMeasure{{Agg: "count"}}}, W: 99}}}
	id, err := SaveCustomDashboard(tenantID, "u1003", "owner1003", "Store Manager", cd)
	if err != nil {
		t.Fatal(err)
	}
	made = append(made, id)
	got, err := GetCustomDashboardFor(tenantID, "u1003", "owner1003", "Store Manager", id)
	if err != nil || !got.Mine || !got.CanEdit || got.Widgets[0].W != 12 {
		t.Fatalf("the owner opens it, may change it, and the width is clamped to 12: %+v %v", got, err)
	}
	if _, err := GetCustomDashboardFor(tenantID, "u9", "other", "Cashier", id); err != nil {
		t.Errorf("shared with the Cashier role: %v", err)
	}
	if _, err := GetCustomDashboardFor(tenantID, "u9", "other", "Warehouse Operator", id); code(err) != "REPORT-0989" {
		t.Errorf("not shared with this role: want REPORT-0989, got %v", err)
	}
	if home, err := HomeDashboardFor(tenantID, "u8", "acc", "Accountant"); err != nil || home == nil || home.ID != id {
		t.Errorf("the Accountant role's home: %+v %v", home, err)
	}
	cd.ID, cd.Name = id, "Changed by someone else"
	if _, err := SaveCustomDashboard(tenantID, "u9", "other", "Cashier", cd); code(err) != "REPORT-0989" {
		t.Errorf("a viewer cannot change it: want REPORT-0989, got %v", err)
	}
	copyID, err := CopyCustomDashboard(tenantID, "u9", "other", "Cashier", id)
	if err != nil {
		t.Fatal(err)
	}
	made = append(made, copyID)
	if c, err := GetCustomDashboardFor(tenantID, "u9", "other", "Cashier", copyID); err != nil || !c.Mine || len(c.ShareRoles) != 0 || !strings.HasPrefix(c.Name, "Copy of ") {
		t.Errorf("the copy is the copier's own, unshared: %+v %v", c, err)
	}
	legacy, _ := ListDashboardLayouts(tenantID, "u1003", "Store Manager")
	for _, l := range legacy {
		if l["id"] == id {
			t.Errorf("a custom dashboard stays out of the report-tile layouts")
		}
	}

	// Every ready-made widget runs with its dashboard's filters.
	now := time.Now()
	for _, rm := range ReadyMadeDashboards() {
		for _, w := range rm.Widgets {
			q := ApplyDashboardFilters(*w.Query, rm.Filters, w.OwnFilters, now)
			if _, err := RunDatasetQuery(tenantID, DatasetViewer{Role: "Admin", UserID: "admin"}, q); err != nil {
				t.Errorf("%s / %s: %v", rm.Name, w.Title, err)
			}
		}
		if _, err := GetCustomDashboardFor(tenantID, "u9", "other", "Cashier", rm.ID); err != nil {
			t.Errorf("ready-made %s opens for anyone: %v", rm.ID, err)
		}
	}

	// The dashboard's filters: dates, store, brand where the dataset has them.
	f := DashboardFilters{Range: "custom", From: "2026-01-01", To: "2026-01-31", Store: "HO", Brand: "Acme", Channel: "Shopify"}
	q := ApplyDashboardFilters(DatasetQuery{Dataset: "sales_lines"}, f, false, now)
	if q.DateFrom != "2026-01-01" || q.Store != "HO" || len(q.Filters) != 1 || q.Filters[0].Field != "brand" {
		t.Errorf("sales lines take the dates, the store and the brand (no channel field): %+v", q)
	}
	if own := ApplyDashboardFilters(DatasetQuery{Dataset: "sales_lines"}, f, true, now); own.DateFrom != "" || own.Store != "" {
		t.Errorf("a widget with its own filters keeps them: %+v", own)
	}

	// The digest: a number and bars in HTML, the CSV beside it.
	body, csvText, _ := CustomDashboardDigestContent(tenantID, ReadyMadeDashboards()[0], DatasetViewer{Role: "Admin", UserID: "admin"}, now)
	if !strings.Contains(body, "<h3") || !strings.Contains(csvText, "## Sales") {
		t.Errorf("the digest holds each widget: %.200s / %.200s", body, csvText)
	}
	if _, err := DatasetResultXLSX("Bills", DatasetResult{Columns: []DatasetColumn{{Key: "count", Label: "Count", Type: "number"}}, Rows: []map[string]interface{}{{"count": 3.0}}}); err != nil {
		t.Errorf("Excel export: %v", err)
	}
}
