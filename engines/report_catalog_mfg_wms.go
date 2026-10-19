package engines

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// Catalog reports - Manufacturing and WMS (Stage 75.3.7, Wave C).
//
// Manufacturing: BOM (components: sku, qty per unit, scrap %), Routing
// (operations on work centers), ProductionOrder (quantity, completed_qty,
// rework_qty, actual_cost), QualityInspection, SubcontractOrder and the
// replenishment SupplyProposals. Components are backflushed at standard when
// an order completes, so issue = BOM x completed and usage variance shows
// only scrap allowance - the ERP does not record a separate actual issue.
// WMS: ASN, Appointment, YardCheckIn, DockDoor, WarehouseTask (putaway, pick,
// replenishment...), Wave, bin_stock / bin_stock_lpn / bin_allocation,
// TaskCompletionLog (who did a task, when), CycleCountLine, LPN,
// ShippingPackage, LoadingTask and the stock ledger's bin moves.

// --- manufacturing ----------------------------------------------------------

func bomComponents(m map[string]interface{}) []map[string]interface{} {
	return catLineList(m["components"])
}

var bomCols = map[string]catFn{
	"BOM Code":       func(r *catRow) interface{} { return firstNonEmpty(r.hs("code"), r.ID) },
	"Parent Item":    func(r *catRow) interface{} { return r.nameOf("Item", r.hs("parent_item")) },
	"Component Item": func(r *catRow) interface{} { return r.nameOf("Item", r.ls("sku")) },
	"Component Qty":  catLN("qty"),
	"Purchase Price": func(r *catRow) interface{} { return catItemCost(r) },
	"Standard Cost":  func(r *catRow) interface{} { return r.hn("standard_cost") },
	"Cost Value": func(r *catRow) interface{} {
		return catItemCost(r) * r.ln("qty") * (1 + r.ln("scrap_percent")/100)
	},
}

var catBOMLines = &catSource{Label: "BOM", Doctype: "BOM", Lines: "components", StoreField: "-", SKU: "sku", Qty: "qty", Cols: bomCols}

func poBOM(r *catRow) map[string]interface{} {
	if snap, ok := r.H["bom_snapshot"].(map[string]interface{}); ok && len(bomComponents(snap)) > 0 {
		return snap
	}
	return r.master("BOM", r.hs("bom_id"))
}

func poWorkCenter(r *catRow) interface{} {
	ops := catLineList(r.master("Routing", r.hs("routing_id"))["operations"])
	if len(ops) == 0 {
		return nil
	}
	return r.nameOf("WorkCenter", catStr(ops[0]["work_center_id"]))
}

func poOpenProd(r *catRow) bool {
	switch r.Status {
	case "Completed", "Closed", "Cancelled":
		return false
	}
	return true
}

var prodCols = map[string]catFn{
	"Production Order No": func(r *catRow) interface{} { return firstNonEmpty(r.hs("code"), r.ID) },
	"Document Date":       func(r *catRow) interface{} { return catDay(r.Created) },
	"Parent Item":         func(r *catRow) interface{} { return r.nameOf("Item", catStr(poBOM(r)["parent_item"])) },
	"BOM Code":            catH("bom_id"),
	"Planned Qty":         catHN("quantity"),
	"Completed Qty":       catHN("completed_qty"),
	"WIP Qty": func(r *catRow) interface{} {
		if !poOpenProd(r) {
			return 0.0
		}
		return mathMax0(r.hn("quantity") - r.hn("completed_qty"))
	},
	"Work Center":  poWorkCenter,
	"Routing":      catH("routing_id"),
	"Actual Cost":  catHN("actual_cost"),
	"Scrap Qty":    catHN("rework_qty"),
	"Yield %":      func(r *catRow) interface{} { return catPct(r.hn("completed_qty"), r.hn("quantity")) },
	"Promise Date": catHD("due_date"),
	"To Location":  catStoreName,
	"QC Result":    func(r *catRow) interface{} { return prodQC(r)["result"] },
	"Reason":       func(r *catRow) interface{} { return firstNonEmpty(catStr(prodQC(r)["remarks"]), r.hs("notes")) },
	"Standard Cost": func(r *catRow) interface{} {
		return catNum(poBOM(r)["standard_cost"]) * r.hn("completed_qty")
	},
	"Cost Variance": func(r *catRow) interface{} {
		return r.hn("actual_cost") - catNum(poBOM(r)["standard_cost"])*r.hn("completed_qty")
	},
	"Margin %": func(r *catRow) interface{} {
		parent := r.master("Item", catStr(poBOM(r)["parent_item"]))
		price := catNum(parent["mrp"]) / (1 + catNum(parent["gst_rate"])/100)
		if r.hn("completed_qty") == 0 || price == 0 {
			return nil
		}
		unit := r.hn("actual_cost") / r.hn("completed_qty")
		return catPct(price-unit, price)
	},
}

// prodQC is the order's latest quality inspection.
func prodQC(r *catRow) map[string]interface{} {
	m, ok := r.lk.extra["prodqc"].(map[string]map[string]interface{})
	if !ok {
		m = map[string]map[string]interface{}{}
		rows, err := dbQuery(fmt.Sprintf(`SELECT data->>'production_order_id', COALESCE(data->>'result', ''), COALESCE(data->>'remarks', '')
			FROM %s.documents WHERE doctype = 'QualityInspection' AND deleted_at IS NULL AND COALESCE(data->>'production_order_id', '') <> '' ORDER BY created_at`, r.lk.schema))
		if err == nil {
			for rows.Next() {
				var po, res, rem string
				if rows.Scan(&po, &res, &rem) == nil {
					m[po] = map[string]interface{}{"result": res, "remarks": rem}
				}
			}
			rows.Close()
		}
		r.lk.extra["prodqc"] = m
	}
	return m[r.ID]
}

var catProdOrders = &catSource{Label: "Production order", Doctype: "ProductionOrder",
	Statuses: []string{"Draft", "Released", "In Progress", "Completed", "Closed", "Cancelled"}, Cols: prodCols}

// prodComponentRows: one row per production order and BOM component, issued
// at standard against what was completed.
func prodComponentRows(tenantID, schema string, p map[string]string) ([]*catRow, error) {
	orders, err := catProdOrders.docRows(schema, p, false)
	if err != nil {
		return nil, err
	}
	lk := newCatLookups(schema)
	for pass := 0; pass < 2; pass++ { // the BOMs, once
		for _, o := range orders {
			o.lk, o.src = lk, catProdOrders
			poBOM(o)
		}
		lk.load()
	}
	var out []*catRow
	for _, o := range orders {
		for i, c := range bomComponents(poBOM(o)) {
			r := *o
			r.N = i + 1
			r.L = map[string]interface{}{"sku": c["sku"], "per_unit": catNum(c["qty"]), "scrap_percent": catNum(c["scrap_percent"])}
			out = append(out, &r)
		}
	}
	return out, nil
}

var prodComponentCols = mergeCols(prodCols, map[string]catFn{
	"Component Item": func(r *catRow) interface{} { return r.nameOf("Item", r.ls("sku")) },
	"Component Qty": func(r *catRow) interface{} {
		return r.ln("per_unit") * r.hn("quantity") * (1 + r.ln("scrap_percent")/100)
	},
	"Qty": func(r *catRow) interface{} {
		return r.ln("per_unit") * r.hn("completed_qty") * (1 + r.ln("scrap_percent")/100)
	},
	"Variance Qty": func(r *catRow) interface{} {
		return r.ln("per_unit") * r.hn("completed_qty") * r.ln("scrap_percent") / 100
	},
	"From Location": catStoreName,
	"Standard Cost": func(r *catRow) interface{} { return r.ln("per_unit") * r.hn("completed_qty") * catItemCost(r) },
	"Actual Cost": func(r *catRow) interface{} {
		return r.ln("per_unit") * r.hn("completed_qty") * (1 + r.ln("scrap_percent")/100) * catItemCost(r)
	},
	"Cost Variance": func(r *catRow) interface{} {
		return r.ln("per_unit") * r.hn("completed_qty") * r.ln("scrap_percent") / 100 * catItemCost(r)
	},
	"Status": func(r *catRow) interface{} { return r.Status },
})

var catProdComponents = &catSource{Label: "Production issue", Rows: prodComponentRows, SKU: "sku", Cols: prodComponentCols}

var catQI = &catSource{Label: "Quality inspection", Doctype: "QualityInspection", SKU: "item",
	Statuses: []string{"Draft", "Released", "Rejected", "Open", "Closed"},
	Cols: map[string]catFn{
		"Document Date":       func(r *catRow) interface{} { return catDay(r.Created) },
		"Production Order No": func(r *catRow) interface{} { return firstNonEmpty(r.hs("production_order_id"), r.hs("source_id")) },
		"Parent Item":         func(r *catRow) interface{} { return r.nameOf("Item", r.hs("item")) },
		"Work Center": func(r *catRow) interface{} {
			o := r.master("ProductionOrder", r.hs("production_order_id"))
			ops := catLineList(r.master("Routing", catStr(o["routing_id"]))["operations"])
			if len(ops) == 0 {
				return nil
			}
			return r.nameOf("WorkCenter", catStr(ops[0]["work_center_id"]))
		},
		"QC Result": func(r *catRow) interface{} { return firstNonEmpty(r.hs("result"), r.hs("disposition")) },
		"Rejected Qty": func(r *catRow) interface{} {
			if strings.EqualFold(r.hs("result"), "Fail") || strings.EqualFold(r.hs("result"), "Rejected") {
				return r.hn("qty_inspected")
			}
			return 0.0
		},
		"Qty":       catHN("qty_inspected"),
		"Reason":    func(r *catRow) interface{} { return firstNonEmpty(r.hs("disposition_note"), r.hs("remarks")) },
		"User Name": func(r *catRow) interface{} { return firstNonEmpty(r.hs("inspected_by"), r.CreatedBy) },
	}}

var catWorkCenterLoad = &catSource{Label: "Production order", Doctype: "ProductionOrder", Cols: mergeCols(prodCols, map[string]catFn{
	"Work Center": func(r *catRow) interface{} { return firstNonEmpty(catStr(poWorkCenter(r)), "(no routing)") },
})}

var catProposalsMRP = &catSource{Label: "Supply proposal", Doctype: "SupplyProposal", SKU: "sku",
	Statuses: []string{"Proposed", "Approved", "Rejected", "Converted"},
	Cols: map[string]catFn{
		"Component Item":     func(r *catRow) interface{} { return firstNonEmpty(r.hs("item_name"), r.nameOf("Item", r.hs("sku"))) },
		"Available Qty":      catHN("available"),
		"Reserved Qty":       catNotHeld,
		"MRP Suggestion Qty": func(r *catRow) interface{} { return firstPositive(r.hn("approved_qty"), r.hn("suggested_qty")) },
		"Promise Date": func(r *catRow) interface{} {
			if v := r.hn("daily_velocity"); v > 0 {
				days := math.Floor(r.hn("available") / v)
				return time.Now().AddDate(0, 0, int(days)).Format("2006-01-02") // when stock runs out at today's rate
			}
			return nil
		},
		"Status": func(r *catRow) interface{} { return r.Status + " - " + firstNonEmpty(r.hs("action"), r.hs("reason")) },
	}}

var catSubcontract = &catSource{Label: "Job work", Doctype: "SubcontractOrder", Vendor: "vendor_id", SKU: "sent_item_id",
	Cols: map[string]catFn{
		"Document Date":        catHD("sent_date"),
		"Job Work No":          func(r *catRow) interface{} { return firstNonEmpty(r.hs("code"), r.ID) },
		"Component Item":       func(r *catRow) interface{} { return r.nameOf("Item", r.hs("sent_item_id")) },
		"Qty":                  catHN("sent_qty"),
		"Expected Return Date": catHD("received_date"),
		"Work Result": func(r *catRow) interface{} {
			if exp := r.hn("expected_received_qty"); exp > 0 {
				return fmt.Sprintf("%v of %v back", r.hn("actual_received_qty"), exp)
			}
			return nil
		},
	}}

// Batch / lot movements behind production (the stock ledger's lots).
var catLotLedger = &catSource{Label: "Lot movement", Doctype: "StockLedgerEntry", StoreField: "warehouse_id", SKU: "item_id", Qty: "qty",
	Where: "COALESCE(data->>'batch_no', '') <> '' OR COALESCE(data->>'serial_no', '') <> ''",
	Cols: mergeCols(ledgerCols, map[string]catFn{
		"Document Date":       func(r *catRow) interface{} { return catDay(r.Created) },
		"Production Order No": func(r *catRow) interface{} { return ledgerVoucherNo(r) },
		"Parent Item":         func(r *catRow) interface{} { return r.nameOf("Item", r.hs("item_id")) },
		"Component Item":      func(r *catRow) interface{} { return r.hs("batch_no") },
		"Barcode":             func(r *catRow) interface{} { return firstNonEmpty(r.hs("serial_no"), r.hs("batch_no")) },
		"Status":              func(r *catRow) interface{} { return firstNonEmpty(r.hs("to_status"), r.Status) },
		"LPN":                 catNotHeld,
		"Bin Code":            func(r *catRow) interface{} { return firstNonEmpty(r.hs("to_location_id"), r.hs("from_location_id")) },
		"RF User":             func(r *catRow) interface{} { return firstNonEmpty(r.hs("user_id"), r.CreatedBy) },
	})}

// --- WMS -------------------------------------------------------------------------

// taskLog: who completed a warehouse task and when (TaskCompletionLog).
func taskLog(r *catRow, taskID string) (user string, minutes interface{}) {
	m, ok := r.lk.extra["tasklog"].(map[string][2]interface{})
	if !ok {
		m = map[string][2]interface{}{}
		rows, err := dbQuery(fmt.Sprintf(`SELECT l.data->>'reference_id', COALESCE(l.data->>'user_id', ''),
			EXTRACT(EPOCH FROM (l.created_at - t.created_at)) / 60
			FROM %[1]s.documents l LEFT JOIN %[1]s.documents t ON t.id = l.data->>'reference_id'
			WHERE l.doctype = 'TaskCompletionLog' AND l.deleted_at IS NULL`, r.lk.schema))
		if err == nil {
			for rows.Next() {
				var ref, user string
				var mins *float64
				if rows.Scan(&ref, &user, &mins) == nil {
					var v interface{}
					if mins != nil && *mins >= 0 {
						v = math.Round(*mins*10) / 10
					}
					m[ref] = [2]interface{}{user, v}
				}
			}
			rows.Close()
		}
		r.lk.extra["tasklog"] = m
	}
	v := m[taskID]
	return catStr(v[0]), v[1]
}

func whTaskOpen(r *catRow) bool {
	switch r.Status {
	case "Completed", "Done", "Cancelled", "Closed":
		return false
	}
	return true
}

var whTaskCols = map[string]catFn{
	"Document Date":  func(r *catRow) interface{} { return catDay(r.Created) },
	"Document No":    func(r *catRow) interface{} { return firstNonEmpty(r.hs("code"), r.ID) },
	"Pick List No":   func(r *catRow) interface{} { return firstNonEmpty(r.hs("code"), r.ID) },
	"Wave No":        catH("wave_id"),
	"Barcode":        func(r *catRow) interface{} { return r.item()["barcode"] },
	"LPN":            catH("lpn"),
	"From Location":  catH("from_bin"),
	"To Location":    catH("to_bin"),
	"Bin Code":       func(r *catRow) interface{} { return firstNonEmpty(r.hs("to_bin"), r.hs("from_bin")) },
	"Putaway Status": func(r *catRow) interface{} { return r.Status },
	"Qty":            catHN("qty"),
	"Picked Qty":     func(r *catRow) interface{} { return whPicked(r) },
	"Variance Qty":   func(r *catRow) interface{} { return whPicked(r) - r.hn("qty") },
	"Reason":         catH("reason_code"),
	"RF User":        func(r *catRow) interface{} { u, _ := taskLog(r, r.ID); return u },
	"Task Duration Min": func(r *catRow) interface{} {
		_, m := taskLog(r, r.ID)
		return m
	},
	"SLA Age Hours": func(r *catRow) interface{} {
		if !whTaskOpen(r) {
			return nil
		}
		return math.Round(r.since(r.Created).Hours()*10) / 10
	},
	"Order ID": func(r *catRow) interface{} {
		if t := r.master("FulfillmentTask", r.hs("source_doc_id")); t != nil {
			return firstNonEmpty(catStr(r.master("SalesOrder", catStr(t["order_id"]))["channel_order_id"]), catStr(t["order_id"]))
		}
		return r.hs("source_doc_id")
	},
	"Shipment ID": func(r *catRow) interface{} {
		if r.hs("source_doc_type") == "FulfillmentTask" {
			return r.hs("source_doc_id")
		}
		return nil
	},
	"Status": func(r *catRow) interface{} { return r.Status },
}

// whPicked is what the task's allocations picked (bin_allocation), else its
// own quantity once done.
func whPicked(r *catRow) float64 {
	m, ok := r.lk.extra["picked"].(map[string]float64)
	if !ok {
		m = map[string]float64{}
		rows, err := dbQuery(fmt.Sprintf(`SELECT task_id, COALESCE(SUM(picked_qty), 0) FROM %s.bin_allocation GROUP BY 1`, r.lk.schema))
		if err == nil {
			for rows.Next() {
				var id string
				var q float64
				if rows.Scan(&id, &q) == nil {
					m[id] = q
				}
			}
			rows.Close()
		}
		r.lk.extra["picked"] = m
	}
	if v, ok := m[r.ID]; ok {
		return v
	}
	if !whTaskOpen(r) && r.Status != "Cancelled" {
		return r.hn("qty")
	}
	return 0
}

func whTasks(taskTypes ...string) *catSource {
	quoted := make([]string, len(taskTypes))
	for i, t := range taskTypes {
		quoted[i] = "'" + t + "'"
	}
	return &catSource{Label: "Warehouse task", Doctype: "WarehouseTask", StoreField: "location_code", SKU: "item", Qty: "qty",
		Where: "data->>'task_type' IN (" + strings.Join(quoted, ", ") + ")", Cols: whTaskCols}
}

var catPutaway = whTasks("Putaway", "PUTAWAY", "putaway")
var catPicks = whTasks("Pick", "PICK", "pick", "Picking")
var catReplen = whTasks("Replenishment", "REPLENISHMENT", "replenishment", "Replenish")

var catASNs = &catSource{Label: "ASN", Doctype: "ASN", Number: "asn_number", Vendor: "vendor", DateField: "expected_date",
	Cols: map[string]catFn{
		"Document No": func(r *catRow) interface{} { return r.number() },
		"Appointment No": func(r *catRow) interface{} {
			return asnAppointment(r)["code"]
		},
		"Dock Door": func(r *catRow) interface{} {
			a := asnAppointment(r)
			return firstNonEmpty(catStr(a["actual_dock_door"]), catStr(a["dock_door"]))
		},
		"Qty": catSumLines("expected_items", "qty"),
	}}

func asnAppointment(r *catRow) map[string]interface{} {
	m, ok := r.lk.extra["asnappt"].(map[string]map[string]interface{})
	if !ok {
		m = map[string]map[string]interface{}{}
		rows, err := dbQuery(fmt.Sprintf(`SELECT data->>'asn_id', id, COALESCE(data->>'dock_door', ''), COALESCE(data->>'actual_dock_door', '')
			FROM %s.documents WHERE doctype = 'Appointment' AND deleted_at IS NULL AND COALESCE(data->>'asn_id', '') <> ''`, r.lk.schema))
		if err == nil {
			for rows.Next() {
				var asn, id, door, actual string
				if rows.Scan(&asn, &id, &door, &actual) == nil {
					m[asn] = map[string]interface{}{"code": id, "dock_door": door, "actual_dock_door": actual}
				}
			}
			rows.Close()
		}
		r.lk.extra["asnappt"] = m
	}
	return m[r.ID]
}

// GRN lines as warehouse receiving, with the dock door of the ASN's appointment.
var catReceiving = &catSource{Label: "GRN", Doctype: "GRN", Lines: "received_items", Number: "code", Vendor: "vendor", SKU: "sku", Qty: "qty",
	Cols: mergeCols(grnCols, map[string]catFn{
		"LPN": catNotHeld,
		"Dock Door": func(r *catRow) interface{} {
			a := asnAppointmentByID(r, r.hs("asn_id"))
			return firstNonEmpty(catStr(a["actual_dock_door"]), catStr(a["dock_door"]))
		},
		"QC Result": func(r *catRow) interface{} {
			if r.ln("rejected_qty")+r.ln("damaged_qty") > 0 {
				return "Rejected in part"
			}
			return "Accepted"
		},
		"Status": func(r *catRow) interface{} { return r.Status },
	})}

func asnAppointmentByID(r *catRow, asn string) map[string]interface{} {
	saved := r.ID
	r.ID = asn
	a := asnAppointment(r)
	r.ID = saved
	return a
}

// binStockRows: one row per bin and SKU (bin_stock), with its LPN.
func binStockRows(tenantID, schema string, p map[string]string) ([]*catRow, error) {
	q := fmt.Sprintf(`SELECT b.bin_code, b.sku, b.location_code, COALESCE(b.condition, ''), b.qty, b.updated_at,
		COALESCE((SELECT string_agg(DISTINCT l.lpn_code, ', ') FROM %[1]s.bin_stock_lpn l WHERE l.bin_code = b.bin_code AND l.sku = b.sku), '')
		FROM %[1]s.bin_stock b WHERE b.qty <> 0`, schema)
	var args []interface{}
	if v := strings.TrimSpace(p["location"]); v != "" {
		args = append(args, pqStringArray(strings.Split(v, ",")))
		q += " AND b.location_code = ANY($1)"
	}
	rows, err := dbQuery(q+" ORDER BY b.bin_code, b.sku LIMIT 50000", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*catRow
	for rows.Next() {
		var bin, sku, loc, cond, lpn string
		var qty float64
		var at time.Time
		if rows.Scan(&bin, &sku, &loc, &cond, &qty, &at, &lpn) != nil {
			continue
		}
		out = append(out, &catRow{ID: bin + "/" + sku, Created: at, H: map[string]interface{}{"bin": bin, "sku": sku, "location": loc,
			"condition": cond, "qty": qty, "lpn": lpn, "on_hand": qty, "available": qty}})
	}
	return out, rows.Err()
}

func binCondition(r *catRow) string {
	if c := r.hs("condition"); c != "" {
		return c
	}
	return firstNonEmpty(catStr(r.master("Bin", r.hs("bin"))["bin_status"]), "Good")
}

var binCols = mergeCols(invCols, map[string]catFn{
	"Bin Code":      catH("bin"),
	"Bin Condition": func(r *catRow) interface{} { return binCondition(r) },
	"Available Qty": func(r *catRow) interface{} {
		if strings.EqualFold(binCondition(r), "Good") || strings.EqualFold(binCondition(r), "Available") {
			return r.hn("qty")
		}
		return 0.0
	},
	"Blocked Qty": func(r *catRow) interface{} {
		if strings.EqualFold(binCondition(r), "Good") || strings.EqualFold(binCondition(r), "Available") {
			return 0.0
		}
		return r.hn("qty")
	},
	"LPN": catH("lpn"),
})

var catBins = &catSource{Label: "Bin stock", Rows: binStockRows, SKU: "sku", Qty: "qty", Cols: binCols}

var catBinMaster = &catSource{Label: "Bin", Doctype: "Bin", Statuses: []string{"Active", "Inactive", "Blocked"},
	Cols: map[string]catFn{
		"Bin Code":      func(r *catRow) interface{} { return firstNonEmpty(r.hs("bin_code"), r.ID) },
		"Bin Condition": func(r *catRow) interface{} { return firstNonEmpty(r.hs("bin_status"), "Available") },
		"Available Qty": func(r *catRow) interface{} { return binQty(r) },
		"Blocked Qty": func(r *catRow) interface{} {
			if s := r.hs("bin_status"); s != "" && s != "Available" && s != "Active" {
				return binQty(r)
			}
			return 0.0
		},
		"Remarks": func(r *catRow) interface{} {
			if c := r.hn("capacity"); c > 0 {
				return fmt.Sprintf("%.0f%% of capacity used", binQty(r)/c*100)
			}
			return nil
		},
	}}

func binQty(r *catRow) float64 {
	m, ok := r.lk.extra["binqty"].(map[string]float64)
	if !ok {
		m = map[string]float64{}
		rows, err := dbQuery(fmt.Sprintf(`SELECT bin_code, SUM(qty) FROM %s.bin_stock GROUP BY 1`, r.lk.schema))
		if err == nil {
			for rows.Next() {
				var b string
				var q float64
				if rows.Scan(&b, &q) == nil {
					m[b] = q
				}
			}
			rows.Close()
		}
		r.lk.extra["binqty"] = m
	}
	return m[firstNonEmpty(r.hs("bin_code"), r.ID)]
}

// Bin-to-bin moves inside a warehouse (the stock ledger).
var catBinMoves = &catSource{Label: "Bin move", Doctype: "StockLedgerEntry", StoreField: "warehouse_id", SKU: "item_id", Qty: "qty",
	Where: "COALESCE(data->>'from_location_id', '') <> '' OR COALESCE(data->>'to_location_id', '') <> '' OR data->>'voucher_type' IN ('BinMove', 'Putaway', 'BinReplenishment')",
	Cols: mergeCols(ledgerCols, map[string]catFn{
		"Document No":   func(r *catRow) interface{} { return ledgerVoucherNo(r) },
		"Document Date": func(r *catRow) interface{} { return catDay(r.Created) },
		"Barcode":       func(r *catRow) interface{} { return r.item()["barcode"] },
		"Qty": func(r *catRow) interface{} {
			if q := r.hn("qty"); q != 0 {
				return q
			}
			return nil // a move inside the store changes no total
		},
		"Status": func(r *catRow) interface{} { return firstNonEmpty(r.hs("to_status"), "Moved") },
	})}

// Transfers executed by the warehouse: pick tasks against the transfer.
var catTransferExec = &catSource{Label: "Transfer", Doctype: "TransferOrder", Lines: "items", Number: "transfer_number", StoreField: "from_warehouse",
	FromStore: "from_warehouse", ToStore: "to_warehouse", SKU: "sku", Qty: "qty",
	Cols: mergeCols(transferCols, map[string]catFn{
		"Transfer Date": func(r *catRow) interface{} { return r.date() },
		"From Location": func(r *catRow) interface{} { return r.nameOf("Location", r.hs("from_warehouse")) },
		"To Location":   func(r *catRow) interface{} { return r.nameOf("Location", r.hs("to_warehouse")) },
		"Barcode":       func(r *catRow) interface{} { return r.item()["barcode"] },
		"Transfer Qty":  catLN("qty"),
		"Picked Qty":    func(r *catRow) interface{} { return transferLineQty(r, "dispatched_items") },
		"Packed Qty":    func(r *catRow) interface{} { return transferLineQty(r, "dispatched_items") },
		"Dispatched Qty": func(r *catRow) interface{} {
			return transferLineQty(r, "dispatched_items")
		},
	})}

func transferLineQty(r *catRow, field string) float64 {
	total := 0.0
	for _, l := range catLineList(r.H[field]) {
		if catStr(l["sku"]) == r.sku() {
			total += catNum(l["qty"])
		}
	}
	return total
}

// The warehouse cockpit: what is waiting at each step, per warehouse.
func cockpitRows(tenantID, schema string, p map[string]string) ([]*catRow, error) {
	counts := map[string]map[string]float64{}
	add := func(q, key string) {
		rows, err := dbQuery(fmt.Sprintf(q, schema))
		if err != nil {
			return
		}
		defer rows.Close()
		for rows.Next() {
			var loc string
			var v float64
			if rows.Scan(&loc, &v) == nil && loc != "" {
				if counts[loc] == nil {
					counts[loc] = map[string]float64{}
				}
				counts[loc][key] += v
			}
		}
	}
	add(`SELECT data->>'location', COUNT(*) FROM %s.documents WHERE doctype = 'ASN' AND deleted_at IS NULL AND COALESCE(data->>'received_by_grn', '') = '' AND status NOT IN ('Cancelled', 'Received', 'Closed') GROUP BY 1`, "inbound")
	add(`SELECT data->>'location_code', SUM(COALESCE(NULLIF(data->>'qty', '')::numeric, 0)) FROM %s.documents WHERE doctype = 'WarehouseTask' AND deleted_at IS NULL AND lower(data->>'task_type') = 'putaway' AND status NOT IN ('Completed', 'Done', 'Cancelled') GROUP BY 1`, "putaway")
	add(`SELECT data->>'location_code', SUM(COALESCE(NULLIF(data->>'qty', '')::numeric, 0)) FROM %s.documents WHERE doctype = 'WarehouseTask' AND deleted_at IS NULL AND lower(data->>'task_type') IN ('pick', 'picking') AND status NOT IN ('Completed', 'Done', 'Cancelled') GROUP BY 1`, "pick")
	add(`SELECT data->>'location_code', COUNT(*) FROM %s.documents WHERE doctype = 'FulfillmentTask' AND deleted_at IS NULL AND status IN ('Picked', 'Picking') GROUP BY 1`, "pack")
	add(`SELECT data->>'location_code', COUNT(*) FROM %s.documents WHERE doctype = 'ShippingPackage' AND deleted_at IS NULL AND status NOT IN ('Loaded', 'Dispatched', 'Shipped', 'Delivered', 'Cancelled') GROUP BY 1`, "dispatch")
	add(`SELECT data->>'location_code', COUNT(*) FROM %s.documents WHERE doctype IN ('Hold', 'FloorAssistRequest') AND deleted_at IS NULL AND status NOT IN ('Released', 'Closed', 'Resolved', 'Cancelled') GROUP BY 1`, "exceptions")
	var out []*catRow
	for loc, c := range counts {
		if v := p["location"]; v != "" && !containsString(strings.Split(v, ","), loc) {
			continue
		}
		h := map[string]interface{}{"location": loc}
		for k, v := range c {
			h[k] = v
		}
		out = append(out, &catRow{ID: loc, H: h})
	}
	return out, nil
}

var catCockpit = &catSource{Label: "Warehouse", Rows: cockpitRows,
	Cols: map[string]catFn{
		"Warehouse":            catStoreName,
		"Pending Inbound Qty":  catHN("inbound"),
		"Pending Putaway Qty":  catHN("putaway"),
		"Pending Pick Qty":     catHN("pick"),
		"Pending Pack Qty":     catHN("pack"),
		"Pending Dispatch Qty": catHN("dispatch"),
		"Exception Count":      catHN("exceptions"),
	}}

var catLPNs = &catSource{Label: "LPN", Doctype: "LPN", StoreField: "-",
	Cols: map[string]catFn{
		"Document Date": func(r *catRow) interface{} { return catDay(r.Created) },
		"LPN":           func(r *catRow) interface{} { return firstNonEmpty(r.hs("lpn_code"), r.ID) },
		"Carton No": func(r *catRow) interface{} {
			if strings.Contains(strings.ToLower(r.hs("container_type")), "carton") {
				return firstNonEmpty(r.hs("lpn_code"), r.ID)
			}
			return nil
		},
		"Pallet No": func(r *catRow) interface{} {
			if strings.Contains(strings.ToLower(r.hs("container_type")), "pallet") {
				return firstNonEmpty(r.hs("lpn_code"), r.ID)
			}
			return nil
		},
		"Location Name": func(r *catRow) interface{} { l, _ := lpnStock(r); return l },
		"Qty":           func(r *catRow) interface{} { _, q := lpnStock(r); return q },
	}}

func lpnStock(r *catRow) (string, float64) {
	m, ok := r.lk.extra["lpnstock"].(map[string][2]interface{})
	if !ok {
		m = map[string][2]interface{}{}
		rows, err := dbQuery(fmt.Sprintf(`SELECT lpn_code, string_agg(DISTINCT bin_code, ', '), SUM(qty) FROM %s.bin_stock_lpn GROUP BY 1`, r.lk.schema))
		if err == nil {
			for rows.Next() {
				var lpn, bins string
				var q float64
				if rows.Scan(&lpn, &bins, &q) == nil {
					m[lpn] = [2]interface{}{bins, q}
				}
			}
			rows.Close()
		}
		r.lk.extra["lpnstock"] = m
	}
	v := m[firstNonEmpty(r.hs("lpn_code"), r.ID)]
	return catStr(v[0]), catNum(v[1])
}

var packCols = mergeCols(ecomCols, map[string]catFn{
	"Document Date": func(r *catRow) interface{} { return catDay(r.Created) },
	"Shipment ID":   func(r *catRow) interface{} { return firstNonEmpty(r.hs("code"), r.ID) },
	"Carton No":     func(r *catRow) interface{} { return firstNonEmpty(r.hs("package_type"), r.hs("code")) },
	"LPN":           catNotHeld,
	"Picked Qty":    catSumLines("items", "qty"),
	"Packed Qty":    catSumLines("items", "qty"),
	"Dispatch Qty":  catSumLines("items", "qty"),
	"User Name":     func(r *catRow) interface{} { return r.CreatedBy },
	"Tracking Number": func(r *catRow) interface{} {
		return ecomBooking(r, r.ID, r.hs("order_id"))["tracking"]
	},
	"Shipping Provider": func(r *catRow) interface{} { return ecomBooking(r, r.ID, r.hs("order_id"))["carrier"] },
	"Vehicle No":        func(r *catRow) interface{} { return r.master("LoadingTask", r.hs("loading_task_id"))["trailer_no"] },
	"Dock Door":         func(r *catRow) interface{} { return r.master("LoadingTask", r.hs("loading_task_id"))["dock_door"] },
	"Status":            func(r *catRow) interface{} { return r.Status },
})

var catPackages = &catSource{Label: "Package", Doctype: "ShippingPackage", StoreField: "location_code", Cols: packCols}

var catCycleCounts = &catSource{Label: "Cycle count", Doctype: "CycleCountLine", SKU: "sku", Qty: "counted_qty",
	Cols: mergeCols(catCycleLines.Cols, map[string]catFn{
		"Cycle Count No": func(r *catRow) interface{} {
			return firstNonEmpty(r.hs("count_session"), catStr(r.master("PhysicalInventory", r.hs("physical_inventory"))["code"]), r.hs("code"))
		},
		"Bin Code":  catH("bin"),
		"Barcode":   func(r *catRow) interface{} { return r.item()["barcode"] },
		"User Name": func(r *catRow) interface{} { return firstNonEmpty(r.hs("posted_by"), r.CreatedBy) },
		"Status":    func(r *catRow) interface{} { return r.Status },
		"Achievement %": func(r *catRow) interface{} {
			sys := r.hn("system_qty")
			if sys == 0 {
				return nil
			}
			return math.Round((1-math.Abs(r.hn("counted_qty")-sys)/sys)*1000) / 10
		},
		"Location Name": catStoreName,
	})}

// Labour: completed tasks per user (TaskCompletionLog).
var catTaskLogs = &catSource{Label: "Task", Doctype: "TaskCompletionLog", StoreField: "location_code",
	Cols: map[string]catFn{
		"RF User":       catH("user_id"),
		"Pick List No":  catH("reference_id"),
		"Picked Qty":    catHN("qty"),
		"Document Date": func(r *catRow) interface{} { return catDay(r.Created) },
		"Document Type": catH("task_type"),
		"Qty":           catHN("qty"),
		"Task Duration Min": func(r *catRow) interface{} {
			_, m := taskLog(r, r.hs("reference_id"))
			return m
		},
		"SLA Age Hours": catNotHeld,
		"Achievement %": catNotHeld,
		"Status":        catConst("Done"),
	}}

var catAppointments = &catSource{Label: "Appointment", Doctype: "Appointment", DateField: "appointment_date",
	Statuses: []string{"Scheduled", "Checked In", "At Door", "Completed", "No Show", "Cancelled"},
	StoreOf: func(r *catRow) string {
		return firstNonEmpty(catStr(r.master("DockDoor", r.hs("dock_door"))["location"]), catStr(r.master("ASN", r.hs("asn_id"))["location"]))
	},
	Cols: map[string]catFn{
		"Document Date":  func(r *catRow) interface{} { return r.date() },
		"Appointment No": func(r *catRow) interface{} { return r.ID },
		"Vendor Name": func(r *catRow) interface{} {
			if a := r.master("ASN", r.hs("asn_id")); a != nil {
				return r.nameOf("Vendor", catStr(a["vendor"]))
			}
			return r.hs("carrier")
		},
		"Dock Door":  func(r *catRow) interface{} { return firstNonEmpty(r.hs("actual_dock_door"), r.hs("dock_door")) },
		"Vehicle No": catH("trailer_no"),
		"SLA Due At": func(r *catRow) interface{} {
			return strings.TrimSpace(catDay(r.hv("appointment_date")) + " " + r.hs("end_time"))
		},
		"Qty": func(r *catRow) interface{} {
			return catSumLines("expected_items", "qty")(&catRow{H: r.master("ASN", r.hs("asn_id"))})
		},
	}}

var catYard = &catSource{Label: "Yard", Doctype: "YardCheckIn", StoreOf: func(r *catRow) string {
	return catStr(r.master("DockDoor", r.hs("dock_door"))["location"])
},
	Cols: map[string]catFn{
		"Document Date":  func(r *catRow) interface{} { return catDay(firstNonEmpty(r.hs("checked_in_at"), catStamp(r.Created))) },
		"Vehicle No":     catH("trailer_no"),
		"Appointment No": catH("appointment_id"),
		"Dock Door":      catH("dock_door"),
		"Yard Status":    func(r *catRow) interface{} { return firstNonEmpty(r.hs("yard_location"), r.Status) },
		"SLA Age Hours": func(r *catRow) interface{} {
			if r.hs("checked_out_at") != "" {
				return nil
			}
			return math.Round(r.since(r.Created).Hours()*10) / 10
		},
	}}

var _ = catWire("cat-bom-register", &catReport{Src: catBOMLines})
var _ = catWire("cat-bom-costing-report", &catReport{Src: catBOMLines})
var _ = catWire("cat-production-order-register", &catReport{Src: catProdOrders})
var _ = catWire("cat-work-in-process-report", &catReport{Src: catProdOrders, Keep: poOpenProd})
var _ = catWire("cat-material-issue-consumption-report", &catReport{Src: catProdComponents, Keep: func(r *catRow) bool { return r.hn("completed_qty") > 0 }})
var _ = catWire("cat-material-usage-variance", &catReport{Src: catProdComponents, Keep: func(r *catRow) bool { return r.hn("completed_qty") > 0 }})
var _ = catWire("cat-production-receipt-completion", &catReport{Src: catProdOrders, Keep: func(r *catRow) bool { return r.hn("completed_qty") > 0 }})
var _ = catWire("cat-yield-scrap-analysis", &catReport{Src: catProdOrders, Keep: func(r *catRow) bool { return r.hn("completed_qty") > 0 }})
var _ = catWire("cat-quality-inspection-report", &catReport{Src: catQI})
var _ = catWire("cat-rework-rejection-report", &catReport{Src: catQI,
	Keep: func(r *catRow) bool {
		res := strings.ToLower(r.hs("result") + " " + r.hs("disposition"))
		return strings.Contains(res, "fail") || strings.Contains(res, "reject") || strings.Contains(res, "rework")
	}})
var _ = catWire("cat-production-schedule", &catReport{Src: catProdOrders, Keep: poOpenProd})
var _ = catWire("cat-work-center-capacity-load", &catReport{Src: catWorkCenterLoad,
	Cols:  map[string]catFn{"Achievement %": catPost},
	Group: []string{"From Date", "To Date", "Work Center"},
	Post: func(row map[string]interface{}) {
		row["Achievement %"] = catPct(catNum(row["Completed Qty"]), catNum(row["Planned Qty"]))
	}})
var _ = catWire("cat-mrp-suggestions", &catReport{Src: catProposalsMRP})
var _ = catWire("cat-subcontracting-job-work-report", &catReport{Src: catSubcontract})
var _ = catWire("cat-batch-lot-traceability", &catReport{Src: catLotLedger})
var _ = catWire("cat-standard-vs-actual-cost", &catReport{Src: catProdOrders, Keep: func(r *catRow) bool { return r.hn("completed_qty") > 0 }})

var _ = catWire("cat-inbound-asn-register", &catReport{Src: catASNs})
var _ = catWire("cat-receiving-grn-detail", &catReport{Src: catReceiving})
var _ = catWire("cat-inbound-qc-exception", &catReport{Src: catReceiving, Keep: func(r *catRow) bool { return r.ln("rejected_qty")+r.ln("damaged_qty") > 0 }})
var _ = catWire("cat-putaway-pending-report", &catReport{Src: catPutaway, Keep: whTaskOpen})
var _ = catWire("cat-putaway-completion-report", &catReport{Src: catPutaway, Keep: func(r *catRow) bool { return !whTaskOpen(r) && r.Status != "Cancelled" }})
var _ = catWire("cat-bin-inventory-report", &catReport{Src: catBins, NoDates: true})
var _ = catWire("cat-location-movement-register", &catReport{Src: catBinMoves})
var _ = catWire("cat-wms-stock-transfer-execution-report", &catReport{Src: catTransferExec})
var _ = catWire("cat-warehouse-cockpit-kpi-summary", &catReport{Src: catCockpit, NoDates: true})
var _ = catWire("cat-bin-utilization-condition-report", &catReport{Src: catBinMaster, NoDates: true})
var _ = catWire("cat-lpn-carton-pallet-register", &catReport{Src: catLPNs})
var _ = catWire("cat-bin-replenishment-report", &catReport{Src: catReplen})
var _ = catWire("cat-pick-list-report", &catReport{Src: catPicks})
var _ = catWire("cat-wave-batch-picking-report", &catReport{Src: catPicks, Keep: func(r *catRow) bool { return r.hs("wave_id") != "" }})
var _ = catWire("cat-picking-exception-report", &catReport{Src: catPicks,
	Keep: func(r *catRow) bool {
		return r.hs("reason_code") != "" || (!whTaskOpen(r) && whPicked(r) < r.hn("qty"))
	}})
var _ = catWire("cat-packing-report", &catReport{Src: catPackages})
var _ = catWire("cat-dispatch-shipment-register", &catReport{Src: catPackages,
	Keep: func(r *catRow) bool {
		switch r.Status {
		case "Loaded", "Dispatched", "Shipped", "Delivered", "Manifested":
			return true
		}
		return r.hs("loaded_at") != ""
	}})
var _ = catWire("cat-cycle-count-register", &catReport{Src: catCycleCounts})
var _ = catWire("cat-cycle-count-variance-report", &catReport{Src: catCycleCounts, Keep: func(r *catRow) bool { return r.hn("counted_qty") != r.hn("system_qty") }})
var _ = catWire("cat-bin-stock-aging", &catReport{Src: catBins, NoDates: true})
var _ = catWire("cat-lot-serial-barcode-traceability", &catReport{Src: catLotLedger})
var _ = catWire("cat-rf-mobile-picking-productivity", &catReport{Src: catTaskLogs,
	Group: []string{"From Date", "To Date", "RF User"}, Agg: map[string]string{"Pick List No": "countd", "Task Duration Min": "sum"},
	Post: func(row map[string]interface{}) {
		if mins := catNum(row["Task Duration Min"]); mins > 0 {
			row["Status"] = fmt.Sprintf("%.1f units an hour", catNum(row["Picked Qty"])/(mins/60))
		}
	}})
var _ = catWire("cat-dock-door-utilization", &catReport{Src: catAppointments})
var _ = catWire("cat-appointment-calendar-report", &catReport{Src: catAppointments})
var _ = catWire("cat-yard-trailer-board-report", &catReport{Src: catYard})
var _ = catWire("cat-warehouse-sla-throughput-summary", &catReport{Src: catTaskLogs,
	Group: []string{"Document Date", "Document Type"}, Agg: map[string]string{"Status": "first"}})
var _ = catWire("cat-warehouse-inventory-accuracy-summary", &catReport{Src: catCycleCounts,
	Cols:  map[string]catFn{"Achievement %": catPost},
	Group: []string{"From Date", "To Date", "Location Name", "Bin Code"},
	Post: func(row map[string]interface{}) {
		if sys := catNum(row["System Qty"]); sys != 0 {
			row["Achievement %"] = math.Round((1-math.Abs(catNum(row["Difference Qty"]))/sys)*1000) / 10
		}
	}})
