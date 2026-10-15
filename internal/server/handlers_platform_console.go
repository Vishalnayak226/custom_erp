package server

import (
	"encoding/json"
	"net/http"
	"strconv"

	"custom_erp/engines"
)

// Platform data console (Stage 60.6) - see engines/platform_console.go for who
// may use it, what it hides and what it refuses. The route tier is LevelAdmin;
// requirePlatformOperator narrows that to the server's operator allowlist, so
// a client's own Super Admin cannot reach another client's tables.

func requirePlatformOperator(w http.ResponseWriter, r *http.Request) bool {
	if engines.IsPlatformOperator(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-Username"), r.Header.Get("Resolved-Role")) {
		return true
	}
	writeAPIError(w, r, "PLATFORM-0317", "")
	return false
}

// platformTargetTenant resolves the {tenant} path value against the registry.
// GetTenantSchema falls back to the default schema for an unknown id, which
// here would mean editing the wrong client - so an id not in the registry is
// refused rather than resolved.
func platformTargetTenant(w http.ResponseWriter, r *http.Request) (string, bool) {
	target := r.PathValue("tenant")
	if target == "default" {
		return target, true
	}
	tenants, err := engines.PlatformTenants()
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return "", false
	}
	for _, t := range tenants {
		if t.TenantID == target {
			return target, true
		}
	}
	writeAPIErrorGeneric(w, r, http.StatusNotFound, "No client with that id")
	return "", false
}

// GET /api/v1/platform/me - lets the screen say "not an operator" plainly.
func handlePlatformMe(w http.ResponseWriter, r *http.Request) {
	ok := engines.IsPlatformOperator(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-Username"), r.Header.Get("Resolved-Role"))
	_ = json.NewEncoder(w).Encode(map[string]bool{"operator": ok})
}

// GET /api/v1/platform/tenants
func handlePlatformTenants(w http.ResponseWriter, r *http.Request) {
	if !requirePlatformOperator(w, r) {
		return
	}
	tenants, err := engines.PlatformTenants()
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	hasDefault := false
	for _, t := range tenants {
		hasDefault = hasDefault || t.TenantID == "default"
	}
	if !hasDefault {
		tenants = append([]engines.PlatformTenant{{TenantID: "default", Name: "Default (template) client"}}, tenants...)
	}
	_ = json.NewEncoder(w).Encode(tenants)
}

// GET /api/v1/platform/tenants/{tenant}/tables
func handlePlatformTables(w http.ResponseWriter, r *http.Request) {
	if !requirePlatformOperator(w, r) {
		return
	}
	target, ok := platformTargetTenant(w, r)
	if !ok {
		return
	}
	tables, err := engines.PlatformTables(target)
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(tables)
}

// GET /api/v1/platform/tenants/{tenant}/tables/{table}?doctype=&q=&limit=&offset=
func handlePlatformRows(w http.ResponseWriter, r *http.Request) {
	if !requirePlatformOperator(w, r) {
		return
	}
	target, ok := platformTargetTenant(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	page, err := engines.PlatformRows(target, r.PathValue("table"), engines.PlatformRowFilter{
		Doctype: q.Get("doctype"), Q: q.Get("q"), Limit: limit, Offset: offset,
	})
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(page)
}

// POST /api/v1/platform/tenants/{tenant}/tables/{table}/row
// Body: {"key": {"id": "..."}, "set": {"status": "Active", "data": "{...}"}}
func handlePlatformUpdateRow(w http.ResponseWriter, r *http.Request) {
	if !requirePlatformOperator(w, r) {
		return
	}
	target, ok := platformTargetTenant(w, r)
	if !ok {
		return
	}
	var req struct {
		Key map[string]string  `json:"key"`
		Set map[string]*string `json:"set"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid payload")
		return
	}
	result, err := engines.PlatformUpdateRow(target, r.PathValue("table"), req.Key, req.Set,
		r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-Username"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(result)
}

// allowTenantTarget lets a client's own Super Admin read their own client's
// record (target blank or their own id); any other client needs a platform
// operator. Used by the read-only per-client admin endpoints (Stage 60.6:
// "client can't change - or see - another client").
func allowTenantTarget(r *http.Request, target string) bool {
	if engines.IsPlatformOperator(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-Username"), r.Header.Get("Resolved-Role")) {
		return true
	}
	return engines.IsSuperAdmin(r.Header.Get("Resolved-Role")) && (target == "" || target == r.Header.Get("Resolved-Tenant-ID"))
}

// GET /api/v1/platform/tenants/{tenant}/records - every record type as a table.
func handlePlatformRecordTypes(w http.ResponseWriter, r *http.Request) {
	if !requirePlatformOperator(w, r) {
		return
	}
	target, ok := platformTargetTenant(w, r)
	if !ok {
		return
	}
	types, err := engines.PlatformRecordTypes(target)
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(types)
}

// GET /api/v1/platform/tenants/{tenant}/records/{doctype}?q=&limit=&offset=
func handlePlatformRecords(w http.ResponseWriter, r *http.Request) {
	if !requirePlatformOperator(w, r) {
		return
	}
	target, ok := platformTargetTenant(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	page, err := engines.PlatformRecords(target, r.PathValue("doctype"), engines.PlatformRowFilter{Q: q.Get("q"), Limit: limit, Offset: offset})
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(page)
}

// POST /api/v1/platform/tenants/{tenant}/records/{doctype}/{id}
// Body: {"set": {"vendor": "VEN-7", "status": "Approved", "grand_total": "1200"}}
func handlePlatformUpdateRecord(w http.ResponseWriter, r *http.Request) {
	if !requirePlatformOperator(w, r) {
		return
	}
	target, ok := platformTargetTenant(w, r)
	if !ok {
		return
	}
	var req struct {
		Set map[string]*string `json:"set"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid payload")
		return
	}
	result, err := engines.PlatformUpdateRecord(target, r.PathValue("doctype"), r.PathValue("id"), req.Set,
		r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-Username"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(result)
}
