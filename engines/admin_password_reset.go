package engines

import (
	"crypto/rand"
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// Stage 49.2.4 (last two open clauses) - admin-assisted "helpdesk" password
// reset. Before this there was no admin-driven way to reset another user's
// password at all - only self-service change (handleChangePassword) and
// self-service emailed-token reset (24.28, engines/password_reset.go). An
// ordinary target is still a single, fully audited admin action - the same
// shape handleAdminResetUserMFA already established for MFA. A PRIVILEGED
// target (a Super Admin account) instead requires a second, different Super
// Admin to approve first, via the same maker-checker machinery
// (approval_rules + SubmitForApproval/DecideApproval) every other high-risk
// doctype in this codebase already rides - see
// db/migrations_stage49_2_4_admin_password_reset.sql for the seeded rule.
//
// The generated one-time password is handed back to exactly one operator:
// the sole actor in the ungated case, or the APPROVER (never the requester)
// in the dual-control case - the whole point of requiring a second admin is
// that one admin alone cannot both request and learn a privileged
// credential.

// PasswordResetResult is what a caller needs to tell the operator what just
// happened.
type PasswordResetResult struct {
	Immediate    bool
	DocumentID   string
	TempPassword string
	Detail       string
}

// tempPasswordAlphabet reuses the exact same unambiguous 32-character set
// engines/mfa_recovery.go's recovery codes use (no 0/O/1/I) - these
// passwords get read aloud or typed over a phone call, which is the whole
// point of avoiding characters that are easy to mis-hear or mis-type.
func generateTemporaryPassword() (string, error) {
	raw := make([]byte, 20)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	var sb strings.Builder
	for _, b := range raw {
		sb.WriteByte(recoveryCodeAlphabet[int(b)%len(recoveryCodeAlphabet)])
	}
	return sb.String(), nil
}

func lookupPasswordResetTarget(tenantID, targetUserID string) (username, role, email string, err error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", "", "", err
	}
	err = db.DB.QueryRow(fmt.Sprintf(
		`SELECT username, role, COALESCE(email, '') FROM %s.users WHERE id = $1`, schema), targetUserID).
		Scan(&username, &role, &email)
	if err != nil {
		return "", "", "", fmt.Errorf("target user not found")
	}
	return username, role, email, nil
}

// executePasswordReset is the one place that actually mints a new one-time
// password and installs it - called either immediately (ungated) or from
// ExecuteApprovedPasswordResetRequest (dual control, after approval).
func executePasswordReset(tenantID, targetUserID, targetUsername, targetEmail string) (string, error) {
	tempPassword, err := generateTemporaryPassword()
	if err != nil {
		return "", err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(tempPassword), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	// credential_version + 1 revokes every session already open on this
	// account, the same mechanism 49.2.4's self-service change/reset already
	// use - a password an admin just had to reset is exactly the situation
	// where any standing session (possibly the attacker's) must not survive.
	if _, err := db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.users SET password_hash = $1, credential_version = credential_version + 1 WHERE id = $2`, schema),
		string(hash), targetUserID); err != nil {
		return "", err
	}
	InvalidateLiveUserState(tenantID, targetUserID)
	SendPasswordChangedNotice(tenantID, targetEmail, targetUsername, "an administrator-issued password reset")
	return tempPassword, nil
}

func passwordResetDetail(targetUsername, tempPassword string) string {
	return fmt.Sprintf(
		"One-time password for %s: %s\n\nGive this to them through a secure channel now - it will not be shown again, and it has already replaced their password.",
		targetUsername, tempPassword)
}

// RequestAdminPasswordReset is the one entry point handleRequestPasswordReset
// calls. actorRole must already be a Super Admin - the caller gates that
// itself (requireHRAdmin), matching every other admin-identity handler.
func RequestAdminPasswordReset(tenantID, actorUserID, actorRole, targetUserID, reason string) (*PasswordResetResult, error) {
	targetUsername, targetRole, targetEmail, err := lookupPasswordResetTarget(tenantID, targetUserID)
	if err != nil {
		return nil, err
	}
	privileged := IsSuperAdmin(targetRole)
	reason = strings.TrimSpace(reason)
	if privileged && reason == "" {
		return nil, &ValidationError{Code: "GLOBAL-0002", SubFor: "reason",
			Message: "a reason is required to reset a privileged (Super Admin) account's password"}
	}

	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	docID := NewDocID("PWDRST")

	if !privileged {
		tempPassword, execErr := executePasswordReset(tenantID, targetUserID, targetUsername, targetEmail)
		if execErr != nil {
			return nil, execErr
		}
		payload := map[string]interface{}{
			"target_user_id": targetUserID, "target_username": targetUsername, "target_role": targetRole,
			"reason": reason, "status": "Approved", "approved_by": actorUserID,
		}
		if marshaled, marshalErr := json.Marshal(payload); marshalErr == nil {
			if _, insErr := db.DB.Exec(fmt.Sprintf(
				`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'PasswordResetRequest', $2, 'Approved', $3)`, schema),
				docID, marshaled, actorUserID); insErr != nil {
				// The reset itself already succeeded - a failure to write the
				// evidence row must not be reported as a failed reset, only logged.
				LogSystemError(tenantID, actorUserID, "Low", "User Access & Security",
					fmt.Sprintf("password reset executed for %s but evidence row %s failed to write: %v", targetUsername, docID, insErr), "")
			}
		}
		LogAuditEvent(tenantID, actorUserID, "USER_MANAGEMENT", "PASSWORD_RESET_BY_ADMIN",
			fmt.Sprintf("Password reset for %s (reason: %s)", targetUsername, orNoneGiven(reason)))
		return &PasswordResetResult{Immediate: true, DocumentID: docID, TempPassword: tempPassword,
			Detail: passwordResetDetail(targetUsername, tempPassword)}, nil
	}

	// Dual control: park as Draft, then submit - DecideApproval's own
	// maker-checker check is what actually forces a second, different Super
	// Admin; executePasswordReset does not run until that approval lands.
	payload := map[string]interface{}{
		"target_user_id": targetUserID, "target_username": targetUsername, "target_role": targetRole,
		"reason": reason, "status": "Draft",
	}
	marshaled, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	if _, err := db.DB.Exec(fmt.Sprintf(
		`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'PasswordResetRequest', $2, 'Draft', $3)`, schema),
		docID, marshaled, actorUserID); err != nil {
		return nil, err
	}
	if err := SubmitForApproval(tenantID, "PasswordResetRequest", docID, actorUserID, actorRole); err != nil {
		_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET status = 'Failed' WHERE doctype = 'PasswordResetRequest' AND id = $1`, schema), docID)
		return nil, err
	}
	LogAuditEvent(tenantID, actorUserID, "USER_MANAGEMENT", "PASSWORD_RESET_REQUESTED_DUAL_CONTROL",
		fmt.Sprintf("Password reset requested for privileged account %s; awaiting a second Super Admin's approval (reason: %s)", targetUsername, reason))
	return &PasswordResetResult{
		Immediate: false, DocumentID: docID,
		Detail: fmt.Sprintf("%s is a Super Admin account, so this request needs a second Super Admin's approval before it takes effect. It now appears in Approvals.", targetUsername),
	}, nil
}

// ExecuteApprovedPasswordResetRequest runs the actual reset once a
// PasswordResetRequest has been Approved. Called from handleDecideApproval
// (not from DecideApproval itself) because, unlike every other doctype's
// approval hook in engines/approval.go, this one has a result - the one-time
// password - that only the approver may ever see, and DecideApproval's own
// generic hooks have no path back to the HTTP response.
func ExecuteApprovedPasswordResetRequest(tenantID, docID string) (*PasswordResetResult, error) {
	data, status, _, err := fetchDocument(tenantID, "PasswordResetRequest", docID)
	if err != nil {
		return nil, err
	}
	if status != "Approved" {
		return nil, fmt.Errorf("document %s is not Approved (status: %s)", docID, status)
	}
	targetUserID, _ := data["target_user_id"].(string)
	targetUsername, _ := data["target_username"].(string)
	_, _, targetEmail, lookupErr := lookupPasswordResetTarget(tenantID, targetUserID)
	if lookupErr != nil {
		return nil, lookupErr
	}

	tempPassword, err := executePasswordReset(tenantID, targetUserID, targetUsername, targetEmail)
	if err != nil {
		return nil, err
	}
	return &PasswordResetResult{DocumentID: docID, TempPassword: tempPassword,
		Detail: passwordResetDetail(targetUsername, tempPassword)}, nil
}

func orNoneGiven(reason string) string {
	if reason == "" {
		return "(none given)"
	}
	return reason
}
