package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Stage 55.18 - service desk and project context, read-only.
//
// ServiceTicket and ServiceContract already have their lifecycle
// (service_management.go) and Project its GL dimension (project.go). What a
// service desk lacked was the working view: my queue with each ticket's
// customer, contract coverage, due dates, the next step the lifecycle allows
// and its status history; a contract with the tickets it covers; and a
// project's cost and revenue traced to the documents that posted them. All
// of it reads existing rows - no new state, no SLA or billing engine.

var serviceTicketNextStep = map[string]string{
	"Draft": "Assign a technician", "Assigned": "Start work", "InProgress": "Resolve with notes", "Resolved": "Close",
}

var servicePriorityRank = map[string]int{"Critical": 0, "High": 1, "Medium": 2, "Low": 3}

// serviceStatusHistory reads a ticket's status changes from the documents edit
// log - one row per field before Stage 47.16.4a ("Field \"status\" modified
// from X to Y"), one row per save since (parseAuditFieldChanges) - on the
// session clock those unsigned rows are written in.
func serviceStatusHistory(schema, docID string) []map[string]interface{} {
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT COALESCE(user_id, ''), COALESCE(details, ''), created_at AT TIME ZONE current_setting('TimeZone')
		FROM %s.audit_logs
		WHERE (details LIKE 'Field "status" modified%%' AND details LIKE '%%' || $1)
		   OR (entity_type = 'ServiceTicket' AND entity_id = $1 AND details LIKE $2)
		ORDER BY created_at, id`, schema), docID, auditEditPrefix+`%"status": {"new": %`)
	if err != nil {
		return []map[string]interface{}{}
	}
	defer rows.Close()
	out := []map[string]interface{}{}
	for rows.Next() {
		var user, details string
		var at time.Time
		if rows.Scan(&user, &details, &at) != nil {
			continue
		}
		change := details
		_, changes := parseAuditFieldChanges(details)
		for _, c := range changes {
			if c.Field == "status" {
				change = `from "` + c.Old + `" to "` + c.New + `"`
			}
		}
		out = append(out, map[string]interface{}{"at": at, "by": user, "change": change})
	}
	return out
}

// serviceContractCoverage says whether a contract covers work today.
func serviceContractCoverage(data map[string]interface{}, status string) map[string]interface{} {
	included, _ := parityNumber(data["visits_included"])
	used, _ := parityNumber(data["visits_used"])
	start, _ := data["start_date"].(string)
	end, _ := data["end_date"].(string)
	today := BusinessToday()
	covered := status == "Active" && (start == "" || start <= today) && (end == "" || end >= today) && used < included
	reason := "Covered"
	switch {
	case status != "Active":
		reason = "Contract is " + status
	case start != "" && start > today:
		reason = "Starts " + start
	case end != "" && end < today:
		reason = "Ended " + end
	case used >= included:
		reason = fmt.Sprintf("All %v visits used", included)
	}
	return map[string]interface{}{
		"status": status, "start_date": start, "end_date": end, "visits_included": included,
		"visits_used": used, "visits_remaining": included - used, "covered": covered, "coverage": reason,
	}
}

// 134.6 (47.10.1): most open tickets the service desk loads, earliest respond/resolve-by date first.
const serviceDeskTicketCap = 1000

// ServiceDesk returns open tickets - overdue first, then by priority and due
// date. mine narrows to the viewer's tickets plus unassigned ones.
func ServiceDesk(tenantID, userID string, mine bool) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, status, data, created_at AT TIME ZONE current_setting('TimeZone')
		FROM %s.documents
		WHERE doctype = 'ServiceTicket' AND deleted_at IS NULL AND status NOT IN ('Closed', 'Cancelled')
		ORDER BY LEAST(NULLIF(data->>'respond_by_date', ''), NULLIF(data->>'resolve_by_date', '')) NULLS LAST, created_at, id
		LIMIT %d`, schema, serviceDeskTicketCap))
	if err != nil {
		return nil, err
	}
	type ticket struct {
		id, status string
		data       map[string]interface{}
		created    time.Time
	}
	var tickets []ticket
	for rows.Next() {
		var t ticket
		var raw []byte
		if err := rows.Scan(&t.id, &t.status, &raw, &t.created); err != nil {
			rows.Close()
			return nil, err
		}
		_ = json.Unmarshal(raw, &t.data)
		tickets = append(tickets, t)
	}
	rows.Close()

	today := BusinessToday()
	out := []map[string]interface{}{}
	for _, t := range tickets {
		str := func(k string) string { v, _ := t.data[k].(string); return v }
		assigned := str("assigned_to")
		if mine && assigned != "" && assigned != userID {
			continue
		}
		respondBy, resolveBy := str("respond_by_date"), str("resolve_by_date")
		due := ""
		switch {
		case t.status == "Draft" && respondBy != "" && respondBy < today:
			due = "Response overdue (by " + respondBy + ")"
		case t.status != "Resolved" && resolveBy != "" && resolveBy < today:
			due = "Resolution overdue (by " + resolveBy + ")"
		case resolveBy == today || (t.status == "Draft" && respondBy == today):
			due = "Due today"
		}
		row := map[string]interface{}{
			"id": t.id, "status": t.status, "customer": str("customer"), "description": str("description"),
			"priority": str("priority"), "assigned_to": assigned, "respond_by_date": respondBy,
			"resolve_by_date": resolveBy, "due": due, "overdue": strings.Contains(due, "overdue"),
			"next_step": serviceTicketNextStep[t.status], "created_at": t.created, "asset": str("asset"),
			"history": serviceStatusHistory(schema, t.id),
		}
		if contractID := str("service_contract_id"); contractID != "" {
			cdata, cstatus, err := fetchDocData(tenantID, "ServiceContract", contractID)
			if err == nil {
				cov := serviceContractCoverage(cdata, cstatus)
				cov["id"] = contractID
				row["contract"] = cov
			} else {
				row["contract"] = map[string]interface{}{"id": contractID, "covered": false, "coverage": "Contract not found"}
			}
		}
		out = append(out, row)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a["overdue"] != b["overdue"] {
			return a["overdue"].(bool)
		}
		pa, pb := servicePriorityRank[a["priority"].(string)], servicePriorityRank[b["priority"].(string)]
		if pa != pb {
			return pa < pb
		}
		return a["resolve_by_date"].(string) < b["resolve_by_date"].(string)
	})
	return out, nil
}

// ServiceContractView returns a contract's coverage and every ticket it
// covers, newest first.
func ServiceContractView(tenantID, contractID string) (map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	data, status, err := fetchDocData(tenantID, "ServiceContract", contractID)
	if err != nil {
		return nil, &ValidationError{Code: "GLOBAL-0004", Message: fmt.Sprintf("service contract %s not found", contractID)}
	}
	cov := serviceContractCoverage(data, status)
	cov["id"] = contractID
	cov["customer"] = data["customer"]
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, status, COALESCE(data->>'description', ''), COALESCE(data->>'assigned_to', ''), created_at AT TIME ZONE current_setting('TimeZone')
		FROM %s.documents WHERE doctype = 'ServiceTicket' AND deleted_at IS NULL AND data->>'service_contract_id' = $1
		ORDER BY created_at DESC`, schema), contractID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tickets := []map[string]interface{}{}
	for rows.Next() {
		var id, st, desc, assigned string
		var at time.Time
		if err := rows.Scan(&id, &st, &desc, &assigned, &at); err != nil {
			return nil, err
		}
		tickets = append(tickets, map[string]interface{}{"id": id, "status": st, "description": desc, "assigned_to": assigned, "created_at": at})
	}
	cov["tickets"] = tickets
	return cov, rows.Err()
}

// ProjectLedger returns a project's revenue, expense and net from the GL
// project dimension, and the source documents behind them - the trace from
// a project figure to what posted it.
func ProjectLedger(tenantID, project string) (map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	project = strings.TrimSpace(project)
	// An Inactive project is still readable here - its history does not vanish.
	var name, customer, status sql.NullString
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT data->>'name', data->>'customer', status FROM %s.documents
		WHERE doctype = 'Project' AND deleted_at IS NULL AND id = $1`, schema), project).Scan(&name, &customer, &status)
	if err == sql.ErrNoRows || project == "" {
		return nil, &ValidationError{Code: "GLOBAL-0004", Message: fmt.Sprintf("project %q not found", project)}
	} else if err != nil {
		return nil, err
	}

	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT p.document_type, p.document_id, MIN(p.created_at) AT TIME ZONE current_setting('TimeZone'),
		       COALESCE(SUM(CASE WHEN a.account_type = 'Revenue' THEN p.credit - p.debit ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN a.account_type = 'Expense' THEN p.debit - p.credit ELSE 0 END), 0)
		FROM %s.gl_postings p JOIN %s.gl_accounts a ON a.account_code = p.account_code
		WHERE p.project = $1 AND a.account_type IN ('Revenue', 'Expense')
		GROUP BY p.document_type, p.document_id
		ORDER BY MIN(p.created_at) DESC
		LIMIT 500`, schema, schema), project)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var revenuePaise, expensePaise int64
	docs := []map[string]interface{}{}
	for rows.Next() {
		var docType, docID string
		var at time.Time
		var rev, exp int64
		if err := rows.Scan(&docType, &docID, &at, &rev, &exp); err != nil {
			return nil, err
		}
		revenuePaise += rev
		expensePaise += exp
		docs = append(docs, map[string]interface{}{
			"document_type": docType, "document_id": docID, "posted_at": at,
			"revenue": PaiseToRupees(rev), "expense": PaiseToRupees(exp),
		})
	}
	return map[string]interface{}{
		"project": project, "name": name.String, "customer": customer.String, "status": status.String,
		"revenue": PaiseToRupees(revenuePaise), "expense": PaiseToRupees(expensePaise),
		"net": PaiseToRupees(revenuePaise - expensePaise), "documents": docs,
	}, rows.Err()
}
