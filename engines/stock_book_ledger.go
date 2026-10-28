package engines

import (
	"custom_erp/db"
	"fmt"
)

// Stage 55.6 - finding historic drift between the stock book and the stock
// ledger, so it can be settled by a count rather than by editing numbers.
//
// The known cause it was built for: before the 2026-10-09 transfer fix a
// dispatch moved units into transit without taking them off the source's
// on_hand, so the same units were on the books twice. The ledger recorded the
// dispatch correctly (a negative line at the source), so for every SKU and
// location the book's on_hand and the sum of its ledger lines should agree;
// where they do not, this report says by how much.
//
// Two honest limits, stated on the report too: ledger lines written before
// Stage 132.20 / 135.16 were best-effort, after the stock committed (a failed
// write left a gap that also shows here), and condition changes record the
// available-to-sell move rather than an on-hand one, so they are left out of
// the sum. Nothing is corrected here - a counted, approved cycle count is the
// only way to change on_hand.
//
// Stage 135.17: the same row also checks a store's own locations (Alteration,
// Display, ...). Their stock is inside available and held back by blocked, so
// the sum of stock_location_balance must equal blocked.

// StockBookLedgerRow is one SKU at one location.
type StockBookLedgerRow struct {
	Sku            string  `json:"sku"`
	Location       string  `json:"location_code"`
	OnHand         float64 `json:"on_hand"`
	LedgerSum      float64 `json:"ledger_sum"`
	Difference     float64 `json:"difference"`
	Lines          int     `json:"ledger_lines"`
	Blocked        int     `json:"blocked"`
	InOwnLocations int     `json:"in_own_locations"`
	Hint           string  `json:"hint"`
}

// StockBookVsLedger compares every SKU/location (or one of each) and returns
// the rows that differ, or all rows when mismatchesOnly is false.
func StockBookVsLedger(tenantID, sku, location string, mismatchesOnly bool) ([]StockBookLedgerRow, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		WITH ledger AS (
			SELECT data->>'item_id' AS sku, data->>'warehouse_id' AS loc,
			       SUM(COALESCE(NULLIF(data->>'qty', '')::numeric, 0)) AS qty, COUNT(*) AS n
			  FROM %[1]s.documents
			 WHERE doctype = 'StockLedgerEntry' AND deleted_at IS NULL
			   AND COALESCE(data->>'voucher_type', '') <> 'ConditionChange'
			 GROUP BY 1, 2
		), own AS (
			SELECT sku, location_code AS loc, SUM(qty) AS qty
			  FROM %[1]s.stock_location_balance
			 GROUP BY 1, 2
		)
		SELECT COALESCE(ia.sku, l.sku, o.sku), COALESCE(ia.location_code, l.loc, o.loc),
		       COALESCE(ia.on_hand, 0), COALESCE(l.qty, 0), COALESCE(l.n, 0),
		       COALESCE(ia.blocked, 0), COALESCE(o.qty, 0)
		  FROM %[1]s.inventory_availability ia
		  FULL OUTER JOIN ledger l ON l.sku = ia.sku AND l.loc = ia.location_code
		  FULL OUTER JOIN own o ON o.sku = COALESCE(ia.sku, l.sku) AND o.loc = COALESCE(ia.location_code, l.loc)
		 WHERE ($1 = '' OR COALESCE(ia.sku, l.sku, o.sku) = $1)
		   AND ($2 = '' OR COALESCE(ia.location_code, l.loc, o.loc) = $2)
		 ORDER BY 1, 2`, schema), sku, location)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []StockBookLedgerRow{}
	for rows.Next() {
		var r StockBookLedgerRow
		if err := rows.Scan(&r.Sku, &r.Location, &r.OnHand, &r.LedgerSum, &r.Lines, &r.Blocked, &r.InOwnLocations); err != nil {
			return nil, err
		}
		r.Difference = round2(r.OnHand - r.LedgerSum)
		switch {
		case r.Difference == 0:
			r.Hint = "agrees"
		case r.Lines == 0:
			r.Hint = "no ledger history - stock loaded before the ledger existed, or by import; count it to start its history"
		case r.Difference > 0:
			r.Hint = "book is higher than the ledger - e.g. a transfer dispatched before 2026-10-09 never left the source's books; count it"
		default:
			r.Hint = "book is lower than the ledger - a ledger line may be missing or doubled; count it"
		}
		ownOff := r.InOwnLocations != r.Blocked
		if ownOff {
			msg := fmt.Sprintf("the store's own locations hold %d but %d is set aside for them - move the difference back to Main, or count those locations", r.InOwnLocations, r.Blocked)
			if r.Difference == 0 {
				r.Hint = msg
			} else {
				r.Hint += "; " + msg
			}
		}
		if mismatchesOnly && r.Difference == 0 && !ownOff {
			continue
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func init() {
	RegisterReport(ReportDefinition{
		ID: "stock-book-vs-ledger", Label: "Stock Book vs Ledger (drift finder)", Category: "Inventory",
		Columns: []ReportColumn{
			{Key: "sku", Label: "Item"},
			{Key: "location_code", Label: "Location"},
			{Key: "on_hand", Label: "On hand (book)"},
			{Key: "ledger_sum", Label: "Ledger total"},
			{Key: "difference", Label: "Difference"},
			{Key: "ledger_lines", Label: "Ledger lines"},
			{Key: "in_own_locations", Label: "In own locations"},
			{Key: "blocked", Label: "Set aside (book)"},
			{Key: "hint", Label: "What it suggests"},
		},
		Params: []ReportParam{
			{Key: "sku", Label: "Item Code (optional)", Type: "text"},
			{Key: "location", Label: "Location (optional)", Type: "text"},
			{Key: "all", Label: "Include rows that agree", Type: "select", Options: "No,Yes"},
		},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			rows, err := StockBookVsLedger(tenantID, params["sku"], params["location"], params["all"] != "Yes")
			if err != nil {
				return nil, err
			}
			return structsToRows(rows)
		},
	})
}
