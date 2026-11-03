package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
)

// Stage 146.2 - what one bill earns. PlanLoyaltyEarn is the single reading of
// the earn rules (blocked member, scheme, tier multiplier, bonus offers) used
// by both the earn at checkout and the till's "you will earn" preview, so the
// two cannot disagree.

// LoyaltyEarnLine is one bill line as the earn rules read it: Net is what the
// line cost after its offer share, before any points were used.
type LoyaltyEarnLine struct {
	Sku string
	Qty int
	Net float64
}

// LoyaltyEarnInput is a bill to be earned on. BillDate is blank for today.
type LoyaltyEarnInput struct {
	CustomerID string
	Location   string
	BillDate   string
	Net        int // rupees paid in money (after points used)
	Lines      []LoyaltyEarnLine
}

// LoyaltyEarnRow is one ledger row to write: the base points, or one bonus rule.
type LoyaltyEarnRow struct {
	Points  int
	Reason  string // blank for the base points; "Bonus: <offer>" for a rule
	OfferID string
	Bonus   bool
}

// LoyaltyEarnPlan is the answer: the rows, their total, and why nothing was
// earned when the member is blocked.
type LoyaltyEarnPlan struct {
	Rows    []LoyaltyEarnRow
	Total   int
	Blocked string // non-blank: the member earns nothing, and why
	Scheme  string // the scheme the base rate came from, blank = tenant setting
}

// PlanLoyaltyEarn works out the rows a bill earns. It writes nothing.
func PlanLoyaltyEarn(tenantID string, in LoyaltyEarnInput) (*LoyaltyEarnPlan, error) {
	plan := &LoyaltyEarnPlan{}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	if why := loyaltyEarnBlockReason(tenantID, loyaltyCustomerFactsFor(schema, in.CustomerID)); why != "" {
		plan.Blocked = why
		return plan, nil
	}
	rupeesPerPoint := GetSettingInt(tenantID, "loyalty.rupees_per_point")
	day := in.BillDate
	if day == "" {
		day = BusinessToday()
	}
	if s := loyaltySchemeFor(schema, in.Location, day); s != nil {
		rupeesPerPoint, plan.Scheme = s.RupeesPerPoint, s.Code
	}
	if rupeesPerPoint <= 0 {
		rupeesPerPoint = 100 // defensive: the setting is min-1 validated, never 0
	}
	if base := int(float64(in.Net/rupeesPerPoint) * loyaltyEarnMultiplierForCustomer(tenantID, in.CustomerID)); base > 0 {
		plan.Rows = append(plan.Rows, LoyaltyEarnRow{Points: base})
	}
	plan.Rows = append(plan.Rows, planLoyaltyBonus(tenantID, in)...)
	for _, r := range plan.Rows {
		plan.Total += r.Points
	}
	return plan, nil
}

// loyaltyEarnInputForCart reads the store, bill date and lines off the
// POSCart the points are earned on (best effort: a missing cart earns on the
// money alone, with no store).
func loyaltyEarnInputForCart(schema, customerID string, net int, cartNumber string) LoyaltyEarnInput {
	in := LoyaltyEarnInput{CustomerID: customerID, Net: net}
	var raw []byte
	if db.DB.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents WHERE doctype = 'POSCart' AND id = $1`, schema), cartNumber).Scan(&raw) != nil {
		return in
	}
	var d map[string]interface{}
	if json.Unmarshal(raw, &d) != nil {
		return in
	}
	in.Location = strField(d, "location")
	items, _ := d["items"].([]interface{})
	for _, it := range items {
		m, _ := it.(map[string]interface{})
		qty := int(numFromInterface(m["qty"]))
		in.Lines = append(in.Lines, LoyaltyEarnLine{Sku: strField(m, "sku"), Qty: qty,
			Net: numFromInterface(m["sale_price"])*float64(qty) - numFromInterface(m["offer_share"])})
	}
	return in
}

// previewLoyaltyEarn fills the till's "you will earn N" on a quote, and zeroes
// the redeem side for a blocked member. It never fails the quote.
func previewLoyaltyEarn(tenantID, customerID, location string, result *QuoteResult, l *QuoteLoyalty) {
	in := LoyaltyEarnInput{CustomerID: customerID, Location: location, Net: int(result.Total - l.Value)}
	for _, ln := range result.Lines {
		in.Lines = append(in.Lines, LoyaltyEarnLine{Sku: ln.Sku, Qty: ln.Qty, Net: ln.NetTotal})
	}
	if schema, err := db.GetTenantSchema(tenantID); err == nil && loyaltyCustomerFactsFor(schema, customerID).Blocked {
		l.MaxPoints, l.Points, l.Value = 0, 0, 0
		in.Net = int(result.Total)
	}
	if p, err := PlanLoyaltyEarn(tenantID, in); err == nil {
		l.EarnPoints, l.EarnBlocked = p.Total, p.Blocked
	}
}
