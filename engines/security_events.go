package engines

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"custom_erp/db"
)

// Stage 49.11.1: the three event classes the security event taxonomy
// (docs/security/audit-and-logging-policy.md § Security event taxonomy)
// named as not yet emitted. Each is written at the one place it happens:
//
//   - SENSITIVE_READ / BULK   - the generic list endpoint returned rows of a
//     doctype whose sensitive fields (sensitive_fields.go) this role may read
//     (handlers_core_doc_engine.go); SENSITIVE_READ / REPORT - a report with
//     a sensitive column ran unmasked for this role (RunReportContext).
//   - INVARIANT / REFUSED     - an invariant check refused a write (a posted
//     document's money, posted_amounts.go); INVARIANT / FLAGGED - a check
//     found the books disagreeing without refusing anything (a report's
//     control totals, control_totals.go). The 49.5.4 fraud patterns keep
//     their SECURITY_ALERT row (security_detection.go): one row per firing,
//     not two.
//   - AUDIT_VERIFY / FAILED   - the audit-chain verifier found a break;
//     AUDIT_VERIFY / UNVERIFIED - it could not run (47.7.7: silence is not
//     a pass). Written by the scheduled job and the on-demand verify alike.
//     A passing run writes nothing; the Security pane reads the last run
//     from the job itself (GetSecurityPosture).
const (
	SensitiveReadAction = "SENSITIVE_READ"
	InvariantAction     = "INVARIANT"
	AuditVerifyAction   = "AUDIT_VERIFY"
)

// invariantFlagDedup keeps a FLAGGED invariant from writing a row on every
// report open: the books disagreeing is one event until it is fixed, not one
// per viewer. A REFUSED write is always its own row.
const invariantFlagDedup = 24 * time.Hour

const maxSecurityEventFilterBytes = 512

// boundFilter keeps the filter a reader asked with readable but bounded;
// the audit row names the question, never a payload.
func boundFilter(filter string) string {
	filter = strings.TrimSpace(filter)
	if filter == "" {
		return "-"
	}
	if len(filter) > maxSecurityEventFilterBytes {
		filter = filter[:maxSecurityEventFilterBytes] + "..."
	}
	return filter
}

// LogSensitiveBulkRead records a list read of a doctype with sensitive fields
// this role can actually see. A masked role, an empty page, or a doctype with
// no sensitive field is not a sensitive read.
func LogSensitiveBulkRead(ctx context.Context, tenantID, userID, role, doctype string, rows int, filter string) {
	if rows <= 0 {
		return
	}
	var fields []string
	for _, a := range ExplainSensitiveFields(role, doctype) {
		if a.CanRead {
			fields = append(fields, a.Field)
		}
	}
	if len(fields) == 0 {
		return
	}
	LogAuditEventCtx(ctx, tenantID, userID, SensitiveReadAction, "BULK",
		fmt.Sprintf("%s rows=%d fields=%s filter=%s", doctype, rows, strings.Join(fields, ","), boundFilter(filter)))
}

// logSensitiveReportRead records a report that carries a sensitive column
// and ran unmasked for this role.
func logSensitiveReportRead(ctx context.Context, tenantID, userID, role, reportID string, columns []ReportColumn, rows int, params map[string]string) {
	if rows <= 0 || reportMasked(columns, role) {
		return
	}
	var sensitive []string
	for _, c := range columns {
		if c.Sensitive {
			sensitive = append(sensitive, c.Key)
		}
	}
	if len(sensitive) == 0 {
		return
	}
	keys := make([]string, 0, len(params))
	for k, v := range params {
		if strings.TrimSpace(v) != "" {
			keys = append(keys, k+"="+v)
		}
	}
	sort.Strings(keys)
	LogAuditEventCtx(ctx, tenantID, userID, SensitiveReadAction, "REPORT",
		fmt.Sprintf("report=%s rows=%d columns=%s filter=%s", reportID, rows, strings.Join(sensitive, ","), boundFilter(strings.Join(keys, "&"))))
}

// reportReadRoleKey carries the caller's real role through a run that reads
// unmasked on their behalf (RunReportWithControlTotals), so the sensitive-read
// row names the role that saw the answer, not the one that computed it.
type reportReadRoleKey struct{}

// LogInvariantEvent records an invariant check firing: outcome REFUSED when
// the write was stopped, FLAGGED when the books were found disagreeing. A
// FLAGGED rule repeats at most once a day per client.
func LogInvariantEvent(tenantID, userID, rule, outcome, details string) {
	rule = strings.TrimSpace(rule)
	if rule == "" {
		return
	}
	if strings.TrimSpace(userID) == "" {
		userID = "system"
	}
	prefix := "rule=" + rule + " "
	if outcome == "FLAGGED" {
		if schema, err := db.GetTenantSchema(tenantID); err == nil {
			var exists bool
			if err := db.DB.QueryRow(fmt.Sprintf(`SELECT EXISTS (SELECT 1 FROM %s.audit_logs
				WHERE action = $1 AND status = $2 AND details LIKE $3 AND created_at > $4::timestamp)`, schema),
				InvariantAction, outcome, likeEscape(prefix)+"%", utcStamp(time.Now().Add(-invariantFlagDedup))).Scan(&exists); err == nil && exists {
				return
			}
		}
	}
	LogAuditEvent(tenantID, userID, InvariantAction, outcome, prefix+strings.TrimSpace(details))
}

// checkPostedAmountsFrozen is validatePostedAmountsFrozen plus its event: a
// refused change to posted money is an INVARIANT / REFUSED row naming the
// document and the actor bound to this request (posting_actor.go).
func checkPostedAmountsFrozen(tenantID, doctype, docID, priorStatus string, priorData, payload map[string]interface{}) error {
	err := validatePostedAmountsFrozen(doctype, docID, priorStatus, priorData, payload)
	if err != nil {
		LogInvariantEvent(tenantID, currentPostingActor().PostedBy, "posted_amounts_frozen", "REFUSED", doctype+" "+docID+": "+err.Error())
	}
	return err
}

// RecordAuditVerification writes the verifier's finding as an audit event:
// FAILED when the chain is broken, UNVERIFIED when the verifier could not
// run. A clean run writes nothing - the job's own result is the record.
func RecordAuditVerification(tenantID string, v *AuditVerification, err error) {
	switch {
	case err != nil:
		LogAuditEvent(tenantID, "system", AuditVerifyAction, "UNVERIFIED",
			"audit verification could not run: "+err.Error()+" - treat the evidence as unverified, not as passing")
	case v != nil && !v.Intact:
		LogAuditEvent(tenantID, "system", AuditVerifyAction, "FAILED",
			fmt.Sprintf("%d tampered row(s) %v, %d bad checkpoint(s) %v, %d row(s) signed by key(s) not loaded %v",
				len(v.TamperedRows), v.TamperedRows, len(v.CheckpointsBad), v.CheckpointsBad, v.UnverifiableRows, v.MissingKeys))
	}
}

// PostureVerify is the last verifier run the Security pane shows.
type PostureVerify struct {
	At      time.Time `json:"at"`
	Outcome string    `json:"outcome"` // Passed, Failed, Could not run
	Detail  string    `json:"detail,omitempty"`
}

// lastAuditVerification is the later of the last scheduled verify job and
// the last AUDIT_VERIFY event (an on-demand verify that found a break).
func lastAuditVerification(schema string) *PostureVerify {
	var out *PostureVerify
	var at time.Time
	var status, lastErr string
	var intact *bool
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(completed_at, updated_at), status, last_error,
		(result->>'intact')::boolean FROM %s.jobs WHERE job_type = $1 AND status IN ('Succeeded', 'Failed', 'DeadLettered')
		ORDER BY COALESCE(completed_at, updated_at) DESC LIMIT 1`, schema), AuditVerifyJobType).Scan(&at, &status, &lastErr, &intact); err == nil {
		out = &PostureVerify{At: at}
		switch {
		case status != "Succeeded":
			out.Outcome, out.Detail = "Could not run", lastErr
		case intact != nil && !*intact:
			out.Outcome = "Failed"
		default:
			out.Outcome = "Passed"
		}
	}
	var evAt time.Time
	var evStatus, evDetails string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT created_at AT TIME ZONE 'UTC', status, details FROM %s.audit_logs
		WHERE action = $1 ORDER BY created_at DESC LIMIT 1`, schema), AuditVerifyAction).Scan(&evAt, &evStatus, &evDetails); err == nil {
		if out == nil || evAt.After(out.At) {
			outcome := "Failed"
			if evStatus == "UNVERIFIED" {
				outcome = "Could not run"
			}
			out = &PostureVerify{At: evAt, Outcome: outcome, Detail: evDetails}
		}
	}
	return out
}
