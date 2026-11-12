package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
)

// Stage 146.4.5: a channel's listings - pull, link by SKU or barcode after a
// preview, and the per-listing stock switch. Reading is for anyone who can
// open the channel; pulling, linking and switching are for an administrator.

func handleChannelListings(w http.ResponseWriter, r *http.Request) {
	tenant, channel := r.Header.Get("Resolved-Tenant-ID"), r.PathValue("channel")
	rows, err := engines.ListChannelListings(tenant, channel, r.URL.Query().Get("filter"))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to list the listings")
		return
	}
	can := false
	if cfg, cErr := engines.LoadChannelRuntimeConfig(tenant, channel); cErr == nil {
		can = engines.ChannelCanListProducts(cfg.Platform)
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"listings": rows, "can_pull": can})
}

func handleChannelListingsPull(w http.ResponseWriter, r *http.Request) {
	if !requireHRAdmin(w, r, r.Header.Get("Resolved-Role")) {
		return
	}
	ctx, cancel := shopifyCtx(r)
	defer cancel()
	res, err := engines.PullChannelListings(ctx, r.Header.Get("Resolved-Tenant-ID"), r.PathValue("channel"), r.Header.Get("Resolved-Username"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(res)
}

func handleChannelListingsLinkPreview(w http.ResponseWriter, r *http.Request) {
	pv, err := engines.PreviewListingLinks(r.Header.Get("Resolved-Tenant-ID"), r.PathValue("channel"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(pv)
}

func handleChannelListingsLink(w http.ResponseWriter, r *http.Request) {
	if !requireHRAdmin(w, r, r.Header.Get("Resolved-Role")) {
		return
	}
	var req struct {
		Refs []string `json:"refs"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	linked, left, err := engines.CommitListingLinks(r.Header.Get("Resolved-Tenant-ID"), r.PathValue("channel"), r.Header.Get("Resolved-Username"), req.Refs)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]int{"linked": linked, "left": left})
}

func handleChannelListingsStockSync(w http.ResponseWriter, r *http.Request) {
	if !requireHRAdmin(w, r, r.Header.Get("Resolved-Role")) {
		return
	}
	var req struct {
		Ref string `json:"ref"`
		On  bool   `json:"on"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Ref == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Choose a listing")
		return
	}
	if err := engines.SetListingStockSync(r.Header.Get("Resolved-Tenant-ID"), r.PathValue("channel"), req.Ref, req.On, r.Header.Get("Resolved-Username")); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "saved", "on": req.On})
}
