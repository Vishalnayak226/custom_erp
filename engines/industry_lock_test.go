package engines

// Stage 51.3 - once an industry profile is set for a tenant it must be
// locked. This test uses its own throwaway tenant schema
// (uniqueLifecycleTenant/ProvisionTenantSchema/dropLifecycleTenant, the same
// pattern tenant_lifecycle_test.go already established) rather than the
// shared "default" dev tenant every other engines test runs against -
// SwitchIndustryProfile mutates doctype_meta/doctype_fields for whatever the
// profile JSON touches, and a real industry switch against the shared dev DB
// has caused real cross-session test pollution before (Brand's fields
// accumulating overrides from multiple industries run by different
// sessions). Isolating this test avoids adding to that.

import (
	"custom_erp/db"
	"testing"
	"time"
)

func TestIndustryLockAndOverride(t *testing.T) {
	db.InitDB(testConnStr())

	tenantID, schemaName := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schemaName)

	if _, err := ProvisionTenantSchema(tenantID, schemaName, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}

	lock, err := GetIndustryLock(tenantID)
	if err != nil {
		t.Fatalf("GetIndustryLock (fresh tenant): %v", err)
	}
	if lock.Locked {
		t.Fatalf("a freshly provisioned tenant must not already be locked, got %+v", lock)
	}

	if err := SwitchIndustryProfile(tenantID, "../public/profiles/jewelry.json", "alice", false, ""); err != nil {
		t.Fatalf("first switch (no prior lock) should succeed: %v", err)
	}

	lock, err = GetIndustryLock(tenantID)
	if err != nil {
		t.Fatalf("GetIndustryLock (after first switch): %v", err)
	}
	if !lock.Locked || lock.IndustryCode != "JEWELRY" || lock.SetBy != "alice" || lock.OverrideCount != 0 {
		t.Fatalf("unexpected lock state after first switch: %+v", lock)
	}
	firstSetAt := lock.SetAt

	// SwitchIndustryProfile itself trusts isOverride and does not re-check
	// the lock - the refusal of a plain (non-override) second switch is
	// handleSwitchIndustry's job (internal/server/handlers_core_doc_engine.go),
	// exercised there via a route test, not duplicated here. What's unique to
	// this function, and what this proves: an override switch correctly
	// increments override_count instead of resetting it, records the new
	// acting user, and advances set_at.
	time.Sleep(1100 * time.Millisecond) // ensure a distinguishable set_at (second-resolution timestamp)
	if err := SwitchIndustryProfile(tenantID, "../public/profiles/food_bev.json", "bob", true, "correcting a wrong initial setup"); err != nil {
		t.Fatalf("override switch should succeed: %v", err)
	}

	lock, err = GetIndustryLock(tenantID)
	if err != nil {
		t.Fatalf("GetIndustryLock (after override): %v", err)
	}
	if lock.IndustryCode != "FOOD_BEV" {
		t.Errorf("industry_code after override = %q, want FOOD_BEV", lock.IndustryCode)
	}
	if lock.SetBy != "bob" {
		t.Errorf("set_by after override = %q, want bob", lock.SetBy)
	}
	if lock.OverrideCount != 1 {
		t.Errorf("override_count after one override = %d, want 1", lock.OverrideCount)
	}
	if !lock.SetAt.After(firstSetAt) {
		t.Errorf("set_at did not advance across the override: first=%v second=%v", firstSetAt, lock.SetAt)
	}
}
