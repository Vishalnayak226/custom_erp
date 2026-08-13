package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
)

// handlePIMProductGroupMembers resolves both static and dynamic groups through
// one read endpoint. Group creation/editing remains on the generic document
// API, preserving the existing metadata/RBAC/audit machinery.
func handlePIMProductGroupMembers(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	role := r.Header.Get("Resolved-Role")
	allowed, err := checkPermission(tenantID, role, "PIMProductGroup", "read")
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if !allowed {
		writeAPIError(w, r, "GLOBAL-0011", "")
		return
	}
	resolved, err := engines.ResolvePIMProductGroup(tenantID, r.PathValue("id"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(resolved)
}

// handlePIMProductGroupExport (Stage 36.1.3) streams one group's live
// membership as CSV. Reading a group requires read on PIMProductGroup and read
// on Item: the export carries product data, so group access alone must not
// become a way around Item permissions.
func handlePIMProductGroupExport(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	role := r.Header.Get("Resolved-Role")
	for _, doctype := range []string{"PIMProductGroup", "Item"} {
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
	groupID, csvBytes, err := engines.ExportPIMProductGroupCSV(tenantID, r.PathValue("id"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", "attachment; filename="+csvAttachmentName("product_group", groupID))
	_, _ = w.Write(csvBytes)
}

// csvAttachmentName keeps a document id out of the Content-Disposition header's
// grammar - an id is user-supplied data and must not be able to inject quotes,
// separators or CRLF into a response header.
func csvAttachmentName(prefix, id string) string {
	safe := make([]rune, 0, len(id))
	for _, ch := range id {
		switch {
		case ch >= 'a' && ch <= 'z', ch >= 'A' && ch <= 'Z', ch >= '0' && ch <= '9', ch == '-', ch == '_':
			safe = append(safe, ch)
		default:
			safe = append(safe, '_')
		}
	}
	if len(safe) == 0 {
		return prefix + ".csv"
	}
	return prefix + "_" + string(safe) + ".csv"
}
