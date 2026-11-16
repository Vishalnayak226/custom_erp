package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/lib/pq"
)

// Ask 215.7/215.8 (user, 2026-11-15): the rest of the standard offer types
// and the conditions that limit when an offer applies. The evaluator itself
// is in pos_offers.go; this file holds what the new types and conditions add.

const (
	// offerScopeDesignList targets designs (ProductFamily by design number,
	// code or id) - every item of a listed design. Its lists also take SKUs.
	offerScopeDesignList = "Design List"

	// offerTypeBuyAGetB: buy buy_qty from the scope (list A), get get_qty from
	// get_list (list B) free or at get_discount_pct off.
	offerTypeBuyAGetB = "Buy A Get B"
	// offerTypeSpendSlabs: the highest slab the scoped amount reaches.
	offerTypeSpendSlabs = "Spend Slabs"
	// offerTypeQtySlabs: the highest slab the scoped quantity reaches.
	offerTypeQtySlabs = "Quantity Slabs"
	// offerTypeFixedPrice: every unit in scope sells at offer_price.
	offerTypeFixedPrice = "Fixed Price"
	// offerTypeCombo: one unit from each combo list together at offer_price.
	offerTypeCombo = "Combo"
)

// offerClock is the moment the day and time conditions are judged at, in the
// business's time zone. Tests set it.
var offerClock = func() time.Time { return time.Now().In(BusinessLocation()) }

// offerSlab is one step of a slab offer: from this amount (or quantity) up,
// value off - rupees, or a percentage when pct.
type offerSlab struct {
	from  float64
	value float64
	pct   bool
}

// parseOfferSlabs reads "2000=200; 5000=10%": steps separated by ";", "|" or
// new lines, each "from=value" (or "from:value"), a value ending in % being
// a percentage. Commas inside a number ("2,000") are ignored. Sorted by from.
func parseOfferSlabs(v string) ([]offerSlab, error) {
	var out []offerSlab
	seen := map[float64]bool{}
	for _, part := range strings.FieldsFunc(v, func(r rune) bool { return r == ';' || r == '|' || r == '\n' || r == '\r' }) {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		k := strings.IndexAny(part, "=:")
		if k < 0 {
			return nil, fmt.Errorf("slab %q needs from=value, for example 2000=200 or 5000=10%%", part)
		}
		num := func(s string) (float64, error) {
			s = strings.NewReplacer(",", "", "₹", "", " ", "").Replace(strings.TrimSpace(s))
			return strconv.ParseFloat(s, 64)
		}
		from, err := num(part[:k])
		if err != nil || from <= 0 {
			return nil, fmt.Errorf("slab %q: the amount or quantity before = must be a number above 0", part)
		}
		val := strings.TrimSpace(part[k+1:])
		pct := strings.HasSuffix(val, "%")
		value, err := num(strings.TrimSuffix(val, "%"))
		if err != nil || value <= 0 || (pct && value > 100) {
			return nil, fmt.Errorf("slab %q: the value after = must be an amount above 0 or a percentage up to 100%%", part)
		}
		if seen[from] {
			return nil, fmt.Errorf("slab %q: two slabs start at %v", part, from)
		}
		seen[from] = true
		out = append(out, offerSlab{from: from, value: value, pct: pct})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].from < out[j].from })
	return out, nil
}

// offerSlabFor is the highest slab measure reaches.
func offerSlabFor(slabs []offerSlab, measure float64) (offerSlab, bool) {
	var hit offerSlab
	ok := false
	for _, s := range slabs {
		if measure+1e-9 >= s.from {
			hit, ok = s, true
		}
	}
	return hit, ok
}

func (s offerSlab) label() string {
	if s.pct {
		return fmt.Sprintf("%.4g%%", s.value)
	}
	return fmt.Sprintf("%.2f", s.value)
}

var offerWeekdays = []string{"sunday", "monday", "tuesday", "wednesday", "thursday", "friday", "saturday"}

func offerWeekday(tok string) (int, bool) {
	if len(tok) < 3 {
		return 0, false
	}
	for i, d := range offerWeekdays {
		if strings.HasPrefix(d, tok) {
			return i, true
		}
	}
	return 0, false
}

// parseOfferDays reads the days an offer runs: day names (Mon, Tuesday, ...),
// ranges (Mon-Fri), Weekdays, Weekend or All, separated by commas or spaces.
// Blank is every day (nil).
func parseOfferDays(v string) ([]bool, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil, nil
	}
	days := make([]bool, 7)
	for _, tok := range strings.FieldsFunc(strings.ToLower(v), func(r rune) bool { return r == ',' || r == ' ' || r == ';' || r == '/' }) {
		switch tok {
		case "all", "daily", "everyday":
			for i := range days {
				days[i] = true
			}
			continue
		case "weekday", "weekdays":
			for i := 1; i <= 5; i++ {
				days[i] = true
			}
			continue
		case "weekend", "weekends":
			days[0], days[6] = true, true
			continue
		}
		if a, b, isRange := strings.Cut(tok, "-"); isRange {
			from, ok1 := offerWeekday(a)
			to, ok2 := offerWeekday(b)
			if !ok1 || !ok2 {
				return nil, fmt.Errorf("%q is not a day range like Mon-Fri", tok)
			}
			for d := from; ; d = (d + 1) % 7 {
				days[d] = true
				if d == to {
					break
				}
			}
			continue
		}
		d, ok := offerWeekday(tok)
		if !ok {
			return nil, fmt.Errorf("%q is not a day: use Mon, Tue, ... Sun, Mon-Fri, Weekdays or Weekend", tok)
		}
		days[d] = true
	}
	return days, nil
}

// parseOfferTime reads "HH:MM" (or "HH", or "HH:MM:SS" from a time box) as
// minutes after midnight; -1 for blank. 24:00 is the end of the day.
func parseOfferTime(v string) (int, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return -1, nil
	}
	parts := strings.Split(v, ":")
	if len(parts) > 3 {
		return 0, fmt.Errorf("%q is not a time like 18:30", v)
	}
	h, err := strconv.Atoi(parts[0])
	m := 0
	if err == nil && len(parts) > 1 {
		m, err = strconv.Atoi(parts[1])
	}
	if err != nil || h < 0 || m < 0 || m > 59 || h > 24 || (h == 24 && m > 0) {
		return 0, fmt.Errorf("%q is not a time like 18:30", v)
	}
	return h*60 + m, nil
}

// offerOpenNow (215.8) is the day and time condition: the offer runs on its
// days, between its times. A window that ends before it starts (22:00 to
// 02:00) runs over midnight. A condition that does not parse never matches -
// a broken rule must not widen an offer.
func offerOpenNow(rule offerRule, now time.Time) bool {
	if rule.never {
		return false
	}
	if len(rule.days) == 7 && !rule.days[int(now.Weekday())] {
		return false
	}
	if !rule.timed {
		return true
	}
	from, to := rule.hourFrom, rule.hourTo
	m := now.Hour()*60 + now.Minute()
	switch {
	case from >= 0 && to >= 0 && from <= to:
		return m >= from && m < to
	case from >= 0 && to >= 0:
		return m >= from || m < to
	case from >= 0:
		return m >= from
	default:
		return m < to
	}
}

// parseOfferRuleExtras fills a rule's 215.7/215.8 fields from its document.
func parseOfferRuleExtras(r *offerRule, d map[string]interface{}) {
	r.getDiscountPct = numFromInterface(d["get_discount_pct"])
	r.getList = strField(d, "get_list")
	r.offerPrice = numFromInterface(d["offer_price"])
	r.comboLists = splitOfferComboLists(strField(d, "combo_lists"))
	r.excludeList = strField(d, "exclude_list")
	r.maxUses = int(numFromInterface(d["max_uses"]))
	r.maxUsesPerCustomer = int(numFromInterface(d["max_uses_per_customer"]))
	r.getSet = offerCodeSet(r.getList)
	r.excludeSet = offerCodeSet(r.excludeList)
	for _, l := range r.comboLists {
		r.comboSets = append(r.comboSets, offerCodeSet(l))
	}
	if r.scope == offerScopeDesignList {
		// Never nil: a blank design list matches nothing, not the whole bill.
		r.skuSet = map[string]bool{}
		for k := range offerCodeSet(r.scopeValue) {
			r.skuSet[k] = true
		}
	}
	if slabs, err := parseOfferSlabs(strField(d, "slabs")); err == nil {
		r.slabs = slabs
	}
	days, err := parseOfferDays(strField(d, "days"))
	from, err1 := parseOfferTime(strField(d, "hour_from"))
	to, err2 := parseOfferTime(strField(d, "hour_to"))
	if err != nil || err1 != nil || err2 != nil {
		r.never = true // see offerOpenNow
		return
	}
	r.days = days
	r.hourFrom, r.hourTo = from, to
	r.timed = from >= 0 || to >= 0
}

// splitOfferComboLists reads a Combo offer's lists: one list per line (or
// separated by "|"), codes inside a list by commas or semicolons.
func splitOfferComboLists(v string) []string {
	var out []string
	for _, l := range strings.FieldsFunc(v, func(r rune) bool { return r == '\n' || r == '\r' || r == '|' }) {
		if l = strings.TrimSpace(l); l != "" && len(splitSKUList(l)) > 0 {
			out = append(out, l)
		}
	}
	return out
}

// offerWholeBill is a rule whose scope is the whole bill.
func offerWholeBill(rule offerRule) bool {
	return rule.scope == offerScopeBill || (rule.scopeValue == "" && rule.skuSet == nil)
}

// offerSetHas is a line named by a code list: by its SKU, or by any of the
// item and design codes resolveLineKeys gave it.
func offerSetHas(set map[string]bool, line OfferCartLine) bool {
	if len(set) == 0 {
		return false
	}
	if set[strings.ToUpper(strings.TrimSpace(line.Sku))] {
		return true
	}
	for _, k := range line.keys {
		if set[k] {
			return true
		}
	}
	return false
}

// offerExcluded (215.8): an excluded item never takes the offer, even when
// the offer is for the whole bill.
func offerExcluded(rule offerRule, line OfferCartLine) bool {
	return len(rule.excludeSet) > 0 && offerSetHas(rule.excludeSet, line)
}

// offerGetPct is the % off a get unit: blank, 0 or over 100 is free.
func offerGetPct(rule offerRule) float64 {
	if rule.getDiscountPct <= 0 || rule.getDiscountPct > 100 {
		return 100
	}
	return rule.getDiscountPct
}

// offerBuyAGetBDiscount: for every buy_qty units bought from list A (the
// scope), get_qty units from list B are free or at get % off. B units are
// taken cheapest first, the units only on list B before those on both; A
// units are then taken from what is left, the units only on list A first,
// so a unit is never both bought and got.
func offerBuyAGetBDiscount(rule offerRule, lines []OfferCartLine) (float64, string) {
	type unit struct {
		price   float64
		a, b    bool
		claimed bool
	}
	var gets, buys []*unit
	for _, l := range lines {
		a := offerLineMatches(rule, l)
		b := !offerExcluded(rule, l) && offerSetHas(rule.getSet, l)
		for i := 0; i < l.Qty; i++ {
			u := &unit{price: l.SalePrice, a: a, b: b}
			if b {
				gets = append(gets, u)
			}
			if a {
				buys = append(buys, u)
			}
		}
	}
	sort.SliceStable(gets, func(i, j int) bool {
		if gets[i].a != gets[j].a {
			return !gets[i].a
		}
		return gets[i].price < gets[j].price
	})
	sort.SliceStable(buys, func(i, j int) bool {
		if buys[i].b != buys[j].b {
			return !buys[i].b
		}
		return buys[i].price > buys[j].price
	})
	take := func(pool []*unit, n int) []*unit {
		var out []*unit
		for _, u := range pool {
			if len(out) == n {
				break
			}
			if !u.claimed {
				out = append(out, u)
			}
		}
		if len(out) < n {
			return nil
		}
		return out
	}
	pct := offerGetPct(rule)
	discount, groups := 0.0, 0
	for {
		g := take(gets, rule.getQty)
		if g == nil {
			break
		}
		for _, u := range g {
			u.claimed = true
		}
		by := take(buys, rule.buyQty)
		if by == nil {
			break
		}
		for _, u := range by {
			u.claimed = true
		}
		for _, u := range g {
			discount += u.price * pct / 100
		}
		groups++
	}
	if discount <= 0 {
		return 0, ""
	}
	how := "free"
	if pct < 100 {
		how = fmt.Sprintf("at %.4g%% off", pct)
	}
	return discount, fmt.Sprintf("buy %d of %s, get %d of the get items %s (%d time(s))", rule.buyQty, offerScopeLabel(rule), rule.getQty, how, groups)
}

// offerSlabDiscount: Spend Slabs measure the scoped amount, Quantity Slabs
// the scoped units; the highest slab reached takes its value off the scope.
func offerSlabDiscount(rule offerRule, lines []OfferCartLine, gross float64) (float64, string) {
	base := offerScopedAmount(rule, lines, gross)
	if base <= 0 || len(rule.slabs) == 0 {
		return 0, ""
	}
	measure, unit := base, "spend"
	if rule.offerType == offerTypeQtySlabs {
		measure, unit = float64(offerQualifyingQty(rule, lines)), "buy"
	}
	s, ok := offerSlabFor(rule.slabs, measure)
	if !ok {
		return 0, ""
	}
	d := s.value
	if s.pct {
		d = base * s.value / 100
	}
	if d > base {
		d = base
	}
	from := fmt.Sprintf("%.2f", s.from)
	if rule.offerType == offerTypeQtySlabs {
		from = fmt.Sprintf("%g", s.from)
	}
	return d, fmt.Sprintf("%s %s on %s: %s off", unit, from, offerScopeLabel(rule), s.label())
}

// offerFixedPriceDiscount sells every unit in scope at offer_price; a unit
// already cheaper keeps its price.
func offerFixedPriceDiscount(rule offerRule, lines []OfferCartLine) (float64, string) {
	if rule.offerPrice <= 0 {
		return 0, ""
	}
	discount, units := 0.0, 0
	for _, p := range offerQualifyingUnitPrices(rule, lines) {
		if p > rule.offerPrice {
			discount += p - rule.offerPrice
			units++
		}
	}
	if discount <= 0 {
		return 0, ""
	}
	return discount, fmt.Sprintf("%.2f each on %s (%d unit(s))", rule.offerPrice, offerScopeLabel(rule), units)
}

// offerComboIndex is the combo list a line belongs to (the first it is on),
// or -1.
func offerComboIndex(rule offerRule, line OfferCartLine) int {
	if offerExcluded(rule, line) {
		return -1
	}
	for i, s := range rule.comboSets {
		if offerSetHas(s, line) {
			return i
		}
	}
	return -1
}

// offerComboDiscount prices one unit from each combo list together at
// offer_price, as many combos as the cart completes. The dearest units are
// combined first, as for a bundle, so adding an item never raises the bill.
func offerComboDiscount(rule offerRule, lines []OfferCartLine) (float64, string) {
	n := len(rule.comboSets)
	if n < 2 || rule.offerPrice <= 0 {
		return 0, ""
	}
	pools := make([][]float64, n)
	for _, l := range lines {
		if k := offerComboIndex(rule, l); k >= 0 {
			for i := 0; i < l.Qty; i++ {
				pools[k] = append(pools[k], l.SalePrice)
			}
		}
	}
	combos := -1
	for _, p := range pools {
		sort.Sort(sort.Reverse(sort.Float64Slice(p)))
		if combos < 0 || len(p) < combos {
			combos = len(p)
		}
	}
	discount := 0.0
	for c := 0; c < combos; c++ {
		normal := 0.0
		for _, p := range pools {
			normal += p[c]
		}
		if normal > rule.offerPrice {
			discount += normal - rule.offerPrice
		}
	}
	if discount <= 0 {
		return 0, ""
	}
	return discount, fmt.Sprintf("combo of %d for %.2f (%d combo(s))", n, rule.offerPrice, combos)
}

// offerCodeSet is a list of codes (SKUs, item ids, barcodes, design numbers
// or design codes) as an upper-cased set; nil for a blank list.
func offerCodeSet(v string) map[string]bool {
	codes := splitSKUList(v)
	if len(codes) == 0 {
		return nil
	}
	out := make(map[string]bool, len(codes))
	for _, c := range codes {
		out[strings.ToUpper(c)] = true
	}
	return out
}

// offersUseCodeLists is true when a rule matches lines by design or by an
// item list other than its SKU List scope - the only time the cart's items
// and designs are looked up (resolveLineKeys).
func offersUseCodeLists(rules []offerRule) bool {
	for _, r := range rules {
		if r.scope == offerScopeDesignList || r.getSet != nil || r.excludeSet != nil || len(r.comboSets) > 0 {
			return true
		}
	}
	return false
}

// resolveLineKeys gives each cart line the codes a list can name it by: its
// item's id, code and barcode, and its design's reference, id, code and
// design number. One query for the cart's own items, so a list of thousands
// of codes costs nothing more at the till. Mutates lines in place, like
// resolveLineCategories. A failed lookup fails the evaluation: an exclusion
// that went unseen would widen an offer.
func resolveLineKeys(tenantID string, lines []OfferCartLine) error {
	skus := make([]string, 0, len(lines))
	for _, l := range lines {
		if s := strings.TrimSpace(l.Sku); s != "" {
			skus = append(skus, s)
		}
	}
	if len(skus) == 0 {
		return nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		WITH it AS (
		  SELECT id, COALESCE(data->>'code', '') AS code, COALESCE(data->>'barcode', '') AS barcode,
		         COALESCE(NULLIF(data->>'family', ''), NULLIF(data->>'parent_product_code', ''), '') AS ref
		    FROM %[1]s.documents
		   WHERE doctype = 'Item' AND deleted_at IS NULL AND (data->>'code' = ANY($1) OR id = ANY($1)))
		SELECT it.id, it.code, it.barcode, it.ref, COALESCE(f.id, ''), COALESCE(f.data->>'code', ''), COALESCE(f.data->>'design_number', '')
		  FROM it
		  LEFT JOIN LATERAL (
		    SELECT id, data FROM %[1]s.documents
		     WHERE doctype = 'ProductFamily' AND deleted_at IS NULL AND it.ref <> ''
		       AND (id = it.ref OR data->>'code' = it.ref)
		     LIMIT 1) f ON TRUE`, schema), pq.Array(skus))
	if err != nil {
		return err
	}
	defer rows.Close()
	keys := map[string][]string{}
	for rows.Next() {
		var id, code, barcode, ref, fid, fcode, fdn string
		if err := rows.Scan(&id, &code, &barcode, &ref, &fid, &fcode, &fdn); err != nil {
			return err
		}
		var k []string
		for _, v := range []string{id, code, barcode, ref, fid, fcode, fdn} {
			if v = strings.ToUpper(strings.TrimSpace(v)); v != "" {
				k = append(k, v)
			}
		}
		keys[id] = k
		if code != "" {
			keys[code] = k
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range lines {
		lines[i].keys = keys[strings.TrimSpace(lines[i].Sku)]
	}
	return nil
}

// offerUsesLeft (215.8) is false once an offer has reached its use limit, in
// total or for this customer. A use is a Paid or Settled bill the offer took
// money off; the bill being priced never counts itself. A per-customer limit
// needs a customer on the bill. A count that fails keeps the offer off.
func offerUsesLeft(tenantID string, rule offerRule, customerID, cartNumber string) bool {
	if rule.maxUses <= 0 && rule.maxUsesPerCustomer <= 0 {
		return true
	}
	customerID = strings.TrimSpace(customerID)
	if rule.maxUsesPerCustomer > 0 && customerID == "" {
		return false
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return false
	}
	probe, _ := json.Marshal(map[string]interface{}{"applied_offers": []map[string]string{{"offer_id": rule.id}}})
	q := fmt.Sprintf(`SELECT COUNT(*) FROM %s.documents
		WHERE doctype = 'POSCart' AND status IN ('Paid', 'Settled') AND deleted_at IS NULL
		  AND id <> $1 AND data @> $2::jsonb`, schema)
	if rule.maxUses > 0 {
		var n int
		if db.DB.QueryRow(q, cartNumber, string(probe)).Scan(&n) != nil || n >= rule.maxUses {
			return false
		}
	}
	if rule.maxUsesPerCustomer > 0 {
		var n int
		if db.DB.QueryRow(q+` AND data->>'customer_id' = $3`, cartNumber, string(probe), customerID).Scan(&n) != nil || n >= rule.maxUsesPerCustomer {
			return false
		}
	}
	return true
}

// validateOffer (215.7-215.9) checks an Offer has what its type needs, and
// that its conditions read. Fields of other types are not looked at, so an
// offer saved before a field existed stays editable.
func validateOffer(docData map[string]interface{}) error {
	bad := func(msg string) error { return &ValidationError{Code: "POSOFF-0882", Message: msg} }
	typ := strings.TrimSpace(strField(docData, "offer_type"))
	num := func(k string) float64 { return numFromInterface(docData[k]) }
	str := func(k string) string { return strings.TrimSpace(strField(docData, k)) }
	switch typ {
	case offerTypeBuyXGetY, offerTypeBuyAGetB:
		// Only these types read it: a box hidden by another type keeps its old
		// value, and that must not stop the save.
		if p := num("get_discount_pct"); p < 0 || p > 100 {
			return bad("Get % off must be from 0 to 100 (blank is free).")
		}
		if num("buy_qty") < 1 || num("get_qty") < 1 {
			return bad(typ + " needs a buy qty and a get qty of at least 1.")
		}
		if typ == offerTypeBuyAGetB && len(splitSKUList(str("get_list"))) == 0 {
			return bad("Buy A Get B needs the get items: the SKUs or designs that are discounted.")
		}
	case offerTypeSpendSlabs, offerTypeQtySlabs:
		slabs, err := parseOfferSlabs(str("slabs"))
		if err != nil {
			return bad(err.Error())
		}
		if len(slabs) == 0 {
			return bad(typ + " needs slabs, for example 2000=200; 5000=10%.")
		}
	case offerTypeFixedPrice:
		if num("offer_price") <= 0 {
			return bad("Fixed Price needs the offer price per unit.")
		}
	case offerTypeCombo:
		if len(splitOfferComboLists(str("combo_lists"))) < 2 {
			return bad("A combo needs at least two lists, one per line.")
		}
		if num("offer_price") <= 0 {
			return bad("A combo needs its offer price.")
		}
	}
	if strField(docData, "scope") == offerScopeDesignList && len(splitSKUList(str("scope_value"))) == 0 {
		return bad("Design List needs the design numbers.")
	}
	if _, err := parseOfferDays(str("days")); err != nil {
		return bad(err.Error())
	}
	for _, k := range []string{"hour_from", "hour_to"} {
		if _, err := parseOfferTime(str(k)); err != nil {
			return bad(err.Error())
		}
	}
	if num("max_uses") < 0 || num("max_uses_per_customer") < 0 {
		return bad("Use limits cannot be below 0.")
	}
	return nil
}
