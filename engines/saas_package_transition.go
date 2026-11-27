package engines

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"custom_erp/db"
)

// 50.12 / BLD-024 and BLD-025 (ask 193, ADR-009): the package transition
// contract, and what a downgrade, disable and re-enable do.
//
// The contract (BLD-023: always-on core; sold alone are Full ERP, PIM, WMS and
// OMS; HR, Procurement + RFQ, Manufacturing, CRM, Assets and Expenses are
// add-ons that need one of those four; any other mix is unsupported):
//
//   - A transition is ONE database transaction. Either every module row moves
//     to the target or none does, so a failure never leaves a half-applied
//     plan (ApplyPackageSelection, which loops, cannot promise that).
//   - It is serialised per client (advisory lock) and idempotent on a
//     request id: a retry returns what the first call did.
//   - Every attempt, applied or refused, is a row in
//     public.saas_package_transitions with the actor and the before/after.
//   - Disabling stops future work only: moduleGate and RequireModules refuse
//     the module, and workers that name it skip the client
//     (listTenantSchemas). Nothing is deleted, so re-enabling restores the
//     client's documents exactly as they were. Drafts and pending approvals
//     stay as they are, frozen; ImpactOfPackages lists them first.
//   - No privilege carries over: access is the module_entitlements row, which
//     the same transaction rewrites.

// packageBundleBases are the packages sold on their own.
var packageBundleBases = []string{"erp_full", "pim", "wms", "oms"}

// ValidatePackageBundle says whether a set of packages is a supported
// commercial combination (BLD-023). The reason is for the customer.
func ValidatePackageBundle(packageKeys []string) (bool, string) {
	if len(packageKeys) == 0 {
		return false, "choose at least one package: Full ERP, PIM, WMS or OMS"
	}
	hasBase := false
	for _, k := range packageKeys {
		if _, ok := ProductPackages[k]; !ok {
			return false, fmt.Sprintf("%q is not a package we sell", k)
		}
		for _, b := range packageBundleBases {
			if k == b {
				hasBase = true
			}
		}
	}
	if !hasBase {
		return false, "HR, Procurement, Manufacturing, CRM, Fixed Assets and Expenses are add-ons: they need Full ERP, PIM, WMS or OMS"
	}
	return true, ""
}

// OpenWork is unfinished work in a module a transition would switch off.
type OpenWork struct {
	Module  string `json:"module"`
	Doctype string `json:"doctype"`
	Count   int    `json:"count"`
}

// PackageImpact is the preview of a transition: nothing is changed.
type PackageImpact struct {
	TenantID  string     `json:"tenant_id"`
	Packages  []string   `json:"packages"`
	Supported bool       `json:"supported"`
	Reason    string     `json:"reason,omitempty"`
	Enable    []string   `json:"enable"`
	Disable   []string   `json:"disable"`
	Keep      []string   `json:"keep"`
	OpenWork  []OpenWork `json:"open_work"`
	Retention string     `json:"retention"`
}

const packageRetentionNote = "Switched-off modules keep every record; they cannot be used until the module is switched on again, and then everything is back as it was."

// targetModules is the optional modules a package selection implies, with the
// prerequisites of each pulled in (rfq needs procurement).
func targetModules(packageKeys []string) map[string]bool {
	out := map[string]bool{}
	for _, m := range ExpandPackagesToModules(packageKeys) {
		out[m] = true
		for _, p := range moduleDependencies[m] {
			out[p] = true
		}
	}
	return out
}

type saasRowQuerier interface {
	Query(string, ...interface{}) (*sql.Rows, error)
}

func enabledOptionalModules(q saasRowQuerier, schema string) (map[string]bool, []string, error) {
	rows, err := q.Query(fmt.Sprintf(`SELECT m.module_key, COALESCE(e.enabled, FALSE)
		FROM public.modules m LEFT JOIN %s.module_entitlements e ON e.module_key = m.module_key
		WHERE NOT m.is_core ORDER BY m.module_key`, schema))
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	on, all := map[string]bool{}, []string{}
	for rows.Next() {
		var k string
		var en bool
		if err := rows.Scan(&k, &en); err != nil {
			return nil, nil, err
		}
		all = append(all, k)
		if en {
			on[k] = true
		}
	}
	return on, all, rows.Err()
}

func saasSortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// saasTextArray renders a Postgres text[] literal without a driver helper.
func saasTextArray(items []string) string {
	q := make([]string, len(items))
	for i, s := range items {
		q[i] = `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
	}
	return "{" + strings.Join(q, ",") + "}"
}

// ImpactOfPackages previews a transition: modules gained and lost, and the
// drafts / pending approvals / open documents left frozen in lost modules.
func ImpactOfPackages(tenantID string, packageKeys []string) (*PackageImpact, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	imp := &PackageImpact{TenantID: tenantID, Packages: packageKeys, Retention: packageRetentionNote,
		Enable: []string{}, Disable: []string{}, Keep: []string{}, OpenWork: []OpenWork{}}
	imp.Supported, imp.Reason = ValidatePackageBundle(packageKeys)
	want := targetModules(packageKeys)
	now, all, err := enabledOptionalModules(db.DB, schema)
	if err != nil {
		return nil, err
	}
	var lose []string
	for _, m := range all {
		switch {
		case want[m] && !now[m]:
			imp.Enable = append(imp.Enable, m)
		case !want[m] && now[m]:
			imp.Disable = append(imp.Disable, m)
			lose = append(lose, m)
		case want[m] && now[m]:
			imp.Keep = append(imp.Keep, m)
		}
	}
	if len(lose) > 0 {
		rows, err := db.DB.Query(fmt.Sprintf(`SELECT m.module_key, d.doctype, count(*)
			FROM %[1]s.documents d JOIN %[1]s.doctype_meta m ON m.name = d.doctype
			WHERE m.module_key = ANY($1::text[]) AND d.deleted_at IS NULL AND d.status IN ('Draft','Pending Approval','Open')
			GROUP BY 1, 2 ORDER BY 1, 2`, schema), saasTextArray(lose))
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var w OpenWork
			if err := rows.Scan(&w.Module, &w.Doctype, &w.Count); err != nil {
				return nil, err
			}
			imp.OpenWork = append(imp.OpenWork, w)
		}
	}
	return imp, nil
}

// PackageTransition is what one transition did.
type PackageTransition struct {
	RequestID string   `json:"request_id"`
	Status    string   `json:"status"` // applied | refused
	Detail    string   `json:"detail,omitempty"`
	Before    []string `json:"before"`
	After     []string `json:"after"`
	Replayed  bool     `json:"replayed,omitempty"`
}

func recordRefusedTransition(tenantID, requestID, actor string, packageKeys []string, why string) {
	pk, _ := json.Marshal(packageKeys)
	_, _ = db.DB.Exec(`INSERT INTO public.saas_package_transitions (tenant_id, request_id, actor, packages, status, detail)
		VALUES ($1, $2, $3, $4, 'refused', $5) ON CONFLICT (tenant_id, request_id) DO NOTHING`, tenantID, requestID, actor, string(pk), why)
}

// TransitionTenantPackages moves a client to a package selection in one
// transaction. enforceBundle applies the BLD-023 combination rule (the
// subscription path always does; an operator's override on the admin screen
// passes false and is audited all the same).
func TransitionTenantPackages(tenantID string, packageKeys []string, actor, requestID string, enforceBundle bool) (*PackageTransition, error) {
	if requestID == "" {
		requestID = fmt.Sprintf("auto-%d", time.Now().UnixNano())
	}
	fail := func(msg string) (*PackageTransition, error) {
		recordRefusedTransition(tenantID, requestID, actor, packageKeys, msg)
		return nil, &ValidationError{Code: "SAAS-0191", Message: msg}
	}
	for _, k := range packageKeys {
		if _, ok := ProductPackages[k]; !ok {
			return fail(fmt.Sprintf("unknown package: %s", k))
		}
	}
	if enforceBundle {
		if ok, why := ValidatePackageBundle(packageKeys); !ok {
			return fail(why)
		}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext($1))`, "pkgtransition:"+tenantID); err != nil {
		return nil, err
	}
	var prevStatus, prevDetail, prevBefore, prevAfter string
	err = tx.QueryRow(`SELECT status, detail, from_modules::text, to_modules::text FROM public.saas_package_transitions
		WHERE tenant_id = $1 AND request_id = $2`, tenantID, requestID).Scan(&prevStatus, &prevDetail, &prevBefore, &prevAfter)
	if err == nil {
		out := &PackageTransition{RequestID: requestID, Status: prevStatus, Detail: prevDetail, Replayed: true}
		_ = json.Unmarshal([]byte(prevBefore), &out.Before)
		_ = json.Unmarshal([]byte(prevAfter), &out.After)
		return out, nil
	}
	if err != sql.ErrNoRows {
		return nil, err
	}
	now, all, err := enabledOptionalModules(tx, schema)
	if err != nil {
		return nil, err
	}
	want := targetModules(packageKeys)
	for _, m := range all {
		if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.module_entitlements (module_key, enabled, granted_by, granted_at)
			VALUES ($1, $2, $3, CURRENT_TIMESTAMP)
			ON CONFLICT (module_key) DO UPDATE SET enabled = EXCLUDED.enabled, granted_by = EXCLUDED.granted_by, granted_at = EXCLUDED.granted_at`, schema),
			m, want[m], actor); err != nil {
			return nil, &ValidationError{Code: "SAAS-0191", Message: fmt.Sprintf("Could not switch module %q; nothing was changed, so the same request can be retried", m)}
		}
	}
	before, after := saasSortedKeys(now), saasSortedKeys(want)
	b, _ := json.Marshal(before)
	a, _ := json.Marshal(after)
	pk, _ := json.Marshal(packageKeys)
	if _, err := tx.Exec(`INSERT INTO public.saas_package_transitions (tenant_id, request_id, actor, from_modules, to_modules, packages, status, detail)
		VALUES ($1, $2, $3, $4, $5, $6, 'applied', '')`, tenantID, requestID, actor, string(b), string(a), string(pk)); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	forgetSweepView() // 283.2: background workers see the new modules on their next tick
	LogAuditEvent(tenantID, actor, "saas.package_transition", "Applied", fmt.Sprintf("%v -> %v", before, after))
	return &PackageTransition{RequestID: requestID, Status: "applied", Before: before, After: after}, nil
}
