package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"custom_erp/engines"
)

// Stage 38.1 - the first curated /api/public/v1 handlers.
//
// Every handler here is thin on purpose. The projection lives in
// engines/public_api_v1.go so the shape a public caller sees is defined in one
// place; authentication, scope, rate limiting, idempotency and traffic logging
// all live in publicAPIMiddleware. What is left in this file is parameter
// parsing and one encode - which is exactly how much handler code a public
// endpoint should have, because anything more is behaviour that is not covered
// by the middleware's guarantees.

func publicPagingParams(r *http.Request) (limit, offset int) {
	limit, _ = strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("limit")))
	offset, _ = strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("offset")))
	return limit, offset
}

// writePublicError keeps the public surface on the same error envelope as the
// rest of the platform (error/code/correlation_id/retryable), which is what
// docs/specs/public_api_v1.md promises integrators.
func writePublicError(w http.ResponseWriter, r *http.Request, err error) {
	if err == engines.ErrPublicNotFound {
		writeAPIErrorGeneric(w, r, http.StatusNotFound, "No such resource.")
		return
	}
	writeEngineError(w, r, err, http.StatusUnprocessableEntity)
}

// GET /api/public/v1/items - scope items:read
func handlePublicListItems(w http.ResponseWriter, r *http.Request) {
	limit, offset := publicPagingParams(r)
	page, err := engines.ListPublicItems(r.Header.Get("Resolved-Tenant-ID"), r.URL.Query().Get("updated_since"), limit, offset)
	if err != nil {
		writePublicError(w, r, err)
		return
	}
	_ = json.NewEncoder(w).Encode(page)
}

// GET /api/public/v1/items/{code} - scope items:read
func handlePublicGetItem(w http.ResponseWriter, r *http.Request) {
	item, err := engines.GetPublicItem(r.Header.Get("Resolved-Tenant-ID"), r.PathValue("code"))
	if err != nil {
		writePublicError(w, r, err)
		return
	}
	_ = json.NewEncoder(w).Encode(item)
}

// GET /api/public/v1/inventory?sku=...&location= - scope inventory:read
func handlePublicInventory(w http.ResponseWriter, r *http.Request) {
	levels, err := engines.ListPublicInventory(r.Header.Get("Resolved-Tenant-ID"),
		r.URL.Query().Get("sku"), r.URL.Query().Get("location"))
	if err != nil {
		writePublicError(w, r, err)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"data": levels, "count": len(levels)})
}

// GET /api/public/v1/orders/{id}/status - scope orders:read. The path segment
// accepts either our order id or the caller's own channel order id.
func handlePublicOrderStatus(w http.ResponseWriter, r *http.Request) {
	status, err := engines.GetPublicOrderStatus(r.Header.Get("Resolved-Tenant-ID"), r.PathValue("id"))
	if err != nil {
		writePublicError(w, r, err)
		return
	}
	_ = json.NewEncoder(w).Encode(status)
}
