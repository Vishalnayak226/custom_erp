package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
)

// Stage 55.7: the inbound queue (expected deliveries with their appointment
// and yard stage, and stock waiting for putaway) and the two yard actions the
// server now guards - checking a trailer in once, and spotting it at a free,
// active door.

func handleInboundWork(w http.ResponseWriter, r *http.Request) {
	if !rfqAllowed(w, r, "ASN", "read") {
		return
	}
	out, err := engines.InboundWork(r.Header.Get("Resolved-Tenant-ID"), r.URL.Query().Get("location"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(out)
}

func handleYardCheckIn(w http.ResponseWriter, r *http.Request) {
	var in engines.CheckInInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request body")
		return
	}
	if !rfqAllowed(w, r, "YardCheckIn", "create") {
		return
	}
	id, err := engines.CheckInTrailer(r.Header.Get("Resolved-Tenant-ID"), in, r.Header.Get("Resolved-User-ID"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": id, "status": "InYard"})
}

func handleYardAssignDoor(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DockDoor string `json:"dock_door"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request body")
		return
	}
	if !rfqAllowed(w, r, "YardCheckIn", "update") {
		return
	}
	id := r.PathValue("id")
	if err := engines.AssignYardDoor(r.Header.Get("Resolved-Tenant-ID"), id, req.DockDoor, r.Header.Get("Resolved-User-ID")); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": id, "status": "AtDoor", "dock_door": req.DockDoor})
}
