package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
	"time"
)

func TestStage3711Dashboards(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}

	cleanupDoctype := func(doctype string, ids ...string) {
		for _, id := range ids {
			db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1 AND doctype = $2", id, doctype)
		}
	}

	t.Run("SaveDashboardLayout rejects an empty name, owner, tile list, and a tile with no report_id", func(t *testing.T) {
		if _, err := SaveDashboardLayout(tenantID, "manager1", "", "", []DashboardTileSpec{{ReportID: "sla-breach"}}); err == nil {
			t.Fatalf("expected an empty name to be rejected")
		}
		if _, err := SaveDashboardLayout(tenantID, "", "My Dashboard", "", []DashboardTileSpec{{ReportID: "sla-breach"}}); err == nil {
			t.Fatalf("expected an empty owner to be rejected")
		}
		if _, err := SaveDashboardLayout(tenantID, "manager1", "My Dashboard", "", nil); err == nil {
			t.Fatalf("expected an empty tile list to be rejected")
		}
		if _, err := SaveDashboardLayout(tenantID, "manager1", "My Dashboard", "", []DashboardTileSpec{{Title: "No Report ID"}}); err == nil {
			t.Fatalf("expected a tile with no report_id to be rejected")
		}
	})

	t.Run("SaveDashboardLayout + ListDashboardLayouts: owner sees their own private layout, a stranger to the role does not, a role-mate does", func(t *testing.T) {
		layoutID, err := SaveDashboardLayout(tenantID, "manager1", "TEST3711A Private", "", []DashboardTileSpec{
			{ReportID: "sla-breach", Title: "SLA Breaches"},
		})
		if err != nil {
			t.Fatalf("SaveDashboardLayout: %v", err)
		}
		defer cleanupDoctype("DashboardLayout", layoutID)

		own, err := ListDashboardLayouts(tenantID, "manager1", "Store Manager")
		if err != nil {
			t.Fatalf("ListDashboardLayouts (owner): %v", err)
		}
		if !containsLayoutID(own, layoutID) {
			t.Fatalf("expected the owner to see their own private layout, got %+v", own)
		}

		stranger, err := ListDashboardLayouts(tenantID, "cashier1", "Cashier")
		if err != nil {
			t.Fatalf("ListDashboardLayouts (stranger): %v", err)
		}
		if containsLayoutID(stranger, layoutID) {
			t.Fatalf("expected a private layout to stay invisible to a non-owner outside its role, got %+v", stranger)
		}
	})

	t.Run("a role-shared layout is visible to anyone in that role, not just its owner", func(t *testing.T) {
		layoutID, err := SaveDashboardLayout(tenantID, "manager1", "TEST3711B Shared", "Cashier", []DashboardTileSpec{
			{ReportID: "sales-register", Title: "Sales Register"},
		})
		if err != nil {
			t.Fatalf("SaveDashboardLayout: %v", err)
		}
		defer cleanupDoctype("DashboardLayout", layoutID)

		roleMate, err := ListDashboardLayouts(tenantID, "cashier1", "Cashier")
		if err != nil {
			t.Fatalf("ListDashboardLayouts (role-mate): %v", err)
		}
		if !containsLayoutID(roleMate, layoutID) {
			t.Fatalf("expected a Cashier-shared layout to be visible to another Cashier, got %+v", roleMate)
		}

		outsider, err := ListDashboardLayouts(tenantID, "otheruser", "Store Manager")
		if err != nil {
			t.Fatalf("ListDashboardLayouts (outsider): %v", err)
		}
		if containsLayoutID(outsider, layoutID) {
			t.Fatalf("expected a Cashier-shared layout to stay invisible to a Store Manager, got %+v", outsider)
		}
	})

	t.Run("DeleteDashboardLayout only lets the owner delete their own layout", func(t *testing.T) {
		layoutID, err := SaveDashboardLayout(tenantID, "manager1", "TEST3711C Delete", "", []DashboardTileSpec{
			{ReportID: "sla-breach", Title: "SLA Breaches"},
		})
		if err != nil {
			t.Fatalf("SaveDashboardLayout: %v", err)
		}
		defer cleanupDoctype("DashboardLayout", layoutID)

		if err := DeleteDashboardLayout(tenantID, "cashier1", layoutID); err == nil {
			t.Fatalf("expected a non-owner delete to be refused")
		}
		if err := DeleteDashboardLayout(tenantID, "manager1", layoutID); err != nil {
			t.Fatalf("expected the owner's delete to succeed: %v", err)
		}
		after, err := ListDashboardLayouts(tenantID, "manager1", "Store Manager")
		if err != nil {
			t.Fatalf("ListDashboardLayouts (after delete): %v", err)
		}
		if containsLayoutID(after, layoutID) {
			t.Fatalf("expected the deleted layout to no longer be listed")
		}
	})

	t.Run("DefaultDashboardTiles returns the four legacy exec-dashboard cards as data", func(t *testing.T) {
		tiles := DefaultDashboardTiles()
		if len(tiles) != 4 {
			t.Fatalf("expected 4 default tiles, got %d: %+v", len(tiles), tiles)
		}
		for _, tile := range tiles {
			if tile.ReportID == "" || tile.Title == "" {
				t.Fatalf("expected every default tile to have both a report_id and a title, got %+v", tile)
			}
		}
	})

	t.Run("processDashboardDigests runs every tile in the referenced layout and advances the schedule", func(t *testing.T) {
		layoutID, err := SaveDashboardLayout(tenantID, "manager1", "TEST3711D Digest Layout", "", []DashboardTileSpec{
			{ReportID: "sla-breach", Title: "SLA Breaches"},
			{ReportID: "sales-register", Title: "Sales Register"},
		})
		if err != nil {
			t.Fatalf("SaveDashboardLayout: %v", err)
		}
		defer cleanupDoctype("DashboardLayout", layoutID)

		digestID := "TEST3711D-DIGEST"
		defer cleanupDoctype("DashboardDigest", digestID)
		yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
		digestData, _ := json.Marshal(map[string]interface{}{
			"dashboard_layout_id": layoutID, "frequency": "Daily", "requested_role": "Store Manager",
			"next_run_date": yesterday, "status": "Active",
		})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'DashboardDigest', $2, 'Active', 'system')",
			digestID, digestData); err != nil {
			t.Fatalf("seed DashboardDigest: %v", err)
		}

		processDashboardDigests(schema)

		var dataStr string
		if err := db.DB.QueryRow("SELECT data FROM "+schema+".documents WHERE id = $1", digestID).Scan(&dataStr); err != nil {
			t.Fatalf("reload DashboardDigest: %v", err)
		}
		var data map[string]interface{}
		_ = json.Unmarshal([]byte(dataStr), &data)
		if status, _ := data["last_run_status"].(string); status != "Delivered" {
			t.Fatalf("expected last_run_status to be Delivered, got %+v (full doc: %+v)", data["last_run_status"], data)
		}
		if nextRun, _ := data["next_run_date"].(string); nextRun <= yesterday {
			t.Fatalf("expected next_run_date to advance past %s, got %v", yesterday, nextRun)
		}
	})
}

func containsLayoutID(layouts []map[string]interface{}, id string) bool {
	for _, l := range layouts {
		if l["id"] == id {
			return true
		}
	}
	return false
}
