package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// 49.6.8: what erasure and correction reach beyond the Customer row itself.
//
// Erasure removes the subject's attachments (the files and their names) and the
// saved views that carry their id, phone or e-mail. There is no separate
// search-index table: search runs over documents.data, so the anonymising update
// is what removes the subject from search. Logs, the audit trail and backups are
// not rewritten; they age out by their retention periods.

// customerCorrectableFields are the Customer fields a correction request may set:
// the personal-data register's identity, contact, address and demographic rows,
// minus the key, the consent proof and the system state.
var customerCorrectableFields = map[string]bool{
	"name": true, "phone": true, "phone_country": true, "email": true,
	"address": true, "address_line2": true, "city": true, "state": true, "state_code": true,
	"pincode": true, "pan": true, "gstin": true,
	"date_of_birth": true, "date_of_anniversary": true, "gender": true,
}

// validateCustomerCorrections refuses an empty set or a field outside the list.
func validateCustomerCorrections(c map[string]interface{}) error {
	if len(c) == 0 {
		return fmt.Errorf("a correction needs the corrected values")
	}
	for f := range c {
		if !customerCorrectableFields[f] {
			return fmt.Errorf("field %q cannot be corrected here", f)
		}
	}
	return nil
}

// checkCustomerCorrection holds the corrected values to the Customer's own
// field rules (ValidateDocument: formats, lengths, links) on the record as it
// would read after the change. A rule the record already broke before the
// correction does not block it; only a break the correction causes does.
func checkCustomerCorrection(tenantID, customerID string, c map[string]interface{}) error {
	if err := validateCustomerCorrections(c); err != nil {
		return err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var raw []byte
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents WHERE doctype = 'Customer' AND id = $1`, schema), customerID).Scan(&raw); err != nil {
		return fmt.Errorf("customer %s not found", customerID)
	}
	before := map[string]interface{}{}
	after := map[string]interface{}{}
	_ = json.Unmarshal(raw, &before)
	_ = json.Unmarshal(raw, &after)
	for f, v := range c {
		after[f] = v
	}
	errAfter := ValidateDocument(tenantID, "Customer", after)
	if errAfter != nil {
		if errBefore := ValidateDocument(tenantID, "Customer", before); errBefore == nil || errBefore.Error() != errAfter.Error() {
			return errAfter
		}
	}
	// Keep what ValidateDocument cleaned (a phone written "+91 (98765) 43210"
	// is stored the way a normal save would store it).
	for f := range c {
		c[f] = after[f]
	}
	return nil
}

// applyCustomerCorrection writes the approved values to the Customer record
// and leaves an audit row naming the fields (not the values).
func applyCustomerCorrection(tenantID, customerID, requestID, actor string, c map[string]interface{}) error {
	if err := checkCustomerCorrection(tenantID, customerID, c); err != nil {
		return err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	patch, err := json.Marshal(c)
	if err != nil {
		return err
	}
	res, err := db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = data || $1::jsonb, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'Customer' AND id = $2`, schema),
		patch, customerID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("customer %s not found", customerID)
	}
	names := make([]string, 0, len(c))
	for f := range c {
		names = append(names, f)
	}
	sort.Strings(names)
	LogAuditEvent(tenantID, actor, "data_subject_correction_applied", "Success",
		fmt.Sprintf("request %s corrected %s on customer %s", requestID, strings.Join(names, ", "), customerID))
	return nil
}

// eraseCustomerAttachments deletes the subject's files from the media store and
// blanks their names. The rows stay, marked deleted, as the record that a file existed.
func eraseCustomerAttachments(schema, customerID string) (int, error) {
	rows, err := db.DB.Query(fmt.Sprintf(
		`SELECT id, storage_key FROM %s.document_attachments WHERE doctype = 'Customer' AND document_id = $1`, schema), customerID)
	if err != nil {
		return 0, err
	}
	type file struct{ id, key string }
	var files []file
	for rows.Next() {
		var f file
		if err := rows.Scan(&f.id, &f.key); err != nil {
			rows.Close()
			return 0, err
		}
		files = append(files, f)
	}
	rows.Close()
	for _, f := range files {
		if checkAttachmentKey(f.key) == nil {
			_ = serverMedia.Remove(f.key)
		}
		if _, err := db.DB.Exec(fmt.Sprintf(
			`UPDATE %s.document_attachments SET file_name = 'Erased file', deleted_at = COALESCE(deleted_at, CURRENT_TIMESTAMP), deleted_by = COALESCE(deleted_by, 'erasure') WHERE id = $1`, schema), f.id); err != nil {
			return 0, err
		}
	}
	return len(files), nil
}

// eraseCustomerSavedViews deletes saved views (UserShortcut) that carry the
// subject's id or one of the given contact values. A value matches only as a
// whole token (no letter or digit either side), so erasing CUST-1 leaves a
// view filtered on CUST-12 alone.
func eraseCustomerSavedViews(schema, customerID string, needles ...string) (int64, error) {
	all := []string{customerID}
	for _, n := range needles {
		if len(strings.TrimSpace(n)) >= 5 {
			all = append(all, strings.TrimSpace(n))
		}
	}
	var total int64
	for _, n := range all {
		pattern := `(^|[^A-Za-z0-9])` + regexp.QuoteMeta(n) + `([^A-Za-z0-9]|$)`
		res, err := db.DB.Exec(fmt.Sprintf(
			`DELETE FROM %s.documents WHERE doctype = $1 AND data::text ~* $2`, schema), UserShortcutDoctype, pattern)
		if err != nil {
			return total, err
		}
		c, _ := res.RowsAffected()
		total += c
	}
	return total, nil
}
