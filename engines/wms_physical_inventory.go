package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Stage 42.5.1: Physical inventory (full/annual count). Distinct from
// 26.5.9's GetABCCycleCountPlan (an ongoing, velocity-sampled spot check),
// this counts EVERY SKU on hand at a location - or, scoped to a zone, every
// SKU physically binned within it - and freezes the bins involved for the
// duration so a putaway or pick can't change what's being counted out from
// under the counters. It is deliberately NOT a second counting/
// reconciliation mechanism: every line StartPhysicalInventory creates is a
// plain CycleCountLine, and reconciliation/posting/approval is
// ReconcileCycleCount/PostCycleCountAdjustment exactly as they already work
// (see engines/wms.go, engines/wms_pack_count.go) - a PhysicalInventory
// header is only a governed way to create, freeze for, and close out a large
// batch of them together.
//
// The freeze reuses 42.2.6's existing bin_status='Counting' value rather
// than inventing a second freeze flag: PutawayToBin and the directed-putaway
// candidate query already refuse a Counting bin, and this Stage extends that
// same check into picking allocation (engines/traceability.go's
// allocateByOrder/allocateFEFO) so a bin frozen for a physical count is
// unavailable on both sides of the operation, not just inbound.

var errPhysicalInventoryNotFound = errors.New("physical inventory not found")

type frozenBin struct {
	BinCode     string `json:"bin_code"`
	PriorStatus string `json:"prior_status"`
}

// StartPhysicalInventory (42.5.1) freezes every Active bin at locationCode
// (or, if zoneCode is set, every Active bin in that zone) by setting its
// bin_status to 'Counting', then creates one Draft CycleCountLine per SKU on
// hand in scope, grouped under a fresh PhysicalInventory header's id as
// their count_session. Refuses to start if any bin in scope is already
// Counting (an overlapping physical inventory already has it frozen) or if
// there is nothing on hand to count.
func StartPhysicalInventory(tenantID, locationCode, zoneCode, userID string) (piID string, lineCount int, err error) {
	locationCode = strings.TrimSpace(locationCode)
	zoneCode = strings.TrimSpace(zoneCode)
	if locationCode == "" {
		return "", 0, errors.New("location is required")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", 0, err
	}

	var locStatus string
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT status FROM %s.documents WHERE doctype = 'Location' AND id = $1`, schema), locationCode).
		Scan(&locStatus); err != nil {
		if err == sql.ErrNoRows {
			return "", 0, fmt.Errorf("location %s not found", locationCode)
		}
		return "", 0, err
	}
	if locStatus != "Active" {
		return "", 0, fmt.Errorf("location %s is not Active", locationCode)
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return "", 0, err
	}
	defer tx.Rollback()
	if err := db.SetSearchPath(tx, schema); err != nil {
		return "", 0, err
	}

	binQuery := fmt.Sprintf(`
		SELECT data->>'bin_code', COALESCE(data->>'bin_status', '')
		FROM %s.documents WHERE doctype = 'Bin' AND status = 'Active' AND data->>'location' = $1`, schema)
	var binRows *sql.Rows
	if zoneCode != "" {
		binQuery += ` AND data->>'zone' = $2`
		binRows, err = tx.Query(binQuery, locationCode, zoneCode)
	} else {
		binRows, err = tx.Query(binQuery, locationCode)
	}
	if err != nil {
		return "", 0, err
	}
	var frozen []frozenBin
	for binRows.Next() {
		var code, status string
		if err := binRows.Scan(&code, &status); err != nil {
			binRows.Close()
			return "", 0, err
		}
		if status == "Counting" {
			binRows.Close()
			return "", 0, fmt.Errorf("bin %s is already frozen for another physical inventory", code)
		}
		frozen = append(frozen, frozenBin{BinCode: code, PriorStatus: status})
	}
	binRows.Close()
	if err := binRows.Err(); err != nil {
		return "", 0, err
	}

	var skus []string
	if zoneCode == "" {
		skuRows, err := tx.Query(fmt.Sprintf(
			`SELECT sku FROM %s.inventory_availability WHERE location_code = $1 AND on_hand > 0`, schema), locationCode)
		if err != nil {
			return "", 0, err
		}
		for skuRows.Next() {
			var sku string
			if err := skuRows.Scan(&sku); err != nil {
				skuRows.Close()
				return "", 0, err
			}
			skus = append(skus, sku)
		}
		skuRows.Close()
		if err := skuRows.Err(); err != nil {
			return "", 0, err
		}
	} else {
		skuRows, err := tx.Query(fmt.Sprintf(`
			SELECT DISTINCT bs.sku FROM %s.bin_stock bs
			JOIN %s.documents b ON b.doctype = 'Bin' AND b.data->>'bin_code' = bs.bin_code
			WHERE bs.location_code = $1 AND b.data->>'zone' = $2 AND bs.qty > 0`, schema, schema),
			locationCode, zoneCode)
		if err != nil {
			return "", 0, err
		}
		for skuRows.Next() {
			var sku string
			if err := skuRows.Scan(&sku); err != nil {
				skuRows.Close()
				return "", 0, err
			}
			skus = append(skus, sku)
		}
		skuRows.Close()
		if err := skuRows.Err(); err != nil {
			return "", 0, err
		}
	}
	if len(skus) == 0 {
		return "", 0, fmt.Errorf("no on-hand stock to count at location %s%s", locationCode, func() string {
			if zoneCode != "" {
				return " zone " + zoneCode
			}
			return ""
		}())
	}

	// Best-effort classification tag - a failure here must never block
	// starting the count itself, only skip the (purely informational)
	// cycle_class tag on each line.
	classBySku := map[string]string{}
	if plan, perr := GetCycleCountPlan(tenantID, locationCode); perr == nil {
		for _, s := range plan {
			classBySku[s.Sku] = s.Tier
		}
	}

	for _, fb := range frozen {
		if _, err := tx.Exec(fmt.Sprintf(
			`UPDATE %s.documents SET data = data || '{"bin_status": "Counting"}'::jsonb, updated_at = CURRENT_TIMESTAMP
			 WHERE doctype = 'Bin' AND data->>'bin_code' = $1`, schema), fb.BinCode); err != nil {
			return "", 0, err
		}
	}

	piID = NewDocID("PI")
	frozenJSON, _ := json.Marshal(frozen)
	now := time.Now().UTC().Format(time.RFC3339)
	headerData := map[string]interface{}{
		"code": piID, "location": locationCode, "zone": zoneCode, "status": "Counting",
		"line_count": len(skus), "started_at": now, "frozen_bins": json.RawMessage(frozenJSON),
	}
	headerMarshaled, _ := json.Marshal(headerData)
	if _, err := tx.Exec(fmt.Sprintf(
		`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'PhysicalInventory', $2, 'Counting', $3)`, schema),
		piID, headerMarshaled, userID); err != nil {
		return "", 0, err
	}

	for _, sku := range skus {
		lineID := NewDocIDCompact("CCL")
		lineData := map[string]interface{}{
			"id": lineID, "count_session": piID, "physical_inventory": piID,
			"location": locationCode, "sku": sku, "status": "Draft",
		}
		if cls, ok := classBySku[sku]; ok {
			lineData["cycle_class"] = cls
		}
		lineMarshaled, _ := json.Marshal(lineData)
		if _, err := tx.Exec(fmt.Sprintf(
			`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'CycleCountLine', $2, 'Draft', $3)`, schema),
			lineID, lineMarshaled, userID); err != nil {
			return "", 0, err
		}
	}

	if err := tx.Commit(); err != nil {
		return "", 0, err
	}
	LogAuditEvent(tenantID, userID, "WMS_PHYSICAL_INVENTORY_START", "SUCCESS",
		fmt.Sprintf("Started physical inventory %s at %s%s - %d bin(s) frozen, %d line(s) created", piID, locationCode, zoneCode, len(frozen), len(skus)))
	return piID, len(skus), nil
}

// SubmitPhysicalInventoryCount (42.5.1) lets a counter record their counted
// qty against a line StartPhysicalInventory created - the only way to fill
// in counted_qty on such a line, mirroring 26.5.10's SubmitRecountValue
// (CycleCountLine.allow_update stays FALSE for the same reason there).
func SubmitPhysicalInventoryCount(tenantID, lineID string, countedQty float64, userID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var status, dataStr string
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT status, data FROM %s.documents WHERE doctype = 'CycleCountLine' AND id = $1`, schema), lineID).
		Scan(&status, &dataStr); err != nil {
		return fmt.Errorf("cycle count line not found: %v", err)
	}
	if status != "Draft" {
		return fmt.Errorf("line %s is %s, not Draft - cannot record a count", lineID, status)
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return err
	}
	if pi, _ := data["physical_inventory"].(string); pi == "" {
		return fmt.Errorf("line %s is not a physical inventory line", lineID)
	}
	data["counted_qty"] = countedQty
	marshaled, _ := json.Marshal(data)
	_, err = db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2`, schema),
		marshaled, lineID)
	return err
}

func getPhysicalInventory(schema, piID string) (map[string]interface{}, string, error) {
	var status, dataStr string
	err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT status, data FROM %s.documents WHERE doctype = 'PhysicalInventory' AND id = $1`, schema), piID).
		Scan(&status, &dataStr)
	if err == sql.ErrNoRows {
		return nil, "", errPhysicalInventoryNotFound
	} else if err != nil {
		return nil, "", err
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return nil, "", err
	}
	return data, status, nil
}

// ReconcilePhysicalInventory (42.5.1) runs the existing ReconcileCycleCount
// against this PhysicalInventory's count_session (its own id, by
// construction - see StartPhysicalInventory), then advances the header from
// Counting to Reconciling. Safe to call more than once, exactly like
// ReconcileCycleCount itself - a repeat call only picks up lines that were
// still Draft (e.g. counted after the first pass, or a RequestRecount line).
func ReconcilePhysicalInventory(tenantID, piID, actorUserID, actorRole string) (posted, pendingApproval int, err error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, 0, err
	}
	_, status, err := getPhysicalInventory(schema, piID)
	if err != nil {
		return 0, 0, err
	}
	if status == "Closed" || status == "Cancelled" {
		return 0, 0, fmt.Errorf("physical inventory %s is %s - cannot reconcile", piID, status)
	}

	posted, pendingApproval, err = ReconcileCycleCount(tenantID, piID, actorUserID, actorRole)
	if err != nil {
		return posted, pendingApproval, err
	}
	if status == "Counting" {
		if _, err := db.DB.Exec(fmt.Sprintf(
			`UPDATE %s.documents SET data = data || '{"status": "Reconciling"}'::jsonb, status = 'Reconciling', updated_at = CURRENT_TIMESTAMP
			 WHERE doctype = 'PhysicalInventory' AND id = $1`, schema), piID); err != nil {
			return posted, pendingApproval, err
		}
	}
	return posted, pendingApproval, nil
}

// ClosePhysicalInventory (42.5.1) unfreezes every bin StartPhysicalInventory
// froze (restoring each one's exact prior bin_status, not a blanket
// 'Available' - a bin that was already Blocked for an unrelated reason
// before the count started goes back to Blocked, not to falsely-available)
// and marks the header Closed. Refuses while any of its CycleCountLine rows
// have not reached Posted - a physical inventory that "closes" over
// still-open lines would silently abandon them with no further trigger to
// finish reconciling.
func ClosePhysicalInventory(tenantID, piID, userID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	data, status, err := getPhysicalInventory(schema, piID)
	if err != nil {
		return err
	}
	if status == "Closed" {
		return fmt.Errorf("physical inventory %s is already Closed", piID)
	}
	if status == "Cancelled" {
		return fmt.Errorf("physical inventory %s was Cancelled", piID)
	}

	var openCount int
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT COUNT(*) FROM %s.documents WHERE doctype = 'CycleCountLine' AND data->>'count_session' = $1 AND status != 'Posted'`, schema),
		piID).Scan(&openCount); err != nil {
		return err
	}
	if openCount > 0 {
		return fmt.Errorf("physical inventory %s still has %d line(s) not yet Posted", piID, openCount)
	}

	if err := unfreezePhysicalInventoryBins(schema, data); err != nil {
		return err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = data || jsonb_build_object('status', 'Closed'::text, 'closed_at', $1::text), status = 'Closed', updated_at = CURRENT_TIMESTAMP
		 WHERE doctype = 'PhysicalInventory' AND id = $2`, schema), now, piID); err != nil {
		return err
	}
	LogAuditEvent(tenantID, userID, "WMS_PHYSICAL_INVENTORY_CLOSE", "SUCCESS", fmt.Sprintf("Closed physical inventory %s", piID))
	return nil
}

// CancelPhysicalInventory (42.5.1) is the escape hatch for a count started
// in error or abandoned mid-way: unfreezes its bins immediately and marks it
// Cancelled without requiring every line to be Posted first. Lines already
// created are left exactly as they are (Draft/Posted/whatever they reached)
// for the audit trail - only the header's status and the bin freeze change.
func CancelPhysicalInventory(tenantID, piID, userID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	data, status, err := getPhysicalInventory(schema, piID)
	if err != nil {
		return err
	}
	if status == "Closed" || status == "Cancelled" {
		return fmt.Errorf("physical inventory %s is already %s", piID, status)
	}

	if err := unfreezePhysicalInventoryBins(schema, data); err != nil {
		return err
	}
	if _, err := db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = data || '{"status": "Cancelled"}'::jsonb, status = 'Cancelled', updated_at = CURRENT_TIMESTAMP
		 WHERE doctype = 'PhysicalInventory' AND id = $1`, schema), piID); err != nil {
		return err
	}
	LogAuditEvent(tenantID, userID, "WMS_PHYSICAL_INVENTORY_CANCEL", "SUCCESS", fmt.Sprintf("Cancelled physical inventory %s", piID))
	return nil
}

func unfreezePhysicalInventoryBins(schema string, headerData map[string]interface{}) error {
	raw, ok := headerData["frozen_bins"]
	if !ok || raw == nil {
		return nil
	}
	// headerData came through json.Unmarshal into map[string]interface{}, so
	// frozen_bins is a []interface{} of map[string]interface{}, not the
	// []frozenBin it was marshaled from - round-trip through JSON once to
	// get back a typed slice rather than hand-walking the interface{}.
	rawJSON, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	var frozen []frozenBin
	if err := json.Unmarshal(rawJSON, &frozen); err != nil {
		return err
	}
	for _, fb := range frozen {
		prior := fb.PriorStatus
		if prior == "" {
			prior = "Available"
		}
		if _, err := db.DB.Exec(fmt.Sprintf(
			`UPDATE %s.documents SET data = data || jsonb_build_object('bin_status', $1::text), updated_at = CURRENT_TIMESTAMP
			 WHERE doctype = 'Bin' AND data->>'bin_code' = $2`, schema), prior, fb.BinCode); err != nil {
			return err
		}
	}
	return nil
}
