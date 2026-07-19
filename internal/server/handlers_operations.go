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
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	results, err := engines.GetAssetRegister(tenantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
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
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		AssetID string `json:"asset_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.AssetID == "" {
		http.Error(w, "Field 'asset_id' is required", http.StatusBadRequest)
		return
	}
	if err := engines.CapitalizeAsset(tenantID, req.AssetID); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	engines.LogAuditEvent(tenantID, userID, "ASSET_CAPITALIZE", "SUCCESS", fmt.Sprintf("Asset %s capitalised", req.AssetID))
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "capitalised"})
}

func handleTransferAsset(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	username := r.Header.Get("Resolved-Username")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		AssetID      string `json:"asset_id"`
		NewLocation  string `json:"new_location"`
		NewCustodian string `json:"new_custodian"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.AssetID == "" || req.NewLocation == "" {
		http.Error(w, "Fields 'asset_id' and 'new_location' are required", http.StatusBadRequest)
		return
	}
	if err := engines.TransferAsset(tenantID, req.AssetID, req.NewLocation, req.NewCustodian, username); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "transferred"})
}

func handleDisposeAsset(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		AssetID      string `json:"asset_id"`
		DisposalType string `json:"disposal_type"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.AssetID == "" || req.DisposalType == "" {
		http.Error(w, "Fields 'asset_id' and 'disposal_type' are required", http.StatusBadRequest)
		return
	}
	if err := engines.DisposeAsset(tenantID, req.AssetID, req.DisposalType); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
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
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ClaimID string `json:"claim_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ClaimID == "" {
		http.Error(w, "Field 'claim_id' is required", http.StatusBadRequest)
		return
	}
	if err := engines.VerifyExpenseClaim(tenantID, req.ClaimID); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	engines.LogAuditEvent(tenantID, userID, "EXPENSE_VERIFY", "SUCCESS", fmt.Sprintf("Expense claim %s finance-verified", req.ClaimID))
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "verified"})
}

func handlePayExpenseClaim(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ClaimID string `json:"claim_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ClaimID == "" {
		http.Error(w, "Field 'claim_id' is required", http.StatusBadRequest)
		return
	}
	payable, err := engines.PayExpenseClaim(tenantID, req.ClaimID)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
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
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		CustomerID  string `json:"customer_id"`
		Points      int    `json:"points"`
		ReferenceID string `json:"reference_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.CustomerID == "" || req.Points <= 0 {
		http.Error(w, "Fields 'customer_id' and a positive 'points' are required", http.StatusBadRequest)
		return
	}
	discountValue, err := engines.RedeemLoyaltyPoints(tenantID, req.CustomerID, req.Points, req.ReferenceID)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"discount_value": discountValue})
}

func handleLoyaltyLedger(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	customerID := r.URL.Query().Get("customer_id")
	if customerID == "" {
		http.Error(w, "Query parameter 'customer_id' is required", http.StatusBadRequest)
		return
	}
	balance, err := engines.GetLoyaltyBalance(tenantID, customerID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	ledger, err := engines.GetLoyaltyLedger(tenantID, customerID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
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
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		OrderID string `json:"order_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.OrderID == "" {
		http.Error(w, "Field 'order_id' is required", http.StatusBadRequest)
		return
	}
	if err := engines.IssueProductionMaterial(tenantID, req.OrderID); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	engines.LogAuditEvent(tenantID, userID, "PRODUCTION_MATERIAL_ISSUE", "SUCCESS", fmt.Sprintf("Material issued for production order %s", req.OrderID))
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "material_issued"})
}

func handleCompleteProductionOrder(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		OrderID string `json:"order_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.OrderID == "" {
		http.Error(w, "Field 'order_id' is required", http.StatusBadRequest)
		return
	}
	if err := engines.CompleteProductionOrder(tenantID, req.OrderID); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	engines.LogAuditEvent(tenantID, userID, "PRODUCTION_ORDER_COMPLETE", "SUCCESS", fmt.Sprintf("Production order %s completed, finished goods received", req.OrderID))
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "completed"})
}

func handleShopifyProductMap(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}
	if !verifyShopifyWebhookSignature(r, body) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid webhook signature"})
		return
	}

	var req struct {
		Sku        string `json:"sku"`
		ChannelSku string `json:"channel_sku"`
	}

	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "Invalid mapping payload", http.StatusBadRequest)
		return
	}

	if req.Sku == "" || req.ChannelSku == "" {
		http.Error(w, "Fields 'sku' and 'channel_sku' are required", http.StatusBadRequest)
		return
	}

	err = engines.MapChannelProduct(tenantID, "Shopify", req.Sku, req.ChannelSku)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
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
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}
	if !verifyShopifyWebhookSignature(r, body) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid webhook signature"})
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
		http.Error(w, "Invalid webhook payload", http.StatusBadRequest)
		return
	}

	if req.ID == "" || len(req.LineItems) == 0 {
		http.Error(w, "Fields 'id' and 'line_items' are required", http.StatusBadRequest)
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
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
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
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		TaskID string `json:"task_id"`
		Status string `json:"status"` // "Picking", "Packed", "Dispatched", "Rejected"
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid transition payload", http.StatusBadRequest)
		return
	}

	if req.TaskID == "" || req.Status == "" {
		http.Error(w, "Fields 'task_id' and 'status' are required", http.StatusBadRequest)
		return
	}

	err := engines.TransitionTaskStatus(tenantID, req.TaskID, req.Status)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
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
		w.WriteHeader(http.StatusMethodNotAllowed)
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
		http.Error(w, "Invalid return payload", http.StatusBadRequest)
		return
	}

	if req.ReturnLocation == "" || req.OriginalOrderID == "" || len(req.Items) == 0 {
		http.Error(w, "Fields 'return_location', 'original_order_id', and 'items' are required", http.StatusBadRequest)
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

	err := engines.ProcessReturnAnywhere(tenantID, req.ReturnLocation, req.OriginalOrderID, itemsInterface)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	// Save dynamic SalesReturn document
	schema, err := db.GetTenantSchema(tenantID)
	if err == nil {
		payloadBytes, _ := json.Marshal(req)
		query := fmt.Sprintf(`
			INSERT INTO %s.documents (id, doctype, data, status, created_by) 
			VALUES ($1, 'SalesReturn', $2, 'Returned', 'system')`, schema)
		_, _ = db.DB.Exec(query, fmt.Sprintf("RET-%s", req.OriginalOrderID), payloadBytes)
	}

	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":            "refunded",
		"original_order_id": req.OriginalOrderID,
		"returned_location": req.ReturnLocation,
	})
}

func handleDispatchTransferOrder(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		TransferOrderID string `json:"transfer_order_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TransferOrderID == "" {
		http.Error(w, "Field 'transfer_order_id' is required", http.StatusBadRequest)
		return
	}
	if err := engines.DispatchTransferOrder(tenantID, req.TransferOrderID, userID); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "Dispatched", "transfer_order_id": req.TransferOrderID})
}

func handleReceiveTransferOrder(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		TransferOrderID string `json:"transfer_order_id"`
		ReceivedItems   []struct {
			Sku string `json:"sku"`
			Qty int    `json:"qty"`
		} `json:"received_items"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TransferOrderID == "" {
		http.Error(w, "Field 'transfer_order_id' is required", http.StatusBadRequest)
		return
	}
	itemsInterface := make([]interface{}, len(req.ReceivedItems))
	for i, item := range req.ReceivedItems {
		itemsInterface[i] = map[string]interface{}{"sku": item.Sku, "qty": item.Qty}
	}
	if err := engines.ReceiveTransferOrder(tenantID, req.TransferOrderID, userID, itemsInterface); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "Received", "transfer_order_id": req.TransferOrderID})
}

func handleConvertRequisition(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		RequisitionID string `json:"requisition_id"`
		Target        string `json:"target"` // "RFQ" or "PurchaseOrder"
		StoreCode     string `json:"store_code"`
		FinancialYear string `json:"financial_year"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RequisitionID == "" || req.Target == "" || req.StoreCode == "" || req.FinancialYear == "" {
		http.Error(w, "Fields 'requisition_id', 'target', 'store_code', and 'financial_year' are required", http.StatusBadRequest)
		return
	}
	newID, err := engines.ConvertRequisitionToOrder(tenantID, req.RequisitionID, req.Target, req.StoreCode, req.FinancialYear, userID)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "converted", "requisition_id": req.RequisitionID, "target": req.Target, "new_document_id": newID})
}

func handleMatchVendorInvoice(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		InvoiceID        string  `json:"invoice_id"`
		POID             string  `json:"po_id"`
		GRNID            string  `json:"grn_id"`
		TolerancePercent float64 `json:"tolerance_percent"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.InvoiceID == "" || req.POID == "" || req.GRNID == "" {
		http.Error(w, "Fields 'invoice_id', 'po_id', and 'grn_id' are required", http.StatusBadRequest)
		return
	}
	matched, err := engines.Match3Way(tenantID, req.POID, req.GRNID, req.InvoiceID, req.TolerancePercent)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
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
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		InvoiceID      string `json:"invoice_id"`
		OverrideReason string `json:"override_reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.InvoiceID == "" {
		http.Error(w, "Field 'invoice_id' is required", http.StatusBadRequest)
		return
	}
	amountPaid, err := engines.PayVendorInvoice(tenantID, req.InvoiceID, userID, req.OverrideReason)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "Paid", "invoice_id": req.InvoiceID, "amount_paid": amountPaid})
}

func handleScaleTest(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		NumStores       int `json:"num_stores"`
		NumWorkers      int `json:"num_workers"`
		NumTransactions int `json:"num_transactions"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid scale test parameters", http.StatusBadRequest)
		return
	}

	if req.NumStores <= 0 || req.NumWorkers <= 0 || req.NumTransactions <= 0 {
		http.Error(w, "Parameters 'num_stores', 'num_workers', and 'num_transactions' must be positive integers", http.StatusBadRequest)
		return
	}

	// 1. Seed test data
	err := engines.SeedScaleTestData(tenantID, req.NumStores, "BAR-SCALE", 1000)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("Failed to seed scale data: %v", err)})
		return
	}

	// 2. Run simulation
	report, err := engines.RunScaleSimulation(tenantID, req.NumWorkers, req.NumTransactions, "BAR-SCALE", req.NumStores)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("Failed to execute scale simulation: %v", err)})
		return
	}

	_ = json.NewEncoder(w).Encode(report)
}

func handleMarketplaceReconcile(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
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
		http.Error(w, "Invalid reconciliation payload", http.StatusBadRequest)
		return
	}

	if req.SettlementID == "" || req.Channel == "" || req.TotalSale <= 0 {
		http.Error(w, "Fields 'settlement_id', 'channel', and positive 'total_sale' are required", http.StatusBadRequest)
		return
	}

	err := engines.ProcessMarketplaceSettlement(tenantID, req.Channel, req.SettlementID, req.TotalSale, req.Commission, req.NetPayout, req.OrderIDs)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
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
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		OrderID        string `json:"order_id"`
		Carrier        string `json:"carrier"`
		TrackingNumber string `json:"tracking_number"`
		ShippingCharge int    `json:"shipping_charge"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid logistics payload", http.StatusBadRequest)
		return
	}

	if req.OrderID == "" || req.Carrier == "" || req.TrackingNumber == "" {
		http.Error(w, "Fields 'order_id', 'carrier', and 'tracking_number' are required", http.StatusBadRequest)
		return
	}

	bookingID, err := engines.CreateLogisticsBooking(tenantID, req.OrderID, req.Carrier, req.TrackingNumber, req.ShippingCharge)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":          "shipped",
		"booking_id":      bookingID,
		"tracking_number": req.TrackingNumber,
	})
}

func handleReplenishmentSuggestions(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	locCode := r.URL.Query().Get("location_code")
	if locCode == "" {
		http.Error(w, "Query parameter 'location_code' is required", http.StatusBadRequest)
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
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	_ = json.NewEncoder(w).Encode(suggestions)
}

func handleSLABreaches(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	threshold := 120.0 // Default to 2 hours
	if threshStr := r.URL.Query().Get("threshold"); threshStr != "" {
		_, _ = fmt.Sscanf(threshStr, "%f", &threshold)
	}

	reports, err := engines.GetSLABreaches(tenantID, threshold)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	_ = json.NewEncoder(w).Encode(reports)
}

func handleDemandForecast(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		LocationCode string `json:"location_code"`
		SKU          string `json:"sku"`
		ForecastDays int    `json:"forecast_days"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid forecasting payload", http.StatusBadRequest)
		return
	}

	if req.LocationCode == "" || req.SKU == "" || req.ForecastDays <= 0 {
		http.Error(w, "Fields 'location_code', 'sku', and positive 'forecast_days' are required", http.StatusBadRequest)
		return
	}

	forecasted, err := engines.ForecastDemand(tenantID, req.LocationCode, req.SKU, req.ForecastDays)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
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
