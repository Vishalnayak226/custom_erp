package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Stage 148.6 / 148.7: vendor price agreements and PO discounts.
//
// A vendor agreement is a PriceListVersion with a vendor on it (the sales
// price-list engine, D2: there is no second price engine). Its approval,
// versioning and supersede flow are the price list's own. This file reads its
// rows for one vendor, picks the rule for each PO line, and writes what it
// decided onto the line (StampPOPricing) so every reader of the PO - the
// preview, the print, the GST totals, the GRN, the vendor bill - sees the same
// numbers without asking again.
//
// The line keeps the list price in `rate`. The discount, the free quantity and
// the order discount come out as `net_value` (what the vendor is paid for the
// paid units) and `free_qty` (units that arrive at no charge).
//
// A vendor with no agreement, and a PO with no discount, is not touched: its
// items JSON stays byte-identical.

const (
	// SettingVendorAgreementExpiryDays is how many days before an agreement ends
	// the buyer is told. 0 = off.
	SettingVendorAgreementExpiryDays = "procurement.vendor_agreement_expiry_alert_days"
	VendorAgreementExpiringEvent     = "Vendor Agreement Expiring"

	vendorAgreementOverlapCode  = "PURCHA-4970"
	vendorAgreementGapCode      = "PURCHA-4971"
	vendorAgreementNoScopeCode  = "PURCHA-4972"
	vendorAgreementDiscountCode = "PURCHA-4973"
	vendorAgreementBuyGetCode   = "PURCHA-4974"
	poOrderDiscountCode         = "PURCHA-4975"
)

func init() {
	RegisterSetting(SettingDefinition{Key: SettingVendorAgreementExpiryDays, Module: "Procurement & GRN", Label: "Tell the buyer before a vendor agreement ends",
		Type: SettingTypeInt, Default: "7", Unit: "days", Min: settingBound(0), Max: settingBound(365),
		Description: "Days before a vendor agreement ends that the \"Vendor Agreement Expiring\" notification goes out, once. 0 = off."})
}

// vendorAgreementRow is one row of an approved vendor PriceListVersion.
type vendorAgreementRow struct {
	RuleID                 string
	VersionFrom, VersionTo string
	SKU, Design, Category  string
	Price, MinQty, MaxQty  float64
	DiscountPct            float64
	VendorCode             string
	PackMultiple           int
	CountAcross            bool
	BuyQty, GetQty         int
	ValidFrom, ValidTo     string
	CommitQty, CommitValue float64
}

func (r vendorAgreementRow) from() string { return firstNonEmpty(r.ValidFrom, r.VersionFrom) }
func (r vendorAgreementRow) to() string   { return firstNonEmpty(r.ValidTo, r.VersionTo) }

// specificity is D2's precedence: item, then design, then category, then the
// whole vendor. (The item's default rate is the line's own, typed or empty.)
func (r vendorAgreementRow) specificity() int {
	switch {
	case r.SKU != "":
		return 4
	case r.Design != "":
		return 3
	case r.Category != "":
		return 2
	}
	return 1
}

func parseAgreementRows(versionID, from, to, itemsRaw string) []vendorAgreementRow {
	var raw []map[string]interface{}
	if strings.TrimSpace(itemsRaw) == "" || json.Unmarshal([]byte(itemsRaw), &raw) != nil {
		return nil
	}
	rows := make([]vendorAgreementRow, 0, len(raw))
	for i, m := range raw {
		rows = append(rows, vendorAgreementRow{
			RuleID: fmt.Sprintf("%s#%d", versionID, i), VersionFrom: from, VersionTo: to,
			SKU: strings.TrimSpace(pimString(m["sku"])), Design: strings.TrimSpace(pimString(m["design"])), Category: strings.TrimSpace(pimString(m["category"])),
			Price: numFromInterface(m["price"]), MinQty: numFromInterface(m["min_qty"]), MaxQty: numFromInterface(m["max_qty"]),
			DiscountPct: numFromInterface(m["discount_pct"]), VendorCode: strings.TrimSpace(pimString(m["vendor_code"])),
			PackMultiple: int(numFromInterface(m["pack_multiple"])), CountAcross: strings.EqualFold(strings.TrimSpace(pimString(m["count_across_po"])), "yes"),
			BuyQty: int(numFromInterface(m["buy_qty"])), GetQty: int(numFromInterface(m["get_qty"])),
			ValidFrom: strings.TrimSpace(pimString(m["valid_from"])), ValidTo: strings.TrimSpace(pimString(m["valid_to"])),
			CommitQty: numFromInterface(m["commit_qty"]), CommitValue: numFromInterface(m["commit_value"]),
		})
	}
	return rows
}

// loadVendorAgreementRows reads every Approved (or since-superseded) vendor
// agreement row for one vendor. Dates are applied by the caller so an expired
// row can be named.
func loadVendorAgreementRows(tenantID, vendor string) []vendorAgreementRow {
	if strings.TrimSpace(vendor) == "" {
		return nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil
	}
	rs, err := db.DB.Query(fmt.Sprintf(`SELECT id, COALESCE(data->>'effective_from',''), COALESCE(data->>'effective_to',''), COALESCE(data->>'items','')
		FROM %s.documents WHERE doctype = 'PriceListVersion' AND deleted_at IS NULL AND status IN ('Approved','Superseded')
		  AND data->>'vendor' = $1 ORDER BY id`, schema), vendor)
	if err != nil {
		return nil
	}
	defer rs.Close()
	var out []vendorAgreementRow
	for rs.Next() {
		var id, from, to, items string
		if rs.Scan(&id, &from, &to, &items) == nil {
			out = append(out, parseAgreementRows(id, from, to, items)...)
		}
	}
	return out
}

type poItemAttrs struct{ sku, design, category string }

func (a poItemAttrs) matches(r vendorAgreementRow) bool {
	switch r.specificity() {
	case 4:
		return r.SKU == a.sku
	case 3:
		return r.Design != "" && r.Design == a.design
	case 2:
		return r.Category != "" && r.Category == a.category
	}
	return true
}

func agreementDay(d string) string {
	t, err := time.Parse("2006-01-02", d)
	if err != nil {
		return d
	}
	return t.Format("2 Jan")
}

func rupeeText(v float64) string {
	s := strconv.FormatFloat(v, 'f', 2, 64)
	s = strings.TrimSuffix(strings.TrimSuffix(s, "0"), "0")
	s = strings.TrimSuffix(s, ".")
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	intPart, frac := s, ""
	if i := strings.Index(s, "."); i >= 0 {
		intPart, frac = s[:i], s[i:]
	}
	for i := len(intPart) - 3; i > 0; i -= 3 {
		intPart = intPart[:i] + "," + intPart[i:]
	}
	if neg {
		intPart = "-" + intPart
	}
	return "₹" + intPart + frac
}

func pctText(v float64) string { return strconv.FormatFloat(round2(v), 'f', -1, 64) + "%" }

// resolveAgreementRule picks the rule for one line. It returns the row, or a
// reason no row applied. basis(row) is the quantity the break is read against
// (the line's own, or the PO's total for a "count across the PO" row).
func resolveAgreementRule(rows []vendorAgreementRow, a poItemAttrs, asOf string, basis func(vendorAgreementRow) float64) (*vendorAgreementRow, string) {
	var best *vendorAgreementRow
	var scoped []vendorAgreementRow
	for _, r := range rows {
		if !a.matches(r) {
			continue
		}
		scoped = append(scoped, r)
		if (r.from() != "" && r.from() > asOf) || (r.to() != "" && r.to() < asOf) {
			continue
		}
		q := basis(r)
		if q < r.MinQty || (r.MaxQty > 0 && q > r.MaxQty) {
			continue
		}
		r := r
		if best == nil || r.specificity() > best.specificity() || (r.specificity() == best.specificity() && r.from() > best.from()) {
			best = &r
		}
	}
	if best != nil {
		return best, ""
	}
	if len(scoped) == 0 {
		return nil, "no agreement for this item"
	}
	expired, future, below := "", "", 0.0
	for _, r := range scoped {
		switch {
		case r.to() != "" && r.to() < asOf:
			if r.to() > expired {
				expired = r.to()
			}
		case r.from() != "" && r.from() > asOf:
			if future == "" || r.from() < future {
				future = r.from()
			}
		default:
			if below == 0 || r.MinQty < below {
				below = r.MinQty
			}
		}
	}
	switch {
	case below > 0:
		return nil, fmt.Sprintf("no agreement: qty below the %s+ break", strconv.FormatFloat(below, 'f', -1, 64))
	case future != "":
		return nil, "no agreement: starts " + agreementDay(future)
	case expired != "":
		return nil, "no agreement: expired " + agreementDay(expired)
	}
	return nil, "no agreement for this item"
}

// derivedPOLineKeys are written by the server on every stamp. A value a client
// sends for one is discarded, so the line always states what the server decided.
var derivedPOLineKeys = []string{"net_value", "free_qty", "rule_id", "rule_note", "discount_note", "agreement_discount_pct", "vendor_code", "pack_multiple",
	"commit_qty", "commit_used_qty", "commit_value", "commit_used_value"}

func poDiscountErr(code, msg string) error {
	return &ValidationError{Code: code, SubFor: "Discount", Message: msg}
}

// StampPOPricing is the one place a PO line is priced from a vendor agreement
// and discounted. It runs at the PO's shared save choke point
// (validatePurchaseOrderEditRules, ApplyPurchaseOrderTotals), on the PO
// preview's copy of the payload, and for every PO a server path builds (RFQ
// award, supply proposals). It rewrites payload["items"] only when a line
// changed, so a PO with no agreement and no discount stays byte-identical.
func StampPOPricing(tenantID string, payload map[string]interface{}) error {
	itemsStr, _ := payload["items"].(string)
	if strings.TrimSpace(itemsStr) == "" {
		return nil
	}
	var lines []map[string]interface{}
	if err := json.Unmarshal([]byte(itemsStr), &lines); err != nil || len(lines) == 0 {
		return nil // a malformed list is the PO's own validation's to refuse
	}
	orig := make([]map[string]interface{}, len(lines))
	for i, l := range lines {
		cp := make(map[string]interface{}, len(l))
		for k, v := range l {
			cp[k] = v
		}
		orig[i] = cp
	}

	vendor := strings.TrimSpace(firstNonEmpty(str(payload, "vendor"), str(payload, "vendor_id")))
	rows := loadVendorAgreementRows(tenantID, vendor)
	// The agreement is read as of the day the PO was priced, fixed on first
	// stamp: approving the PO later, or a new agreement arriving, does not
	// re-price it.
	asOf := strings.TrimSpace(firstNonEmpty(str(payload, "order_date"), str(payload, "pricing_date")))
	if len(asOf) > 10 {
		asOf = asOf[:10]
	}
	if asOf == "" {
		asOf = BusinessToday()
		if len(rows) > 0 {
			payload["pricing_date"] = asOf
		}
	}

	// Attributes, resolved once per SKU.
	attrCache := map[string]poItemAttrs{}
	attrsOf := func(sku string) poItemAttrs {
		if a, ok := attrCache[sku]; ok {
			return a
		}
		a := poItemAttrs{sku: sku}
		if item, err := ResolveItemBySKU(tenantID, sku); err == nil {
			a.design = firstNonEmpty(strField(item.Data, "parent_product_code"), strField(item.Data, "family"))
			a.category = strField(item.Data, "category")
		}
		attrCache[sku] = a
		return a
	}
	// The vendor's own code finds the item when the line has none (a PO upload).
	for _, l := range lines {
		sku := strings.TrimSpace(pimString(l["sku"]))
		code := strings.TrimSpace(pimString(l["vendor_code"]))
		if code == "" {
			continue
		}
		if sku != "" {
			if _, err := ResolveItemBySKU(tenantID, sku); err == nil {
				continue
			}
		}
		for _, r := range rows {
			if r.SKU != "" && strings.EqualFold(r.VendorCode, code) {
				l["sku"] = r.SKU
				break
			}
		}
	}
	qtyOf := func(l map[string]interface{}) float64 { return numFromInterface(l["qty"]) }

	type lineCalc struct {
		gross, net int64
		pct        float64 // agreement discount
		lpct       float64
		lamt       float64
	}
	calcs := make([]lineCalc, len(lines))
	for i, l := range lines {
		for _, k := range derivedPOLineKeys {
			delete(l, k)
		}
		sku := strings.TrimSpace(pimString(l["sku"]))
		qty := qtyOf(l)
		if sku != "" && len(rows) > 0 {
			a := attrsOf(sku)
			basis := func(r vendorAgreementRow) float64 {
				if !r.CountAcross {
					return qty
				}
				sum := 0.0
				for _, o := range lines {
					osku := strings.TrimSpace(pimString(o["sku"]))
					if osku != "" && attrsOf(osku).matches(r) {
						sum += qtyOf(o)
					}
				}
				return sum
			}
			rule, why := resolveAgreementRule(rows, a, asOf, basis)
			priced, _ := l["price_locked"].(bool)
			if rule != nil && !priced {
				rate := numFromInterface(l["rate"])
				prev, hadPrev := l["agreement_rate"]
				if rule.Price > 0 && (rate <= 0 || (hadPrev && rate == numFromInterface(prev))) {
					l["rate"], l["agreement_rate"] = rule.Price, rule.Price
				} else if !(rule.Price > 0) || rate != rule.Price {
					delete(l, "agreement_rate")
				}
				note := rupeeText(numFromInterface(l["rate"]))
				if rule.VendorCode != "" {
					note += " · " + rule.VendorCode
				}
				if rule.MinQty > 1 {
					note += ", " + strconv.FormatFloat(rule.MinQty, 'f', -1, 64) + "+ break"
				}
				l["rule_note"], l["rule_id"] = note, rule.RuleID
				if rule.VendorCode != "" {
					l["vendor_code"] = rule.VendorCode
				}
				if rule.PackMultiple > 1 {
					l["pack_multiple"] = rule.PackMultiple
				}
				if rule.DiscountPct > 0 {
					l["agreement_discount_pct"] = rule.DiscountPct
					calcs[i].pct = rule.DiscountPct
				}
				if rule.BuyQty > 0 && rule.GetQty > 0 && qty > 0 {
					if free := int(qty) / rule.BuyQty * rule.GetQty; free > 0 {
						l["free_qty"] = free
					}
				}
				if rule.CommitQty > 0 || rule.CommitValue > 0 {
					l["commit_qty"], l["commit_value"] = rule.CommitQty, rule.CommitValue
				}
			} else if rule != nil && priced {
				l["rule_note"] = "quoted price kept"
			} else {
				l["rule_note"] = why
				delete(l, "agreement_rate")
			}
		}
		calcs[i].lpct, calcs[i].lamt = numFromInterface(l["discount_pct"]), numFromInterface(l["discount_amt"])
		if calcs[i].lpct < 0 || calcs[i].lpct > 100 || calcs[i].lamt < 0 {
			return poDiscountErr(vendorAgreementDiscountCode, fmt.Sprintf("line %d: enter a discount from 0 to 100 percent, or an amount that is not negative", i+1))
		}
	}

	// Net value of each line, in paise: both percentages compound, then the
	// rupee discount comes off.
	var orderNet int64
	for i, l := range lines {
		c := &calcs[i]
		c.gross = RupeesToPaise(numFromInterface(l["rate"]) * qtyOf(l))
		net := float64(c.gross) * (1 - c.pct/100) * (1 - c.lpct/100)
		c.net = int64(math.Round(net)) - RupeesToPaise(c.lamt)
		if c.net < 0 {
			c.net = 0
		}
		orderNet += c.net
		if c.pct > 0 || c.lpct > 0 || c.lamt > 0 {
			var parts []string
			switch {
			case c.pct > 0 && c.lpct > 0:
				parts = append(parts, fmt.Sprintf("%s + %s = %s", pctText(c.pct), pctText(c.lpct), pctText((1-(1-c.pct/100)*(1-c.lpct/100))*100)))
			case c.pct > 0:
				parts = append(parts, pctText(c.pct))
			case c.lpct > 0:
				parts = append(parts, pctText(c.lpct))
			}
			if c.lamt > 0 {
				parts = append(parts, rupeeText(c.lamt))
			}
			l["discount_note"] = strings.Join(parts, " + ") + " off"
		}
	}

	// The order discount, spread over the lines in proportion to their value.
	opct, oamt := numFromInterface(payload["order_discount_pct"]), numFromInterface(payload["order_discount_amt"])
	if opct < 0 || opct > 100 || oamt < 0 {
		return poDiscountErr(poOrderDiscountCode, "enter an order discount from 0 to 100 percent, or an amount that is not negative")
	}
	if opct > 0 || oamt > 0 {
		total := int64(math.Round(float64(orderNet)*opct/100)) + RupeesToPaise(oamt)
		if total > orderNet {
			return poDiscountErr(poOrderDiscountCode, "the order discount is more than the order value")
		}
		left, last := total, -1
		for i := range lines {
			if calcs[i].net > 0 {
				last = i
			}
		}
		for i, l := range lines {
			if calcs[i].net == 0 {
				continue
			}
			share := left
			if i != last {
				share = int64(math.Round(float64(total) * float64(calcs[i].net) / float64(orderNet)))
				if share > left {
					share = left
				}
			}
			left -= share
			calcs[i].net -= share
			n, _ := l["discount_note"].(string)
			add := "order " + rupeeText(float64(share)/100)
			if n == "" {
				l["discount_note"] = add + " off"
			} else {
				l["discount_note"] = n + " + " + add
			}
		}
	}
	for i, l := range lines {
		c := calcs[i]
		if c.net != c.gross {
			l["net_value"] = float64(c.net) / 100
		}
	}
	stampCommitments(tenantID, payload, vendor, lines)

	if reflect.DeepEqual(orig, lines) {
		return nil
	}
	b, err := json.Marshal(lines)
	if err != nil {
		return err
	}
	payload["items"] = string(b)
	return nil
}

// stampCommitments adds how much of each commitment (qty or value) the
// vendor's other open POs, and this one, already use.
func stampCommitments(tenantID string, payload map[string]interface{}, vendor string, lines []map[string]interface{}) {
	need := map[string]bool{}
	for _, l := range lines {
		if id, _ := l["rule_id"].(string); id != "" && (numFromInterface(l["commit_qty"]) > 0 || numFromInterface(l["commit_value"]) > 0) {
			need[id] = true
		}
	}
	if len(need) == 0 {
		return
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return
	}
	self := firstNonEmpty(str(payload, "code"), str(payload, "po_number"))
	usedQty, usedVal := map[string]float64{}, map[string]float64{}
	rs, err := db.DB.Query(fmt.Sprintf(`SELECT COALESCE(data->>'items','') FROM %s.documents
		WHERE doctype = 'PurchaseOrder' AND deleted_at IS NULL AND status NOT IN ('Cancelled','Rejected')
		  AND data->>'vendor' = $1 AND id <> $2 AND COALESCE(data->>'items','') LIKE '%%"rule_id"%%'`, schema), vendor, self)
	if err == nil {
		defer rs.Close()
		for rs.Next() {
			var raw string
			if rs.Scan(&raw) != nil {
				continue
			}
			var other []map[string]interface{}
			if json.Unmarshal([]byte(raw), &other) != nil {
				continue
			}
			for _, o := range other {
				if id, _ := o["rule_id"].(string); need[id] {
					usedQty[id] += numFromInterface(o["qty"])
					usedVal[id] += poLineNetValue(o)
				}
			}
		}
	}
	for _, l := range lines {
		id, _ := l["rule_id"].(string)
		if !need[id] {
			continue
		}
		usedQty[id] += numFromInterface(l["qty"])
		usedVal[id] += poLineNetValue(l)
	}
	for _, l := range lines {
		if id, _ := l["rule_id"].(string); need[id] {
			l["commit_used_qty"], l["commit_used_value"] = usedQty[id], round2(usedVal[id])
		}
	}
}

func poLineNetValue(l map[string]interface{}) float64 {
	if v, ok := l["net_value"]; ok {
		return numFromInterface(v)
	}
	return numFromInterface(l["rate"]) * numFromInterface(l["qty"])
}

// POLineNet is what the vendor is paid for a line's paid units: the stamped net
// value when the line was discounted, otherwise rate x qty as always.
func POLineNet(l POLineInput) float64 {
	if l.NetValue != nil {
		return *l.NetValue
	}
	return l.Rate * float64(l.Qty)
}

// poLineBilledRate is the price of one received unit of a PO line: the list
// rate as always, or the discounted value spread over the paid and free units.
func poLineBilledRate(it poItemLine) float64 {
	if it.NetValue == nil && it.FreeQty == 0 {
		return it.Rate
	}
	net := it.Rate * float64(it.Qty)
	if it.NetValue != nil {
		net = *it.NetValue
	}
	if n := it.Qty + it.FreeQty; n > 0 {
		return net / float64(n)
	}
	return it.Rate
}

// poLineUnitRate is the paid price of one unit, for the GST engine.
func poLineUnitRate(l POLineInput) float64 {
	if l.NetValue == nil || l.Qty <= 0 {
		return l.Rate
	}
	return *l.NetValue / float64(l.Qty)
}

// ValidateVendorAgreementRows is ValidatePriceListVersionDocument's vendor half:
// every row needs a scope (unless it is a plain vendor-wide discount), discounts
// must be in range, buy-N-get-M needs both, and the qty breaks of one scope and
// window must neither overlap nor leave a gap.
func ValidateVendorAgreementRows(payload map[string]interface{}) error {
	itemsRaw, _ := payload["items"].(string)
	rows := parseAgreementRows("v", pimString(payload["effective_from"]), pimString(payload["effective_to"]), itemsRaw)
	for i, r := range rows {
		label := fmt.Sprintf("row %d", i+1)
		if r.specificity() == 1 && r.Price > 0 {
			return &ValidationError{Code: vendorAgreementNoScopeCode, SubFor: "Prices", Message: label + ": pick an item, design or category for a price"}
		}
		if r.DiscountPct < 0 || r.DiscountPct > 100 || r.Price < 0 || r.MinQty < 0 || r.MaxQty < 0 {
			return &ValidationError{Code: vendorAgreementDiscountCode, SubFor: "Prices", Message: label + ": enter a discount from 0 to 100 percent and amounts that are not negative"}
		}
		if (r.BuyQty > 0) != (r.GetQty > 0) || r.BuyQty < 0 || r.GetQty < 0 || r.PackMultiple < 0 {
			return &ValidationError{Code: vendorAgreementBuyGetCode, SubFor: "Prices", Message: label + ": fill both Buy and Get with whole numbers above zero"}
		}
		if r.MaxQty > 0 && r.MaxQty < math.Max(r.MinQty, 1) {
			return &ValidationError{Code: vendorAgreementOverlapCode, SubFor: "Prices", Message: label + ": Qty to is below Qty from"}
		}
		if r.ValidFrom != "" && r.ValidTo != "" && r.ValidTo < r.ValidFrom {
			return &ValidationError{Code: "GLOBAL-0002", SubFor: "Prices", Message: label + ": valid to cannot be before valid from"}
		}
	}
	// Breaks: group by scope + window, then read them in order.
	groups := map[string][]vendorAgreementRow{}
	var keys []string
	for _, r := range rows {
		k := fmt.Sprintf("%d|%s|%s|%s|%s|%s", r.specificity(), r.SKU, r.Design, r.Category, r.from(), r.to())
		if _, ok := groups[k]; !ok {
			keys = append(keys, k)
		}
		groups[k] = append(groups[k], r)
	}
	for _, k := range keys {
		g := groups[k]
		if len(g) < 2 {
			continue
		}
		sort.SliceStable(g, func(i, j int) bool { return math.Max(g[i].MinQty, 1) < math.Max(g[j].MinQty, 1) })
		for i := 1; i < len(g); i++ {
			prev, cur := g[i-1], g[i]
			if prev.MaxQty == 0 || math.Max(cur.MinQty, 1) <= prev.MaxQty {
				return &ValidationError{Code: vendorAgreementOverlapCode, SubFor: "Prices", Message: fmt.Sprintf("qty breaks overlap at %s", strconv.FormatFloat(math.Max(cur.MinQty, 1), 'f', -1, 64))}
			}
			if math.Max(cur.MinQty, 1) > prev.MaxQty+1 {
				return &ValidationError{Code: vendorAgreementGapCode, SubFor: "Prices", Message: fmt.Sprintf("there is a gap between %s and %s", strconv.FormatFloat(prev.MaxQty, 'f', -1, 64), strconv.FormatFloat(math.Max(cur.MinQty, 1), 'f', -1, 64))}
			}
		}
	}
	return nil
}

// RunVendorAgreementExpiryAlerts sends "Vendor Agreement Expiring" once per
// agreement end date, N days ahead, through the notification path the app
// already uses.
func RunVendorAgreementExpiryAlerts(tenantID, schema string) error {
	n := GetSettingInt(tenantID, SettingVendorAgreementExpiryDays)
	if n <= 0 {
		return nil
	}
	today := BusinessToday()
	t, err := time.Parse("2006-01-02", today)
	if err != nil {
		return err
	}
	limit := t.AddDate(0, 0, n).Format("2006-01-02")
	rs, err := db.DB.Query(fmt.Sprintf(`SELECT id, COALESCE(data->>'vendor',''), data->>'effective_to' FROM %s.documents
		WHERE doctype = 'PriceListVersion' AND deleted_at IS NULL AND status = 'Approved' AND COALESCE(data->>'vendor','') <> ''
		  AND COALESCE(data->>'effective_to','') >= $1 AND COALESCE(data->>'effective_to','') <= $2
		  AND COALESCE(data->>'expiry_alerted_for','') <> data->>'effective_to' ORDER BY id LIMIT 500`, schema), today, limit)
	if err != nil {
		return err
	}
	type due struct{ id, vendor, to string }
	var list []due
	for rs.Next() {
		var d due
		if rs.Scan(&d.id, &d.vendor, &d.to) == nil {
			list = append(list, d)
		}
	}
	rs.Close()
	for _, d := range list {
		if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || jsonb_build_object('expiry_alerted_for', data->>'effective_to') WHERE id = $1 AND doctype = 'PriceListVersion'`, schema), d.id); err != nil {
			return err
		}
		DispatchNotification(tenantID, VendorAgreementExpiringEvent, d.id, map[string]string{"agreement": d.id, "vendor": d.vendor, "ends": d.to})
	}
	return nil
}

// --- Stage 146.4.2: supply proposals -> one Draft PO per default vendor ------

type proposalPOLine struct {
	SKU  string
	Qty  int
	Cost float64 // the item cost, used when the vendor has no agreement price
}

// quoteCostRupees is the item cost a proposal line is priced at when no
// agreement applies (as before Stage 146.4.2).
func quoteCostRupees(tenantID, sku string) float64 {
	if c, ok := ResolveQuoteUnitCostPaise(tenantID, sku); ok {
		return PaiseToRupees(c)
	}
	return 0
}

// proposalVendors names the vendor of each proposal: the one chosen, or the
// item's default vendor. An item with neither is refused before a number is
// taken.
func proposalVendors(tenantID string, ids []string, chosen string) (map[string]string, []string, error) {
	out := map[string]string{}
	seen := map[string]bool{}
	var order []string
	chosen = strings.TrimSpace(chosen)
	for _, id := range ids {
		v := chosen
		if v == "" {
			d, _, err := fetchDocData(tenantID, "SupplyProposal", id)
			if err != nil {
				return nil, nil, err
			}
			sku := str(d, "sku")
			if item, err := ResolveItemBySKU(tenantID, sku); err == nil {
				v = strings.TrimSpace(strField(item.Data, "default_vendor"))
			}
			if v == "" {
				return nil, nil, proposalErr("choose a vendor for the purchase order: %s has no default vendor", sku)
			}
		}
		out[id] = v
		if !seen[v] {
			seen[v] = true
			order = append(order, v)
		}
	}
	sort.Strings(order)
	return out, order, nil
}

// PriceProposalPOLines prices the lines of a PO built from supply proposals the
// way the PO form prices them: the vendor agreement's price, its discount and
// free units, and the quantity rounded up to its pack multiple. A vendor with
// no agreement gets the item cost, exactly as before. It updates lines[i].Qty
// to the rounded quantity.
func PriceProposalPOLines(tenantID, vendor string, lines []proposalPOLine) (itemsJSON string, total float64, err error) {
	build := func(withCost bool) map[string]interface{} {
		items := make([]map[string]interface{}, len(lines))
		for i, l := range lines {
			rate := 0.0
			if withCost {
				rate = l.Cost
			}
			items[i] = map[string]interface{}{"sku": l.SKU, "qty": l.Qty, "rate": rate}
		}
		return map[string]interface{}{"vendor": vendor, "items": itemsJSONOf(items)}
	}
	if len(loadVendorAgreementRows(tenantID, vendor)) == 0 {
		type plain struct {
			SKU  string  `json:"sku"`
			Qty  int     `json:"qty"`
			Rate float64 `json:"rate"`
		}
		out := make([]plain, len(lines))
		for i, l := range lines {
			out[i] = plain{l.SKU, l.Qty, l.Cost}
			total += l.Cost * float64(l.Qty)
		}
		b, _ := json.Marshal(out)
		return string(b), total, nil
	}
	for pass := 0; pass < 2; pass++ { // a pack round-up can move a quantity into the next break
		p := build(false)
		if err := StampPOPricing(tenantID, p); err != nil {
			return "", 0, err
		}
		changed := false
		for i, m := range poLinesFromPayload(p) {
			if pack := int(numFromInterface(m["pack_multiple"])); pack > 1 && lines[i].Qty%pack != 0 {
				lines[i].Qty += pack - lines[i].Qty%pack
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	p := build(false)
	if err := StampPOPricing(tenantID, p); err != nil {
		return "", 0, err
	}
	ms := poLinesFromPayload(p)
	for i, m := range ms {
		if numFromInterface(m["rate"]) <= 0 {
			m["rate"] = lines[i].Cost
		}
	}
	p["items"] = itemsJSONOf(ms)
	if err := StampPOPricing(tenantID, p); err != nil {
		return "", 0, err
	}
	for _, m := range poLinesFromPayload(p) {
		total += poLineNetValue(m)
	}
	return p["items"].(string), total, nil
}

func itemsJSONOf(items []map[string]interface{}) string {
	b, _ := json.Marshal(items)
	return string(b)
}

func poLinesFromPayload(p map[string]interface{}) []map[string]interface{} {
	var out []map[string]interface{}
	s, _ := p["items"].(string)
	_ = json.Unmarshal([]byte(s), &out)
	return out
}
