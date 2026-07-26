package engines

import (
	"context"
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"log"
	"time"
)

// Stage 26.7.3: Loyalty tiering + accrual/expiry. loyalty_tier_rules
// (db/migrations_stage26_7_crm_loyalty.sql) is a small self-service admin
// config table, the same pattern as approval_rules (Stage 24.8): a tier's
// min_spend threshold and its earn_multiplier. Customer.loyalty_tier is an
// additive field, recomputed after every earn (RecomputeLoyaltyTier) off
// lifetime POSCart spend - never manually authoritative, mirroring
// GetLoyaltyBalance's own "never a stored/editable source of truth" rule
// one level up.

// LoyaltyTierRule is one tier's threshold/multiplier.
type LoyaltyTierRule struct {
	ID             int     `json:"id"`
	Tier           string  `json:"tier"`
	MinSpend       float64 `json:"min_spend"`
	EarnMultiplier float64 `json:"earn_multiplier"`
}

// GetLoyaltyTierRules lists every configured tier, lowest threshold first.
func GetLoyaltyTierRules(tenantID string) ([]LoyaltyTierRule, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(
		`SELECT id, tier, min_spend, earn_multiplier FROM %s.loyalty_tier_rules ORDER BY min_spend ASC`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rules []LoyaltyTierRule
	for rows.Next() {
		var r LoyaltyTierRule
		if err := rows.Scan(&r.ID, &r.Tier, &r.MinSpend, &r.EarnMultiplier); err != nil {
			return nil, err
		}
		rules = append(rules, r)
	}
	return rules, nil
}

// UpsertLoyaltyTierRule creates or edits one tier's threshold/multiplier.
func UpsertLoyaltyTierRule(tenantID, tier string, minSpend, earnMultiplier float64) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	if tier == "" {
		return fmt.Errorf("tier is required")
	}
	if earnMultiplier <= 0 {
		return fmt.Errorf("earn_multiplier must be positive")
	}
	if minSpend < 0 {
		return fmt.Errorf("min_spend cannot be negative")
	}
	_, err = db.DB.Exec(fmt.Sprintf(`
		INSERT INTO %s.loyalty_tier_rules (tier, min_spend, earn_multiplier) VALUES ($1, $2, $3)
		ON CONFLICT (tier) DO UPDATE SET min_spend = EXCLUDED.min_spend, earn_multiplier = EXCLUDED.earn_multiplier`, schema),
		tier, minSpend, earnMultiplier)
	return err
}

// DeleteLoyaltyTierRule removes one tier - the admin screen's counterpart
// to UpsertLoyaltyTierRule, same convention as DeleteApprovalRule.
func DeleteLoyaltyTierRule(tenantID string, ruleID int) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	res, err := db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.loyalty_tier_rules WHERE id = $1`, schema), ruleID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("loyalty tier rule %d not found", ruleID)
	}
	return nil
}

func customerLoyaltyTier(schema, customerID string) string {
	var dataStr string
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT data FROM %s.documents WHERE doctype = 'Customer' AND id = $1`, schema), customerID).Scan(&dataStr); err != nil {
		return ""
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return ""
	}
	tier, _ := data["loyalty_tier"].(string)
	return tier
}

// loyaltyEarnMultiplierForCustomer looks up the customer's current tier's
// earn_multiplier - 1 (no boost) if the customer has no formal Customer
// record, no tier set yet, or no rule matches, so tiering is purely additive
// on top of the flat rate rather than a hard dependency of it.
func loyaltyEarnMultiplierForCustomer(tenantID, customerID string) float64 {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 1
	}
	tier := customerLoyaltyTier(schema, customerID)
	if tier == "" {
		return 1
	}
	var multiplier float64
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT earn_multiplier FROM %s.loyalty_tier_rules WHERE tier = $1`, schema), tier).Scan(&multiplier); err != nil {
		return 1
	}
	return multiplier
}

// RecomputeLoyaltyTier re-evaluates a customer's tier off lifetime Paid
// POSCart spend against loyalty_tier_rules' thresholds (highest threshold
// met wins), and updates Customer.loyalty_tier if it changed. A customer
// with no formal Customer record (e.g. a walk-in identified only by ID) or
// no configured rules is not an error - there's simply nothing to store.
func RecomputeLoyaltyTier(tenantID, customerID string) (string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	var lifetimeSpend float64
	if err := db.DB.QueryRow(fmt.Sprintf(`
		SELECT COALESCE(SUM((data->>'amount_paid')::numeric), 0) FROM %s.documents
		WHERE doctype = 'POSCart' AND status = 'Paid' AND data->>'customer_id' = $1`, schema), customerID).
		Scan(&lifetimeSpend); err != nil {
		return "", err
	}

	rows, err := db.DB.Query(fmt.Sprintf(`SELECT tier, min_spend FROM %s.loyalty_tier_rules ORDER BY min_spend DESC`, schema))
	if err != nil {
		return "", err
	}
	var newTier string
	for rows.Next() {
		var tier string
		var minSpend float64
		if err := rows.Scan(&tier, &minSpend); err != nil {
			rows.Close()
			return "", err
		}
		if lifetimeSpend >= minSpend {
			newTier = tier
			break
		}
	}
	rows.Close()
	if newTier == "" {
		return "", nil
	}

	var dataStr string
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT data FROM %s.documents WHERE doctype = 'Customer' AND id = $1`, schema), customerID).Scan(&dataStr); err != nil {
		return newTier, nil
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return newTier, nil
	}
	if existing, _ := data["loyalty_tier"].(string); existing == newTier {
		return newTier, nil
	}
	data["loyalty_tier"] = newTier
	marshaled, err := json.Marshal(data)
	if err != nil {
		return newTier, err
	}
	_, err = db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = $1, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'Customer' AND id = $2`, schema),
		marshaled, customerID)
	return newTier, err
}

// loyaltyExpiryReferenceDoctype tags the expiry sweep's own Burn rows so
// sweepLoyaltyExpiryForSchema can tell "already expired" apart from a
// customer-initiated redemption without a second transaction_type value -
// every existing balance query (GetLoyaltyBalance, the loyalty-summary/
// points-liability/RFM reports) already treats any non-Earn row as a
// deduction, so this needs zero changes to any of them.
const loyaltyExpiryReferenceDoctype = "LoyaltyExpiry"

// sweepLoyaltyExpiryForSchema expires lapsed Earn lots per customer. Not
// full FIFO lot-consumption (this ledger has no per-lot "remaining" tracking
// to consume against) - a documented approximation: it expires
// min(newly-lapsed-since-last-sweep, current balance), so a customer can
// never be pushed to a negative balance by a lot that a later redemption
// already effectively consumed. The same kind of deliberate simplification
// this file's other constants already carry.
func sweepLoyaltyExpiryForSchema(schema string) {
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT customer_id,
		       COALESCE(SUM(CASE WHEN transaction_type = 'Earn' AND expires_at IS NOT NULL AND expires_at <= CURRENT_TIMESTAMP THEN points ELSE 0 END), 0) AS expired_earned,
		       COALESCE(SUM(CASE WHEN transaction_type = 'Burn' AND reference_doctype = '%s' THEN points ELSE 0 END), 0) AS already_expired,
		       COALESCE(SUM(CASE WHEN transaction_type = 'Earn' THEN points ELSE -points END), 0) AS current_balance
		FROM %s.loyalty_point_ledger GROUP BY customer_id`, loyaltyExpiryReferenceDoctype, schema))
	if err != nil {
		log.Printf("[LOYALTY-EXPIRY] Failed to scan schema %s: %v", schema, err)
		return
	}
	type dueRow struct {
		customerID                                 string
		expiredEarned, alreadyExpired, currentBalance int
	}
	var due []dueRow
	for rows.Next() {
		var r dueRow
		if err := rows.Scan(&r.customerID, &r.expiredEarned, &r.alreadyExpired, &r.currentBalance); err == nil {
			due = append(due, r)
		}
	}
	rows.Close()

	for _, r := range due {
		toExpire := r.expiredEarned - r.alreadyExpired
		if toExpire <= 0 {
			continue
		}
		if toExpire > r.currentBalance {
			toExpire = r.currentBalance
		}
		if toExpire <= 0 {
			continue
		}
		if err := insertLoyaltyLedgerEntryInSchema(schema, r.customerID, "Burn", toExpire, loyaltyExpiryReferenceDoctype, "", nil); err != nil {
			log.Printf("[LOYALTY-EXPIRY] Failed to expire %d points for customer %s in schema %s: %v", toExpire, r.customerID, schema, err)
		}
	}
}

// StartLoyaltyExpiryWorker polls every tenant schema (re-queried each tick,
// same convention as StartOutboxWorker) for lapsed loyalty point lots.
func StartLoyaltyExpiryWorker(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if db.DB == nil {
					continue
				}
				schemas, err := listTenantSchemas()
				if err != nil {
					log.Printf("[LOYALTY-EXPIRY] Failed to list tenant schemas: %v", err)
					continue
				}
				for _, schema := range schemas {
					sweepLoyaltyExpiryForSchema(schema)
				}
			}
		}
	}()
}
