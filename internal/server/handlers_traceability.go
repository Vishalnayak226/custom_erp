package server

import (
	"encoding/json"
	"net/http"
	"strconv"

	"custom_erp/engines"
)

// Stage 42.1 - the traceability floor actions: assigning received stock to a
// lot, consuming a lot at pick, holding and releasing a lot, and the expiry
// sweep.
//
// Kept in its own file next to handlers_wms.go / handlers_wms_enterprise.go /
// handlers_wms_p2.go, the same one-file-per-stage convention those three
// already follow, and behind the same moduleGate("wms", ...) every other
// floor-ops route uses (see routes.go).
//
// The three read paths a user needs - near-expiry watchlist, batch stock
// inquiry, batch movement history - deliberately have NO handler here: they are
// ReportDefinitions (engines/traceability_reports.go) and are served by the
// existing generic report endpoint, which already gives them filtering, export,
// scheduling and column masking. Adding bespoke endpoints for them would be the
// parallel-third-way this repo's first principle exists to prevent.
//
// writeEngineError rather than writeAPIErrorGeneric on the gated paths: the
// engine returns precisely-coded *engines.ValidationError values here
// (INVENT-0106 batch expired, INV-0256 FEFO violation, INVENT-0104 blocked
// stock, INVENT-0115 serial/batch mismatch), and those codes are the whole
// reason the message matrix carries them.

// handleBatchPutaway assigns already-binned stock to a lot (42.1.3).
func handleBatchPutaway(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	var req struct {
		BinCode   string `json:"bin_code"`
		Sku       string `json:"sku"`
		BatchNo   string `json:"batch_no"`
		Condition string `json:"condition"`
		Qty       int    `json:"qty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.BinCode == "" || req.Sku == "" || req.BatchNo == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'bin_code', 'sku' and 'batch_no' are required")
		return
	}
	if err := engines.RecordBatchPutaway(tenantID, req.BinCode, req.Sku, req.BatchNo, req.Condition, req.Qty, userID); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "assigned", "bin_code": req.BinCode, "sku": req.Sku, "batch_no": req.BatchNo, "qty": req.Qty,
	})
}

// handleBatchConsume removes a lot's stock from a bin against a consuming
// document (42.1.3). ValidateBatchForIssue runs first, so every 42.1.6 expiry
// and hold gate applies to a manually-chosen lot exactly as it applies to an
// allocation-chosen one - the single-lot expression of the same rule.
func handleBatchConsume(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	var req struct {
		BinCode     string `json:"bin_code"`
		Sku         string `json:"sku"`
		BatchNo     string `json:"batch_no"`
		Condition   string `json:"condition"`
		Qty         int    `json:"qty"`
		VoucherType string `json:"voucher_type"`
		VoucherID   string `json:"voucher_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.BinCode == "" || req.Sku == "" || req.BatchNo == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'bin_code', 'sku' and 'batch_no' are required")
		return
	}
	if err := engines.ValidateBatchForIssue(tenantID, req.Sku, req.BatchNo); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	if err := engines.ConsumeBatchStock(tenantID, req.BinCode, req.Sku, req.BatchNo, req.Condition, req.Qty,
		req.VoucherType, req.VoucherID, userID); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "consumed", "bin_code": req.BinCode, "sku": req.Sku, "batch_no": req.BatchNo, "qty": req.Qty,
	})
}

// handleBatchStatus holds, releases or writes off a lot (42.1.6).
func handleBatchStatus(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	var req struct {
		Sku     string `json:"sku"`
		BatchNo string `json:"batch_no"`
		Status  string `json:"status"`
		Reason  string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Sku == "" || req.BatchNo == "" || req.Status == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'sku', 'batch_no' and 'status' are required")
		return
	}
	if err := engines.SetBatchStatus(tenantID, req.Sku, req.BatchNo, req.Status, req.Reason, userID); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "updated", "sku": req.Sku, "batch_no": req.BatchNo, "batch_status": req.Status,
	})
}

// handleBatchExpirySweep runs the near-expiry quarantine pass (42.1.6).
//
// Deliberately an operator-triggered POST rather than a background timer. The
// sweep moves stock out of `available` and is therefore something a warehouse
// must be able to point at a person and a time for; wiring it to a scheduler is
// 42.D5's decision about the per-tenant scheduler, not something to assume
// here. It is idempotent, so a tenant that does want it on a schedule can call
// it from the existing scheduled-job path without a second implementation.
func handleBatchExpirySweep(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	result, err := engines.SweepExpiredBatches(tenantID, userID)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(result)
}

// handleBatchAllocationPreview answers "what would a pick of N units of this
// SKU take, and from which lots" without committing anything (42.1.5).
//
// This exists because FEFO's most common support question is "why did it pick
// that one" - and the honest answer needs the ordering, the expiry dates and
// the shortfall together. Read-only, so it is safe to call from the batch
// inquiry screen while a wave is being planned.
func handleBatchAllocationPreview(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	sku := r.URL.Query().Get("sku")
	location := r.URL.Query().Get("location")
	if sku == "" || location == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Query parameters 'sku' and 'location' are required")
		return
	}
	qty, _ := strconv.Atoi(r.URL.Query().Get("qty"))
	if qty <= 0 {
		qty = 1
	}
	candidates, shortfall, err := engines.AllocateFromStock(tenantID, sku, location, qty)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	if candidates == nil {
		candidates = []engines.AllocationCandidate{}
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"sku":         sku,
		"location":    location,
		"requested":   qty,
		"strategy":    engines.ResolveAllocationStrategy(tenantID, sku),
		"allocations": candidates,
		"shortfall":   shortfall,
	})
}
