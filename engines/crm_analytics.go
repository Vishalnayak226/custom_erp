package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"time"
)

// Stage 26.7.9/26.7.10/26.7.11 (CRM/Loyalty Sprint P2 follow-up): customer
// householding/merge, CLV/cohort/churn analytics, two-way CleverTap segment
// sync. Go-ahead given 2026-07-27 for all five P2 bundles previously
// deferred pending a real pilot customer/measured need - built as generic
// capability extending the same customer-linked data 26.7.1's RFM
// segmentation and 26.7.7's Customer 360 already read.

func init() {
	RegisterReport(ReportDefinition{
		ID: "customer-lifetime-value", Label: "Customer Lifetime Value", Category: "CRM",
		Columns: []ReportColumn{
			{Key: "customer_id", Label: "Customer"}, {Key: "lifetime_value", Label: "Lifetime Value", Sensitive: true},
			{Key: "order_count", Label: "Orders"}, {Key: "first_order_at", Label: "First Order"},
			{Key: "last_order_at", Label: "Last Order"}, {Key: "churn_flag", Label: "Churned?"},
		},
		Params: []ReportParam{{Key: "churn_days", Label: "Churn Threshold Days (default 90)", Type: "text"}},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			days := int(numFromInterface(params["churn_days"]))
			rows, err := GetCustomerLifetimeValue(tenantID, days)
			if err != nil {
				return nil, err
			}
			return structsToRows(rows)
		},
	})

	RegisterReport(ReportDefinition{
		ID: "cohort-retention", Label: "Cohort Retention", Category: "CRM",
		Columns: []ReportColumn{
			{Key: "cohort_month", Label: "Cohort Month"}, {Key: "cohort_size", Label: "Cohort Size"},
			{Key: "month_offset", Label: "Month Offset"}, {Key: "retained_count", Label: "Retained"},
		},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			rows, err := GetCohortRetention(tenantID)
			if err != nil {
				return nil, err
			}
			return structsToRows(rows)
		},
	})
}

// MergeCustomers (26.7.9) reassigns every POSCart/SalesInvoice/Voucher/
// loyalty_point_ledger row from a duplicate customer record onto the
// surviving primary, then marks the duplicate Merged rather than deleting
// it - a lookup on the old id can still resolve forward via merged_into,
// and the existing append-only audit trigger already logs every
// reassignment UPDATE this makes.
func MergeCustomers(tenantID, primaryID, duplicateID, userID string) error {
	if primaryID == "" || duplicateID == "" {
		return &ValidationError{Code: "GLOBAL-0002", Message: "primary_customer_id and duplicate_customer_id are both required"}
	}
	if primaryID == duplicateID {
		return &ValidationError{Code: "GLOBAL-0002", Message: "primary_customer_id and duplicate_customer_id must be different"}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}

	var primaryStatus, dupStatus string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT status FROM %s.documents WHERE doctype = 'Customer' AND id = $1`, schema), primaryID).Scan(&primaryStatus); err != nil {
		return &ValidationError{Code: "GLOBAL-0004", Message: fmt.Sprintf("primary customer %s not found", primaryID)}
	}
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT status FROM %s.documents WHERE doctype = 'Customer' AND id = $1`, schema), duplicateID).Scan(&dupStatus); err != nil {
		return &ValidationError{Code: "GLOBAL-0004", Message: fmt.Sprintf("duplicate customer %s not found", duplicateID)}
	}
	if dupStatus == "Merged" {
		return &ValidationError{Code: "GLOBAL-0002", Message: fmt.Sprintf("customer %s is already merged into another record", duplicateID)}
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	reassign := []struct{ doctype, field string }{
		{"POSCart", "customer_id"},
		{"SalesInvoice", "customer"},
		{"Voucher", "customer_id"},
	}
	for _, r := range reassign {
		if _, err := tx.Exec(fmt.Sprintf(
			`UPDATE %s.documents SET data = jsonb_set(data, '{%s}', to_jsonb($1::text)), updated_at = CURRENT_TIMESTAMP WHERE doctype = $2 AND data->>'%s' = $3`,
			schema, r.field, r.field), primaryID, r.doctype, duplicateID); err != nil {
			return fmt.Errorf("failed reassigning %s: %v", r.doctype, err)
		}
	}
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.loyalty_point_ledger SET customer_id = $1 WHERE customer_id = $2`, schema), primaryID, duplicateID); err != nil {
		return fmt.Errorf("failed reassigning loyalty ledger: %v", err)
	}

	var dupDataStr string
	if err := tx.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents WHERE doctype = 'Customer' AND id = $1`, schema), duplicateID).Scan(&dupDataStr); err != nil {
		return err
	}
	var dupData map[string]interface{}
	if err := json.Unmarshal([]byte(dupDataStr), &dupData); err != nil {
		return err
	}
	dupData["merged_into"] = primaryID
	marshaled, err := json.Marshal(dupData)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $1, status = 'Merged', updated_at = CURRENT_TIMESTAMP WHERE doctype = 'Customer' AND id = $2`, schema), marshaled, duplicateID); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	LogAuditEvent(tenantID, userID, "CUSTOMER_MERGE", "SUCCESS", fmt.Sprintf("merged %s into %s", duplicateID, primaryID))
	return nil
}

// CustomerLifetimeValue (26.7.10): lifetime Paid revenue per customer
// across both POSCart and SalesInvoice, same two revenue sources 26.6.1's
// P&L report already reads, unioned by customer.
type CustomerLifetimeValue struct {
	CustomerID    string  `json:"customer_id"`
	LifetimeValue float64 `json:"lifetime_value"`
	OrderCount    int     `json:"order_count"`
	FirstOrderAt  string  `json:"first_order_at"`
	LastOrderAt   string  `json:"last_order_at"`
	ChurnFlag     bool    `json:"churn_flag"`
}

func GetCustomerLifetimeValue(tenantID string, churnDays int) ([]CustomerLifetimeValue, error) {
	if churnDays <= 0 {
		churnDays = 90
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		WITH orders AS (
			SELECT data->>'customer_id' AS customer_id, COALESCE((data->>'total_sale_price')::numeric, 0) AS amount, created_at
			FROM %s.documents WHERE doctype = 'POSCart' AND status = 'Paid' AND COALESCE(data->>'customer_id', '') != ''
			UNION ALL
			SELECT data->>'customer' AS customer_id, COALESCE((data->>'total_amount')::numeric, 0) AS amount, created_at
			FROM %s.documents WHERE doctype = 'SalesInvoice' AND COALESCE(data->>'customer', '') != ''
		)
		SELECT customer_id, SUM(amount), COUNT(*), MIN(created_at), MAX(created_at)
		FROM orders GROUP BY customer_id ORDER BY SUM(amount) DESC`, schema, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cutoff := time.Now().AddDate(0, 0, -churnDays)
	out := []CustomerLifetimeValue{}
	for rows.Next() {
		var c CustomerLifetimeValue
		var first, last time.Time
		if err := rows.Scan(&c.CustomerID, &c.LifetimeValue, &c.OrderCount, &first, &last); err != nil {
			continue
		}
		c.LifetimeValue = roundTo2(c.LifetimeValue)
		c.FirstOrderAt = first.Format("2006-01-02")
		c.LastOrderAt = last.Format("2006-01-02")
		c.ChurnFlag = last.Before(cutoff)
		out = append(out, c)
	}
	return out, nil
}

// CohortRetention (26.7.10): buckets customers by their first-ever order's
// month (the "cohort"), then reports how many of that cohort placed
// another order in each subsequent month offset - a standard cohort-
// retention shape, computed from the same orders CLV above reads.
type CohortRow struct {
	CohortMonth   string `json:"cohort_month"`
	CohortSize    int    `json:"cohort_size"`
	MonthOffset   int    `json:"month_offset"`
	RetainedCount int    `json:"retained_count"`
}

func GetCohortRetention(tenantID string) ([]CohortRow, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		WITH orders AS (
			SELECT data->>'customer_id' AS customer_id, created_at
			FROM %s.documents WHERE doctype = 'POSCart' AND status = 'Paid' AND COALESCE(data->>'customer_id', '') != ''
			UNION ALL
			SELECT data->>'customer' AS customer_id, created_at
			FROM %s.documents WHERE doctype = 'SalesInvoice' AND COALESCE(data->>'customer', '') != ''
		),
		first_order AS (
			SELECT customer_id, date_trunc('month', MIN(created_at)) AS cohort_month
			FROM orders GROUP BY customer_id
		),
		cohort_sizes AS (
			SELECT cohort_month, COUNT(*) AS cohort_size FROM first_order GROUP BY cohort_month
		)
		SELECT to_char(f.cohort_month, 'YYYY-MM'), cs.cohort_size,
		       (date_part('year', date_trunc('month', o.created_at)) - date_part('year', f.cohort_month)) * 12 +
		       (date_part('month', date_trunc('month', o.created_at)) - date_part('month', f.cohort_month)) AS month_offset,
		       COUNT(DISTINCT o.customer_id)
		FROM orders o
		JOIN first_order f ON f.customer_id = o.customer_id
		JOIN cohort_sizes cs ON cs.cohort_month = f.cohort_month
		GROUP BY f.cohort_month, cs.cohort_size, month_offset
		ORDER BY f.cohort_month ASC, month_offset ASC`, schema, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []CohortRow{}
	for rows.Next() {
		var c CohortRow
		var monthOffset float64
		if err := rows.Scan(&c.CohortMonth, &c.CohortSize, &monthOffset, &c.RetainedCount); err != nil {
			continue
		}
		c.MonthOffset = int(monthOffset)
		out = append(out, c)
	}
	return out, nil
}

// ReceiveCleverTapSegmentSync (26.7.11): the inbound half of "two-way"
// segment sync - LogCustomerEventToCleverTap (Stage 26.7.4) already pushes
// events out; this receives a segment-membership push back in, verified
// against the same clevertap_credentials.passcode SaveCleverTapCredential
// stores, same "per-tenant shared-secret header" shape
// VerifyBigCommerceWebhook's caller uses.
func getCleverTapPasscode(tenantID string) (string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	var passcode string
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT passcode FROM %s.clevertap_credentials WHERE active = TRUE ORDER BY created_at DESC LIMIT 1`, schema)).Scan(&passcode)
	return passcode, err
}

func VerifyCleverTapWebhookPasscode(tenantID, provided string) bool {
	if provided == "" {
		return false
	}
	stored, err := getCleverTapPasscode(tenantID)
	if err != nil || stored == "" {
		return false
	}
	return stored == provided
}

func ReceiveCleverTapSegmentSync(tenantID, customerID string, segments []string) error {
	if customerID == "" {
		return &ValidationError{Code: "GLOBAL-0002", Message: "customer_id is required"}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	segJSON, err := json.Marshal(segments)
	if err != nil {
		return err
	}
	res, err := db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = jsonb_set(data, '{clevertap_segments}', to_jsonb($1::text)), updated_at = CURRENT_TIMESTAMP WHERE doctype = 'Customer' AND id = $2`, schema),
		string(segJSON), customerID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return &ValidationError{Code: "GLOBAL-0004", Message: fmt.Sprintf("customer %s not found", customerID)}
	}
	LogAuditEvent(tenantID, "clevertap", "CLEVERTAP_SEGMENT_SYNC_RECEIVED", "SUCCESS", fmt.Sprintf("customer=%s segments=%v", customerID, segments))
	return nil
}
