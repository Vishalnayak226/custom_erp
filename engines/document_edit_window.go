package engines

import (
	"custom_erp/db"
	"database/sql"
	"fmt"
	"time"
)

// Stage 30.7: a configurable "how long after creation may this document still
// be edited" window, the PO-edit-days / GRN-edit-days control.
//
// This is deliberately generic rather than two bespoke checks. Every doctype
// that wants a window declares it once in documentEditWindowSettings below,
// pointing at a registered setting key; ValidateTransactionalRules calls
// validateDocumentEditWindow for every doctype at its existing shared choke
// point, so adding a window to a third doctype later is one map entry plus one
// RegisterSetting call - no new validation plumbing and no missed call site.
//
// The window is orthogonal to the existing edit rules, not a replacement for
// them: a PO still needs an amendment reason once Approved (PURCHA-0085) and
// still can't be amended after full receipt (PO-0252). This adds a purely
// time-based bound on top.

// documentEditWindowSettings maps a doctype to the setting key holding its
// edit window in days. A doctype absent from this map has no time-based limit.
var documentEditWindowSettings = map[string]string{
	"PurchaseOrder": "procurement.po_edit_window_days",
	"GRN":           "procurement.grn_edit_window_days",
}

// validateDocumentEditWindow blocks an edit to a document older than its
// doctype's configured window.
//
// A window of 0 means "no time limit" - that is the registered default for
// both doctypes, so this is a no-op until an admin actually sets one, and no
// existing tenant's behavior changes when this ships.
//
// Only applies to an edit: docID == "" or priorData == nil is a create, which
// is never blocked (the same create-vs-edit test validatePurchaseOrderEditRules
// already uses).
func validateDocumentEditWindow(tenantID, doctype, docID string, priorData map[string]interface{}) error {
	if docID == "" || priorData == nil {
		return nil
	}
	settingKey, hasWindow := documentEditWindowSettings[doctype]
	if !hasWindow {
		return nil
	}
	windowDays := GetSettingInt(tenantID, settingKey)
	if windowDays <= 0 {
		return nil
	}

	createdAt, err := documentCreatedAt(tenantID, doctype, docID)
	if err != nil || createdAt.IsZero() {
		// Can't establish an age - fail open rather than block a legitimate
		// edit on a transient read error, the same posture GetSetting* takes.
		return nil
	}

	ageDays := int(time.Since(createdAt).Hours() / 24)
	if ageDays > windowDays {
		return &ValidationError{Message: fmt.Sprintf(
			"%s %s was created %d day(s) ago and can no longer be edited - the edit window for this document type is %d day(s). Raise it under Configuration if this needs to change.",
			editWindowDoctypeLabel(doctype), docID, ageDays, windowDays)}
	}
	return nil
}

// documentCreatedAt reads a document's creation timestamp. Returns a zero time
// (no error) when the document isn't found - the caller treats that as "can't
// establish an age" and lets the edit through, leaving not-found to the
// handlers that actually own that case.
func documentCreatedAt(tenantID, doctype, docID string) (time.Time, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return time.Time{}, err
	}
	var createdAt time.Time
	err = db.DB.QueryRow(fmt.Sprintf(
		`SELECT created_at FROM %s.documents WHERE doctype = $1 AND id = $2`, schema), doctype, docID).Scan(&createdAt)
	if err == sql.ErrNoRows {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, err
	}
	return createdAt, nil
}

// editWindowDoctypeLabel renders a doctype the way a user sees it on screen,
// so the error message reads "Purchase Order PO-001", not "PurchaseOrder ...".
func editWindowDoctypeLabel(doctype string) string {
	switch doctype {
	case "PurchaseOrder":
		return "Purchase Order"
	case "GRN":
		return "GRN"
	}
	return doctype
}
