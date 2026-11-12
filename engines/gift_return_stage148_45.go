package engines

import (
	"database/sql"
	"fmt"
)

// Stage 148.45: a returned gift reverses the account its sale charged (the
// item's promotional expense), not COGS. The account is only taken when the
// original sale really posted a gift cost there, so a sale made before the
// item became a gift, or a sale that went to COGS, stays on COGS.

// giftReturnAccount is the expense account the original POS sale charged for
// sku as a gift, or "" when the line is not a gift or was not posted as one.
func giftReturnAccount(tx *sql.Tx, schema, tenantID, originalOrderID, sku string) string {
	if originalOrderID == "" {
		return ""
	}
	policy, _, known := ItemPolicyForSKU(tenantID, sku)
	if !known || policy.Sellability != SellPromoOnly || !policy.Tracked() || !policy.Valuated() || policy.ExpenseAccount == "" {
		return ""
	}
	var posted bool
	err := tx.QueryRow(fmt.Sprintf(`
		SELECT EXISTS(SELECT 1 FROM %s.gl_postings
		 WHERE document_type = 'POSCart' AND document_id = $1 AND account_code = $2 AND debit > 0
		   AND idempotency_key LIKE '%%:GIFT_COST:%%')`, schema), originalOrderID, policy.ExpenseAccount).Scan(&posted)
	if err != nil || !posted {
		return ""
	}
	return policy.ExpenseAccount
}

// returnCostLegs is the receipt's journal: stock back in (1200) against COGS
// for ordinary lines and against each gift account for gift lines.
func returnCostLegs(cogsRupees float64, gifts map[string]float64) (debits, credits map[string]int64) {
	debits, credits = map[string]int64{}, map[string]int64{}
	var total int64
	if p := RupeesToPaise(cogsRupees); p > 0 {
		credits["5100"] += p
		total += p
	}
	for acct, v := range gifts {
		if p := RupeesToPaise(v); p > 0 {
			credits[acct] += p
			total += p
		}
	}
	debits["1200"] = total
	return debits, credits
}
