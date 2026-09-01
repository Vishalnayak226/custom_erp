package engines

import (
	"context"
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"
)

// Stage 37.11: role dashboards with savable layouts and scheduled digests.
//
// DashboardLayout follows the OMSSavedView precedent (engines/oms_console.go)
// exactly - a plain document the engine writes directly rather than routing
// through the generic doc-create API, giving it RBAC/audit for free without
// a bespoke preferences table. The one addition beyond OMSSavedView's shape
// is an optional `role`: a layout saved with a role is a shared default for
// everyone holding that role ("role dashboards"), while an unset role keeps
// it private to its owner ("savable layouts") - both halves of 37.11.1/
// 37.11.2 live in the same doctype and the same List call.
//
// DashboardDigest (37.11.3) is the ScheduledReport precedent
// (engines/scheduled_reports.go) applied to a layout instead of a single
// report: a plain registered doctype the generic doc API already handles
// create/list/delete for, with this file's worker as the only writer back to
// it. It runs every tile in the referenced layout and delivers one combined
// CSV through the existing outbox, reusing RunReport/reportRowsToCSV rather
// than any new export mechanism.

// DashboardTileSpec is one tile in a saved layout: which registered report
// to run, and the label to show above it.
type DashboardTileSpec struct {
	ReportID string `json:"report_id"`
	Title    string `json:"title"`
}

// DefaultDashboardTiles is the fallback layout shown to anyone with no saved
// or role-default DashboardLayout of their own - the same four exception
// reports the hardcoded exec dashboard (renderExecDashboard, public/app.js)
// has shown since Stage 20, now expressed as data instead of literal code so
// a saved layout can override it without a separate code path.
func DefaultDashboardTiles() []DashboardTileSpec {
	return []DashboardTileSpec{
		{ReportID: "exception-stale-approvals", Title: "Stale Approvals"},
		{ReportID: "exception-failed-syncs", Title: "Failed Syncs"},
		{ReportID: "exception-negative-stock", Title: "Negative Stock Flags"},
		{ReportID: "sla-breach", Title: "SLA Breaches"},
	}
}

// SaveDashboardLayout stores a named set of report tiles. role is optional -
// leave it blank for a private layout, or set it to make this layout appear
// as a shared default for everyone holding that role.
func SaveDashboardLayout(tenantID, userID, name, role string, tiles []DashboardTileSpec) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("a dashboard layout needs a name")
	}
	if strings.TrimSpace(userID) == "" {
		return "", fmt.Errorf("a dashboard layout needs an owner")
	}
	if len(tiles) == 0 {
		return "", fmt.Errorf("a dashboard layout needs at least one tile")
	}
	for _, tile := range tiles {
		if strings.TrimSpace(tile.ReportID) == "" {
			return "", fmt.Errorf("every tile needs a report_id")
		}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	tilesJSON, err := json.Marshal(tiles)
	if err != nil {
		return "", err
	}
	layoutID := NewDocID("DBL")
	doc, err := json.Marshal(map[string]interface{}{
		"code": layoutID, "name": name, "owner": userID, "role": strings.TrimSpace(role),
		"tiles_json": string(tilesJSON),
	})
	if err != nil {
		return "", err
	}
	// created_by is 'system', not userID, matching SaveOMSView's own note:
	// the owner this layout is scoped by lives in data->>'owner', and
	// created_by carries a users FK that an engine-internal caller may not
	// satisfy.
	if _, err := db.DB.Exec(fmt.Sprintf(
		`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'DashboardLayout', $2, 'Active', 'system')`, schema),
		layoutID, doc); err != nil {
		return "", err
	}
	return layoutID, nil
}

// ListDashboardLayouts returns the layouts visible to a user: their own,
// plus any role-shared default for the role passed in. Not built on
// queryDocs (engines/oms_console.go) - that helper only takes one query
// argument, and this needs two (owner and role).
func ListDashboardLayouts(tenantID, userID, role string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT d.id, COALESCE(d.data->>'name', ''), COALESCE(d.data->>'tiles_json', '[]'), COALESCE(d.data->>'owner', ''), COALESCE(d.data->>'role', '')
		FROM %s.documents d
		WHERE d.doctype = 'DashboardLayout' AND d.deleted_at IS NULL AND d.status = 'Active'
		  AND (COALESCE(d.data->>'owner', '') = $1 OR COALESCE(d.data->>'role', '') = $2)
		ORDER BY d.created_at DESC`, schema), userID, role)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]interface{}{}
	for rows.Next() {
		var id, name, tilesJSON, owner, layoutRole string
		if err := rows.Scan(&id, &name, &tilesJSON, &owner, &layoutRole); err != nil {
			return nil, err
		}
		var tiles []DashboardTileSpec
		_ = json.Unmarshal([]byte(tilesJSON), &tiles)
		out = append(out, map[string]interface{}{"id": id, "name": name, "owner": owner, "role": layoutRole, "tiles": tiles})
	}
	return out, rows.Err()
}

// DeleteDashboardLayout soft-deletes a layout, and only the owner's own -
// a role-shared layout is still only editable by whoever saved it.
func DeleteDashboardLayout(tenantID, userID, layoutID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	res, err := db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET deleted_at = CURRENT_TIMESTAMP WHERE id = $1 AND doctype = 'DashboardLayout' AND COALESCE(data->>'owner', '') = $2 AND deleted_at IS NULL`, schema),
		layoutID, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("dashboard layout %s not found", layoutID)
	}
	return nil
}

// getDashboardLayoutTiles reads a layout's tiles directly by schema (not
// tenantID), for the digest worker below which already iterates schemas the
// way every other schema-fanout worker in this codebase does.
func getDashboardLayoutTiles(schema, layoutID string) ([]DashboardTileSpec, error) {
	var tilesJSON string
	err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT COALESCE(data->>'tiles_json', '[]') FROM %s.documents WHERE id = $1 AND doctype = 'DashboardLayout' AND deleted_at IS NULL`, schema),
		layoutID).Scan(&tilesJSON)
	if err != nil {
		return nil, err
	}
	var tiles []DashboardTileSpec
	if err := json.Unmarshal([]byte(tilesJSON), &tiles); err != nil {
		return nil, err
	}
	return tiles, nil
}

// StartDashboardDigestWorker polls every tenant schema for Active
// DashboardDigest documents whose next_run_date has arrived - identical
// ticker/schema-fanout shape to StartScheduledReportWorker.
func StartDashboardDigestWorker(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if db.DB == nil {
					continue
				}
				schemas, err := listTenantSchemas()
				if err != nil {
					log.Printf("[DASHBOARD_DIGEST] Failed to list tenant schemas: %v", err)
					continue
				}
				for _, schema := range schemas {
					processDashboardDigests(schema)
				}
			}
		}
	}()
}

func processDashboardDigests(schema string) {
	rows, err := db.DB.Query(fmt.Sprintf(
		`SELECT id, data FROM %s.documents WHERE doctype = 'DashboardDigest' AND status = 'Active'`, schema))
	if err != nil {
		log.Printf("[DASHBOARD_DIGEST] query failed for %s: %v", schema, err)
		return
	}
	type dueDigest struct {
		id   string
		data map[string]interface{}
	}
	var due []dueDigest
	today := time.Now().Format("2006-01-02")
	for rows.Next() {
		var id, dataStr string
		if err := rows.Scan(&id, &dataStr); err != nil {
			continue
		}
		var data map[string]interface{}
		if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
			log.Printf("[DASHBOARD_DIGEST] corrupt DashboardDigest %s: %v", id, err)
			continue
		}
		nextRun, _ := data["next_run_date"].(string)
		if nextRun != "" && nextRun <= today {
			due = append(due, dueDigest{id: id, data: data})
		}
	}
	rows.Close()

	for _, d := range due {
		layoutID, _ := d.data["dashboard_layout_id"].(string)
		role, _ := d.data["requested_role"].(string)
		runStatus := "Delivered"

		tiles, terr := getDashboardLayoutTiles(schema, layoutID)
		if terr != nil {
			runStatus = "Failed: " + terr.Error()
		} else if len(tiles) == 0 {
			runStatus = "Failed: dashboard layout has no tiles"
		} else {
			var combinedCSV strings.Builder
			totalRows := 0
			for _, tile := range tiles {
				def, resultRows, _, rerr := RunReport(schema, tile.ReportID, role, "", nil)
				combinedCSV.WriteString("## " + tile.Title + "\n")
				if rerr != nil {
					combinedCSV.WriteString("(failed: " + rerr.Error() + ")\n\n")
					continue
				}
				csvText, cerr := reportRowsToCSV(*def, resultRows)
				if cerr != nil {
					combinedCSV.WriteString("(failed: " + cerr.Error() + ")\n\n")
					continue
				}
				combinedCSV.WriteString(csvText)
				combinedCSV.WriteString("\n\n")
				totalRows += len(resultRows)
			}

			recipientEmail, _ := d.data["recipient_email"].(string)
			webhookURL, _ := d.data["webhook_url"].(string)
			tx, txErr := db.DB.Begin()
			if txErr == nil {
				_ = db.SetSearchPath(tx, schema)
				if perr := PublishEvent(tx, schema, "dashboard.digest_delivery", map[string]interface{}{
					"digest_id": d.id, "dashboard_layout_id": layoutID, "tile_count": len(tiles), "row_count": totalRows,
					"recipient_email": recipientEmail, "webhook_url": webhookURL, "csv": combinedCSV.String(),
				}); perr == nil {
					_ = tx.Commit()
				} else {
					_ = tx.Rollback()
					runStatus = "Failed: " + perr.Error()
				}
			} else {
				runStatus = "Failed: " + txErr.Error()
			}
		}

		nextRun, _ := d.data["next_run_date"].(string)
		frequency, _ := d.data["frequency"].(string)
		d.data["next_run_date"] = advanceScheduleDate(nextRun, frequency)
		d.data["last_run_at"] = time.Now().Format(time.RFC3339)
		d.data["last_run_status"] = runStatus
		marshaled, merr := json.Marshal(d.data)
		if merr != nil {
			log.Printf("[DASHBOARD_DIGEST] marshal failed for %s: %v", d.id, merr)
			continue
		}
		if _, err := db.DB.Exec(fmt.Sprintf(
			`UPDATE %s.documents SET data = $1, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'DashboardDigest' AND id = $2`, schema),
			marshaled, d.id); err != nil {
			log.Printf("[DASHBOARD_DIGEST] update failed for %s: %v", d.id, err)
		}
	}
}
