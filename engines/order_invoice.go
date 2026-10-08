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
	// Stage 35.4.2 widened this check, and the widening is load-bearing.
	//
	// It used to look only for its own "INV-<orderID>". Once packages can be
	// invoiced individually (GenerateInvoiceForPackage), an order can already
	// carry one or more invoices under generated ids by the time it reaches
	// Shipped - and the handover cascade calls this function on exactly that
	// transition. Under the old check, every package-invoiced order would have
	// been invoiced a second time, in full, for the whole order value.
	//
	// So the question is now "does this order have any invoice", answered
	// against sales_order_id. A split order with two package invoices returns
	// the first and creates nothing, which is right: the pack path has already
	// billed every parcel, and there is nothing left for the order-level
	// fallback to do.
	invoiceID := "INV-" + orderID
	var existing string
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT id FROM %s.documents
		 WHERE doctype = 'SalesInvoice' AND deleted_at IS NULL AND status <> 'Cancelled'
		   AND (id = $1 OR data->>'sales_order_id' = $2)
		 ORDER BY created_at, id LIMIT 1`, schema), invoiceID, orderID).Scan(&existing)
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
	// 2026-10-07: bill what shipped, with its GST. Dispatch records
	// shipped_qty on each line; a short-picked unit never left, so its value
	// comes off the draft (lines dispatched before that carry no shipped_qty
	// and are billed in full, as before). The billed lines then go through the
	// same ComputeGSTForLines + reconcileInvoiceAmounts a package invoice uses,
	// so PostSalesInvoice can book the tax to GST Output Payable.
	shortValue := 0.0
	var gstLines []GSTLineInput
	var location string
	if lineRows, err := db.DB.Query(fmt.Sprintf(`
		SELECT data FROM %s.documents
		WHERE doctype = 'SalesOrderLine' AND data->>'order_id' = $1 AND deleted_at IS NULL
		ORDER BY id`, schema), orderID); err == nil {
		for lineRows.Next() {
			var raw []byte
			if lineRows.Scan(&raw) != nil {
				continue
			}
			var line map[string]interface{}
			if json.Unmarshal(raw, &line) != nil {
				continue
			}
			if st, _ := line["line_status"].(string); st == "Cancelled" {
				continue
			}
			if location == "" {
				location, _ = line["location_code"].(string)
			}
			qty := numFromInterface(line["qty"])
			billed := qty
			if _, has := line["shipped_qty"]; has {
				billed = numFromInterface(line["shipped_qty"])
				if short := qty - billed; short > 0 {
					shortValue += short * numFromInterface(line["unit_price"])
				}
			}
			sku, _ := line["sku"].(string)
			if billed > 0 && sku != "" {
				gstLines = append(gstLines, GSTLineInput{Sku: sku, Qty: int(billed), UnitRate: numFromInterface(line["unit_price"])})
			}
		}
		lineRows.Close()
	}
	total = round2(total - shortValue)
	if total <= 0 {
		return "", fmt.Errorf("sales order %s has no invoiceable total", orderID)
	}
	invoiceData := map[string]interface{}{
		"code": orderID, "invoice_number": invoiceID, "sales_order_id": orderID,
		"customer": orderData["customer_name"], "location": location,
		"total_amount": total, "status": "Draft",
	}
	if shortValue > 0 {
		invoiceData["short_shipped_value"] = shortValue
	}
	if len(gstLines) > 0 {
		interstate, basis := resolveOrderInterstate(tenantID, schema, orderID, location)
		if breakdown, gerr := ComputeGSTForLines(tenantID, gstLines, interstate); gerr == nil && breakdown.TotalAmount > 0 {
			amounts := reconcileInvoiceAmounts(breakdown, total, interstate)
			invoiceData["taxable_amount"] = amounts.Taxable
			invoiceData["non_taxable"] = amounts.NonTaxable
			invoiceData["cgst"] = amounts.CGST
			invoiceData["sgst"] = amounts.SGST
			invoiceData["igst"] = amounts.IGST
			invoiceData["total_tax"] = amounts.TotalTax
			invoiceData["interstate"] = interstate
			invoiceData["gst_basis"] = basis
		} else if gerr != nil {
			// Never block the shipment cascade on tax data: the draft is
			// still created, says why it carries no GST, and posts as before.
			invoiceData["gst_note"] = "GST not computed: " + gerr.Error()
		}
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
