package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"log"
	"strings"
)

// Supplier portal (Stage 26.4.10).
//
// A supplier is a normal ERP login whose role reaches exactly one doctype -
// SupplierSubmission - rather than a separate portal application. See
// db/migrations_stage26_4_10_supplier_portal.sql for why that shape was
// chosen over a second app.
//
// This file holds the two pieces that shape cannot get from the generic
// doctype machinery for free:
//
//   - SupplierCodeForUser, the row-level scope a Supplier session is confined
//     to (enforced in handleGenericDoc, the one choke point every document
//     read and write already passes through);
//   - ApplyApprovedSupplierSubmission, which turns a QC-approved submission
//     into real ProductContent.

// SupplierCodeForUser returns the Vendor code a login speaks for, or "" for
// an ordinary internal user. Read per-request rather than carried as a token
// claim: adding a claim would change the shape of every session token in the
// system for the benefit of one role, and this is a single indexed lookup on
// a path only Supplier sessions take.
func SupplierCodeForUser(tenantID, userID string) (string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	var code string
	err = db.DB.QueryRow(fmt.Sprintf(
		"SELECT COALESCE(supplier_code, '') FROM %s.users WHERE id = $1", schema), userID).Scan(&code)
	if err != nil {
		return "", err
	}
	return code, nil
}

// ApplyApprovedSupplierSubmission copies a QC-approved submission onto a
// ProductContent row, creating it if the item/language pair has none yet.
//
// The content lands as **Draft**, never Approved. That is the important part:
// QC approval here means "this supplier's copy is acceptable to work with",
// not "publish it". ProductContent has its own approval gate (db/migration.sql
// seeds an approval_rules row for it), and short-circuiting that from an
// outside party's submission would let a supplier's text reach a live sales
// channel without an internal publish decision ever being made.
//
// Best-effort, called outside DecideApproval's transaction - the same posture
// as the Stage 26.4.6 content snapshot next to it. A failure here must not
// roll back an approval decision that has already been recorded; it is logged
// so the reviewer can retry by re-saving the content by hand.
func ApplyApprovedSupplierSubmission(tenantID, submissionID string, data map[string]interface{}, actorUserID string) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		log.Printf("[SUPPLIER-PORTAL] could not resolve schema for %s: %v", tenantID, err)
		return
	}

	str := func(key string) string {
		if v, ok := data[key]; ok && v != nil {
			return strings.TrimSpace(fmt.Sprintf("%v", v))
		}
		return ""
	}

	productID := str("product_id")
	language := str("language")
	if productID == "" || language == "" {
		log.Printf("[SUPPLIER-PORTAL] submission %s approved but has no product_id/language - nothing to apply", submissionID)
		return
	}

	// ProductContent's id convention is "<item_code>::<language>" (see
	// db/migration.sql) - reusing it means an approved submission updates the
	// item's existing content for that language instead of creating a second,
	// competing row for the same pair.
	contentID := productID + "::" + language

	var existing string
	err = db.DB.QueryRow(fmt.Sprintf(
		`SELECT COALESCE(data::text, '{}') FROM %s.documents
		 WHERE doctype = 'ProductContent' AND id = $1 AND deleted_at IS NULL`, schema), contentID).Scan(&existing)

	content := map[string]interface{}{}
	if err == nil {
		_ = json.Unmarshal([]byte(existing), &content)
	}

	// Field-for-field, and only the fields the supplier actually filled in -
	// a blank optional field in a submission means "I have nothing to add",
	// not "erase what the team already wrote".
	content["code"] = contentID
	content["product_id"] = productID
	content["language"] = language
	for _, f := range []string{"title", "short_desc", "long_desc", "seo_title", "tags"} {
		if v := str(f); v != "" {
			content[f] = v
		}
	}
	// Provenance: who this text came from, so a reviewer looking at the
	// content later can tell it originated outside the company.
	content["owner"] = str("supplier_code")
	content["status"] = "Draft"

	encoded, err := json.Marshal(content)
	if err != nil {
		log.Printf("[SUPPLIER-PORTAL] could not encode content for %s: %v", contentID, err)
		return
	}

	if _, err := db.DB.Exec(fmt.Sprintf(`
		INSERT INTO %s.documents (id, doctype, data, status, created_by)
		VALUES ($1, 'ProductContent', $2, 'Draft', $3)
		ON CONFLICT (id) DO UPDATE SET data = EXCLUDED.data, status = 'Draft'`, schema),
		contentID, string(encoded), actorUserID); err != nil {
		log.Printf("[SUPPLIER-PORTAL] could not apply submission %s to ProductContent %s: %v", submissionID, contentID, err)
		LogSystemError(tenantID, actorUserID, "Medium", "PIM",
			fmt.Sprintf("supplier submission %s was approved but its content could not be written to %s: %v", submissionID, contentID, err), "")
		return
	}

	LogAuditEvent(tenantID, actorUserID, "PIM", "SUPPLIER_SUBMISSION_APPLIED",
		fmt.Sprintf("Approved supplier submission %s applied to ProductContent %s as Draft", submissionID, contentID))
}
