package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// Stage 42.4.8/42.4.9 - Loading: LoadingTask against a DockDoor (42.3.1) +
// Trailer (42.3.4), scan-verified carton-to-trailer, feeding the existing
// Manifest (engines/marketplace.go's GenerateManifest/HandoverManifest,
// untouched here). The Bill of Lading (42.4.9) is assembled read-only from a
// completed LoadingTask and rendered client-side through the existing
// browser print-sheet path - no new stored doctype, no new renderer, per
// the plan's own instruction.
//
// Deliberately does NOT modify HandoverManifest to require a LoadingTask -
// that would be a real behaviour change to a well-tested, unrelated-team
// choke point every existing Manifest workflow already depends on.
// LoadingTask is additive: the recommended new step before a warehouse
// calls HandoverManifest, not a hard gate in front of it.

// LoadingTaskInfo is one LoadingTask document, flattened.
type LoadingTaskInfo struct {
	DocID               string  `json:"doc_id"`
	DockDoor            string  `json:"dock_door"`
	TrailerNo           string  `json:"trailer_no"`
	ManifestID          string  `json:"manifest_id,omitempty"`
	ExpectedCartonCount int     `json:"expected_carton_count,omitempty"`
	ScannedCartonCount  int     `json:"scanned_carton_count"`
	PalletExchangeOut   float64 `json:"pallet_exchange_out,omitempty"`
	PalletExchangeIn    float64 `json:"pallet_exchange_in,omitempty"`
	Status              string  `json:"status"`
	LoadedBy            string  `json:"loaded_by,omitempty"`
	Notes               string  `json:"notes,omitempty"`
}

// CreateLoadingTask (42.4.8) opens a new load against a dock door + trailer,
// optionally pre-linked to a Manifest (GenerateManifest's own output id) so
// ScanCartonToTrailer can verify each scanned package actually belongs to
// this load. expectedCartonCount, when the caller supplies it (e.g. read
// from the Manifest's own shipment_count), is purely informational until
// EvaluatePreShipGate's require_all_cartons_scanned check reads it.
func CreateLoadingTask(tenantID, dockDoor, trailerNo, manifestID string, expectedCartonCount int, userID string) (string, error) {
	if dockDoor == "" || trailerNo == "" {
		return "", errors.New("dock_door and trailer_no are required")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	taskID := NewDocID("LOAD")
	data := map[string]interface{}{
		"code": taskID, "dock_door": dockDoor, "trailer_no": trailerNo, "manifest_id": manifestID,
		"expected_carton_count": expectedCartonCount, "scanned_carton_count": 0, "status": "Planned",
	}
	payload, _ := json.Marshal(data)
	if _, err := db.DB.Exec(fmt.Sprintf(
		`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'LoadingTask', $2, 'Planned', $3)`, schema),
		taskID, payload, userID); err != nil {
		return "", err
	}
	LogAuditEvent(tenantID, userID, "WMS_LOADING_TASK_CREATE", "SUCCESS", fmt.Sprintf("Opened loading task %s: door %s, trailer %s", taskID, dockDoor, trailerNo))
	return taskID, nil
}

func getLoadingTask(schema, taskID string) (*LoadingTaskInfo, error) {
	var dataStr, status string
	err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT data, COALESCE(status, '') FROM %s.documents WHERE doctype = 'LoadingTask' AND id = $1`, schema), taskID).Scan(&dataStr, &status)
	if err == sql.ErrNoRows {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	var data map[string]interface{}
	_ = json.Unmarshal([]byte(dataStr), &data)
	info := &LoadingTaskInfo{DocID: taskID, Status: status}
	info.DockDoor = strField(data, "dock_door")
	info.TrailerNo = strField(data, "trailer_no")
	info.ManifestID = strField(data, "manifest_id")
	info.ExpectedCartonCount = int(numFromInterface(data["expected_carton_count"]))
	info.ScannedCartonCount = int(numFromInterface(data["scanned_carton_count"]))
	info.PalletExchangeOut = numFromInterface(data["pallet_exchange_out"])
	info.PalletExchangeIn = numFromInterface(data["pallet_exchange_in"])
	info.LoadedBy = strField(data, "loaded_by")
	info.Notes = strField(data, "notes")
	return info, nil
}

// GetLoadingTask is the read path handlers/BOL assembly use.
func GetLoadingTask(tenantID, taskID string) (*LoadingTaskInfo, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	return getLoadingTask(schema, taskID)
}

// ScanCartonToTrailer (42.4.8) records one ShippingPackage as physically
// loaded - refuses a package not Invoiced (i.e. still Draft or already
// Shipped/Cancelled) and, when the task carries a manifest_id, refuses a
// package whose own LogisticsBooking is not manifested under that same
// Manifest, so a scan cannot load the wrong shipment onto this trailer.
// First scan flips the task Planned -> Loading.
func ScanCartonToTrailer(tenantID, loadingTaskID, packageCode, userID string) error {
	if packageCode == "" {
		return errors.New("package_code is required")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	task, err := getLoadingTask(schema, loadingTaskID)
	if err != nil {
		return err
	}
	if task == nil {
		return fmt.Errorf("loading task %s not found", loadingTaskID)
	}
	if task.Status != "Planned" && task.Status != "Loading" {
		return fmt.Errorf("loading task %s is %s - cannot scan cartons", loadingTaskID, task.Status)
	}

	var pkgStatus string
	var alreadyLoaded sql.NullString
	err = db.DB.QueryRow(fmt.Sprintf(
		`SELECT status, data->>'loaded_at' FROM %s.documents WHERE doctype = 'ShippingPackage' AND id = $1 AND deleted_at IS NULL`, schema),
		packageCode).Scan(&pkgStatus, &alreadyLoaded)
	if err == sql.ErrNoRows {
		return fmt.Errorf("shipping package %s not found", packageCode)
	} else if err != nil {
		return err
	}
	if pkgStatus != "Invoiced" {
		return fmt.Errorf("package %s is %s, not Invoiced - it is not ready to load", packageCode, pkgStatus)
	}
	if alreadyLoaded.Valid && alreadyLoaded.String != "" {
		return fmt.Errorf("package %s has already been loaded", packageCode)
	}
	if task.ManifestID != "" {
		var bookingManifest string
		err = db.DB.QueryRow(fmt.Sprintf(
			`SELECT COALESCE(data->>'manifest_id', '') FROM %s.documents WHERE doctype = 'LogisticsBooking' AND data->>'shipping_package_id' = $1`, schema),
			packageCode).Scan(&bookingManifest)
		if err == nil && bookingManifest != "" && bookingManifest != task.ManifestID {
			return fmt.Errorf("package %s belongs to manifest %s, not this load's manifest %s", packageCode, bookingManifest, task.ManifestID)
		}
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := db.SetSearchPath(tx, schema); err != nil {
		return err
	}
	if _, err := tx.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = data || jsonb_build_object('loaded_at', CURRENT_TIMESTAMP::text, 'loading_task_id', $1::text) WHERE doctype = 'ShippingPackage' AND id = $2`, schema),
		loadingTaskID, packageCode); err != nil {
		return err
	}
	newStatus := "Loading"
	if _, err := tx.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = data || jsonb_build_object('scanned_carton_count', COALESCE((data->>'scanned_carton_count')::int, 0) + 1, 'status', $1::text), status = $1, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'LoadingTask' AND id = $2`, schema),
		newStatus, loadingTaskID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	LogAuditEvent(tenantID, userID, "WMS_LOADING_SCAN", "SUCCESS", fmt.Sprintf("Loaded package %s onto task %s", packageCode, loadingTaskID))
	return nil
}

// CompleteLoadingTask (42.4.8, and 42.4.10's final gate) runs
// EvaluatePreShipGate before moving the task Loading -> Loaded. Departed is
// a separate, later manual step (the trailer physically leaves the yard),
// not implied by completing the load.
func CompleteLoadingTask(tenantID, loadingTaskID, userID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	task, err := getLoadingTask(schema, loadingTaskID)
	if err != nil {
		return err
	}
	if task == nil {
		return fmt.Errorf("loading task %s not found", loadingTaskID)
	}
	if task.Status != "Loading" {
		return fmt.Errorf("loading task %s is %s, not Loading", loadingTaskID, task.Status)
	}
	if err := EvaluatePreShipGate(tenantID, task); err != nil {
		return err
	}
	if _, err := db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = data || jsonb_build_object('status', 'Loaded', 'loaded_by', $1::text), status = 'Loaded', updated_at = CURRENT_TIMESTAMP WHERE doctype = 'LoadingTask' AND id = $2`, schema),
		userID, loadingTaskID); err != nil {
		return err
	}
	LogAuditEvent(tenantID, userID, "WMS_LOADING_COMPLETE", "SUCCESS", fmt.Sprintf("Loading task %s completed (%d cartons)", loadingTaskID, task.ScannedCartonCount))
	LogCompletedWarehouseTask(tenantID, NewWarehouseTask{
		TaskType: "Load", LocationCode: task.DockDoor, Qty: float64(task.ScannedCartonCount),
		SourceDocType: "LoadingTask", SourceDocID: loadingTaskID,
	}, userID)
	return nil
}

// DepartLoadingTask marks the trailer as having left the dock - purely a
// status/timestamp record, no further gate.
func DepartLoadingTask(tenantID, loadingTaskID, userID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	task, err := getLoadingTask(schema, loadingTaskID)
	if err != nil {
		return err
	}
	if task == nil {
		return fmt.Errorf("loading task %s not found", loadingTaskID)
	}
	if task.Status != "Loaded" {
		return fmt.Errorf("loading task %s is %s, not Loaded", loadingTaskID, task.Status)
	}
	if _, err := db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = data || jsonb_build_object('status', 'Departed'), status = 'Departed', updated_at = CURRENT_TIMESTAMP WHERE doctype = 'LoadingTask' AND id = $1`, schema),
		loadingTaskID); err != nil {
		return err
	}
	LogAuditEvent(tenantID, userID, "WMS_LOADING_DEPART", "SUCCESS", fmt.Sprintf("Loading task %s departed", loadingTaskID))
	return nil
}

// RecordPalletExchange (42.4.9) sets the pallets-given/received counters on
// a load - a plain field patch, kept as its own small function so a manager
// can record the exchange at handover without re-running the whole loading
// flow.
func RecordPalletExchange(tenantID, loadingTaskID string, out, in float64, userID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	if _, err := db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = data || jsonb_build_object('pallet_exchange_out', $1::numeric, 'pallet_exchange_in', $2::numeric), updated_at = CURRENT_TIMESTAMP WHERE doctype = 'LoadingTask' AND id = $3`, schema),
		out, in, loadingTaskID); err != nil {
		return err
	}
	LogAuditEvent(tenantID, userID, "WMS_PALLET_EXCHANGE", "SUCCESS", fmt.Sprintf("Loading task %s pallet exchange recorded: %.0f out / %.0f in", loadingTaskID, out, in))
	return nil
}

// BOLPackageLine is one carton on the Bill of Lading.
type BOLPackageLine struct {
	PackageCode string  `json:"package_code"`
	OrderID     string  `json:"order_id,omitempty"`
	WeightKg    float64 `json:"weight_kg,omitempty"`
}

// BillOfLading (42.4.9) is assembled read-only from a completed LoadingTask
// - no stored document, rendered client-side through the existing
// print-sheet path. GetLoadingTask is called by the handler separately; this
// only returns the carton lines.
func BillOfLading(tenantID, loadingTaskID string) ([]BOLPackageLine, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, COALESCE(data->>'order_id', ''), COALESCE(NULLIF(data->>'weight_kg', '')::numeric, 0)
		FROM %s.documents WHERE doctype = 'ShippingPackage' AND data->>'loading_task_id' = $1
		ORDER BY id`, schema), loadingTaskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BOLPackageLine{}
	for rows.Next() {
		var l BOLPackageLine
		if err := rows.Scan(&l.PackageCode, &l.OrderID, &l.WeightKg); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}
