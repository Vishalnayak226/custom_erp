package engines

import (
	"custom_erp/db"
	"database/sql"
	"fmt"
	"sort"
	"time"

	"github.com/lib/pq"
)

// inboxApprovalFacts are the per-document facts the approvals part of the task
// inbox needs: who created it, when it last changed, whether the viewer made
// its current approval round (madeApprovalRound) and which approver slab
// matches its amount. They used to be read with three queries a document;
// loadInboxApprovalFacts reads them for every pending document at once.
type inboxApprovalFacts struct {
	createdBy string
	waiting   time.Time
	made      bool
}

type inboxApprovalBatch struct {
	facts map[string]inboxApprovalFacts // key: doctype + "\x00" + id
	rules map[string][]inboxApprovalRule
}

type inboxApprovalRule struct {
	min  float64
	max  sql.NullFloat64
	role string
}

func inboxFactKey(doctype, id string) string { return doctype + "\x00" + id }

// loadInboxApprovalFacts runs two queries however many documents are pending:
// one over documents + approval_log, one over approval_rules.
func loadInboxApprovalFacts(schema, userID string, pending []map[string]interface{}) (*inboxApprovalBatch, error) {
	b := &inboxApprovalBatch{facts: map[string]inboxApprovalFacts{}, rules: map[string][]inboxApprovalRule{}}
	if len(pending) == 0 {
		return b, nil
	}
	ids := make([]string, 0, len(pending))
	types := make([]string, 0, len(pending))
	seenType := map[string]bool{}
	var doctypes []string
	for _, doc := range pending {
		doctype, _ := doc["doctype"].(string)
		id, _ := doc["id"].(string)
		ids = append(ids, id)
		types = append(types, doctype)
		if !seenType[doctype] {
			seenType[doctype] = true
			doctypes = append(doctypes, doctype)
		}
	}
	// The same expression as madeApprovalRound, evaluated for every row.
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT d.doctype, d.id, COALESCE(d.created_by, ''), d.updated_at AT TIME ZONE current_setting('TimeZone'),
		       COALESCE(d.updated_by, '') = $3 OR EXISTS (
			SELECT 1 FROM %[1]s.approval_log l
			WHERE l.doctype = d.doctype AND l.document_id = d.id AND l.actor_user_id = $3 AND l.action IN ('Submitted', 'Modified')
			  AND l.id > COALESCE((SELECT MAX(x.id) FROM %[1]s.approval_log x
				WHERE x.doctype = d.doctype AND x.document_id = d.id AND x.action IN ('Approved', 'Rejected')), 0))
		FROM %[1]s.documents d
		JOIN unnest($1::text[], $2::text[]) AS p(id, doctype) ON p.id = d.id AND p.doctype = d.doctype`, schema),
		pq.Array(ids), pq.Array(types), userID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var doctype, id string
		var f inboxApprovalFacts
		if err := rows.Scan(&doctype, &id, &f.createdBy, &f.waiting, &f.made); err != nil {
			rows.Close()
			return nil, err
		}
		b.facts[inboxFactKey(doctype, id)] = f
	}
	rows.Close()

	rrows, err := db.DB.Query(fmt.Sprintf(`
		SELECT doctype, min_amount, max_amount, required_role FROM %s.approval_rules
		WHERE doctype = ANY($1) ORDER BY doctype, min_amount DESC`, schema), pq.Array(doctypes))
	if err != nil {
		return nil, err
	}
	defer rrows.Close()
	for rrows.Next() {
		var doctype string
		var r inboxApprovalRule
		if err := rrows.Scan(&doctype, &r.min, &r.max, &r.role); err != nil {
			return nil, err
		}
		b.rules[doctype] = append(b.rules[doctype], r)
	}
	return b, rrows.Err()
}

// requiredRole is requiredApproverRoleFor with the slab read from the batch:
// a PO takes a matching routing rule, anything else the amount bands.
func (b *inboxApprovalBatch) requiredRole(tenantID, doctype string, data map[string]interface{}, amount float64) string {
	if doctype == "PurchaseOrder" {
		if r := matchPORoutingRule(tenantID, data, amount); r != nil {
			return r.Role
		}
	}
	rules := b.rules[doctype]
	sort.SliceStable(rules, func(i, j int) bool { return rules[i].min > rules[j].min })
	for _, r := range rules {
		if r.min <= amount && (!r.max.Valid || r.max.Float64 >= amount) {
			return r.role
		}
	}
	return ""
}
