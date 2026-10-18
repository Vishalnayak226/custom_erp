package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Stage 73.2.9 - customer reference on B2B sales. A business customer's own
// purchase order number (customer_reference) is entered on a sales order or a
// sales invoice, carried from the order onto the invoice raised from it
// (order_invoice.go, pack_invoice.go), and printed on the invoice
// (qz_payload.go BuildInvoicePayload, the browser print sheet). This report
// finds the orders and invoices by it - the question a B2B customer asks is
// "what happened to our PO 4500123?".

func customerReferenceReport(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	from, to := strings.TrimSpace(params["from_date"]), strings.TrimSpace(params["to_date"])
	if from == "" {
		from = "1900-01-01"
	}
	if to == "" {
		to = time.Now().Format("2006-01-02")
	}
	ref, customer := strings.TrimSpace(params["customer_reference"]), strings.TrimSpace(params["customer"])
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, doctype, status, data, to_char(created_at, 'YYYY-MM-DD') FROM %s.documents
		WHERE doctype IN ('SalesOrder', 'SalesInvoice') AND deleted_at IS NULL
		  AND COALESCE(data->>'customer_reference', '') <> ''
		  AND created_at >= $1::date AND created_at < ($2::date + 1)
		  AND ($3 = '' OR data->>'customer_reference' ILIKE '%%' || $3 || '%%')
		  AND ($4 = '' OR COALESCE(data->>'customer', data->>'customer_name', '') ILIKE '%%' || $4 || '%%')
		ORDER BY data->>'customer_reference', created_at LIMIT 5000`, schema), from, to, ref, customer)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]interface{}{}
	// Stage 76.11: an order is shown by its number on both rows - its own and
	// the invoice raised from it - not by the invoice's internal reference.
	orderNumbers := map[string]string{}
	orderNumber := func(id string) string {
		if id == "" {
			return ""
		}
		if n, ok := orderNumbers[id]; ok {
			return n
		}
		orderNumbers[id] = DocumentNumber(tenantID, "SalesOrder", id)
		return orderNumbers[id]
	}
	for rows.Next() {
		var id, doctype, status, dataStr, date string
		if err := rows.Scan(&id, &doctype, &status, &dataStr, &date); err != nil {
			return nil, err
		}
		var data map[string]interface{}
		_ = json.Unmarshal([]byte(dataStr), &data)
		row := map[string]interface{}{"customer_reference": strField(data, "customer_reference"), "date": date,
			"amount": round2(numFromInterface(data["total_amount"]))}
		if doctype == "SalesInvoice" {
			row["document"], row["number"] = "Sales Invoice", saleDocNumber(id, data)
			row["customer"], row["order"], row["status"] = strField(data, "customer"), orderNumber(strField(data, "sales_order_id")), status
		} else {
			row["document"], row["number"] = "Sales Order", saleDocNumber(id, data)
			row["customer"], row["order"], row["status"] = strField(data, "customer_name"), "", strField(data, "order_status")
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func init() {
	RegisterReport(ReportDefinition{
		ID: "sales-by-customer-reference", Label: "Sales by Customer Reference", Category: "Sales",
		Columns: []ReportColumn{{Key: "customer_reference", Label: "Customer PO / Ref"}, {Key: "document", Label: "Document"},
			{Key: "number", Label: "Number"}, {Key: "date", Label: "Date"}, {Key: "customer", Label: "Customer"},
			{Key: "order", Label: "From order"}, {Key: "amount", Label: "Amount"}, {Key: "status", Label: "Status"}},
		Params: []ReportParam{{Key: "from_date", Label: "From", Type: "date"}, {Key: "to_date", Label: "To", Type: "date"},
			{Key: "customer_reference", Label: "Customer PO / Ref", Type: "text"}, {Key: "customer", Label: "Customer", Type: "text"}},
		Run: customerReferenceReport,
	})
}
