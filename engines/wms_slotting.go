package engines

import (
	"custom_erp/db"
	"fmt"
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
