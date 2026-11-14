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

// exportsPausedRunStatus is a schedule's last result when the export freeze
// stopped its run (146.5.6c, 170.43).
const exportsPausedRunStatus = "Refused: exports are paused for this business"

// scheduledExportFrozen is the check a scheduled export makes before it runs:
// true (and the refusal audited) while the export freeze is on. The caller
// skips the delivery but still moves the schedule on, so nothing piles up
// for when the freeze lifts. runBy is "" for the worker.
func scheduledExportFrozen(tenantID, runBy, what string) bool {
	if !GetSettingBool(tenantID, SettingFreezeExports) {
		return false
	}
	actor := runBy
	if actor == "" {
		actor = "system"
	}
	LogAuditEvent(tenantID, actor, "DATA_EXPORT", "REFUSED_FROZEN", what)
	return true
}

// isExportDeliveryEvent names the outbox events that carry an export's data
// to an outside recipient. While the export freeze is on none of them leaves
// by webhook, even one queued before the freeze (170.43).
func isExportDeliveryEvent(eventName string) bool {
	switch eventName {
	case "report.scheduled_delivery", "dashboard.digest_delivery", "pim.export.scheduled_delivery":
		return true
	}
	return false
}

// SettingFreezeIntegrations is the per-client integration freeze (49.12.3a).
const SettingFreezeIntegrations = "security.freeze_integrations"

// SetIntegrationFreeze turns the integration freeze on or off, with a reason.
// While on, every outside call for this client is refused at the one gate
// they all pass (outside_calls.go) and its inbound webhooks are refused
// (internal/server inboundWebhookGate).
func SetIntegrationFreeze(tenantID string, on bool, actor, reason string) error {
	reason, err := emergencyReason(reason)
	if err != nil {
		return err
	}
	if err := SetSetting(tenantID, SettingFreezeIntegrations, fmt.Sprintf("%t", on), actor); err != nil {
		return err
	}
	status := "INTEGRATIONS_FROZEN"
	if !on {
		status = "INTEGRATIONS_UNFROZEN"
	}
	LogAuditEvent(tenantID, actor, "SECURITY_CONTROL", status, "Reason: "+reason)
	return nil
}

// SettingReadOnly is the per-client read-only mode (49.12.3, owner's answer
// 2026-11-14: sign-in, sign-out and reads stay open; every other change is
// refused, with a banner).
const SettingReadOnly = "security.read_only"

// SetReadOnlyMode turns read-only mode on or off, with a reason. While on, the
// one middleware gate (internal/server readOnlyRefuses) turns away every
// change this client's people, API clients and webhook senders send. Jobs the
// server runs on its own keep running: the integration freeze stops outside
// calls, and suspending the client stops everything.
func SetReadOnlyMode(tenantID string, on bool, actor, reason string) error {
	reason, err := emergencyReason(reason)
	if err != nil {
		return err
	}
	if err := SetSetting(tenantID, SettingReadOnly, fmt.Sprintf("%t", on), actor); err != nil {
		return err
	}
	status := "READ_ONLY_ON"
	if !on {
		status = "READ_ONLY_OFF"
	}
	LogAuditEvent(tenantID, actor, "SECURITY_CONTROL", status, "Reason: "+reason)
	return nil
}
