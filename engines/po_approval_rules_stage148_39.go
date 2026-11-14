package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Stage 148.39: PO approval beyond the amount. PORoutingRule rows (Procurement
// master) route a purchase order by amount band plus vendor group and/or item
// category to a role; the most specific rule that matches wins, and a PO no
// rule matches falls to the approval_rules bands exactly as before. A rule may
// escalate: after N days waiting, a second named role may decide too (the
// reminder job stamps it). ApprovalDelegate rows say who covers an approver for
// a date range. Both are read at the one place the approval engine asks who
// may decide (requiredApproverRoleFor / mayAlsoDecide), so every way a PO is
// submitted or decided - the form, a bulk upload, a combined requisition, a
// retro PO - goes through them.

type poRoutingRule struct {
	ID, VendorGroup, ItemCategory, Role, EscalateRole string
	Min                                               float64
	Max                                               *float64
	EscalateDays                                      int
}

func (r poRoutingRule) specificity() int {
	n := 0
	if r.VendorGroup != "" {
		n++
	}
	if r.ItemCategory != "" {
		n++
	}
	return n
}

func activePORoutingRules(tenantID string) []poRoutingRule {
	rows, err := dayLimitRows(tenantID, `SELECT id, data FROM {s}.documents WHERE doctype = 'PORoutingRule' AND deleted_at IS NULL
		AND COALESCE(data->>'status', 'Active') = 'Active' LIMIT 500`, nil,
		func(scan func(...interface{}) error) (map[string]interface{}, error) {
			var id string
			var raw []byte
			if err := scan(&id, &raw); err != nil {
				return nil, err
			}
			d := map[string]interface{}{}
			if err := json.Unmarshal(raw, &d); err != nil {
				return nil, err
			}
			d["_id"] = id
			return d, nil
		})
	if err != nil {
		return nil
	}
	var out []poRoutingRule
	for _, d := range rows {
		r := poRoutingRule{ID: strField(d, "_id"), VendorGroup: strings.TrimSpace(strField(d, "vendor_group")),
			ItemCategory: strings.TrimSpace(strField(d, "item_category")), Role: strings.TrimSpace(strField(d, "required_role")),
			EscalateRole: strings.TrimSpace(strField(d, "escalate_role")), Min: numFromInterface(d["min_amount"]),
			EscalateDays: int(numFromInterface(d["escalate_after_days"]))}
		if lineNumberSet(d, "max_amount") && numFromInterface(d["max_amount"]) > 0 {
			m := numFromInterface(d["max_amount"])
			r.Max = &m
		}
		if r.Role != "" {
			out = append(out, r)
		}
	}
	return out
}

// matchPORoutingRule returns the rule a PO falls under, or nil: the rule's
// amount band holds the amount, its vendor group is the PO vendor's, and its
// item category is that of a line. Most specific first, then the narrowest band.
func matchPORoutingRule(tenantID string, data map[string]interface{}, amount float64) *poRoutingRule {
	rules := activePORoutingRules(tenantID)
	if len(rules) == 0 {
		return nil
	}
	group, cats := poVendorGroup(tenantID, data), poItemCategories(tenantID, data)
	var hit []poRoutingRule
	for _, r := range rules {
		if amount < r.Min || (r.Max != nil && amount > *r.Max) {
			continue
		}
		if r.VendorGroup != "" && !strings.EqualFold(r.VendorGroup, group) {
			continue
		}
		if r.ItemCategory != "" && !cats[strings.ToLower(r.ItemCategory)] {
			continue
		}
		hit = append(hit, r)
	}
	if len(hit) == 0 {
		return nil
	}
	sort.SliceStable(hit, func(i, j int) bool {
		if hit[i].specificity() != hit[j].specificity() {
			return hit[i].specificity() > hit[j].specificity()
		}
		if hit[i].Min != hit[j].Min {
			return hit[i].Min > hit[j].Min
		}
		return hit[i].ID < hit[j].ID
	})
	return &hit[0]
}

func poVendorGroup(tenantID string, data map[string]interface{}) string {
	vendor := firstNonEmpty(strField(data, "vendor"), strField(data, "vendor_id"))
	if vendor == "" {
		return ""
	}
	v, _, err := fetchDocData(tenantID, "Vendor", vendor)
	if err != nil || v == nil {
		return ""
	}
	return strings.TrimSpace(strField(v, "vendor_group"))
}

func poItemCategories(tenantID string, data map[string]interface{}) map[string]bool {
	out := map[string]bool{}
	lines, _ := ParsePOLines(data["items"])
	for _, l := range lines {
		if l.SKU == "" {
			continue
		}
		it, _, err := fetchDocData(tenantID, "Item", l.SKU)
		if err == nil && it != nil {
			if c := strings.ToLower(strings.TrimSpace(strField(it, "category"))); c != "" {
				out[c] = true
			}
		}
	}
	return out
}

// requiredApproverRoleFor is the one place the approver of a document is
// chosen: a PO takes a matching routing rule, anything else (and a PO no rule
// matches) the amount bands, as before.
func requiredApproverRoleFor(tenantID, doctype string, data map[string]interface{}, amount float64) (string, error) {
	if doctype == "PurchaseOrder" {
		if r := matchPORoutingRule(tenantID, data, amount); r != nil {
			return r.Role, nil
		}
	}
	if doctype == "ExpenseClaim" { // 170.29: an over-limit claim goes to the configured approver
		if r := expenseOverLimitApproverRole(tenantID, data); r != "" {
			return r, nil
		}
	}
	role, err := requiredApproverRole(tenantID, doctype, amount)
	if role == "" && err == nil {
		role = severityHoldFallbackRole(doctype, data) // 74.1.1: a High-severity reason with no rule goes to Admin
	}
	return role, err
}

// mayAlsoDecide says a role other than the required one may still decide a PO:
// the role its rule escalated to, or a user covering an approver of that role
// today. The maker-checker rules run before this and are untouched; a cover
// also never decides what they raised or sent for approval themselves, whatever
// approvals.self_approval says for the approver's own role.
func mayAlsoDecide(tenantID, doctype, docID string, data map[string]interface{}, createdBy, actorUserID, actorRole, requiredRole string) bool {
	if doctype != "PurchaseOrder" {
		return false
	}
	if esc := strField(data, "escalated_to_role"); esc != "" && esc == actorRole {
		return true
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return false
	}
	if actorUserID == createdBy {
		return false
	}
	if made, err := madeApprovalRound(db.DB, schema, doctype, docID, actorUserID); err != nil || made {
		return false
	}
	var n int
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %[1]s.documents d JOIN %[1]s.users u
		ON (u.id::text = d.data->>'approver_user' OR LOWER(u.username) = LOWER(d.data->>'approver_user'))
		WHERE d.doctype = 'ApprovalDelegate' AND d.deleted_at IS NULL AND COALESCE(d.data->>'status', 'Active') = 'Active'
		  AND (d.data->>'delegate_user' = $1 OR LOWER(d.data->>'delegate_user') = LOWER(COALESCE((SELECT username FROM %[1]s.users WHERE id::text = $1), '')))
		  AND d.data->>'from_date' <= $2 AND d.data->>'to_date' >= $2 AND u.role = $3 AND u.status = 'Active'`, schema),
		actorUserID, BusinessToday(), requiredRole).Scan(&n)
	return err == nil && n > 0
}

// RunPORuleEscalations stamps escalated_to_role on a PO that has waited longer
// than its rule's escalate_after_days, and sends the usual escalation
// notification. It runs from RunPOApprovalReminders, the existing job.
func RunPORuleEscalations(tenantID, schema string) error {
	if len(activePORoutingRules(tenantID)) == 0 {
		return nil
	}
	rows, err := dayLimitRows(tenantID, `SELECT id, data, EXTRACT(EPOCH FROM (NOW() - updated_at))::bigint / 86400
		FROM {s}.documents WHERE doctype = 'PurchaseOrder' AND deleted_at IS NULL AND status = 'Pending Approval'
		  AND COALESCE(data->>'escalated_to_role', '') = '' ORDER BY updated_at LIMIT 500`, nil,
		func(scan func(...interface{}) error) (map[string]interface{}, error) {
			var id string
			var raw []byte
			var days int64
			if err := scan(&id, &raw, &days); err != nil {
				return nil, err
			}
			d := map[string]interface{}{}
			if err := json.Unmarshal(raw, &d); err != nil {
				return nil, err
			}
			return map[string]interface{}{"id": id, "data": d, "days": days}, nil
		})
	if err != nil {
		return err
	}
	for _, r := range rows {
		data := r["data"].(map[string]interface{})
		rule := matchPORoutingRule(tenantID, data, DocumentBaseAmount(data))
		if rule == nil || rule.EscalateDays <= 0 || rule.EscalateRole == "" || r["days"].(int64) < int64(rule.EscalateDays) {
			continue
		}
		id := r["id"].(string)
		// updated_at is left alone so the wait keeps counting.
		if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || jsonb_build_object('escalated_to_role', $2::text, 'escalated_at', $3::text) WHERE id = $1 AND doctype = 'PurchaseOrder'`, schema),
			id, rule.EscalateRole, time.Now().UTC().Format(time.RFC3339)); err != nil {
			return err
		}
		DispatchNotification(tenantID, "PO Approval Escalation", id, map[string]string{"po_id": id, "vendor": firstNonEmpty(strField(data, "vendor"), strField(data, "vendor_id")),
			"amount": fmt.Sprint(numFromInterface(data["total_amount"])), "hours_waiting": fmt.Sprint(r["days"].(int64) * 24), "escalated_to": rule.EscalateRole})
	}
	return nil
}
