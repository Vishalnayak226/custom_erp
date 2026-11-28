package engines

// Item 324.2 (ask 324): the sourcing engine behind stock requests. For each
// item a location asks for, it ranks every other active location that holds
// the item and plans which of them should send how much.
//
// A source is ranked on four things, each worth a share of 100 points:
//   - fill (40): how much of the asked quantity it can spare. Spare is the
//     rebalance rule of 273.2 - available stock less what the source keeps
//     for its own sales (twice its lead-time sales plus its safety stock) -
//     less what open requests and rebalance proposals already take from it;
//   - distance (25): km between PIN codes from the PIN master, else the same
//     city or state;
//   - how it sells there (20): an item that does not sell at the source, or
//     has two months of cover, is good to move;
//   - kind (15): a warehouse before a store (HO 5).
//
// The plan fills each line from the best source first and prefers a source
// already sending something else, so a request becomes as few shipments as
// it can. Every option says why it was ranked where it is.

import (
	"custom_erp/db"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/lib/pq"
)

const (
	// StockRequestReasonCategory is the Reason Code category for a client's
	// own request reasons (Masters > Reason Codes).
	StockRequestReasonCategory = "Stock Request"
	stockRequestDefaultLead    = 7
	stockRequestDefaultSafety  = 0
	stockRequestMaxOptions     = 8
	stockRequestSameSourcePts  = 15
)

// StockRequestReasons are offered before the client's own Stock Request codes.
// Any typed text is accepted too.
var StockRequestReasons = []string{"Replenishment", "Advance order", "Customer order", "Event / launch", "Display", "Stock-out"}

// isBuiltInStockRequestReason says whether text is one of the built-in
// reasons, ignoring case.
func isBuiltInStockRequestReason(text string) bool {
	for _, r := range StockRequestReasons {
		if strings.EqualFold(r, strings.TrimSpace(text)) {
			return true
		}
	}
	return false
}

// StockRequestLineIn is one line of a request: an item, how many, and
// optionally the location it must come from.
type StockRequestLineIn struct {
	SKU  string  `json:"sku"`
	Qty  float64 `json:"qty"`
	From string  `json:"from,omitempty"`
}

// SourceOption is one location that could send an item.
type SourceOption struct {
	Location     string   `json:"location"`
	Name         string   `json:"name"`
	Type         string   `json:"type"`
	Available    float64  `json:"available"`
	Keeps        float64  `json:"keeps"`
	Promised     float64  `json:"promised"`
	Spare        float64  `json:"spare"`
	DistanceKm   float64  `json:"distance_km"` // -1 = not known
	DistanceNote string   `json:"distance_note"`
	CoverDays    float64  `json:"cover_days"` // -1 = does not sell there
	Score        float64  `json:"score"`
	Why          []string `json:"why"`
}

// SourcePick is one shipment's share of a line.
type SourcePick struct {
	From     string  `json:"from"`
	FromName string  `json:"from_name"`
	Qty      float64 `json:"qty"`
}

// SourcedLine is one asked item with its ranked sources and its plan.
type SourcedLine struct {
	SKU       string         `json:"sku"`
	Name      string         `json:"name"`
	Qty       float64        `json:"qty"`
	Suggested float64        `json:"suggested"`
	HaveHere  float64        `json:"have_here"`
	Pinned    string         `json:"pinned,omitempty"`
	Options   []SourceOption `json:"options"`
	Plan      []SourcePick   `json:"plan"`
	Short     float64        `json:"short"`
}

// SourcingShipment is what one source would send for the whole request.
type SourcingShipment struct {
	From         string  `json:"from"`
	FromName     string  `json:"from_name"`
	Lines        int     `json:"lines"`
	Units        float64 `json:"units"`
	DistanceKm   float64 `json:"distance_km"`
	DistanceNote string  `json:"distance_note"`
}

// SourcingPlan is the engine's answer for one requesting location.
type SourcingPlan struct {
	Location     string             `json:"location"`
	LocationName string             `json:"location_name"`
	Lines        []SourcedLine      `json:"lines"`
	Shipments    []SourcingShipment `json:"shipments"`
}

type requestLocation struct {
	ID, Code, Name, Type, Pincode, City, State, Status string
	Auto                                               bool
}

func (l *requestLocation) active() bool { return l.Status == "" || l.Status == "Active" }

func (l *requestLocation) label() string {
	if l.Name != "" {
		return l.Name
	}
	if l.Code != "" {
		return l.Code
	}
	return l.ID
}

// requestLocations reads every location once, keyed by id, with an alias
// map from id or code to id (an id always wins over a code).
type requestLocations struct {
	byID  map[string]*requestLocation
	alias map[string]string
}

func (r requestLocations) canon(ref string) string {
	ref = strings.TrimSpace(ref)
	if id, ok := r.alias[ref]; ok {
		return id
	}
	return ref
}

func (r requestLocations) get(ref string) *requestLocation { return r.byID[r.canon(ref)] }

func loadRequestLocations(schema string) (requestLocations, error) {
	out := requestLocations{byID: map[string]*requestLocation{}, alias: map[string]string{}}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, COALESCE(data->>'code', ''), COALESCE(data->>'name', ''), COALESCE(data->>'type', ''),
		       COALESCE(data->>'pincode', ''), COALESCE(data->>'city', ''), COALESCE(data->>'state', ''),
		       COALESCE(status, ''), COALESCE(data->>'auto_request_stock', '')
		  FROM %s.documents WHERE doctype = 'Location' AND deleted_at IS NULL`, schema))
	if err != nil {
		return out, err
	}
	defer rows.Close()
	var list []*requestLocation
	for rows.Next() {
		l := &requestLocation{}
		var auto string
		if err := rows.Scan(&l.ID, &l.Code, &l.Name, &l.Type, &l.Pincode, &l.City, &l.State, &l.Status, &auto); err != nil {
			return out, err
		}
		l.Auto = auto == "Yes"
		list = append(list, l)
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	for _, l := range list {
		out.byID[l.ID] = l
		out.alias[l.ID] = l.ID
	}
	for _, l := range list {
		if l.Code != "" {
			if _, taken := out.alias[l.Code]; !taken {
				out.alias[l.Code] = l.ID
			}
		}
	}
	return out, nil
}

// mergeRequestLines joins repeated items, keeps the first order, rounds each
// quantity to whole units and drops blank items.
func mergeRequestLines(in []StockRequestLineIn) []StockRequestLineIn {
	var out []StockRequestLineIn
	at := map[string]int{}
	for _, l := range in {
		sku := strings.TrimSpace(l.SKU)
		if sku == "" {
			continue
		}
		key := sku + "|" + strings.TrimSpace(l.From)
		if i, ok := at[key]; ok {
			out[i].Qty += math.Max(0, math.Round(l.Qty))
			continue
		}
		at[key] = len(out)
		out = append(out, StockRequestLineIn{SKU: sku, Qty: math.Max(0, math.Round(l.Qty)), From: strings.TrimSpace(l.From)})
	}
	return out
}

// stockRequestPromised is what open transfers and open rebalance proposals
// already take from each source, keyed source|sku. Approved and Packed
// transfers count only when they do not hold their stock (with the hold on,
// available already excludes them).
func stockRequestPromised(tenantID, schema string, locs requestLocations, skus []string) (map[string]float64, error) {
	want := map[string]bool{}
	for _, s := range skus {
		want[s] = true
	}
	statuses := []string{"Draft", "Pending Approval"}
	if !GetSettingBool(tenantID, TransferReserveSetting) {
		statuses = append(statuses, "Approved", "Packed")
	}
	out := map[string]float64{}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT COALESCE(data->>'from_warehouse', ''), COALESCE(data->>'items', '')
		  FROM %s.documents
		 WHERE doctype = 'TransferOrder' AND deleted_at IS NULL AND status = ANY($1)`, schema), pq.Array(statuses))
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var from, items string
		if rows.Scan(&from, &items) != nil || items == "" {
			continue
		}
		lines, err := parseTransferItems(items)
		if err != nil {
			continue
		}
		from = locs.canon(from)
		for _, l := range lines {
			if want[l.Sku] {
				out[from+"|"+l.Sku] += float64(l.Qty)
			}
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	prows, err := db.DB.Query(fmt.Sprintf(`
		SELECT COALESCE(data->>'from_location', ''), COALESCE(data->>'sku', ''),
		       SUM(COALESCE(NULLIF(data->>'suggested_qty', '')::numeric, 0))::float8
		  FROM %s.documents
		 WHERE doctype = 'SupplyProposal' AND deleted_at IS NULL AND status = 'Proposed'
		   AND data->>'source' = 'Rebalance' AND data->>'sku' = ANY($1)
		 GROUP BY 1, 2`, schema), pq.Array(skus))
	if err != nil {
		return nil, err
	}
	defer prows.Close()
	for prows.Next() {
		var from, sku string
		var q float64
		if prows.Scan(&from, &sku, &q) == nil {
			out[locs.canon(from)+"|"+sku] += q
		}
	}
	return out, prows.Err()
}

// itemNames reads the display name of each item code.
func itemNames(schema string, skus []string) map[string]string {
	out := map[string]string{}
	if len(skus) == 0 {
		return out
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, COALESCE(data->>'code', ''), COALESCE(data->>'name', data->>'item_name', '')
		  FROM %s.documents
		 WHERE doctype = 'Item' AND deleted_at IS NULL AND (id = ANY($1) OR data->>'code' = ANY($1))`, schema), pq.Array(skus))
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id, code, name string
		if rows.Scan(&id, &code, &name) == nil && name != "" {
			out[id] = name
			if code != "" {
				out[code] = name
			}
		}
	}
	return out
}

// sourceScore ranks one option for a still-unfilled quantity.
func sourceScore(o *SourceOption, need float64) float64 {
	fill := 1.0
	if need > 0 {
		fill = math.Min(o.Spare, need) / need
	}
	s := 40 * fill
	switch {
	case o.DistanceKm >= 0:
		s += 25 / (1 + o.DistanceKm/100)
	case o.DistanceNote == "same city":
		s += 20
	case o.DistanceNote == "same state":
		s += 10
	default:
		s += 3
	}
	if o.CoverDays < 0 || o.CoverDays >= 60 {
		s += 20
	} else {
		s += 20 * o.CoverDays / 60
	}
	switch {
	case strings.EqualFold(o.Type, "Warehouse"):
		s += 15
	case strings.EqualFold(o.Type, "HO"):
		s += 5
	}
	return math.Round(s*10) / 10
}

func fmtUnits(q float64) string { return fmt.Sprintf("%g", q) }

// SuggestRequestSources ranks the sources for each line a location asks for
// and plans the shipments. A line with no quantity takes what the location's
// reorder rules say it needs; a line naming its source keeps that source.
func SuggestRequestSources(tenantID, requester string, in []StockRequestLineIn) (*SourcingPlan, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	locs, err := loadRequestLocations(schema)
	if err != nil {
		return nil, err
	}
	req := locs.get(requester)
	if req == nil {
		return nil, &ValidationError{Code: "STOCKT-0494", Message: fmt.Sprintf("location %q was not found", requester)}
	}
	plan := &SourcingPlan{Location: req.ID, LocationName: req.label(), Lines: []SourcedLine{}, Shipments: []SourcingShipment{}}
	lines := mergeRequestLines(in)
	if len(lines) == 0 {
		return plan, nil
	}
	skus := make([]string, 0, len(lines))
	seen := map[string]bool{}
	for _, l := range lines {
		if !seen[l.SKU] {
			seen[l.SKU] = true
			skus = append(skus, l.SKU)
		}
	}

	// What the requester's own reorder rules say it needs.
	suggested := map[string]float64{}
	if sugg, err := GetReplenishmentSuggestionsConfigured(tenantID, req.ID, stockRequestDefaultLead, stockRequestDefaultSafety); err == nil {
		for _, s := range sugg {
			if seen[s.SKU] {
				suggested[s.SKU] = float64(s.SuggestedQty)
			}
		}
	}

	type stockAt struct {
		raw, loc, sku string
		available     float64
	}
	var stock []stockAt
	haveHere := map[string]float64{}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT location_code, sku, available::float8 FROM %s.inventory_availability
		 WHERE sku = ANY($1) AND available > 0`, schema), pq.Array(skus))
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var s stockAt
		if rows.Scan(&s.raw, &s.sku, &s.available) != nil {
			continue
		}
		s.loc = locs.canon(s.raw)
		if s.loc == req.ID {
			haveHere[s.sku] += s.available
			continue
		}
		if l := locs.byID[s.loc]; l == nil || !l.active() {
			continue
		}
		stock = append(stock, s)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	promised, err := stockRequestPromised(tenantID, schema, locs, skus)
	if err != nil {
		return nil, err
	}
	velocity := map[string]map[string]float64{}
	if v, err := SalesVelocityAllLocations(tenantID, 30); err == nil {
		for loc, bySKU := range v {
			c := locs.canon(loc)
			if velocity[c] == nil {
				velocity[c] = map[string]float64{}
			}
			for sku, q := range bySKU {
				velocity[c][sku] += q
			}
		}
	}
	pins := []string{req.Pincode}
	for _, s := range stock {
		pins = append(pins, locs.byID[s.loc].Pincode)
	}
	coords, _ := pinCoordinates(schema, pins)

	options := map[string][]SourceOption{}
	for _, s := range stock {
		src := locs.byID[s.loc]
		lead, safety := stockRequestDefaultLead, stockRequestDefaultSafety
		if l, sf, found := resolveReorderPointConfig(tenantID, s.sku, s.raw); found {
			lead, safety = l, sf
		}
		v := velocity[s.loc][s.sku]
		keeps := 2*math.Ceil(v*float64(lead)) + float64(safety)
		taken := promised[s.loc+"|"+s.sku]
		spare := math.Floor(s.available - keeps - taken)
		if spare < 1 {
			continue
		}
		o := SourceOption{Location: src.ID, Name: src.label(), Type: src.Type, Available: s.available,
			Keeps: keeps, Promised: taken, Spare: spare, DistanceKm: -1, CoverDays: -1}
		if km, ok := pinDistanceKm(coords, req.Pincode, src.Pincode); ok {
			o.DistanceKm = math.Round(km)
			o.DistanceNote = fmt.Sprintf("%g km", o.DistanceKm)
		} else if req.City != "" && strings.EqualFold(req.City, src.City) {
			o.DistanceNote = "same city"
		} else if req.State != "" && strings.EqualFold(req.State, src.State) {
			o.DistanceNote = "same state"
		}
		if v > 0 {
			o.CoverDays = math.Round(s.available / v)
		}
		o.Why = append(o.Why, "can spare "+fmtUnits(spare))
		if o.DistanceNote != "" {
			o.Why = append(o.Why, o.DistanceNote)
		} else {
			o.Why = append(o.Why, "distance not known")
		}
		switch {
		case v <= 0:
			o.Why = append(o.Why, "not selling there")
		case o.CoverDays >= 60:
			o.Why = append(o.Why, "slow seller there")
		default:
			o.Why = append(o.Why, fmt.Sprintf("%g days of cover", o.CoverDays))
		}
		if strings.EqualFold(src.Type, "Warehouse") {
			o.Why = append(o.Why, "warehouse")
		}
		if taken > 0 {
			o.Why = append(o.Why, fmtUnits(taken)+" already promised")
		}
		options[s.sku] = append(options[s.sku], o)
	}

	names := itemNames(schema, skus)
	for _, l := range lines {
		qty := l.Qty
		if qty <= 0 {
			qty = suggested[l.SKU]
		}
		sl := SourcedLine{SKU: l.SKU, Name: names[l.SKU], Qty: qty, Suggested: suggested[l.SKU], HaveHere: haveHere[l.SKU],
			Options: []SourceOption{}, Plan: []SourcePick{}}
		if l.From != "" {
			if src := locs.get(l.From); src != nil {
				sl.Pinned = src.ID
			} else {
				sl.Pinned = l.From
			}
		}
		for _, o := range options[l.SKU] {
			o.Score = sourceScore(&o, qty)
			sl.Options = append(sl.Options, o)
		}
		sort.SliceStable(sl.Options, func(i, j int) bool {
			a, b := sl.Options[i], sl.Options[j]
			if a.Score != b.Score {
				return a.Score > b.Score
			}
			if (a.DistanceKm >= 0) != (b.DistanceKm >= 0) {
				return a.DistanceKm >= 0
			}
			if a.DistanceKm != b.DistanceKm {
				return a.DistanceKm < b.DistanceKm
			}
			return a.Spare > b.Spare
		})
		plan.Lines = append(plan.Lines, sl)
	}

	// The plan: bigger lines first, so they anchor the shipments the smaller
	// ones then join.
	order := make([]int, len(plan.Lines))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(i, j int) bool { return plan.Lines[order[i]].Qty > plan.Lines[order[j]].Qty })
	chosen := map[string]bool{}
	for _, idx := range order {
		l := &plan.Lines[idx]
		need := l.Qty
		if need < 1 {
			continue
		}
		if l.Pinned != "" {
			name := l.Pinned
			if src := locs.byID[l.Pinned]; src != nil {
				name = src.label()
			}
			l.Plan = append(l.Plan, SourcePick{From: l.Pinned, FromName: name, Qty: need})
			chosen[l.Pinned] = true
			continue
		}
		used := map[string]bool{}
		for need >= 1 {
			best, bestScore := -1, -1.0
			for i := range l.Options {
				o := &l.Options[i]
				if used[o.Location] {
					continue
				}
				sc := sourceScore(o, need)
				if chosen[o.Location] {
					sc += stockRequestSameSourcePts
				}
				if sc > bestScore {
					best, bestScore = i, sc
				}
			}
			if best < 0 {
				break
			}
			o := &l.Options[best]
			used[o.Location] = true
			take := math.Min(o.Spare, need)
			l.Plan = append(l.Plan, SourcePick{From: o.Location, FromName: o.Name, Qty: take})
			chosen[o.Location] = true
			need -= take
		}
		if need > 0 {
			l.Short = need
		}
	}

	// Shipments in the order the lines first name them; options capped for
	// the screen (a planned source is always kept).
	at := map[string]int{}
	for i := range plan.Lines {
		l := &plan.Lines[i]
		for _, p := range l.Plan {
			j, ok := at[p.From]
			if !ok {
				j = len(plan.Shipments)
				at[p.From] = j
				sh := SourcingShipment{From: p.From, FromName: p.FromName, DistanceKm: -1}
				for _, o := range l.Options {
					if o.Location == p.From {
						sh.DistanceKm, sh.DistanceNote = o.DistanceKm, o.DistanceNote
					}
				}
				plan.Shipments = append(plan.Shipments, sh)
			}
			plan.Shipments[j].Lines++
			plan.Shipments[j].Units += p.Qty
		}
		if len(l.Options) > stockRequestMaxOptions {
			keep := append([]SourceOption{}, l.Options[:stockRequestMaxOptions]...)
			for _, o := range l.Options[stockRequestMaxOptions:] {
				for _, p := range l.Plan {
					if p.From == o.Location {
						keep = append(keep, o)
						break
					}
				}
			}
			l.Options = keep
		}
	}
	return plan, nil
}
