package engines

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"custom_erp/db"
)

// Ask 215.2/215.3: a B2B bill at the till. A buyer GSTIN on the bill makes it
// a B2B tax invoice. CGST Rule 46 needs the buyer's GSTIN, name and address on
// it; the buyer's purchase order (number and date) is asked by default as other
// retail systems do, and a setting makes it optional. The till sends the block
// as "b2b"; checkout checks it here, before any side effect, and stores it on
// the bill so the print, the GSTR-1 and the sales reports read one record.

// POSB2BBuyer is the buyer block of a B2B till bill.
type POSB2BBuyer struct {
	GSTIN     string `json:"gstin"`
	LegalName string `json:"legal_name"`
	TradeName string `json:"trade_name,omitempty"`
	Address   string `json:"address"`
	City      string `json:"city"`
	Pincode   string `json:"pincode"`
	State     string `json:"state,omitempty"`      // from the GSTIN
	StateCode string `json:"state_code,omitempty"` // from the GSTIN
	Email     string `json:"email,omitempty"`
	Contact   string `json:"contact,omitempty"`
	PONumber  string `json:"po_number,omitempty"`
	PODate    string `json:"po_date,omitempty"` // YYYY-MM-DD
	// Delivery is "collect" (the buyer takes the goods at the counter, the
	// default) or "ship" (the store sends them to ShipAddress).
	Delivery    string `json:"delivery,omitempty"`
	ShipName    string `json:"ship_name,omitempty"`
	ShipAddress string `json:"ship_address,omitempty"`
	ShipCity    string `json:"ship_city,omitempty"`
	ShipPincode string `json:"ship_pincode,omitempty"`
	ShipState   string `json:"ship_state,omitempty"`
	// Set by the server.
	PlaceOfSupply string `json:"place_of_supply,omitempty"` // state code
	ReverseCharge string `json:"reverse_charge,omitempty"`  // "No": a till sale never is
}

var pinPattern = regexp.MustCompile(`^[1-9][0-9]{5}$`)

// Shipped is true when the store sends the goods.
func (b *POSB2BBuyer) Shipped() bool {
	return b != nil && strings.EqualFold(strings.TrimSpace(b.Delivery), "ship")
}

// normalise trims every field and upper-cases the GSTIN.
func (b *POSB2BBuyer) normalise() {
	for _, f := range []*string{&b.LegalName, &b.TradeName, &b.Address, &b.City, &b.Pincode, &b.State, &b.Email,
		&b.Contact, &b.PONumber, &b.PODate, &b.Delivery, &b.ShipName, &b.ShipAddress, &b.ShipCity, &b.ShipPincode, &b.ShipState} {
		*f = strings.TrimSpace(*f)
	}
	b.GSTIN = strings.ToUpper(strings.TrimSpace(b.GSTIN))
	b.Delivery = strings.ToLower(b.Delivery)
	if b.Delivery != "ship" {
		b.Delivery = "collect"
	}
}

// POSB2BPlaceOfSupply is the bill's place of supply and whether that makes it
// inter-state (IGST). Collected at the counter: the store's state (IGST Act
// s.10(1)(a), movement ends at the store). Shipped on the buyer's order: the
// buyer's GSTIN state (s.10(1)(b), bill-to-ship-to). It checks no fields, so
// the till's running quote can use it while the cashier is still typing.
func POSB2BPlaceOfSupply(tenantID, location string, b *POSB2BBuyer) (place string, interstate bool) {
	if b == nil {
		return "", false
	}
	store, _ := LocationStateCode(tenantID, location)
	buyer := StateCodeFromGSTIN(b.GSTIN)
	place = store
	if b.Shipped() && buyer != "" {
		place = buyer
	}
	return place, store != "" && place != "" && place != store
}

// PreparePOSB2B checks a B2B bill's buyer block and fills what the server
// decides: the buyer's state, the place of supply and reverse charge. It
// returns whether the bill is inter-state.
func PreparePOSB2B(tenantID, location string, b *POSB2BBuyer) (bool, error) {
	if b == nil {
		return false, nil
	}
	b.normalise()
	if !gstinPattern.MatchString(b.GSTIN) || !ValidGSTINChecksum(b.GSTIN) {
		return false, &ValidationError{Code: "CUSTOM-0869", SubFor: "b2b.gstin", Message: fmt.Sprintf("GSTIN %q is not a valid GSTIN (15 characters, last one a check digit).", b.GSTIN)}
	}
	b.StateCode = StateCodeFromGSTIN(b.GSTIN)
	if b.StateCode == "" {
		return false, &ValidationError{Code: "CUSTOM-0869", SubFor: "b2b.gstin", Message: fmt.Sprintf("GSTIN %q starts with no Indian state code.", b.GSTIN)}
	}
	if name, ok := indianStateNames[b.StateCode]; ok {
		b.State = name
	}
	var missing []string
	if b.LegalName == "" {
		missing = append(missing, "legal name")
	}
	if b.Address == "" {
		missing = append(missing, "address")
	}
	if b.City == "" {
		missing = append(missing, "city")
	}
	if !pinPattern.MatchString(b.Pincode) {
		missing = append(missing, "6-digit PIN")
	}
	if len(missing) > 0 {
		return false, &ValidationError{Code: "POSOFF-0879", SubFor: "b2b", Message: "A B2B bill needs the buyer's " + strings.Join(missing, ", ") + "."}
	}
	if b.PODate != "" {
		d, err := time.Parse("2006-01-02", b.PODate)
		if err != nil {
			return false, &ValidationError{Code: "POSOFF-0880", SubFor: "b2b.po_date", Message: fmt.Sprintf("PO date %q is not a date.", b.PODate)}
		}
		if d.Format("2006-01-02") > BusinessToday() {
			return false, &ValidationError{Code: "POSOFF-0880", SubFor: "b2b.po_date", Message: "The PO date is after today."}
		}
	}
	if !GetSettingBool(tenantID, "pos.b2b.po_optional") && (b.PONumber == "" || b.PODate == "") {
		return false, &ValidationError{Code: "POSOFF-0880", SubFor: "b2b.po_number", Message: "A B2B bill needs the buyer's PO number and PO date."}
	}
	if b.Shipped() {
		var miss []string
		if b.ShipAddress == "" {
			miss = append(miss, "address")
		}
		if b.ShipCity == "" {
			miss = append(miss, "city")
		}
		if !pinPattern.MatchString(b.ShipPincode) {
			miss = append(miss, "6-digit PIN")
		}
		if b.ShipState == "" || StateCodeFromName(b.ShipState) == "" {
			miss = append(miss, "state")
		}
		if len(miss) > 0 {
			return false, &ValidationError{Code: "POSOFF-0881", SubFor: "b2b.ship", Message: "Shipped goods need the ship-to " + strings.Join(miss, ", ") + "."}
		}
		if b.ShipName == "" {
			b.ShipName = b.LegalName
		}
	} else {
		b.ShipName, b.ShipAddress, b.ShipCity, b.ShipPincode, b.ShipState = "", "", "", "", ""
	}
	b.ReverseCharge = "No"
	place, interstate := POSB2BPlaceOfSupply(tenantID, location, b)
	b.PlaceOfSupply = place
	return interstate, nil
}

// LookupCustomerByGSTIN finds the customer with this GSTIN, so a known buyer
// fills the till's B2B panel. Deleted, merged and walk-in records are not
// candidates; the oldest wins when two share it.
func LookupCustomerByGSTIN(tenantID, raw string) (CustomerRef, bool, error) {
	g := strings.ToUpper(strings.TrimSpace(raw))
	if !gstinPattern.MatchString(g) || !ValidGSTINChecksum(g) {
		return CustomerRef{}, false, &ValidationError{Code: "CUSTOM-0869", SubFor: "gstin", Message: fmt.Sprintf("GSTIN %q is not a valid GSTIN (15 characters, last one a check digit).", g)}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return CustomerRef{}, false, err
	}
	var id string
	var data []byte
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT id, data FROM %s.documents
		 WHERE doctype = 'Customer' AND UPPER(data->>'gstin') = $1 AND deleted_at IS NULL AND status != 'Cancelled'
		   AND COALESCE(data->>'merged_into', '') = '' AND COALESCE(data->>'walk_in', '') <> 'Yes'
		 ORDER BY created_at LIMIT 1`, schema), g).Scan(&id, &data)
	if errors.Is(err, sql.ErrNoRows) {
		return CustomerRef{}, false, nil
	}
	if err != nil {
		return CustomerRef{}, false, err
	}
	var m map[string]interface{}
	_ = json.Unmarshal(data, &m)
	return customerRefFrom(id, m), true, nil
}

// POSB2BFromData reads the buyer block a bill stored, or nil.
func POSB2BFromData(data map[string]interface{}) *POSB2BBuyer {
	raw, ok := data["b2b"].(map[string]interface{})
	if !ok || strings.TrimSpace(strField(raw, "gstin")) == "" {
		return nil
	}
	return &POSB2BBuyer{
		GSTIN: strings.ToUpper(strField(raw, "gstin")), LegalName: strField(raw, "legal_name"), TradeName: strField(raw, "trade_name"),
		Address: strField(raw, "address"), City: strField(raw, "city"), Pincode: strField(raw, "pincode"),
		State: strField(raw, "state"), StateCode: strField(raw, "state_code"), Email: strField(raw, "email"), Contact: strField(raw, "contact"),
		PONumber: strField(raw, "po_number"), PODate: strField(raw, "po_date"), Delivery: strField(raw, "delivery"),
		ShipName: strField(raw, "ship_name"), ShipAddress: strField(raw, "ship_address"), ShipCity: strField(raw, "ship_city"),
		ShipPincode: strField(raw, "ship_pincode"), ShipState: strField(raw, "ship_state"),
		PlaceOfSupply: strField(raw, "place_of_supply"), ReverseCharge: strField(raw, "reverse_charge"),
	}
}
