package server

import (
	"encoding/json"
	"net/http"

	"custom_erp/engines"
)

// handleTaskInbox is Stage 73.3.3: GET /api/v1/inbox - the viewer's approvals,
// order-desk queues and catalog tasks in one list. OMS and PIM rows appear only
// when the client has the module and the role may read the source documents,
// the same two checks their own screens apply.
func handleTaskInbox(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	role := r.Header.Get("Resolved-Role")
	scope := engines.TaskInboxScope{
		UserID: r.Header.Get("Resolved-User-ID"), Username: r.Header.Get("Resolved-Username"),
		Role: role, Location: r.Header.Get("Resolved-Location"),
	}
	if engines.RequireModules(tenantID, "oms") == nil {
		scope.OMS, _ = checkPermission(tenantID, role, "SalesOrder", "read")
	}
	if engines.RequireModules(tenantID, "pim") == nil {
		scope.PIM, _ = checkPermission(tenantID, role, "PIMTask", "read")
	}
	// Stage 101.12: the CRM worklist, for anyone who may read customers.
	if engines.RequireModules(tenantID, "crm_loyalty") == nil {
		scope.CRM, _ = checkPermission(tenantID, role, "Customer", "read")
	}
	inbox, err := engines.ListTaskInbox(tenantID, scope)
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(inbox)
}
