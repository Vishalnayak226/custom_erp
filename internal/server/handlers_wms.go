package server

import (
	"encoding/json"
	"net/http"

	"custom_erp/engines"
)

// Stage 20 Track B.2 (WMS Maturity): putaway, bin-grouped pick lists,
// transfer-order pack/box-mapping, and cycle-count reconciliation. All
// role-open (any authenticated user), matching how handleCheckout and the
// Stage 20.7/20.8 POS session endpoints are scoped - a warehouse operator
// role doesn't exist separately from Store Manager/Cashier/HR-Admin today.

func handlePutaway(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	var req struct {
		BinCode string `json:"bin_code"`
		Sku     string `json:"sku"`
		Qty     int    `json:"qty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.BinCode == "" || req.Sku == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'bin_code' and 'sku' are required")
		return
	}
	if err := engines.PutawayToBin(tenantID, req.BinCode, req.Sku, req.Qty, userID); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

// handlePlaceHold (Stage 42.3.5) is the only creation path for a Hold
// document - role-open like handlePutaway, since PlaceHold's own checks
// (Active hold code, qty vs on-hand) are the real gate, not who is allowed
// to call it.
func handlePlaceHold(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	var req struct {
		HoldCode     string `json:"hold_code"`
		Sku          string `json:"sku"`
		LocationCode string `json:"location_code"`
		BatchNo      string `json:"batch_no"`
		Qty          int    `json:"qty"`
		Reason       string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.HoldCode == "" || req.Sku == "" || req.LocationCode == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'hold_code', 'sku' and 'location_code' are required")
		return
	}
	holdID, err := engines.PlaceHold(tenantID, req.HoldCode, req.Sku, req.LocationCode, req.BatchNo, req.Qty, req.Reason, userID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "success", "id": holdID})
}

func handlePickList(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	taskID := r.URL.Query().Get("task_id")
	if taskID == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Query parameter 'task_id' is required")
		return
	}
	lines, err := engines.GenerateBinPickList(tenantID, taskID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if lines == nil {
		lines = []engines.PickListLine{}
	}
	_ = json.NewEncoder(w).Encode(lines)
}

func handleBinConditionTransition(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	var req struct {
		BinCode       string `json:"bin_code"`
		Sku           string `json:"sku"`
		Qty           int    `json:"qty"`
		FromCondition string `json:"from_condition"`
		ToCondition   string `json:"to_condition"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.BinCode == "" || req.Sku == "" || req.FromCondition == "" || req.ToCondition == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'bin_code', 'sku', 'from_condition', and 'to_condition' are required")
		return
	}
	if err := engines.TransitionBinStockCondition(tenantID, req.BinCode, req.Sku, req.Qty, req.FromCondition, req.ToCondition, userID); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

// Stage 26.12.3: FulfillmentTask Pick/Pack scan endpoints - see
// engines/fulfillment_pickpack.go's own package doc comment for how this
// relates to (and deliberately doesn't duplicate) the bin-pick-list
// endpoints above. Same role-open convention as the rest of this file.

func handlePickScan(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	var req struct {
		TaskID string `json:"task_id"`
		Scan   string `json:"scan"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TaskID == "" || req.Scan == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'task_id' and 'scan' are required")
		return
	}
	sku, pickedQty, err := engines.ScanPickItem(tenantID, req.TaskID, req.Scan)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	// Stage 42.2.2: additive WarehouseTask retrofit, done from the handler
	// (which has Resolved-User-ID) rather than inside ScanPickItem itself,
	// which deliberately keeps its existing signature - see
	// FulfillmentTaskLocationCode's own comment for why.
	if loc, lerr := engines.FulfillmentTaskLocationCode(tenantID, req.TaskID); lerr == nil {
		userID := r.Header.Get("Resolved-User-ID")
		engines.LogCompletedWarehouseTask(tenantID, engines.NewWarehouseTask{
			TaskType: "Pick", LocationCode: loc, Item: sku, Qty: 1,
			SourceDocType: "FulfillmentTask", SourceDocID: req.TaskID,
		}, userID)
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"sku": sku, "picked_qty": pickedQty})
}

func handlePackScan(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	var req struct {
		TaskID string `json:"task_id"`
		Scan   string `json:"scan"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TaskID == "" || req.Scan == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'task_id' and 'scan' are required")
		return
	}
	sku, packedQty, err := engines.ScanPackItem(tenantID, req.TaskID, req.Scan)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"sku": sku, "packed_qty": packedQty})
}

func handleShortPick(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	var req struct {
		TaskID     string `json:"task_id"`
		Sku        string `json:"sku"`
		ReasonCode string `json:"reason_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TaskID == "" || req.Sku == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'task_id' and 'sku' are required")
		return
	}
	if err := engines.ShortPickLine(tenantID, req.TaskID, req.Sku, req.ReasonCode); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "short_picked"})
}

func handlePackTransferOrder(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	var req struct {
		TransferOrderID string                   `json:"transfer_order_id"`
		Boxes           []map[string]interface{} `json:"boxes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TransferOrderID == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'transfer_order_id' is required")
		return
	}
	if err := engines.PackTransferOrder(tenantID, req.TransferOrderID, userID, req.Boxes); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "Packed"})
}

func handleReconcileCycleCount(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	role := r.Header.Get("Resolved-Role")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	var req struct {
		CountSession string `json:"count_session"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.CountSession == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'count_session' is required")
		return
	}
	posted, pendingApproval, err := engines.ReconcileCycleCount(tenantID, req.CountSession, userID, role)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"posted_no_variance": posted,
		"pending_approval":   pendingApproval,
	})
}
