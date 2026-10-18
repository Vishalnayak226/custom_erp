package engines

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
)

// Stage 73.1.1 - the per-line allocation of a POS sale.
//
// A bill carries three bill-level figures that no line stored before this
// stage: the offer discount, the loyalty points redeemed and the gift cards
// tendered. The sale posted revenue and cash at the lines' list prices, GST was
// backed out of those list prices, and a return refunded a line at its list
// price - so a bill with an offer over-stated cash, revenue and output tax, and
// returning one line of a discounted or points-funded bill paid the customer
// back more than they had paid, in cash.
//
// SaleLineAllocation is each line's share of all three, fixed when the sale
// posts and stored on the cart, so the sale, its tax, every partial return and
// the drawer agree by construction. Amounts are paise; every split uses the
// largest-remainder method, so the shares always add back to the bill exactly
// and the same inputs always produce the same split.

// Tender keys a SaleLineAllocation uses besides the cart's payment mode.
const (
	TenderLoyaltyPoints = "Loyalty Points"
	TenderGiftCard      = "Gift Card"
)

// SaleLineAllocation is one sold line's share of what the bill took off and
// how it was paid. NetPaise = GrossPaise - OfferPaise is the tax-inclusive
// value the customer was charged for the line; Tenders splits it by how it was
// paid (the payment mode, Gift Card, Loyalty Points) and always adds up to it.
type SaleLineAllocation struct {
	SKU        string           `json:"sku"`
	Qty        int              `json:"qty"`
	GrossPaise int64            `json:"gross_paise"`
	OfferPaise int64            `json:"offer_paise"`
	NetPaise   int64            `json:"net_paise"`
	Points     int              `json:"points,omitempty"`
	Tenders    map[string]int64 `json:"tenders"`
}

// allocatePaise splits total across weights pro rata, largest remainder first
// (ties to the earlier line), so the parts always sum to total. With no weight
// at all the whole amount lands on the first line rather than vanishing.
func allocatePaise(total int64, weights []int64) []int64 {
	out := make([]int64, len(weights))
	if total <= 0 || len(weights) == 0 {
		return out
	}
	var sum int64
	for _, w := range weights {
		if w > 0 {
			sum += w
		}
	}
	if sum <= 0 {
		out[0] = total
		return out
	}
	type rem struct {
		i int
		r int64
	}
	rems := make([]rem, 0, len(weights))
	var given int64
	for i, w := range weights {
		if w <= 0 {
			continue
		}
		out[i] = total * w / sum
		given += out[i]
		rems = append(rems, rem{i, total * w % sum})
	}
	sort.SliceStable(rems, func(a, b int) bool { return rems[a].r > rems[b].r })
	for k := 0; given < total; k++ {
		out[rems[k%len(rems)].i]++
		given++
	}
	return out
}

// buildSaleAllocation splits a sale's points, gift cards and money tenders
// across its lines. Points follow each line's net value, gift cards what
// points left unpaid, and each money tender (73.1.6: in the order settled,
// the payment mode's rest last) what is still unpaid - the order a bill is
// settled in. When the money tenders add up to what is left, as a settled sale's
// always do, every line's tenders add up to its net exactly.
func buildSaleAllocation(skus []string, qtys []int, gross, offer []int64, points int, pointValuePaise, giftPaise int64, money []tenderPart) []SaleLineAllocation {
	n := len(skus)
	net := make([]int64, n)
	for i := range skus {
		net[i] = gross[i] - offer[i]
	}
	pointShares := allocatePaise(int64(points), net)
	left := make([]int64, n)
	for i := range skus {
		left[i] = net[i] - pointShares[i]*pointValuePaise
	}
	out := make([]SaleLineAllocation, n)
	for i := range skus {
		out[i] = SaleLineAllocation{SKU: skus[i], Qty: qtys[i], GrossPaise: gross[i], OfferPaise: offer[i], NetPaise: net[i],
			Points: int(pointShares[i]), Tenders: map[string]int64{}}
		if v := pointShares[i] * pointValuePaise; v != 0 {
			out[i].Tenders[TenderLoyaltyPoints] = v
		}
	}
	settle := func(key string, total int64) {
		for i, share := range allocatePaise(total, left) {
			if share != 0 {
				out[i].Tenders[key] += share
				left[i] -= share
			}
		}
	}
	settle(TenderGiftCard, giftPaise)
	for _, m := range money {
		settle(m.Key, m.Paise)
	}
	return out
}

// cartOfferShares is each stored cart line's share of the bill's offer
// discount, in paise. A cart stored since 73.1.1 carries the quote's own
// per-line shares; an older one (or one waiting on approval across the
// upgrade) has only the bill total, which is split by line value.
func cartOfferShares(gross []int64, stored []float64, offerDiscount float64) []int64 {
	shares := make([]int64, len(gross))
	var storedSum int64
	for i, s := range stored {
		shares[i] = RupeesToPaise(s)
		storedSum += shares[i]
	}
	total := RupeesToPaise(offerDiscount)
	if storedSum == total {
		return shares
	}
	return allocatePaise(total, gross)
}

// POSCartItemsFromQuote is the stored-cart shape of a quote's resolved lines.
// handleCheckout overwrites the browser's items with these before the cart is
// stored (Stage 47.2), so everything downstream - finalization, the receipt,
// the approval path and a return - prices from the server's own figures.
// Stage 73.1.1 adds each line's offer share and net total.
func POSCartItemsFromQuote(quote *QuoteResult) []map[string]interface{} {
	items := make([]map[string]interface{}, 0, len(quote.Lines))
	for _, l := range quote.Lines {
		item := map[string]interface{}{
			"sku": l.Sku, "qty": l.Qty,
			"sale_price":      l.UnitPrice,
			"reference_price": l.ReferencePrice,
			"price_source":    l.PriceSource,
			"offer_share":     l.OfferShare,
			"net_total":       l.NetTotal,
		}
		if l.OverrideID != "" {
			item["override_id"] = l.OverrideID
		}
		if l.PriceListCode != "" {
			item["price_list_code"] = l.PriceListCode
		}
		if l.PromotionRef != "" {
			item["promotion_ref"] = l.PromotionRef
		}
		if l.BillMode != "" {
			item["bill_mode"] = l.BillMode
		}
		items = append(items, item)
	}
	return items
}

// skuSaleShare is one SKU's share of a sale, summed across the bill's lines,
// which is the grain a return claims at.
type skuSaleShare struct {
	Qty      int
	NetPaise int64
	Points   int
	Tenders  map[string]int64
}

// loadSaleAllocation returns a paid POSCart's allocation by SKU, and the
// customer it was sold to. A cart finalized since 73.1.1 carries it; an older
// one is rebuilt from what it does record - the offer total, the points its
// ledger burned, its gift card tenders and its payment mode - with the same
// split a new sale would get. ok is false when there is no paid cart.
func loadSaleAllocation(q interface {
	QueryRow(string, ...interface{}) *sql.Row
}, tenantID, schema, cartID string) (map[string]*skuSaleShare, string, bool, error) {
	var dataStr string
	err := q.QueryRow(fmt.Sprintf(
		`SELECT data FROM %s.documents WHERE doctype = 'POSCart' AND id = $1 AND status = 'Paid'`, schema), cartID).Scan(&dataStr)
	if err == sql.ErrNoRows {
		return nil, "", false, nil
	}
	if err != nil {
		return nil, "", false, err
	}
	var cart struct {
		posCart
		Allocation []SaleLineAllocation `json:"allocation"`
	}
	if err := json.Unmarshal([]byte(dataStr), &cart); err != nil {
		return nil, "", false, err
	}
	lines := cart.Allocation
	if len(lines) == 0 {
		skus := make([]string, len(cart.Items))
		qtys := make([]int, len(cart.Items))
		gross := make([]int64, len(cart.Items))
		stored := make([]float64, len(cart.Items))
		for i, it := range cart.Items {
			skus[i], qtys[i], stored[i] = it.Sku, it.Qty, it.OfferShare
			gross[i] = RupeesToPaise(it.SalePrice) * int64(it.Qty)
		}
		var burned int
		_ = q.QueryRow(fmt.Sprintf(`
			SELECT COALESCE(SUM(CASE WHEN transaction_type = 'Burn' THEN points ELSE -points END), 0)
			FROM %s.loyalty_point_ledger
			WHERE reference_doctype = 'POSCart' AND (reference_id = $1 OR reference_id = $2)`, schema),
			cartID, cartID+":REDEMPTION-REVERSAL").Scan(&burned)
		if burned < 0 {
			burned = 0
		}
		var gift, net int64
		for _, g := range cart.GiftCardTenders {
			gift += g.AmountPaise
		}
		offers := cartOfferShares(gross, stored, cart.OfferDiscount)
		for i := range gross {
			net += gross[i] - offers[i]
		}
		pointValue := RupeesToPaise(float64(redemptionValuePerPointFor(tenantID)))
		mode := cart.PaymentMode
		if mode == "" {
			mode = "Cash"
		}
		lines = buildSaleAllocation(skus, qtys, gross, offers, burned, pointValue, gift,
			[]tenderPart{{Key: mode, Paise: net - int64(burned)*pointValue - gift}})
	}
	bySku := map[string]*skuSaleShare{}
	for _, l := range lines {
		s := bySku[l.SKU]
		if s == nil {
			s = &skuSaleShare{Tenders: map[string]int64{}}
			bySku[l.SKU] = s
		}
		s.Qty += l.Qty
		s.NetPaise += l.NetPaise
		s.Points += l.Points
		for k, v := range l.Tenders {
			s.Tenders[k] += v
		}
	}
	return bySku, cart.CustomerID, true, nil
}

// unitShare is the part of total that units (from, from+qty] of sold carry,
// counted cumulatively so a line returned in pieces refunds exactly its whole
// value by the time its last unit comes back - never a paisa more or less.
func unitShare(total int64, from, qty, sold int) int64 {
	if sold <= 0 || qty <= 0 {
		return 0
	}
	cum := func(n int) int64 {
		if n >= sold {
			return total
		}
		return total * int64(n) / int64(sold)
	}
	return cum(from+qty) - cum(from)
}

// applySaleAllocationToRecords prices each return line from the sale's own
// allocation for the units it claims, given how many of that SKU earlier
// returns already claimed. Runs under the original sale's lock, which is what
// makes "already claimed" exact.
func applySaleAllocationToRecords(records []returnItemRecord, alloc map[string]*skuSaleShare, claimed map[string]int) {
	for i := range records {
		s := alloc[records[i].SKU]
		if s == nil || s.Qty <= 0 {
			continue
		}
		from, qty := claimed[records[i].SKU], records[i].Qty
		r := &records[i]
		r.Allocated = true
		r.Points = int(unitShare(int64(s.Points), from, qty, s.Qty))
		r.Tenders = map[string]int64{}
		r.NetPaise = 0
		for k, v := range s.Tenders {
			share := unitShare(v, from, qty, s.Qty)
			if k == TenderLoyaltyPoints && s.Points > 0 {
				// Whole points at the sale's point value, so the money value
				// of the points always matches the points given back.
				share = int64(r.Points) * (v / int64(s.Points))
			}
			if share != 0 {
				r.Tenders[k] = share
				// The net is the sum of the parts - each part is exact over a
				// line returned in pieces, so the net is too, and a refund's
				// journal always balances to the paisa.
				r.NetPaise += share
			}
		}
		r.OriginalUnitPrice = PaiseToRupees(r.NetPaise) / float64(qty)
		claimed[r.SKU] += qty
	}
}

// moneyPaise is the part of a return line paid back in money - everything
// except the share paid for with points, which goes back as points (73.1.3).
func (r returnItemRecord) moneyPaise() int64 {
	if !r.Allocated {
		return RupeesToPaise(r.OriginalUnitPrice * float64(r.Qty))
	}
	return r.NetPaise - r.Tenders[TenderLoyaltyPoints]
}
