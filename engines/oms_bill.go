package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
)

// Stage 88.3.2 / 88.3.3 (D4, D5): two bills for an OMS order.
//
// The OMS bill is made at Pack, one per package, from what is in the box. It
// takes its number from its own series (OB), prints with the OMS bill
// template, goes to the customer - and its number is the GST invoice number.
// It posts nothing.
//
// The ERP bill is an ordinary Sales Invoice made from it when the manifest is
// handed over: the OMS bill's lines and tax copied exactly, the OMS bill's
// number as its invoice_number, a link back to it, and posted at once. GST is
// read from the posted ledger and the invoice registers, so the sale is
// counted once, under the OMS number.
//
// This replaces 35.4.2's pack-time Draft Sales Invoice for new packages
// (88.3.4). A package already invoiced that way keeps its invoice and is
// handed over exactly as before; GenerateInvoiceForPackage is unchanged.

const (
	OMSBillIssued = "Issued" // given to the customer, no ERP bill yet
	OMSBillBilled = "Billed" // its ERP bill (Sales Invoice) exists
)

// GenerateOMSBillForPackage bills one Draft package and moves it to Invoiced,
// the status the label and manifest rules (35.4.3) already wait for.
// Idempotent on the package: a second call returns the first bill, and a
// package billed the 35.4.2 way returns its Sales Invoice id instead.
func GenerateOMSBillForPackage(tenantID, packageID, userID string, opts PackInvoiceOptions) (billID, billNumber string, err error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", "", err
	}
	p, err := loadShippingPackage(schema, packageID)
	if err != nil {
		return "", "", err
	}
	if p.OMSBillID != "" {
		number, _ := omsBillField(schema, p.OMSBillID, "bill_number")
		return p.OMSBillID, number, nil
	}
	if p.SalesInvoiceID != "" {
		return p.SalesInvoiceID, "", nil
	}
	if p.Status == "Cancelled" {
		return "", "", fmt.Errorf("shipping package %s is cancelled and cannot be billed", packageID)
	}
	if err := shippingPackageMutableStatus(p.Status); err != nil {
		return "", "", err
	}
	if len(p.Items) == 0 {
		return "", "", fmt.Errorf("shipping package %s has nothing in it to bill", packageID)
	}
	bill, err := packageBillFigures(tenantID, schema, p, opts)
	if err != nil {
		return "", "", err
	}

	billID = NewDocID("OB")
	billNumber = IssueDocumentNumber(tenantID, "OMSBill", billID, "", p.LocationCode)
	customerName, _ := orderFieldString(schema, p.OrderID, "customer_name")
	data := map[string]interface{}{
		"code":                billID,
		"bill_number":         billNumber,
		"sales_order_id":      p.OrderID,
		"shipping_package_id": p.ID,
		"fulfillment_task_id": p.TaskID,
		"customer":            customerName,
		"location":            p.LocationCode,
		"total_amount":        bill.amounts.Total,
		"taxable_amount":      bill.amounts.Taxable,
		"non_taxable":         bill.amounts.NonTaxable,
		"cgst":                bill.amounts.CGST,
		"sgst":                bill.amounts.SGST,
		"igst":                bill.amounts.IGST,
		"total_tax":           bill.amounts.TotalTax,
		"interstate":          bill.interstate,
		"gst_basis":           bill.basis,
		"sales_invoice_id":    "",
		"status":              OMSBillIssued,
	}
	if encodedLines, err := json.Marshal(bill.lines); err == nil {
		data["items"] = string(encodedLines)
	}
	if ref, _ := orderFieldString(schema, p.OrderID, "customer_reference"); ref != "" {
		data["customer_reference"] = ref
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return "", "", err
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return "", "", err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(fmt.Sprintf(
		`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'OMSBill', $2, $3, 'system')`, schema),
		billID, encoded, OMSBillIssued); err != nil {
		return "", "", err
	}
	if err := ValidateStatusTransition(tenantID, "ShippingPackage", p.Status, "Invoiced", nil); err != nil {
		return "", "", err
	}
	p.Status = "Invoiced"
	p.OMSBillID = billID
	if err := p.save(tx, schema); err != nil {
		return "", "", err
	}
	if err := tx.Commit(); err != nil {
		return "", "", err
	}
	LogAuditEvent(tenantID, userID, "CREATE_OMS_BILL", "SUCCESS",
		fmt.Sprintf("OMS bill %s (%s) for shipping package %s (order %s, total=%.2f, %s)", billID, billNumber, packageID, p.OrderID, bill.amounts.Total, bill.basis))
	return billID, billNumber, nil
}

// RaiseERPBillForPackage makes and posts the ERP bill for a package's OMS
// bill (88.3.3). Called by HandoverManifest for every parcel it hands over.
// Idempotent: a package that already has its Sales Invoice returns it. A
// posting refusal (a closed period, say) leaves the invoice Draft for Finance
// and is returned as the error; the invoice id is returned either way.
func RaiseERPBillForPackage(tenantID, packageID, userID string) (string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	p, err := loadShippingPackage(schema, packageID)
	if err != nil {
		return "", err
	}
	if p.SalesInvoiceID != "" || p.OMSBillID == "" {
		return p.SalesInvoiceID, nil
	}
	var billStr string
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT data FROM %s.documents WHERE doctype = 'OMSBill' AND id = $1 AND deleted_at IS NULL`, schema), p.OMSBillID).Scan(&billStr); err != nil {
		return "", fmt.Errorf("OMS bill %s of package %s not found: %v", p.OMSBillID, packageID, err)
	}
	var bill map[string]interface{}
	if err := json.Unmarshal([]byte(billStr), &bill); err != nil {
		return "", err
	}

	invoiceID := NewDocID("SINV")
	invoice := map[string]interface{}{"code": invoiceID, "status": "Draft",
		// D5: the OMS bill's number is the GST invoice; this bill carries it.
		"invoice_number": bill["bill_number"], "oms_bill_id": p.OMSBillID, "oms_bill_number": bill["bill_number"]}
	for _, k := range []string{"sales_order_id", "shipping_package_id", "customer", "location", "total_amount", "taxable_amount",
		"non_taxable", "cgst", "sgst", "igst", "total_tax", "interstate", "gst_basis", "items", "customer_reference"} {
		if v, ok := bill[k]; ok {
			invoice[k] = v
		}
	}
	encoded, err := json.Marshal(invoice)
	if err != nil {
		return "", err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(fmt.Sprintf(
		`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'SalesInvoice', $2, 'Draft', 'system')`, schema),
		invoiceID, encoded); err != nil {
		return "", err
	}
	if _, err := tx.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = data || jsonb_build_object('sales_invoice_id', $1::text, 'status', $2::text), status = $2, updated_at = CURRENT_TIMESTAMP
		  WHERE doctype = 'OMSBill' AND id = $3`, schema), invoiceID, OMSBillBilled, p.OMSBillID); err != nil {
		return "", err
	}
	p.SalesInvoiceID = invoiceID
	if err := p.save(tx, schema); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	LogAuditEvent(tenantID, userID, "CREATE_SALES_INVOICE", "SUCCESS",
		fmt.Sprintf("ERP bill %s from OMS bill %s (%v) at manifest handover, package %s", invoiceID, p.OMSBillID, bill["bill_number"], packageID))
	if _, err := PostSalesInvoice(tenantID, invoiceID, userID); err != nil {
		return invoiceID, fmt.Errorf("ERP bill %s was made but not posted: %v", invoiceID, err)
	}
	return invoiceID, nil
}

func omsBillField(schema, billID, field string) (string, error) {
	var v string
	err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT COALESCE(data->>$1, '') FROM %s.documents WHERE doctype = 'OMSBill' AND id = $2`, schema), field, billID).Scan(&v)
	return v, err
}

// jsonLines reads a document's line array, stored either as JSON text (most
// documents) or as a JSON array (a fulfillment task's items).
func jsonLines(raw interface{}) []map[string]interface{} {
	var out []map[string]interface{}
	switch v := raw.(type) {
	case string:
		_ = json.Unmarshal([]byte(v), &out)
	case []interface{}:
		for _, x := range v {
			if m, ok := x.(map[string]interface{}); ok {
				out = append(out, m)
			}
		}
	}
	return out
}
