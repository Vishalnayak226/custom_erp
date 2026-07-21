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
		writeAPIErrorGeneric(w, r, http.StatusBadRequest, "Fields 'bin_code' and 'sku' are required")
		return
	}
	if err := engines.PutawayToBin(tenantID, req.BinCode, req.Sku, req.Qty, userID); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusBadRequest, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

func handlePickList(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	taskID := r.URL.Query().Get("task_id")
	if taskID == "" {
		writeAPIErrorGeneric(w, r, http.StatusBadRequest, "Query parameter 'task_id' is required")
		return
	}
	lines, err := engines.GenerateBinPickList(tenantID, taskID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusBadRequest, err.Error())
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
		writeAPIErrorGeneric(w, r, http.StatusBadRequest, "Fields 'bin_code', 'sku', 'from_condition', and 'to_condition' are required")
		return
	}
	if err := engines.TransitionBinStockCondition(tenantID, req.BinCode, req.Sku, req.Qty, req.FromCondition, req.ToCondition, userID); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusBadRequest, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "success"})
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
		writeAPIErrorGeneric(w, r, http.StatusBadRequest, "Field 'transfer_order_id' is required")
		return
	}
	if err := engines.PackTransferOrder(tenantID, req.TransferOrderID, userID, req.Boxes); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusBadRequest, err.Error())
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
		writeAPIErrorGeneric(w, r, http.StatusBadRequest, "Field 'count_session' is required")
		return
	}
	posted, pendingApproval, err := engines.ReconcileCycleCount(tenantID, req.CountSession, userID, role)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusBadRequest, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"posted_no_variance": posted,
		"pending_approval":   pendingApproval,
	})
}
