package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
	"net/url"
)

// Stage 148.36: custom-order configurator (module custom_orders). The choices,
// the price preview and the save that makes the Item and a Draft sales order.
// The module gate sits on the routes (routes.go); the masters are the generic
// doctypes MetalRate, CustomChoice and CustomOrderRule.

type customOrderRequest struct {
	engines.CustomOrderInput
	engines.CustomOrderCustomer
}

func decodeCustomOrder(w http.ResponseWriter, r *http.Request) (*customOrderRequest, bool) {
	var req customOrderRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request payload")
		return nil, false
	}
	return &req, true
}

func handleCustomOrderChoices(w http.ResponseWriter, r *http.Request) {
	out, err := engines.CustomOrderChoices(r.Header.Get("Resolved-Tenant-ID"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(out)
}

func handleCustomOrderQuote(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeCustomOrder(w, r)
	if !ok {
		return
	}
	p, err := engines.PriceCustomOrder(r.Header.Get("Resolved-Tenant-ID"), req.CustomOrderInput)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(p)
}

// handleCreateCustomOrder runs behind a command key (routes.go): one press with one
// key makes one item and one order, however many times it arrives.
func handleCreateCustomOrder(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if !requireCreate(w, r, "SalesOrder") || !requireCreate(w, r, "Item") {
		return
	}
	req, ok := decodeCustomOrder(w, r)
	if !ok {
		return
	}
	p, err := engines.PriceCustomOrder(tenantID, req.CustomOrderInput)
	if err == nil {
		err = engines.CheckCustomOrderReady(p, req.CustomOrderCustomer)
	}
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	// The item goes through the normal item save: its rules, its numbering.
	c := newSWCaller(r)
	status, out := c.call("/api/v1/doc/{doctype}", http.MethodPost, "/api/v1/doc/"+url.PathEscape("Item"),
		map[string]string{"doctype": "Item"}, engines.CustomOrderItemBody(tenantID, p), handleGenericDoc)
	if status < 200 || status >= 300 {
		writeAPIErrorDetail(w, r, "SALESP-0767", "", swMessage(status, out))
		return
	}
	saved := map[string]interface{}{}
	_ = json.Unmarshal(out, &saved)
	itemID := swText(saved["id"])
	if itemID == "" {
		itemID = swText(saved["code"])
	}
	res, err := engines.SaveCustomOrder(tenantID, r.Header.Get("Resolved-User-ID"), p, itemID, req.CustomOrderCustomer)
	if err != nil {
		// The item was made a moment ago for this order; do not leave it behind.
		engines.DiscardCustomOrderItem(tenantID, itemID)
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(res)
}
