package engines

import (
	"custom_erp/db"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// Stage 42.1 traceability tests. Own file, same db.InitDB / tenantID="default"
// convention as TestWMSEnterprise.
//
// Every subtest uses SKUs and batch numbers prefixed with its own marker and
// cleans up after itself, because this suite shares one database with the rest
// of the package (the shared-DB fixture-debris gotcha recorded against the
// finance suite) and a leftover Batch row would silently change a later FEFO
// ordering assertion.

// traceCleanup removes every artefact one subtest created. Deliberately
// deletes by pattern rather than by id: EnsureBatch mints its own ids, and a
// test that failed half-way through must still not leave a batch behind for
// the next one to trip over.
func traceCleanup(t *testing.T, schema, skuPrefix string) {
	t.Helper()
	_, _ = db.DB.Exec("DELETE FROM " + schema + ".bin_stock_batch WHERE sku LIKE '" + skuPrefix + "%'")
	_, _ = db.DB.Exec("DELETE FROM " + schema + ".bin_stock WHERE sku LIKE '" + skuPrefix + "%'")
	_, _ = db.DB.Exec("DELETE FROM " + schema + ".inventory_availability WHERE sku LIKE '" + skuPrefix + "%'")
	_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'Batch' AND data->>'item' LIKE '" + skuPrefix + "%'")
	_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'Item' AND data->>'code' LIKE '" + skuPrefix + "%'")
	_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'StockLedgerEntry' AND data->>'item_id' LIKE '" + skuPrefix + "%'")
	_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'Bin' AND data->>'bin_code' LIKE 'BIN-" + skuPrefix + "%'")
}

// seedTrackedItem creates an Item with a traceability configuration. The
// document id is deliberately NOT the code (it is "ITEM-"+code, matching how
// every other test in this package seeds an item and how live data looks),
// which is exactly the reason Batch.item is a Data field rather than a Link.
func seedTrackedItem(t *testing.T, schema, sku, mode string, shelfLife, minOnReceipt, minOnPick int) {
	t.Helper()
	data, _ := json.Marshal(map[string]interface{}{
		"code": sku, "name": sku, "barcode": "BC-" + sku,
		"hsn_code": "6109", "tax_treatment": "Taxable", "gst_rate": 5,
		"tracking_mode": mode, "shelf_life_days": shelfLife,
		"min_shelf_life_on_receipt_days": minOnReceipt,
		"min_shelf_life_on_pick_days":    minOnPick,
	})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Item', $2, 'Active', 'system')",
		"ITEM-"+sku, data); err != nil {
		t.Fatalf("seed item %s: %v", sku, err)
	}
}

// seedBinWithStock creates an Active Bin and puts qty of sku into it, going
// through the real PutawayToBin so bin_stock's own invariant (a bin can never
// claim more than the location has unassigned) is satisfied the way production
// satisfies it.
func seedBinWithStock(t *testing.T, schema, binCode, sku, location string, qty int) {
	t.Helper()
	binData, _ := json.Marshal(map[string]interface{}{
		"bin_code": binCode, "location": location, "zone": "Z1", "aisle": "A1", "rack": "R1", "status": "Active",
	})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Bin', $2, 'Active', 'system')",
		"BIN-"+binCode, binData); err != nil {
		t.Fatalf("seed bin %s: %v", binCode, err)
	}
	if _, err := db.DB.Exec(`INSERT INTO `+schema+`.inventory_availability (sku, location_code, on_hand, available)
		VALUES ($1, $2, $3, $3)
		ON CONFLICT (sku, location_code) DO UPDATE SET
			on_hand = `+schema+`.inventory_availability.on_hand + EXCLUDED.on_hand,
			available = `+schema+`.inventory_availability.available + EXCLUDED.available`,
		sku, location, qty); err != nil {
		t.Fatalf("seed availability for %s: %v", sku, err)
	}
	if err := PutawayToBin("default", binCode, sku, qty, "system"); err != nil {
		t.Fatalf("putaway %d x %s into %s: %v", qty, sku, binCode, err)
	}
}

func isoDaysFromNow(d int) string {
	return time.Now().UTC().AddDate(0, 0, d).Format("2006-01-02")
}

func TestTraceability(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("Failed to get tenant schema: %v", err)
	}

	// -----------------------------------------------------------------------
	// 42.1.1 - the tracking flags, and the guarantee that a None item is
	// untouched by everything else in this Stage.
	// -----------------------------------------------------------------------
	t.Run("ItemTrackingFlags", func(t *testing.T) {
		const sku = "SKU-TRACE-FLAG"
		traceCleanup(t, schema, "SKU-TRACE-FLAG")
		defer traceCleanup(t, schema, "SKU-TRACE-FLAG")
		seedTrackedItem(t, schema, sku, TrackingBatch, 180, 30, 15)

		tracking, err := GetItemTracking(tenantID, sku)
		if err != nil {
			t.Fatalf("GetItemTracking: %v", err)
		}
		if !tracking.TracksBatch() {
			t.Errorf("expected TracksBatch() for mode %q", tracking.Mode)
		}
		if tracking.TracksSerial() {
			t.Errorf("mode %q must not track serials", tracking.Mode)
		}
		if tracking.ShelfLifeDays != 180 || tracking.MinShelfLifeOnPickDays != 15 {
			t.Errorf("shelf-life fields not read back: %+v", tracking)
		}
		if got := ResolveAllocationStrategy(tenantID, sku); got != StrategyFEFO {
			t.Errorf("expected FEFO for a batch-tracked item, got %q", got)
		}

		// An unknown SKU must return "no gates apply" rather than an error -
		// this is called per line on the receipt path, and turning a deleted
		// item into a hard failure here would duplicate a rejection that the
		// item-existence validation already owns.
		unknown, err := GetItemTracking(tenantID, "SKU-TRACE-DOES-NOT-EXIST")
		if err != nil {
			t.Fatalf("GetItemTracking on an unknown sku must not error: %v", err)
		}
		if unknown.TracksBatch() || unknown.Mode != TrackingNone {
			t.Errorf("unknown sku must default to None, got %+v", unknown)
		}
		if got := ResolveAllocationStrategy(tenantID, "SKU-TRACE-DOES-NOT-EXIST"); got != StrategyFIFO {
			t.Errorf("expected FIFO for an untracked item, got %q", got)
		}
	})

	// -----------------------------------------------------------------------
	// 42.1.2 - the Batch master: auto-registration, the derive-expiry rule,
	// and the deliberate refusal to overwrite an existing lot's dates.
	// -----------------------------------------------------------------------
	t.Run("BatchMasterRegistration", func(t *testing.T) {
		const sku = "SKU-TRACE-REG"
		traceCleanup(t, schema, "SKU-TRACE-REG")
		defer traceCleanup(t, schema, "SKU-TRACE-REG")
		seedTrackedItem(t, schema, sku, TrackingBatch, 100, 0, 0)

		mfg := isoDaysFromNow(-10)
		b, err := EnsureBatch(tenantID, BatchInfo{BatchNo: "LOT-A", Item: sku, MfgDate: mfg}, 50, "system")
		if err != nil {
			t.Fatalf("EnsureBatch: %v", err)
		}
		// No expiry was supplied, so it must have been derived from the item's
		// 100-day shelf life.
		wantExpiry := time.Now().UTC().AddDate(0, 0, -10).AddDate(0, 0, 100).Format("2006-01-02")
		if b.ExpiryDate != wantExpiry {
			t.Errorf("expected expiry derived as mfg + shelf_life = %s, got %q", wantExpiry, b.ExpiryDate)
		}
		if b.Status != BatchActive {
			t.Errorf("a newly registered batch must be Active, got %q", b.Status)
		}

		// A second receipt of the same lot must NOT move the expiry: the first
		// receipt is authoritative, and a mistyped date on receipt #4 silently
		// re-dating stock already on the shelf is the bug this prevents.
		again, err := EnsureBatch(tenantID, BatchInfo{BatchNo: "LOT-A", Item: sku, ExpiryDate: isoDaysFromNow(999)}, 10, "system")
		if err != nil {
			t.Fatalf("EnsureBatch (second receipt): %v", err)
		}
		if again.ExpiryDate != wantExpiry {
			t.Errorf("second receipt must not overwrite an existing expiry: got %q, want %q", again.ExpiryDate, wantExpiry)
		}
		if again.DocID != b.DocID {
			t.Errorf("second receipt of the same lot must reuse the same Batch document (%s vs %s)", again.DocID, b.DocID)
		}

		// But a blank IS filled in - the supplier batch was unknown at first
		// receipt and known at the second.
		filled, err := EnsureBatch(tenantID, BatchInfo{BatchNo: "LOT-A", Item: sku, SupplierBatch: "SUP-77"}, 5, "system")
		if err != nil {
			t.Fatalf("EnsureBatch (fill blank): %v", err)
		}
		if filled.SupplierBatch != "SUP-77" {
			t.Errorf("a blank field must be filled by a later receipt, got %q", filled.SupplierBatch)
		}

		// (item, batch_no) uniqueness, and the item-must-exist rule.
		if err := validateBatchMasterRules(tenantID, "SOME-OTHER-ID",
			map[string]interface{}{"item": sku, "batch_no": "LOT-A"}); err == nil {
			t.Error("expected a duplicate (item, batch_no) to be rejected")
		}
		if err := validateBatchMasterRules(tenantID, "NEW-ID",
			map[string]interface{}{"item": "SKU-TRACE-REG-NOPE", "batch_no": "LOT-Z"}); err == nil {
			t.Error("expected a batch against a nonexistent item to be rejected")
		}
		// Expiry before manufacture is always a typo, and letting it save means
		// FEFO allocates that lot first forever.
		if err := validateBatchMasterRules(tenantID, "NEW-ID2", map[string]interface{}{
			"item": sku, "batch_no": "LOT-BACKWARDS", "mfg_date": isoDaysFromNow(10), "expiry_date": isoDaysFromNow(5),
		}); err == nil {
			t.Error("expected expiry-before-manufacture to be rejected")
		}
		// A lot number reused across two DIFFERENT items is legitimate and must
		// save - this is the case that makes batch_no unusable as a document id.
		seedTrackedItem(t, schema, "SKU-TRACE-REG2", TrackingBatch, 0, 0, 0)
		if err := validateBatchMasterRules(tenantID, "NEW-ID3",
			map[string]interface{}{"item": "SKU-TRACE-REG2", "batch_no": "LOT-A"}); err != nil {
			t.Errorf("the same lot number under a different item must be allowed: %v", err)
		}
	})

	// -----------------------------------------------------------------------
	// 42.1.3 - the batch sub-ledger, and the invariant that keeps it from
	// becoming a second source of truth.
	// -----------------------------------------------------------------------
	t.Run("BatchStockNeverExceedsBin", func(t *testing.T) {
		const sku = "SKU-TRACE-BIN"
		const bin = "BIN-SKU-TRACE-BIN-01"
		traceCleanup(t, schema, "SKU-TRACE-BIN")
		defer traceCleanup(t, schema, "SKU-TRACE-BIN")
		seedTrackedItem(t, schema, sku, TrackingBatch, 0, 0, 0)
		seedBinWithStock(t, schema, bin, sku, "WH01", 10)

		if _, err := EnsureBatch(tenantID, BatchInfo{BatchNo: "LOT-B1", Item: sku}, 10, "system"); err != nil {
			t.Fatalf("EnsureBatch: %v", err)
		}
		if err := RecordBatchPutaway(tenantID, bin, sku, "LOT-B1", "Good", 6, "system"); err != nil {
			t.Fatalf("RecordBatchPutaway 6 of 10: %v", err)
		}
		// 6 assigned of 10 in the bin; assigning 5 more would claim 11.
		if err := RecordBatchPutaway(tenantID, bin, sku, "LOT-B1", "Good", 5, "system"); err == nil {
			t.Error("expected batch assignment beyond the bin's own qty to be refused")
		}
		// The remaining 4 must still fit exactly.
		if _, err := EnsureBatch(tenantID, BatchInfo{BatchNo: "LOT-B2", Item: sku}, 4, "system"); err != nil {
			t.Fatalf("EnsureBatch LOT-B2: %v", err)
		}
		if err := RecordBatchPutaway(tenantID, bin, sku, "LOT-B2", "Good", 4, "system"); err != nil {
			t.Errorf("the exact remaining qty must be assignable: %v", err)
		}

		// An unregistered lot must be refused - the sub-ledger may be
		// incomplete, but it may never point at a batch nobody registered.
		if err := RecordBatchPutaway(tenantID, bin, sku, "LOT-GHOST", "Good", 1, "system"); err == nil {
			t.Error("expected an unregistered batch to be refused")
		}

		// Consumption decrements the sub-ledger and stamps the ledger.
		if err := ConsumeBatchStock(tenantID, bin, sku, "LOT-B1", "Good", 2, "SalesOrder", "SO-TRACE-1", "system"); err != nil {
			t.Fatalf("ConsumeBatchStock: %v", err)
		}
		if err := ConsumeBatchStock(tenantID, bin, sku, "LOT-B1", "Good", 99, "SalesOrder", "SO-TRACE-1", "system"); err == nil {
			t.Error("expected consuming more than the lot holds to be refused")
		}
		rows, err := GetBatchStock(tenantID, sku, "WH01", "LOT-B1")
		if err != nil {
			t.Fatalf("GetBatchStock: %v", err)
		}
		if len(rows) != 1 || rows[0].Qty != 4 {
			t.Errorf("expected LOT-B1 to hold 6-2=4, got %+v", rows)
		}

		// The movement is on the append-only ledger, carrying its lot - which
		// is the whole reason recall needs no history table of its own.
		hist, err := GetBatchMovementHistory(tenantID, sku, "LOT-B1")
		if err != nil {
			t.Fatalf("GetBatchMovementHistory: %v", err)
		}
		foundConsume := false
		for _, h := range hist {
			if h.VoucherType == "SalesOrder" && h.VoucherID == "SO-TRACE-1" && h.Qty == -2 {
				foundConsume = true
			}
		}
		if !foundConsume {
			t.Errorf("expected the consumption to appear on the batch's ledger history, got %+v", hist)
		}
	})

	// -----------------------------------------------------------------------
	// 42.1.4 - batch capture at receipt.
	// -----------------------------------------------------------------------
	t.Run("BatchCaptureAtReceipt", func(t *testing.T) {
		const sku = "SKU-TRACE-GRN"
		traceCleanup(t, schema, "SKU-TRACE-GRN")
		defer traceCleanup(t, schema, "SKU-TRACE-GRN")
		seedTrackedItem(t, schema, sku, TrackingBatch, 0, 60, 0)

		// A batch-tracked item received with no lot number is refused, and
		// refused BEFORE anything posts.
		noBatch := []interface{}{map[string]interface{}{"sku": sku, "qty": 5.0}}
		if _, err := PostGRNReceiptWithQC(tenantID, "WH01", noBatch, "system", "GRN-TRACE-NB"); err == nil {
			t.Error("expected a batch-tracked receipt with no lot number to be refused")
		}
		var posted int
		_ = db.DB.QueryRow("SELECT COALESCE(SUM(on_hand),0) FROM "+schema+".inventory_availability WHERE sku = $1", sku).Scan(&posted)
		if posted != 0 {
			t.Errorf("a refused receipt must post nothing, but on_hand is %d", posted)
		}

		// Short-dated goods are refused at the door: the item wants 60 days
		// left on receipt and this delivery has 10.
		shortDated := []interface{}{map[string]interface{}{
			"sku": sku, "qty": 5.0, "batch_no": "LOT-SHORT", "expiry_date": isoDaysFromNow(10),
		}}
		if _, err := PostGRNReceiptWithQC(tenantID, "WH01", shortDated, "system", "GRN-TRACE-SD"); err == nil {
			t.Error("expected short-dated goods to be refused on receipt")
		}

		// A good receipt posts stock AND auto-registers the lot.
		ok := []interface{}{map[string]interface{}{
			"sku": sku, "qty": 10.0, "accepted_qty": 8.0, "damaged_qty": 2.0,
			"batch_no": "LOT-GOOD", "mfg_date": isoDaysFromNow(-5), "expiry_date": isoDaysFromNow(400),
		}}
		if _, err := PostGRNReceiptWithQC(tenantID, "WH01", ok, "system", "GRN-TRACE-OK"); err != nil {
			t.Fatalf("PostGRNReceiptWithQC: %v", err)
		}
		batch, err := GetBatch(tenantID, sku, "LOT-GOOD")
		if err != nil || batch == nil {
			t.Fatalf("expected LOT-GOOD to be auto-registered by the receipt (err=%v)", err)
		}
		if batch.ExpiryDate != isoDaysFromNow(400) {
			t.Errorf("expected the receipt's own expiry to be kept, got %q", batch.ExpiryDate)
		}
		// The existing accepted/rejected/damaged split is untouched by 42.1.4.
		var onHand, available, damaged int
		if err := db.DB.QueryRow("SELECT on_hand, available, damaged FROM "+schema+".inventory_availability WHERE sku = $1 AND location_code = 'WH01'", sku).
			Scan(&onHand, &available, &damaged); err != nil {
			t.Fatalf("read availability: %v", err)
		}
		if onHand != 10 || available != 8 || damaged != 2 {
			t.Errorf("expected on_hand=10 available=8 damaged=2, got %d/%d/%d", onHand, available, damaged)
		}
		// The accepted portion must produce exactly ONE ledger entry, and that
		// entry must carry the lot. Writing a separate batch-stamped entry
		// alongside the one PostInventoryLedgerWithVoucher already writes would
		// double-count the same physical movement in every report that sums
		// ledger qty - which is why batch_no rides on the accepted line instead.
		//
		// Scoped to the entries with no to_status: the damaged split writes its
		// own positive entry tagged ToStatus=Damaged (26.10.1's behaviour, since
		// on_hand really did move into the damaged bucket), and that one is not
		// what this assertion is about.
		var ledgerRows, ledgerQty int
		if err := db.DB.QueryRow(`SELECT COUNT(*), COALESCE(SUM((data->>'qty')::numeric),0)::int FROM `+schema+`.documents
			WHERE doctype = 'StockLedgerEntry' AND data->>'item_id' = $1 AND data->>'voucher_id' = 'GRN-TRACE-OK'
			  AND (data->>'qty')::numeric > 0 AND COALESCE(data->>'to_status', '') = ''`, sku).Scan(&ledgerRows, &ledgerQty); err != nil {
			t.Fatalf("read ledger: %v", err)
		}
		if ledgerRows != 1 || ledgerQty != 8 {
			t.Errorf("expected exactly one accepted-qty ledger entry of 8, got %d rows totalling %d", ledgerRows, ledgerQty)
		}
		var ledgerBatch string
		if err := db.DB.QueryRow(`SELECT COALESCE(data->>'batch_no','') FROM `+schema+`.documents
			WHERE doctype = 'StockLedgerEntry' AND data->>'item_id' = $1 AND data->>'voucher_id' = 'GRN-TRACE-OK'
			  AND (data->>'qty')::numeric > 0 AND COALESCE(data->>'to_status', '') = ''`, sku).Scan(&ledgerBatch); err != nil {
			t.Fatalf("read ledger batch: %v", err)
		}
		if ledgerBatch != "LOT-GOOD" {
			t.Errorf("expected the receipt's ledger entry to carry batch LOT-GOOD, got %q", ledgerBatch)
		}
		// And the whole on_hand movement is still accounted for exactly once.
		var totalPositive int
		if err := db.DB.QueryRow(`SELECT COALESCE(SUM((data->>'qty')::numeric),0)::int FROM `+schema+`.documents
			WHERE doctype = 'StockLedgerEntry' AND data->>'item_id' = $1 AND data->>'voucher_id' = 'GRN-TRACE-OK'
			  AND (data->>'qty')::numeric > 0`, sku).Scan(&totalPositive); err != nil {
			t.Fatalf("read ledger total: %v", err)
		}
		if totalPositive != 10 {
			t.Errorf("expected the ledger to account for on_hand=10 exactly once, got %d", totalPositive)
		}
	})

	// -----------------------------------------------------------------------
	// 42.1.5 / 42.1.6 - FEFO, and the gates that filter it. The core of the
	// phase.
	// -----------------------------------------------------------------------
	t.Run("FEFOAllocationAndExpiryGates", func(t *testing.T) {
		const sku = "SKU-TRACE-FEFO"
		const binA = "BIN-SKU-TRACE-FEFO-A"
		const binB = "BIN-SKU-TRACE-FEFO-B"
		const binC = "BIN-SKU-TRACE-FEFO-C"
		traceCleanup(t, schema, "SKU-TRACE-FEFO")
		defer traceCleanup(t, schema, "SKU-TRACE-FEFO")
		// 7-day minimum remaining shelf life to pick.
		seedTrackedItem(t, schema, sku, TrackingBatch, 0, 0, 7)
		seedBinWithStock(t, schema, binA, sku, "WH01", 5)
		seedBinWithStock(t, schema, binB, sku, "WH01", 5)
		seedBinWithStock(t, schema, binC, sku, "WH01", 5)

		// binA holds the LATEST-expiring lot but was binned FIRST, which is
		// precisely the case where FIFO and FEFO disagree - if this test passes
		// under FIFO it proves nothing.
		lots := []struct {
			bin, lot, expiry string
		}{
			{binA, "LOT-LATE", isoDaysFromNow(365)},
			{binB, "LOT-SOON", isoDaysFromNow(30)},
			{binC, "LOT-TOOSOON", isoDaysFromNow(3)}, // inside the 7-day pick minimum
		}
		for _, l := range lots {
			if _, err := EnsureBatch(tenantID, BatchInfo{BatchNo: l.lot, Item: sku, ExpiryDate: l.expiry}, 5, "system"); err != nil {
				t.Fatalf("EnsureBatch %s: %v", l.lot, err)
			}
			if err := RecordBatchPutaway(tenantID, l.bin, sku, l.lot, "Good", 5, "system"); err != nil {
				t.Fatalf("RecordBatchPutaway %s: %v", l.lot, err)
			}
		}

		got, shortfall, err := AllocateFromStock(tenantID, sku, "WH01", 8)
		if err != nil {
			t.Fatalf("AllocateFromStock: %v", err)
		}
		// FEFO: LOT-SOON (30d) first, then LOT-LATE (365d). LOT-TOOSOON is
		// invisible - it is inside the item's minimum remaining shelf life.
		if len(got) != 2 {
			t.Fatalf("expected 2 allocation lines, got %d: %+v", len(got), got)
		}
		if got[0].BatchNo != "LOT-SOON" || got[0].Qty != 5 {
			t.Errorf("expected the earliest-expiry lot first (LOT-SOON x5), got %+v", got[0])
		}
		if got[1].BatchNo != "LOT-LATE" || got[1].Qty != 3 {
			t.Errorf("expected LOT-LATE x3 second, got %+v", got[1])
		}
		if shortfall != 0 {
			t.Errorf("expected no shortfall for 8 of 10 pickable units, got %d", shortfall)
		}

		// Asking for more than the PICKABLE stock reports a shortfall rather
		// than reaching into the short-dated lot. The 5 units of LOT-TOOSOON
		// are physically there and deliberately not offered.
		_, shortfall, err = AllocateFromStock(tenantID, sku, "WH01", 15)
		if err != nil {
			t.Fatalf("AllocateFromStock (over-ask): %v", err)
		}
		if shortfall != 5 {
			t.Errorf("expected a shortfall of 5 (the short-dated lot must not be allocated), got %d", shortfall)
		}

		// The single-lot expression of the same rule, for a scan-driven pick.
		if err := ValidateBatchForIssue(tenantID, sku, "LOT-TOOSOON"); err == nil {
			t.Error("expected a lot inside the pick minimum to be refused for issue")
		}
		if err := ValidateBatchForIssue(tenantID, sku, "LOT-SOON"); err != nil {
			t.Errorf("expected a lot outside the pick minimum to be issuable: %v", err)
		}
		if err := ValidateBatchForIssue(tenantID, sku, ""); err == nil {
			t.Error("expected a batch-tracked item with no lot number to be refused for issue")
		}

		// A held lot disappears from allocation entirely, whatever its dates.
		if err := SetBatchStatus(tenantID, sku, "LOT-SOON", BatchQuarantined, "damaged pallet", "system"); err != nil {
			t.Fatalf("SetBatchStatus: %v", err)
		}
		got, _, err = AllocateFromStock(tenantID, sku, "WH01", 3)
		if err != nil {
			t.Fatalf("AllocateFromStock (after quarantine): %v", err)
		}
		if len(got) != 1 || got[0].BatchNo != "LOT-LATE" {
			t.Errorf("a quarantined lot must not be allocated; got %+v", got)
		}
		if err := ValidateBatchForIssue(tenantID, sku, "LOT-SOON"); err == nil {
			t.Error("expected a quarantined lot to be refused for issue")
		}
		// Releasing a hold demands a stated reason - it is the first thing a
		// recall audit asks about.
		if err := SetBatchStatus(tenantID, sku, "LOT-SOON", BatchActive, "", "system"); err == nil {
			t.Error("expected releasing a quarantined lot with no reason to be refused")
		}
		if err := SetBatchStatus(tenantID, sku, "LOT-SOON", BatchActive, "QC re-inspected, pallet intact", "system"); err != nil {
			t.Errorf("expected a reasoned release to be allowed: %v", err)
		}
	})

	// -----------------------------------------------------------------------
	// The regression that matters most: a warehouse with no batch-tracked item
	// must get exactly the pick list it got before Stage 42 existed.
	// -----------------------------------------------------------------------
	t.Run("UntrackedItemStillAllocatesFIFO", func(t *testing.T) {
		const sku = "SKU-TRACE-FIFO"
		const binOld = "BIN-SKU-TRACE-FIFO-OLD"
		const binNew = "BIN-SKU-TRACE-FIFO-NEW"
		traceCleanup(t, schema, "SKU-TRACE-FIFO")
		defer traceCleanup(t, schema, "SKU-TRACE-FIFO")
		seedTrackedItem(t, schema, sku, "", 0, 0, 0) // no tracking_mode at all
		seedBinWithStock(t, schema, binOld, sku, "WH01", 4)
		seedBinWithStock(t, schema, binNew, sku, "WH01", 4)
		// Force binOld to look older than binNew, which is what FIFO orders on.
		if _, err := db.DB.Exec("UPDATE "+schema+".bin_stock SET updated_at = CURRENT_TIMESTAMP - INTERVAL '10 days' WHERE bin_code = $1 AND sku = $2", binOld, sku); err != nil {
			t.Fatalf("age binOld: %v", err)
		}

		got, shortfall, err := AllocateFromStock(tenantID, sku, "WH01", 6)
		if err != nil {
			t.Fatalf("AllocateFromStock: %v", err)
		}
		if len(got) != 2 || got[0].BinCode != binOld {
			t.Fatalf("expected oldest-binned stock first (FIFO), got %+v", got)
		}
		if got[0].BatchNo != "" || got[1].BatchNo != "" {
			t.Errorf("an untracked item's allocation must carry no batch at all, got %+v", got)
		}
		if shortfall != 0 {
			t.Errorf("expected no shortfall, got %d", shortfall)
		}
		// And with a batch sub-ledger row present but the item untracked, FIFO
		// still wins - the strategy follows the item's declaration, not
		// whatever data happens to exist.
		if got := ResolveAllocationStrategy(tenantID, sku); got != StrategyFIFO {
			t.Errorf("expected FIFO for an item with no tracking_mode, got %q", got)
		}
	})

	// -----------------------------------------------------------------------
	// 42.1.6 - the expiry sweep, and its idempotence.
	// -----------------------------------------------------------------------
	t.Run("ExpirySweepQuarantines", func(t *testing.T) {
		const sku = "SKU-TRACE-SWEEP"
		const bin = "BIN-SKU-TRACE-SWEEP-01"
		traceCleanup(t, schema, "SKU-TRACE-SWEEP")
		defer traceCleanup(t, schema, "SKU-TRACE-SWEEP")
		seedTrackedItem(t, schema, sku, TrackingBatch, 0, 0, 0)
		seedBinWithStock(t, schema, bin, sku, "WH01", 6)

		if _, err := EnsureBatch(tenantID, BatchInfo{BatchNo: "LOT-DEAD", Item: sku, ExpiryDate: isoDaysFromNow(-1)}, 6, "system"); err != nil {
			t.Fatalf("EnsureBatch: %v", err)
		}
		if err := RecordBatchPutaway(tenantID, bin, sku, "LOT-DEAD", "Good", 6, "system"); err != nil {
			t.Fatalf("RecordBatchPutaway: %v", err)
		}

		var availableBefore int
		_ = db.DB.QueryRow("SELECT available FROM "+schema+".inventory_availability WHERE sku = $1 AND location_code = 'WH01'", sku).Scan(&availableBefore)

		res, err := SweepExpiredBatches(tenantID, "system")
		if err != nil {
			t.Fatalf("SweepExpiredBatches: %v", err)
		}
		if res.BatchesExpired < 1 || res.QtyQuarantined < 6 {
			t.Errorf("expected the expired lot to be swept and its 6 units quarantined, got %+v", res)
		}

		batch, err := GetBatch(tenantID, sku, "LOT-DEAD")
		if err != nil || batch == nil {
			t.Fatalf("re-read batch: %v", err)
		}
		if batch.Status != BatchExpired {
			t.Errorf("expected status Expired, got %q", batch.Status)
		}
		// The stock left `available` through the existing condition-transition
		// path, so it is no longer sellable.
		var availableAfter, qcHold int
		if err := db.DB.QueryRow("SELECT available, qc_hold FROM "+schema+".inventory_availability WHERE sku = $1 AND location_code = 'WH01'", sku).
			Scan(&availableAfter, &qcHold); err != nil {
			t.Fatalf("read availability: %v", err)
		}
		if availableAfter != availableBefore-6 {
			t.Errorf("expected available to drop by 6 (%d -> %d), got %d", availableBefore, availableBefore-6, availableAfter)
		}
		// The batch sub-ledger moved with it, so the two never disagree.
		rows, err := GetBatchStock(tenantID, sku, "WH01", "LOT-DEAD")
		if err != nil {
			t.Fatalf("GetBatchStock: %v", err)
		}
		for _, r := range rows {
			if r.Condition == "Good" && r.Qty > 0 {
				t.Errorf("expected no Good-condition stock left for an expired lot, got %+v", r)
			}
		}
		// Nothing is allocatable from it any more.
		_, shortfall, err := AllocateFromStock(tenantID, sku, "WH01", 1)
		if err != nil {
			t.Fatalf("AllocateFromStock after sweep: %v", err)
		}
		if shortfall != 1 {
			t.Errorf("expected expired stock to be unallocatable, got shortfall %d", shortfall)
		}

		// Running the sweep again must quarantine nothing twice.
		res2, err := SweepExpiredBatches(tenantID, "system")
		if err != nil {
			t.Fatalf("SweepExpiredBatches (second run): %v", err)
		}
		for _, note := range res2.Notes {
			if strings.Contains(note, sku) {
				t.Errorf("second sweep should have skipped %s entirely, but noted: %s", sku, note)
			}
		}
	})

	// -----------------------------------------------------------------------
	// 42.1.1 - the item-side validation, which exists to stop two silently
	// unusable configurations reaching a warehouse floor.
	// -----------------------------------------------------------------------
	t.Run("ItemTrackingValidation", func(t *testing.T) {
		cases := []struct {
			name    string
			payload map[string]interface{}
			wantErr bool
		}{
			{"unrecognised mode", map[string]interface{}{"tracking_mode": "Lots"}, true},
			{"negative shelf life", map[string]interface{}{"shelf_life_days": -1}, true},
			{"receipt minimum exceeds shelf life", map[string]interface{}{"shelf_life_days": 30, "min_shelf_life_on_receipt_days": 45}, true},
			{"pick minimum exceeds shelf life", map[string]interface{}{"shelf_life_days": 30, "min_shelf_life_on_pick_days": 45}, true},
			{"sane configuration", map[string]interface{}{"tracking_mode": TrackingBatch, "shelf_life_days": 180, "min_shelf_life_on_receipt_days": 90, "min_shelf_life_on_pick_days": 30}, false},
			{"nothing set at all", map[string]interface{}{}, false},
		}
		for _, c := range cases {
			err := validateItemTrackingFields(c.payload)
			if c.wantErr && err == nil {
				t.Errorf("%s: expected rejection, got none", c.name)
			}
			if !c.wantErr && err != nil {
				t.Errorf("%s: expected acceptance, got %v", c.name, err)
			}
		}
	})
}

