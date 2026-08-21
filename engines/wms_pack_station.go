package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
)

// Stage 42.4.5/42.4.6 - PackStation/PackTemplate masters (plain Masters,
// zero bespoke frontend, same free ride Zone/PutawayStrategy get) plus the
// one real piece of new behaviour: PackingValidationTemplate's pre-pack
// checklist, wired additively into CompletePackTask (engines/
// fulfillment_pickpack.go, itself untouched) via
// CompletePackTaskWithValidation below - the same "read what you need from
// the caller, don't change the tested choke point's signature" pattern
// 42.2.2 used for ScanPickItem/handlePickScan.
//
// PackTemplate.customer is Data holding the customer NAME, not a Link -
// SalesOrder has no Customer Link field today (only customer_name, a plain
// Data field, per migrations_stage26_12_1_order_engine.sql), so matching
// against a Customer document id would never resolve. Matched as text
// against SalesOrder.customer_name, same as PackingValidationTemplate's own
// applies_to_value for the Customer scope.

// PackingValidationResult is what EvaluatePackingValidation found - which
// template (if any) applied, and whether it wants blind packing.
type PackingValidationResult struct {
	TemplateID         string `json:"template_id,omitempty"`
	RequireWeightCheck bool   `json:"require_weight_check"`
	RequireDocuments   bool   `json:"require_documents"`
	BlindPack          bool   `json:"blind_pack"`
}

// resolvePackingValidationTemplate finds the Active PackingValidationTemplate
// that applies to a fulfillment task - Item scope (any line's SKU matches)
// takes priority over Customer scope (the order's customer_name matches),
// which takes priority over an All-scope template, mirroring the specificity
// order PackTemplate resolution below uses. No match is not an error - a
// tenant that never configures one packs exactly as it did before this item.
func resolvePackingValidationTemplate(schema string, task *pickPackTask) (*PackingValidationResult, error) {
	skus := map[string]bool{}
	for _, it := range task.items {
		if it.SKU != "" {
			skus[it.SKU] = true
		}
	}
	customerName, _ := task.data["order_id"].(string)
	if orderID, ok := task.data["order_id"].(string); ok && orderID != "" {
		_ = db.DB.QueryRow(fmt.Sprintf(
			`SELECT COALESCE(data->>'customer_name', '') FROM %s.documents WHERE doctype = 'SalesOrder' AND id = $1`, schema), orderID).Scan(&customerName)
	}

	rows, err := db.DB.Query(fmt.Sprintf(
		`SELECT id, data FROM %s.documents WHERE doctype = 'PackingValidationTemplate' AND status = 'Active'`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var itemMatch, customerMatch, allMatch *PackingValidationResult
	for rows.Next() {
		var id, dataStr string
		if err := rows.Scan(&id, &dataStr); err != nil {
			return nil, err
		}
		var data map[string]interface{}
		_ = json.Unmarshal([]byte(dataStr), &data)
		res := &PackingValidationResult{
			TemplateID:         id,
			RequireWeightCheck: strField(data, "require_weight_check") == "Yes",
			RequireDocuments:   strField(data, "require_documents") == "Yes",
			BlindPack:          strField(data, "blind_pack") == "Yes",
		}
		switch strField(data, "applies_to") {
		case "Item":
			if skus[strField(data, "applies_to_value")] {
				itemMatch = res
			}
		case "Customer":
			if customerName != "" && strField(data, "applies_to_value") == customerName {
				customerMatch = res
			}
		case "All":
			allMatch = res
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if itemMatch != nil {
		return itemMatch, nil
	}
	if customerMatch != nil {
		return customerMatch, nil
	}
	return allMatch, nil
}

// GetPackingValidation (42.4.6) is the read-only lookup the pack screen
// calls before showing its confirm form - whether to hide the expected qty
// (blind_pack) and whether weight/documents will be required at complete.
func GetPackingValidation(tenantID, taskID string) (*PackingValidationResult, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	task, err := loadPickPackTask(schema, taskID)
	if err != nil {
		return nil, err
	}
	res, err := resolvePackingValidationTemplate(schema, task)
	if err != nil {
		return nil, err
	}
	if res == nil {
		return &PackingValidationResult{}, nil
	}
	return res, nil
}

// CompletePackTaskWithValidation (42.4.6) runs the matched
// PackingValidationTemplate's pre-pack checklist, then defers to the
// existing, unchanged CompletePackTask for the actual completion (item
// pick/pack-qty reconciliation it already enforces). weightKg/
// documentsConfirmed are only checked when the matched template actually
// requires them - a task with no matching template behaves exactly as
// handlePackComplete always did.
func CompletePackTaskWithValidation(tenantID, taskID string, weightKg float64, documentsConfirmed bool, userID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	task, err := loadPickPackTask(schema, taskID)
	if err != nil {
		return err
	}
	res, err := resolvePackingValidationTemplate(schema, task)
	if err != nil {
		return err
	}
	if res != nil {
		if res.RequireWeightCheck && weightKg <= 0 {
			return fmt.Errorf("packing validation template %s requires a captured weight before this task can be packed", res.TemplateID)
		}
		if res.RequireDocuments && !documentsConfirmed {
			return fmt.Errorf("packing validation template %s requires required documents to be confirmed present before this task can be packed", res.TemplateID)
		}
	}
	if err := CompletePackTask(tenantID, taskID); err != nil {
		return err
	}
	if weightKg > 0 {
		if _, err := db.DB.Exec(fmt.Sprintf(
			`UPDATE %s.documents SET data = data || jsonb_build_object('packed_weight_kg', $1::numeric) WHERE doctype = 'FulfillmentTask' AND id = $2`, schema),
			weightKg, taskID); err != nil {
			LogSystemError(tenantID, "", "WARN", "CompletePackTaskWithValidation", fmt.Sprintf("failed to record packed_weight_kg for %s: %v", taskID, err), "")
		}
	}
	if res != nil {
		LogAuditEvent(tenantID, userID, "WMS_PACKING_VALIDATION_PASSED", "SUCCESS",
			fmt.Sprintf("Task %s packed against validation template %s", taskID, res.TemplateID))
	}
	return nil
}

// ResolvePackTemplate (42.4.5) picks the most specific Active PackTemplate
// for one SKU/customer - Item match, then Customer match, then the blank
// "applies to all" template - the same specificity order
// resolvePackingValidationTemplate uses above. Purely advisory: the pack
// screen shows the result (carton type/dunnage/required docs & labels), it
// does not gate anything.
type PackTemplateInfo struct {
	DocID             string `json:"doc_id"`
	Name              string `json:"name"`
	CartonType        string `json:"carton_type,omitempty"`
	Dunnage           string `json:"dunnage,omitempty"`
	DocumentsRequired string `json:"documents_required,omitempty"`
	LabelsRequired    string `json:"labels_required,omitempty"`
}

func ResolvePackTemplate(tenantID, sku, customerName string) (*PackTemplateInfo, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(
		`SELECT id, data FROM %s.documents WHERE doctype = 'PackTemplate' AND status = 'Active'`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var itemMatch, customerMatch, allMatch *PackTemplateInfo
	for rows.Next() {
		var id, dataStr string
		if err := rows.Scan(&id, &dataStr); err != nil {
			return nil, err
		}
		var data map[string]interface{}
		_ = json.Unmarshal([]byte(dataStr), &data)
		info := &PackTemplateInfo{
			DocID: id, Name: strField(data, "name"), CartonType: strField(data, "carton_type"),
			Dunnage: strField(data, "dunnage"), DocumentsRequired: strField(data, "documents_required"),
			LabelsRequired: strField(data, "labels_required"),
		}
		item := strField(data, "item")
		customer := strField(data, "customer")
		switch {
		case item != "" && item == sku:
			itemMatch = info
		case customer != "" && customer == customerName:
			customerMatch = info
		case item == "" && customer == "":
			allMatch = info
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if itemMatch != nil {
		return itemMatch, nil
	}
	if customerMatch != nil {
		return customerMatch, nil
	}
	if allMatch != nil {
		return allMatch, nil
	}
	return nil, nil
}
