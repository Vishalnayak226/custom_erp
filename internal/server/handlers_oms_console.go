package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
	"strconv"
)

// Stage 35.2 - the OMS Console's HTTP surface.
//
// Read endpoints only, plus the bulk-action and saved-view writes. Every
// single-order mutation the console's action bar fires already has a route
// (POST /api/v1/orders/{id}/hold, /release-hold, /cancel from 26.12.1, and
// 35.3's mutation routes) and is reused as-is rather than duplicated here -
// the console is a screen over the existing engines, not a second API for
// them.

// omsConsoleFilterFromQuery reads the faceted filter off the query string.
func omsConsoleFilterFromQuery(r *http.Request) engines.OrderConsoleFilter {
	q := r.URL.Query()
	atoi := func(key string) int {
		n, _ := strconv.Atoi(q.Get(key))
		return n
	}
	return engines.OrderConsoleFilter{
		Channel:    q.Get("channel"),
		Status:     q.Get("status"),
		HoldReason: q.Get("hold_reason"),
		Location:   q.Get("location"),
		FromDate:   q.Get("from_date"),
		ToDate:     q.Get("to_date"),
		SLAMinutes: atoi("sla_minutes"),
		Limit:      atoi("limit"),
		Offset:     atoi("offset"),
	}
}

// handleOMSOrderList is 35.2.1: the cross-channel, faceted, paginated order
// list. Replaces the console's old habit of fetching every SalesOrder,
// FulfillmentTask, LogisticsBooking and SalesInvoice in full and joining them
// in the browser.
func handleOMSOrderList(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	result, err := engines.ListOrdersForConsole(tenantID, omsConsoleFilterFromQuery(r))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(result)
}

// handleOMSOrderDetail is 35.2.2: everything about one order on one page.
func handleOMSOrderDetail(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	detail, err := engines.GetOrderConsoleDetail(tenantID, r.PathValue("id"))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusNotFound, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(detail)
}

// handleOMSConsoleTiles is 35.2.4: the four headline counts, each produced by
// running its already-registered report.
func handleOMSConsoleTiles(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"tiles": engines.OMSConsoleTiles(tenantID, r.Header.Get("Resolved-Role")),
	})
}

// handleOMSOrderSearch is 35.2.6: one lookup across order id, channel order
// id, AWB, phone, customer name and SKU.
func handleOMSOrderSearch(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	results, err := engines.SearchOrdersGlobal(tenantID, r.URL.Query().Get("q"), limit)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"results": results})
}

// handleOMSBulkAction is 35.2.5. Returns 200 with a per-order breakdown even
// when some orders failed: a bulk action over a mixed selection is expected to
// be partially applicable (an order already Shipped cannot be cancelled), and
// a blanket 4xx would hide the ones that did work.
func handleOMSBulkAction(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req struct {
		Action     string   `json:"action"`
		OrderIDs   []string `json:"order_ids"`
		ReasonCode string   `json:"reason_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid payload")
		return
	}
	if len(req.OrderIDs) == 0 {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'order_ids' must contain at least one order")
		return
	}
	result, err := engines.BulkOrderAction(tenantID, req.Action, req.OrderIDs, req.ReasonCode, r.Header.Get("Resolved-User-ID"))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(result)
}

// handleOMSSavedViews lists (GET) or creates (POST) the console's saved views.
func handleOMSSavedViews(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	switch r.Method {
	case http.MethodGet:
		views, err := engines.ListOMSViews(tenantID, userID)
		if err != nil {
			writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"views": views})
	case http.MethodPost:
		var req struct {
			Name   string                     `json:"name"`
			Filter engines.OrderConsoleFilter `json:"filter"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid payload")
			return
		}
		viewID, err := engines.SaveOMSView(tenantID, userID, req.Name, req.Filter)
		if err != nil {
			writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"id": viewID, "status": "saved"})
	default:
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// handleOMSDeleteSavedView removes one of the caller's own saved views.
func handleOMSDeleteSavedView(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodDelete {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	if err := engines.DeleteOMSView(tenantID, r.Header.Get("Resolved-User-ID"), r.PathValue("id")); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusNotFound, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
}
