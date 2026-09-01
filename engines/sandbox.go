package engines

import (
	"crypto/rand"
	"custom_erp/db"
	"encoding/hex"
	"fmt"
	"time"
)

// Stage 38.7: self-service sandbox tenant for integrators. A sandbox is a
// normal tenant - provisioned through the same ProvisionTenantSchema every
// real tenant uses (engines/saas.go), so it gets the identical fresh
// admin/module/RBAC baseline - flagged is_sandbox so external side effects
// can be turned off for it (checked by Stage 38.4's webhook delivery job
// handler) and given an expiry so an unattended sandbox does not accumulate
// forever.
//
// Deliberately does NOT seed a synthetic demo dataset (items/customers/
// orders): the integrator's whole point is testing their own integration
// against the Public API v1 (38.1) by creating their own records through it,
// same as any other fresh tenant. A canned demo catalog is a real, separate,
// materially larger feature (representative data across every module,
// believable relationships between it) - not attempted here.
//
// "Self-service" describes the integrator's experience once a sandbox
// exists: their own login, their own API credentials (Stage 38.2), full use
// of the app/API with no path back to production data. Provisioning itself
// stays Super-Admin-gated, the same as every other tenant-provisioning
// action in this codebase (handleProvisionTenant) - a public, unauthenticated
// self-signup flow is a distinct, larger feature this stage does not build.

const sandboxDefaultTTLDays = 14

// generateSandboxSuffix returns an 8-hex-character random suffix - short
// enough to stay a readable tenant_id, long enough that two concurrent
// requests never collide.
func generateSandboxSuffix() (string, error) {
	raw := make([]byte, 4)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

// ProvisionSandboxTenant creates a new sandbox tenant and returns its id,
// schema name, and the one-time admin password (ProvisionTenantSchema's own
// return value - never persisted in plaintext, same guarantee as any other
// tenant). ttlDays <= 0 falls back to sandboxDefaultTTLDays. appVersion is
// passed straight through to ProvisionTenantSchema (its own doc comment: the
// running binary's own currentAppVersion(), or "" from tests/tooling) - this
// package does not read that itself, the same way ProvisionTenantSchema's
// existing signature already keeps that concern with its caller.
func ProvisionSandboxTenant(appVersion string, ttlDays int) (tenantID, schemaName, adminPassword string, err error) {
	if ttlDays <= 0 {
		ttlDays = sandboxDefaultTTLDays
	}
	suffix, err := generateSandboxSuffix()
	if err != nil {
		return "", "", "", err
	}
	tenantID = "sandbox-" + suffix
	schemaName = "tenant_sandbox_" + suffix

	adminPassword, err = ProvisionTenantSchema(tenantID, schemaName, appVersion)
	if err != nil {
		return "", "", "", err
	}

	if _, err := db.DB.Exec(`
		UPDATE public.tenants SET is_sandbox = TRUE, sandbox_expires_at = CURRENT_TIMESTAMP + ($1 || ' days')::interval
		WHERE tenant_id = $2`, ttlDays, tenantID); err != nil {
		return "", "", "", fmt.Errorf("provisioned but failed to flag %s as a sandbox: %v", tenantID, err)
	}
	return tenantID, schemaName, adminPassword, nil
}

// IsSandboxTenant reports whether tenantID is a sandbox, for callers that
// already have a tenantID (API handlers).
func IsSandboxTenant(tenantID string) (bool, error) {
	var isSandbox bool
	err := db.DB.QueryRow(`SELECT is_sandbox FROM public.tenants WHERE tenant_id = $1`, tenantID).Scan(&isSandbox)
	if err != nil {
		return false, nil // unknown tenant_id (e.g. "default", never registered) - not a sandbox
	}
	return isSandbox, nil
}

// IsSandboxSchema is IsSandboxTenant's schema-scoped twin, for the
// job-runner worker loop (engines/jobrunner.go), which iterates schemas the
// same way every other background worker in this codebase does.
func IsSandboxSchema(schema string) (bool, error) {
	var isSandbox bool
	err := db.DB.QueryRow(`SELECT is_sandbox FROM public.tenants WHERE schema_name = $1`, schema).Scan(&isSandbox)
	if err != nil {
		return false, nil
	}
	return isSandbox, nil
}

// IsSandboxExpired reports whether tenantID is a sandbox whose expiry has
// passed - checked at login (handleLogin) and on the Stage 29.8 live
// session re-check (apiMiddleware), so an expired sandbox locks out both new
// logins and any session already in progress, not just the next login.
func IsSandboxExpired(tenantID string) (bool, error) {
	var isSandbox bool
	var expiresAt *time.Time
	err := db.DB.QueryRow(`SELECT is_sandbox, sandbox_expires_at FROM public.tenants WHERE tenant_id = $1`, tenantID).Scan(&isSandbox, &expiresAt)
	if err != nil {
		return false, nil
	}
	if !isSandbox || expiresAt == nil {
		return false, nil
	}
	return time.Now().After(*expiresAt), nil
}

// sandboxTransactionalTables are the per-tenant tables ProvisionTenantSchema's
// own comment already documents as "correctly empty for a new tenant" - the
// exact set a reset wipes back to that same fresh state. Config/master data
// (doctype_meta, gl_accounts, role_permissions, ...), users, and settings are
// deliberately left untouched: a reset clears business data, not the
// integrator's own login or configuration.
var sandboxTransactionalTables = []string{
	"documents",
	"gl_postings",
	"inventory_availability",
	"inventory_reservation",
	"integration_event_outbox",
	"integration_event_log",
	"async_jobs",
	"api_request_log",
	"api_idempotency_keys",
	"bin_stock",
	"bin_stock_lpn",
	"bin_stock_batch",
	"bin_stock_owner",
	"approval_log",
	"payment_utr_log",
	"pim_publish_log",
	"pim_publish_queue",
	"pos_offline_heartbeats",
	"product_content_versions",
	"sticker_print_log",
	"clevertap_event_log",
	"loyalty_point_ledger",
}

// ResetSandboxTenant truncates a sandbox's business data back to the fresh
// state ProvisionTenantSchema originally created and pushes its expiry back
// out by another full TTL window. Refuses outright on any tenant that is not
// flagged is_sandbox - the one safety check standing between this and
// wiping a real tenant's data.
func ResetSandboxTenant(tenantID string, ttlDays int) error {
	isSandbox, err := IsSandboxTenant(tenantID)
	if err != nil {
		return err
	}
	if !isSandbox {
		return fmt.Errorf("%s is not a sandbox tenant - refusing to reset it", tenantID)
	}
	if ttlDays <= 0 {
		ttlDays = sandboxDefaultTTLDays
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, table := range sandboxTransactionalTables {
		var exists bool
		if err := tx.QueryRow(`SELECT to_regclass($1) IS NOT NULL`, schema+"."+table).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			continue
		}
		if _, err := tx.Exec(fmt.Sprintf("TRUNCATE TABLE %s.%s CASCADE", schema, table)); err != nil {
			return fmt.Errorf("failed to truncate %s: %v", table, err)
		}
	}
	if _, err := tx.Exec(`UPDATE public.tenants SET sandbox_expires_at = CURRENT_TIMESTAMP + ($1 || ' days')::interval WHERE tenant_id = $2`, ttlDays, tenantID); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteSandboxTenant permanently drops a sandbox's schema and its registry
// row. Refuses on any tenant not flagged is_sandbox, same guard as
// ResetSandboxTenant - the only thing standing between this and dropping a
// real tenant's entire schema.
func DeleteSandboxTenant(tenantID string) error {
	isSandbox, err := IsSandboxTenant(tenantID)
	if err != nil {
		return err
	}
	if !isSandbox {
		return fmt.Errorf("%s is not a sandbox tenant - refusing to delete it", tenantID)
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	if _, err := db.DB.Exec(fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", schema)); err != nil {
		return err
	}
	_, err = db.DB.Exec(`DELETE FROM public.tenants WHERE tenant_id = $1`, tenantID)
	return err
}

// ListSandboxTenants returns every sandbox tenant for the admin visibility
// list, newest first.
func ListSandboxTenants() ([]map[string]interface{}, error) {
	rows, err := db.DB.Query(`
		SELECT tenant_id, schema_name, sandbox_expires_at, created_at
		FROM public.tenants WHERE is_sandbox = TRUE ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]interface{}{}
	for rows.Next() {
		var tenantID, schemaName string
		var expiresAt, createdAt *time.Time
		if err := rows.Scan(&tenantID, &schemaName, &expiresAt, &createdAt); err != nil {
			return nil, err
		}
		expired := expiresAt != nil && time.Now().After(*expiresAt)
		out = append(out, map[string]interface{}{
			"tenant_id": tenantID, "schema_name": schemaName, "sandbox_expires_at": expiresAt,
			"created_at": createdAt, "expired": expired,
		})
	}
	return out, rows.Err()
}
