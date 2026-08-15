package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// Stage 35.4.5: credit note on cancellation-after-invoice.
//
// The gap this closes. CancelOrder releases reservations and marks lines
// Cancelled, which is correct for an order that was never invoiced. Once an
// invoice exists the cancellation is no longer only an operational event: a tax
// document has been issued against that sale, and it cannot be un-issued. The
// instrument for reversing it is a credit note, and until now nothing produced
// one - so a cancellation after invoicing left the receivable standing and the
// GST already declared.
//
// CreditNote and PostCreditNote already exist (Stage 20c). This file does not
// re-implement them; it works out which invoices a cancellation affects and
// raises a Draft note against each. Posting stays a finance action, matching
// the same split GenerateInvoiceForPackage keeps: an operations cancellation
// never writes to the GL by itself.

// IssueCancellationCreditNotes raises one Draft CreditNote per live invoice on
// a cancelled order, and returns the ids it created or found.
//
// Idempotent per invoice, which matters because a cancellation can be retried
// and because this is called from CancelOrder's own path: a second run returns
// the existing notes rather than doubling the credit.
//
// Draft invoices are deliberately skipped. A Draft invoice has not been posted,
// nothing was recognised, and nothing was declared - crediting it would create
// a reversal for a transaction that never happened. It is cancelled outright
// instead.
func IssueCancellationCreditNotes(tenantID, orderID, reasonCode, userID string) ([]string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(orderID) == "" {
		return nil, fmt.Errorf("order_id is required")
	}

	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, data, status FROM %s.documents
		 WHERE doctype = 'SalesInvoice' AND deleted_at IS NULL
		   AND data->>'sales_order_id' = $1
		 ORDER BY created_at, id`, schema), orderID)
	if err != nil {
		return nil, err
	}
	type invoiceRow struct {
		id, status string
		data       map[string]interface{}
	}
	var invoices []invoiceRow
	for rows.Next() {
		var r invoiceRow
		var dataStr string
		if err := rows.Scan(&r.id, &dataStr, &r.status); err != nil {
			rows.Close()
			return nil, err
		}
		if err := json.Unmarshal([]byte(dataStr), &r.data); err != nil {
			rows.Close()
			return nil, err
		}
		invoices = append(invoices, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	created := []string{}
	for _, inv := range invoices {
		switch inv.status {
		case "Cancelled":
			continue
		case "Draft":
			// Never posted, so there is nothing to reverse. Void it directly.
			if _, err := db.DB.Exec(fmt.Sprintf(
				`UPDATE %s.documents SET data = jsonb_set(jsonb_set(data, '{status}', '"Cancelled"'), '{cancel_reason}', to_jsonb($1::text)),
				        status = 'Cancelled', updated_at = CURRENT_TIMESTAMP
				  WHERE doctype = 'SalesInvoice' AND id = $2`, schema), reasonCode, inv.id); err != nil {
				return nil, err
			}
			LogAuditEvent(tenantID, userID, "CANCEL_SALES_INVOICE", "SUCCESS",
				fmt.Sprintf("Cancelled unposted draft invoice %s on cancellation of order %s (%s)", inv.id, orderID, reasonCode))
			continue
		}

		existing, err := existingCreditNoteForInvoice(schema, inv.id)
		if err != nil {
			return nil, err
		}
		if existing != "" {
			created = append(created, existing)
			continue
		}

		amount := int(numFromInterface(inv.data["total_amount"]))
		if amount <= 0 {
			continue
		}
		customer, _ := inv.data["customer"].(string)
		noteID := NewDocID("CRN")
		noteData := map[string]interface{}{
			"code":             noteID,
			"note_number":      noteID,
			"customer_id":      customer,
			"sales_order_id":   orderID,
			"sales_invoice_id": inv.id,
			"amount":           amount,
			"reason":           fmt.Sprintf("Order cancelled after invoicing (%s)", reasonCode),
			"status":           "Draft",
		}
		encoded, err := json.Marshal(noteData)
		if err != nil {
			return nil, err
		}
		if _, err := db.DB.Exec(fmt.Sprintf(
			`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'CreditNote', $2, 'Draft', 'system')`, schema),
			noteID, encoded); err != nil {
			return nil, err
		}
		LogAuditEvent(tenantID, userID, "CREATE_CREDIT_NOTE", "SUCCESS",
			fmt.Sprintf("Raised draft credit note %s for %d against invoice %s (order %s cancelled: %s)", noteID, amount, inv.id, orderID, reasonCode))
		created = append(created, noteID)
	}
	return created, nil
}

// existingCreditNoteForInvoice keeps the credit idempotent. A Cancelled note is
// not a match - voiding one is how an operator says "raise that again".
func existingCreditNoteForInvoice(schema, invoiceID string) (string, error) {
	var id string
	err := db.DB.QueryRow(fmt.Sprintf(`
		SELECT id FROM %s.documents
		 WHERE doctype = 'CreditNote' AND deleted_at IS NULL
		   AND data->>'sales_invoice_id' = $1 AND status <> 'Cancelled'
		 ORDER BY created_at, id LIMIT 1`, schema), invoiceID).Scan(&id)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return id, err
}

// cancelShippingPackagesForOrder voids any package on a cancelled order that
// has not physically shipped. Without this, cancelling an order would leave a
// Draft package that still looks bookable, and a booking made against it would
// then ship goods for an order nobody is going to pay for.
//
// A Shipped package is left alone - it is already gone, and the credit note
// above is the correct instrument for it.
func cancelShippingPackagesForOrder(tenantID, schema, orderID, reasonCode, userID string) error {
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id FROM %s.documents
		 WHERE doctype = 'ShippingPackage' AND deleted_at IS NULL
		   AND data->>'order_id' = $1 AND status IN ('Draft', 'Invoiced')`, schema), orderID)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range ids {
		if err := CancelShippingPackage(tenantID, id, reasonCode, userID); err != nil {
			return fmt.Errorf("cancel shipping package %s: %v", id, err)
		}
	}
	return nil
}
