package engines

import (
	"custom_erp/db"
	"database/sql"
	"fmt"
	"strings"
)

// Place of supply derivation (Stage 40.1).
//
// Whether a purchase is inter-state (IGST) or intra-state (CGST+SGST) is not
// an opinion - it follows from where the buying entity is registered and
// where the vendor is. Until this file it was a checkbox on the PO screen
// that a maker ticked from memory, on a screen that already had both parties
// on it, which meant a wrong tick silently produced the wrong tax split and
// the wrong GSTR-2 bucket.
//
// The derivation runs at the same generic-document choke point everything
// else about a PO already passes through, so it applies to the bespoke PO
// screen, the generic record form, the PR->PO conversion and any API caller
// alike, with nothing wired per screen.
//
// State comes from the GSTIN's first two digits wherever a GSTIN exists -
// that is the authoritative state code on a registered party, and it cannot
// drift from a separately-typed state field. A `state` field is the fallback
// for an unregistered/composition vendor with no GSTIN.

// indianStateNames maps the GSTIN state code to its state/UT name. Used for
// display on the printed PO ("Maharashtra (27)"), never for the interstate
// decision itself - that only ever compares the two codes.
//
// Codes 97 (Other Territory) and 99 (Centre Jurisdiction) are the two
// non-geographic codes the GSTN issues; they are included so a GSTIN
// carrying one is recognised rather than treated as malformed.
var indianStateNames = map[string]string{
	"01": "Jammu & Kashmir", "02": "Himachal Pradesh", "03": "Punjab",
	"04": "Chandigarh", "05": "Uttarakhand", "06": "Haryana",
	"07": "Delhi", "08": "Rajasthan", "09": "Uttar Pradesh",
	"10": "Bihar", "11": "Sikkim", "12": "Arunachal Pradesh",
	"13": "Nagaland", "14": "Manipur", "15": "Mizoram",
	"16": "Tripura", "17": "Meghalaya", "18": "Assam",
	"19": "West Bengal", "20": "Jharkhand", "21": "Odisha",
	"22": "Chhattisgarh", "23": "Madhya Pradesh", "24": "Gujarat",
	"25": "Daman & Diu", "26": "Dadra & Nagar Haveli and Daman & Diu",
	"27": "Maharashtra", "28": "Andhra Pradesh (Old)", "29": "Karnataka",
	"30": "Goa", "31": "Lakshadweep", "32": "Kerala",
	"33": "Tamil Nadu", "34": "Puducherry", "35": "Andaman & Nicobar Islands",
	"36": "Telangana", "37": "Andhra Pradesh", "38": "Ladakh",
	"97": "Other Territory", "99": "Centre Jurisdiction",
}

// stateNameToCode is the reverse lookup, built once from indianStateNames so
// the two cannot drift. It resolves a hand-typed `state` field ("Maharashtra")
// on a vendor with no GSTIN. Matching is case-insensitive and ignores spaces
// and punctuation, so "Tamil Nadu", "tamilnadu" and "TAMIL-NADU" all resolve.
var stateNameToCode = func() map[string]string {
	out := make(map[string]string, len(indianStateNames))
	for code, name := range indianStateNames {
		out[normalizeStateName(name)] = code
	}
	// The two spellings in real use that the canonical names above do not
	// cover on their own.
	out["orissa"] = "21"
	out["pondicherry"] = "34"
	out["newdelhi"] = "07"
	return out
}()

func normalizeStateName(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// StateCodeFromGSTIN returns the two-digit state code a GSTIN encodes, or ""
// if the string is not shaped like a GSTIN or carries a code the GSTN does
// not issue. Deliberately tolerant of surrounding whitespace and lower case,
// since this reads stored master data rather than validating an input - the
// format gate for that is ValidateFieldFormats.
func StateCodeFromGSTIN(gstin string) string {
	g := strings.ToUpper(strings.TrimSpace(gstin))
	if len(g) < 2 {
		return ""
	}
	code := g[:2]
	if _, known := indianStateNames[code]; !known {
		return ""
	}
	return code
}

// StateCodeFromName resolves a hand-typed state name (or a bare two-digit
// code) to its GSTIN state code. Returns "" when nothing matches.
func StateCodeFromName(state string) string {
	s := strings.TrimSpace(state)
	if s == "" {
		return ""
	}
	if _, known := indianStateNames[s]; known {
		return s
	}
	return stateNameToCode[normalizeStateName(s)]
}

// StateLabel renders a state code for display: "Maharashtra (27)". An unknown
// or empty code renders as "Not set" so the PO screen and the printed PO both
// say something honest rather than showing a blank.
func StateLabel(code string) string {
	if code == "" {
		return "Not set"
	}
	if name, known := indianStateNames[code]; known {
		return fmt.Sprintf("%s (%s)", name, code)
	}
	return code
}

// PlaceOfSupply is the outcome of comparing the two parties' states.
//
// Derived is false when either side's state could not be established - an
// unregistered vendor with no state recorded, a Location with no Legal Entity
// linked, or a Legal Entity with neither GSTIN nor state. In that case
// Interstate is meaningless and the caller keeps whatever the human chose:
// guessing intra-state (the zero value) on missing data would quietly charge
// CGST+SGST on what may well be an IGST purchase.
type PlaceOfSupply struct {
	BuyerStateCode  string `json:"buyer_state_code"`
	VendorStateCode string `json:"vendor_state_code"`
	BuyerStateLabel string `json:"buyer_state_label"`
	VendorState     string `json:"vendor_state_label"`
	Interstate      bool   `json:"interstate"`
	Derived         bool   `json:"derived"`
	// Reason explains a Derived=false outcome in the words the PO screen
	// shows the maker, so the fix ("this vendor has no GSTIN or state") is
	// actionable rather than a silent fallback.
	Reason string `json:"reason,omitempty"`
}

// Summary is the one-line form stored on the PO (place_of_supply) and printed
// on the vendor's copy.
func (p PlaceOfSupply) Summary() string {
	if !p.Derived {
		return ""
	}
	kind := "Intra-state"
	if p.Interstate {
		kind = "Inter-state"
	}
	return fmt.Sprintf("%s: vendor %s -> entity %s", kind, p.VendorStateCode, p.BuyerStateCode)
}

// vendorStateCode resolves a Vendor's state, GSTIN first then its state field.
func vendorStateCode(tenantID, vendorID string) (string, error) {
	if strings.TrimSpace(vendorID) == "" {
		return "", nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	var gstin, state sql.NullString
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT data->>'gstin', data->>'state'
		  FROM %s.documents
		 WHERE doctype = 'Vendor' AND deleted_at IS NULL
		   AND (id = $1 OR data->>'code' = $1)
		 ORDER BY CASE WHEN id = $1 THEN 0 ELSE 1 END
		 LIMIT 1`, schema), vendorID).Scan(&gstin, &state)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if code := StateCodeFromGSTIN(gstin.String); code != "" {
		return code, nil
	}
	return StateCodeFromName(state.String), nil
}

// buyerStateCode resolves the buying entity's state by walking
// Location -> LegalEntity, reading the entity's GSTIN first then its state.
func buyerStateCode(tenantID, locationCode string) (string, error) {
	if strings.TrimSpace(locationCode) == "" {
		return "", nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	var gstin, state sql.NullString
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT e.data->>'gstin', e.data->>'state'
		  FROM %s.documents l
		  JOIN %s.documents e
		    ON e.doctype = 'LegalEntity' AND e.deleted_at IS NULL
		   AND (e.id = l.data->>'legal_entity' OR e.data->>'code' = l.data->>'legal_entity')
		 WHERE l.doctype = 'Location' AND l.deleted_at IS NULL
		   AND (l.id = $1 OR l.data->>'code' = $1)
		 LIMIT 1`, schema, schema), locationCode).Scan(&gstin, &state)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if code := StateCodeFromGSTIN(gstin.String); code != "" {
		return code, nil
	}
	return StateCodeFromName(state.String), nil
}

// ResolvePlaceOfSupply compares the buying Location's Legal Entity against the
// Vendor and reports whether the supply is inter-state.
//
// A lookup failure is never fatal: it produces Derived=false with a reason,
// because a database hiccup resolving an optional convenience must not stop a
// PO being saved.
func ResolvePlaceOfSupply(tenantID, locationCode, vendorID string) PlaceOfSupply {
	out := PlaceOfSupply{}

	buyer, err := buyerStateCode(tenantID, locationCode)
	if err != nil {
		out.Reason = "could not read the buying entity's state"
		return out
	}
	vendor, errV := vendorStateCode(tenantID, vendorID)
	if errV != nil {
		out.Reason = "could not read the vendor's state"
		return out
	}

	out.BuyerStateCode, out.VendorStateCode = buyer, vendor
	out.BuyerStateLabel, out.VendorState = StateLabel(buyer), StateLabel(vendor)

	switch {
	case buyer == "" && vendor == "":
		out.Reason = "neither the buying entity nor the vendor has a GSTIN or state recorded"
	case buyer == "":
		out.Reason = "the Location's Legal Entity has no GSTIN or state recorded"
	case vendor == "":
		out.Reason = "this vendor has no GSTIN or state recorded"
	default:
		out.Interstate = buyer != vendor
		out.Derived = true
	}
	return out
}

// ApplyPlaceOfSupply stamps the derived supply type onto a PurchaseOrder
// payload in place, and returns what it derived.
//
// It respects an explicit human override: once interstate_override is set on
// the document, the derivation is reported but never overwrites `interstate`.
// That is what makes the checkbox on the PO screen meaningful - a maker who
// knows about a bill-to/ship-to split that the two master records do not
// capture can still say so, and their choice survives the next save.
func ApplyPlaceOfSupply(tenantID string, payload map[string]interface{}) PlaceOfSupply {
	location, _ := payload["location"].(string)
	vendor, _ := payload["vendor"].(string)
	if vendor == "" {
		vendor, _ = payload["vendor_id"].(string)
	}

	pos := ResolvePlaceOfSupply(tenantID, location, vendor)
	payload["place_of_supply"] = pos.Summary()

	if truthy(payload["interstate_override"]) {
		return pos
	}
	if pos.Derived {
		payload["interstate"] = pos.Interstate
	}
	return pos
}

// truthy reads a Check field's value across the shapes it arrives in: a real
// bool from a JSON body, and the "true"/"1"/"on" strings a form post or a CSV
// import produces.
func truthy(v interface{}) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		switch strings.ToLower(strings.TrimSpace(t)) {
		case "true", "1", "yes", "on":
			return true
		}
	case float64:
		return t != 0
	}
	return false
}
