package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
)

// Stage 148.41: hold a serial piece for a customer. Placing and releasing need
// what a sales order needs (create on SalesOrder) and the store the piece is in;
// the list is read on SerialNumber.

func handleListPieceHolds(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if allowed, err := checkPermission(tenantID, r.Header.Get("Resolved-Role"), "SerialNumber", "read"); err != nil || !allowed {
		writeAPIError(w, r, "GLOBAL-0011", "")
		return
	}
	enabled := engines.GetSettingBool(tenantID, engines.SettingPieceHolds)
	holds := []engines.PieceHoldView{}
	if enabled {
		var err error
		if holds, err = engines.ListPieceHolds(tenantID); err != nil {
			writeEngineError(w, r, err, http.StatusInternalServerError)
			return
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"enabled": enabled, "holds": holds})
}

// pieceHoldStoreAccess finds the piece and refuses a store the caller has not been given.
func pieceHoldStoreAccess(w http.ResponseWriter, r *http.Request, sku, serialNo string) bool {
	piece, err := engines.GetSerial(r.Header.Get("Resolved-Tenant-ID"), sku, serialNo)
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return false
	}
	if piece == nil || piece.LocationCode == "" {
		return true // the engine answers an unknown piece
	}
	return requireStoreAccess(w, r, piece.LocationCode)
}

func handlePlacePieceHold(w http.ResponseWriter, r *http.Request) {
	if !requireCreate(w, r, "SalesOrder") {
		return
	}
	var req engines.PieceHoldRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request payload")
		return
	}
	if !pieceHoldStoreAccess(w, r, req.SKU, req.SerialNo) {
		return
	}
	view, err := engines.PlacePieceHold(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-User-ID"), req)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(view)
}

func handleReleasePieceHold(w http.ResponseWriter, r *http.Request) {
	if !requireCreate(w, r, "SalesOrder") {
		return
	}
	var req struct {
		SKU      string `json:"sku"`
		SerialNo string `json:"serial_no"`
		Reason   string `json:"reason"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request payload")
		return
	}
	if !pieceHoldStoreAccess(w, r, req.SKU, req.SerialNo) {
		return
	}
	if err := engines.ReleasePieceHold(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-User-ID"), req.SKU, req.SerialNo, req.Reason); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "released"})
}
