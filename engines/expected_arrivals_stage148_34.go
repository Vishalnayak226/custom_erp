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

// Stage 148.34 - Expected arrivals.
//
// One list of what is due to come in: Approved PO lines (each instalment of a
// line, or the PO's expected delivery date, 148.11) and transfers in transit,
// per store and day, late first. A row carries the document and its type, so
// the report screen opens the PO or the transfer, where the existing receive
// screens take part of it. The report is cut to a store user's own store by
// the report framework (StoreKey).
//
// A changed date re-opens the vendor's confirmation instead of dropping the
// line: the PO's expected delivery date on a line with no instalments, or a
// line's instalment dates (reopenPOConfirmations, run at the PO save choke
// point, and SetPOLineControl for the direct edit). The line stays listed, with
// its confirmation shown as "Re-opened".
//
// A transfer has no due date of its own: with transfers.in_transit_alert_days
// above 0 it is due that many days after it was dispatched (and late after);
// with it at 0 it is listed as "In transit", never late.

func init() {
	RegisterReport(ReportDefinition{ID: "expected-arrivals", Label: "Expected arrivals", Category: "Procurement",
		Params: []ReportParam{{Key: "as_of", Label: "As of", Type: "date"}},
		Columns: []ReportColumn{{Key: "due", Label: "Due"}, {Key: "store", Label: "Store"}, {Key: "state", Label: "State"},
			{Key: "kind", Label: "Kind"}, {Key: "doc_no", Label: "No.", Type: "doclink"}, {Key: "line_no", Label: "Line"},
			{Key: "from", Label: "Vendor / from"}, {Key: "sku", Label: "Item"}, {Key: "name", Label: "Name"},
			{Key: "due_qty", Label: "Due qty"}, {Key: "pending", Label: "Still to come"},
			{Key: "confirmation", Label: "Vendor confirmation"}, {Key: "confirmation_no", Label: "Confirmation no."}},
		Run: func(tenantID string, p map[string]string) ([]map[string]interface{}, error) {
			return ExpectedArrivals(tenantID, p["as_of"])
		}, StoreKey: "store"})
}

func arrivalState(due string, asOf time.Time) string {
	if due == "" {
		return "No date"
	}
	d, err := time.Parse("2006-01-02", due)
	if err != nil {
		return "No date"
	}
	switch days := int(math.Round(asOf.Sub(d).Hours() / 24)); {
	case days > 0:
		return fmt.Sprintf("Late %d days", days)
	case days == 0:
		return "Due today"
	}
	return "Upcoming"
}

func confirmationState(l map[string]interface{}) string {
	switch {
	case strField(l, "confirmation_no") != "" || strField(l, "confirmation_date") != "":
		return "Confirmed"
	case strField(l, "confirmation_reopened_at") != "":
		return "Re-opened"
	}
	return ""
}

// ExpectedArrivals lists what is still to come in, late first within a store.
func ExpectedArrivals(tenantID, asOf string) ([]map[string]interface{}, error) {
	if strings.TrimSpace(asOf) == "" {
		asOf = BusinessToday()
	}
	asOfDay, err := time.Parse("2006-01-02", asOf)
	if err != nil {
		return nil, poLCBad("the date must be YYYY-MM-DD")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	type doc struct {
		id, doctype string
		data        map[string]interface{}
	}
	var docs []doc
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, doctype, data FROM %s.documents
		WHERE deleted_at IS NULL AND ((doctype = 'PurchaseOrder' AND status = 'Approved')
		   OR (doctype = 'TransferOrder' AND status IN ('Dispatched', 'Partially Received')))
		ORDER BY created_at, id`, schema))
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id, dt, raw string
		if err := rows.Scan(&id, &dt, &raw); err != nil {
			rows.Close()
			return nil, err
		}
		var d map[string]interface{}
		if json.Unmarshal([]byte(raw), &d) == nil {
			docs = append(docs, doc{id, dt, d})
		}
	}
	rows.Close()

	names := map[string]string{}
	nameOf := func(sku string) string {
		if n, ok := names[sku]; ok {
			return n
		}
		n := ""
		if it, err := ResolveItemBySKU(tenantID, sku); err == nil {
			n = strField(it.Data, "name")
		}
		names[sku] = n
		return n
	}
	transitDays := GetSettingInt(tenantID, SettingTransitAlertDays)
	out := []map[string]interface{}{}
	for _, d := range docs {
		if d.doctype == "TransferOrder" {
			bal, err := loadTransferBalances(d.data)
			if err != nil {
				continue
			}
			due := ""
			if transitDays > 0 {
				if at, err := time.Parse(time.RFC3339, strField(d.data, "dispatched_at")); err == nil {
					due = at.AddDate(0, 0, transitDays).Format("2006-01-02")
				}
			}
			state := "In transit"
			if due != "" {
				state = arrivalState(due, asOfDay)
			}
			for i, b := range bal {
				if b.Open() <= 0 {
					continue
				}
				out = append(out, map[string]interface{}{
					"due": due, "store": strField(d.data, "to_warehouse"), "state": state, "kind": "Transfer",
					"doc_no": d.id, "doc_no_doctype": "TransferOrder", "doc_no_id": d.id, "line_no": i + 1,
					"from": strField(d.data, "from_warehouse"), "sku": b.Sku, "name": nameOf(b.Sku),
					"due_qty": b.Dispatched, "pending": b.Open(), "confirmation": "", "confirmation_no": ""})
			}
			continue
		}
		lines, err := parsePOLineMaps(strField(d.data, "items"))
		if err != nil || len(lines) == 0 {
			continue
		}
		open, taken, err := poLineOpenQty(tenantID, d.id, lines)
		if err != nil {
			continue
		}
		store := firstNonEmpty(strField(d.data, "target_warehouse"), strField(d.data, "location"))
		poNo := firstNonEmpty(strField(d.data, "po_number"), strField(d.data, "code"), d.id)
		for i, l := range lines {
			if stopped, _ := l["stopped"].(bool); stopped || open[i] <= 1e-9 {
				continue
			}
			qty, closed := poLineDueQty(l), numFromInterface(l["closed_qty"])
			var inst []POInstalment
			if raw, ok := l["instalments"]; ok {
				b, _ := json.Marshal(raw)
				_ = json.Unmarshal(b, &inst)
			}
			if len(inst) == 0 {
				inst = []POInstalment{{Qty: qty, Date: strField(d.data, "expected_delivery_date")}}
			}
			sort.SliceStable(inst, func(a, b int) bool { return inst[a].Date < inst[b].Date })
			pend := make([]float64, len(inst))
			rem := taken[i]
			for j, in := range inst { // what came in fills the earliest instalments
				fill := math.Min(rem, in.Qty)
				rem -= fill
				pend[j] = in.Qty - fill
			}
			cut := closed
			for j := len(pend) - 1; j >= 0 && cut > 0; j-- { // what was closed cuts the latest
				c := math.Min(cut, pend[j])
				pend[j] -= c
				cut -= c
			}
			sku := strField(l, "sku")
			for j, in := range inst {
				if pend[j] <= 1e-9 {
					continue
				}
				out = append(out, map[string]interface{}{
					"due": in.Date, "store": store, "state": arrivalState(in.Date, asOfDay), "kind": "Purchase order",
					"doc_no": poNo, "doc_no_doctype": "PurchaseOrder", "doc_no_id": d.id, "line_no": i + 1,
					"from": firstNonEmpty(strField(d.data, "vendor"), strField(d.data, "vendor_id")), "sku": sku, "name": nameOf(sku),
					"due_qty": in.Qty, "pending": pend[j], "confirmation": confirmationState(l), "confirmation_no": strField(l, "confirmation_no")})
			}
		}
	}
	// per store; late first (the earliest date first), then those with no date
	sort.SliceStable(out, func(a, b int) bool {
		sa, sb := out[a]["store"].(string), out[b]["store"].(string)
		if sa != sb {
			return sa < sb
		}
		da, db2 := out[a]["due"].(string), out[b]["due"].(string)
		if (da == "") != (db2 == "") {
			return da != ""
		}
		return da < db2
	})
	return out, nil
}

// --- a changed date re-opens the vendor's confirmation ------------------------------

func poInstalmentDates(l map[string]interface{}) string {
	raw, ok := l["instalments"]
	if !ok {
		return ""
	}
	var inst []POInstalment
	b, _ := json.Marshal(raw)
	_ = json.Unmarshal(b, &inst)
	ds := make([]string, 0, len(inst))
	for _, in := range inst {
		ds = append(ds, in.Date)
	}
	sort.Strings(ds)
	return strings.Join(ds, ",")
}

// reopenConfirmation clears a line's vendor confirmation and marks it re-opened.
func reopenConfirmation(l map[string]interface{}) bool {
	if strField(l, "confirmation_no") == "" && strField(l, "confirmation_date") == "" {
		return false
	}
	delete(l, "confirmation_no")
	delete(l, "confirmation_date")
	l["confirmation_reopened_at"] = time.Now().UTC().Format(time.RFC3339)
	return true
}

// reopenPOConfirmations runs on a PO save, after the line control is carried:
// when the PO's expected delivery date changed, a line with no instalments of
// its own, and when a line's instalment dates changed, that line, goes back to
// "waiting for the vendor to confirm". A save that sets a new confirmation
// keeps it. The line is never dropped from the arrivals list.
func reopenPOConfirmations(priorData, payload map[string]interface{}) {
	if priorData == nil {
		return
	}
	priorItems := strField(priorData, "items")
	itemsStr, hasItems := payload["items"].(string)
	if !hasItems || itemsStr == "" {
		itemsStr = priorItems
	}
	prior, err1 := parsePOLineMaps(priorItems)
	lines, err2 := parsePOLineMaps(itemsStr)
	if err1 != nil || err2 != nil || len(prior) == 0 || len(lines) == 0 {
		return
	}
	dateMoved := false
	if v, ok := payload["expected_delivery_date"]; ok {
		dateMoved = strings.TrimSpace(fmt.Sprintf("%v", v)) != strField(priorData, "expected_delivery_date")
	}
	used := make([]bool, len(prior))
	changed := false
	for _, l := range lines {
		var p map[string]interface{}
		for pi, c := range prior {
			if !used[pi] && strField(c, "sku") == strField(l, "sku") {
				used[pi], p = true, c
				break
			}
		}
		if p == nil {
			continue
		}
		if strField(l, "confirmation_no") != strField(p, "confirmation_no") || strField(l, "confirmation_date") != strField(p, "confirmation_date") {
			if strField(l, "confirmation_reopened_at") != "" {
				delete(l, "confirmation_reopened_at") // a new confirmation was given
				changed = true
			}
			continue
		}
		moved := poInstalmentDates(l) != poInstalmentDates(p)
		if !moved && dateMoved && poInstalmentDates(l) == "" {
			moved = true
		}
		if moved && reopenConfirmation(l) {
			changed = true
		}
	}
	if changed {
		b, _ := json.Marshal(lines)
		payload["items"] = string(b)
	}
}
