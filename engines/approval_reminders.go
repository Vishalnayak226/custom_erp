package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"strings"
)

// 73.4.9 (ask 178): approval SLA for every approval-gated document, beside the
// purchase-order reminders (146.4.6) and PO rule escalation (148.39) it
// mirrors. A document still Pending Approval after approvals.reminder_hours
// sends "Approval Reminder" once; after approvals.escalation_hours it sends
// "Approval Escalation" once, and from then on the role
// approvals.escalate_to_role names may decide it as well (mayAlsoDecide).
// Purchase orders keep their own settings and rules and are left out here.
// All off by default: with the settings at 0 nothing is sent and nobody new
// may decide anything.
const (
	SettingApprovalReminderHours   = "approvals.reminder_hours"
	SettingApprovalEscalationHours = "approvals.escalation_hours"
	SettingApprovalEscalateToRole  = "approvals.escalate_to_role"

	// approvalEscalateRoleCode is "Approver not found" in the error catalog.
	approvalEscalateRoleCode = "APPROV-0157"
)

func init() {
	hours := func(key, label, desc string) {
		RegisterSetting(SettingDefinition{Key: key, Module: "Security", Label: label, Type: SettingTypeInt, Default: "0", Unit: "hours",
			Min: settingBound(0), Max: settingBound(8760), Description: desc + " Purchase orders use their own settings under Procurement. 0 = off."})
	}
	hours(SettingApprovalReminderHours, "Remind approvers of a waiting document after",
		"Hours any document waits for approval before the \"Approval Reminder\" notification goes out, once per wait.")
	hours(SettingApprovalEscalationHours, "Escalate a waiting document after",
		"Hours any document waits for approval before the \"Approval Escalation\" notification goes out, once per wait; the escalation role below may then decide it too.")
	RegisterSetting(SettingDefinition{
		Key: SettingApprovalEscalateToRole, Module: "Security",
		Label: "Escalate waiting documents to role", Type: SettingTypeString, Default: "",
		Description: "The role that may also decide a document once it has waited past the escalation hours. Blank: the escalation is a notice only. Never covers a document that needs a second admin.",
	})
	settingChangeHooks[SettingApprovalEscalateToRole] = func(tenantID, from, to, by string) error {
		to = strings.TrimSpace(to)
		if to == "" || roleKnownIn(TenantRoleNames(tenantID), to) {
			return nil
		}
		return &ValidationError{Code: approvalEscalateRoleCode, Message: fmt.Sprintf("%q is not a role here. Use a role from Settings > Roles.", to)}
	}
}

// approvalEscalationHoursFor is the escalation wait after which this role may
// also decide a waiting document, or 0 when it is not the escalation role (or
// escalation is off).
func approvalEscalationHoursFor(tenantID, actorRole string) int {
	hours := GetSettingInt(tenantID, SettingApprovalEscalationHours)
	role := strings.TrimSpace(GetSettingString(tenantID, SettingApprovalEscalateToRole))
	if hours <= 0 || role == "" || actorRole == "" || !roleKnownIn([]string{role}, actorRole) {
		return 0
	}
	return hours
}

// approvalWaitedHours is how long a document has waited since it last changed,
// by the database clock (as the reminder job counts it); -1 if unreadable.
func approvalWaitedHours(tenantID, doctype, docID string) int64 {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return -1
	}
	var h int64
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT EXTRACT(EPOCH FROM (NOW() - updated_at))::bigint / 3600 FROM %s.documents
		WHERE id = $1 AND doctype = $2 AND deleted_at IS NULL AND status = 'Pending Approval'`, schema), docID, doctype).Scan(&h); err != nil {
		return -1
	}
	return h
}

// RunApprovalReminders sends the reminder, then the escalation, for each
// document other than a PO still Pending Approval. Each goes once per wait:
// the stamp holds the updated_at it was sent for, so a document edited and
// resubmitted waits afresh. It runs hourly from the day-limit jobs.
func RunApprovalReminders(tenantID, schema string) error {
	escalateTo := strings.TrimSpace(GetSettingString(tenantID, SettingApprovalEscalateToRole))
	for _, step := range []struct{ setting, event, stamp string }{
		{SettingApprovalReminderHours, "Approval Reminder", "approval_reminded_at"},
		{SettingApprovalEscalationHours, "Approval Escalation", "approval_escalated_at"},
	} {
		n := GetSettingInt(tenantID, step.setting)
		if n <= 0 {
			continue
		}
		docs, err := dayLimitRows(tenantID, fmt.Sprintf(`SELECT id, doctype, data, EXTRACT(EPOCH FROM (NOW() - updated_at))::bigint / 3600
			FROM {s}.documents WHERE doctype <> 'PurchaseOrder' AND deleted_at IS NULL AND status = 'Pending Approval'
			  AND updated_at <= NOW() - make_interval(hours => $1) AND COALESCE(data->>'%s', '') <> updated_at::text
			ORDER BY updated_at LIMIT 500`, step.stamp), []interface{}{n},
			func(scan func(...interface{}) error) (map[string]interface{}, error) {
				var id, doctype string
				var raw []byte
				var waited int64
				if err := scan(&id, &doctype, &raw, &waited); err != nil {
					return nil, err
				}
				data := map[string]interface{}{}
				_ = json.Unmarshal(raw, &data) // a corrupt row still gets its notice, without the extras
				return map[string]interface{}{"id": id, "doctype": doctype, "data": data, "hours": waited}, nil
			})
		if err != nil {
			return err
		}
		for _, r := range docs {
			id, doctype, data := r["id"].(string), r["doctype"].(string), r["data"].(map[string]interface{})
			// Stamped without touching updated_at, so the wait keeps counting.
			if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || jsonb_build_object($3::text, updated_at::text) WHERE id = $1 AND doctype = $2`, schema),
				id, doctype, step.stamp); err != nil {
				return err
			}
			vars := map[string]string{"doc_id": id, "doctype": doctype, "code": firstNonEmpty(strField(data, "code"), strField(data, "name"), id),
				"amount": fmt.Sprint(DocumentBaseAmount(data)), "hours_waiting": fmt.Sprint(r["hours"])}
			if step.stamp == "approval_escalated_at" && escalateTo != "" && !secondAdminDoctypes[doctype] && !isDualControlDoctype(doctype) {
				vars["escalated_to"] = escalateTo
			}
			DispatchNotification(tenantID, step.event, id, vars)
		}
	}
	return nil
}
