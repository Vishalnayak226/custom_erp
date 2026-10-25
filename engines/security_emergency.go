package engines

import (
	"custom_erp/db"
	"errors"
	"fmt"
	"strings"
)

// Stage 49.12.3: emergency controls a client administrator can use in the
// first minutes of an incident, each with a reason, an audit row and a tested
// way back - no undocumented kill command.
//
//   - Sign a user out everywhere: bumps credential_version, the same switch a
//     password change uses (49.2.2), so every token minted before it is void
//     at its next request. The password is untouched; the user signs in again.
//   - Sign everyone out (except the administrator doing it): the same, for
//     every user of the client.
//   - Freeze exports: the security.freeze_exports setting stops every bulk
//     export at the one middleware choke point (isBulkExport). Undo is the
//     same setting turned off.
//
// Wider controls already exist and are named in the runbook rather than
// duplicated: suspend a client (SuspendTenant), revoke an API credential,
// ERP_DISABLE_EXTERNAL_SIDE_EFFECTS for every outbound effect.

// SettingFreezeExports is the per-client export freeze.
const SettingFreezeExports = "security.freeze_exports"

// ErrEmergencyReasonRequired is returned when a control is used without a reason.
var ErrEmergencyReasonRequired = errors.New("a reason is required for an emergency control")

func emergencyReason(reason string) (string, error) {
	reason = strings.TrimSpace(reason)
	if len(reason) < 5 {
		return "", ErrEmergencyReasonRequired
	}
	return redactAndBoundLogField(reason, 500), nil
}

// SignOutUserEverywhere ends every session of one user. Returns the username.
func SignOutUserEverywhere(tenantID, userID, actor, reason string) (string, error) {
	reason, err := emergencyReason(reason)
	if err != nil {
		return "", err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	var username string
	if err := db.DB.QueryRow(fmt.Sprintf(`UPDATE %s.users SET credential_version = credential_version + 1
		WHERE id::text = $1 RETURNING username`, schema), userID).Scan(&username); err != nil {
		return "", fmt.Errorf("user not found")
	}
	// Remembered devices skip the 2FA code; a stolen-session response must
	// make the next sign-in ask for it again.
	_, _ = db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.mfa_trusted_devices WHERE user_id::text = $1`, schema), userID)
	InvalidateLiveUserState(tenantID, userID) // now, not after the ~30s cache window
	LogAuditEvent(tenantID, actor, "USER_MANAGEMENT", "SESSIONS_REVOKED",
		fmt.Sprintf("Every session of %s ended. Reason: %s", username, reason))
	return username, nil
}

// SignOutAllUsers ends every session of every user of the client except the
// administrator doing it (who would otherwise lock themselves out mid-incident).
func SignOutAllUsers(tenantID, actorUserID, actor, reason string) (int, error) {
	reason, err := emergencyReason(reason)
	if err != nil {
		return 0, err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, err
	}
	res, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.users SET credential_version = credential_version + 1
		WHERE id::text <> $1`, schema), actorUserID)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	_, _ = db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.mfa_trusted_devices WHERE user_id::text <> $1`, schema), actorUserID)
	InvalidateLiveUserStatesForTenant(tenantID)
	LogAuditEvent(tenantID, actor, "USER_MANAGEMENT", "ALL_SESSIONS_REVOKED",
		fmt.Sprintf("Every session of %d user(s) ended (the administrator's own kept). Reason: %s", n, reason))
	return int(n), nil
}

// SetExportFreeze turns the export freeze on or off, with a reason.
func SetExportFreeze(tenantID string, on bool, actor, reason string) error {
	reason, err := emergencyReason(reason)
	if err != nil {
		return err
	}
	if err := SetSetting(tenantID, SettingFreezeExports, fmt.Sprintf("%t", on), actor); err != nil {
		return err
	}
	status := "EXPORTS_FROZEN"
	if !on {
		status = "EXPORTS_UNFROZEN"
	}
	LogAuditEvent(tenantID, actor, "SECURITY_CONTROL", status, "Reason: "+reason)
	return nil
}
