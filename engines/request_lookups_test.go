package engines

import (
	"custom_erp/db"
	"fmt"
	"testing"
	"time"
)

// Stage 49.18.8: two of the three lookups every request made are now cached,
// the third deliberately is not. The sandbox expiry comes from the tenant gate
// row (refreshed on invalidation, compared with now on every call); a module
// switched off is refused on the very next check, cache or no cache.
func TestStage49188SandboxFromGateEntitlementStaysFresh(t *testing.T) {
	db.InitDB(testConnStr())

	tenantID := fmt.Sprintf("s49188%d", time.Now().UnixNano()%1_000_000_000)
	if _, err := db.DB.Exec(`INSERT INTO public.tenants (tenant_id, name, schema_name, is_sandbox, sandbox_expires_at)
		VALUES ($1, $1, $2, TRUE, NOW() + INTERVAL '1 hour')`, tenantID, "tenant_"+tenantID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		db.DB.Exec(`DELETE FROM public.tenants WHERE tenant_id = $1`, tenantID)
		InvalidateTenantLifecycleCache(tenantID)
	}()
	InvalidateTenantLifecycleCache(tenantID)

	if expired, _ := IsSandboxExpired(tenantID); expired {
		t.Fatal("a sandbox an hour from expiry reported expired")
	}
	// Expiry moved into the past behind the cache's back: the cached row
	// still answers until the entry is invalidated (as every writer does).
	db.DB.Exec(`UPDATE public.tenants SET sandbox_expires_at = NOW() - INTERVAL '1 minute' WHERE tenant_id = $1`, tenantID)
	if expired, _ := IsSandboxExpired(tenantID); expired && authStateCacheTTL() > 0 {
		t.Fatal("the sandbox check is still querying the row on every call - it should read the gate entry")
	}
	InvalidateTenantLifecycleCache(tenantID)
	if expired, _ := IsSandboxExpired(tenantID); !expired {
		t.Fatal("after invalidation an expired sandbox must report expired")
	}

	// The entitlement is never cached: a raw flip is seen immediately.
	const module = "pim"
	var was bool
	if err := db.DB.QueryRow(`SELECT enabled FROM tenant_default.module_entitlements WHERE module_key = $1`, module).Scan(&was); err != nil {
		t.Skipf("no %s entitlement row: %v", module, err)
	}
	defer db.DB.Exec(`UPDATE tenant_default.module_entitlements SET enabled = $1 WHERE module_key = $2`, was, module)
	for _, want := range []bool{false, true, false} {
		db.DB.Exec(`UPDATE tenant_default.module_entitlements SET enabled = $1 WHERE module_key = $2`, want, module)
		if got := RequireModules("default", module) == nil; got != want {
			t.Fatalf("module %s set to %v, RequireModules says enabled=%v - the entitlement must stay a fresh read", module, want, got)
		}
	}

	// The doctype's module is cached and still right.
	first, err := ModuleForDoctype("default", "Item")
	if err != nil {
		t.Fatal(err)
	}
	if again, _ := ModuleForDoctype("default", "Item"); again != first || first == "" {
		t.Fatalf("Item's module: %q then %q", first, again)
	}
}
