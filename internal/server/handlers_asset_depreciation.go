package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"custom_erp/engines"
)

// Stage 55.16 (Batch D): POST /api/v1/assets/depreciation/run posts every
// Capitalised asset's ended, unposted months now - the monthly job's own
// work, on demand. Period close owns the books, so only an Accountant or a
// Super Admin may run it (CanManagePeriodClose); the job itself needs no one.
func handleRunAssetDepreciation(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if !engines.CanManagePeriodClose(r.Header.Get("Resolved-Role")) {
		writeAPIError(w, r, "ASSET-0536", "")
		return
	}
	result, err := engines.RunAssetDepreciation(r.Context(), tenantID)
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	engines.LogAuditEvent(tenantID, r.Header.Get("Resolved-User-ID"), "ASSET_DEPRECIATION_RUN", "SUCCESS",
		fmt.Sprintf("Depreciation run through %s: %d month(s) posted (%.2f), %d skipped, %d asset(s) checked",
			result.ThroughMonth, result.MonthsPosted, result.AmountPosted, result.MonthsSkipped, result.AssetsChecked))
	_ = json.NewEncoder(w).Encode(result)
}

// GET /api/v1/assets/{id}/disposal-preview?proceeds=&received_into= - what
// disposing now would post (depreciation first, then the disposal entry and
// its gain or loss), and the accounts proceeds can be received into.
func handleAssetDisposalPreview(w http.ResponseWriter, r *http.Request) {
	proceeds := 0.0
	if raw := r.URL.Query().Get("proceeds"); raw != "" {
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			writeAPIError(w, r, "ASSET-0537", "")
			return
		}
		proceeds = v
	}
	preview, err := engines.PreviewAssetDisposal(r.Header.Get("Resolved-Tenant-ID"), r.PathValue("id"), proceeds, r.URL.Query().Get("received_into"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusNotFound)
		return
	}
	_ = json.NewEncoder(w).Encode(preview)
}
