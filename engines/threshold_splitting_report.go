package engines

import (
	"custom_erp/db"
	"fmt"
	"math"
	"strings"
	"time"
)

// Stage 49.10.6b (1) (docs/security/insider-scenarios.md, "price/refund/
// threshold fraud"): the threshold-splitting report. A user who keeps a
// purchase, a discount or a refund under an approval limit by cutting it into
// several records shows up as several records just under that limit, by one
// person, on one day.
//
// The limits are the client's own approval slabs (Settings -> Approval Rules):
// every rule with a "from" amount above zero is a limit for its record type -
// a purchase order at 50,000, a till discount (POSCart) at 10%, a loyalty
// redemption at 500, and any refund, return or other slab the client adds.
// The amount compared is the one the approval engine routes on
// (DocumentBaseAmount); a till bill's is its discount percentage, the larger
// of the declared and the measured one, as the checkout gate reads it.

// thresholdSplittingDefaults: within 10% under the limit, at least two a day.
const (
	thresholdSplittingBandPct  = 10.0
	thresholdSplittingMinCount = 2
)

func init() {
	RegisterReport(ReportDefinition{
		ID: "threshold-splitting", Label: "Threshold splitting", Category: "Finance", StoreKey: "location",
		Columns: []ReportColumn{
			{Key: "day", Label: "Day"},
			{Key: "user", Label: "User"},
			{Key: "doctype", Label: "Record type"},
			{Key: "location", Label: "Store"},
			{Key: "limit", Label: "Limit"},
			{Key: "count", Label: "Records"},
			{Key: "total", Label: "Together"},
			{Key: "over_limit", Label: "Together over limit"},
			{Key: "documents", Label: "Record numbers"},
		},
		Params: []ReportParam{
			{Key: "from", Label: "From (default 30 days ago)", Type: "date"},
			{Key: "to", Label: "To (default today)", Type: "date"},
			{Key: "band_pct", Label: "Within % under the limit (default 10)", Type: "text"},
			{Key: "min_count", Label: "At least this many a day (default 2)", Type: "text"},
		},
		Run: thresholdSplittingRows,
	})
}

// jsonNumberSQL reads a document field only when it is a JSON number, as
// extractAmount does.
func jsonNumberSQL(field string) string {
	return fmt.Sprintf(`CASE WHEN jsonb_typeof(d.data->'%[1]s') = 'number' THEN (d.data->>'%[1]s')::numeric END`, field)
}

// thresholdSplittingAmountSQL is the amount the approval engine routes a
// record on: the base-currency twin first, then extractAmount's fields, in
// their order (DocumentBaseAmount).
func thresholdSplittingAmountSQL() string {
	var generic []string
	for _, f := range documentAmountFields {
		generic = append(generic, jsonNumberSQL("base_"+f))
	}
	for _, f := range []string{"total_amount", "amount", "discount_amount", "variance_qty", "invoice_amount", "points_value"} {
		generic = append(generic, jsonNumberSQL(f))
	}
	return fmt.Sprintf(`CASE d.doctype
		WHEN 'POSCart' THEN GREATEST(%s, %s, %s)
		WHEN 'POSPriceOverride' THEN %s
		ELSE COALESCE(%s) END`,
		jsonNumberSQL("discount_pct"), jsonNumberSQL("manual_discount_pct"), jsonNumberSQL("discount_amount"),
		jsonNumberSQL("discount_pct"), strings.Join(generic, ", "))
}

func thresholdSplittingRows(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	from, to := strings.TrimSpace(params["from"]), strings.TrimSpace(params["to"])
	for key, v := range map[string]string{"from": from, "to": to} {
		if _, perr := time.Parse("2006-01-02", v); v != "" && perr != nil {
			return nil, &ValidationError{Code: "GLOBAL-0002", SubFor: key, Message: fmt.Sprintf("%s must be a date (YYYY-MM-DD)", key)}
		}
	}
	band := thresholdSplittingBandPct
	if v := numFromInterface(params["band_pct"]); v > 0 && v < 100 {
		band = v
	}
	minCount := thresholdSplittingMinCount
	if v := int(numFromInterface(params["min_count"])); v >= 2 {
		minCount = v
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		WITH limits AS (
			SELECT DISTINCT doctype, min_amount::numeric AS lim FROM %[1]s.approval_rules WHERE min_amount > 0
		), docs AS (
			SELECT d.id, d.doctype, l.lim, d.created_at,
			       public.erp_local_date(d.created_at) AS day,
			       COALESCE(NULLIF(NULLIF(d.created_by, ''), 'system'), NULLIF(d.data->>'requested_by', ''), NULLIF(d.data->>'cashier', ''), COALESCE(d.created_by, '')) AS person,
			       COALESCE(NULLIF(d.data->>'location', ''), d.data->>'location_code', '') AS location,
			       %[2]s AS amount
			  FROM %[1]s.documents d JOIN limits l ON l.doctype = d.doctype
			 WHERE d.deleted_at IS NULL AND COALESCE(d.status, '') <> 'Cancelled'
			   AND d.created_at >= public.erp_day_start(COALESCE(NULLIF($1, '')::date, public.erp_today() - 29))
			   AND d.created_at < public.erp_day_start(COALESCE(NULLIF($2, '')::date, public.erp_today()) + 1)
		)
		SELECT x.day::text, COALESCE(u.username, x.person), x.doctype, x.location, x.lim::float8, COUNT(*), SUM(x.amount)::float8,
		       string_agg(x.id, ', ' ORDER BY x.created_at)
		  FROM docs x LEFT JOIN %[1]s.users u ON u.id = x.person
		 WHERE x.amount >= x.lim * (1 - $3::numeric / 100) AND x.amount < x.lim
		 GROUP BY x.day, x.person, u.username, x.doctype, x.location, x.lim
		HAVING COUNT(*) >= $4
		 ORDER BY x.day DESC, COUNT(*) DESC, 2`, schema, thresholdSplittingAmountSQL()), from, to, band, minCount)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]interface{}{}
	for rows.Next() {
		var day, user, doctype, location, docs string
		var limit, total float64
		var count int
		if err := rows.Scan(&day, &user, &doctype, &location, &limit, &count, &total, &docs); err != nil {
			return nil, err
		}
		over := "No"
		if total >= limit {
			over = "Yes"
		}
		out = append(out, map[string]interface{}{
			"day": day, "user": user, "doctype": doctype, "location": location, "limit": limit,
			"count": count, "total": math.Round(total*100) / 100, "over_limit": over, "documents": docs,
		})
	}
	return out, rows.Err()
}
