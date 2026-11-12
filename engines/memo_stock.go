package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// Stage 148.16: memo / consignment stock (module memo_stock, off by default).
//
// Decision D5: a vendor-owned bucket at zero value. No ownership journal and no
// separate consignment warehouse.
//
//   - A receipt marked "Memo stock" adds its units to stock but not to our cost:
//     no item_cost blend, no Dr 1200 / Cr 2100. Each line becomes a memo lot
//     (memo_stock_lots) holding the rate we agreed with the vendor.
//   - The units in memo lots stay out of the stock in hand the average is spread
//     over and out of the stock value reports.
//   - A sale (POS or invoice) takes memo units first, oldest lot first. The
//     units leave our cost: they are not booked at the average. Instead the
//     sale raises one vendor bill per vendor for the units sold, at the vendor
//     agreement's rate (148.6), else the rate on the receipt, and books the
//     cost at that rate: Dr 5100 / Cr 2100. The bill is Matched; paying it is
//     the ordinary vendor payment.
//   - A return to the vendor of memo units raises no debit note and moves no
//     cost; the lot's open quantity goes down.
//
// Everything past the receipt follows the lots, not the module switch, so a
// client who switches the module off with memo stock still open keeps the same
// postings for it. A client who never used it has no lots and no change.

const (
	ModuleMemoStock = "memo_stock"

	memoOffCode      = "PURCHA-5050"
	memoReturnCode   = "PURCHA-5051"
	memoVendorCode   = "PURCHA-5052"
	memoCancelCode   = "PURCHA-5053"
	memoTrackedCode  = "PURCHA-5054"
	memoBillCode     = "PURCHA-5055"
	costSourceMemo   = "Memo rate"
	memoYes          = "Yes"
	memoReceiptField = "memo_stock"
)

// moduleGatedFields hides a field until its module is on, beside settingGatedFields.
var moduleGatedFields = map[string]map[string]string{
	"GRN":            {memoReceiptField: ModuleMemoStock},
	"SalesInvoice":   {"salesman_id": ModuleSalesIncentives, "salesman_split": ModuleSalesIncentives},                                                                                 // 148.30
	"Customer":       {"bill_to_customer": ModuleWholesale, "summary_invoice": ModuleWholesale, "group_by_place_of_supply": ModuleWholesale, "group_by_customer_po": ModuleWholesale}, // 148.31
	"SalesOrderLine": {"instalments": ModuleWholesale},                                                                                                                                // 148.31
	"SalesOrder":     {"delivery_date": ModuleWholesale},                                                                                                                              // 148.31
	"Item":           {"dropship": ModuleWholesale},                                                                                                                                   // 148.32
	"PurchaseOrder":  {"dropship_order_id": ModuleWholesale, "ship_to_address": ModuleWholesale},                                                                                      // 148.32
}

func memoErr(code, msg string) error { return &ValidationError{Code: code, Message: msg} }

// memoNoBillOnReceipt is the refusal for anything that would owe or value a memo
// receipt itself: its sales raise the bills.
func memoNoBillOnReceipt() error {
	return memoErr(memoBillCode, "A memo receipt has no bill of its own: its sales raise the bills.")
}

// isMemoReceipt says this receipt's units are the vendor's, so no bill, payment,
// correction or landed cost belongs on it. While the module is on the field
// decides, as it does at posting. With the module off only a receipt that made
// lots still counts: everything past the receipt follows the lots.
func isMemoReceipt(tenantID, grnID string, grn map[string]interface{}) bool {
	if !strings.EqualFold(strField(grn, memoReceiptField), memoYes) {
		return false
	}
	if on, _ := IsModuleEnabled(tenantID, ModuleMemoStock); on {
		return true
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil || !memoTable(db.DB, schema) {
		return false
	}
	var made bool
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM %s.memo_stock_lots WHERE grn_id = $1)`, schema), grnID).Scan(&made)
	return err == nil && made
}

// memoTable reports whether the tenant's memo lots table exists. A tenant that
// has not had the migration yet reads as having no memo stock.
func memoTable(q queryRower, schema string) bool {
	var ok bool
	if err := q.QueryRow(`SELECT to_regclass($1) IS NOT NULL`, schema+".memo_stock_lots").Scan(&ok); err != nil {
		return false
	}
	return ok
}

// memoTrimLotsTx: stock that left by a write-off, a count or a loss took memo units
// with it and nothing records which. Open memo units can never be more than the
// units held, so the excess is marked lost on the oldest lots: no sale draws it and
// no vendor bill sees it. stockIn is the units of a receipt being costed, already
// posted, which are not memo units. The vendor is settled by hand for lost units.
func memoTrimLotsTx(tx *sql.Tx, schema, sku string, stockIn float64) error {
	if !memoTable(tx, schema) {
		return nil
	}
	rows, err := tx.Query(fmt.Sprintf(`
		SELECT id, (qty_received - qty_sold - qty_returned - qty_lost)::float8 FROM %s.memo_stock_lots
		WHERE sku = $1 AND qty_received - qty_sold - qty_returned - qty_lost > 0 ORDER BY received_at, id FOR UPDATE`, schema), sku)
	if err != nil {
		return err
	}
	type openLot struct {
		id   int64
		open float64
	}
	var lots []openLot
	var totalOpen float64
	for rows.Next() {
		var lot openLot
		if err := rows.Scan(&lot.id, &lot.open); err != nil {
			rows.Close()
			return err
		}
		lots = append(lots, lot)
		totalOpen += lot.open
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if len(lots) == 0 {
		return nil
	}
	held, err := stockInHandBeforeMemo(tx, schema, sku)
	if err != nil {
		return err
	}
	excess := totalOpen - (held - stockIn)
	for _, lot := range lots {
		if excess <= 0.0005 {
			break
		}
		take := math.Min(lot.open, excess)
		if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.memo_stock_lots SET qty_lost = qty_lost + $2 WHERE id = $1`, schema), lot.id, take); err != nil {
			return err
		}
		excess -= take
	}
	return nil
}

// memoOpenBySKU is the memo units still held, per SKU, never more than the
// stock on hand (units written off or lost are not memo units any more).
func memoOpenBySKU(q queryRower, schema string) map[string]float64 {
	out := map[string]float64{}
	if !memoTable(q, schema) {
		return out
	}
	rows, err := q.Query(fmt.Sprintf(`
		SELECT l.sku, LEAST(SUM(l.qty_received - l.qty_sold - l.qty_returned - l.qty_lost),
		       COALESCE((SELECT SUM(ia.on_hand) FROM %[1]s.inventory_availability ia WHERE ia.sku = l.sku), 0))::float8
		FROM %[1]s.memo_stock_lots l
		GROUP BY l.sku HAVING SUM(l.qty_received - l.qty_sold - l.qty_returned - l.qty_lost) > 0`, schema))
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var sku string
		var n float64
		if rows.Scan(&sku, &n) == nil && n > 0 {
			out[sku] = n
		}
	}
	return out
}

func memoOpenQty(q queryRower, schema, sku string) float64 {
	if !memoTable(q, schema) {
		return 0
	}
	var n float64
	if err := q.QueryRow(fmt.Sprintf(`
		SELECT COALESCE(LEAST(COALESCE(SUM(l.qty_received - l.qty_sold - l.qty_returned - l.qty_lost), 0),
		       COALESCE((SELECT SUM(ia.on_hand) FROM %[1]s.inventory_availability ia WHERE ia.sku = $1), 0)), 0)::float8
		FROM %[1]s.memo_stock_lots l WHERE l.sku = $1`, schema), sku).Scan(&n); err != nil || n < 0 {
		return 0
	}
	return n
}

// stockInHandForCost is the units the average is spread over: what
// stockInHandBeforeMemo counts, less the units a vendor still owns.
func stockInHandForCost(q queryRower, schema, sku string) (float64, error) {
	held, err := stockInHandBeforeMemo(q, schema, sku)
	if err != nil {
		return 0, err
	}
	return math.Max(held-memoOpenQty(q, schema, sku), 0), nil
}

// memoValuePaise is the value the stock reports must leave out: memo units at
// the average rate they would otherwise be counted at.
func memoValuePaise(q queryRower, schema string) int64 {
	open := memoOpenBySKU(q, schema)
	if len(open) == 0 {
		return 0
	}
	var total int64
	for sku, n := range open {
		var rate int64
		_ = q.QueryRow(fmt.Sprintf(`SELECT COALESCE(avg_unit_cost_paise, 0) FROM %s.item_cost WHERE item_code = $1`, schema), sku).Scan(&rate)
		total += rate * int64(n)
	}
	return total
}

// checkMemoGRN runs on a new GRN: Memo stock is Yes or blank, Yes needs the
// module and a vendor.
func checkMemoGRN(tenantID string, payload map[string]interface{}) error {
	v := strings.TrimSpace(strField(payload, memoReceiptField))
	if v == "" {
		delete(payload, memoReceiptField)
		return nil
	}
	if !strings.EqualFold(v, memoYes) {
		return memoErr(memoOffCode, "Memo stock is Yes or blank.")
	}
	if on, _ := IsModuleEnabled(tenantID, ModuleMemoStock); !on {
		delete(payload, memoReceiptField)
		return memoErr(memoOffCode, "Memo stock is off. An admin turns it on in Modules.")
	}
	payload[memoReceiptField] = memoYes
	if strings.TrimSpace(strField(payload, "vendor")) == "" && len(GRNPOIDs(payload)) == 0 {
		return memoErr(memoVendorCode, "Pick the vendor who owns the memo stock.")
	}
	return nil
}

// memoReceiptTx runs first in the receipt costing. For a memo receipt it
// records the lots and leaves the cost alone (handled true); any other receipt
// is left to the ordinary costing.
func memoReceiptTx(tx *sql.Tx, tenantID, schema, grnID, part string, accepted []struct {
	SKU string
	Qty float64
}, grnCosts map[string]int64, poID string) (handled bool, err error) {
	if grnID == "" || !memoTable(tx, schema) {
		return false, nil
	}
	var raw string
	if err := tx.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents WHERE doctype = 'GRN' AND id = $1`, schema), grnID).Scan(&raw); err != nil {
		return false, nil
	}
	var grn map[string]interface{}
	if json.Unmarshal([]byte(raw), &grn) != nil || !strings.EqualFold(strField(grn, memoReceiptField), memoYes) {
		return false, nil
	}
	if on, _ := IsModuleEnabled(tenantID, ModuleMemoStock); !on {
		return false, nil
	}
	vendor := strings.TrimSpace(strField(grn, "vendor"))
	if vendor == "" && poID != "" {
		if po, _, perr := fetchDocData(tenantID, "PurchaseOrder", poID); perr == nil {
			vendor = strings.TrimSpace(firstNonEmpty(strField(po, "vendor"), strField(po, "vendor_id")))
		}
	}
	if vendor == "" {
		return false, memoErr(memoVendorCode, "Pick the vendor who owns the memo stock.")
	}
	// One lot per SKU of the receipt part: two lines of one SKU are one lot of both.
	qtyBySKU := map[string]float64{}
	var skus []string
	for _, line := range accepted {
		if line.Qty <= 0 {
			continue
		}
		if tr, _ := GetItemTracking(tenantID, line.SKU); tr.TracksSerial() || tr.TracksBatch() {
			return true, memoErr(memoTrackedCode, fmt.Sprintf("%s is tracked by serial or batch, so it cannot be received as memo stock.", line.SKU))
		}
		if _, seen := qtyBySKU[line.SKU]; !seen {
			skus = append(skus, line.SKU)
		}
		qtyBySKU[line.SKU] += line.Qty
	}
	for _, sku := range skus {
		rate, ok := grnCosts[sku]
		if !ok {
			rate, _ = resolvePOBaseUnitCostPaise(tenantID, poID, sku)
		}
		if _, err := tx.Exec(fmt.Sprintf(`
			INSERT INTO %s.memo_stock_lots (grn_id, part, sku, vendor_id, location, qty_received, rate_paise)
			VALUES ($1, $2, $3, $4, $5, $6, $7) ON CONFLICT (grn_id, part, sku) DO NOTHING`, schema),
			grnID, part, sku, vendor, strField(grn, "location"), qtyBySKU[sku], rate); err != nil {
			return true, fmt.Errorf("memo lot for %s: %w", sku, err)
		}
	}
	// Charges on a memo receipt (freight and the like) are not capitalised: the units are
	// at no value, so no charge entry is made and a return has none to reverse.
	return true, nil
}

// memoAgreedRatePaise is the vendor agreement's rate for qty units of sku
// today, after the agreement's discount; 0 when there is none.
func memoAgreedRatePaise(tenantID, vendor, sku string, qty float64) int64 {
	rows := loadVendorAgreementRows(tenantID, vendor)
	if len(rows) == 0 {
		return 0
	}
	attrs, _ := designRowItemAttrs(tenantID, sku)
	rule, _ := resolveAgreementRule(rows, attrs, BusinessToday(), func(vendorAgreementRow) float64 { return qty })
	if rule == nil || !(rule.Price > 0) {
		return 0
	}
	return RupeesToPaise(rule.Price * (1 - rule.DiscountPct/100))
}

type memoLot struct {
	id     int64
	vendor string
	open   float64
	rate   int64
}

type memoBillLine struct {
	SKU   string  `json:"sku"`
	Qty   float64 `json:"qty"`
	Rate  float64 `json:"rate"`
	Value float64 `json:"value"`
	paise int64
}

// settleMemoStockTx runs in a sale's transaction after its stock has moved and
// its named pieces are costed. The units drawn from memo lots come out of the
// sale's cost at the average (the returned delta, negative) and are booked at
// the memo rate against a vendor bill instead. The bill's own cost posting is
// made here; the caller only adds the delta to its cost.
func settleMemoStockTx(tx *sql.Tx, tenantID, schema, docType, docID, userID string, lines []saleCostLine) (int64, error) {
	if !memoTable(tx, schema) {
		return 0, nil
	}
	order := make([]int, len(lines))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return lines[order[a]].SKU < lines[order[b]].SKU })
	var delta int64
	bills := map[string][]memoBillLine{}
	salesRows := map[string][]int64{}
	var vendors []string
	for _, i := range order {
		l := &lines[i]
		want := float64(l.Qty - len(l.serialNos))
		if l.BatchNo != "" || want <= 0 || len(l.Parts) > 0 {
			continue
		}
		rows, err := tx.Query(fmt.Sprintf(`
			SELECT id, vendor_id, (qty_received - qty_sold - qty_returned - qty_lost)::float8, rate_paise FROM %s.memo_stock_lots
			WHERE sku = $1 AND qty_received - qty_sold - qty_returned - qty_lost > 0 ORDER BY received_at, id FOR UPDATE`, schema), l.SKU)
		if err != nil {
			return 0, err
		}
		var lots []memoLot
		for rows.Next() {
			var lot memoLot
			if err := rows.Scan(&lot.id, &lot.vendor, &lot.open, &lot.rate); err != nil {
				rows.Close()
				return 0, err
			}
			lots = append(lots, lot)
		}
		rows.Close()
		var takenQty float64
		var takenPaise int64
		for _, lot := range lots {
			take := math.Floor(math.Min(lot.open, want-takenQty))
			if take <= 0 {
				continue
			}
			rate := memoAgreedRatePaise(tenantID, lot.vendor, l.SKU, take)
			if rate <= 0 {
				rate = lot.rate
			}
			value := int64(math.Round(float64(rate) * take))
			if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.memo_stock_lots SET qty_sold = qty_sold + $2 WHERE id = $1`, schema), lot.id, take); err != nil {
				return 0, err
			}
			var saleID int64
			if err := tx.QueryRow(fmt.Sprintf(`
				INSERT INTO %s.memo_stock_sales (lot_id, source_doctype, source_id, sku, vendor_id, qty, rate_paise)
				VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`, schema), lot.id, docType, docID, l.SKU, lot.vendor, take, rate).Scan(&saleID); err != nil {
				return 0, err
			}
			if _, seen := bills[lot.vendor]; !seen {
				vendors = append(vendors, lot.vendor)
			}
			bills[lot.vendor] = append(bills[lot.vendor], memoBillLine{SKU: l.SKU, Qty: take, Rate: PaiseToRupees(rate), Value: PaiseToRupees(value), paise: value})
			salesRows[lot.vendor] = append(salesRows[lot.vendor], saleID)
			takenQty += take
			takenPaise += value
		}
		if takenQty > 0 {
			avg := int64(takenQty) * l.unitPaise
			delta -= avg
			l.ValuePaise += takenPaise - avg
			l.Source = costSourceMemo
		}
		if err := memoTrimLotsTx(tx, schema, l.SKU, 0); err != nil {
			return 0, err
		}
	}
	sort.Strings(vendors)
	for _, vendor := range vendors {
		var total int64
		for _, b := range bills[vendor] {
			total += b.paise
		}
		billID := NewDocID("VI")
		doc := map[string]interface{}{
			"code": billID, "invoice_number": billID, "vendor_id": vendor, "invoice_amount": PaiseToRupees(total),
			"invoice_date": BusinessToday(), "financial_year": documentFinancialYear(time.Now()),
			"status": "Matched", "source_doctype": docType, "source_doc_id": docID,
			"memo_sale": memoYes, "memo_lines": bills[vendor], "payment_basis_exact": true,
		}
		raw, _ := json.Marshal(doc)
		if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'VendorInvoice', $2, 'Matched', $3)`, schema),
			billID, raw, userID); err != nil {
			return 0, fmt.Errorf("the memo vendor bill could not be made: %w", err)
		}
		if total > 0 {
			if err := PostDoubleEntryTx(tx, tenantID, schema, "VendorInvoice", billID,
				map[string]int64{"5100": total}, map[string]int64{"2100": total}, "",
				fmt.Sprintf("VendorInvoice:%s:MEMO_SALE", billID)); err != nil {
				return 0, fmt.Errorf("memo cost posting failed: %w", err)
			}
		}
		for _, id := range salesRows[vendor] {
			if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.memo_stock_sales SET bill_id = $2 WHERE id = $1`, schema), id, billID); err != nil {
				return 0, err
			}
		}
	}
	return delta, nil
}

// memoReturnTx runs in a purchase return's transaction. A return of units from
// a memo receipt raises no debit note and moves no cost: the lines are valued at
// zero and the lot's open quantity goes down. memo is false for any other
// return, which is left untouched.
func memoReturnTx(tx *sql.Tx, schema, grnID string, lines []PurchaseReturnLine) (memo bool, err error) {
	if grnID == "" || !memoTable(tx, schema) {
		return false, nil
	}
	var n int
	if err := tx.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.memo_stock_lots WHERE grn_id = $1`, schema), grnID).Scan(&n); err != nil || n == 0 {
		return false, err
	}
	for i := range lines {
		l := &lines[i]
		if l.StockBucket != PurchaseReturnBucketAccepted {
			continue
		}
		rows, err := tx.Query(fmt.Sprintf(`
			SELECT id, (qty_received - qty_sold - qty_returned - qty_lost)::float8 FROM %s.memo_stock_lots
			WHERE grn_id = $1 AND sku = $2 AND qty_received - qty_sold - qty_returned - qty_lost > 0 ORDER BY id FOR UPDATE`, schema), grnID, l.SKU)
		if err != nil {
			return true, err
		}
		type open struct {
			id int64
			n  float64
		}
		var lots []open
		for rows.Next() {
			var o open
			if err := rows.Scan(&o.id, &o.n); err != nil {
				rows.Close()
				return true, err
			}
			lots = append(lots, o)
		}
		rows.Close()
		left := l.Qty
		for _, o := range lots {
			take := math.Min(o.n, left)
			if take <= 0 {
				continue
			}
			if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.memo_stock_lots SET qty_returned = qty_returned + $2 WHERE id = $1`, schema), o.id, take); err != nil {
				return true, err
			}
			left -= take
		}
		if left > 1e-9 {
			return true, memoErr(memoReturnCode, fmt.Sprintf("Only part of %s is still unsold memo stock; the rest was sold and is on a vendor bill.", l.SKU))
		}
		l.UnitCost, l.LineValue = 0, 0
	}
	return true, nil
}

// refuseCancelAfterMemoSale stops a sale document that drew memo stock from
// being cancelled: its vendor bill and cost are already booked.
func refuseCancelAfterMemoSale(q queryRower, schema, docType, docID string) error {
	if !memoTable(q, schema) {
		return nil
	}
	var n int
	if err := q.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.memo_stock_sales WHERE source_doctype = $1 AND source_id = $2`, schema), docType, docID).Scan(&n); err != nil || n == 0 {
		return err
	}
	return memoErr(memoCancelCode, "This sale took memo stock and raised a vendor bill, so it cannot be cancelled. Take the goods back with a return instead.")
}

// refuseBillCancelAfterMemoSale is the same stop for the till's Cancel Bill, whose
// bill is a POS cart or, for an online order, the invoice its shipment made.
func refuseBillCancelAfterMemoSale(tenantID, billNo string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	if err := refuseCancelAfterMemoSale(db.DB, schema, "POSCart", billNo); err != nil {
		return err
	}
	return refuseCancelAfterMemoSale(db.DB, schema, "SalesInvoice", salesInvoiceForOrder(tenantID, billNo))
}

func init() {
	RegisterReport(ReportDefinition{ID: "memo-stock", Label: "Memo stock", Category: "Procurement", ModuleKey: ModuleMemoStock, StoreKey: "-",
		Columns: []ReportColumn{{Key: "vendor", Label: "Vendor"}, {Key: "sku", Label: "SKU"}, {Key: "grn", Label: "Receipt"}, {Key: "received", Label: "Received"},
			{Key: "sold", Label: "Sold"}, {Key: "returned", Label: "Returned"}, {Key: "lost", Label: "Lost"}, {Key: "open", Label: "Open"}, {Key: "rate", Label: "Rate"}},
		Run: memoStockReport})
}

func memoStockReport(tenantID string, _ map[string]string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	if !memoTable(db.DB, schema) {
		return nil, nil
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT vendor_id, sku, grn_id, qty_received::float8, qty_sold::float8, qty_returned::float8, qty_lost::float8,
		       (qty_received - qty_sold - qty_returned - qty_lost)::float8, rate_paise
		FROM %s.memo_stock_lots ORDER BY vendor_id, sku, received_at, id LIMIT 5000`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]interface{}{}
	for rows.Next() {
		var vendor, sku, grn string
		var rec, sold, ret, lost, open float64
		var rate int64
		if err := rows.Scan(&vendor, &sku, &grn, &rec, &sold, &ret, &lost, &open, &rate); err != nil {
			return nil, err
		}
		out = append(out, map[string]interface{}{"vendor": vendor, "sku": sku, "grn": grn, "received": rec, "sold": sold,
			"returned": ret, "lost": lost, "open": open, "rate": PaiseToRupees(rate)})
	}
	return out, rows.Err()
}

// memoSoldSKU is whether any memo stock of the SKU has been sold.
func memoSoldSKU(q queryRower, schema, sku string) bool {
	if !memoTable(q, schema) {
		return false
	}
	var n int
	return q.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.memo_stock_sales WHERE sku = $1`, schema), sku).Scan(&n) == nil && n > 0
}
