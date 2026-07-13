package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
)

// ApprovalRule is one amount-slab -> required-role routing entry for a
// doctype. A doctype with zero rules simply isn't approval-gated.
type ApprovalRule struct {
	ID           int      `json:"id"`
	Doctype      string   `json:"doctype"`
	MinAmount    float64  `json:"min_amount"`
	MaxAmount    *float64 `json:"max_amount"`
	RequiredRole string   `json:"required_role"`
}

// GetApprovalRules lists every configured routing rule (admin-facing
// configuration screen, same self-service pattern as prefix_configs).
func GetApprovalRules(tenantID string) ([]ApprovalRule, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, doctype, min_amount, max_amount, required_role
		FROM %s.approval_rules ORDER BY doctype, min_amount`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rules []ApprovalRule
	for rows.Next() {
		var r ApprovalRule
		if err := rows.Scan(&r.ID, &r.Doctype, &r.MinAmount, &r.MaxAmount, &r.RequiredRole); err != nil {
			return nil, err
		}
		rules = append(rules, r)
	}
	return rules, nil
}

// requiredApproverRole finds the rule matching doctype+amount. Returns
// ("", nil) if the doctype has no rules configured at all (not gated) -
// callers must distinguish that from a real error.
func requiredApproverRole(tenantID, doctype string, amount float64) (string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	var role string
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT required_role FROM %s.approval_rules
		WHERE doctype = $1 AND min_amount <= $2 AND (max_amount IS NULL OR max_amount >= $2)
		ORDER BY min_amount DESC LIMIT 1`, schema), doctype, amount).Scan(&role)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return role, err
}

// IsApprovalGated reports whether any rule exists for doctype at all -
// used to decide whether the generic doc-update path needs to run the
// re-approval-on-edit check for this doctype.
func IsApprovalGated(tenantID, doctype string) (bool, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return false, err
	}
	var count int
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.approval_rules WHERE doctype = $1`, schema), doctype).Scan(&count)
	return count > 0, err
}

func fetchDocument(tenantID, doctype, docID string) (data map[string]interface{}, status string, createdBy string, err error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, "", "", err
	}
	var dataStr string
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT data, status, created_by FROM %s.documents
		WHERE doctype = $1 AND id = $2`, schema), doctype, docID).Scan(&dataStr, &status, &createdBy)
	if err != nil {
		return nil, "", "", err
	}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return nil, "", "", err
	}
	return data, status, createdBy, nil
}

func logApprovalAction(tenantID, doctype, docID, action, actorUserID, actorRole string, amount float64, comment string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	_, err = db.DB.Exec(fmt.Sprintf(`
		INSERT INTO %s.approval_log (doctype, document_id, action, actor_user_id, actor_role, amount, comment)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`, schema), doctype, docID, action, actorUserID, actorRole, amount, comment)
	return err
}

func setDocumentStatus(tenantID, doctype, docID, newStatus string, data map[string]interface{}) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	data["status"] = newStatus
	marshaled, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = db.DB.Exec(fmt.Sprintf(`
		UPDATE %s.documents SET data = $1, status = $2, updated_at = CURRENT_TIMESTAMP
		WHERE doctype = $3 AND id = $4`, schema), marshaled, newStatus, doctype, docID)
	return err
}

// SubmitForApproval moves a Draft document into Pending Approval and logs
// the submission. Requires a configured approval_rules entry for the
// doctype - there's no silent no-op path for a doctype nobody set up.
func SubmitForApproval(tenantID, doctype, docID, requesterUserID, requesterRole string) error {
	data, status, _, err := fetchDocument(tenantID, doctype, docID)
	if err != nil {
		return fmt.Errorf("document not found: %v", err)
	}
	if status != "Draft" {
		return fmt.Errorf("only a Draft document can be submitted for approval (current status: %s)", status)
	}

	amount := extractAmount(data)
	role, err := requiredApproverRole(tenantID, doctype, amount)
	if err != nil {
		return err
	}
	if role == "" {
		return fmt.Errorf("%s has no approval rule configured - nothing to route this to", doctype)
	}

	if err := setDocumentStatus(tenantID, doctype, docID, "Pending Approval", data); err != nil {
		return err
	}
	return logApprovalAction(tenantID, doctype, docID, "Submitted", requesterUserID, requesterRole, amount, "")
}

// DecideApproval approves or rejects a Pending Approval document. Enforces:
//  1. Maker-checker segregation: the approver can never be the document's
//     original creator, regardless of role - including HR/Admin.
//  2. Role authorization: the approver's role must match the amount-slab's
//     required_role, or be HR/Admin (the existing systemwide catch-all
//     admin role, same override this codebase already grants it elsewhere).
//  3. Location match: a non-HR/Admin approver must be at the document's
//     location (same "location" field convention used by the generic doc
//     endpoint's own object-level authorization).
func DecideApproval(tenantID, doctype, docID, actorUserID, actorRole, actorLocation, decision, comment string) error {
	if decision != "Approved" && decision != "Rejected" {
		return fmt.Errorf("decision must be 'Approved' or 'Rejected'")
	}

	data, status, createdBy, err := fetchDocument(tenantID, doctype, docID)
	if err != nil {
		return fmt.Errorf("document not found: %v", err)
	}
	if status != "Pending Approval" {
		return fmt.Errorf("document is not awaiting approval (current status: %s)", status)
	}
	if actorUserID == createdBy {
		return fmt.Errorf("maker-checker violation: you cannot approve or reject a document you submitted")
	}

	amount := extractAmount(data)
	requiredRole, err := requiredApproverRole(tenantID, doctype, amount)
	if err != nil {
		return err
	}
	if actorRole != "HR/Admin" && actorRole != requiredRole {
		return fmt.Errorf("this amount requires approval from role '%s'", requiredRole)
	}
	if actorRole != "HR/Admin" {
		if docLoc, ok := data["location"].(string); ok && docLoc != "" && docLoc != actorLocation {
			return fmt.Errorf("this document belongs to a different location")
		}
	}

	if err := setDocumentStatus(tenantID, doctype, docID, decision, data); err != nil {
		return err
	}
	return logApprovalAction(tenantID, doctype, docID, decision, actorUserID, actorRole, amount, comment)
}

// ResetToPendingOnEdit implements "re-approval-on-edit": editing a document
// that was already Approved sends it back through the approval flow rather
// than letting the edit silently stand approved. Callers should only invoke
// this when the document's status *before* the edit was "Approved" - it
// re-derives the amount from the freshly-saved data so a routing-relevant
// change (e.g. total_amount) re-evaluates against the right slab next time.
func ResetToPendingOnEdit(tenantID, doctype, docID, editorUserID, editorRole string, data map[string]interface{}) error {
	amount := extractAmount(data)
	if err := setDocumentStatus(tenantID, doctype, docID, "Pending Approval", data); err != nil {
		return err
	}
	return logApprovalAction(tenantID, doctype, docID, "Modified", editorUserID, editorRole, amount, "Reset to Pending Approval after edit")
}

// ListPendingApprovals returns every Pending Approval document across all
// approval-gated doctypes, scoped the same way the generic doc list is:
// HR/Admin sees everything, everyone else only their own location's queue.
func ListPendingApprovals(tenantID, role, location string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}

	query := fmt.Sprintf(`
		SELECT id, doctype, data FROM %s.documents
		WHERE status = 'Pending Approval'`, schema)
	var args []interface{}
	if role != "HR/Admin" {
		query += " AND (COALESCE(data->>'location', data->>'location_code') = $1 OR COALESCE(data->>'location', data->>'location_code') IS NULL)"
		args = append(args, location)
	}
	query += " ORDER BY updated_at DESC"

	rows, err := db.DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []map[string]interface{}
	for rows.Next() {
		var id, doctype, dataStr string
		if err := rows.Scan(&id, &doctype, &dataStr); err != nil {
			return nil, err
		}
		var data map[string]interface{}
		_ = json.Unmarshal([]byte(dataStr), &data)
		data["id"] = id
		data["doctype"] = doctype
		results = append(results, data)
	}
	return results, nil
}

// extractAmount pulls a routing amount out of a document's data, checking
// the field names actually used by seeded doctypes (total_amount today;
// extend this list as more doctypes are approval-gated).
func extractAmount(data map[string]interface{}) float64 {
	for _, key := range []string{"total_amount", "amount"} {
		if v, ok := data[key]; ok {
			switch n := v.(type) {
			case float64:
				return n
			case int:
				return float64(n)
			}
		}
	}
	return 0
}
