package server

import (
	"custom_erp/engines"
	"encoding/json"
	"fmt"
	"net/http"
)

// POST /api/v1/me/mfa/devices/forget (Stage 125.8): My Profile's "Forget
// remembered devices". Every browser this person ticked "Remember this
// device" on asks for the authenticator code again at its next sign-in,
// this one included. The session itself carries on.
func handleForgetMyMFADevices(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if userID == "" {
		writeAPIError(w, r, "GLOBAL-0009", "")
		return
	}
	forgotten, err := engines.ForgetMFADevices(tenantID, userID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Could not forget your remembered devices - try again")
		return
	}
	engines.LogAuditEventCtx(r.Context(), tenantID, r.Header.Get("Resolved-Username"), "SECURITY", "MFA_DEVICES_FORGOTTEN",
		fmt.Sprintf("Forgot %d remembered device(s); each asks for the authenticator code again", forgotten))
	_ = json.NewEncoder(w).Encode(map[string]int64{"forgotten": forgotten})
}
