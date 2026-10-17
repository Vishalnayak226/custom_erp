package engines

import (
	"custom_erp/db"
	"testing"
	"time"
)

// Stage 65.6: a trial that runs out stops the client the way a pause does -
// sessions already signed in included, and without waiting for any cache - and
// extending it or marking the client paid gives access back at once.
func TestStage656TrialEndStopsClientAndEndingItRestores(t *testing.T) {
	db.InitDB(testConnStr())

	tenantID, schemaName := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schemaName)
	if _, err := ProvisionTenantSchema(tenantID, schemaName, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}

	endsAt, err := SetTenantTrial(tenantID, 30, "tester", "")
	if err != nil || endsAt == nil {
		t.Fatalf("SetTenantTrial(30) = %v, %v", endsAt, err)
	}
	if d := time.Until(*endsAt); d < 29*24*time.Hour || d > 31*24*time.Hour {
		t.Errorf("trial ends in %v, want about 30 days", d)
	}
	if ok, err := TenantIsOperational(tenantID); err != nil || !ok {
		t.Errorf("client on a running trial: operational=%v err=%v, want true", ok, err)
	}

	// A trial about to run out, read once so both caches hold it as working.
	soon := time.Now().Add(1200 * time.Millisecond)
	if _, err := db.DB.Exec(`UPDATE public.tenants SET trial_ends_at = $1 WHERE tenant_id = $2`, soon, tenantID); err != nil {
		t.Fatal(err)
	}
	InvalidateTenantLifecycleCache(tenantID)
	if _, err := ResolveLiveUserState(tenantID, "admin"); err != nil {
		t.Fatalf("session before the trial ends: %v", err)
	}
	time.Sleep(time.Until(soon) + 300*time.Millisecond)
	// No cache invalidation here on purpose: the cached gate must notice the
	// date itself.
	if ok, _ := TenantIsOperational(tenantID); ok {
		t.Error("client still operational after its trial ended")
	}
	if _, err := ResolveLiveUserState(tenantID, "admin"); !IsUserNotActiveError(err) {
		t.Errorf("a signed-in session survived the end of the trial: err = %v", err)
	}

	if _, err := SetTenantTrial(tenantID, 7, "tester", "asked for more time"); err != nil {
		t.Fatalf("extend: %v", err)
	}
	if _, err := ResolveLiveUserState(tenantID, "admin"); err != nil {
		t.Errorf("session after extending the trial: %v", err)
	}

	if _, err := db.DB.Exec(`UPDATE public.tenants SET trial_ends_at = NOW() - INTERVAL '1 day' WHERE tenant_id = $1`, tenantID); err != nil {
		t.Fatal(err)
	}
	InvalidateTenantLifecycleCache(tenantID)
	if ends, err := SetTenantTrial(tenantID, 0, "tester", "paid"); err != nil || ends != nil {
		t.Fatalf("mark paid = %v, %v; want no end date", ends, err)
	}
	if ok, err := TenantIsOperational(tenantID); err != nil || !ok {
		t.Errorf("paid client: operational=%v err=%v, want true", ok, err)
	}

	names := eventNames(t, tenantID)
	if !hasEvent(names, TenantEventTrialSet) || !hasEvent(names, TenantEventTrialEnded) {
		t.Errorf("evidence trail = %v, want the trial set and ended", names)
	}

	for _, days := range []int{-1, MaxTrialDays + 1} {
		if _, err := SetTenantTrial(tenantID, days, "tester", ""); err == nil {
			t.Errorf("SetTenantTrial(%d) accepted", days)
		}
	}
}
