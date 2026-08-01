package engines

import (
	"custom_erp/db"
	"fmt"
	"time"
)

// CRM/Loyalty (Stage 13.13d, scoped MVP per the CRM/Loyalty add-on
// blueprint Sec.3.4/3.5): the Loyalty Point Ledger and earn/burn only.
// Customer 360, Segmentation, Campaign Management, Voucher/Coupon Engine,
// Customer Service, and Consent & Privacy are explicitly out of scope.

// redemptionValuePerPointFor is the burn rate (default 1 point = Rs.1 on
// redemption). Stage 30.7 made it the last of the three loyalty rates to
// become tenant-configurable - the earn rate (loyalty.rupees_per_point) and
// point expiry (loyalty.point_expiry_days) already were. All three are read
// at their use sites on each call, so an admin edit applies immediately.
func redemptionValuePerPointFor(tenantID string) int {
	return GetSettingInt(tenantID, "loyalty.redemption_value_per_point")
}

// LoyaltyLedgerEntry is one earn/burn transaction.
type LoyaltyLedgerEntry struct {
	TransactionType string    `json:"transaction_type"`
	Points          int       `json:"points"`
	ReferenceID     string    `json:"reference_id"`
	CreatedAt       time.Time `json:"created_at"`
}

// insertLoyaltyLedgerEntryInSchema is the schema-level primitive - needed
// directly by StartLoyaltyExpiryWorker (engines/loyalty_tiering.go), which
// already has the schema from its own tenant-schema scan and no tenantID to
// re-derive it from, the same split CreateJournalVoucher/
// createJournalVoucherInSchema uses. expiresAt is nil for Burn rows (and
// for the expiry sweep's own Burn rows) - only an Earn row's points lapse.
func insertLoyaltyLedgerEntryInSchema(schema, customerID, transactionType string, points int, referenceDoctype, referenceID string, expiresAt *time.Time) error {
	_, err := db.DB.Exec(fmt.Sprintf(`
		INSERT INTO %s.loyalty_point_ledger (customer_id, transaction_type, points, reference_doctype, reference_id, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)`, schema), customerID, transactionType, points, referenceDoctype, referenceID, expiresAt)
	return err
}

func insertLoyaltyLedgerEntry(tenantID, customerID, transactionType string, points int, referenceDoctype, referenceID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	return insertLoyaltyLedgerEntryInSchema(schema, customerID, transactionType, points, referenceDoctype, referenceID, nil)
}

// GetLoyaltyBalance computes a customer's current point balance as
// SUM(Earn) - SUM(Burn) from the ledger - never a stored/editable field,
// per the blueprint's explicit "Never Do This: directly edit point balance"
// rule.
func GetLoyaltyBalance(tenantID, customerID string) (int, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, err
	}
	var balance int
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT COALESCE(SUM(CASE WHEN transaction_type = 'Earn' THEN points ELSE -points END), 0)
		FROM %s.loyalty_point_ledger WHERE customer_id = $1`, schema), customerID).Scan(&balance)
	return balance, err
}

// GetLoyaltyLedger lists a customer's point transaction history, most
// recent first.
func GetLoyaltyLedger(tenantID, customerID string) ([]LoyaltyLedgerEntry, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT transaction_type, points, COALESCE(reference_id, ''), created_at
		FROM %s.loyalty_point_ledger WHERE customer_id = $1 ORDER BY created_at DESC`, schema), customerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []LoyaltyLedgerEntry
	for rows.Next() {
		var e LoyaltyLedgerEntry
		if err := rows.Scan(&e.TransactionType, &e.Points, &e.ReferenceID, &e.CreatedAt); err != nil {
			return nil, err
		}
		results = append(results, e)
	}
	return results, nil
}

// RedeemLoyaltyPoints burns points against a customer's balance (checking
// funds first - no negative balance is ever allowed, per the blueprint's
// "no negative balance" POS redemption control) and returns the rupee
// discount value it's worth, for the caller (checkout) to apply.
func RedeemLoyaltyPoints(tenantID, customerID string, points int, referenceID string) (discountValue int, err error) {
	if points <= 0 {
		return 0, nil
	}
	balance, err := GetLoyaltyBalance(tenantID, customerID)
	if err != nil {
		return 0, err
	}
	if points > balance {
		// CUSTOM-0134 (Stage 25.5): "Loyalty points insufficient" - an exact
		// scenario match, already returned as a 422 at its one call site
		// (handleRedeemLoyaltyPoints), matching the catalog's own status.
		return 0, &ValidationError{Code: "CUSTOM-0134", Message: fmt.Sprintf("insufficient loyalty points: requested %d, balance %d", points, balance)}
	}
	if err := insertLoyaltyLedgerEntry(tenantID, customerID, "Burn", points, "POSCart", referenceID); err != nil {
		return 0, err
	}
	return points * redemptionValuePerPointFor(tenantID), nil
}

// LoyaltyRedemptionValue is what `points` are worth in rupees, without
// touching the ledger - so a caller can show the customer the discount and
// check it against the balance before anything is burned. Stage 30.2.5's POS
// flow uses this at "Redeem Points" time and only burns at checkout.
func LoyaltyRedemptionValue(tenantID string, points int) int {
	if points <= 0 {
		return 0
	}
	return points * redemptionValuePerPointFor(tenantID)
}

// ReverseLoyaltyRedemption gives back points burned for a sale that then
// failed to complete (Stage 30.2.5). The ledger is append-only by design -
// the blueprint's "never edit a point balance" rule - so the reversal is a
// compensating Earn row referencing the same cart, which restores the balance
// while leaving the original Burn visible in the customer's history.
//
// The restored points deliberately carry no expiry: they are a correction of
// something that never happened, not a fresh accrual, so re-dating their
// lifetime would quietly shorten or extend it.
func ReverseLoyaltyRedemption(tenantID, customerID string, points int, referenceID string) error {
	if points <= 0 || customerID == "" {
		return nil
	}
	return insertLoyaltyLedgerEntry(tenantID, customerID, "Earn", points, "POSCart", referenceID+":REDEMPTION-REVERSAL")
}

// EarnLoyaltyPoints credits points for a completed sale. netSaleAmount
// should already exclude any redemption discount applied to the same
// checkout (points aren't earned on the portion paid for with points).
// Stage 26.7.3: the base points are scaled by the customer's current tier's
// earn_multiplier (engines/loyalty_tiering.go), the earned lot is stamped
// with an expiry (loyaltyPointExpiryDays out), and the customer's tier is
// re-evaluated against their new lifetime spend - both best-effort
// enhancements on top of the points already earned/inserted, so a failure
// in either never undoes or blocks the earn itself.
func EarnLoyaltyPoints(tenantID, customerID string, netSaleAmount int, referenceID string) error {
	rupeesPerPoint := GetSettingInt(tenantID, "loyalty.rupees_per_point")
	if rupeesPerPoint <= 0 {
		rupeesPerPoint = 100 // defensive: the setting is min-1 validated, never 0
	}
	basePoints := netSaleAmount / rupeesPerPoint
	if basePoints <= 0 {
		return nil
	}
	points := int(float64(basePoints) * loyaltyEarnMultiplierForCustomer(tenantID, customerID))
	if points <= 0 {
		return nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	expiresAt := time.Now().AddDate(0, 0, GetSettingInt(tenantID, "loyalty.point_expiry_days"))
	if err := insertLoyaltyLedgerEntryInSchema(schema, customerID, "Earn", points, "POSCart", referenceID, &expiresAt); err != nil {
		return err
	}
	if GetSettingBool(tenantID, "loyalty.recompute_tier_on_earn") {
		if _, err := RecomputeLoyaltyTier(tenantID, customerID); err != nil {
			LogSystemError(tenantID, "", "ERROR", "EarnLoyaltyPoints", fmt.Sprintf("tier recompute failed for customer %s: %v", customerID, err), "")
		}
	}
	return nil
}
