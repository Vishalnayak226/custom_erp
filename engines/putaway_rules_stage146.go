package engines

import (
	"database/sql"
	"fmt"
	"strings"

	"custom_erp/db"
)

// Stage 146.3.7: put-away rules by item, design or category. A PutawayRule
// names where a kind of stock goes (a zone or a bin). The most specific rule
// wins - item, then design (Item.family), then category (Item.category), then
// the location default (no item / design / category) - and an exact location
// beats a rule for every location. The PackTemplate resolution is the model.
// No rules means the suggestion is exactly what it was. Home bins keep first
// place; a rule is tried before the directed strategy.

// putawayRuleRow is one Active rule.
type putawayRuleRow struct {
	ID, Item, Design, Category, Location, Zone, Bin string
	rank                                            int
}

// putawayRulesFor lists the rules that apply to sku at locationCode, most
// specific first. Rules that name a different item, design, category or
// location are left out.
func putawayRulesFor(schema, sku, locationCode string) ([]putawayRuleRow, error) {
	var design, category string
	_ = db.DB.QueryRow(fmt.Sprintf(`
		SELECT COALESCE(data->>'family', ''), COALESCE(data->>'category', '')
		FROM %s.documents WHERE doctype = 'Item' AND deleted_at IS NULL AND data->>'code' = $1 LIMIT 1`, schema), sku).Scan(&design, &category)
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, COALESCE(data->>'item', ''), COALESCE(data->>'design', ''), COALESCE(data->>'category', ''),
		       COALESCE(data->>'location_code', ''), COALESCE(data->>'zone', ''), COALESCE(data->>'bin_code', '')
		FROM %s.documents
		WHERE doctype = 'PutawayRule' AND status = 'Active' AND deleted_at IS NULL
		  AND COALESCE(data->>'location_code', '') IN ('', $1)`, schema), locationCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []putawayRuleRow
	for rows.Next() {
		var r putawayRuleRow
		if err := rows.Scan(&r.ID, &r.Item, &r.Design, &r.Category, &r.Location, &r.Zone, &r.Bin); err != nil {
			return nil, err
		}
		switch {
		case r.Item != "":
			if r.Item != sku {
				continue
			}
			r.rank = 0
		case r.Design != "":
			if design == "" || r.Design != design {
				continue
			}
			r.rank = 2
		case r.Category != "":
			if category == "" || !strings.EqualFold(r.Category, category) {
				continue
			}
			r.rank = 4
		default:
			r.rank = 6
		}
		if r.Location == "" {
			r.rank++ // an exact location beats every location
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && (out[j].rank < out[j-1].rank || (out[j].rank == out[j-1].rank && out[j].ID < out[j-1].ID)); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out, nil
}

func (r putawayRuleRow) label() string {
	switch {
	case r.Item != "":
		return "item " + r.Item
	case r.Design != "":
		return "design " + r.Design
	case r.Category != "":
		return "category " + r.Category
	}
	return "location default"
}

// ruleBinUsable reports whether a rule's bin can take qty of sku now.
func ruleBinUsable(tx *sql.Tx, schema, binCode, sku, locationCode string, qty int, batchNo string) bool {
	var loc, status, opState, zone string
	var maxQty, maxWeight, maxVolume float64
	err := tx.QueryRow(fmt.Sprintf(`
		SELECT COALESCE(data->>'location', ''), COALESCE(status, ''), COALESCE(data->>'bin_status', ''), COALESCE(data->>'zone', ''),
		       COALESCE(NULLIF(data->>'capacity', '')::numeric, 0), COALESCE(NULLIF(data->>'max_weight', '')::numeric, 0),
		       COALESCE(NULLIF(data->>'max_volume', '')::numeric, 0)
		FROM %s.documents WHERE doctype = 'Bin' AND deleted_at IS NULL AND data->>'bin_code' = $1 LIMIT 1`, schema), binCode).
		Scan(&loc, &status, &opState, &zone, &maxQty, &maxWeight, &maxVolume)
	if err != nil || status != "Active" || (loc != "" && locationCode != "" && loc != locationCode) {
		return false
	}
	if opState == "Blocked" || opState == "Full" || opState == "Counting" {
		return false
	}
	if checkHazmatCompatibility(tx, schema, sku, zone, binCode) != nil {
		return false
	}
	if enforceBinCapacity(tx, schema, binCode, sku, qty, maxQty, maxWeight, maxVolume) != nil {
		return false
	}
	return enforceBinMixRule(tx, schema, binCode, sku, batchNo) == nil
}

type ruleBin struct{ bin, note string }

// suggestRuleBin is the put-away suggestion from the rules: the first rule,
// most specific first, whose bin (or a bin of whose zone) can take the stock.
// ("", "", nil) when there are no rules; ("", note, nil) when none can be used.
func suggestRuleBin(tenantID, sku, locationCode string, qty int, batchNo string) (bin, note string, err error) {
	bins, note, err := ruleBins(tenantID, sku, locationCode, qty, batchNo, 1)
	if err != nil || len(bins) == 0 {
		return "", note, err
	}
	return bins[0].bin, bins[0].note, nil
}

// ruleBins lists up to limit usable bins from the rules in order (0 = all),
// and, when there are none, a note on why the rules could not be used.
func ruleBins(tenantID, sku, locationCode string, qty int, batchNo string, limit int) ([]ruleBin, string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, "", err
	}
	rules, err := putawayRulesFor(schema, sku, locationCode)
	if err != nil || len(rules) == 0 {
		return nil, "", err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, "", err
	}
	defer tx.Rollback()
	if err := db.SetSearchPath(tx, schema); err != nil {
		return nil, "", err
	}
	var out []ruleBin
	seen := map[string]bool{}
	var skipped []string
	for _, r := range rules {
		if r.Bin != "" {
			if seen[r.Bin] {
				continue
			}
			if ruleBinUsable(tx, schema, r.Bin, sku, locationCode, qty, batchNo) {
				seen[r.Bin] = true
				out = append(out, ruleBin{r.Bin, fmt.Sprintf("put-away rule for %s: bin %s", r.label(), r.Bin)})
			} else {
				skipped = append(skipped, fmt.Sprintf("%s (%s)", r.Bin, r.label()))
			}
		} else if r.Zone != "" {
			cands, cerr := loadPutawayCandidates(tenantID, schema, locationCode, sku, qty, true)
			if cerr != nil {
				return nil, "", cerr
			}
			var inZone []putawayBinCandidate
			for _, c := range cands {
				if c.ZoneCode == r.Zone && !seen[c.BinCode] {
					inZone = append(inZone, c)
				}
			}
			inZone = dropMixRefusedBins(schema, sku, batchNo, inZone)
			sortPutawayCandidates(inZone, map[string]bool{"capacity": true}, "")
			for _, c := range inZone {
				seen[c.BinCode] = true
				out = append(out, ruleBin{c.BinCode, fmt.Sprintf("put-away rule for %s: zone %s", r.label(), r.Zone)})
			}
			if len(inZone) == 0 {
				skipped = append(skipped, fmt.Sprintf("zone %s (%s)", r.Zone, r.label()))
			}
		}
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	if len(out) == 0 && len(skipped) > 0 {
		return nil, "no put-away rule can take it - " + strings.Join(skipped, ", "), nil
	}
	return out, "", nil
}

// validatePutawayRuleMasterRules: a rule names at most one of item, design
// and category, and a zone or a bin to send the stock to; what it names must
// exist.
func validatePutawayRuleMasterRules(tenantID, docID string, payload map[string]interface{}) error {
	bad := func(format string, a ...interface{}) error {
		return &ValidationError{Code: "WMS-1314", SubFor: "PutawayRule", Message: fmt.Sprintf(format, a...)}
	}
	item, design, category := strField(payload, "item"), strField(payload, "design"), strField(payload, "category")
	n := 0
	for _, v := range []string{item, design, category} {
		if v != "" {
			n++
		}
	}
	if n > 1 {
		return bad("a put-away rule is for an item, a design or a category - not more than one")
	}
	zone, bin := strField(payload, "zone"), strField(payload, "bin_code")
	if zone == "" && bin == "" {
		return bad("say which zone or bin the stock goes to")
	}
	if strField(payload, "status") != "Active" {
		return nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil
	}
	exists := func(doctype, field, v string) bool {
		var c int
		_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.documents WHERE doctype = $1 AND deleted_at IS NULL AND data->>$2 = $3`, schema), doctype, field, v).Scan(&c)
		return c > 0
	}
	if bin != "" && !exists("Bin", "bin_code", bin) {
		return bad("bin %s does not exist", bin)
	}
	if zone != "" && !exists("Zone", "code", zone) {
		return bad("zone %s does not exist", zone)
	}
	if item != "" && !exists("Item", "code", item) {
		return bad("item %s does not exist", item)
	}
	return nil
}
