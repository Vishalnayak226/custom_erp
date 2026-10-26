package engines

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// Catalog reports - OMS, PIM / PIMS and CRM (Stage 75.3.7, Wave C).
//
// OMS: SalesOrder / SalesOrderLine, the fulfilment tasks (a shipment being
// picked and packed - its SLA is the OMS SLA Breach report's 120 minutes),
// ShippingPackage, inventory_reservation, ReturnRequest / RefundRequest.
// PIM: Item, ProductFamily, the attribute definitions, groups, family
// mappings and values, PIMProductProfile (completeness), PIMTask, the media
// and the channel publish queue / log. CRM: Customer, the till bills by
// customer, loyalty_point_ledger, the offers applied on bills, ServiceTicket.
// The ERP holds no leads, opportunities or customer feedback, so those three
// catalog rows wait for a module.

const omsSLAMinutes = 120

// --- OMS ---------------------------------------------------------------------

func orderID(r *catRow, field string) interface{} {
	o := r.master("SalesOrder", r.hs(field))
	return firstNonEmpty(catStr(o["channel_order_id"]), catStr(o["code"]), r.hs(field))
}

var orderHeaderCols = map[string]catFn{
	"Order Date":      func(r *catRow) interface{} { return catDay(r.Created) },
	"Order ID":        func(r *catRow) interface{} { return firstNonEmpty(r.hs("channel_order_id"), r.hs("code"), r.ID) },
	"Channel":         catH("channel"),
	"Customer ID":     catH("customer_id"),
	"Customer Name":   catH("customer_name"),
	"Customer Mobile": catH("customer_phone"),
	"Order Status":    func(r *catRow) interface{} { return firstNonEmpty(r.hs("order_status"), r.Status) },
	"Gross Amount":    catHN("total_amount"),
	"Discount Amount": catNotHeld, // a channel order arrives at its selling price
	"Net Amount":      catHN("total_amount"),
	"Payment Status":  catH("payment_status"),
	"UC Status":       func(r *catRow) interface{} { return ecomUC(r, r.ID) },
	"Integration Status": func(r *catRow) interface{} {
		if ecomUC(r, r.ID) != "" || r.hs("channel_order_id") != "" {
			return "Mapped"
		}
		return "Not from a channel"
	},
	"Error Code":    catNotHeld,
	"Error Message": func(r *catRow) interface{} { return firstNonEmpty(r.hs("hold_reason"), r.hs("cancel_reason")) },
}

var catOrderHeaders = &catSource{Label: "Sales order", Doctype: "SalesOrder", StoreField: "location_code", Cols: orderHeaderCols}

var orderLineCols = map[string]catFn{
	"Order Date": func(r *catRow) interface{} { return catDay(r.master("SalesOrder", r.hs("order_id"))["_created_at"]) },
	"Order ID":   func(r *catRow) interface{} { return orderID(r, "order_id") },
	"Channel":    func(r *catRow) interface{} { return r.master("SalesOrder", r.hs("order_id"))["channel"] },
	"Channel SKU": func(r *catRow) interface{} {
		return ecomChannelSKU(r, catStr(r.master("SalesOrder", r.hs("order_id"))["channel"]))
	},
	"MRP":          func(r *catRow) interface{} { return r.item()["mrp"] },
	"Order Status": func(r *catRow) interface{} { return r.master("SalesOrder", r.hs("order_id"))["order_status"] },
	"Discount Amount": func(r *catRow) interface{} {
		if mrp := catNum(r.item()["mrp"]); mrp > r.hn("unit_price") && r.hn("unit_price") > 0 {
			return (mrp - r.hn("unit_price")) * r.hn("qty")
		}
		return 0.0
	},
	"Net Amount":          func(r *catRow) interface{} { return r.hn("unit_price") * r.hn("qty") },
	"Line Status":         catH("line_status"),
	"Cancellation Reason": func(r *catRow) interface{} { return r.master("SalesOrder", r.hs("order_id"))["cancel_reason"] },
	"User Name": func(r *catRow) interface{} {
		return firstNonEmpty(catStr(r.master("SalesOrder", r.hs("order_id"))["cancelled_by"]), r.CreatedBy)
	},
	"Status": func(r *catRow) interface{} { return firstNonEmpty(r.hs("line_status"), r.Status) },
}

var catOrderLines = &catSource{Label: "Order line", Doctype: "SalesOrderLine", StoreField: "location_code", SKU: "sku", Qty: "qty", Cols: orderLineCols}

// Fulfilment tasks are the shipments being picked and packed.
func taskBooking(r *catRow) map[string]interface{} {
	m, ok := r.lk.extra["taskbookings"].(map[string]map[string]interface{})
	if !ok {
		m = map[string]map[string]interface{}{}
		rows, err := dbQuery(fmt.Sprintf(`SELECT data->>'fulfillment_task_id', COALESCE(NULLIF(data->>'tracking_number', ''), data->>'awb_number', ''), COALESCE(data->>'carrier', '')
			FROM %s.documents WHERE doctype = 'LogisticsBooking' AND deleted_at IS NULL AND COALESCE(data->>'fulfillment_task_id', '') <> ''`, r.lk.schema))
		if err == nil {
			for rows.Next() {
				var task, tr, carrier string
				if rows.Scan(&task, &tr, &carrier) == nil {
					m[task] = map[string]interface{}{"tracking": tr, "carrier": carrier}
				}
			}
			rows.Close()
		}
		r.lk.extra["taskbookings"] = m
	}
	return m[r.ID]
}

func taskOpen(r *catRow) bool {
	switch r.Status {
	case "Pending", "Picking", "Packed", "Allocated", "Released":
		return true
	}
	return false
}

func taskSLADue(r *catRow) time.Time { return r.Created.Add(omsSLAMinutes * time.Minute) }

var taskCols = map[string]catFn{
	"Order Date":         func(r *catRow) interface{} { return catDay(r.master("SalesOrder", r.hs("order_id"))["_created_at"]) },
	"Order ID":           func(r *catRow) interface{} { return orderID(r, "order_id") },
	"Channel":            func(r *catRow) interface{} { return r.master("SalesOrder", r.hs("order_id"))["channel"] },
	"Order Status":       func(r *catRow) interface{} { return r.master("SalesOrder", r.hs("order_id"))["order_status"] },
	"Shipment ID":        func(r *catRow) interface{} { return firstNonEmpty(r.hs("code"), r.ID) },
	"Shipment Sequence":  func(r *catRow) interface{} { return taskSeq(r) },
	"Allocated Location": func(r *catRow) interface{} { return r.nameOf("Location", r.storeID()) },
	"Fulfilment Status":  func(r *catRow) interface{} { return r.Status },
	"Tracking Number":    func(r *catRow) interface{} { return taskBooking(r)["tracking"] },
	"Shipping Provider":  func(r *catRow) interface{} { return taskBooking(r)["carrier"] },
	"Promise Date":       catNotHeld,
	"SLA Due At":         func(r *catRow) interface{} { return catStamp(taskSLADue(r)) },
	"SLA Age Hours": func(r *catRow) interface{} {
		if !taskOpen(r) {
			return nil
		}
		return math.Round(r.since(r.Created).Hours()*10) / 10
	},
	"Status": func(r *catRow) interface{} {
		switch {
		case !taskOpen(r):
			return "Done"
		case r.since(taskSLADue(r)) > 0:
			return "SLA breached"
		}
		return "Within SLA"
	},
}

func taskSeq(r *catRow) interface{} {
	m, ok := r.lk.extra["taskseq"].(map[string]int)
	if !ok {
		m = map[string]int{}
		rows, err := dbQuery(fmt.Sprintf(`SELECT id, ROW_NUMBER() OVER (PARTITION BY data->>'order_id' ORDER BY created_at, id) FROM %s.documents
			WHERE doctype = 'FulfillmentTask' AND deleted_at IS NULL`, r.lk.schema))
		if err == nil {
			for rows.Next() {
				var id string
				var n int
				if rows.Scan(&id, &n) == nil {
					m[id] = n
				}
			}
			rows.Close()
		}
		r.lk.extra["taskseq"] = m
	}
	if n := m[r.ID]; n > 0 {
		return float64(n)
	}
	return nil
}

var catTasks = &catSource{Label: "Shipment", Doctype: "FulfillmentTask", StoreField: "location_code", Cols: taskCols}

// Reservations held for orders.
func reservationRows(tenantID, schema string, p map[string]string) ([]*catRow, error) {
	q := fmt.Sprintf(`SELECT id::text, sku, location_code, quantity, COALESCE(reservation_type, ''), expires_at, created_at, COALESCE(order_id, ''), COALESCE(line_id, '')
		FROM %s.inventory_reservation WHERE TRUE`, schema)
	var args []interface{}
	if v := strings.TrimSpace(p["location"]); v != "" {
		args = append(args, pqStringArray(strings.Split(v, ",")))
		q += " AND location_code = ANY($1)"
	}
	rows, err := dbQuery(q+" ORDER BY created_at DESC LIMIT 20000", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*catRow
	for rows.Next() {
		var id, sku, loc, typ, order, line string
		var qty float64
		var expires *time.Time
		var at time.Time
		if rows.Scan(&id, &sku, &loc, &qty, &typ, &expires, &at, &order, &line) != nil {
			continue
		}
		h := map[string]interface{}{"sku": sku, "location": loc, "qty": qty, "type": typ, "order_id": order, "line_id": line}
		if expires != nil {
			h["expires"] = catStamp(*expires)
		}
		out = append(out, &catRow{ID: id, Created: at, H: h})
	}
	return out, rows.Err()
}

var catReservations = &catSource{Label: "Reservation", Rows: reservationRows, SKU: "sku", Qty: "qty",
	Cols: map[string]catFn{
		"Order ID":           func(r *catRow) interface{} { return orderID(r, "order_id") },
		"Shipment ID":        func(r *catRow) interface{} { return ecomOrderDocs(r)["p:"+r.hs("order_id")] },
		"Allocated Location": func(r *catRow) interface{} { return r.nameOf("Location", r.storeID()) },
		"Available Qty":      func(r *catRow) interface{} { return ecomAvailable(r) },
		"Reserved Qty":       catHN("qty"),
		"Allocation Status":  func(r *catRow) interface{} { return firstNonEmpty(r.hs("type"), "Reserved") },
		"Promise Date":       catH("expires"),
		"Status": func(r *catRow) interface{} {
			if e := r.hs("expires"); e != "" && e < catStamp(time.Now()) {
				return "Expired"
			}
			return "Held"
		},
	}}

// Refunds (RefundRequest) by the return they pay out.
func refundFor(r *catRow, returnID string) map[string]interface{} {
	m, ok := r.lk.extra["refunds"].(map[string]map[string]interface{})
	if !ok {
		m = map[string]map[string]interface{}{}
		rows, err := dbQuery(fmt.Sprintf(`SELECT COALESCE(data->>'return_request_id', ''), status, COALESCE(NULLIF(data->>'net_amount', ''), data->>'amount', '0')
			FROM %s.documents WHERE doctype = 'RefundRequest' AND deleted_at IS NULL`, r.lk.schema))
		if err == nil {
			for rows.Next() {
				var rr, st, amt string
				if rows.Scan(&rr, &st, &amt) == nil && rr != "" {
					m[rr] = map[string]interface{}{"status": st, "amount": catNum(amt)}
				}
			}
			rows.Close()
		}
		r.lk.extra["refunds"] = m
	}
	return m[returnID]
}

var catReturnRTO = &catSource{Label: "Return / RTO", Doctype: "ReturnRequest", StoreField: "return_location", Lines: "items", Number: "code", SKU: "sku", Qty: "qty",
	Cols: map[string]catFn{
		"Document Date": func(r *catRow) interface{} { return r.date() },
		"Order ID": func(r *catRow) interface{} {
			if o := r.master("SalesOrder", r.hs("original_order_id")); o != nil {
				return firstNonEmpty(catStr(o["channel_order_id"]), r.hs("original_order_id"))
			}
			return returnBillNo(r)
		},
		"Shipment ID": func(r *catRow) interface{} { return ecomOrderDocs(r)["p:"+r.hs("original_order_id")] },
		"Tracking Number": func(r *catRow) interface{} {
			b := r.master("LogisticsBooking", r.hs("booking_id"))
			return firstNonEmpty(catStr(b["tracking_number"]), catStr(b["awb_number"]))
		},
		"Return Status": func(r *catRow) interface{} { return r.hs("request_type") + ": " + r.Status },
		"Reason": func(r *catRow) interface{} {
			return firstNonEmpty(r.ls("reason"), r.ls("disposition"), r.hs("rejection_reason"))
		},
		"Refund Status": func(r *catRow) interface{} { return refundFor(r, r.ID)["status"] },
		"Refund Amount": func(r *catRow) interface{} { return refundFor(r, r.ID)["amount"] },
	}}

var catRefunds = &catSource{Label: "Refund", Doctype: "RefundRequest", StoreOf: func(r *catRow) string {
	return catStr(r.master("ReturnRequest", r.hs("return_request_id"))["return_location"])
},
	Cols: map[string]catFn{
		"Document Date": func(r *catRow) interface{} { return r.date() },
		"Order ID": func(r *catRow) interface{} {
			order := firstNonEmpty(r.hs("original_order_id"), catStr(r.master("ReturnRequest", r.hs("return_request_id"))["original_order_id"]))
			if o := r.master("SalesOrder", order); o != nil {
				return firstNonEmpty(catStr(o["channel_order_id"]), order)
			}
			if c := r.master("POSCart", order); c != nil {
				return firstNonEmpty(catStr(c["bill_number"]), order)
			}
			return order
		},
		"Shipment ID": func(r *catRow) interface{} { return ecomOrderDocs(r)["p:"+r.hs("original_order_id")] },
		"Customer Name": func(r *catRow) interface{} {
			order := firstNonEmpty(r.hs("original_order_id"), catStr(r.master("ReturnRequest", r.hs("return_request_id"))["original_order_id"]))
			if o := r.master("SalesOrder", order); o != nil {
				return o["customer_name"]
			}
			if c := r.master("POSCart", order); c != nil {
				return r.nameOf("Customer", catStr(c["customer_id"]))
			}
			return nil
		},
		"Refund Amount": func(r *catRow) interface{} { return firstPositive(r.hn("net_amount"), r.hn("amount")) },
		"Refund Status": func(r *catRow) interface{} { return r.Status },
		"Reference No":  func(r *catRow) interface{} { return firstNonEmpty(r.hs("code"), r.ID) + " " + r.hs("refund_method") },
		"Created At":    func(r *catRow) interface{} { return catStamp(r.Created) },
	}}

// Order to invoice: what was ordered, packed and invoiced per order.
func orderPackedQty(r *catRow) float64 {
	m, ok := r.lk.extra["packedqty"].(map[string]float64)
	if !ok {
		m = map[string]float64{}
		rows, err := dbQuery(fmt.Sprintf(`SELECT data->>'order_id', COALESCE(data->>'items', '[]') FROM %s.documents WHERE doctype = 'ShippingPackage' AND deleted_at IS NULL`, r.lk.schema))
		if err == nil {
			for rows.Next() {
				var o, items string
				if rows.Scan(&o, &items) == nil {
					for _, l := range catLineList(items) {
						m[o] += catNum(l["qty"])
					}
				}
			}
			rows.Close()
		}
		r.lk.extra["packedqty"] = m
	}
	return m[r.ID]
}

var catOrderInvoice = &catSource{Label: "Sales order", Doctype: "SalesOrder", StoreField: "location_code",
	Cols: mergeCols(orderHeaderCols, map[string]catFn{
		"Shipment ID": func(r *catRow) interface{} { return ecomOrderDocs(r)["p:"+r.ID] },
		"Invoice Code": func(r *catRow) interface{} {
			return r.master("SalesInvoice", ecomOrderDocs(r)["i:"+r.ID])["invoice_number"]
		},
		"POS Invoice No": catNotHeld,
		"Invoice Status": func(r *catRow) interface{} {
			if inv := r.master("SalesInvoice", ecomOrderDocs(r)["i:"+r.ID]); inv != nil {
				return inv["_status"]
			}
			return "Not invoiced"
		},
		"POS Bill Status": catNotHeld,
		"Package Amount":  catHN("total_amount"),
		"Net Amount": func(r *catRow) interface{} {
			return r.master("SalesInvoice", ecomOrderDocs(r)["i:"+r.ID])["total_amount"]
		},
		"Variance Qty": func(r *catRow) interface{} { return ecomOrderQty(r) - orderPackedQty(r) },
	})}

// ATP per SKU and location, by the client's own sellable rule (stock by location).
func atpRows(tenantID, schema string, p map[string]string) ([]*catRow, error) {
	q := fmt.Sprintf(`SELECT location_code, sku, available, blocked, hold_qty, qc_hold, damaged, reserved, safety_stock, channel_buffer
		FROM %s.inventory_availability WHERE available <> 0 OR reserved <> 0`, schema)
	var args []interface{}
	if v := strings.TrimSpace(p["location"]); v != "" {
		args = append(args, pqStringArray(strings.Split(v, ",")))
		q += " AND location_code = ANY($1)"
	}
	rows, err := dbQuery(q+" ORDER BY sku, location_code LIMIT 50000", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	policy := atsPolicyFor(schema)
	var out []*catRow
	for rows.Next() {
		var store, sku string
		var available, blocked, hold, qc, damaged, reserved, safety, buffer int
		if rows.Scan(&store, &sku, &available, &blocked, &hold, &qc, &damaged, &reserved, &safety, &buffer) != nil {
			continue
		}
		out = append(out, &catRow{ID: store + "/" + sku, H: map[string]interface{}{"location": store, "sku": sku, "available": float64(available),
			"reserved": float64(reserved), "atp": float64(computeATS(policy, available, reserved, safety, blocked, qc, damaged, buffer, hold))}})
	}
	return out, rows.Err()
}

var catATP = &catSource{Label: "Stock", Rows: atpRows, SKU: "sku",
	Cols: map[string]catFn{
		"Allocated Location": func(r *catRow) interface{} { return r.nameOf("Location", r.storeID()) },
		"Available Qty":      catHN("available"),
		"Reserved Qty":       catHN("reserved"),
		"ATP Qty":            catHN("atp"),
		"Promise Date": func(r *catRow) interface{} {
			if r.hn("atp") > 0 {
				return BusinessToday() // ships from stock today
			}
			return nil
		},
	}}

// --- PIM ----------------------------------------------------------------------

func pimMediaCount(r *catRow) float64 {
	m, ok := r.lk.extra["mediacount"].(map[string]float64)
	if !ok {
		m = map[string]float64{}
		rows, err := dbQuery(fmt.Sprintf(`SELECT data->>'item', COUNT(*) FROM %s.documents WHERE doctype = 'ProductMedia' AND status = 'Active' AND deleted_at IS NULL GROUP BY 1`, r.lk.schema))
		if err == nil {
			for rows.Next() {
				var item string
				var n float64
				if rows.Scan(&item, &n) == nil {
					m[item] = n
				}
			}
			rows.Close()
		}
		r.lk.extra["mediacount"] = m
	}
	return m[r.sku()]
}

// pimProfile is the item's enrichment profile (completeness, missing fields).
func pimProfile(r *catRow) map[string]interface{} {
	m, ok := r.lk.extra["profiles"].(map[string]map[string]interface{})
	if !ok {
		m = map[string]map[string]interface{}{}
		rows, err := dbQuery(fmt.Sprintf(`SELECT data->>'product_id', data FROM %s.documents WHERE doctype = 'PIMProductProfile' AND deleted_at IS NULL`, r.lk.schema))
		if err == nil {
			for rows.Next() {
				var item string
				var raw []byte
				if rows.Scan(&item, &raw) == nil {
					if l := catLineList("[" + string(raw) + "]"); len(l) == 1 {
						m[item] = l[0]
					}
				}
			}
			rows.Close()
		}
		r.lk.extra["profiles"] = m
	}
	return m[r.sku()]
}

func pimMissing(r *catRow) []string {
	var out []string
	for _, s := range catLineListStrings(catStr(pimProfile(r)["missing_fields_json"])) {
		out = append(out, s)
	}
	return out
}

// pimPublish: the latest publish state of an item on a channel (queue + log).
func pimPublish(r *catRow, channel string) (status, errMsg string, retries float64, at string) {
	m, ok := r.lk.extra["publish"].(map[string][4]string)
	if !ok {
		m = map[string][4]string{}
		rows, err := dbQuery(fmt.Sprintf(`SELECT q.item_code, q.channel_code, q.status, q.retry_count, to_char(q.updated_at, 'YYYY-MM-DD HH24:MI'),
			COALESCE((SELECT l.error_message FROM %[1]s.pim_publish_log l WHERE l.job_id = q.job_id ORDER BY l.created_at DESC LIMIT 1), '')
			FROM %[1]s.pim_publish_queue q`, r.lk.schema))
		if err == nil {
			for rows.Next() {
				var item, ch, st, upd, msg string
				var n int
				if rows.Scan(&item, &ch, &st, &n, &upd, &msg) == nil {
					m[item+"/"+ch] = [4]string{st, msg, fmt.Sprint(n), upd}
					if _, has := m[item+"/"]; !has {
						m[item+"/"] = [4]string{st, msg, fmt.Sprint(n), upd}
					}
				}
			}
			rows.Close()
		}
		r.lk.extra["publish"] = m
	}
	v := m[r.sku()+"/"+channel]
	return v[0], v[1], catNum(v[2]), v[3]
}

func pimReadiness(r *catRow) string {
	score := catNum(pimProfile(r)["completeness_score"])
	switch {
	case pimMediaCount(r) == 0:
		return "Not ready - no image"
	case score > 0 && score < 80:
		return "Not ready - incomplete"
	}
	return "Ready"
}

var pimItemCols = map[string]catFn{
	"Completeness %":          func(r *catRow) interface{} { return pimProfile(r)["completeness_score"] },
	"Missing Attribute Count": func(r *catRow) interface{} { return float64(len(pimMissing(r))) },
	"Media Count":             func(r *catRow) interface{} { return pimMediaCount(r) },
	"Channel Readiness":       func(r *catRow) interface{} { return pimReadiness(r) },
	"Image URL":               func(r *catRow) interface{} { return catItemImage(r) },
	"Status":                  func(r *catRow) interface{} { return r.Status },
}

var catPIMItems = &catSource{Label: "Item", Doctype: "Item", StoreField: "-", SKU: "id", Statuses: []string{"Active", "Inactive", "Draft"},
	Cols: mergeCols(catItems.Cols, pimItemCols)}

// One row per item and missing attribute.
var catPIMMissing = &catSource{Label: "Item", Rows: func(tenantID, schema string, p map[string]string) ([]*catRow, error) {
	items, err := (&catSource{Doctype: "Item", StoreField: "-"}).docRows(schema, map[string]string{}, true)
	if err != nil {
		return nil, err
	}
	var out []*catRow
	lk := newCatLookups(schema)
	for _, it := range items {
		it.src, it.lk = catPIMItems, lk
		for _, f := range pimMissing(it) {
			r := *it
			r.L = map[string]interface{}{"attribute": f, "id": it.ID}
			out = append(out, &r)
		}
	}
	return out, nil
}, StoreField: "-", SKU: "id",
	Cols: map[string]catFn{
		"Attribute Name": catL("attribute"),
		"Family Attribute Required": func(r *catRow) interface{} {
			if pimFamilyMandatory(r, catStr(r.item()["family"]), r.ls("attribute")) {
				return "Yes"
			}
			return "No"
		},
		"Channel":            catNotHeld,
		"Publication Status": func(r *catRow) interface{} { s, _, _, _ := pimPublish(r, ""); return s },
	}}

// pimFamilyMandatory: whether a family marks an attribute mandatory.
func pimFamilyMandatory(r *catRow, family, attribute string) bool {
	m := pimFamilyAttrs(r)
	return m[family+"/"+attribute] == "Yes" || m[family+"/"+strings.ToLower(attribute)] == "Yes"
}

func pimFamilyAttrs(r *catRow) map[string]string {
	if m, ok := r.lk.extra["familyattrs"].(map[string]string); ok {
		return m
	}
	m := map[string]string{}
	rows, err := dbQuery(fmt.Sprintf(`SELECT COALESCE(data->>'family', ''), COALESCE(data->>'attribute', ''), COALESCE(data->>'mandatory', '')
		FROM %s.documents WHERE doctype = 'ProductFamilyAttribute' AND deleted_at IS NULL`, r.lk.schema))
	if err == nil {
		for rows.Next() {
			var f, a, mand string
			if rows.Scan(&f, &a, &mand) == nil {
				v := "No"
				if strings.EqualFold(mand, "Yes") || strings.EqualFold(mand, "true") || mand == "1" {
					v = "Yes"
				}
				m[f+"/"+a], m[f+"/"+strings.ToLower(a)] = v, v
				if v == "Yes" {
					m["any/"+a] = "Yes"
				}
				m["families/"+a] = strings.Trim(m["families/"+a]+", "+f, ", ")
			}
		}
		rows.Close()
	}
	r.lk.extra["familyattrs"] = m
	return m
}

var catAttrDefs = &catSource{Label: "Attribute", Doctype: "ProductAttributeDef", StoreField: "-",
	Cols: map[string]catFn{
		"Attribute Name":  func(r *catRow) interface{} { return firstNonEmpty(r.hs("label"), r.hs("code")) },
		"Attribute Value": func(r *catRow) interface{} { return firstNonEmpty(r.hs("value_options"), r.hs("value_type")) },
		"Product Family":  func(r *catRow) interface{} { return pimFamilyAttrs(r)["families/"+r.ID] },
		"Family Attribute Required": func(r *catRow) interface{} {
			if pimFamilyAttrs(r)["any/"+r.ID] == "Yes" {
				return "Yes"
			}
			return "No"
		},
		"Attribute Group": func(r *catRow) interface{} { return r.nameOf("ProductAttributeGroup", r.hs("group")) },
		"Sequence No":     func(r *catRow) interface{} { return r.master("ProductAttributeGroup", r.hs("group"))["display_order"] },
		"Mandatory Flag": func(r *catRow) interface{} {
			if pimFamilyAttrs(r)["any/"+r.ID] == "Yes" {
				return "Yes"
			}
			return "No"
		},
	}}

var catFamilyAttrs = &catSource{Label: "Family attribute", Doctype: "ProductFamilyAttribute", StoreField: "-",
	Cols: map[string]catFn{
		"Product Family": func(r *catRow) interface{} { return r.nameOf("ProductFamily", r.hs("family")) },
		"Attribute Group": func(r *catRow) interface{} {
			return r.nameOf("ProductAttributeGroup", catStr(r.master("ProductAttributeDef", r.hs("attribute"))["group"]))
		},
		"Attribute Name": func(r *catRow) interface{} {
			return firstNonEmpty(catStr(r.master("ProductAttributeDef", r.hs("attribute"))["label"]), r.hs("attribute"))
		},
		"Mandatory Flag": catH("mandatory"),
	}}

// Item x channel from the publish queue.
func publishRows(failedOnly bool) func(tenantID, schema string, p map[string]string) ([]*catRow, error) {
	return func(tenantID, schema string, p map[string]string) ([]*catRow, error) {
		q := fmt.Sprintf(`SELECT job_id::text, item_code, channel_code, status, retry_count, created_at, updated_at FROM %s.pim_publish_queue`, schema)
		if failedOnly {
			q += " WHERE status IN ('failed', 'Failed', 'error', 'Error', 'dead', 'Dead')"
		}
		rows, err := dbQuery(q + " ORDER BY updated_at DESC LIMIT 20000")
		if err != nil {
			return nil, nil // no channel publishing on this client
		}
		defer rows.Close()
		var out []*catRow
		for rows.Next() {
			var id, item, ch, st string
			var n int
			var created, updated time.Time
			if rows.Scan(&id, &item, &ch, &st, &n, &created, &updated) != nil {
				continue
			}
			out = append(out, &catRow{ID: id, Status: st, Created: created, H: map[string]interface{}{"item": item, "channel": ch,
				"retries": float64(n), "updated": catStamp(updated)}})
		}
		return out, rows.Err()
	}
}

var publishCols = mergeCols(pimItemCols, map[string]catFn{
	"Channel":            catH("channel"),
	"Channel SKU":        func(r *catRow) interface{} { return ecomChannelSKU(r, r.hs("channel")) },
	"Publication Status": func(r *catRow) interface{} { return r.Status },
	"Publication Error":  func(r *catRow) interface{} { _, e, _, _ := pimPublish(r, r.hs("channel")); return e },
	"Retry Count":        catHN("retries"),
	"Last Retry At":      catH("updated"),
	"Created At":         func(r *catRow) interface{} { return catStamp(r.Created) },
	"Status":             func(r *catRow) interface{} { return r.master("Item", r.hs("item"))["_status"] },
})

var catPublish = &catSource{Label: "Channel publication", Rows: publishRows(false), StoreField: "-", SKU: "item", Cols: publishCols}
var catPublishFailed = &catSource{Label: "Channel publication", Rows: publishRows(true), StoreField: "-", SKU: "item", Cols: publishCols}

var catCategoryMaps = &catSource{Label: "Category mapping", Doctype: "ChannelCategoryMap", StoreField: "-",
	Cols: map[string]catFn{
		"Product Family":  catNotHeld,
		"Category Name":   func(r *catRow) interface{} { return r.nameOf("Category", r.hs("erp_category")) },
		"Channel":         func(r *catRow) interface{} { return r.nameOf("Channel", r.hs("channel")) },
		"Attribute Name":  catConst("Category"),
		"Attribute Value": catH("channel_category"),
		"Mapping Status": func(r *catRow) interface{} {
			if r.hs("channel_category") == "" {
				return "Not mapped"
			}
			return "Mapped"
		},
	}}

// Items sharing a barcode or EAN with another item.
var catDuplicateCodes = &catSource{Label: "Item", Rows: func(tenantID, schema string, p map[string]string) ([]*catRow, error) {
	rows, err := dbQuery(fmt.Sprintf(`SELECT i.id, COALESCE(i.status, ''), i.created_at, i.data,
		CASE WHEN COALESCE(i.data->>'barcode', '') <> '' AND (SELECT COUNT(*) FROM %[1]s.documents x WHERE x.doctype = 'Item' AND x.deleted_at IS NULL AND x.data->>'barcode' = i.data->>'barcode') > 1 THEN 'Barcode shared with another item' ELSE '' END,
		CASE WHEN COALESCE(i.data->>'gtin', '') <> '' AND (SELECT COUNT(*) FROM %[1]s.documents x WHERE x.doctype = 'Item' AND x.deleted_at IS NULL AND x.data->>'gtin' = i.data->>'gtin') > 1 THEN 'EAN shared with another item' ELSE '' END
		FROM %[1]s.documents i WHERE i.doctype = 'Item' AND i.deleted_at IS NULL`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*catRow
	for rows.Next() {
		var id, status, why1, why2 string
		var at time.Time
		var raw []byte
		if rows.Scan(&id, &status, &at, &raw, &why1, &why2) != nil || (why1 == "" && why2 == "") {
			continue
		}
		h := map[string]interface{}{}
		if l := catLineList("[" + string(raw) + "]"); len(l) == 1 {
			h = l[0]
		}
		h["_why"] = strings.Trim(why1+"; "+why2, "; ")
		out = append(out, &catRow{ID: id, Status: status, Created: at, H: h})
	}
	return out, rows.Err()
}, StoreField: "-", SKU: "id", Cols: map[string]catFn{"Reason": catH("_why"), "Status": func(r *catRow) interface{} { return r.Status }}}

var catProductChanges = &catSource{Label: "Product change", Rows: auditChangeRows([]string{"Item", "ProductFamily", "ProductAttributeValue", "ProductContent"}, nil),
	StoreField: "-", SKU: "doc_id",
	Cols: map[string]catFn{
		"Audit ID":      func(r *catRow) interface{} { return r.ID },
		"SKU":           catH("doc_id"),
		"Field Changed": func(r *catRow) interface{} { return r.hs("doctype") + ": " + r.hs("field") },
		"Old Value":     catH("old"),
		"New Value":     catH("new"),
		"Edit Reason":   catNotHeld,
		"Edited By":     func(r *catRow) interface{} { return r.CreatedBy },
		"Edited At":     func(r *catRow) interface{} { return catStamp(r.Created) },
	}}

// Channel listings with their price, stock and publish state.
func channelListingRows(tenantID, schema string, p map[string]string) ([]*catRow, error) {
	rows, err := dbQuery(fmt.Sprintf(`SELECT m.sku, m.channel, COALESCE(m.channel_sku, ''), m.updated_at, COALESCE(m.location_code, ''),
		COALESCE((SELECT SUM(a.available) FROM %[1]s.inventory_availability a WHERE a.sku = m.sku AND (m.location_code IS NULL OR m.location_code = '' OR a.location_code = m.location_code)), 0)
		FROM %[1]s.channel_product_mapping m ORDER BY m.sku, m.channel LIMIT 20000`, schema))
	if err != nil {
		return nil, nil
	}
	defer rows.Close()
	var out []*catRow
	for rows.Next() {
		var sku, ch, chSKU, loc string
		var at time.Time
		var avail float64
		if rows.Scan(&sku, &ch, &chSKU, &at, &loc, &avail) != nil {
			continue
		}
		out = append(out, &catRow{ID: sku + "/" + ch, Created: at, H: map[string]interface{}{"item": sku, "channel": ch, "channel_sku": chSKU,
			"available": avail, "location": loc}})
	}
	return out, rows.Err()
}

var catListings = &catSource{Label: "Channel listing", Rows: channelListingRows, StoreField: "-", SKU: "item",
	Cols: map[string]catFn{
		"Channel SKU":   catH("channel_sku"),
		"Channel":       catH("channel"),
		"Available Qty": catHN("available"),
		"ERP Sync Time": func(r *catRow) interface{} { return catStamp(r.Created) },
		"Publication Status": func(r *catRow) interface{} {
			s, _, _, _ := pimPublish(r, r.hs("channel"))
			return firstNonEmpty(s, "Listed")
		},
		"Publication Error": func(r *catRow) interface{} { _, e, _, _ := pimPublish(r, r.hs("channel")); return e },
	}}

var catPIMTasks = &catSource{Label: "PIM task", Doctype: "PIMTask", StoreField: "-", SKU: "item_code",
	Statuses: []string{"Open", "In Progress", "Blocked", "Done", "Cancelled"},
	Cols: map[string]catFn{
		"Assigned To": func(r *catRow) interface{} { return firstNonEmpty(r.hs("assignee"), r.hs("assignee_role")) },
		"Updated At":  func(r *catRow) interface{} { return firstNonEmpty(r.hs("completed_at"), catStamp(r.Created)) },
		"Ageing Days": func(r *catRow) interface{} { return math.Floor(r.since(r.Created).Hours() / 24) },
		"Item Name":   func(r *catRow) interface{} { return firstNonEmpty(catStr(r.item()["name"]), r.hs("title")) },
	}}

// --- CRM ----------------------------------------------------------------------

type custSales struct {
	bills      float64
	net        float64
	last       string
	firstStore string
}

// customerSales: paid till bills per customer, once per run.
func customerSales(r *catRow, customer string) custSales {
	m, ok := r.lk.extra["custsales"].(map[string]custSales)
	if !ok {
		m = map[string]custSales{}
		rows, err := dbQuery(fmt.Sprintf(`SELECT data->>'customer_id', COUNT(*), COALESCE(SUM(NULLIF(data->>'amount_paid', '')::numeric), 0),
			to_char(MAX(created_at), 'YYYY-MM-DD'), MIN(data->>'location') FROM %s.documents
			WHERE doctype = 'POSCart' AND status = 'Paid' AND deleted_at IS NULL AND COALESCE(data->>'customer_id', '') <> '' GROUP BY 1`, r.lk.schema))
		if err == nil {
			for rows.Next() {
				var c, last, store string
				var s custSales
				if rows.Scan(&c, &s.bills, &s.net, &last, &store) == nil {
					s.last, s.firstStore = last, store
					m[c] = s
				}
			}
			rows.Close()
		}
		r.lk.extra["custsales"] = m
	}
	return m[customer]
}

func loyaltyBalance(r *catRow, customer string) float64 {
	m, ok := r.lk.extra["loyalty"].(map[string]float64)
	if !ok {
		m = map[string]float64{}
		rows, err := dbQuery(fmt.Sprintf(`SELECT customer_id, COALESCE(SUM(points), 0) FROM %s.loyalty_point_ledger GROUP BY 1`, r.lk.schema))
		if err == nil {
			for rows.Next() {
				var c string
				var pts float64
				if rows.Scan(&c, &pts) == nil {
					m[c] = pts
				}
			}
			rows.Close()
		}
		r.lk.extra["loyalty"] = m
	}
	return m[customer]
}

func rfmScore(s custSales) (r, f, m int) {
	days := 9999.0
	if t, err := time.Parse("2006-01-02", s.last); err == nil {
		days = time.Since(t).Hours() / 24
	}
	switch {
	case days <= 30:
		r = 5
	case days <= 60:
		r = 4
	case days <= 90:
		r = 3
	case days <= 180:
		r = 2
	default:
		r = 1
	}
	switch {
	case s.bills >= 10:
		f = 5
	case s.bills >= 5:
		f = 4
	case s.bills >= 3:
		f = 3
	case s.bills >= 2:
		f = 2
	default:
		f = 1
	}
	switch {
	case s.net >= 50000:
		m = 5
	case s.net >= 20000:
		m = 4
	case s.net >= 10000:
		m = 3
	case s.net >= 5000:
		m = 2
	default:
		m = 1
	}
	return
}

func customerSegment(r *catRow) string {
	if t := r.hs("loyalty_tier"); t != "" {
		return t
	}
	s := customerSales(r, r.ID)
	if s.bills == 0 {
		return "No purchase yet"
	}
	rr, f, _ := rfmScore(s)
	switch {
	case rr >= 4 && f >= 4:
		return "Champion"
	case rr >= 4:
		return "Recent"
	case f >= 3:
		return "Loyal, slipping"
	case rr <= 2:
		return "Lapsed"
	}
	return "Occasional"
}

var customerCols = map[string]catFn{
	"Customer ID":      func(r *catRow) interface{} { return firstNonEmpty(r.hs("code"), r.ID) },
	"Customer Name":    catH("name"),
	"Customer Mobile":  catH("phone"),
	"Segment":          func(r *catRow) interface{} { return customerSegment(r) },
	"Loyalty Points":   func(r *catRow) interface{} { return loyaltyBalance(r, r.ID) },
	"Last Sale Date":   func(r *catRow) interface{} { return customerSales(r, r.ID).last },
	"Net Sales Amount": func(r *catRow) interface{} { return customerSales(r, r.ID).net },
	"Bill Count":       func(r *catRow) interface{} { return customerSales(r, r.ID).bills },
	"Feedback Score":   catNotHeld,
	"RFM Score": func(r *catRow) interface{} {
		s := customerSales(r, r.ID)
		if s.bills == 0 {
			return nil
		}
		a, b, c := rfmScore(s)
		return fmt.Sprintf("%d%d%d", a, b, c)
	},
	"Store Name": func(r *catRow) interface{} { return r.nameOf("Location", customerSales(r, r.ID).firstStore) },
	"Status": func(r *catRow) interface{} {
		if r.hs("merged_into") != "" {
			return "Merged into " + r.hs("merged_into")
		}
		return r.Status
	},
	"Campaign": func(r *catRow) interface{} {
		if birthdaySoon(r.hs("date_of_birth")) {
			return "Birthday"
		}
		if birthdaySoon(r.hs("date_of_anniversary")) {
			return "Anniversary"
		}
		return nil
	},
}

// birthdaySoon: the date's day and month fall in the next 30 days.
func birthdaySoon(date string) bool {
	t, err := time.Parse("2006-01-02", catDay(date))
	if err != nil {
		return false
	}
	now := time.Now()
	next := time.Date(now.Year(), t.Month(), t.Day(), 0, 0, 0, 0, now.Location())
	if next.Before(now.Truncate(24 * time.Hour)) {
		next = next.AddDate(1, 0, 0)
	}
	return next.Sub(now) <= 30*24*time.Hour
}

var catCustomers = &catSource{Label: "Customer", Doctype: "Customer", StoreField: "-", Where: "COALESCE(data->>'walk_in', '') <> 'Yes'", Cols: customerCols}

// Promotions on bills: each offer applied to a paid bill.
func offerUseRows(tenantID, schema string, p map[string]string) ([]*catRow, error) {
	bills, err := catPOSBills.docRows(schema, p, false)
	if err != nil {
		return nil, err
	}
	var out []*catRow
	for _, b := range bills {
		for i, o := range catLineList(b.H["applied_offers"]) {
			r := *b
			r.N = i + 1
			r.L = o
			out = append(out, &r)
		}
	}
	return out, nil
}

var catOfferUses = &catSource{Label: "Offer applied", Rows: offerUseRows, Customer: "customer_id", CustomerName: "customer_name",
	Cols: mergeCols(posSaleCols, map[string]catFn{
		"Document Date":   func(r *catRow) interface{} { return r.date() },
		"Coupon Code":     func(r *catRow) interface{} { return firstNonEmpty(r.ls("coupon_code"), r.ls("offer_id")) },
		"Campaign":        func(r *catRow) interface{} { return firstNonEmpty(r.ls("name"), r.ls("offer_id")) },
		"Channel":         catConst("Retail"),
		"Discount Amount": catLN("discount"),
		"Net Amount":      catHN("amount_paid"),
		"Net Sales Amount": func(r *catRow) interface{} {
			return r.hn("amount_paid") / float64(len(catLineList(r.H["applied_offers"])))
		},
		"Bill Count":    func(r *catRow) interface{} { return posBillNo(r) },
		"Achievement %": catNotHeld,
		"Status":        func(r *catRow) interface{} { return r.Status },
	})}

func loyaltyRows(tenantID, schema string, p map[string]string) ([]*catRow, error) {
	q := fmt.Sprintf(`SELECT id::text, customer_id, COALESCE(transaction_type, ''), points, COALESCE(reference_doctype, ''), COALESCE(reference_id, ''), created_at
		FROM %s.loyalty_point_ledger WHERE TRUE`, schema)
	var args []interface{}
	if v := p["from"]; v != "" {
		args = append(args, v)
		q += fmt.Sprintf(" AND created_at >= public.erp_day_start($%d::date)", len(args))
	}
	if v := p["to"]; v != "" {
		args = append(args, v)
		q += fmt.Sprintf(" AND created_at < public.erp_day_start($%d::date + 1)", len(args))
	}
	rows, err := dbQuery(q+" ORDER BY customer_id, created_at LIMIT 50000", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*catRow
	for rows.Next() {
		var id, cust, typ, refType, refID string
		var pts float64
		var at time.Time
		if rows.Scan(&id, &cust, &typ, &pts, &refType, &refID, &at) != nil {
			continue
		}
		out = append(out, &catRow{ID: id, Created: at, H: map[string]interface{}{"customer_id": cust, "type": typ, "points": pts, "ref_type": refType, "ref_id": refID}})
	}
	return out, rows.Err()
}

var catLoyalty = &catSource{Label: "Loyalty", Rows: loyaltyRows, Customer: "customer_id", StoreOf: func(r *catRow) string {
	return catStr(r.master(r.hs("ref_type"), r.hs("ref_id"))["location"])
},
	Cols: map[string]catFn{
		"Document Date": func(r *catRow) interface{} { return catDay(r.Created) },
		"Sales Invoice No": func(r *catRow) interface{} {
			if r.hs("ref_type") == "POSCart" {
				return catDocNumber(r, "POSCart", r.hs("ref_id"))
			}
			return nil
		},
		"Sales Return No": func(r *catRow) interface{} {
			if r.hs("ref_type") == "ReturnRequest" {
				return catDocNumber(r, "ReturnRequest", r.hs("ref_id"))
			}
			return nil
		},
		"Loyalty Points": catHN("points"),
		"Edit Action":    catH("type"),
	}}

var catServiceCases = &catSource{Label: "Service case", Doctype: "ServiceTicket", StoreField: "-", Customer: "customer", Number: "code",
	Statuses: []string{"Open", "In Progress", "Resolved", "Closed", "Cancelled"},
	Cols: map[string]catFn{
		"Case No":     func(r *catRow) interface{} { return r.number() },
		"Created At":  func(r *catRow) interface{} { return catStamp(r.Created) },
		"Case Status": func(r *catRow) interface{} { return r.Status },
		"Store Name": func(r *catRow) interface{} {
			return r.nameOf("Location", catStr(r.master("Asset", r.hs("asset"))["location"]))
		},
		"Reason":    catH("description"),
		"User Name": func(r *catRow) interface{} { return firstNonEmpty(r.hs("assigned_to"), r.CreatedBy) },
		"Remarks": func(r *catRow) interface{} {
			return firstNonEmpty(r.hs("resolution_notes"), r.hs("cancellation_reason"))
		},
	}}

var catCustomerChanges = &catSource{Label: "Customer change", Rows: auditChangeRows([]string{"Customer"}, []string{"phone", "merged_into", "name", "phone_country", "email"}),
	StoreField: "-", Customer: "doc_id",
	Cols: map[string]catFn{
		"Audit ID":      func(r *catRow) interface{} { return r.ID },
		"Customer ID":   catH("doc_id"),
		"Field Changed": catH("field"),
		"Old Value":     catH("old"),
		"New Value":     catH("new"),
		"Edit Reason":   catNotHeld,
		"Edited By":     func(r *catRow) interface{} { return r.CreatedBy },
		"Edited At":     func(r *catRow) interface{} { return catStamp(r.Created) },
	}}

var _ = catWire("cat-order-register", &catReport{Src: catOrderHeaders})
var _ = catWire("cat-order-line-detail", &catReport{Src: catOrderLines})
var _ = catWire("cat-order-allocation-reservation-report", &catReport{Src: catReservations})
var _ = catWire("cat-fulfilment-status-report", &catReport{Src: catTasks})
var _ = catWire("cat-split-shipment-report", &catReport{Src: catEcomPackages,
	Keep: func(r *catRow) bool {
		return catNum(ecomPackageSeq(r, r.hs("order_id"), r.ID)) > 1 || orderPackageCount(r) > 1
	},
	Cols: map[string]catFn{
		"Allocated Location": func(r *catRow) interface{} { return r.nameOf("Location", r.storeID()) },
		"Fulfilment Status":  func(r *catRow) interface{} { return r.Status },
		"Order ID":           func(r *catRow) interface{} { return orderID(r, "order_id") },
	}})
var _ = catWire("cat-pending-order-sla-aging", &catReport{Src: catTasks, Keep: taskOpen})
var _ = catWire("cat-order-cancellation-report", &catReport{Src: catOrderLines,
	Keep: func(r *catRow) bool {
		return r.hs("line_status") == "Cancelled" || catStr(r.master("SalesOrder", r.hs("order_id"))["order_status"]) == "Cancelled"
	}})
var _ = catWire("cat-return-rto-register", &catReport{Src: catReturnRTO})
var _ = catWire("cat-refund-status-report", &catReport{Src: catRefunds})
var _ = catWire("cat-order-to-invoice-reconciliation", &catReport{Src: catOrderInvoice})
var _ = catWire("cat-atp-promise-inventory-report", &catReport{Src: catATP, NoDates: true})
var _ = catWire("cat-channel-order-reconciliation", &catReport{Src: catOrderHeaders,
	Keep: func(r *catRow) bool { ch := r.hs("channel"); return ch != "" && !strings.EqualFold(ch, "Manual") }})
var _ = catWire("cat-oms-integration-error-retry", &catReport{Src: catIntegrationAll})

var _ = catWire("cat-product-master-report", &catReport{Src: catPIMItems})
var _ = catWire("cat-product-completeness-report", &catReport{Src: catPIMItems})
var _ = catWire("cat-pim-workbench-queue", &catReport{Src: catPIMTasks})
var _ = catWire("cat-missing-attribute-report", &catReport{Src: catPIMMissing, NoDates: true})
var _ = catWire("cat-product-family-variant-matrix", &catReport{Src: catPIMItems, Keep: func(r *catRow) bool { return r.hs("family") != "" }})
var _ = catWire("cat-attribute-definition-report", &catReport{Src: catAttrDefs})
var _ = catWire("cat-attribute-group-report", &catReport{Src: catAttrDefs})
var _ = catWire("cat-family-attribute-mapping-report", &catReport{Src: catFamilyAttrs})
var _ = catWire("cat-media-image-coverage-report", &catReport{Src: catPIMItems})
var _ = catWire("cat-channel-readiness-report", &catReport{Src: catPublish, NoDates: true})
var _ = catWire("cat-channel-publication-status", &catReport{Src: catPublish, NoDates: true})
var _ = catWire("cat-channel-publication-error-report", &catReport{Src: catPublishFailed, NoDates: true})
var _ = catWire("cat-category-attribute-mapping-report", &catReport{Src: catCategoryMaps})
var _ = catWire("cat-duplicate-sku-barcode-exception", &catReport{Src: catDuplicateCodes, NoDates: true})
var _ = catWire("cat-product-change-audit", &catReport{Src: catProductChanges})
var _ = catWire("cat-price-inventory-publish-status", &catReport{Src: catListings, NoDates: true})

var _ = catWire("cat-customer-360-summary", &catReport{Src: catCustomers})
var _ = catWire("cat-campaign-performance", &catReport{Src: catOfferUses,
	Group: []string{"From Date", "To Date", "Campaign", "Channel"}, Agg: map[string]string{"Bill Count": "countd"}})
var _ = catWire("cat-customer-segmentation-report", &catReport{Src: catCustomers, NoDates: true})
var _ = catWire("cat-repeat-purchase-report", &catReport{Src: catCustomers, NoDates: true,
	Keep: func(r *catRow) bool { return customerSales(r, r.ID).bills >= 2 }})
var _ = catWire("cat-loyalty-points-ledger", &catReport{Src: catLoyalty, Cols: map[string]catFn{"Balance": catPost}, PostAll: func(rows []map[string]interface{}) {
	bal := map[string]float64{}
	for _, row := range rows { // oldest first per customer (the query's order)
		k := catStr(row["Customer ID"])
		bal[k] += catNum(row["Loyalty Points"])
		row["Balance"] = bal[k]
	}
}})
var _ = catWire("cat-coupon-voucher-usage-report", &catReport{Src: catOfferUses})
var _ = catWire("cat-birthday-anniversary-campaign-list", &catReport{Src: catCustomers, NoDates: true,
	Keep: func(r *catRow) bool {
		return birthdaySoon(r.hs("date_of_birth")) || birthdaySoon(r.hs("date_of_anniversary"))
	}})
var _ = catWire("cat-complaint-service-case-register", &catReport{Src: catServiceCases})
var _ = catWire("cat-customer-merge-mobile-change-audit", &catReport{Src: catCustomerChanges})
var _ = catWire("cat-customer-inactivity-churn-report", &catReport{Src: catCustomers, NoDates: true,
	Keep: func(r *catRow) bool {
		s := customerSales(r, r.ID)
		if s.bills == 0 {
			return false
		}
		t, err := time.Parse("2006-01-02", s.last)
		return err == nil && time.Since(t) > 90*24*time.Hour
	},
	Cols: map[string]catFn{"Status": func(r *catRow) interface{} {
		t, _ := time.Parse("2006-01-02", customerSales(r, r.ID).last)
		if time.Since(t) > 180*24*time.Hour {
			return "Lapsed (no purchase in 6 months)"
		}
		return "At risk (no purchase in 3 months)"
	}}})

func orderPackageCount(r *catRow) float64 {
	m, ok := r.lk.extra["pkgcount"].(map[string]float64)
	if !ok {
		m = map[string]float64{}
		rows, err := dbQuery(fmt.Sprintf(`SELECT data->>'order_id', COUNT(*) FROM %s.documents WHERE doctype = 'ShippingPackage' AND deleted_at IS NULL GROUP BY 1`, r.lk.schema))
		if err == nil {
			for rows.Next() {
				var o string
				var n float64
				if rows.Scan(&o, &n) == nil {
					m[o] = n
				}
			}
			rows.Close()
		}
		r.lk.extra["pkgcount"] = m
	}
	return m[r.hs("order_id")]
}
