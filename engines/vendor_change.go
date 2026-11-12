package engines

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"custom_erp/db"
)

// Stage 148.10 - vendor changes wait for approval.
//
// SettingVendorApprovalFields lists the vendor fields whose change needs a
// second person. When an existing vendor is saved with one of them changed
// (record save, CSV import, Setup Workbook - all through the same two entry
// points, HoldVendorChanges), the live value is kept and a VendorChange request
// holding before / after goes through the maker-checker approval engine; the
// decision hook applies it once. A brand-new vendor is not held. While a
// request waits, payments to that vendor are refused (checkVendorPaymentAllowedTx).
// An empty list means every edit applies at once.

const (
	SettingVendorApprovalFields = "procurement.vendor_approval_fields"
	VendorChangeDoctype         = "VendorChange"
	defaultVendorApprovalFields = "bank_account_number,bank_ifsc,gstin,payment_term,credit_limit,vendor_group"
)

func init() {
	RegisterSetting(SettingDefinition{
		Key: SettingVendorApprovalFields, Module: "Procurement",
		Label: "Vendor fields that need approval to change", Type: SettingTypeString, Default: defaultVendorApprovalFields,
		Description: "Comma-separated vendor fields. A change to one of them waits for a second person's approval and payments to the vendor pause meanwhile; the old value stays until then. Blank: every edit applies at once.",
	})
	RegisterApprovalDecisionHook(VendorChangeDoctype, false, func(tenantID, docID, decision, actor string) {
		if decision != "Approved" {
			return
		}
		if err := applyVendorChange(tenantID, docID, actor); err != nil {
			LogSystemError(tenantID, "", "ERROR", "vendor_change", fmt.Sprintf("approved change %s not applied: %v", docID, err), "")
		}
	})
}

// VendorApprovalFields is the configured list.
func VendorApprovalFields(tenantID string) []string {
	var out []string
	for _, f := range strings.Split(GetSettingString(tenantID, SettingVendorApprovalFields), ",") {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	return out
}

func vendorChangeText(v interface{}) string {
	switch x := v.(type) {
	case nil:
		return ""
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case string:
		return strings.TrimSpace(x)
	}
	return strings.TrimSpace(fmt.Sprint(v))
}

// HoldVendorChanges is the one guard every vendor-edit path calls with the
// stored record (prior) and the record about to be written (payload). It
// returns the fields held, after putting their live values back into payload
// and filing the request. prior == nil is a new vendor: nothing is held.
func HoldVendorChanges(tenantID, vendorID string, prior, payload map[string]interface{}, userID, role string) ([]string, error) {
	if prior == nil || payload == nil {
		return nil, nil
	}
	watched := VendorApprovalFields(tenantID)
	if len(watched) == 0 {
		return nil, nil
	}
	before, after := map[string]interface{}{}, map[string]interface{}{}
	var held []string
	for _, f := range watched {
		nv, present := payload[f]
		if !present || vendorChangeText(nv) == vendorChangeText(prior[f]) {
			continue
		}
		held = append(held, f)
		before[f], after[f] = vendorChangeText(prior[f]), nv
	}
	if len(held) == 0 {
		return nil, nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	id := NewDocID("VCH")
	bj, _ := json.Marshal(before)
	aj, _ := json.Marshal(after)
	doc := map[string]interface{}{
		"code": id, "vendor_id": vendorID, "vendor_name": strField(prior, "name"), "fields": strings.Join(held, ", "),
		"summary":    vendorChangeSummary(strField(prior, "name"), held, before, after),
		"old_values": string(bj), "new_values": string(aj), "requested_by": userID, "status": "Draft",
	}
	b, _ := json.Marshal(doc)
	if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'VendorChange', $2, 'Draft', $3)`, schema), id, b, userID); err != nil {
		return nil, err
	}
	if err := SubmitForApproval(tenantID, VendorChangeDoctype, id, userID, role); err != nil {
		_, _ = db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.documents WHERE doctype = 'VendorChange' AND id = $1`, schema), id)
		return nil, err
	}
	for _, f := range held {
		if _, had := prior[f]; had {
			payload[f] = prior[f]
		} else {
			delete(payload, f)
		}
	}
	LogAuditEvent(tenantID, userID, "VENDOR_CHANGE_REQUESTED", "SUCCESS", fmt.Sprintf("Vendor %s: %s wait for approval (%s)", vendorID, strings.Join(held, ", "), id))
	return held, nil
}

// vendorChangeSummary is the line the approver reads. old_values and new_values are
// Super Admin only (sensitive_fields.go) because they hold the vendor's bank
// details, which the approver's own role may not see: those show their last four.
func vendorChangeSummary(name string, held []string, before, after map[string]interface{}) string {
	show := func(f string, v interface{}) string {
		s := vendorChangeText(v)
		if s == "" {
			return "blank"
		}
		if _, secret := sensitiveFields["Vendor"][f]; secret {
			if len(s) <= 4 {
				return "****"
			}
			return "****" + s[len(s)-4:]
		}
		return s
	}
	parts := []string{name}
	for _, f := range held {
		parts = append(parts, fmt.Sprintf("%s: %s → %s", strings.ReplaceAll(f, "_", " "), show(f, before[f]), show(f, after[f])))
	}
	return strings.Join(parts, " · ")
}

// applyVendorChange writes an approved request onto the vendor once (applied_at
// marks it) and records the edit in the audit log, so the bank-then-paid alert
// sees a bank change when it really lands.
func applyVendorChange(tenantID, docID, actor string) error {
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
	if err := tx.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents WHERE id = $1 AND doctype = 'VendorChange' FOR UPDATE`, schema), docID).Scan(&raw); err != nil {
		return err
	}
	var req map[string]interface{}
	if err := json.Unmarshal(raw, &req); err != nil {
		return err
	}
	if strField(req, "applied_at") != "" {
		return nil
	}
	var after map[string]interface{}
	if err := json.Unmarshal([]byte(strField(req, "new_values")), &after); err != nil {
		return err
	}
	vendorID := strField(req, "vendor_id")
	var vraw []byte
	if err := tx.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents WHERE id = $1 AND doctype = 'Vendor' FOR UPDATE`, schema), vendorID).Scan(&vraw); err != nil {
		return err
	}
	var vendor map[string]interface{}
	if err := json.Unmarshal(vraw, &vendor); err != nil {
		return err
	}
	prior := map[string]interface{}{}
	for k, v := range vendor {
		prior[k] = v
	}
	for k, v := range after {
		vendor[k] = v
	}
	vb, _ := json.Marshal(vendor)
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2 AND doctype = 'Vendor'`, schema), vb, vendorID); err != nil {
		return err
	}
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || jsonb_build_object('applied_at', to_char(now() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')) WHERE id = $1`, schema), docID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	LogDocumentEdit(tenantID, strField(req, "requested_by"), "Vendor", vendorID, prior, vendor, "approved change "+docID+" by "+actor)
	return nil
}

// holdVendorImportChanges is the CSV import's entry to HoldVendorChanges: a row
// that updates an existing vendor has its listed-field changes filed as a
// request and its live values written back into the row. A dry run files
// nothing. A row that cannot be held (no approval rule) is dropped with the
// reason rather than applied unchecked.
func holdVendorImportChanges(tx *sql.Tx, tenantID, doctype, userID, role string, dryRun bool, writes []importWrite, result *ImportResult) []importWrite {
	if doctype != "Vendor" || dryRun || len(writes) == 0 || len(VendorApprovalFields(tenantID)) == 0 {
		return writes
	}
	kept := writes[:0]
	for _, w := range writes {
		var raw []byte
		if err := tx.QueryRow(`SELECT data FROM documents WHERE doctype = 'Vendor' AND id = $1`, w.id).Scan(&raw); err != nil {
			kept = append(kept, w) // a new vendor is not held
			continue
		}
		var prior, row map[string]interface{}
		if json.Unmarshal(raw, &prior) != nil || json.Unmarshal([]byte(w.data), &row) != nil {
			kept = append(kept, w)
			continue
		}
		held, err := HoldVendorChanges(tenantID, w.id, prior, row, userID, role)
		if err != nil {
			recordImportError(result, w.rowNumber, fmt.Sprintf("a change to %s could not be sent for approval: %v", w.id, err))
			continue
		}
		if len(held) > 0 {
			b, _ := json.Marshal(row)
			w.data = string(b)
		}
		kept = append(kept, w)
	}
	return kept
}
