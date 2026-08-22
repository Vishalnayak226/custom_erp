package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
)

func handleBundleAvailability(w http.ResponseWriter, r *http.Request) {
	rows, err := engines.GetBundleAvailability(r.Header.Get("Resolved-Tenant-ID"), r.PathValue("sku"))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"bundle_sku": r.PathValue("sku"), "locations": rows})
}

func handleBundleAssembly(w http.ResponseWriter, r *http.Request) {
	var req struct {
		BundleSKU    string `json:"bundle_sku"`
		LocationCode string `json:"location_code"`
		Quantity     int    `json:"quantity"`
		RequestKey   string `json:"idempotency_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid bundle operation payload")
		return
	}
	operation := "Assemble"
	if r.PathValue("operation") == "disassemble" {
		operation = "Disassemble"
	} else if r.PathValue("operation") != "assemble" {
		writeAPIErrorGeneric(w, r, http.StatusNotFound, "Unknown bundle operation")
		return
	}
	id, err := engines.PostBundleAssembly(r.Header.Get("Resolved-Tenant-ID"), req.BundleSKU, req.LocationCode, req.Quantity, operation, r.Header.Get("Resolved-User-ID"), req.RequestKey)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"operation_id": id, "status": "Completed", "operation": operation})
}
