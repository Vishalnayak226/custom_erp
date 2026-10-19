package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
	"strings"
)

// GET /api/v1/reason-codes?category= (Stage 74.1.1): the reason codes usable
// today for one action, for a screen's reason picker. Codes are not
// sensitive; anyone signed in who records the exception needs to see them.
func handleListReasonCodes(w http.ResponseWriter, r *http.Request) {
	category := strings.TrimSpace(r.URL.Query().Get("category"))
	if category == "" {
		writeAPIErrorGeneric(w, r, http.StatusBadRequest, "category is required")
		return
	}
	codes, err := engines.ListReasonCodes(r.Header.Get("Resolved-Tenant-ID"), category)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to load the reason codes")
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"codes":    codes,
		"required": engines.ReasonCodesRequired(r.Header.Get("Resolved-Tenant-ID")),
	})
}
