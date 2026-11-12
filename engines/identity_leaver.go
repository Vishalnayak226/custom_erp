package engines

import (
	"context"
	"custom_erp/db"
	"fmt"
	"strings"
	"time"
)

// Stage 49.2.1 (leaver): the HR record and the login are one lifecycle. An
// Employee who has left - status Inactive or Exited, or an exit date that has
// come - has the linked user disabled the way Deactivate does it: status
// Inactive, the live session dropped at once, an audit row naming why. The
// disable is never held back by an approval; when the joiner/mover/leaver
// approval setting is on, the leaver is recorded for a second person to review
// afterwards (identity_access_change.go).

// EmployeeHasLeft reports whether an Employee's status or exit date means the
// person has left. An exit date counts from its own day.
func EmployeeHasLeft(status, exitDate string, now time.Time) bool {
	switch strings.TrimSpace(status) {
	case "Inactive", "Exited":
		return true
	}
	d := strings.TrimSpace(exitDate)
	if len(d) >= 10 {
		if t, err := time.Parse("2006-01-02", d[:10]); err == nil {
			return !t.After(now)
		}
	}
	return false
}

// disableLeaverUser disables the user linked to a leaving Employee. The
// platform Super Admin, the support user and the client's last active admin
// are never disabled by HR data: the save still succeeds and the caller logs
// the reason.
func disableLeaverUser(ctx context.Context, tenantID, schema string, ch EmployeeAccessChange) error {
	var role, status string
	err := db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(role, ''), COALESCE(status, '') FROM %s.users u WHERE id = $1 AND %s`, schema, NotSupportUserSQL("u")), ch.UserID).Scan(&role, &status)
	if err != nil || status != "Active" {
		return nil // no such user, the support user, or already disabled: nothing to do
	}
	if IsSuperAdmin(role) {
		return &ValidationError{Code: "GLOBAL-0002", Message: "the linked user is a Super Admin; deactivate it from Users"}
	}
	if HasFullAccess(role) {
		if n, cErr := countActiveAdmins(schema); cErr != nil || n <= 1 {
			return &ValidationError{Code: "GLOBAL-0002", Message: "the linked user is the last active admin; add another admin first"}
		}
	}
	res, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.users SET status = 'Inactive' WHERE id = $1 AND status = 'Active'`, schema), ch.UserID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil
	}
	InvalidateLiveUserState(tenantID, ch.UserID)
	actor := ch.Actor
	if actor == "" {
		actor = "system"
	}
	// The administrator's own wording, so detectDisabledUserActivity watches it.
	LogAuditEventCtx(ctx, tenantID, actor, "USER_MANAGEMENT", "USER_STATUS_CHANGED",
		fmt.Sprintf("Set user %s status to Inactive - Employee left (HR access link)", ch.UserID))
	recordLeaverForReview(ctx, tenantID, ch.UserID, actor, "Employee left")
	return nil
}
