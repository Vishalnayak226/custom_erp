package engines

import (
	"custom_erp/db"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"
)

const undercutThresholdKey = "market.undercut_threshold_pct"

// snapshotThresholdSetting reports the stored system_settings row, if any.
// Deliberately not GetSettingString: that resolves through to the registry
// default ("0") when no row exists, so it cannot distinguish "explicitly set
// to 0" from "never set", and restoring its result would leave a row behind
// in the shared dev database where there was none before.
func snapshotThresholdSetting(schema string) (string, bool) {
	var v sql.NullString
	_ = db.DB.QueryRow("SELECT value FROM " + schema + ".system_settings WHERE key = '" + undercutThresholdKey + "'").Scan(&v)
	return v.String, v.Valid
}

// restoreThresholdSetting puts the setting back exactly as it was found -
// including putting back "no row at all", which SetSetting cannot express.
func restoreThresholdSetting(tenantID, schema, prev string, existed bool) {
	if existed {
		_ = SetSetting(tenantID, undercutThresholdKey, prev, "system")
		return
	}
	_, _ = db.DB.Exec("DELETE FROM " + schema + ".system_settings WHERE key = '" + undercutThresholdKey + "'")
	settingsCacheMu.Lock()
	delete(settingsCache, settingsCacheKey(schema, undercutThresholdKey))
	settingsCacheMu.Unlock()
}

// Stage 34.1-34.3 engine tests. Own file, same convention
// reports_stage26_10_test.go / wms_enterprise_test.go follow for staying out
// of engines_test.go while other work may be mid-editing it.
//
// Every fixture is prefixed MI34 and deleted at the end, because these tests
// run against the shared dev database (see the note in ai_handover.md) - a
// leftover CompetitorPrice row would show up in a real report run.
func TestMarketIntelligenceStage34(t *testing.T) {
	connStr := testConnStr()
	db.InitDB(connStr)

	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("Failed to get tenant schema: %v", err)
	}

	sku := "SKU-MI34-WIDGET"
	cleanup := func() {
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id LIKE 'MI34-%' OR (doctype = 'Item' AND id = '" + sku + "')")
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'CompetitorPrice' AND data->>'our_item' = '" + sku + "'")
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'SalesOrderLine' AND data->>'sku' = '" + sku + "'")
	}
	cleanup()
	defer cleanup()

	mustExec := func(t *testing.T, q string, args ...interface{}) {
		t.Helper()
		if _, err := db.DB.Exec(q, args...); err != nil {
			t.Fatalf("setup exec failed: %v", err)
		}
	}

	// The item, and our own last transacted price: 100 via a Sales Order line.
	mustExec(t, fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by)
		VALUES ($1, 'Item', $2::jsonb, 'Active', 'system')`, schema),
		sku, `{"code":"`+sku+`","name":"MI34 Widget","barcode":"MI34BC","hsn_code":"1234","gst_rate":"18"}`)
	mustExec(t, fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by)
		VALUES ('MI34-SOL-1', 'SalesOrderLine', $1::jsonb, 'Active', 'system')`, schema),
		`{"code":"MI34-SOL-1","sku":"`+sku+`","qty":"1","unit_price":"100"}`)

	t.Run("DoctypeIsRegisteredAndValidates", func(t *testing.T) {
		fields, err := GetDocTypeMeta(tenantID, "CompetitorPrice")
		if err != nil {
			t.Fatalf("GetDocTypeMeta failed: %v", err)
		}
		if len(fields) == 0 {
			t.Fatal("CompetitorPrice has no doctype_fields - migration not applied?")
		}

		// Mandatory fields are enforced by the shared choke point, not by any
		// bespoke code in this Stage.
		err = ValidateDocument(tenantID, "CompetitorPrice", map[string]interface{}{
			"code": "MI34-CP-BAD", "platform": "Amazon",
		})
		if err == nil {
			t.Error("expected ValidateDocument to reject a row missing competitor_price/observed_at")
		}

		// A platform outside the Select list is rejected.
		err = ValidateDocument(tenantID, "CompetitorPrice", map[string]interface{}{
			"code": "MI34-CP-BAD2", "platform": "NotARealMarketplace",
			"competitor_price": "10", "observed_at": "2026-08-07", "status": "Active",
		})
		if err == nil {
			t.Error("expected ValidateDocument to reject an out-of-list platform")
		}

		// A link to a non-existent Item is rejected.
		err = ValidateDocument(tenantID, "CompetitorPrice", map[string]interface{}{
			"code": "MI34-CP-BAD3", "platform": "Amazon", "our_item": "SKU-MI34-DOES-NOT-EXIST",
			"competitor_price": "10", "observed_at": "2026-08-07", "status": "Active",
		})
		if err == nil {
			t.Error("expected ValidateDocument to reject a dangling our_item Link")
		}

		// The real thing passes.
		if err := ValidateDocument(tenantID, "CompetitorPrice", map[string]interface{}{
			"code": "MI34-CP-OK", "platform": "Amazon", "our_item": sku,
			"competitor_price": "80", "observed_at": "2026-08-07", "status": "Active",
		}); err != nil {
			t.Errorf("expected a well-formed CompetitorPrice to validate, got %v", err)
		}
	})

	t.Run("CSVIngestionViaBulkImport", func(t *testing.T) {
		csv := "code,our_item,platform,competitor_price,mrp,observed_at,status\n" +
			"MI34-CP-1," + sku + ",Amazon,80,120,2026-08-01,Active\n" +
			"MI34-CP-2," + sku + ",Flipkart,90,120,2026-08-02,Active\n"
		res, err := BulkImportCSV(tenantID, "CompetitorPrice", strings.NewReader(csv), "system", false)
		if err != nil {
			t.Fatalf("BulkImportCSV failed: %v", err)
		}
		if res.SuccessRows != 2 || res.FailedRows != 0 {
			t.Fatalf("expected 2 imported rows and 0 failures, got %d/%d (%v)", res.SuccessRows, res.FailedRows, res.Errors)
		}
	})

	t.Run("PriceGapReportPicksCheapestCompetitor", func(t *testing.T) {
		rows, err := GetCompetitorPriceGapReport(tenantID, "", "", sku, "")
		if err != nil {
			t.Fatalf("GetCompetitorPriceGapReport failed: %v", err)
		}
		if len(rows) != 1 {
			t.Fatalf("expected exactly 1 gap row for %s, got %d", sku, len(rows))
		}
		r := rows[0]
		// 80 (Amazon) is cheaper than 90 (Flipkart), so it is the one to act on.
		if r["best_competitor_price"] != 80.0 {
			t.Errorf("expected best_competitor_price=80, got %v", r["best_competitor_price"])
		}
		if r["best_platform"] != "Amazon" {
			t.Errorf("expected best_platform=Amazon, got %v", r["best_platform"])
		}
		if r["our_price"] != 100.0 {
			t.Errorf("expected our_price=100 (from the Sales Order line), got %v", r["our_price"])
		}
		if r["our_price_source"] != "Sales Order" {
			t.Errorf("expected our_price_source='Sales Order', got %v", r["our_price_source"])
		}
		if r["gap_amount"] != 20.0 {
			t.Errorf("expected gap_amount=20, got %v", r["gap_amount"])
		}
		if r["gap_pct"] != 25.0 {
			t.Errorf("expected gap_pct=25 (20/80), got %v", r["gap_pct"])
		}
		if r["position"] != "Above" {
			t.Errorf("expected position=Above, got %v", r["position"])
		}
		if r["observations"] != 2 {
			t.Errorf("expected observations=2, got %v", r["observations"])
		}
	})

	t.Run("PriceGapReportFilters", func(t *testing.T) {
		// Narrowing to Flipkart must change which competitor row wins.
		rows, err := GetCompetitorPriceGapReport(tenantID, "Flipkart", "", sku, "")
		if err != nil {
			t.Fatalf("filtered report failed: %v", err)
		}
		if len(rows) != 1 || rows[0]["best_competitor_price"] != 90.0 {
			t.Fatalf("expected the Flipkart row at 90, got %v", rows)
		}

		// A 'since' later than every observation must exclude the SKU entirely.
		rows, err = GetCompetitorPriceGapReport(tenantID, "", "2026-09-01", sku, "")
		if err != nil {
			t.Fatalf("since-filtered report failed: %v", err)
		}
		if len(rows) != 0 {
			t.Errorf("expected no rows for observations after 2026-09-01, got %d", len(rows))
		}

		// Position filter: we are Above, so filtering for Below excludes us.
		rows, err = GetCompetitorPriceGapReport(tenantID, "", "", sku, "Below")
		if err != nil {
			t.Fatalf("position-filtered report failed: %v", err)
		}
		if len(rows) != 0 {
			t.Errorf("expected no Below rows, got %d", len(rows))
		}
	})

	t.Run("SkuWithNoTransactedPriceIsReportedNotFabricated", func(t *testing.T) {
		orphan := "SKU-MI34-NEVERSOLD"
		defer func() {
			_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1 OR id = 'MI34-CP-ORPHAN'", orphan)
		}()
		mustExec(t, fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by)
			VALUES ($1, 'Item', $2::jsonb, 'Active', 'system')`, schema),
			orphan, `{"code":"`+orphan+`","name":"MI34 Never Sold","barcode":"MI34NS","hsn_code":"1234","gst_rate":"18"}`)
		mustExec(t, fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by)
			VALUES ('MI34-CP-ORPHAN', 'CompetitorPrice', $1::jsonb, 'Active', 'system')`, schema),
			`{"code":"MI34-CP-ORPHAN","our_item":"`+orphan+`","platform":"Amazon","competitor_price":"50","observed_at":"2026-08-01","status":"Active"}`)

		rows, err := GetCompetitorPriceGapReport(tenantID, "", "", orphan, "")
		if err != nil {
			t.Fatalf("orphan report failed: %v", err)
		}
		if len(rows) != 1 {
			t.Fatalf("expected the never-sold SKU to still be reported, got %d rows", len(rows))
		}
		if rows[0]["position"] != "No price on file" {
			t.Errorf("expected position='No price on file', got %v", rows[0]["position"])
		}
		if rows[0]["our_price"] != nil {
			t.Errorf("expected our_price to be null, not a fabricated 0, got %v", rows[0]["our_price"])
		}
		if rows[0]["gap_amount"] != nil {
			t.Errorf("expected gap_amount to be null, got %v", rows[0]["gap_amount"])
		}
	})

	t.Run("UndercutWorkerAlertsOnlyOverThreshold", func(t *testing.T) {
		// Our price is 100, cheapest competitor is 80 => 20% undercut.
		prev, existed := snapshotThresholdSetting(schema)
		defer restoreThresholdSetting(tenantID, schema, prev, existed)

		countLogs := func() int {
			var n int
			_ = db.DB.QueryRow("SELECT COUNT(*) FROM " + schema + ".documents WHERE doctype = 'NotificationLog' AND data->>'event' = 'Competitor Undercut' AND data->>'order_id' = '" + sku + "'").Scan(&n)
			return n
		}
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'NotificationLog' AND data->>'event' = 'Competitor Undercut'")

		// Threshold above the actual gap: nothing fires.
		if err := SetSetting(tenantID, "market.undercut_threshold_pct", "50", "system"); err != nil {
			t.Fatalf("SetSetting failed: %v", err)
		}
		if err := alertUndercutsForTenant(tenantID, schema, time.Now().Add(-24*time.Hour)); err != nil {
			t.Fatalf("alertUndercutsForTenant failed: %v", err)
		}
		if n := countLogs(); n != 0 {
			t.Errorf("expected no alert at a 50%% threshold for a 20%% undercut, got %d", n)
		}

		// Threshold below the actual gap: it fires. DispatchNotification writes
		// a NotificationLog row even with no template configured (Skipped-
		// NoTemplate), which is what makes this observable without inventing a
		// webhook.
		if err := SetSetting(tenantID, "market.undercut_threshold_pct", "10", "system"); err != nil {
			t.Fatalf("SetSetting failed: %v", err)
		}
		if err := alertUndercutsForTenant(tenantID, schema, time.Now().Add(-24*time.Hour)); err != nil {
			t.Fatalf("alertUndercutsForTenant failed: %v", err)
		}
		if n := countLogs(); n == 0 {
			t.Error("expected an alert at a 10% threshold for a 20% undercut, got none")
		}

		// De-duplication: a `since` in the future means no observation is new,
		// so a second cycle raises nothing further.
		before := countLogs()
		if err := alertUndercutsForTenant(tenantID, schema, time.Now().Add(1*time.Hour)); err != nil {
			t.Fatalf("alertUndercutsForTenant (future since) failed: %v", err)
		}
		if after := countLogs(); after != before {
			t.Errorf("expected no new alerts for already-seen observations, went %d -> %d", before, after)
		}

		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'NotificationLog' AND data->>'event' = 'Competitor Undercut'")
	})

	t.Run("ThresholdZeroDisablesAlerting", func(t *testing.T) {
		prev, existed := snapshotThresholdSetting(schema)
		defer restoreThresholdSetting(tenantID, schema, prev, existed)
		if err := SetSetting(tenantID, "market.undercut_threshold_pct", "0", "system"); err != nil {
			t.Fatalf("SetSetting failed: %v", err)
		}
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'NotificationLog' AND data->>'event' = 'Competitor Undercut'")
		if err := alertUndercutsForTenant(tenantID, schema, time.Now().Add(-24*time.Hour)); err != nil {
			t.Fatalf("alertUndercutsForTenant failed: %v", err)
		}
		var n int
		_ = db.DB.QueryRow("SELECT COUNT(*) FROM " + schema + ".documents WHERE doctype = 'NotificationLog' AND data->>'event' = 'Competitor Undercut'").Scan(&n)
		if n != 0 {
			t.Errorf("expected the 0 default to disable alerting entirely, got %d alerts", n)
		}
	})
}
