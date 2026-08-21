package engines

import (
	"custom_erp/db"
	"database/sql"
	"fmt"
	"strings"
)

// Stage 42.1.10 - UOM conversion (Infor §15).
//
// Before this file there was no unit-of-measure concept anywhere in the tree:
// every qty in engines/db is implicitly "one each." UOMConversion closes that
// as a direct (item, from_uom, to_uom, factor) edge - "1 from_uom of this
// item equals `factor` to_uom" - looked up (and inverted) by ConvertUOMQty,
// the one choke point every consumer wired up this phase goes through
// (cartonization, pick UoM display, 3PL billing units).
//
// Deliberately no multi-hop conversion graph (EA -> CASE -> PALLET chained
// through an intermediate unit): every consumer this phase has only ever
// needs a single direct or inverse edge, and a graph walk over rows nobody
// has entered yet is exactly the premature generality this repo's guidelines
// warn against. If a real chained conversion is ever needed, that is a
// deliberate later addition to this one function, not a guess made here.

// ConvertUOMQty converts qty from fromUOM to toUOM for one item's own
// UOMConversion rows. Equal (or either blank) UOMs are a no-op - this is what
// keeps every existing caller that has never heard of a UOM unaffected: a
// blank fromUOM/toUOM is exactly what "the qty is already in whatever unit
// the caller means" looks like.
//
// Tries the direct edge (item, fromUOM, toUOM) first, then the inverse edge
// (item, toUOM, fromUOM) with the factor divided out, so a tenant only has to
// enter "1 CASE = 12 EA" once and both directions work.
func ConvertUOMQty(tenantID, sku string, qty float64, fromUOM, toUOM string) (float64, error) {
	fromUOM, toUOM = strings.TrimSpace(fromUOM), strings.TrimSpace(toUOM)
	if fromUOM == "" || toUOM == "" || strings.EqualFold(fromUOM, toUOM) {
		return qty, nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, err
	}

	var factor float64
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT (data->>'factor')::numeric FROM %s.documents
		WHERE doctype = 'UOMConversion' AND COALESCE(status, '') = 'Active'
		  AND data->>'item' = $1 AND data->>'from_uom' = $2 AND data->>'to_uom' = $3
		LIMIT 1`, schema), sku, fromUOM, toUOM).Scan(&factor)
	if err == nil {
		return qty * factor, nil
	} else if err != sql.ErrNoRows {
		return 0, err
	}

	// No direct edge - try the inverse.
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT (data->>'factor')::numeric FROM %s.documents
		WHERE doctype = 'UOMConversion' AND COALESCE(status, '') = 'Active'
		  AND data->>'item' = $1 AND data->>'from_uom' = $2 AND data->>'to_uom' = $3
		LIMIT 1`, schema), sku, toUOM, fromUOM).Scan(&factor)
	if err == sql.ErrNoRows {
		return 0, &ValidationError{Code: "GLOBAL-0002", SubFor: "UOM",
			Message: fmt.Sprintf("no conversion is defined between %s and %s for item %s", fromUOM, toUOM, sku)}
	} else if err != nil {
		return 0, err
	}
	if factor == 0 {
		return 0, &ValidationError{Code: "GLOBAL-0002", SubFor: "UOM",
			Message: fmt.Sprintf("the %s -> %s conversion for item %s has a zero factor", toUOM, fromUOM, sku)}
	}
	return qty / factor, nil
}

// uomExists reports whether an Active UOM with this code is registered -
// the existence check UOMConversion's from_uom/to_uom run against, the same
// role Batch's data->>'code' check plays for Item.
func uomExists(tenantID, code string) (bool, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return false, err
	}
	var exists bool
	err = db.DB.QueryRow(fmt.Sprintf(
		`SELECT EXISTS(SELECT 1 FROM %s.documents WHERE doctype = 'UOM' AND data->>'code' = $1 AND COALESCE(status, '') = 'Active')`, schema),
		code).Scan(&exists)
	return exists, err
}

// validateUOMConversionMasterRules (Stage 42.1.10) enforces what UOMConversion's
// generic metadata pass cannot express: from_uom and to_uom must both be real,
// Active UOM codes, a UOM cannot convert to itself, the factor must be
// positive, and (item, from_uom, to_uom) is unique - the same "natural key
// isn't PK-friendly, so check it here" shape every other constraint master in
// this Stage uses.
func validateUOMConversionMasterRules(tenantID, docID string, payload map[string]interface{}) error {
	item := strField(payload, "item")
	fromUOM := strField(payload, "from_uom")
	toUOM := strField(payload, "to_uom")
	if item == "" || fromUOM == "" || toUOM == "" {
		// All three are mandatory; ValidateDocument has already said so.
		return nil
	}

	if strings.EqualFold(fromUOM, toUOM) {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "To UOM",
			Message: fmt.Sprintf("From UOM and To UOM cannot both be %q - a unit does not need a conversion to itself", fromUOM)}
	}
	if numFromInterface(payload["factor"]) <= 0 {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "Factor",
			Message: "factor must be a positive number"}
	}

	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var itemExists bool
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT EXISTS(SELECT 1 FROM %s.documents WHERE doctype = 'Item' AND data->>'code' = $1 AND deleted_at IS NULL)`, schema),
		item).Scan(&itemExists); err != nil {
		return err
	}
	if !itemExists {
		return &ValidationError{Code: "META-0198", SubFor: "Item Code (SKU)",
			Message: fmt.Sprintf("no item with code %q exists - a UOM conversion must belong to a real item", item)}
	}
	for _, uom := range []struct{ label, code string }{{"From UOM", fromUOM}, {"To UOM", toUOM}} {
		ok, err := uomExists(tenantID, uom.code)
		if err != nil {
			return err
		}
		if !ok {
			return &ValidationError{Code: "META-0198", SubFor: uom.label,
				Message: fmt.Sprintf("no Active UOM %q is registered", uom.code)}
		}
	}

	var existingID string
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT id FROM %s.documents
		WHERE doctype = 'UOMConversion' AND data->>'item' = $1 AND data->>'from_uom' = $2 AND data->>'to_uom' = $3 AND id != $4
		LIMIT 1`, schema), item, fromUOM, toUOM, docID).Scan(&existingID)
	if err == nil {
		return &ValidationError{Code: "MASTER-0053", Message: fmt.Sprintf(
			"a conversion from %s to %s already exists for item %s (%s)", fromUOM, toUOM, item, existingID)}
	}
	return nil
}
