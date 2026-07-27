package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
)

// CreateSalesInvoiceFromOrder creates one idempotent Draft SalesInvoice after
// a SalesOrder has fully shipped. Posting/settlement remain explicit finance
// actions, so shipping automation never silently creates a GL posting.
func CreateSalesInvoiceFromOrder(tenantID, orderID, userID string) (string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	var orderDataBytes []byte
	var orderStatus string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT data, status FROM %s.documents WHERE id = $1 AND doctype = 'SalesOrder' AND deleted_at IS NULL`, schema), orderID).Scan(&orderDataBytes, &orderStatus); err != nil {
		return "", fmt.Errorf("sales order %s not found: %v", orderID, err)
	}
	if orderStatus != "Shipped" && orderStatus != "Delivered" {
		return "", fmt.Errorf("sales order %s must be Shipped before invoicing (current status: %s)", orderID, orderStatus)
	}
	invoiceID := "INV-" + orderID
	var existing string
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT id FROM %s.documents WHERE id = $1 AND doctype = 'SalesInvoice' AND deleted_at IS NULL`, schema), invoiceID).Scan(&existing)
	if err == nil {
		return existing, nil
	}
	if err != sql.ErrNoRows {
		return "", err
	}

	var orderData map[string]interface{}
	if err := json.Unmarshal(orderDataBytes, &orderData); err != nil {
		return "", err
	}
	total := numFromInterface(orderData["total_amount"])
	if total <= 0 {
		return "", fmt.Errorf("sales order %s has no invoiceable total", orderID)
	}
	var location string
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(data->>'location_code', '') FROM %s.documents WHERE doctype = 'SalesOrderLine' AND data->>'order_id' = $1 AND deleted_at IS NULL ORDER BY id LIMIT 1`, schema), orderID).Scan(&location)
	invoiceData := map[string]interface{}{
		"code": orderID, "invoice_number": invoiceID, "sales_order_id": orderID,
		"customer": orderData["customer_name"], "location": location,
		"total_amount": total, "status": "Draft",
	}
	encoded, err := json.Marshal(invoiceData)
	if err != nil {
		return "", err
	}
	// The shipment event can originate from a system connector identity that
	// isn't a tenant user. Documents.created_by is a tenant-user FK, so the
	// established system actor is used for the system-created invoice while
	// the human/integration actor remains in the audit log below.
	_, err = db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'SalesInvoice', $2, 'Draft', 'system')`, schema), invoiceID, encoded)
	if err != nil {
		return "", err
	}
	LogAuditEvent(tenantID, userID, "CREATE_SALES_INVOICE", "SUCCESS", fmt.Sprintf("Created draft invoice %s for shipped order %s", invoiceID, orderID))
	return invoiceID, nil
}
