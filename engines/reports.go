package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"log"
	"time"
)

// GetCurrentStockReport lists on-hand/available/reserved stock per SKU per
// location - the read model already backing the availability API, exposed
// here as a browsable report instead of a per-SKU lookup.
func GetCurrentStockReport(tenantID string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT sku, location_code, on_hand, available, committed, reserved, safety_stock
		FROM %s.inventory_availability ORDER BY location_code, sku`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []map[string]interface{}
	for rows.Next() {
		var sku, location string
		var onHand, available, committed, reserved, safetyStock int
		if err := rows.Scan(&sku, &location, &onHand, &available, &committed, &reserved, &safetyStock); err != nil {
			return nil, err
		}
		results = append(results, map[string]interface{}{
			"sku": sku, "location_code": location, "on_hand": onHand,
			"available": available, "committed": committed, "reserved": reserved, "safety_stock": safetyStock,
		})
	}
	return results, nil
}

// SalesRegisterEntry is one completed sale.
type SalesRegisterEntry struct {
	CartNumber  string    `json:"cart_number"`
	Location    string    `json:"location"`
	PaymentMode string    `json:"payment_mode"`
	Status      string    `json:"status"`
	SaleTotal   float64   `json:"sale_total"`
	CreatedAt   time.Time `json:"created_at"`
}

// GetSalesRegisterReport lists completed sales (Paid or Settled POSCarts).
// The sale total isn't a stored column - handleCheckout persists the raw
// checkout request (cart_number/location/payment_mode/items), so this sums
// qty*sale_price from the items array actually saved, the same figures the
// checkout response itself was computed from.
func GetSalesRegisterReport(tenantID string) ([]SalesRegisterEntry, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, data, status, created_at FROM %s.documents
		WHERE doctype = 'POSCart' AND status IN ('Paid', 'Settled')
		ORDER BY created_at DESC`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []SalesRegisterEntry
	for rows.Next() {
		var id, status, dataStr string
		var createdAt time.Time
		if err := rows.Scan(&id, &dataStr, &status, &createdAt); err != nil {
			return nil, err
		}
		var cart struct {
			Location    string `json:"location"`
			PaymentMode string `json:"payment_mode"`
			Items       []struct {
				Qty       float64 `json:"qty"`
				SalePrice float64 `json:"sale_price"`
			} `json:"items"`
		}
		if err := json.Unmarshal([]byte(dataStr), &cart); err != nil {
			// 24.18: distinct from the already-covered "old-shaped record
			// with no items field" case (valid JSON, just a different
			// shape, which unmarshals with err == nil and cart.Items empty
			// - that's the report's existing, verified "degrades to 0, not
			// a crash" behavior). This branch is genuinely malformed JSON -
			// skip the row rather than silently reporting it as a 0-value
			// sale.
			log.Printf("[REPORTS] corrupt POSCart %s: %v", id, err)
			continue
		}

		total := 0.0
		for _, item := range cart.Items {
			total += item.Qty * item.SalePrice
		}
		results = append(results, SalesRegisterEntry{
			CartNumber: id, Location: cart.Location, PaymentMode: cart.PaymentMode,
			Status: status, SaleTotal: total, CreatedAt: createdAt,
		})
	}
	return results, nil
}

// GetVendorLedgerReport lists PurchaseOrders, optionally filtered to one
// vendor - the closest thing to a vendor sub-ledger this codebase's data
// model supports today (no separate AP invoice/payment tracking exists).
func GetVendorLedgerReport(tenantID, vendorFilter string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	query := fmt.Sprintf(`
		SELECT id, data->>'vendor' AS vendor, data->>'po_number' AS po_number,
		       COALESCE((data->>'total_amount')::numeric, 0) AS total_amount, status, created_at
		FROM %s.documents WHERE doctype = 'PurchaseOrder'`, schema)
	var args []interface{}
	if vendorFilter != "" {
		query += " AND data->>'vendor' = $1"
		args = append(args, vendorFilter)
	}
	query += " ORDER BY data->>'vendor', created_at DESC"

	rows, err := db.DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []map[string]interface{}
	for rows.Next() {
		var id, vendor, poNumber, status string
		var totalAmount float64
		var createdAt time.Time
		if err := rows.Scan(&id, &vendor, &poNumber, &totalAmount, &status, &createdAt); err != nil {
			return nil, err
		}
		results = append(results, map[string]interface{}{
			"id": id, "vendor": vendor, "po_number": poNumber,
			"total_amount": totalAmount, "status": status, "created_at": createdAt,
		})
	}
	return results, nil
}

// PayablesAgeingBucket sums outstanding PurchaseOrder value by age.
type PayablesAgeingBucket struct {
	Bucket string  `json:"bucket"`
	Count  int     `json:"count"`
	Amount float64 `json:"amount"`
}

// GetPayablesAgeingReport buckets "Approved" (i.e. approved but not yet
// Closed - this codebase's data model has no separate paid/settled flag for
// POs, so Closed is the closest existing proxy for "no longer payable") POs
// by age since creation. This is a reasonable approximation given the
// existing status model, not a claim that a real AP sub-ledger exists.
func GetPayablesAgeingReport(tenantID string) ([]PayablesAgeingBucket, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT COALESCE((data->>'total_amount')::numeric, 0), created_at
		FROM %s.documents WHERE doctype = 'PurchaseOrder' AND status = 'Approved'`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	buckets := map[string]*PayablesAgeingBucket{
		"0-30":   {Bucket: "0-30 days"},
		"31-60":  {Bucket: "31-60 days"},
		"61-90":  {Bucket: "61-90 days"},
		"90plus": {Bucket: "90+ days"},
	}
	order := []string{"0-30", "31-60", "61-90", "90plus"}

	now := time.Now()
	for rows.Next() {
		var amount float64
		var createdAt time.Time
		if err := rows.Scan(&amount, &createdAt); err != nil {
			return nil, err
		}
		ageDays := int(now.Sub(createdAt).Hours() / 24)
		var key string
		switch {
		case ageDays <= 30:
			key = "0-30"
		case ageDays <= 60:
			key = "31-60"
		case ageDays <= 90:
			key = "61-90"
		default:
			key = "90plus"
		}
		buckets[key].Count++
		buckets[key].Amount += amount
	}

	results := make([]PayablesAgeingBucket, 0, len(order))
	for _, key := range order {
		results = append(results, *buckets[key])
	}
	return results, nil
}

// GetReceivablesAgeingReport buckets "Approved" (invoiced/AR-recognized but
// not yet Paid - see engines/sales_invoice.go) SalesInvoices by age since
// creation, mirroring GetPayablesAgeingReport's PurchaseOrder-Approved
// approximation on the receivable side (Stage 20.33).
func GetReceivablesAgeingReport(tenantID string) ([]PayablesAgeingBucket, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT COALESCE((data->>'total_amount')::numeric, 0), created_at
		FROM %s.documents WHERE doctype = 'SalesInvoice' AND status = 'Approved'`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	buckets := map[string]*PayablesAgeingBucket{
		"0-30":   {Bucket: "0-30 days"},
		"31-60":  {Bucket: "31-60 days"},
		"61-90":  {Bucket: "61-90 days"},
		"90plus": {Bucket: "90+ days"},
	}
	order := []string{"0-30", "31-60", "61-90", "90plus"}

	now := time.Now()
	for rows.Next() {
		var amount float64
		var createdAt time.Time
		if err := rows.Scan(&amount, &createdAt); err != nil {
			return nil, err
		}
		ageDays := int(now.Sub(createdAt).Hours() / 24)
		var key string
		switch {
		case ageDays <= 30:
			key = "0-30"
		case ageDays <= 60:
			key = "31-60"
		case ageDays <= 90:
			key = "61-90"
		default:
			key = "90plus"
		}
		buckets[key].Count++
		buckets[key].Amount += amount
	}

	results := make([]PayablesAgeingBucket, 0, len(order))
	for _, key := range order {
		results = append(results, *buckets[key])
	}
	return results, nil
}

// GSTReturnSummary is a periodic GSTR-1/3B-shaped aggregation of GST
// already calculated and posted per-transaction (Stage 13.10/17.5's
// PostSalesGSTBooking) - report-only (Stage 20.29), explicitly not
// e-filing/IRN (that's Stage 20.30/20.31, blocked on real GSP credentials).
// Derived entirely from gl_postings: TaxableValue is account 4100's net
// balance in the window (Sales Revenue net of the tax portion
// PostSalesGSTBooking moves back out of it), and the three tax accounts are
// each account's net credit balance in the same window.
type GSTReturnSummary struct {
	StartDate         string  `json:"start_date"`
	EndDate           string  `json:"end_date"`
	TaxableValue      float64 `json:"taxable_value"`
	OutputCGST        float64 `json:"output_cgst"`
	OutputSGST        float64 `json:"output_sgst"`
	OutputIGST        float64 `json:"output_igst"`
	TotalTaxLiability float64 `json:"total_tax_liability"`
	TransactionCount  int     `json:"transaction_count"`
}

func glAccountNetBalance(schema, accountCode, startDate, endDate string) (net float64, err error) {
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT COALESCE(SUM(credit), 0) - COALESCE(SUM(debit), 0) FROM %s.gl_postings
		WHERE account_code = $1 AND created_at >= $2::date AND created_at < ($3::date + 1)`, schema),
		accountCode, startDate, endDate).Scan(&net)
	return net, err
}

// GetGSTReturnSummary aggregates output-tax liability for [startDate, endDate]
// (inclusive, "YYYY-MM-DD").
func GetGSTReturnSummary(tenantID, startDate, endDate string) (*GSTReturnSummary, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	if startDate == "" || endDate == "" {
		return nil, fmt.Errorf("start_date and end_date are required")
	}

	taxableValue, err := glAccountNetBalance(schema, "4100", startDate, endDate)
	if err != nil {
		return nil, err
	}
	cgst, err := glAccountNetBalance(schema, "2200", startDate, endDate)
	if err != nil {
		return nil, err
	}
	sgst, err := glAccountNetBalance(schema, "2201", startDate, endDate)
	if err != nil {
		return nil, err
	}
	igst, err := glAccountNetBalance(schema, "2202", startDate, endDate)
	if err != nil {
		return nil, err
	}

	var txnCount int
	if err := db.DB.QueryRow(fmt.Sprintf(`
		SELECT COUNT(DISTINCT document_id) FROM %s.gl_postings
		WHERE account_code = '4100' AND created_at >= $1::date AND created_at < ($2::date + 1)`, schema),
		startDate, endDate).Scan(&txnCount); err != nil {
		return nil, err
	}

	return &GSTReturnSummary{
		StartDate: startDate, EndDate: endDate,
		TaxableValue: taxableValue, OutputCGST: cgst, OutputSGST: sgst, OutputIGST: igst,
		TotalTaxLiability: cgst + sgst + igst, TransactionCount: txnCount,
	}, nil
}
