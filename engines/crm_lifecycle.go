package engines

import (
	"custom_erp/db"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Stage 101.2.2 - RFM and lifecycle. Both are computed from the bills when
// asked, never stored, so a return or a merge is reflected the next time
// anyone looks ("recomputed after returns and merges" by construction).
//
// Lifecycle states never overlap; the first that fits wins:
//
//	Never bought  no paid bill
//	Lapsed        last bill older than at-risk days
//	At risk       last bill older than active days
//	Reactivated   came back after a gap longer than at-risk days
//	New           one bill, or every bill within the first new days
//	Active repeat everyone else
//
// Thresholds are versioned: each change is a new crm_lifecycle_versions row,
// so a report can say which version it used.

// CRMLifecycleStates in display order.
var CRMLifecycleStates = []string{"New", "Active repeat", "Reactivated", "At risk", "Lapsed", "Never bought"}

// CRMLifecycleThresholds is one version of the thresholds.
type CRMLifecycleThresholds struct {
	Version    int       `json:"version"`
	NewDays    int       `json:"new_days"`
	ActiveDays int       `json:"active_days"`
	AtRiskDays int       `json:"at_risk_days"`
	CreatedBy  string    `json:"created_by,omitempty"`
	CreatedAt  time.Time `json:"created_at,omitempty"`
}

var crmDefaultLifecycle = CRMLifecycleThresholds{Version: 0, NewDays: 30, ActiveDays: 90, AtRiskDays: 180}

func crmLifecycleThresholds(schema string) CRMLifecycleThresholds {
	var t CRMLifecycleThresholds
	err := db.DB.QueryRow(fmt.Sprintf(`SELECT version, new_days, active_days, at_risk_days, COALESCE(created_by, ''), created_at
		FROM %s.crm_lifecycle_versions ORDER BY version DESC LIMIT 1`, schema)).
		Scan(&t.Version, &t.NewDays, &t.ActiveDays, &t.AtRiskDays, &t.CreatedBy, &t.CreatedAt)
	if err != nil {
		return crmDefaultLifecycle
	}
	return t
}

// GetCRMLifecycleThresholds returns the thresholds in use.
func GetCRMLifecycleThresholds(tenantID string) (CRMLifecycleThresholds, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return crmDefaultLifecycle, err
	}
	return crmLifecycleThresholds(schema), nil
}

// SaveCRMLifecycleThresholds adds a new version; earlier ones stay.
func SaveCRMLifecycleThresholds(tenantID string, t CRMLifecycleThresholds, actor string) (CRMLifecycleThresholds, error) {
	if t.NewDays < 1 || t.ActiveDays < 1 || t.AtRiskDays < 1 || t.NewDays > t.ActiveDays || t.ActiveDays >= t.AtRiskDays {
		return t, crmErr(CRMCodeSegment, "the days must grow: new <= active < at risk (for example 30, 90, 180)")
	}
	schema, err := crmSchema(tenantID)
	if err != nil {
		return t, err
	}
	err = db.DB.QueryRow(fmt.Sprintf(`INSERT INTO %s.crm_lifecycle_versions (new_days, active_days, at_risk_days, created_by) VALUES ($1, $2, $3, $4)
		RETURNING version, created_at`, schema), t.NewDays, t.ActiveDays, t.AtRiskDays, actor).Scan(&t.Version, &t.CreatedAt)
	t.CreatedBy = actor
	return t, err
}

// crmNumSQL casts a text expression to numeric only when it is a number, so
// one malformed record cannot fail a whole segment count.
func crmNumSQL(expr string) string {
	return fmt.Sprintf(`(CASE WHEN (%[1]s) ~ '^\s*-?[0-9]+(\.[0-9]+)?\s*$' THEN (%[1]s)::numeric END)`, expr)
}

// crmItemsSQL is a bill's lines as a JSON array (old test carts kept them as
// a string; those count as no lines).
func crmItemsSQL(alias string) string {
	return fmt.Sprintf(`(CASE WHEN jsonb_typeof(%[1]s.data->'items') = 'array' THEN %[1]s.data->'items' ELSE '[]'::jsonb END)`, alias)
}

// crmBillAmountSQL is a paid bill's value: amount_paid (stored since
// 73.3.1), else its lines less the offer discount - posCartSpendSQL's rule,
// written defensively.
func crmBillAmountSQL(alias string) string {
	return fmt.Sprintf(`COALESCE(%s, (SELECT COALESCE(SUM(COALESCE(%s, 0) * COALESCE(%s, 0)), 0) FROM jsonb_array_elements(%s) li) - COALESCE(%s, 0))`,
		crmNumSQL(alias+".data->>'amount_paid'"), crmNumSQL("li->>'qty'"), crmNumSQL("li->>'sale_price'"), crmItemsSQL(alias), crmNumSQL(alias+".data->>'offer_discount'"))
}

// crmFactsCTE is one row per live customer with what segments, lifecycle and
// the customer page read. customerFilter is "" for everyone, or a SQL
// predicate on the customer id parameter for one customer (passed as $1).
func crmFactsCTE(schema string, th CRMLifecycleThresholds, oneCustomer bool) string {
	billFilter, custFilter := "", ""
	if oneCustomer {
		billFilter = " AND b.data->>'customer_id' = $1"
		custFilter = " AND cu.id = $1"
	}
	return fmt.Sprintf(`WITH bills AS (
	  SELECT b.data->>'customer_id' AS cid, b.id, b.created_at, %[2]s AS amount, COALESCE(b.data->>'location', '') AS store
	    FROM %[1]s.documents b
	   WHERE b.doctype = 'POSCart' AND b.status = 'Paid' AND b.deleted_at IS NULL AND COALESCE(b.data->>'customer_id', '') <> ''%[3]s
	), rets AS (
	  SELECT c.data->>'customer_id' AS cid, COUNT(*) AS returns, SUM(COALESCE(%[5]s, 0)) AS refunded
	    FROM %[1]s.documents r JOIN %[1]s.documents c ON c.id = r.data->>'original_order_id' AND c.doctype = 'POSCart'
	   WHERE r.doctype = 'ReturnRequest' AND r.status IN ('Approved', 'Received', 'QC Complete', 'Closed') AND r.deleted_at IS NULL
	   GROUP BY 1
	), agg AS (
	  SELECT cid, COUNT(*) AS visits, SUM(amount) AS gross, MIN(created_at) AS first_at, MAX(created_at) AS last_at,
	         (array_agg(created_at ORDER BY created_at DESC))[2] AS prev_at,
	         (array_agg(store ORDER BY created_at DESC))[1] AS last_store
	    FROM bills GROUP BY cid
	), pts AS (
	  SELECT customer_id AS cid,
	         SUM(CASE WHEN transaction_type = 'Earn' THEN points ELSE -points END) FILTER (WHERE pending_until IS NULL OR pending_until <= NOW()) AS points,
	         SUM(CASE WHEN transaction_type = 'Earn' THEN points ELSE -points END) FILTER (WHERE pending_until > NOW()) AS pending
	    FROM %[1]s.loyalty_point_ledger WHERE cancelled_at IS NULL GROUP BY 1
	), facts AS (
	  SELECT cu.id, cu.created_at, cu.data AS data,
	         COALESCE(cu.data->>'name', '') AS name, COALESCE(cu.data->>'phone', '') AS phone, COALESCE(cu.data->>'email', '') AS email,
	         COALESCE(cu.data->>'gender', '') AS gender, COALESCE(cu.data->>'city', '') AS city, COALESCE(cu.data->>'state', '') AS state,
	         COALESCE(cu.data->>'loyalty_tier', '') AS tier, COALESCE(cu.data->>'tags', '') AS tags,
	         COALESCE(cu.data->>'preferred_store', '') AS preferred_store, COALESCE(cu.data->>'preferred_salesman', '') AS preferred_salesman,
	         CASE WHEN cu.data->>'date_of_birth' ~ '^\d{4}-\d{2}-\d{2}' THEN (cu.data->>'date_of_birth')::date END AS dob,
	         CASE WHEN cu.data->>'date_of_anniversary' ~ '^\d{4}-\d{2}-\d{2}' THEN (cu.data->>'date_of_anniversary')::date END AS anniversary,
	         COALESCE(a.visits, 0) AS visits, COALESCE(a.gross, 0) - COALESCE(r.refunded, 0) AS spend, COALESCE(r.returns, 0) AS returns,
	         CASE WHEN COALESCE(a.visits, 0) > 0 THEN (COALESCE(a.gross, 0) - COALESCE(r.refunded, 0)) / a.visits END AS avg_bill,
	         a.first_at, a.last_at, a.prev_at, COALESCE(a.last_store, '') AS last_store,
	         COALESCE(p.points, 0) AS points, COALESCE(p.pending, 0) AS points_pending,
	         CASE WHEN a.last_at IS NOT NULL THEN (LOCALTIMESTAMP::date - a.last_at::date) END AS days_since
	    FROM %[1]s.documents cu
	    LEFT JOIN agg a ON a.cid = cu.id LEFT JOIN rets r ON r.cid = cu.id LEFT JOIN pts p ON p.cid = cu.id
	   WHERE cu.doctype = 'Customer' AND cu.deleted_at IS NULL AND cu.status <> 'Merged'
	     AND COALESCE(cu.data->>'walk_in', '') <> 'Yes' AND COALESCE(cu.data->>'anonymized_at', '') = ''%[4]s
	), scored AS (
	  SELECT f.*, %[6]s AS lifecycle,
	         CASE WHEN f.visits > 0 THEN
	           (((ROW_NUMBER() OVER (PARTITION BY (f.visits > 0) ORDER BY f.last_at ASC) - 1) * 5 / COUNT(*) OVER (PARTITION BY (f.visits > 0))) + 1)
	         + (((ROW_NUMBER() OVER (PARTITION BY (f.visits > 0) ORDER BY f.visits ASC) - 1) * 5 / COUNT(*) OVER (PARTITION BY (f.visits > 0))) + 1)
	         + (((ROW_NUMBER() OVER (PARTITION BY (f.visits > 0) ORDER BY f.spend ASC) - 1) * 5 / COUNT(*) OVER (PARTITION BY (f.visits > 0))) + 1)
	         END AS rfm_total
	    FROM facts f
	), cf AS (
	  SELECT s.*, CASE WHEN s.rfm_total IS NULL THEN '' WHEN s.rfm_total >= 13 THEN 'Champions' WHEN s.rfm_total >= 10 THEN 'Loyal Customers'
	                   WHEN s.rfm_total >= 7 THEN 'Potential Loyalists' WHEN s.rfm_total >= 4 THEN 'At Risk' ELSE 'Lost' END AS rfm
	    FROM scored s
	)`, schema, crmBillAmountSQL("b"), billFilter, custFilter, crmNumSQL("r.data->>'total_refund_eligible'"), crmLifecycleCaseSQL("f", th))
}

// crmLifecycleCaseSQL is the lifecycle rule as SQL over a facts row.
func crmLifecycleCaseSQL(f string, th CRMLifecycleThresholds) string {
	return fmt.Sprintf(`(CASE WHEN %[1]s.visits = 0 THEN 'Never bought'
	  WHEN %[1]s.days_since > %[4]d THEN 'Lapsed'
	  WHEN %[1]s.days_since > %[3]d THEN 'At risk'
	  WHEN %[1]s.prev_at IS NOT NULL AND (%[1]s.last_at::date - %[1]s.prev_at::date) > %[4]d THEN 'Reactivated'
	  WHEN %[1]s.visits = 1 OR (LOCALTIMESTAMP::date - %[1]s.first_at::date) <= %[2]d THEN 'New'
	  ELSE 'Active repeat' END)`, f, th.NewDays, th.ActiveDays, th.AtRiskDays)
}

// CRMLifecycleCount is how many customers are in one state.
type CRMLifecycleCount struct {
	State string `json:"state"`
	Count int    `json:"count"`
}

// GetCRMLifecycleCounts counts customers per lifecycle state.
func GetCRMLifecycleCounts(tenantID string) ([]CRMLifecycleCount, CRMLifecycleThresholds, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, crmDefaultLifecycle, err
	}
	th := crmLifecycleThresholds(schema)
	rows, err := db.DB.Query(crmFactsCTE(schema, th, false) + ` SELECT lifecycle, COUNT(*) FROM cf GROUP BY lifecycle`)
	if err != nil {
		return nil, th, err
	}
	defer rows.Close()
	counts := map[string]int{}
	for rows.Next() {
		var s string
		var n int
		if err := rows.Scan(&s, &n); err != nil {
			return nil, th, err
		}
		counts[s] = n
	}
	out := make([]CRMLifecycleCount, 0, len(CRMLifecycleStates))
	for _, s := range CRMLifecycleStates {
		out = append(out, CRMLifecycleCount{State: s, Count: counts[s]})
	}
	return out, th, rows.Err()
}

// crmCustomerFactsRow is one customer's facts.
type crmCustomerFactsRow struct {
	Visits        int
	Spend         float64
	Returns       int
	AvgBill       sql.NullFloat64
	FirstAt       sql.NullTime
	LastAt        sql.NullTime
	LastStore     string
	Points        int
	PointsPending int
	DaysSince     sql.NullInt64
	Lifecycle     string
	RFM           string
}

func crmCustomerFacts(schema, customerID string) (crmCustomerFactsRow, error) {
	var f crmCustomerFactsRow
	th := crmLifecycleThresholds(schema)
	// RFM is relative to every customer, so it needs the whole base; the
	// rest is the one customer's. Both from the same CTE text.
	err := db.DB.QueryRow(crmFactsCTE(schema, th, false)+` SELECT visits, spend, returns, avg_bill, first_at, last_at, last_store, points, points_pending, days_since, lifecycle, rfm
		FROM cf WHERE id = $1`, customerID).
		Scan(&f.Visits, &f.Spend, &f.Returns, &f.AvgBill, &f.FirstAt, &f.LastAt, &f.LastStore, &f.Points, &f.PointsPending, &f.DaysSince, &f.Lifecycle, &f.RFM)
	if errors.Is(err, sql.ErrNoRows) {
		return f, ErrCRMNotFound
	}
	return f, err
}

func crmTitle(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
