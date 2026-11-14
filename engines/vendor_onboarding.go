package engines

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"custom_erp/db"
)

// Supplier onboarding and approved-vendor-only sourcing (PROC > Vendors).
//
//   - procurement.vendor_onboarding (off): a new vendor starts Applied. A
//     VendorOnboarding request goes through the maker-checker engine (the
//     same one as VendorChange); its decision hook sets the vendor's
//     onboarding_status. A vendor that is not Approved cannot be put on a new
//     purchase order. A vendor with no status (every existing one) counts as
//     Approved, so nothing changes until the setting is turned on.
//   - procurement.approved_vendor_only (off): the approved vendor list
//     (ApprovedVendor: item or category -> vendor). A PO line whose item or
//     its category has entries is refused unless the PO vendor is listed.
//     Items with no entries are unaffected.
//
// Both checks sit in checkPOVendorHold, the one step every PO path already runs.

const (
	SettingVendorOnboarding  = "procurement.vendor_onboarding"
	SettingApprovedVendorOnl = "procurement.approved_vendor_only"

	VendorOnboardingDoctype = "VendorOnboarding"
	ApprovedVendorDoctype   = "ApprovedVendor"

	VendorOnboardingApplied  = "Applied"
	VendorOnboardingApproved = "Approved"
	VendorOnboardingRejected = "Rejected"
	VendorOnboardingBlocked  = "Blocked"

	VendorNotApprovedCode    = "VENDOR-0300"
	VendorNotOnAVLCode       = "VENDOR-0301"
	VendorOnboardRequestCode = "VENDOR-0302"
	VendorChecklistCode      = "VENDOR-0303"
	VendorOnboardPendingCode = "VENDOR-0304"
	ApprovedVendorRowCode    = "VENDOR-0305"
	ApprovedVendorDupCode    = "VENDOR-0306"
)

// vendorChecklistFields are the three documents an approver ticks off.
var vendorChecklistFields = []string{"doc_gstin_checked", "doc_pan_checked", "doc_bank_proof_checked"}

func init() {
	RegisterSetting(SettingDefinition{
		Key: SettingVendorOnboarding, Module: "Procurement",
		Label: "Vendor onboarding approval", Type: SettingTypeBool, Default: "false",
		Description: "On: a new vendor starts as Applied and cannot be used on a new purchase order until an approver marks it Approved. Vendors with no onboarding status count as Approved.",
	})
	RegisterSetting(SettingDefinition{
		Key: SettingApprovedVendorOnl, Module: "Procurement",
		Label: "Approved vendors only", Type: SettingTypeBool, Default: "false",
		Description: "On: a purchase order line whose item or category has approved vendors listed is refused for any other vendor. Items with no list are unaffected.",
	})
	RegisterApprovalDecisionHook(VendorOnboardingDoctype, false, func(tenantID, docID, decision, actor string) {
		if err := applyVendorOnboarding(tenantID, docID, decision, actor); err != nil {
			LogSystemError(tenantID, "", "ERROR", "vendor_onboarding", fmt.Sprintf("decision on %s not applied: %v", docID, err), "")
		}
	})
}

// vendorOnboardingBlocks is the status that keeps a vendor off new orders, or ""
// when it may be used (setting off, no status, or Approved).
func vendorOnboardingBlocks(tenantID string, vendor map[string]interface{}) string {
	if !GetSettingBool(tenantID, SettingVendorOnboarding) {
		return ""
	}
	st := strings.TrimSpace(strField(vendor, "onboarding_status"))
	if st == "" || st == VendorOnboardingApproved {
		return ""
	}
	return st
}

// StampVendorOnboarding is the one guard the save paths call with the stored
// record (prior, nil for a new vendor) and the record about to be written. The
// status is never taken from the caller: a new vendor starts Applied when the
// setting is on (blank when off), and an edit keeps the stored value. Only the
// request's decision changes it afterwards.
func StampVendorOnboarding(tenantID string, prior, payload map[string]interface{}) {
	if payload == nil {
		return
	}
	if prior != nil {
		if v, had := prior["onboarding_status"]; had {
			payload["onboarding_status"] = v
		} else {
			delete(payload, "onboarding_status")
		}
		return
	}
	if GetSettingBool(tenantID, SettingVendorOnboarding) {
		payload["onboarding_status"] = VendorOnboardingApplied
	} else {
		delete(payload, "onboarding_status")
	}
}

// RequestVendorOnboarding files the decision (Approved or Blocked) for a vendor and
// submits it to the approval engine. Approved needs the three documents ticked.
func RequestVendorOnboarding(tenantID, vendorID, target, userID, role string) (string, error) {
	target = strings.TrimSpace(target)
	if target != VendorOnboardingApproved && target != VendorOnboardingBlocked {
		return "", &ValidationError{Code: VendorOnboardRequestCode, SubFor: "Vendor", Message: "The decision must be Approved or Blocked"}
	}
	if !GetSettingBool(tenantID, SettingVendorOnboarding) {
		return "", &ValidationError{Code: VendorOnboardRequestCode, SubFor: "Vendor", Message: "Vendor onboarding is switched off in Settings"}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	vid, v, ok := findVendorRow(db.DB, schema, vendorID)
	if !ok {
		return "", &ValidationError{Code: VendorOnboardRequestCode, SubFor: "Vendor", Message: "Vendor not found"}
	}
	name := firstNonEmpty(strField(v, "name"), vid)
	current := strings.TrimSpace(strField(v, "onboarding_status"))
	if current == "" || current == target {
		return "", &ValidationError{Code: VendorOnboardRequestCode, SubFor: "Vendor", Message: fmt.Sprintf("Vendor %s is already %s", name, firstNonEmpty(current, VendorOnboardingApproved))}
	}
	if target == VendorOnboardingApproved {
		var missing []string
		for _, f := range vendorChecklistFields {
			if strField(v, f) != "Yes" {
				missing = append(missing, strings.TrimSuffix(strings.TrimPrefix(strings.ReplaceAll(f, "_", " "), "doc "), " checked"))
			}
		}
		if len(missing) > 0 {
			return "", &ValidationError{Code: VendorChecklistCode, SubFor: "Vendor", Message: fmt.Sprintf(
				"Tick the documents checklist first: %s", strings.Join(missing, ", "))}
		}
	}
	var waiting bool
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT EXISTS (SELECT 1 FROM %s.documents WHERE doctype = 'VendorOnboarding' AND deleted_at IS NULL AND status = 'Pending Approval' AND data->>'vendor_id' = $1)`, schema), vid).Scan(&waiting)
	if waiting {
		return "", &ValidationError{Code: VendorOnboardPendingCode, SubFor: "Vendor", Message: fmt.Sprintf("A decision on vendor %s is already waiting for approval", name)}
	}
	id := NewDocID("VON")
	doc := map[string]interface{}{
		"code": id, "vendor_id": vid, "vendor_name": name, "target_status": target,
		"summary":      fmt.Sprintf("%s · %s → %s", name, current, target),
		"requested_by": userID, "status": "Draft",
	}
	b, _ := json.Marshal(doc)
	if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'VendorOnboarding', $2, 'Draft', $3)`, schema), id, b, userID); err != nil {
		return "", err
	}
	if err := SubmitForApproval(tenantID, VendorOnboardingDoctype, id, userID, role); err != nil {
		_, _ = db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.documents WHERE doctype = 'VendorOnboarding' AND id = $1`, schema), id)
		return "", err
	}
	LogAuditEvent(tenantID, userID, "VENDOR_ONBOARDING_REQUESTED", "SUCCESS", fmt.Sprintf("Vendor %s: %s requested (%s)", vid, target, id))
	return id, nil
}

// applyVendorOnboarding writes a decided request onto the vendor. Approved sets
// the requested status (the checklist is checked again); Rejected on an approval
// request marks the vendor Rejected, and on a block request changes nothing.
func applyVendorOnboarding(tenantID, docID, decision, actor string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var raw []byte
	if err := tx.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents WHERE id = $1 AND doctype = 'VendorOnboarding' FOR UPDATE`, schema), docID).Scan(&raw); err != nil {
		return err
	}
	var req map[string]interface{}
	if err := json.Unmarshal(raw, &req); err != nil {
		return err
	}
	if strField(req, "applied_at") != "" {
		return nil
	}
	target := strField(req, "target_status")
	newStatus := ""
	switch {
	case decision == "Approved" && (target == VendorOnboardingApproved || target == VendorOnboardingBlocked):
		newStatus = target
	case decision == "Rejected" && target == VendorOnboardingApproved:
		newStatus = VendorOnboardingRejected
	}
	vendorID := strField(req, "vendor_id")
	if newStatus != "" {
		var vraw []byte
		if err := tx.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents WHERE id = $1 AND doctype = 'Vendor' FOR UPDATE`, schema), vendorID).Scan(&vraw); err != nil {
			return err
		}
		var vendor map[string]interface{}
		if err := json.Unmarshal(vraw, &vendor); err != nil {
			return err
		}
		if newStatus == VendorOnboardingApproved {
			for _, f := range vendorChecklistFields {
				if strField(vendor, f) != "Yes" {
					return fmt.Errorf("documents checklist not complete on vendor %s", vendorID)
				}
			}
		}
		if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || jsonb_build_object('onboarding_status', $1::text), updated_at = CURRENT_TIMESTAMP WHERE id = $2 AND doctype = 'Vendor'`, schema), newStatus, vendorID); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || jsonb_build_object('applied_at', to_char(now() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')) WHERE id = $1`, schema), docID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if newStatus != "" {
		LogAuditEvent(tenantID, actor, "VENDOR_ONBOARDING_DECIDED", "SUCCESS", fmt.Sprintf("Vendor %s is now %s (%s)", vendorID, newStatus, docID))
	}
	return nil
}

// checkPOVendorSourcing is the two sourcing checks for an order being drafted.
func checkPOVendorSourcing(tenantID string, po map[string]interface{}) error {
	ref := firstNonEmpty(strField(po, "vendor"), strField(po, "vendor_id"))
	if db.DB == nil || strings.TrimSpace(ref) == "" {
		return nil
	}
	onboard := GetSettingBool(tenantID, SettingVendorOnboarding)
	avl := GetSettingBool(tenantID, SettingApprovedVendorOnl)
	if !onboard && !avl {
		return nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	vid, v, ok := findVendorRow(db.DB, schema, ref)
	if !ok {
		return nil
	}
	name := firstNonEmpty(strField(v, "name"), ref)
	if st := vendorOnboardingBlocks(tenantID, v); st != "" {
		return &ValidationError{Code: VendorNotApprovedCode, SubFor: "Vendor", Message: fmt.Sprintf(
			"Vendor %s is %s and cannot be used on a new purchase order until it is Approved", name, st)}
	}
	if !avl {
		return nil
	}
	for _, ln := range jsonRows(po["items"]) {
		sku := strings.TrimSpace(strField(ln, "sku"))
		if sku == "" {
			continue
		}
		listed, names, err := approvedVendorsForItem(db.DB, schema, sku)
		if err != nil {
			return err
		}
		if len(listed) == 0 || listed[vid] {
			continue
		}
		return &ValidationError{Code: VendorNotOnAVLCode, SubFor: "Vendor", Message: fmt.Sprintf(
			"Vendor %s is not an approved vendor for %s. Approved: %s", name, sku, strings.Join(names, ", "))}
	}
	return nil
}

type rowsQuerier interface {
	Query(query string, args ...interface{}) (*sql.Rows, error)
	QueryRow(query string, args ...interface{}) *sql.Row
}

// approvedVendorsForItem is the set of vendor ids the list names for an item
// (by its code) or its category, with their display names.
func approvedVendorsForItem(q rowsQuerier, schema, sku string) (map[string]bool, []string, error) {
	var category string
	_ = q.QueryRow(fmt.Sprintf(`SELECT COALESCE(data->>'category', '') FROM %s.documents
		WHERE doctype = 'Item' AND deleted_at IS NULL AND (id = $1 OR data->>'code' = $1) LIMIT 1`, schema), sku).Scan(&category)
	rows, err := q.Query(fmt.Sprintf(`
		SELECT a.data->>'vendor_id', COALESCE(NULLIF(v.data->>'name', ''), a.data->>'vendor_id')
		  FROM %s.documents a LEFT JOIN %s.documents v ON v.doctype = 'Vendor' AND v.id = a.data->>'vendor_id'
		 WHERE a.doctype = 'ApprovedVendor' AND a.deleted_at IS NULL
		   AND (a.data->>'item_id' = $1 OR ($2 <> '' AND a.data->>'category' = $2))
		 ORDER BY 2`, schema, schema), sku, category)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	set := map[string]bool{}
	var names []string
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, nil, err
		}
		if !set[id] {
			set[id] = true
			names = append(names, name)
		}
	}
	return set, names, rows.Err()
}

// validateApprovedVendorRow: an entry names an item or a category (one of them)
// and a vendor, once.
func validateApprovedVendorRow(tenantID, docID string, payload map[string]interface{}) error {
	item, cat := strings.TrimSpace(strField(payload, "item_id")), strings.TrimSpace(strField(payload, "category"))
	if (item == "") == (cat == "") {
		return &ValidationError{Code: ApprovedVendorRowCode, SubFor: "ApprovedVendor", Message: "Give an item or a category, not both and not neither"}
	}
	vendor := strings.TrimSpace(strField(payload, "vendor_id"))
	if vendor == "" || db.DB == nil {
		return nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var n int
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.documents WHERE doctype = 'ApprovedVendor' AND deleted_at IS NULL AND id <> $1
		AND data->>'vendor_id' = $2 AND COALESCE(data->>'item_id', '') = $3 AND COALESCE(data->>'category', '') = $4`, schema),
		docID, vendor, item, cat).Scan(&n)
	if n > 0 {
		return &ValidationError{Code: ApprovedVendorDupCode, SubFor: "ApprovedVendor", Message: "This vendor is already listed for that item or category"}
	}
	return nil
}

// stampVendorImportOnboarding is the CSV import's entry to StampVendorOnboarding:
// a row never sets the status, and a new vendor row starts Applied when the
// setting is on. A dry run is left as it is.
func stampVendorImportOnboarding(tx *sql.Tx, tenantID, doctype string, dryRun bool, writes []importWrite) []importWrite {
	if doctype != "Vendor" || dryRun {
		return writes
	}
	for i, w := range writes {
		var row, prior map[string]interface{}
		if json.Unmarshal([]byte(w.data), &row) != nil {
			continue
		}
		var raw []byte
		if err := tx.QueryRow(`SELECT data FROM documents WHERE doctype = 'Vendor' AND id = $1`, w.id).Scan(&raw); err == nil {
			_ = json.Unmarshal(raw, &prior)
		}
		StampVendorOnboarding(tenantID, prior, row)
		if b, err := json.Marshal(row); err == nil {
			writes[i].data = string(b)
		}
	}
	return writes
}
