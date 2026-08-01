package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
	"time"
)

// Stage 26.10 (Reports and BI Sprint) engine tests. Kept in its own file,
// same convention wms_enterprise_test.go established for staying out of
// engines_test.go while other concurrent work is mid-editing it.
func TestReportsStage26_10(t *testing.T) {
	connStr := testConnStr()
	db.InitDB(connStr)

	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("Failed to get tenant schema: %v", err)
	}

	t.Run("StockLedgerWiredThroughPostInventoryLedger", func(t *testing.T) {
		sku := "SKU-RPT2610-LEDGER"
		location := "WH-RPT2610"
		_, _ = db.DB.Exec("DELETE FROM "+schema+".inventory_availability WHERE sku = $1", sku)
		_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'StockLedgerEntry' AND data->>'item_id' = $1", sku)

		items := []interface{}{map[string]interface{}{"sku": sku, "qty": 20.0}}
		if _, err := PostInventoryLedgerWithVoucher(tenantID, location, items, false, "GRN", "GRN-RPT2610-1", "tester"); err != nil {
			t.Fatalf("PostInventoryLedgerWithVoucher failed: %v", err)
		}

		rows, err := GetStockLedgerReport(tenantID, sku, location, "", "", "")
		if err != nil {
			t.Fatalf("GetStockLedgerReport failed: %v", err)
		}
		if len(rows) != 1 {
			t.Fatalf("expected 1 ledger row, got %d", len(rows))
		}
		if rows[0]["voucher_type"] != "GRN" || rows[0]["voucher_id"] != "GRN-RPT2610-1" {
			t.Errorf("expected voucher_type=GRN voucher_id=GRN-RPT2610-1, got %v/%v", rows[0]["voucher_type"], rows[0]["voucher_id"])
		}
		if rows[0]["running_balance"] != 20.0 {
			t.Errorf("expected running_balance=20, got %v", rows[0]["running_balance"])
		}

		// Idempotency: a retried post with the same voucher tags must not
		// double-write a second ledger line for the same SKU/location.
		if _, err := PostInventoryLedgerWithVoucher(tenantID, location, items, false, "GRN", "GRN-RPT2610-1", "tester"); err != nil {
			t.Fatalf("second PostInventoryLedgerWithVoucher failed: %v", err)
		}
		rows2, err := GetStockLedgerReport(tenantID, sku, location, "", "", "")
		if err != nil {
			t.Fatalf("GetStockLedgerReport (after retry) failed: %v", err)
		}
		if len(rows2) != 1 {
			t.Errorf("expected idempotent retry to still leave exactly 1 ledger row, got %d", len(rows2))
		}
	})

	t.Run("StockLedgerConditionChangeAndPutaway", func(t *testing.T) {
		sku := "SKU-RPT2610-COND"
		binGood := "BIN-RPT2610-GOOD"
		binDamaged := "BIN-RPT2610-DAM"
		location := "WH-RPT2610-B"
		_, _ = db.DB.Exec("DELETE FROM "+schema+".inventory_availability WHERE sku = $1", sku)
		_, _ = db.DB.Exec("DELETE FROM "+schema+".bin_stock WHERE sku = $1", sku)
		_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'Bin' AND data->>'bin_code' IN ($1, $2)", binGood, binDamaged)
		_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'StockLedgerEntry' AND data->>'item_id' = $1", sku)

		for _, b := range []string{binGood, binDamaged} {
			binData, _ := json.Marshal(map[string]interface{}{"bin_code": b, "location": location})
			if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Bin', $2, 'Active', 'system')", "BINDOC-"+b, binData); err != nil {
				t.Fatalf("failed to seed bin %s: %v", b, err)
			}
		}

		if _, err := PostInventoryLedgerWithVoucher(tenantID, location, []interface{}{map[string]interface{}{"sku": sku, "qty": 15.0}}, false, "GRN", "GRN-RPT2610-COND", "tester"); err != nil {
			t.Fatalf("seeding on-hand stock failed: %v", err)
		}
		if err := PutawayToBin(tenantID, binGood, sku, 15, "tester"); err != nil {
			t.Fatalf("PutawayToBin failed: %v", err)
		}
		if err := TransitionBinStockCondition(tenantID, binGood, sku, 4, "Good", "Damaged", "tester"); err != nil {
			t.Fatalf("TransitionBinStockCondition failed: %v", err)
		}

		rows, err := GetStockLedgerReport(tenantID, sku, location, "", "", "")
		if err != nil {
			t.Fatalf("GetStockLedgerReport failed: %v", err)
		}
		var sawPutaway, sawCondition bool
		for _, r := range rows {
			switch r["voucher_type"] {
			case "Putaway":
				sawPutaway = true
				if r["to_location_id"] != binGood {
					t.Errorf("expected putaway to_location_id=%s, got %v", binGood, r["to_location_id"])
				}
			case "ConditionChange":
				sawCondition = true
				if r["from_status"] != "Good" || r["to_status"] != "Damaged" {
					t.Errorf("expected Good->Damaged, got %v->%v", r["from_status"], r["to_status"])
				}
				if r["qty"] != -4.0 {
					t.Errorf("expected qty=-4 (left the Good/available bucket), got %v", r["qty"])
				}
			}
		}
		if !sawPutaway {
			t.Error("expected a Putaway stock ledger entry")
		}
		if !sawCondition {
			t.Error("expected a ConditionChange stock ledger entry")
		}
	})

	t.Run("ExceptionQueues", func(t *testing.T) {
		// Stale approval: a Pending Approval document backdated well past
		// any reasonable threshold.
		staleID := "RPT2610-STALE-1"
		_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", staleID)
		itemData, _ := json.Marshal(map[string]interface{}{"code": staleID, "name": "Stale Test Item"})
		longAgo := time.Now().Add(-72 * time.Hour)
		if _, err := db.DB.Exec(
			"INSERT INTO "+schema+".documents (id, doctype, data, status, created_by, created_at, updated_at) VALUES ($1, 'Item', $2, 'Pending Approval', 'system', $3, $3)",
			staleID, itemData, longAgo); err != nil {
			t.Fatalf("failed to seed stale approval fixture: %v", err)
		}
		stale, err := GetStaleApprovals(tenantID, 24)
		if err != nil {
			t.Fatalf("GetStaleApprovals failed: %v", err)
		}
		found := false
		for _, s := range stale {
			if s.ID == staleID {
				found = true
			}
		}
		if !found {
			t.Errorf("expected %s to appear in stale approvals past 24h threshold", staleID)
		}

		// Failed sync.
		var eventID string
		if err := db.DB.QueryRow("INSERT INTO " + schema + ".integration_event_outbox (event_name, payload, status, attempts) VALUES ('rpt2610.test_event', '{}', 'Failed', 4) RETURNING id").Scan(&eventID); err != nil {
			t.Fatalf("failed to seed failed-sync fixture: %v", err)
		}
		failed, err := GetFailedSyncs(tenantID)
		if err != nil {
			t.Fatalf("GetFailedSyncs failed: %v", err)
		}
		found = false
		for _, f := range failed {
			if f.ID == eventID {
				found = true
			}
		}
		if !found {
			t.Errorf("expected event %s to appear in failed syncs", eventID)
		}

		// Negative stock flag (POSOfflineSyncVariance).
		flagID := "RPT2610-NEGFLAG-1"
		_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", flagID)
		flagData, _ := json.Marshal(map[string]interface{}{
			"cart_number": "CART-RPT2610", "sku": "SKU-RPT2610-NEG", "location": "WH-RPT2610",
			"shortfall_qty": 3, "resulting_available": -3, "status": "Open",
		})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'POSOfflineSyncVariance', $2, 'Open', 'system')", flagID, flagData); err != nil {
			t.Fatalf("failed to seed negative-stock-flag fixture: %v", err)
		}
		negFlags, err := GetNegativeStockFlags(tenantID)
		if err != nil {
			t.Fatalf("GetNegativeStockFlags failed: %v", err)
		}
		found = false
		for _, f := range negFlags {
			if f.ID == flagID {
				found = true
				if f.Sku != "SKU-RPT2610-NEG" || f.ShortfallQty != 3 {
					t.Errorf("unexpected flag contents: %+v", f)
				}
			}
		}
		if !found {
			t.Errorf("expected flag %s to appear in negative stock flags", flagID)
		}
	})

	t.Run("ScheduledReportWorker", func(t *testing.T) {
		scheduleID := "SCHEDRPT-TEST-1"
		_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", scheduleID)
		_, _ = db.DB.Exec("DELETE FROM "+schema+".integration_event_outbox WHERE event_name = 'report.scheduled_delivery'")

		yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
		scheduleData, _ := json.Marshal(map[string]interface{}{
			"report_id": "current-stock", "frequency": "Daily", "requested_role": "HR/Admin",
			"recipient_email": "ops@example.com", "next_run_date": yesterday, "status": "Active",
		})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'ScheduledReport', $2, 'Active', 'system')", scheduleID, scheduleData); err != nil {
			t.Fatalf("failed to seed ScheduledReport fixture: %v", err)
		}

		processScheduledReports(schema)

		var dataStr string
		if err := db.DB.QueryRow("SELECT data FROM "+schema+".documents WHERE id = $1", scheduleID).Scan(&dataStr); err != nil {
			t.Fatalf("failed to re-read ScheduledReport: %v", err)
		}
		var data map[string]interface{}
		if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
			t.Fatalf("failed to unmarshal ScheduledReport data: %v", err)
		}
		if data["last_run_status"] != "Delivered" {
			t.Errorf("expected last_run_status=Delivered, got %v", data["last_run_status"])
		}
		nextRun, _ := data["next_run_date"].(string)
		today := time.Now().Format("2006-01-02")
		if nextRun <= today {
			t.Errorf("expected next_run_date to advance past today (%s), got %s", today, nextRun)
		}

		var eventCount int
		if err := db.DB.QueryRow("SELECT COUNT(*) FROM "+schema+".integration_event_outbox WHERE event_name = 'report.scheduled_delivery'").Scan(&eventCount); err != nil {
			t.Fatalf("failed to count outbox events: %v", err)
		}
		if eventCount != 1 {
			t.Errorf("expected exactly 1 report.scheduled_delivery outbox event, got %d", eventCount)
		}

		// A second tick before the (now-advanced) next_run_date must be a no-op.
		processScheduledReports(schema)
		if err := db.DB.QueryRow("SELECT COUNT(*) FROM "+schema+".integration_event_outbox WHERE event_name = 'report.scheduled_delivery'").Scan(&eventCount); err != nil {
			t.Fatalf("failed to re-count outbox events: %v", err)
		}
		if eventCount != 1 {
			t.Errorf("expected still exactly 1 outbox event after a second tick before next_run_date, got %d", eventCount)
		}
	})

	// 26.10.7: report query-load instrumentation - the measurement
	// mechanism 26.10.6 (BI data mart) is gated on.
	t.Run("ReportPerformanceInstrumentation", func(t *testing.T) {
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'ReportRunLog' AND data->>'report_id' = 'exception-negative-stock'")

		if _, _, _, err := RunReport(tenantID, "exception-negative-stock", "HR/Admin", "manager1", nil); err != nil {
			t.Fatalf("RunReport failed: %v", err)
		}
		if _, _, _, err := RunReport(tenantID, "exception-negative-stock", "HR/Admin", "manager1", nil); err != nil {
			t.Fatalf("second RunReport failed: %v", err)
		}

		var directCount int
		if err := db.DB.QueryRow("SELECT COUNT(*) FROM " + schema + ".documents WHERE doctype = 'ReportRunLog' AND data->>'report_id' = 'exception-negative-stock'").Scan(&directCount); err != nil {
			t.Fatalf("direct count query failed: %v", err)
		}
		if directCount != 2 {
			t.Fatalf("expected 2 ReportRunLog rows written directly, got %d", directCount)
		}

		perf, err := GetReportPerformance(tenantID, "", "")
		if err != nil {
			t.Fatalf("GetReportPerformance failed: %v", err)
		}
		var found *ReportPerformance
		for i := range perf {
			if perf[i].ReportID == "exception-negative-stock" {
				found = &perf[i]
			}
		}
		if found == nil {
			t.Fatalf("expected a report-performance row for exception-negative-stock, got none in %+v", perf)
		}
		if found.RunCount != 2 {
			t.Errorf("expected run_count=2, got %d", found.RunCount)
		}

		// The report-performance report itself must not log its own runs -
		// otherwise every call would inflate its own next call's count.
		if _, _, _, err := RunReport(tenantID, "report-performance", "HR/Admin", "manager1", nil); err != nil {
			t.Fatalf("RunReport(report-performance) failed: %v", err)
		}
		var selfLogCount int
		if err := db.DB.QueryRow("SELECT COUNT(*) FROM " + schema + ".documents WHERE doctype = 'ReportRunLog' AND data->>'report_id' = 'report-performance'").Scan(&selfLogCount); err != nil {
			t.Fatalf("failed to count self-log rows: %v", err)
		}
		if selfLogCount != 0 {
			t.Errorf("expected report-performance to never log its own runs, found %d", selfLogCount)
		}
	})
}
