package engines

import (
	"custom_erp/db"
	"fmt"
)

// Taxonomy versioning (Stage 26.4.3). ProductFamily/ProductAttributeDef/
// ProductFamilyAttribute/ProductAttributeGroup are all plain generic
// doctypes (Stage 15/26.4.1) with no dedicated engine of their own - every
// field-level change on them is already captured by the existing
// tenant_default.audit_logs trigger (db/migration.sql section 16,
// log_document_changes/log_document_insert_delete), which fires on every
// documents INSERT/UPDATE/DELETE regardless of doctype. This file adds no
// new table and no new trigger - it only reads that existing audit trail
// back out, filtered to one taxonomy document, instead of building a
// parallel version-history mechanism.

// taxonomyHistoryDoctypes is the allowlist of doctypes GetTaxonomyHistory
// will report on - the same "explicit allowlist over a caller-provided
// query" pattern engines/pim_reports.go's ListPIMReport already uses, so
// this can't be pointed at an unrelated doctype's audit trail.
var taxonomyHistoryDoctypes = map[string]bool{
	"ProductFamily":          true,
	"ProductAttributeDef":    true,
	"ProductFamilyAttribute": true,
	"ProductAttributeGroup":  true,
}

// IsTaxonomyHistoryDoctype reports whether doctype is one of the taxonomy
// doctypes GetTaxonomyHistory supports.
func IsTaxonomyHistoryDoctype(doctype string) bool {
	return taxonomyHistoryDoctypes[doctype]
}

type TaxonomyHistoryEntry struct {
	UserID    string `json:"user_id"`
	Action    string `json:"action"`
	Details   string `json:"details"`
	CreatedAt string `json:"created_at"`
}

// GetTaxonomyHistory returns every audit_logs row for one taxonomy
// document, oldest first. The two WHERE branches mirror the two message
// shapes log_document_changes/log_document_insert_delete actually write:
// a CREATE row always starts with "Created Document ID: <id> with data:",
// an UPDATE row always ends with "for Document ID: <id>" (nothing
// trailing) - anchoring on start/end rather than a bare substring match
// avoids one document's id ever matching another's audit rows as a
// false-positive substring.
func GetTaxonomyHistory(tenantID, doctype, docID string) ([]TaxonomyHistoryEntry, error) {
	if !IsTaxonomyHistoryDoctype(doctype) {
		return nil, fmt.Errorf("taxonomy history is not available for doctype %q", doctype)
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}

	createAction := "CREATE_" + doctype
	updateAction := "UPDATE_" + doctype
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT user_id, action, details, created_at::text FROM %s.audit_logs
		WHERE (action = $1 AND details LIKE 'Created Document ID: ' || $2 || ' with data:%%')
		   OR (action = $3 AND details LIKE '%%for Document ID: ' || $2)
		ORDER BY created_at ASC`, schema), createAction, docID, updateAction)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []TaxonomyHistoryEntry{}
	for rows.Next() {
		var e TaxonomyHistoryEntry
		if err := rows.Scan(&e.UserID, &e.Action, &e.Details, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
