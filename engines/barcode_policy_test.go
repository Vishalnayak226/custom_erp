package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"testing"
	"time"
)

// Stage 57.7: receipt-date barcodes are valid EAN-13s in the GS1 internal
// range, carry the day, and cannot collide with stable "020..." codes.
func TestDatedEANFromParts(t *testing.T) {
	day := time.Date(2026, 10, 6, 15, 0, 0, 0, time.Local)
	code, err := datedEANFromParts(day, 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(code) != 13 || code[:8] != "02261006" || code[8:12] != "0007" {
		t.Fatalf("got %q, want 02 + 261006 + 0007 + check digit", code)
	}
	check, err := gs1CheckDigit(code[:12])
	if err != nil || int(code[12]-'0') != check {
		t.Fatalf("bad check digit on %q", code)
	}
	if code[:3] == "020" {
		t.Fatalf("dated code %q collides with the stable 020 range", code)
	}
	if _, err := datedEANFromParts(day, 10000); err == nil {
		t.Fatal("a 10,000th code in one day should be refused, not wrapped")
	}
	if _, err := datedEANFromParts(day, 0); err == nil {
		t.Fatal("sequence 0 should be refused")
	}
}

// Stage 57.7: with per-receipt-date barcodes on, a receipt registers one
// dated code per SKU per day, that code scans as the SKU, the receipt's
// stickers get it, and a second receipt the same day reuses it. With
// generation off, no permanent barcode is issued at receipt.
func TestEnsureReceiptBarcodesPerDate(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schemaName := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schemaName)
	if _, err := ProvisionTenantSchema(tenantID, schemaName, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	if _, err := db.DB.Exec(`INSERT INTO ` + schemaName + `.documents (doctype, id, data, status, created_by) VALUES
		('Item', 'TEE-RED-M', '{"code":"TEE-RED-M","name":"Tee Red M"}', 'Active', 'admin')`); err != nil {
		t.Fatalf("seed item: %v", err)
	}
	if err := SetSetting(tenantID, "inventory.barcode_generate_at", "off", "test"); err != nil {
		t.Fatalf("set generate_at: %v", err)
	}
	if err := SetSetting(tenantID, "inventory.barcode_per_receipt_date", "yes", "test"); err != nil {
		t.Fatalf("set per_receipt_date: %v", err)
	}
	items := []interface{}{map[string]interface{}{"sku": "TEE-RED-M", "qty": 4.0}}

	EnsureReceiptBarcodes(tenantID, "GRN-T1", items)

	var permanent sql.NullString
	if err := db.DB.QueryRow(`SELECT data->>'barcode' FROM ` + schemaName + `.documents WHERE id='TEE-RED-M'`).Scan(&permanent); err != nil {
		t.Fatal(err)
	}
	if permanent.Valid && permanent.String != "" {
		t.Errorf("generation is off, but receipt issued permanent barcode %q", permanent.String)
	}
	dated := ReceiptBarcodeFor(tenantID, "TEE-RED-M", "GRN-T1")
	if len(dated) != 13 || dated[:2] != "02" {
		t.Fatalf("receipt-date barcode = %q, want an 02-range EAN-13", dated)
	}
	resolved, err := ResolveItemBySKU(tenantID, dated)
	if err != nil || resolved.ID != "TEE-RED-M" || resolved.MatchedOn != "registered_barcode" {
		t.Fatalf("scanning %s did not resolve to the SKU: %+v %v", dated, resolved, err)
	}

	EnsureReceiptBarcodes(tenantID, "GRN-T2", items) // same day, second receipt
	var n int
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM ` + schemaName + `.documents WHERE doctype='ItemBarcode' AND data->>'item'='TEE-RED-M'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("want one dated barcode per SKU per day, got %d", n)
	}
	if again := ReceiptBarcodeFor(tenantID, "TEE-RED-M", "GRN-T2"); again != dated {
		t.Errorf("same-day second receipt's stickers got %q, want the day's %q", again, dated)
	}
}

// Stage 61.1: every SKU on a new GRN gets one batch number - B + receipt date
// + the day's running number - shared by that SKU's lines; a typed number is
// kept and covers the SKU's blank lines; a batch-tracked item is left for the
// clerk to type the carton's lot. The lot then scans back to its receipt date
// and age, by batch number or by the SKU.
func TestPrepareGRNBatchNumbersAndAgeing(t *testing.T) {
	if got := grnBatchFromParts(time.Date(2026, 10, 15, 9, 0, 0, 0, time.Local), 7); got != "B261015-0007" {
		t.Fatalf("grnBatchFromParts = %q, want B261015-0007", got)
	}

	db.InitDB(testConnStr())
	tenantID, schemaName := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schemaName)
	if _, err := ProvisionTenantSchema(tenantID, schemaName, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	if _, err := db.DB.Exec(`INSERT INTO ` + schemaName + `.documents (doctype, id, data, status, created_by) VALUES
		('Item', 'TEE-BLU-M', '{"code":"TEE-BLU-M","name":"Tee Blue M"}', 'Active', 'admin'),
		('Item', 'CAP-RED', '{"code":"CAP-RED","name":"Cap Red"}', 'Active', 'admin'),
		('Item', 'SYRUP-1L', '{"code":"SYRUP-1L","name":"Syrup 1L","tracking_mode":"Batch"}', 'Active', 'admin')`); err != nil {
		t.Fatalf("seed items: %v", err)
	}
	// The series row migrations_stage61_1 seeds. Its blank separator runs the
	// day and the counter together ("2610150001"); the first live run read
	// that as one number and issued B261015-2610150001.
	if _, err := db.DB.Exec(`INSERT INTO ` + schemaName + `.prefix_configs (doc_type, prefix, separator, padding_width, reset_frequency, active_status, include_store)
		VALUES ('GRNBatchSeq', '', '', 4, 'ANNUAL', TRUE, FALSE) ON CONFLICT (doc_type) DO NOTHING`); err != nil {
		t.Fatalf("seed GRNBatchSeq: %v", err)
	}

	payload := map[string]interface{}{"received_items": `[
		{"sku":"TEE-BLU-M","qty":2},
		{"sku":"CAP-RED","qty":1},
		{"sku":"TEE-BLU-M","qty":3},
		{"sku":"CAP-RED","qty":4,"batch_no":"VENDOR-LOT-9"},
		{"sku":"SYRUP-1L","qty":5}]`}
	if err := PrepareGRNBatchNumbers(tenantID, true, payload); err != nil {
		t.Fatalf("PrepareGRNBatchNumbers: %v", err)
	}
	var lines []map[string]interface{}
	if err := json.Unmarshal([]byte(payload["received_items"].(string)), &lines); err != nil {
		t.Fatal(err)
	}
	tee := lines[0]["batch_no"]
	want := "B" + time.Now().Format("060102") + "-"
	if s, _ := tee.(string); len(s) != 12 || s[:8] != want {
		t.Fatalf("TEE-BLU-M batch = %v, want %sNNNN", tee, want)
	}
	if lines[2]["batch_no"] != tee {
		t.Errorf("second TEE-BLU-M line got %v, want the SKU's %v", lines[2]["batch_no"], tee)
	}
	if lines[1]["batch_no"] != "VENDOR-LOT-9" || lines[3]["batch_no"] != "VENDOR-LOT-9" {
		t.Errorf("CAP-RED lines got %v / %v, want the typed VENDOR-LOT-9 on both", lines[1]["batch_no"], lines[3]["batch_no"])
	}
	if b, ok := lines[4]["batch_no"]; ok {
		t.Errorf("batch-tracked SYRUP-1L was given %v; the clerk must type the carton's lot", b)
	}

	edit := map[string]interface{}{"received_items": `[{"sku":"TEE-BLU-M","qty":2}]`}
	if err := PrepareGRNBatchNumbers(tenantID, false, edit); err != nil || edit["received_items"] != `[{"sku":"TEE-BLU-M","qty":2}]` {
		t.Errorf("an edit must not be renumbered: %v %v", edit["received_items"], err)
	}

	teeBatch := tee.(string)
	if _, err := EnsureBatch(tenantID, BatchInfo{BatchNo: teeBatch, Item: "TEE-BLU-M", GRN: "GRN-61-T1"}, 5, "test"); err != nil {
		t.Fatalf("EnsureBatch: %v", err)
	}
	byBatch, err := GetBatchAgeing(tenantID, teeBatch, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(byBatch) != 1 || byBatch[0].Sku != "TEE-BLU-M" ||
		byBatch[0].GRN != "GRN-61-T1" || byBatch[0].ReceivedOn != time.Now().Format("2006-01-02") ||
		byBatch[0].AgeDays != 0 || byBatch[0].ReceivedQty != 5 {
		t.Fatalf("scan of %s = %+v", teeBatch, byBatch)
	}
	bySKU, err := GetBatchAgeing(tenantID, "TEE-BLU-M", 0)
	if err != nil || len(bySKU) != 1 || bySKU[0].BatchNo != teeBatch {
		t.Fatalf("scan of the SKU = %+v %v, want its one lot", bySKU, err)
	}
	if old, err := GetBatchAgeing(tenantID, teeBatch, 30); err != nil || len(old) != 0 {
		t.Errorf("a lot received today is not 30 days old: %+v %v", old, err)
	}
	if none, err := GetBatchAgeing(tenantID, "NO-SUCH-CODE", 0); err != nil || len(none) != 0 {
		t.Errorf("unknown scan = %+v %v, want no rows and no error", none, err)
	}
}

// Stage 61.1: two lines of one SKU (and lot) on one voucher are one ledger
// movement of their summed qty. The idempotency key is voucher + SKU + lot,
// so before this the second line was dropped as a replay - the ledger showed
// 2 of a GRN's 2+4 while availability took all 6.
func TestStockLedgerSumsRepeatedSKULines(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schemaName := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schemaName)
	if _, err := ProvisionTenantSchema(tenantID, schemaName, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	if _, err := PostInventoryLedgerWithVoucher(tenantID, "LOC-61", []interface{}{
		map[string]interface{}{"sku": "TEE-61", "qty": 2, "batch_no": "B261015-0001"},
		map[string]interface{}{"sku": "CAP-61", "qty": 3},
		map[string]interface{}{"sku": "TEE-61", "qty": 4, "batch_no": "B261015-0001"},
		map[string]interface{}{"sku": "CAP-61", "qty": 1},
		map[string]interface{}{"sku": "TEE-61", "qty": 5, "batch_no": "B261015-0002"},
	}, false, "GRN", "GRN-61-LEDGER", "test"); err != nil {
		t.Fatalf("post: %v", err)
	}
	want := map[string]float64{"TEE-61|B261015-0001": 6, "CAP-61|": 4, "TEE-61|B261015-0002": 5}
	rows, err := db.DB.Query(`SELECT data->>'item_id', COALESCE(data->>'batch_no', ''), (data->>'qty')::numeric
		FROM ` + schemaName + `.documents WHERE doctype = 'StockLedgerEntry' AND data->>'voucher_id' = 'GRN-61-LEDGER'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := map[string]float64{}
	for rows.Next() {
		var sku, batch string
		var qty float64
		if err := rows.Scan(&sku, &batch, &qty); err != nil {
			t.Fatal(err)
		}
		got[sku+"|"+batch] += qty
	}
	if len(got) != len(want) {
		t.Fatalf("ledger = %v, want %v", got, want)
	}
	for k, q := range want {
		if got[k] != q {
			t.Errorf("ledger %s = %v, want %v (all ledger rows: %v)", k, got[k], q, got)
		}
	}
}
