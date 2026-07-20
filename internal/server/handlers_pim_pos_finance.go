package server

import (
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

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
		http.Error(w, "Multipart payload exceeds limit", http.StatusBadRequest)
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "CSV file is mandatory under multipart FormFile 'file'", http.StatusBadRequest)
		return
	}
	defer file.Close()

	res, err := engines.BulkImportCSV(tenantID, doctype, file, userID, false)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	jobID, errJob := engines.RecordImportJob(tenantID, doctype, res, userID)
	if errJob != nil {
		engines.LogSystemError(tenantID, r.Header.Get("Resolved-Correlation-ID"), "IMPORT_JOB_RECORD_FAILED", r.URL.Path, errJob.Error(), "")
	}

	responseBytes, _ := json.Marshal(res)
	var responseMap map[string]interface{}
	_ = json.Unmarshal(responseBytes, &responseMap)
	if jobID != "" {
		responseMap["import_job_id"] = jobID
	}
	_ = json.NewEncoder(w).Encode(responseMap)
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
		http.Error(w, "Multipart payload exceeds limit", http.StatusBadRequest)
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "CSV file is mandatory under multipart FormFile 'file'", http.StatusBadRequest)
		return
	}
	defer file.Close()

	res, err := engines.BulkImportCSV(tenantID, doctype, file, userID, true)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(res)
}

// handlePIMImportJobErrors serves a completed ImportJob's row-level failures
// as a downloadable CSV, same Content-Disposition pattern as the CSV import
// template endpoint above.
func handlePIMImportJobErrors(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	jobID := r.PathValue("id")

	csvBytes, err := engines.GetImportJobErrorCSV(tenantID, jobID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
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
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if role != "HR/Admin" {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Only HR/Admin can configure channel credentials"})
		return
	}
	channelCode := r.PathValue("code")
	var fields map[string]string
	if err := json.NewDecoder(r.Body).Decode(&fields); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := engines.SaveChannelCredential(tenantID, channelCode, fields); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
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
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	channelCode := r.PathValue("channelCode")

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return
	}

	cred, credErr := engines.GetChannelWebhookSecret(tenantID, channelCode)
	if credErr != nil || cred == "" {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "channel has no webhook secret configured"})
		return
	}
	sig := r.Header.Get("X-Bc-Webhook-Signature")
	if !engines.VerifyBigCommerceWebhook(body, sig, cred) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid webhook signature"})
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
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s_template.csv", doctype))
	_, _ = w.Write(templateBytes)
}

func handleGetAvailability(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	sku := r.URL.Query().Get("sku")
	location := r.URL.Query().Get("location")

	if sku == "" || location == "" {
		http.Error(w, "Query parameters 'sku' and 'location' are required", http.StatusBadRequest)
		return
	}

	res, err := engines.GetAvailableToSell(tenantID, sku, location)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	_ = json.NewEncoder(w).Encode(res)
}

func handleCreateReservation(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
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
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}

	if req.Sku == "" || req.Location == "" || req.Qty <= 0 {
		http.Error(w, "Fields 'sku', 'location', and positive 'qty' are required", http.StatusBadRequest)
		return
	}

	expiry := req.ExpirySecond
	if expiry <= 0 {
		expiry = 300 // default 5 minutes
	}

	resID, err := engines.CreateReservation(tenantID, req.Sku, req.Location, req.Qty, req.ResType, expiry)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
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
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		CartNumber  string  `json:"cart_number"`
		Location    string  `json:"location"`
		PaymentMode string  `json:"payment_mode"`
		CustomerID  string  `json:"customer_id"`
		Interstate  bool    `json:"interstate"`
		DiscountPct float64 `json:"discount_pct"`
		Items       []struct {
			Sku       string  `json:"sku"`
			Qty       int     `json:"qty"`
			SalePrice float64 `json:"sale_price"`
			CostPrice float64 `json:"cost_price"`
		} `json:"items"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid checkout payload", http.StatusBadRequest)
		return
	}

	if req.CartNumber == "" || req.Location == "" || len(req.Items) == 0 {
		http.Error(w, "Fields 'cart_number', 'location', and 'items' are required", http.StatusBadRequest)
		return
	}

	// Reject non-positive qty/prices before any side effect runs. Below this line,
	// item.Qty is negated to decrement stock (see loop below) - an already-negative
	// qty would flip to positive and silently ADD stock instead of being rejected,
	// and would do so via PostInventoryLedger's own committed transaction, before
	// the later GL-posting step even runs its own (unrelated) sign validation.
	for _, item := range req.Items {
		if item.Sku == "" || item.Qty <= 0 {
			http.Error(w, fmt.Sprintf("Item quantity must be positive (sku=%q, qty=%d)", item.Sku, item.Qty), http.StatusBadRequest)
			return
		}
		if item.SalePrice < 0 || item.CostPrice < 0 {
			http.Error(w, fmt.Sprintf("Item prices cannot be negative (sku=%q)", item.Sku), http.StatusBadRequest)
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
		http.Error(w, fmt.Sprintf("GST validation failed: %v", gstErr), http.StatusBadRequest)
		return
	}

	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to resolve tenant schema"})
		return
	}

	// Stage 20.7: an open cashier session (for this location, opened by this
	// cashier) is a precondition for every sale - opened/closed via
	// POST /api/v1/pos/session/open and /close, never spoofable through this
	// endpoint since the session lookup below is keyed off the caller's own
	// resolved identity, not anything in the request body.
	sessionID, sessErr := engines.GetOpenSessionForCashier(tenantID, req.Location, cashier)
	if sessErr != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to check cashier session"})
		return
	}
	if sessionID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "No open POS session for this location - open a cashier session before completing a sale."})
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
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to evaluate discount approval rules"})
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
		_ = json.Unmarshal(rawReq, &storedPayload)
	}
	storedPayload["gst_breakdown"] = gstBreakdown
	storedPayload["pos_session"] = sessionID
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
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "This cart is already being processed or was already completed"})
		return
	} else if claimErr != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to claim checkout"})
		return
	}

	if requiredRole != "" {
		if err := engines.SubmitForApproval(tenantID, "POSCart", req.CartNumber, claimant, role); err != nil {
			_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET status = 'Failed', updated_at = CURRENT_TIMESTAMP WHERE doctype = 'POSCart' AND id = $1`, schema), req.CartNumber)
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("Failed to route for approval: %v", err)})
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
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": finalizeErr.Error()})
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
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		POSProfile  string  `json:"pos_profile"`
		Location    string  `json:"location"`
		OpeningCash float64 `json:"opening_cash"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Location == "" {
		http.Error(w, "Field 'location' is required", http.StatusBadRequest)
		return
	}

	id, err := engines.OpenPOSSession(tenantID, req.POSProfile, req.Location, cashier, userID, req.OpeningCash)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
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
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		SessionID   string  `json:"session_id"`
		CountedCash float64 `json:"counted_cash"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.SessionID == "" {
		http.Error(w, "Field 'session_id' is required", http.StatusBadRequest)
		return
	}

	expected, variance, err := engines.ClosePOSSession(tenantID, req.SessionID, cashier, req.CountedCash)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	engines.LogAuditEvent(tenantID, cashier, "POS_SESSION", "CLOSED", fmt.Sprintf("Session %s closed, variance %.2f", req.SessionID, variance))
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":        "Closed",
		"session_id":    req.SessionID,
		"expected_cash": expected,
		"counted_cash":  req.CountedCash,
		"variance":      variance,
	})
}

// handlePOSSessionCurrent tells the POS screen whether the caller already
// has an open session for a location, so it can show Open/Close Session UI
// instead of surfacing handleCheckout's 400 only after the cashier tries to sell.
func handlePOSSessionCurrent(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	cashier := r.Header.Get("Resolved-Username")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	location := r.URL.Query().Get("location")
	if location == "" {
		http.Error(w, "Query parameter 'location' is required", http.StatusBadRequest)
		return
	}

	id, err := engines.GetOpenSessionForCashier(tenantID, location, cashier)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to look up session"})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"session_id": id, "open": id != ""})
}

func handleTrialBalance(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	res, err := engines.GetTrialBalance(tenantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
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
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(periods)

	case http.MethodPost:
		if role != "HR/Admin" {
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "Only HR/Admin can create accounting periods"})
			return
		}
		var req struct {
			PeriodName string `json:"period_name"`
			StartDate  string `json:"start_date"`
			EndDate    string `json:"end_date"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.PeriodName == "" || req.StartDate == "" || req.EndDate == "" {
			http.Error(w, "period_name, start_date, and end_date are required", http.StatusBadRequest)
			return
		}
		id, err := engines.CreateAccountingPeriod(tenantID, req.PeriodName, req.StartDate, req.EndDate, userID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"id": id, "status": "created"})

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func handleCloseAccountingPeriod(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	role := r.Header.Get("Resolved-Role")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if role != "HR/Admin" {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Only HR/Admin can close accounting periods"})
		return
	}
	periodID := r.PathValue("id")
	if err := engines.CloseAccountingPeriod(tenantID, periodID, userID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
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
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Doctype    string `json:"doctype"`
		DocumentID string `json:"document_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Doctype == "" || req.DocumentID == "" {
		http.Error(w, "Fields 'doctype' and 'document_id' are required", http.StatusBadRequest)
		return
	}

	allowed, err := checkPermission(tenantID, role, req.Doctype, "update")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !allowed {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "You do not have permission to submit this document."})
		return
	}

	if err := engines.SubmitForApproval(tenantID, req.Doctype, req.DocumentID, userID, role); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
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
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Doctype    string `json:"doctype"`
		DocumentID string `json:"document_id"`
		Decision   string `json:"decision"`
		Comment    string `json:"comment"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Doctype == "" || req.DocumentID == "" || req.Decision == "" {
		http.Error(w, "Fields 'doctype', 'document_id', and 'decision' are required", http.StatusBadRequest)
		return
	}

	if err := engines.DecideApproval(tenantID, req.Doctype, req.DocumentID, userID, role, location, req.Decision, req.Comment); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
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
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("Approved but failed to complete the sale: %v", finalizeErr)})
			return
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "decided", "decision": req.Decision})
}

// handleListPendingApprovals returns the caller's approval inbox.
func handleListPendingApprovals(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	role := r.Header.Get("Resolved-Role")
	location := r.Header.Get("Resolved-Location")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	results, err := engines.ListPendingApprovals(tenantID, role, location)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if results == nil {
		results = []map[string]interface{}{}
	}
	_ = json.NewEncoder(w).Encode(results)
}

// handleApprovalRules lists the amount-slab/role routing configuration.
// Read-only for now (edited directly via seed data / a future admin form,
// same as this project's other configuration tables started out).
func handleApprovalRules(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	rules, err := engines.GetApprovalRules(tenantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if rules == nil {
		rules = []engines.ApprovalRule{}
	}
	_ = json.NewEncoder(w).Encode(rules)
}

// handleCalculateGST computes the CGST/SGST/IGST split for a taxable amount
// and rate (Stage 13.10). The rate itself comes from the caller (typically
// an Item's HSN-classified gst_rate field) - this endpoint is the
// calculation step, not an HSN-to-rate lookup service.
func handleCalculateGST(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		TaxableAmount float64 `json:"taxable_amount"`
		GSTRate       float64 `json:"gst_rate"`
		Interstate    bool    `json:"interstate"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request payload", http.StatusBadRequest)
		return
	}
	result, err := engines.CalculateGST(req.TaxableAmount, req.GSTRate, req.Interstate)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(result)
}

// Report catalog (Stage 13.11) - prioritized per the gap analysis's own
// list: Current Stock, Sales Register, Vendor Ledger, Payables Ageing.
func handleCurrentStockReport(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	results, err := engines.GetCurrentStockReport(tenantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
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
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	results, err := engines.GetSalesRegisterReport(tenantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
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
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	vendor := r.URL.Query().Get("vendor")
	results, err := engines.GetVendorLedgerReport(tenantID, vendor)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
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
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	results, err := engines.GetPayablesAgeingReport(tenantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(results)
}

// RFQ / Vendor Quote / Quote Comparison (Stage 13.12). RFQ/VendorQuote
// creation and listing go through the existing generic doc endpoint like
// Vendor/Customer did (Stage 13.9) - these two handlers cover only the
// comparison view and the winner-selection action, which need logic the
// generic endpoint doesn't have.
