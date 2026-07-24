package engines

import (
	"custom_erp/db"
	"database/sql"
	"fmt"
	"sort"
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

// ProductPackage is a sellable product SKU - a named shorthand for "enable
// this set of module_keys" plus the URL prefix that product is reachable
// at (Stage 27: modular product packaging, so PIM/WMS/OMS/HR/etc. can each
// be licensed to a client independently of the others). A package has no
// per-tenant state of its own - the per-tenant state is entirely the
// existing module_entitlements table below; a package is only ever used to
// bulk-set entitlements at provisioning/reconfiguration time, never read at
// request time, so it can never drift out of sync with what's actually
// enforced.
type ProductPackage struct {
	PackageKey  string
	DisplayName string
	URLPrefix   string   // e.g. "/wms" - leading slash, no trailing slash
	Modules     []string // module_keys this package grants, beyond the always-on is_core set
}

// ProductPackages is the master definition of every sellable product and
// where it lives. Adding a new product is a one-entry change here (plus,
// if it needs a genuinely new capability, a module_key migration like
// db/migrations_stage27_product_packaging.sql) - nothing else needs to
// change to give it a working URL (internal/server/routes.go's SPA
// fallback loop) or make it provisionable (handleProvisionTenant's
// `packages` field). "reports" is deliberately not a package of its own -
// it's useful alongside every product, not distinctive to any one of them,
// so ExpandPackagesToModules always includes it for any non-empty
// selection rather than making a client name it explicitly. "stickers"
// (barcode/label printing) is listed explicitly under the two packages
// it's actually relevant to (warehouse put-away/PIM catalog labeling)
// rather than being force-included everywhere.
var ProductPackages = map[string]ProductPackage{
	"pim":           {"pim", "Product Information Management", "/pims", []string{"pim", "stickers"}},
	"wms":           {"wms", "Warehouse Management", "/wms", []string{"wms", "stickers"}},
	"oms":           {"oms", "Order Management", "/oms", []string{"oms"}},
	"hr":            {"hr", "HR & People", "/hr", []string{"hr"}},
	"procurement":   {"procurement", "Procurement & Vendors", "/procurement", []string{"procurement", "rfq"}},
	"manufacturing": {"manufacturing", "Manufacturing", "/manufacturing", []string{"manufacturing"}},
	"crm":           {"crm", "CRM & Loyalty", "/crm", []string{"crm_loyalty"}},
	"assets":        {"assets", "Fixed Assets", "/assets", []string{"assets"}},
	"expenses":      {"expenses", "Expense Management", "/expenses", []string{"expenses"}},
	"erp_full": {"erp_full", "Full ERP Suite", "/erp", []string{
		"pim", "wms", "oms", "hr", "procurement", "rfq", "manufacturing",
		"crm_loyalty", "assets", "expenses", "stickers", "reports",
	}},
}

// ExpandPackagesToModules unions the module_keys granted by a set of
// package keys (unknown package keys are silently skipped - same
// fail-safe convention as IsModuleEnabled's missing-row case), always
// including "reports" when the selection is non-empty. The always-on
// is_core modules are never included here since they don't need to be -
// SetModuleEntitlement already refuses to disable them regardless of what
// a caller passes.
func ExpandPackagesToModules(packageKeys []string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(m string) {
		if !seen[m] {
			seen[m] = true
			out = append(out, m)
		}
	}
	for _, pk := range packageKeys {
		pkg, ok := ProductPackages[pk]
		if !ok {
			continue
		}
		for _, m := range pkg.Modules {
			add(m)
		}
	}
	if len(out) > 0 {
		add("reports")
	}
	return out
}

// packageModuleSet returns the full set of module_keys a package implies a
// tenant has enabled, including the "reports" module ExpandPackagesToModules
// always adds alongside any real product selection - kept as the single
// definition of "what does owning exactly this package look like" so
// ResolveSoleProductPackage below compares against the same set
// provisioning actually produces.
func packageModuleSet(pkg ProductPackage) map[string]bool {
	set := map[string]bool{}
	for _, m := range pkg.Modules {
		set[m] = true
	}
	set["reports"] = true
	return set
}

// ResolveSoleProductPackage returns the one non-full ProductPackage whose
// module set exactly matches a tenant's enabled optional (non-core)
// modules, or nil if zero or more than one package would fit (e.g. the
// full suite, or a custom combination that doesn't correspond to exactly
// one named product). Used only so the frontend can land a genuinely
// single-product tenant straight on their own URL (e.g. bare "/" ->
// "/wms") - a pure navigation convenience, never an access-control
// decision (that's still moduleGate on every request, regardless of what
// this resolves to).
func ResolveSoleProductPackage(enabledModules []string) *ProductPackage {
	enabledSet := map[string]bool{}
	for _, m := range enabledModules {
		enabledSet[m] = true
	}

	var match *ProductPackage
	for key, pkg := range ProductPackages {
		if key == "erp_full" {
			continue
		}
		want := packageModuleSet(pkg)
		if len(want) != len(enabledSet) {
			continue
		}
		equal := true
		for m := range want {
			if !enabledSet[m] {
				equal = false
				break
			}
		}
		if !equal {
			continue
		}
		if match != nil {
			return nil // ambiguous - more than one package fits
		}
		p := pkg
		match = &p
	}
	return match
}

// ResolveOwnedPackages returns every non-full-suite package whose entire
// module set (via packageModuleSet, so "reports" counts as always owned
// alongside any real product) is a subset of a tenant's enabled optional
// modules - i.e. every product this tenant could navigate to, not just the
// one ResolveSoleProductPackage would auto-redirect to. Used only to drive
// the frontend's product switcher for a tenant that owns 2+ products but
// not the full suite; sorted by PackageKey for a stable response.
func ResolveOwnedPackages(enabledModules []string) []ProductPackage {
	enabledSet := map[string]bool{}
	for _, m := range enabledModules {
		enabledSet[m] = true
	}

	var out []ProductPackage
	for key, pkg := range ProductPackages {
		if key == "erp_full" {
			continue
		}
		owned := true
		for m := range packageModuleSet(pkg) {
			if !enabledSet[m] {
				owned = false
				break
			}
		}
		if owned {
			out = append(out, pkg)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PackageKey < out[j].PackageKey })
	return out
}

// IsFullSuite reports whether a tenant's enabled optional modules cover
// every module the "erp_full" package grants - i.e. this tenant has
// (at least) the complete product line, not a curated subset. Used so the
// frontend's product switcher stays hidden for a full-suite tenant even
// though such a tenant technically satisfies every individual package's
// requirements too (ResolveOwnedPackages would otherwise list all of
// them) - a full-suite tenant should see exactly today's one unified
// sidebar, never a "switch product" affordance.
func IsFullSuite(enabledModules []string) bool {
	enabledSet := map[string]bool{}
	for _, m := range enabledModules {
		enabledSet[m] = true
	}
	for m := range packageModuleSet(ProductPackages["erp_full"]) {
		if !enabledSet[m] {
			return false
		}
	}
	return true
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

// moduleDependencies lists, for a module_key, which OTHER OPTIONAL (non-
// is_core) module_keys it requires to function. is_core modules
// (core/master_data/inventory/sales/finance) are permanently enabled for
// every tenant already, so they never need an entry here - this map only
// exists for dependencies between two modules that can each independently
// be turned off. Extend this when a future module genuinely depends on
// another optional one; do not pre-populate speculative entries.
var moduleDependencies = map[string][]string{
	"rfq": {"procurement"}, // RFQ/vendor-quote comparison is meaningless without Procurement
}

// dependentsOf returns which OTHER modules in moduleDependencies list
// moduleKey as a prerequisite - used to refuse disabling a module that
// something else currently enabled still needs.
func dependentsOf(moduleKey string) []string {
	var out []string
	for dependent, prereqs := range moduleDependencies {
		for _, p := range prereqs {
			if p == moduleKey {
				out = append(out, dependent)
				break
			}
		}
	}
	return out
}

// SetModuleEntitlement enables or disables a module for a tenant. Core
// modules (public.modules.is_core) can never be disabled - the check is
// server-side here, not left to the caller/UI, since this is the only
// function that can actually flip the entitlement. Enabling a module also
// transitively enables any unmet prerequisites from moduleDependencies
// (atomically, in one transaction, so a partial failure never leaves a
// module on without something it needs); disabling a module is refused if
// another currently-enabled module still depends on it.
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

		for _, dependent := range dependentsOf(moduleKey) {
			enabledNow, err := IsModuleEnabled(tenantID, dependent)
			if err != nil {
				return err
			}
			if enabledNow {
				return fmt.Errorf("cannot disable module '%s': module '%s' depends on it and is still enabled", moduleKey, dependent)
			}
		}
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	upsert := fmt.Sprintf(`
		INSERT INTO %s.module_entitlements (module_key, enabled, granted_by, granted_at)
		VALUES ($1, $2, $3, CURRENT_TIMESTAMP)
		ON CONFLICT (module_key) DO UPDATE SET enabled = EXCLUDED.enabled, granted_by = EXCLUDED.granted_by, granted_at = EXCLUDED.granted_at`, schema)

	if enabled {
		for _, prereq := range moduleDependencies[moduleKey] {
			if _, err := tx.Exec(upsert, prereq, true, grantedBy); err != nil {
				return err
			}
		}
	}

	if _, err := tx.Exec(upsert, moduleKey, enabled, grantedBy); err != nil {
		return err
	}

	return tx.Commit()
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
// gating the generic doc CRUD route (internal/server/handlers_core_doc_engine.go's handleGenericDoc) where the
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
