package server

import (
	"encoding/json"
	"net/http"

	"custom_erp/engines"
)

// 34.4: the Myntra market-price harvester. Reading needs the CompetitorPrice
// read right; starting a run and re-parsing are admin-only, like the setting
// that switches the feature on.

func handlePIMMarketHarvest(w http.ResponseWriter, r *http.Request) {
	tenantID, _, ok := pimTaskGuard(w, r, "CompetitorPrice", "read")
	if !ok {
		return
	}
	runs, err := engines.MyntraHarvestRuns(tenantID, 20)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	out := engines.MyntraHarvestStatus(tenantID)
	out["runs"] = runs
	out["can_run"] = engines.HasFullAccess(r.Header.Get("Resolved-Role")) && r.Header.Get("Resolved-Purpose") != "extension"
	_ = json.NewEncoder(w).Encode(out)
}

func handlePIMMarketHarvestRun(w http.ResponseWriter, r *http.Request) {
	if !requireAdminGate(w, r, engines.HasFullAccess(r.Header.Get("Resolved-Role"))) {
		return
	}
	var req engines.MyntraHarvestRequest
	if !pimDecodeBody(w, r, &req) {
		return
	}
	id, err := engines.EnqueueMyntraHarvest(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-User-ID"), req)
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]string{"job_id": id})
}

func handlePIMMarketHarvestReparse(w http.ResponseWriter, r *http.Request) {
	if !requireAdminGate(w, r, engines.HasFullAccess(r.Header.Get("Resolved-Role"))) {
		return
	}
	var req struct {
		Date string `json:"date"`
	}
	if !pimDecodeBody(w, r, &req) {
		return
	}
	sum, err := engines.ReparseMyntraHarvest(r.Header.Get("Resolved-Tenant-ID"), req.Date)
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(sum)
}
