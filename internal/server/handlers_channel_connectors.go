package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
	"strings"
)

func handleConnectorDescriptors(w http.ResponseWriter, r *http.Request) {
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"connectors": engines.ListConnectorDescriptors()})
}
func handleChannelConnectorCredentials(w http.ResponseWriter, r *http.Request) {
	if !engines.HasFullAccess(r.Header.Get("Resolved-Role")) {
		writeAPIError(w, r, "GLOBAL-0011", "")
		return
	}
	tenant, channel := r.Header.Get("Resolved-Tenant-ID"), r.PathValue("channel")
	cfg, err := engines.LoadChannelRuntimeConfig(tenant, channel)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if r.Method == http.MethodGet {
		// 49.8.5: configured, plus owner / last use / expiry - never the secret.
		info, err := engines.GetChannelCredentialInfo(tenant, channel)
		if err != nil {
			writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"channel": channel, "platform": cfg.Platform, "configured": info.Configured, "owner": info.Owner, "last_used_at": info.LastUsedAt, "expires_at": info.ExpiresAt, "expired": info.Expired})
		return
	}
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var fields map[string]string
	if err := json.NewDecoder(r.Body).Decode(&fields); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid credential payload")
		return
	}
	expiresAt := takeCredentialExpiry(fields)
	connector, err := engines.ResolveOmnichannelConnector(cfg.Platform)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if err := connector.ValidateCredentials(fields); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if err := engines.SaveChannelCredentialBy(tenant, channel, fields, credentialActor(r), expiresAt); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "saved", "channel": channel, "platform": cfg.Platform})
}

// credentialActor (49.8.5) is the owner a saved credential is recorded
// under: the signed-in user's name, else their id.
func credentialActor(r *http.Request) string {
	if u := strings.TrimSpace(r.Header.Get("Resolved-Username")); u != "" {
		return u
	}
	return strings.TrimSpace(r.Header.Get("Resolved-User-ID"))
}

// takeCredentialExpiry (49.8.5) lifts an optional expires_at out of a
// credential payload, so it is stored as the row's expiry and never inside
// the encrypted fields: nil when absent, "" to clear.
func takeCredentialExpiry(fields map[string]string) *string {
	v, ok := fields["expires_at"]
	if !ok {
		return nil
	}
	delete(fields, "expires_at")
	v = strings.TrimSpace(v)
	return &v
}

// handleConnectorDisconnect (49.8.5): POST {kind, code} removes a connector
// credential and the state that would keep calling with it; admin only.
func handleConnectorDisconnect(w http.ResponseWriter, r *http.Request) {
	if !engines.HasFullAccess(r.Header.Get("Resolved-Role")) {
		writeAPIError(w, r, "GLOBAL-0011", "")
		return
	}
	var req struct {
		Kind string `json:"kind"`
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Kind == "" || req.Code == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "kind and code are required")
		return
	}
	cleaned, err := engines.DisconnectConnectorCredential(r.Header.Get("Resolved-Tenant-ID"), req.Kind, req.Code, credentialActor(r))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "disconnected", "kind": req.Kind, "code": req.Code, "cleaned": cleaned})
}
func handleChannelOrderPull(w http.ResponseWriter, r *http.Request) {
	processed, failed, err := engines.PullChannelOrders(r.Context(), r.Header.Get("Resolved-Tenant-ID"), r.PathValue("channel"))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "complete", "processed": processed, "failed": failed})
}
func handleChannelInventorySync(w http.ResponseWriter, r *http.Request) {
	processed, failed, err := engines.SyncChannelInventory(r.Context(), r.Header.Get("Resolved-Tenant-ID"), r.PathValue("channel"))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "complete", "processed": processed, "failed": failed})
}
func handleChannelStatusPush(w http.ResponseWriter, r *http.Request) {
	var req engines.ConnectorStatusUpdate
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ChannelOrderID == "" || req.Status == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "channel_order_id and status are required")
		return
	}
	if err := engines.PushChannelOrderStatus(r.Context(), r.Header.Get("Resolved-Tenant-ID"), r.PathValue("channel"), req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "pushed", "channel_order_id": req.ChannelOrderID})
}
func handleChannelSKUMappings(w http.ResponseWriter, r *http.Request) {
	tenant := r.Header.Get("Resolved-Tenant-ID")
	if r.Method == http.MethodGet {
		rows, err := engines.ListChannelSKUMappings(tenant, r.URL.Query().Get("channel"))
		if err != nil {
			writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		if rows == nil {
			rows = []engines.ChannelSKUMapping{}
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"mappings": rows})
		return
	}
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req engines.ChannelSKUMapping
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid SKU mapping payload")
		return
	}
	if err := engines.UpsertChannelSKUMapping(tenant, req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "mapped", "channel": req.Channel, "channel_sku": req.ChannelSKU, "sku": req.SKU})
}
func handleChannelSKUExceptions(w http.ResponseWriter, r *http.Request) {
	rows, err := engines.ListOpenChannelSKUExceptions(r.Header.Get("Resolved-Tenant-ID"), r.URL.Query().Get("channel"))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"exceptions": rows})
}
func handleConnectorHealth(w http.ResponseWriter, r *http.Request) {
	health, err := engines.GetConnectorHealth(r.Header.Get("Resolved-Tenant-ID"))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if health == nil {
		health = []engines.ChannelHealth{}
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"channels": health})
}

// handleChannelSetupCheck (Stage 130.19, 55.21) says whether a channel's saved
// credentials would connect and how to set it up - never a credential value.
func handleChannelSetupCheck(w http.ResponseWriter, r *http.Request) {
	if !engines.HasFullAccess(r.Header.Get("Resolved-Role")) {
		writeAPIError(w, r, "GLOBAL-0011", "")
		return
	}
	out, err := engines.CheckChannelSetup(r.Header.Get("Resolved-Tenant-ID"), r.PathValue("channel"))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(out)
}
