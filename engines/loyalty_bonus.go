package engines

import (
	"custom_erp/db"
	"sort"
	"strings"
)

// Stage 146.2.1 - Bonus Points offers. Each live rule adds one ledger row on
// top of the base points; a non-stackable rule that applies ends the list.

// planLoyaltyBonus returns the bonus rows a bill earns. Errors are swallowed
// into "no bonus": a bad offer must never stop the base points.
func planLoyaltyBonus(tenantID string, in LoyaltyEarnInput) []LoyaltyEarnRow {
	all, err := loadActiveOffers(tenantID)
	if err != nil {
		return nil
	}
	var rules []offerRule
	for _, r := range all {
		if r.offerType == offerTypeBonusPoints && r.bonusPoints > 0 && r.couponCode == "" {
			rules = append(rules, r)
		}
	}
	if len(rules) == 0 {
		return nil
	}
	rules = offersForCustomerTags(rules, tenantID, in.CustomerID)
	resolveOfferSKUSets(tenantID, rules)
	sort.SliceStable(rules, func(i, j int) bool {
		if rules[i].priority != rules[j].priority {
			return rules[i].priority < rules[j].priority
		}
		return rules[i].name < rules[j].name
	})
	lines := make([]OfferCartLine, 0, len(in.Lines))
	sum := 0.0
	for _, l := range in.Lines {
		lines = append(lines, OfferCartLine{Sku: l.Sku, Qty: l.Qty, SalePrice: l.Net / float64(maxInt(l.Qty, 1))})
		sum += l.Net
	}
	if offersUseCategoryScope(rules) {
		_ = resolveLineCategories(tenantID, lines)
	}
	scale := 1.0
	if sum > float64(in.Net) && sum > 0 {
		scale = float64(in.Net) / sum
	}
	today := in.BillDate
	if today == "" {
		today = BusinessToday()
	}
	tier := ""
	if in.CustomerID != "" {
		if schema, err := db.GetTenantSchema(tenantID); err == nil {
			tier = customerLoyaltyTier(schema, in.CustomerID)
		}
	}
	var rows []LoyaltyEarnRow
	for _, r := range rules {
		if !offerIsLive(r, today) || !storeListed(r.stores, in.Location) {
			continue
		}
		if r.customerTier != "" && !strings.EqualFold(r.customerTier, tier) {
			continue
		}
		if r.minBillAmount > 0 && float64(in.Net) < r.minBillAmount {
			continue
		}
		if r.minQty > 0 && offerQualifyingQty(r, lines) < r.minQty {
			continue
		}
		pts := bonusPointsFor(r, lines, in.Net, scale)
		if pts <= 0 {
			continue
		}
		rows = append(rows, LoyaltyEarnRow{Points: pts, Reason: "Bonus: " + r.name, OfferID: r.id, Bonus: true})
		if !r.stackable {
			break
		}
	}
	return rows
}

func bonusPointsFor(r offerRule, lines []OfferCartLine, net int, scale float64) int {
	whole := r.scope == offerScopeBill || (r.scopeValue == "" && r.skuSet == nil)
	switch r.bonusBasis {
	case "Unit":
		return offerQualifyingQty(r, lines) * r.bonusPoints
	case "Bill":
		if !whole && offerQualifyingQty(r, lines) == 0 {
			return 0
		}
		return r.bonusPoints
	default: // Spend
		step := r.bonusStep
		if step < 1 {
			step = 1
		}
		scoped := float64(net)
		if !whole {
			scoped = offerScopedAmount(r, lines, 0) * scale
		}
		return int(scoped/step) * r.bonusPoints
	}
}
