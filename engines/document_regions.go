package engines

import (
	"custom_erp/db"
	"database/sql"
	"fmt"
	"strings"
)

// State fields on documents (Stage 76.2).
//
// Runs inside validateDocumentWith - the one choke point every save, CSV
// import and API write passes through - BEFORE the mandatory check, so a
// Vendor whose GSTIN names its state satisfies a mandatory State without the
// user picking it twice.
//
// For every state field (a Data field named "state" or "<x>_state"):
//   - the value is stored as the region's canonical name, whatever spelling,
//     short code or tax code was typed or imported ("ka", "29" -> Karnataka);
//   - a blank plain "state" is filled from the same document's GSTIN, and a
//     state that contradicts the GSTIN is refused - the GSTIN is what the tax
//     authority registered, so it is the one that is right;
//   - a "<x>state_code" companion, where the doctype declares one, gets the
//     region's short code (KA) - set by the server, never typed;
//   - a Location with no state takes its Legal Entity's.
//
// A master (Vendor, Location, Legal Entity...) refuses a state that is not on
// the country's list. A transaction keeps one it cannot resolve: a channel
// order from abroad, or with a misspelt state, must never be lost over an
// address field - the same rule the phone engine follows (Stage 41).

// isStateField reports whether a field holds a state name. Select fields
// called "*_state" (POSCart.payment_state) are workflow states, not places.
func isStateField(f FieldMeta) bool {
	if f.Fieldtype != "Data" {
		return false
	}
	n := strings.ToLower(strings.TrimSpace(f.Fieldname))
	return n == "state" || strings.HasSuffix(n, "_state")
}

// stateCodeField is the companion that carries a state field's short code.
func stateCodeField(stateField string) string { return stateField + "_code" }

// documentCountry is the country a document's address is in: its own
// country field when it carries one the product knows, otherwise the
// tenant's home country.
func documentCountry(home string, docData map[string]interface{}) string {
	for _, key := range []string{"country", "customer_country", "shipping_country"} {
		if v := strings.ToUpper(strings.TrimSpace(strField(docData, key))); len(v) == 2 {
			if _, known := phoneRulesByISO2[v]; known {
				return v
			}
		}
	}
	return home
}

// applyDocumentRegions is described at the top of this file. country is the
// tenant's home country; strict is true for a master.
func applyDocumentRegions(tenantID, doctype, home string, strict bool, fields []FieldMeta, docData map[string]interface{}) error {
	if len(docData) == 0 {
		return nil
	}
	declared := map[string]FieldMeta{}
	for _, f := range fields {
		declared[f.Fieldname] = f
	}
	country := documentCountry(home, docData)

	for _, f := range fields {
		if !isStateField(f) {
			continue
		}
		raw := strings.TrimSpace(strField(docData, f.Fieldname))

		// A Location sits in its Legal Entity's state: the entity's GST
		// registration is per state, so a store under it cannot be elsewhere.
		if raw == "" && doctype == "Location" && f.Fieldname == "state" {
			raw = legalEntityState(tenantID, strField(docData, "legal_entity"))
		}

		// The GSTIN's first two digits are the state it is registered in.
		var fromTaxID Region
		var haveTaxID bool
		if f.Fieldname == "state" && country == "IN" {
			if _, hasGSTIN := declared["gstin"]; hasGSTIN {
				g := strings.ToUpper(strings.TrimSpace(strField(docData, "gstin")))
				if gstinPattern.MatchString(g) && ValidGSTINChecksum(g) {
					fromTaxID, haveTaxID = RegionFromTaxCode("IN", g[:2])
				}
			}
		}

		if raw == "" && haveTaxID {
			raw = fromTaxID.Name
		}
		if raw == "" {
			if _, ok := declared[stateCodeField(f.Fieldname)]; ok {
				docData[stateCodeField(f.Fieldname)] = ""
			}
			continue
		}

		region, ok := ResolveRegion(country, raw)
		if !ok {
			if strict && HasRegionList(country) {
				return &ValidationError{SubFor: f.Label, Message: fmt.Sprintf(
					"%s %q is not a state of %s. Choose one from the list.", f.Label, raw, countryDisplayName(country))}
			}
			docData[f.Fieldname] = raw
			continue
		}
		if haveTaxID && region.TaxCode != fromTaxID.TaxCode {
			return &ValidationError{SubFor: f.Label, Message: fmt.Sprintf(
				"%s is %s, but GSTIN %s is registered in %s (code %s). Choose %s, or check the GSTIN.",
				f.Label, region.Name, strings.ToUpper(strField(docData, "gstin")), fromTaxID.Name, fromTaxID.TaxCode, fromTaxID.Name)}
		}
		docData[f.Fieldname] = region.Name
		if _, ok := declared[stateCodeField(f.Fieldname)]; ok {
			docData[stateCodeField(f.Fieldname)] = region.Short
		}
	}
	return nil
}

// legalEntityState reads a Legal Entity's state, or its GSTIN's state when
// the state itself was never filled in.
func legalEntityState(tenantID, entity string) string {
	entity = strings.TrimSpace(entity)
	if entity == "" {
		return ""
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return ""
	}
	var state, gstin sql.NullString
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT data->>'state', data->>'gstin' FROM %s.documents
		WHERE doctype = 'LegalEntity' AND deleted_at IS NULL AND (id = $1 OR data->>'code' = $1)
		ORDER BY CASE WHEN id = $1 THEN 0 ELSE 1 END LIMIT 1`, schema), entity).Scan(&state, &gstin); err != nil {
		return ""
	}
	if s := strings.TrimSpace(state.String); s != "" {
		return s
	}
	if code := StateCodeFromGSTIN(gstin.String); code != "" {
		return indianStateNames[code]
	}
	return ""
}

// countryDisplayName names a country for a message ("India"), falling back
// to its code.
func countryDisplayName(iso2 string) string {
	if r, ok := phoneRulesByISO2[strings.ToUpper(iso2)]; ok && r.Name != "" {
		return r.Name
	}
	return iso2
}

// addressLines is a record's Address Line 1 and Line 2 as one printable line
// (Stage 76.3). Every print block reads an address through here, so a second
// line is never silently left off a PO, challan or receipt.
func addressLines(doc map[string]interface{}) string {
	parts := make([]string, 0, 2)
	for _, key := range []string{"address", "address_line2"} {
		if v := strings.TrimSpace(strField(doc, key)); v != "" {
			parts = append(parts, v)
		}
	}
	return strings.Join(parts, ", ")
}
