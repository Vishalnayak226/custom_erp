package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"strings"
)

// Stage 146.2.2 - a dated Loyalty Scheme: spend per point for a set of stores
// over a date window. A sale earns under its own store's scheme on the bill
// date; with no scheme it earns on the tenant settings, exactly as before.

type loyaltyScheme struct {
	ID             string
	Code           string
	Name           string
	ValidFrom      string
	ValidTo        string
	Stores         string
	RupeesPerPoint int
}

func loadLoyaltySchemes(schema string) []loyaltyScheme {
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, data FROM %s.documents WHERE doctype = 'LoyaltyScheme' AND status = 'Active' AND deleted_at IS NULL`, schema))
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []loyaltyScheme
	for rows.Next() {
		var id, raw string
		if rows.Scan(&id, &raw) != nil {
			continue
		}
		var d map[string]interface{}
		if json.Unmarshal([]byte(raw), &d) != nil {
			continue
		}
		out = append(out, loyaltyScheme{ID: id, Code: strField(d, "code"), Name: strField(d, "name"),
			ValidFrom: strField(d, "valid_from"), ValidTo: strField(d, "valid_to"),
			Stores: strings.TrimSpace(strField(d, "stores")), RupeesPerPoint: int(numFromInterface(d["rupees_per_point"]))})
	}
	return out
}

func (s loyaltyScheme) liveOn(date string) bool {
	return (s.ValidFrom == "" || date >= s.ValidFrom) && (s.ValidTo == "" || date <= s.ValidTo)
}

// loyaltySchemeFor is the scheme a sale at store on date earns under, or nil.
// A scheme naming the store beats an all-stores one; then the latest start.
func loyaltySchemeFor(schema, store, date string) *loyaltyScheme {
	var best *loyaltyScheme
	for _, s := range loadLoyaltySchemes(schema) {
		if s.RupeesPerPoint < 1 || !s.liveOn(date) || !storeListed(s.Stores, store) {
			continue
		}
		s := s
		if best == nil || (s.Stores != "" && best.Stores == "") ||
			((s.Stores != "") == (best.Stores != "") && s.ValidFrom > best.ValidFrom) {
			best = &s
		}
	}
	return best
}

func loyaltyStoresOverlap(a, b string) bool {
	if strings.TrimSpace(a) == "" || strings.TrimSpace(b) == "" {
		return true
	}
	for _, x := range strings.Split(a, ",") {
		if storeListed(b, strings.TrimSpace(x)) {
			return true
		}
	}
	return false
}

// validateLoyaltySchemeMasterRules refuses a bad window and two schemes that
// overlap for a store (dates x stores).
func validateLoyaltySchemeMasterRules(tenantID, docID string, payload map[string]interface{}) error {
	from, to := strField(payload, "valid_from"), strField(payload, "valid_to")
	if numFromInterface(payload["rupees_per_point"]) < 1 {
		return &ValidationError{Code: "CUSTOM-0692", SubFor: "rupees_per_point", Message: "spend per point must be at least 1"}
	}
	if from != "" && to != "" && to < from {
		return &ValidationError{Code: "CUSTOM-0692", SubFor: "valid_to", Message: "the scheme ends before it starts"}
	}
	if st := strField(payload, "status"); st != "" && !strings.EqualFold(st, "Active") {
		return nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil
	}
	stores := strings.TrimSpace(strField(payload, "stores"))
	for _, o := range loadLoyaltySchemes(schema) {
		if o.ID == docID || !loyaltyStoresOverlap(stores, o.Stores) {
			continue
		}
		if (to == "" || o.ValidFrom == "" || o.ValidFrom <= to) && (o.ValidTo == "" || from == "" || from <= o.ValidTo) {
			return &ValidationError{Code: "CUSTOM-0690", Message: fmt.Sprintf("scheme %s already covers these stores on these dates", o.Code)}
		}
	}
	return nil
}
