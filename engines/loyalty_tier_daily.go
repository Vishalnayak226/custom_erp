package engines

import (
	"custom_erp/db"
	"fmt"
	"log"
	"time"
)

// Stage 146.1.1 - tiers are re-evaluated before an earn (loyalty.go) and, when
// loyalty.tier_daily_reevaluation is on, once a day for every member whose
// tier could have moved. Every change lands on the customer's timeline.

// CRMEventTierChanged is the crm_events row written per tier change. It is not
// offered to journeys; the customer timeline reads it.
const CRMEventTierChanged = "tier_changed"

// loyaltyTierPassBatch bounds one daily pass: members are taken oldest-tier
// first, so a tenant with more members than this finishes over a few days.
const loyaltyTierPassBatch = 2000

// loyaltyTierLapses is whether a member with no qualifying spend should lose
// the tier they hold: only on a tenant that counts spend over a window (with
// all-time spend a tier is never lost, as before).
func loyaltyTierLapses(tenantID, schema, customerID string) bool {
	if GetSettingInt(tenantID, "loyalty.tier_window_days") <= 0 {
		return false
	}
	return customerLoyaltyTier(schema, customerID) != ""
}

// recordLoyaltyTierChange puts one tier move on the customer's timeline.
func recordLoyaltyTierChange(schema, customerID, from, to string) {
	ref := fmt.Sprintf("%s:%s>%s:%d", customerID, from, to, time.Now().UnixNano())
	if err := RecordCRMEvent(schema, customerID, CRMEventTierChanged, ref, map[string]interface{}{"from": from, "to": to}); err != nil {
		log.Printf("[LOYALTY-TIER] tier change for %s not recorded: %v", customerID, err)
	}
}

// reevaluateLoyaltyTiersForSchema re-evaluates every member who holds a tier
// or has paid bills inside the tier window, and returns how many moved. Safe to
// run again: a member already at the right tier is not touched.
func reevaluateLoyaltyTiersForSchema(schema string) int {
	var tenantID string
	if err := db.DB.QueryRow(`SELECT tenant_id FROM public.tenants WHERE schema_name = $1`, schema).Scan(&tenantID); err != nil {
		return 0
	}
	window := GetSettingInt(tenantID, "loyalty.tier_window_days")
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id FROM %[1]s.documents WHERE doctype = 'Customer' AND deleted_at IS NULL AND
		  (COALESCE(data->>'loyalty_tier', '') <> '' OR id IN (
		     SELECT data->>'customer_id' FROM %[1]s.documents WHERE doctype = 'POSCart' AND status = 'Paid'
		     AND COALESCE(data->>'customer_id', '') <> '' AND ($1::int = 0 OR created_at >= NOW() - make_interval(days => $1::int))))
		ORDER BY updated_at LIMIT %[2]d`, schema, loyaltyTierPassBatch), window)
	if err != nil {
		log.Printf("[LOYALTY-TIER] member list failed for %s: %v", schema, err)
		return 0
	}
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	changed := 0
	for _, id := range ids {
		if _, moved, err := recomputeLoyaltyTierChanged(tenantID, id); err != nil {
			log.Printf("[LOYALTY-TIER] %s in %s: %v", id, schema, err)
		} else if moved {
			changed++
		}
	}
	return changed
}

// runDailyLoyaltyTierPass runs the pass once per tenant per day (a marker row
// in crm_events claims the day, so two servers or two ticks do not repeat it).
func runDailyLoyaltyTierPass(schema string) {
	var tenantID string
	if err := db.DB.QueryRow(`SELECT tenant_id FROM public.tenants WHERE schema_name = $1`, schema).Scan(&tenantID); err != nil {
		return
	}
	if !GetSettingBool(tenantID, "loyalty.tier_daily_reevaluation") {
		return
	}
	res, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.crm_events (customer_id, event_type, ref_id, dedupe_key, processed_at)
		VALUES ('_system', 'tier_pass', $1, $2, NOW()) ON CONFLICT (dedupe_key) DO NOTHING`, schema), BusinessToday(), "tier_pass:"+BusinessToday())
	if err != nil {
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return
	}
	if moved := reevaluateLoyaltyTiersForSchema(schema); moved > 0 {
		log.Printf("[LOYALTY-TIER] %s: %d member tier(s) re-evaluated", schema, moved)
	}
}
