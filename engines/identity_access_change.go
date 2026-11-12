package engines

import (
	"context"
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"strings"
)

// Stage 49.2.1 (joiner / mover / leaver approval). One more use of the
// maker-checker engine, behind security.identity_change_approval (Off by
// default, so nothing changes until an admin turns it on):
//
//   - Joiner: the new user is created Inactive and the request activates it on
//     approval. The password hash never goes into the request.
//   - Mover: a store (location) change is held and applied on approval.
//   - Leaver: never delayed. The account is already disabled (identity_leaver.go);
//     the request is a second person's review of it, and a rejection is recorded
//     but does not turn the account back on - reinstating is a deliberate act
//     from Users.
//
// The doctype is dual control: whoever asked can never decide it.

const (
	AccessChangeRequestDoctype = "AccessChangeRequest"
	// SettingIdentityChangeApproval switches the joiner / mover / leaver approval on.
	SettingIdentityChangeApproval = "security.identity_change_approval"

	AccessChangeJoiner = "Joiner"
	AccessChangeMover  = "Mover"
	AccessChangeLeaver = "Leaver"
)

func init() {
	RegisterApprovalDecisionHook(AccessChangeRequestDoctype, true, applyAccessChangeDecision)
}

// IdentityChangeApprovalOn reports whether joiners and movers wait for approval.
func IdentityChangeApprovalOn(tenantID string) bool {
	return GetSettingBool(tenantID, SettingIdentityChangeApproval)
}

// RequestAccessChange files and submits a request. detail is what Approvals
// shows; location is the mover's new store. Returns the request id.
func RequestAccessChange(tenantID, kind, targetUserID, detail, location, actorUserID, actorRole string) (string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	var username string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT username FROM %s.users WHERE id = $1`, schema), targetUserID).Scan(&username); err != nil {
		return "", &ValidationError{Code: "GLOBAL-0002", Message: "user not found"}
	}
	id := NewDocID("ACR")
	raw, err := json.Marshal(map[string]interface{}{
		"name": kind + ": " + username + " - " + detail, "kind": kind,
		"target_user_id": targetUserID, "target_username": username,
		"detail": detail, "location_code": location, "requested_by": actorUserID, "status": "Draft",
	})
	if err != nil {
		return "", err
	}
	if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, 'Draft', $4)`, schema),
		id, AccessChangeRequestDoctype, raw, actorUserID); err != nil {
		return "", err
	}
	if err := SubmitForApproval(tenantID, AccessChangeRequestDoctype, id, actorUserID, actorRole); err != nil {
		_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET status = 'Failed' WHERE doctype = $1 AND id = $2`, schema), AccessChangeRequestDoctype, id)
		return "", err
	}
	return id, nil
}

// recordLeaverForReview files the review of an immediate disable when the
// approval setting is on. Best effort: it never undoes or delays the disable.
func recordLeaverForReview(ctx context.Context, tenantID, userID, actor, detail string) {
	if !IdentityChangeApprovalOn(tenantID) {
		return
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return
	}
	var actorID, actorRole string
	if actor != "" && actor != "system" {
		_ = db.DB.QueryRow(fmt.Sprintf(`SELECT id, COALESCE(role, '') FROM %s.users WHERE username = $1 OR id = $1 LIMIT 1`, schema), actor).Scan(&actorID, &actorRole)
	}
	if actorID == "" {
		return // a system disable has no maker to put in the queue
	}
	if _, err := RequestAccessChange(tenantID, AccessChangeLeaver, userID, detail+" - already disabled, for review", "", actorID, actorRole); err != nil {
		LogSystemError(tenantID, actorID, "Low", "User Access & Security", "leaver review not filed for "+userID+": "+err.Error(), "")
	}
}

// applyAccessChangeDecision runs after a decision is saved.
func applyAccessChangeDecision(tenantID, docID, decision, actorUserID string) {
	data, _, _, err := fetchDocument(tenantID, AccessChangeRequestDoctype, docID)
	if err != nil {
		return
	}
	kind, _ := data["kind"].(string)
	target, _ := data["target_user_id"].(string)
	username, _ := data["target_username"].(string)
	approved := decision == "Approved"
	switch {
	case approved && kind == AccessChangeJoiner:
		if schema, e := db.GetTenantSchema(tenantID); e == nil {
			MarkUserReactivated(schema, target)
			if _, e := db.DB.Exec(fmt.Sprintf(`UPDATE %s.users SET status = 'Active' WHERE id = $1 AND status = 'Inactive'`, schema), target); e == nil {
				InvalidateLiveUserState(tenantID, target)
				LogAuditEvent(tenantID, actorUserID, "USER_MANAGEMENT", "USER_STATUS_CHANGED",
					fmt.Sprintf("Set user %s status to Active - joiner approved (%s)", target, docID))
			}
		}
	case approved && kind == AccessChangeMover:
		loc, _ := data["location_code"].(string)
		if schema, e := db.GetTenantSchema(tenantID); e == nil && strings.TrimSpace(loc) != "" {
			if _, e := db.DB.Exec(fmt.Sprintf(`UPDATE %s.users SET location_code = $1 WHERE id = $2`, schema), loc, target); e == nil {
				InvalidateLiveUserState(tenantID, target)
				LogAuditEvent(tenantID, actorUserID, "USER_MANAGEMENT", "USER_LOCATION_CHANGED",
					fmt.Sprintf("Set user %s location to %s - mover approved (%s)", target, loc, docID))
			}
		}
	}
	verb := "approved"
	if !approved {
		verb = "rejected"
	}
	LogAuditEvent(tenantID, actorUserID, "USER_MANAGEMENT", "ACCESS_CHANGE_"+strings.ToUpper(verb),
		fmt.Sprintf("%s request for %s %s (%s)", strings.ToLower(kind), username, verb, docID))
}
