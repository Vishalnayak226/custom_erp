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

// pointsPerRupee/redemptionValuePerPoint are the simplified earn/burn rates
// for this MVP (1 point per Rs.100 spent; 1 point = Rs.1 on redemption) -
// the blueprint's full Loyalty Engine makes these tenant-configurable
// (program/tier/earn-rule/burn-rule tables); a fixed rate is the deliberate
// simplification for this pass.
const (
	rupeesPerPoint          = 100
	redemptionValuePerPoint = 1
)

// LoyaltyLedgerEntry is one earn/burn transaction.
type LoyaltyLedgerEntry struct {
	TransactionType string    `json:"transaction_type"`
	Points          int       `json:"points"`
	ReferenceID     string    `json:"reference_id"`
	CreatedAt       time.Time `json:"created_at"`
}

func insertLoyaltyLedgerEntry(tenantID, customerID, transactionType string, points int, referenceDoctype, referenceID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	_, err = db.DB.Exec(fmt.Sprintf(`
		INSERT INTO %s.loyalty_point_ledger (customer_id, transaction_type, points, reference_doctype, reference_id)
		VALUES ($1, $2, $3, $4, $5)`, schema), customerID, transactionType, points, referenceDoctype, referenceID)
	return err
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
		return 0, fmt.Errorf("insufficient loyalty points: requested %d, balance %d", points, balance)
	}
	if err := insertLoyaltyLedgerEntry(tenantID, customerID, "Burn", points, "POSCart", referenceID); err != nil {
		return 0, err
	}
	return points * redemptionValuePerPoint, nil
}

// EarnLoyaltyPoints credits points for a completed sale. netSaleAmount
// should already exclude any redemption discount applied to the same
// checkout (points aren't earned on the portion paid for with points).
func EarnLoyaltyPoints(tenantID, customerID string, netSaleAmount int, referenceID string) error {
	points := netSaleAmount / rupeesPerPoint
	if points <= 0 {
		return nil
	}
	return insertLoyaltyLedgerEntry(tenantID, customerID, "Earn", points, "POSCart", referenceID)
}
