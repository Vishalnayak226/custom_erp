package engines

import (
	"crypto/hmac"
	"crypto/sha256"
	"custom_erp/db"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Stage 47.2 - the server-authoritative POS quote (audit findings A-02/A-38).
//
// Before this file, a POS line's sale price and cost price came from exactly
// one place: the browser. public/app.js pushed every cart line as
// `{ salePrice: 0, costPrice: 0 }` and let the cashier type both, and
// handleCheckout decided whether the sale needed manager approval by reading a
// THIRD, unrelated client field - discount_pct. Nothing tied those three
// numbers together, so the same money could be rung up two ways: declared as a
// discount (gated on approval) or silently as a low price with discount_pct=0
// (not gated), delivering identical figures to the till, to inventory and to
// the GL. Cost was worse: whatever the client said went straight to COGS.
//
// ResolvePOSQuote is now the ONLY thing that decides what a POS line costs.
// Every price it returns comes from tenant master data - an approved
// PriceListVersion (Stage 37.6.4's effective-dated resolver, which had no
// production caller until this stage), the Item's own sale_price/mrp, or an
// explicitly approved POSPriceOverride - and it is re-resolved inside
// checkout, so a stale or tampered browser payload cannot influence the
// receipt, the GST return, the revenue posting or the approval decision.
//
// Deliberately NOT in this file: cost. See ResolveQuoteUnitCostPaise below -
// cost never travels on a quote, because a quote is what the cashier's screen
// renders, and 47.2.2's acceptance line is "Cashier never receives margin/cost
// fields."

// Price sources, in the precedence order ResolvePOSQuote applies them. The
// string is stored on the cart line and shown in evidence, so a later dispute
// can say exactly WHY a line was priced the way it was.
const (
	// PriceSourceOverride - an approved POSPriceOverride for this cart+SKU
	// (47.2.3). Beats every master price by construction: it is the only
	// mechanism by which a human may deviate from master data at all.
	PriceSourceOverride = "override"
	// PriceSourceContractList - the customer's own contract price list
	// (Customer.price_list_code).
	PriceSourceContractList = "contract_price_list"
	// PriceSourceDefaultList - the tenant/channel default price list
	// (setting pos.default_price_list).
	PriceSourceDefaultList = "default_price_list"
	// PriceSourceItemMaster - Item.sale_price.
	PriceSourceItemMaster = "item_sale_price"
	// PriceSourceItemMRP - Item.mrp, when no sale price is configured. Still
	// server-side master data, so still authoritative.
	PriceSourceItemMRP = "item_mrp"
	// PriceSourceCashierEntered - NOTHING on the server prices this SKU, and
	// pos.pricing_mode is 'assisted', so the operator's own typed figure is
	// used. This is the one source the server cannot verify, which is exactly
	// why QuoteResult.HasUnverifiedPrice is set for it and why checkout routes
	// such a cart through discount approval whenever the tenant has configured
	// one. In 'strict' mode this source cannot occur - the line is rejected.
	PriceSourceCashierEntered = "cashier_entered"
)

// Pricing modes (setting pos.pricing_mode).
const (
	// PricingModeAssisted accepts an operator-typed price for a SKU the tenant
	// has not priced anywhere, and records it as unverified. Default, because
	// every tenant that exists today prices entirely at the till - making
	// strict the default would stop every one of them selling.
	PricingModeAssisted = "assisted"
	// PricingModeStrict refuses to sell anything the server cannot price.
	PricingModeStrict = "strict"
)

// quoteValidity is how long a quote's version is honoured before checkout
// treats it as stale on age alone (47.2.4). Short: a quote is a screenful of
// prices a cashier is looking at right now, not a document.
const quoteValidity = 30 * time.Minute

// QuoteLineRequest is one line of what the client is ASKING for - a selection,
// never an assertion. Note what is absent: no sale price, no cost price, no
// discount, no tax. FallbackUnitPrice is the operator's typed figure and is
// consulted for one case only (assisted mode, no server price at all).
type QuoteLineRequest struct {
	Sku               string  `json:"sku"`
	Qty               int     `json:"qty"`
	FallbackUnitPrice float64 `json:"unit_price,omitempty"`
}

// QuoteRequest is the full set of selection inputs 47.2.1 names: item,
// location, customer/contract/price list, channel, quantity, date/time,
// currency, tax basis and eligible promotions.
type QuoteRequest struct {
	CartNumber  string             `json:"cart_number"`
	Location    string             `json:"location"`
	CustomerID  string             `json:"customer_id"`
	Channel     string             `json:"channel"`
	Currency    string             `json:"currency"`
	Interstate  bool               `json:"interstate"`
	CouponCodes []string           `json:"coupon_codes"`
	Lines       []QuoteLineRequest `json:"items"`
	// AsOf lets a backdated or offline-replayed sale price against the rules
	// that were in force when it happened. Zero means "now".
	AsOf time.Time `json:"-"`
}

// QuoteLine is one priced line. Every monetary field on it was computed here,
// on the server, from tenant master data.
type QuoteLine struct {
	Sku string `json:"sku"`
	Qty int    `json:"qty"`
	// UnitPrice is what this line is actually charged at, GST inclusive
	// (this codebase's sale-side rate convention - see ComputeGSTForLines).
	UnitPrice float64 `json:"unit_price"`
	// ReferencePrice is what master data says the line is worth WITHOUT any
	// override - the denominator of the discount the approval gate keys on.
	ReferencePrice float64 `json:"reference_price"`
	MRP            float64 `json:"mrp,omitempty"`
	LineTotal      float64 `json:"line_total"`
	PriceSource    string  `json:"price_source"`
	PriceListCode  string  `json:"price_list_code,omitempty"`
	OverrideID     string  `json:"override_id,omitempty"`
	// DiscountPct is this line's own deviation below ReferencePrice, 0 when
	// the line is charged at (or above) master price.
	DiscountPct float64 `json:"discount_pct"`
}

// QuoteResult is the whole authoritative answer: what the receipt says, what
// GST is filed on, what revenue is booked at, and what the approval gate
// decides from. One computation, four consumers - the same reason
// FinalizePOSCheckout reads the stored cart rather than taking totals as
// parameters.
type QuoteResult struct {
	QuoteID string `json:"quote_id"`
	// Version is an HMAC digest of this quote's full priced content, keyed
	// with the server's own signing secret. It is both the rule/price version
	// 47.2.1 asks for and the tamper-evident identity: a client cannot mint a
	// version for prices the server did not produce, and any change to any
	// input (a superseded price list, an edited item price, a new offer, a
	// revoked override) produces a different one, which is what makes
	// checkout's staleness check in 47.2.4 exact rather than heuristic.
	Version   string    `json:"quote_version"`
	IssuedAt  time.Time `json:"issued_at"`
	ExpiresAt time.Time `json:"expires_at"`

	Location   string `json:"location"`
	CustomerID string `json:"customer_id,omitempty"`
	Channel    string `json:"channel,omitempty"`
	Currency   string `json:"currency"`
	Interstate bool   `json:"interstate"`

	Lines []QuoteLine `json:"lines"`

	// Subtotal is the sum of line totals at charged prices; ReferenceSubtotal
	// the same at master prices. Their difference is the manual money.
	Subtotal          float64 `json:"subtotal"`
	ReferenceSubtotal float64 `json:"reference_subtotal"`
	// ManualDiscountPct is (ReferenceSubtotal - Subtotal) / ReferenceSubtotal,
	// as a percentage - the server's own answer to "how far below master price
	// is this cart being sold?", which is what the discount-approval gate now
	// keys on instead of the client's discount_pct field. THIS IS THE A-02
	// FIX: it is derived from the money actually charged, so declaring a
	// discount honestly and hiding it in the unit price produce the identical
	// number and therefore the identical approval requirement.
	ManualDiscountPct float64 `json:"manual_discount_pct"`

	AppliedOffers  []AppliedOffer `json:"applied_offers"`
	OfferDiscount  float64        `json:"offer_discount"`
	UnmatchedCodes []string       `json:"unmatched_coupon_codes,omitempty"`

	GSTBreakdown GSTBreakdown `json:"gst_breakdown"`

	// Total is what the customer owes before loyalty redemption, which is
	// applied at finalization because it burns a real balance.
	Total float64 `json:"total"`

	// HasUnverifiedPrice is true when any line fell through to
	// PriceSourceCashierEntered. Checkout treats such a cart as needing
	// approval whenever the tenant has configured ANY POSCart approval rule:
	// the server cannot check a price it has no reference for, so a human
	// must. A tenant with no rule configured is unaffected, which is exactly
	// today's behaviour for them.
	HasUnverifiedPrice bool     `json:"has_unverified_price"`
	UnpricedSKUs       []string `json:"unpriced_skus,omitempty"`
}

// ResolvePOSQuote prices a cart from tenant master data alone.
//
// It is called from three places and must return the same answer for the same
// inputs in all three: the /pos/quote endpoint the cashier's screen renders
// from, checkout's own re-resolution (so a stale browser cannot influence the
// sale), and the price-override command (so an override is judged against the
// same reference price the sale will be).
func ResolvePOSQuote(tenantID string, req QuoteRequest) (*QuoteResult, error) {
	if len(req.Lines) == 0 {
		return nil, &ValidationError{Code: "GLOBAL-0002", SubFor: "items", Message: "a quote needs at least one line"}
	}
	asOf := req.AsOf
	if asOf.IsZero() {
		asOf = time.Now()
	}
	asOfDate := asOf.Format("2006-01-02")

	currency := strings.TrimSpace(req.Currency)
	if currency == "" {
		currency = GetSettingString(tenantID, SettingKeyFunctionalCurrency)
		if currency == "" {
			currency = "INR"
		}
	}

	mode := GetSettingString(tenantID, "pos.pricing_mode")
	if mode != PricingModeStrict {
		mode = PricingModeAssisted
	}

	contractList := customerPriceListCode(tenantID, req.CustomerID)
	defaultList := strings.TrimSpace(GetSettingString(tenantID, "pos.default_price_list"))
	overrides := approvedPriceOverrides(tenantID, req.CartNumber)

	result := &QuoteResult{
		QuoteID:    NewDocIDCompact("POSQ"),
		IssuedAt:   asOf.UTC(),
		ExpiresAt:  asOf.UTC().Add(quoteValidity),
		Location:   req.Location,
		CustomerID: req.CustomerID,
		Channel:    req.Channel,
		Currency:   currency,
		Interstate: req.Interstate,
		Lines:      make([]QuoteLine, 0, len(req.Lines)),
	}

	for _, in := range req.Lines {
		sku := strings.TrimSpace(in.Sku)
		if sku == "" || in.Qty <= 0 {
			return nil, &ValidationError{Code: "GLOBAL-0002", SubFor: "items",
				Message: fmt.Sprintf("every quote line needs a SKU and a positive quantity (sku=%q, qty=%d)", in.Sku, in.Qty)}
		}

		itemPrice, itemMRP := itemMasterPrices(tenantID, sku)

		// Reference price: what master data says this line is worth, ignoring
		// any override. Precedence is contract list, then default list, then
		// the item's own sale price, then its MRP.
		reference, refSource, refList := 0.0, "", ""
		if contractList != "" {
			if p, found, _ := ResolvePriceForSKU(tenantID, contractList, sku, asOfDate); found && p > 0 {
				reference, refSource, refList = p, PriceSourceContractList, contractList
			}
		}
		if reference == 0 && defaultList != "" {
			if p, found, _ := ResolvePriceForSKU(tenantID, defaultList, sku, asOfDate); found && p > 0 {
				reference, refSource, refList = p, PriceSourceDefaultList, defaultList
			}
		}
		if reference == 0 && itemPrice > 0 {
			reference, refSource = itemPrice, PriceSourceItemMaster
		}
		if reference == 0 && itemMRP > 0 {
			reference, refSource = itemMRP, PriceSourceItemMRP
		}

		line := QuoteLine{Sku: sku, Qty: in.Qty, MRP: itemMRP, PriceListCode: refList}

		switch {
		case reference > 0:
			line.ReferencePrice = round2(reference)
			line.UnitPrice = line.ReferencePrice
			line.PriceSource = refSource
		case mode == PricingModeStrict:
			// 47.2.2: strict tenants do not sell what they have not priced.
			return nil, &ValidationError{Code: "MASTER-0047", SubFor: sku,
				Message: fmt.Sprintf("no server-side price is configured for %q - set the item's Sale Price, or add it to an approved price list, before it can be sold (pos.pricing_mode is 'strict')", sku)}
		default:
			// Assisted: the operator's figure, recorded for what it is.
			if in.FallbackUnitPrice <= 0 {
				return nil, &ValidationError{Code: "MASTER-0047", SubFor: sku,
					Message: fmt.Sprintf("no server-side price is configured for %q and no price was entered for it", sku)}
			}
			line.UnitPrice = round2(in.FallbackUnitPrice)
			line.ReferencePrice = line.UnitPrice
			line.PriceSource = PriceSourceCashierEntered
			result.HasUnverifiedPrice = true
			result.UnpricedSKUs = append(result.UnpricedSKUs, sku)
		}

		// 47.2.3: an approved override replaces the charged price but never
		// the reference - so the deviation stays visible in the totals, in the
		// receipt and in the evidence, rather than being absorbed silently.
		if ov, ok := overrides[sku]; ok {
			line.UnitPrice = round2(ov.price)
			line.OverrideID = ov.id
			line.PriceSource = PriceSourceOverride
		}

		if line.ReferencePrice > 0 && line.UnitPrice < line.ReferencePrice {
			line.DiscountPct = round2((line.ReferencePrice - line.UnitPrice) / line.ReferencePrice * 100)
		}
		line.LineTotal = round2(line.UnitPrice * float64(in.Qty))

		result.Subtotal += line.LineTotal
		result.ReferenceSubtotal += round2(line.ReferencePrice * float64(in.Qty))
		result.Lines = append(result.Lines, line)
	}

	result.Subtotal = round2(result.Subtotal)
	result.ReferenceSubtotal = round2(result.ReferenceSubtotal)
	if result.ReferenceSubtotal > 0 && result.Subtotal < result.ReferenceSubtotal {
		result.ManualDiscountPct = round2((result.ReferenceSubtotal - result.Subtotal) / result.ReferenceSubtotal * 100)
	}

	// GST and offers are both computed from the RESOLVED prices, never the
	// requested ones - this is what makes the receipt, the GST return and the
	// revenue posting agree with each other by construction.
	gstLines := make([]GSTLineInput, len(result.Lines))
	offerLines := make([]OfferCartLine, len(result.Lines))
	for i, l := range result.Lines {
		gstLines[i] = GSTLineInput{Sku: l.Sku, Qty: l.Qty, UnitRate: l.UnitPrice}
		offerLines[i] = OfferCartLine{Sku: l.Sku, Qty: l.Qty, SalePrice: l.UnitPrice}
	}
	breakdown, err := ComputeGSTForLines(tenantID, gstLines, req.Interstate)
	if err != nil {
		return nil, err
	}
	result.GSTBreakdown = breakdown

	offerEval, err := EvaluatePOSOffers(tenantID, OfferEvaluationInput{
		Lines:       offerLines,
		CustomerID:  req.CustomerID,
		CouponCodes: req.CouponCodes,
	})
	if err != nil {
		return nil, err
	}
	result.AppliedOffers = offerEval.Applied
	result.OfferDiscount = offerEval.TotalDiscount
	result.UnmatchedCodes = offerEval.UnmatchedCodes

	result.Total = round2(result.Subtotal - result.OfferDiscount)
	if result.Total < 0 {
		result.Total = 0
	}

	result.Version = signQuoteVersion(tenantID, result)
	return result, nil
}

// signQuoteVersion produces the tamper-evident version digest. It covers every
// figure that could change what the customer pays, plus the tenant, so a
// version minted for one tenant's cart can never validate against another's.
// Deliberately NOT covered: QuoteID, IssuedAt and ExpiresAt - two quotes for
// the identical cart under identical rules must share a version, which is the
// whole point of using it as the staleness check.
func signQuoteVersion(tenantID string, q *QuoteResult) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "pos-quote-v1|%s|%s|%s|%s|%s|%t", tenantID, q.Location, q.CustomerID, q.Channel, q.Currency, q.Interstate)
	for _, l := range q.Lines {
		fmt.Fprintf(&sb, "|%s:%d:%.2f:%.2f:%s:%s", l.Sku, l.Qty, l.UnitPrice, l.ReferencePrice, l.PriceSource, l.OverrideID)
	}
	fmt.Fprintf(&sb, "|%.2f|%.2f|%.2f|%.2f|%.2f", q.Subtotal, q.ReferenceSubtotal, q.OfferDiscount, q.GSTBreakdown.TotalTax, q.Total)
	// Offers can change what is applied without changing the totals (a
	// like-for-like swap), which still means the cashier is looking at a
	// different bill than the one that will print.
	codes := make([]string, 0, len(q.AppliedOffers))
	for _, o := range q.AppliedOffers {
		codes = append(codes, fmt.Sprintf("%s:%.2f", o.OfferID, o.Discount))
	}
	sort.Strings(codes)
	sb.WriteString("|" + strings.Join(codes, ","))

	mac := hmac.New(sha256.New, jwtSigningKey.secret)
	mac.Write([]byte(sb.String()))
	return hex.EncodeToString(mac.Sum(nil))[:32]
}

// QuoteIsStale reports whether a version the client is holding still describes
// the quote the server just re-resolved (47.2.4). An empty submitted version
// means the client never asked for a quote at all, which is not staleness -
// checkout simply prices it fresh.
func QuoteIsStale(submittedVersion string, fresh *QuoteResult) bool {
	if strings.TrimSpace(submittedVersion) == "" {
		return false
	}
	return submittedVersion != fresh.Version
}

// --- master-data lookups ---------------------------------------------------

// itemMasterPrices reads Item.sale_price and Item.mrp. A missing item, or one
// with neither field set, returns zeroes - the caller decides what that means
// (rejection in strict mode, an operator-entered price in assisted).
func itemMasterPrices(tenantID, sku string) (salePrice, mrp float64) {
	data, _, err := fetchDocData(tenantID, "Item", sku)
	if err != nil {
		return 0, 0
	}
	salePrice, _ = parityNumber(data["sale_price"])
	mrp, _ = parityNumber(data["mrp"])
	return salePrice, mrp
}

// customerPriceListCode resolves the customer's contract price list. A walk-in
// sale (no customer) or a customer without one returns "".
func customerPriceListCode(tenantID, customerID string) string {
	if strings.TrimSpace(customerID) == "" {
		return ""
	}
	data, _, err := fetchDocData(tenantID, "Customer", customerID)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(pimString(data["price_list_code"]))
}

type resolvedOverride struct {
	id    string
	price float64
}

// approvedPriceOverrides loads every Approved POSPriceOverride for a cart, by
// SKU. Only 'Approved' counts: one still Pending Approval, or Rejected, or
// already Consumed by a completed sale, must not price anything.
func approvedPriceOverrides(tenantID, cartNumber string) map[string]resolvedOverride {
	out := map[string]resolvedOverride{}
	if strings.TrimSpace(cartNumber) == "" {
		return out
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return out
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, data->>'sku', data->>'override_price' FROM %s.documents
		WHERE doctype = 'POSPriceOverride' AND deleted_at IS NULL AND status = 'Approved'
		  AND data->>'cart_number' = $1
		ORDER BY created_at ASC`, schema), cartNumber)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id, sku, priceStr string
		if err := rows.Scan(&id, &sku, &priceStr); err != nil {
			continue
		}
		var price float64
		if _, err := fmt.Sscanf(priceStr, "%f", &price); err != nil || price < 0 {
			continue
		}
		// Last approved override for a SKU wins (ORDER BY created_at), so a
		// corrected second override supersedes the first without needing a
		// delete on an evidence document.
		out[sku] = resolvedOverride{id: id, price: price}
	}
	return out
}

// MarkOverridesConsumed retires every Approved override for a cart once the
// sale it authorised has actually completed. Without this, a cart number
// replayed (or a Failed cart re-submitted) would silently inherit an
// authorisation that was granted for a different sale - the override document
// stays as evidence either way, it simply stops pricing anything.
func MarkOverridesConsumed(tenantID, cartNumber string) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil || strings.TrimSpace(cartNumber) == "" {
		return
	}
	if _, err := db.DB.Exec(fmt.Sprintf(`
		UPDATE %s.documents
		SET data = jsonb_set(data, '{status}', to_jsonb('Consumed'::text)),
		    status = 'Consumed', updated_at = CURRENT_TIMESTAMP
		WHERE doctype = 'POSPriceOverride' AND status = 'Approved' AND data->>'cart_number' = $1`, schema),
		cartNumber); err != nil {
		LogSystemError(tenantID, "", "ERROR", "MarkOverridesConsumed",
			fmt.Sprintf("cart %s: could not retire its price overrides: %v", cartNumber, err), "")
	}
}

// --- 47.2.3: the price-override command ------------------------------------

// PriceOverrideRequest is what POST /api/v1/pos/price-override accepts. Note
// what it does NOT accept: the reference price. That is resolved here, from
// the same quote engine the sale will use, so an approver cannot be shown - or
// judged against - a reference the requester invented.
type PriceOverrideRequest struct {
	CartNumber    string  `json:"cart_number"`
	Sku           string  `json:"sku"`
	Qty           int     `json:"qty"`
	Location      string  `json:"location"`
	CustomerID    string  `json:"customer_id"`
	OverridePrice float64 `json:"override_price"`
	Reason        string  `json:"reason"`
}

// PriceOverrideResult is the command's answer.
type PriceOverrideResult struct {
	OverrideID     string  `json:"override_id"`
	Status         string  `json:"status"`
	ReferencePrice float64 `json:"reference_price"`
	OverridePrice  float64 `json:"override_price"`
	DiscountPct    float64 `json:"discount_pct"`
	RequiredRole   string  `json:"required_role,omitempty"`
	QuoteVersion   string  `json:"quote_version"`
}

// RecordPriceOverride is the separate, capability-gated command 47.2.3 asks
// for. It replaces "type a lower number into the price box" with: resolve the
// server's own reference price, compute the real deviation from it, check that
// deviation against the tenant's configured threshold, and write immutable
// evidence carrying reason, actor, before/after amount and quote version.
//
// The threshold reuses the existing approval_rules slabs (doctype
// 'POSPriceOverride'), so a tenant configures it on the same Approval Rules
// screen as every other maker-checker limit. No rule configured means the
// capability itself is the whole gate - which is still strictly more than the
// nothing that guarded this before.
func RecordPriceOverride(tenantID, actorUserID, actorRole string, req PriceOverrideRequest) (*PriceOverrideResult, error) {
	sku := strings.TrimSpace(req.Sku)
	if strings.TrimSpace(req.CartNumber) == "" || sku == "" {
		return nil, &ValidationError{Code: "GLOBAL-0002", SubFor: "cart_number", Message: "cart_number and sku are required"}
	}
	if req.OverridePrice < 0 {
		return nil, &ValidationError{Code: "GLOBAL-0002", SubFor: "override_price", Message: "an override price cannot be negative"}
	}
	if strings.TrimSpace(req.Reason) == "" {
		// A price deviation with no stated reason is not evidence, it is a
		// hole with a signature on it.
		return nil, &ValidationError{Code: "GLOBAL-0002", SubFor: "reason", Message: "a reason is required for a price override"}
	}
	if strings.TrimSpace(req.Location) == "" {
		// Location is what scopes this evidence to one store (scope_policy.go
		// treats POSPriceOverride as location-mandatory). An override written
		// without one would be either invisible or visible everywhere,
		// depending on the reader's scope - neither is acceptable for a record
		// of a price deviation.
		return nil, &ValidationError{Code: "GLOBAL-0002", SubFor: "location", Message: "a location is required for a price override"}
	}
	qty := req.Qty
	if qty <= 0 {
		qty = 1
	}

	// Resolve the reference WITHOUT this cart's existing overrides applying to
	// the line being overridden - RecordPriceOverride passes an empty cart
	// number precisely so a second override is judged against master price,
	// not against the first override's already-reduced figure.
	quote, err := ResolvePOSQuote(tenantID, QuoteRequest{
		Location:   req.Location,
		CustomerID: req.CustomerID,
		Lines:      []QuoteLineRequest{{Sku: sku, Qty: qty, FallbackUnitPrice: req.OverridePrice}},
	})
	if err != nil {
		return nil, err
	}
	line := quote.Lines[0]
	reference := line.ReferencePrice

	discountPct := 0.0
	if reference > 0 && req.OverridePrice < reference {
		discountPct = round2((reference - req.OverridePrice) / reference * 100)
	}

	// 47.2.3's "allowed threshold": above it, the override is not the
	// approver's to grant and lands Pending Approval for a higher role.
	requiredRole, err := RequiredApproverRoleForAmount(tenantID, "POSPriceOverride", discountPct)
	if err != nil {
		return nil, err
	}
	status := "Approved"
	approvedBy := actorUserID
	if requiredRole != "" && !roleSatisfies(actorRole, requiredRole) {
		status = "Pending Approval"
		approvedBy = ""
	}

	overrideID := NewDocID("POSOVR")
	payload := map[string]interface{}{
		"cart_number":     req.CartNumber,
		"sku":             sku,
		"location":        req.Location,
		"reference_price": reference,
		"override_price":  round2(req.OverridePrice),
		"discount_pct":    discountPct,
		"reason":          strings.TrimSpace(req.Reason),
		"requested_by":    actorUserID,
		"approved_by":     approvedBy,
		"quote_version":   quote.Version,
		"status":          status,
	}
	if status == "Pending Approval" {
		// extractAmount (engines/approval.go) reads discount_amount as the
		// slab value - percentage here, same convention handleCheckout's
		// POSCart gate already uses.
		payload["discount_amount"] = discountPct
	}
	marshaled, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	insertStatus := status
	if status == "Pending Approval" {
		// SubmitForApproval requires a Draft starting status, same as
		// handleCheckout's discount-gated cart claim.
		insertStatus = "Draft"
	}
	if _, err := db.DB.Exec(fmt.Sprintf(`
		INSERT INTO %s.documents (id, doctype, data, status, created_by)
		VALUES ($1, 'POSPriceOverride', $2, $3, $4)`, schema),
		overrideID, marshaled, insertStatus, actorUserID); err != nil {
		return nil, err
	}
	if status == "Pending Approval" {
		if err := SubmitForApproval(tenantID, "POSPriceOverride", overrideID, actorUserID, actorRole); err != nil {
			_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET status = 'Failed' WHERE doctype = 'POSPriceOverride' AND id = $1`, schema), overrideID)
			return nil, err
		}
	}

	LogAuditEvent(tenantID, actorUserID, "POS_PRICE_OVERRIDE", status, fmt.Sprintf(
		"cart %s sku %s: reference %.2f -> %.2f (%.2f%% off), reason %q, quote %s",
		req.CartNumber, sku, reference, req.OverridePrice, discountPct, req.Reason, quote.Version))

	return &PriceOverrideResult{
		OverrideID:     overrideID,
		Status:         status,
		ReferencePrice: reference,
		OverridePrice:  round2(req.OverridePrice),
		DiscountPct:    discountPct,
		RequiredRole:   requiredRole,
		QuoteVersion:   quote.Version,
	}, nil
}

// roleSatisfies reports whether the acting role can itself grant an override
// the rules say needs requiredRole. Super Admin always can; otherwise the role
// must be the named one exactly - deliberately not a hierarchy, because this
// codebase has no role ranking to consult and inventing one here would be a
// guess that silently widens authority.
func roleSatisfies(actorRole, requiredRole string) bool {
	if IsSuperAdmin(actorRole) {
		return true
	}
	return actorRole == requiredRole
}

// --- 47.2.2: server-side cost resolution -----------------------------------

// ResolveQuoteUnitCostPaise is the cost half of "the client asserts nothing".
// Precedence: a real recorded moving-average cost (engines/costing.go), then
// the item's own standard_cost master field, then zero.
//
// There is deliberately no client fallback. ResolveCOGSUnitCostPaise, which
// this replaces at the checkout call site, took the browser's cost_price as
// its last resort - which was, until Stage 37.3.3, the ONLY cost that ever
// reached COGS. Zero is the honest answer when the tenant has never recorded
// what an item cost, and recordCostingGap makes that visible instead of
// letting a client-supplied number paper over it.
func ResolveQuoteUnitCostPaise(tenantID, sku string) (costPaise int64, resolved bool) {
	if unitCostPaise, hasCost, err := GetItemUnitCost(tenantID, sku); err == nil && hasCost {
		return unitCostPaise, true
	}
	if data, _, err := fetchDocData(tenantID, "Item", sku); err == nil {
		if std, _ := parityNumber(data["standard_cost"]); std > 0 {
			return RupeesToPaise(std), true
		}
	}
	return 0, false
}

// RecordCostingGap writes one POSCostingGap per line whose cost could not be
// resolved from any server-side source, so a zero COGS is a visible, reviewable
// fact rather than a silent understatement of cost of sales. Best-effort and
// non-blocking, exactly like recordOfflineSyncVariance: the sale has already
// happened by the time this runs.
func RecordCostingGap(tenantID, cartNumber, sku string, qty int, saleValue float64) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return
	}
	payload, err := json.Marshal(map[string]interface{}{
		"cart_number": cartNumber,
		"sku":         sku,
		"qty":         qty,
		"sale_value":  round2(saleValue),
		"status":      "Open",
	})
	if err != nil {
		return
	}
	if _, err := db.DB.Exec(fmt.Sprintf(`
		INSERT INTO %s.documents (id, doctype, data, status, created_by)
		VALUES ($1, 'POSCostingGap', $2, 'Open', 'system')`, schema),
		NewDocID("POSCOSTGAP"), payload); err != nil {
		LogSystemError(tenantID, "", "ERROR", "RecordCostingGap",
			fmt.Sprintf("cart %s sku %s: could not record the costing gap: %v", cartNumber, sku, err), "")
	}
}
