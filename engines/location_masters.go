package engines

import (
	"custom_erp/db"
	"database/sql"
	"fmt"
	"strings"
)

// ValidateLocationReference is Stage 17.9's validation half: a transaction
// may only reference a Location code that exists in the new master and is
// Active. Deliberately stricter than the generic Link-field check
// (verifyDocumentExists only excludes 'Cancelled', not 'Inactive') since
// the acceptance gate specifically requires inactive locations to be
// rejected too, and applying that stricter rule through the generic Link
// mechanism would silently change behavior for every other existing Link
// field (Vendor, Customer, etc.) - out of scope here.
func ValidateLocationReference(tenantID, locationCode string) error {
	if locationCode == "" {
		return nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var status string
	err = db.DB.QueryRow(fmt.Sprintf(
		`SELECT status FROM %s.documents WHERE doctype = 'Location' AND id = $1 AND deleted_at IS NULL`, schema),
		locationCode).Scan(&status)
	if err == sql.ErrNoRows {
		return fmt.Errorf("location '%s' is not a registered Location", locationCode)
	}
	if err != nil {
		return err
	}
	if status != "Active" {
		return fmt.Errorf("location '%s' is not Active", locationCode)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Stage 53.2: may a POS till sell from this location?
//
// Before this, nothing stopped a cashier opening a session - or ringing up a
// whole sale - against a warehouse or against HO. The live screenshot that
// opened Stage 53 is exactly that: a till bound to HO, offering to complete a
// sale there.
//
// The answer is resolved in one place, called from both POS entry points
// (OpenPOSSession and handleCheckout), rather than duplicated at each - the
// repo's "attach it at the one shared choke point" rule, which is also what
// makes a third POS entry point added later covered for free.
//
// Resolution order, and why each step is what it is:
//
//	no Location record at all  -> sellable. Location codes are still free text
//	                              on most doctypes (Stage 17.9 deliberately
//	                              declined to convert them to Link fields), so
//	                              an unregistered code must keep working rather
//	                              than start refusing sales at a live till.
//	sellable = 'Yes' / 'No'    -> honoured exactly as written. This is the
//	                              normal case after 53.1's backfill.
//	sellable blank or absent   -> derived from type: 'Store' sells, anything
//	                              else does not. Covers a Location created
//	                              after 53.1 by someone who skipped the field;
//	                              53.1 keeps the field optional precisely so
//	                              that save is not rejected, and this is where
//	                              that omission gets a safe answer.
func LocationIsSellable(tenantID, locationCode string) (bool, error) {
	if locationCode == "" {
		return false, nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return false, err
	}
	var sellable, locType sql.NullString
	err = db.DB.QueryRow(fmt.Sprintf(
		`SELECT data->>'sellable', data->>'type' FROM %s.documents
		  WHERE doctype = 'Location' AND id = $1 AND deleted_at IS NULL`, schema),
		locationCode).Scan(&sellable, &locType)
	if err == sql.ErrNoRows {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	switch strings.TrimSpace(sellable.String) {
	case "Yes":
		return true, nil
	case "No":
		return false, nil
	}
	return strings.TrimSpace(locType.String) == "Store", nil
}

// ValidatePOSSellableLocation is the guard itself: the coded refusal both POS
// entry points return. Separate from LocationIsSellable so a caller that only
// wants the fact (the POS screen's own location picker, say) is not forced to
// parse an error to get it.
func ValidatePOSSellableLocation(tenantID, locationCode string) error {
	ok, err := LocationIsSellable(tenantID, locationCode)
	if err != nil {
		return err
	}
	if ok {
		return nil
	}
	return &ValidationError{
		Code: "POSOFF-0245",
		Message: fmt.Sprintf("'%s' is not a selling location - a POS till cannot sell from it. "+
			"Choose the store you are selling from, or have an administrator set Sellable = Yes on this Location.",
			locationCode),
	}
}

// ---------------------------------------------------------------------------
// Stage 53.12: the receipt header.
//
// A POS receipt used to print the location CODE and nothing else - "HO",
// "BKC01". A customer could not tell which branch of a chain they had bought
// from, and it would not stand up as a tax document.
//
// ReceiptStoreHeader resolves the store and the legal entity behind it. It is
// deliberately forgiving: a receipt must print. A missing Location record, a
// Location with no legal entity, or a LegalEntity with no GSTIN each simply
// leave that line off the receipt rather than failing the print and leaving a
// cashier with a paid sale and no paper.
type ReceiptStore struct {
	Code       string
	Name       string
	EntityName string
	GSTIN      string
	State      string
}

func ReceiptStoreHeader(tenantID, locationCode string) ReceiptStore {
	out := ReceiptStore{Code: locationCode, Name: locationCode}
	if locationCode == "" {
		return out
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return out
	}
	var name, entity sql.NullString
	err = db.DB.QueryRow(fmt.Sprintf(
		`SELECT data->>'name', data->>'legal_entity' FROM %s.documents
		  WHERE doctype = 'Location' AND id = $1 AND deleted_at IS NULL`, schema),
		locationCode).Scan(&name, &entity)
	if err != nil {
		return out
	}
	if strings.TrimSpace(name.String) != "" {
		out.Name = strings.TrimSpace(name.String)
	}
	entityCode := strings.TrimSpace(entity.String)
	if entityCode == "" {
		return out
	}
	var entName, gstin, state sql.NullString
	err = db.DB.QueryRow(fmt.Sprintf(
		`SELECT data->>'name', data->>'gstin', data->>'state' FROM %s.documents
		  WHERE doctype = 'LegalEntity' AND id = $1 AND deleted_at IS NULL`, schema),
		entityCode).Scan(&entName, &gstin, &state)
	if err != nil {
		return out
	}
	out.EntityName = strings.TrimSpace(entName.String)
	if out.EntityName == "" {
		out.EntityName = entityCode
	}
	out.GSTIN = strings.TrimSpace(gstin.String)
	out.State = strings.TrimSpace(state.String)
	return out
}

// Line is how the store reads on one line of a receipt: the shop's name, with
// its code in brackets when the two differ. A Location whose name was never
// filled in stores its code in both fields, and "HO (HO)" is not a heading.
func (s ReceiptStore) Line() string {
	if s.Name == "" || s.Name == s.Code {
		return s.Code
	}
	return fmt.Sprintf("%s (%s)", s.Name, s.Code)
}
