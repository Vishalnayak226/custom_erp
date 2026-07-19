package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
)

// ConvertRequisitionToOrder is Stage 17.7's one-time conversion: an Approved
// PurchaseRequisition becomes a new Draft RFQ or PO, pre-filled with the
// requisition's description/quantity/estimated amount. The requisition is
// then marked Converted (row-locked, status-gated) so a second conversion
// attempt - a retry, a double-click - is rejected rather than creating a
// duplicate downstream document.
func ConvertRequisitionToOrder(tenantID, requisitionID, target, storeCode, financialYear, userID string) (string, error) {
	if target != "RFQ" && target != "PurchaseOrder" {
		return "", fmt.Errorf("target must be 'RFQ' or 'PurchaseOrder'")
	}

	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}

	// Generated via its own transaction (GenerateSequence manages that
	// internally, same as every other doctype's number generation in this
	// codebase) before the conversion transaction below opens - a failure
	// past this point leaves a gap in the sequence, not a duplicate or lost
	// number, the normal and accepted behavior for a sequence counter.
	seqDocType := "PO"
	if target == "RFQ" {
		seqDocType = "RFQ"
	}
	newCode, err := GenerateSequence(tenantID, seqDocType, storeCode, financialYear)
	if err != nil {
		return "", fmt.Errorf("could not generate %s number: %v", target, err)
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if err := db.SetSearchPath(tx, schema); err != nil {
		return "", err
	}

	var dataStr, status string
	if err := tx.QueryRow(fmt.Sprintf(
		`SELECT data, status FROM %s.documents WHERE doctype = 'PurchaseRequisition' AND id = $1 FOR UPDATE`, schema),
		requisitionID).Scan(&dataStr, &status); err != nil {
		return "", fmt.Errorf("requisition not found: %v", err)
	}
	if status != "Approved" {
		return "", fmt.Errorf("requisition must be Approved to convert (current status: %s)", status)
	}

	var prData map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &prData); err != nil {
		return "", err
	}
	description, _ := prData["description"].(string)
	quantity, _ := prData["quantity"].(float64)
	totalAmount, _ := prData["total_amount"].(float64)

	var newData map[string]interface{}
	if target == "RFQ" {
		newData = map[string]interface{}{
			"id": newCode, "code": newCode, "description": description, "quantity": quantity,
			"status": "Draft", "source_requisition_id": requisitionID,
		}
	} else {
		newData = map[string]interface{}{
			"id": newCode, "code": newCode, "po_number": newCode, "vendor": "", "vendor_id": "",
			"target_warehouse": "", "location": "", "items": "[]", "total_amount": totalAmount,
			"status": "Draft", "source_requisition_id": requisitionID,
		}
	}
	newDataBytes, _ := json.Marshal(newData)
	if _, err := tx.Exec(fmt.Sprintf(
		`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, 'Draft', $4)`, schema),
		newCode, target, newDataBytes, userID); err != nil {
		return "", fmt.Errorf("could not create %s: %v", target, err)
	}

	prData["status"] = "Converted"
	prData["converted_to_doctype"] = target
	prData["converted_to_id"] = newCode
	updatedPRBytes, _ := json.Marshal(prData)
	if _, err := tx.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = $1, status = 'Converted', updated_at = CURRENT_TIMESTAMP WHERE doctype = 'PurchaseRequisition' AND id = $2`, schema),
		updatedPRBytes, requisitionID); err != nil {
		return "", err
	}

	if err := tx.Commit(); err != nil {
		return "", err
	}

	LogAuditEvent(tenantID, userID, "CONVERT_REQUISITION", "SUCCESS",
		fmt.Sprintf("Converted requisition %s to %s %s", requisitionID, target, newCode))
	return newCode, nil
}
