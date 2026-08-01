package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Stage 30.7: the POS offer evaluator. Offers are configured in the ERP as
// Offer documents (db/migrations_stage28_2_pos_offers.sql, a flat-schema
// Master so create/list/edit come free from the generic doctype machinery)
// and are applied here, server-side, at checkout - so a change to an offer in
// the ERP takes effect on the very next POS sale with no POS-side deploy or
// cache to clear.
//
// Server-side evaluation is the whole point: the POS screen shows what the
// evaluator returns, but the discount that actually reaches the sale is
// recomputed here from the tenant's own Offer rows. A tampered client can't
// invent a discount, and an offline-replayed cart is re-evaluated against the
// rules as they stand when it lands.

// OfferScope / OfferType values, mirroring the doctype's Select options.
const (
	offerScopeBill     = "Bill"
	offerScopeItem     = "Item"
	offerScopeCategory = "Category"

	offerTypePercentage  = "Percentage Off"
	offerTypeFlat        = "Flat Off"
	offerTypeBuyXGetY    = "Buy X Get Y"
	offerTypeBundlePrice = "Bundle Price"
)

// OfferCartLine is one line of the cart being priced. category is resolved
// server-side from the line's Item (never trusted from the client) and only
// when a category-scoped offer actually exists - see resolveLineCategories.
type OfferCartLine struct {
	Sku       string  `json:"sku"`
	Qty       int     `json:"qty"`
	SalePrice float64 `json:"sale_price"`

	category string
}

// OfferEvaluationInput is everything the evaluator needs about a sale.
type OfferEvaluationInput struct {
	Lines       []OfferCartLine `json:"lines"`
	CustomerID  string          `json:"customer_id"`
	CouponCodes []string        `json:"coupon_codes"`
}

// AppliedOffer is one offer that matched, and what it took off the bill.
type AppliedOffer struct {
	OfferID     string  `json:"offer_id"`
	Name        string  `json:"name"`
	OfferType   string  `json:"offer_type"`
	Scope       string  `json:"scope"`
	ScopeValue  string  `json:"scope_value,omitempty"`
	CouponCode  string  `json:"coupon_code,omitempty"`
	Discount    float64 `json:"discount"`
	Description string  `json:"description"`
}

// OfferEvaluation is the evaluator's full answer.
type OfferEvaluation struct {
	Applied        []AppliedOffer `json:"applied"`
	TotalDiscount  float64        `json:"total_discount"`
	GrossAmount    float64        `json:"gross_amount"`
	NetAmount      float64        `json:"net_amount"`
	UnmatchedCodes []string       `json:"unmatched_codes,omitempty"`
}

// offerRule is one Offer document parsed into its evaluated form.
type offerRule struct {
	id                string
	name              string
	offerType         string
	scope             string
	scopeValue        string
	discountPct       float64
	discountAmount    float64
	buyQty            int
	getQty            int
	bundleQty         int
	bundlePrice       float64
	minBillAmount     float64
	minQty            int
	maxDiscountAmount float64
	couponCode        string
	customerTier      string
	validFrom         string
	validTo           string
	priority          float64
	stackable         bool
}

// EvaluatePOSOffers resolves every Active Offer against a cart and returns the
// ones that apply plus the total discount.
//
// Ordering and stacking: offers are sorted by priority (lower first, then by
// name so the result is deterministic for equal priorities). A non-stackable
// offer that applies ends evaluation - nothing after it is considered. This
// keeps "one big offer OR several small ones" predictable for the cashier
// rather than depending on row order.
func EvaluatePOSOffers(tenantID string, input OfferEvaluationInput) (*OfferEvaluation, error) {
	gross := 0.0
	totalQty := 0
	for _, l := range input.Lines {
		gross += l.SalePrice * float64(l.Qty)
		totalQty += l.Qty
	}
	gross = round2(gross)

	result := &OfferEvaluation{Applied: []AppliedOffer{}, GrossAmount: gross, NetAmount: gross}

	rules, err := loadActiveOffers(tenantID)
	if err != nil {
		return nil, err
	}
	if len(rules) == 0 {
		if len(input.CouponCodes) > 0 {
			result.UnmatchedCodes = normalizedCoupons(input.CouponCodes)
		}
		return result, nil
	}

	tier := ""
	if input.CustomerID != "" {
		if schema, schemaErr := db.GetTenantSchema(tenantID); schemaErr == nil {
			tier = customerLoyaltyTier(schema, input.CustomerID)
		}
	}

	// Only pay for the category lookup when a category-scoped offer exists.
	if offersUseCategoryScope(rules) {
		if err := resolveLineCategories(tenantID, input.Lines); err != nil {
			return nil, err
		}
	}

	suppliedCodes := map[string]bool{}
	for _, c := range normalizedCoupons(input.CouponCodes) {
		suppliedCodes[c] = true
	}
	usedCodes := map[string]bool{}

	sort.SliceStable(rules, func(i, j int) bool {
		if rules[i].priority != rules[j].priority {
			return rules[i].priority < rules[j].priority
		}
		return rules[i].name < rules[j].name
	})

	today := time.Now().Format("2006-01-02")
	remaining := gross

	for _, rule := range rules {
		if !offerIsLive(rule, today) {
			continue
		}
		if rule.customerTier != "" && !strings.EqualFold(rule.customerTier, tier) {
			continue
		}
		if rule.couponCode != "" && !suppliedCodes[strings.ToUpper(rule.couponCode)] {
			continue
		}
		if rule.minBillAmount > 0 && gross < rule.minBillAmount {
			continue
		}
		if rule.minQty > 0 && offerQualifyingQty(rule, input.Lines) < rule.minQty {
			continue
		}

		discount, description := offerDiscountFor(rule, input.Lines, gross)
		if discount <= 0 {
			continue
		}
		if rule.maxDiscountAmount > 0 && discount > rule.maxDiscountAmount {
			discount = rule.maxDiscountAmount
		}
		// Never let the stack drive the bill below zero.
		if discount > remaining {
			discount = remaining
		}
		if discount <= 0 {
			continue
		}
		discount = round2(discount)

		result.Applied = append(result.Applied, AppliedOffer{
			OfferID: rule.id, Name: rule.name, OfferType: rule.offerType,
			Scope: rule.scope, ScopeValue: rule.scopeValue, CouponCode: rule.couponCode,
			Discount: discount, Description: description,
		})
		remaining = round2(remaining - discount)
		if rule.couponCode != "" {
			usedCodes[strings.ToUpper(rule.couponCode)] = true
		}
		if !rule.stackable {
			break
		}
	}

	for _, c := range normalizedCoupons(input.CouponCodes) {
		if !usedCodes[c] {
			result.UnmatchedCodes = append(result.UnmatchedCodes, c)
		}
	}

	total := 0.0
	for _, a := range result.Applied {
		total += a.Discount
	}
	result.TotalDiscount = round2(total)
	result.NetAmount = round2(gross - result.TotalDiscount)
	return result, nil
}

// offerDiscountFor computes one rule's rupee discount against the cart, and a
// short human description of how it was arrived at (shown on the POS screen
// and stored on the cart for the receipt/audit trail).
func offerDiscountFor(rule offerRule, lines []OfferCartLine, gross float64) (float64, string) {
	switch rule.offerType {
	case offerTypePercentage:
		if rule.discountPct <= 0 {
			return 0, ""
		}
		base := offerScopedAmount(rule, lines, gross)
		return base * rule.discountPct / 100, fmt.Sprintf("%.4g%% off %s", rule.discountPct, offerScopeLabel(rule))

	case offerTypeFlat:
		if rule.discountAmount <= 0 {
			return 0, ""
		}
		// A flat offer can't take off more than the scope it applies to is worth.
		base := offerScopedAmount(rule, lines, gross)
		if base <= 0 {
			return 0, ""
		}
		d := rule.discountAmount
		if d > base {
			d = base
		}
		return d, fmt.Sprintf("flat %.2f off %s", rule.discountAmount, offerScopeLabel(rule))

	case offerTypeBuyXGetY:
		if rule.buyQty <= 0 || rule.getQty <= 0 {
			return 0, ""
		}
		return offerBuyXGetYDiscount(rule, lines)

	case offerTypeBundlePrice:
		if rule.bundleQty <= 0 || rule.bundlePrice <= 0 {
			return 0, ""
		}
		return offerBundleDiscount(rule, lines)
	}
	return 0, ""
}

// offerBuyXGetYDiscount gives the cheapest qualifying units away free, once
// per completed buy+get group. Cheapest-free is the industry-standard reading
// of "buy 2 get 1" and is also the customer-safe one (never over-discounts).
func offerBuyXGetYDiscount(rule offerRule, lines []OfferCartLine) (float64, string) {
	prices := offerQualifyingUnitPrices(rule, lines)
	group := rule.buyQty + rule.getQty
	if len(prices) < group {
		return 0, ""
	}
	sort.Float64s(prices) // cheapest first
	groups := len(prices) / group
	freeUnits := groups * rule.getQty
	discount := 0.0
	for i := 0; i < freeUnits && i < len(prices); i++ {
		discount += prices[i]
	}
	if discount <= 0 {
		return 0, ""
	}
	return discount, fmt.Sprintf("buy %d get %d free on %s (%d free unit(s))", rule.buyQty, rule.getQty, offerScopeLabel(rule), freeUnits)
}

// offerBundleDiscount prices each complete group of bundleQty qualifying units
// at bundlePrice, discounting the difference. Units beyond a complete bundle
// stay at their normal price.
func offerBundleDiscount(rule offerRule, lines []OfferCartLine) (float64, string) {
	prices := offerQualifyingUnitPrices(rule, lines)
	if len(prices) < rule.bundleQty {
		return 0, ""
	}
	// Bundle the most expensive units first - that's what a customer expects
	// from "any 3 for 999", and it's the only reading where adding an item
	// can't make the bill go up.
	sort.Sort(sort.Reverse(sort.Float64Slice(prices)))
	bundles := len(prices) / rule.bundleQty
	discount := 0.0
	for b := 0; b < bundles; b++ {
		normal := 0.0
		for i := b * rule.bundleQty; i < (b+1)*rule.bundleQty; i++ {
			normal += prices[i]
		}
		if normal > rule.bundlePrice {
			discount += normal - rule.bundlePrice
		}
	}
	if discount <= 0 {
		return 0, ""
	}
	return discount, fmt.Sprintf("%d for %.2f on %s (%d bundle(s))", rule.bundleQty, rule.bundlePrice, offerScopeLabel(rule), bundles)
}

// offerQualifyingUnitPrices expands the lines a rule applies to into one entry
// per unit, so qty-based offers can reason about individual units.
func offerQualifyingUnitPrices(rule offerRule, lines []OfferCartLine) []float64 {
	var out []float64
	for _, l := range lines {
		if !offerLineMatches(rule, l) {
			continue
		}
		for i := 0; i < l.Qty; i++ {
			out = append(out, l.SalePrice)
		}
	}
	return out
}

// offerScopedAmount is the rupee value of the part of the cart a rule applies
// to - the whole bill for a Bill-scoped offer, otherwise just its lines.
func offerScopedAmount(rule offerRule, lines []OfferCartLine, gross float64) float64 {
	if rule.scope == offerScopeBill || rule.scopeValue == "" {
		return gross
	}
	sum := 0.0
	for _, l := range lines {
		if offerLineMatches(rule, l) {
			sum += l.SalePrice * float64(l.Qty)
		}
	}
	return sum
}

func offerQualifyingQty(rule offerRule, lines []OfferCartLine) int {
	if rule.scope == offerScopeBill || rule.scopeValue == "" {
		total := 0
		for _, l := range lines {
			total += l.Qty
		}
		return total
	}
	total := 0
	for _, l := range lines {
		if offerLineMatches(rule, l) {
			total += l.Qty
		}
	}
	return total
}

// offerLineMatches decides whether one cart line falls inside a rule's scope.
// Category matching resolves the line's SKU to its Item category once per
// evaluation via the cache the caller primes (offerCategoryOf), so a
// category-scoped offer doesn't issue a query per line.
func offerLineMatches(rule offerRule, line OfferCartLine) bool {
	switch rule.scope {
	case offerScopeBill:
		return true
	case offerScopeItem:
		return strings.EqualFold(strings.TrimSpace(rule.scopeValue), strings.TrimSpace(line.Sku))
	case offerScopeCategory:
		return strings.EqualFold(strings.TrimSpace(rule.scopeValue), strings.TrimSpace(line.category))
	}
	return false
}

func offerScopeLabel(rule offerRule) string {
	switch rule.scope {
	case offerScopeItem:
		return "item " + rule.scopeValue
	case offerScopeCategory:
		return "category " + rule.scopeValue
	}
	return "the bill"
}

// offerIsLive checks an offer's validity window against today's date. Blank
// bounds mean open-ended in that direction.
func offerIsLive(rule offerRule, today string) bool {
	if rule.validFrom != "" && today < rule.validFrom {
		return false
	}
	if rule.validTo != "" && today > rule.validTo {
		return false
	}
	return true
}

func normalizedCoupons(codes []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, c := range codes {
		c = strings.ToUpper(strings.TrimSpace(c))
		if c == "" || seen[c] {
			continue
		}
		seen[c] = true
		out = append(out, c)
	}
	return out
}

func offersUseCategoryScope(rules []offerRule) bool {
	for _, r := range rules {
		if r.scope == offerScopeCategory {
			return true
		}
	}
	return false
}

// resolveLineCategories fills in each line's Item category in one query for
// the whole cart, rather than a lookup per line. Mutates lines in place, so
// callers must pass the slice the evaluator actually reads.
//
// A SKU with no Item row (or no category set) simply keeps an empty category
// and matches no category-scoped offer - an unknown item must never silently
// pick up someone else's discount.
func resolveLineCategories(tenantID string, lines []OfferCartLine) error {
	if len(lines) == 0 {
		return nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	skus := make([]string, 0, len(lines))
	seen := map[string]bool{}
	for _, l := range lines {
		if l.Sku != "" && !seen[l.Sku] {
			seen[l.Sku] = true
			skus = append(skus, l.Sku)
		}
	}
	if len(skus) == 0 {
		return nil
	}

	// Placeholder list rather than a pq.Array - lib/pq is only an indirect
	// dependency here (it's the driver), and the repo's lightweight-first rule
	// says don't promote it to a direct one for something a few lines of
	// plain SQL building already covers. Values stay parameterised.
	placeholders := make([]string, len(skus))
	args := make([]interface{}, len(skus))
	for i, sku := range skus {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = sku
	}
	rows, err := db.DB.Query(fmt.Sprintf(
		`SELECT COALESCE(data->>'code', ''), COALESCE(data->>'category', '')
		 FROM %s.documents WHERE doctype = 'Item' AND data->>'code' IN (%s)`,
		schema, strings.Join(placeholders, ",")), args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	byCode := map[string]string{}
	for rows.Next() {
		var code, category string
		if err := rows.Scan(&code, &category); err != nil {
			return err
		}
		byCode[code] = category
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range lines {
		lines[i].category = byCode[lines[i].Sku]
	}
	return nil
}

// loadActiveOffers reads every Active Offer document for the tenant. Read on
// each evaluation rather than cached in-process, for the same reason the
// settings registry reads per use: an offer edited in the ERP must apply to
// the very next sale, with no restart and nothing to invalidate.
func loadActiveOffers(tenantID string) ([]offerRule, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(
		`SELECT id, data FROM %s.documents WHERE doctype = 'Offer' AND status = 'Active'`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []offerRule
	for rows.Next() {
		var id, dataStr string
		if err := rows.Scan(&id, &dataStr); err != nil {
			return nil, err
		}
		var d map[string]interface{}
		if err := json.Unmarshal([]byte(dataStr), &d); err != nil {
			continue // a malformed offer row must never break checkout
		}
		out = append(out, offerRule{
			id:                id,
			name:              strField(d, "name"),
			offerType:         strField(d, "offer_type"),
			scope:             strField(d, "scope"),
			scopeValue:        strField(d, "scope_value"),
			discountPct:       numFromInterface(d["discount_pct"]),
			discountAmount:    numFromInterface(d["discount_amount"]),
			buyQty:            int(numFromInterface(d["buy_qty"])),
			getQty:            int(numFromInterface(d["get_qty"])),
			bundleQty:         int(numFromInterface(d["bundle_qty"])),
			bundlePrice:       numFromInterface(d["bundle_price"]),
			minBillAmount:     numFromInterface(d["min_bill_amount"]),
			minQty:            int(numFromInterface(d["min_qty"])),
			maxDiscountAmount: numFromInterface(d["max_discount_amount"]),
			couponCode:        strings.ToUpper(strings.TrimSpace(strField(d, "coupon_code"))),
			customerTier:      strings.TrimSpace(strField(d, "customer_tier")),
			validFrom:         strField(d, "valid_from"),
			validTo:           strField(d, "valid_to"),
			priority:          numFromInterface(d["priority"]),
			stackable:         !strings.EqualFold(strings.TrimSpace(strField(d, "stackable")), "No"),
		})
	}
	return out, rows.Err()
}
