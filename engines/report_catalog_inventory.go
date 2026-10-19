package engines

import (
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"
)

// Catalog reports - Inventory (Stage 75.3.5, Wave A).
//
// On-hand stock per store and SKU is inventory_availability (with its
// blocked / in-transit / QC / damaged buckets); every movement is a
// StockLedgerEntry (signed qty: + in, - out, 0 for a move inside a store);
// bins are bin_stock; the receipt-date barcodes are ItemBarcode records;
// counts are PhysicalInventory + CycleCountLine; label prints are
// sticker_print_log; an item's cost is its moving average (item_cost).
// The ERP has no bulk stock upload, so those two catalog rows wait.

// invRows: one row per store and SKU with stock.
func invRows(tenantID, schema string, p map[string]string) ([]*catRow, error) {
	q := fmt.Sprintf(`SELECT location_code, sku, on_hand, available, committed, reserved, in_transit, blocked + hold_qty, qc_hold, damaged, updated_at
		FROM %s.inventory_availability WHERE (on_hand <> 0 OR available <> 0 OR in_transit <> 0)`, schema)
	var args []interface{}
	if v := strings.TrimSpace(p["location"]); v != "" {
		args = append(args, pqStringArray(strings.Split(v, ",")))
		q += " AND location_code = ANY($1)"
	}
	rows, err := dbQuery(q+" ORDER BY location_code, sku LIMIT 50000", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*catRow
	for rows.Next() {
		var store, sku string
		var onHand, avail, committed, reserved, transit, blocked, qc, damaged float64
		var at time.Time
		if rows.Scan(&store, &sku, &onHand, &avail, &committed, &reserved, &transit, &blocked, &qc, &damaged, &at) != nil {
			continue
		}
		out = append(out, &catRow{ID: store + "/" + sku, Created: at, H: map[string]interface{}{
			"location": store, "sku": sku, "on_hand": onHand, "available": avail, "committed": committed, "reserved": reserved,
			"in_transit": transit, "blocked": blocked, "qc_hold": qc, "damaged": damaged}})
	}
	return out, rows.Err()
}

// invBin is the first bin holding the SKU at the store (bin_stock).
func invBin(r *catRow) map[string]interface{} {
	bins, ok := r.lk.extra["bins"].(map[string]string)
	if !ok {
		bins = map[string]string{}
		rows, err := dbQuery(fmt.Sprintf(`SELECT DISTINCT ON (location_code, sku) location_code, sku, bin_code FROM %s.bin_stock
			WHERE qty > 0 ORDER BY location_code, sku, qty DESC`, r.lk.schema))
		if err == nil {
			for rows.Next() {
				var store, sku, bin string
				if rows.Scan(&store, &sku, &bin) == nil {
					bins[store+"/"+sku] = bin
				}
			}
			rows.Close()
		}
		r.lk.extra["bins"] = bins
	}
	code := bins[r.storeID()+"/"+r.sku()]
	if code == "" {
		return nil
	}
	if b := r.master("Bin", code); b != nil {
		return b
	}
	return map[string]interface{}{"bin_code": code}
}

// invLastMoves: per store/SKU the last receipt and the last sale (ledger).
func invLastMoves(r *catRow) (lastGRN, lastSale string) {
	m, ok := r.lk.extra["lastmoves"].(map[string][2]string)
	if !ok {
		m = map[string][2]string{}
		rows, err := dbQuery(fmt.Sprintf(`SELECT data->>'warehouse_id', data->>'item_id',
			to_char(MAX(created_at) FILTER (WHERE data->>'voucher_type' = 'GRN'), 'YYYY-MM-DD'),
			to_char(MAX(created_at) FILTER (WHERE data->>'voucher_type' IN ('POSInvoice', 'SalesOrder', 'FulfillmentTask')), 'YYYY-MM-DD')
			FROM %s.documents WHERE doctype = 'StockLedgerEntry' AND deleted_at IS NULL GROUP BY 1, 2`, r.lk.schema))
		if err == nil {
			for rows.Next() {
				var store, sku string
				var grn, sale *string
				if rows.Scan(&store, &sku, &grn, &sale) == nil {
					var v [2]string
					if grn != nil {
						v[0] = *grn
					}
					if sale != nil {
						v[1] = *sale
					}
					m[store+"/"+sku] = v
				}
			}
			rows.Close()
		}
		r.lk.extra["lastmoves"] = m
	}
	v := m[r.storeID()+"/"+r.sku()]
	return v[0], v[1]
}

func invAgeDays(r *catRow) interface{} {
	grn, _ := invLastMoves(r)
	if grn == "" {
		return nil
	}
	t, err := time.ParseInLocation("2006-01-02", grn, time.Local)
	if err != nil {
		return nil
	}
	return math.Floor(time.Since(t).Hours() / 24)
}

func invAgeBucket(days float64) string {
	switch {
	case days <= 30:
		return "0-30 days"
	case days <= 60:
		return "31-60 days"
	case days <= 90:
		return "61-90 days"
	case days <= 180:
		return "91-180 days"
	case days <= 365:
		return "181-365 days"
	}
	return "Over 365 days"
}

func invMRP(r *catRow) float64 { return catNum(r.item()["mrp"]) }

// invStockStatus names what the stock is doing.
func invStockStatus(r *catRow) string {
	switch {
	case r.hn("available") > 0:
		return "Available"
	case r.hn("in_transit") > 0:
		return "In transit"
	case r.hn("qc_hold") > 0:
		return "QC hold"
	case r.hn("damaged") > 0:
		return "Damaged"
	case r.hn("blocked") > 0:
		return "Blocked"
	case r.hn("on_hand") < 0:
		return "Negative"
	}
	return "Nil"
}

var invCols = map[string]catFn{
	"Available Qty":     catHN("available"),
	"Blocked Qty":       catHN("blocked"),
	"In-Transit Qty":    catHN("in_transit"),
	"Closing Stock Qty": catHN("on_hand"),
	"MRP Value":         func(r *catRow) interface{} { return r.hn("on_hand") * invMRP(r) },
	"Stock Status":      func(r *catRow) interface{} { return invStockStatus(r) },
	"Location Code":     catStoreCode,
	"Location Name":     catStoreName,
	"Rack": func(r *catRow) interface{} {
		if b := invBin(r); b != nil {
			return firstNonEmpty(catStr(b["rack"]), catStr(b["bin_code"]))
		}
		return nil
	},
	"Shelf": func(r *catRow) interface{} {
		if b := invBin(r); b != nil {
			return b["aisle"]
		}
		return nil
	},
	"Box": func(r *catRow) interface{} {
		if b := invBin(r); b != nil {
			return b["bin_code"]
		}
		return nil
	},
	"Stock Age Days": invAgeDays,
	"Age Bucket": func(r *catRow) interface{} {
		if d, ok := invAgeDays(r).(float64); ok {
			return invAgeBucket(d)
		}
		return nil
	},
	"Last GRN Date":                 func(r *catRow) interface{} { g, _ := invLastMoves(r); return g },
	"Last Sale Date":                func(r *catRow) interface{} { _, s := invLastMoves(r); return s },
	"Unique Design Count":           func(r *catRow) interface{} { return firstNonEmpty(catStr(r.item()["family"]), r.sku()) },
	"Stock Value at Purchase Price": func(r *catRow) interface{} { return r.hn("on_hand") * catItemCost(r) },
	"Stock Value at Landed Cost":    func(r *catRow) interface{} { return r.hn("on_hand") * catItemCost(r) },
	"Stock Value at Selling Price": func(r *catRow) interface{} {
		return r.hn("on_hand") * firstPositive(catNum(r.item()["sale_price"]), invMRP(r))
	},
	"Stock Value at MRP": func(r *catRow) interface{} { return r.hn("on_hand") * invMRP(r) },
	"Stock Value Excl. Tax": func(r *catRow) interface{} {
		return r.hn("on_hand") * invMRP(r) / (1 + catNum(r.item()["gst_rate"])/100)
	},
	"Category Code":          func(r *catRow) interface{} { return r.item()["category"] },
	"Image URL":              func(r *catRow) interface{} { return catItemImage(r) },
	"Transaction HSN Code":   catItemProp("hsn_code"),
	"Configured Attribute 1": func(r *catRow) interface{} { return invAttr(r, 0) },
	"Configured Attribute 2": func(r *catRow) interface{} { return invAttr(r, 1) },
}

// invAttr is the item's n-th variant value, attributes in name order.
func invAttr(r *catRow, n int) interface{} {
	vals, _ := r.item()["variant_option_values"].(map[string]interface{})
	keys := make([]string, 0, len(vals))
	for k := range vals {
		keys = append(keys, k)
	}
	keys = catSorted(keys)
	if n < len(keys) {
		return keys[n] + ": " + catStr(vals[keys[n]])
	}
	return nil
}

var catStock = &catSource{Label: "Stock", Rows: invRows, SKU: "sku", Qty: "on_hand", Cols: invCols}

// --- the stock ledger ------------------------------------------------------

var ledgerCols = map[string]catFn{
	"Movement Date": func(r *catRow) interface{} { return catStamp(r.Created) },
	"Movement Type": func(r *catRow) interface{} {
		q := r.hn("qty")
		dir := "Move"
		if q > 0 {
			dir = "In"
		} else if q < 0 {
			dir = "Out"
		}
		return firstNonEmpty(r.hs("voucher_type"), "Movement") + " (" + dir + ")"
	},
	"Source Document Type": catH("voucher_type"),
	"Source Document No":   func(r *catRow) interface{} { return ledgerVoucherNo(r) },
	"Stock Status": func(r *catRow) interface{} {
		return firstNonEmpty(r.hs("to_status"), r.hs("from_status"), "Available")
	},
	"Created By":    func(r *catRow) interface{} { return firstNonEmpty(r.hs("user_id"), r.CreatedBy) },
	"From Location": func(r *catRow) interface{} { return firstNonEmpty(r.hs("from_location_id"), r.hs("warehouse_id")) },
	"To Location":   func(r *catRow) interface{} { return firstNonEmpty(r.hs("to_location_id"), r.hs("warehouse_id")) },
	"Location Code": catStoreCode,
	"Location Name": catStoreName,
}

// ledgerVoucherNo is the printed number of the document behind a movement.
func ledgerVoucherNo(r *catRow) interface{} {
	id, vt := r.hs("voucher_id"), r.hs("voucher_type")
	doctype := map[string]string{"POSInvoice": "POSCart", "GRN": "GRN", "TransferOrder": "TransferOrder", "PurchaseReturn": "PurchaseReturn", "SalesOrder": "SalesOrder", "CycleCount": "CycleCountLine"}[vt]
	if doctype == "" || id == "" {
		return id
	}
	if d := r.master(doctype, id); d != nil {
		for _, f := range []string{"bill_number", "transfer_number", "code", "po_number"} {
			if v := catStr(d[f]); v != "" {
				return v
			}
		}
	}
	return id
}

var catLedger = &catSource{Label: "Stock movement", Doctype: "StockLedgerEntry", StoreField: "warehouse_id", SKU: "item_id", Qty: "qty", Cols: ledgerCols}

// ledgerPeriodRows: opening, in, out and closing per store and SKU for the
// period (From / To; blank From = from the start, blank To = today).
func ledgerPeriodRows(tenantID, schema string, p map[string]string) ([]*catRow, error) {
	from, to := strings.TrimSpace(p["from"]), strings.TrimSpace(p["to"])
	if from == "" {
		from = "1900-01-01"
	}
	if to == "" {
		to = time.Now().Format("2006-01-02")
	}
	q := fmt.Sprintf(`SELECT data->>'warehouse_id', data->>'item_id',
		COALESCE(SUM((data->>'qty')::numeric) FILTER (WHERE created_at < $1::date), 0),
		COALESCE(SUM((data->>'qty')::numeric) FILTER (WHERE created_at >= $1::date AND created_at < $2::date + 1 AND (data->>'qty')::numeric > 0), 0),
		COALESCE(-SUM((data->>'qty')::numeric) FILTER (WHERE created_at >= $1::date AND created_at < $2::date + 1 AND (data->>'qty')::numeric < 0), 0),
		COALESCE(SUM((data->>'qty')::numeric) FILTER (WHERE created_at < $2::date + 1), 0)
		FROM %s.documents WHERE doctype = 'StockLedgerEntry' AND deleted_at IS NULL AND data->>'qty' ~ '^-?[0-9.]+$'`, schema)
	args := []interface{}{from, to}
	if v := strings.TrimSpace(p["location"]); v != "" {
		args = append(args, pqStringArray(strings.Split(v, ",")))
		q += " AND data->>'warehouse_id' = ANY($3)"
	}
	rows, err := dbQuery(q+" GROUP BY 1, 2 ORDER BY 1, 2 LIMIT 50000", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*catRow
	for rows.Next() {
		var store, sku string
		var open, in, outQty, closing float64
		if rows.Scan(&store, &sku, &open, &in, &outQty, &closing) != nil {
			continue
		}
		if open == 0 && in == 0 && outQty == 0 && closing == 0 {
			continue
		}
		out = append(out, &catRow{ID: store + "/" + sku, H: map[string]interface{}{"location": store, "sku": sku,
			"opening": open, "inward": in, "outward": outQty, "on_hand": closing}})
	}
	return out, rows.Err()
}

var catLedgerPeriod = &catSource{Label: "Stock", Rows: ledgerPeriodRows, SKU: "sku", Qty: "on_hand",
	Cols: mergeCols(invCols, map[string]catFn{
		"Opening Stock Qty": catHN("opening"),
		"Inward Qty":        catHN("inward"),
		"Outward Qty":       catHN("outward"),
		"From Date":         func(r *catRow) interface{} { return firstNonEmpty(r.p["from"], "") },
		"To Date":           func(r *catRow) interface{} { return firstNonEmpty(r.p["to"], time.Now().Format("2006-01-02")) },
	})}

// --- barcodes, counts, labels, MRP changes ---------------------------------

func barcodeGRN(r *catRow) map[string]interface{} { return r.master("GRN", r.hs("grn")) }

// grnLine is the received line of the barcode's item on its GRN.
func grnLine(grn map[string]interface{}, sku string) map[string]interface{} {
	for _, l := range catLineList(grn["received_items"]) {
		if catStr(l["sku"]) == sku {
			return l
		}
	}
	return nil
}

var barcodeCols = map[string]catFn{
	"Rack":    func(r *catRow) interface{} { return invCols["Rack"](r) },
	"Shelf":   func(r *catRow) interface{} { return invCols["Shelf"](r) },
	"Box":     func(r *catRow) interface{} { return invCols["Box"](r) },
	"Barcode": catH("code"),
	"PO No": func(r *catRow) interface{} {
		if g := barcodeGRN(r); g != nil {
			if po := r.master("PurchaseOrder", catStr(g["po_id"])); po != nil {
				return firstNonEmpty(catStr(po["po_number"]), catStr(g["po_id"]))
			}
			return g["po_id"]
		}
		return nil
	},
	"GRN No":        func(r *catRow) interface{} { return firstNonEmpty(catStr(barcodeGRN(r)["code"]), r.hs("grn")) },
	"GRN Date":      catHD("received_on"),
	"Document Date": catHD("received_on"),
	"Vendor Name": func(r *catRow) interface{} {
		if g := barcodeGRN(r); g != nil {
			return r.nameOf("Vendor", catStr(g["vendor"]))
		}
		return nil
	},
	"Vendor Code": func(r *catRow) interface{} { return barcodeGRN(r)["vendor"] },
	"Purchase Price": func(r *catRow) interface{} {
		if l := grnLine(barcodeGRN(r), r.sku()); l != nil {
			return firstPositive(catNum(l["invoice_rate"]), catNum(l["po_rate"]))
		}
		return catItemCost(r)
	},
	"Landed Cost": func(r *catRow) interface{} { return catItemCost(r) },
	"Cost Value":  func(r *catRow) interface{} { return catItemCost(r) },
	"MRP Value":   func(r *catRow) interface{} { return invMRP(r) },
	"Margin Amount": func(r *catRow) interface{} {
		mrp := invMRP(r) / (1 + catNum(r.item()["gst_rate"])/100)
		return mrp - catItemCost(r)
	},
	"Margin %": func(r *catRow) interface{} {
		mrp := invMRP(r) / (1 + catNum(r.item()["gst_rate"])/100)
		return catPct(mrp-catItemCost(r), mrp)
	},
	"Stock Status": func(r *catRow) interface{} { return r.Status },
	"Qty":          catConst(1.0),
}

var catBarcodes = &catSource{Label: "Barcode", Doctype: "ItemBarcode", SKU: "item", DateField: "received_on",
	StoreOf: func(r *catRow) string { return catStr(barcodeGRN(r)["location"]) },
	Cols:    barcodeCols}

// Count sessions and their lines.
func countTotals(r *catRow) (system, counted, diff float64) {
	totals, ok := r.lk.extra["counttotals"].(map[string][3]float64)
	if !ok {
		totals = map[string][3]float64{}
		rows, err := dbQuery(fmt.Sprintf(`SELECT COALESCE(data->>'physical_inventory', data->>'count_session'),
			SUM(COALESCE((data->>'system_qty')::numeric, 0)), SUM(COALESCE((data->>'counted_qty')::numeric, 0))
			FROM %s.documents WHERE doctype = 'CycleCountLine' AND deleted_at IS NULL GROUP BY 1`, r.lk.schema))
		if err == nil {
			for rows.Next() {
				var id string
				var s, c float64
				if rows.Scan(&id, &s, &c) == nil {
					totals[id] = [3]float64{s, c, c - s}
				}
			}
			rows.Close()
		}
		r.lk.extra["counttotals"] = totals
	}
	t := totals[r.ID]
	return t[0], t[1], t[2]
}

var catCounts = &catSource{Label: "Physical count", Doctype: "PhysicalInventory", DateField: "started_at", Number: "code",
	Statuses: []string{"Open", "Counting", "Review", "Closed", "Posted"},
	Cols: map[string]catFn{
		"Count Reference": func(r *catRow) interface{} { return r.number() },
		"System Qty":      func(r *catRow) interface{} { s, _, _ := countTotals(r); return s },
		"Physical Qty":    func(r *catRow) interface{} { _, c, _ := countTotals(r); return c },
		"Difference Qty":  func(r *catRow) interface{} { _, _, d := countTotals(r); return d },
		"Approved By": func(r *catRow) interface{} {
			return firstNonEmpty(r.hs("approved_by"), r.hs("closed_by"), r.hs("posted_by"))
		},
	}}

var catCycleLines = &catSource{Label: "Count line", Doctype: "CycleCountLine", SKU: "sku", Qty: "counted_qty",
	Cols: map[string]catFn{
		"Count Reference": func(r *catRow) interface{} {
			if pi := r.master("PhysicalInventory", r.hs("physical_inventory")); pi != nil {
				return firstNonEmpty(catStr(pi["code"]), r.hs("physical_inventory"))
			}
			return firstNonEmpty(r.hs("physical_inventory"), r.hs("count_session"))
		},
		"System Qty":      catHN("system_qty"),
		"Physical Qty":    catHN("counted_qty"),
		"Difference Qty":  func(r *catRow) interface{} { return r.hn("counted_qty") - r.hn("system_qty") },
		"Reason":          catH("variance_reason_code"),
		"Approval Status": func(r *catRow) interface{} { return r.Status },
	}}

// Label prints and reprints (sticker_print_log).
func stickerRows(tenantID, schema string, p map[string]string) ([]*catRow, error) {
	q := fmt.Sprintf(`SELECT id::text, COALESCE(sku, ''), COALESCE(barcode, ''), COALESCE(printed_by, ''), copies, COALESCE(reprint_reason, ''),
		printed_at, COALESCE(source_doctype, ''), COALESCE(source_doc_id, '') FROM %s.sticker_print_log WHERE TRUE`, schema)
	var args []interface{}
	if v := p["from"]; v != "" {
		args = append(args, v)
		q += fmt.Sprintf(" AND printed_at >= $%d::date", len(args))
	}
	if v := p["to"]; v != "" {
		args = append(args, v)
		q += fmt.Sprintf(" AND printed_at < $%d::date + 1", len(args))
	}
	rows, err := dbQuery(q+" ORDER BY printed_at DESC LIMIT 20000", args...)
	if err != nil {
		return nil, nil // no label printing on this client yet
	}
	defer rows.Close()
	var out []*catRow
	for rows.Next() {
		var id, sku, barcode, by, reason, srcType, srcID string
		var copies int
		var at time.Time
		if rows.Scan(&id, &sku, &barcode, &by, &copies, &reason, &at, &srcType, &srcID) != nil {
			continue
		}
		out = append(out, &catRow{ID: id, CreatedBy: by, Created: at, H: map[string]interface{}{"sku": sku, "barcode": barcode,
			"reason": firstNonEmpty(reason, fmt.Sprintf("Printed %d from %s %s", copies, srcType, srcID)), "source_doctype": srcType, "source_id": srcID}})
	}
	return out, rows.Err()
}

var catStickers = &catSource{Label: "Label print", Rows: stickerRows, SKU: "sku",
	StoreOf: func(r *catRow) string {
		if d := r.master(r.hs("source_doctype"), r.hs("source_id")); d != nil {
			return firstNonEmpty(catStr(d["location"]), catStr(d["location_code"]), catStr(d["from_warehouse"]))
		}
		return ""
	},
	Cols: map[string]catFn{
		"Barcode":       func(r *catRow) interface{} { return firstNonEmpty(r.hs("barcode"), catStr(r.item()["barcode"])) },
		"Document Date": func(r *catRow) interface{} { return catDay(r.Created) },
	}}

// --- field changes from the audit trail ------------------------------------

var auditFieldPattern = regexp.MustCompile(`^Field "([^"]+)" (?:modified from "(.*)" to "(.*)"|added with value "(.*)") for Document ID: (.+)$`)

// auditChangeRows: one row per recorded field change of the doctypes (and,
// when fields are given, only those fields) - the documents trigger's log.
func auditChangeRows(doctypes []string, fields []string) func(tenantID, schema string, p map[string]string) ([]*catRow, error) {
	return func(tenantID, schema string, p map[string]string) ([]*catRow, error) {
		actions := make([]string, len(doctypes))
		for i, d := range doctypes {
			actions[i] = "UPDATE_" + d
		}
		q := fmt.Sprintf(`SELECT id::text, action, COALESCE(user_id, ''), details, created_at FROM %s.audit_logs WHERE action = ANY($1)`, schema)
		args := []interface{}{pqStringArray(actions)}
		if len(doctypes) == 0 { // every document type
			q = fmt.Sprintf(`SELECT id::text, action, COALESCE(user_id, ''), details, created_at FROM %s.audit_logs WHERE action LIKE $1`, schema)
			args = []interface{}{"UPDATE\\_%"}
		}
		if len(fields) > 0 {
			conds := make([]string, len(fields))
			for i, f := range fields {
				args = append(args, `Field "`+f+`" %`)
				conds[i] = fmt.Sprintf("details LIKE $%d", len(args))
			}
			q += " AND (" + strings.Join(conds, " OR ") + ")"
		}
		if v := p["from"]; v != "" {
			args = append(args, v)
			q += fmt.Sprintf(" AND created_at >= $%d::date", len(args))
		}
		if v := p["to"]; v != "" {
			args = append(args, v)
			q += fmt.Sprintf(" AND created_at < $%d::date + 1", len(args))
		}
		rows, err := dbQuery(q+" ORDER BY created_at DESC LIMIT 20000", args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []*catRow
		for rows.Next() {
			var id, action, user, details string
			var at time.Time
			if rows.Scan(&id, &action, &user, &details, &at) != nil {
				continue
			}
			m := auditFieldPattern.FindStringSubmatch(details)
			if m == nil {
				continue
			}
			old, newVal := m[2], m[3]
			if m[4] != "" || (m[2] == "" && m[3] == "") {
				old, newVal = "", m[4]
			}
			out = append(out, &catRow{ID: id, CreatedBy: user, Created: at, H: map[string]interface{}{
				"doctype": strings.TrimPrefix(action, "UPDATE_"), "doc_id": m[5], "field": m[1], "old": old, "new": newVal}})
		}
		return out, rows.Err()
	}
}

var catMRPChanges = &catSource{Label: "MRP change", Rows: auditChangeRows([]string{"Item", "ProductFamily"}, []string{"mrp"}), StoreField: "-",
	Cols: map[string]catFn{
		"Document Date": func(r *catRow) interface{} { return catDay(r.Created) },
		"Document No":   func(r *catRow) interface{} { return r.ID },
		"Design No": func(r *catRow) interface{} {
			if r.hs("doctype") == "ProductFamily" {
				return r.hs("doc_id")
			}
			if i := r.master("Item", r.hs("doc_id")); i != nil {
				return i["family"]
			}
			return nil
		},
		"Combination ID": func(r *catRow) interface{} {
			if r.hs("doctype") == "Item" {
				return r.hs("doc_id")
			}
			return nil
		},
		"Barcode": func(r *catRow) interface{} {
			if i := r.master("Item", r.hs("doc_id")); i != nil && r.hs("doctype") == "Item" {
				return i["barcode"]
			}
			return nil
		},
		"MRP":       func(r *catRow) interface{} { return catNum(r.hv("old")) },
		"New Value": func(r *catRow) interface{} { return catNum(r.hv("new")) },
		"Reason":    catConst("MRP edited on the item / design record"),
		"Status":    catConst("Applied"),
	}}

// Negative stock: available below zero, with the last movement that took it there.
func invLastOut(r *catRow) string {
	m, ok := r.lk.extra["lastout"].(map[string]string)
	if !ok {
		m = map[string]string{}
		rows, err := dbQuery(fmt.Sprintf(`SELECT DISTINCT ON (data->>'warehouse_id', data->>'item_id') data->>'warehouse_id', data->>'item_id',
			COALESCE(data->>'voucher_type', '') || ' ' || COALESCE(data->>'voucher_id', '')
			FROM %s.documents WHERE doctype = 'StockLedgerEntry' AND deleted_at IS NULL AND data->>'qty' LIKE '-%%'
			ORDER BY data->>'warehouse_id', data->>'item_id', created_at DESC`, r.lk.schema))
		if err == nil {
			for rows.Next() {
				var store, sku, v string
				if rows.Scan(&store, &sku, &v) == nil {
					m[store+"/"+sku] = strings.TrimSpace(v)
				}
			}
			rows.Close()
		}
		r.lk.extra["lastout"] = m
	}
	return m[r.storeID()+"/"+r.sku()]
}

var _ = catWire("cat-current-stock-summary", &catReport{Src: catStock, NoDates: true,
	Group: []string{"As Of Date", "Store Code", "Store Name", "Location Code", "Location Name", "Category Name", "SKU"}})
var _ = catWire("cat-current-stock-itemwise-detail", &catReport{Src: catStock, NoDates: true})
var _ = catWire("cat-current-stock-barcode-detail", &catReport{Src: catBarcodes, NoDates: true,
	Keep: func(r *catRow) bool { return r.Status == "Active" }})
var _ = catWire("cat-stock-details-period-wise", &catReport{Src: catLedgerPeriod})
var _ = catWire("cat-stock-ledger", &catReport{Src: catLedger})
var _ = catWire("cat-stock-ledger-category-summary", &catReport{Src: catLedgerPeriod,
	Group: []string{"From Date", "To Date", "Category Code", "Category Name", "Store Code", "Store Name", "Location Code", "Location Name"}})
var _ = catWire("cat-stock-aging", &catReport{Src: catStock, NoDates: true, Keep: func(r *catRow) bool { return r.hn("on_hand") > 0 }})
var _ = catWire("cat-stock-valuation-summary", &catReport{Src: catStock, NoDates: true,
	Group: []string{"As Of Date", "Store Code", "Store Name", "Location Name", "Category Name"}})
var _ = catWire("cat-barcode-wise-cost-and-mrp-report", &catReport{Src: catBarcodes, NoDates: true})
var _ = catWire("cat-stock-movement-history", &catReport{Src: catLedger})
var _ = catWire("cat-inventory-by-location-summary", &catReport{Src: catStock, NoDates: true,
	Group: []string{"As Of Date", "Store Code", "Store Name", "Location Code", "Location Name"}, Agg: map[string]string{"Unique Design Count": "countd"}})
var _ = catWire("cat-inventory-matrix-summary", &catReport{Src: catStock, NoDates: true,
	Group: []string{"As Of Date", "Store Code", "Store Name", "Category Name", "Design No"}, Agg: map[string]string{"Unique Design Count": "countd"}})
var _ = catWire("cat-current-stock-with-image", &catReport{Src: catStock, NoDates: true, Keep: func(r *catRow) bool { return r.hn("available") > 0 }})
var _ = catWire("cat-physical-stock-count-register", &catReport{Src: catCounts})
var _ = catWire("cat-physical-stock-variance-report", &catReport{Src: catCycleLines})
var _ = catWire("cat-negative-stock-exception", &catReport{Src: catStock, NoDates: true,
	Keep: func(r *catRow) bool { return r.hn("on_hand") < 0 || r.hn("available") < 0 },
	Cols: map[string]catFn{
		"Reason":             catConst("Sold or moved out beyond the stock on hand"),
		"Source Document No": func(r *catRow) interface{} { return invLastOut(r) },
	}})
var _ = catWire("cat-mrp-change-register", &catReport{Src: catMRPChanges})
var _ = catWire("cat-barcode-reprint-label-history", &catReport{Src: catStickers})
var _ = catWire("cat-unique-design-inventory-summary", &catReport{Src: catStock, NoDates: true,
	Group: []string{"As Of Date", "Store Code", "Store Name", "Category Name"}, Agg: map[string]string{"Unique Design Count": "countd"}})
