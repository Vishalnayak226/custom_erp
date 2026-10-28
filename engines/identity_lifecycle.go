package engines

import (
	"context"
	"custom_erp/db"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/lib/pq"
)

// Stage 49.2.1 / 49.2.6: identity lifecycle - accounts and API credentials
// nobody uses are switched off after a policy period.
//
// Both sweeps are off until a client's administrator chooses a period
// (security.dormant_account_days, security.api_credential_unused_days; 0 is
// the default), so a release never disables anything on its own. Each item's
// idle time runs from the latest of: its last use (or its creation), its
// reactivation (accounts), when the policy setting was last changed, and the
// client's last suspend/resume - so turning the policy on, re-enabling an
// account or resuming a suspended client always starts a fresh window, and
// a sign-in nobody could make (the client was suspended) never counts against
// anyone. Only operational clients are swept, for the same reason.
//
// An account is disabled exactly as an administrator's Deactivate does it -
// status Inactive, the live session dropped at once - and never the platform
// Super Admin, the client's support user, or the client's last active admin.
// A credential is revoked through RevokeAPICredential, which also ends its
// OAuth tokens. Each one writes an audit row.

const (
	// SettingDormantAccountDays disables accounts not signed in for this
	// many days; 0 is off.
	SettingDormantAccountDays = "security.dormant_account_days"
	// SettingAPICredentialUnusedDays revokes API credentials not used for
	// this many days; 0 is off.
	SettingAPICredentialUnusedDays = "security.api_credential_unused_days"

	// identityLifecycleMigration added users.last_sign_in_at. Sign-ins are
	// known only from when it was applied, so an account with none recorded
	// is idle from then (or from its creation, if later) - not from creation.
	identityLifecycleMigration = "migrations_stage49_2_1_identity_lifecycle.sql"

	// postureDormantDays is the review threshold the Security pane uses
	// while the dormant-account policy is off.
	postureDormantDays = 90

	identityLifecycleRowCap = 500
)

// RecordSignIn stamps users.last_sign_in_at. Called wherever the user's own
// action issues a full session (password, the authenticator step after it, a
// store-phone PIN, a store switch, a password change - 49.2.1a: the dormant
// sweep must never disable someone who is working) - never for a purpose
// token or a support user's session. Best effort: a failure here never fails
// the sign-in.
func RecordSignIn(tenantID, userID string) {
	if db.DB == nil || strings.TrimSpace(userID) == "" {
		return
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return
	}
	_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.users SET last_sign_in_at = NOW() WHERE id = $1`, schema), userID)
}

// MarkUserReactivated stamps users.reactivated_at when an account that is not
// Active is about to be made Active, so it gets a fresh dormant window. Call
// it before the status update. Best effort, like RecordSignIn.
func MarkUserReactivated(schema, userID string) {
	if db.DB == nil || strings.TrimSpace(userID) == "" {
		return
	}
	_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.users SET reactivated_at = NOW() WHERE id = $1 AND COALESCE(status, '') <> 'Active'`, schema), userID)
}

// DormantAccount is one Active account with no sign-in for the period.
type DormantAccount struct {
	ID         string     `json:"-"`
	Username   string     `json:"username"`
	Role       string     `json:"-"`
	LastSignIn *time.Time `json:"last_sign_in_at,omitempty"`
	IdleSince  time.Time  `json:"idle_since"`
}

// dormantAccounts lists Active accounts (the support user left out) idle for
// at least days, longest idle first. forSweep adds the sweep's own floors -
// the policy setting's last change and the client's last lifecycle change -
// which the Security pane's review list does not need. onlyIDs narrows the
// list (tests); nil is every account.
func dormantAccounts(schema string, days int, forSweep bool, onlyIDs []string) ([]DormantAccount, error) {
	settingKey, tenantSchema := "", ""
	if forSweep {
		settingKey, tenantSchema = SettingDormantAccountDays, schema
	}
	// GREATEST ignores NULLs, so a floor that does not apply simply drops out.
	// created_at is a session-local timestamp; the cast reads it in the same
	// session time zone it was written in.
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, username, role, last_sign_in_at, idle_since FROM (
			SELECT u.id, u.username, COALESCE(u.role, '') AS role, u.last_sign_in_at,
			       GREATEST(
			         COALESCE(u.last_sign_in_at, GREATEST(u.created_at::timestamptz,
			           (SELECT m.applied_at::timestamptz FROM public.schema_migrations m WHERE m.migration_file = $2))),
			         u.reactivated_at,
			         (SELECT s.updated_at FROM %[1]s.system_settings s WHERE s.key = $3),
			         (SELECT MAX(t.lifecycle_changed_at) FROM public.tenants t WHERE t.schema_name = $4)) AS idle_since
			FROM %[1]s.users u
			WHERE u.status = 'Active' AND %[2]s AND ($5::text[] IS NULL OR u.id = ANY($5::text[]))
		) d
		WHERE idle_since < NOW() - make_interval(days => $1)
		ORDER BY idle_since, username LIMIT %[3]d`, schema, NotSupportUserSQL("u"), identityLifecycleRowCap),
		days, identityLifecycleMigration, settingKey, tenantSchema, pq.Array(onlyIDs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DormantAccount{}
	for rows.Next() {
		var a DormantAccount
		var last sql.NullTime
		if err := rows.Scan(&a.ID, &a.Username, &a.Role, &last, &a.IdleSince); err != nil {
			return nil, err
		}
		if last.Valid {
			a.LastSignIn = &last.Time
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// pickDormantToDisable drops the accounts the sweep never disables: the
// platform Super Admin, and whichever admin would leave the client with none.
// candidates come longest idle first, so the admin kept is the one seen most
// recently. activeAdmins is the client's Active full-access accounts.
func pickDormantToDisable(candidates []DormantAccount, activeAdmins int) []DormantAccount {
	var out []DormantAccount
	for _, c := range candidates {
		if IsSuperAdmin(c.Role) {
			continue
		}
		if HasFullAccess(c.Role) {
			if activeAdmins <= 1 {
				continue
			}
			activeAdmins--
		}
		out = append(out, c)
	}
	return out
}

func countActiveAdmins(schema string) (int, error) {
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT COALESCE(role, '') FROM %s.users u WHERE u.status = 'Active' AND %s`, schema, NotSupportUserSQL("u")))
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var role string
		if err := rows.Scan(&role); err != nil {
			return 0, err
		}
		if HasFullAccess(role) {
			n++
		}
	}
	return n, rows.Err()
}

// SweepDormantAccounts disables this client's accounts not signed in for the
// policy period and returns how many it disabled. A no-op while the policy is
// off.
func SweepDormantAccounts(tenantID string) (int, error) {
	return sweepDormantAccounts(tenantID, nil)
}

func sweepDormantAccounts(tenantID string, onlyIDs []string) (int, error) {
	days := GetSettingInt(tenantID, SettingDormantAccountDays)
	if days <= 0 {
		return 0, nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, err
	}
	candidates, err := dormantAccounts(schema, days, true, onlyIDs)
	if err != nil || len(candidates) == 0 {
		return 0, err
	}
	admins, err := countActiveAdmins(schema)
	if err != nil {
		return 0, err
	}
	disabled := 0
	for _, a := range pickDormantToDisable(candidates, admins) {
		// Re-checked in the update: a sign-in since the list was read keeps
		// the account.
		res, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.users SET status = 'Inactive'
			WHERE id = $1 AND status = 'Active' AND (last_sign_in_at IS NULL OR last_sign_in_at < NOW() - make_interval(days => $2))`, schema), a.ID, days)
		if err != nil {
			return disabled, err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			continue
		}
		InvalidateLiveUserState(tenantID, a.ID)
		// The administrator's own wording first, so the disabled-account
		// detection (detectDisabledUserActivity) watches this account too.
		LogAuditEvent(tenantID, "system", "USER_MANAGEMENT", "USER_STATUS_CHANGED",
			fmt.Sprintf("Set user %s status to Inactive - not signed in for %d days (dormant account policy)", a.ID, days))
		disabled++
	}
	return disabled, nil
}

// SweepUnusedAPICredentials revokes this client's API credentials not used
// for the policy period and returns how many it revoked. A no-op while the
// policy is off.
func SweepUnusedAPICredentials(tenantID string) (int, error) {
	return sweepUnusedAPICredentials(tenantID, nil)
}

func sweepUnusedAPICredentials(tenantID string, onlyIDs []string) (int, error) {
	days := GetSettingInt(tenantID, SettingAPICredentialUnusedDays)
	if days <= 0 {
		return 0, nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT c.id::text, c.name FROM %[1]s.api_credentials c
		WHERE c.status = 'Active' AND c.revoked_at IS NULL
		  AND GREATEST(COALESCE(c.last_used_at, c.created_at),
		        (SELECT s.updated_at FROM %[1]s.system_settings s WHERE s.key = $2),
		        (SELECT MAX(t.lifecycle_changed_at) FROM public.tenants t WHERE t.schema_name = $3))
		      < NOW() - make_interval(days => $1)
		  AND ($4::text[] IS NULL OR c.id::text = ANY($4::text[]))
		ORDER BY c.name LIMIT %[2]d`, schema, identityLifecycleRowCap),
		days, SettingAPICredentialUnusedDays, schema, pq.Array(onlyIDs))
	if err != nil {
		return 0, err
	}
	type cred struct{ id, name string }
	var stale []cred
	for rows.Next() {
		var c cred
		if err := rows.Scan(&c.id, &c.name); err != nil {
			rows.Close()
			return 0, err
		}
		stale = append(stale, c)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()
	revoked := 0
	for _, c := range stale {
		if err := RevokeAPICredential(tenantID, c.id); err != nil {
			var ve *ValidationError
			if errors.As(err, &ve) {
				continue // revoked meanwhile
			}
			return revoked, err
		}
		LogAuditEvent(tenantID, "system", "API_CREDENTIAL_REVOKED", "SUCCESS",
			fmt.Sprintf("credential=%s name=%q - not used for %d days (unused API access policy)", c.id, c.name, days))
		revoked++
	}
	return revoked, nil
}

// StartIdentityLifecycleSweep runs both sweeps hourly on the scheduler leader,
// for operational clients only (a suspended client's people cannot sign in,
// so their idle time is not theirs).
func StartIdentityLifecycleSweep(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if !IsSchedulerLeader() || db.DB == nil {
					continue
				}
				schemas, err := listTenantSchemas()
				if err != nil {
					log.Printf("[IDENTITY-LIFECYCLE] failed to list tenant schemas: %v", err)
					continue
				}
				for _, schema := range schemas {
					tenantID, idErr := tenantIDForSchema(schema)
					if idErr != nil {
						continue
					}
					if _, err := SweepDormantAccounts(tenantID); err != nil {
						LogBackgroundJobError("IDENTITY-LIFECYCLE", "dormant accounts", schema, err)
					}
					if _, err := SweepUnusedAPICredentials(tenantID); err != nil {
						LogBackgroundJobError("IDENTITY-LIFECYCLE", "unused API credentials", schema, err)
					}
				}
			}
		}
	}()
}
