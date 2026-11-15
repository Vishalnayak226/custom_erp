package engines

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"custom_erp/db"
)

// Stage 49.2.3: MFA assurance tiers.
//
// Two admin roles are MFA-gated at sign-in (RequiresMFA). The other actions the
// security standard names - finance approval, payroll, connector secrets, bulk
// sensitive export, break-glass - run for roles that are not, so one client
// setting decides what happens when someone without an authenticator reaches
// them:
//
//	off      nothing changes (the default - no client is locked out by a release)
//	report   the action runs; one audit row says it would have been refused
//	enforce  the action is refused until the person has set up an authenticator,
//	         and an enrolled person is asked for the code at sign-in
//
// Enforcement is one check in apiMiddleware (MFATiersForAction names the tiers
// an action belongs to), so a new route in a tier needs no code of its own.
//
// Passkeys (49.2.3, engines/webauthn.go) are an optional second factor on the
// MFA challenge; TOTP plus single-use hashed recovery codes remain the base.
// See docs/security/mfa-assurance-tiers.md.

// SettingMFATierPolicy is the client's switch.
const SettingMFATierPolicy = "security.mfa_tier_policy"

const (
	MFAPolicyOff     = "off"
	MFAPolicyReport  = "report"
	MFAPolicyEnforce = "enforce"
)

// Tier keys.
const (
	MFATierAdmin           = "admin"
	MFATierFinanceApproval = "finance_approval"
	MFATierPayroll         = "payroll"
	MFATierConnectorSecret = "connector_secrets"
	MFATierBulkExport      = "bulk_export"
	MFATierBreakGlass      = "break_glass"
	MFATierPrivilegeChange = "privilege_change" // 49.3.6
)

// MFATier describes one tier for the guide and the security pane.
type MFATier struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

// MFATiers lists every tier, in the order the standard names them.
var MFATiers = []MFATier{
	{MFATierAdmin, "Administrator"},
	{MFATierFinanceApproval, "Finance approval"},
	{MFATierPayroll, "Payroll"},
	{MFATierConnectorSecret, "Connector secrets"},
	{MFATierBulkExport, "Bulk export"},
	{MFATierBreakGlass, "Break-glass"},
	{MFATierPrivilegeChange, "Privilege change"},
}

// MFATierAuditAction is the audit action of a report/refusal row.
const MFATierAuditAction = "MFA_TIER"

// MFATierPolicy reads the client's policy; anything unrecognised is off.
func MFATierPolicy(tenantID string) string {
	switch strings.ToLower(strings.TrimSpace(GetSettingString(tenantID, SettingMFATierPolicy))) {
	case MFAPolicyReport:
		return MFAPolicyReport
	case MFAPolicyEnforce:
		return MFAPolicyEnforce
	}
	return MFAPolicyOff
}

// mfaFinanceApprovalRoutes are the actions that approve or release money.
var mfaFinanceApprovalRoutes = map[string]bool{
	"POST /api/v1/approval/decide":                          true,
	"POST /api/v1/approval/bulk-decide":                     true,
	"POST /api/v1/refunds/{id}/approve":                     true,
	"POST /api/v1/finance/payment-proposal/{id}/execute":    true,
	"POST /api/v1/finance/payment-proposal/{id}/record-utr": true,
	"POST /api/v1/procurement/vendor-invoice/pay":           true,
	"POST /api/v1/procurement/vendor-invoice/pay-with-tds":  true,
	"POST /api/v1/expenses/pay":                             true,
}

// MFATiersForAction names the tiers a request belongs to, from its registered
// route pattern ("POST /api/v1/..."), the capability that route needs and
// whether it is a bulk export.
func MFATiersForAction(pattern, capability string, bulkExport bool) []string {
	var tiers []string
	add := func(t string) { tiers = append(tiers, t) }
	switch {
	case strings.HasPrefix(pattern, "POST /api/v1/admin/security/freeze-"),
		pattern == "POST /api/v1/admin/security/sign-out-all",
		pattern == "POST /api/v1/admin/users/reset-mfa":
		add(MFATierBreakGlass)
	}
	// 49.3.6: changing what a role may do is step-up work like the rest.
	switch pattern {
	case "POST /api/v1/admin/role-permissions", "POST /api/v1/admin/role-permissions/rollback":
		add(MFATierPrivilegeChange)
	}
	if capability == "hr.payroll" {
		add(MFATierPayroll)
	}
	if mfaFinanceApprovalRoutes[pattern] {
		add(MFATierFinanceApproval)
	}
	if capability == "admin.integrations" || capability == "admin.api-credentials" ||
		strings.Contains(pattern, "/credentials") {
		add(MFATierConnectorSecret)
	}
	if bulkExport {
		add(MFATierBulkExport)
	}
	return tiers
}

// UserHasMFA reports whether the user has an active authenticator.
func UserHasMFA(tenantID, userID string) bool {
	enabled, _, err := GetUserMFAStatus(tenantID, userID)
	return err == nil && enabled
}

// RequiresMFAAtLogin is RequiresMFA plus the client's tier policy: under
// "enforce", anyone who has set up an authenticator is asked for it at sign-in,
// so the factor the tiers rely on is actually presented.
func RequiresMFAAtLogin(tenantID, userID, role string) bool {
	if RequiresMFA(role) {
		return true
	}
	return MFATierPolicy(tenantID) == MFAPolicyEnforce && UserHasMFA(tenantID, userID)
}

// MFATierDecision is the outcome of EvaluateMFATier.
type MFATierDecision struct {
	Refuse bool     // enforce, and the person has no authenticator
	Report bool     // report, and the person has no authenticator
	Tiers  []string // the tiers that applied
}

// EvaluateMFATier decides one request. Administrators are already MFA-gated at
// sign-in and never reach a refusal here.
func EvaluateMFATier(tenantID, userID, role string, tiers []string) MFATierDecision {
	d := MFATierDecision{Tiers: tiers}
	if len(tiers) == 0 || RequiresMFA(role) {
		return d
	}
	policy := MFATierPolicy(tenantID)
	if policy == MFAPolicyOff || UserHasMFA(tenantID, userID) {
		return d
	}
	d.Refuse = policy == MFAPolicyEnforce
	d.Report = policy == MFAPolicyReport
	return d
}

var mfaTierSeen sync.Map // tenant|user|tiers -> time of last audit row

// LogMFATierDecision writes the audit row for a report or a refusal, once per
// person per tier set per hour so a busy screen cannot flood the log.
func LogMFATierDecision(ctx context.Context, tenantID, username string, d MFATierDecision, command string) {
	status := "REPORT_WOULD_REFUSE"
	if d.Refuse {
		status = "REFUSED"
	} else if !d.Report {
		return
	}
	key := tenantID + "|" + username + "|" + strings.Join(d.Tiers, ",") + "|" + status
	if prev, ok := mfaTierSeen.Load(key); ok && time.Since(prev.(time.Time)) < time.Hour {
		return
	}
	mfaTierSeen.Store(key, time.Now())
	LogAuditEventCtx(ctx, tenantID, username, MFATierAuditAction, status,
		fmt.Sprintf("%s needs an authenticator (%s); none is set up", command, strings.Join(d.Tiers, ", ")))
}

// MFATierWouldRefuse14d counts the audit rows the policy wrote in 14 days, for
// the security pane: the measured size of the change before it is switched on.
func MFATierWouldRefuse14d(tenantID string) int {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0
	}
	var n int
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.audit_logs WHERE action = $1
		AND created_at > $2::timestamp`, schema), MFATierAuditAction, utcStamp(time.Now().AddDate(0, 0, -14))).Scan(&n)
	return n
}
