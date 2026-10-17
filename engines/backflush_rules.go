package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// Sales-triggered automatic consumption (Stage 72.3-72.5, the user's
// blueprint section 9; the first request was "Build this also for pos" on the
// backflush note). A BackflushRule maps a sold line - by item, attribute,
// category, item type or every item - to a component the store uses up with
// it: Small Red Box for earrings, one butter paper for every packed product.
// When the sale posts, the components leave store stock inside the sale's own
// transaction, linked to the bill, and the cashier never scans them.
//
// Rules in the same group (Box, Butter paper...) compete: the most specific
// wins (blueprint 9.2 - item, attribute, category, item type, all items; a
// store's own rule before the all-stores one; then the higher priority
// number). Rules in different groups add up. The cashier can switch a line's
// default to another component of the same group, or to none, where the rule
// allows it (9.4).

// Backflush rule bases, in specificity order.
const (
	BackflushBasisItem      = "Item"
	BackflushBasisAttribute = "Attribute"
	BackflushBasisCategory  = "Category"
	BackflushBasisItemType  = "Item Type"
	BackflushBasisAll       = "All items"

	ShortStockAllow = "allow"
	ShortStockWarn  = "warn"
	ShortStockBlock = "block"
)

var backflushBasisRank = map[string]int{
	BackflushBasisItem: 0, BackflushBasisAttribute: 1, BackflushBasisCategory: 2,
	BackflushBasisItemType: 3, BackflushBasisAll: 4,
}

type backflushRule struct {
	ID              string
	Version         string
	Basis           string
	BasisValue      string
	basisAliases    map[string]bool // lower-case values this rule's basis matches
	Location        string
	Component       string // SKU code
	ComponentName   string
	Group           string
	QtyPerUnit      int
	Priority        float64
	OverrideAllowed bool
	ReasonRequired  bool
	NoneAllowed     bool
	ShortStock      string
	From, To        string
}

// rank orders candidates within a group: lower wins.
func (r backflushRule) rank() int {
	rank := backflushBasisRank[r.Basis] * 2
	if r.Location == "" {
		rank++
	}
	return rank
}

// ConsumptionChoice is one component a cashier may switch to.
type ConsumptionChoice struct {
	Component string `json:"component"`
	Name      string `json:"name"`
}

// ConsumptionLine is one sold line x group: the component consumed with it.
// Stored on the POSCart (without Choices) so the bill keeps the rule and
// version it was posted under - a later rule edit never rewrites history.
type ConsumptionLine struct {
	ForSKU           string              `json:"for_sku"`
	Group            string              `json:"group"`
	RuleID           string              `json:"rule_id"`
	RuleVersion      string              `json:"rule_version"`
	DefaultComponent string              `json:"default_component"`
	DefaultName      string              `json:"default_name,omitempty"`
	Component        string              `json:"component"`
	ComponentName    string              `json:"component_name,omitempty"`
	QtyPerUnit       int                 `json:"qty_per_unit"`
	Qty              int                 `json:"qty"`
	Overridden       bool                `json:"overridden,omitempty"`
	Reason           string              `json:"reason,omitempty"`
	ShortStock       string              `json:"short_stock_action"`
	OverrideAllowed  bool                `json:"override_allowed"`
	ReasonRequired   bool                `json:"reason_required"`
	NoneAllowed      bool                `json:"none_allowed"`
	Choices          []ConsumptionChoice `json:"choices,omitempty"`
}

// ConsumptionOverride is the cashier's change to one line x group.
type ConsumptionOverride struct {
	Sku       string `json:"sku"`
	Group     string `json:"group"`
	Component string `json:"component"` // "" = no component (No box)
	Reason    string `json:"reason"`
}

// ConsumptionShortage is a component the store does not hold enough of.
type ConsumptionShortage struct {
	Component string `json:"component"`
	Name      string `json:"name"`
	Needed    int    `json:"needed"`
	Available int    `json:"available"`
	Action    string `json:"action"`
}

// ConsumptionLineInput is a priced sale line.
type ConsumptionLineInput struct {
	Sku string
	Qty int
}

// loadBackflushRules reads the tenant's Active rules in effect on asOf for a
// sale at location.
func loadBackflushRules(tenantID, location string, asOf time.Time) ([]backflushRule, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, data::text, COALESCE(version, 1) FROM %s.documents
		WHERE doctype = 'BackflushRule' AND deleted_at IS NULL AND COALESCE(status, 'Active') = 'Active'`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	day := asOf.Format("2006-01-02")
	var out []backflushRule
	for rows.Next() {
		var id, raw string
		var version int
		if rows.Scan(&id, &raw, &version) != nil {
			continue
		}
		var d map[string]interface{}
		if json.Unmarshal([]byte(raw), &d) != nil {
			continue
		}
		r := parseBackflushRule(id, d)
		r.Version = fmt.Sprintf("%s@v%d", id, version)
		if r.Component == "" || r.QtyPerUnit <= 0 {
			continue
		}
		if r.Location != "" && !strings.EqualFold(r.Location, location) {
			continue
		}
		if (r.From != "" && day < r.From) || (r.To != "" && day > r.To) {
			continue
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		out[i].basisAliases = backflushBasisAliases(tenantID, out[i])
		if item, err := ResolveItemBySKU(tenantID, out[i].Component); err == nil && item != nil {
			if code := strings.TrimSpace(pimString(item.Data["code"])); code != "" {
				out[i].Component = code
			}
			out[i].ComponentName = itemLabel(item.Data, out[i].Component)
		}
	}
	return out, nil
}

func parseBackflushRule(id string, d map[string]interface{}) backflushRule {
	r := backflushRule{
		ID:              id,
		Basis:           normalizeBackflushBasis(pimString(d["basis"])),
		BasisValue:      strings.TrimSpace(pimString(d["basis_value"])),
		Location:        strings.TrimSpace(pimString(d["location"])),
		Component:       strings.TrimSpace(pimString(d["component_item"])),
		Group:           strings.TrimSpace(pimString(d["group"])),
		QtyPerUnit:      int(math.Round(numFromInterface(d["qty_per_unit"]))),
		Priority:        numFromInterface(d["priority"]),
		OverrideAllowed: !strings.EqualFold(strings.TrimSpace(pimString(d["override_allowed"])), "No"),
		ReasonRequired:  yesValue(d["reason_required"]),
		NoneAllowed:     !strings.EqualFold(strings.TrimSpace(pimString(d["no_component_allowed"])), "No"),
		ShortStock:      shortStockAction(pimString(d["short_stock_action"])),
		From:            ruleDay(pimString(d["effective_from"])),
		To:              ruleDay(pimString(d["effective_to"])),
	}
	if _, set := d["qty_per_unit"]; !set || pimString(d["qty_per_unit"]) == "" {
		r.QtyPerUnit = 1
	}
	if r.Group == "" {
		r.Group = "Packaging"
	}
	return r
}

func normalizeBackflushBasis(v string) string {
	for basis := range backflushBasisRank {
		if policyKey(basis) == policyKey(v) {
			return basis
		}
	}
	return BackflushBasisAll
}

func shortStockAction(v string) string {
	switch policyKey(v) {
	case "block":
		return ShortStockBlock
	case "warn":
		return ShortStockWarn
	}
	// The user's decision (2026-10-17): short packaging never stops a sale.
	return ShortStockAllow
}

func ruleDay(v string) string {
	v = strings.TrimSpace(v)
	if len(v) >= 10 {
		return v[:10]
	}
	return v
}

// backflushBasisAliases is every lower-case value an item's attribute must
// equal to match the rule: the value itself, plus - for Item and Category -
// the record's code and name, so a rule naming "CAT/000003" matches an item
// whose category reads "Earrings" and the other way round.
func backflushBasisAliases(tenantID string, r backflushRule) map[string]bool {
	aliases := map[string]bool{}
	v := strings.ToLower(r.BasisValue)
	if v == "" {
		return aliases
	}
	aliases[v] = true
	switch r.Basis {
	case BackflushBasisItem:
		if item, err := ResolveItemBySKU(tenantID, r.BasisValue); err == nil && item != nil {
			aliases[strings.ToLower(pimString(item.Data["code"]))] = true
			aliases[strings.ToLower(item.ID)] = true
		}
	case BackflushBasisCategory:
		if data, _, err := fetchDocData(tenantID, "Category", r.BasisValue); err == nil && data != nil {
			aliases[strings.ToLower(pimString(data["name"]))] = true
		}
		if schema, err := db.GetTenantSchema(tenantID); err == nil {
			var code string
			if db.DB.QueryRow(fmt.Sprintf(`SELECT id FROM %s.documents WHERE doctype = 'Category' AND deleted_at IS NULL AND lower(data->>'name') = $1 LIMIT 1`, schema), v).Scan(&code) == nil {
				aliases[strings.ToLower(code)] = true
			}
		}
	}
	delete(aliases, "")
	return aliases
}

// matches reports whether a rule applies to an item.
func (r backflushRule) matches(sku string, item map[string]interface{}) bool {
	switch r.Basis {
	case BackflushBasisAll:
		return true
	case BackflushBasisItem:
		return r.basisAliases[strings.ToLower(sku)] || r.basisAliases[strings.ToLower(pimString(item["code"]))]
	case BackflushBasisCategory:
		return r.basisAliases[strings.ToLower(strings.TrimSpace(pimString(item["category"])))]
	case BackflushBasisItemType:
		return strings.EqualFold(ItemTypeName(item), r.BasisValue)
	case BackflushBasisAttribute:
		key, want, ok := strings.Cut(r.BasisValue, ":")
		if !ok {
			return false
		}
		for _, pair := range strings.Split(pimString(item["variant_option_values"]), ";") {
			k, val, ok := strings.Cut(pair, ":")
			if ok && strings.EqualFold(strings.TrimSpace(k), strings.TrimSpace(key)) && strings.EqualFold(strings.TrimSpace(val), strings.TrimSpace(want)) {
				return true
			}
		}
	}
	return false
}

// ResolveSaleConsumption works out what a sale consumes: for every line sold
// normally, the winning rule of each group, with the cashier's overrides
// applied and checked. Promotional, free and stored-value lines never trigger
// packaging - the rules describe how merchandise is packed.
func ResolveSaleConsumption(tenantID, location string, lines []ConsumptionLineInput, overrides []ConsumptionOverride, asOf time.Time) ([]ConsumptionLine, error) {
	if asOf.IsZero() {
		asOf = time.Now()
	}
	rules, err := loadBackflushRules(tenantID, location, asOf)
	if err != nil || len(rules) == 0 {
		return nil, err
	}
	// Choices per group: every component this store's rules of the group name.
	choices := map[string][]ConsumptionChoice{}
	seen := map[string]bool{}
	for _, r := range rules {
		key := strings.ToLower(r.Group) + "|" + strings.ToLower(r.Component)
		if seen[key] {
			continue
		}
		seen[key] = true
		g := strings.ToLower(r.Group)
		choices[g] = append(choices[g], ConsumptionChoice{Component: r.Component, Name: r.ComponentName})
	}
	for g := range choices {
		sort.Slice(choices[g], func(i, j int) bool { return choices[g][i].Name < choices[g][j].Name })
	}
	overrideFor := map[string]ConsumptionOverride{}
	for _, o := range overrides {
		overrideFor[strings.ToLower(strings.TrimSpace(o.Sku))+"|"+strings.ToLower(strings.TrimSpace(o.Group))] = o
	}

	var out []ConsumptionLine
	used := map[string]bool{}
	for _, line := range lines {
		if line.Qty <= 0 {
			continue
		}
		policy, item, ok := ItemPolicyForSKU(tenantID, line.Sku)
		if !ok || policy.Sellability != SellNormal {
			continue
		}
		best := map[string]backflushRule{}
		for _, r := range rules {
			if !r.matches(line.Sku, item) {
				continue
			}
			g := strings.ToLower(r.Group)
			cur, have := best[g]
			if !have || r.rank() < cur.rank() || (r.rank() == cur.rank() && (r.Priority > cur.Priority || (r.Priority == cur.Priority && r.ID < cur.ID))) {
				best[g] = r
			}
		}
		groups := make([]string, 0, len(best))
		for g := range best {
			groups = append(groups, g)
		}
		sort.Strings(groups)
		for _, g := range groups {
			r := best[g]
			cl := ConsumptionLine{
				ForSKU: line.Sku, Group: r.Group, RuleID: r.ID, RuleVersion: r.Version,
				DefaultComponent: r.Component, DefaultName: r.ComponentName,
				Component: r.Component, ComponentName: r.ComponentName,
				QtyPerUnit: r.QtyPerUnit, Qty: r.QtyPerUnit * line.Qty, ShortStock: r.ShortStock,
				OverrideAllowed: r.OverrideAllowed, ReasonRequired: r.ReasonRequired, NoneAllowed: r.NoneAllowed,
				Choices: choices[g],
			}
			key := strings.ToLower(line.Sku) + "|" + g
			if o, has := overrideFor[key]; has {
				used[key] = true
				if err := applyConsumptionOverride(&cl, o); err != nil {
					return nil, err
				}
			}
			out = append(out, cl)
		}
	}
	for key, o := range overrideFor {
		if !used[key] {
			return nil, &ValidationError{Code: "POSOFF-0415", SubFor: o.Sku, Message: fmt.Sprintf(
				"no packaging rule applies to %s in group %q, so there is nothing to change - remove that packaging choice", o.Sku, o.Group)}
		}
	}
	return out, nil
}

func applyConsumptionOverride(cl *ConsumptionLine, o ConsumptionOverride) error {
	want := strings.TrimSpace(o.Component)
	if strings.EqualFold(want, cl.DefaultComponent) {
		return nil
	}
	if !cl.OverrideAllowed {
		return &ValidationError{Code: "POSOFF-0415", SubFor: cl.ForSKU, Message: fmt.Sprintf(
			"%s for %s is fixed by its rule and cannot be changed at the till", cl.Group, cl.ForSKU)}
	}
	if want == "" {
		if !cl.NoneAllowed {
			return &ValidationError{Code: "POSOFF-0415", SubFor: cl.ForSKU, Message: fmt.Sprintf(
				"%s for %s cannot be left out - choose one of the offered options", cl.Group, cl.ForSKU)}
		}
		cl.Component, cl.ComponentName, cl.Qty = "", "", 0
	} else {
		found := false
		for _, c := range cl.Choices {
			if strings.EqualFold(c.Component, want) {
				cl.Component, cl.ComponentName, found = c.Component, c.Name, true
				break
			}
		}
		if !found {
			return &ValidationError{Code: "POSOFF-0415", SubFor: cl.ForSKU, Message: fmt.Sprintf(
				"%s is not one of the %s options set up for this store", want, cl.Group)}
		}
	}
	cl.Overridden = true
	cl.Reason = strings.TrimSpace(o.Reason)
	if cl.ReasonRequired && cl.Reason == "" {
		return &ValidationError{Code: "POSOFF-0414", SubFor: cl.ForSKU, Message: fmt.Sprintf(
			"say why %s for %s was changed from %s - the rule asks for a reason", strings.ToLower(cl.Group), cl.ForSKU, cl.DefaultName)}
	}
	return nil
}

// ConsumptionComponentTotals sums what a sale consumes per component.
func ConsumptionComponentTotals(lines []ConsumptionLine) map[string]int {
	totals := map[string]int{}
	for _, l := range lines {
		if l.Component != "" && l.Qty > 0 {
			totals[l.Component] += l.Qty
		}
	}
	return totals
}

// CheckConsumptionStock compares what a sale consumes with what the store
// holds. A Block rule's shortage refuses the sale (ITM-012); Warn and Allow
// are reported and the sale goes ahead.
func CheckConsumptionStock(tenantID, location string, lines []ConsumptionLine) ([]ConsumptionShortage, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	action := map[string]string{}
	names := map[string]string{}
	for _, l := range lines {
		if l.Component == "" {
			continue
		}
		names[l.Component] = l.ComponentName
		if a := action[l.Component]; a != ShortStockBlock && (l.ShortStock == ShortStockBlock || a == "" || (a == ShortStockAllow && l.ShortStock == ShortStockWarn)) {
			action[l.Component] = l.ShortStock
		}
	}
	totals := ConsumptionComponentTotals(lines)
	components := make([]string, 0, len(totals))
	for c := range totals {
		components = append(components, c)
	}
	sort.Strings(components)
	var out []ConsumptionShortage
	for _, c := range components {
		var available int
		if err := db.DB.QueryRow(fmt.Sprintf(`SELECT available FROM %s.inventory_availability WHERE sku = $1 AND location_code = $2`, schema), c, location).Scan(&available); err != nil && err != sql.ErrNoRows {
			return nil, err
		}
		if available < totals[c] {
			out = append(out, ConsumptionShortage{Component: c, Name: names[c], Needed: totals[c], Available: available, Action: action[c]})
		}
	}
	return out, nil
}

// BlockingShortage returns the ITM-012 refusal for the first Block shortage.
func BlockingShortage(shortages []ConsumptionShortage) error {
	for _, s := range shortages {
		if s.Action == ShortStockBlock {
			return &ValidationError{Code: "POSOFF-0416", SubFor: s.Component, Message: fmt.Sprintf(
				"this store has %d of %s and this sale uses %d - its rule does not allow a short sale; choose another option under Packaging, or receive stock first", s.Available, labelOr(s.Name, s.Component), s.Needed)}
		}
	}
	return nil
}

func labelOr(name, fallback string) string {
	if strings.TrimSpace(name) != "" {
		return name
	}
	return fallback
}

// postSaleConsumptionTx takes a sale's components out of store stock and books
// the cost of the valuated ones, inside the sale's transaction. Block rules
// were checked at checkout and are posted strictly (a shortage that appeared
// since then fails the sale, as the rule says); everything else may go short,
// and the shortfall is reported after commit.
func postSaleConsumptionTx(tx *sql.Tx, schema, tenantID, cartNumber, location string, lines []ConsumptionLine, offlineSynced bool) ([]NegativeStockEvent, []PostedStockLine, error) {
	strict, lenient := map[string]int{}, map[string]int{}
	for _, l := range lines {
		if l.Component == "" || l.Qty <= 0 {
			continue
		}
		if l.ShortStock == ShortStockBlock && !offlineSynced {
			strict[l.Component] += l.Qty
		} else {
			lenient[l.Component] += l.Qty
		}
	}
	toItems := func(m map[string]int) []interface{} {
		items := make([]interface{}, 0, len(m))
		for sku, qty := range m {
			items = append(items, map[string]interface{}{"sku": sku, "qty": -qty})
		}
		return items
	}
	_, strictLines, err := PostInventoryLedgerWithVoucherTx(tx, schema, location, toItems(strict), false)
	if err != nil {
		return nil, nil, fmt.Errorf("packaging consumption: %w", err)
	}
	negative, lenientLines, err := PostInventoryLedgerWithVoucherTx(tx, schema, location, toItems(lenient), true)
	if err != nil {
		return nil, nil, fmt.Errorf("packaging consumption: %w", err)
	}

	// Cost: valuated components move from Inventory to their profile's expense
	// account at moving-average cost; non-valuated ones were expensed when
	// received, so only quantity moves.
	byAccount := map[string]int64{}
	for sku, qty := range ConsumptionComponentTotals(lines) {
		policy, _, ok := ItemPolicyForSKU(tenantID, sku)
		if !ok || !policy.Valuated() {
			continue
		}
		unit, _ := ResolveQuoteUnitCostPaise(tenantID, sku)
		if unit > 0 {
			byAccount[policy.ExpenseAccount] += unit * int64(qty)
		}
	}
	accounts := make([]string, 0, len(byAccount))
	for a := range byAccount {
		accounts = append(accounts, a)
	}
	sort.Strings(accounts)
	for _, acct := range accounts {
		if err := PostDoubleEntryTx(tx, tenantID, schema, "POSCart", cartNumber,
			map[string]int64{acct: byAccount[acct]}, map[string]int64{"1200": byAccount[acct]},
			"", fmt.Sprintf("POSCart:%s:CONSUMPTION:%s", cartNumber, acct)); err != nil {
			return nil, nil, fmt.Errorf("packaging consumption GL: %v", err)
		}
	}
	return negative, append(strictLines, lenientLines...), nil
}
