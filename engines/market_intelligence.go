package engines

import (
	"context"
	"custom_erp/db"
	"database/sql"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"
)

// Stage 34.2/34.3 - what the CompetitorPrice rows from 34.1 are actually for.
//
// 34.2 is a catalog report (RegisterReport), not a bespoke screen, so it
// inherits filters, column profiles, CSV export and the async export worker
// for free. 34.3 is a Start...Worker(ctx, interval), the shape the other ten
// background workers already use.
//
// ---------------------------------------------------------------------------
// The thing the checklist assumed and this repo does not have: a price master
// ---------------------------------------------------------------------------
// 34.2 was written as "joins CompetitorPrice to our Item/price list". There is
// no price list, and `Item` has no price field of any kind (verified against
// doctype_fields: code/name/barcode/weight/volume/category/hsn_code/gst_rate/
// family/parent_product_code/variant_option_values). The POS cashier types the
// sale price by hand per line (app.js's posCart pushes salePrice: 0 and renders
// an editable input), and SalesOrderLine.unit_price is filled per order.
//
// So "our price" here is deliberately the **last price we actually transacted
// at**, resolved per SKU at report time from the two places a real price is
// recorded, newest wins:
//
//	SalesOrderLine.unit_price   - flat field, the reliable source
//	POSCart items[].sale_price  - retail, and therefore the one that matters
//	                              most in a shop, but see the storage note below
//
// That is honest about what the data can support, needs no schema change, and
// invents no pricing architecture (price lists per channel/customer-group with
// effective dates is a real feature and a real decision, not something to slip
// in under a competitor-pricing item). Its cost: a SKU that has never been sold
// has no "our price" at all, and those rows report Position "No price on file"
// rather than a fabricated zero. If a genuine price master is ever added, this
// is the one function to repoint.
//
// POSCart.items storage note: some writers store the line array as a real JSON
// array, others as a JSON-encoded *string* (both shapes are present in the dev
// database). Both are normalised below. The `LIKE '[%'` guard is what keeps a
// malformed value from aborting the whole report on the ::jsonb cast; PG16's
// pg_input_is_valid would be tidier but would pin this query to PG16+, and
// nothing else in this repo does.

// competitorOurPriceCTE resolves one row per SKU: the most recent transacted
// unit price and where it came from. Written once and shared by the 34.2
// report and the 34.3 worker so the two can never disagree about what our
// price is - the same "one choke point" rule the rest of the repo follows.
// %[1]s is the tenant schema.
const competitorOurPriceCTE = `
our_prices AS (
    SELECT DISTINCT ON (sku) sku, price, src, observed
      FROM (
        SELECT data->>'sku' AS sku,
               (NULLIF(data->>'unit_price',''))::numeric AS price,
               'Sales Order' AS src,
               created_at AS observed
          FROM %[1]s.documents
         WHERE doctype = 'SalesOrderLine' AND deleted_at IS NULL
           AND COALESCE(status,'') <> 'Cancelled'
           AND COALESCE(data->>'sku','') <> ''
           AND COALESCE(data->>'unit_price','') ~ '^[0-9]+(\.[0-9]+)?$'
           AND (data->>'unit_price')::numeric > 0
        UNION ALL
        SELECT line->>'sku' AS sku,
               (NULLIF(line->>'sale_price',''))::numeric AS price,
               'POS' AS src,
               d.created_at AS observed
          FROM %[1]s.documents d
          CROSS JOIN LATERAL jsonb_array_elements(
                CASE
                  WHEN jsonb_typeof(d.data->'items') = 'array' THEN d.data->'items'
                  WHEN jsonb_typeof(d.data->'items') = 'string'
                       AND d.data->>'items' LIKE '[%%' THEN (d.data->>'items')::jsonb
                  ELSE '[]'::jsonb
                END) AS line
         WHERE d.doctype = 'POSCart' AND d.deleted_at IS NULL
           AND COALESCE(d.status,'') <> 'Cancelled'
           AND COALESCE(line->>'sku','') <> ''
           AND COALESCE(line->>'sale_price','') ~ '^[0-9]+(\.[0-9]+)?$'
           AND (line->>'sale_price')::numeric > 0
      ) AS priced
     ORDER BY sku, observed DESC
)`

// competitorBestCTE picks, per linked SKU, the single cheapest current
// competitor observation - "best" meaning best for the shopper and therefore
// worst for us, which is the one worth acting on. Filters are applied inside
// the CTE so "cheapest on Amazon" is answerable, not just "cheapest anywhere".
// %[1]s is the tenant schema.
const competitorBestCTE = `
best_competitor AS (
    SELECT DISTINCT ON (data->>'our_item')
           data->>'our_item'            AS sku,
           (data->>'competitor_price')::numeric AS price,
           COALESCE(data->>'platform','')       AS platform,
           COALESCE(data->>'observed_at','')    AS observed_at,
           COALESCE(data->>'source_url','')     AS source_url
      FROM %[1]s.documents
     WHERE doctype = 'CompetitorPrice' AND deleted_at IS NULL
       AND COALESCE(status,'') = 'Active'
       AND COALESCE(data->>'our_item','') <> ''
       AND COALESCE(data->>'competitor_price','') ~ '^[0-9]+(\.[0-9]+)?$'
       AND (data->>'competitor_price')::numeric > 0
       AND ($1 = '' OR data->>'platform' = $1)
       AND ($2 = '' OR COALESCE(data->>'observed_at','') >= $2)
       AND ($3 = '' OR data->>'our_item' = $3)
     ORDER BY data->>'our_item', (data->>'competitor_price')::numeric ASC
)`

// GetCompetitorPriceGapReport is 34.2. platform/since/sku are optional
// filters ("" means no filter); since is an ISO date compared against
// observed_at, which is stored as a plain YYYY-MM-DD string so a lexical
// comparison is also a chronological one.
func GetCompetitorPriceGapReport(tenantID, platform, since, sku, position string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}

	query := fmt.Sprintf(`
WITH `+competitorBestCTE+`,`+competitorOurPriceCTE+`,
items AS (
    SELECT id, COALESCE(data->>'name','') AS name FROM %[1]s.documents
     WHERE doctype = 'Item' AND deleted_at IS NULL
),
counts AS (
    SELECT data->>'our_item' AS sku, COUNT(*) AS n
      FROM %[1]s.documents
     WHERE doctype = 'CompetitorPrice' AND deleted_at IS NULL
       AND COALESCE(status,'') = 'Active' AND COALESCE(data->>'our_item','') <> ''
     GROUP BY 1
)
SELECT b.sku,
       COALESCE(i.name,''),
       o.price::float8,
       COALESCE(o.src,''),
       b.price::float8,
       b.platform,
       b.observed_at,
       b.source_url,
       COALESCE(c.n, 0)
  FROM best_competitor b
  LEFT JOIN our_prices o ON o.sku = b.sku
  LEFT JOIN items i      ON i.id  = b.sku
  LEFT JOIN counts c     ON c.sku = b.sku
 ORDER BY b.sku`, schema)

	rows, err := db.DB.Query(query, platform, since, sku)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []map[string]interface{}{}
	for rows.Next() {
		var (
			skuVal, itemName, ourSrc      string
			bestPlatform, observedAt, url string
			ourPrice                      sql.NullFloat64
			bestPrice                     float64
			observations                  int
		)
		if err := rows.Scan(&skuVal, &itemName, &ourPrice, &ourSrc, &bestPrice,
			&bestPlatform, &observedAt, &url, &observations); err != nil {
			return nil, err
		}

		row := map[string]interface{}{
			"sku":                   skuVal,
			"item_name":             itemName,
			"our_price_source":      ourSrc,
			"best_competitor_price": bestPrice,
			"best_platform":         bestPlatform,
			"observed_at":           observedAt,
			"source_url":            url,
			"observations":          observations,
			"our_price":             nil,
			"gap_amount":            nil,
			"gap_pct":               nil,
		}

		// No transacted price for this SKU: report the competitor side and say
		// plainly why the comparison is missing, rather than emitting a zero
		// that would read as "we sell it for nothing" and rank top of the gap.
		if !ourPrice.Valid {
			row["position"] = "No price on file"
			if !matchesGapPosition(position, "No price on file") {
				continue
			}
			out = append(out, row)
			continue
		}

		gap := ourPrice.Float64 - bestPrice
		row["our_price"] = ourPrice.Float64
		row["gap_amount"] = round2(gap)
		if bestPrice > 0 {
			row["gap_pct"] = round2(gap / bestPrice * 100)
		}

		pos := "At"
		switch {
		case gap > 0:
			pos = "Above"
		case gap < 0:
			pos = "Below"
		}
		row["position"] = pos
		if !matchesGapPosition(position, pos) {
			continue
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// matchesGapPosition applies the optional Position filter. Done in Go rather
// than SQL because position is derived from the gap, which is itself derived
// from a LEFT JOIN that may be null - expressing it in the WHERE clause would
// mean repeating the whole price-resolution expression.
func matchesGapPosition(want, got string) bool {
	return want == "" || strings.EqualFold(want, got)
}

func init() {
	RegisterReport(ReportDefinition{
		ID: "competitor-price-gap", Label: "Competitor Price Gap", Category: "Sales",
		Columns: []ReportColumn{
			{Key: "sku", Label: "Our SKU"},
			{Key: "item_name", Label: "Item"},
			{Key: "our_price", Label: "Our Price"},
			{Key: "our_price_source", Label: "Our Price From"},
			{Key: "best_competitor_price", Label: "Best Competitor Price"},
			{Key: "best_platform", Label: "Platform"},
			{Key: "gap_amount", Label: "Gap (₹)"},
			{Key: "gap_pct", Label: "Gap (%)"},
			{Key: "position", Label: "We Are"},
			{Key: "observed_at", Label: "Observed On"},
			{Key: "observations", Label: "Observations"},
			{Key: "source_url", Label: "Source"},
		},
		Params: []ReportParam{
			{Key: "platform", Label: "Platform (optional)", Type: "select",
				Options: "Amazon,Flipkart,Myntra,Meesho,Ajio,Nykaa,Tata Cliq,JioMart,eBay,Own Website,Other"},
			{Key: "sku", Label: "Our SKU (optional)", Type: "text"},
			{Key: "since", Label: "Observed on or after (optional)", Type: "date"},
			{Key: "position", Label: "We are (optional)", Type: "select",
				Options: "Above,At,Below,No price on file"},
		},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			return GetCompetitorPriceGapReport(tenantID, params["platform"], params["since"], params["sku"], params["position"])
		},
	})
}

// ---------------------------------------------------------------------------
// 34.3 - undercut alert worker
// ---------------------------------------------------------------------------

// StartCompetitorUndercutWorker scans every tenant for SKUs where the cheapest
// current competitor observation sits more than
// `market.undercut_threshold_pct` below our own last transacted price, and
// fires the EXISTING notification path (DispatchNotification, the same one
// order/return lifecycle events use) for each. No new scheduling
// infrastructure and no new dispatch mechanism.
//
// Like StartPatchIntakeWorker it only ever reads business state and writes
// audit rows - it never changes a price, a document or a configuration. A
// price decision stays a human one.
//
// Alerting is de-duplicated through public.competitor_undercut_state's
// last_run_at (same single-row shape as public.patch_intake_state): a cycle
// only considers CompetitorPrice rows created since the previous successful
// cycle, so one observation alerts at most once no matter how often the
// worker ticks or how long the undercut persists.
func StartCompetitorUndercutWorker(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if db.DB == nil {
					continue
				}
				if err := runCompetitorUndercutCycle(); err != nil {
					log.Printf("[UNDERCUT] cycle failed: %v", err)
				}
			}
		}
	}()
}

func runCompetitorUndercutCycle() error {
	lastRun, err := getUndercutLastRun()
	if err != nil {
		return fmt.Errorf("failed to read last run time: %w", err)
	}

	schemas, err := listTenantSchemas()
	if err != nil {
		return fmt.Errorf("failed to list tenant schemas: %w", err)
	}

	cycleStart := time.Now()
	for _, schema := range schemas {
		tenantID, tErr := tenantIDForSchema(schema)
		if tErr != nil {
			continue
		}
		if err := alertUndercutsForTenant(tenantID, schema, lastRun); err != nil {
			log.Printf("[UNDERCUT] failed scanning schema %s: %v", schema, err)
		}
	}
	return setUndercutLastRun(cycleStart)
}

// alertUndercutsForTenant is exported-in-spirit for the test: it does one
// tenant's worth of work and returns how many alerts it raised.
func alertUndercutsForTenant(tenantID, schema string, since time.Time) error {
	threshold := GetSettingFloat(tenantID, "market.undercut_threshold_pct")
	if threshold <= 0 {
		return nil // disabled for this tenant
	}

	// Only observations recorded since the last cycle can produce a new alert.
	query := fmt.Sprintf(`
WITH new_obs AS (
    SELECT DISTINCT data->>'our_item' AS sku
      FROM %[1]s.documents
     WHERE doctype = 'CompetitorPrice' AND deleted_at IS NULL
       AND COALESCE(status,'') = 'Active'
       AND COALESCE(data->>'our_item','') <> ''
       AND created_at > $1
),`+competitorOurPriceCTE+`,
cheapest AS (
    SELECT DISTINCT ON (data->>'our_item')
           data->>'our_item' AS sku,
           (data->>'competitor_price')::numeric AS price,
           COALESCE(data->>'platform','') AS platform,
           COALESCE(data->>'source_url','') AS source_url
      FROM %[1]s.documents
     WHERE doctype = 'CompetitorPrice' AND deleted_at IS NULL
       AND COALESCE(status,'') = 'Active'
       AND COALESCE(data->>'our_item','') <> ''
       AND COALESCE(data->>'competitor_price','') ~ '^[0-9]+(\.[0-9]+)?$'
       AND (data->>'competitor_price')::numeric > 0
     ORDER BY data->>'our_item', (data->>'competitor_price')::numeric ASC
)
SELECT c.sku, o.price::float8, c.price::float8, c.platform, c.source_url
  FROM cheapest c
  JOIN new_obs n  ON n.sku = c.sku
  JOIN our_prices o ON o.sku = c.sku
 WHERE o.price > 0
   AND (o.price - c.price) / o.price * 100 >= $2`, schema)

	rows, err := db.DB.Query(query, since, threshold)
	if err != nil {
		return err
	}
	defer rows.Close()

	type undercut struct {
		sku, platform, url  string
		ourPrice, compPrice float64
	}
	var found []undercut
	for rows.Next() {
		var u undercut
		if err := rows.Scan(&u.sku, &u.ourPrice, &u.compPrice, &u.platform, &u.url); err != nil {
			return err
		}
		found = append(found, u)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, u := range found {
		pct := (u.ourPrice - u.compPrice) / u.ourPrice * 100
		DispatchNotification(tenantID, "Competitor Undercut", u.sku, map[string]string{
			"sku":              u.sku,
			"our_price":        strconv.FormatFloat(u.ourPrice, 'f', 2, 64),
			"competitor_price": strconv.FormatFloat(u.compPrice, 'f', 2, 64),
			"undercut_pct":     strconv.FormatFloat(round2(pct), 'f', 2, 64),
			"platform":         u.platform,
			"source_url":       u.url,
		})
	}
	if len(found) > 0 {
		log.Printf("[UNDERCUT] tenant %s: %d SKU(s) undercut by >= %.2f%%", tenantID, len(found), threshold)
	}
	return nil
}

func getUndercutLastRun() (time.Time, error) {
	var lastRun sql.NullTime
	err := db.DB.QueryRow("SELECT last_run_at FROM public.competitor_undercut_state WHERE id = 1").Scan(&lastRun)
	if err == sql.ErrNoRows || !lastRun.Valid {
		// First run ever: look back 24h so a fresh install's first cycle is
		// not a guaranteed no-op. Same reasoning as getPatchIntakeLastRun.
		return time.Now().Add(-24 * time.Hour), nil
	}
	if err != nil {
		return time.Time{}, err
	}
	return lastRun.Time, nil
}

func setUndercutLastRun(t time.Time) error {
	_, err := db.DB.Exec(`
		INSERT INTO public.competitor_undercut_state (id, last_run_at) VALUES (1, $1)
		ON CONFLICT (id) DO UPDATE SET last_run_at = EXCLUDED.last_run_at`, t)
	return err
}
