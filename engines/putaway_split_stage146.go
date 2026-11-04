package engines

import (
	"fmt"
	"math"
	"strings"

	"custom_erp/db"
)

// Stage 146.3.15: a put-away that fits no single bin can be suggested as
// parts - the best bin's room first, then the next - and a bin that would
// reach a set fill percent gives a warning before the hard capacity block.
//
//	wms.putaway_split         off: one bin only, exactly as before
//	wms.putaway_fill_warn_pct 0: no warning
//
// The split never puts a second item into a single-item bin: bins come from
// the same lists the single suggestion uses (home bins, put-away rules, the
// directed strategy), all passed through the bin mix rule.

// PutawayPart is one bin and how many pieces go into it.
type PutawayPart struct {
	BinCode string `json:"bin_code"`
	Qty     int    `json:"qty"`
}

// PutawayPlan is the put-away suggestion for a quantity. BinCode is set when
// the whole quantity goes to one bin; Parts lists every bin (one or more).
type PutawayPlan struct {
	BinCode string        `json:"bin_code"`
	Reason  string        `json:"reason"`
	Parts   []PutawayPart `json:"parts,omitempty"`
	Warning string        `json:"warning,omitempty"`
}

// binRoom is how many more pieces of sku the bin takes under its quantity,
// weight and volume limits. limited is false when the bin has no limit.
func binRoom(schema, binCode, sku string) (room int, limited bool, capacity, cur float64) {
	var maxQty, maxWeight, maxVolume float64
	err := db.DB.QueryRow(fmt.Sprintf(`
		SELECT COALESCE(NULLIF(data->>'capacity', '')::numeric, 0), COALESCE(NULLIF(data->>'max_weight', '')::numeric, 0),
		       COALESCE(NULLIF(data->>'max_volume', '')::numeric, 0)
		FROM %s.documents WHERE doctype = 'Bin' AND deleted_at IS NULL AND data->>'bin_code' = $1 LIMIT 1`, schema), binCode).
		Scan(&maxQty, &maxWeight, &maxVolume)
	if err != nil || (maxQty <= 0 && maxWeight <= 0 && maxVolume <= 0) {
		return 0, false, 0, 0
	}
	var curWeight, curVolume, itemWeight, itemVolume float64
	_ = db.DB.QueryRow(fmt.Sprintf(`
		SELECT COALESCE(SUM(bs.qty), 0),
		       COALESCE(SUM(bs.qty * COALESCE(NULLIF(i.data->>'weight', '')::numeric, 0)), 0),
		       COALESCE(SUM(bs.qty * COALESCE(NULLIF(i.data->>'volume', '')::numeric, 0)), 0)
		FROM %[1]s.bin_stock bs
		LEFT JOIN %[1]s.documents i ON i.doctype = 'Item' AND i.data->>'code' = bs.sku
		WHERE bs.bin_code = $1`, schema), binCode).Scan(&cur, &curWeight, &curVolume)
	_ = db.DB.QueryRow(fmt.Sprintf(`
		SELECT COALESCE(NULLIF(data->>'weight', '')::numeric, 0), COALESCE(NULLIF(data->>'volume', '')::numeric, 0)
		FROM %s.documents WHERE doctype = 'Item' AND data->>'code' = $1`, schema), sku).Scan(&itemWeight, &itemVolume)
	room = math.MaxInt32
	if maxQty > 0 {
		room = int(math.Min(float64(room), math.Floor(maxQty-cur)))
	}
	if maxWeight > 0 && itemWeight > 0 {
		room = int(math.Min(float64(room), math.Floor((maxWeight-curWeight)/itemWeight)))
	}
	if maxVolume > 0 && itemVolume > 0 {
		room = int(math.Min(float64(room), math.Floor((maxVolume-curVolume)/itemVolume)))
	}
	if room < 0 {
		room = 0
	}
	return room, true, maxQty, cur
}

// PutawayFillWarning says so when putting addQty of sku into the bin would
// fill it to wms.putaway_fill_warn_pct of its capacity or more. "" when the
// setting is 0, the bin has no quantity capacity, or it stays below.
func PutawayFillWarning(tenantID, binCode, sku string, addQty int) string {
	pct := GetSettingInt(tenantID, "wms.putaway_fill_warn_pct")
	if pct <= 0 || addQty <= 0 {
		return ""
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return ""
	}
	_, limited, capacity, cur := binRoom(schema, binCode, sku)
	if !limited || capacity <= 0 {
		return ""
	}
	after := cur + float64(addQty)
	fill := int(math.Round(after / capacity * 100))
	if fill < pct {
		return ""
	}
	return fmt.Sprintf("bin %s will be %d%% full (%.0f of %.0f)", binCode, fill, after, capacity)
}

// splitBinOrder lists the bins a split may use, best first, each only once.
func splitBinOrder(tenantID, schema, sku, locationCode, batchNo string) ([]string, error) {
	var order []string
	seen := map[string]bool{}
	add := func(b string) {
		if b != "" && !seen[b] {
			seen[b] = true
			order = append(order, b)
		}
	}
	homes, err := homeBinsFor(tenantID, sku, locationCode, 1, batchNo)
	if err != nil {
		return nil, err
	}
	for _, h := range homes {
		if h.Usable {
			add(h.BinCode)
		}
	}
	rb, _, err := ruleBins(tenantID, sku, locationCode, 1, batchNo, 0)
	if err != nil {
		return nil, err
	}
	for _, r := range rb {
		add(r.bin)
	}
	criteria, err := resolvePutawayCriteria(tenantID, locationCode)
	if err != nil {
		return nil, err
	}
	if len(criteria) > 0 {
		cands, err := loadPutawayCandidates(tenantID, schema, locationCode, sku, 1, criteria["capacity"])
		if err != nil {
			return nil, err
		}
		cands = dropMixRefusedBins(schema, sku, batchNo, cands)
		if criteria["hazmat_temp"] {
			if cands, err = filterHazmatTemp(tenantID, schema, sku, cands); err != nil {
				return nil, err
			}
		}
		sortPutawayCandidates(cands, criteria, "")
		for _, c := range cands {
			add(c.BinCode)
		}
	}
	return order, nil
}

// SuggestPutawayPlan is SuggestPutawayBin for a whole quantity: one bin when
// one takes it (the answer is the same as SuggestPutawayBin's), otherwise,
// with wms.putaway_split on, the parts that fit the best bins' room.
func SuggestPutawayPlan(tenantID, sku, locationCode string, qty int, batchNo string) (PutawayPlan, error) {
	bin, reason, err := SuggestPutawayBin(tenantID, sku, locationCode, qty, batchNo)
	if err != nil {
		return PutawayPlan{}, err
	}
	if bin != "" {
		return PutawayPlan{BinCode: bin, Reason: reason, Parts: []PutawayPart{{bin, qty}}, Warning: PutawayFillWarning(tenantID, bin, sku, qty)}, nil
	}
	if !GetSettingBool(tenantID, "wms.putaway_split") {
		return PutawayPlan{Reason: reason}, nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return PutawayPlan{}, err
	}
	order, err := splitBinOrder(tenantID, schema, sku, locationCode, batchNo)
	if err != nil {
		return PutawayPlan{}, err
	}
	left := qty
	var parts []PutawayPart
	for _, b := range order {
		if left <= 0 {
			break
		}
		room, limited, _, _ := binRoom(schema, b, sku)
		take := left
		if limited && room < take {
			take = room
		}
		if take > 0 {
			parts = append(parts, PutawayPart{b, take})
			left -= take
		}
	}
	if len(parts) == 0 {
		return PutawayPlan{Reason: reason}, nil
	}
	var names, warns []string
	for _, p := range parts {
		names = append(names, fmt.Sprintf("%s %d", p.BinCode, p.Qty))
		if w := PutawayFillWarning(tenantID, p.BinCode, sku, p.Qty); w != "" {
			warns = append(warns, w)
		}
	}
	plan := PutawayPlan{Parts: parts, Warning: strings.Join(warns, "; ")}
	if left > 0 {
		plan.Reason = fmt.Sprintf("split over %d bins (%s) - %d left with no room", len(parts), strings.Join(names, ", "), left)
	} else {
		plan.Reason = fmt.Sprintf("split over %d bins: %s", len(parts), strings.Join(names, ", "))
	}
	if len(parts) == 1 && left == 0 {
		plan.BinCode = parts[0].BinCode
	}
	return plan, nil
}
