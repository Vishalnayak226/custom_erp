package engines

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"custom_erp/db"
)

// Stage 157.4: the on-screen run of a big report pages instead of hitting the
// export cap; the cap still refuses an unpaged (export / scheduled) run.
func TestStage1574LedgerPagesOnScreenAndCapStillRefusesExport(t *testing.T) {
	tenantID, schema := cancellationTestTenant(t)
	if err := SetSetting(tenantID, "platform.max_sync_report_rows", "100", "test"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by)
		SELECT 'SLE-1574-'||g, 'StockLedgerEntry',
		       jsonb_build_object('item_id','SKU-1574','warehouse_id','WH-1574','qty',1,'voucher_type','GRN','voucher_id','G'||g),
		       'Active','test' FROM generate_series(1,150) g`, schema)); err != nil {
		t.Fatalf("seed: %v", err)
	}
	params := map[string]string{"sku": "SKU-1574"}

	// Unpaged (what an export / scheduled run does): still over the cap.
	_, _, _, err := RunReportContext(context.Background(), tenantID, "stock-ledger", RoleSuperAdmin, "u1", params)
	var verr *ValidationError
	if !errors.As(err, &verr) || verr.Code != "REPORT-0161" {
		t.Fatalf("an unpaged run over the cap must be refused with REPORT-0161, got %v", err)
	}

	// Paged (the screen): first page loads, with the true total.
	pg := &ReportPage{Limit: 50, Offset: 0, Desc: true}
	_, rows, _, err := RunReportContext(WithReportPage(context.Background(), pg), tenantID, "stock-ledger", RoleSuperAdmin, "u1", params)
	if err != nil {
		t.Fatalf("paged run: %v", err)
	}
	if len(rows) != 50 || pg.Total != 150 {
		t.Fatalf("want 50 rows of 150, got %d of %d", len(rows), pg.Total)
	}
	// Newest first: the running balance of the first row is the largest (150).
	if b := numFromInterface(rows[0]["running_balance"]); b != 150 {
		t.Fatalf("newest first: want balance 150, got %v", b)
	}
	// Last page is the remainder.
	pg2 := &ReportPage{Limit: 50, Offset: 100, Desc: true}
	_, rows, _, err = RunReportContext(WithReportPage(context.Background(), pg2), tenantID, "stock-ledger", RoleSuperAdmin, "u1", params)
	if err != nil || len(rows) != 50 || pg2.Total != 150 {
		t.Fatalf("last page: rows=%d total=%d err=%v", len(rows), pg2.Total, err)
	}

	// A filter nothing matches: an empty page, not a refusal.
	pg3 := &ReportPage{Limit: 10}
	_, _, _, err = RunReportContext(WithReportPage(context.Background(), pg3), tenantID, "stock-ledger", RoleSuperAdmin, "u1", map[string]string{"sku": "SKU-NONE"})
	if err != nil || pg3.Total != 0 {
		t.Fatalf("empty paged run: total=%d err=%v", pg3.Total, err)
	}
}
