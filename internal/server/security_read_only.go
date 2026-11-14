package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"custom_erp/engines"
)

// Stage 49.12.3 (owner's answer, 2026-11-14): read-only mode. While an
// administrator has it on, sign-in, sign-out and every read stay open, and
// every other change is refused at the one middleware gate - 423 with the
// stable marker X-Refused: READ_ONLY. Every answer to a signed-in request
// carries X-Read-Only: 1 while it is on, which is what shows the app's banner.

// readOnlyOpen names the write routes read-only mode leaves open: signing in,
// the emergency controls (so the mode can be turned off and an incident still
// answered), and the POST routes that only read - previews, quotes and the
// dashboard query. Every name here must be a registered route
// (TestStage49123ReadOnlyMode).
var readOnlyOpen = map[string]bool{
	"POST /api/v1/login":                                  true,
	"POST /api/v1/store-app/pin/login":                    true,
	"POST /api/v1/store-app/device/check":                 true,
	"POST /api/v1/admin/security/read-only":               true,
	"POST /api/v1/admin/security/sign-out-user":           true,
	"POST /api/v1/admin/security/sign-out-all":            true,
	"POST /api/v1/admin/security/sessions/end":            true,
	"POST /api/v1/admin/security/freeze-exports":          true,
	"POST /api/v1/admin/security/freeze-integrations":     true,
	"POST /api/v1/dashboards/query":                       true,
	"POST /api/v1/gst/calculate":                          true,
	"POST /api/v1/pos/quote":                              true,
	"POST /api/v1/pos/offers/preview":                     true,
	"POST /api/v1/crm/voucher/validate":                   true,
	"POST /api/v1/sales/custom-orders/quote":              true,
	"POST /api/v1/inventory/adjustments/preview":          true,
	"POST /api/v1/inventory/treatment-correction/preview": true,
	"POST /api/v1/pricing/endings/preview":                true,
	"POST /api/v1/procurement/purchase-order/preview":     true,
	"POST /api/v1/procurement/bulk-upload/preview":        true,
	"POST /api/v1/sales/bulk-upload/preview":              true,
	"POST /api/v1/pim/product-sheet/preview":              true,
	"POST /api/v1/pim/import/{doctype}/preview":           true,
	"POST /api/v1/pim/import-templates/{id}/preview":      true,
	"POST /api/v1/admin/setup-workbook/preview":           true,
	"POST /api/v1/wms/cross-dock/check":                   true,
	"POST /api/v1/wms/cartonization/suggest":              true,
	"POST /api/v1/wms/cartonization/suggest-v2":           true,
	"POST /api/v1/lines/resolve":                          true,
	"POST /api/v1/procurement/design/match":               true,
}

// readOnlyRefuses reports whether read-only mode turns this request away.
// Reads, sign-in and sign-out (/api/v1/auth/), a person's own password and
// two-factor settings (/api/v1/me/) and bulk exports - reads, which the
// export freeze governs - pass.
func readOnlyRefuses(r *http.Request, pattern string) bool {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	}
	p := r.URL.Path
	return !strings.HasPrefix(p, "/api/v1/auth/") && !strings.HasPrefix(p, "/api/v1/me/") &&
		!readOnlyOpen[pattern] && !isBulkExport(r)
}

// refuseReadOnly answers a refused change: 423, the marker header and one
// audit row naming the route.
func refuseReadOnly(w http.ResponseWriter, r *http.Request, tenantID, actor, command string) {
	engines.LogAuditEventCtx(r.Context(), tenantID, actor, "SECURITY_CONTROL", "REFUSED_READ_ONLY", command)
	w.Header().Set("X-Refused", "READ_ONLY")
	writeAPIErrorGeneric(w, r, http.StatusLocked, "This business is read-only for now. An administrator paused changes; you can still look things up.")
}

// handleReadOnlyMode turns read-only mode on or off: full admin, a reason and
// the administrator's own password, audited as SECURITY_CONTROL.
func handleReadOnlyMode(w http.ResponseWriter, r *http.Request) {
	req, ok := readEmergencyRequest(w, r)
	if !ok {
		return
	}
	if err := engines.SetReadOnlyMode(r.Header.Get("Resolved-Tenant-ID"), req.On, r.Header.Get("Resolved-Username"), req.Reason); err != nil {
		writeEmergencyError(w, r, err)
		return
	}
	detail, flag := "This business is read-only.", "1"
	if !req.On {
		detail, flag = "Changes are open again.", "0"
	}
	w.Header().Set("X-Read-Only", flag) // the banner follows at once
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "success", "detail": detail})
}
