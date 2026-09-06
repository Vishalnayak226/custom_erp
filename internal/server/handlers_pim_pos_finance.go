package server

import (
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"custom_erp/db"
	"custom_erp/engines"
)

// CSV bulk import/PIM import preview, channel credential config, the
// BigCommerce inbound webhook, POS checkout/availability/reservations, and
// Finance/GL: trial balance, accounting periods, the approval/maker-checker
// workflow engine, GST calculation, and the core report catalog.

func handleBulkImport(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	doctype := r.PathValue("doctype")
	// Bug fix (found while verifying Stage 15.2's import preview, which
	// copies this handler's shape): this read "Resolved-Role" (e.g.
	// "HR/Admin") into a variable literally named userID, which then got
	// written as documents.created_by - a column with a hard FK to
	// users(id). A role string is never a valid user id, so every bulk
	// import row insert has always failed its FK constraint. Fixed to the
	// actual user id header, matching every other handler in this file
	// (e.g. handleCapitalizeAsset, handlePIMPublish).
	userID := r.Header.Get("Resolved-User-ID")
	role := r.Header.Get("Resolved-Role")

	if err := r.ParseMultipartForm(5 << 20); err != nil {
		writeAPIError(w, r, "GLOBAL-0007", "")
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "CSV file is mandatory under multipart FormFile 'file'")
		return
	}
	defer file.Close()

	res, err := engines.BulkImportCSV(tenantID, doctype, file, userID, role, false)
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}

	jobID, errJob := engines.RecordImportJob(tenantID, doctype, res, userID)
	if errJob != nil {
		engines.LogSystemError(tenantID, r.Header.Get("Resolved-Correlation-ID"), "IMPORT_JOB_RECORD_FAILED", r.URL.Path, errJob.Error(), "")
	}

	// Round-trips res through JSON (struct -> map) purely to splice in
	// import_job_id afterward - res is this engine's own well-formed
	// struct, not external input, so a marshal/unmarshal failure here would
	// mean an internal bug, not corrupt data. Guarded anyway (24.18) so
	// that case degrades to "no job ID attached" instead of a nil-map panic
	// on the assignment below.
	responseBytes, marshalErr := json.Marshal(res)
	responseMap := map[string]interface{}{}
	if marshalErr == nil {
		if err := json.Unmarshal(responseBytes, &responseMap); err != nil {
			engines.LogSystemError(tenantID, r.Header.Get("Resolved-Correlation-ID"), "ERROR", r.URL.Path, fmt.Sprintf("failed to round-trip import response: %v", err), "")
			responseMap = map[string]interface{}{}
		}
	} else {
		engines.LogSystemError(tenantID, r.Header.Get("Resolved-Correlation-ID"), "ERROR", r.URL.Path, fmt.Sprintf("failed to marshal import response: %v", marshalErr), "")
	}
	if jobID != "" {
		responseMap["import_job_id"] = jobID
	}
	annotateImportResult(r, res, responseMap)
	_ = json.NewEncoder(w).Encode(responseMap)
}

// annotateImportResult (Stage 25.5) attaches DATAIM-0165 ("Excel row
// validation failed", every row rejected) or DATAIM-0187 ("Partial
// upload", some rows rejected) as an annotation on the normal 200 import
// response - not a rejected request, since the existing per-row
// Errors/error-CSV-download flow (RecordImportJob/GetImportJobErrorCSV)
// this predates already IS the correct transport for a partially- or
// fully-failed import; changing the response envelope for either scenario
// would break that flow rather than improve it.
func annotateImportResult(r *http.Request, res *engines.ImportResult, responseMap map[string]interface{}) {
	if res.FailedRows == 0 {
		return
	}
	code := "DATAIM-0187"
	if res.SuccessRows == 0 {
		code = "DATAIM-0165"
	}
	entry := errorCatalog[code]
	logForEntry(r, entry, entry.UserMessage)
	responseMap["code"] = code
	responseMap["message"] = entry.UserMessage
}

// handlePIMImportPreview (Stage 15.2, V2 §6.2/§16 Phase 3): the same CSV
// parse/validate/existence-check logic as handleBulkImport, run with
// dryRun=true - nothing is written, giving the create/update/reject preview
// V2's Import Job screen wants before a user commits a bulk file.
func handlePIMImportPreview(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	doctype := r.PathValue("doctype")
	userID := r.Header.Get("Resolved-User-ID")
	role := r.Header.Get("Resolved-Role")

	if err := r.ParseMultipartForm(5 << 20); err != nil {
		writeAPIError(w, r, "GLOBAL-0007", "")
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "CSV file is mandatory under multipart FormFile 'file'")
		return
	}
	defer file.Close()

	res, err := engines.BulkImportCSV(tenantID, doctype, file, userID, role, true)
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	responseBytes, marshalErr := json.Marshal(res)
	responseMap := map[string]interface{}{}
	if marshalErr == nil {
		_ = json.Unmarshal(responseBytes, &responseMap)
	}
	annotateImportResult(r, res, responseMap)
	_ = json.NewEncoder(w).Encode(responseMap)
}

// handlePIMImportJobErrors serves a completed ImportJob's row-level failures
// as a downloadable CSV, same Content-Disposition pattern as the CSV import
// template endpoint above.
func handlePIMImportJobErrors(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	jobID := r.PathValue("id")

	csvBytes, err := engines.GetImportJobErrorCSV(tenantID, jobID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusNotFound, err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=import_errors_%s.csv", jobID))
	_, _ = w.Write(csvBytes)
}

// handleSaveChannelCredential (Stage 16.1) stores a channel's API
// credential fields (access token, shop domain, etc. - shape varies by
// platform) encrypted at rest via engines.SaveChannelCredential. Write-
// only by design: this handler never reads a credential back, and there
// is no corresponding GET route anywhere in this file.
func handleSaveChannelCredential(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	role := r.Header.Get("Resolved-Role")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	if !engines.IsSuperAdmin(role) {
		writeAPIError(w, r, "GLOBAL-0011", "")
		return
	}
	channelCode := r.PathValue("code")
	var fields map[string]string
	if err := json.NewDecoder(r.Body).Decode(&fields); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "invalid request body")
		return
	}
	if err := engines.SaveChannelCredential(tenantID, channelCode, fields); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "saved", "channel": channelCode})
}

// handleBigCommerceWebhook (Stage 16.3) verifies and acknowledges an
// inbound BigCommerce webhook (product/inventory/order events). The
// channel code in the URL identifies which stored credential's
// webhook_secret field to verify against - BigCommerce webhook payloads
// do not self-identify which of possibly several configured channels
// they belong to. Scope note, stated explicitly: this acknowledges and
// logs a verified webhook rather than driving a full order-import
// pipeline the way the existing Shopify order webhook does - BigCommerce
// order sync-back is not yet built, only inbound signature verification
// (Part A.7 of the Stage 16 plan) plus a place for that logic to grow
// into.
func handleBigCommerceWebhook(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	channelCode := r.PathValue("channelCode")

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "failed to read request body")
		return
	}

	cred, credErr := engines.GetChannelWebhookSecret(tenantID, channelCode)
	if credErr != nil || cred == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnauthorized, "channel has no webhook secret configured")
		return
	}
	sig := r.Header.Get("X-Bc-Webhook-Signature")
	if !engines.VerifyBigCommerceWebhook(body, sig, cred) {
		writeAPIErrorGeneric(w, r, http.StatusUnauthorized, "invalid webhook signature")
		return
	}

	engines.LogAuditEvent(tenantID, "system", "BIGCOMMERCE_WEBHOOK_RECEIVED", "SUCCESS", fmt.Sprintf("channel=%s bytes=%d", channelCode, len(body)))

	// Stage 35.1.2: this handler used to stop at the acknowledgement above, so
	// every BigCommerce order was verified, audited and then dropped. A
	// BigCommerce webhook body carries only {scope, data:{type,id}} - never the
	// order itself - so the order is read back over the API and imported
	// through the same ImportChannelSalesOrder path Shopify and Unicommerce
	// use. Non-order scopes (product/inventory/customer hooks) keep the old
	// acknowledge-only behaviour.
	var hook struct {
		Scope string `json:"scope"`
		Data  struct {
			Type string `json:"type"`
			ID   int64  `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &hook); err != nil || hook.Data.Type != "order" || hook.Data.ID == 0 {
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "acknowledged"})
		return
	}

	orderID, importErr := engines.ImportBigCommerceOrder(tenantID, channelCode, hook.Data.ID)
	if importErr != nil {
		// Deliberately 200, not 4xx/5xx. BigCommerce retries a failed hook and
		// then disables the subscription after repeated failures; a missing
		// credential or an unmapped SKU must not cost the store its webhook.
		// The failure is audited so it is visible in the connector log.
		engines.LogAuditEvent(tenantID, "system", "BIGCOMMERCE_ORDER_IMPORT", "FAILURE",
			fmt.Sprintf("channel=%s scope=%s bc_order=%d: %v", channelCode, hook.Scope, hook.Data.ID, importErr))
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "acknowledged", "import_error": importErr.Error()})
		return
	}
	engines.LogAuditEvent(tenantID, "system", "BIGCOMMERCE_ORDER_IMPORT", "SUCCESS",
		fmt.Sprintf("channel=%s bc_order=%d order=%s", channelCode, hook.Data.ID, orderID))
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "imported", "order_id": orderID})
}

func handleGetImportTemplate(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	doctype := r.PathValue("doctype")

	templateBytes, err := engines.GenerateCSVTemplate(tenantID, doctype)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s_template.csv", doctype))
	_, _ = w.Write(templateBytes)
}

func handleGetAvailability(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	sku := r.URL.Query().Get("sku")
	location := r.URL.Query().Get("location")

	if sku == "" || location == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Query parameters 'sku' and 'location' are required")
		return
	}

	res, err := engines.GetAvailableToSell(tenantID, sku, location)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	_ = json.NewEncoder(w).Encode(res)
}

func handleCreateReservation(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req struct {
		Sku          string `json:"sku"`
		Location     string `json:"location"`
		Qty          int    `json:"qty"`
		ResType      string `json:"res_type"`
		ExpirySecond int    `json:"expiry"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid payload")
		return
	}

	if req.Sku == "" || req.Location == "" || req.Qty <= 0 {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'sku', 'location', and positive 'qty' are required")
		return
	}

	expiry := req.ExpirySecond
	if expiry <= 0 {
		expiry = 300 // default 5 minutes
	}

	resID, err := engines.CreateReservation(tenantID, req.Sku, req.Location, req.Qty, req.ResType, expiry)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":         "reserved",
		"reservation_id": resID,
	})
}

func handleCheckout(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	role := r.Header.Get("Resolved-Role")
	cashier := r.Header.Get("Resolved-Username")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req struct {
		CartNumber string `json:"cart_number"`
		// IdempotencyKey (Stage 47.3.1) is the caller's own key for THIS
		// command execution. It defaults to cart_number below, which
		// reproduces the pre-47.3 guarantee exactly for any client that does
		// not send one - but a client that sends a stable key gets the real
		// one: a retry cannot double-post even if it invents a new cart
		// number, which is precisely what public/app.js used to do.
		IdempotencyKey string  `json:"idempotency_key"`
		Location       string  `json:"location"`
		PaymentMode    string  `json:"payment_mode"`
		CustomerID     string  `json:"customer_id"`
		Interstate     bool    `json:"interstate"`
		DiscountPct    float64 `json:"discount_pct"`
		// RedeemPoints (Stage 30.2.5): loyalty points to pay part of this
		// sale with. Validated below and burned by FinalizePOSCheckout only
		// once the sale actually goes through - never at "Redeem Points"
		// click time, which is how a cashier used to be able to destroy a
		// customer's points on a cart that was then abandoned.
		RedeemPoints int `json:"redeem_points"`
		// CouponCodes (Stage 30.7): coupon-gated offers the cashier keyed in.
		// A code that matches no live offer is reported back rather than
		// silently ignored, so the cashier learns it didn't apply.
		CouponCodes []string `json:"coupon_codes"`
		// OfflineSynced (20.13): set only by the POS screen's own offline
		// queue when replaying a sale that was rung up while disconnected -
		// never set by a normal live checkout. Stamped onto the stored cart
		// (see storedPayload below) so FinalizePOSCheckout can allow the
		// resulting stock to go negative instead of rejecting a sale whose
		// goods already physically left the store.
		OfflineSynced bool `json:"offline_synced"`
		// QuoteVersion (Stage 47.2.4): the version of the quote the cashier's
		// screen is showing. Checkout ALWAYS re-resolves prices server-side;
		// this field only decides what happens when the re-resolved answer
		// differs from what the operator was looking at - reject with the new
		// figures, rather than silently charging either one.
		QuoteVersion string `json:"quote_version"`
		// AuthorizeOnly (Stage 47.3.3) stops after reserving the stock and
		// stamping payment_state=Initiated, so the till can take the card to a
		// terminal without a database transaction held open across that round
		// trip. The sale is completed by POST /api/v1/pos/payment/confirm, or
		// released by /void.
		AuthorizeOnly bool `json:"authorize_only"`
		// AcceptPriceChange is the operator's explicit confirmation of a
		// changed price after such a rejection.
		AcceptPriceChange bool `json:"accept_price_change"`
		// Items carries SELECTION inputs only. sale_price is accepted for one
		// narrowing purpose - an item the tenant has not priced anywhere, in
		// pos.pricing_mode 'assisted' (see engines.ResolvePOSQuote) - and is
		// ignored outright for every item that has a server price. cost_price
		// is gone: it is no longer read, stored or returned anywhere (47.2.2).
		Items []struct {
			Sku       string  `json:"sku"`
			Qty       int     `json:"qty"`
			SalePrice float64 `json:"sale_price"`
		} `json:"items"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid checkout payload")
		return
	}

	if req.CartNumber == "" || req.Location == "" || len(req.Items) == 0 {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'cart_number', 'location', and 'items' are required")
		return
	}

	// --- Stage 47.3.1: claim the COMMAND before any mutation --------------
	//
	// This runs before every validation below, not after, because the point of
	// a command claim is that a duplicate never re-executes the command - and
	// "re-executes" includes re-running validations that have side effects
	// (the loyalty balance read, the offer evaluation, the session lookup).
	// The claim is released again for a request that is rejected outright, so
	// a genuine mistake can be corrected and resubmitted under the same key.
	//
	// The key namespaces to the caller's own identity as well as the tenant:
	// two cashiers cannot collide on a client-generated key, and one cashier's
	// key cannot be used to replay another's sale back at them.
	idempotencyKey := strings.TrimSpace(req.IdempotencyKey)
	explicitKey := idempotencyKey != ""
	if !explicitKey {
		idempotencyKey = req.CartNumber
	}
	idempotencyKey = userID + ":" + idempotencyKey
	// The digest is what tells a retry from a key collision. When the client
	// supplied its OWN key, that key is the sale's identity and the cart
	// number is just a label on it - so the cart number is excluded from the
	// digest, and a retry that mints a fresh cart number (which is exactly
	// what the POS screen used to do on every attempt, and the reason A-03's
	// cart-number guard did not hold) is correctly recognised as the same
	// command. When no key was supplied, the cart number IS the key, so it
	// stays in the digest and nothing changes for an older client.
	digestSource := req
	if explicitKey {
		digestSource.CartNumber = ""
	}
	requestDigest := engines.CommandDigest(digestSource)
	claim, claimErr := engines.ClaimCommand(tenantID, "pos.checkout", idempotencyKey, requestDigest, r.Header.Get("Resolved-Correlation-ID"))
	if claimErr != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to claim this checkout")
		return
	}
	switch claim.Outcome {
	case engines.ClaimReplay:
		// INT-0222 "This request was already processed. Duplicate action was
		// ignored." - the catalog's own scenario, and a 200, because from the
		// caller's point of view the sale succeeded. The body is the original
		// response verbatim, so a till that lost the first reply prints the
		// same receipt rather than a different one.
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(claim.Response)
		return
	case engines.ClaimInProgress:
		writeAPIErrorDetail(w, r, "POSOFF-0241", "",
			"This sale is still being processed. Wait a moment and check the sale before retrying - do not ring it up again.")
		return
	case engines.ClaimPayloadMismatch:
		writeAPIErrorGeneric(w, r, http.StatusConflict,
			"This idempotency key was already used for a different sale. Use a new key for a new sale.")
		return
	}
	// From here on, EVERY exit has to settle the claim or the key stays locked
	// until its lease expires. There are a dozen early returns below, so this
	// is a deferred default rather than a call before each one - the same
	// reasoning as attaching a rule at a shared choke point instead of sweeping
	// call sites: a validation added later is covered without anyone
	// remembering to.
	//
	// Default = release, because a request rejected by validation mutated
	// nothing and the operator should be able to fix the input and resubmit
	// under the same key immediately. The two paths that settle it themselves
	// are the sale completing (CompleteCommandTx, inside the sale's own
	// transaction) and the sale failing mid-post (FailCommand, which keeps the
	// attempt visible instead of erasing it).
	claimSettled := false
	defer func() {
		if !claimSettled {
			engines.ReleaseCommand(tenantID, claim.Key)
		}
	}()

	// Reject non-positive qty/prices before any side effect runs. Below this line,
	// item.Qty is negated to decrement stock (see loop below) - an already-negative
	// qty would flip to positive and silently ADD stock instead of being rejected,
	// and would do so via PostInventoryLedger's own committed transaction, before
	// the later GL-posting step even runs its own (unrelated) sign validation.
	for _, item := range req.Items {
		if item.Sku == "" || item.Qty <= 0 {
			writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, fmt.Sprintf("Item quantity must be positive (sku=%q, qty=%d)", item.Sku, item.Qty))
			return
		}
		if item.SalePrice < 0 {
			writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, fmt.Sprintf("Item prices cannot be negative (sku=%q)", item.Sku))
			return
		}
	}

	// Stage 47.2 - the single authoritative pricing step. ResolvePOSQuote
	// prices every line from tenant master data (approved price list, item
	// master, or an approved POSPriceOverride), computes GST from those
	// resolved prices and evaluates offers against them. It replaces three
	// separate computations that used to run off client-submitted prices here:
	// the GST enforcement block (Stage 17.5), the offer evaluation (Stage
	// 30.7), and - crucially - the discount figure the approval gate keyed on.
	//
	// GST enforcement itself is unchanged and still happens before any side
	// effect: ComputeGSTForLines inside the quote rejects a line whose Item is
	// missing hsn_code/gst_rate exactly as it did when called from here.
	quoteLines := make([]engines.QuoteLineRequest, len(req.Items))
	for i, item := range req.Items {
		quoteLines[i] = engines.QuoteLineRequest{Sku: item.Sku, Qty: item.Qty, FallbackUnitPrice: item.SalePrice}
	}
	quote, quoteErr := engines.ResolvePOSQuote(tenantID, engines.QuoteRequest{
		CartNumber:  req.CartNumber,
		Location:    req.Location,
		CustomerID:  req.CustomerID,
		Interstate:  req.Interstate,
		CouponCodes: req.CouponCodes,
		Lines:       quoteLines,
	})
	if quoteErr != nil {
		writeEngineError(w, r, quoteErr, http.StatusUnprocessableEntity)
		return
	}
	gstBreakdown := quote.GSTBreakdown

	// 47.2.4: a stale quote is never silently used and never silently
	// repriced. The operator is shown what changed and confirms it.
	if engines.QuoteIsStale(req.QuoteVersion, quote) && !req.AcceptPriceChange {
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":            "price_changed",
			"cart_number":       req.CartNumber,
			"message":           "Prices for this cart changed since it was quoted. Review the new total and confirm to continue.",
			"submitted_version": req.QuoteVersion,
			// Safe to return whole: a QuoteResult carries no cost or margin
			// field at all, by construction - see the note at the top of
			// engines/pos_quote.go on why cost never travels on a quote.
			"quote": quote,
		})
		return
	}

	// Loyalty redemption pre-checks (Stage 30.2.5). The authoritative balance
	// check is RedeemLoyaltyPoints' own, inside FinalizePOSCheckout - this one
	// exists so the ordinary "not enough points" case is rejected here, before
	// the cart is claimed and before any side effect runs, instead of failing
	// half-way through a sale.
	loyaltyDiscount := 0
	if req.RedeemPoints > 0 {
		if req.CustomerID == "" {
			writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "A customer is required to redeem loyalty points")
			return
		}
		balance, balErr := engines.GetLoyaltyBalance(tenantID, req.CustomerID)
		if balErr != nil {
			writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to check the loyalty balance")
			return
		}
		if req.RedeemPoints > balance {
			writeAPIErrorDetail(w, r, "CUSTOM-0134", "", fmt.Sprintf("%s has %d loyalty point(s); this sale is trying to redeem %d.", req.CustomerID, balance, req.RedeemPoints))
			return
		}
		loyaltyDiscount = engines.LoyaltyRedemptionValue(tenantID, req.RedeemPoints)
	}

	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to resolve tenant schema")
		return
	}

	// Stage 20.7: an open cashier session (for this location, opened by this
	// cashier) is a precondition for every sale - opened/closed via
	// POST /api/v1/pos/session/open and /close, never spoofable through this
	// endpoint since the session lookup below is keyed off the caller's own
	// resolved identity, not anything in the request body.
	sessionID, sessErr := engines.GetOpenSessionForCashier(tenantID, req.Location, cashier)
	if sessErr != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to check cashier session")
		return
	}
	if sessionID == "" {
		writeAPIError(w, r, "POSOFF-0238", "")
		return
	}

	// Stage 20.10: a discount above a configured percentage routes through
	// the existing maker-checker approval engine (engines/approval.go, same
	// one PurchaseOrder/VendorInvoice already use) instead of completing the
	// sale immediately. requiredRole == "" means either no discount or no
	// approval_rules slab matches it - the normal synchronous path below.
	//
	// Stage 47.2.3 (audit A-02) is what the gate keys on now. It used to be
	// req.DiscountPct alone - a bare client-reported number with no enforced
	// relationship to any price - so the identical discounted sale could be
	// declared honestly (gated) or hidden in a low unit price with
	// discount_pct=0 (not gated). gateDiscountPct is the larger of what the
	// client declared and what the SERVER measured (quote.ManualDiscountPct =
	// how far below master price this cart is actually being sold), so the two
	// submissions now produce the identical requirement. The declared figure is
	// still honoured rather than discarded: a cashier who says "20% off" on a
	// cart the server cannot price should still be taken at their word.
	measuredDiscountPct := req.DiscountPct
	if quote.ManualDiscountPct > measuredDiscountPct {
		measuredDiscountPct = quote.ManualDiscountPct
	}
	gateDiscountPct := measuredDiscountPct
	var requiredRole string
	if gateDiscountPct > 0 {
		requiredRole, err = engines.RequiredApproverRoleForAmount(tenantID, "POSCart", gateDiscountPct)
		if err != nil {
			writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to evaluate discount approval rules")
			return
		}
	}
	// The residual A-02 case: a line nothing on the server prices, in assisted
	// mode. There is no reference to measure a discount against, so the server
	// cannot judge the price at all - and "cannot judge" must not silently mean
	// "allow". A tenant that has configured ANY POSCart approval slab has said
	// it wants prices reviewed, so an unverifiable one goes to the lowest such
	// slab's approver. A tenant with no slab configured is untouched, which is
	// exactly its behaviour today. Strict mode never reaches this - an unpriced
	// line is rejected outright in ResolvePOSQuote.
	if requiredRole == "" && quote.HasUnverifiedPrice {
		var slabAmount float64
		requiredRole, slabAmount, err = engines.LowestApproverRoleForDoctype(tenantID, "POSCart")
		if err != nil {
			writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to evaluate discount approval rules")
			return
		}
		// Record the slab's own amount, not the (meaningless) zero we measured:
		// SubmitForApproval re-derives the approver from the stored document, so
		// a stored amount below the slab we just chose would be rejected as
		// "no approval rule configured" rather than reaching that approver.
		if requiredRole != "" && slabAmount > gateDiscountPct {
			gateDiscountPct = slabAmount
		}
	}

	// Idempotency guard: atomically claim this cart_number before any side effect
	// (inventory decrement, GL posting) runs. Without this, a duplicate submission
	// - a network retry, a double-click, or two requests racing - would each pass
	// through independently and double-deduct stock / double-post GL, while the
	// final document row (a plain upsert) silently overwrites to look like only
	// one sale happened. Only the request whose INSERT/claim actually applies
	// proceeds; a duplicate of an already-Paid cart gets the original result
	// replayed back, and a duplicate that arrives while the first is still being
	// processed is told to wait rather than reprocessing. A discount-gated cart
	// claims as 'Draft' instead of 'Processing' - SubmitForApproval below requires
	// that starting status - and finalization (inventory/GL) waits for approval.
	// Store the computed GST breakdown alongside the cart payload (Stage
	// 17.5's "auto-compute and store" half) - merged via a generic map
	// rather than a new struct field, since req.Items/etc. above stay the
	// minimal client-facing request shape.
	storedPayload := map[string]interface{}{}
	if rawReq, errReq := json.Marshal(req); errReq == nil {
		if err := json.Unmarshal(rawReq, &storedPayload); err != nil {
			// 24.18: storedPayload stays the pre-initialized empty map
			// (never nil, so no panic risk on the assignments below), but a
			// failure here would silently drop the whole cart payload
			// (items, location, etc.) from what gets stored - worth logging
			// even though req is this handler's own already-validated
			// struct, not external input.
			engines.LogSystemError(tenantID, r.Header.Get("Resolved-Correlation-ID"), "ERROR", r.URL.Path, fmt.Sprintf("failed to round-trip checkout payload: %v", err), "")
		}
	}
	// Stage 47.2: overwrite the client's items with the SERVER's resolved
	// lines before anything is stored. This is the seam that makes every
	// downstream consumer authoritative for free, with no call-site change:
	// FinalizePOSCheckout reads the stored cart (not this request) for the
	// prices it decrements stock, posts revenue and computes loyalty from; the
	// receipt renders from it; the approval path re-finalizes from it; and a
	// return resolves its eligible price from it. The client's own sale_price
	// survives only where the quote itself fell back to it - an item the
	// tenant has not priced, in assisted mode - and is labelled as such by
	// price_source on the line.
	resolvedItems := make([]map[string]interface{}, 0, len(quote.Lines))
	for _, l := range quote.Lines {
		item := map[string]interface{}{
			"sku": l.Sku, "qty": l.Qty,
			"sale_price":      l.UnitPrice,
			"reference_price": l.ReferencePrice,
			"price_source":    l.PriceSource,
		}
		if l.OverrideID != "" {
			item["override_id"] = l.OverrideID
		}
		if l.PriceListCode != "" {
			item["price_list_code"] = l.PriceListCode
		}
		resolvedItems = append(resolvedItems, item)
	}
	storedPayload["items"] = resolvedItems
	storedPayload["gst_breakdown"] = gstBreakdown
	storedPayload["pos_session"] = sessionID
	storedPayload["offline_synced"] = req.OfflineSynced
	// Stage 30.7: persist which offers were applied and what each took off, so
	// the receipt, the audit trail and any later dispute can all reconstruct
	// how this bill's price was reached - the same reason gst_breakdown is
	// stored rather than recomputed on demand. Stage 47.2 adds the quote
	// identity and the server-measured discount next to them, for the same
	// reason: they are what the approval decision was actually made on.
	storedPayload["applied_offers"] = quote.AppliedOffers
	storedPayload["offer_discount"] = quote.OfferDiscount
	storedPayload["quote_version"] = quote.Version
	storedPayload["quote_id"] = quote.QuoteID
	storedPayload["reference_subtotal"] = quote.ReferenceSubtotal
	storedPayload["manual_discount_pct"] = quote.ManualDiscountPct
	storedPayload["has_unverified_price"] = quote.HasUnverifiedPrice
	if requiredRole != "" {
		// Percentage, not rupees - see extractAmount's comment in engines/approval.go.
		// 47.2.3: the server-measured figure, not req.DiscountPct - so the
		// approver sees, and the approval log records, the discount that was
		// really being granted rather than the one the till claimed.
		storedPayload["discount_amount"] = gateDiscountPct
	}
	payloadBytes, _ := json.Marshal(storedPayload)

	claimStatus := "Processing"
	if requiredRole != "" {
		claimStatus = "Draft"
	}
	claimant := userID
	if claimant == "" {
		claimant = "system"
	}
	claimQuery := fmt.Sprintf(`
		INSERT INTO %s.documents (id, doctype, data, status, created_by)
		VALUES ($1, 'POSCart', $2, $3, $4)
		ON CONFLICT (id) DO UPDATE SET
			data = EXCLUDED.data, status = EXCLUDED.status, updated_at = CURRENT_TIMESTAMP
		WHERE %s.documents.status = 'Failed'
		RETURNING id`, schema, schema)
	var claimedID string
	// The cart-row claim (Stage 20.10) stays alongside the command claim above,
	// not replaced by it: they guard different things. This one keeps ONE cart
	// number from being processed twice - which still matters, because a cart
	// number is also the receipt number - while the command claim is what makes
	// a retry safe regardless of what cart number it carries.
	cartClaimErr := db.DB.QueryRow(claimQuery, req.CartNumber, payloadBytes, claimStatus, claimant).Scan(&claimedID)
	if cartClaimErr == sql.ErrNoRows {
		var existingStatus, existingData string
		lookupErr := db.DB.QueryRow(fmt.Sprintf(
			`SELECT status, data FROM %s.documents WHERE doctype = 'POSCart' AND id = $1`, schema),
			req.CartNumber).Scan(&existingStatus, &existingData)
		if lookupErr == nil && existingStatus == "Paid" {
			// Stage 47.2.2: the replay is reconstructed from the SERVER's own
			// stored resolved prices, and no longer reports a cost total at
			// all - cost_price is not stored on a cart any more, and a replay
			// must never become the one path that leaks a figure the live
			// response withholds.
			var existing struct {
				Items []struct {
					Qty       int     `json:"qty"`
					SalePrice float64 `json:"sale_price"`
				} `json:"items"`
			}
			replaySale := 0
			if json.Unmarshal([]byte(existingData), &existing) == nil {
				for _, it := range existing.Items {
					replaySale += int(it.SalePrice) * it.Qty
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status":      "completed",
				"cart_number": req.CartNumber,
				"sale_total":  replaySale,
			})
			return
		}
		if lookupErr == nil && existingStatus == "Pending Approval" {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status":      "pending_approval",
				"cart_number": req.CartNumber,
			})
			return
		}
		if req.OfflineSynced {
			// POSOFF-0241 (Stage 25.7): "Offline invoice sync conflict" -
			// an offline-queued cart replaying against a cart_number the
			// server already has in a state that isn't a clean Paid/
			// Pending-Approval replay (still Processing, or Failed from a
			// prior partial attempt) is exactly this scenario; a live
			// (non-offline) duplicate submission hitting this same branch
			// is a different, unrelated race, so it keeps the existing
			// generic 409 below.
			writeAPIError(w, r, "POSOFF-0241", "")
			return
		}
		writeAPIErrorGeneric(w, r, http.StatusConflict, "This cart is already being processed or was already completed")
		return
	} else if cartClaimErr != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to claim checkout")
		return
	}

	if requiredRole != "" {
		if err := engines.SubmitForApproval(tenantID, "POSCart", req.CartNumber, claimant, role); err != nil {
			_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET status = 'Failed', updated_at = CURRENT_TIMESTAMP WHERE doctype = 'POSCart' AND id = $1`, schema), req.CartNumber)
			writeEngineError(w, r, err, http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":        "pending_approval",
			"cart_number":   req.CartNumber,
			"required_role": requiredRole,
			// The MEASURED discount, not the slab amount gateDiscountPct may
			// have been raised to for the unverified-price case - a cashier
			// told "Discount of 10.0% requires approval" on a cart with no
			// discount on it would reasonably think the till was broken.
			"message": posApprovalMessage(measuredDiscountPct, requiredRole, quote.HasUnverifiedPrice),
			"quote_version": quote.Version,
		})
		// A cart waiting on approval has not posted anything, but it HAS
		// consumed its cart number and raised an approval request - a retry
		// under the same key must be told the same thing, not raise a second
		// request. So the claim is failed rather than released: ClaimCommand
		// turns a Failed claim back into a fresh one on retry, and the cart-row
		// claim above then reports "pending_approval" for it.
		engines.FailCommand(tenantID, claim.Key, "sale routed to approval; not posted")
		claimSettled = true
		return
	}

	// Stage 47.3.1/47.3.2: one call, one transaction, and the idempotency
	// record completes inside it. What a duplicate replays is exactly this
	// response body, which is why it is built BEFORE the call and handed in.
	response := map[string]interface{}{
		"status":      "completed",
		"cart_number": req.CartNumber,
		// gst_breakdown/applied_offers are the quote's, which is what was
		// posted - see the stored cart's own copies.
		"gst_breakdown": gstBreakdown,
		// Stage 30.2.5: what the customer actually pays, after any loyalty
		// points they spent on this sale - so the receipt and the cash drawer
		// agree without the cashier doing the subtraction in their head.
		"loyalty_points_redeemed": req.RedeemPoints,
		"loyalty_discount":        loyaltyDiscount,
		// Stage 30.7: offers applied to this sale, recomputed server-side.
		// unmatched_coupon_codes lets the POS tell the cashier a code didn't
		// apply instead of silently dropping it.
		"applied_offers":         quote.AppliedOffers,
		"offer_discount":         quote.OfferDiscount,
		"unmatched_coupon_codes": quote.UnmatchedCodes,
		// Stage 47.2: the identity of the prices this sale actually used, so
		// the receipt, a dispute and the audit trail all name the same quote.
		"quote_version": quote.Version,
		"quote_id":      quote.QuoteID,
	}

	// Stage 47.3.3: stop before posting when the caller is going to a payment
	// terminal. The stock is held, the cart is Initiated, and no database
	// transaction is open while the card is tapped.
	if req.AuthorizeOnly {
		auth, authErr := engines.AuthorizePOSSale(tenantID, req.CartNumber)
		if authErr != nil {
			engines.FailCommand(tenantID, claim.Key, authErr.Error())
			claimSettled = true
			var shortage *engines.InsufficientStockError
			if errors.As(authErr, &shortage) {
				writeAPIErrorDetail(w, r, "INVENT-0101", "", shortage.Error())
				return
			}
			writeEngineError(w, r, authErr, http.StatusInternalServerError)
			return
		}
		// The claim is failed rather than completed: the sale has NOT happened
		// yet, so a duplicate must not be replayed a "completed" response. A
		// retry re-acquires the key (ClaimCommand revives a Failed claim) and
		// the cart-row claim then reports the authorization already in flight.
		engines.FailCommand(tenantID, claim.Key, "authorized, awaiting payment confirmation")
		claimSettled = true
		response["status"] = "authorized"
		response["payment_state"] = auth.PaymentState
		response["amount_due"] = auth.AmountDue - float64(loyaltyDiscount) - quote.OfferDiscount
		response["sale_total"] = auth.AmountDue
		_ = json.NewEncoder(w).Encode(response)
		return
	}

	outcome, finalizeErr := engines.FinalizePOSCheckoutCommitted(
		tenantID, req.CartNumber, r.Header.Get("Resolved-Correlation-ID"), claim.Key, response)
	if finalizeErr != nil {
		// The whole sale rolled back - nothing was posted. Recording the
		// attempt as Failed (rather than releasing the key) is what lets the
		// operator retry it AND lets a supervisor see that a sale was
		// attempted and did not happen; ClaimCommand turns a Failed claim back
		// into a live one on the retry.
		engines.FailCommand(tenantID, claim.Key, finalizeErr.Error())
		claimSettled = true
		// 47.3.4: a business shortage and a retryable database conflict are
		// different answers to the cashier. "Out of stock" must never be shown
		// for a lock conflict, and "try again" must never be shown for stock
		// that genuinely is not there.
		var shortage *engines.InsufficientStockError
		if errors.As(finalizeErr, &shortage) {
			writeAPIErrorDetail(w, r, "INVENT-0101", "", shortage.Error())
			return
		}
		if engines.IsRetryableDBConflict(finalizeErr) {
			writeAPIErrorGeneric(w, r, http.StatusConflict,
				"Another till was completing a sale for the same item. Nothing was charged - please try again.")
			return
		}
		writeEngineError(w, r, finalizeErr, http.StatusInternalServerError)
		return
	}
	// The sale committed, and CompleteCommandTx committed with it.
	claimSettled = true

	// FinalizePOSCheckout caps the redemption at the sale value and returns the
	// unusable remainder to the customer's balance, so mirror that here rather
	// than reporting a negative amount due.
	if float64(outcome.LoyaltyDiscount) > outcome.SaleTotal {
		outcome.LoyaltyDiscount = int(outcome.SaleTotal)
	}
	response["sale_total"] = outcome.SaleTotal
	response["loyalty_discount"] = outcome.LoyaltyDiscount
	response["amount_due"] = outcome.SaleTotal - float64(outcome.LoyaltyDiscount) - quote.OfferDiscount
	// 47.2.2's acceptance line, literally: "Cashier never receives
	// margin/cost fields." cost_total was returned to every caller before this
	// stage, which handed the till the exact figure Stage 16.7 already hid on
	// the Item form. The rule itself is not re-decided here - RoleMaySeeCost
	// reads the same cost/margin policy engines/sensitive_fields.go applies to
	// every stored cost field.
	//
	// Note it is added AFTER the response was stored for replay, so a replayed
	// duplicate cannot leak a cost total to a role the live call withheld it
	// from.
	if engines.RoleMaySeeCost(role) {
		response["cost_total"] = outcome.CostTotal
	}
	_ = json.NewEncoder(w).Encode(response)
}

// posApprovalMessage explains WHY a sale is waiting, in the operator's terms.
// The unverified-price case is a genuinely different reason from an ordinary
// over-threshold discount, and saying "Discount of 0.0% requires approval" -
// which is what the old single-sentence message would print for it - is worse
// than useless at a till.
func posApprovalMessage(discountPct float64, requiredRole string, unverifiedPrice bool) string {
	if unverifiedPrice && discountPct <= 0 {
		return fmt.Sprintf("This sale includes an item with no price on record, so its price cannot be verified. %s approval is required before it completes.", requiredRole)
	}
	if unverifiedPrice {
		return fmt.Sprintf("Discount of %.1f%%, and an item with no price on record, require %s approval before this sale completes.", discountPct, requiredRole)
	}
	return fmt.Sprintf("Discount of %.1f%% requires %s approval before this sale completes.", discountPct, requiredRole)
}

// handlePOSQuote (Stage 47.2.1) prices a cart server-side and returns the
// quote the till renders. It asserts nothing and changes nothing: no document
// is written, no stock moves, no approval is raised. Its only job is to give
// the cashier the SAME prices checkout will use, plus the quote version that
// lets checkout detect that they have since moved (47.2.4).
//
// This is what replaces "the cashier types a price": the POS screen calls it
// on every cart change and renders what comes back read-only.
func handlePOSQuote(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req engines.QuoteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid quote payload")
		return
	}
	if req.Location == "" || len(req.Lines) == 0 {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'location' and 'items' are required")
		return
	}
	quote, err := engines.ResolvePOSQuote(tenantID, req)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(quote)
}

// handlePOSPriceOverride (Stage 47.2.3) is the separate, capability-gated
// command that replaces typing a lower number into the price box. Route
// capability "pos.price_override" is what keeps it away from the till; the
// approval_rules slab for doctype 'POSPriceOverride' is what keeps a
// supervisor from granting more than the tenant allows them to.
func handlePOSPriceOverride(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	role := r.Header.Get("Resolved-Role")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req engines.PriceOverrideRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid price-override payload")
		return
	}
	result, err := engines.RecordPriceOverride(tenantID, userID, role, req)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	if result.Status == "Pending Approval" {
		// SALESP-0123 "Discount exceeds your allowed limit. Approval is
		// required." - the catalog scenario this is, exactly.
		writeAPIErrorDetail(w, r, "SALESP-0123", "", fmt.Sprintf(
			"A %.1f%% reduction (₹%.2f down to ₹%.2f) is above what %s may grant; it has been sent to %s for approval as %s.",
			result.DiscountPct, result.ReferencePrice, result.OverridePrice, role, result.RequiredRole, result.OverrideID))
		return
	}
	_ = json.NewEncoder(w).Encode(result)
}

// handlePOSPaymentConfirm and handlePOSPaymentVoid (Stage 47.3.3) are the two
// ends of the payment-provider round trip. The authorization itself is raised
// by handleCheckout when the request asks for it (authorize_only), so there is
// no third endpoint: a cart is authorized by the same call that would
// otherwise have completed it, which keeps one code path for pricing,
// approval, session and idempotency instead of two that must be kept in step.
func handlePOSPaymentConfirm(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	role := r.Header.Get("Resolved-Role")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req struct {
		CartNumber        string `json:"cart_number"`
		ProviderReference string `json:"payment_reference"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.CartNumber) == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'cart_number' is required")
		return
	}
	outcome, err := engines.ConfirmPOSSale(tenantID, req.CartNumber, strings.TrimSpace(req.ProviderReference), r.Header.Get("Resolved-Correlation-ID"))
	if err != nil {
		var shortage *engines.InsufficientStockError
		if errors.As(err, &shortage) {
			writeAPIErrorDetail(w, r, "INVENT-0101", "", shortage.Error())
			return
		}
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	response := map[string]interface{}{
		"status":        "completed",
		"cart_number":   req.CartNumber,
		"payment_state": engines.PaymentStatePosted,
		"sale_total":    outcome.SaleTotal,
	}
	if engines.RoleMaySeeCost(role) {
		response["cost_total"] = outcome.CostTotal
	}
	_ = json.NewEncoder(w).Encode(response)
}

func handlePOSPaymentVoid(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req struct {
		CartNumber string `json:"cart_number"`
		Reason     string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.CartNumber) == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'cart_number' is required")
		return
	}
	if err := engines.VoidPOSSale(tenantID, req.CartNumber, strings.TrimSpace(req.Reason)); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "voided", "cart_number": req.CartNumber, "payment_state": engines.PaymentStateVoided,
	})
}

// handlePOSSessionOpen opens a cashier session (Stage 20.7). Cashier/user
// identity always comes from the caller's own resolved headers, never the
// request body - see migrations_stage20a_pos_maturity.sql's comment on why
// POSSession grants no generic create permission to any role.
func handlePOSSessionOpen(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	cashier := r.Header.Get("Resolved-Username")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req struct {
		POSProfile  string  `json:"pos_profile"`
		Location    string  `json:"location"`
		OpeningCash float64 `json:"opening_cash"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Location == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'location' is required")
		return
	}

	id, err := engines.OpenPOSSession(tenantID, req.POSProfile, req.Location, cashier, userID, req.OpeningCash)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	engines.LogAuditEvent(tenantID, cashier, "POS_SESSION", "OPENED", fmt.Sprintf("Session %s opened at %s", id, req.Location))
	_ = json.NewEncoder(w).Encode(map[string]string{"session_id": id, "status": "Open"})
}

// handlePOSSessionClose closes the caller's own open session, computing the
// counted-vs-expected cash variance server-side (Stage 20.8).
func handlePOSSessionClose(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	cashier := r.Header.Get("Resolved-Username")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req struct {
		SessionID      string  `json:"session_id"`
		CountedCash    float64 `json:"counted_cash"`
		VarianceReason string  `json:"variance_reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.SessionID == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'session_id' is required")
		return
	}

	expected, variance, offlineGapCartNumbers, err := engines.ClosePOSSession(tenantID, req.SessionID, cashier, req.CountedCash, req.VarianceReason)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	engines.LogAuditEvent(tenantID, cashier, "POS_SESSION", "CLOSED", fmt.Sprintf("Session %s closed, variance %.2f", req.SessionID, variance))
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":            "Closed",
		"session_id":        req.SessionID,
		"expected_cash":     expected,
		"counted_cash":      req.CountedCash,
		"variance":          variance,
		"offline_queue_gap": offlineGapCartNumbers, // 24.36: non-empty if carts were heartbeated but never synced - see POSOfflineQueueGap
	})
}

// handlePOSOfflineHeartbeat (24.36) records the calling cashier's currently-
// queued offline cart_numbers for their own open session - a best-effort
// beacon (see public/app.js's sendOfflineQueueHeartbeat) so a gap between
// what was queued and what actually synced leaves a server-side trace
// (checked at close time - see ClosePOSSession/detectOfflineQueueGap)
// instead of vanishing the moment browser storage is cleared. Scoped to the
// caller's own currently-open session, same as checkout/session-close, so
// one cashier can't plant a heartbeat against another's session.
func handlePOSOfflineHeartbeat(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	cashier := r.Header.Get("Resolved-Username")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req struct {
		SessionID   string   `json:"session_id"`
		Location    string   `json:"location"`
		CartNumbers []string `json:"cart_numbers"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.SessionID == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'session_id' is required")
		return
	}

	// Only ever record against the caller's own genuinely-open session -
	// never trust session_id alone to prove ownership.
	openSessionID, err := engines.GetOpenSessionForCashier(tenantID, req.Location, cashier)
	if err != nil || openSessionID == "" || openSessionID != req.SessionID {
		writeAPIErrorGeneric(w, r, http.StatusForbidden, "No matching open session for this cashier/location")
		return
	}

	if err := engines.RecordOfflineHeartbeat(tenantID, req.SessionID, cashier, req.Location, req.CartNumbers); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to record heartbeat")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handlePOSSessionCurrent tells the POS screen whether the caller already
// has an open session for a location, so it can show Open/Close Session UI
// instead of surfacing handleCheckout's 400 only after the cashier tries to sell.
func handlePOSSessionCurrent(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	cashier := r.Header.Get("Resolved-Username")
	if r.Method != http.MethodGet {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	location := r.URL.Query().Get("location")
	if location == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Query parameter 'location' is required")
		return
	}

	id, err := engines.GetOpenSessionForCashier(tenantID, location, cashier)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to look up session")
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"session_id": id, "open": id != ""})
}

func handleTrialBalance(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// as_of is mandatory (Stage 29.7.4) - a trial balance is an as-at-a-date
	// statement, and the unbounded version had to aggregate the whole ledger.
	// Reported through writeEngineError so the caller gets the engine's own
	// message naming the missing parameter rather than a bare 400.
	asOf := r.URL.Query().Get("as_of")

	// Stage 37.5.1: optional dimension narrowing, additive query params - an
	// existing caller that never sends any of these gets the identical
	// whole-tenant trial balance it always has.
	filter := engines.FinancialReportFilter{
		CostCenter: r.URL.Query().Get("cost_center"),
		Department: r.URL.Query().Get("department"),
		Entity:     r.URL.Query().Get("entity"),
	}
	res, err := engines.GetTrialBalance(tenantID, asOf, filter)
	if err != nil {
		// writeEngineError already routes a coded *ValidationError through the
		// catalog and everything else to the fallback status.
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}

	_ = json.NewEncoder(w).Encode(res)
}

func handleAccountingPeriods(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	role := r.Header.Get("Resolved-Role")

	switch r.Method {
	case http.MethodGet:
		periods, err := engines.ListAccountingPeriods(tenantID)
		if err != nil {
			writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		_ = json.NewEncoder(w).Encode(periods)

	case http.MethodPost:
		if !engines.IsSuperAdmin(role) {
			writeAPIError(w, r, "GLOBAL-0011", "")
			return
		}
		var req struct {
			PeriodName string `json:"period_name"`
			StartDate  string `json:"start_date"`
			EndDate    string `json:"end_date"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.PeriodName == "" || req.StartDate == "" || req.EndDate == "" {
			writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "period_name, start_date, and end_date are required")
			return
		}
		id, err := engines.CreateAccountingPeriod(tenantID, req.PeriodName, req.StartDate, req.EndDate, userID)
		if err != nil {
			if verr, ok := err.(*engines.ValidationError); ok && verr.Code != "" {
				writeAPIErrorDetail(w, r, verr.Code, verr.SubFor, verr.Message)
			} else {
				writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
			}
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"id": id, "status": "created"})

	default:
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func handleCloseAccountingPeriod(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	role := r.Header.Get("Resolved-Role")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	if !engines.IsSuperAdmin(role) {
		writeAPIError(w, r, "GLOBAL-0011", "")
		return
	}
	periodID := r.PathValue("id")
	if err := engines.CloseAccountingPeriod(tenantID, periodID, userID); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "closed"})
}

// handleSubmitApproval moves a Draft document into the approval queue.
func handleSubmitApproval(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	role := r.Header.Get("Resolved-Role")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req struct {
		Doctype    string `json:"doctype"`
		DocumentID string `json:"document_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Doctype == "" || req.DocumentID == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'doctype' and 'document_id' are required")
		return
	}

	allowed, err := checkPermission(tenantID, role, req.Doctype, "update")
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if !allowed {
		writeAPIError(w, r, "GLOBAL-0011", "")
		return
	}

	if err := engines.SubmitForApproval(tenantID, req.Doctype, req.DocumentID, userID, role); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "submitted"})
}

// handleDecideApproval approves or rejects a Pending Approval document.
func handleDecideApproval(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	role := r.Header.Get("Resolved-Role")
	userID := r.Header.Get("Resolved-User-ID")
	location := r.Header.Get("Resolved-Location")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req struct {
		Doctype    string `json:"doctype"`
		DocumentID string `json:"document_id"`
		Decision   string `json:"decision"`
		Comment    string `json:"comment"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Doctype == "" || req.DocumentID == "" || req.Decision == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'doctype', 'document_id', and 'decision' are required")
		return
	}

	if err := engines.DecideApproval(tenantID, req.Doctype, req.DocumentID, userID, role, location, req.Decision, req.Comment); err != nil {
		// APPROV-0159 (Stage 25.5): a precisely-coded *ValidationError (the
		// reject-reason-missing check) takes priority over the doctype-
		// specific PURCHA-0083 case below, which only ever fires for a
		// different error (ErrApprovalRoleMismatch).
		if verr, ok := err.(*engines.ValidationError); ok && verr.Code != "" {
			writeAPIErrorDetail(w, r, verr.Code, verr.SubFor, verr.Message)
			return
		}
		// PURCHA-0083 (Stage 25 Batch 3): DecideApproval's role-mismatch
		// failure is generic across every approval-gated doctype
		// (POSCart/VendorInvoice/CycleCountLine/PurchaseOrder/...) - only
		// PurchaseOrder has a catalog scenario worded for it specifically,
		// so it's mapped here rather than inside DecideApproval itself,
		// which would need to pick one doctype's wording for every caller.
		if req.Doctype == "PurchaseOrder" && errors.Is(err, engines.ErrApprovalRoleMismatch) {
			writeAPIError(w, r, "PURCHA-0083", "")
			return
		}
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	engines.LogAuditEvent(tenantID, userID, "APPROVAL_DECISION", req.Decision, fmt.Sprintf("%s %s: %s", req.Doctype, req.DocumentID, req.Decision))

	// Stage 20.10: a discount-gated POS sale never ran its inventory/GL side
	// effects at request time (handleCheckout only submitted it for
	// approval) - an Approved decision is what actually completes the sale.
	// A Rejected cart is intentionally left as-is: no side effects ever ran,
	// so there's nothing to undo.
	if req.Doctype == "POSCart" && req.Decision == "Approved" {
		if _, _, finalizeErr := engines.FinalizePOSCheckout(tenantID, req.DocumentID, r.Header.Get("Resolved-Correlation-ID")); finalizeErr != nil {
			writeAPIErrorGeneric(w, r, http.StatusInternalServerError, fmt.Sprintf("Approved but failed to complete the sale: %v", finalizeErr))
			return
		}
	}
	// Stage 20.22: a cycle-count variance never adjusted inventory at
	// reconcile time - only an Approved decision actually posts it, same
	// finalize-on-approve pattern as POSCart's discount gate just above.
	if req.Doctype == "CycleCountLine" && req.Decision == "Approved" {
		if finalizeErr := engines.PostCycleCountAdjustment(tenantID, req.DocumentID, userID); finalizeErr != nil {
			writeAPIErrorGeneric(w, r, http.StatusInternalServerError, fmt.Sprintf("Approved but failed to post the adjustment: %v", finalizeErr))
			return
		}
	}
	// 24.11: a VendorInvoice override never paid at submit time - only an
	// Approved decision actually posts the GL entry and marks it Paid, same
	// finalize-on-approve pattern as the two cases just above.
	if req.Doctype == "VendorInvoice" && req.Decision == "Approved" {
		if _, finalizeErr := engines.FinalizeVendorInvoiceOverridePayment(tenantID, req.DocumentID, userID); finalizeErr != nil {
			writeAPIErrorGeneric(w, r, http.StatusInternalServerError, fmt.Sprintf("Approved but failed to complete the payment: %v", finalizeErr))
			return
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "decided", "decision": req.Decision})
}

// handleBulkDecideApproval (Stage 26.4.6) applies one decision to a bounded
// selection of Pending Approval documents - see engines.BulkDecideApproval.
// Deliberately does not run the doctype-specific finalize-on-approve side
// effects handleDecideApproval's single-document path runs above (POSCart
// checkout completion, cycle-count posting, vendor-invoice payment) - this
// endpoint is scoped to PIM content approval (bulk-approving ProductContent
// from the Workbench), which has no such side effect, and adding those
// unconditionally here would silently change behavior for any other
// doctype a caller might select in bulk.
func handleBulkDecideApproval(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	role := r.Header.Get("Resolved-Role")
	userID := r.Header.Get("Resolved-User-ID")
	location := r.Header.Get("Resolved-Location")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req struct {
		Doctype     string   `json:"doctype"`
		DocumentIDs []string `json:"document_ids"`
		Decision    string   `json:"decision"`
		Comment     string   `json:"comment"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Doctype == "" || req.Decision == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'doctype', 'document_ids', and 'decision' are required")
		return
	}
	succeeded, failed, err := engines.BulkDecideApproval(tenantID, req.Doctype, req.DocumentIDs, userID, role, location, req.Decision, req.Comment)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	engines.LogAuditEvent(tenantID, userID, "APPROVAL_BULK_DECISION", req.Decision, fmt.Sprintf("%s: %d succeeded, %d failed", req.Doctype, len(succeeded), len(failed)))
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"succeeded": succeeded, "failed": failed})
}

// handleApprovalLog (Stage 26.4.5) surfaces one document's existing
// approval_log history, in particular a rejection's mandatory comment.
func handleApprovalLog(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	doctype := r.URL.Query().Get("doctype")
	documentID := r.URL.Query().Get("document_id")
	if doctype == "" || documentID == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Query parameters 'doctype' and 'document_id' are required")
		return
	}
	results, err := engines.ListApprovalLog(tenantID, doctype, documentID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(results)
}

// handlePIMContentVersions (Stage 26.4.6) lists a ProductContent's approved
// version history.
func handlePIMContentVersions(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	contentID := r.PathValue("id")
	results, err := engines.ListProductContentVersions(tenantID, contentID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(results)
}

// handlePIMContentRollback (Stage 26.4.6) restores a prior approved
// ProductContent snapshot as the current Draft content.
func handlePIMContentRollback(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	role := r.Header.Get("Resolved-Role")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	contentID := r.PathValue("id")
	var req struct {
		VersionID int `json:"version_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.VersionID == 0 {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'version_id' is required")
		return
	}
	if err := engines.RollbackProductContentVersion(tenantID, contentID, req.VersionID, userID, role); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "rolled_back"})
}

// handleListPendingApprovals returns the caller's approval inbox.
func handleListPendingApprovals(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	role := r.Header.Get("Resolved-Role")
	location := r.Header.Get("Resolved-Location")
	if r.Method != http.MethodGet {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	results, err := engines.ListPendingApprovals(tenantID, role, location)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if results == nil {
		results = []map[string]interface{}{}
	}
	_ = json.NewEncoder(w).Encode(results)
}

// handleApprovalRules lists and manages the amount-slab/role routing
// configuration. GET is open to any authenticated role (rules are read
// during submit-time routing decisions by non-admin users too); POST
// (create/edit, Stage 24.8) and DELETE (Stage 26.3.3, the admin screen's
// "remove a mistaken rule" action) are HR/Admin-only, same as every other
// global config screen (labels/sequence/prefix, Stage 24.2).
func handleApprovalRules(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	switch r.Method {
	case http.MethodGet:
		rules, err := engines.GetApprovalRules(tenantID)
		if err != nil {
			writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		if rules == nil {
			rules = []engines.ApprovalRule{}
		}
		_ = json.NewEncoder(w).Encode(rules)

	case http.MethodPost:
		// 24.8: the only write path into approval_rules - HR/Admin-only,
		// same as every other global config screen (labels/sequence/prefix,
		// Stage 24.2). Runs the save-time overlap check UpsertApprovalRule
		// implements before the row is written.
		if !requireHRAdmin(w, r, r.Header.Get("Resolved-Role")) {
			return
		}
		var req struct {
			ID           *int     `json:"id"`
			Doctype      string   `json:"doctype"`
			MinAmount    float64  `json:"min_amount"`
			MaxAmount    *float64 `json:"max_amount"`
			RequiredRole string   `json:"required_role"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid payload")
			return
		}
		newID, err := engines.UpsertApprovalRule(tenantID, req.Doctype, req.MinAmount, req.MaxAmount, req.RequiredRole, req.ID)
		if err != nil {
			writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
			return
		}
		engines.LogAuditEvent(tenantID, r.Header.Get("Resolved-User-ID"), "SAVE_APPROVAL_RULE", "SUCCESS",
			fmt.Sprintf("%s [%v, %v] -> %s", req.Doctype, req.MinAmount, req.MaxAmount, req.RequiredRole))
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "saved", "id": newID})

	case http.MethodDelete:
		if !requireHRAdmin(w, r, r.Header.Get("Resolved-Role")) {
			return
		}
		idStr := r.URL.Query().Get("id")
		ruleID, err := strconv.Atoi(idStr)
		if err != nil {
			writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Query param 'id' must be a valid rule id")
			return
		}
		if err := engines.DeleteApprovalRule(tenantID, ruleID); err != nil {
			writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
			return
		}
		engines.LogAuditEvent(tenantID, r.Header.Get("Resolved-User-ID"), "DELETE_APPROVAL_RULE", "SUCCESS", fmt.Sprintf("rule id %d", ruleID))
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "deleted", "id": ruleID})

	default:
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// handleCalculateGST computes the CGST/SGST/IGST split for a taxable amount
// and rate (Stage 13.10). The rate itself comes from the caller (typically
// an Item's HSN-classified gst_rate field) - this endpoint is the
// calculation step, not an HSN-to-rate lookup service.
func handleCalculateGST(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req struct {
		TaxableAmount float64 `json:"taxable_amount"`
		GSTRate       float64 `json:"gst_rate"`
		Interstate    bool    `json:"interstate"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request payload")
		return
	}
	result, err := engines.CalculateGST(req.TaxableAmount, req.GSTRate, req.Interstate)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(result)
}

// Report catalog (Stage 13.11) - prioritized per the gap analysis's own
// list: Current Stock, Sales Register, Vendor Ledger, Payables Ageing.
func handleCurrentStockReport(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	results, err := engines.GetCurrentStockReport(tenantID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if results == nil {
		results = []map[string]interface{}{}
	}
	_ = json.NewEncoder(w).Encode(results)
}

func handleSalesRegisterReport(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	results, err := engines.GetSalesRegisterReport(tenantID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if results == nil {
		results = []engines.SalesRegisterEntry{}
	}
	_ = json.NewEncoder(w).Encode(results)
}

func handleVendorLedgerReport(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	vendor := r.URL.Query().Get("vendor")
	results, err := engines.GetVendorLedgerReport(tenantID, vendor)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if results == nil {
		results = []map[string]interface{}{}
	}
	_ = json.NewEncoder(w).Encode(results)
}

func handlePayablesAgeingReport(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	results, err := engines.GetPayablesAgeingReport(tenantID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(results)
}

// RFQ / Vendor Quote / Quote Comparison (Stage 13.12). RFQ/VendorQuote
// creation and listing go through the existing generic doc endpoint like
// Vendor/Customer did (Stage 13.9) - these two handlers cover only the
// comparison view and the winner-selection action, which need logic the
// generic endpoint doesn't have.
