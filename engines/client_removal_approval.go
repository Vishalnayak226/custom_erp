package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Stage 49.10.6b (4) (docs/security/insider-scenarios.md, "destructive
// operator"): a client's data reset or purge needs a second person.
//
// One admin of the operator's own client asks for it (tenantctl
// request-removal); the request is a ClientDataRemoval record in that client,
// sent through the ordinary approval engine (SubmitForApproval). Another admin
// of the same client approves it in Approvals - the doctype is dual-control,
// so DecideApproval refuses the requester whatever the self-approval setting
// says (APPROV-0840). Only then does `tenantctl reset-data ... -request <id>`
// or `tenantctl purge ... -request <id>` run: ResetTenantData and PurgeTenant
// check the request and spend it inside their own transaction, so it is used
// once, for the client and the act it names, within a week of its approval.
//
// Both people are evidenced: the request, the decision and the reset or
// purge each write a row to the target client's lifecycle trail (which
// outlives a purge) and to the operator client's audit log.
//
// The approver signs in to the app (with its MFA); tenantctl names the
// requester with -actor, which must be an active admin login of that client.
// Someone with the server's shell can still reach the database directly -
// this is the product's own path, as the ledger's triggers are.

// ClientDataRemovalDoctype is the request record.
const ClientDataRemovalDoctype = "ClientDataRemoval"

// The two acts a request can ask for.
const (
	ClientRemovalReset = "reset"
	ClientRemovalPurge = "purge"
)

// Lifecycle evidence of a request and its decision.
const (
	TenantEventRemovalRequested = "removal_requested"
	TenantEventRemovalApproved  = "removal_approved"
	TenantEventRemovalRejected  = "removal_rejected"
)

// ClientRemovalValidDays is how long an approval stays good for.
const ClientRemovalValidDays = 7

func init() {
	RegisterApprovalDecisionHook(ClientDataRemovalDoctype, true, recordClientRemovalDecision)
}

// ClientRemovalTenant is the client the requests live in: the operator's own,
// the first named in ERP_PLATFORM_OPERATORS, else "default" (IsPlatformTenant).
func ClientRemovalTenant() string {
	for _, entry := range strings.Split(os.Getenv(platformOperatorsEnv), ",") {
		if t, _, ok := strings.Cut(strings.TrimSpace(entry), ":"); ok && strings.TrimSpace(t) != "" {
			return strings.ToLower(strings.TrimSpace(t))
		}
	}
	return "default"
}

// RequestClientDataRemoval files a request to reset or purge tenantID and
// sends it for approval. requester is the asking admin's login on the
// operator's client. Returns the request's id.
func RequestClientDataRemoval(tenantID, action, reason, requester string) (string, error) {
	action = strings.ToLower(strings.TrimSpace(action))
	if action != ClientRemovalReset && action != ClientRemovalPurge {
		return "", fmt.Errorf("the action is %q or %q", ClientRemovalReset, ClientRemovalPurge)
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return "", fmt.Errorf("a reason is required and is recorded in the client's evidence trail")
	}
	if _, err := GetTenantLifecycle(tenantID); err != nil {
		return "", err
	}
	home := ClientRemovalTenant()
	schema, err := db.GetTenantSchema(home)
	if err != nil {
		return "", err
	}
	var userID, username, role string
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT id, username, role FROM %s.users WHERE LOWER(username) = LOWER($1) AND status = 'Active'`, schema),
		strings.TrimSpace(requester)).Scan(&userID, &username, &role)
	if err != nil || !HasFullAccess(role) {
		return "", fmt.Errorf("%q is not an active admin login of the operator's client %s - give your own login with -actor; another admin of %s approves it", requester, home, home)
	}
	// The rule the approval engine routes on. Put in place here, in the one
	// client that holds these requests, rather than shipped to every client.
	if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.approval_rules (doctype, min_amount, max_amount, required_role)
		VALUES ($1, 0, NULL, $2) ON CONFLICT (doctype, min_amount) DO NOTHING`, schema), ClientDataRemovalDoctype, RoleSuperAdmin); err != nil {
		return "", err
	}
	id := NewDocID("CDR")
	verb := "Reset"
	if action == ClientRemovalPurge {
		verb = "Purge"
	}
	// name is what Approvals shows the approver: what, to whom, and why.
	label := verb + " " + tenantID + ": " + reason
	if r := []rune(label); len(r) > 120 {
		label = string(r[:117]) + "..."
	}
	raw, err := json.Marshal(map[string]interface{}{
		"name": label, "target_tenant": tenantID, "action": action,
		"reason": reason, "requested_by": username, "status": "Draft",
	})
	if err != nil {
		return "", err
	}
	if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, 'Draft', $4)`, schema),
		id, ClientDataRemovalDoctype, raw, userID); err != nil {
		return "", err
	}
	if err := SubmitForApproval(home, ClientDataRemovalDoctype, id, userID, role); err != nil {
		_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET status = 'Failed' WHERE doctype = $1 AND id = $2`, schema), ClientDataRemovalDoctype, id)
		return "", err
	}
	_ = RecordTenantLifecycleEvent(tenantID, TenantEventRemovalRequested, home+":"+username,
		fmt.Sprintf("%s requested (%s), waiting for another admin of %s to approve it; reason: %s", action, id, home, reason))
	LogAuditEvent(home, userID, "CLIENT_DATA_REMOVAL", "REQUESTED",
		fmt.Sprintf("%s of client %s requested (%s); reason: %s", action, tenantID, id, reason))
	return id, nil
}

// recordClientRemovalDecision evidences the second person's decision.
func recordClientRemovalDecision(tenantID, docID, decision, actorUserID string) {
	if !strings.EqualFold(tenantID, ClientRemovalTenant()) {
		return
	}
	data, _, _, err := fetchDocument(tenantID, ClientDataRemovalDoctype, docID)
	if err != nil {
		return
	}
	target, _ := data["target_tenant"].(string)
	action, _ := data["action"].(string)
	event, verb := TenantEventRemovalApproved, "approved"
	if decision != "Approved" {
		event, verb = TenantEventRemovalRejected, "rejected"
	}
	who := actorUserID
	if schema, err := db.GetTenantSchema(tenantID); err == nil {
		who = clientRemovalUsername(db.DB, schema, actorUserID)
	}
	_ = RecordTenantLifecycleEvent(target, event, tenantID+":"+who, fmt.Sprintf("%s %s (%s)", action, verb, docID))
	LogAuditEvent(tenantID, actorUserID, "CLIENT_DATA_REMOVAL", strings.ToUpper(verb),
		fmt.Sprintf("%s of client %s %s (%s)", action, target, verb, docID))
}

// clientRemovalApproval is a request ResetTenantData or PurgeTenant spent.
type clientRemovalApproval struct {
	ID, RequestedBy, ApprovedBy string
}

func (a clientRemovalApproval) evidence() string {
	return fmt.Sprintf("requested by %s, approved by %s (%s)", a.RequestedBy, a.ApprovedBy, a.ID)
}

// spendClientRemovalApprovalTx checks that requestID asks for this action on
// this client, was asked for by an admin and approved by another, within
// ClientRemovalValidDays, and was not used before - then marks it used in tx,
// so it is spent only if the reset or purge it allowed commits.
func spendClientRemovalApprovalTx(tx *sql.Tx, tenantID, action, requestID, actor string) (clientRemovalApproval, error) {
	home := ClientRemovalTenant()
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return clientRemovalApproval{}, fmt.Errorf("a second person must approve it first: tenantctl request-removal -tenant %s -action %s, approved in Approvals by another admin of %s, then -request <id>", tenantID, action, home)
	}
	schema, err := db.GetTenantSchema(home)
	if err != nil {
		return clientRemovalApproval{}, err
	}
	var raw, status, createdBy string
	if err := tx.QueryRow(fmt.Sprintf(`SELECT data, status, COALESCE(created_by, '') FROM %s.documents WHERE doctype = $1 AND id = $2 FOR UPDATE`, schema),
		ClientDataRemovalDoctype, requestID).Scan(&raw, &status, &createdBy); err != nil {
		return clientRemovalApproval{}, fmt.Errorf("removal request %s is not in %s", requestID, home)
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return clientRemovalApproval{}, err
	}
	if target, _ := data["target_tenant"].(string); target != tenantID {
		return clientRemovalApproval{}, fmt.Errorf("removal request %s is for client %q, not %s", requestID, target, tenantID)
	}
	if act, _ := data["action"].(string); act != action {
		return clientRemovalApproval{}, fmt.Errorf("removal request %s asks for a %s, not a %s", requestID, act, action)
	}
	if status != "Approved" {
		return clientRemovalApproval{}, fmt.Errorf("removal request %s is %s - another admin of %s approves it in Approvals first", requestID, status, home)
	}
	var requester, requesterRole, approver, approverRole string
	var fresh bool
	_ = tx.QueryRow(fmt.Sprintf(`SELECT actor_user_id, actor_role FROM %s.approval_log WHERE doctype = $1 AND document_id = $2 AND action = 'Submitted' ORDER BY id DESC LIMIT 1`, schema),
		ClientDataRemovalDoctype, requestID).Scan(&requester, &requesterRole)
	_ = tx.QueryRow(fmt.Sprintf(`SELECT actor_user_id, actor_role, created_at >= LOCALTIMESTAMP - make_interval(days => $3)
		FROM %s.approval_log WHERE doctype = $1 AND document_id = $2 AND action = 'Approved' ORDER BY id DESC LIMIT 1`, schema),
		ClientDataRemovalDoctype, requestID, ClientRemovalValidDays).Scan(&approver, &approverRole, &fresh)
	if approver == "" || requester == "" || approver == requester || approver == createdBy || !HasFullAccess(approverRole) || !HasFullAccess(requesterRole) {
		return clientRemovalApproval{}, fmt.Errorf("removal request %s was not approved by a second admin", requestID)
	}
	if !fresh {
		return clientRemovalApproval{}, fmt.Errorf("removal request %s was approved more than %d days ago - ask again", requestID, ClientRemovalValidDays)
	}
	data["status"] = "Done"
	data["done_by"] = actor
	marked, err := json.Marshal(data)
	if err != nil {
		return clientRemovalApproval{}, err
	}
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $1, status = 'Done', updated_at = CURRENT_TIMESTAMP WHERE doctype = $2 AND id = $3`, schema),
		marked, ClientDataRemovalDoctype, requestID); err != nil {
		return clientRemovalApproval{}, err
	}
	return clientRemovalApproval{ID: requestID,
		RequestedBy: home + ":" + clientRemovalUsername(tx, schema, requester),
		ApprovedBy:  home + ":" + clientRemovalUsername(tx, schema, approver)}, nil
}

// clientRemovalUsername is a login's username, else its id.
func clientRemovalUsername(q interface {
	QueryRow(string, ...interface{}) *sql.Row
}, schema, userID string) string {
	var name string
	if q.QueryRow(fmt.Sprintf(`SELECT username FROM %s.users WHERE id = $1`, schema), userID).Scan(&name) == nil && name != "" {
		return name
	}
	return userID
}
