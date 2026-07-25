package server

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"custom_erp/db"
	"custom_erp/engines"
)

// Fixed Assets, Expense Management, CRM/Loyalty, Manufacturing, the Shopify
// product-map/order-webhook, store fulfillment (task transitions, Return
// Anywhere), transfer-order dispatch/receive, purchase-requisition
// conversion, vendor-invoice match/pay, scale testing, marketplace
// settlement reconciliation, logistics booking, and the optimization engines
// (replenishment suggestions, SLA breach checks, demand forecasting).

func handleAssetRegister(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	results, err := engines.GetAssetRegister(tenantID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if results == nil {
		results = []engines.AssetRegisterEntry{}
	}
	_ = json.NewEncoder(w).Encode(results)
}

func handleCapitalizeAsset(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req struct {
		AssetID string `json:"asset_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.AssetID == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'asset_id' is required")
		return
	}
	if err := engines.CapitalizeAsset(tenantID, req.AssetID); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	engines.LogAuditEvent(tenantID, userID, "ASSET_CAPITALIZE", "SUCCESS", fmt.Sprintf("Asset %s capitalised", req.AssetID))
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "capitalised"})
}

func handleTransferAsset(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	username := r.Header.Get("Resolved-Username")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req struct {
		AssetID      string `json:"asset_id"`
		NewLocation  string `json:"new_location"`
		NewCustodian string `json:"new_custodian"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.AssetID == "" || req.NewLocation == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'asset_id' and 'new_location' are required")
		return
	}
	if err := engines.TransferAsset(tenantID, req.AssetID, req.NewLocation, req.NewCustodian, username); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "transferred"})
}

func handleDisposeAsset(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req struct {
		AssetID      string `json:"asset_id"`
		DisposalType string `json:"disposal_type"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.AssetID == "" || req.DisposalType == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'asset_id' and 'disposal_type' are required")
		return
	}
	if err := engines.DisposeAsset(tenantID, req.AssetID, req.DisposalType); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	engines.LogAuditEvent(tenantID, userID, "ASSET_DISPOSE", "SUCCESS", fmt.Sprintf("Asset %s disposed (%s)", req.AssetID, req.DisposalType))
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "disposed"})
}

// Expense Management (Stage 13.13c). Claim creation/listing and Manager
// Approval use the existing generic doc endpoint + approval engine (Stage
// 13.8); these two handlers cover the Finance Verification and Payment
// stages that come after.
func handleVerifyExpenseClaim(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req struct {
		ClaimID string `json:"claim_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ClaimID == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'claim_id' is required")
		return
	}
	if err := engines.VerifyExpenseClaim(tenantID, req.ClaimID); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	engines.LogAuditEvent(tenantID, userID, "EXPENSE_VERIFY", "SUCCESS", fmt.Sprintf("Expense claim %s finance-verified", req.ClaimID))
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "verified"})
}

func handlePayExpenseClaim(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req struct {
		ClaimID string `json:"claim_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ClaimID == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'claim_id' is required")
		return
	}
	payable, err := engines.PayExpenseClaim(tenantID, req.ClaimID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	engines.LogAuditEvent(tenantID, userID, "EXPENSE_PAY", "SUCCESS", fmt.Sprintf("Expense claim %s paid, payable_amount=%d", req.ClaimID, payable))
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "paid", "payable_amount": payable})
}

// CRM/Loyalty (Stage 13.13d, scoped MVP). Redemption is a standalone action
// (not wired into checkout's GL math - see handleCheckout) that burns
// points and returns their rupee discount value; the cashier applies that
// as a manual price adjustment before submitting the checkout.
func handleRedeemLoyaltyPoints(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req struct {
		CustomerID  string `json:"customer_id"`
		Points      int    `json:"points"`
		ReferenceID string `json:"reference_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.CustomerID == "" || req.Points <= 0 {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'customer_id' and a positive 'points' are required")
		return
	}
	discountValue, err := engines.RedeemLoyaltyPoints(tenantID, req.CustomerID, req.Points, req.ReferenceID)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"discount_value": discountValue})
}

func handleLoyaltyLedger(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	customerID := r.URL.Query().Get("customer_id")
	if customerID == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Query parameter 'customer_id' is required")
		return
	}
	balance, err := engines.GetLoyaltyBalance(tenantID, customerID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	ledger, err := engines.GetLoyaltyLedger(tenantID, customerID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if ledger == nil {
		ledger = []engines.LoyaltyLedgerEntry{}
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"balance": balance, "ledger": ledger})
}

// Manufacturing (Stage 13.13e, scoped MVP). BOM/ProductionOrder creation
// and listing use the same generic doc endpoint as Vendor/Customer/etc;
// these two handlers cover the material-issue and completion actions,
// which need logic (BOM explosion, inventory posting) the generic endpoint
// doesn't have.
func handleIssueProductionMaterial(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req struct {
		OrderID string `json:"order_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.OrderID == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'order_id' is required")
		return
	}
	if err := engines.IssueProductionMaterial(tenantID, req.OrderID); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	engines.LogAuditEvent(tenantID, userID, "PRODUCTION_MATERIAL_ISSUE", "SUCCESS", fmt.Sprintf("Material issued for production order %s", req.OrderID))
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "material_issued"})
}

func handleCompleteProductionOrder(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req struct {
		OrderID string `json:"order_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.OrderID == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'order_id' is required")
		return
	}
	if err := engines.CompleteProductionOrder(tenantID, req.OrderID); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	engines.LogAuditEvent(tenantID, userID, "PRODUCTION_ORDER_COMPLETE", "SUCCESS", fmt.Sprintf("Production order %s completed, finished goods received", req.OrderID))
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "completed"})
}

func handleShopifyProductMap(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Failed to read request body")
		return
	}
	if !verifyShopifyWebhookSignature(r, body) {
		writeAPIErrorGeneric(w, r, http.StatusUnauthorized, "Invalid webhook signature")
		return
	}

	var req struct {
		Sku        string `json:"sku"`
		ChannelSku string `json:"channel_sku"`
	}

	if err := json.Unmarshal(body, &req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid mapping payload")
		return
	}

	if req.Sku == "" || req.ChannelSku == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'sku' and 'channel_sku' are required")
		return
	}

	err = engines.MapChannelProduct(tenantID, "Shopify", req.Sku, req.ChannelSku)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":  "mapped",
		"sku":     req.Sku,
		"channel": "Shopify",
	})
}

func handleShopifyOrderWebhook(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Failed to read request body")
		return
	}
	if !verifyShopifyWebhookSignature(r, body) {
		writeAPIErrorGeneric(w, r, http.StatusUnauthorized, "Invalid webhook signature")
		return
	}

	var req struct {
		ID        string `json:"id"`
		LineItems []struct {
			Sku string `json:"sku"`
			Qty int    `json:"qty"`
		} `json:"line_items"`
	}

	if err := json.Unmarshal(body, &req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid webhook payload")
		return
	}

	if req.ID == "" || len(req.LineItems) == 0 {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'id' and 'line_items' are required")
		return
	}

	// Convert structure to slice of maps
	var items []map[string]interface{}
	for _, item := range req.LineItems {
		items = append(items, map[string]interface{}{
			"sku": item.Sku,
			"qty": item.Qty,
		})
	}

	orderID, err := engines.ImportChannelOrder(tenantID, "Shopify", req.ID, items)
	if err != nil {
		if err.Error() == "ORDER_ALREADY_IMPORTED" {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"status":  "ignored",
				"details": "Order already processed (idempotency check)",
			})
			return
		}
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":   "imported",
		"order_id": orderID,
	})
}

func handleFulfillmentTaskTransition(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req struct {
		TaskID string `json:"task_id"`
		Status string `json:"status"` // "Picking", "Packed", "Dispatched", "Rejected"
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid transition payload")
		return
	}

	if req.TaskID == "" || req.Status == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'task_id' and 'status' are required")
		return
	}

	err := engines.TransitionTaskStatus(tenantID, req.TaskID, req.Status)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":     "transitioned",
		"task_id":    req.TaskID,
		"new_status": req.Status,
	})
}

func handleFulfillmentReturn(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req struct {
		ReturnLocation  string `json:"return_location"`
		OriginalOrderID string `json:"original_order_id"`
		Items           []struct {
			Sku       string  `json:"sku"`
			Qty       int     `json:"qty"`
			SalePrice float64 `json:"sale_price"`
			CostPrice float64 `json:"cost_price"`
		} `json:"items"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid return payload")
		return
	}

	if req.ReturnLocation == "" || req.OriginalOrderID == "" || len(req.Items) == 0 {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'return_location', 'original_order_id', and 'items' are required")
		return
	}

	// Convert items structure to interface slice
	itemsInterface := make([]interface{}, len(req.Items))
	for i, item := range req.Items {
		itemsInterface[i] = map[string]interface{}{
			"sku":        item.Sku,
			"qty":        item.Qty,
			"sale_price": item.SalePrice,
			"cost_price": item.CostPrice,
		}
	}

	totalRefund, err := engines.ProcessReturnAnywhere(tenantID, req.ReturnLocation, req.OriginalOrderID, itemsInterface)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}

	// Save dynamic SalesReturn document. Field names match SalesReturn's
	// own declared doctype_fields schema (return_number/invoice_id/
	// amount_refunded, db/migration.sql) - a prior version of this handler
	// persisted req's own field names (original_order_id, no amount_refunded
	// at all) instead, which silently didn't match that schema and meant
	// engines.sumPriorReturns (Stage 25 Batch 3, SALESR-0130's already-
	// returned-qty check) would never have found this record. Fixed here
	// rather than left for a future caller to rediscover, since getting
	// SALESR-0130 right requires this to be correct going forward.
	returnID := fmt.Sprintf("RET-%s", req.OriginalOrderID)
	schema, err := db.GetTenantSchema(tenantID)
	if err == nil {
		docData := map[string]interface{}{
			"return_number":   returnID,
			"invoice_id":      req.OriginalOrderID,
			"amount_refunded": totalRefund,
			"items":           req.Items,
			"return_location": req.ReturnLocation,
		}
		payloadBytes, _ := json.Marshal(docData)
		query := fmt.Sprintf(`
			INSERT INTO %s.documents (id, doctype, data, status, created_by)
			VALUES ($1, 'SalesReturn', $2, 'Returned', 'system')`, schema)
		_, _ = db.DB.Exec(query, returnID, payloadBytes)
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":             "refunded",
		"original_order_id":  req.OriginalOrderID,
		"returned_location":  req.ReturnLocation,
		"amount_refunded":    totalRefund,
	})
}


func handleDispatchTransferOrder(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req struct {
		TransferOrderID string `json:"transfer_order_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TransferOrderID == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'transfer_order_id' is required")
		return
	}
	if err := engines.DispatchTransferOrder(tenantID, req.TransferOrderID, userID); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "Dispatched", "transfer_order_id": req.TransferOrderID})
}

func handleReceiveTransferOrder(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req struct {
		TransferOrderID string `json:"transfer_order_id"`
		ReceivedItems   []struct {
			Sku    string `json:"sku"`
			Qty    int    `json:"qty"`
			Reason string `json:"reason"`
		} `json:"received_items"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TransferOrderID == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'transfer_order_id' is required")
		return
	}
	itemsInterface := make([]interface{}, len(req.ReceivedItems))
	for i, item := range req.ReceivedItems {
		itemsInterface[i] = map[string]interface{}{"sku": item.Sku, "qty": item.Qty, "reason": item.Reason}
	}
	if err := engines.ReceiveTransferOrder(tenantID, req.TransferOrderID, userID, itemsInterface); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "Received", "transfer_order_id": req.TransferOrderID})
}

func handleConvertRequisition(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req struct {
		RequisitionID string `json:"requisition_id"`
		Target        string `json:"target"` // "RFQ" or "PurchaseOrder"
		StoreCode     string `json:"store_code"`
		FinancialYear string `json:"financial_year"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RequisitionID == "" || req.Target == "" || req.StoreCode == "" || req.FinancialYear == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'requisition_id', 'target', 'store_code', and 'financial_year' are required")
		return
	}
	newID, err := engines.ConvertRequisitionToOrder(tenantID, req.RequisitionID, req.Target, req.StoreCode, req.FinancialYear, userID)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "converted", "requisition_id": req.RequisitionID, "target": req.Target, "new_document_id": newID})
}

func handleMatchVendorInvoice(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req struct {
		InvoiceID        string  `json:"invoice_id"`
		POID             string  `json:"po_id"`
		GRNID            string  `json:"grn_id"`
		TolerancePercent float64 `json:"tolerance_percent"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.InvoiceID == "" || req.POID == "" || req.GRNID == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'invoice_id', 'po_id', and 'grn_id' are required")
		return
	}
	matched, err := engines.Match3Way(tenantID, req.POID, req.GRNID, req.InvoiceID, req.TolerancePercent)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	status := "MismatchHold"
	if matched {
		status = "Matched"
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"invoice_id": req.InvoiceID, "matched": matched, "status": status})
}

func handlePayVendorInvoice(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	role := r.Header.Get("Resolved-Role")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req struct {
		InvoiceID      string `json:"invoice_id"`
		OverrideReason string `json:"override_reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.InvoiceID == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'invoice_id' is required")
		return
	}
	amountPaid, pendingApproval, err := engines.PayVendorInvoice(tenantID, req.InvoiceID, userID, role, req.OverrideReason)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	// 24.11: an override no longer pays inline - it's routed to the approval
	// engine (engines/approval.go), same "pending_approval" response shape
	// the POS discount gate (Stage 20.10) already uses for the same reason.
	if pendingApproval {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "pending_approval", "invoice_id": req.InvoiceID})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "Paid", "invoice_id": req.InvoiceID, "amount_paid": amountPaid})
}

func handleScaleTest(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req struct {
		NumStores       int `json:"num_stores"`
		NumWorkers      int `json:"num_workers"`
		NumTransactions int `json:"num_transactions"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid scale test parameters")
		return
	}

	if req.NumStores <= 0 || req.NumWorkers <= 0 || req.NumTransactions <= 0 {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Parameters 'num_stores', 'num_workers', and 'num_transactions' must be positive integers")
		return
	}

	// 1. Seed test data
	err := engines.SeedScaleTestData(tenantID, req.NumStores, "BAR-SCALE", 1000)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, fmt.Sprintf("Failed to seed scale data: %v", err))
		return
	}

	// 2. Run simulation
	report, err := engines.RunScaleSimulation(tenantID, req.NumWorkers, req.NumTransactions, "BAR-SCALE", req.NumStores)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, fmt.Sprintf("Failed to execute scale simulation: %v", err))
		return
	}

	_ = json.NewEncoder(w).Encode(report)
}

func handleMarketplaceReconcile(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req struct {
		Channel      string   `json:"channel"`
		SettlementID string   `json:"settlement_id"`
		TotalSale    int      `json:"total_sale"`
		Commission   int      `json:"commission"`
		NetPayout    int      `json:"net_payout"`
		OrderIDs     []string `json:"order_ids"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid reconciliation payload")
		return
	}

	if req.SettlementID == "" || req.Channel == "" || req.TotalSale <= 0 {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'settlement_id', 'channel', and positive 'total_sale' are required")
		return
	}

	err := engines.ProcessMarketplaceSettlement(tenantID, req.Channel, req.SettlementID, req.TotalSale, req.Commission, req.NetPayout, req.OrderIDs)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":        "reconciled",
		"settlement_id": req.SettlementID,
		"net_received":  fmt.Sprintf("%d", req.NetPayout),
	})
}

func handleLogisticsBook(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req struct {
		OrderID            string `json:"order_id"`
		FulfillmentTaskID  string `json:"fulfillment_task_id"`
		Carrier            string `json:"carrier"`
		TrackingNumber     string `json:"tracking_number"`
		DestinationPincode string `json:"destination_pincode"`
		ShippingCharge     int    `json:"shipping_charge"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid logistics payload")
		return
	}

	if req.OrderID == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'order_id' is required")
		return
	}
	// Stage 26.12.4: a blank carrier with no destination pincode has nothing
	// to auto-select against - WMSLOG-0137 ("logistics partner required"),
	// same as before. A blank carrier WITH a pincode now attempts real
	// auto-selection via engines.CheckCourierServiceability; finding no
	// serviceable courier is the real WMSLOG-0138 ("AWB generation failed")
	// case the catalog reserved for this before any AWB-generation call
	// existed to attach it to.
	if req.Carrier == "" {
		if req.DestinationPincode == "" {
			writeAPIError(w, r, "WMSLOG-0137", "")
			return
		}
		options, errS := engines.CheckCourierServiceability(tenantID, req.DestinationPincode)
		if errS != nil {
			writeAPIErrorGeneric(w, r, http.StatusInternalServerError, errS.Error())
			return
		}
		if len(options) == 0 {
			writeAPIError(w, r, "WMSLOG-0138", "")
			return
		}
	}

	bookingID, err := engines.CreateLogisticsBooking(tenantID, req.OrderID, req.FulfillmentTaskID, req.Carrier, req.TrackingNumber, req.DestinationPincode, req.ShippingCharge)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":     "booked",
		"booking_id": bookingID,
	})
}

// handleCourierServiceability (Stage 26.12.4) previews which couriers, if
// any, service a destination pincode - lets the frontend booking form show
// a live courier choice before submitting, off the same
// engines.CheckCourierServiceability CreateLogisticsBooking uses internally.
func handleCourierServiceability(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	pincode := r.URL.Query().Get("destination_pincode")
	options, err := engines.CheckCourierServiceability(tenantID, pincode)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if options == nil {
		options = []engines.CourierOption{}
	}
	_ = json.NewEncoder(w).Encode(options)
}

// handleGenerateManifest (Stage 26.12.4) groups every AWB-assigned shipment
// for one courier+location pair into a new Manifest.
func handleGenerateManifest(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req struct {
		Courier      string `json:"courier"`
		LocationCode string `json:"location_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid manifest payload")
		return
	}
	manifestID, count, err := engines.GenerateManifest(tenantID, req.Courier, req.LocationCode)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"manifest_id":    manifestID,
		"shipment_count": count,
	})
}

// handleHandoverManifest (Stage 26.12.4) is the handover cascade's HTTP
// entry point - see engines.HandoverManifest for the shipment/task/order
// three-way status cascade this triggers.
func handleHandoverManifest(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req struct {
		ManifestID string `json:"manifest_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ManifestID == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'manifest_id' is required")
		return
	}
	if err := engines.HandoverManifest(tenantID, req.ManifestID, userID); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "handed_over", "manifest_id": req.ManifestID})
}

// handleShipmentTracking (Stage 26.12.4) is the tracking-sync ingestion
// point (engines.RecordDeliveryEvent) - WMSLOG-0139 ("delivery status
// could not be updated") is the catalog code reserved for exactly this
// call before it existed.
func handleShipmentTracking(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req struct {
		BookingID string `json:"booking_id"`
		Status    string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.BookingID == "" || req.Status == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'booking_id' and 'status' are required")
		return
	}
	if err := engines.RecordDeliveryEvent(tenantID, req.BookingID, req.Status, userID); err != nil {
		writeAPIError(w, r, "WMSLOG-0139", "")
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "updated", "booking_id": req.BookingID, "new_status": req.Status})
}

// handleShipmentRTO (Stage 26.12.4) captures a courier-reported RTO event
// (engines.RecordRTO) - stock/refund handling is Stage 26.12.5's scope.
func handleShipmentRTO(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req struct {
		BookingID string `json:"booking_id"`
		Reason    string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.BookingID == "" || req.Reason == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'booking_id' and 'reason' are required")
		return
	}
	if err := engines.RecordRTO(tenantID, req.BookingID, req.Reason, userID); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "rto", "booking_id": req.BookingID})
}

// handleShippingLabel (Stage 26.12.4) returns engines.GenerateShippingLabel's
// plain-text label for one booking.
func handleShippingLabel(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	bookingID := r.URL.Query().Get("booking_id")
	if bookingID == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Query parameter 'booking_id' is required")
		return
	}
	label, err := engines.GenerateShippingLabel(tenantID, bookingID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(label))
}

func handleReplenishmentSuggestions(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	locCode := r.URL.Query().Get("location_code")
	if locCode == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Query parameter 'location_code' is required")
		return
	}

	// Parse optional lead_time and safety_stock parameters
	leadTime := 7
	safetyStock := 10
	if ltStr := r.URL.Query().Get("lead_time"); ltStr != "" {
		_, _ = fmt.Sscanf(ltStr, "%d", &leadTime)
	}
	if ssStr := r.URL.Query().Get("safety_stock"); ssStr != "" {
		_, _ = fmt.Sscanf(ssStr, "%d", &safetyStock)
	}

	suggestions, err := engines.GetReplenishmentSuggestions(tenantID, locCode, leadTime, safetyStock)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	_ = json.NewEncoder(w).Encode(suggestions)
}

func handleSLABreaches(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	threshold := 120.0 // Default to 2 hours
	if threshStr := r.URL.Query().Get("threshold"); threshStr != "" {
		_, _ = fmt.Sscanf(threshStr, "%f", &threshold)
	}

	reports, err := engines.GetSLABreaches(tenantID, threshold)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	_ = json.NewEncoder(w).Encode(reports)
}

func handleDemandForecast(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req struct {
		LocationCode string `json:"location_code"`
		SKU          string `json:"sku"`
		ForecastDays int    `json:"forecast_days"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid forecasting payload")
		return
	}

	if req.LocationCode == "" || req.SKU == "" || req.ForecastDays <= 0 {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'location_code', 'sku', and positive 'forecast_days' are required")
		return
	}

	forecasted, err := engines.ForecastDemand(tenantID, req.LocationCode, req.SKU, req.ForecastDays)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"location_code":     req.LocationCode,
		"sku":               req.SKU,
		"forecast_days":     req.ForecastDays,
		"forecasted_demand": forecasted,
	})
}

// =========================================================================
// Stage 9.1: Unicommerce Integration Handlers
// =========================================================================
