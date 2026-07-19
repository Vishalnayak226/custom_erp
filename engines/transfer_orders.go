package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
)

type transferLine struct {
	Sku string `json:"sku"`
	Qty int    `json:"qty"`
}

func parseTransferItems(itemsJSON string) ([]transferLine, error) {
	var lines []transferLine
	if itemsJSON == "" {
		return nil, fmt.Errorf("transfer order has no items")
	}
	if err := json.Unmarshal([]byte(itemsJSON), &lines); err != nil {
		return nil, fmt.Errorf("could not parse items JSON: %v", err)
	}
	if len(lines) == 0 {
		return nil, fmt.Errorf("transfer order has no items")
	}
	for _, l := range lines {
		if l.Sku == "" || l.Qty <= 0 {
			return nil, fmt.Errorf("every transfer line needs a sku and a positive qty (sku=%q qty=%d)", l.Sku, l.Qty)
		}
	}
	return lines, nil
}

// DispatchTransferOrder moves an Approved TransferOrder to Dispatched: each
// line's qty is floor-checked and moved out of the source location's
// `available` into its `in_transit` (Stage 17.6 migration) - never received
// out of thin air, and never dispatched twice (row-locked, status-gated).
func DispatchTransferOrder(tenantID, transferOrderID, userID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := db.SetSearchPath(tx, schema); err != nil {
		return err
	}

	var dataStr, status string
	if err := tx.QueryRow(fmt.Sprintf(
		`SELECT data, status FROM %s.documents WHERE doctype = 'TransferOrder' AND id = $1 FOR UPDATE`, schema),
		transferOrderID).Scan(&dataStr, &status); err != nil {
		return fmt.Errorf("transfer order not found: %v", err)
	}
	if status != "Approved" {
		return fmt.Errorf("transfer order must be Approved to dispatch (current status: %s)", status)
	}

	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return err
	}
	itemsStr, _ := data["items"].(string)
	lines, err := parseTransferItems(itemsStr)
	if err != nil {
		return err
	}
	fromWarehouse, _ := data["from_warehouse"].(string)
	if fromWarehouse == "" {
		return fmt.Errorf("transfer order is missing from_warehouse")
	}

	for _, line := range lines {
		var available int
		if err := tx.QueryRow(fmt.Sprintf(
			`SELECT available FROM %s.inventory_availability WHERE sku = $1 AND location_code = $2 FOR UPDATE`, schema),
			line.Sku, fromWarehouse).Scan(&available); err != nil {
			if err == sql.ErrNoRows {
				return fmt.Errorf("insufficient stock for SKU %s at %s: no inventory record", line.Sku, fromWarehouse)
			}
			return err
		}
		if available < line.Qty {
			return fmt.Errorf("insufficient stock for SKU %s at %s: available=%d, requested=%d", line.Sku, fromWarehouse, available, line.Qty)
		}
		if _, err := tx.Exec(fmt.Sprintf(
			`UPDATE %s.inventory_availability SET available = available - $1, in_transit = in_transit + $1, updated_at = CURRENT_TIMESTAMP
			 WHERE sku = $2 AND location_code = $3`, schema),
			line.Qty, line.Sku, fromWarehouse); err != nil {
			return err
		}
	}

	data["status"] = "Dispatched"
	data["dispatched_items"] = itemsStr
	updatedBytes, _ := json.Marshal(data)
	if _, err := tx.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = $1, status = 'Dispatched', updated_at = CURRENT_TIMESTAMP WHERE doctype = 'TransferOrder' AND id = $2`, schema),
		updatedBytes, transferOrderID); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	LogAuditEvent(tenantID, userID, "DISPATCH_TRANSFER_ORDER", "SUCCESS", fmt.Sprintf("Dispatched transfer order %s from %s", transferOrderID, fromWarehouse))
	return nil
}

// ReceiveTransferOrder moves a Dispatched TransferOrder to Received. Each
// line's dispatched qty always leaves the source's in_transit (it
// physically left, whatever arrived); only what's actually confirmed
// received is added to the destination's available. A shortfall
// (receivedQty < dispatchedQty) is recorded as a variance rather than
// silently reconciled - never a surplus (an over-receive is rejected,
// since more can't arrive than was dispatched).
func ReceiveTransferOrder(tenantID, transferOrderID, userID string, receivedItems []interface{}) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := db.SetSearchPath(tx, schema); err != nil {
		return err
	}

	var dataStr, status string
	if err := tx.QueryRow(fmt.Sprintf(
		`SELECT data, status FROM %s.documents WHERE doctype = 'TransferOrder' AND id = $1 FOR UPDATE`, schema),
		transferOrderID).Scan(&dataStr, &status); err != nil {
		return fmt.Errorf("transfer order not found: %v", err)
	}
	if status != "Dispatched" {
		return fmt.Errorf("transfer order must be Dispatched to receive (current status: %s)", status)
	}

	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return err
	}
	dispatchedStr, _ := data["dispatched_items"].(string)
	dispatchedLines, err := parseTransferItems(dispatchedStr)
	if err != nil {
		return err
	}
	toWarehouse, _ := data["to_warehouse"].(string)
	if toWarehouse == "" {
		return fmt.Errorf("transfer order is missing to_warehouse")
	}
	fromWarehouse, _ := data["from_warehouse"].(string)

	receivedBySku := map[string]int{}
	for _, itemVal := range receivedItems {
		itemMap, ok := itemVal.(map[string]interface{})
		if !ok {
			continue
		}
		sku, _ := itemMap["sku"].(string)
		qty := 0
		if q, exists := itemMap["qty"]; exists {
			switch v := q.(type) {
			case float64:
				qty = int(v)
			case int:
				qty = v
			}
		}
		receivedBySku[sku] = qty
	}

	type varianceLine struct {
		Sku        string `json:"sku"`
		Dispatched int    `json:"dispatched_qty"`
		Received   int    `json:"received_qty"`
		Shortfall  int    `json:"shortfall"`
	}
	var variances []varianceLine

	for _, line := range dispatchedLines {
		receivedQty, ok := receivedBySku[line.Sku]
		if !ok {
			receivedQty = 0
		}
		if receivedQty < 0 || receivedQty > line.Qty {
			return fmt.Errorf("received qty for SKU %s must be between 0 and the dispatched qty %d (got %d)", line.Sku, line.Qty, receivedQty)
		}

		if _, err := tx.Exec(fmt.Sprintf(
			`UPDATE %s.inventory_availability SET in_transit = in_transit - $1, updated_at = CURRENT_TIMESTAMP
			 WHERE sku = $2 AND location_code = $3`, schema),
			line.Qty, line.Sku, fromWarehouse); err != nil {
			return err
		}

		if receivedQty > 0 {
			if _, err := tx.Exec(fmt.Sprintf(
				`INSERT INTO %s.inventory_availability (sku, location_code, on_hand, available)
				 VALUES ($1, $2, $3, $3)
				 ON CONFLICT (sku, location_code) DO UPDATE SET
					on_hand = %s.inventory_availability.on_hand + $3,
					available = %s.inventory_availability.available + $3,
					updated_at = CURRENT_TIMESTAMP`, schema, schema, schema),
				line.Sku, toWarehouse, receivedQty); err != nil {
				return err
			}
		}

		if receivedQty < line.Qty {
			variances = append(variances, varianceLine{Sku: line.Sku, Dispatched: line.Qty, Received: receivedQty, Shortfall: line.Qty - receivedQty})
		}
	}

	receivedBytes, _ := json.Marshal(receivedItems)
	data["status"] = "Received"
	data["received_items"] = string(receivedBytes)
	if len(variances) > 0 {
		varianceBytes, _ := json.Marshal(variances)
		data["receive_variance"] = string(varianceBytes)
	}
	updatedBytes, _ := json.Marshal(data)
	if _, err := tx.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = $1, status = 'Received', updated_at = CURRENT_TIMESTAMP WHERE doctype = 'TransferOrder' AND id = $2`, schema),
		updatedBytes, transferOrderID); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	details := fmt.Sprintf("Received transfer order %s at %s", transferOrderID, toWarehouse)
	if len(variances) > 0 {
		varianceBytes, _ := json.Marshal(variances)
		details = fmt.Sprintf("%s with shortage variance: %s", details, string(varianceBytes))
	}
	LogAuditEvent(tenantID, userID, "RECEIVE_TRANSFER_ORDER", "SUCCESS", details)
	return nil
}
