package engines

import (
	"fmt"
	"regexp"
	"strings"

	"custom_erp/db"
)

// UPI QR on the till (185.1, ask 185). BharatPe has no public merchant API, and
// neither do PhonePe or Paytm business accounts for a small store, but every one
// of them pays out to a UPI ID. A store's UPI ID (Location.upi_id) - or a UPI
// tender's own (POSTender.upi_id, for a second provider) - lets the till show a
// standard NPCI upi://pay QR for the exact amount; the customer pays in any UPI
// app and the cashier keys the UTR the tender already asks for. The provider's
// settlement statement is then matched by that UTR (185.2, acquirer_settlement.go).

// upiIDPattern is a UPI virtual payment address: handle@provider.
var upiIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{1,255}@[A-Za-z][A-Za-z0-9.-]{1,63}$`)

// ValidateUPIID is the save check on a Location or POSTender: a blank UPI ID is
// fine; anything else must look like name@provider, and only a UPI tender takes one.
func ValidateUPIID(doctype string, data map[string]interface{}) error {
	v := strings.TrimSpace(strField(data, "upi_id"))
	if v == "" {
		return nil
	}
	if !upiIDPattern.MatchString(v) {
		return &ValidationError{Code: "POSOFF-0878", SubFor: "upi_id", Message: fmt.Sprintf("%q is not a UPI ID - it looks like name@provider, for example store@ybl", v)}
	}
	if doctype == "POSTender" && !strings.EqualFold(strField(data, "kind"), "UPI") {
		return &ValidationError{Code: "POSOFF-0878", SubFor: "upi_id", Message: "only a UPI tender takes a UPI ID"}
	}
	return nil
}

// storeUPIPayee is a store's own UPI ID and the name a customer's app shows.
func storeUPIPayee(schema, store string) (upiID, name string) {
	if store == "" {
		return "", ""
	}
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT btrim(COALESCE(data->>'upi_id','')), btrim(COALESCE(NULLIF(data->>'name',''), id))
		FROM %s.documents WHERE doctype = 'Location' AND id = $1 AND deleted_at IS NULL`, schema), store).Scan(&upiID, &name)
	return upiID, name
}
