package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"log"
	"time"
)

// init registers every report into the Stage 20 Track B.4 catalog -
// existing reports.go functions wrapped as-is, plus the Stage 20.40 catalog
// additions. Registration order is the catalog's display order.
func init() {
	RegisterReport(ReportDefinition{
		ID: "current-stock", Label: "Current Stock", Category: "Inventory",
		Columns: []ReportColumn{
			{Key: "sku", Label: "SKU"}, {Key: "location_code", Label: "Location"},
			{Key: "on_hand", Label: "On Hand"}, {Key: "available", Label: "Available"},
			{Key: "committed", Label: "Committed"}, {Key: "reserved", Label: "Reserved"},
			{Key: "safety_stock", Label: "Safety Stock"},
		},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			return GetCurrentStockReport(tenantID)
		},
	})

	RegisterReport(ReportDefinition{
		ID: "sales-register", Label: "Sales Register", Category: "Sales",
		Columns: []ReportColumn{
			{Key: "cart_number", Label: "Cart Number"}, {Key: "location", Label: "Location"},
			{Key: "payment_mode", Label: "Payment Mode"}, {Key: "status", Label: "Status"},
			{Key: "sale_total", Label: "Sale Total", Sensitive: true}, {Key: "created_at", Label: "Date"},
		},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			entries, err := GetSalesRegisterReport(tenantID)
			if err != nil {
				return nil, err
			}
			return structsToRows(entries)
		},
	})

	RegisterReport(ReportDefinition{
		ID: "vendor-ledger", Label: "Vendor Ledger", Category: "Procurement",
		Columns: []ReportColumn{
			{Key: "id", Label: "PO ID"}, {Key: "vendor", Label: "Vendor"}, {Key: "po_number", Label: "PO Number"},
			{Key: "total_amount", Label: "Total Amount", Sensitive: true}, {Key: "status", Label: "Status"},
			{Key: "created_at", Label: "Date"},
		},
		Params: []ReportParam{{Key: "vendor_id", Label: "Vendor (optional)", Type: "text"}},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			return GetVendorLedgerReport(tenantID, params["vendor_id"])
		},
	})

	RegisterReport(ReportDefinition{
		ID: "payables-ageing", Label: "Payables Ageing", Category: "Finance",
		Columns: []ReportColumn{
			{Key: "bucket", Label: "Age Bucket"}, {Key: "count", Label: "PO Count"},
			{Key: "amount", Label: "Outstanding Amount", Sensitive: true},
		},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			buckets, err := GetPayablesAgeingReport(tenantID)
			if err != nil {
				return nil, err
			}
			return structsToRows(buckets)
		},
		DrillDown: func(tenantID, rowKey string, params map[string]string) ([]map[string]interface{}, error) {
			return ageingBucketDrillDown(tenantID, "PurchaseOrder", "Approved", "total_amount", rowKey)
		},
	})

	RegisterReport(ReportDefinition{
		ID: "receivables-ageing", Label: "Receivables Ageing", Category: "Finance",
		Columns: []ReportColumn{
			{Key: "bucket", Label: "Age Bucket"}, {Key: "count", Label: "Invoice Count"},
			{Key: "amount", Label: "Outstanding Amount", Sensitive: true},
		},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			buckets, err := GetReceivablesAgeingReport(tenantID)
			if err != nil {
				return nil, err
			}
			return structsToRows(buckets)
		},
		DrillDown: func(tenantID, rowKey string, params map[string]string) ([]map[string]interface{}, error) {
			return ageingBucketDrillDown(tenantID, "SalesInvoice", "Approved", "total_amount", rowKey)
		},
	})

	RegisterReport(ReportDefinition{
		ID: "gst-return-summary", Label: "GST Return Summary", Category: "Finance",
		Columns: []ReportColumn{
			{Key: "start_date", Label: "From"}, {Key: "end_date", Label: "To"},
			{Key: "taxable_value", Label: "Taxable Value", Sensitive: true},
			{Key: "output_cgst", Label: "Output CGST", Sensitive: true},
			{Key: "output_sgst", Label: "Output SGST", Sensitive: true},
			{Key: "output_igst", Label: "Output IGST", Sensitive: true},
			{Key: "total_tax_liability", Label: "Total Tax Liability", Sensitive: true},
			{Key: "transaction_count", Label: "Transactions"},
		},
		Params: []ReportParam{
			{Key: "start", Label: "From", Type: "date", Required: true},
			{Key: "end", Label: "To", Type: "date", Required: true},
		},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			summary, err := GetGSTReturnSummary(tenantID, params["start"], params["end"])
			if err != nil {
				return nil, err
			}
			return structsToRows(summary)
		},
		DrillDown: func(tenantID, rowKey string, params map[string]string) ([]map[string]interface{}, error) {
			return gstReturnDrillDown(tenantID, params["start"], params["end"])
		},
	})

	RegisterReport(ReportDefinition{
		ID: "grn-register", Label: "GRN Register", Category: "Procurement",
		Columns: []ReportColumn{
			{Key: "id", Label: "GRN ID"}, {Key: "po_id", Label: "PO Reference"},
			{Key: "item_lines", Label: "Item Lines"}, {Key: "status", Label: "Status"}, {Key: "created_at", Label: "Date"},
		},
		Run: getGRNRegisterReport,
	})

	RegisterReport(ReportDefinition{
		ID: "cash-book", Label: "Cash Book", Category: "Finance",
		Columns: []ReportColumn{
			{Key: "created_at", Label: "Date"}, {Key: "document_type", Label: "Document Type"},
			{Key: "document_id", Label: "Document ID"}, {Key: "debit", Label: "Debit", Sensitive: true},
			{Key: "credit", Label: "Credit", Sensitive: true}, {Key: "balance", Label: "Running Balance", Sensitive: true},
		},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			return getGLAccountBook(tenantID, "1100")
		},
	})

	RegisterReport(ReportDefinition{
		ID: "bank-book", Label: "Bank Book", Category: "Finance",
		Columns: []ReportColumn{
			{Key: "created_at", Label: "Date"}, {Key: "document_type", Label: "Document Type"},
			{Key: "document_id", Label: "Document ID"}, {Key: "debit", Label: "Debit", Sensitive: true},
			{Key: "credit", Label: "Credit", Sensitive: true}, {Key: "balance", Label: "Running Balance", Sensitive: true},
		},
		Params: []ReportParam{{Key: "bank_account", Label: "Bank Account", Type: "text", Required: true}},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			glCode, err := bankAccountGLCode(tenantID, params["bank_account"])
			if err != nil {
				return nil, err
			}
			return getGLAccountBook(tenantID, glCode)
		},
	})

	RegisterReport(ReportDefinition{
		ID: "asset-register", Label: "Asset Register", Category: "Assets",
		Columns: []ReportColumn{
			{Key: "id", Label: "Asset ID"}, {Key: "code", Label: "Code"}, {Key: "category", Label: "Category"},
			{Key: "cost", Label: "Cost", Sensitive: true}, {Key: "location", Label: "Location"},
			{Key: "custodian", Label: "Custodian"}, {Key: "status", Label: "Status"},
			{Key: "accumulated_depreciation", Label: "Accumulated Depreciation", Sensitive: true},
			{Key: "net_block", Label: "Net Block", Sensitive: true},
		},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			entries, err := GetAssetRegister(tenantID)
			if err != nil {
				return nil, err
			}
			return structsToRows(entries)
		},
	})

	RegisterReport(ReportDefinition{
		ID: "loyalty-summary", Label: "Loyalty Ledger Summary", Category: "CRM",
		Columns: []ReportColumn{
			{Key: "customer_id", Label: "Customer"}, {Key: "total_earned", Label: "Total Earned"},
			{Key: "total_burned", Label: "Total Redeemed"}, {Key: "balance", Label: "Current Balance", Sensitive: true},
			{Key: "transaction_count", Label: "Transactions"},
		},
		Run: getLoyaltySummaryReport,
	})

	RegisterReport(ReportDefinition{
		ID: "production-order-status", Label: "Production Order Status", Category: "Manufacturing",
		Columns: []ReportColumn{
			{Key: "id", Label: "Order ID"}, {Key: "bom_id", Label: "BOM"}, {Key: "item", Label: "Finished Item"},
			{Key: "qty", Label: "Qty"}, {Key: "status", Label: "Status"}, {Key: "created_at", Label: "Date"},
		},
		Run: getProductionOrderStatusReport,
	})

	RegisterReport(ReportDefinition{
		ID: "rfq-comparison", Label: "RFQ Comparison Export", Category: "Procurement",
		Columns: []ReportColumn{
			{Key: "id", Label: "Quote ID"}, {Key: "vendor_id", Label: "Vendor"},
			{Key: "quoted_price", Label: "Quoted Price", Sensitive: true}, {Key: "status", Label: "Status"},
		},
		Params: []ReportParam{{Key: "rfq_id", Label: "RFQ ID", Type: "text", Required: true}},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			return GetVendorQuotesForRFQ(tenantID, params["rfq_id"])
		},
	})
}

// ageBucketLabel re-derives the exact same boundaries
// GetPayablesAgeingReport/GetReceivablesAgeingReport use (0-30/31-60/61-90/90+
// days), kept in one place so a drill-down can never disagree with the
// summary report it's drilling into.
func ageBucketLabel(ageDays int) string {
	switch {
	case ageDays <= 30:
		return "0-30 days"
	case ageDays <= 60:
		return "31-60 days"
	case ageDays <= 90:
		return "61-90 days"
	default:
		return "90+ days"
	}
}

// ageingBucketDrillDown filters doctype/status documents to the ones that
// roll up into one age bucket (Stage 20.38), so a click on a Payables/
// Receivables Ageing summary row shows exactly the documents behind it.
func ageingBucketDrillDown(tenantID, doctype, status, amountField, bucketLabel string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, data, COALESCE((data->>$1)::numeric, 0), created_at
		FROM %s.documents WHERE doctype = $2 AND status = $3`, schema), amountField, doctype, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	now := time.Now()
	var results []map[string]interface{}
	for rows.Next() {
		var id, dataStr string
		var amount float64
		var createdAt time.Time
		if err := rows.Scan(&id, &dataStr, &amount, &createdAt); err != nil {
			return nil, err
		}
		if ageBucketLabel(int(now.Sub(createdAt).Hours()/24)) != bucketLabel {
			continue
		}
		// 24.18: dataStr/data was scanned and unmarshaled but never
		// actually read - dead code, removed rather than given error
		// handling for a value nothing uses.
		results = append(results, map[string]interface{}{
			"id": id, amountField: amount, "created_at": createdAt,
		})
	}
	if results == nil {
		results = []map[string]interface{}{}
	}
	return results, nil
}

func getGRNRegisterReport(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, data, status, created_at FROM %s.documents WHERE doctype = 'GRN' ORDER BY created_at DESC`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var results []map[string]interface{}
	for rows.Next() {
		var id, dataStr, status string
		var createdAt time.Time
		if err := rows.Scan(&id, &dataStr, &status, &createdAt); err != nil {
			return nil, err
		}
		var data map[string]interface{}
		if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
			// 24.18: id/status/created_at come from real columns, not the
			// corrupt JSON, so the row is still worth surfacing (with
			// po_id/item_lines defaulted) rather than dropped outright -
			// this is a diagnostic report, not a financial total.
			log.Printf("[REPORTS] corrupt GRN %s: %v", id, err)
		}
		poID, _ := data["po_id"].(string)
		itemLines := 0
		if receivedStr, ok := data["received_items"].(string); ok && receivedStr != "" {
			var items []grnItemLine
			if json.Unmarshal([]byte(receivedStr), &items) == nil {
				itemLines = len(items)
			}
		}
		results = append(results, map[string]interface{}{
			"id": id, "po_id": poID, "item_lines": itemLines, "status": status, "created_at": createdAt,
		})
	}
	return results, nil
}

// getGLAccountBook lists every gl_postings row for one account
// chronologically with a running balance - the shared query behind both
// Cash Book (fixed account 1100) and Bank Book (a BankAccount's own
// gl_account_code).
func getGLAccountBook(tenantID, accountCode string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT document_type, document_id, debit, credit, created_at FROM %s.gl_postings
		WHERE account_code = $1 ORDER BY created_at ASC`, schema), accountCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var results []map[string]interface{}
	balance := 0
	for rows.Next() {
		var docType, docID string
		var debit, credit int
		var createdAt time.Time
		if err := rows.Scan(&docType, &docID, &debit, &credit, &createdAt); err != nil {
			return nil, err
		}
		balance += debit - credit
		results = append(results, map[string]interface{}{
			"document_type": docType, "document_id": docID, "debit": debit, "credit": credit,
			"balance": balance, "created_at": createdAt,
		})
	}
	if results == nil {
		results = []map[string]interface{}{}
	}
	return results, nil
}

func bankAccountGLCode(tenantID, bankAccountID string) (string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	var dataStr string
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT data FROM %s.documents WHERE doctype = 'BankAccount' AND id = $1`, schema), bankAccountID).Scan(&dataStr); err != nil {
		return "", fmt.Errorf("bank account not found: %v", err)
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		// 24.18: this resolves an actual GL account code to post/report
		// against - a corrupt record must surface as an error, not silently
		// fall through to the "no gl_account_code configured" message below
		// (which would misdescribe a data-corruption problem as a
		// configuration one).
		return "", fmt.Errorf("bank account %s has corrupt stored data: %v", bankAccountID, err)
	}
	code, _ := data["gl_account_code"].(string)
	if code == "" {
		return "", fmt.Errorf("bank account %s has no gl_account_code configured", bankAccountID)
	}
	return code, nil
}

func getLoyaltySummaryReport(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT customer_id,
		       COALESCE(SUM(CASE WHEN transaction_type = 'Earn' THEN points ELSE 0 END), 0) AS total_earned,
		       COALESCE(SUM(CASE WHEN transaction_type = 'Burn' THEN points ELSE 0 END), 0) AS total_burned,
		       COUNT(*) AS transaction_count
		FROM %s.loyalty_point_ledger GROUP BY customer_id ORDER BY customer_id`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var results []map[string]interface{}
	for rows.Next() {
		var customerID string
		var totalEarned, totalBurned, txnCount int
		if err := rows.Scan(&customerID, &totalEarned, &totalBurned, &txnCount); err != nil {
			return nil, err
		}
		results = append(results, map[string]interface{}{
			"customer_id": customerID, "total_earned": totalEarned, "total_burned": totalBurned,
			"balance": totalEarned - totalBurned, "transaction_count": txnCount,
		})
	}
	if results == nil {
		results = []map[string]interface{}{}
	}
	return results, nil
}

func getProductionOrderStatusReport(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, data, status, created_at FROM %s.documents WHERE doctype = 'ProductionOrder' ORDER BY created_at DESC`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var results []map[string]interface{}
	for rows.Next() {
		var id, dataStr, status string
		var createdAt time.Time
		if err := rows.Scan(&id, &dataStr, &status, &createdAt); err != nil {
			return nil, err
		}
		var data map[string]interface{}
		if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
			log.Printf("[REPORTS] corrupt ProductionOrder %s: %v", id, err)
		}
		bomID, _ := data["bom_id"].(string)
		item, _ := data["item"].(string)
		qty := numFromInterface(data["qty"])
		results = append(results, map[string]interface{}{
			"id": id, "bom_id": bomID, "item": item, "qty": qty, "status": status, "created_at": createdAt,
		})
	}
	if results == nil {
		results = []map[string]interface{}{}
	}
	return results, nil
}

// gstReturnDrillDown shows the individual gl_postings rows behind the GST
// Return Summary's four aggregated accounts (4100 net taxable value plus
// the three tax-payable accounts) for the same [start, end] window.
func gstReturnDrillDown(tenantID, startDate, endDate string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	if startDate == "" || endDate == "" {
		return nil, fmt.Errorf("start and end are required")
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT account_code, document_type, document_id, debit, credit, created_at FROM %s.gl_postings
		WHERE account_code IN ('4100', '2200', '2201', '2202') AND created_at >= $1::date AND created_at < ($2::date + 1)
		ORDER BY created_at ASC`, schema), startDate, endDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var results []map[string]interface{}
	for rows.Next() {
		var accountCode, docType, docID string
		var debit, credit int
		var createdAt time.Time
		if err := rows.Scan(&accountCode, &docType, &docID, &debit, &credit, &createdAt); err != nil {
			return nil, err
		}
		results = append(results, map[string]interface{}{
			"account_code": accountCode, "document_type": docType, "document_id": docID,
			"debit": debit, "credit": credit, "created_at": createdAt,
		})
	}
	if results == nil {
		results = []map[string]interface{}{}
	}
	return results, nil
}
