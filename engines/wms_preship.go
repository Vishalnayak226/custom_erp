package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
)

// Stage 42.4.10 - Pre-ship validation rules (Infor Sec17-style): the final
// gate before dispatch, called from CompleteLoadingTask
// (engines/wms_loading.go) right before a load moves Loading -> Loaded. A
// tenant with no Active PreShipValidationRule for the load's location is
// left with exactly the carton-count check CompleteLoadingTask's caller
// already expects - inert-until-configured, the same posture every other
// gate this Stage added.

// resolvePreShipRule looks up the Active PreShipValidationRule for
// locationCode, falling back to a blank-location_code "applies everywhere"
// row - same fallback shape resolveDispatchOrder (42.2.4) already uses.
func resolvePreShipRule(schema, locationCode string) (map[string]interface{}, error) {
	var dataStr string
	err := db.DB.QueryRow(fmt.Sprintf(`
		SELECT data FROM %s.documents WHERE doctype = 'PreShipValidationRule' AND status = 'Active'
		  AND data->>'location_code' = $1 LIMIT 1`, schema), locationCode).Scan(&dataStr)
	if err == sql.ErrNoRows {
		err = db.DB.QueryRow(fmt.Sprintf(`
			SELECT data FROM %s.documents WHERE doctype = 'PreShipValidationRule' AND status = 'Active'
			  AND COALESCE(data->>'location_code', '') = '' LIMIT 1`, schema)).Scan(&dataStr)
	}
	if err == sql.ErrNoRows {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return nil, err
	}
	return data, nil
}

// EvaluatePreShipGate runs the matched rule's three checks against
// loadingTaskID's own dock door location. No matched rule is a no-op.
func EvaluatePreShipGate(tenantID string, task *LoadingTaskInfo) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var locationCode string
	_ = db.DB.QueryRow(fmt.Sprintf(
		`SELECT COALESCE(data->>'location', '') FROM %s.documents WHERE doctype = 'DockDoor' AND data->>'code' = $1`, schema), task.DockDoor).Scan(&locationCode)

	rule, err := resolvePreShipRule(schema, locationCode)
	if err != nil {
		return err
	}
	if rule == nil {
		return nil
	}

	if strField(rule, "require_all_cartons_scanned") == "Yes" {
		if task.ExpectedCartonCount > 0 && task.ScannedCartonCount < task.ExpectedCartonCount {
			return fmt.Errorf("pre-ship gate: only %d of %d expected cartons have been scanned", task.ScannedCartonCount, task.ExpectedCartonCount)
		}
	}

	skus, err := loadedPackageSkus(schema, task.DocID)
	if err != nil {
		return err
	}

	if strField(rule, "require_hold_free") == "Yes" && locationCode != "" {
		for _, sku := range skus {
			var held bool
			if err := db.DB.QueryRow(fmt.Sprintf(
				`SELECT EXISTS(SELECT 1 FROM %s.documents WHERE doctype = 'Hold' AND status = 'Active' AND data->>'sku' = $1 AND data->>'location_code' = $2)`, schema),
				sku, locationCode).Scan(&held); err != nil {
				return err
			}
			if held {
				return fmt.Errorf("pre-ship gate: SKU %s at location %s has an Active hold - release it before this load can complete", sku, locationCode)
			}
		}
	}

	if strField(rule, "require_documents_present") == "Yes" {
		var missing int
		if err := db.DB.QueryRow(fmt.Sprintf(
			`SELECT COUNT(*) FROM %s.documents WHERE doctype = 'ShippingPackage' AND data->>'loading_task_id' = $1
			   AND COALESCE(data->>'sales_invoice_id', '') = ''`, schema), task.DocID).Scan(&missing); err != nil {
			return err
		}
		if missing > 0 {
			return fmt.Errorf("pre-ship gate: %d loaded package(s) have no invoice attached", missing)
		}
	}
	return nil
}

func loadedPackageSkus(schema, loadingTaskID string) ([]string, error) {
	rows, err := db.DB.Query(fmt.Sprintf(
		`SELECT data->'items' FROM %s.documents WHERE doctype = 'ShippingPackage' AND data->>'loading_task_id' = $1`, schema), loadingTaskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := map[string]bool{}
	var out []string
	for rows.Next() {
		var itemsStr sql.NullString
		if err := rows.Scan(&itemsStr); err != nil {
			return nil, err
		}
		if !itemsStr.Valid || itemsStr.String == "" {
			continue
		}
		var items []map[string]interface{}
		if err := json.Unmarshal([]byte(itemsStr.String), &items); err != nil {
			continue
		}
		for _, it := range items {
			if sku := strField(it, "sku"); sku != "" && !seen[sku] {
				seen[sku] = true
				out = append(out, sku)
			}
		}
	}
	return out, rows.Err()
}
