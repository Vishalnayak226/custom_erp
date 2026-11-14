package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"custom_erp/engines"
)

// Leases, capital work in progress and asset adjustments (ask 170, items
// 170.5-170.7). Each handler is the thin edge of an engines function.

func handleLeaseActivate(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	data, err := engines.ActivateLease(tenantID, r.PathValue("id"), userID)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(data)
}

func handleLeaseRunDue(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	n, err := engines.RunLeasePostingsForTenant(tenantID)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]int{"periods_posted": n})
}

func handleAssetCWIPStart(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	var req struct {
		AssetID string  `json:"asset_id"`
		Budget  float64 `json:"budget"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.AssetID) == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'asset_id' is required")
		return
	}
	if err := engines.MoveAssetToCWIP(tenantID, req.AssetID, userID, req.Budget); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "CWIP"})
}

func handleAssetCWIPCapitalize(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	var req struct {
		AssetID         string  `json:"asset_id"`
		UsefulLifeYears float64 `json:"useful_life_years"`
		Date            string  `json:"date"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.AssetID) == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'asset_id' is required")
		return
	}
	res, err := engines.CapitalizeCWIPAsset(tenantID, req.AssetID, userID, req.UsefulLifeYears, req.Date)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(res)
}

func handleAssetCWIPTotal(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	n, err := engines.CWIPCollectedPaise(tenantID, r.PathValue("id"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]float64{"collected": engines.PaiseToRupees(n)})
}

func handleAssetAdjust(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	var req struct {
		AssetID string `json:"asset_id"`
		engines.AssetAdjustmentInput
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.AssetID) == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'asset_id' is required")
		return
	}
	if req.IdempotencyKey == "" {
		req.IdempotencyKey = r.Header.Get("Idempotency-Key")
	}
	res, err := engines.AdjustAsset(tenantID, req.AssetID, userID, req.AssetAdjustmentInput)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(res)
}
