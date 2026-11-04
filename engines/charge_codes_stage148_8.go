package engines

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"math/bits"
	"sort"
	"strconv"
	"strings"

	"custom_erp/db"
)

// Stage 148.8: charge codes that cost themselves.
//
// TradeChargeCode is the master, TradeChargeRule puts a code on lines by vendor
// group x item group, and the "charges" table on a PO, GRN or transfer holds the
// rows a person typed. When a receipt posts, every code that applies becomes one
// TradeChargeEntry: the amount, each line's share or the reason it has none, and
// the journal under that entry's own id. Everything is keyed on the entry id, the
// hash of receipt + part + code, so a retry finds it and posts nothing.
//
// With no TradeChargeCode on file the hook reads one EXISTS and leaves.

const (
	chargeEntryDoctype    = "TradeChargeEntry"
	chargeDefaultClearing = "2110"
	chargeBasisQty        = "Qty"
	chargeBasisWeight     = "Weight"
	chargeRepeatEvery     = "Every receipt"
	chargeScopeStocked    = "Stocked lines only"
	chargePayeeThirdParty = "Third party"
	chargeStatusEstimated = "Estimated"
	chargeStatusPosted    = "Posted"
	chargeStatusTruedUp   = "Trued up"
	chargeStatusReversed  = "Reversed"
	chargeStatusSkipped   = "Skipped"
)

type tradeChargeCode struct {
	ID, Code, Name, UsedFor, Kind, Payee, Carrier, Clearing, IntoCost, GL, Basis, Scope, Repeat string
	GSTRate                                                                                     float64
	Active                                                                                      bool
}

func (c *tradeChargeCode) label() string      { return firstNonEmpty(c.Code, c.ID) }
func (c *tradeChargeCode) forBuying() bool    { return c.UsedFor != "Selling" }
func (c *tradeChargeCode) thirdParty() bool   { return c.Payee == chargePayeeThirdParty }
func (c *tradeChargeCode) goesIntoCost() bool { return c.IntoCost != "No" }

func (c *tradeChargeCode) creditAccount() string {
	if c.thirdParty() {
		return firstNonEmpty(c.Clearing, chargeDefaultClearing)
	}
	return "2100"
}

type tradeChargeRule struct {
	ID, VendorGroup, ItemGroup, Code, Method string
	Rate                                     float64
}

func tradeChargeCodesOnFile(q queryRower, schema string) (bool, error) {
	var on bool
	err := q.QueryRow(fmt.Sprintf(`SELECT EXISTS (SELECT 1 FROM %s.documents WHERE doctype = 'TradeChargeCode' AND deleted_at IS NULL)`, schema)).Scan(&on)
	return on, err
}

func chargeMasterRows(q queryRower, schema, doctype string) (map[string]map[string]interface{}, []string, error) {
	rows, err := q.Query(fmt.Sprintf(`SELECT id, data FROM %s.documents WHERE doctype = $1 AND deleted_at IS NULL ORDER BY id`, schema), doctype)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	out := map[string]map[string]interface{}{}
	var ids []string
	for rows.Next() {
		var id string
		var raw []byte
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, nil, err
		}
		var d map[string]interface{}
		if json.Unmarshal(raw, &d) != nil {
			continue
		}
		out[id] = d
		ids = append(ids, id)
	}
	return out, ids, rows.Err()
}

// loadTradeChargeCodes is keyed by a code's document id and by its own code.
func loadTradeChargeCodes(q queryRower, schema string) (map[string]*tradeChargeCode, error) {
	docs, ids, err := chargeMasterRows(q, schema, "TradeChargeCode")
	if err != nil {
		return nil, err
	}
	out := map[string]*tradeChargeCode{}
	for _, id := range ids {
		d := docs[id]
		c := &tradeChargeCode{ID: id, Code: strField(d, "code"), Name: strField(d, "charge_name"), UsedFor: strField(d, "used_for"),
			Kind: strField(d, "charge_kind"), Payee: strField(d, "payee"), Carrier: strField(d, "carrier"), Clearing: strField(d, "clearing_account"),
			IntoCost: strField(d, "into_cost"), GL: strField(d, "gl_account"), Basis: strField(d, "basis"), Scope: strField(d, "scope"),
			Repeat: strField(d, "repeat_on"), GSTRate: numFromInterface(d["gst_rate"]), Active: strField(d, "status") != "Inactive"}
		out[id] = c
		if _, taken := out[c.Code]; c.Code != "" && !taken {
			out[c.Code] = c
		}
	}
	return out, nil
}

func loadTradeChargeRules(q queryRower, schema string) ([]*tradeChargeRule, error) {
	docs, ids, err := chargeMasterRows(q, schema, "TradeChargeRule")
	if err != nil {
		return nil, err
	}
	var out []*tradeChargeRule
	for _, id := range ids {
		d := docs[id]
		if strField(d, "status") == "Inactive" {
			continue
		}
		out = append(out, &tradeChargeRule{ID: id, VendorGroup: strings.TrimSpace(strField(d, "vendor_group")), ItemGroup: strings.TrimSpace(strField(d, "item_group")),
			Code: strField(d, "charge_code"), Method: strField(d, "method"), Rate: numFromInterface(d["rate"])})
	}
	return out, nil
}

func chargeDocQ(q queryRower, schema, doctype, id string) (map[string]interface{}, error) {
	var raw []byte
	if err := q.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents WHERE doctype = $1 AND id = $2 AND deleted_at IS NULL`, schema), doctype, id).Scan(&raw); err != nil {
		return nil, err
	}
	var d map[string]interface{}
	return d, json.Unmarshal(raw, &d)
}

// ---- Shares ----------------------------------------------------------------

// spreadPaise splits total over the weights, floor each, the last line with any
// weight taking the rounding, so the shares always sum to the total exactly. A
// line with no weight gets nothing. The landed cost voucher shares this.
func spreadPaise(total int64, weights []int64) []int64 {
	out := make([]int64, len(weights))
	var sum int64
	last := -1
	for i, w := range weights {
		if w > 0 {
			sum += w
			last = i
		}
	}
	if last < 0 {
		return out
	}
	abs, sign := total, int64(1)
	if total < 0 {
		abs, sign = -total, -1
	}
	var given int64
	for i, w := range weights {
		switch {
		case w <= 0:
		case i == last:
			out[i] = sign * (abs - given)
		default:
			part := mulDivPaise(abs, w, sum)
			given += part
			out[i] = sign * part
		}
	}
	return out
}

// mulDivPaise is a*b/c without the overflow. b must not exceed c.
func mulDivPaise(a, b, c int64) int64 {
	hi, lo := bits.Mul64(uint64(a), uint64(b))
	q, _ := bits.Div64(hi, lo, uint64(c))
	return int64(q)
}

func chargeMilli(v float64) int64 { return int64(math.Round(v * 1000)) }

// receiptChargeLine is one SKU a receipt puts into stock.
type receiptChargeLine struct {
	SKU     string
	Qty     float64
	Value   int64
	Costed  bool
	Stocked bool
	Group   string
	Grams   float64
}

func (l receiptChargeLine) weightBy(basis string) int64 {
	switch basis {
	case chargeBasisQty:
		return chargeMilli(l.Qty)
	case chargeBasisWeight:
		return chargeMilli(l.Grams)
	}
	return l.Value
}

func chargeBasisReason(basis string) string {
	switch basis {
	case chargeBasisQty:
		return "No quantity on this line"
	case chargeBasisWeight:
		return "No weight on this line"
	}
	return "No value on this line"
}

// finishChargeLines merges a receipt's lines by SKU and fills what only a charge
// needs. Called after the gate, so a tenant with no codes pays for none of it.
func finishChargeLines(tenantID string, in []receiptChargeLine, grams map[string]float64) []receiptChargeLine {
	bySKU := map[string]*receiptChargeLine{}
	var skus []string
	for _, l := range in {
		m := bySKU[l.SKU]
		if m == nil {
			m = &receiptChargeLine{SKU: l.SKU}
			bySKU[l.SKU] = m
			skus = append(skus, l.SKU)
		}
		m.Qty += l.Qty
		m.Value += l.Value
		m.Costed = m.Costed || l.Costed
	}
	sort.Strings(skus)
	out := make([]receiptChargeLine, 0, len(skus))
	for _, sku := range skus {
		m := bySKU[sku]
		m.Stocked = true
		if policy, _, known := ItemPolicyForSKU(tenantID, sku); known && !policy.Tracked() {
			m.Stocked = false
		}
		m.Group = itemGroupForSKU(tenantID, sku)
		m.Grams = grams[sku]
		out = append(out, *m)
	}
	return out
}

// grnChargeDocLines reads a GRN's own lines into charge lines, for what has not
// posted yet: the later children of a step GRN, and the estimate on its screen.
func grnChargeDocLines(tenantID string, lines []map[string]interface{}, keep func(l map[string]interface{}) bool) []receiptChargeLine {
	var in []receiptChargeLine
	grams := map[string]float64{}
	for _, l := range lines {
		qty := grnStepAccepted(l)
		if qty <= 0 || !keep(l) {
			continue
		}
		sku := strField(l, "sku")
		value, _ := grnOurLineAmount(l)
		in = append(in, receiptChargeLine{SKU: sku, Qty: qty, Value: RupeesToPaise(value), Costed: true})
		if g, ok := lineWeightGrams(l); ok {
			grams[sku] += g
		}
	}
	return finishChargeLines(tenantID, in, grams)
}

// ---- What a receipt owes ---------------------------------------------------

// chargeSlice is an amount to spread: a typed row, or one Fixed rule.
type chargeSlice struct {
	key    string
	amount int64
	reach  func(l receiptChargeLine) bool
}

// chargeAsk is one code on one receipt. Percent and Per unit rules are per line
// and follow what was received; rows and Fixed rules are slices.
type chargeAsk struct {
	key, raw      string
	code          *tradeChargeCode
	fromRule      bool
	typedHere     bool // typed on the GRN or transfer itself, always charged
	only, exclude map[string]bool
	slices        []*chargeSlice
	perLine       map[string]int64
	ruleLines     map[string]bool
	vendorGroup   string
}

type chargeAsks struct {
	byKey map[string]*chargeAsk
	order []string
}

func chargeSKUList(raw string) map[string]bool {
	out := map[string]bool{}
	for _, s := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ';' || r == '\n' }) {
		if s = strings.TrimSpace(s); s != "" {
			out[s] = true
		}
	}
	return out
}

func (s *chargeAsks) addRows(rows []map[string]interface{}, codes map[string]*tradeChargeCode, here bool) {
	for _, row := range rows {
		raw := strings.TrimSpace(strField(row, "charge_code"))
		amount := RupeesToPaise(numFromInterface(row["amount"]))
		if raw == "" || amount <= 0 {
			continue
		}
		code := codes[raw]
		key := raw
		if code != nil {
			key = code.ID
		}
		a := s.byKey[key]
		if a != nil && a.typedHere && !here {
			continue // the GRN's row replaces the PO's
		}
		if a == nil {
			a = &chargeAsk{key: key, raw: raw, code: code, typedHere: here, only: map[string]bool{}, exclude: map[string]bool{},
				slices: []*chargeSlice{{key: "row", reach: func(receiptChargeLine) bool { return true }}}}
			s.byKey[key] = a
			s.order = append(s.order, key)
		}
		a.slices[0].amount += amount
		for sku := range chargeSKUList(strField(row, "only_lines")) {
			a.only[sku] = true
		}
		for sku := range chargeSKUList(strField(row, "exclude_lines")) {
			a.exclude[sku] = true
		}
	}
}

// addRules lets the rules reach the lines. Per code and line the most specific
// rule wins: vendor group + item group, vendor group, item group, any.
func (s *chargeAsks) addRules(rules []*tradeChargeRule, codes map[string]*tradeChargeCode, lines []receiptChargeLine, vendorGroup string) {
	byCode := map[string][]*tradeChargeRule{}
	var keys []string
	for _, r := range rules {
		code := codes[r.Code]
		if code == nil || !code.Active || !code.forBuying() || s.byKey[code.ID] != nil {
			continue
		}
		if byCode[code.ID] == nil {
			keys = append(keys, code.ID)
		}
		byCode[code.ID] = append(byCode[code.ID], r)
	}
	sort.Strings(keys)
	for _, key := range keys {
		rs := byCode[key]
		best := func(group string) *tradeChargeRule {
			var found *tradeChargeRule
			score := -1
			for _, r := range rs {
				if (r.VendorGroup != "" && !strings.EqualFold(r.VendorGroup, vendorGroup)) || (r.ItemGroup != "" && !strings.EqualFold(r.ItemGroup, group)) {
					continue
				}
				n := 0
				if r.VendorGroup != "" {
					n += 2
				}
				if r.ItemGroup != "" {
					n++
				}
				if n > score { // rules come in id order, so a tie keeps the lower id
					found, score = r, n
				}
			}
			return found
		}
		a := &chargeAsk{key: key, raw: key, code: codes[key], fromRule: true, only: map[string]bool{}, exclude: map[string]bool{},
			perLine: map[string]int64{}, ruleLines: map[string]bool{}, vendorGroup: vendorGroup}
		fixed := map[string]bool{}
		for _, l := range lines {
			r := best(l.Group)
			if r == nil {
				continue
			}
			a.ruleLines[l.SKU] = true
			switch r.Method {
			case "Percent":
				a.perLine[l.SKU] = sharePaise(l.Value, r.Rate, 100)
			case "Per unit":
				a.perLine[l.SKU] = RupeesToPaise(r.Rate * l.Qty)
			case "Fixed":
				if !fixed[r.ID] {
					fixed[r.ID] = true
					rule := r
					a.slices = append(a.slices, &chargeSlice{key: rule.ID, amount: RupeesToPaise(rule.Rate),
						reach: func(l receiptChargeLine) bool { return best(l.Group) == rule }})
				}
			}
		}
		if len(a.ruleLines) == 0 {
			continue // reached nothing here: no entry, the view says why
		}
		s.byKey[key] = a
		s.order = append(s.order, key)
	}
}

func chargeNoRuleReason(vendorGroup, itemGroup string) string {
	return fmt.Sprintf("No rule for vendor group %s and item group %s", firstNonEmpty(vendorGroup, "(none)"), firstNonEmpty(itemGroup, "(none)"))
}

func (a *chargeAsk) scopeReason(l receiptChargeLine) string {
	switch {
	case a.code != nil && a.code.Scope == chargeScopeStocked && !l.Stocked:
		return "Not a stocked line"
	case a.exclude[l.SKU]:
		return "Left out on the document"
	case len(a.only) > 0 && !a.only[l.SKU]:
		return "Not named on the document"
	case !l.Costed:
		return "No cost on this line"
	}
	return ""
}

// chargeVendorBears says why freight or insurance is the vendor's. The PO's own
// freight term comes first, then the vendor's FOB / CIF. Only a rule asks: a row
// someone typed is a charge someone decided on.
func chargeVendorBears(c *tradeChargeCode, pos []map[string]interface{}, terms string) string {
	if c == nil {
		return ""
	}
	if c.Kind == "Freight" {
		for _, po := range pos {
			switch strField(po, "freight_by") {
			case "Vendor":
				return "Vendor bears freight on this PO"
			case "Buyer":
				return ""
			}
		}
	}
	if (c.Kind == "Freight" || c.Kind == "Insurance") && terms == "CIF" {
		return "CIF: the vendor bears this"
	}
	return ""
}

// chargePlan is what one code does to one receipt.
type chargePlan struct {
	ask    *chargeAsk
	Lines  []receiptChargeLine
	Shares map[string]int64
	Why    map[string]string
	Spread map[string]int64 // what each slice put on this part
	Total  int64
	Skip   string
}

func (p *chargePlan) skipAll(reason string) *chargePlan {
	p.Skip = reason
	for _, l := range p.Lines {
		if p.Why[l.SKU] == "" {
			p.Why[l.SKU] = reason
		}
	}
	return p
}

// chargedElsewhere finds another receipt of the same POs that already carried
// this code's fixed amount.
func chargedElsewhere(q queryRower, schema, key, sourceID string, poIDs []string) (string, error) {
	for _, po := range poIDs {
		ref, _ := json.Marshal([]string{po})
		var other string
		err := q.QueryRow(fmt.Sprintf(`SELECT data->>'source_id' FROM %s.documents
			WHERE doctype = $1 AND deleted_at IS NULL AND status <> $2 AND data->>'code_id' = $3 AND data->>'source_id' <> $4
			  AND COALESCE((data->>'spread_paise')::bigint, 0) > 0 AND data->'po_ids' @> $5::jsonb
			ORDER BY created_at, id LIMIT 1`, schema),
			chargeEntryDoctype, chargeStatusSkipped, key, sourceID, string(ref)).Scan(&other)
		if err == nil {
			return other, nil
		}
		if err != sql.ErrNoRows {
			return "", err
		}
	}
	return "", nil
}

// earlierSpread is what the source's other parts already took of each slice.
func earlierSpread(q queryRower, schema, sourceID, key, part string) (map[string]int64, error) {
	rows, err := q.Query(fmt.Sprintf(`SELECT data->'spread' FROM %s.documents
		WHERE doctype = $1 AND deleted_at IS NULL AND status <> $2 AND data->>'source_id' = $3 AND data->>'code_id' = $4 AND COALESCE(data->>'part', '') <> $5`, schema),
		chargeEntryDoctype, chargeStatusSkipped, sourceID, key, part)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var m map[string]int64
		_ = json.Unmarshal(raw, &m)
		for k, v := range m {
			out[k] += v
		}
	}
	return out, rows.Err()
}

// planCharge works out one code. part is the child of a step GRN, later what its
// other children have still to post: this part takes its slice of what is left,
// so the parts sum to the charge however the quantities moved in between.
func planCharge(q queryRower, schema, sourceID, part string, a *chargeAsk, lines, later []receiptChargeLine, bears string, poIDs []string) (*chargePlan, error) {
	p := &chargePlan{ask: a, Lines: lines, Shares: map[string]int64{}, Why: map[string]string{}, Spread: map[string]int64{}}
	switch {
	case a.code == nil:
		return p.skipAll(fmt.Sprintf("Code %s is not on file", a.raw)), nil
	case !a.code.Active:
		return p.skipAll(fmt.Sprintf("Code %s is inactive", a.code.label())), nil
	case !a.code.forBuying():
		return p.skipAll(fmt.Sprintf("Code %s is for selling only", a.code.label())), nil
	case a.fromRule && bears != "":
		return p.skipAll(bears), nil
	}
	slices := a.slices
	var once string
	if len(slices) > 0 && !a.typedHere && a.code.Repeat != chargeRepeatEvery {
		other, err := chargedElsewhere(q, schema, a.key, sourceID, poIDs)
		if err != nil {
			return nil, err
		}
		if other != "" {
			once, slices = "Once: charged on GRN "+other, nil
		}
	}
	earlier := map[string]int64{}
	if part != "" && len(slices) > 0 {
		var err error
		if earlier, err = earlierSpread(q, schema, sourceID, a.key, part); err != nil {
			return nil, err
		}
	}
	basis := a.code.Basis
	for _, s := range slices {
		var in []int
		var weights []int64
		var base, laterBase int64
		for i, l := range lines {
			if a.scopeReason(l) != "" || !s.reach(l) {
				continue
			}
			if w := l.weightBy(basis); w > 0 {
				in, weights = append(in, i), append(weights, w)
				base += w
			}
		}
		for _, l := range later {
			if a.scopeReason(l) == "" && s.reach(l) {
				if w := l.weightBy(basis); w > 0 {
					laterBase += w
				}
			}
		}
		amount := s.amount
		if part != "" {
			amount = max(s.amount-earlier[s.key], 0)
			if laterBase > 0 {
				amount = mulDivPaise(amount, base, base+laterBase)
			}
		}
		if len(in) == 0 || amount <= 0 {
			continue
		}
		p.Spread[s.key] = amount
		for n, share := range spreadPaise(amount, weights) {
			p.Shares[lines[in[n]].SKU] += share
		}
	}
	for _, l := range lines {
		reason := a.scopeReason(l)
		if reason == "" {
			p.Shares[l.SKU] += a.perLine[l.SKU]
		}
		p.Total += p.Shares[l.SKU]
		if p.Shares[l.SKU] > 0 {
			continue
		}
		switch {
		case reason != "":
			p.Why[l.SKU] = reason
		case a.fromRule && !a.ruleLines[l.SKU]:
			p.Why[l.SKU] = chargeNoRuleReason(a.vendorGroup, l.Group)
		case once != "":
			p.Why[l.SKU] = once
		default:
			p.Why[l.SKU] = chargeBasisReason(basis)
		}
	}
	if p.Total <= 0 {
		reason := once
		for _, l := range lines {
			if reason == "" {
				reason = p.Why[l.SKU]
			}
		}
		return p.skipAll(firstNonEmpty(reason, "No line to carry it")), nil
	}
	return p, nil
}

func chargeVendorOf(grn map[string]interface{}, pos []map[string]interface{}) string {
	id := firstNonEmpty(strField(grn, "vendor_id"), strField(grn, "vendor"))
	for _, po := range pos {
		id = firstNonEmpty(id, strField(po, "vendor"), strField(po, "vendor_id"))
	}
	return id
}

func chargePOsOf(q queryRower, schema string, grn map[string]interface{}) ([]string, []map[string]interface{}, error) {
	poIDs := GRNPOIDs(grn)
	sort.Strings(poIDs)
	var pos []map[string]interface{}
	for _, id := range poIDs {
		po, err := chargeDocQ(q, schema, "PurchaseOrder", id)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		pos = append(pos, po)
	}
	return poIDs, pos, nil
}

func planReceiptCharges(q queryRower, tenantID, schema, grnID string, grn map[string]interface{}, part string, lines, later []receiptChargeLine) ([]*chargePlan, error) {
	codes, err := loadTradeChargeCodes(q, schema)
	if err != nil || len(codes) == 0 {
		return nil, err
	}
	poIDs, pos, err := chargePOsOf(q, schema, grn)
	if err != nil {
		return nil, err
	}
	vendorID := chargeVendorOf(grn, pos)
	asks := &chargeAsks{byKey: map[string]*chargeAsk{}}
	asks.addRows(jsonRows(grn["charges"]), codes, true)
	for _, po := range pos {
		asks.addRows(jsonRows(po["charges"]), codes, false)
	}
	rules, err := loadTradeChargeRules(q, schema)
	if err != nil {
		return nil, err
	}
	if len(rules) > 0 {
		asks.addRules(rules, codes, lines, partyGroup(tenantID, "Vendor", "vendor_group", vendorID))
	}
	if len(asks.order) == 0 {
		return nil, nil
	}
	terms := partyGroup(tenantID, "Vendor", "freight_terms", vendorID)
	var plans []*chargePlan
	for _, key := range asks.order {
		a := asks.byKey[key]
		plan, err := planCharge(q, schema, grnID, part, a, lines, later, chargeBearsFor(a, pos, terms), poIDs)
		if err != nil {
			return nil, err
		}
		plans = append(plans, plan)
	}
	return plans, nil
}

func chargeBearsFor(a *chargeAsk, pos []map[string]interface{}, terms string) string {
	if !a.fromRule {
		return ""
	}
	return chargeVendorBears(a.code, pos, terms)
}

// ---- Posting ---------------------------------------------------------------

type chargeEntrySource struct {
	Doctype, ID, GRNID, Part, Location, VendorID string
	POIDs                                        []string
}

func chargeEntryID(source, part, key string) string {
	sum := sha256.Sum256([]byte(source + "|" + part + "|" + key))
	return "CHG-" + hex.EncodeToString(sum[:])[:20]
}

func chargeRupees(paise int64) string { return strconv.FormatFloat(PaiseToRupees(paise), 'f', 2, 64) }

func chargeEntryNote(lines []map[string]interface{}) string {
	parts := make([]string, 0, len(lines))
	for _, l := range lines {
		if share := int64(numFromInterface(l["share"])); share > 0 {
			parts = append(parts, fmt.Sprintf("%s Rs %s", strField(l, "sku"), chargeRupees(share)))
		} else {
			parts = append(parts, fmt.Sprintf("%s none: %s", strField(l, "sku"), strField(l, "reason")))
		}
	}
	note := []rune(strings.Join(parts, "; "))
	if len(note) > 240 {
		return string(note[:237]) + "..."
	}
	return string(note)
}

// postChargeEntryTx writes the entry and its journal. The insert is the gate: an
// entry that is already there was posted by an earlier try, and nothing runs.
func postChargeEntryTx(tx *sql.Tx, tenantID, schema string, src chargeEntrySource, p *chargePlan) error {
	id := chargeEntryID(src.ID, src.Part, p.ask.key)
	code := p.ask.code
	lines := make([]map[string]interface{}, 0, len(p.Lines))
	for _, l := range p.Lines {
		share := float64(p.Shares[l.SKU])
		if p.Skip != "" {
			share = 0
		}
		line := map[string]interface{}{"sku": l.SKU, "qty": l.Qty, "share": share, "returned_qty": 0.0, "returned_paise": 0.0}
		if why := p.Why[l.SKU]; why != "" {
			line["reason"] = why
		}
		lines = append(lines, line)
	}
	var spreadTotal int64
	for _, v := range p.Spread {
		spreadTotal += v
	}
	data := map[string]interface{}{"code": id, "code_id": p.ask.key, "source_doctype": src.Doctype, "source_id": src.ID, "grn_id": src.GRNID, "part": src.Part,
		"po_ids": src.POIDs, "location": src.Location, "charge_code": p.ask.raw, "lines": lines, "note": chargeEntryNote(lines),
		"amount": 0.0, "amount_paise": 0, "open_amount": 0.0, "open_paise": 0, "returned_paise": 0, "spread": p.Spread, "spread_paise": spreadTotal}
	status := chargeStatusSkipped
	if code != nil {
		data["charge_code"], data["charge_label"], data["kind"], data["payee"] = code.label(), code.Name, code.Kind, code.Payee
		data["into_cost"], data["gl_account"], data["basis"] = code.IntoCost, code.GL, firstNonEmpty(code.Basis, "Value")
		data["gst_rate"], data["repeat_on"] = code.GSTRate, firstNonEmpty(code.Repeat, "Once")
		data["credit_account"] = code.creditAccount()
		data["vendor_id"] = src.VendorID
		if code.thirdParty() {
			data["vendor_id"] = code.Carrier
		}
	}
	if p.Skip != "" {
		data["skip_reason"] = p.Skip
		data["spread"], data["spread_paise"] = map[string]int64{}, 0
	} else {
		data["amount"], data["amount_paise"] = PaiseToRupees(p.Total), p.Total
		status = chargeStatusPosted
		if code.thirdParty() {
			status = chargeStatusEstimated
			data["open_amount"], data["open_paise"] = PaiseToRupees(p.Total), p.Total
		}
	}
	data["status"] = status
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	var inserted string
	err = tx.QueryRow(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system')
		ON CONFLICT (id) DO NOTHING RETURNING id`, schema), id, chargeEntryDoctype, raw, status).Scan(&inserted)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return fmt.Errorf("charge entry: %w", err)
	}
	if p.Skip != "" {
		return nil
	}
	legs := map[string]int64{code.creditAccount(): -p.Total}
	if code.goesIntoCost() {
		for _, l := range p.Lines {
			if share := p.Shares[l.SKU]; share != 0 {
				if _, _, err := addCostAdjustmentLegsTx(tx, tenantID, schema, l.SKU, src.GRNID, l.Qty, share, legs); err != nil {
					return fmt.Errorf("charge %s on %s: %w", code.label(), l.SKU, err)
				}
			}
		}
	} else {
		legs[code.GL] += p.Total
	}
	if err := postSignedLegsTx(tx, tenantID, schema, chargeEntryDoctype, id, legs, fmt.Sprintf("%s:%s:POST", chargeEntryDoctype, id)); err != nil {
		return fmt.Errorf("charge %s journal: %w", code.label(), err)
	}
	return nil
}

// applyReceiptChargesTx is the one place a receipt takes its charges: every way
// in (the one-step GRN, the steps, the warehouse and store app paths) comes through
// recordGRNReceiptCostingPartTx, which calls this once the goods have posted.
func applyReceiptChargesTx(tx *sql.Tx, tenantID, schema, grnID, part string, in []receiptChargeLine) error {
	if on, err := tradeChargeCodesOnFile(tx, schema); err != nil || !on {
		return err
	}
	grn, err := chargeDocQ(tx, schema, "GRN", grnID)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	docLines := grnLineMaps(grn)
	grams := map[string]float64{}
	for _, l := range docLines {
		if part != "" && strField(l, "child") != part {
			continue
		}
		if g, ok := lineWeightGrams(l); ok {
			grams[strField(l, "sku")] += g
		}
	}
	lines := finishChargeLines(tenantID, in, grams)
	var later []receiptChargeLine
	if part != "" {
		later = grnChargeDocLines(tenantID, docLines, func(l map[string]interface{}) bool {
			return strField(l, "child") != part && strField(l, "posted_at") == ""
		})
	}
	poIDs, pos, err := chargePOsOf(tx, schema, grn)
	if err != nil {
		return err
	}
	// Two receipts of one PO at once must not both be "the first".
	for _, po := range poIDs {
		if _, err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext($1))`, schema+":charge-once:"+po); err != nil {
			return err
		}
	}
	plans, err := planReceiptCharges(tx, tenantID, schema, grnID, grn, part, lines, later)
	if err != nil {
		return fmt.Errorf("charges: %w", err)
	}
	if len(plans) == 0 {
		return nil
	}
	if poIDs == nil {
		poIDs = []string{}
	}
	src := chargeEntrySource{Doctype: "GRN", ID: grnID, GRNID: grnID, Part: part, POIDs: poIDs, Location: strField(grn, "location"), VendorID: chargeVendorOf(grn, pos)}
	for _, plan := range plans {
		if err := postChargeEntryTx(tx, tenantID, schema, src, plan); err != nil {
			return err
		}
	}
	return nil
}

// applyTransferChargesTx puts a transfer's typed charges on the units this receipt
// brought in. Cost is company-wide (148.1 D1), so the freight raises the item's
// average; the journal carries the receiving store through the entry's location.
func applyTransferChargesTx(tx *sql.Tx, tenantID, schema, transferID string, data map[string]interface{}, eventNo int, toWarehouse string, received map[string]float64) error {
	rows := jsonRows(data["charges"])
	if len(rows) == 0 {
		return nil
	}
	codes, err := loadTradeChargeCodes(tx, schema)
	if err != nil {
		return err
	}
	grams := map[string]float64{}
	for _, l := range jsonRows(data["items"]) {
		if g, ok := lineWeightGrams(l); ok {
			grams[strField(l, "sku")] += g
		}
	}
	var in []receiptChargeLine
	for sku, qty := range received {
		if qty <= 0 {
			continue
		}
		cost, ok := ResolveQuoteUnitCostPaise(tenantID, sku)
		in = append(in, receiptChargeLine{SKU: sku, Qty: qty, Value: sharePaise(cost, qty, 1), Costed: ok && cost > 0})
	}
	if len(in) == 0 {
		return nil
	}
	lines := finishChargeLines(tenantID, in, grams)
	asks := &chargeAsks{byKey: map[string]*chargeAsk{}}
	asks.addRows(rows, codes, true)
	part := strconv.Itoa(eventNo)
	src := chargeEntrySource{Doctype: "TransferOrder", ID: transferID, Part: part, POIDs: []string{}, Location: toWarehouse}
	for _, key := range asks.order {
		a := asks.byKey[key]
		var plan *chargePlan
		if a.code != nil && a.code.Repeat != chargeRepeatEvery && eventNo > 1 {
			done, err := earlierSpread(tx, schema, transferID, key, part)
			if err != nil {
				return err
			}
			if done["row"] > 0 {
				plan = (&chargePlan{ask: a, Lines: lines, Shares: map[string]int64{}, Why: map[string]string{}, Spread: map[string]int64{}}).skipAll("Once: charged on an earlier receipt")
			}
		}
		if plan == nil {
			// The whole amount on this receipt: "" keeps the part slicing out of it.
			if plan, err = planCharge(tx, schema, transferID, "", a, lines, nil, "", nil); err != nil {
				return err
			}
		}
		if err := postChargeEntryTx(tx, tenantID, schema, src, plan); err != nil {
			return err
		}
	}
	return nil
}

// ---- Saving the master -----------------------------------------------------

func chargeAccount(schema, code, field string) (string, error) {
	flags, err := glAccountFlagsQ(db.DB, schema, []string{code})
	if err != nil {
		return "", err
	}
	f, ok := flags[code]
	if !ok || !f.Exists {
		return "", &ValidationError{Code: "FIN-0890", SubFor: field, Message: fmt.Sprintf("account %s is not in the chart of accounts", code)}
	}
	if f.Blocked {
		return "", &ValidationError{Code: "FIN-0890", SubFor: field, Message: fmt.Sprintf("account %s is blocked", code)}
	}
	var kind string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT account_type FROM %s.gl_accounts WHERE account_code = $1`, schema), code).Scan(&kind); err != nil {
		return "", err
	}
	return kind, nil
}

func validateTradeChargeCode(tenantID string, d map[string]interface{}) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	usedFor := strField(d, "used_for")
	buying, selling := usedFor != "Selling", usedFor == "Selling" || usedFor == "Both"
	payee, intoCost := strField(d, "payee"), strField(d, "into_cost")
	if buying && payee == "" {
		return &ValidationError{Code: "FIN-0892", SubFor: "Paid to", Message: "say who is paid: the vendor or a third party"}
	}
	if buying && intoCost == "" {
		return &ValidationError{Code: "FIN-0892", SubFor: "Into item cost", Message: "say whether this goes into item cost"}
	}
	if gst := numFromInterface(d["gst_rate"]); gst < 0 || gst > 100 {
		return &ValidationError{Code: "FIN-0893", SubFor: "GST rate %", Message: fmt.Sprintf("GST rate %v is not between 0 and 100", gst)}
	}
	gl := strings.TrimSpace(strField(d, "gl_account"))
	if gl == "" && (selling || intoCost == "No") {
		return &ValidationError{Code: "FIN-0890", SubFor: "GL account", Message: "this charge needs a GL account"}
	}
	if gl != "" {
		kind, err := chargeAccount(schema, gl, "GL account")
		if err != nil {
			return err
		}
		bad := kind == "Equity" || kind == "Liability"
		if !selling {
			bad = bad || kind == "Revenue"
		}
		if bad {
			return &ValidationError{Code: "FIN-0891", SubFor: "GL account", Message: fmt.Sprintf("account %s is a %s account and cannot carry this charge", gl, strings.ToLower(kind))}
		}
	}
	if buying && payee == chargePayeeThirdParty {
		clearing := firstNonEmpty(strings.TrimSpace(strField(d, "clearing_account")), chargeDefaultClearing)
		kind, err := chargeAccount(schema, clearing, "Clearing account")
		if err != nil {
			return err
		}
		if kind != "Liability" {
			return &ValidationError{Code: "FIN-0891", SubFor: "Clearing account", Message: fmt.Sprintf("clearing account %s must be a liability account", clearing)}
		}
		if clearing == gl {
			return &ValidationError{Code: "FIN-0891", SubFor: "Clearing account", Message: "the clearing account cannot also be the GL account"}
		}
	}
	return nil
}

func validateTradeChargeRule(tenantID string, d map[string]interface{}) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	codes, err := loadTradeChargeCodes(db.DB, schema)
	if err != nil {
		return err
	}
	raw := strField(d, "charge_code")
	code := codes[raw]
	if code == nil || !code.Active {
		return &ValidationError{Code: "PURCHA-4980", SubFor: "Charge code", Message: fmt.Sprintf("charge code %s is not an active code", raw)}
	}
	if !code.forBuying() {
		return &ValidationError{Code: "PURCHA-4981", SubFor: "Charge code", Message: fmt.Sprintf("charge code %s is for selling only", code.label())}
	}
	rate := numFromInterface(d["rate"])
	if rate <= 0 {
		return &ValidationError{Code: "PURCHA-4984", SubFor: "Rate", Message: "a rule needs a rate above nought"}
	}
	if strField(d, "method") == "Percent" && rate > 100 {
		return &ValidationError{Code: "PURCHA-4984", SubFor: "Rate", Message: "a percent cannot be over 100"}
	}
	return nil
}

func validateTradeChargeRows(tenantID, doctype string, d map[string]interface{}) error {
	rows := jsonRows(d["charges"])
	if len(rows) == 0 {
		return nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	codes, err := loadTradeChargeCodes(db.DB, schema)
	if err != nil {
		return err
	}
	lineField := "items"
	if doctype == "GRN" {
		lineField = "received_items"
	}
	onDoc := map[string]bool{}
	for _, l := range jsonRows(d[lineField]) {
		onDoc[strField(l, "sku")] = true
	}
	status := strField(d, "status")
	for i, row := range rows {
		raw := strField(row, "charge_code")
		sub := fmt.Sprintf("Charges row %d", i+1)
		code := codes[raw]
		if code == nil {
			return &ValidationError{Code: "PURCHA-4980", SubFor: sub, Message: fmt.Sprintf("charge code %q is not on file", raw)}
		}
		// Only a new document is refused: a code retired later must not lock a PO
		// already on its way. Its receipt says why it went uncharged.
		if !code.Active && (status == "" || status == "Draft" || (doctype == "GRN" && status == "Pending")) {
			return &ValidationError{Code: "PURCHA-4980", SubFor: sub, Message: fmt.Sprintf("charge code %s is inactive", code.label())}
		}
		if !code.forBuying() {
			return &ValidationError{Code: "PURCHA-4981", SubFor: sub, Message: fmt.Sprintf("charge code %s is for selling only", code.label())}
		}
		if numFromInterface(row["amount"]) <= 0 {
			return &ValidationError{Code: "PURCHA-4982", SubFor: sub, Message: "a charge needs an amount above nought"}
		}
		if len(onDoc) > 0 {
			for _, field := range []string{"only_lines", "exclude_lines"} {
				for sku := range chargeSKUList(strField(row, field)) {
					if !onDoc[sku] {
						return &ValidationError{Code: "PURCHA-4982", SubFor: sub, Message: fmt.Sprintf("%s is not a line on this document", sku)}
					}
				}
			}
		}
		if doctype == "TransferOrder" && !code.thirdParty() {
			return &ValidationError{Code: "PURCHA-4983", SubFor: sub, Message: fmt.Sprintf("charge code %s pays the vendor, and a transfer has none: use a third party code", code.label())}
		}
	}
	return nil
}

// validateTradeChargeDocument runs at ValidateDocument's shared exit, so the
// generic form, a CSV import and an engine all meet the same checks.
func validateTradeChargeDocument(tenantID, doctype string, d map[string]interface{}) error {
	switch doctype {
	case "TradeChargeCode":
		return validateTradeChargeCode(tenantID, d)
	case "TradeChargeRule":
		return validateTradeChargeRule(tenantID, d)
	case chargeEntryDoctype:
		return &ValidationError{Code: "FIN-0894", Message: "charge entries are written by the receipts; they cannot be saved by hand"}
	case "PurchaseOrder", "GRN", "TransferOrder":
		return validateTradeChargeRows(tenantID, doctype, d)
	}
	return nil
}
