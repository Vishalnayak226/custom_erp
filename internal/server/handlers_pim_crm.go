package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
	"strings"
)

// Stage 140.12: a marketplace's ready-made feed layout on a channel (.22) and
// the CRM's readiness check, test connection and sender presets (.26).

// handlePIMApplyChannelPack sets a channel up from its platform's pack. It
// writes field maps, attributes and an export template, so the caller needs
// create rights on all three.
func handlePIMApplyChannelPack(w http.ResponseWriter, r *http.Request) {
	tenantID, role := r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-Role")
	for _, doctype := range []string{"ChannelFieldMap", "ProductAttributeDef", "PIMExportTemplate"} {
		allowed, err := checkPermission(tenantID, role, doctype, "create")
		if err != nil {
			writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		if !allowed {
			writeAPIError(w, r, "GLOBAL-0011", "")
			return
		}
	}
	res, err := engines.ApplyChannelPack(tenantID, r.PathValue("code"), r.Header.Get("Resolved-Username"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(res)
}

// handleCRMReadiness answers whether a channel can send, and what is missing.
func handleCRMReadiness(w http.ResponseWriter, r *http.Request) {
	channels := []string{engines.CRMChannelWhatsApp, engines.CRMChannelSMS, engines.CRMChannelEmail}
	if c := strings.TrimSpace(r.URL.Query().Get("channel")); c != "" {
		channels = []string{c}
	}
	out := []*engines.CRMReadiness{}
	for _, c := range channels {
		res, err := engines.CRMChannelReadiness(r.Header.Get("Resolved-Tenant-ID"), c)
		if err != nil {
			writeEngineError(w, r, err, http.StatusInternalServerError)
			return
		}
		out = append(out, res)
	}
	_ = json.NewEncoder(w).Encode(out)
}

// handleCRMSenderTest is a sender's "test connection".
func handleCRMSenderTest(w http.ResponseWriter, r *http.Request) {
	var body struct {
		To string `json:"to"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	res, err := engines.TestCRMSender(r.Context(), r.Header.Get("Resolved-Tenant-ID"), r.PathValue("id"), body.To)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(res)
}

// handleCRMSenderPresets lists the providers a sender form can be filled from.
func handleCRMSenderPresets(w http.ResponseWriter, r *http.Request) {
	_ = json.NewEncoder(w).Encode(engines.CRMSenderPresets())
}
