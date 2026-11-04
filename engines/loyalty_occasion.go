package engines

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// Stage 146.2.7 - birthday and anniversary points. A CRM journey step
// "give_points" credits a fixed number of points once per customer per
// occasion per year. The ledger row's reference ("birthday:2026") is covered
// by a unique index (db/migrations_stage146_2_7_occasion_online_points.sql), so a
// re-run journey, a second trigger or a second journey on the same occasion
// can never pay twice. Blocked members and never-earn tags get nothing, the
// points expire like any earn, and nothing happens until an admin adds the
// step to a journey.

const loyaltyOccasionDoctype = "LoyaltyOccasion"

var loyaltyOccasionNames = map[string]string{"birthday": "Birthday points", "anniversary": "Anniversary points"}

// crmGivePointsOccasion is the occasion a give_points step pays for: its own,
// else the one its journey starts on.
func crmGivePointsOccasion(s CRMJourneyStep, trigger CRMJourneyTrigger) string {
	occasion := strings.ToLower(strings.TrimSpace(s.Occasion))
	if occasion == "" && trigger.Type == "date" {
		occasion = trigger.Date
	}
	if _, ok := loyaltyOccasionNames[occasion]; !ok {
		return ""
	}
	return occasion
}

// crmGivePointsTx pays the step inside the journey's own transaction. It
// returns the points given (0 when none) and a short note for the run history.
func crmGivePointsTx(tx *sql.Tx, schema, customerID string, s CRMJourneyStep, trigger CRMJourneyTrigger) (int, string, error) {
	occasion := crmGivePointsOccasion(s, trigger)
	if occasion == "" || s.Points <= 0 {
		return 0, "no occasion or points set", nil
	}
	tenantID, err := tenantIDForSchema(schema)
	if err != nil {
		return 0, "", err
	}
	if IsWalkInCustomer(tenantID, customerID) {
		return 0, "walk-in customer", nil
	}
	if why := loyaltyEarnBlockReason(tenantID, loyaltyCustomerFactsFor(schema, customerID)); why != "" {
		return 0, "no points: " + why, nil
	}
	year := BusinessToday()[:4]
	ref := occasion + ":" + year
	expires := time.Now().AddDate(0, 0, GetSettingIntForSchema(schema, "loyalty.point_expiry_days"))
	res, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.loyalty_point_ledger (customer_id, transaction_type, points, reference_doctype, reference_id, expires_at, reason)
		VALUES ($1, 'Earn', $2, $3, $4, $5, $6)
		ON CONFLICT (customer_id, reference_id) WHERE reference_doctype = 'LoyaltyOccasion' DO NOTHING`, schema),
		customerID, s.Points, loyaltyOccasionDoctype, ref, expires, loyaltyOccasionNames[occasion])
	if err != nil {
		return 0, "", err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return 0, fmt.Sprintf("%s %s already given", occasion, year), nil
	}
	// With accrual accounting on, the points' value is a liability from now.
	if err := postLoyaltyValueTx(tx, tenantID, schema, loyaltyOccasionDoctype, customerID+":"+ref, loyaltyOccasionDoctype+":"+customerID+":"+ref+":ACCRUE",
		int64(s.Points)*loyaltyPointValuePaise(tenantID), true, ""); err != nil {
		return 0, "", err
	}
	return s.Points, fmt.Sprintf("%d point(s) for %s %s", s.Points, occasion, year), nil
}
