package engines

import (
	"fmt"

	"custom_erp/db"
)

// Stage 146.2.8 - what the till and the messages say about a member's points.

// loyaltyExpiringAndPending returns the usable points that lapse within the
// next 30 days (never more than the usable balance, since a burn spends the
// oldest lots first) and the points still waiting for their return window.
func loyaltyExpiringAndPending(schema, customerID string, balance int) (expiring, pending int) {
	_ = db.DB.QueryRow(fmt.Sprintf(`
		SELECT COALESCE(SUM(CASE WHEN transaction_type = 'Earn' AND `+crmUsablePointsSQL+` AND expires_at > NOW() AND expires_at <= NOW() + interval '30 days' THEN points ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN pending_until > NOW() AND cancelled_at IS NULL THEN CASE WHEN transaction_type = 'Earn' THEN points WHEN transaction_type = 'Reversal' THEN -points ELSE 0 END ELSE 0 END), 0)
		FROM %s.loyalty_point_ledger WHERE customer_id = $1`, schema), customerID).Scan(&expiring, &pending)
	if expiring > balance {
		expiring = balance
	}
	if expiring < 0 {
		expiring = 0
	}
	if pending < 0 {
		pending = 0
	}
	return expiring, pending
}

// loyaltyLastBillEarned is what the customer's most recent earning document
// paid them, for the points.earned message variable.
func loyaltyLastBillEarned(schema, customerID string) int {
	var n int
	_ = db.DB.QueryRow(fmt.Sprintf(`
		SELECT COALESCE(SUM(points), 0) FROM %[1]s.loyalty_point_ledger
		WHERE customer_id = $1 AND transaction_type = 'Earn' AND reference_id = (
			SELECT reference_id FROM %[1]s.loyalty_point_ledger
			WHERE customer_id = $1 AND transaction_type = 'Earn' AND reference_id IS NOT NULL
			ORDER BY created_at DESC, id DESC LIMIT 1)`, schema), customerID).Scan(&n)
	return n
}
