package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Stage 114.6 (user, 2026-10-19: "I need a mrp logic as marigin cal ... One
// with wright wise pp wise mrp. only pp range wise mrp with a multillier
// single multiplier etc."; decision: a suggestion at the goods receipt's MRP
// step, set per Product type with one default).
//
// An MRPRule record is one way to work the suggested MRP out of the landed
// purchase price (the GRN price incl. GST), rounded up to the tenant's MRP
// price ending like every suggestion:
//
//	Multiplier               price x multiplier
//	Purchase price bands     price x the multiplier of the band it falls in
//	Weight + purchase price  weight (g) x rate per gram + price x multiplier
//
// The item's Product Type names the rule (mrp_rule); else the Active rule
// ticked Default; else the markup the MRP step always used. See
// db/migrations_stage114_6_mrp_rules.sql.

const (
	MRPMethodMultiplier = "Multiplier"
	MRPMethodBands      = "Purchase price bands"
	MRPMethodWeight     = "Weight + purchase price"
)

// MRPRule is one MRPRule record, read for the suggestion.
type MRPRule struct {
	ID          string
	Name        string
	Method      string
	Multiplier  float64
	RatePerGram float64
	Bands       []MRPBand
}

// MRPBand: a price up to UpTo is multiplied by Multiplier; UpTo 0 is the
// open band above the last amount.
type MRPBand struct {
	UpTo       float64
	Multiplier float64
}

var mrpBandPattern = regexp.MustCompile(`^(?i)(above|over|\*|[0-9]+(?:\.[0-9]+)?)\s*[=:]\s*([0-9]+(?:\.[0-9]+)?)$`)

// ParseMRPBands reads "500=3, 2000=2.5, above=2" - pairs split by commas,
// semicolons or new lines, "=" or ":" between the amount and its
// multiplier. A price above the last amount takes the "above" band, or the
// last band's multiplier when there is none.
func ParseMRPBands(text string) ([]MRPBand, error) {
	var out []MRPBand
	open := false
	for _, part := range strings.FieldsFunc(text, func(r rune) bool { return r == ',' || r == ';' || r == '\n' }) {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		m := mrpBandPattern.FindStringSubmatch(part)
		if m == nil {
			return nil, fmt.Errorf("%q is not a band - write each as amount=multiplier, e.g. 500=3, and the last as above=2", part)
		}
		mult, _ := strconv.ParseFloat(m[2], 64)
		if mult <= 0 {
			return nil, fmt.Errorf("the multiplier in %q must be more than 0", part)
		}
		if _, err := strconv.ParseFloat(m[1], 64); err != nil {
			if open {
				return nil, fmt.Errorf("only one \"above\" band")
			}
			open = true
			out = append(out, MRPBand{Multiplier: mult})
			continue
		}
		upTo, _ := strconv.ParseFloat(m[1], 64)
		if upTo <= 0 {
			return nil, fmt.Errorf("the amount in %q must be more than 0", part)
		}
		out = append(out, MRPBand{UpTo: upTo, Multiplier: mult})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("enter at least one band, e.g. 500=3, 2000=2.5, above=2")
	}
	// Bounded bands in ascending order, the open one last.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].UpTo == 0 || out[j].UpTo == 0 {
			return out[j].UpTo == 0 && out[i].UpTo != 0
		}
		return out[i].UpTo < out[j].UpTo
	})
	for i := 1; i < len(out); i++ {
		if out[i].UpTo != 0 && out[i].UpTo == out[i-1].UpTo {
			return nil, fmt.Errorf("two bands end at %v", out[i].UpTo)
		}
	}
	return out, nil
}

// Apply is the rule's MRP, before rounding, for a landed price incl. GST and
// a weight in grams (0 when unknown).
func (r MRPRule) Apply(price, weight float64) float64 {
	if price <= 0 {
		return 0
	}
	switch r.Method {
	case MRPMethodBands:
		for _, b := range r.Bands {
			if b.UpTo == 0 || price <= b.UpTo {
				return price * b.Multiplier
			}
		}
		if n := len(r.Bands); n > 0 {
			return price * r.Bands[n-1].Multiplier
		}
		return 0
	case MRPMethodWeight:
		return math.Max(weight, 0)*r.RatePerGram + price*r.Multiplier
	default:
		return price * r.Multiplier
	}
}

// mrpRuleFromData reads an MRPRule record; an unusable one is an error, which
// is also what refuses it on save (ValidateMRPRule).
func mrpRuleFromData(id string, data map[string]interface{}) (*MRPRule, error) {
	r := &MRPRule{ID: id, Name: strField(data, "name"), Method: strings.TrimSpace(strField(data, "method")),
		Multiplier: numFromInterface(data["multiplier"]), RatePerGram: numFromInterface(data["rate_per_gram"])}
	if r.Name == "" {
		r.Name = id
	}
	switch r.Method {
	case MRPMethodMultiplier:
		if r.Multiplier <= 0 {
			return nil, fmt.Errorf("a Multiplier rule needs a multiplier more than 0, e.g. 3")
		}
	case MRPMethodBands:
		bands, err := ParseMRPBands(strField(data, "bands"))
		if err != nil {
			return nil, err
		}
		r.Bands = bands
	case MRPMethodWeight:
		if r.RatePerGram <= 0 {
			return nil, fmt.Errorf("a Weight + purchase price rule needs a rate per gram more than 0")
		}
		if r.Multiplier < 0 {
			return nil, fmt.Errorf("the multiplier cannot be below 0")
		}
	default:
		return nil, fmt.Errorf("choose the method: %s, %s or %s", MRPMethodMultiplier, MRPMethodBands, MRPMethodWeight)
	}
	return r, nil
}

// ValidateMRPRule refuses a rule that could not suggest anything
// (ValidateDocument, for every MRPRule save).
func ValidateMRPRule(data map[string]interface{}) error {
	if _, err := mrpRuleFromData("", data); err != nil {
		return &ValidationError{Code: "MASTER-0896", Message: err.Error()}
	}
	return nil
}

// mrpRuleForItem is the rule that suggests this item's MRP: its Product
// Type's, else the default rule; nil when neither is set (or usable).
func mrpRuleForItem(tenantID string, item map[string]interface{}) *MRPRule {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil
	}
	active := `deleted_at IS NULL AND COALESCE(NULLIF(data->>'status', ''), status, 'Active') = 'Active'`
	read := func(query string, args ...interface{}) *MRPRule {
		var id, raw string
		if db.DB.QueryRow(query, args...).Scan(&id, &raw) != nil {
			return nil
		}
		data := map[string]interface{}{}
		if json.Unmarshal([]byte(raw), &data) != nil {
			return nil
		}
		r, err := mrpRuleFromData(id, data)
		if err != nil {
			return nil
		}
		return r
	}
	if pt := strings.TrimSpace(strField(item, "product_type")); pt != "" {
		var ref string
		_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(data->>'mrp_rule', '') FROM %s.documents
			WHERE doctype = 'ProductType' AND deleted_at IS NULL AND (id = $1 OR data->>'code' = $1 OR lower(data->>'name') = lower($1))
			ORDER BY (id = $1) DESC LIMIT 1`, schema), pt).Scan(&ref)
		if ref = strings.TrimSpace(ref); ref != "" {
			if r := read(fmt.Sprintf(`SELECT id, data::text FROM %s.documents
				WHERE doctype = 'MRPRule' AND %s AND (id = $1 OR data->>'code' = $1) LIMIT 1`, schema, active), ref); r != nil {
				return r
			}
		}
	}
	return read(fmt.Sprintf(`SELECT id, data::text FROM %s.documents
		WHERE doctype = 'MRPRule' AND %s AND lower(COALESCE(data->>'is_default', '')) IN ('true', '1', 'yes')
		ORDER BY id LIMIT 1`, schema, active))
}

// suggestedMRPFor is the MRP step's suggestion and the rule that made it
// ("" = the markup the step always used).
func suggestedMRPFor(tenantID string, item map[string]interface{}, cost, gstRate, weight, markup float64, ending string) (float64, string) {
	if cost <= 0 {
		return 0, ""
	}
	if r := mrpRuleForItem(tenantID, item); r != nil {
		return roundUpToEnding(r.Apply(cost*(1+gstRate/100), weight), ending), r.Name
	}
	return suggestedMRP(tenantID, item, cost, gstRate, markup, ending), ""
}

// grnLineWeight is the weight a line was received at (asked at the GRN), else
// the item's gross weight; 0 when neither is known.
func grnLineWeight(line, item map[string]interface{}) float64 {
	if vals, ok := line["item_values"].(map[string]interface{}); ok {
		for _, k := range []string{"gross_weight", "weight"} {
			if w := numFromInterface(vals[k]); w > 0 {
				return w
			}
		}
	}
	for _, k := range []string{"gross_weight", "weight"} {
		if w := numFromInterface(item[k]); w > 0 {
			return w
		}
	}
	return 0
}
