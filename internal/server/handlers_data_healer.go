package server

import (
	"encoding/json"
	"net/http"

	"custom_erp/engines"
)

// Data Healer (ask 207): the heals a client runs after a release
// (engines/data_healer.go). Every signed-in user reads the list - the banner
// on every screen and the save check on the pages a heal covers need it; the
// records and Run heal follow the DataHeal type's read and update rights.
// Extension tokens skip the route capability check, so each handler refuses
// them itself.

// healMaxTyped bounds the values one Run heal takes from the screen; the
// screen lists at most 500 records.
const healMaxTyped = 5000

func healExtensionRefused(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("Resolved-Purpose") == "extension" {
		writeAPIErrorGeneric(w, r, http.StatusForbidden, "Not allowed.")
		return true
	}
	return false
}

func dataHealAllowed(w http.ResponseWriter, r *http.Request, action string) bool {
	if healExtensionRefused(w, r) {
		return false
	}
	ok, _ := checkPermission(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-Role"), "DataHeal", action)
	if !ok {
		writeAPIErrorGeneric(w, r, http.StatusForbidden, "Not allowed.")
	}
	return ok
}

func handleListHeals(w http.ResponseWriter, r *http.Request) {
	if healExtensionRefused(w, r) {
		return
	}
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	heals, err := engines.ListHeals(tenantID)
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	canRun, _ := checkPermission(tenantID, r.Header.Get("Resolved-Role"), "DataHeal", "update")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"heals": heals, "can_run": canRun,
		"can_rename": engines.IsSuperAdmin(r.Header.Get("Resolved-Role"))})
}

func handleHealRecords(w http.ResponseWriter, r *http.Request) {
	if !dataHealAllowed(w, r, "read") {
		return
	}
	st, recs, err := engines.HealRecords(r.Header.Get("Resolved-Tenant-ID"), r.PathValue("id"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"heal": st, "records": recs})
}

func handleRunHeal(w http.ResponseWriter, r *http.Request) {
	if !dataHealAllowed(w, r, "update") {
		return
	}
	var req struct {
		Typed map[string]string `json:"typed"`
	}
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "The values could not be read.")
			return
		}
	}
	if len(req.Typed) > healMaxTyped {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Too many typed values.")
		return
	}
	res, err := engines.RunHeal(r.Header.Get("Resolved-Tenant-ID"), r.PathValue("id"), r.Header.Get("Resolved-Username"), req.Typed)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(res)
}

// Item 201.4: change an item's name on past documents
// (engines/data_healer_rename.go). Super Admin only, both the preview and
// the change.
func pastRenameRequest(w http.ResponseWriter, r *http.Request) (engines.PastRenameRequest, bool) {
	var req engines.PastRenameRequest
	if healExtensionRefused(w, r) || !requireSuperAdmin(w, r, r.Header.Get("Resolved-Role")) {
		return req, false
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "The form could not be read.")
		return req, false
	}
	return req, true
}

func handlePastRenamePreview(w http.ResponseWriter, r *http.Request) {
	req, ok := pastRenameRequest(w, r)
	if !ok {
		return
	}
	out, err := engines.PastRenamePreviewFor(r.Header.Get("Resolved-Tenant-ID"), req)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(out)
}

func handlePastRenameApply(w http.ResponseWriter, r *http.Request) {
	req, ok := pastRenameRequest(w, r)
	if !ok {
		return
	}
	res, err := engines.PastRenameApply(r.Header.Get("Resolved-Tenant-ID"), req, r.Header.Get("Resolved-Username"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(res)
}
