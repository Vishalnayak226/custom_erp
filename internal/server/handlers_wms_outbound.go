package server

import (
	"encoding/json"
	"net/http"

	"custom_erp/engines"
)

// Stage 42.4 - Outbound depth's HTTP surface: wave creation/lifecycle,
// sortation, cartonization v2, packing validation, deconsolidation, loading
// + Bill of Lading, and VAS task completion. Own file, same
// one-file-per-stage convention handlers_traceability.go/handlers_wms*.go
// already follow, behind the same moduleGate("wms", ...) every other
// floor-ops route uses (wired in routes.go).

// ---------------------------------------------------------------------------
// 42.4.1/42.4.2 - Wave.
// ---------------------------------------------------------------------------

func handleWaveCreate(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	var req struct {
		LocationCode   string   `json:"location_code"`
		WaveTemplateID string   `json:"wave_template_id"`
		TaskIDs        []string `json:"task_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.LocationCode == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'location_code' is required")
		return
	}
	waveID, tagged, err := engines.CreateWave(tenantID, req.LocationCode, req.WaveTemplateID, req.TaskIDs, "Manual", userID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"wave_id": waveID, "tagged": tagged})
}

func handleWaveTemplateRun(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	var req struct {
		WaveTemplateID string `json:"wave_template_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.WaveTemplateID == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'wave_template_id' is required")
		return
	}
	waveID, tagged, err := engines.RunWaveTemplateAutoCreate(tenantID, req.WaveTemplateID, userID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if waveID == "" {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"wave_id": nil, "tagged": 0, "message": "no matching tasks were due"})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"wave_id": waveID, "tagged": tagged})
}

func handleWaveTransition(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	var req struct {
		WaveID string `json:"wave_id"`
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.WaveID == "" || req.Status == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'wave_id' and 'status' are required")
		return
	}
	if err := engines.TransitionWaveStatus(tenantID, req.WaveID, req.Status, userID); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "updated"})
}

func handleWaveMonitor(w http.ResponseWriter, r *http.Request) {
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
	rows, err := engines.GetWaveMonitor(tenantID, locationCode)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"waves": rows})
}

// ---------------------------------------------------------------------------
// 42.4.3 - Sortation.
// ---------------------------------------------------------------------------

func handleSortSlotProvision(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	var req struct {
		Station  string `json:"station"`
		NumSlots int    `json:"num_slots"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Station == "" || req.NumSlots <= 0 {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'station' and a positive 'num_slots' are required")
		return
	}
	created, err := engines.ProvisionSortSlots(tenantID, req.Station, req.NumSlots, userID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"created": created})
}

func handleSortSlotAssign(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	var req struct {
		Station           string `json:"station"`
		WaveID            string `json:"wave_id"`
		FulfillmentTaskID string `json:"fulfillment_task_id"`
		Sku               string `json:"sku"`
		QtyExpected       int    `json:"qty_expected"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Station == "" || req.FulfillmentTaskID == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'station' and 'fulfillment_task_id' are required")
		return
	}
	slot, err := engines.AssignSortSlot(tenantID, req.Station, req.WaveID, req.FulfillmentTaskID, req.Sku, req.QtyExpected, userID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"slot": slot})
}

func handleSortSlotConfirm(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	var req struct {
		SlotID string `json:"slot_id"`
		Qty    int    `json:"qty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.SlotID == "" || req.Qty <= 0 {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'slot_id' and a positive 'qty' are required")
		return
	}
	slot, err := engines.ConfirmSortSlot(tenantID, req.SlotID, req.Qty, userID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"slot": slot})
}

func handleSortSlotClear(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	var req struct {
		SlotID string `json:"slot_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.SlotID == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'slot_id' is required")
		return
	}
	if err := engines.ClearSortSlot(tenantID, req.SlotID, userID); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "cleared"})
}

func handleSortSlotList(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	station := r.URL.Query().Get("station")
	if station == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Query parameter 'station' is required")
		return
	}
	slots, err := engines.ListSortSlots(tenantID, station)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"slots": slots})
}

// ---------------------------------------------------------------------------
// 42.4.4 - Cartonization v2.
// ---------------------------------------------------------------------------

func handleCartonizationSuggestV2(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	var req struct {
		CartonType string                        `json:"carton_type"`
		Items      []engines.CartonizationItemV2 `json:"items"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.CartonType == "" || len(req.Items) == 0 {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'carton_type' and a non-empty 'items' array are required")
		return
	}
	boxes, err := engines.SuggestCartonizationV2(tenantID, req.CartonType, req.Items)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"boxes": boxes})
}

// ---------------------------------------------------------------------------
// 42.4.5/42.4.6 - Pack template resolution + validated pack completion.
// ---------------------------------------------------------------------------

func handlePackingValidation(w http.ResponseWriter, r *http.Request) {
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
	res, err := engines.GetPackingValidation(tenantID, taskID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(res)
}

func handlePackTemplateResolve(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	sku := r.URL.Query().Get("sku")
	customer := r.URL.Query().Get("customer")
	tmpl, err := engines.ResolvePackTemplate(tenantID, sku, customer)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"template": tmpl})
}

// handlePackCompleteValidated (42.4.6) is the /wms/pack-complete handler -
// additive request fields (weight_kg/documents_confirmed) are only
// consulted when a matched PackingValidationTemplate actually requires them.
func handlePackCompleteValidated(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	var req struct {
		TaskID             string  `json:"task_id"`
		WeightKg           float64 `json:"weight_kg"`
		DocumentsConfirmed bool    `json:"documents_confirmed"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TaskID == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'task_id' is required")
		return
	}
	if err := engines.CompletePackTaskWithValidation(tenantID, req.TaskID, req.WeightKg, req.DocumentsConfirmed, userID); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "packed"})
}

// ---------------------------------------------------------------------------
// 42.4.7 - Deconsolidation.
// ---------------------------------------------------------------------------

func handleDeconsolidateLPN(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	var req struct {
		SourceLPN string `json:"source_lpn"`
		BinCode   string `json:"bin_code"`
		Sku       string `json:"sku"`
		Condition string `json:"condition"`
		Splits    []struct {
			DestLPN string `json:"dest_lpn"`
			Qty     int    `json:"qty"`
		} `json:"splits"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.SourceLPN == "" || req.BinCode == "" || req.Sku == "" || len(req.Splits) == 0 {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'source_lpn', 'bin_code', 'sku' and a non-empty 'splits' array are required")
		return
	}
	splits := make([]engines.DeconsolidationSplit, len(req.Splits))
	for i, s := range req.Splits {
		splits[i] = engines.DeconsolidationSplit{DestLPN: s.DestLPN, Qty: s.Qty}
	}
	if err := engines.DeconsolidateLPN(tenantID, req.SourceLPN, req.BinCode, req.Sku, req.Condition, splits, userID); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "deconsolidated"})
}

// ---------------------------------------------------------------------------
// 42.4.8/42.4.9 - Loading + Bill of Lading.
// ---------------------------------------------------------------------------

func handleLoadingTaskCreate(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	var req struct {
		DockDoor            string `json:"dock_door"`
		TrailerNo           string `json:"trailer_no"`
		ManifestID          string `json:"manifest_id"`
		ExpectedCartonCount int    `json:"expected_carton_count"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.DockDoor == "" || req.TrailerNo == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'dock_door' and 'trailer_no' are required")
		return
	}
	taskID, err := engines.CreateLoadingTask(tenantID, req.DockDoor, req.TrailerNo, req.ManifestID, req.ExpectedCartonCount, userID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"loading_task_id": taskID})
}

func handleLoadingScan(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	var req struct {
		LoadingTaskID string `json:"loading_task_id"`
		PackageCode   string `json:"package_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.LoadingTaskID == "" || req.PackageCode == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'loading_task_id' and 'package_code' are required")
		return
	}
	if err := engines.ScanCartonToTrailer(tenantID, req.LoadingTaskID, req.PackageCode, userID); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "scanned"})
}

func handleLoadingComplete(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	var req struct {
		LoadingTaskID string `json:"loading_task_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.LoadingTaskID == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'loading_task_id' is required")
		return
	}
	if err := engines.CompleteLoadingTask(tenantID, req.LoadingTaskID, userID); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "loaded"})
}

func handleLoadingDepart(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	var req struct {
		LoadingTaskID     string  `json:"loading_task_id"`
		PalletExchangeOut float64 `json:"pallet_exchange_out"`
		PalletExchangeIn  float64 `json:"pallet_exchange_in"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.LoadingTaskID == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'loading_task_id' is required")
		return
	}
	if req.PalletExchangeOut > 0 || req.PalletExchangeIn > 0 {
		if err := engines.RecordPalletExchange(tenantID, req.LoadingTaskID, req.PalletExchangeOut, req.PalletExchangeIn, userID); err != nil {
			writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
			return
		}
	}
	if err := engines.DepartLoadingTask(tenantID, req.LoadingTaskID, userID); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "departed"})
}

// handleBillOfLading (42.4.9) serves the completed load's own record plus
// its carton lines - the frontend renders this through the existing
// print-sheet pattern, no PDF is generated server-side.
func handleBillOfLading(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	loadingTaskID := r.URL.Query().Get("loading_task_id")
	if loadingTaskID == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Query parameter 'loading_task_id' is required")
		return
	}
	task, err := engines.GetLoadingTask(tenantID, loadingTaskID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if task == nil {
		writeAPIErrorGeneric(w, r, http.StatusNotFound, "loading task not found")
		return
	}
	lines, err := engines.BillOfLading(tenantID, loadingTaskID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"loading_task": task, "packages": lines})
}

// ---------------------------------------------------------------------------
// 42.4.11 - VAS.
// ---------------------------------------------------------------------------

func handleVASTaskCreate(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	var req struct {
		LocationCode string  `json:"location_code"`
		FromBin      string  `json:"from_bin"`
		ToBin        string  `json:"to_bin"`
		OutputItem   string  `json:"output_item"`
		OutputQty    float64 `json:"output_qty"`
		BomID        string  `json:"bom_id"`
		Queue        string  `json:"queue"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.LocationCode == "" || req.OutputItem == "" || req.BomID == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'location_code', 'output_item' and 'bom_id' are required")
		return
	}
	taskID, err := engines.CreateVASTask(tenantID, req.LocationCode, req.FromBin, req.ToBin, req.OutputItem, req.OutputQty, req.BomID, req.Queue, userID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"task_id": taskID})
}

func handleVASTaskComplete(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	var req struct {
		TaskID    string `json:"task_id"`
		OutputLPN string `json:"output_lpn"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TaskID == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'task_id' is required")
		return
	}
	if err := engines.CompleteVASTask(tenantID, req.TaskID, req.OutputLPN, userID); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "completed"})
}
