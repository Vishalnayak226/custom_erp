package engines

import (
	"custom_erp/db"
	"database/sql"
	"fmt"
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
