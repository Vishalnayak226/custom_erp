package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
	"strings"
)

// Stage 168.3: a vendor's onboarding decision is filed here and then decided
// through the ordinary approval routes (/api/v1/approval/decide), so there is
// one approval path. The caller needs the right to change a Vendor.
func handleVendorOnboardingRequest(w http.ResponseWriter, r *http.Request) {
	if refuseExtensionToken(w, r) {
		return
	}
	var req struct {
		VendorID string `json:"vendor_id"`
		Decision string `json:"decision"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.VendorID) == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'vendor_id' and 'decision' are required")
		return
	}
	tenantID, role := r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-Role")
	if allowed, err := checkPermission(tenantID, role, "Vendor", "update"); err != nil || !allowed {
		writeAPIError(w, r, "GLOBAL-0011", "")
		return
	}
	id, err := engines.RequestVendorOnboarding(tenantID, strings.TrimSpace(req.VendorID), req.Decision, r.Header.Get("Resolved-User-ID"), role)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"id": id, "status": "Pending Approval"})
}
