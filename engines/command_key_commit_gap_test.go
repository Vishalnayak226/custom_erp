package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"testing"
)

// 49.4.7: what a process death between a keyed handler's commit and the key's
// own record does. The checkout records the key inside its transaction, so it
// has no such window. A command wrapped by internal/server commandKeyed commits
// first and records after (CompleteCommand): a death in between leaves the
// business row committed and the claim InProgress. Owner's answer: such a claim
// is never taken over - the retry is told "still being processed" (GLOBAL-4971)
// until the claim's 7-day retention ends, and only then is the key new again.
func TestStage4947bDeathBetweenHandlerCommitAndKeyRecord(t *testing.T) {
	spInitDB()
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatal(err)
	}
	marker := NewDocID("GAP")
	key := NewDocID("gap-key")
	defer db.DB.Exec(`DELETE FROM `+schema+`.documents WHERE doctype = 'CourierTrackingEvent' AND data->>'marker' = $1`, marker)
	defer db.DB.Exec(`DELETE FROM `+schema+`.command_idempotency WHERE idempotency_key LIKE $1`, "%"+key)
	digest := CommandDigest(map[string]string{"marker": marker})
	data, _ := json.Marshal(map[string]string{"marker": marker})
	run := func() {
		if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id,doctype,data,status,created_by) VALUES ($1,'CourierTrackingEvent',$2,'Processed','system')`, schema), NewDocID("CTE"), data); err != nil {
			t.Fatal(err)
		}
	}
	claim := func(corr string) ClaimOutcome {
		t.Helper()
		c, err := ClaimCommandRecordedAfter("default", "test.gap", key, digest, corr)
		if err != nil {
			t.Fatal(err)
		}
		if c.Outcome == ClaimAcquired {
			run()
		}
		return c.Outcome
	}
	lapse := func(ago string) {
		t.Helper()
		if _, err := db.DB.Exec(`UPDATE `+schema+`.command_idempotency SET lease_expires_at = CURRENT_TIMESTAMP - $2::interval WHERE idempotency_key = $1`, "test.gap:"+key, ago); err != nil {
			t.Fatal(err)
		}
	}

	if o := claim("corr-1"); o != ClaimAcquired { // the handler commits ... and the process dies before CompleteCommand
		t.Fatalf("first claim: %v", o)
	}
	if o := claim("corr-2"); o != ClaimInProgress {
		t.Fatalf("retry inside the lease: %v", o)
	}
	lapse("1 second")
	if o := claim("corr-3"); o != ClaimInProgress {
		t.Fatalf("retry after the lease: %v, want still in progress (never taken over)", o)
	}
	if n := killTestEffects(t, schema, marker); n != 1 {
		t.Fatalf("the command ran %d times, want once", n)
	}
	// Past the claim's retention the sweep removes it and the key is new again.
	lapse(fmt.Sprintf("%d days 1 minute", commandClaimRetentionDays))
	purgeSettledCommandClaims(schema)
	if o := claim("corr-4"); o != ClaimAcquired {
		t.Fatalf("retry after the retention: %v, want a new claim", o)
	}
}
