package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"custom_erp/engines"
)

// Stage 100: custom dashboards' HTTP surface (engines/dashboard_datasets.go,
// dashboard_custom.go). Every read is cut by the viewer's stores and money
// rule inside the engine; here a dataset is offered and run only when the
// role may read its record type, and building needs Create on Dashboard
// Layout ("Build dashboards", set per role in Roles).

func datasetReadable(r *http.Request) func(string) bool {
	tenantID, role := r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-Role")
	seen := map[string]bool{}
	return func(doctype string) bool {
		if v, ok := seen[doctype]; ok {
			return v
		}
		ok, err := checkPermission(tenantID, role, doctype, "read")
		seen[doctype] = err == nil && ok
		return seen[doctype]
	}
}

// handleDashboardDatasets lists the datasets the caller may chart.
func handleDashboardDatasets(w http.ResponseWriter, r *http.Request) {
	build, _ := checkPermission(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-Role"), "DashboardLayout", "create")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"datasets":    engines.ListDashboardDatasets(r.Header.Get("Resolved-Role"), datasetReadable(r)),
		"can_build":   build,
		"sees_money":  engines.RoleSeesMoney(r.Header.Get("Resolved-Role")),
		"refresh_min": engines.GetSettingInt(r.Header.Get("Resolved-Tenant-ID"), "reports.dashboard_refresh_minutes"),
	})
}

// handleDashboardQuery runs one query ({query}) or a dashboard's worth at
// once ({queries}, up to 40 - one request per dashboard load). Each answer
// is {result} or {error}, so one widget's problem does not blank the rest.
func handleDashboardQuery(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Query   *engines.DatasetQuery  `json:"query"`
		Queries []engines.DatasetQuery `json:"queries"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request payload")
		return
	}
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	viewer := engines.DatasetViewer{Role: r.Header.Get("Resolved-Role"), UserID: r.Header.Get("Resolved-User-ID")}
	readable := datasetReadable(r)
	run := func(q engines.DatasetQuery) (engines.DatasetResult, error) {
		ds, ok := engines.DashboardDatasetByKey(q.Dataset)
		if !ok {
			return engines.DatasetResult{}, &engines.ValidationError{Code: "REPORT-0988", Message: fmt.Sprintf("%q is not a dataset", q.Dataset)}
		}
		if !readable(ds.Doctype) {
			return engines.DatasetResult{}, &engines.ValidationError{Code: "REPORT-0988", Message: "your role may not read " + ds.Label}
		}
		return engines.RunDatasetQuery(tenantID, viewer, q)
	}
	if req.Query != nil {
		res, err := run(*req.Query)
		if err != nil {
			writeEngineError(w, r, err, http.StatusUnprocessableEntity)
			return
		}
		_ = json.NewEncoder(w).Encode(res)
		return
	}
	if len(req.Queries) > 40 {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "At most 40 queries at once")
		return
	}
	out := make([]map[string]interface{}, len(req.Queries))
	for i, q := range req.Queries {
		if res, err := run(q); err != nil {
			out[i] = map[string]interface{}{"error": err.Error()}
		} else {
			out[i] = map[string]interface{}{"result": res}
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"results": out})
}

// handleCustomDashboards lists (GET) the dashboards the caller may open, or
// saves (POST) one - building needs Create on Dashboard Layout.
func handleCustomDashboards(w http.ResponseWriter, r *http.Request) {
	tenantID, userID, username, role := r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-User-ID"), r.Header.Get("Resolved-Username"), r.Header.Get("Resolved-Role")
	if r.Method == http.MethodGet {
		list, err := engines.ListCustomDashboards(tenantID, userID, username, role)
		if err != nil {
			writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to list dashboards")
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"dashboards": list})
		return
	}
	if ok, _ := checkPermission(tenantID, role, "DashboardLayout", "create"); !ok {
		writeAPIError(w, r, "GLOBAL-0011", "")
		return
	}
	var cd engines.CustomDashboard
	if err := json.NewDecoder(r.Body).Decode(&cd); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request payload")
		return
	}
	id, err := engines.SaveCustomDashboard(tenantID, userID, username, role, cd)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"id": id, "status": "saved"})
}

// handleCustomDashboard is one dashboard: GET it, DELETE it (the owner's).
func handleCustomDashboard(w http.ResponseWriter, r *http.Request) {
	tenantID, userID, username, role := r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-User-ID"), r.Header.Get("Resolved-Username"), r.Header.Get("Resolved-Role")
	id := r.PathValue("id")
	if r.Method == http.MethodDelete {
		if err := engines.DeleteCustomDashboard(tenantID, userID, role, id); err != nil {
			writeEngineError(w, r, err, http.StatusUnprocessableEntity)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
		return
	}
	cd, err := engines.GetCustomDashboardFor(tenantID, userID, username, role, id)
	if err != nil {
		writeEngineError(w, r, err, http.StatusNotFound)
		return
	}
	_ = json.NewEncoder(w).Encode(cd)
}

// handleCopyCustomDashboard copies a dashboard (a ready-made or a shared
// one) into one the caller owns and may change.
func handleCopyCustomDashboard(w http.ResponseWriter, r *http.Request) {
	tenantID, userID, username, role := r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-User-ID"), r.Header.Get("Resolved-Username"), r.Header.Get("Resolved-Role")
	if ok, _ := checkPermission(tenantID, role, "DashboardLayout", "create"); !ok {
		writeAPIError(w, r, "GLOBAL-0011", "")
		return
	}
	id, err := engines.CopyCustomDashboard(tenantID, userID, username, role, r.PathValue("id"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"id": id, "status": "copied"})
}

// handleHomeDashboard is the dashboard set as the caller's role's home, or
// {"dashboard": null}.
func handleHomeDashboard(w http.ResponseWriter, r *http.Request) {
	cd, err := engines.HomeDashboardFor(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-User-ID"), r.Header.Get("Resolved-Username"), r.Header.Get("Resolved-Role"))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to read the home dashboard")
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"dashboard": cd})
}

// handleDashboardExport is one widget's rows as an Excel file (100.3.4).
func handleDashboardExport(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title string               `json:"title"`
		Query engines.DatasetQuery `json:"query"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request payload")
		return
	}
	ds, ok := engines.DashboardDatasetByKey(req.Query.Dataset)
	if !ok || !datasetReadable(r)(ds.Doctype) {
		writeAPIError(w, r, "REPORT-0988", "")
		return
	}
	req.Query.Limit = 1000
	res, err := engines.RunDatasetQuery(r.Header.Get("Resolved-Tenant-ID"),
		engines.DatasetViewer{Role: r.Header.Get("Resolved-Role"), UserID: r.Header.Get("Resolved-User-ID")}, req.Query)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	book, err := engines.DatasetResultXLSX(req.Title, res)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to build the file")
		return
	}
	name := strings.Map(func(c rune) rune {
		if strings.ContainsRune(`/\:*?"<>|`, c) {
			return '-'
		}
		return c
	}, strings.TrimSpace(req.Title))
	if name == "" {
		name = "dashboard"
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s %s.xlsx"`, name, engines.BusinessToday()))
	_, _ = w.Write(book)
}

// handleScheduleDashboardDigest emails a dashboard on a schedule, run as
// the caller (their stores, their role's money rule).
func handleScheduleDashboardDigest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Frequency string `json:"frequency"`
		Email     string `json:"email"`
		FirstRun  string `json:"first_run"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request payload")
		return
	}
	id, err := engines.ScheduleCustomDashboardDigest(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-User-ID"), r.Header.Get("Resolved-Username"),
		r.Header.Get("Resolved-Role"), r.PathValue("id"), req.Frequency, req.Email, req.FirstRun)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"id": id, "status": "scheduled"})
}
