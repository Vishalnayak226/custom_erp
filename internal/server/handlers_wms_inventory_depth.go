package server

import (
	"encoding/json"
	"net/http"

	"custom_erp/engines"
)

// Stage 42.5 (Inventory control depth): physical inventory, the
// tenant-configurable CycleClass cycle-count plan, demand-driven/
// wave-triggered/dynamic pick-face replenishment, facility hierarchy/
// copy/cross-facility inventory inquiry, and (42.5.5) multi-owner stock
// segregation assign/consume. Kept in its own file, same per-Stage split
// as handlers_wms_outbound.go/handlers_wms_enterprise.go.
// 42.5.4 (slotting v2), the two facility inquiries, and the owner-stock
// inquiry add no routes of their own here - they're ReportDefinitions
// served by the generic report endpoint, the same "reports own the read
// paths" split routes.go's own comments already document for the
// batch/serial inquiries.

func handlePhysicalInventoryStart(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	var req struct {
		Location string `json:"location"`
		Zone     string `json:"zone"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Location == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'location' is required")
		return
	}
	piID, lineCount, err := engines.StartPhysicalInventory(tenantID, req.Location, req.Zone, userID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"physical_inventory_id": piID, "line_count": lineCount})
}

func handlePhysicalInventorySubmitCount(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	var req struct {
		LineID     string  `json:"line_id"`
		CountedQty float64 `json:"counted_qty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.LineID == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'line_id' is required")
		return
	}
	if err := engines.SubmitPhysicalInventoryCount(tenantID, req.LineID, req.CountedQty, userID); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

func handlePhysicalInventoryReconcile(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	role := r.Header.Get("Resolved-Role")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	var req struct {
		PhysicalInventoryID string `json:"physical_inventory_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.PhysicalInventoryID == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'physical_inventory_id' is required")
		return
	}
	posted, pendingApproval, err := engines.ReconcilePhysicalInventory(tenantID, req.PhysicalInventoryID, userID, role)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"posted": posted, "pending_approval": pendingApproval})
}

func handlePhysicalInventoryClose(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	var req struct {
		PhysicalInventoryID string `json:"physical_inventory_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.PhysicalInventoryID == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'physical_inventory_id' is required")
		return
	}
	if err := engines.ClosePhysicalInventory(tenantID, req.PhysicalInventoryID, userID); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "closed"})
}

func handlePhysicalInventoryCancel(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	var req struct {
		PhysicalInventoryID string `json:"physical_inventory_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.PhysicalInventoryID == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'physical_inventory_id' is required")
		return
	}
	if err := engines.CancelPhysicalInventory(tenantID, req.PhysicalInventoryID, userID); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "cancelled"})
}

func handleCycleCountPlan(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	locationCode := r.URL.Query().Get("location_code")
	if locationCode == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Query parameter 'location_code' is required")
		return
	}
	plan, err := engines.GetCycleCountPlan(tenantID, locationCode)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if plan == nil {
		plan = []engines.ABCCycleCountSuggestion{}
	}
	_ = json.NewEncoder(w).Encode(plan)
}

func handleDemandDrivenReplenishmentSuggestions(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	locationCode := r.URL.Query().Get("location_code")
	if locationCode == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Query parameter 'location_code' is required")
		return
	}
	suggestions, err := engines.GetDemandDrivenReplenishmentSuggestions(tenantID, locationCode)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if suggestions == nil {
		suggestions = []engines.BinReplenishmentSuggestion{}
	}
	_ = json.NewEncoder(w).Encode(suggestions)
}

func handleWaveReplenishmentSuggestions(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	waveID := r.URL.Query().Get("wave_id")
	if waveID == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Query parameter 'wave_id' is required")
		return
	}
	suggestions, err := engines.GetWaveReplenishmentSuggestions(tenantID, waveID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if suggestions == nil {
		suggestions = []engines.BinReplenishmentSuggestion{}
	}
	_ = json.NewEncoder(w).Encode(suggestions)
}

func handleDynamicPickFaceSuggestions(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	locationCode := r.URL.Query().Get("location_code")
	if locationCode == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Query parameter 'location_code' is required")
		return
	}
	coverageDays := queryIntOrDefault(r, "coverage_days", 3)
	suggestions, err := engines.GetDynamicPickFaceSuggestions(tenantID, locationCode, coverageDays)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if suggestions == nil {
		suggestions = []engines.DynamicPickFaceSuggestion{}
	}
	_ = json.NewEncoder(w).Encode(suggestions)
}

func handleApplyDynamicPickFaceMinMax(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	var req struct {
		BinCode string `json:"bin_code"`
		Sku     string `json:"sku"`
		MinQty  int    `json:"min_qty"`
		MaxQty  int    `json:"max_qty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.BinCode == "" || req.Sku == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'bin_code' and 'sku' are required")
		return
	}
	if err := engines.ApplyDynamicPickFaceMinMax(tenantID, req.BinCode, req.Sku, req.MinQty, req.MaxQty, userID); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

func handleFacilityChildren(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	parentCode := r.URL.Query().Get("parent_code")
	if parentCode == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Query parameter 'parent_code' is required")
		return
	}
	children, err := engines.GetChildLocations(tenantID, parentCode)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if children == nil {
		children = []engines.FacilityNode{}
	}
	_ = json.NewEncoder(w).Encode(children)
}

func handleFacilityDescendants(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	facilityCode := r.URL.Query().Get("facility_code")
	if facilityCode == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Query parameter 'facility_code' is required")
		return
	}
	descendants, err := engines.GetFacilityDescendants(tenantID, facilityCode)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if descendants == nil {
		descendants = []string{}
	}
	_ = json.NewEncoder(w).Encode(descendants)
}

// handleOwnerStockAssign assigns already-binned stock to a 3PL owner (42.5.5).
func handleOwnerStockAssign(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	var req struct {
		BinCode   string `json:"bin_code"`
		Sku       string `json:"sku"`
		OwnerID   string `json:"owner_id"`
		Condition string `json:"condition"`
		Qty       int    `json:"qty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.BinCode == "" || req.Sku == "" || req.OwnerID == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'bin_code', 'sku' and 'owner_id' are required")
		return
	}
	if err := engines.RecordOwnerStock(tenantID, req.BinCode, req.Sku, req.OwnerID, req.Condition, req.Qty, userID); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "assigned", "bin_code": req.BinCode, "sku": req.Sku, "owner_id": req.OwnerID, "qty": req.Qty,
	})
}

// handleOwnerStockConsume removes an owner's stock from a bin against a
// consuming voucher (42.5.5).
func handleOwnerStockConsume(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	var req struct {
		BinCode     string `json:"bin_code"`
		Sku         string `json:"sku"`
		OwnerID     string `json:"owner_id"`
		Condition   string `json:"condition"`
		Qty         int    `json:"qty"`
		VoucherType string `json:"voucher_type"`
		VoucherID   string `json:"voucher_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.BinCode == "" || req.Sku == "" || req.OwnerID == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'bin_code', 'sku' and 'owner_id' are required")
		return
	}
	if err := engines.ConsumeOwnerStock(tenantID, req.BinCode, req.Sku, req.OwnerID, req.Condition, req.Qty, req.VoucherType, req.VoucherID, userID); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "consumed"})
}

func handleFacilityCopy(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	var req struct {
		SourceLocation string `json:"source_location"`
		TargetLocation string `json:"target_location"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.SourceLocation == "" || req.TargetLocation == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'source_location' and 'target_location' are required")
		return
	}
	zonesCopied, binsCopied, err := engines.CopyFacilityConfig(tenantID, req.SourceLocation, req.TargetLocation, userID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"zones_copied": zonesCopied, "bins_copied": binsCopied})
}

// --- Stage 47.5.1: single-owner-per-warehouse (audit A-05) -----------------

// handleWarehouseOwnerAssign dedicates a location to one 3PL owner.
//
// This is the supported way to run 3PL stock in this build. Allocation and
// picking take no owner parameter anywhere in the codebase, so two owners in
// one building means one client's order can be filled from another client's
// segregated stock - the exact A-05 finding. Dedicating the warehouse removes
// the boundary that was going unenforced.
func handleWarehouseOwnerAssign(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req struct {
		LocationCode string `json:"location_code"`
		OwnerID      string `json:"owner_id"`
		Note         string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid payload")
		return
	}
	if err := engines.AssignWarehouseOwner(tenantID, req.LocationCode, req.OwnerID, userID, req.Note); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "assigned", "location_code": req.LocationCode, "owner_id": req.OwnerID,
	})
}

// handleMixedOwnerLocations lists every warehouse that violates the
// single-owner rule.
//
// It exists because the 47.5.1 migration deliberately does NOT rewrite
// pre-existing mixed data - aborting would block every later migration on such
// a database, and silently choosing a winner would re-attribute somebody's
// stock without being asked. Naming them is the honest option, and this is how
// an operator finds the list.
func handleMixedOwnerLocations(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	rows, err := engines.ListMixedOwnerLocations(tenantID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to check owner isolation")
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"single_owner_enforced": engines.SingleOwnerEnforced(tenantID),
		"mixed_locations":       rows,
		"count":                 len(rows),
	})
}
