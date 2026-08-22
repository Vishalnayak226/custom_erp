package engines

import (
	"custom_erp/db"
	"database/sql"
	"fmt"
	"sort"
)

// Stage 26.5.12 (WMS Enterprise Maturity Sprint P2 follow-up): slotting/
// re-slotting optimizer. Go-ahead given 2026-07-27 for all five P2 bundles
// previously deferred pending a real warehouse-scale pilot - built as a
// read-only suggestion (same no-auto-move precedent GetReplenishmentSuggestions/
// GetBinReplenishmentSuggestions/GetABCCycleCountPlan already set), reusing
// 26.5.9's own ABC velocity classification directly instead of a second
// tiering pass.
//
// The "distance from the pack station" this needs has no real warehouse-
// geometry model to draw on, so it reuses the one signal this codebase
// already has for exactly this purpose: 26.5.5's bin_type (PickFace vs
// Reserve) - PickFace bins are, by that field's own existing definition,
// the pick-optimized/accessible locations, and Reserve is bulk/back
// storage. A fast-moving (Tier A) SKU with no PickFace presence is
// suggested INTO the first Active PickFace bin with spare capacity; a
// slow-moving (Tier C) SKU occupying a PickFace bin is suggested back OUT
// to Reserve, freeing that pick-face slot for a faster mover.
type SlottingSuggestion struct {
	Sku           string  `json:"sku"`
	LocationCode  string  `json:"location_code"`
	Tier          string  `json:"tier"`
	DailyVelocity float64 `json:"daily_velocity"`
	Action        string  `json:"action"` // "Move to PickFace" or "Move to Reserve"
	FromBinCode   string  `json:"from_bin_code"`
	ToBinCode     string  `json:"to_bin_code"`
	Qty           int     `json:"qty"`
	Reason        string  `json:"reason"`
}

type binSkuStock struct {
	BinCode string
	BinType string
	Qty     int
}

func init() {
	RegisterReport(ReportDefinition{
		ID: "slotting-suggestions", Label: "Slotting / Re-Slotting Suggestions", Category: "WMS",
		Columns: []ReportColumn{
			{Key: "sku", Label: "SKU"}, {Key: "tier", Label: "Velocity Tier"}, {Key: "daily_velocity", Label: "Daily Velocity"},
			{Key: "action", Label: "Suggested Action"}, {Key: "from_bin_code", Label: "From Bin"}, {Key: "to_bin_code", Label: "To Bin"},
			{Key: "qty", Label: "Qty"}, {Key: "reason", Label: "Reason"},
		},
		Params: []ReportParam{{Key: "location_code", Label: "Location", Type: "text", Required: true}},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			rows, err := GetSlottingSuggestions(tenantID, params["location_code"])
			if err != nil {
				return nil, err
			}
			return structsToRows(rows)
		},
	})
}

func GetSlottingSuggestions(tenantID, locationCode string) ([]SlottingSuggestion, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}

	tiers, err := GetABCCycleCountPlan(tenantID, locationCode, 0, 0, 0)
	if err != nil {
		return nil, err
	}

	// Every Active bin at this location, with its type - looked up once and
	// reused for every SKU below rather than a per-SKU bin query.
	binRows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, COALESCE(data->>'bin_code', ''), COALESCE(data->>'bin_type', ''), COALESCE(data->>'capacity', '0')
		FROM %s.documents WHERE doctype = 'Bin' AND status = 'Active' AND data->>'location' = $1`, schema), locationCode)
	if err != nil {
		return nil, err
	}
	type binMeta struct {
		BinType  string
		Capacity int
	}
	binTypeByCode := map[string]binMeta{}
	var pickFaceBinsInOrder []string
	var reserveBinsInOrder []string
	for binRows.Next() {
		var id, binCode, binType, capStr string
		if err := binRows.Scan(&id, &binCode, &binType, &capStr); err != nil {
			continue
		}
		if binCode == "" {
			continue
		}
		binTypeByCode[binCode] = binMeta{BinType: binType, Capacity: int(numFromInterface(capStr))}
		if binType == "PickFace" {
			pickFaceBinsInOrder = append(pickFaceBinsInOrder, binCode)
		} else if binType == "Reserve" {
			reserveBinsInOrder = append(reserveBinsInOrder, binCode)
		}
	}
	binRows.Close()

	suggestions := []SlottingSuggestion{}
	for _, tier := range tiers {
		if tier.Tier != "A" && tier.Tier != "C" {
			continue
		}
		stockRows, err := db.DB.Query(fmt.Sprintf(
			`SELECT bin_code, COALESCE(qty, 0) FROM %s.bin_stock WHERE sku = $1 AND location_code = $2 AND condition = 'Good' AND qty > 0`, schema),
			tier.Sku, locationCode)
		if err != nil {
			continue
		}
		var stocks []binSkuStock
		for stockRows.Next() {
			var s binSkuStock
			if err := stockRows.Scan(&s.BinCode, &s.Qty); err != nil {
				continue
			}
			s.BinType = binTypeByCode[s.BinCode].BinType
			stocks = append(stocks, s)
		}
		stockRows.Close()

		hasPickFace, hasReserve := false, false
		for _, s := range stocks {
			if s.BinType == "PickFace" {
				hasPickFace = true
			} else if s.BinType == "Reserve" {
				hasReserve = true
			}
		}

		if tier.Tier == "A" && !hasPickFace && hasReserve {
			// Fast mover stuck entirely in Reserve - suggest a slot into the
			// first PickFace bin that isn't already this SKU's own bin.
			target := ""
			for _, b := range pickFaceBinsInOrder {
				target = b
				break
			}
			if target == "" {
				continue // no PickFace bin exists at this location to suggest into
			}
			fromBin, qty := stocks[0].BinCode, stocks[0].Qty
			for _, s := range stocks {
				if s.Qty > qty {
					fromBin, qty = s.BinCode, s.Qty
				}
			}
			suggestions = append(suggestions, SlottingSuggestion{
				Sku: tier.Sku, LocationCode: locationCode, Tier: tier.Tier, DailyVelocity: tier.DailyVelocity,
				Action: "Move to PickFace", FromBinCode: fromBin, ToBinCode: target, Qty: qty,
				Reason: "Tier A (fast-moving) SKU has no pick-face presence - currently Reserve-only",
			})
		} else if tier.Tier == "C" && hasPickFace {
			target := ""
			for _, b := range reserveBinsInOrder {
				target = b
				break
			}
			if target == "" {
				continue
			}
			for _, s := range stocks {
				if s.BinType != "PickFace" {
					continue
				}
				suggestions = append(suggestions, SlottingSuggestion{
					Sku: tier.Sku, LocationCode: locationCode, Tier: tier.Tier, DailyVelocity: tier.DailyVelocity,
					Action: "Move to Reserve", FromBinCode: s.BinCode, ToBinCode: target, Qty: s.Qty,
					Reason: "Tier C (slow-moving) SKU is occupying pick-face space a faster mover could use",
				})
			}
		}
	}
	return suggestions, nil
}

// ------------------------------------------------------------------
// 42.5.4: Slotting v2 - unslotting, capacity/dimension-driven re-slot,
// clean-location consolidation
// ------------------------------------------------------------------

func init() {
	RegisterReport(ReportDefinition{
		ID: "unslotting-suggestions", Label: "Unslotting Suggestions (discontinued items)", Category: "WMS",
		Columns: []ReportColumn{
			{Key: "sku", Label: "SKU"}, {Key: "action", Label: "Action"}, {Key: "from_bin_code", Label: "From Bin"},
			{Key: "to_bin_code", Label: "To Bin"}, {Key: "qty", Label: "Qty"}, {Key: "reason", Label: "Reason"},
		},
		Params: []ReportParam{{Key: "location_code", Label: "Location", Type: "text", Required: true}},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			rows, err := GetUnslottingSuggestions(tenantID, params["location_code"])
			if err != nil {
				return nil, err
			}
			return structsToRows(rows)
		},
	})
	RegisterReport(ReportDefinition{
		ID: "consolidation-suggestions", Label: "Clean-Location Consolidation Suggestions", Category: "WMS",
		Columns: []ReportColumn{
			{Key: "sku", Label: "SKU"}, {Key: "action", Label: "Action"}, {Key: "from_bin_code", Label: "From Bin"},
			{Key: "to_bin_code", Label: "To Bin"}, {Key: "qty", Label: "Qty"}, {Key: "reason", Label: "Reason"},
		},
		Params: []ReportParam{{Key: "location_code", Label: "Location", Type: "text", Required: true}},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			rows, err := GetConsolidationSuggestions(tenantID, params["location_code"])
			if err != nil {
				return nil, err
			}
			return structsToRows(rows)
		},
	})
}

// GetUnslottingSuggestions (42.5.4) finds stock still sitting in a bin for a
// SKU whose Item master has been Cancelled - this codebase's actual "this
// item is leaving the catalogue" signal (Item has no separate
// Active/Inactive lifecycle field; every other Item-status check in this
// codebase, e.g. engines/pim.go's public API filters, already treats
// status = 'Cancelled' as "not a live item" the same way). The item is
// going away regardless of how it ranks on velocity, so unlike the
// ABC-driven Tier C case above, this fires independent of tier and even for
// a slow mover that would otherwise be left alone. Prefers a Reserve bin
// with capacity as the destination (freeing the PickFace slot outright); if
// none has room, still reports the suggestion with ToBinCode left blank
// rather than silently dropping it, the same "shortage with no source"
// visibility pattern BinReplenishmentSuggestion already uses for the
// opposite case.
func GetUnslottingSuggestions(tenantID, locationCode string) ([]SlottingSuggestion, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}

	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT bs.bin_code, bs.sku, bs.qty, COALESCE(b.data->>'bin_type', '')
		FROM %s.bin_stock bs
		LEFT JOIN %s.documents b ON b.doctype = 'Bin' AND b.data->>'bin_code' = bs.bin_code
		JOIN %s.documents i ON i.doctype = 'Item' AND i.data->>'code' = bs.sku
		WHERE bs.location_code = $1 AND bs.condition = 'Good' AND bs.qty > 0 AND i.status = 'Cancelled'`,
		schema, schema, schema), locationCode)
	if err != nil {
		return nil, err
	}
	type candidate struct {
		BinCode string
		Sku     string
		Qty     int
		BinType string
	}
	var candidates []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.BinCode, &c.Sku, &c.Qty, &c.BinType); err != nil {
			rows.Close()
			return nil, err
		}
		candidates = append(candidates, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, nil
	}

	reserveRows, err := db.DB.Query(fmt.Sprintf(`
		SELECT data->>'bin_code' FROM %s.documents
		WHERE doctype = 'Bin' AND status = 'Active' AND data->>'location' = $1 AND data->>'bin_type' = 'Reserve'`, schema), locationCode)
	if err != nil {
		return nil, err
	}
	var reserveBins []string
	for reserveRows.Next() {
		var code string
		if err := reserveRows.Scan(&code); err != nil {
			reserveRows.Close()
			return nil, err
		}
		reserveBins = append(reserveBins, code)
	}
	reserveRows.Close()
	if err := reserveRows.Err(); err != nil {
		return nil, err
	}

	suggestions := []SlottingSuggestion{}
	for _, c := range candidates {
		if c.BinType == "Reserve" {
			continue // already out of pick-face reach, nothing to reclaim
		}
		target := ""
		for _, rb := range reserveBins {
			fits, _, ferr := binCapacityHeadroom(schema, rb, c.Sku, c.Qty)
			if ferr == nil && fits {
				target = rb
				break
			}
		}
		suggestions = append(suggestions, SlottingSuggestion{
			Sku: c.Sku, LocationCode: locationCode, Action: "Unslot", FromBinCode: c.BinCode, ToBinCode: target, Qty: c.Qty,
			Reason: fmt.Sprintf("Item %s is Inactive - bin %s space should be reclaimed", c.Sku, c.BinCode),
		})
	}
	return suggestions, nil
}

// binCapacityHeadroom is enforceBinCapacity's read-only sibling: same
// capacity/weight/volume accounting (engines/wms.go), but against db.DB
// directly rather than an in-flight putaway transaction, so a slotting
// suggestion can ask "would this fit" without holding a lock or committing
// anything. spareQty is how many more units could still fit purely by the
// bin's own qty capacity (0 if maxQty is unset - unbounded by qty, only by
// weight/volume if those are set), used to rank candidate bins.
func binCapacityHeadroom(schema, binCode, sku string, addQty int) (fits bool, spareQty float64, err error) {
	var maxQty, maxWeight, maxVolume float64
	if err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT COALESCE(NULLIF(data->>'capacity', '')::numeric, 0),
		       COALESCE(NULLIF(data->>'max_weight', '')::numeric, 0),
		       COALESCE(NULLIF(data->>'max_volume', '')::numeric, 0)
		FROM %s.documents WHERE doctype = 'Bin' AND data->>'bin_code' = $1`, schema), binCode).
		Scan(&maxQty, &maxWeight, &maxVolume); err != nil && err != sql.ErrNoRows {
		return false, 0, err
	}
	var curQty, curWeight, curVolume float64
	if err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT COALESCE(SUM(bs.qty), 0),
		       COALESCE(SUM(bs.qty * COALESCE(NULLIF(i.data->>'weight', '')::numeric, 0)), 0),
		       COALESCE(SUM(bs.qty * COALESCE(NULLIF(i.data->>'volume', '')::numeric, 0)), 0)
		FROM %s.bin_stock bs
		LEFT JOIN %s.documents i ON i.doctype = 'Item' AND i.data->>'code' = bs.sku
		WHERE bs.bin_code = $1`, schema, schema), binCode).
		Scan(&curQty, &curWeight, &curVolume); err != nil && err != sql.ErrNoRows {
		return false, 0, err
	}
	var itemWeight, itemVolume float64
	if err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT COALESCE(NULLIF(data->>'weight', '')::numeric, 0), COALESCE(NULLIF(data->>'volume', '')::numeric, 0)
		FROM %s.documents WHERE doctype = 'Item' AND data->>'code' = $1`, schema), sku).
		Scan(&itemWeight, &itemVolume); err != nil && err != sql.ErrNoRows {
		return false, 0, err
	}
	newQty := curQty + float64(addQty)
	newWeight := curWeight + float64(addQty)*itemWeight
	newVolume := curVolume + float64(addQty)*itemVolume
	if maxQty > 0 && newQty > maxQty {
		return false, 0, nil
	}
	if maxWeight > 0 && newWeight > maxWeight {
		return false, 0, nil
	}
	if maxVolume > 0 && newVolume > maxVolume {
		return false, 0, nil
	}
	if maxQty > 0 {
		spareQty = maxQty - curQty
	}
	return true, spareQty, nil
}

// GetDimensionAwareSlottingSuggestions (42.5.4) is GetSlottingSuggestions'
// Tier-A "needs a pick-face slot" case, redone to actually check whether a
// candidate PickFace bin has room (v1 always suggested the first Active
// PickFace bin it found, with no capacity check at all) - among every
// PickFace bin the move would fit into, picks the one with the most spare
// qty headroom (binCapacityHeadroom), so a move is never suggested into a
// bin that's already full, and prefers spreading load rather than always
// targeting the same first bin.
func GetDimensionAwareSlottingSuggestions(tenantID, locationCode string) ([]SlottingSuggestion, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	tiers, err := GetABCCycleCountPlan(tenantID, locationCode, 0, 0, 0)
	if err != nil {
		return nil, err
	}

	pickFaceRows, err := db.DB.Query(fmt.Sprintf(`
		SELECT data->>'bin_code' FROM %s.documents
		WHERE doctype = 'Bin' AND status = 'Active' AND data->>'location' = $1 AND data->>'bin_type' = 'PickFace'`, schema), locationCode)
	if err != nil {
		return nil, err
	}
	var pickFaceBins []string
	for pickFaceRows.Next() {
		var code string
		if err := pickFaceRows.Scan(&code); err != nil {
			pickFaceRows.Close()
			return nil, err
		}
		pickFaceBins = append(pickFaceBins, code)
	}
	pickFaceRows.Close()
	if err := pickFaceRows.Err(); err != nil {
		return nil, err
	}
	if len(pickFaceBins) == 0 {
		return nil, nil
	}

	suggestions := []SlottingSuggestion{}
	for _, tier := range tiers {
		if tier.Tier != "A" {
			continue
		}
		stockRows, err := db.DB.Query(fmt.Sprintf(`
			SELECT bs.bin_code, bs.qty, COALESCE(b.data->>'bin_type', '')
			FROM %s.bin_stock bs LEFT JOIN %s.documents b ON b.doctype = 'Bin' AND b.data->>'bin_code' = bs.bin_code
			WHERE bs.sku = $1 AND bs.location_code = $2 AND bs.condition = 'Good' AND bs.qty > 0`, schema, schema),
			tier.Sku, locationCode)
		if err != nil {
			return nil, err
		}
		var stocks []binSkuStock
		for stockRows.Next() {
			var s binSkuStock
			if err := stockRows.Scan(&s.BinCode, &s.Qty, &s.BinType); err != nil {
				stockRows.Close()
				return nil, err
			}
			stocks = append(stocks, s)
		}
		stockRows.Close()
		if err := stockRows.Err(); err != nil {
			return nil, err
		}

		hasPickFace := false
		fromBin, fromQty := "", 0
		for _, s := range stocks {
			if s.BinType == "PickFace" {
				hasPickFace = true
			}
			if s.Qty > fromQty {
				fromBin, fromQty = s.BinCode, s.Qty
			}
		}
		if hasPickFace || fromBin == "" {
			continue
		}

		bestBin, bestHeadroom := "", -1.0
		for _, pf := range pickFaceBins {
			fits, headroom, ferr := binCapacityHeadroom(schema, pf, tier.Sku, fromQty)
			if ferr != nil || !fits {
				continue
			}
			if bestBin == "" || headroom > bestHeadroom {
				bestBin, bestHeadroom = pf, headroom
			}
		}
		if bestBin == "" {
			continue // no PickFace bin at this location can actually fit it
		}
		suggestions = append(suggestions, SlottingSuggestion{
			Sku: tier.Sku, LocationCode: locationCode, Tier: tier.Tier, DailyVelocity: tier.DailyVelocity,
			Action: "Move to PickFace", FromBinCode: fromBin, ToBinCode: bestBin, Qty: fromQty,
			Reason: "Tier A (fast-moving) SKU has no pick-face presence - currently Reserve-only, and this bin has room for it",
		})
	}
	return suggestions, nil
}

// GetConsolidationSuggestions (42.5.4) is "clean-location consolidation":
// a SKU fragmented across several bins of the same bin_type at one location
// wastes both bins and pick-path distance. For each such SKU, if the
// smallest holding bin's entire qty would fit into the largest holding bin
// without exceeding capacity, suggests moving all of it there - fully
// emptying the smaller bin rather than partially topping up the larger one,
// so the freed bin can be reused for something else instead of staying
// half-occupied.
func GetConsolidationSuggestions(tenantID, locationCode string) ([]SlottingSuggestion, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT bs.sku, bs.bin_code, bs.qty, COALESCE(b.data->>'bin_type', '')
		FROM %s.bin_stock bs LEFT JOIN %s.documents b ON b.doctype = 'Bin' AND b.data->>'bin_code' = bs.bin_code
		WHERE bs.location_code = $1 AND bs.condition = 'Good' AND bs.qty > 0`, schema, schema), locationCode)
	if err != nil {
		return nil, err
	}
	type placement struct {
		BinCode string
		Qty     int
		BinType string
	}
	bySku := map[string][]placement{}
	for rows.Next() {
		var sku string
		var p placement
		if err := rows.Scan(&sku, &p.BinCode, &p.Qty, &p.BinType); err != nil {
			rows.Close()
			return nil, err
		}
		bySku[sku] = append(bySku[sku], p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var skus []string
	for sku := range bySku {
		skus = append(skus, sku)
	}
	sort.Strings(skus)

	suggestions := []SlottingSuggestion{}
	for _, sku := range skus {
		byType := map[string][]placement{}
		for _, p := range bySku[sku] {
			byType[p.BinType] = append(byType[p.BinType], p)
		}
		for binType, placements := range byType {
			if len(placements) < 2 {
				continue
			}
			sort.Slice(placements, func(i, j int) bool { return placements[i].Qty < placements[j].Qty })
			smallest, largest := placements[0], placements[len(placements)-1]
			if smallest.BinCode == largest.BinCode {
				continue
			}
			fits, _, ferr := binCapacityHeadroom(schema, largest.BinCode, sku, smallest.Qty)
			if ferr != nil || !fits {
				continue
			}
			suggestions = append(suggestions, SlottingSuggestion{
				Sku: sku, LocationCode: locationCode, Action: "Consolidate", FromBinCode: smallest.BinCode,
				ToBinCode: largest.BinCode, Qty: smallest.Qty,
				Reason: fmt.Sprintf("SKU is split across %d %s bins - consolidating frees bin %s entirely", len(placements), binType, smallest.BinCode),
			})
		}
	}
	return suggestions, nil
}
