package engines

import (
	"database/sql"
	"fmt"
	"time"
)

// Stage 73.1.2 / 73.1.3 - what a return does to the customer's points. Both
// run inside ProcessRefundRequest's transaction, under the customer's loyalty
// lock (lockedLoyaltyBalanceTx), so a refund and a till redemption of the same
// balance serialize. The ledger stays append-only: nothing is edited, every
// effect is a new row naming the refund it came from.
//
// Policy (user decisions D01-D03, 2026-10-17):
//   - D01: earned points are reversed pro rata on the money value returned -
//     the same base they were earned on (net of the offer and of points).
//   - D02: the part of the line paid for with points goes back as points, at
//     the points it was paid with; money only for the money-paid part.
//   - D03: if the customer has already spent the points being reversed, the
//     balance may go negative and later earnings pay it back. The cash refund
//     is never cut to cover them.

// LoyaltyTxnReversal is the ledger type for an earned-points reversal. Every
// balance query counts any non-Earn row as a deduction, so it needs no change
// anywhere; it is its own type so the history and the liability report can
// tell it apart from a redemption.
const LoyaltyTxnReversal = "Reversal"

// reverseEarnedPointsTx takes back the points this refund's money no longer
// earns. The target is cumulative - the sale's earn times the share of its
// money value refunded so far, this refund included - less what earlier
// refunds already reversed, so a bill returned in pieces reverses exactly what
// it earned by the time the last piece comes back.
func reverseEarnedPointsTx(tx *sql.Tx, schema, customerID, cartID, refundRequestID string, saleMoneyPaise int64) (int, error) {
	if customerID == "" || saleMoneyPaise <= 0 {
		return 0, nil
	}
	if _, err := lockedLoyaltyBalanceTx(tx, schema, customerID); err != nil {
		return 0, err
	}
	var earned int64
	if err := tx.QueryRow(fmt.Sprintf(`
		SELECT COALESCE(SUM(points), 0) FROM %s.loyalty_point_ledger
		WHERE customer_id = $1 AND transaction_type = 'Earn' AND reference_doctype = 'POSCart' AND reference_id = $2`, schema),
		customerID, cartID).Scan(&earned); err != nil {
		return 0, err
	}
	if earned <= 0 {
		return 0, nil
	}
	// Money refunded against this bill so far: every processed refund of it,
	// plus this one (processed in the caller's transaction, already marked).
	var refundedPaise int64
	if err := tx.QueryRow(fmt.Sprintf(`
		SELECT COALESCE(SUM(ROUND(COALESCE((data->>'net_amount')::numeric, (data->>'amount')::numeric, 0) * 100
		                    - COALESCE((data->>'points_value')::numeric, 0) * 100)), 0)::bigint
		FROM %s.documents
		WHERE doctype = 'RefundRequest' AND data->>'original_order_id' = $1 AND data->>'status' = 'Processed'`, schema),
		cartID).Scan(&refundedPaise); err != nil {
		return 0, err
	}
	target := earned
	if refundedPaise < saleMoneyPaise {
		target = earned * refundedPaise / saleMoneyPaise
	}
	var already int64
	if err := tx.QueryRow(fmt.Sprintf(`
		SELECT COALESCE(SUM(points), 0) FROM %s.loyalty_point_ledger
		WHERE customer_id = $1 AND transaction_type = $2 AND reference_doctype = 'RefundRequest'
		  AND reference_id IN (SELECT id FROM %s.documents WHERE doctype = 'RefundRequest' AND data->>'original_order_id' = $3)`, schema, schema),
		customerID, LoyaltyTxnReversal, cartID).Scan(&already); err != nil {
		return 0, err
	}
	reverse := int(target - already)
	if reverse <= 0 {
		return 0, nil
	}
	if _, err := tx.Exec(fmt.Sprintf(`
		INSERT INTO %s.loyalty_point_ledger (customer_id, transaction_type, points, reference_doctype, reference_id)
		VALUES ($1, $2, $3, 'RefundRequest', $4)`, schema),
		customerID, LoyaltyTxnReversal, reverse, refundRequestID); err != nil {
		return 0, err
	}
	return reverse, nil
}

// restoreRedeemedPointsTx gives back the points a returned line was paid with.
//
// They keep the latest expiry the customer's points had when the sale was
// made - the original lots are not tracked per redemption, so this is the
// most generous reading of "their original expiry" - but never less than the
// configured grace from today, so a same-day return can never hand back
// points that lapse before the customer could use them.
func restoreRedeemedPointsTx(tx *sql.Tx, tenantID, schema, customerID, cartID, refundRequestID string, points int) error {
	if customerID == "" || points <= 0 {
		return nil
	}
	if _, err := lockedLoyaltyBalanceTx(tx, schema, customerID); err != nil {
		return err
	}
	var latest sql.NullTime
	if err := tx.QueryRow(fmt.Sprintf(`
		SELECT MAX(l.expires_at) FROM %s.loyalty_point_ledger l, %s.documents c
		WHERE c.doctype = 'POSCart' AND c.id = $2
		  AND l.customer_id = $1 AND l.transaction_type = 'Earn' AND l.expires_at IS NOT NULL
		  AND l.created_at <= c.created_at AND l.expires_at > c.created_at`, schema, schema),
		customerID, cartID).Scan(&latest); err != nil {
		return err
	}
	expires := time.Now().AddDate(0, 0, GetSettingInt(tenantID, "loyalty.restored_points_grace_days"))
	if latest.Valid && latest.Time.After(expires) {
		expires = latest.Time
	}
	_, err := tx.Exec(fmt.Sprintf(`
		INSERT INTO %s.loyalty_point_ledger (customer_id, transaction_type, points, reference_doctype, reference_id, expires_at)
		VALUES ($1, 'Earn', $2, 'RefundRequest', $3, $4)`, schema),
		customerID, points, refundRequestID, expires)
	return err
}
