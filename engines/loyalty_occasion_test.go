package engines

import (
	"testing"
	"time"
)

// Stage 146.2.7 - a journey step "give points": once per occasion per customer
// per year, however many times the journey runs.
func TestStage14627BirthdayPointsPayOncePerYear(t *testing.T) {
	e := newCRMTestEnv(t)
	today := time.Now().In(crmLocation(e.schema)).Format("2006-01-02")
	c, _ := e.customer("Meera Iyer", map[string]interface{}{"date_of_birth": "1990" + today[4:]})
	blocked, _ := e.customer("Blocked Bhavna", map[string]interface{}{"date_of_birth": "1991" + today[4:], "loyalty_blocked": "Yes", "loyalty_block_reason": "Fraud"})

	// A step with no points, or an unknown occasion, is refused when saved.
	bad := CRMJourneyDefinition{Trigger: CRMJourneyTrigger{Type: "date", Date: "birthday"}, Steps: []CRMJourneyStep{{Type: "give_points", Points: 0}}}
	j0, err := SaveCRMJourney(e.tenantID, "", "Bad", "", bad, "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := PublishCRMJourney(e.tenantID, j0.ID, "test"); err == nil {
		t.Fatal("a give-points step with no points must not publish")
	}

	def := CRMJourneyDefinition{Trigger: CRMJourneyTrigger{Type: "date", Date: "birthday"}, Steps: []CRMJourneyStep{{Type: "give_points", Points: 50}}}
	mk := func(name string) string {
		j, err := SaveCRMJourney(e.tenantID, "", name, "", def, "test")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := PublishCRMJourney(e.tenantID, j.ID, "test"); err != nil {
			t.Fatal(err)
		}
		if _, err := SetCRMJourneyEnabled(e.tenantID, j.ID, true, false, "test"); err != nil {
			t.Fatal(err)
		}
		return j.ID
	}
	first, second := mk("Birthday A"), mk("Birthday B")
	run := func() {
		crmJourneyDailyTriggers(e.schema)
		crmAdvanceJourneys(e.schema)
	}
	run()
	run() // the second tick of the same day
	bal := func(id string) int { b, _ := GetLoyaltyBalance(e.tenantID, id); return b }
	if got := bal(c); got != 50 {
		t.Fatalf("balance %d after two ticks and two journeys, want exactly 50", got)
	}
	// A run ended and started again (a second trigger, another journey) never pays twice.
	e.exec(`DELETE FROM {s}.crm_journey_runs WHERE customer_id = $1`, c)
	run()
	if got := bal(c); got != 50 {
		t.Fatalf("balance %d after the runs were cleared and the journeys ran again, want 50", got)
	}
	if n := e.count(`SELECT COUNT(*) FROM {s}.loyalty_point_ledger WHERE customer_id = $1 AND reference_doctype = 'LoyaltyOccasion'`, c); n != 1 {
		t.Fatalf("%d occasion rows, want 1", n)
	}
	if r := e.str(`SELECT reason FROM {s}.loyalty_point_ledger WHERE customer_id = $1 AND reference_doctype = 'LoyaltyOccasion'`, c); r != "Birthday points" {
		t.Errorf("reason %q, want it to name the occasion", r)
	}
	if e.count(`SELECT COUNT(*) FROM {s}.loyalty_point_ledger WHERE customer_id = $1 AND expires_at IS NOT NULL`, c) != 1 {
		t.Error("birthday points carry the normal expiry")
	}
	// A blocked member is paid nothing, and the run says why.
	if got := bal(blocked); got != 0 {
		t.Errorf("a blocked member got %d points", got)
	}
	// Next year is a new occasion.
	e.exec(`UPDATE {s}.loyalty_point_ledger SET reference_id = 'birthday:1999' WHERE customer_id = $1`, c)
	e.exec(`DELETE FROM {s}.crm_journey_runs WHERE customer_id = $1`, c)
	run()
	if got := bal(c); got != 100 {
		t.Fatalf("balance %d in a new year, want 100", got)
	}
	_, _ = first, second
}
