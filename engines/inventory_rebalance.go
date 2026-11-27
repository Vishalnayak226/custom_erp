package engines

import (
	"custom_erp/db"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/lib/pq"
)

// Item 273.2 - move before you buy.
//
// A store short of an item is often next to a store holding more of it than
// that store sells. The "rebalance" source of the supply proposals finds,
// for each item short at the planned location (the same reorder calculation
// the Reorder source uses), the other location with the most to spare and
// proposes a transfer from it. Nothing moves until the planner approves the
// proposal into a Draft transfer and that transfer is dispatched.
//
// What a location can spare: its available stock less what it keeps - its
// own reorder point plus one more lead time of its own sales, so giving
// stock away never trips its own reorder - and less what other open (or
// approved, not yet dispatched) rebalance proposals already take from it.
// A warehouse that sells nothing keeps only its safety stock.

// RebalanceLine is one proposed move.
type RebalanceLine struct {
	Sku            string                 `json:"sku"`
	To             string                 `json:"to"`
	ToAvailable    float64                `json:"to_available"`
	ToReorderPoint float64                `json:"to_reorder_point"`
	ToVelocity     float64                `json:"to_velocity"`
	Short          float64                `json:"short"`
	From           string                 `json:"from"`
	FromAvailable  float64                `json:"from_available"`
	FromKeeps      float64                `json:"from_keeps"`
	Qty            float64                `json:"qty"`
	Parts          map[string]interface{} `json:"-"`
}

// RebalanceSuggestions lists, for the items short at location, the transfer
// from the location with the most to spare. Items no other location can
// spare are left out - they stay for the Reorder source to buy.
func RebalanceSuggestions(tenantID, location string, leadTimeDays, safetyStock int) ([]RebalanceLine, error) {
	sugg, err := GetReplenishmentSuggestionsConfigured(tenantID, location, leadTimeDays, safetyStock)
	if err != nil {
		return nil, err
	}
	short := map[string]ReplenishmentSuggestion{}
	var skus []string
	for _, s := range sugg {
		if s.SuggestedQty > 0 {
			short[s.SKU] = s
			skus = append(skus, s.SKU)
		}
	}
	out := []RebalanceLine{}
	if len(skus) == 0 {
		return out, nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	// Stock elsewhere, at locations that are not switched off.
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT ia.sku, ia.location_code, ia.available
		  FROM %[1]s.inventory_availability ia
		 WHERE ia.sku = ANY($1) AND ia.location_code <> $2 AND ia.available > 0
		   AND NOT EXISTS (SELECT 1 FROM %[1]s.documents l
		                    WHERE l.doctype = 'Location' AND l.deleted_at IS NULL AND l.status <> 'Active'
		                      AND (l.id = ia.location_code OR l.data->>'code' = ia.location_code))`, schema),
		pq.Array(skus), location)
	if err != nil {
		return nil, err
	}
	type stockAt struct {
		sku, loc  string
		available float64
	}
	var stock []stockAt
	for rows.Next() {
		var s stockAt
		if rows.Scan(&s.sku, &s.loc, &s.available) == nil {
			stock = append(stock, s)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(stock) == 0 {
		return out, nil
	}
	// What other rebalance proposals already take from each location: open
	// ones, and approved ones whose transfer has not left yet.
	claimed := map[string]float64{} // from|sku
	crows, err := db.DB.Query(fmt.Sprintf(`
		SELECT p.data->>'from_location', p.data->>'sku',
		       SUM(COALESCE(NULLIF(p.data->>'approved_qty', '')::numeric, NULLIF(p.data->>'suggested_qty', '')::numeric, 0))::float8
		  FROM %[1]s.documents p
		  LEFT JOIN %[1]s.documents t ON t.doctype = 'TransferOrder' AND t.id = p.data->>'target_id' AND t.deleted_at IS NULL
		 WHERE p.doctype = 'SupplyProposal' AND p.deleted_at IS NULL AND p.data->>'source' = 'Rebalance'
		   AND p.data->>'sku' = ANY($1) AND p.data->>'location' <> $2
		   AND (p.status = 'Proposed' OR (p.status = 'Converted' AND t.status IN ('Draft', 'Pending Approval', 'Approved')))
		 GROUP BY 1, 2`, schema), pq.Array(skus), location)
	if err != nil {
		return nil, err
	}
	for crows.Next() {
		var from, sku string
		var q float64
		if crows.Scan(&from, &sku, &q) == nil {
			claimed[from+"|"+sku] = q
		}
	}
	crows.Close()
	velocity, err := SalesVelocityAllLocations(tenantID, 30)
	if err != nil {
		return nil, err
	}
	best := map[string]RebalanceLine{}
	bestSpare := map[string]float64{}
	for _, s := range stock {
		lead, safety := leadTimeDays, safetyStock
		if l, sf, found := resolveReorderPointConfig(tenantID, s.sku, s.loc); found {
			lead, safety = l, sf
		}
		sells := math.Ceil(velocity[s.loc][s.sku] * float64(lead))
		keeps := 2*sells + float64(safety)
		spare := math.Floor(s.available - keeps - claimed[s.loc+"|"+s.sku])
		if spare < 1 {
			continue
		}
		if cur, seen := best[s.sku]; seen && (bestSpare[s.sku] > spare || (bestSpare[s.sku] == spare && cur.From < s.loc)) {
			continue
		}
		bestSpare[s.sku] = spare
		need := short[s.sku]
		best[s.sku] = RebalanceLine{Sku: s.sku, To: location, ToAvailable: float64(need.Available),
			ToReorderPoint: float64(need.ReorderPoint), ToVelocity: need.DailyVelocity, Short: float64(need.SuggestedQty),
			From: s.loc, FromAvailable: s.available, FromKeeps: keeps, Qty: math.Min(float64(need.SuggestedQty), spare),
			Parts: map[string]interface{}{"inward": need.Inward, "on_order": need.OnOrder, "in_transit": need.InTransit,
				"back_orders": need.BackOrders, "position": need.Position}}
	}
	for _, l := range best {
		out = append(out, l)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Sku < out[j].Sku })
	return out, nil
}

// proposalSources picks the location each transfer proposal comes from: the
// planner's choice for all of them, else each proposal's own suggestion (a
// rebalance proposal names one). One Draft transfer is made per source.
func proposalSources(tenantID string, ids []string, chosen string) (map[string]string, []string, error) {
	out := map[string]string{}
	seen := map[string]bool{}
	var order []string
	chosen = strings.TrimSpace(chosen)
	for _, id := range ids {
		from := chosen
		if from == "" {
			d, _, err := fetchDocData(tenantID, "SupplyProposal", id)
			if err != nil {
				return nil, nil, err
			}
			if from = strings.TrimSpace(str(d, "from_location")); from == "" {
				return nil, nil, proposalErr("choose the location the stock comes from: %s has no suggested location", str(d, "sku"))
			}
		}
		loc, err := ResolveLocationReference(tenantID, from)
		if err != nil {
			return nil, nil, proposalErr("from location: %v", err)
		}
		out[id] = loc
		if !seen[loc] {
			seen[loc] = true
			order = append(order, loc)
		}
	}
	sort.Strings(order)
	return out, order, nil
}
