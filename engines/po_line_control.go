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

// Stage 148.11 - PO line control.
//
// Each PO line may carry, inside the PO's own "items" JSON (so nothing in the
// schema changes and a PO that sets none of it behaves exactly as before):
//
//	instalments        [{qty, date}] - when each part is due; they add up to the line
//	confirmation_no / confirmation_date - the vendor's confirmation
//	under_pct / over_pct - the line's own tolerance; blank = the item's, read LIVE
//	                       at receiving (D365 copies it once and never again)
//	full_delivery_only - part deliveries are refused
//	closed_qty         - the rest of the line that will never come (stop, close
//	                     rest, or auto-close within the under tolerance)
//	stopped            - the line is stopped: no receipt, and its rest is closed
//
// Everything that reads "what is still open on a PO" goes through
// fetchPOItemQuantities, which nets closed_qty off, so a closed line stops
// holding the PO open and every receiving path (GRN rules, the step flow's
// Verify, the reorder position) sees the same number.

// POInstalment is one part delivery of a PO line.
type POInstalment struct {
	Qty  float64 `json:"qty"`
	Date string  `json:"date"`
}

// The keys a PO line may carry for line control.
var poLineControlKeys = []string{"instalments", "confirmation_no", "confirmation_date", "under_pct", "over_pct",
	"full_delivery_only", "closed_qty", "closed_reason", "closed_note", "closed_at", "closed_by", "closed_auto",
	"stopped", "stop_reason", "stop_note", "requisition_id", "requisition_line", "confirmation_reopened_at"}

const (
	poLCItemUnder = "under_delivery_pct"
	poLCItemOver  = "over_delivery_pct"
)

func parsePOLineMaps(itemsStr string) ([]map[string]interface{}, error) {
	var lines []map[string]interface{}
	if strings.TrimSpace(itemsStr) == "" {
		return nil, nil
	}
	if err := json.Unmarshal([]byte(itemsStr), &lines); err != nil {
		return nil, err
	}
	return lines, nil
}

// pctFrom reads a percentage that may be absent, null or blank.
func pctFrom(v interface{}) *float64 {
	switch x := v.(type) {
	case nil:
		return nil
	case string:
		if strings.TrimSpace(x) == "" {
			return nil
		}
	}
	n := numFromInterface(v)
	return &n
}

// itemTolerance is the item's under / over delivery % (nil = not set).
func itemTolerance(tenantID, sku string) (under, over *float64) {
	it, err := ResolveItemBySKU(tenantID, sku)
	if err != nil || it == nil {
		return nil, nil
	}
	return pctFrom(it.Data[poLCItemUnder]), pctFrom(it.Data[poLCItemOver])
}

// poSkuControl is a PO's line control for one SKU, with the tolerance
// resolved: the line's own, else the item's (read now), else nil.
type poSkuControl struct {
	Closed   float64
	Stopped  bool
	Under    *float64
	Over     *float64
	FullOnly bool
}

// poSkuControlsFromItems resolves the control per SKU from a PO's items JSON.
// A PO with no control keys yields entries that change nothing.
func poSkuControlsFromItems(tenantID, itemsStr string) map[string]*poSkuControl {
	lines, err := parsePOLineMaps(itemsStr)
	if err != nil {
		return nil
	}
	out := map[string]*poSkuControl{}
	stoppedAll := map[string]bool{}
	for _, l := range lines {
		sku := strField(l, "sku")
		if sku == "" {
			continue
		}
		c := out[sku]
		if c == nil {
			c = &poSkuControl{}
			out[sku] = c
			stoppedAll[sku] = true
		}
		c.Closed += numFromInterface(l["closed_qty"])
		if b, _ := l["stopped"].(bool); !b {
			stoppedAll[sku] = false
		}
		if b, _ := l["full_delivery_only"].(bool); b {
			c.FullOnly = true
		}
		if c.Under == nil {
			c.Under = pctFrom(l["under_pct"])
		}
		if c.Over == nil {
			c.Over = pctFrom(l["over_pct"])
		}
	}
	for sku, c := range out {
		c.Stopped = stoppedAll[sku]
		if c.Under == nil || c.Over == nil {
			iu, io := itemTolerance(tenantID, sku)
			if c.Under == nil {
				c.Under = iu
			}
			if c.Over == nil {
				c.Over = io
			}
		}
	}
	return out
}

func poSkuControls(tenantID, poID string) map[string]*poSkuControl {
	po, _, err := fetchDocData(tenantID, "PurchaseOrder", poID)
	if err != nil {
		return nil
	}
	return poSkuControlsFromItems(tenantID, strField(po, "items"))
}

// effectiveOverPct is the over-receipt tolerance for a SKU: nothing once the
// line is closed, else the line / item % when set, else the vendor's rule.
func (c *poSkuControl) effectiveOverPct(vendorPct float64) float64 {
	if c == nil {
		return vendorPct
	}
	if c.Closed > 0 {
		return 0
	}
	if c.Over != nil {
		return *c.Over
	}
	return vendorPct
}

func (c *poSkuControl) underPct() float64 {
	if c == nil || c.Under == nil {
		return 0
	}
	return *c.Under
}

// fullDeliveryShort reports a full-delivery-only line whose delivery (all
// receipts of the SKU so far, this one included) is below ordered less the
// under tolerance.
func (c *poSkuControl) fullDeliveryShort(ordered, delivered float64) bool {
	if c == nil || !c.FullOnly || c.Closed > 0 || ordered <= 0 {
		return false
	}
	return delivered+1e-9 < ordered*(1-c.underPct()/100)
}

func fullDeliveryError(sku, poID string, ordered, delivered float64) error {
	return &ValidationError{Code: "PURCHA-4986", Message: fmt.Sprintf(
		"SKU %q on PO %s is full delivery only: %v of %v would be delivered - the vendor must deliver the whole line", sku, poID, delivered, ordered)}
}

func stoppedLineError(sku, poID string) error {
	return &ValidationError{Code: "PURCHA-4985", Message: fmt.Sprintf("SKU %q on PO %s is stopped - no more can be received on it", sku, poID)}
}

// grnFullDeliveryCheck is the step flow's full-delivery rule, run at Verify
// (the receipt in steps skips the save-time PO checks).
func grnFullDeliveryCheck(tenantID string, g *stepGRN) error {
	headerPO := strField(g.data, "po_id")
	type key struct{ po, sku string }
	got := map[key]float64{}
	var order []key
	for _, l := range g.lines {
		k := key{firstNonEmpty(strField(l, "po_id"), headerPO), strField(l, "sku")}
		if k.po == "" {
			continue
		}
		if _, seen := got[k]; !seen {
			order = append(order, k)
		}
		got[k] += grnGRNQty(l)
	}
	ctls := map[string]map[string]*poSkuControl{}
	for _, k := range order {
		if _, ok := ctls[k.po]; !ok {
			ctls[k.po] = poSkuControls(tenantID, k.po)
		}
		c := ctls[k.po][k.sku]
		if c == nil || !c.FullOnly {
			continue
		}
		o, err := fetchPOItemQuantities(tenantID, k.po)
		if err != nil {
			continue
		}
		before, err := fetchGRNReceivedQuantities(tenantID, k.po, g.id)
		if err != nil {
			return err
		}
		if c.fullDeliveryShort(o[k.sku], before[k.sku]+got[k]) {
			return fullDeliveryError(k.sku, k.po, o[k.sku], before[k.sku]+got[k])
		}
	}
	return nil
}

// --- validating and carrying line control on a PO save -----------------------

// validatePOLineControl checks the control keys of a PO's lines.
func validatePOLineControl(itemsStr string) error {
	lines, err := parsePOLineMaps(itemsStr)
	if err != nil {
		return nil // a malformed items list is the PO's own validation's to refuse
	}
	for i, l := range lines {
		if err := validateOnePOLine(i+1, l); err != nil {
			return err
		}
	}
	return nil
}

func poLCBad(format string, a ...interface{}) error {
	return &ValidationError{Code: "PURCHA-4987", Message: fmt.Sprintf(format, a...)}
}

func validateOnePOLine(no int, l map[string]interface{}) error {
	for _, k := range []string{"under_pct", "over_pct"} {
		if p := pctFrom(l[k]); p != nil && (*p < 0 || *p > 100 || math.IsNaN(*p)) {
			return poLCBad("line %d: %s must be between 0 and 100", no, strings.ReplaceAll(k, "_", " "))
		}
	}
	if d := strField(l, "confirmation_date"); d != "" {
		if _, err := time.Parse("2006-01-02", d); err != nil {
			return poLCBad("line %d: the confirmation date must be a date (YYYY-MM-DD)", no)
		}
	}
	raw, ok := l["instalments"]
	if !ok || raw == nil {
		return nil
	}
	b, _ := json.Marshal(raw)
	var inst []POInstalment
	if err := json.Unmarshal(b, &inst); err != nil {
		return poLCBad("line %d: the instalments are not readable", no)
	}
	if len(inst) == 0 {
		return nil
	}
	var sum float64
	for _, in := range inst {
		if in.Qty <= 0 {
			return poLCBad("line %d: every instalment needs a quantity above zero", no)
		}
		if _, err := time.Parse("2006-01-02", in.Date); err != nil {
			return poLCBad("line %d: every instalment needs a date (YYYY-MM-DD)", no)
		}
		sum += in.Qty
	}
	if q := numFromInterface(l["qty"]); math.Abs(sum-q) > 1e-9 {
		return poLCBad("line %d: the instalments add up to %v but the line is %v", no, sum, q)
	}
	return nil
}

// carryPOLineControls keeps the control of a line when an amendment rebuilds
// the lines from the PO form, which only knows sku, qty and rate. A line is
// matched to the prior line of the same SKU in order; instalments are kept only
// while they still add up to the new quantity.
func carryPOLineControls(priorItems string, payload map[string]interface{}) {
	newStr, _ := payload["items"].(string)
	if newStr == "" || newStr == priorItems {
		return
	}
	prior, err1 := parsePOLineMaps(priorItems)
	lines, err2 := parsePOLineMaps(newStr)
	if err1 != nil || err2 != nil || len(prior) == 0 {
		return
	}
	used := make([]bool, len(prior))
	changed := false
	for _, l := range lines {
		sku := strField(l, "sku")
		for pi, p := range prior {
			if used[pi] || strField(p, "sku") != sku {
				continue
			}
			used[pi] = true
			for _, k := range poLineControlKeys {
				if _, has := l[k]; has {
					continue
				}
				v, ok := p[k]
				if !ok {
					continue
				}
				if k == "instalments" {
					probe := map[string]interface{}{"qty": l["qty"], "instalments": v}
					if validateOnePOLine(1, probe) != nil {
						continue
					}
				}
				l[k] = v
				changed = true
			}
			break
		}
	}
	if changed {
		b, _ := json.Marshal(lines)
		payload["items"] = string(b)
	}
}

// --- changing a PO's line control ---------------------------------------------

// editPOLines locks the PO, lets mutate change its line maps, and writes the
// items back. The PO must exist and not be Cancelled or Closed.
func editPOLines(tenantID, poID string, mutate func(lines []map[string]interface{}) error) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var raw, status string
	if err := tx.QueryRow(fmt.Sprintf(`SELECT data, status FROM %s.documents WHERE doctype = 'PurchaseOrder' AND id = $1 AND deleted_at IS NULL FOR UPDATE`, schema), poID).Scan(&raw, &status); err != nil {
		return poLCBad("purchase order %s not found", poID)
	}
	if status == "Cancelled" || status == "Closed" {
		return &ValidationError{Code: "PURCHA-0489", Message: fmt.Sprintf("purchase order %s is %s - its lines can no longer be changed", poID, strings.ToLower(status))}
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return err
	}
	lines, err := parsePOLineMaps(strField(data, "items"))
	if err != nil {
		return poLCBad("purchase order %s has unreadable lines", poID)
	}
	if err := mutate(lines); err != nil {
		return err
	}
	items, _ := json.Marshal(lines)
	patch, _ := json.Marshal(map[string]interface{}{"items": string(items)})
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || $1::jsonb, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'PurchaseOrder' AND id = $2`, schema), patch, poID); err != nil {
		return err
	}
	return tx.Commit()
}

func poLineAt(lines []map[string]interface{}, lineNo int) (map[string]interface{}, error) {
	if lineNo < 1 || lineNo > len(lines) {
		return nil, poLCBad("there is no line %d on this purchase order", lineNo)
	}
	return lines[lineNo-1], nil
}

// SetPOLineControl changes the control keys of one line (1-based). A nil or
// blank value clears the key. Quantity and rate never change here, so no
// amendment is needed; the change is audited.
func SetPOLineControl(tenantID, poID, userID string, lineNo int, patch map[string]interface{}) error {
	allowed := map[string]bool{"instalments": true, "confirmation_no": true, "confirmation_date": true,
		"under_pct": true, "over_pct": true, "full_delivery_only": true}
	for k := range patch {
		if !allowed[k] {
			return poLCBad("%q is not a line-control field", k)
		}
	}
	err := editPOLines(tenantID, poID, func(lines []map[string]interface{}) error {
		l, err := poLineAt(lines, lineNo)
		if err != nil {
			return err
		}
		before := poInstalmentDates(l) // 148.34
		defer func() {
			_, setNo := patch["confirmation_no"]
			_, setOn := patch["confirmation_date"]
			if setNo || setOn {
				if strField(l, "confirmation_no") != "" || strField(l, "confirmation_date") != "" {
					delete(l, "confirmation_reopened_at")
				}
			} else if poInstalmentDates(l) != before {
				reopenConfirmation(l)
			}
		}()
		for k, v := range patch {
			if s, ok := v.(string); v == nil || (ok && strings.TrimSpace(s) == "") {
				delete(l, k)
				continue
			}
			if k == "instalments" {
				if arr, ok := v.([]interface{}); ok && len(arr) == 0 {
					delete(l, k)
					continue
				}
				if arr, ok := v.([]map[string]interface{}); ok && len(arr) == 0 {
					delete(l, k)
					continue
				}
			}
			if k == "full_delivery_only" {
				if b, _ := v.(bool); !b {
					delete(l, k)
					continue
				}
			}
			l[k] = v
		}
		return validateOnePOLine(lineNo, l)
	})
	if err != nil {
		return err
	}
	b, _ := json.Marshal(patch)
	LogAuditEvent(tenantID, userID, "PO_LINE_CONTROL", "SUCCESS", fmt.Sprintf("PO %s line %d: %s", poID, lineNo, string(b)))
	return nil
}

// poLineDueQty is what the vendor owes on a line: the paid units and the free
// ones (Stage 148.7), which arrive with them.
func poLineDueQty(l map[string]interface{}) float64 {
	return numFromInterface(l["qty"]) + numFromInterface(l["free_qty"])
}

// poLineOpenQty is what is still to come on each line: the line's quantity
// less what was accepted (pooled per SKU in line order) and what was closed.
func poLineOpenQty(tenantID, poID string, lines []map[string]interface{}) ([]float64, []float64, error) {
	accepted, err := fetchGRNAcceptedQuantities(tenantID, poID, "")
	if err != nil {
		return nil, nil, err
	}
	pool := map[string]float64{}
	for k, v := range accepted {
		pool[k] = v
	}
	open := make([]float64, len(lines))
	taken := make([]float64, len(lines))
	for i, l := range lines {
		sku := strField(l, "sku")
		need := math.Max(0, poLineDueQty(l)-numFromInterface(l["closed_qty"]))
		t := math.Min(pool[sku], need)
		pool[sku] -= t
		taken[i] = t
		open[i] = need - t
	}
	return open, taken, nil
}

func requireCancellationReason(tenantID, reasonCode, what string) error {
	if strings.TrimSpace(reasonCode) == "" {
		return &ValidationError{Code: "PURCHA-4988", Message: what + " needs a reason code (Reason Codes, category \"Cancellation\")"}
	}
	if err := requireActiveReasonCode(tenantID, reasonCode, "Cancellation"); err != nil {
		return &ValidationError{Code: "PURCHA-4988", Message: fmt.Sprintf("%s: %v", what, err)}
	}
	return nil
}

// closePOLine closes the open rest of one line; stop also refuses any further
// receipt on it. It returns the quantity closed.
func closePOLine(tenantID, poID, userID string, lineNo int, reasonCode, note string, stop bool) (float64, error) {
	what := "closing the rest of a PO line"
	if stop {
		what = "stopping a PO line"
	}
	if err := requireCancellationReason(tenantID, reasonCode, what); err != nil {
		return 0, err
	}
	var closed float64
	err := editPOLines(tenantID, poID, func(lines []map[string]interface{}) error {
		l, err := poLineAt(lines, lineNo)
		if err != nil {
			return err
		}
		open, _, err := poLineOpenQty(tenantID, poID, lines)
		if err != nil {
			return err
		}
		closed = open[lineNo-1]
		if closed <= 1e-9 && !stop {
			return &ValidationError{Code: "PURCHA-4988", Message: fmt.Sprintf("nothing is open on line %d of PO %s", lineNo, poID)}
		}
		stamp := time.Now().UTC().Format(time.RFC3339)
		l["closed_qty"] = numFromInterface(l["closed_qty"]) + closed
		l["closed_reason"], l["closed_note"], l["closed_at"], l["closed_by"] = reasonCode, strings.TrimSpace(note), stamp, userID
		delete(l, "closed_auto")
		if stop {
			l["stopped"], l["stop_reason"], l["stop_note"] = true, reasonCode, strings.TrimSpace(note)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	verb := "CLOSE_REST"
	if stop {
		verb = "STOP"
	}
	LogAuditEvent(tenantID, userID, "PO_LINE_"+verb, "SUCCESS", fmt.Sprintf("PO %s line %d: %v unit(s) closed (%s%s)", poID, lineNo, closed, reasonCode, reasonSuffix(note)))
	return closed, nil
}

// StopPOLine stops a line with a reason: its open rest is closed and no more
// is received on it.
func StopPOLine(tenantID, poID, userID string, lineNo int, reasonCode, note string) error {
	_, err := closePOLine(tenantID, poID, userID, lineNo, reasonCode, note, true)
	return err
}

// ClosePOLineRest closes what is still open on a line (the overdue list's
// action); the line stays receivable only up to what was accepted.
func ClosePOLineRest(tenantID, poID, userID string, lineNo int, reasonCode, note string) (float64, error) {
	return closePOLine(tenantID, poID, userID, lineNo, reasonCode, note, false)
}

// AutoClosePOLines closes the rest of every line that has received at least
// (100 - under %) of its quantity, using the line's or the item's under %
// (read now). A line with no under % is never touched. It returns how many
// lines it closed.
func AutoClosePOLines(tenantID, poID, userID string) (int, error) {
	n := 0
	po, status, err := fetchDocData(tenantID, "PurchaseOrder", poID)
	if err != nil || status != "Approved" {
		return 0, nil
	}
	probe, _ := parsePOLineMaps(strField(po, "items"))
	any := false
	for _, l := range probe {
		if pctFrom(l["under_pct"]) != nil {
			any = true
		}
	}
	if !any {
		for _, l := range probe {
			if u, _ := itemTolerance(tenantID, strField(l, "sku")); u != nil && *u > 0 {
				any = true
				break
			}
		}
	}
	if !any {
		return 0, nil
	}
	var summary []string
	err = editPOLines(tenantID, poID, func(lines []map[string]interface{}) error {
		_, taken, err := poLineOpenQty(tenantID, poID, lines)
		if err != nil {
			return err
		}
		for i, l := range lines {
			qty := poLineDueQty(l)
			if numFromInterface(l["closed_qty"]) > 0 || taken[i] <= 0 || taken[i] >= qty-1e-9 {
				continue
			}
			under := pctFrom(l["under_pct"])
			if under == nil {
				under, _ = itemTolerance(tenantID, strField(l, "sku"))
			}
			if under == nil || *under <= 0 || taken[i]+1e-9 < qty*(1-*under/100) {
				continue
			}
			rest := qty - taken[i]
			l["closed_qty"] = rest
			l["closed_reason"] = "Within under-delivery tolerance"
			l["closed_auto"] = true
			l["closed_at"] = time.Now().UTC().Format(time.RFC3339)
			l["closed_by"] = userID
			summary = append(summary, fmt.Sprintf("line %d (%s) %v", i+1, strField(l, "sku"), rest))
			n++
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	if n > 0 {
		LogAuditEvent(tenantID, userID, "PO_LINE_AUTO_CLOSE", "SUCCESS", fmt.Sprintf("PO %s: rest closed within the under tolerance: %s", poID, strings.Join(summary, ", ")))
	}
	return n, nil
}

// AutoClosePOLinesForGRN runs AutoClosePOLines for every PO a posted receipt
// drew from. Best effort: a receipt that posted is never undone by this.
func AutoClosePOLinesForGRN(tenantID, grnID, userID string) {
	g, _, err := fetchDocData(tenantID, "GRN", grnID)
	if err != nil {
		return
	}
	for _, po := range GRNPOIDs(g) {
		if _, err := AutoClosePOLines(tenantID, po, userID); err != nil {
			LogSystemError(tenantID, userID, "WARN", "AutoClosePOLines", fmt.Sprintf("PO %s: %v", po, err), "")
		}
	}
}

// --- vendor challan used before ---------------------------------------------------

// VendorChallanSeenThisYear returns the goods receipt that already used this
// vendor challan number in the same financial year as challanDate (blank date =
// today), or "" . The receipt itself (grnID) and cancelled receipts do not count.
func VendorChallanSeenThisYear(tenantID, grnID, vendor, challanNo, challanDate string) string {
	challanNo = strings.TrimSpace(challanNo)
	if vendor == "" || challanNo == "" {
		return ""
	}
	day := time.Now()
	if t, err := time.Parse("2006-01-02", strings.TrimSpace(challanDate)); err == nil {
		day = t
	} else if t, err := time.Parse("2006-01-02", BusinessToday()); err == nil {
		day = t
	}
	fyStart := time.Date(day.Year(), time.April, 1, 0, 0, 0, 0, time.UTC)
	if day.Month() < time.April {
		fyStart = fyStart.AddDate(-1, 0, 0)
	}
	fyEnd := fyStart.AddDate(1, 0, 0)
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return ""
	}
	var other string
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT COALESCE(data->>'code', id) FROM %s.documents
		 WHERE doctype = 'GRN' AND deleted_at IS NULL AND status <> 'Cancelled' AND id <> $3
		   AND data->>'vendor' = $1 AND lower(trim(data->>'vendor_challan_no')) = lower($2)
		   AND COALESCE(NULLIF(data->>'vendor_challan_date', ''), to_char(created_at, 'YYYY-MM-DD')) >= $4
		   AND COALESCE(NULLIF(data->>'vendor_challan_date', ''), to_char(created_at, 'YYYY-MM-DD')) < $5
		 ORDER BY created_at LIMIT 1`, schema), vendor, challanNo, grnID, fyStart.Format("2006-01-02"), fyEnd.Format("2006-01-02")).Scan(&other)
	if err != nil {
		return ""
	}
	return other
}

// --- the overdue-lines list ---------------------------------------------------------

func init() {
	RegisterReport(ReportDefinition{ID: "po-overdue-lines", Label: "Overdue PO lines", Category: "Procurement",
		Params: []ReportParam{{Key: "as_of", Label: "As of", Type: "date"}},
		Columns: []ReportColumn{{Key: "po", Label: "PO"}, {Key: "po_id", Label: "PO id"}, {Key: "line_no", Label: "Line"}, {Key: "vendor", Label: "Vendor"},
			{Key: "sku", Label: "Item"}, {Key: "name", Label: "Name"}, {Key: "due", Label: "Due"}, {Key: "days_late", Label: "Days late"},
			{Key: "due_qty", Label: "Due qty"}, {Key: "pending", Label: "Still to come"}, {Key: "confirmation_no", Label: "Vendor confirmation"},
			{Key: "confirmation_date", Label: "Confirmed on"}},
		Run: func(tenantID string, p map[string]string) ([]map[string]interface{}, error) {
			return POOverdueLines(tenantID, p["as_of"])
		}, StoreKey: "-"})
}

// POOverdueLines lists the instalments (or, for a line with none, the PO's
// expected delivery date) due before asOf (blank = today) that have not come in.
// What was accepted fills the earliest instalments first.
func POOverdueLines(tenantID, asOf string) ([]map[string]interface{}, error) {
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
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, data FROM %s.documents
		WHERE doctype = 'PurchaseOrder' AND status = 'Approved' AND deleted_at IS NULL ORDER BY created_at, id`, schema))
	if err != nil {
		return nil, err
	}
	type po struct {
		id   string
		data map[string]interface{}
	}
	var pos []po
	for rows.Next() {
		var id, raw string
		if err := rows.Scan(&id, &raw); err != nil {
			rows.Close()
			return nil, err
		}
		var d map[string]interface{}
		if json.Unmarshal([]byte(raw), &d) == nil {
			pos = append(pos, po{id, d})
		}
	}
	rows.Close()

	names := map[string]string{}
	out := []map[string]interface{}{}
	for _, p := range pos {
		lines, err := parsePOLineMaps(strField(p.data, "items"))
		if err != nil || len(lines) == 0 {
			continue
		}
		_, taken, err := poLineOpenQty(tenantID, p.id, lines)
		if err != nil {
			continue
		}
		for i, l := range lines {
			qty, closed := poLineDueQty(l), numFromInterface(l["closed_qty"])
			if qty-closed-taken[i] <= 1e-9 {
				continue
			}
			var inst []POInstalment
			if raw, ok := l["instalments"]; ok {
				b, _ := json.Marshal(raw)
				_ = json.Unmarshal(b, &inst)
			}
			if len(inst) == 0 {
				if d := strField(p.data, "expected_delivery_date"); d != "" {
					inst = []POInstalment{{Qty: qty, Date: d}}
				}
			}
			sort.SliceStable(inst, func(a, b int) bool { return inst[a].Date < inst[b].Date })
			pend := make([]float64, len(inst))
			rem := taken[i]
			for j, in := range inst {
				fill := math.Min(rem, in.Qty)
				rem -= fill
				pend[j] = in.Qty - fill
			}
			cut := closed
			for j := len(pend) - 1; j >= 0 && cut > 0; j-- {
				c := math.Min(cut, pend[j])
				pend[j] -= c
				cut -= c
			}
			sku := strField(l, "sku")
			if _, ok := names[sku]; !ok {
				if it, err := ResolveItemBySKU(tenantID, sku); err == nil {
					names[sku] = strField(it.Data, "name")
				} else {
					names[sku] = ""
				}
			}
			for j, in := range inst {
				due, err := time.Parse("2006-01-02", in.Date)
				if err != nil || pend[j] <= 1e-9 || !due.Before(asOfDay) {
					continue
				}
				out = append(out, map[string]interface{}{
					"po": firstNonEmpty(strField(p.data, "po_number"), strField(p.data, "code"), p.id), "po_id": p.id, "line_no": i + 1,
					"vendor": firstNonEmpty(strField(p.data, "vendor"), strField(p.data, "vendor_id")), "sku": sku, "name": names[sku],
					"due": in.Date, "days_late": int(asOfDay.Sub(due).Hours() / 24), "due_qty": in.Qty, "pending": pend[j],
					"confirmation_no": strField(l, "confirmation_no"), "confirmation_date": strField(l, "confirmation_date")})
			}
		}
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a]["due"].(string) < out[b]["due"].(string) })
	return out, nil
}

// --- transfers: cancelled lines and the item's tolerance -----------------------------

// CancelTransferLines cancels lines (by SKU) of a transfer that has not been
// dispatched. The rest goes on; when every line is cancelled the transfer
// itself becomes Cancelled. The lines stay in the document, flagged.
func CancelTransferLines(tenantID, transferID, userID, reasonCode string, skus []string) error {
	if strings.TrimSpace(reasonCode) == "" {
		return &ValidationError{Code: "PURCHA-4989", Message: "cancelling transfer lines needs a reason code (Reason Codes, category \"Cancellation\")"}
	}
	if err := requireActiveReasonCode(tenantID, reasonCode, "Cancellation"); err != nil {
		return &ValidationError{Code: "PURCHA-4989", Message: fmt.Sprintf("cancelling transfer lines: %v", err)}
	}
	if len(skus) == 0 {
		return &ValidationError{Code: "PURCHA-4989", Message: "choose the lines to cancel"}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	data, status, err := lockTransferOrderTx(tx, schema, transferID)
	if err != nil {
		return err
	}
	switch status {
	case "Draft", "Pending Approval", "Approved", "Packed":
	default:
		return &ValidationError{Code: "PURCHA-4989", Message: fmt.Sprintf("lines of a %s transfer cannot be cancelled - once dispatched, send what is in transit back with Return to source", status)}
	}
	var lines []map[string]interface{}
	if err := json.Unmarshal([]byte(strField(data, "items")), &lines); err != nil {
		return &ValidationError{Code: "PURCHA-4989", Message: "the transfer's lines are not readable"}
	}
	want := map[string]bool{}
	for _, s := range skus {
		want[s] = false
	}
	active := 0
	for _, l := range lines {
		sku := strField(l, "sku")
		if _, ok := want[sku]; ok {
			want[sku] = true
			l["cancelled"], l["cancel_reason_code"] = true, reasonCode
		}
		if b, _ := l["cancelled"].(bool); !b {
			active++
		}
	}
	for s, found := range want {
		if !found {
			return &ValidationError{Code: "PURCHA-4989", Message: fmt.Sprintf("%s is not on this transfer", s)}
		}
	}
	b, _ := json.Marshal(lines)
	data["items"] = string(b)
	enc, _ := json.Marshal(data)
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $1, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'TransferOrder' AND id = $2`, schema), enc, transferID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	LogAuditEvent(tenantID, userID, "CANCEL_TRANSFER_LINES", "SUCCESS", fmt.Sprintf("Transfer %s: line(s) %s cancelled (%s)", transferID, strings.Join(skus, ", "), reasonCode))
	if active == 0 {
		return CancelTransferOrder(tenantID, transferID, userID, reasonCode, "every line cancelled")
	}
	SyncTransferReservation(tenantID, transferID)
	return nil
}

// transferTolerance is the item's under / over delivery % for a transfer
// line, read now (nil = not set).
func transferTolerance(tenantID, sku string) (under, over *float64) {
	return itemTolerance(tenantID, sku)
}

type poReconCtl struct {
	LineNo  int
	Closed  float64
	Stopped bool
}

// poReconControl is the first line number, the closed quantity and the stopped
// flag per SKU, for the PO balance view. A missing SKU yields the zero value.
func poReconControl(itemsStr string) map[string]poReconCtl {
	out := map[string]poReconCtl{}
	lines, _ := parsePOLineMaps(itemsStr)
	for i, l := range lines {
		sku := strField(l, "sku")
		c, seen := out[sku]
		if !seen {
			c = poReconCtl{LineNo: i + 1, Stopped: true}
		}
		c.Closed += numFromInterface(l["closed_qty"])
		if b, _ := l["stopped"].(bool); !b {
			c.Stopped = false
		}
		out[sku] = c
	}
	return out
}
