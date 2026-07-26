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

	res, err := engines.BulkImportCSV(tenantID, doctype, file, userID, false)
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

	res, err := engines.BulkImportCSV(tenantID, doctype, file, userID, true)
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
	if role != "HR/Admin" {
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
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "acknowledged"})
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
		CartNumber  string  `json:"cart_number"`
		Location    string  `json:"location"`
		PaymentMode string  `json:"payment_mode"`
		CustomerID  string  `json:"customer_id"`
		Interstate  bool    `json:"interstate"`
		DiscountPct float64 `json:"discount_pct"`
		// OfflineSynced (20.13): set only by the POS screen's own offline
		// queue when replaying a sale that was rung up while disconnected -
		// never set by a normal live checkout. Stamped onto the stored cart
		// (see storedPayload below) so FinalizePOSCheckout can allow the
		// resulting stock to go negative instead of rejecting a sale whose
		// goods already physically left the store.
		OfflineSynced bool `json:"offline_synced"`
		Items         []struct {
			Sku       string  `json:"sku"`
			Qty       int     `json:"qty"`
			SalePrice float64 `json:"sale_price"`
			CostPrice float64 `json:"cost_price"`
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
		if item.SalePrice < 0 || item.CostPrice < 0 {
			writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, fmt.Sprintf("Item prices cannot be negative (sku=%q)", item.Sku))
			return
		}
	}

	// GST enforcement (Stage 17.5): every line's Item must carry hsn_code +
	// gst_rate before checkout can proceed - resolved and validated here,
	// before any side effect (inventory decrement, GL posting) runs, same
	// as the qty/price checks above. sale_price is treated as tax-inclusive
	// (MRP convention), so the taxable amount is backed out of it.
	gstLines := make([]engines.GSTLineInput, len(req.Items))
	for i, item := range req.Items {
		gstLines[i] = engines.GSTLineInput{Sku: item.Sku, Qty: item.Qty, UnitRate: item.SalePrice}
	}
	gstBreakdown, gstErr := engines.ComputeGSTForLines(tenantID, gstLines, req.Interstate)
	if gstErr != nil {
		writeEngineError(w, r, gstErr, http.StatusUnprocessableEntity)
		return
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
	var requiredRole string
	if req.DiscountPct > 0 {
		requiredRole, err = engines.RequiredApproverRoleForAmount(tenantID, "POSCart", req.DiscountPct)
		if err != nil {
			writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to evaluate discount approval rules")
			return
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
	storedPayload["gst_breakdown"] = gstBreakdown
	storedPayload["pos_session"] = sessionID
	storedPayload["offline_synced"] = req.OfflineSynced
	if requiredRole != "" {
		// Percentage, not rupees - see extractAmount's comment in engines/approval.go.
		storedPayload["discount_amount"] = req.DiscountPct
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
	claimErr := db.DB.QueryRow(claimQuery, req.CartNumber, payloadBytes, claimStatus, claimant).Scan(&claimedID)
	if claimErr == sql.ErrNoRows {
		var existingStatus, existingData string
		lookupErr := db.DB.QueryRow(fmt.Sprintf(
			`SELECT status, data FROM %s.documents WHERE doctype = 'POSCart' AND id = $1`, schema),
			req.CartNumber).Scan(&existingStatus, &existingData)
		if lookupErr == nil && existingStatus == "Paid" {
			var existing struct {
				Items []struct {
					Qty       int     `json:"qty"`
					SalePrice float64 `json:"sale_price"`
					CostPrice float64 `json:"cost_price"`
				} `json:"items"`
			}
			replaySale, replayCost := 0, 0
			if json.Unmarshal([]byte(existingData), &existing) == nil {
				for _, it := range existing.Items {
					replaySale += int(it.SalePrice) * it.Qty
					replayCost += int(it.CostPrice) * it.Qty
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status":      "completed",
				"cart_number": req.CartNumber,
				"sale_total":  replaySale,
				"cost_total":  replayCost,
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
	} else if claimErr != nil {
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
			"message":       fmt.Sprintf("Discount of %.1f%% requires %s approval before this sale completes.", req.DiscountPct, requiredRole),
		})
		return
	}

	saleTotal, costTotal, finalizeErr := engines.FinalizePOSCheckout(tenantID, req.CartNumber, r.Header.Get("Resolved-Correlation-ID"))
	if finalizeErr != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, finalizeErr.Error())
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":        "completed",
		"cart_number":   req.CartNumber,
		"sale_total":    saleTotal,
		"cost_total":    costTotal,
		"gst_breakdown": gstBreakdown,
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

	res, err := engines.GetTrialBalance(tenantID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
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
		if role != "HR/Admin" {
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
				writeAPIError(w, r, verr.Code, verr.SubFor)
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
	if role != "HR/Admin" {
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
			writeAPIError(w, r, verr.Code, verr.SubFor)
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
