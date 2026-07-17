package engines

import (
	"custom_erp/db"
	"database/sql"
	"fmt"
)

// Module describes one row of the global public.modules catalog.
type Module struct {
	ModuleKey      string `json:"module_key"`
	DisplayName    string `json:"display_name"`
	Description    string `json:"description"`
	IsCore         bool   `json:"is_core"`
	DefaultEnabled bool   `json:"default_enabled"`
}

// ModuleEntitlement is a catalog module joined with one tenant's current
// enabled/disabled state for it.
type ModuleEntitlement struct {
	ModuleKey   string `json:"module_key"`
	DisplayName string `json:"display_name"`
	IsCore      bool   `json:"is_core"`
	Enabled     bool   `json:"enabled"`
}

// IsModuleEnabled checks whether a functional module is enabled for the
// tenant. Fails closed - same shape as IsFeatureEnabled: any DB error or a
// module never registered for this tenant resolves to false, not true.
func IsModuleEnabled(tenantID string, moduleKey string) (bool, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return false, err
	}

	query := fmt.Sprintf("SELECT enabled FROM %s.module_entitlements WHERE module_key = $1", schema)
	var enabled bool
	err = db.DB.QueryRow(query, moduleKey).Scan(&enabled)
	if err != nil {
		return false, nil
	}
	return enabled, nil
}

// SetModuleEntitlement enables or disables a module for a tenant. Core
// modules (public.modules.is_core) can never be disabled - the check is
// server-side here, not left to the caller/UI, since this is the only
// function that can actually flip the entitlement.
func SetModuleEntitlement(tenantID string, moduleKey string, enabled bool, grantedBy string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}

	if !enabled {
		var isCore bool
		err = db.DB.QueryRow("SELECT is_core FROM public.modules WHERE module_key = $1", moduleKey).Scan(&isCore)
		if err == sql.ErrNoRows {
			return fmt.Errorf("unknown module_key: %s", moduleKey)
		}
		if err != nil {
			return err
		}
		if isCore {
			return fmt.Errorf("module '%s' is a core module and cannot be disabled", moduleKey)
		}
	}

	query := fmt.Sprintf(`
		INSERT INTO %s.module_entitlements (module_key, enabled, granted_by, granted_at)
		VALUES ($1, $2, $3, CURRENT_TIMESTAMP)
		ON CONFLICT (module_key) DO UPDATE SET enabled = EXCLUDED.enabled, granted_by = EXCLUDED.granted_by, granted_at = EXCLUDED.granted_at`, schema)
	_, err = db.DB.Exec(query, moduleKey, enabled, grantedBy)
	return err
}

// ListModuleEntitlements returns the full module catalog joined with this
// tenant's current entitlement state (a module with no row yet - e.g. a
// tenant provisioned before this module was added to the catalog - falls
// back to the catalog's default_enabled, matching how a never-set feature
// flag already behaves elsewhere in this codebase).
func ListModuleEntitlements(tenantID string) ([]ModuleEntitlement, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}

	query := fmt.Sprintf(`
		SELECT m.module_key, m.display_name, m.is_core, COALESCE(e.enabled, m.default_enabled) AS enabled
		FROM public.modules m
		LEFT JOIN %s.module_entitlements e ON e.module_key = m.module_key
		ORDER BY m.module_key`, schema)
	rows, err := db.DB.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ModuleEntitlement
	for rows.Next() {
		var me ModuleEntitlement
		if err := rows.Scan(&me.ModuleKey, &me.DisplayName, &me.IsCore, &me.Enabled); err != nil {
			return nil, err
		}
		out = append(out, me)
	}
	return out, rows.Err()
}

// ListModules returns the global module catalog (tenant-independent).
func ListModules() ([]Module, error) {
	rows, err := db.DB.Query("SELECT module_key, display_name, COALESCE(description, ''), is_core, default_enabled FROM public.modules ORDER BY module_key")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Module
	for rows.Next() {
		var m Module
		if err := rows.Scan(&m.ModuleKey, &m.DisplayName, &m.Description, &m.IsCore, &m.DefaultEnabled); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ModuleForDoctype resolves the module_key a given doctype belongs to, for
// gating the generic doc CRUD route (main.go's handleGenericDoc) where the
// doctype is a runtime path parameter and can't be gated at route-
// registration time the way the fixed module routes are. Returns "" (no
// error) for a doctype with no module_key assigned - such doctypes are
// treated as ungated/core, matching the additive nature of this migration.
func ModuleForDoctype(tenantID string, doctype string) (string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}

	query := fmt.Sprintf("SELECT COALESCE(module_key, '') FROM %s.doctype_meta WHERE name = $1", schema)
	var moduleKey string
	err = db.DB.QueryRow(query, doctype).Scan(&moduleKey)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return moduleKey, nil
}
