package server

import (
	"net/http"
	"strings"

	"custom_erp/engines"
)

// POST /api/v1/auth/logout (162.15): signs this session out on the server, so
// its token is refused from the next request on - a copied token included.
// Other sessions of the same user stay open (Sign out everywhere ends those).
// The browser clears its own storage whatever this answers.
func handleLogout(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	claims, err := engines.ParseToken(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if err != nil || tenantID == "" || userID == "" {
		writeAPIError(w, r, "GLOBAL-0009", "")
		return
	}
	if err := engines.EndUserSession(tenantID, userID, claims["user"], claims["jti"], engines.TokenExpiry(claims)); err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	engines.LogAuditEventCtx(r.Context(), tenantID, claims["user"], "AUTH", "SIGNED_OUT", "Signed out")
	writeJSON(w, http.StatusOK, map[string]string{"status": "signed_out"})
}
