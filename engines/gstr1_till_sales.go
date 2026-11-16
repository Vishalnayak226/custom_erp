package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"
)

// Ask 170.35: till bills and till returns in the GSTR-1. A till sale has no
// invoice document, so it is read from the cart the ledger posted and dated by
// the day its revenue was posted; a till return is the return request whose
// GST credit note (credit_note_no, 130.12) names a till bill, dated by its
// refund. Lines are worked out the way checkout and the return's GST reversal
// worked them out, then tied to the tax actually posted, so the file and the
// ledger agree to the paisa.

// gstr1TillDocs is the month's till bills and till returns for every GSTIN.
func gstr1TillDocs(c *gstr1Ctx, first, last time.Time) ([]gstr1Doc, []string, error) {
	startS, endS := first.Format("2006-01-02"), last.Format("2006-01-02")
	var docs []gstr1Doc
	var warnings []string

	type src struct {
		id   string
		data map[string]interface{}
		day  time.Time
	}
	read := func(query string) ([]src, error) {
		rows, err := db.DB.Query(query, startS, endS)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []src
		for rows.Next() {
			var s src
			var raw []byte
			if err := rows.Scan(&s.id, &raw, &s.day); err != nil {
				return nil, err
			}
			_ = json.Unmarshal(raw, &s.data)
			out = append(out, s)
		}
		return out, rows.Err()
	}

	bills, err := read(fmt.Sprintf(`
		SELECT d.id, d.data, public.erp_local_date(g.first_at)
		  FROM (SELECT document_id, MIN(created_at) AS first_at FROM %[1]s.gl_postings
		         WHERE document_type = 'POSCart' AND idempotency_key LIKE 'POSCart:%%:SALE_REVENUE'
		           AND created_at < public.erp_day_start($2::date + 1)
		         GROUP BY document_id
		        HAVING MIN(created_at) >= public.erp_day_start($1::date)) g
		  JOIN %[1]s.documents d ON d.id = g.document_id AND d.doctype = 'POSCart'
		 ORDER BY d.id`, c.schema))
	if err != nil {
		return nil, nil, err
	}
	for _, b := range bills {
		d, err := gstr1TillBill(c, b.id, b.data, b.day)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("till bill %s left out: %v", firstNonEmpty(strField(b.data, "bill_number"), b.id), err))
			continue
		}
		docs = append(docs, d)
	}

	returns, err := read(fmt.Sprintf(`
		SELECT d.id, d.data, public.erp_local_date(g.first_at)
		  FROM (SELECT r.data->>'return_request_id' AS rr, MIN(p.created_at) AS first_at
		          FROM %[1]s.gl_postings p
		          JOIN %[1]s.documents r ON r.id = p.document_id AND r.doctype = 'RefundRequest'
		         WHERE p.document_type = 'RefundRequest' AND p.idempotency_key LIKE 'RefundRequest:%%:REFUND'
		           AND p.created_at < public.erp_day_start($2::date + 1)
		         GROUP BY 1
		        HAVING MIN(p.created_at) >= public.erp_day_start($1::date)) g
		  JOIN %[1]s.documents d ON d.id = g.rr AND d.doctype = 'ReturnRequest'
		 WHERE COALESCE(d.data->>'credit_note_no', '') <> ''
		 ORDER BY d.id`, c.schema))
	if err != nil {
		return nil, nil, err
	}
	for _, r := range returns {
		d, ok, err := gstr1TillReturn(c, r.id, r.data, r.day)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("till credit note %s left out: %v", strField(r.data, "credit_note_no"), err))
			continue
		}
		if ok {
			docs = append(docs, d)
		}
	}
	return docs, warnings, nil
}

// gstr1TillParty sets the seller, buyer and place of supply a till bill (and
// its return) is reported under.
func gstr1TillParty(c *gstr1Ctx, d *gstr1Doc, cart posCart, data map[string]interface{}, interstate bool) error {
	d.gstin = strings.ToUpper(strings.TrimSpace(c.store(cart.Location).GSTIN))
	d.party = cart.CustomerID
	if len(d.gstin) < 2 {
		return nil // the store has no GSTIN: in no file
	}
	sellerState := d.gstin[:2]
	buyerState := ""
	if strings.TrimSpace(cart.CustomerID) != "" {
		b := c.buyer(cart.CustomerID)
		if g := strings.ToUpper(strings.TrimSpace(b.GSTIN)); gstinRe.MatchString(g) {
			d.ctin = g
		}
		buyerState = irpPartyFrom(b).Stcd
	}
	// Ask 215.3: a B2B bill names its buyer in its own block, which wins.
	if b2b := POSB2BFromData(data); b2b != nil && gstinRe.MatchString(b2b.GSTIN) {
		d.ctin = b2b.GSTIN
		buyerState = b2b.GSTIN[:2]
	}
	d.interstate = interstate
	d.pos = sellerState
	if interstate {
		d.pos = firstNonEmpty(strField(data, "place_of_supply"), buyerState)
		if len(d.pos) > 2 {
			d.pos = d.pos[:2]
		}
		if d.pos == "" || d.pos == sellerState {
			return fmt.Errorf("charged IGST but the customer has no other state: give the customer a state or GSTIN")
		}
	}
	return nil
}

func gstr1TillBill(c *gstr1Ctx, id string, data map[string]interface{}, day time.Time) (gstr1Doc, error) {
	var cart posCart
	raw, _ := json.Marshal(data)
	if err := json.Unmarshal(raw, &cart); err != nil {
		return gstr1Doc{}, err
	}
	d := gstr1Doc{key: "POSCart:" + id, till: true, num: firstNonEmpty(strField(data, "bill_number"), id), date: gstr1Date(day)}
	bd := cart.GSTBreakdown
	if err := gstr1TillParty(c, &d, cart, data, bd.Interstate); err != nil || d.gstin == "" {
		return d, err
	}
	var lines []GSTLineInput
	for _, l := range cart.Items {
		if l.Qty <= 0 {
			continue
		}
		// The quote's net: the line less its share of the offers.
		lines = append(lines, GSTLineInput{Sku: l.Sku, Qty: l.Qty, UnitRate: round2(l.SalePrice*float64(l.Qty)-l.OfferShare) / float64(l.Qty)})
	}
	items, err := gstr1TillItems(c, lines, bd.Interstate)
	if err != nil {
		return d, err
	}
	d.items = items
	gstr1TieTax(d.items, RupeesToPaise(bd.CGST), RupeesToPaise(bd.SGST), RupeesToPaise(bd.IGST))
	for _, it := range d.items {
		d.val += it.val
	}
	return d, nil
}

// gstr1TillReturn is a till return's GST credit note. ok is false for a
// return against anything but a till bill (an online order's return).
func gstr1TillReturn(c *gstr1Ctx, id string, data map[string]interface{}, day time.Time) (gstr1Doc, bool, error) {
	against := firstNonEmpty(strField(data, "credit_note_against"), strField(data, "original_order_id"))
	cartData, _, err := fetchDocData(c.tenantID, "POSCart", against)
	if err != nil || cartData == nil {
		return gstr1Doc{}, false, nil
	}
	var cart posCart
	raw, _ := json.Marshal(cartData)
	_ = json.Unmarshal(raw, &cart)
	interstate := originalSaleWasInterstate(c.tenantID, against)
	d := gstr1Doc{key: "ReturnRequest:" + id, till: true, note: true, num: strField(data, "credit_note_no"), date: gstr1Date(day)}
	if err := gstr1TillParty(c, &d, cart, cartData, interstate); err != nil || d.gstin == "" {
		return d, true, err
	}
	lines := refundedGSTLines(data)
	items, err := gstr1TillItems(c, lines, interstate)
	if err != nil {
		return d, true, err
	}
	d.items = items
	// The reversal posted is ComputeGSTForLines over the same lines.
	if bd, err := ComputeGSTForLines(c.tenantID, lines, interstate); err == nil {
		gstr1TieTax(d.items, RupeesToPaise(bd.CGST), RupeesToPaise(bd.SGST), RupeesToPaise(bd.IGST))
	}
	for _, it := range d.items {
		d.val += it.val
	}
	var billVal int64
	for _, l := range cart.Items {
		billVal += RupeesToPaise(l.SalePrice*float64(l.Qty) - l.OfferShare)
	}
	d.origB2CL = d.ctin == "" && d.interstate && PaiseToRupees(billVal) > gstr1B2CLLimit(d.date)
	return d, true, nil
}

// gstr1TillItems works each line out as ComputeGSTForLines does: the price is
// tax-inclusive, the tax is backed out of it; an exempt or nil-rated line
// carries its whole value and no tax.
func gstr1TillItems(c *gstr1Ctx, lines []GSTLineInput, interstate bool) ([]gstr1Item, error) {
	var out []gstr1Item
	for _, l := range lines {
		if l.Qty <= 0 {
			continue
		}
		info := c.item(l.Sku)
		if info.err != nil {
			return nil, info.err
		}
		gross := RupeesToPaise(l.UnitRate * float64(l.Qty))
		g := gstr1Item{qty: float64(l.Qty), hsn: info.tax.HSNCode, desc: firstNonEmpty(strField(info.meta, "name"), l.Sku),
			uqc: gstr1UQC(irpUnit(strField(info.meta, "uom"))), txval: gross}
		var tax int64
		if info.tax.Taxable() {
			g.rate = info.tax.GSTRate
			g.txval = int64(math.Round(float64(gross) / (1 + g.rate/100)))
			tax = gross - g.txval
		} else {
			g.nilKind = c.kind(l.Sku) // zero-rated stays a 0% line
		}
		gstr1SplitTax(&g, tax, interstate)
		out = append(out, g)
	}
	return out, nil
}

// gstr1TieTax moves the paise that line-by-line rounding leaves between the
// lines and the tax actually posted onto the largest taxed line, keeping each
// line's value. A difference over a rupee is left alone: the lines are then
// right and the reconciliation shows the gap.
func gstr1TieTax(items []gstr1Item, cgst, sgst, igst int64) {
	big := -1
	var c, s, i int64
	for n, it := range items {
		c, s, i = c+it.c, s+it.s, i+it.i
		if it.nilKind == "" && it.rate > 0 && (big < 0 || it.txval > items[big].txval) {
			big = n
		}
	}
	if big < 0 {
		return
	}
	dc, ds, di := cgst-c, sgst-s, igst-i
	if dc == 0 && ds == 0 && di == 0 || absInt64(dc) > 100 || absInt64(ds) > 100 || absInt64(di) > 100 {
		return
	}
	g := &items[big]
	g.c, g.s, g.i = g.c+dc, g.s+ds, g.i+di
	g.txval -= dc + ds + di
}
