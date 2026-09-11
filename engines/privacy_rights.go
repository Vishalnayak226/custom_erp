package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"time"
)

// Stage 49.6.8 - privacy rights and lifecycle: a generic, evidenced request
// lifecycle for data-subject rights (access/export/correction/erasure/
// anonymization/consent-withdrawal), plus a concrete, tested erasure/
// anonymization implementation for Customer - the doctype every deployment
// of this product actually holds real personal data on
// (engines/sensitive_fields.go's SensitiveCategoryPersonal).
//
// Scope, stated honestly rather than implied: this closes the REQUEST
// LIFECYCLE (create, maker-checker decide, execute, evidence, legal-hold
// refusal) and ONE concrete data flow. It does not sweep every table, the
// search index, background jobs, log files or backups for a subject's data -
// 49.6.8's full acceptance bar ("across primary tables, search/index, jobs,
// files, logs, audit and backups") remains open beyond Customer; see
// AnonymizeCustomer/ExportCustomerData's own doc comments for exactly what
// each does and does not reach, and ExecuteDataSubjectRequest for what
// happens when a request names any other doctype (a clear error, not a
// silent no-op).
//
// The refuse-with-recorded-reason shape mirrors
// engines/tenant_lifecycle.go's PurgeTenant exactly (Stage 49.1.5): a
// statutory conflict (legal hold here) is SURFACED as a refusal with its
// reason, not silently resolved in either direction - 49.6.8's own wording.
// Anonymization, not hard deletion, is the erasure mechanism: Sales/Invoice/
// Return documents reference customer_id and must survive for statutory
// financial retention (GL, GST), so deleting the row would either orphan
// those records or force deleting them too - neither of which happens here.

const (
	PrivacyRequestAccess          = "access"
	PrivacyRequestExport          = "export"
	PrivacyRequestCorrection      = "correction"
	PrivacyRequestErasure         = "erasure"
	PrivacyRequestAnonymize       = "anonymize"
	PrivacyRequestConsentWithdraw = "consent_withdraw"
)

const (
	PrivacyRequestPending   = "Pending"
	PrivacyRequestApproved  = "Approved"
	PrivacyRequestDenied    = "Denied"
	PrivacyRequestCompleted = "Completed"
)

var validPrivacyRequestTypes = map[string]bool{
	PrivacyRequestAccess: true, PrivacyRequestExport: true, PrivacyRequestCorrection: true,
	PrivacyRequestErasure: true, PrivacyRequestAnonymize: true, PrivacyRequestConsentWithdraw: true,
}

// DataSubjectRequest is one row of the tenant's data_subject_requests table -
// the evidence trail for a privacy-rights request from open to close.
type DataSubjectRequest struct {
	ID             string
	SubjectDoctype string
	SubjectID      string
	RequestType    string
	Status         string
	Reason         string
	RequestedBy    string
	RequestedAt    time.Time
	DecidedBy      string
	DecisionNote   string
	CompletedAt    *time.Time
}

// RequestDataSubjectAction opens a new pending request. Recording the
// request itself - before anything executes - is what makes "who asked, and
// when" reconstructable even if the request is later denied.
func RequestDataSubjectAction(tenantID, subjectDoctype, subjectID, requestType, reason, requestedBy string) (string, error) {
	if !validPrivacyRequestTypes[requestType] {
		return "", fmt.Errorf("unknown data subject request type %q", requestType)
	}
	if subjectDoctype == "" || subjectID == "" {
		return "", fmt.Errorf("subject doctype and id are required")
	}
	if requestedBy == "" {
		return "", fmt.Errorf("requested_by is required")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	id := NewDocID("DSR")
	_, err = db.DB.Exec(fmt.Sprintf(
		`INSERT INTO %s.data_subject_requests (id, subject_doctype, subject_id, request_type, status, reason, requested_by)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`, schema),
		id, subjectDoctype, subjectID, requestType, PrivacyRequestPending, reason, requestedBy)
	if err != nil {
		return "", err
	}
	LogAuditEvent(tenantID, requestedBy, "data_subject_request_created", "Success",
		fmt.Sprintf("%s request %s opened for %s %s: %s", requestType, id, subjectDoctype, subjectID, reason))
	return id, nil
}

// DecideDataSubjectRequest is the maker-checker gate: whoever decides a
// request must be a different account from whoever raised it. This is a
// distinct, lighter-weight mechanism than engines/approval.go's
// amount-threshold approval_rules engine - a privacy request has no
// monetary amount to gate on - but the same separation-of-duties principle
// this codebase already applies to approvals.
func DecideDataSubjectRequest(tenantID, requestID, decidedBy string, approve bool, note string) error {
	if decidedBy == "" {
		return fmt.Errorf("decided_by is required")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	req, err := getDataSubjectRequest(tenantID, requestID)
	if err != nil {
		return err
	}
	if req.Status != PrivacyRequestPending {
		return fmt.Errorf("request %s is %s, not pending", requestID, req.Status)
	}
	if decidedBy == req.RequestedBy {
		return fmt.Errorf("the requester cannot also decide their own data subject request (maker-checker)")
	}
	status := PrivacyRequestDenied
	if approve {
		status = PrivacyRequestApproved
	}
	if _, err := db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.data_subject_requests SET status = $1, decided_by = $2, decided_at = CURRENT_TIMESTAMP, decision_note = $3 WHERE id = $4`, schema),
		status, decidedBy, note, requestID); err != nil {
		return err
	}
	LogAuditEvent(tenantID, decidedBy, "data_subject_request_decided", "Success",
		fmt.Sprintf("request %s %s: %s", requestID, status, note))
	return nil
}

// ExecuteDataSubjectRequest runs an approved request and marks it completed.
// Only Customer erasure/anonymize/export/access is implemented today -
// correction goes through the doctype's ordinary edit path instead of a
// bespoke one here, and consent-withdrawal's concrete effect is
// [needs decision: which marketing/communication flag it should clear -
// no such flag exists on Customer yet], so both are recorded as completed
// with no automated side effect rather than invented. Any other subject
// doctype is a clear, named gap, not a silent no-op.
func ExecuteDataSubjectRequest(tenantID, requestID, actor string) error {
	req, err := getDataSubjectRequest(tenantID, requestID)
	if err != nil {
		return err
	}
	if req.Status != PrivacyRequestApproved {
		return fmt.Errorf("request %s is %s, not approved", requestID, req.Status)
	}
	if req.SubjectDoctype != "Customer" {
		return fmt.Errorf("execution is not implemented for doctype %q yet - only Customer erasure/anonymize/export/access is built (Stage 49.6.8)", req.SubjectDoctype)
	}
	switch req.RequestType {
	case PrivacyRequestErasure, PrivacyRequestAnonymize:
		if err := AnonymizeCustomer(tenantID, req.SubjectID, actor, req.Reason); err != nil {
			return err
		}
	case PrivacyRequestExport, PrivacyRequestAccess:
		if _, err := ExportCustomerData(tenantID, req.SubjectID); err != nil {
			return err
		}
	case PrivacyRequestCorrection, PrivacyRequestConsentWithdraw:
		// No automated action - see doc comment above.
	default:
		return fmt.Errorf("unknown request type %q", req.RequestType)
	}
	return markDataSubjectRequestCompleted(tenantID, requestID, actor)
}

func markDataSubjectRequestCompleted(tenantID, requestID, actor string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	if _, err := db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.data_subject_requests SET status = $1, completed_at = CURRENT_TIMESTAMP WHERE id = $2`, schema),
		PrivacyRequestCompleted, requestID); err != nil {
		return err
	}
	LogAuditEvent(tenantID, actor, "data_subject_request_completed", "Success", fmt.Sprintf("request %s completed", requestID))
	return nil
}

func getDataSubjectRequest(tenantID, requestID string) (DataSubjectRequest, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return DataSubjectRequest{}, err
	}
	var r DataSubjectRequest
	err = db.DB.QueryRow(fmt.Sprintf(
		`SELECT id, subject_doctype, subject_id, request_type, status, COALESCE(reason,''), requested_by, requested_at, COALESCE(decided_by,''), COALESCE(decision_note,''), completed_at
		 FROM %s.data_subject_requests WHERE id = $1`, schema), requestID).
		Scan(&r.ID, &r.SubjectDoctype, &r.SubjectID, &r.RequestType, &r.Status, &r.Reason, &r.RequestedBy, &r.RequestedAt, &r.DecidedBy, &r.DecisionNote, &r.CompletedAt)
	if err != nil {
		return DataSubjectRequest{}, fmt.Errorf("data subject request %s not found: %v", requestID, err)
	}
	return r, nil
}

// ListDataSubjectRequests returns every request for one subject, newest
// first - the evidence an operator or a DPDP/GDPR audit needs to show a
// request was raised, decided and completed, not silently dropped.
func ListDataSubjectRequests(tenantID, subjectDoctype, subjectID string) ([]DataSubjectRequest, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(
		`SELECT id, subject_doctype, subject_id, request_type, status, COALESCE(reason,''), requested_by, requested_at, COALESCE(decided_by,''), COALESCE(decision_note,''), completed_at
		 FROM %s.data_subject_requests WHERE subject_doctype = $1 AND subject_id = $2 ORDER BY requested_at DESC`, schema),
		subjectDoctype, subjectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DataSubjectRequest
	for rows.Next() {
		var r DataSubjectRequest
		if err := rows.Scan(&r.ID, &r.SubjectDoctype, &r.SubjectID, &r.RequestType, &r.Status, &r.Reason, &r.RequestedBy, &r.RequestedAt, &r.DecidedBy, &r.DecisionNote, &r.CompletedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

// SetSubjectLegalHold places or lifts a legal hold on any document, keyed by
// doctype - the same ad-hoc JSONB-field convention this codebase already
// uses for fields with no dedicated column (Item.cost_price, etc, per
// sensitive_fields.go's own header note), so it applies to any doctype with
// no per-doctype migration required. AnonymizeCustomer refuses while this is
// set, mirroring PurgeTenant's legal-hold refusal exactly.
func SetSubjectLegalHold(tenantID, doctype, subjectID string, on bool, actor, reason string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	patch, err := json.Marshal(map[string]interface{}{"legal_hold": on, "legal_hold_reason": reason})
	if err != nil {
		return err
	}
	res, err := db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = data || $1::jsonb, updated_at = CURRENT_TIMESTAMP WHERE doctype = $2 AND id = $3`, schema),
		patch, doctype, subjectID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("%s %s not found", doctype, subjectID)
	}
	event := "legal_hold_cleared"
	if on {
		event = "legal_hold_set"
	}
	LogAuditEvent(tenantID, actor, event, "Success", fmt.Sprintf("%s %s: %s (reason: %s)", doctype, subjectID, event, reason))
	return nil
}

// AnonymizeCustomer is the concrete erasure/anonymization implementation for
// Customer. It does NOT hard-delete the row - see this file's header for
// why - it replaces the PII fields with a fixed placeholder and clears the
// rest, leaving the row's id (and therefore every Sales/Invoice/Return
// reference to it) intact.
//
// Refuses, and records the refusal exactly like PurgeTenant, when the
// customer is under legal hold. [needs decision: whether an OPEN
// (unpaid/unfulfilled) order should ALSO block anonymization - this
// function does not check that today, so a name/phone on an in-flight order
// is anonymized along with everything else. A product/legal call on this
// specific conflict is needed before broadening the refusal set further.]
func AnonymizeCustomer(tenantID, customerID, actor, reason string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var raw []byte
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents WHERE doctype = 'Customer' AND id = $1`, schema), customerID).Scan(&raw); err != nil {
		return fmt.Errorf("customer %s not found: %v", customerID, err)
	}
	var data map[string]interface{}
	if err := json.Unmarshal(raw, &data); err != nil {
		return err
	}
	if hold, _ := data["legal_hold"].(bool); hold {
		heldReason, _ := data["legal_hold_reason"].(string)
		refusal := fmt.Sprintf("refused: customer %s is under legal hold (%s)", customerID, heldReason)
		LogAuditEvent(tenantID, actor, "data_subject_erasure_refused", "Refused", refusal)
		return fmt.Errorf("%s", refusal)
	}

	patch, err := json.Marshal(map[string]interface{}{
		"name": "Redacted Customer", "phone": "", "email": "", "date_of_birth": nil,
		"anonymized_at": time.Now().UTC().Format(time.RFC3339), "anonymized_by": actor,
	})
	if err != nil {
		return err
	}
	if _, err := db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = data || $1::jsonb, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'Customer' AND id = $2`, schema),
		patch, customerID); err != nil {
		return err
	}
	LogAuditEvent(tenantID, actor, "data_subject_erasure_completed", "Success",
		fmt.Sprintf("customer %s anonymized (reason: %s)", customerID, reason))
	return nil
}

// ExportCustomerData answers the access/portability right for the Customer
// master record itself. It does NOT sweep Sales/Invoice/Return history,
// loyalty ledger entries, search index rows, background job payloads, log
// lines or backups that also mention this customer - that full-system sweep
// is the part of 49.6.8's acceptance bar this session leaves open; see this
// file's header.
func ExportCustomerData(tenantID, customerID string) (map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	var raw []byte
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents WHERE doctype = 'Customer' AND id = $1`, schema), customerID).Scan(&raw); err != nil {
		return nil, fmt.Errorf("customer %s not found: %v", customerID, err)
	}
	var data map[string]interface{}
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, err
	}
	return data, nil
}
