package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
)

// Stage 36.4: PIM export & syndication depth. Template/schedule/catalog
// authoring (create/list/edit/delete) all use the same generic document API
// as every other PIM master; these handlers cover the actions the generic
// endpoint doesn't have - running a template, minting a catalog share link,
// serving the public share view, and pulling a channel's live state back.

// handlePIMExportTemplateRun (36.4.1/36.4.3) streams a template's CSV.
// Requires read on both PIMExportTemplate and Item, the same
// handlePIMProductGroupExport reasoning: template access alone must not
// become a way around Item permissions, since the export carries product
// data.
func handlePIMExportTemplateRun(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	role := r.Header.Get("Resolved-Role")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	for _, doctype := range []string{"PIMExportTemplate", "Item"} {
		allowed, err := checkPermission(tenantID, role, doctype, "read")
		if err != nil {
			writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		if !allowed {
			writeAPIError(w, r, "GLOBAL-0011", "")
			return
		}
	}
	templateID := r.PathValue("id")
	csvBytes, err := engines.RunPIMExportTemplate(tenantID, templateID)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", "attachment; filename="+csvAttachmentName("pim_export", templateID))
	_, _ = w.Write(csvBytes)
}

// handlePIMCatalogRotateShareToken (36.4.4) mints/rotates a catalog's share
// link, returning the raw token exactly once - the same shape
// handlePIMImportScheduleRotateHookToken already gives.
func handlePIMCatalogRotateShareToken(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	role := r.Header.Get("Resolved-Role")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	allowed, err := checkPermission(tenantID, role, "PIMCatalog", "update")
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if !allowed {
		writeAPIError(w, r, "GLOBAL-0011", "")
		return
	}
	catalogID := r.PathValue("id")
	token, rErr := engines.RotatePIMCatalogShareToken(tenantID, catalogID)
	if rErr != nil {
		writeEngineError(w, r, rErr, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"share_token": token, "tenant_id": tenantID})
}

// handlePIMCatalogShare (36.4.4) is the one unauthenticated route in this
// file - a partner holds only the token, never a session, the same posture
// handlePIMImportHook already takes for its own inbound token. Unlike that
// header-carried token, this one travels as a query parameter: the link has
// to be openable from a plain browser click, which cannot attach a custom
// header. Its path is static and listed once in middleware.go's
// publicRoutes; apiMiddleware resolves Resolved-Tenant-ID from
// ?tenant_id=/X-Tenant-ID/Host the same way it does for every other route,
// public or not, so the generated share link carries ?tenant_id= alongside
// ?token=. An unknown token, a deactivated catalog and an expired one all
// answer the same generic 404 - which of those is true is not information
// this endpoint should confirm.
func handlePIMCatalogShare(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	token := r.URL.Query().Get("token")
	if token == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Query parameter 'token' is required")
		return
	}
	view, err := engines.ResolvePIMCatalogShareToken(tenantID, token)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusNotFound, "Catalog not found")
		return
	}
	_ = json.NewEncoder(w).Encode(view)
}

// handlePIMChannelPullState (36.4.5) is "bulk channel download": pull every
// selected item's current state back from the channel (for a connector that
// supports it) and diff it against what a republish would send. Selection
// reuses ResolvePIMBulkTargetIDs (the 36.1.3 seam), so a product group and
// an explicit selection stay mutually exclusive here exactly as they are
// for bulk edit. One item's failure (never published yet, connector doesn't
// support a read, rate budget exhausted) never aborts the rest - the same
// per-item outcome shape 36.2.5's bulk actions already report.
func handlePIMChannelPullState(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	role := r.Header.Get("Resolved-Role")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	for _, doctype := range []string{"Channel", "Item"} {
		allowed, err := checkPermission(tenantID, role, doctype, "read")
		if err != nil {
			writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		if !allowed {
			writeAPIError(w, r, "GLOBAL-0011", "")
			return
		}
	}
	channelCode := r.PathValue("code")
	var req struct {
		ItemCodes []string `json:"item_codes"`
		GroupID   string   `json:"group_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid payload JSON")
		return
	}
	itemCodes, err := engines.ResolvePIMBulkTargetIDs(tenantID, "Item", req.GroupID, req.ItemCodes)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if len(itemCodes) == 0 {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "No items to pull - send item_codes or a group_id")
		return
	}
	results := engines.BulkPullChannelState(tenantID, channelCode, itemCodes)
	_ = json.NewEncoder(w).Encode(results)
}
