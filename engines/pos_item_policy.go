package engines

import (
	"fmt"
	"sort"
	"strings"
)

// Stage 72.2 / 72.7: the till's item-policy checks, run from ResolvePOSQuote -
// the one place the cashier's screen, checkout and the price-override command
// all price a cart - so no path can bill around them.

// posSellabilityGate refuses an item that may not be billed as merchandise
// (blueprint 14.1, ITM-003): a Not sellable item (packaging, consumables, a
// fixed asset) or a stored-value item, which is issued only through Gift
// Card -> Issue. Each refused attempt is logged once per cart (UAT-28).
func posSellabilityGate(tenantID, cartNumber, location, sku string, item map[string]interface{}, policy ItemPolicy) error {
	var code, msg string
	name := posLineLabel(item, sku)
	switch policy.Sellability {
	case SellNot:
		// A fixed asset keeps its own, more specific refusal (ASSET-0273, Stage 57.8).
		if policy.Class == ClassFixedAsset || policy.Accounting == AccountingFixedAsset {
			if err := RejectNonSellableItem(tenantID, sku); err != nil {
				return err
			}
		}
		code = "POSOFF-0411"
		msg = fmt.Sprintf("%s is not sold at the till (%s, Sellability: Not sellable)", name, policyLabel(policy.Class))
		if policy.Consumption == ConsumeAutoBackflush {
			msg += " - it is taken out of stock automatically with the sale; use Packaging to change it"
		}
	case SellStoredValueOnly:
		code = "POSOFF-0413"
		msg = fmt.Sprintf("%s is a stored-value item - issue it with Gift Card -> Issue, not as a bill line", name)
	default:
		return nil
	}
	logPOSPolicyAttempt(tenantID, cartNumber, location, sku, ItemExceptionNotSellable, code, msg)
	return &ValidationError{Code: code, SubFor: sku, Message: msg}
}

// authorizePromoLines checks every promo-only line against what authorises it
// (blueprint 10.2, ITM-004): its parent item on the bill (a Free With Item
// type - resolveFreeItemLines has already checked the quantity) or a Gift
// With Purchase offer this cart qualifies for, up to the offer's quantity.
func authorizePromoLines(tenantID string, req QuoteRequest, result *QuoteResult) error {
	allowed := map[string]int{}
	offers := map[string][]string{}
	for _, g := range result.Gifts {
		key := strings.ToLower(g.GiftItem)
		allowed[key] += g.Qty
		offers[key] = append(offers[key], g.OfferID)
	}
	for i := range result.Lines {
		line := &result.Lines[i]
		policy, item, known := ItemPolicyForSKU(tenantID, line.Sku)
		if !known || policy.Sellability != SellPromoOnly {
			continue
		}
		if ItemRules(tenantID, item).NeedsParentItem {
			continue // authorised by its parent item, checked in resolveFreeItemLines
		}
		key := strings.ToLower(line.Sku)
		if code := strings.ToLower(pimString(item["code"])); code != "" && allowed[code] > 0 {
			key = code
		}
		if line.Qty > allowed[key] {
			msg := fmt.Sprintf("%s is a promotional gift and this bill has not earned it", posLineLabel(item, line.Sku))
			if allowed[key] > 0 {
				msg = fmt.Sprintf("this bill has earned %d of %s, not %d - remove the extra", allowed[key], posLineLabel(item, line.Sku), line.Qty)
			}
			logPOSPolicyAttempt(tenantID, req.CartNumber, req.Location, line.Sku, ItemExceptionPromoOnly, "POSOFF-0412", msg)
			return &ValidationError{Code: "POSOFF-0412", SubFor: line.Sku, Message: msg + " (a Gift With Purchase offer has to qualify it)"}
		}
		ids := append([]string(nil), offers[key]...)
		sort.Strings(ids)
		line.PriceSource = PriceSourceGWP
		line.PromotionRef = strings.Join(ids, ",")
	}
	return nil
}

func logPOSPolicyAttempt(tenantID, cartNumber, location, sku, processType, code, msg string) {
	RecordItemException(tenantID, ItemExceptionInput{
		ProcessType: processType, SourceDoctype: "POSCart", SourceID: cartNumber,
		SKU: sku, Location: location, Code: code, Message: msg, Qty: 1,
		Status:   ItemExceptionStatusLogged,
		DedupKey: "pos-attempt|" + cartNumber + "|" + strings.ToLower(sku) + "|" + code,
	})
}

// saleIssue is what one sold line takes out of stock and what it costs.
type saleIssue struct {
	stock        []interface{} // ledger lines, negative qty
	handled      bool          // false: an ordinary line, costed by the caller as before
	costPaise    int64         // whole line, not per unit
	costResolved bool
	giftAccount  string // set: the cost is a promotional expense, not COGS
}

// saleStockIssue applies the item's policy to a sold line (Stage 72.9-72.11):
//
//   - a Virtual Product Bundle issues its components, costed at theirs;
//   - a not-tracked item (service, non-stock) issues nothing and costs nothing;
//   - a non-valuated item issues its quantity at no cost - it was expensed when
//     received;
//   - a promo-only gift issues normally, and its cost is booked to the gift
//     profile's expense account (5170 by default) instead of COGS;
//   - everything else is the ordinary line the caller always costed.
func saleStockIssue(tenantID, sku string, qty int) saleIssue {
	if bundle, found, err := loadActiveProductBundle(tenantID, sku); err == nil && found && bundle.FulfillmentMode == "Virtual" && len(bundle.Components) > 0 {
		out := saleIssue{handled: true, costResolved: true}
		for _, c := range bundle.Components {
			childQty := c.Quantity * qty
			if childQty <= 0 {
				continue
			}
			policy, _, known := ItemPolicyForSKU(tenantID, c.SKU)
			if known && !policy.Tracked() {
				continue
			}
			out.stock = append(out.stock, map[string]interface{}{"sku": c.SKU, "qty": -childQty})
			if known && !policy.Valuated() {
				continue
			}
			unit, ok := ResolveQuoteUnitCostPaise(tenantID, c.SKU)
			if !ok {
				out.costResolved = false
			}
			out.costPaise += unit * int64(childQty)
		}
		return out
	}
	policy, _, known := ItemPolicyForSKU(tenantID, sku)
	line := []interface{}{map[string]interface{}{"sku": sku, "qty": -qty}}
	if !known {
		return saleIssue{stock: line}
	}
	switch {
	case !policy.Tracked():
		return saleIssue{handled: true, costResolved: true}
	case !policy.Valuated():
		return saleIssue{stock: line, handled: true, costResolved: true}
	case policy.Sellability == SellPromoOnly:
		unit, ok := ResolveQuoteUnitCostPaise(tenantID, sku)
		return saleIssue{stock: line, handled: true, costPaise: unit * int64(qty), costResolved: ok, giftAccount: policy.ExpenseAccount}
	}
	return saleIssue{stock: line}
}

func sortedPaiseKeys(m map[string]int64) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// posLineLabel names a line as "Name (SKU)" - the till finds the line a
// refusal is about by the SKU in brackets.
func posLineLabel(item map[string]interface{}, sku string) string {
	name := itemLabel(item, sku)
	if strings.EqualFold(name, sku) {
		return "(" + sku + ")"
	}
	return name + " (" + sku + ")"
}
