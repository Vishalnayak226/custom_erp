package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Stage 148.32: drop-ship (module wholesale).
//
// An item marked Drop-ship with a default vendor is sold from the vendor, not
// from our stores. Saving the order raises a Draft purchase order per vendor,
// on the normal PO path (numbering, vendor pricing, place of supply,
// approval), shipped to the customer's address. The order's line quantity and
// delivery date stay in step with that PO while it is Draft; once it is past
// Draft a change is refused with a message, and the vendor is asked instead.
// When the vendor confirms (one list, one confirm-all) the line is shipped:
// the order moves to Shipped, the normal order invoice follows, and the cost
// is booked as the vendor's bill (Dr 5100 cost of goods, Cr 2100 payables).
// No stock moves into or out of our stores: no reservation, no pick task, no
// goods receipt.
//
// An order is all drop-ship or none: mixing drop-ship and stock lines on one
// order is refused (SALESP-0716).

type dropShipPlan struct{ vendor map[string]string } // sku -> vendor

// planDropShip is CreateSalesOrder's one question: nil when wholesale is off
// or no line is a drop-ship item.
func planDropShip(tenantID string, lines []SalesOrderLineInput) (*dropShipPlan, error) {
	if !wholesaleOn(tenantID) {
		return nil, nil
	}
	plan := &dropShipPlan{vendor: map[string]string{}}
	drop := 0
	for _, l := range lines {
		item, err := ResolveItemBySKU(tenantID, l.SKU)
		if err != nil || item == nil || strField(item.Data, "dropship") != "Yes" {
			continue
		}
		vendor := strField(item.Data, "default_vendor")
		if vendor == "" {
			return nil, wsErr(wsNoVendorCode, "%s is drop-ship but has no default vendor.", l.SKU)
		}
		plan.vendor[l.SKU] = vendor
		drop++
	}
	if drop == 0 {
		return nil, nil
	}
	if drop != len(lines) {
		return nil, wsErr(wsMixedOrderCode, "An order is all drop-ship or none. Put the drop-ship items on their own order.")
	}
	return plan, nil
}

// markLine flags a new order line (nil-safe).
func (p *dropShipPlan) markLine(lineDoc map[string]interface{}, sku string) {
	if p == nil {
		return
	}
	lineDoc["dropship"] = "Yes"
	lineDoc["vendor_id"] = p.vendor[sku]
}

type dsLine struct {
	id   string
	data map[string]interface{}
}

func dsOrderLines(schema, orderID string) ([]*dsLine, error) {
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, data FROM %s.documents WHERE doctype = 'SalesOrderLine' AND data->>'order_id' = $1 AND deleted_at IS NULL ORDER BY id`, schema), orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*dsLine
	for rows.Next() {
		var l dsLine
		var raw []byte
		if rows.Scan(&l.id, &raw) != nil || json.Unmarshal(raw, &l.data) != nil {
			continue
		}
		out = append(out, &l)
	}
	return out, rows.Err()
}

type dsPO struct {
	id, status string
	data       map[string]interface{}
}

// dsPOsOf are the live purchase orders raised for an order.
func dsPOsOf(schema, orderID string) []*dsPO {
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, status, data FROM %s.documents WHERE doctype = 'PurchaseOrder' AND deleted_at IS NULL
		 AND status <> 'Cancelled' AND data->>'dropship_order_id' = $1 ORDER BY id`, schema), orderID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []*dsPO
	for rows.Next() {
		var p dsPO
		var raw []byte
		if rows.Scan(&p.id, &p.status, &raw) == nil && json.Unmarshal(raw, &p.data) == nil {
			out = append(out, &p)
		}
	}
	return out
}

func dsLoadPO(schema, poID string) *dsPO {
	var p dsPO
	var raw []byte
	if db.DB.QueryRow(fmt.Sprintf(`SELECT id, status, data FROM %s.documents WHERE doctype = 'PurchaseOrder' AND id = $1 AND deleted_at IS NULL`, schema), poID).Scan(&p.id, &p.status, &raw) != nil || json.Unmarshal(raw, &p.data) != nil {
		return nil
	}
	return &p
}

// dsPOReady: the vendor's order is approved (past the Draft and approval wait).
func dsPOReady(status string) bool {
	switch status {
	case "", "Draft", "Pending Approval", "Rejected", "Cancelled":
		return false
	}
	return true
}

// raiseDropShipPOs makes the Draft PO of each vendor for the order's lines
// that have none yet. Safe to call again: a line with a PO is skipped.
func raiseDropShipPOs(tenantID, orderID, userID string) ([]string, error) {
	schema, order, err := fetchSalesOrder(tenantID, orderID)
	if err != nil {
		return nil, err
	}
	lines, err := dsOrderLines(schema, orderID)
	if err != nil {
		return nil, err
	}
	byVendor := map[string][]*dsLine{}
	for _, l := range lines {
		if strField(l.data, "dropship") == "Yes" && strField(l.data, "po_id") == "" && strField(l.data, "line_status") != "Cancelled" {
			v := strField(l.data, "vendor_id")
			byVendor[v] = append(byVendor[v], l)
		}
	}
	if len(byVendor) == 0 {
		return nil, nil
	}
	var store string
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT id FROM %s.documents WHERE doctype = 'Location' AND deleted_at IS NULL ORDER BY (status = 'Active') DESC, id LIMIT 1`, schema)).Scan(&store)
	if store == "" {
		return nil, wsErr(wsNoVendorCode, "Add a store first: the purchase order needs one.")
	}
	vendors := make([]string, 0, len(byVendor))
	for v := range byVendor {
		vendors = append(vendors, v)
	}
	sort.Strings(vendors)
	var made []string
	for _, vendor := range vendors {
		id, err := raiseDropShipPO(tenantID, schema, userID, store, vendor, orderID, order, byVendor[vendor])
		if err != nil {
			return made, err
		}
		made = append(made, id)
	}
	return made, nil
}

func raiseDropShipPO(tenantID, schema, userID, store, vendor, orderID string, order map[string]interface{}, lines []*dsLine) (string, error) {
	poID, err := GenerateSequence(tenantID, "PO", store, documentFinancialYear(time.Now()))
	if err != nil {
		return "", err
	}
	pl := make([]proposalPOLine, len(lines))
	for i, l := range lines {
		pl[i] = proposalPOLine{SKU: strField(l.data, "sku"), Qty: int(numFromInterface(l.data["qty"])), Cost: quoteCostRupees(tenantID, strField(l.data, "sku"))}
	}
	itemsJSON, total, err := PriceProposalPOLines(tenantID, vendor, pl)
	if err != nil {
		return "", err
	}
	poLines, err := parsePOLineMaps(itemsJSON)
	if err != nil || len(poLines) != len(lines) {
		return "", wsErr(wsBillCode, "The purchase order lines could not be built.")
	}
	for i, l := range lines {
		poLines[i]["sales_line_id"] = l.id
	}
	raw, _ := json.Marshal(poLines)
	po := map[string]interface{}{
		"code": poID, "po_number": poID, "vendor": vendor, "vendor_id": vendor, "location": store, "target_warehouse": store,
		"items": string(raw), "total_amount": round2(total), "gst_mode": GSTModeExclusive, "status": "Draft",
	}
	if d := strField(order, "delivery_date"); d != "" {
		po["expected_delivery_date"] = d
	}
	if err := RequirePOStoreState(tenantID, po); err != nil {
		return "", err
	}
	ApplyPlaceOfSupply(tenantID, po)
	if err := ValidateDocument(tenantID, "PurchaseOrder", po); err != nil {
		return "", err
	}
	if err := ApplyPurchaseOrderTotals(tenantID, po); err != nil { // 148.52
		return "", err
	}
	po["dropship_order_id"] = orderID // set by the system, after the form rules
	po["ship_to_address"] = ShippingAddressText(order)
	b, _ := json.Marshal(po)
	tx, err := db.DB.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'PurchaseOrder', $2, 'Draft', $3)`, schema), poID, b, userID); err != nil {
		return "", err
	}
	for i, l := range lines {
		link, _ := json.Marshal(map[string]interface{}{"po_id": poID, "po_line": i + 1})
		if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || $1::jsonb, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'SalesOrderLine' AND id = $2`, schema), string(link), l.id); err != nil {
			return "", err
		}
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	LogAuditEvent(tenantID, userID, "DROPSHIP_PO_RAISED", "SUCCESS", fmt.Sprintf("Draft PO %s to %s for order %s", poID, vendor, orderID))
	return poID, nil
}

// acceptDropShipOrder: accepting a drop-ship order raises any PO still
// missing (a held order released later) and releases no pick.
func acceptDropShipOrder(tenantID string, order map[string]interface{}, orderID, userID string) (bool, error) {
	if strField(order, "dropship") != "Yes" {
		return false, nil
	}
	_, err := raiseDropShipPOs(tenantID, orderID, userID)
	return true, err
}

func dsLocked(po *dsPO) error {
	return wsErr(wsPOLockedCode, "Purchase order %s is past Draft. Ask the vendor to change it.", po.id)
}

// checkDropShipDate refuses a delivery date the purchase order cannot follow.
func checkDropShipDate(tenantID, orderID string, order map[string]interface{}, date string) error {
	if err := requireWholesale(tenantID); err != nil {
		return err
	}
	if date != "" {
		if _, err := time.Parse("2006-01-02", date); err != nil {
			return wsErr(wsInstalmentCode, "The delivery date must be YYYY-MM-DD.")
		}
	}
	if strField(order, "dropship") != "Yes" {
		return nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	for _, po := range dsPOsOf(schema, orderID) {
		if po.status != "Draft" {
			return dsLocked(po)
		}
	}
	return nil
}

// syncDropShipDate moves the Draft POs' expected date with the order.
func syncDropShipDate(tenantID, schema, orderID, date string) {
	for _, po := range dsPOsOf(schema, orderID) {
		if po.status != "Draft" {
			continue
		}
		if date == "" {
			delete(po.data, "expected_delivery_date")
		} else {
			po.data["expected_delivery_date"] = date
		}
		b, _ := json.Marshal(po.data)
		_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $1, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'PurchaseOrder' AND id = $2`, schema), b, po.id)
	}
}

// editDropShipOrderLines is EditOrderLines for a drop-ship order: a quantity
// or price change, or a removal, follows to the Draft PO in the same step; a
// PO past Draft refuses it. Not a drop-ship order: handled is false and the
// normal path runs.
func editDropShipOrderLines(tenantID, orderID string, changes []OrderLineChange, userID string) (bool, map[string]interface{}, error) {
	schema, order, err := fetchSalesOrder(tenantID, orderID)
	if err != nil || strField(order, "dropship") != "Yes" {
		return false, nil, nil
	}
	if status := strField(order, "order_status"); !orderLineEditableStatuses[status] {
		return true, nil, &ValidationError{Code: "ORDERM-0466", SubFor: "lines", Message: fmt.Sprintf("lines cannot change while the order is %s", status)}
	}
	lines, err := dsOrderLines(schema, orderID)
	if err != nil {
		return true, nil, err
	}
	byID := map[string]*dsLine{}
	for _, l := range lines {
		byID[l.id] = l
	}
	pos := map[string]*dsPO{}
	touched := map[string]bool{}
	for _, c := range changes {
		l := byID[c.LineID]
		if c.LineID == "" || l == nil {
			return true, nil, wsErr(wsNotDropShipCode, "A drop-ship order takes no new line. Use a new order.")
		}
		if po := strField(l.data, "po_id"); po != "" {
			p := pos[po]
			if p == nil {
				if p = dsLoadPO(schema, po); p == nil {
					continue
				}
				pos[po] = p
			}
			if p.status != "Draft" {
				return true, nil, dsLocked(p)
			}
		}
		if c.Remove {
			l.data["line_status"] = "Cancelled"
		} else {
			if c.Qty > 0 {
				l.data["qty"] = c.Qty
			}
			if c.UnitPrice != nil && *c.UnitPrice >= 0 {
				l.data["unit_price"] = *c.UnitPrice
			}
		}
		touched[l.id] = true
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return true, nil, err
	}
	defer tx.Rollback()
	total := 0.0
	for _, l := range lines {
		if touched[l.id] {
			b, _ := json.Marshal(l.data)
			st := "Reserved"
			if strField(l.data, "line_status") == "Cancelled" {
				st = "Cancelled"
			}
			if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $1, status = $2, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'SalesOrderLine' AND id = $3`, schema), b, st, l.id); err != nil {
				return true, nil, err
			}
		}
		if strField(l.data, "line_status") != "Cancelled" {
			total += numFromInterface(l.data["qty"]) * numFromInterface(l.data["unit_price"])
		}
	}
	for _, p := range pos {
		if err := dsResyncPO(tenantID, tx, schema, p, lines); err != nil {
			return true, nil, err
		}
	}
	order["total_amount"] = round2(total)
	ob, _ := json.Marshal(order)
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $1, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'SalesOrder' AND id = $2`, schema), ob, orderID); err != nil {
		return true, nil, err
	}
	if err := tx.Commit(); err != nil {
		return true, nil, err
	}
	LogAuditEvent(tenantID, userID, "ORDER_LINES_EDIT", "SUCCESS", fmt.Sprintf("order=%s drop-ship lines changed, purchase orders followed", orderID))
	return true, map[string]interface{}{"order_id": orderID, "changed": len(touched), "total_amount": round2(total)}, nil
}

// dsResyncPO rebuilds a Draft PO's lines from the order lines it serves.
func dsResyncPO(tenantID string, tx *sql.Tx, schema string, po *dsPO, lines []*dsLine) error {
	vendor := firstNonEmpty(strField(po.data, "vendor_id"), strField(po.data, "vendor"))
	var pl []proposalPOLine
	var ids []string
	for _, l := range lines {
		if strField(l.data, "po_id") == po.id && strField(l.data, "line_status") != "Cancelled" {
			pl = append(pl, proposalPOLine{SKU: strField(l.data, "sku"), Qty: int(numFromInterface(l.data["qty"])), Cost: quoteCostRupees(tenantID, strField(l.data, "sku"))})
			ids = append(ids, l.id)
		}
	}
	if len(pl) == 0 {
		po.data["status"] = "Cancelled"
		b, _ := json.Marshal(po.data)
		_, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $1, status = 'Cancelled', updated_at = CURRENT_TIMESTAMP WHERE doctype = 'PurchaseOrder' AND id = $2`, schema), b, po.id)
		return err
	}
	itemsJSON, total, err := PriceProposalPOLines(tenantID, vendor, pl)
	if err != nil {
		return err
	}
	poLines, err := parsePOLineMaps(itemsJSON)
	if err != nil || len(poLines) != len(ids) {
		return wsErr(wsBillCode, "The purchase order lines could not be rebuilt.")
	}
	for i := range poLines {
		poLines[i]["sales_line_id"] = ids[i]
	}
	raw, _ := json.Marshal(poLines)
	po.data["items"] = string(raw)
	po.data["total_amount"] = round2(total)
	if err := ApplyPurchaseOrderTotals(tenantID, po.data); err != nil { // 148.52: tax and grand total follow the lines
		return err
	}
	b, _ := json.Marshal(po.data)
	_, err = tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $1, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'PurchaseOrder' AND id = $2`, schema), b, po.id)
	return err
}

// refuseDropShipGRN: the goods go to the customer, never to our stores.
func refuseDropShipGRN(tenantID, poID string) error {
	if strings.TrimSpace(poID) == "" {
		return nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil
	}
	var order sql.NullString
	if db.DB.QueryRow(fmt.Sprintf(`SELECT data->>'dropship_order_id' FROM %s.documents WHERE doctype = 'PurchaseOrder' AND id = $1`, schema), poID).Scan(&order) == nil && order.String != "" {
		return wsErr(wsNoGRNCode, "Purchase order %s is a drop-ship: the vendor ships to the customer, so no goods receipt.", poID)
	}
	return nil
}

// DropShipLine is one row of the vendor-confirmation list.
type DropShipLine struct {
	LineID   string  `json:"line_id"`
	OrderID  string  `json:"order_id"`
	Customer string  `json:"customer"`
	Address  string  `json:"address"`
	Sku      string  `json:"sku"`
	Qty      float64 `json:"qty"`
	Vendor   string  `json:"vendor"`
	POID     string  `json:"po_id"`
	POStatus string  `json:"po_status"`
	Expected string  `json:"expected_date"`
	Ready    bool    `json:"ready"`
}

// OpenDropShipLines lists the drop-ship lines the vendor has not shipped yet.
func OpenDropShipLines(tenantID, vendor string) ([]DropShipLine, error) {
	if err := requireWholesale(tenantID); err != nil {
		return nil, err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT l.id, l.data, o.data FROM %s.documents l JOIN %s.documents o ON o.doctype = 'SalesOrder' AND o.id = l.data->>'order_id'
		 WHERE l.doctype = 'SalesOrderLine' AND l.deleted_at IS NULL AND o.deleted_at IS NULL AND l.data->>'dropship' = 'Yes'
		   AND COALESCE(l.data->>'line_status','') NOT IN ('Dispatched','Cancelled') ORDER BY l.id`, schema, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DropShipLine{}
	poStatus := map[string]*dsPO{}
	for rows.Next() {
		var id string
		var lraw, oraw []byte
		var l, o map[string]interface{}
		if rows.Scan(&id, &lraw, &oraw) != nil || json.Unmarshal(lraw, &l) != nil || json.Unmarshal(oraw, &o) != nil {
			continue
		}
		if vendor != "" && strField(l, "vendor_id") != vendor {
			continue
		}
		row := DropShipLine{LineID: id, OrderID: strField(l, "order_id"), Customer: strField(o, "customer_name"), Address: ShippingAddressText(o),
			Sku: strField(l, "sku"), Qty: numFromInterface(l["qty"]), Vendor: strField(l, "vendor_id"), POID: strField(l, "po_id")}
		if row.POID != "" {
			p, ok := poStatus[row.POID]
			if !ok {
				p = dsLoadPO(schema, row.POID)
				poStatus[row.POID] = p
			}
			if p != nil {
				row.POStatus, row.Expected, row.Ready = p.status, strField(p.data, "expected_delivery_date"), dsPOReady(p.status)
			}
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// DropShipConfirmResult says what a confirmation did.
type DropShipConfirmResult struct {
	Confirmed []string `json:"confirmed"`
	Skipped   []string `json:"skipped"`
	Bills     []string `json:"bills"`
	Invoices  []string `json:"invoices"`
}

// ConfirmDropShip records that the vendor shipped. lineIDs names the lines;
// all takes every ready line (of vendor, when given) and skips, rather than
// refuses, those whose PO is not approved yet. A named line whose PO is not
// approved is refused (PURCHA-5106).
func ConfirmDropShip(tenantID, userID string, lineIDs []string, all bool, vendor string) (*DropShipConfirmResult, error) {
	open, err := OpenDropShipLines(tenantID, vendor)
	if err != nil {
		return nil, err
	}
	res := &DropShipConfirmResult{Confirmed: []string{}, Skipped: []string{}, Bills: []string{}, Invoices: []string{}}
	want := map[string]bool{}
	for _, id := range lineIDs {
		want[id] = true
	}
	if !all && len(want) == 0 {
		return nil, wsErr(wsNothingCode, "Choose the lines the vendor shipped.")
	}
	var chosen []DropShipLine
	for _, l := range open {
		if !all && !want[l.LineID] {
			continue
		}
		delete(want, l.LineID)
		if !l.Ready {
			if !all {
				return nil, wsErr(wsPONotReadyCode, "Purchase order %s is not approved yet. Approve it before the vendor ships.", firstNonEmpty(l.POID, "for this line"))
			}
			res.Skipped = append(res.Skipped, l.LineID)
			continue
		}
		chosen = append(chosen, l)
	}
	if len(want) > 0 {
		return nil, wsErr(wsAlreadyCode, "A chosen line is already shipped or not a drop-ship line.")
	}
	if len(chosen) == 0 {
		return res, nil
	}
	return confirmDropShipLines(tenantID, userID, chosen, res)
}

// confirmDropShipLines ships the chosen lines. The lines were read before the
// transaction, so each is locked and re-read here: one already shipped or
// cancelled by a parallel confirmation is refused, never billed twice.
func confirmDropShipLines(tenantID, userID string, chosen []DropShipLine, res *DropShipConfirmResult) (*DropShipConfirmResult, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	// One bill per purchase order of this confirmation, one order status per order.
	byPO := map[string][]DropShipLine{}
	var poOrder []string
	orders := map[string]bool{}
	for _, l := range chosen {
		if _, ok := byPO[l.POID]; !ok {
			poOrder = append(poOrder, l.POID)
		}
		byPO[l.POID] = append(byPO[l.POID], l)
		orders[l.OrderID] = true
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(time.RFC3339)
	for _, poID := range poOrder {
		po := dsLoadPO(schema, poID)
		if po == nil {
			return nil, wsErr(wsPONotReadyCode, "Purchase order %s was not found.", poID)
		}
		poLines, _ := parsePOLineMaps(strField(po.data, "items"))
		rate := map[string]float64{}
		for _, pl := range poLines {
			rate[strField(pl, "sales_line_id")] = numFromInterface(pl["rate"])
		}
		var total int64
		var bill []map[string]interface{}
		var gstLines []GSTLineInput
		for _, l := range byPO[poID] {
			value := RupeesToPaise(rate[l.LineID] * l.Qty)
			total += value
			gl := GSTLineInput{Sku: l.Sku, Qty: int(l.Qty), UnitRate: rate[l.LineID]}
			purchaseLineITCInto(tenantID, po.data, POLineInput{SKU: l.Sku}, &gl)
			gstLines = append(gstLines, gl)
			bill = append(bill, map[string]interface{}{"order": l.OrderID, "sku": l.Sku, "qty": l.Qty, "rate": rate[l.LineID], "value": PaiseToRupees(value)})
			var raw []byte
			if err := tx.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents WHERE doctype = 'SalesOrderLine' AND id = $1 FOR UPDATE`, schema), l.LineID).Scan(&raw); err != nil {
				return nil, err
			}
			var ld map[string]interface{}
			_ = json.Unmarshal(raw, &ld)
			if st := strField(ld, "line_status"); st == "Dispatched" || st == "Cancelled" {
				return nil, wsErr(wsAlreadyCode, "A chosen line is already shipped or cancelled.")
			}
			ld["line_status"], ld["shipped_qty"], ld["dropship_confirmed_at"], ld["dropship_confirmed_by"] = "Dispatched", l.Qty, now, userID
			b, _ := json.Marshal(ld)
			if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $1, status = 'Dispatched', updated_at = CURRENT_TIMESTAMP WHERE doctype = 'SalesOrderLine' AND id = $2`, schema), b, l.LineID); err != nil {
				return nil, err
			}
			res.Confirmed = append(res.Confirmed, l.LineID)
		}
		// The PO is exclusive of GST: the input tax follows the items' tax class,
		// as the PO's own lines do, and the vendor is owed taxable plus tax.
		bd, gerr := ComputeGSTForLinesMode(tenantID, gstLines, truthy(po.data["interstate"]), GSTModeExclusive)
		if gerr != nil {
			return nil, wsErr(wsBillCode, "The vendor bill could not be made: %v", gerr)
		}
		tax := RupeesToPaise(bd.TotalTax)
		blocked := RupeesToPaise(bd.BlockedTax)
		if blocked > tax {
			blocked = tax
		}
		eligible := tax - blocked
		gross := total + tax
		billID := NewDocID("VI")
		vendor := firstNonEmpty(strField(po.data, "vendor_id"), strField(po.data, "vendor"))
		doc := map[string]interface{}{
			"code": billID, "invoice_number": billID, "vendor_id": vendor, "invoice_amount": PaiseToRupees(gross),
			"gst_amount": PaiseToRupees(eligible), "gst_total": PaiseToRupees(tax), "itc_blocked_amount": PaiseToRupees(blocked),
			"taxable_amount": PaiseToRupees(gross - eligible),
			"invoice_date":   BusinessToday(), "financial_year": documentFinancialYear(time.Now()), "status": "Matched",
			"source_doctype": "PurchaseOrder", "source_doc_id": poID, "dropship_order_id": strField(po.data, "dropship_order_id"),
			"dropship_lines": bill, "payment_basis_paise": map[string]int64{"2100": gross}, "payment_basis_exact": true,
		}
		raw, _ := json.Marshal(doc)
		if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'VendorInvoice', $2, 'Matched', $3)`, schema), billID, raw, userID); err != nil {
			return nil, wsErr(wsBillCode, "The vendor bill could not be made: %v", err)
		}
		if gross > 0 {
			debits := map[string]int64{"5100": total + blocked}
			if eligible > 0 {
				debits["1500"] = eligible
			}
			if err := PostDoubleEntryTx(tx, tenantID, schema, "VendorInvoice", billID, debits, map[string]int64{"2100": gross}, "",
				fmt.Sprintf("VendorInvoice:%s:DROPSHIP", billID)); err != nil {
				return nil, wsErr(wsBillCode, "The cost could not be posted: %v", err)
			}
		}
		res.Bills = append(res.Bills, billID)
	}
	// An order whose lines are all shipped is Shipped.
	var shipped []string
	for orderID := range orders {
		var open int
		if err := tx.QueryRow(fmt.Sprintf(`SELECT count(*) FROM %s.documents WHERE doctype = 'SalesOrderLine' AND deleted_at IS NULL AND data->>'order_id' = $1
			 AND COALESCE(data->>'line_status','') NOT IN ('Dispatched','Cancelled')`, schema), orderID).Scan(&open); err != nil {
			return nil, err
		}
		if open == 0 {
			if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = jsonb_set(data, '{order_status}', '"Shipped"'), status = 'Shipped', updated_at = CURRENT_TIMESTAMP
				 WHERE doctype = 'SalesOrder' AND id = $1`, schema), orderID); err != nil {
				return nil, err
			}
			shipped = append(shipped, orderID)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	sort.Strings(shipped)
	for _, orderID := range shipped {
		id, ierr := CreateSalesInvoiceFromOrder(tenantID, orderID, userID)
		if ierr != nil {
			LogSystemError(tenantID, "", "Medium", "Drop-ship", fmt.Sprintf("order %s shipped but not invoiced: %v", orderID, ierr), "")
		} else if id != "" {
			res.Invoices = append(res.Invoices, id)
		}
	}
	sort.Strings(res.Confirmed)
	LogAuditEvent(tenantID, userID, "DROPSHIP_CONFIRMED", "SUCCESS", fmt.Sprintf("%d drop-ship lines shipped by the vendor, bills %s", len(res.Confirmed), strings.Join(res.Bills, ", ")))
	return res, nil
}

// cancelDropShipOrder runs when an order is cancelled: its Draft purchase
// orders go with it (a PO left alive would still be shipped), and a PO past
// Draft refuses the cancel. Not a drop-ship order, or the module off: nothing.
func cancelDropShipOrder(tenantID, schema, orderID string, order map[string]interface{}) error {
	if !wholesaleOn(tenantID) || strField(order, "dropship") != "Yes" {
		return nil
	}
	lines, err := dsOrderLines(schema, orderID)
	if err != nil {
		return err
	}
	pos := dsPOsOf(schema, orderID)
	for _, p := range pos {
		if p.status != "Draft" {
			return wsErr(wsPOLockedCode, "Cancel the PO first.")
		}
	}
	for _, l := range lines {
		l.data["line_status"] = "Cancelled" // the order's own cancel writes the lines
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, p := range pos {
		if err := dsResyncPO(tenantID, tx, schema, p, lines); err != nil {
			return err
		}
	}
	return tx.Commit()
}
