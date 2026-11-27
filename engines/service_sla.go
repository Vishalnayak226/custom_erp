package engines

import (
	"context"
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"
)

// Ask 170 item 170.34: service ticket SLA timers and escalation.
//
// With service.sla_enabled on, a worker stamps every open ServiceTicket with a
// response-due and a resolution-due time, counted in business hours (the CRM
// ticket window, crm.business_hours) from the moment the ticket was created and
// by its priority. The CRM ticket list already reads those two keys
// (first_response_due, resolve_due), so the times show on the ticket and its
// "late" flags follow them. A ticket past a due time is escalated once: it is
// reassigned to the configured user, or the configured role is told, the event
// service.ticket_escalated is published and the audit log notes it. A ticket
// already escalated is never escalated again.

const (
	SettingServiceSLAEnabled    = "service.sla_enabled"
	SettingServiceSLAResponse   = "service.sla_response_hours"
	SettingServiceSLAResolution = "service.sla_resolution_hours"
	SettingServiceSLAEscalateTo = "service.sla_escalation_to"
	serviceSLACode              = "FIN-1725"
	serviceSLADefaultResponse   = "Low=24,Medium=8,High=4,Critical=1"
	serviceSLADefaultResolution = "Low=72,Medium=48,High=24,Critical=8"
)

var serviceSLAPriorities = []string{"Low", "Medium", "High", "Critical"}

func init() {
	RegisterSetting(SettingDefinition{
		Key: SettingServiceSLAEnabled, Module: "Service",
		Label: "Service ticket SLA timers", Type: SettingTypeBool, Default: "false",
		Description: "Ask 170.34: open service tickets get respond-by and resolve-by times in business hours by priority, and a late ticket is escalated once. Off: no times are stamped and nothing is escalated.",
	})
	RegisterSetting(SettingDefinition{
		Key: SettingServiceSLAResponse, Module: "Service",
		Label: "Respond within (business hours)", Type: SettingTypeString, Default: serviceSLADefaultResponse,
		Description: "Ask 170.34: hours to first response by priority, like Low=24,Medium=8,High=4,Critical=1.",
	})
	RegisterSetting(SettingDefinition{
		Key: SettingServiceSLAResolution, Module: "Service",
		Label: "Resolve within (business hours)", Type: SettingTypeString, Default: serviceSLADefaultResolution,
		Description: "Ask 170.34: hours to resolution by priority, like Low=72,Medium=48,High=24,Critical=8.",
	})
	RegisterSetting(SettingDefinition{
		Key: SettingServiceSLAEscalateTo, Module: "Service",
		Label: "Escalate late tickets to", Type: SettingTypeString, Default: "",
		Description: "Ask 170.34: user:<user id> reassigns a late ticket to that user; role:<role name> tells that role. Blank: the ticket is flagged and audited only.",
	})
	// 245.11: plain value checks, so the settings screen's whole-batch check
	// refuses a bad value (422) before anything is written; as change hooks
	// they ran mid-save and a typo came back as a 500.
	for _, k := range []string{SettingServiceSLAResponse, SettingServiceSLAResolution} {
		settingValidators[k] = func(to string) error {
			_, err := parseServiceSLAHours(to)
			return err
		}
	}
	settingValidators[SettingServiceSLAEscalateTo] = func(to string) error {
		to = strings.TrimSpace(to)
		if to != "" && !strings.HasPrefix(to, "user:") && !strings.HasPrefix(to, "role:") {
			return &ValidationError{Code: serviceSLACode, Message: "Write the target as user:<id> or role:<name>."}
		}
		return nil
	}
}

// parseServiceSLAHours reads "Low=24,Medium=8,..." into hours by priority.
func parseServiceSLAHours(s string) (map[string]int, error) {
	out := map[string]int{}
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			return nil, &ValidationError{Code: serviceSLACode, Message: "Write hours as Low=24,Medium=8,High=4,Critical=1."}
		}
		ok := false
		for _, p := range serviceSLAPriorities {
			if strings.EqualFold(strings.TrimSpace(kv[0]), p) {
				ok = true
				kv[0] = p
			}
		}
		h, err := strconv.Atoi(strings.TrimSpace(kv[1]))
		if !ok || err != nil || h < 1 || h > 2000 {
			return nil, &ValidationError{Code: serviceSLACode, Message: "Each priority needs whole hours from 1 to 2000."}
		}
		out[kv[0]] = h
	}
	return out, nil
}

func serviceSLAHoursFor(schema, key, defaults, priority string) int {
	m, err := parseServiceSLAHours(GetSettingStringForSchema(schema, key))
	if err != nil || len(m) == 0 {
		m, _ = parseServiceSLAHours(defaults)
	}
	if h, ok := m[priority]; ok {
		return h
	}
	d, _ := parseServiceSLAHours(defaults)
	if h, ok := d[priority]; ok {
		return h
	}
	return d["Medium"]
}

// RunServiceSLA stamps due times on open tickets and escalates late ones, for
// one tenant. Returns how many tickets were stamped and escalated.
func RunServiceSLA(tenantID, schema string, now time.Time) (stamped, escalated int, err error) {
	if !GetSettingBoolForSchema(schema, SettingServiceSLAEnabled) {
		return 0, 0, nil
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id FROM %s.documents WHERE doctype = 'ServiceTicket' AND deleted_at IS NULL
		AND status NOT IN ('Resolved','Closed','Cancelled') AND COALESCE(data->>'sla_escalated_at','') = '' ORDER BY created_at`, schema))
	if err != nil {
		return 0, 0, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	target := strings.TrimSpace(GetSettingStringForSchema(schema, SettingServiceSLAEscalateTo))
	for _, id := range ids {
		s, e, err := serviceSLAOne(tenantID, schema, id, target, now)
		if err != nil {
			log.Printf("[SERVICE-SLA] %s ticket %s: %v", schema, id, err)
			continue
		}
		stamped += s
		escalated += e
	}
	return stamped, escalated, nil
}

func serviceSLAOne(tenantID, schema, id, target string, now time.Time) (stamped, escalated int, err error) {
	tx, err := db.DB.Begin()
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()
	var raw, status string
	var created time.Time
	if err := tx.QueryRow(fmt.Sprintf(`SELECT data::text, status, created_at FROM %s.documents WHERE doctype = 'ServiceTicket' AND id = $1 FOR UPDATE`, schema), id).Scan(&raw, &status, &created); err != nil {
		return 0, 0, err
	}
	var d map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		return 0, 0, err
	}
	if serviceTicketTerminalStatuses[status] || strField(d, "sla_escalated_at") != "" {
		return 0, 0, nil
	}
	prio := strField(d, "priority")
	if prio == "" {
		prio = "Medium"
	}
	changed := false
	respDue, errR := time.Parse(time.RFC3339, strField(d, "first_response_due"))
	if errR != nil {
		respDue = crmAddBusinessHours(schema, created, serviceSLAHoursFor(schema, SettingServiceSLAResponse, serviceSLADefaultResponse, prio))
		d["first_response_due"] = respDue.Format(time.RFC3339)
		changed = true
	}
	resDue, errS := time.Parse(time.RFC3339, strField(d, "resolve_due"))
	if errS != nil {
		resDue = crmAddBusinessHours(schema, created, serviceSLAHoursFor(schema, SettingServiceSLAResolution, serviceSLADefaultResolution, prio))
		d["resolve_due"] = resDue.Format(time.RFC3339)
		changed = true
	}
	late := (strField(d, "first_response_at") == "" && now.After(respDue)) || (strField(d, "resolved_at") == "" && now.After(resDue))
	if late {
		reason := "resolution"
		if strField(d, "first_response_at") == "" && now.After(respDue) {
			reason = "response"
		}
		d["sla_escalated_at"] = now.UTC().Format(time.RFC3339)
		d["escalated_to"] = target
		if strings.HasPrefix(target, "user:") {
			d["assigned_to"] = strings.TrimPrefix(target, "user:")
		}
		if err := PublishEvent(tx, schema, "service.ticket_escalated", map[string]interface{}{
			"ticket_id": id, "priority": prio, "breach": reason, "escalated_to": target,
		}); err != nil {
			return 0, 0, err
		}
		changed = true
		escalated = 1
	}
	if !changed {
		return 0, 0, nil
	}
	b, _ := json.Marshal(d)
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $1, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'ServiceTicket' AND id = $2`, schema), b, id); err != nil {
		return 0, 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}
	if escalated == 1 {
		LogAuditEvent(tenantID, "system", "SERVICE_TICKET_ESCALATED", "SUCCESS", fmt.Sprintf("ticket=%s priority=%s to=%s", id, prio, target))
	}
	return 1, escalated, nil
}

// StartServiceSLAWorker sweeps every tenant that has the setting on.
func StartServiceSLAWorker(ctx context.Context, interval time.Duration) {
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
				schemas, err := sweepTenantSchemas()
				if err != nil {
					continue
				}
				for _, schema := range schemas {
					tenantID, err := tenantIDForSchema(schema)
					if err != nil {
						continue
					}
					if _, _, err := RunServiceSLA(tenantID, schema, time.Now()); err != nil {
						log.Printf("[SERVICE-SLA] %s: %v", schema, err)
					}
				}
			}
		}
	}()
}
