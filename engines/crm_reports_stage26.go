package engines

import (
	"custom_erp/db"
	"database/sql"
	"fmt"
	"sort"
	"time"
)

// Stage 26.7: RFM segmentation, points-liability, and Customer 360 - new
// ReportDefinition entries over existing POSCart/SalesInvoice/loyalty-ledger
// data (engines/report_registry.go's catalog), same pattern as
// engines/report_definitions.go's existing "loyalty-summary" report. No new
// customer data model. Campaign-ROI (also 26.7.6) is registered alongside
// the new Campaign doctype once that lands, since it has nothing to report
// on until then.

// RFMCustomerSegment is one customer's Recency/Frequency/Monetary scoring.
type RFMCustomerSegment struct {
	CustomerID  string  `json:"customer_id"`
	RecencyDays int     `json:"recency_days"`
	Frequency   int     `json:"frequency"`
	Monetary    float64 `json:"monetary"`
	RScore      int     `json:"r_score"`
	FScore      int     `json:"f_score"`
	MScore      int     `json:"m_score"`
	Segment     string  `json:"segment"`
}

// quintileScores buckets each value into a 1-5 score by its rank among the
// others (not a fixed magnitude threshold, since "high spend" is relative
// to this tenant's own customer base) - a simple rank-based heuristic, not
// a statistical model. higherIsBetter=false means the smallest raw value
// (e.g. recency in days - fewer is better) earns the highest score.
func quintileScores(values []float64, higherIsBetter bool) []int {
	n := len(values)
	idx := make([]int, n)
	for i := range idx {
		idx[i] = i
	}
	sort.Slice(idx, func(a, b int) bool {
		if higherIsBetter {
			return values[idx[a]] < values[idx[b]]
		}
		return values[idx[a]] > values[idx[b]]
	})
	scores := make([]int, n)
	for rank, i := range idx {
		s := (rank * 5 / n) + 1
		if s > 5 {
			s = 5
		}
		scores[i] = s
	}
	return scores
}

// rfmSegmentLabel buckets a combined R+F+M score (range 3-15) into a
// human-readable segment - standard RFM segment names, not a proprietary
// model.
func rfmSegmentLabel(total int) string {
	switch {
	case total >= 13:
		return "Champions"
	case total >= 10:
		return "Loyal Customers"
	case total >= 7:
		return "Potential Loyalists"
	case total >= 4:
		return "At Risk"
	default:
		return "Lost"
	}
}

// GetRFMSegmentation scores every customer with at least one Paid POSCart
// on Recency/Frequency/Monetary. POSCart (not SalesInvoice) is the source
// since its customer_id is the real join key the loyalty ledger also uses
// (SalesInvoice's "customer" field is free text, no enforced identity).
func GetRFMSegmentation(tenantID string) ([]RFMCustomerSegment, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT data->>'customer_id' AS customer_id, MAX(created_at) AS last_txn,
		       COUNT(*) AS frequency, COALESCE(SUM((data->>'amount_paid')::numeric), 0) AS monetary
		FROM %s.documents
		WHERE doctype = 'POSCart' AND status = 'Paid' AND COALESCE(data->>'customer_id', '') <> ''
		GROUP BY data->>'customer_id'`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type raw struct {
		customerID string
		lastTxn    time.Time
		frequency  int
		monetary   float64
	}
	var customers []raw
	for rows.Next() {
		var r raw
		if err := rows.Scan(&r.customerID, &r.lastTxn, &r.frequency, &r.monetary); err != nil {
			return nil, err
		}
		customers = append(customers, r)
	}
	if len(customers) == 0 {
		return []RFMCustomerSegment{}, nil
	}

	now := time.Now()
	recency := make([]float64, len(customers))
	frequency := make([]float64, len(customers))
	monetary := make([]float64, len(customers))
	for i, c := range customers {
		recency[i] = now.Sub(c.lastTxn).Hours() / 24
		frequency[i] = float64(c.frequency)
		monetary[i] = c.monetary
	}
	rScores := quintileScores(recency, false)
	fScores := quintileScores(frequency, true)
	mScores := quintileScores(monetary, true)

	out := make([]RFMCustomerSegment, len(customers))
	for i, c := range customers {
		total := rScores[i] + fScores[i] + mScores[i]
		out[i] = RFMCustomerSegment{
			CustomerID: c.customerID, RecencyDays: int(recency[i]), Frequency: c.frequency, Monetary: c.monetary,
			RScore: rScores[i], FScore: fScores[i], MScore: mScores[i], Segment: rfmSegmentLabel(total),
		}
	}
	return out, nil
}

// PointsLiabilitySummary is the tenant-wide outstanding loyalty-point
// liability - every unredeemed point is a future discount the business
// owes, at the same redemptionValuePerPoint rate checkout uses today.
type PointsLiabilitySummary struct {
	TotalOutstandingPoints int     `json:"total_outstanding_points"`
	TotalLiabilityValue    float64 `json:"total_liability_value"`
	CustomerCount          int     `json:"customer_count"`
}

// GetPointsLiabilityReport sums every customer's positive (Earn-Burn)
// balance - a customer with zero or negative balance (fully redeemed)
// contributes no liability.
func GetPointsLiabilityReport(tenantID string) (*PointsLiabilitySummary, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	var totalPoints, customerCount int
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT COALESCE(SUM(bal), 0), COUNT(*) FROM (
		  SELECT customer_id, SUM(CASE WHEN transaction_type = 'Earn' THEN points ELSE -points END) AS bal
		  FROM %s.loyalty_point_ledger GROUP BY customer_id
		) t WHERE bal > 0`, schema)).Scan(&totalPoints, &customerCount)
	if err != nil {
		return nil, err
	}
	return &PointsLiabilitySummary{
		TotalOutstandingPoints: totalPoints,
		TotalLiabilityValue:    float64(totalPoints) * float64(redemptionValuePerPointFor(tenantID)),
		CustomerCount:          customerCount,
	}, nil
}

// Customer360Profile is a read-model over existing customer-linked
// documents - not a new customer master, per the backlog's own framing.
type Customer360Profile struct {
	CustomerID       string     `json:"customer_id"`
	LoyaltyBalance   int        `json:"loyalty_balance"`
	POSPurchaseCount int        `json:"pos_purchase_count"`
	POSTotalSpend    float64    `json:"pos_total_spend"`
	LastPurchaseAt   *time.Time `json:"last_purchase_at,omitempty"`
	InvoiceCount     int        `json:"invoice_count"`
	InvoiceTotal     float64    `json:"invoice_total"`
}

// GetCustomer360 joins POSCart purchase history, SalesInvoice history (best
// effort - matched by the "customer" free-text field), and the loyalty
// ledger balance for one customer_id into a single profile.
func GetCustomer360(tenantID, customerID string) (*Customer360Profile, error) {
	if customerID == "" {
		return nil, fmt.Errorf("customer_id is required")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}

	profile := &Customer360Profile{CustomerID: customerID}

	balance, err := GetLoyaltyBalance(tenantID, customerID)
	if err != nil {
		return nil, err
	}
	profile.LoyaltyBalance = balance

	var lastPurchase sql.NullTime
	if err := db.DB.QueryRow(fmt.Sprintf(`
		SELECT COUNT(*), COALESCE(SUM((data->>'amount_paid')::numeric), 0), MAX(created_at)
		FROM %s.documents WHERE doctype = 'POSCart' AND status = 'Paid' AND data->>'customer_id' = $1`, schema), customerID).
		Scan(&profile.POSPurchaseCount, &profile.POSTotalSpend, &lastPurchase); err != nil {
		return nil, err
	}
	if lastPurchase.Valid {
		t := lastPurchase.Time
		profile.LastPurchaseAt = &t
	}

	if err := db.DB.QueryRow(fmt.Sprintf(`
		SELECT COUNT(*), COALESCE(SUM((data->>'total_amount')::numeric), 0)
		FROM %s.documents WHERE doctype = 'SalesInvoice' AND data->>'customer' = $1`, schema), customerID).
		Scan(&profile.InvoiceCount, &profile.InvoiceTotal); err != nil {
		return nil, err
	}

	return profile, nil
}

func init() {
	RegisterReport(ReportDefinition{
		ID: "rfm-segmentation", Label: "RFM Customer Segmentation", Category: "CRM",
		Columns: []ReportColumn{
			{Key: "customer_id", Label: "Customer"}, {Key: "recency_days", Label: "Recency (days)"},
			{Key: "frequency", Label: "Frequency"}, {Key: "monetary", Label: "Monetary", Sensitive: true},
			{Key: "r_score", Label: "R"}, {Key: "f_score", Label: "F"}, {Key: "m_score", Label: "M"},
			{Key: "segment", Label: "Segment"},
		},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			segs, err := GetRFMSegmentation(tenantID)
			if err != nil {
				return nil, err
			}
			return structsToRows(segs)
		},
	})

	RegisterReport(ReportDefinition{
		ID: "points-liability", Label: "Loyalty Points Liability", Category: "CRM",
		Columns: []ReportColumn{
			{Key: "total_outstanding_points", Label: "Outstanding Points"},
			{Key: "total_liability_value", Label: "Liability Value", Sensitive: true},
			{Key: "customer_count", Label: "Customers"},
		},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			summary, err := GetPointsLiabilityReport(tenantID)
			if err != nil {
				return nil, err
			}
			return structsToRows(summary)
		},
	})

	RegisterReport(ReportDefinition{
		ID: "customer-360", Label: "Customer 360", Category: "CRM",
		Columns: []ReportColumn{
			{Key: "customer_id", Label: "Customer"}, {Key: "loyalty_balance", Label: "Loyalty Balance", Sensitive: true},
			{Key: "pos_purchase_count", Label: "POS Purchases"}, {Key: "pos_total_spend", Label: "POS Spend", Sensitive: true},
			{Key: "last_purchase_at", Label: "Last Purchase"}, {Key: "invoice_count", Label: "Invoices"},
			{Key: "invoice_total", Label: "Invoice Total", Sensitive: true},
		},
		Params: []ReportParam{{Key: "customer_id", Label: "Customer ID", Type: "text", Required: true}},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			profile, err := GetCustomer360(tenantID, params["customer_id"])
			if err != nil {
				return nil, err
			}
			return structsToRows(profile)
		},
	})
}
