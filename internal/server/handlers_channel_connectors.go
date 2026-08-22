package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
)

func handleConnectorDescriptors(w http.ResponseWriter, r *http.Request) {
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"connectors": engines.ListConnectorDescriptors()})
}
func handleChannelConnectorCredentials(w http.ResponseWriter, r *http.Request) {
	if !engines.IsSuperAdmin(r.Header.Get("Resolved-Role")) {
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
		configured, err := engines.HasChannelCredential(tenant, channel)
		if err != nil {
			writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"channel": channel, "platform": cfg.Platform, "configured": configured})
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
	connector, err := engines.ResolveOmnichannelConnector(cfg.Platform)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if err := connector.ValidateCredentials(fields); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if err := engines.SaveChannelCredential(tenant, channel, fields); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "saved", "channel": channel, "platform": cfg.Platform})
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
