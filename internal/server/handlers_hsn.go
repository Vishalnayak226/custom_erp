package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

// handleHSNSuggest (Stage 66) serves the HSN box's list: codes ranked for the
// item being named (engines/hsn_suggest.go), or with ?hsn= the rates of one
// code, which is how the GST box follows the HSN box.
//
// Gated on reading the HSN catalogue - the same right the HSN picker's own
// search already needed. The client's existing items are drawn on only for
// the record types this role can read, since a suggestion quotes an item's
// name.
func handleHSNSuggest(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	role := r.Header.Get("Resolved-Role")
	allowed, err := checkPermission(tenantID, role, "HSNCode", "read")
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if !allowed {
		writeAPIError(w, r, "GLOBAL-0011", "")
		return
	}
	clip := func(s string) string {
		s = strings.TrimSpace(s)
		if len(s) > 300 {
			s = s[:300]
		}
		return s
	}
	q := engines.HSNSuggestQuery{
		Name:  clip(r.URL.Query().Get("name")),
		Query: clip(r.URL.Query().Get("q")),
		HSN:   clip(r.URL.Query().Get("hsn")),
	}
	if n, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil {
		q.Limit = n
	}
	for _, dt := range []string{"Item", "ItemName"} {
		if ok, err := checkPermission(tenantID, role, dt, "read"); err == nil && ok {
			q.HistoryDoctypes = append(q.HistoryDoctypes, dt)
		}
	}
	res, err := engines.SuggestHSN(tenantID, q)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(res)
}
