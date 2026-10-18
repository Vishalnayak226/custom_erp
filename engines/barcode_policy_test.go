package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"regexp"
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

// Stage 61: blank GRN lines get batch numbers by the Item / Item Type /
// client rule - one per SKU on the GRN (default), one per line, one per SKU per
// day (shared by that day's GRNs at the same location) or off; a typed number
// is kept and covers the SKU's blank lines; a batch-tracked item is left for
// the clerk to type the carton's lot. The number is the GRNBatchSeq series
// (B-YYMMDD-NNNN as seeded).
func TestPrepareGRNBatchNumbersModes(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schemaName := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schemaName)
	if _, err := ProvisionTenantSchema(tenantID, schemaName, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	if _, err := db.DB.Exec(`INSERT INTO ` + schemaName + `.documents (doctype, id, data, status, created_by) VALUES
		('Item', 'TEE-BLU-M', '{"code":"TEE-BLU-M","name":"Tee Blue M"}', 'Active', 'admin'),
		('Item', 'CAP-RED', '{"code":"CAP-RED","name":"Cap Red"}', 'Active', 'admin'),
		('Item', 'SYRUP-1L', '{"code":"SYRUP-1L","name":"Syrup 1L","tracking_mode":"Batch"}', 'Active', 'admin'),
		('Item', 'LINE-SKU', '{"code":"LINE-SKU","name":"Per line","grn_batch_number":"One per GRN line"}', 'Active', 'admin'),
		('Item', 'DAY-SKU', '{"code":"DAY-SKU","name":"Per day","item_type":"DayType"}', 'Active', 'admin'),
		('ItemType', 'DayType', '{"code":"DayType","grn_batch_number":"One per SKU per day","status":"Active"}', 'Active', 'admin')
		ON CONFLICT DO NOTHING`); err != nil {
		t.Fatalf("seed items: %v", err)
	}
	// The series as migrations stage61_1 + stage61_6 leave it.
	if _, err := db.DB.Exec(`INSERT INTO ` + schemaName + `.prefix_configs (doc_type, prefix, separator, padding_width, reset_frequency, active_status, include_store)
		VALUES ('GRNBatchSeq', 'B', '-', 4, 'ANNUAL', TRUE, FALSE)
		ON CONFLICT (doc_type) DO UPDATE SET prefix = 'B', separator = '-', padding_width = 4, reset_frequency = 'ANNUAL', include_store = FALSE`); err != nil {
		t.Fatalf("seed GRNBatchSeq: %v", err)
	}
	prep := func(raw string) []map[string]interface{} {
		t.Helper()
		payload := map[string]interface{}{"location": "LOC-61", "received_items": raw}
		if err := PrepareGRNBatchNumbers(tenantID, true, payload); err != nil {
			t.Fatalf("PrepareGRNBatchNumbers: %v", err)
		}
		var lines []map[string]interface{}
		if err := json.Unmarshal([]byte(payload["received_items"].(string)), &lines); err != nil {
			t.Fatal(err)
		}
		return lines
	}
	shape := regexp.MustCompile(`^B-` + time.Now().Format("060102") + `-\d{4}$`)

	lines := prep(`[
		{"sku":"TEE-BLU-M","qty":2},
		{"sku":"CAP-RED","qty":1},
		{"sku":"TEE-BLU-M","qty":3},
		{"sku":"CAP-RED","qty":4,"batch_no":"VENDOR-LOT-9"},
		{"sku":"SYRUP-1L","qty":5},
		{"sku":"LINE-SKU","qty":1},
		{"sku":"LINE-SKU","qty":1},
		{"sku":"DAY-SKU","qty":3}]`)
	tee, _ := lines[0]["batch_no"].(string)
	if !shape.MatchString(tee) || lines[2]["batch_no"] != tee || lines[0]["batch_auto"] != true {
		t.Fatalf("TEE-BLU-M lines = %v / %v, want one shared auto B-YYMMDD-NNNN", lines[0], lines[2])
	}
	if lines[1]["batch_no"] != "VENDOR-LOT-9" || lines[3]["batch_no"] != "VENDOR-LOT-9" || lines[1]["batch_auto"] != nil {
		t.Errorf("CAP-RED lines = %v / %v, want the typed VENDOR-LOT-9 on both, not marked auto", lines[1], lines[3])
	}
	if b, ok := lines[4]["batch_no"]; ok {
		t.Errorf("batch-tracked SYRUP-1L was given %v; the clerk must type the carton's lot", b)
	}
	l1, _ := lines[5]["batch_no"].(string)
	l2, _ := lines[6]["batch_no"].(string)
	if !shape.MatchString(l1) || !shape.MatchString(l2) || l1 == l2 {
		t.Errorf("per-line item got %q / %q, want two different numbers", l1, l2)
	}
	day, _ := lines[7]["batch_no"].(string)
	if !shape.MatchString(day) {
		t.Fatalf("per-day item got %q", day)
	}

	// The day's batch is registered by the first GRN; a second GRN that day
	// at the same location shares it, and its quantity is added once.
	if _, err := EnsureBatch(tenantID, BatchInfo{BatchNo: day, Item: "DAY-SKU", GRN: "GRN-A", Location: "LOC-61", AutoNumbered: true}, 3, "test"); err != nil {
		t.Fatalf("EnsureBatch: %v", err)
	}
	again := prep(`[{"sku":"DAY-SKU","qty":2},{"sku":"TEE-BLU-M","qty":1}]`)
	if again[0]["batch_no"] != day {
		t.Errorf("second GRN today got %v, want the day's %s", again[0]["batch_no"], day)
	}
	if again[1]["batch_no"] == tee {
		t.Errorf("per-GRN item reused the first GRN's %s", tee)
	}
	for i := 0; i < 2; i++ { // the second call is a replay and must not add again
		if _, err := EnsureBatch(tenantID, BatchInfo{BatchNo: day, Item: "DAY-SKU", GRN: "GRN-B", Location: "LOC-61", AutoNumbered: true}, 2, "test"); err != nil {
			t.Fatalf("EnsureBatch (shared): %v", err)
		}
	}
	rows, err := GetBatchAgeing(tenantID, day, 0, false)
	if err != nil || len(rows) != 1 || rows[0].ReceivedQty != 5 || rows[0].GRN != "GRN-A, GRN-B" {
		t.Fatalf("day batch = %+v %v, want received 5 across GRN-A, GRN-B", rows, err)
	}

	// Off for the client: nothing is numbered (the per-line Item still is,
	// since an Item's own rule wins).
	if err := SetSetting(tenantID, "inventory.grn_batch_number", GRNBatchOff, "test"); err != nil {
		t.Fatal(err)
	}
	off := prep(`[{"sku":"TEE-BLU-M","qty":1},{"sku":"LINE-SKU","qty":1}]`)
	if b, ok := off[0]["batch_no"]; ok {
		t.Errorf("client off, but TEE-BLU-M got %v", b)
	}
	if b, _ := off[1]["batch_no"].(string); !shape.MatchString(b) {
		t.Errorf("Item rule 'One per GRN line' should win over client off, got %q", b)
	}

	edit := map[string]interface{}{"received_items": `[{"sku":"TEE-BLU-M","qty":2}]`}
	if err := PrepareGRNBatchNumbers(tenantID, false, edit); err != nil || edit["received_items"] != `[{"sku":"TEE-BLU-M","qty":2}]` {
		t.Errorf("an edit must not be renumbered: %v %v", edit["received_items"], err)
	}
}

// Stage 61.9: what is left of each batch is the SKU's stock on hand laid
// against its batches newest first; bands follow each batch's age; stock
// beyond everything received is "before batch numbers".
func TestBatchAgeingFIFOAndBands(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schemaName := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schemaName)
	if _, err := ProvisionTenantSchema(tenantID, schemaName, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	dayAgo := func(n int) string { return time.Now().AddDate(0, 0, -n).Format("2006-01-02") }
	if _, err := db.DB.Exec(`INSERT INTO `+schemaName+`.documents (doctype, id, data, status, created_by) VALUES
		('Item', 'AGE-SKU', '{"code":"AGE-SKU","name":"Aged","barcode":"0200000006101"}', 'Active', 'admin'),
		('Item', 'OLD-SKU', '{"code":"OLD-SKU","name":"Older stock"}', 'Active', 'admin'),
		('Batch', 'BATCH-61-1', jsonb_build_object('batch_no','B-OLD','item','AGE-SKU','received_qty',10,'received_on',$1::text,'grn','GRN-1'), 'Active', 'test'),
		('Batch', 'BATCH-61-2', jsonb_build_object('batch_no','B-MID','item','AGE-SKU','received_qty',5,'received_on',$2::text,'grn','GRN-2'), 'Active', 'test'),
		('Batch', 'BATCH-61-3', jsonb_build_object('batch_no','B-NEW','item','AGE-SKU','received_qty',4,'received_on',$3::text,'grn','GRN-3'), 'Active', 'test'),
		('Batch', 'BATCH-61-4', jsonb_build_object('batch_no','B-ONE','item','OLD-SKU','received_qty',10,'received_on',$3::text,'grn','GRN-4'), 'Active', 'test')`,
		dayAgo(100), dayAgo(45), dayAgo(5)); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := db.DB.Exec(`INSERT INTO ` + schemaName + `.inventory_availability (sku, location_code, on_hand, available) VALUES
		('AGE-SKU', 'LOC-A', 8, 8), ('AGE-SKU', 'LOC-B', 4, 4), ('OLD-SKU', 'LOC-A', 15, 15)`); err != nil {
		t.Fatalf("seed stock: %v", err)
	}

	// 12 on hand: B-NEW 4, B-MID 5, B-OLD the remaining 3. Scanned by barcode.
	rows, err := GetBatchAgeing(tenantID, "0200000006101", 0, false)
	if err != nil || len(rows) != 3 {
		t.Fatalf("scan by barcode = %+v %v", rows, err)
	}
	want := []struct {
		batch string
		left  float64
		band  string
	}{{"B-OLD", 3, "91-180"}, {"B-MID", 5, "31-60"}, {"B-NEW", 4, "0-30"}}
	for i, w := range want {
		if rows[i].BatchNo != w.batch || rows[i].EstQtyLeft != w.left || rows[i].AgeBand != w.band {
			t.Errorf("row %d = %s left %v band %s, want %s left %v band %s",
				i, rows[i].BatchNo, rows[i].EstQtyLeft, rows[i].AgeBand, w.batch, w.left, w.band)
		}
	}
	if old, _ := GetBatchAgeing(tenantID, "AGE-SKU", 40, false); len(old) != 2 {
		t.Errorf("older than 40 days = %d rows, want B-OLD and B-MID", len(old))
	}

	// Sell 6: 6 left -> B-NEW 4, B-MID 2, B-OLD nothing.
	if _, err := db.DB.Exec(`UPDATE ` + schemaName + `.inventory_availability SET on_hand = 2 WHERE sku = 'AGE-SKU' AND location_code = 'LOC-A'`); err != nil {
		t.Fatal(err)
	}
	inStock, err := GetBatchAgeing(tenantID, "AGE-SKU", 0, true)
	if err != nil || len(inStock) != 2 || inStock[0].BatchNo != "B-MID" || inStock[0].EstQtyLeft != 2 {
		t.Fatalf("only with stock left = %+v %v, want B-MID 2 then B-NEW 4", inStock, err)
	}

	bands, err := GetStockAgeing(tenantID, "")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]StockAgeingRow{}
	for _, b := range bands {
		got[b.Sku] = b
	}
	age := got["AGE-SKU"]
	if age.OnHand != 6 || age.Band0to30 != 4 || age.Band31to60 != 2 || age.Band91to180 != 0 || age.BeforeBatch != 0 || age.OldestBatch != "B-MID" {
		t.Errorf("AGE-SKU bands = %+v", age)
	}
	old := got["OLD-SKU"]
	if old.OnHand != 15 || old.Band0to30 != 10 || old.BeforeBatch != 5 {
		t.Errorf("OLD-SKU bands = %+v, want 10 in 0-30 and 5 before batch numbers", old)
	}
	if len(bands) < 2 || bands[0].Sku != "OLD-SKU" {
		t.Errorf("stock older than any batch should lead: %+v", bands)
	}
}

// Stage 61.7: a counter series (GRN batch numbers, barcodes) may share a
// prefix with a document series - its values are never record ids - while two
// document series still may not.
func TestCounterSeriesPrefixMayRepeat(t *testing.T) {
	all := []NumberSeries{{DocType: "PurchaseOrder", Prefix: "PO"}}
	batch := NumberSeries{DocType: "GRNBatchSeq", Prefix: "PO", Separator: "-", PaddingWidth: 4, ResetFrequency: "ANNUAL"}
	if err := validateNumberSeries(batch, &NumberSeries{DocType: "GRNBatchSeq", Prefix: "B"}, nil, all, nil); err != nil {
		t.Errorf("GRN batch series with prefix PO refused: %v", err)
	}
	grn := NumberSeries{DocType: "GRN", Prefix: "PO", Separator: "/", PaddingWidth: 5, ResetFrequency: "ANNUAL"}
	if err := validateNumberSeries(grn, &NumberSeries{DocType: "GRN", Prefix: "GRN"}, nil, all, nil); err == nil {
		t.Error("a document series reusing PO's prefix must still be refused")
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
