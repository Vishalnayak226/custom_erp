package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ErrApprovalRoleMismatch (Stage 25 Batch 3) wraps DecideApproval's
// role-mismatch failure so a caller can distinguish it from every other
// reason DecideApproval can fail (document not found, wrong status,
// maker-checker violation) without string-matching the error text. Doctype-
// specific callers - e.g. handleDecideApproval mapping this to PURCHA-0083
// for PurchaseOrder - check errors.Is against this sentinel; every other
// doctype's DecideApproval call is unaffected, since none of them look for it.
var ErrApprovalRoleMismatch = errors.New("approver role does not match the amount's required approval role")

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

// UpsertApprovalRule (24.8) creates or edits one approval_rules row, with a
// save-time overlap check the table itself never had: two rules for the
// same doctype whose [min_amount, max_amount] ranges intersect would
// otherwise silently coexist, and requiredApproverRole's own lookup
// (`ORDER BY min_amount DESC LIMIT 1`) would just pick the higher-min_amount
// one with no warning that the lower one is now partly unreachable.
// ruleID == nil creates a new rule; non-nil edits that row (and excludes it
// from its own overlap check). This is also, incidentally, the first way to
// manage approval_rules through the API at all - previously only reachable
// via a direct SQL migration insert, unlike prefix_configs/feature_flags'
// existing self-service pattern this table's own comment says it follows.
func UpsertApprovalRule(tenantID, doctype string, minAmount float64, maxAmount *float64, requiredRole string, ruleID *int) (int, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, err
	}
	if doctype == "" || requiredRole == "" {
		return 0, fmt.Errorf("doctype and required_role are required")
	}
	if maxAmount != nil && *maxAmount < minAmount {
		return 0, fmt.Errorf("max_amount cannot be less than min_amount")
	}

	excludeID := -1
	if ruleID != nil {
		excludeID = *ruleID
	}
	var overlapping int
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT id FROM %s.approval_rules
		WHERE doctype = $1 AND id != $2
		  AND ($3::numeric IS NULL OR min_amount <= $3)
		  AND (max_amount IS NULL OR max_amount >= $4)
		LIMIT 1`, schema), doctype, excludeID, maxAmount, minAmount).Scan(&overlapping)
	if err == nil {
		return 0, fmt.Errorf("this range overlaps existing approval rule id %d for %s", overlapping, doctype)
	} else if err != sql.ErrNoRows {
		return 0, err
	}

	if ruleID != nil {
		_, err = db.DB.Exec(fmt.Sprintf(`
			UPDATE %s.approval_rules SET min_amount = $1, max_amount = $2, required_role = $3
			WHERE id = $4`, schema), minAmount, maxAmount, requiredRole, *ruleID)
		return *ruleID, err
	}
	var newID int
	err = db.DB.QueryRow(fmt.Sprintf(`
		INSERT INTO %s.approval_rules (doctype, min_amount, max_amount, required_role)
		VALUES ($1, $2, $3, $4) RETURNING id`, schema), doctype, minAmount, maxAmount, requiredRole).Scan(&newID)
	return newID, err
}

// DeleteApprovalRule (Stage 26.3.3) removes one amount-slab routing rule -
// the admin screen's counterpart to UpsertApprovalRule, since a config
// screen that can create/edit rules but never retire a mistaken one isn't
// genuinely usable. Deleting the last rule for a doctype simply makes that
// doctype ungated again, the same as it would be if a rule had never been
// added (requiredApproverRole/IsApprovalGated already treat zero rows that way).
func DeleteApprovalRule(tenantID string, ruleID int) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	res, err := db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.approval_rules WHERE id = $1`, schema), ruleID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("approval rule %d not found", ruleID)
	}
	return nil
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

// RequiredApproverRoleForAmount is the exported form of requiredApproverRole,
// for callers outside this package that need to decide *whether* to route a
// document through approval before creating it (e.g. handleCheckout's
// discount gate, Stage 20.10) rather than after the fact via SubmitForApproval.
func RequiredApproverRoleForAmount(tenantID, doctype string, amount float64) (string, error) {
	return requiredApproverRole(tenantID, doctype, amount)
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
		// ADMINC-0032 (Stage 25.5): "Approval workflow missing" - an exact
		// scenario match for a doctype with no approval_rules row to route
		// this submission to.
		return &ValidationError{Code: "ADMINC-0032", Message: fmt.Sprintf("%s has no approval rule configured - nothing to route this to", doctype)}
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
//
// DecideApproval is wrapped in a single transaction that locks the document row
// (SELECT ... FOR UPDATE) for its whole duration, mirroring the same pattern
// engines/inventory.go uses to prevent oversell. Without this, two concurrent
// decide calls on the same document (a double-click, a retry, or two different
// checkers racing) could both read status="Pending Approval" before either
// commits, so both would pass every check and both would write a decision -
// producing duplicate approval_log rows for what's meant to be a single
// irreversible gate, with the final status determined by write-order rather
// than being deterministically the first decision received.
func DecideApproval(tenantID, doctype, docID, actorUserID, actorRole, actorLocation, decision, comment string) error {
	if decision != "Approved" && decision != "Rejected" {
		return fmt.Errorf("decision must be 'Approved' or 'Rejected'")
	}
	// APPROV-0159 (Stage 25.5): "Reject reason missing" - a brand new
	// check, decision=Rejected previously accepted an empty comment with no
	// enforcement at all.
	if decision == "Rejected" && strings.TrimSpace(comment) == "" {
		return &ValidationError{Code: "APPROV-0159", Message: "a comment is required to reject a document"}
	}

	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var dataStr, status, createdBy string
	err = tx.QueryRow(fmt.Sprintf(`
		SELECT data, status, created_by FROM %s.documents
		WHERE doctype = $1 AND id = $2 FOR UPDATE`, schema), doctype, docID).Scan(&dataStr, &status, &createdBy)
	if err != nil {
		return fmt.Errorf("document not found: %v", err)
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return err
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
		return fmt.Errorf("%w: this amount requires approval from role '%s'", ErrApprovalRoleMismatch, requiredRole)
	}
	if actorRole != "HR/Admin" {
		if docLoc, ok := data["location"].(string); ok && docLoc != "" && docLoc != actorLocation {
			return fmt.Errorf("this document belongs to a different location")
		}
	}

	data["status"] = decision
	marshaled, err := json.Marshal(data)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(fmt.Sprintf(`
		UPDATE %s.documents SET data = $1, status = $2, updated_at = CURRENT_TIMESTAMP
		WHERE doctype = $3 AND id = $4`, schema), marshaled, decision, doctype, docID); err != nil {
		return err
	}
	if _, err := tx.Exec(fmt.Sprintf(`
		INSERT INTO %s.approval_log (doctype, document_id, action, actor_user_id, actor_role, amount, comment)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`, schema), doctype, docID, decision, actorUserID, actorRole, amount, comment); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	// Stage 26.4.6: snapshot the just-approved ProductContent so a later bad
	// edit can be rolled back to this known-good copy. Best-effort and
	// outside the transaction above (the approval itself must never fail
	// because a snapshot write failed) - same "skip rather than fail the
	// whole operation" precedent ListWorkbench already sets.
	if doctype == "ProductContent" && decision == "Approved" {
		snapshotProductContentVersion(tenantID, docID, data, actorUserID)
	}

	// Stage 26.4.10: a QC-approved supplier submission becomes ProductContent
	// - as a Draft, so it still has to clear ProductContent's own approval
	// gate before anything a supplier wrote can reach a live channel.
	if doctype == "SupplierSubmission" && decision == "Approved" {
		ApplyApprovedSupplierSubmission(tenantID, docID, data, actorUserID)
	}

	// Stage 26.6.4: an Approved JournalVoucher posts through PostDoubleEntry
	// here rather than waiting for a separate explicit "post" action - the
	// approval decision itself is the authorization to post.
	if doctype == "JournalVoucher" && decision == "Approved" {
		postApprovedJournalVoucher(tenantID, docID)
	}

	// Stage 26.7.5: an Approved LoyaltyRedemptionRequest (a staff-restricted
	// large burn that VerifyAndRedeemLoyaltyOTP routed here instead of
	// redeeming immediately) actually burns the points here.
	if doctype == "LoyaltyRedemptionRequest" && decision == "Approved" {
		executeApprovedLoyaltyRedemption(tenantID, docID, data)
	}

	return nil
}

// ApprovalLogEntry is one row of a document's approval history - submitted/
// approved/rejected/modified, with whatever comment the actor gave (a
// rejection's comment is mandatory, APPROV-0159).
type ApprovalLogEntry struct {
	Action    string `json:"action"`
	ActorUser string `json:"actor_user_id"`
	ActorRole string `json:"actor_role"`
	Comment   string `json:"comment"`
	CreatedAt string `json:"created_at"`
}

// ListApprovalLog (Stage 26.4.5) surfaces one document's approval_log
// history - in particular, a rejection's mandatory comment, which until now
// was only ever visible by querying the table directly. No new storage:
// approval_log already captures this on every Submit/Approve/Reject/Modified
// action (Stage 13.8).
func ListApprovalLog(tenantID, doctype, docID string) ([]ApprovalLogEntry, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT action, actor_user_id, actor_role, COALESCE(comment, ''), created_at::text
		FROM %s.approval_log WHERE doctype = $1 AND document_id = $2 ORDER BY created_at ASC`, schema), doctype, docID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []ApprovalLogEntry{}
	for rows.Next() {
		var e ApprovalLogEntry
		if err := rows.Scan(&e.Action, &e.ActorUser, &e.ActorRole, &e.Comment, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// BulkDecideApproval (Stage 26.4.6) applies one approve/reject decision to a
// bounded selection of Pending Approval documents. Each DecideApproval call
// is already its own atomic, row-locked transaction, so this is a tolerant
// loop rather than one all-or-nothing transaction: one document failing a
// check (e.g. a maker-checker violation) shouldn't block the other selected
// documents from being decided, mirroring BulkUpdateDocuments' own
// partial-failure reporting shape (PIM-0235) rather than introducing a
// second one.
func BulkDecideApproval(tenantID, doctype string, docIDs []string, actorUserID, actorRole, actorLocation, decision, comment string) (succeeded []string, failed map[string]string, err error) {
	if len(docIDs) == 0 {
		return nil, nil, fmt.Errorf("select at least one document")
	}
	maxBulkDocs := maxPIMBulkEditDocumentsFor(tenantID)
	if len(docIDs) > maxBulkDocs {
		return nil, nil, fmt.Errorf("bulk approval supports at most %d documents at a time", maxBulkDocs)
	}
	succeeded = []string{}
	failed = map[string]string{}
	for _, docID := range docIDs {
		if decErr := DecideApproval(tenantID, doctype, docID, actorUserID, actorRole, actorLocation, decision, comment); decErr != nil {
			failed[docID] = decErr.Error()
		} else {
			succeeded = append(succeeded, docID)
		}
	}
	return succeeded, failed, nil
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
		if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
			// 24.18: a nil map from a failed unmarshal would otherwise panic
			// on the assignment just below ("assignment to entry in nil
			// map") - log and skip this row rather than crashing the whole
			// approvals inbox for every other pending document.
			LogSystemError(tenantID, "", "ERROR", "ListPendingApprovals", fmt.Sprintf("corrupt data for %s %s: %v", doctype, id, err), "")
			continue
		}
		data["id"] = id
		data["doctype"] = doctype
		results = append(results, data)
	}
	return results, nil
}

// extractAmount pulls a routing amount out of a document's data, checking
// the field names actually used by seeded doctypes (total_amount today;
// extend this list as more doctypes are approval-gated). "discount_amount"
// is POSCart-specific (Stage 20.10): its approval_rules row routes on
// discount percentage, not the cart's rupee total, so handleCheckout stores
// the discount percentage under this key rather than "amount"/"total_amount"
// to avoid colliding with those keys' rupee-amount meaning on other doctypes.
// "variance_qty" is CycleCountLine-specific (Stage 20.22): routes on the
// absolute quantity variance, not a rupee amount. "invoice_amount" is
// VendorInvoice-specific (24.11's override-approval routing) - without it,
// every VendorInvoice decision would route/log as amount 0 regardless of
// the real invoice_amount, which happens to still pick the right role
// today only because this stage seeded a single flat 0..NULL rule for the
// doctype - a tiered slab would route every override to the lowest tier.
// "points_value" is LoyaltyRedemptionRequest-specific (Stage 26.7.5): routes
// on the redemption's rupee value (points * redemptionValuePerPoint), not a
// generic "amount"/"total_amount", to avoid colliding with those keys'
// meaning on other doctypes the same way "discount_amount"/"variance_qty"
// already don't.
func extractAmount(data map[string]interface{}) float64 {
	for _, key := range []string{"total_amount", "amount", "discount_amount", "variance_qty", "invoice_amount", "points_value"} {
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
