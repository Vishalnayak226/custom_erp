package engines

import (
	"context"
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"log"
	"time"
)

// Stage 37.9: Quality & maintenance - inspection plans, CoA, NCR/CAPA,
// preventive maintenance. Pre-build audit found all four completely absent.
// GRN receiving's own QC (Stage 26.5.2) is a pure quantity split with no
// structured per-item test list, so InspectionPlan/CoA are new ground -
// PostGRNReceiptWithQC itself is deliberately left untouched. Every doctype
// here uses dedicated engine functions with their own explicit transition
// guards (the IntercompanyTransaction/LandedCostVoucher/ServiceTicket shape
// this session), not Stage 29.8's StatusTransitionRule map (scoped to the
// generic document API path).

// ---------------------------------------------------------------------------
// 37.9.1: InspectionPlan - a pure generic-document Master (the Currency/
// Project precedent), no dedicated engine function needed beyond validation.
// ---------------------------------------------------------------------------

func ValidateInspectionPlanDocument(tenantID string, payload map[string]interface{}) error {
	if size, ok := parityNumber(payload["sample_size"]); ok && size <= 0 {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "Sample Size", Message: "sample_size must be greater than zero"}
	}
	return nil
}

// ---------------------------------------------------------------------------
// 37.9.2: CertificateOfAnalysis. Rejection reuses the real, existing
// TransitionBinStockCondition/bin_stock_batch quarantine mechanism (Stage
// 42.1) rather than a new hold flag.
// ---------------------------------------------------------------------------

func fetchCoA(schema, coaID string) (data map[string]interface{}, status string, err error) {
	var dataStr string
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT data, status FROM %s.documents WHERE doctype = 'CertificateOfAnalysis' AND id = $1`, schema), coaID).
		Scan(&dataStr, &status); err != nil {
		return nil, "", fmt.Errorf("certificate of analysis not found: %v", err)
	}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return nil, "", fmt.Errorf("certificate of analysis %s has corrupt stored data: %v", coaID, err)
	}
	return data, status, nil
}

func updateCoA(schema, coaID, status string, data map[string]interface{}) error {
	data["status"] = status
	marshaled, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = $1, status = $2, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'CertificateOfAnalysis' AND id = $3`, schema),
		marshaled, status, coaID)
	return err
}

// coaOverallResult derives Pass/Fail from test_results - a CoA with any
// Fail row is an overall Fail, matching how a real lab certificate works
// (one failed spec fails the whole batch, not an average).
func coaOverallResult(testResults []map[string]interface{}) string {
	for _, r := range testResults {
		if pimString(r["pass_fail"]) == "Fail" {
			return "Fail"
		}
	}
	return "Pass"
}

// CreateCertificateOfAnalysis creates a Draft CoA and computes its overall
// result from the submitted test rows.
func CreateCertificateOfAnalysis(tenantID, batchNo, item, inspectionPlan string, testResults []map[string]interface{}, userID string) (string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	if batchNo == "" {
		return "", fmt.Errorf("batch_no is required")
	}
	if item == "" {
		return "", fmt.Errorf("item is required")
	}
	if len(testResults) == 0 {
		return "", fmt.Errorf("at least one test result is required")
	}
	for _, r := range testResults {
		if pimString(r["pass_fail"]) != "Pass" && pimString(r["pass_fail"]) != "Fail" {
			return "", fmt.Errorf("every test result's pass_fail must be Pass or Fail")
		}
	}

	resultsJSON, err := json.Marshal(testResults)
	if err != nil {
		return "", err
	}
	id := NewDocID("COA")
	docData := map[string]interface{}{
		"id": id, "code": id, "batch_no": batchNo, "item": item, "inspection_plan": inspectionPlan,
		"test_results": string(resultsJSON), "overall_result": coaOverallResult(testResults), "status": "Draft",
	}
	marshaled, err := json.Marshal(docData)
	if err != nil {
		return "", err
	}
	if _, err := db.DB.Exec(fmt.Sprintf(
		`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'CertificateOfAnalysis', $2, 'Draft', $3)`, schema),
		id, marshaled, userID); err != nil {
		return "", err
	}
	return id, nil
}

// QuarantineBatch moves every currently-Good bin holding of batchNo/sku into
// QC-Hold, across every bin it's found in - the real bin_stock_batch/
// TransitionBinStockCondition mechanism (Stage 42.1), not a new hold flag.
// A batch not yet putaway anywhere (binsAffected=0) is not an error - the
// CoA rejection itself is still recorded either way.
func QuarantineBatch(tenantID, sku, batchNo, userID string) (binsAffected int, err error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(
		`SELECT bin_code, qty FROM %s.bin_stock_batch WHERE batch_no = $1 AND sku = $2 AND condition = 'Good' AND qty > 0`, schema),
		batchNo, sku)
	if err != nil {
		return 0, err
	}
	type binQty struct {
		bin string
		qty int
	}
	var holdings []binQty
	for rows.Next() {
		var h binQty
		if err := rows.Scan(&h.bin, &h.qty); err != nil {
			rows.Close()
			return 0, err
		}
		holdings = append(holdings, h)
	}
	rows.Close()

	for _, h := range holdings {
		// Both calls are required together - TransitionBinStockCondition
		// moves the parent bin_stock aggregate, moveBatchStockCondition
		// moves this batch's own share of it. Calling only the first would
		// leave the batch sub-ledger claiming Good stock the bin itself no
		// longer has (moveBatchStockCondition's own doc comment,
		// engines/traceability.go).
		if err := TransitionBinStockCondition(tenantID, h.bin, sku, h.qty, "Good", "QC-Hold", userID); err != nil {
			LogSystemError(tenantID, "", "ERROR", "QuarantineBatch", fmt.Sprintf("batch %s/sku %s: bin %s: %v", batchNo, sku, h.bin, err), "")
			continue
		}
		if err := moveBatchStockCondition(schema, h.bin, sku, batchNo, "Good", "QC-Hold", h.qty); err != nil {
			LogSystemError(tenantID, "", "ERROR", "QuarantineBatch", fmt.Sprintf("batch %s/sku %s: bin %s: bin_stock moved but bin_stock_batch did not: %v", batchNo, sku, h.bin, err), "")
			continue
		}
		binsAffected++
	}
	return binsAffected, nil
}

// ReleaseCertificateOfAnalysis moves Draft -> Released, only when the
// computed overall_result is Pass - a Fail must be explicitly Rejected, not
// silently released.
func ReleaseCertificateOfAnalysis(tenantID, coaID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	data, status, err := fetchCoA(schema, coaID)
	if err != nil {
		return err
	}
	if status != "Draft" {
		return fmt.Errorf("only a Draft certificate of analysis can be released (current status: %s)", status)
	}
	if data["overall_result"] != "Pass" {
		return fmt.Errorf("a Fail certificate of analysis must be Rejected, not Released")
	}
	return updateCoA(schema, coaID, "Released", data)
}

// RejectCertificateOfAnalysis moves Draft -> Rejected and quarantines the
// batch's currently-known holdings.
func RejectCertificateOfAnalysis(tenantID, coaID, userID string) (binsAffected int, err error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, err
	}
	data, status, err := fetchCoA(schema, coaID)
	if err != nil {
		return 0, err
	}
	if status != "Draft" {
		return 0, fmt.Errorf("only a Draft certificate of analysis can be rejected (current status: %s)", status)
	}
	sku, _ := data["item"].(string)
	batchNo, _ := data["batch_no"].(string)
	binsAffected, err = QuarantineBatch(tenantID, sku, batchNo, userID)
	if err != nil {
		return 0, err
	}
	if err := updateCoA(schema, coaID, "Rejected", data); err != nil {
		return binsAffected, err
	}
	return binsAffected, nil
}

// ---------------------------------------------------------------------------
// 37.9.3: NonConformanceReport. root_cause_reason_code reuses the existing
// ReasonCode mechanism (a new "Quality" category value) rather than a
// second classification system.
// ---------------------------------------------------------------------------

func fetchNCR(schema, ncrID string) (data map[string]interface{}, status string, err error) {
	var dataStr string
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT data, status FROM %s.documents WHERE doctype = 'NonConformanceReport' AND id = $1`, schema), ncrID).
		Scan(&dataStr, &status); err != nil {
		return nil, "", fmt.Errorf("non-conformance report not found: %v", err)
	}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return nil, "", fmt.Errorf("non-conformance report %s has corrupt stored data: %v", ncrID, err)
	}
	return data, status, nil
}

func CreateNonConformanceReport(tenantID, description, sourceDoctype, sourceID, userID string) (string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	if description == "" {
		return "", fmt.Errorf("description is required")
	}
	id := NewDocID("NCR")
	docData := map[string]interface{}{
		"id": id, "code": id, "description": description,
		"source_doctype": sourceDoctype, "source_id": sourceID, "status": "Draft",
	}
	marshaled, err := json.Marshal(docData)
	if err != nil {
		return "", err
	}
	if _, err := db.DB.Exec(fmt.Sprintf(
		`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'NonConformanceReport', $2, 'Draft', $3)`, schema),
		id, marshaled, userID); err != nil {
		return "", err
	}
	return id, nil
}

// InvestigateNonConformanceReport moves Draft -> Investigating and records
// the root cause, validated against the existing ReasonCode "Quality"
// category - the same append-only reuse every prior stage's reason-code
// use already follows, not a second classification system.
func InvestigateNonConformanceReport(tenantID, ncrID, rootCauseReasonCode string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	if rootCauseReasonCode == "" {
		return fmt.Errorf("root_cause_reason_code is required to begin an investigation")
	}
	if err := requireActiveReasonCode(tenantID, rootCauseReasonCode, "Quality"); err != nil {
		return err
	}
	data, status, err := fetchNCR(schema, ncrID)
	if err != nil {
		return err
	}
	if status != "Draft" {
		return fmt.Errorf("only a Draft NCR can move to Investigating (current status: %s)", status)
	}
	data["root_cause_reason_code"] = rootCauseReasonCode
	data["status"] = "Investigating"
	marshaled, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = $1, status = 'Investigating', updated_at = CURRENT_TIMESTAMP WHERE doctype = 'NonConformanceReport' AND id = $2`, schema),
		marshaled, ncrID)
	return err
}

// PlanCorrectiveAction moves Investigating -> CorrectiveActionPlanned,
// requiring the CAPA text itself - this IS the "CAPA" half of NCR/CAPA.
func PlanCorrectiveAction(tenantID, ncrID, correctiveAction string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	if correctiveAction == "" {
		return fmt.Errorf("corrective_action is required")
	}
	data, status, err := fetchNCR(schema, ncrID)
	if err != nil {
		return err
	}
	if status != "Investigating" {
		return fmt.Errorf("only an Investigating NCR can have a corrective action planned (current status: %s)", status)
	}
	data["corrective_action"] = correctiveAction
	data["status"] = "CorrectiveActionPlanned"
	marshaled, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = $1, status = 'CorrectiveActionPlanned', updated_at = CURRENT_TIMESTAMP WHERE doctype = 'NonConformanceReport' AND id = $2`, schema),
		marshaled, ncrID)
	return err
}

// CloseNonConformanceReport moves CorrectiveActionPlanned -> Closed.
func CloseNonConformanceReport(tenantID, ncrID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	data, status, err := fetchNCR(schema, ncrID)
	if err != nil {
		return err
	}
	if status != "CorrectiveActionPlanned" {
		return fmt.Errorf("only an NCR with a planned corrective action can be closed (current status: %s)", status)
	}
	data["status"] = "Closed"
	marshaled, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = $1, status = 'Closed', updated_at = CURRENT_TIMESTAMP WHERE doctype = 'NonConformanceReport' AND id = $2`, schema),
		marshaled, ncrID)
	return err
}

// ---------------------------------------------------------------------------
// 37.9.4: Preventive maintenance. MaintenanceSchedule (the recurring
// definition, the RecurringSalesContract shape, Stage 37.6) + MaintenanceOrder
// (what gets spawned each cycle - a real trackable work item, chosen over a
// dunning-style notify-only scan because preventive maintenance needs an
// actionable, closeable record).
// ---------------------------------------------------------------------------

func ValidateMaintenanceScheduleDocument(tenantID string, payload map[string]interface{}) error {
	if days, ok := parityNumber(payload["interval_days"]); ok && days <= 0 {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "Interval (days)", Message: "interval_days must be greater than zero"}
	}
	return nil
}

func fetchMaintenanceOrder(schema, orderID string) (data map[string]interface{}, status string, err error) {
	var dataStr string
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT data, status FROM %s.documents WHERE doctype = 'MaintenanceOrder' AND id = $1`, schema), orderID).
		Scan(&dataStr, &status); err != nil {
		return nil, "", fmt.Errorf("maintenance order not found: %v", err)
	}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return nil, "", fmt.Errorf("maintenance order %s has corrupt stored data: %v", orderID, err)
	}
	return data, status, nil
}

func updateMaintenanceOrder(schema, orderID, status string, data map[string]interface{}) error {
	data["status"] = status
	marshaled, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = $1, status = $2, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'MaintenanceOrder' AND id = $3`, schema),
		marshaled, status, orderID)
	return err
}

// StartMaintenanceOrder moves Scheduled -> InProgress.
func StartMaintenanceOrder(tenantID, orderID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	data, status, err := fetchMaintenanceOrder(schema, orderID)
	if err != nil {
		return err
	}
	if status != "Scheduled" {
		return fmt.Errorf("only a Scheduled maintenance order can be started (current status: %s)", status)
	}
	return updateMaintenanceOrder(schema, orderID, "InProgress", data)
}

// CompleteMaintenanceOrder moves InProgress -> Completed, requiring
// completion notes.
func CompleteMaintenanceOrder(tenantID, orderID, completionNotes string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	if completionNotes == "" {
		return fmt.Errorf("completion_notes is required to complete a maintenance order")
	}
	data, status, err := fetchMaintenanceOrder(schema, orderID)
	if err != nil {
		return err
	}
	if status != "InProgress" {
		return fmt.Errorf("only an InProgress maintenance order can be completed (current status: %s)", status)
	}
	data["completion_notes"] = completionNotes
	return updateMaintenanceOrder(schema, orderID, "Completed", data)
}

// CancelMaintenanceOrder refuses on an already-terminal order.
func CancelMaintenanceOrder(tenantID, orderID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	data, status, err := fetchMaintenanceOrder(schema, orderID)
	if err != nil {
		return err
	}
	if status == "Completed" || status == "Cancelled" {
		return fmt.Errorf("maintenance order %s is already %s", orderID, status)
	}
	return updateMaintenanceOrder(schema, orderID, "Cancelled", data)
}

// runMaintenanceSchedulingForSchema mirrors runRecurringBillingForSchema
// (engines/deferred_prepaid.go) exactly: scan Active schedules due today or
// earlier, spawn one Scheduled MaintenanceOrder each, advance next_due_date
// by interval_days.
func runMaintenanceSchedulingForSchema(schema string) {
	rows, err := db.DB.Query(fmt.Sprintf(
		`SELECT id, data FROM %s.documents WHERE doctype = 'MaintenanceSchedule' AND status = 'Active' AND deleted_at IS NULL`, schema))
	if err != nil {
		log.Printf("[MAINTENANCE-SCHEDULE] Failed to list schedules in schema %s: %v", schema, err)
		return
	}
	type row struct{ id, data string }
	var schedules []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.data); err == nil {
			schedules = append(schedules, r)
		}
	}
	rows.Close()

	today := time.Now().Format("2006-01-02")
	for _, s := range schedules {
		var data map[string]interface{}
		if err := json.Unmarshal([]byte(s.data), &data); err != nil {
			log.Printf("[MAINTENANCE-SCHEDULE] Skipping corrupt schedule %s in schema %s: %v", s.id, schema, err)
			continue
		}
		nextDueDate, _ := data["next_due_date"].(string)
		intervalDays := int(numFromInterface(data["interval_days"]))
		if nextDueDate == "" || intervalDays <= 0 || nextDueDate > today {
			continue
		}
		asset, _ := data["asset"].(string)
		description, _ := data["description"].(string)

		orderID := NewDocID("MO")
		orderData := map[string]interface{}{
			"id": orderID, "code": orderID, "asset": asset, "description": description,
			"maintenance_schedule_id": s.id, "scheduled_date": nextDueDate, "status": "Scheduled",
		}
		marshaled, err := json.Marshal(orderData)
		if err != nil {
			log.Printf("[MAINTENANCE-SCHEDULE] Failed to marshal order for schedule %s: %v", s.id, err)
			continue
		}
		if _, err := db.DB.Exec(fmt.Sprintf(
			`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'MaintenanceOrder', $2, 'Scheduled', 'system')`, schema),
			orderID, marshaled); err != nil {
			log.Printf("[MAINTENANCE-SCHEDULE] Failed to create order for schedule %s: %v", s.id, err)
			continue
		}

		newNextDueDate, err := time.Parse("2006-01-02", nextDueDate)
		if err != nil {
			log.Printf("[MAINTENANCE-SCHEDULE] Failed to parse next_due_date for schedule %s: %v", s.id, err)
			continue
		}
		newNextDueDate = newNextDueDate.AddDate(0, 0, intervalDays)
		if _, err := db.DB.Exec(fmt.Sprintf(
			`UPDATE %s.documents SET data = jsonb_set(data, '{next_due_date}', to_jsonb($1::text)), updated_at = CURRENT_TIMESTAMP WHERE doctype = 'MaintenanceSchedule' AND id = $2`, schema),
			newNextDueDate.Format("2006-01-02"), s.id); err != nil {
			log.Printf("[MAINTENANCE-SCHEDULE] Failed to advance schedule %s to %s: %v", s.id, newNextDueDate.Format("2006-01-02"), err)
		}
	}
}

// StartMaintenanceSchedulingWorker mirrors StartRecurringBillingWorker
// exactly (engines/deferred_prepaid.go).
func StartMaintenanceSchedulingWorker(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if db.DB == nil {
					continue
				}
				schemas, err := listTenantSchemas()
				if err != nil {
					log.Printf("[MAINTENANCE-SCHEDULE] Failed to list tenant schemas: %v", err)
					continue
				}
				for _, schema := range schemas {
					runMaintenanceSchedulingForSchema(schema)
				}
			}
		}
	}()
}
