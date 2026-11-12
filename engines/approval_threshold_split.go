package engines

import (
	"fmt"

	"custom_erp/db"
)

// SettingApprovalSplitGuard turns on the approval engine's threshold-split
// guard (Stage 49.3.5). Off by default: switching it on changes which role
// approves documents a client already routes today.
const SettingApprovalSplitGuard = "approvals.threshold_split_guard"

func init() {
	RegisterSetting(SettingDefinition{
		Key: SettingApprovalSplitGuard, Module: "Security",
		Label: "Approve split documents at their combined amount", Type: SettingTypeBool, Default: "false",
		Description: "Stage 49.3.5: when a document under an approval limit, with the same person's other documents of the same kind for the same vendor or customer raised around the same time (24 hours either side), reaches that limit together, it goes to the approver the combined amount needs. The same rule the \"documents split to stay under an approval limit\" security check uses. Off: each document is routed on its own amount.",
	})
}

// splitAdjustedAmount is the amount a document is approved at. It is the
// document's own amount unless the guard is on and the document is one piece
// of a split: it is itself in an approval tier's lower half (half the tier up
// to the tier), and with the other documents the detection rule would group
// with it (thresholdSplitCTE: same maker, kind and counterparty, each in the
// same band, created within the rule's window of this one) the total reaches
// the tier. Then it is that total, so the role routing follows the slab the
// whole purchase needs. Read at submit, at decide and in the task inbox, so
// all three agree and a later piece raises the earlier ones too.
func splitAdjustedAmount(tenantID, doctype, docID string, amount float64) (float64, error) {
	if docID == "" || amount <= 0 || !GetSettingBool(tenantID, SettingApprovalSplitGuard) {
		return amount, nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return amount, err
	}
	window := 24 * 60
	for _, r := range securityDetectionRules {
		if r.Key == "threshold_split" {
			window = r.WindowMinutes
		}
	}
	// $1 doc id, $2 doctype, $3 window minutes, $4 this document's amount.
	rows, err := db.DB.Query(fmt.Sprintf(`
		WITH me AS (
			SELECT created_at, created_by,
			       COALESCE(NULLIF(data->>'vendor_id', ''), NULLIF(data->>'vendor', ''),
			                NULLIF(data->>'customer_id', ''), NULLIF(data->>'customer', ''), '') AS party
			FROM %[1]s.documents WHERE id = $1 AND doctype = $2
		), %[2]s
		SELECT t.min_amount::float8, COALESCE(SUM(m.amount), 0)::float8
		FROM tiers t, me
		LEFT JOIN money m ON true
		WHERE t.doctype = $2 AND t.min_amount > $4 AND $4 >= t.min_amount / 2
		  AND m.doctype = $2 AND m.id <> $1 AND m.created_by = me.created_by AND m.party = me.party
		  AND m.amount >= t.min_amount / 2 AND m.amount < t.min_amount
		GROUP BY t.min_amount`,
		schema, thresholdSplitCTE(schema, `d.doctype = $2 AND d.id <> $1
			AND d.created_at BETWEEN (SELECT created_at FROM me) - make_interval(mins => $3)
			                     AND (SELECT created_at FROM me) + make_interval(mins => $3)`)),
		docID, doctype, window, amount)
	if err != nil {
		return amount, err
	}
	defer rows.Close()
	best := amount
	for rows.Next() {
		var tier, others float64
		if err := rows.Scan(&tier, &others); err != nil {
			return amount, err
		}
		if amount+others >= tier && amount+others > best {
			best = amount + others
		}
	}
	return best, rows.Err()
}

// requiredApproverRoleForDoc is requiredApproverRoleFor at the amount the
// document is approved at (splitAdjustedAmount): the one call submit, decide
// and the task inbox make.
func requiredApproverRoleForDoc(tenantID, doctype, docID string, data map[string]interface{}, amount float64) (string, error) {
	adjusted, err := splitAdjustedAmount(tenantID, doctype, docID, amount)
	if err != nil {
		return "", err
	}
	return requiredApproverRoleFor(tenantID, doctype, data, adjusted)
}
