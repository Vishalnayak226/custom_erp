package server

// Stage 49.5.6a: offline sales are validated before they post. handleCheckout
// hands a sale the till sends as rung up offline to holdOfflineSale; POS >
// Offline Sales lists them, and Validate replays the held request through
// handleCheckout itself - as the cashier's sale at its store, in the session
// it was rung up in - so stock, GL, payment and bill happen exactly as for any
// sale. See engines/pos_offline_sales.go for the rules.

import (
	"bytes"
	"context"
	"custom_erp/db"
	"custom_erp/engines"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// offlineReplayKey marks the one in-process checkout call Validate makes. A
// context value, so no request a client sends can carry it.
type offlineReplayKey struct{}

type offlineReplay struct{ session string }

func offlineReplayOf(r *http.Request) *offlineReplay {
	v, _ := r.Context().Value(offlineReplayKey{}).(*offlineReplay)
	return v
}

// checkoutSession is the session a sale posts into: the cashier's open one,
// or - for an offline sale being validated - the one it was held under.
func checkoutSession(r *http.Request, tenantID, location, cashier string) (string, error) {
	if rp := offlineReplayOf(r); rp != nil && rp.session != "" {
		return rp.session, nil
	}
	return engines.GetOpenSessionForCashier(tenantID, location, cashier)
}

// holdOfflineSale stores an offline-queued checkout (raw is its decoded
// request, re-marshalled) as a Pending offline sale. It answers 202 "held for
// validation", which the till's queue treats as synced; the same entry
// replayed again answers the same, with no second row.
func holdOfflineSale(w http.ResponseWriter, r *http.Request, raw []byte) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	cashier := r.Header.Get("Resolved-Username")
	var p struct {
		CartNumber           string            `json:"cart_number"`
		IdempotencyKey       string            `json:"idempotency_key"`
		Location             string            `json:"location"`
		SoldAt               string            `json:"sold_at"`
		CustomerID           string            `json:"customer_id"`
		Interstate           bool              `json:"interstate"`
		CouponCodes          []string          `json:"coupon_codes"`
		ManualDiscountPct    float64           `json:"manual_discount_pct"`
		ManualDiscountAmount float64           `json:"manual_discount_amount"`
		GiftCards            []json.RawMessage `json:"gift_cards"`
		Advances             []json.RawMessage `json:"advances"`
		StoreCredit          float64           `json:"store_credit"`
		ExchangeCreditID     string            `json:"exchange_credit_id"`
		Items                []struct {
			Sku       string  `json:"sku"`
			Qty       int     `json:"qty"`
			SalePrice float64 `json:"sale_price"`
			MRP       float64 `json:"mrp"`
			Barcode   string  `json:"barcode"`
			Scanned   string  `json:"scanned"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid checkout payload")
		return
	}
	// The checks a sale gets before anything is stored - the same answers the
	// checkout gives an offline sale, so a held one can be posted at all.
	for _, item := range p.Items {
		if item.Sku == "" || item.Qty <= 0 {
			writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, fmt.Sprintf("Item quantity must be positive (sku=%q, qty=%d)", item.Sku, item.Qty))
			return
		}
		if item.SalePrice < 0 {
			writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, fmt.Sprintf("Item prices cannot be negative (sku=%q)", item.Sku))
			return
		}
	}
	switch {
	case len(p.GiftCards) > 0:
		writeAPIErrorDetail(w, r, "POSOFF-0434", "", "a gift card cannot pay for a sale rung up offline - take another payment mode")
		return
	case len(p.Advances) > 0:
		writeAPIErrorDetail(w, r, "POSOFF-0872", "", "An advance is used online at the till - it cannot be queued offline or sent to the payment terminal.")
		return
	case p.StoreCredit > 0:
		writeAPIErrorDetail(w, r, "POSOFF-1309", "", "Store credit is used online at the till - it cannot be queued offline or sent to the payment terminal.")
		return
	case p.ExchangeCreditID != "":
		writeAPIErrorDetail(w, r, "POSOFF-0612", "", "An exchange is settled online at the till - it cannot be queued offline or sent to the payment terminal.")
		return
	}
	if err := engines.ValidatePOSSellableLocation(tenantID, p.Location); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	if !requireStoreAccess(w, r, p.Location) {
		return
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to resolve tenant schema")
		return
	}
	// A cart number the server already has (the live attempt went through and
	// only its answer was lost) is that sale, answered as the checkout's own
	// cart-row replay does - never held a second time.
	var cartStatus string
	switch errCart := db.DB.QueryRow(fmt.Sprintf(`SELECT status FROM %s.documents WHERE doctype = 'POSCart' AND id = $1`, schema), p.CartNumber).Scan(&cartStatus); {
	case errCart == sql.ErrNoRows || cartStatus == "Failed":
	case errCart != nil:
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to check the cart")
		return
	case cartStatus == "Paid":
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "completed", "cart_number": p.CartNumber})
		return
	case cartStatus == "Pending Approval":
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "pending_approval", "cart_number": p.CartNumber})
		return
	default:
		writeAPIError(w, r, "POSOFF-0241", "")
		return
	}
	// The session it was rung up in - posted into when it is validated.
	sessionID, err := engines.GetOpenSessionForCashier(tenantID, p.Location, cashier)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to check cashier session")
		return
	}
	if sessionID == "" {
		writeAPIError(w, r, "POSOFF-0238", "")
		return
	}

	// What tells a replay of this sale from a different sale under the same
	// key: the sale's content, not the till's own time stamp - and, when the
	// client sent its own key, not the cart number either (that key is the
	// sale's identity, as in handleCheckout's own digest).
	key := strings.TrimSpace(p.IdempotencyKey)
	var digestSource map[string]interface{}
	_ = json.Unmarshal(raw, &digestSource)
	delete(digestSource, "sold_at")
	if key == "" {
		key = p.CartNumber
	} else {
		delete(digestSource, "cart_number")
	}
	digest := engines.CommandDigest(digestSource)
	items := make([]engines.OfflineSaleItem, len(p.Items))
	lines := make([]engines.QuoteLineRequest, len(p.Items))
	for i, it := range p.Items {
		items[i] = engines.OfflineSaleItem{Sku: it.Sku, Qty: it.Qty, Scanned: it.Scanned}
		lines[i] = engines.QuoteLineRequest{Sku: it.Sku, Qty: it.Qty, FallbackUnitPrice: it.SalePrice, MRP: it.MRP, Barcode: it.Barcode}
	}
	// What the screen shows as the sale's total: the server's own price for
	// it now (read only). Validate prices it again as it posts.
	total := 0.0
	if quote, errQ := engines.ResolvePOSQuote(tenantID, engines.QuoteRequest{
		CartNumber: p.CartNumber, Location: p.Location, CustomerID: p.CustomerID, Interstate: p.Interstate,
		CouponCodes: p.CouponCodes, Lines: lines, ManualDiscountPct: p.ManualDiscountPct, ManualDiscountAmount: p.ManualDiscountAmount,
	}); errQ == nil {
		total = quote.Total
	}
	var soldAt *time.Time
	if t, errT := time.Parse(time.RFC3339, strings.TrimSpace(p.SoldAt)); errT == nil {
		soldAt = &t
	}
	sale, created, err := engines.HoldOfflineSale(tenantID, engines.OfflineSaleHold{
		CartNumber: p.CartNumber, IdempotencyKey: userID + ":" + key, Digest: digest, Payload: raw,
		CashierUserID: userID, Cashier: cashier, CashierRole: r.Header.Get("Resolved-Role"), Location: p.Location,
		POSSession: sessionID, StoreDevice: r.Header.Get("Resolved-Store-Device"), SoldAt: soldAt, Total: total, Items: items,
	})
	if errors.Is(err, engines.ErrOfflineSaleKeyReused) {
		writeAPIErrorGeneric(w, r, http.StatusConflict, "This idempotency key was already used for a different sale. Use a new key for a new sale.")
		return
	}
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":            "held_for_validation",
		"cart_number":       sale.CartNumber,
		"offline_sale_id":   sale.ID,
		"validation_status": sale.Status,
		"already_held":      !created,
		"message":           "Held for validation.",
	})
}

// handleListOfflineSales - GET /api/v1/pos/offline-sales?status=Pending: the
// sales at the stores this person works at.
func handleListOfflineSales(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	switch status {
	case "", engines.OfflineSalePending, engines.OfflineSaleValidated, engines.OfflineSaleRejected:
	default:
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "status is Pending, Validated or Rejected")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	access, err := engines.UserStoreAccess(tenantID, r.Header.Get("Resolved-User-ID"))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusServiceUnavailable, "Unable to verify your stores - please retry shortly")
		return
	}
	sales, err := engines.ListOfflineSales(tenantID, status, limit)
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	out := make([]engines.OfflineSale, 0, len(sales))
	for _, s := range sales {
		if access.Allows(s.Location) {
			out = append(out, s)
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"sales": out})
}

// loadOfflineSale reads one sale for a caller who may work at its store.
func loadOfflineSale(w http.ResponseWriter, r *http.Request) *engines.OfflineSale {
	sale, err := engines.GetOfflineSale(r.Header.Get("Resolved-Tenant-ID"), r.PathValue("id"))
	if errors.Is(err, engines.ErrOfflineSaleNotFound) {
		writeAPIErrorGeneric(w, r, http.StatusNotFound, "Offline sale not found")
		return nil
	}
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return nil
	}
	if !requireStoreAccess(w, r, sale.Location) {
		return nil
	}
	return sale
}

// handleGetOfflineSale - GET /api/v1/pos/offline-sales/{id}: the sale with
// each line's item, ERP barcodes, scanned code and match, checked now.
func handleGetOfflineSale(w http.ResponseWriter, r *http.Request) {
	if sale := loadOfflineSale(w, r); sale != nil {
		_ = json.NewEncoder(w).Encode(sale)
	}
}

// handleValidateOfflineSale - POST /api/v1/pos/offline-sales/{id}/validate.
func handleValidateOfflineSale(w http.ResponseWriter, r *http.Request) {
	decideOfflineSale(w, r, true)
}

// handleRejectOfflineSale - POST /api/v1/pos/offline-sales/{id}/reject {reason}.
func handleRejectOfflineSale(w http.ResponseWriter, r *http.Request) {
	decideOfflineSale(w, r, false)
}

// offlineCheckoutRefused carries the checkout's own refusal of a validated
// sale back to the manager, as the checkout wrote it.
type offlineCheckoutRefused struct {
	status int
	body   []byte
}

func (e *offlineCheckoutRefused) Error() string { return "the checkout refused the sale" }

func decideOfflineSale(w http.ResponseWriter, r *http.Request, validate bool) {
	var req struct {
		Reason string `json:"reason"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	sale := loadOfflineSale(w, r)
	if sale == nil {
		return
	}
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	decided, err := engines.DecideOfflineSale(r.Context(), tenantID, engines.OfflineSaleDecision{
		ID: sale.ID, ActorUserID: r.Header.Get("Resolved-User-ID"), Actor: r.Header.Get("Resolved-Username"),
		Validate: validate, Reason: req.Reason,
	}, func(s *engines.OfflineSale) (string, []byte, error) {
		return postOfflineSale(r, s)
	})
	var refused *offlineCheckoutRefused
	switch {
	case errors.As(err, &refused):
		w.WriteHeader(refused.status)
		_, _ = w.Write(refused.body)
		return
	case errors.Is(err, engines.ErrOfflineSaleNotFound):
		writeAPIErrorGeneric(w, r, http.StatusNotFound, "Offline sale not found")
		return
	case err != nil:
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(decided)
}

// postOfflineSale runs the held request through handleCheckout in-process,
// as its cashier at its store. The manager validating it is the confirmation
// of today's price (accept_price_change): the till could not quote offline.
func postOfflineSale(r *http.Request, sale *engines.OfflineSale) (string, []byte, error) {
	var body map[string]interface{}
	if err := json.Unmarshal(sale.Payload, &body); err != nil {
		return "", nil, err
	}
	body["offline_synced"] = true
	body["accept_price_change"] = true
	raw, err := json.Marshal(body)
	if err != nil {
		return "", nil, err
	}
	ctx := context.WithValue(r.Context(), offlineReplayKey{}, &offlineReplay{session: sale.POSSession})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "/api/v1/checkout", bytes.NewReader(raw))
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Resolved-Tenant-ID", r.Header.Get("Resolved-Tenant-ID"))
	req.Header.Set("Resolved-Correlation-ID", r.Header.Get("Resolved-Correlation-ID"))
	req.Header.Set("Resolved-User-ID", sale.CashierUserID)
	req.Header.Set("Resolved-Username", sale.Cashier)
	req.Header.Set("Resolved-Role", sale.CashierRole)
	req.Header.Set("Resolved-Location", sale.Location)
	rec := &swRecorder{header: http.Header{}, status: http.StatusOK}
	handleCheckout(rec, req)
	out := rec.body.Bytes()
	if rec.status != http.StatusOK {
		return "", nil, &offlineCheckoutRefused{status: rec.status, body: out}
	}
	var res struct {
		Status     string `json:"status"`
		CartNumber string `json:"cart_number"`
		BillNumber string `json:"bill_number"`
	}
	_ = json.Unmarshal(out, &res)
	switch res.Status {
	case "completed":
		return firstNonBlank(res.BillNumber, res.CartNumber), out, nil
	case "pending_approval":
		// Over the discount limit: it waits on Approvals and posts from there,
		// as any such sale does. The cart number is the document.
		return res.CartNumber, out, nil
	}
	return "", nil, &offlineCheckoutRefused{status: http.StatusConflict, body: out}
}
