package engines

import (
	"fmt"
	"strings"
	"time"

	"custom_erp/db"
)

// 47.12.5 (ask 185): the owner-run profile lets a submitter decide their own
// documents; this report is the monthly look at every such decision. Each one
// is the approval_log row DecideApproval marked "[decided by the submitter]".

const ownDecisionMark = "[decided by the submitter]"

type OwnApprovalDecision struct {
	DecidedAt  string  `json:"decided_at"`
	Doctype    string  `json:"doctype"`
	DocumentID string  `json:"document_id"`
	Decision   string  `json:"decision"`
	DecidedBy  string  `json:"decided_by"`
	Role       string  `json:"role"`
	Amount     float64 `json:"amount"`
	Reason     string  `json:"reason"`
}

// ListOwnApprovalDecisions returns the own decisions made between from and to
// (YYYY-MM-DD, both days included, the client's day); empty means open-ended.
func ListOwnApprovalDecisions(tenantID, from, to string) ([]OwnApprovalDecision, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	q := fmt.Sprintf(`SELECT created_at, doctype, document_id, action, COALESCE(actor_user_id, ''),
		COALESCE(actor_role, ''), COALESCE(amount, 0)::float8, COALESCE(comment, '')
		FROM %s.approval_log WHERE comment LIKE $1`, schema)
	args := []interface{}{ownDecisionMark + "%"}
	if from != "" {
		args = append(args, from)
		q += fmt.Sprintf(" AND created_at >= public.erp_day_start($%d::date)", len(args))
	}
	if to != "" {
		args = append(args, to)
		q += fmt.Sprintf(" AND created_at < public.erp_day_start($%d::date + 1)", len(args))
	}
	rows, err := db.DB.Query(q+" ORDER BY created_at DESC LIMIT 20000", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []OwnApprovalDecision{}
	for rows.Next() {
		var r OwnApprovalDecision
		var at time.Time
		if err := rows.Scan(&at, &r.Doctype, &r.DocumentID, &r.Decision, &r.DecidedBy, &r.Role, &r.Amount, &r.Reason); err != nil {
			return nil, err
		}
		r.DecidedAt = at.Format("2006-01-02 15:04")
		r.Reason = strings.TrimSpace(strings.TrimPrefix(r.Reason, ownDecisionMark))
		out = append(out, r)
	}
	return out, rows.Err()
}

func init() {
	RegisterReport(ReportDefinition{ID: "own-approval-decisions", Label: "Own approval decisions", Category: "Admin",
		Columns: []ReportColumn{
			{Key: "decided_at", Label: "Decided"}, {Key: "doctype", Label: "Document type"},
			{Key: "document_id", Label: "Document"}, {Key: "decision", Label: "Decision"},
			{Key: "decided_by", Label: "Decided by"}, {Key: "role", Label: "Role"},
			{Key: "amount", Label: "Amount"}, {Key: "reason", Label: "Reason"},
		},
		Params: []ReportParam{{Key: "from", Label: "From", Type: "date"}, {Key: "to", Label: "To", Type: "date"}},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			rows, err := ListOwnApprovalDecisions(tenantID, params["from"], params["to"])
			if err != nil {
				return nil, err
			}
			return structsToRows(rows)
		}})
}
