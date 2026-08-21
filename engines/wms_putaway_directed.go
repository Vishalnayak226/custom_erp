package engines

import (
	"custom_erp/db"
	"database/sql"
	"fmt"
	"strings"
)

// Stage 42.2.7 - PutawayStrategy master + directed putaway: suggest the bin
// instead of asking the operator to type one. SuggestPutawayBin is
// deliberately advisory - it is never called from PutawayToBin itself, only
// from the Putaway screen's own "Suggest Bin" action, so a bad or missing
// suggestion never blocks a putaway the way validation does. No configured
// Active PutawayStrategy for a location (every tenant's state before this
// item, and any tenant that never opens this master) means "" is returned
// with no error - "falls back to today's manual entry," the plan's own
// framing, satisfied by simply not building a second code path for it.

// putawayCriteriaWeights is the whitelist validatePutawayStrategyMasterRules
// checks PutawayStrategy.criteria against - same closed-whitelist shape
// taskDispatchSortFragments (42.2.4) uses, so a strategy can only ever ask
// for a signal SuggestPutawayBin actually knows how to weigh.
var putawayCriteriaWeights = map[string]bool{
	"velocity":            true,
	"zone_sequence":       true,
	"capacity":            true,
	"hazmat_temp":         true,
	"batch_consolidation": true,
}

type putawayBinCandidate struct {
	BinCode      string
	BinType      string
	ZoneCode     string
	PutawaySeq   int
	HasZoneSeq   bool
	HeadroomQty  float64
	HasHeadroom  bool // false = bin has no configured capacity, i.e. unlimited
	ExistingBatchQty int
}

// SuggestPutawayBin (42.2.7) picks the single best bin for qty units of sku
// arriving at locationCode, honouring whichever of the resolved
// PutawayStrategy's criteria are enabled. Returns "", "", nil (not an error)
// when no strategy is configured or no eligible bin survives filtering - both
// are real, expected outcomes the caller falls back to manual entry for.
func SuggestPutawayBin(tenantID, sku, locationCode string, qty int, batchNo string) (binCode, reason string, err error) {
	if qty <= 0 {
		return "", "", fmt.Errorf("qty must be positive")
	}
	criteria, err := resolvePutawayCriteria(tenantID, locationCode)
	if err != nil {
		return "", "", err
	}
	if len(criteria) == 0 {
		return "", "no directed putaway strategy is configured for this location - enter a bin manually", nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", "", err
	}

	candidates, err := loadPutawayCandidates(tenantID, schema, locationCode, sku, qty, criteria["capacity"])
	if err != nil {
		return "", "", err
	}
	if len(candidates) == 0 {
		return "", "no Active, available bin at this location currently has room for this putaway", nil
	}

	if criteria["batch_consolidation"] && strings.TrimSpace(batchNo) != "" {
		best := -1
		bestQty := -1
		for i, c := range candidates {
			if c.ExistingBatchQty > bestQty {
				bestQty = c.ExistingBatchQty
				best = i
			}
		}
		if best >= 0 && bestQty > 0 {
			return candidates[best].BinCode, fmt.Sprintf("consolidating into bin %s, which already holds batch %s", candidates[best].BinCode, batchNo), nil
		}
	}

	if criteria["hazmat_temp"] {
		candidates, err = filterHazmatTemp(tenantID, schema, sku, candidates)
		if err != nil {
			return "", "", err
		}
		if len(candidates) == 0 {
			return "", "no bin at this location is hazmat/temperature-compatible with this item", nil
		}
	}

	var tier string
	if criteria["velocity"] {
		tier = itemVelocityTier(tenantID, locationCode, sku)
	}

	preferredBinType := ""
	if tier == "A" {
		preferredBinType = "PickFace"
	} else if tier == "C" {
		preferredBinType = "Reserve"
	}

	sortPutawayCandidates(candidates, criteria, preferredBinType)
	winner := candidates[0]
	return winner.BinCode, fmt.Sprintf("suggested by directed putaway (tier=%s, zone=%s, bin_type=%s)", orDash(tier), orDash(winner.ZoneCode), orDash(winner.BinType)), nil
}

// resolvePutawayCriteria mirrors resolveDispatchOrder's cascade: exact
// location match, then the blank-location "applies everywhere" row, then
// "no strategy configured" (empty map).
func resolvePutawayCriteria(tenantID, locationCode string) (map[string]bool, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	var raw string
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT data->>'criteria' FROM %s.documents
		WHERE doctype = 'PutawayStrategy' AND COALESCE(status, '') = 'Active'
		  AND data->>'location_code' = $1
		LIMIT 1`, schema), locationCode).Scan(&raw)
	if err == sql.ErrNoRows {
		err = db.DB.QueryRow(fmt.Sprintf(`
			SELECT data->>'criteria' FROM %s.documents
			WHERE doctype = 'PutawayStrategy' AND COALESCE(status, '') = 'Active'
			  AND COALESCE(data->>'location_code', '') = ''
			LIMIT 1`, schema)).Scan(&raw)
	}
	if err == sql.ErrNoRows {
		return map[string]bool{}, nil
	} else if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, tok := range strings.Split(raw, ",") {
		tok = strings.TrimSpace(tok)
		if putawayCriteriaWeights[tok] {
			out[tok] = true
		}
	}
	return out, nil
}

// loadPutawayCandidates loads every Active, available (bin_status not
// Blocked/Full/Counting) bin at locationCode, always hard-filtering out a
// bin that does not have room for qty when it has a configured capacity -
// suggesting a bin that PutawayToBin would then refuse is worse than no
// suggestion. wantHeadroom additionally computes each surviving bin's
// remaining headroom for the capacity criterion's best-fit sort.
func loadPutawayCandidates(tenantID, schema, locationCode, sku string, qty int, wantHeadroom bool) ([]putawayBinCandidate, error) {
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT data->>'bin_code', COALESCE(data->>'bin_type', ''), COALESCE(data->>'zone', ''),
		       COALESCE(NULLIF(data->>'capacity', '')::numeric, 0),
		       COALESCE(NULLIF(data->>'max_weight', '')::numeric, 0),
		       COALESCE(NULLIF(data->>'max_volume', '')::numeric, 0)
		FROM %s.documents
		WHERE doctype = 'Bin' AND status = 'Active' AND data->>'location' = $1
		  AND COALESCE(data->>'bin_status', '') NOT IN ('Blocked', 'Full', 'Counting')`, schema), locationCode)
	if err != nil {
		return nil, err
	}
	type rawBin struct {
		BinCode                        string
		BinType, Zone                  string
		Capacity, MaxWeight, MaxVolume float64
	}
	var raws []rawBin
	for rows.Next() {
		var b rawBin
		if err := rows.Scan(&b.BinCode, &b.BinType, &b.Zone, &b.Capacity, &b.MaxWeight, &b.MaxVolume); err != nil {
			rows.Close()
			return nil, err
		}
		if b.BinCode == "" {
			continue
		}
		raws = append(raws, b)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Zone putaway_sequence, looked up once per distinct zone code rather
	// than per bin.
	zoneSeq := map[string]int{}
	zoneRows, err := db.DB.Query(fmt.Sprintf(
		`SELECT data->>'code', COALESCE(NULLIF(data->>'putaway_sequence', '')::int, 999999) FROM %s.documents WHERE doctype = 'Zone' AND status = 'Active'`, schema))
	if err == nil {
		for zoneRows.Next() {
			var code string
			var seq int
			if zoneRows.Scan(&code, &seq) == nil {
				zoneSeq[code] = seq
			}
		}
		zoneRows.Close()
	}

	var out []putawayBinCandidate
	for _, b := range raws {
		c := putawayBinCandidate{BinCode: b.BinCode, BinType: b.BinType, ZoneCode: b.Zone}
		if seq, ok := zoneSeq[b.Zone]; ok {
			c.PutawaySeq = seq
			c.HasZoneSeq = true
		} else {
			c.PutawaySeq = 999999
		}

		if b.Capacity > 0 || b.MaxWeight > 0 || b.MaxVolume > 0 {
			var curQty, curWeight, curVolume float64
			if err := db.DB.QueryRow(fmt.Sprintf(`
				SELECT COALESCE(SUM(bs.qty), 0),
				       COALESCE(SUM(bs.qty * COALESCE(NULLIF(i.data->>'weight', '')::numeric, 0)), 0),
				       COALESCE(SUM(bs.qty * COALESCE(NULLIF(i.data->>'volume', '')::numeric, 0)), 0)
				FROM %s.bin_stock bs
				LEFT JOIN %s.documents i ON i.doctype = 'Item' AND i.data->>'code' = bs.sku
				WHERE bs.bin_code = $1`, schema, schema), b.BinCode).Scan(&curQty, &curWeight, &curVolume); err != nil {
				return nil, err
			}
			var itemWeight, itemVolume float64
			_ = db.DB.QueryRow(fmt.Sprintf(`
				SELECT COALESCE(NULLIF(data->>'weight', '')::numeric, 0), COALESCE(NULLIF(data->>'volume', '')::numeric, 0)
				FROM %s.documents WHERE doctype = 'Item' AND data->>'code' = $1`, schema), sku).Scan(&itemWeight, &itemVolume)

			if b.Capacity > 0 && curQty+float64(qty) > b.Capacity {
				continue
			}
			if b.MaxWeight > 0 && curWeight+float64(qty)*itemWeight > b.MaxWeight {
				continue
			}
			if b.MaxVolume > 0 && curVolume+float64(qty)*itemVolume > b.MaxVolume {
				continue
			}
			if wantHeadroom && b.Capacity > 0 {
				c.HeadroomQty = b.Capacity - curQty - float64(qty)
				c.HasHeadroom = true
			}
		}

		var existingQty int
		_ = db.DB.QueryRow(fmt.Sprintf(
			`SELECT COALESCE(SUM(qty), 0) FROM %s.bin_stock_batch WHERE bin_code = $1 AND sku = $2 AND condition = 'Good'`, schema),
			b.BinCode, sku).Scan(&existingQty)
		c.ExistingBatchQty = existingQty

		out = append(out, c)
	}
	return out, nil
}

// filterHazmatTemp keeps only bins whose zone is compatible with the item's
// hazmat_class/temperature_class (both Stage 42.2.7 additions on Item, both
// blank on every item before this Stage - a blank on either side is treated
// as "no restriction," never as a mismatch, so this criterion is a no-op
// until a tenant actually classifies items and zones).
func filterHazmatTemp(tenantID, schema, sku string, candidates []putawayBinCandidate) ([]putawayBinCandidate, error) {
	var hazmatClass, tempClass string
	err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT COALESCE(data->>'hazmat_class', ''), COALESCE(data->>'temperature_class', '') FROM %s.documents WHERE doctype = 'Item' AND data->>'code' = $1`, schema),
		sku).Scan(&hazmatClass, &tempClass)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	if hazmatClass == "" && tempClass == "" {
		return candidates, nil
	}

	zoneAttrs := map[string][2]string{} // zone code -> [hazmat_allowed, temperature_class]
	rows, err := db.DB.Query(fmt.Sprintf(
		`SELECT data->>'code', COALESCE(data->>'hazmat_allowed', ''), COALESCE(data->>'temperature_class', '') FROM %s.documents WHERE doctype = 'Zone' AND status = 'Active'`, schema))
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var code, hazAllowed, temp string
		if err := rows.Scan(&code, &hazAllowed, &temp); err != nil {
			rows.Close()
			return nil, err
		}
		zoneAttrs[code] = [2]string{hazAllowed, temp}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var out []putawayBinCandidate
	for _, c := range candidates {
		attrs, known := zoneAttrs[c.ZoneCode]
		if !known {
			// A bin with no zone (or a zone with no attributes set yet) has
			// nothing to disqualify it - unrestricted, not incompatible.
			out = append(out, c)
			continue
		}
		if hazmatClass != "" && attrs[0] == "No" {
			continue
		}
		if tempClass != "" && attrs[1] != "" && attrs[1] != tempClass {
			continue
		}
		out = append(out, c)
	}
	return out, nil
}

// itemVelocityTier reuses 26.5.9's own GetABCCycleCountPlan directly (same
// precedent GetSlottingSuggestions already set) rather than a second
// tiering pass - returns "" (neutral) if the SKU isn't found, which simply
// drops velocity out of the sort with no preferred bin_type.
func itemVelocityTier(tenantID, locationCode, sku string) string {
	tiers, err := GetABCCycleCountPlan(tenantID, locationCode, 0, 0, 0)
	if err != nil {
		return ""
	}
	for _, t := range tiers {
		if t.Sku == sku {
			return t.Tier
		}
	}
	return ""
}

// sortPutawayCandidates orders candidates by whichever criteria are enabled,
// in the fixed priority velocity > zone_sequence > capacity > bin_code
// (deterministic tie-break, always applied last). "capacity" prefers the
// bin with the LEAST remaining headroom that still fits - the same
// fill-existing-space-before-opening-a-new-one instinct
// SuggestCartonization's first-fit packer already uses (wms_pack_count.go).
func sortPutawayCandidates(candidates []putawayBinCandidate, criteria map[string]bool, preferredBinType string) {
	less := func(a, b putawayBinCandidate) bool {
		if criteria["velocity"] && preferredBinType != "" {
			am, bm := a.BinType == preferredBinType, b.BinType == preferredBinType
			if am != bm {
				return am
			}
		}
		if criteria["zone_sequence"] && a.PutawaySeq != b.PutawaySeq {
			return a.PutawaySeq < b.PutawaySeq
		}
		if criteria["capacity"] && a.HasHeadroom && b.HasHeadroom && a.HeadroomQty != b.HeadroomQty {
			return a.HeadroomQty < b.HeadroomQty
		}
		return a.BinCode < b.BinCode
	}
	for i := 1; i < len(candidates); i++ {
		for j := i; j > 0 && less(candidates[j], candidates[j-1]); j-- {
			candidates[j], candidates[j-1] = candidates[j-1], candidates[j]
		}
	}
}
