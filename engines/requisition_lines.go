package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// Stage 148.14 - requisition with item lines.
//
// A PurchaseRequisition may carry "items": [{sku, qty, store, vendor_id?,
// est_rate?}], each line with its own store. A request with no lines is the
// header-only request it always was (description, quantity, amount).
//
// "Combine open requests for this vendor" takes every open line of that
// vendor from the Approved requests and makes a Draft PO per store (a PO is
// received into one store). Each PO line carries requisition_id and
// requisition_line, each request line carries po_id and po_line, and the PO
// names its requests in source_requisition_ids, so the trace reads both ways.
// The request rows are locked in id order, so two people pressing at once
// cannot take one line twice: the second finds nothing open (PURCHA-5013).
// A line whose PO was cancelled is open again. A request becomes Converted
// once every line is on a PO.
//
// PURCHA-5010 a line is not valid     PURCHA-5011 a header-only request is incomplete
// PURCHA-5012 a line already on a PO cannot change
// PURCHA-5013 nothing open to combine   PURCHA-5014 the vendor cannot be used
// PURCHA-5016 a request with lines goes to a PO only through Combine

func reqBad(code, format string, a ...interface{}) error {
	return &ValidationError{Code: code, Message: fmt.Sprintf(format, a...)}
}

// requisitionLines reads a request's lines whatever shape the caller sent.
func reqLinesOf(v interface{}) ([]map[string]interface{}, error) {
	switch x := v.(type) {
	case nil:
		return nil, nil
	case string:
		return parsePOLineMaps(x)
	default:
		b, err := json.Marshal(x)
		if err != nil {
			return nil, err
		}
		return parsePOLineMaps(string(b))
	}
}

func reqHasLines(data map[string]interface{}) bool {
	ls, err := reqLinesOf(data["items"])
	return err == nil && len(ls) > 0
}

// PrepareRequisitionLines checks and tidies a request's lines and fills the
// header from them; with no lines a new request must have its description,
// quantity and amount, as before. It runs at the shared validation choke
// point, so the form, the API and an import all pass through it.
func PrepareRequisitionLines(tenantID string, isCreate bool, payload map[string]interface{}) error {
	lines, err := reqLinesOf(payload["items"])
	if err != nil {
		return reqBad("PURCHA-5010", "the item lines are not readable")
	}
	if len(lines) == 0 {
		if isCreate {
			for _, k := range []string{"description", "quantity", "total_amount"} {
				if strings.TrimSpace(fmt.Sprintf("%v", payload[k])) == "" || payload[k] == nil {
					return reqBad("PURCHA-5011", "a requisition needs item lines, or a description, a quantity and an estimated amount")
				}
			}
		}
		return nil
	}
	var qty, amount float64
	anyRate := false
	out := make([]map[string]interface{}, 0, len(lines))
	for i, l := range lines {
		no := i + 1
		sku := strings.TrimSpace(strField(l, "sku"))
		if sku == "" {
			return reqBad("PURCHA-5010", "line %d: choose the item", no)
		}
		if it, err := ResolveItemBySKU(tenantID, sku); err != nil || it == nil {
			return reqBad("PURCHA-5010", "line %d: %q is not an item", no, sku)
		}
		q := numFromInterface(l["qty"])
		if !(q > 0) || q != math.Trunc(q) {
			return reqBad("PURCHA-5010", "line %d: the quantity must be a whole number above zero", no)
		}
		storeRef := strings.TrimSpace(strField(l, "store"))
		if storeRef == "" {
			return reqBad("PURCHA-5010", "line %d: choose the store", no)
		}
		store, err := ResolveLocationReference(tenantID, storeRef)
		if err != nil {
			return reqBad("PURCHA-5010", "line %d: %v", no, err)
		}
		nl := map[string]interface{}{"sku": sku, "qty": q, "store": store}
		if v := strings.TrimSpace(strField(l, "vendor_id")); v != "" {
			if _, ok := vendorNameOf(tenantID, v); !ok {
				return reqBad("PURCHA-5010", "line %d: vendor %q is not an active vendor", no, v)
			}
			nl["vendor_id"] = v
		}
		if r := numFromInterface(l["est_rate"]); r > 0 {
			nl["est_rate"] = r
			anyRate = true
			amount += r * q
		} else if r < 0 {
			return reqBad("PURCHA-5010", "line %d: the estimated rate cannot be negative", no)
		}
		for _, k := range []string{"po_id", "po_line"} { // the trace is kept as it is
			if v, ok := l[k]; ok && v != nil && fmt.Sprintf("%v", v) != "" {
				nl[k] = v
			}
		}
		qty += q
		out = append(out, nl)
	}
	b, _ := json.Marshal(out)
	payload["items"] = string(b)
	payload["quantity"] = qty
	if strings.TrimSpace(fmt.Sprintf("%v", payload["description"])) == "" || payload["description"] == nil {
		payload["description"] = fmt.Sprintf("%s x %v%s", strField(out[0], "sku"), out[0]["qty"],
			map[bool]string{true: fmt.Sprintf(" and %d more", len(out)-1), false: ""}[len(out) > 1])
	}
	if anyRate {
		payload["total_amount"] = round2(amount)
	} else if payload["total_amount"] == nil || strings.TrimSpace(fmt.Sprintf("%v", payload["total_amount"])) == "" {
		payload["total_amount"] = 0.0
	}
	return nil
}

// validateRequisitionLineEdit keeps a line that is already on a PO as it is,
// and puts the trace back when the form sends the lines without it.
func validateRequisitionLineEdit(priorData, payload map[string]interface{}) error {
	if priorData == nil {
		return nil
	}
	prior, err := reqLinesOf(priorData["items"])
	if err != nil || len(prior) == 0 {
		return nil
	}
	taken := false
	for _, l := range prior {
		if strField(l, "po_id") != "" {
			taken = true
		}
	}
	if !taken {
		return nil
	}
	if _, sent := payload["items"]; !sent {
		return nil
	}
	now, err := reqLinesOf(payload["items"])
	if err != nil {
		return reqBad("PURCHA-5010", "the item lines are not readable")
	}
	if len(now) != len(prior) {
		return reqBad("PURCHA-5012", "lines already on a purchase order cannot be changed")
	}
	same := func(a, b map[string]interface{}) bool {
		return strField(a, "sku") == strField(b, "sku") && numFromInterface(a["qty"]) == numFromInterface(b["qty"]) &&
			strField(a, "store") == strField(b, "store") && strField(a, "vendor_id") == strField(b, "vendor_id") &&
			numFromInterface(a["est_rate"]) == numFromInterface(b["est_rate"])
	}
	for i := range prior {
		if !same(prior[i], now[i]) {
			return reqBad("PURCHA-5012", "line %d is already on a purchase order and cannot be changed", i+1)
		}
		for _, k := range []string{"po_id", "po_line"} {
			if v, ok := prior[i][k]; ok {
				now[i][k] = v
			}
		}
	}
	b, _ := json.Marshal(now)
	payload["items"] = string(b)
	return nil
}

// CombinedPO is one PO a combine made.
type CombinedPO struct {
	POID         string   `json:"po_id"`
	Store        string   `json:"store"`
	Lines        int      `json:"lines"`
	Requisitions []string `json:"requisitions"`
}

// CombineResult is what a combine made.
type CombineResult struct {
	Vendor    string       `json:"vendor"`
	Documents []CombinedPO `json:"documents"`
}

// CombineRequisitionLines makes the open lines of one vendor into a Draft PO
// per store (storeFilter limits it to one store). See the file note.
func CombineRequisitionLines(tenantID, userID, vendor, storeFilter string) (*CombineResult, error) {
	vendor = strings.TrimSpace(vendor)
	if vendor == "" {
		return nil, reqBad("PURCHA-5014", "choose the vendor to combine the open requests for")
	}
	if _, ok := vendorNameOf(tenantID, vendor); !ok {
		return nil, reqBad("PURCHA-5014", "choose an active vendor to combine the open requests for")
	}
	if err := RequirePOVendorState(tenantID, map[string]interface{}{"vendor": vendor}); err != nil {
		return nil, err
	}
	if err := checkVendorNoNewPO(tenantID, vendor); err != nil { // 148.9: not for a held vendor
		return nil, err
	}
	if strings.TrimSpace(storeFilter) != "" {
		s, err := ResolveLocationReference(tenantID, storeFilter)
		if err != nil {
			return nil, reqBad("PURCHA-5014", "%v", err)
		}
		storeFilter = s
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id FROM %s.documents
		WHERE doctype = 'PurchaseRequisition' AND status = 'Approved' AND deleted_at IS NULL
		  AND COALESCE(data->>'items', '') NOT IN ('', '[]') ORDER BY id`, schema))
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()

	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	type req struct {
		id    string
		data  map[string]interface{}
		lines []map[string]interface{}
	}
	type pick struct {
		r  *req
		ix int
	}
	var reqs []*req
	byStore := map[string][]pick{}
	var storeOrder []string
	poLive := map[string]bool{}
	live := func(po string) bool {
		if v, ok := poLive[po]; ok {
			return v
		}
		var st string
		err := tx.QueryRow(fmt.Sprintf(`SELECT COALESCE(status, '') FROM %s.documents
			WHERE doctype = 'PurchaseOrder' AND id = $1 AND deleted_at IS NULL`, schema), po).Scan(&st)
		ok := err == nil && st != "Cancelled" && st != "Rejected"
		poLive[po] = ok
		return ok
	}
	for _, id := range ids { // id order: the same order for every presser, so no deadlock
		d, st, err := lockDocTx(tx, schema, "PurchaseRequisition", id)
		if err != nil {
			continue
		}
		if st != "Approved" {
			continue
		}
		ls, err := reqLinesOf(d["items"])
		if err != nil {
			continue
		}
		r := &req{id: id, data: d, lines: ls}
		reqs = append(reqs, r)
		for i, l := range ls {
			if strField(l, "vendor_id") != vendor {
				continue
			}
			if po := strField(l, "po_id"); po != "" && live(po) {
				continue
			}
			store := strField(l, "store")
			if storeFilter != "" && store != storeFilter {
				continue
			}
			if _, ok := byStore[store]; !ok {
				storeOrder = append(storeOrder, store)
			}
			byStore[store] = append(byStore[store], pick{r, i})
		}
	}
	if len(storeOrder) == 0 {
		return nil, reqBad("PURCHA-5013", "no open request lines for this vendor")
	}
	sort.Strings(storeOrder)

	fy := documentFinancialYear(time.Now())
	res := &CombineResult{Vendor: vendor}
	touched := map[string]*req{}
	for _, store := range storeOrder {
		picks := byStore[store]
		poID, err := GenerateSequence(tenantID, "PO", store, fy)
		if err != nil {
			return nil, err
		}
		var pl []proposalPOLine
		for _, p := range picks {
			l := p.r.lines[p.ix]
			cost := numFromInterface(l["est_rate"])
			if cost <= 0 {
				cost = quoteCostRupees(tenantID, strField(l, "sku"))
			}
			pl = append(pl, proposalPOLine{SKU: strField(l, "sku"), Qty: int(numFromInterface(l["qty"])), Cost: cost})
		}
		itemsJSON, total, err := PriceProposalPOLines(tenantID, vendor, pl)
		if err != nil {
			return nil, err
		}
		poLines, err := parsePOLineMaps(itemsJSON)
		if err != nil || len(poLines) != len(picks) {
			return nil, reqBad("PURCHA-5010", "the purchase order lines could not be built")
		}
		var reqIDs []string
		seen := map[string]bool{}
		for i, p := range picks {
			poLines[i]["requisition_id"] = p.r.id
			poLines[i]["requisition_line"] = p.ix + 1
			p.r.lines[p.ix]["po_id"] = poID
			p.r.lines[p.ix]["po_line"] = i + 1
			touched[p.r.id] = p.r
			if !seen[p.r.id] {
				seen[p.r.id] = true
				reqIDs = append(reqIDs, p.r.id)
			}
		}
		rawLines, _ := json.Marshal(poLines)
		po := map[string]interface{}{
			"code": poID, "po_number": poID, "vendor": vendor, "vendor_id": vendor, "location": store,
			"target_warehouse": store, "items": string(rawLines), "total_amount": round2(total), "gst_mode": GSTModeExclusive,
			"source_requisition_ids": strings.Join(reqIDs, ","), "status": "Draft",
		}
		if err := RequirePOStoreState(tenantID, po); err != nil {
			return nil, err
		}
		ApplyPlaceOfSupply(tenantID, po)
		if err := ValidateDocument(tenantID, "PurchaseOrder", po); err != nil {
			return nil, err
		}
		b, _ := json.Marshal(po)
		if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'PurchaseOrder', $2, 'Draft', $3)`, schema),
			poID, b, userID); err != nil {
			return nil, err
		}
		res.Documents = append(res.Documents, CombinedPO{POID: poID, Store: store, Lines: len(picks), Requisitions: reqIDs})
	}
	for _, r := range touched {
		open := false
		var pos []string
		seen := map[string]bool{}
		for _, l := range r.lines {
			po := strField(l, "po_id")
			if po == "" || !(live(po) || isNewPO(res, po)) {
				open = true
				continue
			}
			if !seen[po] {
				seen[po] = true
				pos = append(pos, po)
			}
		}
		sort.Strings(pos)
		b, _ := json.Marshal(r.lines)
		r.data["items"] = string(b)
		r.data["po_ids"] = strings.Join(pos, ",")
		status := "Approved"
		if !open {
			status = "Converted"
			r.data["converted_to_doctype"] = "PurchaseOrder"
			r.data["converted_to_id"] = strings.Join(pos, ",")
		}
		if err := writeDocTx(tx, schema, "PurchaseRequisition", r.id, r.data, status); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	var made []string
	for _, d := range res.Documents {
		made = append(made, d.POID)
	}
	LogAuditEvent(tenantID, userID, "COMBINE_REQUISITIONS", "SUCCESS",
		fmt.Sprintf("Combined open requisition lines for vendor %s into %s", vendor, strings.Join(made, ", ")))
	return res, nil
}

func isNewPO(res *CombineResult, po string) bool {
	for _, d := range res.Documents {
		if d.POID == po {
			return true
		}
	}
	return false
}
