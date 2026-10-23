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
	// Stage 73.3.1: what the tier gives (shown to the cashier), and how many
	// days a customer keeps it after their spend drops below its threshold.
	Benefits  string `json:"benefits"`
	GraceDays int    `json:"grace_days"`
}

// GetLoyaltyTierRules lists every configured tier, lowest threshold first.
func GetLoyaltyTierRules(tenantID string) ([]LoyaltyTierRule, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(
		`SELECT id, tier, min_spend, earn_multiplier, COALESCE(benefits, ''), COALESCE(grace_days, 0) FROM %s.loyalty_tier_rules ORDER BY min_spend ASC`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rules []LoyaltyTierRule
	for rows.Next() {
		var r LoyaltyTierRule
		if err := rows.Scan(&r.ID, &r.Tier, &r.MinSpend, &r.EarnMultiplier, &r.Benefits, &r.GraceDays); err != nil {
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

// SaveLoyaltyTierRule (Stage 73.3.1) creates or edits a whole tier from the
// Loyalty screen: threshold, multiplier, benefits and grace.
func SaveLoyaltyTierRule(tenantID string, r LoyaltyTierRule) error {
	if r.GraceDays < 0 {
		return fmt.Errorf("grace_days cannot be negative")
	}
	if err := UpsertLoyaltyTierRule(tenantID, r.Tier, r.MinSpend, r.EarnMultiplier); err != nil {
		return err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	_, err = db.DB.Exec(fmt.Sprintf(`UPDATE %s.loyalty_tier_rules SET benefits = $2, grace_days = $3 WHERE tier = $1`, schema), r.Tier, r.Benefits, r.GraceDays)
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
	// Stage 73.3.1: spend is what the customer's paid bills took (amount_paid,
	// recorded since 73.3.1; older bills' lines less their offer) less what was
	// refunded on them, over the tier window - lifetime when
	// loyalty.tier_window_days is 0. Before this, every bill read 0 here,
	// because nothing ever stored amount_paid, so no one was ever upgraded.
	lifetimeSpend, err := customerTierSpend(tenantID, schema, customerID)
	if err != nil {
		return "", err
	}

	rules, err := GetLoyaltyTierRules(tenantID)
	if err != nil {
		return "", err
	}
	newTier, newMin := "", -1.0
	for _, r := range rules { // lowest threshold first: the last one reached wins
		if lifetimeSpend >= r.MinSpend {
			newTier, newMin = r.Tier, r.MinSpend
		}
	}
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
	existing, _ := data["loyalty_tier"].(string)
	since, _ := data["loyalty_tier_below_since"].(string)
	grace := 0
	for _, r := range rules {
		if r.Tier == existing {
			grace = r.GraceDays
		}
	}
	nextSince := ""
	switch {
	case existing != "" && newMin < tierMinSpend(rules, existing) && grace > 0:
		// Spend has dropped below the current tier: it is kept for that
		// tier's grace days, counted from the first time this was seen.
		nextSince = since
		if nextSince == "" {
			nextSince = time.Now().Format("2006-01-02")
		}
		if d, errD := time.Parse("2006-01-02", nextSince); errD == nil && time.Since(d) < time.Duration(grace)*24*time.Hour {
			newTier = existing
		} else {
			nextSince = ""
		}
	}
	if newTier == existing && nextSince == since {
		return newTier, nil
	}
	if nextSince == "" {
		delete(data, "loyalty_tier_below_since")
	} else {
		data["loyalty_tier_below_since"] = nextSince
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

// customerTierSpend is what a customer's paid bills took over the tier window
// (loyalty.tier_window_days, 0 = all time), less what was refunded on them.
func customerTierSpend(tenantID, schema, customerID string) (float64, error) {
	window := GetSettingInt(tenantID, "loyalty.tier_window_days")
	carts := fmt.Sprintf(`SELECT id FROM %s.documents WHERE doctype = 'POSCart' AND status = 'Paid' AND data->>'customer_id' = $1
		AND ($2::int = 0 OR created_at >= NOW() - make_interval(days => $2::int))`, schema)
	var spend, refunded float64
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(SUM(%s), 0) FROM %s.documents WHERE id IN (%s)`, posCartSpendSQL, schema, carts),
		customerID, window).Scan(&spend); err != nil {
		return 0, err
	}
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(SUM(COALESCE((data->>'net_amount')::numeric, (data->>'amount')::numeric, 0)), 0)
		FROM %s.documents WHERE doctype = 'RefundRequest' AND data->>'status' = 'Processed' AND data->>'original_order_id' IN (%s)`, schema, carts),
		customerID, window).Scan(&refunded)
	return spend - refunded, nil
}

// tierMinSpend is a tier's threshold, -1 for none.
func tierMinSpend(rules []LoyaltyTierRule, tier string) float64 {
	for _, r := range rules {
		if r.Tier == tier {
			return r.MinSpend
		}
	}
	return -1
}

// posCartSpendSQL is what a paid POSCart took from its customer (Stage
// 73.3.1): amount_paid, which finalization records since then, or for an
// older bill its lines less its offer. Unqualified - for queries over
// documents with no alias.
const posCartSpendSQL = `COALESCE((data->>'amount_paid')::numeric,
	(SELECT COALESCE(SUM((i->>'qty')::numeric * (i->>'sale_price')::numeric), 0) FROM jsonb_array_elements(COALESCE(data->'items', '[]'::jsonb)) i)
	- COALESCE((data->>'offer_discount')::numeric, 0))`

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
		customerID                                    string
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
			continue
		}
		// Stage 73.2.12: when accruing, expired points release their liability
		// (breakage) - keyed per customer and day, so a re-run cannot repeat it.
		if loyaltyAccruesForSchema(schema) {
			var tenantID string
			if err := db.DB.QueryRow(`SELECT tenant_id FROM public.tenants WHERE schema_name = $1`, schema).Scan(&tenantID); err == nil {
				day := time.Now().Format("2006-01-02")
				if err := postLoyaltyValue(tenantID, loyaltyExpiryReferenceDoctype, r.customerID+":"+day, "LoyaltyExpiry:"+r.customerID+":"+day,
					int64(toExpire)*loyaltyPointValuePaise(tenantID), false, ""); err != nil {
					log.Printf("[LOYALTY-EXPIRY] %d points expired for %s but the liability was not released: %v", toExpire, r.customerID, err)
				}
			}
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
				if !IsSchedulerLeader() { // 134.7: one server runs each sweep
					continue
				}
				if db.DB == nil {
					continue
				}
				schemas, err := listTenantSchemas("crm_loyalty")
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
