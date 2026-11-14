package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
)

// Assortment bulk add, the markdown calendar and listing drift (PIM).

func handleAssortmentBulk(w http.ResponseWriter, r *http.Request) {
	if !requireHRAdmin(w, r, r.Header.Get("Resolved-Role")) {
		return
	}
	var in engines.BulkAssortmentInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "invalid request body")
		return
	}
	res, err := engines.BulkAddAssortment(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-Username"), in)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(res)
}

func handleMarkdownCalendar(w http.ResponseWriter, r *http.Request) {
	rows, err := engines.MarkdownCalendar(r.Header.Get("Resolved-Tenant-ID"))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to load the markdown plans")
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"plans": rows})
}

func handleListingDrift(w http.ResponseWriter, r *http.Request) {
	tenant, channel := r.Header.Get("Resolved-Tenant-ID"), r.URL.Query().Get("channel")
	rows, err := engines.ChannelDrift(tenant, channel)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	can := false
	if cfg, cErr := engines.LoadChannelRuntimeConfig(tenant, channel); cErr == nil {
		can = engines.ChannelCanListProducts(cfg.Platform)
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"rows": rows, "can_refresh": can})
}
