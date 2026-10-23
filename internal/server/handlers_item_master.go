package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"custom_erp/engines"
)

// Stage 72 - item & material master blueprint: Consumption Entry (72.6) and
// gift cards as stored value (72.8). The till's packaging and gift-with-
// purchase changes ride on the existing /pos/quote and /checkout handlers.

// requireStoreManager admits a store manager or an administrator - the roles
// the blueprint (19.1) gives "approve selected POS overrides / exception
// issue": posting consumption, registering, blocking and cancelling cards.
func requireStoreManager(w http.ResponseWriter, r *http.Request) bool {
	role := r.Header.Get("Resolved-Role")
	if engines.HasFullAccess(role) || engines.CanonicalRole(role) == engines.RoleStoreManager {
		return true
	}
	writeAPIErrorGeneric(w, r, http.StatusForbidden, "Only a store manager or an administrator can do this")
	return false
}

// handlePostConsumptionEntry - POST /api/v1/inventory/consumption.
func handlePostConsumptionEntry(w http.ResponseWriter, r *http.Request) {
	if !requireStoreManager(w, r) {
		return
	}
	var req engines.ConsumptionEntryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid consumption entry")
		return
	}
	result, err := engines.PostConsumptionEntry(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-Username"), req)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(result)
}

// handleConsumptionBookStock - GET /api/v1/inventory/consumption/book?sku=&location=
// gives the form each line's book stock as it is added.
func handleConsumptionBookStock(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	sku, location := strings.TrimSpace(r.URL.Query().Get("sku")), strings.TrimSpace(r.URL.Query().Get("location"))
	item, err := engines.ResolveItemBySKU(tenantID, sku)
	if err != nil || item == nil {
		writeAPIErrorDetail(w, r, "INVENT-0422", sku, "no item "+sku)
		return
	}
	code, _ := item.Data["code"].(string)
	if code == "" {
		code = item.ID
	}
	if err := engines.RefuseNonStockItems(tenantID, []string{code}, "consumed from a store"); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	book, err := engines.BookStockAt(tenantID, code, location)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to read the stock")
		return
	}
	policy := engines.ItemPolicyFor(tenantID, item.Data)
	name, _ := item.Data["name"].(string)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"sku": code, "name": name, "book_qty": book,
		"consumption_method": policy.Consumption, "valuated": policy.Valuated(),
	})
}

// handleRegisterGiftCards - POST /api/v1/stored-value/cards (manager).
func handleRegisterGiftCards(w http.ResponseWriter, r *http.Request) {
	if !requireStoreManager(w, r) {
		return
	}
	var req engines.RegisterCardsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid card registration")
		return
	}
	result, err := engines.RegisterStoredValueCards(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-Username"), req)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(result)
}

// handleIssueGiftCard - POST /api/v1/pos/gift-card/issue: activate or add
// value once the customer has paid. Needs the cashier's open session at the
// store, like a sale (Stage 20.7).
func handleIssueGiftCard(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	cashier := r.Header.Get("Resolved-Username")
	var req engines.StoredValueIssueRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid gift card issue")
		return
	}
	if strings.TrimSpace(req.Location) == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Choose the store first")
		return
	}
	if err := engines.ValidatePOSSellableLocation(tenantID, req.Location); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	if !requireStoreAccess(w, r, req.Location) {
		return
	}
	sessionID, err := engines.GetOpenSessionForCashier(tenantID, req.Location, cashier)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to check cashier session")
		return
	}
	if sessionID == "" {
		writeAPIError(w, r, "POSOFF-0238", "")
		return
	}
	receipt, err := engines.IssueStoredValue(tenantID, cashier, req)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(receipt)
}

// handleGiftCardBalance - POST /api/v1/pos/gift-card/balance {card_number}.
// POST, not GET, so the number never sits in a URL or an access log.
func handleGiftCardBalance(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CardNumber string `json:"card_number"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request")
		return
	}
	view, err := engines.StoredValueBalance(r.Header.Get("Resolved-Tenant-ID"), req.CardNumber)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(view)
}

// handleGiftCardAction - POST /api/v1/stored-value/cards/action
// {card_number, action: block|unblock|cancel, reason, location} (manager).
func handleGiftCardAction(w http.ResponseWriter, r *http.Request) {
	if !requireStoreManager(w, r) {
		return
	}
	var req struct {
		CardNumber string `json:"card_number"`
		Action     string `json:"action"`
		Reason     string `json:"reason"`
		Location   string `json:"location"`
		// Stage 130.8: "reveal" names the card by its id (the Gift Card
		// Liability report's Card ID) - its number is what was lost.
		InstrumentID string `json:"instrument_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request")
		return
	}
	tenantID, user := r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-Username")
	var out interface{}
	var err error
	switch strings.ToLower(strings.TrimSpace(req.Action)) {
	case "block":
		out, err = engines.SetStoredValueBlocked(tenantID, user, req.CardNumber, req.Reason, true)
	case "unblock":
		out, err = engines.SetStoredValueBlocked(tenantID, user, req.CardNumber, req.Reason, false)
	case "cancel":
		out, err = engines.CancelStoredValueIssue(tenantID, user, req.CardNumber, req.Reason, req.Location)
	case "reveal":
		out, err = engines.RevealStoredValueCard(tenantID, r.Header.Get("Resolved-Role"), user, req.InstrumentID)
	default:
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "action must be block, unblock, cancel or reveal")
		return
	}
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(out)
}
