package engines

// Stage 49.1.5 - secure tenant provisioning/deprovisioning.
//
// Every test here uses a tenant id unique to the run: this package's tests
// share one long-lived dev database, and a fixed id would collide with the
// debris of an earlier run (and, worse, with a concurrent one).

import (
	"custom_erp/db"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func uniqueLifecycleTenant(t *testing.T) (tenantID, schemaName string) {
	t.Helper()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano()%1_000_000_000)
	return "lifecycle" + suffix, "tenant_lifecycle" + suffix
}

func dropLifecycleTenant(tenantID, schemaName string) {
	db.DB.Exec("DROP SCHEMA IF EXISTS " + schemaName + " CASCADE")
	db.DB.Exec(`DELETE FROM public.tenants WHERE tenant_id = $1`, tenantID)
	db.DB.Exec(`DELETE FROM public.tenant_lifecycle_events WHERE tenant_id = $1`, tenantID)
	InvalidateTenantLifecycleCache(tenantID)
}

func eventNames(t *testing.T, tenantID string) []string {
	t.Helper()
	events, err := TenantLifecycleEvents(tenantID, 100)
	if err != nil {
		t.Fatalf("TenantLifecycleEvents: %v", err)
	}
	names := make([]string, 0, len(events))
	for _, e := range events {
		names = append(names, e.Event)
	}
	return names
}

func hasEvent(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}

func TestStage4915ProvisioningIsAtomicAndEvidenced(t *testing.T) {
	db.InitDB(testConnStr())

	t.Run("a provisioned tenant records its one-time credential, its expiry and its evidence", func(t *testing.T) {
		tenantID, schemaName := uniqueLifecycleTenant(t)
		defer dropLifecycleTenant(tenantID, schemaName)

		password, err := ProvisionTenantSchema(tenantID, schemaName, "0.1.0-test")
		if err != nil {
			t.Fatalf("ProvisionTenantSchema: %v", err)
		}
		if password == "" {
			t.Fatal("expected a one-time admin password")
		}

		life, err := GetTenantLifecycle(tenantID)
		if err != nil {
			t.Fatalf("GetTenantLifecycle: %v", err)
		}
		if life.Status != TenantStatusActive {
			t.Errorf("new tenant status = %q, want %q", life.Status, TenantStatusActive)
		}
		if life.ProvisionedAt == nil {
			t.Error("provisioned_at was not stamped")
		}
		if !life.BootstrapOutstanding {
			t.Error("expected the one-time credential to be outstanding immediately after provisioning")
		}
		if life.BootstrapExpired {
			t.Error("a freshly issued credential must not already be expired")
		}
		if life.BootstrapExpiresAt == nil {
			t.Fatal("bootstrap_expires_at was not set - the credential would never expire")
		}
		if d := time.Until(*life.BootstrapExpiresAt); d < 71*time.Hour || d > 73*time.Hour {
			t.Errorf("bootstrap expiry is %v away, want ~72h", d)
		}

		// The recorded hash must be exactly the admin row's hash: that byte
		// equality is what "never rotated" means here.
		var storedHash, recordedHash string
		if err := db.DB.QueryRow(fmt.Sprintf(`SELECT password_hash FROM %s.users WHERE id = 'admin'`, schemaName)).Scan(&storedHash); err != nil {
			t.Fatalf("read admin hash: %v", err)
		}
		if err := db.DB.QueryRow(`SELECT bootstrap_password_hash FROM public.tenants WHERE tenant_id = $1`, tenantID).Scan(&recordedHash); err != nil {
			t.Fatalf("read recorded hash: %v", err)
		}
		if storedHash != recordedHash {
			t.Error("the recorded bootstrap hash does not match the admin row's hash")
		}

		// The admin role must be one MFA is mandatory for, or the one-time
		// password alone would be enough to reach the tenant's data.
		var role string
		if err := db.DB.QueryRow(fmt.Sprintf(`SELECT role FROM %s.users WHERE id = 'admin'`, schemaName)).Scan(&role); err != nil {
			t.Fatalf("read admin role: %v", err)
		}
		if !RequiresMFA(role) {
			t.Errorf("provisioned admin role %q is not MFA-mandatory", role)
		}

		names := eventNames(t, tenantID)
		if !hasEvent(names, TenantEventProvisioned) || !hasEvent(names, TenantEventBootstrapIssued) {
			t.Errorf("evidence trail = %v, want it to contain %q and %q", names, TenantEventProvisioned, TenantEventBootstrapIssued)
		}
	})

	t.Run("a provisioning that fails part-way leaves no registry row behind", func(t *testing.T) {
		tenantID, schemaName := uniqueLifecycleTenant(t)
		defer dropLifecycleTenant(tenantID, schemaName)

		// Pre-create the schema with a doctype_meta that does not match the
		// template, so the clone step skips it (CREATE TABLE IF NOT EXISTS)
		// and the seed INSERT ... SELECT * fails on the column mismatch. That
		// is a realistic mid-provisioning failure, and before 49.1.5 it left a
		// registry row resolving to a half-built schema.
		if _, err := db.DB.Exec("CREATE SCHEMA " + schemaName); err != nil {
			t.Fatalf("prepare schema: %v", err)
		}
		if _, err := db.DB.Exec(fmt.Sprintf("CREATE TABLE %s.doctype_meta (not_the_template_shape INT)", schemaName)); err != nil {
			t.Fatalf("prepare table: %v", err)
		}

		if _, err := ProvisionTenantSchema(tenantID, schemaName, "0.1.0-test"); err == nil {
			t.Fatal("expected provisioning to fail against an incompatible pre-existing schema")
		}

		if _, err := GetTenantLifecycle(tenantID); !errors.Is(err, ErrTenantNotRegistered) {
			t.Errorf("after a failed provisioning GetTenantLifecycle err = %v, want ErrTenantNotRegistered - the registry row was not rolled back", err)
		}
	})

	t.Run("an unusable schema name is refused before any DDL runs", func(t *testing.T) {
		if _, err := ProvisionTenantSchema("lifecycle_bad_name", "tenant; DROP SCHEMA public", ""); err == nil {
			t.Fatal("expected a schema name that is not a plain identifier to be refused")
		}
	})
}

func TestStage4915BootstrapCredentialExpiresAndRotates(t *testing.T) {
	db.InitDB(testConnStr())

	tenantID, schemaName := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schemaName)
	if _, err := ProvisionTenantSchema(tenantID, schemaName, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}

	var adminHash string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT password_hash FROM %s.users WHERE id = 'admin'`, schemaName)).Scan(&adminHash); err != nil {
		t.Fatalf("read admin hash: %v", err)
	}

	t.Run("inside its window the credential is pending, and the login path is told when it lapses", func(t *testing.T) {
		state, expiry, err := EvaluateTenantBootstrapCredential(tenantID, adminHash)
		if err != nil {
			t.Fatalf("EvaluateTenantBootstrapCredential: %v", err)
		}
		if state != BootstrapPending {
			t.Errorf("state = %v, want BootstrapPending", state)
		}
		if expiry == nil {
			t.Error("expected the caller to be told when the credential stops working")
		}
	})

	t.Run("past its window it is expired, which is what refuses the login", func(t *testing.T) {
		if _, err := db.DB.Exec(
			`UPDATE public.tenants SET bootstrap_expires_at = NOW() - INTERVAL '1 hour' WHERE tenant_id = $1`, tenantID); err != nil {
			t.Fatalf("age the credential: %v", err)
		}
		state, _, err := EvaluateTenantBootstrapCredential(tenantID, adminHash)
		if err != nil {
			t.Fatalf("EvaluateTenantBootstrapCredential: %v", err)
		}
		if state != BootstrapExpired {
			t.Fatalf("state = %v, want BootstrapExpired", state)
		}
		life, err := GetTenantLifecycle(tenantID)
		if err != nil {
			t.Fatalf("GetTenantLifecycle: %v", err)
		}
		if !life.BootstrapExpired {
			t.Error("the registry view disagrees with the login-path evaluation about expiry")
		}
	})

	t.Run("rotating the password consumes the credential exactly once and records it", func(t *testing.T) {
		// Any write to the admin's password_hash counts, including one made
		// outside the app - which is the point of comparing hashes rather than
		// trusting a flag the app has to remember to set.
		if _, err := db.DB.Exec(fmt.Sprintf(
			`UPDATE %s.users SET password_hash = 'rotated-by-hand' WHERE id = 'admin'`, schemaName)); err != nil {
			t.Fatalf("rotate: %v", err)
		}
		state, _, err := EvaluateTenantBootstrapCredential(tenantID, "rotated-by-hand")
		if err != nil {
			t.Fatalf("EvaluateTenantBootstrapCredential: %v", err)
		}
		if state != BootstrapNotApplicable {
			t.Errorf("state = %v, want BootstrapNotApplicable after rotation", state)
		}
		life, err := GetTenantLifecycle(tenantID)
		if err != nil {
			t.Fatalf("GetTenantLifecycle: %v", err)
		}
		if life.BootstrapConsumedAt == nil {
			t.Error("rotation was not recorded")
		}
		if life.BootstrapOutstanding {
			t.Error("a rotated credential must not still count as outstanding")
		}
		if !hasEvent(eventNames(t, tenantID), TenantEventBootstrapConsumed) {
			t.Error("rotation left no evidence")
		}
	})

	t.Run("re-provisioning over a rotated credential is refused", func(t *testing.T) {
		if _, err := ProvisionTenantSchema(tenantID, schemaName, "0.1.0-test"); err == nil {
			t.Fatal("expected re-provisioning to refuse to overwrite a rotated admin password")
		}
	})

	t.Run("an operator can reissue a one-time credential, and it starts a fresh window", func(t *testing.T) {
		password, err := RotateTenantBootstrapCredential(tenantID, "admin", "tester", "handover credential lost")
		if err != nil {
			t.Fatalf("RotateTenantBootstrapCredential: %v", err)
		}
		if password == "" {
			t.Fatal("expected a new one-time password")
		}
		life, err := GetTenantLifecycle(tenantID)
		if err != nil {
			t.Fatalf("GetTenantLifecycle: %v", err)
		}
		if !life.BootstrapOutstanding || life.BootstrapExpired {
			t.Errorf("after reissue outstanding=%v expired=%v, want true/false", life.BootstrapOutstanding, life.BootstrapExpired)
		}
		if !hasEvent(eventNames(t, tenantID), TenantEventBootstrapRotated) {
			t.Error("the reissue left no evidence")
		}
	})

	t.Run("a reissue requires a reason", func(t *testing.T) {
		if _, err := RotateTenantBootstrapCredential(tenantID, "admin", "tester", ""); err == nil {
			t.Error("expected an unexplained credential reissue to be refused")
		}
	})
}

func TestStage4915SuspensionStopsLiveSessions(t *testing.T) {
	db.InitDB(testConnStr())

	tenantID, schemaName := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schemaName)
	if _, err := ProvisionTenantSchema(tenantID, schemaName, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}

	// Warm both caches, so this proves a suspension reaches a session that is
	// already established rather than only a cold lookup.
	if _, err := ResolveLiveUserState(tenantID, "admin"); err != nil {
		t.Fatalf("ResolveLiveUserState before suspension: %v", err)
	}

	if err := SuspendTenant(tenantID, "tester", "non-payment"); err != nil {
		t.Fatalf("SuspendTenant: %v", err)
	}
	if operational, err := TenantIsOperational(tenantID); err != nil || operational {
		t.Errorf("suspended tenant operational=%v err=%v, want false/nil", operational, err)
	}
	if _, err := ResolveLiveUserState(tenantID, "admin"); !IsUserNotActiveError(err) {
		t.Errorf("an already-cached session survived suspension: err = %v", err)
	}

	if err := SuspendTenant(tenantID, "tester", "again"); err == nil {
		t.Error("expected suspending an already-suspended tenant to be refused")
	}

	if err := ResumeTenant(tenantID, "tester", "payment received"); err != nil {
		t.Fatalf("ResumeTenant: %v", err)
	}
	if _, err := ResolveLiveUserState(tenantID, "admin"); err != nil {
		t.Errorf("ResolveLiveUserState after resume: %v", err)
	}

	names := eventNames(t, tenantID)
	if !hasEvent(names, TenantEventSuspended) || !hasEvent(names, TenantEventResumed) {
		t.Errorf("evidence trail = %v, want suspend and resume recorded", names)
	}

	t.Run("a transition without a reason is refused", func(t *testing.T) {
		if err := SuspendTenant(tenantID, "tester", ""); err == nil {
			t.Error("expected an unexplained suspension to be refused")
		}
	})
}

func TestStage4915PurgeRefusesUntilItIsSafeAndProvesRemoval(t *testing.T) {
	db.InitDB(testConnStr())

	tenantID, schemaName := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schemaName)
	if _, err := ProvisionTenantSchema(tenantID, schemaName, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}

	t.Run("a live tenant cannot be purged at all", func(t *testing.T) {
		if _, err := PurgeTenant(tenantID, "tester", "backup-1"); err == nil {
			t.Fatal("expected a purge of an active tenant to be refused")
		} else if !strings.Contains(err.Error(), "deprovision it first") {
			t.Errorf("refusal reason = %q, want it to name the missing deprovisioning step", err)
		}
	})

	t.Run("retention holds the purge off", func(t *testing.T) {
		until, err := RequestTenantDeprovision(tenantID, "tester", "contract ended", UseDefaultRetention)
		if err != nil {
			t.Fatalf("RequestTenantDeprovision: %v", err)
		}
		if time.Until(until) < 24*time.Hour {
			t.Errorf("retention_until is %s, want the 30-day policy default", until)
		}
		if _, err := PurgeTenant(tenantID, "tester", "backup-1"); err == nil {
			t.Fatal("expected the retention window to refuse the purge")
		} else if !strings.Contains(err.Error(), "retention window") {
			t.Errorf("refusal reason = %q, want it to name retention", err)
		}
		// Deprovisioning must stop traffic immediately, retention or not.
		if operational, _ := TenantIsOperational(tenantID); operational {
			t.Error("a deprovisioning tenant is still serving traffic")
		}
	})

	t.Run("a legal hold outranks an elapsed retention window", func(t *testing.T) {
		if _, err := db.DB.Exec(
			`UPDATE public.tenants SET retention_until = NOW() - INTERVAL '1 day' WHERE tenant_id = $1`, tenantID); err != nil {
			t.Fatalf("age the retention window: %v", err)
		}
		if err := SetTenantLegalHold(tenantID, true, "tester", "matter 2026-14"); err != nil {
			t.Fatalf("SetTenantLegalHold: %v", err)
		}
		if _, err := PurgeTenant(tenantID, "tester", "backup-1"); err == nil {
			t.Fatal("expected the legal hold to refuse the purge")
		} else if !strings.Contains(err.Error(), "legal hold") {
			t.Errorf("refusal reason = %q, want it to name the hold", err)
		}
		if err := SetTenantLegalHold(tenantID, false, "tester", "matter closed"); err != nil {
			t.Fatalf("clear hold: %v", err)
		}
	})

	t.Run("a purge with no backup reference is refused", func(t *testing.T) {
		if _, err := PurgeTenant(tenantID, "tester", "   "); err == nil {
			t.Fatal("expected a purge with no backup reference to be refused")
		}
	})

	t.Run("every refusal is itself recorded", func(t *testing.T) {
		if !hasEvent(eventNames(t, tenantID), TenantEventPurgeRefused) {
			t.Error("refused purges left no evidence")
		}
	})

	t.Run("once every precondition is met the tenant is removed and proved gone", func(t *testing.T) {
		// Warm the caches so the residue proof has something to find if the
		// purge fails to clear them.
		_, _ = ResolveLiveUserState(tenantID, "admin")
		_, _ = TenantIsOperational(tenantID)

		rep, err := PurgeTenant(tenantID, "tester", "pg_dump s3://erp-backups/2026-09-06.dump")
		if err != nil {
			t.Fatalf("PurgeTenant: %v", err)
		}
		if !rep.Clean {
			t.Errorf("residue report is not clean: %+v", rep)
		}
		if rep.RegistryRow || rep.SchemaExists || rep.HostSlug != "" || rep.CachedState || len(rep.PublicRows) != 0 {
			t.Errorf("residue remains: %+v", rep)
		}

		if _, err := GetTenantLifecycle(tenantID); !errors.Is(err, ErrTenantNotRegistered) {
			t.Errorf("after purge GetTenantLifecycle err = %v, want ErrTenantNotRegistered", err)
		}
		// A purged tenant must not merely fail - it must fail closed, as a
		// rejected session rather than a retryable server error.
		if _, err := ResolveLiveUserState(tenantID, "admin"); !IsUserNotActiveError(err) {
			t.Errorf("a session for a purged tenant failed with %v, want the not-active rejection", err)
		}

		// The evidence outlives the tenant: that is the only remaining record
		// that it ever existed.
		names := eventNames(t, tenantID)
		if !hasEvent(names, TenantEventPurged) || !hasEvent(names, TenantEventResidueVerified) {
			t.Errorf("evidence trail = %v, want the purge and its verification recorded", names)
		}
	})
}

func TestStage4915DatabasePrivilegePostureIsReported(t *testing.T) {
	db.InitDB(testConnStr())

	p, err := TenantDatabasePrivilegeReport("tenant_default")
	if err != nil {
		t.Fatalf("TenantDatabasePrivilegeReport: %v", err)
	}
	if p.ConnectedRole == "" {
		t.Error("expected the connected database role to be reported")
	}
	// This asserts the report is wired to reality, not that the dev database
	// is least-privilege - it is not, and saying so is this function's job.
	if p.IsSuperuser && len(p.Findings) == 0 {
		t.Error("connected as a superuser but no least-privilege finding was raised")
	}
}
