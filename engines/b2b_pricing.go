package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/lib/pq"
)

// Stage 148.26 - pricing reaches B2B. A customer's price list (a
// PriceListVersion, 148.6: no second price engine) fills a blank rate on a
// sales order, a direct invoice and a CRM quote. A typed rate always wins; no
// list = today. A row may carry qty breaks, a karat and weight band per design
// group (jewellery), a quota that orders count down, and a floor.
//
// Everything is off until sales.b2b_pricing / sales.below_floor are turned on.

const (
	// HoldMargin is the hold code of an order with a line below its floor and no
	// reason (setting sales.below_floor = hold). Like Credit hold, the generic
	// release refuses it; a Store Manager (or an administrator) releases it.
	HoldMargin = "MARGIN"

	marginApproverRole = "Store Manager"

	settingB2BPricing = "sales.b2b_pricing"
	settingBelowFloor = "sales.below_floor"
)

// B2BPricingOn reports whether a blank B2B rate is filled from the customer's list.
func B2BPricingOn(tenantID string) bool { return GetSettingBool(tenantID, settingB2BPricing) }

// BelowFloorMode is "off", "reason" or "hold".
func BelowFloorMode(tenantID string) string {
	switch v := strings.TrimSpace(GetSettingString(tenantID, settingBelowFloor)); v {
	case "reason", "hold":
		return v
	}
	return "off"
}

// RoleMayReleaseMargin reports whether a role can release a Margin hold.
func RoleMayReleaseMargin(role string) bool {
	return HasFullAccess(role) || normalizeRoleKey(role) == normalizeRoleKey(marginApproverRole)
}

// CustomerPriceQuery asks what a customer pays for an item.
type CustomerPriceQuery struct {
	CustomerID string
	SKU        string
	Qty        float64
	AsOf       string
	// UsedExtra is quota already used by earlier lines of the order being built.
	UsedExtra map[string]float64
}

// CustomerPrice is the answer. Floor is returned even when no rate is found
// (it comes from the row, else the item's floor price).
type CustomerPrice struct {
	Found     bool    `json:"found"`
	Rate      float64 `json:"rate"`
	Reason    string  `json:"reason,omitempty"`
	RowID     string  `json:"row_id,omitempty"`
	Floor     float64 `json:"floor,omitempty"`
	QuotaLeft float64 `json:"quota_left,omitempty"`
	ListCode  string  `json:"list_code,omitempty"`
}

type b2bPriceRow struct {
	idx                                 int
	sku, design, category, from, to     string
	minQty, maxQty, price, floor, quota float64
	karat, weightFrom, weightTo         float64
}

func b2bRowNum(v interface{}) float64 {
	switch x := v.(type) {
	case string:
		f, _ := strconv.ParseFloat(strings.TrimSuffix(strings.ToLower(strings.TrimSpace(x)), "k"), 64)
		return f
	}
	return numFromInterface(v)
}

func parseB2BRows(raw string) []b2bPriceRow {
	var items []map[string]interface{}
	if json.Unmarshal([]byte(raw), &items) != nil {
		return nil
	}
	out := make([]b2bPriceRow, 0, len(items))
	for i, it := range items {
		out = append(out, b2bPriceRow{idx: i, sku: pimString(it["sku"]), design: pimString(it["design"]), category: pimString(it["category"]),
			from: pimString(it["valid_from"]), to: pimString(it["valid_to"]),
			minQty: b2bRowNum(it["min_qty"]), maxQty: b2bRowNum(it["max_qty"]), price: b2bRowNum(it["price"]), floor: b2bRowNum(it["floor"]),
			quota: b2bRowNum(it["quota_qty"]), karat: b2bRowNum(it["karat"]), weightFrom: b2bRowNum(it["weight_from"]), weightTo: b2bRowNum(it["weight_to"])})
	}
	return out
}

// extended says a row uses a key only the B2B resolver understands. The plain
// ResolvePriceForSKU lookup (POS and contract lists) skips such rows.
func (r b2bPriceRow) extended() bool {
	return r.minQty > 1 || r.quota > 0 || r.karat > 0 || r.weightFrom > 0 || r.weightTo > 0 || r.design != "" || r.category != ""
}

type b2bItemAttrs struct {
	design, category string
	karat, weight    float64
	floor            float64
}

func b2bItemAttrsOf(tenantID, sku string) b2bItemAttrs {
	var a b2bItemAttrs
	item, err := ResolveItemBySKU(tenantID, sku)
	if err != nil {
		return a
	}
	d := item.Data
	a.design = firstNonEmpty(strField(d, "parent_product_code"), strField(d, "family"))
	a.category = strField(d, "category")
	a.karat = b2bRowNum(d["purity_karat"])
	for _, k := range []string{"net_weight", "weight", "gross_weight"} {
		if w := b2bRowNum(d[k]); w > 0 {
			a.weight = w
			break
		}
	}
	a.floor = b2bRowNum(d["floor_price"])
	return a
}

func trimNum(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

func (r b2bPriceRow) reasonText(code string, quotaLeft float64) string {
	s := "Price list " + code
	if r.minQty > 1 {
		s += ", " + trimNum(r.minQty) + "+ pcs"
	}
	if r.karat > 0 {
		s += ", " + trimNum(r.karat) + "k"
	}
	if r.weightFrom > 0 || r.weightTo > 0 {
		s += ", " + trimNum(r.weightFrom) + "-" + trimNum(r.weightTo) + " g"
	}
	if r.quota > 0 {
		s += ", " + trimNum(quotaLeft) + " left of quota"
	}
	return s
}

// b2bQuotaUsed is what orders have taken from one list row: the quantity on
// every order line priced by it that has not been cancelled or returned.
func b2bQuotaUsed(schema, rowID string) float64 {
	var used float64
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(SUM((data->>'qty')::numeric), 0) FROM %s.documents
		WHERE doctype = 'SalesOrderLine' AND deleted_at IS NULL AND data->>'price_row' = $1
		  AND COALESCE(data->>'line_status', '') NOT IN ('Cancelled', 'Returned')`, schema), rowID).Scan(&used)
	return used
}

// ResolveCustomerLinePrice finds what a customer pays for an item on their
// price list: the best row by item, then design, then category; the highest
// qty break reached; a jewellery row only when the item's karat and weight fit
// its band; a row whose quota cannot cover the qty is skipped. Found is false
// unless sales.b2b_pricing is on (Floor is still reported).
func ResolveCustomerLinePrice(tenantID string, q CustomerPriceQuery) (CustomerPrice, error) {
	var out CustomerPrice
	sku := strings.TrimSpace(q.SKU)
	if sku == "" {
		return out, nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return out, err
	}
	asOf := strings.TrimSpace(q.AsOf)
	if asOf == "" {
		asOf = BusinessToday()
	}
	qty := q.Qty
	if qty <= 0 {
		qty = 1
	}
	attrs := b2bItemAttrsOf(tenantID, sku)
	out.Floor = attrs.floor
	code := customerPriceListCode(tenantID, q.CustomerID)
	if code == "" {
		return out, nil
	}
	var versionID, itemsRaw string
	if err := db.DB.QueryRow(fmt.Sprintf(`
		SELECT id, COALESCE(data->>'items', '[]') FROM %s.documents
		WHERE doctype = 'PriceListVersion' AND deleted_at IS NULL AND status IN ('Approved', 'Superseded')
		  AND data->>'price_list_code' = $1 AND COALESCE(data->>'vendor','') = ''
		  AND data->>'effective_from' <= $2
		  AND (COALESCE(data->>'effective_to','') = '' OR data->>'effective_to' >= $2)
		ORDER BY data->>'effective_from' DESC LIMIT 1`, schema), code, asOf).Scan(&versionID, &itemsRaw); err != nil {
		return out, nil
	}
	type cand struct {
		row   b2bPriceRow
		score [4]float64
		left  float64
	}
	var best *cand
	for _, r := range parseB2BRows(itemsRaw) {
		if r.price <= 0 {
			continue
		}
		spec := 0.0
		switch {
		case r.sku != "":
			if r.sku != sku {
				continue
			}
			spec = 3
		case r.design != "":
			if r.design != attrs.design {
				continue
			}
			spec = 2
		case r.category != "":
			if r.category != attrs.category {
				continue
			}
			spec = 1
		default:
			continue
		}
		if r.minQty > 0 && qty < r.minQty || r.maxQty > 0 && qty > r.maxQty {
			continue
		}
		if r.from != "" && r.from > asOf || r.to != "" && r.to < asOf {
			continue
		}
		band := 0.0
		if r.karat > 0 {
			if math.Abs(attrs.karat-r.karat) > 0.01 {
				continue
			}
			band++
		}
		if r.weightFrom > 0 || r.weightTo > 0 {
			if attrs.weight <= 0 || attrs.weight < r.weightFrom || r.weightTo > 0 && attrs.weight > r.weightTo {
				continue
			}
			band++
		}
		c := cand{row: r, score: [4]float64{spec, band, r.minQty, 0}}
		rowID := fmt.Sprintf("%s#%d", versionID, r.idx)
		if r.quota > 0 {
			left := r.quota - b2bQuotaUsed(schema, rowID) - q.UsedExtra[rowID]
			if left < qty {
				continue
			}
			c.left = left
		}
		if best == nil || lessScore(best.score, c.score) {
			cc := c
			best = &cc
		}
	}
	if best == nil {
		return out, nil
	}
	if best.row.floor > 0 {
		out.Floor = best.row.floor
	}
	out.ListCode = code
	out.RowID = fmt.Sprintf("%s#%d", versionID, best.row.idx)
	if !B2BPricingOn(tenantID) {
		return out, nil
	}
	out.Found, out.Rate, out.QuotaLeft = true, best.row.price, best.left
	out.Reason = best.row.reasonText(code, best.left)
	return out, nil
}

func lessScore(a, b [4]float64) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

// LineFloor is the lowest rate a line may carry without a reason: the list
// row's floor, else the item's floor price. 0 = no floor.
func LineFloor(tenantID, customerID, sku string, qty float64, asOf string) float64 {
	res, _ := ResolveCustomerLinePrice(tenantID, CustomerPriceQuery{CustomerID: customerID, SKU: sku, Qty: qty, AsOf: asOf})
	return res.Floor
}

// --- orders ----------------------------------------------------------------

type linePriceNote struct {
	Reason, RowID string
	Floor         float64
}

func floorAppliesToChannel(channel string) bool {
	switch strings.ToLower(strings.TrimSpace(channel)) {
	case "manual", "b2b", "":
		return true
	}
	return false // a marketplace's price is theirs, never held or refused here
}

// applyB2BOrderPricing fills the blank rates of an order's lines from the
// customer's list and checks the floor. It returns one note per line, whether
// the order needs a Margin hold, or an error when the mode asks for a reason.
func applyB2BOrderPricing(tenantID, channel, customerID string, lines []SalesOrderLineInput) ([]linePriceNote, bool, error) {
	notes := make([]linePriceNote, len(lines))
	// A replacement goes out free: the customer's list does not fill it in.
	on := B2BPricingOn(tenantID) && !strings.EqualFold(strings.TrimSpace(channel), "Replacement")
	mode := BelowFloorMode(tenantID)
	checkFloor := mode != "off" && floorAppliesToChannel(channel)
	if !on && !checkFloor {
		return notes, false, nil
	}
	used := map[string]float64{}
	var below []int
	for i := range lines {
		l := &lines[i]
		if l.BundleSKU != "" {
			continue
		}
		blank := l.UnitPrice == 0
		if !(on && blank) && !checkFloor {
			continue
		}
		res, err := ResolveCustomerLinePrice(tenantID, CustomerPriceQuery{CustomerID: customerID, SKU: l.SKU, Qty: float64(l.Qty), UsedExtra: used})
		if err != nil {
			return nil, false, err
		}
		if on && blank && res.Found {
			l.UnitPrice = res.Rate
			notes[i].Reason, notes[i].RowID = res.Reason, res.RowID
			if res.QuotaLeft > 0 || strings.Contains(res.Reason, "quota") {
				used[res.RowID] += float64(l.Qty)
			}
		}
		notes[i].Floor = res.Floor
		if checkFloor && res.Floor > 0 && l.UnitPrice > 0 && l.UnitPrice < res.Floor && strings.TrimSpace(l.FloorReason) == "" {
			below = append(below, i+1)
		}
	}
	if len(below) == 0 {
		return notes, false, nil
	}
	if mode == "reason" {
		return nil, false, &ValidationError{Code: "SALESP-0623", SubFor: "SalesOrder",
			Message: fmt.Sprintf("line %s is below the lowest rate - give a reason for it", joinInts(below))}
	}
	return notes, true, nil
}

func joinInts(v []int) string {
	s := make([]string, len(v))
	for i, n := range v {
		s[i] = strconv.Itoa(n)
	}
	return strings.Join(s, ", ")
}

// stampB2BLine records on the order line why it has its rate.
func stampB2BLine(lineDoc map[string]interface{}, notes []linePriceNote, i int, in SalesOrderLineInput) {
	if i < len(notes) {
		if notes[i].Reason != "" {
			lineDoc["price_reason"] = notes[i].Reason
			lineDoc["price_row"] = notes[i].RowID
		}
	}
	if r := strings.TrimSpace(in.FloorReason); r != "" {
		lineDoc["floor_reason"] = truncateRunes(r, 200)
	}
}

// ReleaseMarginHold lets an approver release an order held for a rate below
// its floor, with a reason; the order then goes on through the same checks and
// allocation as any release. The generic release refuses a Margin hold.
func ReleaseMarginHold(tenantID, orderID, approver, role, reason string) error {
	if !RoleMayReleaseMargin(role) {
		return &ValidationError{Code: "SALESP-0622", SubFor: "SalesOrder", Message: "only a Store Manager or an administrator can release a Margin hold"}
	}
	schema, order, err := fetchSalesOrder(tenantID, orderID)
	if err != nil {
		return err
	}
	if strField(order, "order_status") != "On Hold" || strField(order, "hold_reason") != HoldMargin {
		return &ValidationError{Code: "SALESP-0626", SubFor: "SalesOrder", Message: "this order is not on a Margin hold"}
	}
	if strings.TrimSpace(reason) == "" {
		return &ValidationError{Code: "SALESP-0626", SubFor: "SalesOrder", Message: "say why the low rate is accepted"}
	}
	if err := mergeSalesOrderData(schema, orderID, map[string]interface{}{"margin_approved_by": approver, "margin_release_reason": truncateRunes(strings.TrimSpace(reason), 200),
		"margin_approved_at": time.Now().UTC().Format(time.RFC3339)}); err != nil {
		return err
	}
	LogAuditEvent(tenantID, approver, "ORDER_MARGIN_HOLD_RELEASED", "SUCCESS", fmt.Sprintf("order=%s reason=%s", orderID, strings.TrimSpace(reason)))
	return releaseOrderHold(tenantID, orderID, true)
}

var errMarginHold = &ValidationError{Code: "SALESP-0621", SubFor: "SalesOrder", Message: "this order is on a Margin hold - a Store Manager releases it with a reason"}

// --- invoices --------------------------------------------------------------

// enforceInvoiceFloor is the save-time check on a direct invoice: with
// sales.below_floor on, a line below its floor needs a floor_reason. It runs
// where every SalesInvoice save passes (the form and the CSV import).
func enforceInvoiceFloor(tenantID string, data map[string]interface{}) error {
	if BelowFloorMode(tenantID) == "off" {
		return nil
	}
	var lines []map[string]interface{}
	switch v := data["items"].(type) {
	case string:
		_ = json.Unmarshal([]byte(v), &lines)
	case []interface{}:
		for _, x := range v {
			if m, ok := x.(map[string]interface{}); ok {
				lines = append(lines, m)
			}
		}
	}
	if len(lines) == 0 {
		return nil
	}
	customer := strings.TrimSpace(strField(data, "customer_id"))
	if customer == "" {
		c := strings.TrimSpace(strField(data, "customer"))
		customer = firstNonEmpty(c, "")
		if link := ResolveCustomerLink(tenantID, c); link != "" {
			customer = link
		}
	}
	asOf := strings.TrimSpace(strField(data, "invoice_date"))
	var below []int
	for i, l := range lines {
		sku := strings.TrimSpace(pimString(l["sku"]))
		rate := b2bRowNum(l["rate"])
		if sku == "" || rate <= 0 || strings.TrimSpace(pimString(l["floor_reason"])) != "" {
			continue
		}
		if floor := LineFloor(tenantID, customer, sku, b2bRowNum(l["qty"]), asOf); floor > 0 && rate < floor {
			below = append(below, i+1)
		}
	}
	if len(below) > 0 {
		return &ValidationError{Code: "SALESP-0624", SubFor: "SalesInvoice",
			Message: fmt.Sprintf("line %s is below the lowest rate - give a reason for it", joinInts(below))}
	}
	return nil
}

// --- price-list endings -----------------------------------------------------

// EndingChange is one row of the preview.
type EndingChange struct {
	Row  int     `json:"row"`
	Name string  `json:"name"`
	From float64 `json:"from"`
	To   float64 `json:"to"`
}

func validEnding(e string) bool {
	switch e {
	case "0", "9", "49", "99", "none":
		return true
	}
	return false
}

func loadEndingTarget(tenantID, versionID string) (schema string, data map[string]interface{}, status string, rows []map[string]interface{}, err error) {
	schema, err = db.GetTenantSchema(tenantID)
	if err != nil {
		return
	}
	data, status, err = fetchDocData(tenantID, "PriceListVersion", versionID)
	if err != nil {
		return
	}
	if strings.TrimSpace(pimString(data["vendor"])) != "" {
		err = &ValidationError{Code: "SALESP-0625", SubFor: "PriceListVersion", Message: "price endings are for customer price lists, not vendor agreements"}
		return
	}
	switch v := data["items"].(type) {
	case string:
		_ = json.Unmarshal([]byte(v), &rows)
	case []interface{}:
		for _, x := range v {
			if m, ok := x.(map[string]interface{}); ok {
				rows = append(rows, m)
			}
		}
	}
	return
}

// PreviewPriceListEndings shows what the 9 / 49 / 99 (or whole-rupee) ending
// would make of each price on a price list. It saves nothing.
func PreviewPriceListEndings(tenantID, versionID, ending string) ([]EndingChange, error) {
	if !validEnding(ending) {
		return nil, &ValidationError{Code: "SALESP-0626", SubFor: "PriceListVersion", Message: "the ending is 0, 9, 49, 99 or none"}
	}
	_, _, _, rows, err := loadEndingTarget(tenantID, versionID)
	if err != nil {
		return nil, err
	}
	var out []EndingChange
	for i, r := range rows {
		p := b2bRowNum(r["price"])
		if p <= 0 {
			continue
		}
		name := firstNonEmpty(pimString(r["sku"]), pimString(r["design"]), pimString(r["category"]))
		out = append(out, EndingChange{Row: i, Name: name, From: p, To: roundUpToEnding(p, ending)})
	}
	return out, nil
}

// ApplyPriceListEndings writes the previewed prices into a Draft version. An
// approved version is never rewritten: change it with a new version.
func ApplyPriceListEndings(tenantID, versionID, ending string) (int, error) {
	changes, err := PreviewPriceListEndings(tenantID, versionID, ending)
	if err != nil {
		return 0, err
	}
	schema, data, status, rows, err := loadEndingTarget(tenantID, versionID)
	if err != nil {
		return 0, err
	}
	if status != "Draft" {
		return 0, &ValidationError{Code: "SALESP-0625", SubFor: "PriceListVersion", Message: "only a draft price list can be changed - make a new version"}
	}
	n := 0
	for _, c := range changes {
		if c.To != c.From {
			rows[c.Row]["price"] = c.To
			n++
		}
	}
	raw, _ := json.Marshal(rows)
	data["items"] = json.RawMessage(raw)
	b, _ := json.Marshal(data)
	if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $2, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'PriceListVersion' AND id = $1 AND status = 'Draft'`, schema), versionID, b); err != nil {
		return 0, err
	}
	return n, nil
}

// --- an agreement changed ---------------------------------------------------

// flagOpenLinesForPriceChange marks the open order lines and draft quotes a
// newly approved price-list version touches (an item it prices, for a customer
// on that list). It sets a note and nothing else: it never reprices.
func flagOpenLinesForPriceChange(tenantID, versionID string) {
	if !B2BPricingOn(tenantID) {
		return
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return
	}
	data, _, err := fetchDocData(tenantID, "PriceListVersion", versionID)
	if err != nil || strings.TrimSpace(pimString(data["vendor"])) != "" {
		return
	}
	code := strings.TrimSpace(pimString(data["price_list_code"]))
	itemsRaw, _ := json.Marshal(data["items"])
	if s, ok := data["items"].(string); ok {
		itemsRaw = []byte(s)
	}
	skuSet := map[string]bool{}
	var skus []string
	for _, r := range parseB2BRows(string(itemsRaw)) {
		if r.sku != "" && !skuSet[r.sku] {
			skuSet[r.sku] = true
			skus = append(skus, r.sku)
		}
	}
	if code == "" || len(skus) == 0 {
		return
	}
	var customers []string
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id FROM %s.documents WHERE doctype = 'Customer' AND deleted_at IS NULL AND data->>'price_list_code' = $1`, schema), code)
	if err != nil {
		return
	}
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			customers = append(customers, id)
		}
	}
	rows.Close()
	if len(customers) == 0 {
		return
	}
	note := fmt.Sprintf("Price list %s changed %s", code, BusinessToday())
	patch, _ := json.Marshal(map[string]interface{}{"price_changed": note})
	if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %[1]s.documents l SET data = l.data || $1::jsonb, updated_at = CURRENT_TIMESTAMP
		FROM %[1]s.documents o
		WHERE l.doctype = 'SalesOrderLine' AND l.deleted_at IS NULL AND o.doctype = 'SalesOrder' AND o.id = l.data->>'order_id'
		  AND o.data->>'customer_id' = ANY($2) AND l.data->>'sku' = ANY($3)
		  AND COALESCE(l.data->>'line_status', '') NOT IN ('Dispatched', 'Delivered', 'Cancelled', 'Returned', 'Completed')`, schema),
		patch, pq.Array(customers), pq.Array(skus)); err != nil {
		LogSystemError(tenantID, "", "ERROR", "flagOpenLinesForPriceChange", fmt.Sprintf("version %s: %v", versionID, err), "")
	}
	qrows, err := db.DB.Query(fmt.Sprintf(`SELECT id, data FROM %s.documents WHERE doctype = 'SalesQuotation' AND deleted_at IS NULL
		AND status IN ('Draft', 'Pending Approval') AND data->>'customer' = ANY($1)`, schema), pq.Array(customers))
	if err != nil {
		return
	}
	type qd struct {
		id   string
		data []byte
	}
	var quotes []qd
	for qrows.Next() {
		var q qd
		if qrows.Scan(&q.id, &q.data) == nil {
			quotes = append(quotes, q)
		}
	}
	qrows.Close()
	for _, q := range quotes {
		var d map[string]interface{}
		_ = json.Unmarshal(q.data, &d)
		for _, l := range crmQuotationLines(d) {
			if skuSet[l.SKU] {
				_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || $2::jsonb, updated_at = CURRENT_TIMESTAMP WHERE id = $1 AND doctype = 'SalesQuotation'`, schema), q.id, patch)
				break
			}
		}
	}
}

// FlaggedPriceLine is an open order line whose price list has changed.
type FlaggedPriceLine struct {
	OrderID  string  `json:"order_id"`
	Customer string  `json:"customer"`
	SKU      string  `json:"sku"`
	Rate     float64 `json:"rate"`
	Note     string  `json:"note"`
}

// ListFlaggedPriceLines lists the open order lines flagged by a price change.
func ListFlaggedPriceLines(tenantID string) ([]FlaggedPriceLine, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT l.data->>'order_id', COALESCE(o.data->>'customer_name', ''), l.data->>'sku', COALESCE((l.data->>'unit_price')::numeric, 0), l.data->>'price_changed'
		FROM %[1]s.documents l JOIN %[1]s.documents o ON o.doctype = 'SalesOrder' AND o.id = l.data->>'order_id'
		WHERE l.doctype = 'SalesOrderLine' AND l.deleted_at IS NULL AND COALESCE(l.data->>'price_changed', '') <> ''
		  AND COALESCE(l.data->>'line_status', '') NOT IN ('Dispatched', 'Delivered', 'Cancelled', 'Returned', 'Completed')
		ORDER BY l.created_at DESC LIMIT 200`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FlaggedPriceLine{}
	for rows.Next() {
		var f FlaggedPriceLine
		if rows.Scan(&f.OrderID, &f.Customer, &f.SKU, &f.Rate, &f.Note) == nil {
			out = append(out, f)
		}
	}
	return out, rows.Err()
}

// --- quotes -----------------------------------------------------------------

// QuoteMargin is one quoted line's margin.
type QuoteMargin struct {
	SKU       string  `json:"sku"`
	Cost      float64 `json:"cost"`
	MarginAmt float64 `json:"margin"`
	MarginPct float64 `json:"margin_pct"`
}

// CRMQuotationMargins gives each quoted line's margin over cost, only to a
// role that may see cost (nil for any other, and when no line has a known cost).
func CRMQuotationMargins(tenantID, role string, lines []CRMQuotationLine) []QuoteMargin {
	if !RoleMaySeeCost(role) {
		return nil
	}
	out := make([]QuoteMargin, 0, len(lines))
	for _, l := range lines {
		paise, ok := ResolveQuoteUnitCostPaise(tenantID, l.SKU)
		if !ok || l.Rate <= 0 {
			out = append(out, QuoteMargin{SKU: l.SKU})
			continue
		}
		cost := float64(paise) / 100
		out = append(out, QuoteMargin{SKU: l.SKU, Cost: round2(cost), MarginAmt: round2((l.Rate - cost) * float64(l.Qty)), MarginPct: round2((l.Rate - cost) / l.Rate * 100)})
	}
	return out
}

// priceCRMQuoteLines fills a CRM quote's blank rates from the customer's list and
// checks the floor. A line keeps its "why" only while its rate is still the
// list's rate, so a typed rate never carries a list's reason.
func priceCRMQuoteLines(tenantID, customerID string, lines []CRMQuotationLine) error {
	on := B2BPricingOn(tenantID)
	mode := BelowFloorMode(tenantID)
	var below []int
	used := map[string]float64{}
	for i := range lines {
		l := &lines[i]
		l.Why = ""
		if !on && mode == "off" {
			continue
		}
		res, err := ResolveCustomerLinePrice(tenantID, CustomerPriceQuery{CustomerID: customerID, SKU: l.SKU, Qty: float64(l.Qty), UsedExtra: used})
		if err != nil {
			return err
		}
		if on && res.Found && (l.Rate == 0 || l.Rate == res.Rate) {
			l.Rate, l.Why = res.Rate, res.Reason
			if res.QuotaLeft > 0 {
				used[res.RowID] += float64(l.Qty)
			}
		}
		if mode != "off" && res.Floor > 0 && l.Rate > 0 && l.Rate < res.Floor && strings.TrimSpace(l.FloorReason) == "" {
			below = append(below, i+1)
		}
	}
	if len(below) > 0 {
		return &ValidationError{Code: "SALESP-0624", SubFor: "SalesQuotation",
			Message: fmt.Sprintf("line %s is below the lowest rate - give a reason for it", joinInts(below))}
	}
	return nil
}

// customerRowOnly says a raw price-list row uses a key only the B2B resolver
// reads (karat, weight band, quota): the plain lookup leaves such a row alone.
func customerRowOnly(item map[string]interface{}) bool {
	for _, k := range []string{"karat", "weight_from", "weight_to", "quota_qty"} {
		if b2bRowNum(item[k]) > 0 {
			return true
		}
	}
	return false
}

// validateCustomerListRows checks the rows of a customer price list that use
// the 148.26 keys. A row without them is accepted as before.
func validateCustomerListRows(payload map[string]interface{}) error {
	var raw string
	switch v := payload["items"].(type) {
	case string:
		raw = v
	case nil:
		return nil
	default:
		b, _ := json.Marshal(v)
		raw = string(b)
	}
	for i, r := range parseB2BRows(raw) {
		if !(r.karat > 0 || r.weightFrom > 0 || r.weightTo > 0 || r.quota > 0 || r.floor > 0 || r.design != "" || r.category != "") {
			continue
		}
		bad := ""
		switch {
		case r.sku == "" && r.design == "" && r.category == "":
			bad = "name an item, a design or a category"
		case r.price <= 0:
			bad = "a price above 0"
		case r.weightTo > 0 && r.weightTo < r.weightFrom:
			bad = "weight to is below weight from"
		case r.maxQty > 0 && r.maxQty < r.minQty:
			bad = "max qty is below min qty"
		case r.floor > r.price && r.floor > 0:
			bad = "the floor is above the price"
		}
		if bad != "" {
			return &ValidationError{Code: "SALESP-0620", SubFor: "PriceListVersion", Message: fmt.Sprintf("row %d: %s", i+1, bad)}
		}
	}
	return nil
}
