package server

import (
	"encoding/json"
	"net/http"

	"custom_erp/engines"
)

// Stage 55.18 - read-only service desk and project context
// (engines/service_desk.go). Lifecycle actions stay on the existing
// /api/v1/service/ticket/{id}/... routes.

// GET /api/v1/service/desk?mine=1
func handleServiceDesk(w http.ResponseWriter, r *http.Request) {
	tickets, err := engines.ServiceDesk(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-User-ID"), r.URL.Query().Get("mine") == "1")
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"tickets": tickets})
}

// GET /api/v1/service/contracts/{id}
func handleServiceContractView(w http.ResponseWriter, r *http.Request) {
	view, err := engines.ServiceContractView(r.Header.Get("Resolved-Tenant-ID"), r.PathValue("id"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusNotFound)
		return
	}
	_ = json.NewEncoder(w).Encode(view)
}

// GET /api/v1/projects/{id}/ledger
func handleProjectLedger(w http.ResponseWriter, r *http.Request) {
	ledger, err := engines.ProjectLedger(r.Header.Get("Resolved-Tenant-ID"), r.PathValue("id"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusNotFound)
		return
	}
	_ = json.NewEncoder(w).Encode(ledger)
}
