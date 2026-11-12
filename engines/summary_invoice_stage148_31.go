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

// Stage 148.31: one invoice per customer per period (module wholesale).
//
// The payer of an order is its customer's bill_to_customer, else the customer.
// A payer marked summary_invoice is not invoiced order by order: its shipped
// orders wait until finance previews the groups and creates one Draft
// SalesInvoice per group. The group key is the payer and the currency, plus
// the ship-to state and the customer PO when the payer asks for them. A group
// that cannot honestly be one invoice (ship-to states differ and the payer
// does not split by place of supply) is shown as Blocked with its reason,
// never silently split or mixed.
//
// Posting is the normal PostSalesInvoice, so the credit check (148.3) runs on
// the payer and the GST output goes to 2200/2201/2202 as for any invoice. The
// invoice carries summary_order_ids (a comma list, so the stock side of the
// posting treats it like an order invoice: money only) and summary_lines.
//
// "One delivery challan per address" is data on the invoice (delivery_challans):
// the paper that goes with each address, with the instalment dates of its lines.
// It is not the stock-moving DeliveryChallan document: the goods already left.

// SummaryChallan is the delivery paper of one ship-to address.
type SummaryChallan struct {
	Address string               `json:"address"`
	State   string               `json:"state"`
	Orders  []string             `json:"orders"`
	Lines   []SummaryChallanLine `json:"lines"`
}

// SummaryChallanLine is one order line on a challan.
type SummaryChallanLine struct {
	Order      string  `json:"order"`
	Sku        string  `json:"sku"`
	Qty        float64 `json:"qty"`
	Instalment string  `json:"instalments,omitempty"`
}

// SummaryGroup is one would-be invoice.
type SummaryGroup struct {
	Key           string           `json:"key"`
	Payer         string           `json:"payer"`
	PayerName     string           `json:"payer_name"`
	Currency      string           `json:"currency"`
	PlaceOfSupply string           `json:"place_of_supply"`
	CustomerPO    string           `json:"customer_po"`
	Orders        []string         `json:"orders"`
	Total         float64          `json:"total"`
	Interstate    bool             `json:"interstate"`
	GSTBasis      string           `json:"gst_basis"`
	Why           []string         `json:"why"`
	Blocked       bool             `json:"blocked"`
	Reason        string           `json:"reason"`
	Challans      []SummaryChallan `json:"challans"`

	orders []*summaryOrder
}

// SummaryPreview is what the screen shows before anything is created.
type SummaryPreview struct {
	From   string         `json:"from"`
	To     string         `json:"to"`
	Groups []SummaryGroup `json:"groups"`
}

type summaryOrder struct {
	id, payer, customer, currency, po, state, address, location string
	interstate                                                  bool
	basis                                                       string
	total, shipping, short                                      float64
	gst                                                         []GSTLineInput
	lines                                                       []SummaryChallanLine
}

// wsPayer is the customer who receives the invoice for this customer's orders.
func wsPayer(tenantID, customerID string) (string, map[string]interface{}) {
	cust := wsCustomer(tenantID, customerID)
	if cust == nil {
		return customerID, nil
	}
	if bt := strField(cust, "bill_to_customer"); bt != "" && bt != customerID {
		if payer := wsCustomer(tenantID, bt); payer != nil {
			return bt, payer
		}
	}
	return customerID, cust
}

// wholesaleSummaryPayer: this order waits for the period invoice.
func wholesaleSummaryPayer(tenantID string, orderData map[string]interface{}) bool {
	if !wholesaleOn(tenantID) {
		return false
	}
	cid := strField(orderData, "customer_id")
	if cid == "" {
		return false
	}
	_, payer := wsPayer(tenantID, cid)
	return payer != nil && strField(payer, "summary_invoice") == "Yes"
}

// applyBillTo puts the invoice on the bill-to customer; the ordering customer
// stays on it as ship_to_customer_id. Off, or no bill-to: nothing changes.
func applyBillTo(tenantID, schema string, orderData, invoiceData map[string]interface{}) {
	if !wholesaleOn(tenantID) {
		return
	}
	cid := strField(orderData, "customer_id")
	payerID, payer := wsPayer(tenantID, cid)
	if cid == "" || payer == nil || payerID == cid {
		return
	}
	invoiceData["customer_id"] = payerID
	invoiceData["ship_to_customer_id"] = cid
	invoiceData["customer"] = firstNonEmpty(strField(payer, "name"), payerID)
	if strField(orderData, "payment_term") == "" {
		delete(invoiceData, "payment_term")
		delete(invoiceData, "due_date")
		if term := customerPaymentTerm(schema, payerID); term != "" {
			invoiceData["payment_term"] = term
			if due := paymentTermDueDate(tenantID, term, BusinessToday()); due != "" {
				invoiceData["due_date"] = due
			}
		}
	}
}

// liveInvoiceFor: an order already on a live invoice of its own or on a summary one.
func wsOrderInvoiced(schema, orderID string) bool {
	var n int
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT count(*) FROM %s.documents
		 WHERE doctype = 'SalesInvoice' AND deleted_at IS NULL AND status <> 'Cancelled'
		   AND (id = $1 OR data->>'sales_order_id' = $2 OR $2 = ANY(string_to_array(COALESCE(data->>'summary_order_ids',''), ',')))`, schema),
		"INV-"+orderID, orderID).Scan(&n)
	return n > 0
}

func wsLoadOrder(tenantID, schema, id string, data map[string]interface{}) *summaryOrder {
	o := &summaryOrder{id: id, customer: strField(data, "customer_id"), currency: strField(data, "currency"),
		po: strField(data, "customer_reference"), shipping: math.Max(round2(numFromInterface(data["shipping_charge"])), 0)}
	o.payer, _ = wsPayer(tenantID, o.customer)
	o.state = StateCodeFromName(firstNonEmpty(strField(data, "shipping_state"), strField(data, "custom_shipping_state")))
	o.address = strings.TrimSpace(strings.Join([]string{strField(data, "shipping_address"), strField(data, "shipping_city"), strField(data, "shipping_pincode")}, ", "))
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT data FROM %s.documents WHERE doctype = 'SalesOrderLine' AND data->>'order_id' = $1 AND deleted_at IS NULL ORDER BY id`, schema), id)
	if err != nil {
		return o
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		var line map[string]interface{}
		if rows.Scan(&raw) != nil || json.Unmarshal(raw, &line) != nil || strField(line, "line_status") == "Cancelled" {
			continue
		}
		if o.location == "" {
			o.location = strField(line, "location_code")
		}
		qty := numFromInterface(line["qty"])
		billed := qty
		if _, has := line["shipped_qty"]; has {
			billed = numFromInterface(line["shipped_qty"])
			if short := qty - billed; short > 0 {
				o.short += short * numFromInterface(line["unit_price"])
			}
		}
		sku := strField(line, "sku")
		if billed > 0 && sku != "" {
			o.gst = append(o.gst, GSTLineInput{Sku: sku, Qty: int(billed), UnitRate: numFromInterface(line["unit_price"])})
			o.lines = append(o.lines, SummaryChallanLine{Order: id, Sku: sku, Qty: billed, Instalment: strField(line, "instalments")})
		}
	}
	o.total = round2(numFromInterface(data["total_amount"]) - o.short + o.shipping)
	o.interstate, o.basis = resolveOrderInterstate(tenantID, schema, id, o.location)
	return o
}

// PreviewSummaryInvoices lists the groups of shipped, not yet invoiced orders
// of summary payers placed from..to (business days; blank = open end).
func PreviewSummaryInvoices(tenantID, from, to, payerFilter string) (*SummaryPreview, error) {
	if err := requireWholesale(tenantID); err != nil {
		return nil, err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, data, created_at FROM %s.documents
		 WHERE doctype = 'SalesOrder' AND deleted_at IS NULL AND status IN ('Shipped','Delivered') ORDER BY created_at, id`, schema))
	if err != nil {
		return nil, err
	}
	type cand struct {
		id   string
		data map[string]interface{}
	}
	var cands []cand
	for rows.Next() {
		var id string
		var raw []byte
		var created time.Time
		if rows.Scan(&id, &raw, &created) != nil {
			continue
		}
		day := created.In(BusinessLocation()).Format("2006-01-02")
		if (from != "" && day < from) || (to != "" && day > to) {
			continue
		}
		var data map[string]interface{}
		if json.Unmarshal(raw, &data) == nil && wholesaleSummaryPayer(tenantID, data) {
			cands = append(cands, cand{id, data})
		}
	}
	rows.Close()

	groups := map[string]*SummaryGroup{}
	var order []string
	for _, c := range cands {
		o := wsLoadOrder(tenantID, schema, c.id, c.data)
		if payerFilter != "" && o.payer != payerFilter || o.total <= 0 || wsOrderInvoiced(schema, o.id) {
			continue
		}
		_, payer := wsPayer(tenantID, o.customer)
		key := []string{o.payer, o.currency}
		if strField(payer, "group_by_place_of_supply") == "Yes" {
			key = append(key, "S"+o.state)
		}
		if strField(payer, "group_by_customer_po") == "Yes" {
			key = append(key, "P"+o.po)
		}
		k := strings.Join(key, "|")
		g := groups[k]
		if g == nil {
			g = &SummaryGroup{Key: k, Payer: o.payer, PayerName: firstNonEmpty(strField(payer, "name"), o.payer), Currency: o.currency}
			g.Why = []string{"bill-to " + g.PayerName}
			if o.currency != "" {
				g.Why = append(g.Why, "currency "+o.currency)
			}
			if strField(payer, "group_by_place_of_supply") == "Yes" {
				g.PlaceOfSupply = StateLabel(o.state)
				g.Why = append(g.Why, "place of supply "+firstNonEmpty(g.PlaceOfSupply, "not set"))
			}
			if strField(payer, "group_by_customer_po") == "Yes" {
				g.CustomerPO = o.po
				g.Why = append(g.Why, "customer PO "+firstNonEmpty(o.po, "none"))
			}
			groups[k] = g
			order = append(order, k)
		}
		g.orders = append(g.orders, o)
	}
	sort.Strings(order)
	out := &SummaryPreview{From: from, To: to, Groups: []SummaryGroup{}}
	for _, k := range order {
		g := groups[k]
		g.finish()
		out.Groups = append(out.Groups, *g)
	}
	return out, nil
}

// finish fills the totals, the challans and the block reason of a group.
func (g *SummaryGroup) finish() {
	states := map[string]bool{}
	interstate := map[bool]bool{}
	byAddr := map[string]*SummaryChallan{}
	var addrs []string
	for _, o := range g.orders {
		g.Orders = append(g.Orders, o.id)
		g.Total = round2(g.Total + o.total)
		states[o.state] = true
		interstate[o.interstate] = true
		g.Interstate = o.interstate
		g.GSTBasis = o.basis
		key := o.address + "|" + o.state
		c := byAddr[key]
		if c == nil {
			c = &SummaryChallan{Address: o.address, State: StateLabel(o.state)}
			byAddr[key] = c
			addrs = append(addrs, key)
		}
		c.Orders = append(c.Orders, o.id)
		c.Lines = append(c.Lines, o.lines...)
	}
	sort.Strings(addrs)
	for _, a := range addrs {
		g.Challans = append(g.Challans, *byAddr[a])
	}
	if g.PlaceOfSupply == "" && len(states) > 1 {
		g.Blocked = true
		g.Reason = "Ship-to states differ. Turn on split by place of supply for this payer."
	} else if len(interstate) > 1 {
		g.Blocked = true
		g.Reason = "Mixes inter-state and intra-state supplies."
	}
	if g.PlaceOfSupply == "" && len(states) == 1 {
		for s := range states {
			g.PlaceOfSupply = StateLabel(s)
		}
	}
	if g.GSTBasis != "" {
		g.GSTBasis = "Place of supply follows the ship-to. " + g.GSTBasis
	}
}

// CreateSummaryInvoices makes one Draft SalesInvoice per chosen, unblocked
// group. Nothing chosen, or any chosen group blocked, makes nothing.
func CreateSummaryInvoices(tenantID, userID, from, to, payerFilter string, keys []string) ([]string, error) {
	preview, err := PreviewSummaryInvoices(tenantID, from, to, payerFilter)
	if err != nil {
		return nil, err
	}
	if len(keys) == 0 {
		return nil, wsErr(wsNothingCode, "Choose at least one group to invoice.")
	}
	want := map[string]bool{}
	for _, k := range keys {
		want[k] = true
	}
	var chosen []SummaryGroup
	for _, g := range preview.Groups {
		if !want[g.Key] {
			continue
		}
		if g.Blocked {
			return nil, wsErr(wsGroupBlockedCode, "%s cannot be one invoice. %s", g.PayerName, g.Reason)
		}
		chosen = append(chosen, g)
	}
	if len(chosen) != len(want) {
		return nil, wsErr(wsNothingCode, "A chosen group has nothing left to invoice.")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, g := range chosen {
		id, err := insertSummaryInvoice(tenantID, schema, userID, from, to, g)
		if err != nil {
			return ids, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func insertSummaryInvoice(tenantID, schema, userID, from, to string, g SummaryGroup) (string, error) {
	var gst []GSTLineInput
	var lines []map[string]interface{}
	shipping, short := 0.0, 0.0
	location := ""
	pos := map[string]bool{}
	shares := map[string]float64{} // 148.54: what cancelling an order gives back
	for _, o := range g.orders {
		shares[o.id] = o.total
		gst = append(gst, o.gst...)
		shipping = round2(shipping + o.shipping)
		short = round2(short + o.short)
		location = firstNonEmpty(location, o.location)
		if o.po != "" {
			pos[o.po] = true
		}
		for _, l := range o.lines {
			lines = append(lines, map[string]interface{}{"order": o.id, "customer_po": o.po, "sku": l.Sku, "qty": l.Qty, "instalments": l.Instalment})
		}
	}
	id := summaryInvoiceID(schema, g.Orders[0])
	data := map[string]interface{}{
		"code": id, "invoice_number": IssueDocumentNumber(tenantID, "SalesInvoice", id, "", location),
		"customer": g.PayerName, "customer_id": g.Payer, "location": location, "total_amount": g.Total, "status": "Draft",
		"summary_order_ids": strings.Join(g.Orders, ","), "summary_order_amounts": shares, "summary_lines": lines, "delivery_challans": g.Challans,
		"summary_from": from, "summary_to": to, "place_of_supply": g.PlaceOfSupply,
	}
	if g.Currency != "" {
		data["currency"] = g.Currency
	}
	if len(pos) == 1 {
		for p := range pos {
			data["customer_reference"] = p
		}
	}
	if short > 0 {
		data["short_shipped_value"] = short
	}
	if shipping > 0 {
		data["shipping_charge"] = shipping
	}
	if term := customerPaymentTerm(schema, g.Payer); term != "" {
		data["payment_term"] = term
		if due := paymentTermDueDate(tenantID, term, BusinessToday()); due != "" {
			data["due_date"] = due
		}
	}
	if len(gst) > 0 {
		if breakdown, gerr := ComputeGSTForLines(tenantID, gst, g.Interstate); gerr == nil && breakdown.TotalAmount > 0 {
			if shipping > 0 {
				if breakdown.TaxableAmount > 0 && breakdown.GSTRate > 0 {
					data["shipping_gst_rate"] = breakdown.GSTRate
					breakdown.TotalTax = round2(breakdown.TotalTax + shipping - shipping/(1+breakdown.GSTRate/100))
				} else {
					breakdown.ExemptAmount = round2(breakdown.ExemptAmount + shipping)
				}
			}
			a := reconcileInvoiceAmounts(breakdown, g.Total, g.Interstate)
			data["taxable_amount"], data["non_taxable"], data["cgst"], data["sgst"], data["igst"], data["total_tax"] = a.Taxable, a.NonTaxable, a.CGST, a.SGST, a.IGST, a.TotalTax
			data["interstate"], data["gst_basis"] = g.Interstate, g.GSTBasis
		} else if gerr != nil {
			data["gst_note"] = "GST not computed: " + gerr.Error()
		}
	}
	for _, o := range g.orders { // an order that got an invoice meanwhile is not billed twice
		if wsOrderInvoiced(schema, o.id) {
			return "", wsErr(wsNothingCode, "An order in this group was invoiced meanwhile. Preview again.")
		}
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return "", err
	}
	if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'SalesInvoice', $2, 'Draft', $3)`, schema), id, encoded, userID); err != nil {
		return "", err
	}
	LogAuditEvent(tenantID, userID, "CREATE_SUMMARY_INVOICE", "SUCCESS", fmt.Sprintf("Created draft invoice %s for %d orders of %s", id, len(g.Orders), g.Payer))
	return id, nil
}

// summaryInvoiceID is INV-SUM-<first order>; when that invoice exists already
// (a cancelled one being made again) the next free -2, -3... is used.
func summaryInvoiceID(schema, firstOrder string) string {
	base := "INV-SUM-" + firstOrder
	id := base
	for n := 2; n < 1000; n++ {
		var taken bool
		if db.DB.QueryRow(fmt.Sprintf(`SELECT EXISTS (SELECT 1 FROM %s.documents WHERE doctype = 'SalesInvoice' AND id = $1)`, schema), id).Scan(&taken) != nil || !taken {
			break
		}
		id = fmt.Sprintf("%s-%d", base, n)
	}
	return id
}

// refuseSummaryPackageBill: a summary payer's order waits for the period
// invoice, so a package of it is not billed on its own (by invoice or OMS bill).
func refuseSummaryPackageBill(tenantID, orderID string) error {
	if orderID == "" || !wholesaleOn(tenantID) {
		return nil
	}
	_, order, err := fetchSalesOrder(tenantID, orderID)
	if err != nil || !wholesaleSummaryPayer(tenantID, order) {
		return nil
	}
	return wsErr(wsNothingCode, "This customer is billed on one summary invoice. Bill the period, not the package.")
}
