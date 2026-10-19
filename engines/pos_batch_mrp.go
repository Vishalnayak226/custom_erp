package engines

import (
	"custom_erp/db"
	"database/sql"
	"fmt"
	"math"
	"strings"
)

// Stage 94.6 (D2, approved 2026-10-18): MRP lives on the receipt's batch. The
// till charges the MRP of the stock it sells; when one barcode covers a SKU
// that sits on the shelf at two MRPs it asks "999 or 1,099?". Old stock is
// never repriced - selling above the printed MRP is illegal.
//
// A sale does not record which lot it took, so which MRPs are still on the
// shelf is worked out first-in-first-out: the store's stock on hand is the
// newest of its receipts of the SKU, since the oldest sell first. A SKU whose
// batches all carry one MRP (the common case) costs one query.

// BatchMRPChoices lists the MRPs of a SKU on the shelf at a store, newest
// receipt first. Empty when no batch of it carries an MRP.
func BatchMRPChoices(tenantID, sku, location string) []float64 {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil || strings.TrimSpace(sku) == "" {
		return nil
	}
	all := batchMRPsOfSKU(schema, sku)
	if len(all) <= 1 || strings.TrimSpace(location) == "" {
		return distinctMRPs(all)
	}
	var onHand float64
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(SUM(available), 0)::float8 FROM %s.inventory_availability WHERE sku = $1 AND location_code = $2`, schema), sku, location).Scan(&onHand)
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT data->>'batch_no', SUM((data->>'qty')::numeric)::float8, MAX(created_at)
		FROM %s.documents
		WHERE doctype = 'StockLedgerEntry' AND data->>'item_id' = $1 AND data->>'warehouse_id' = $2
		  AND COALESCE(data->>'batch_no', '') <> '' AND (data->>'qty')::numeric > 0
		GROUP BY 1 ORDER BY 3 DESC`, schema), sku, location)
	if err != nil {
		return distinctMRPs(all)
	}
	defer rows.Close()
	var out []float64
	covered := 0.0
	for rows.Next() && covered < onHand-1e-9 {
		var batch string
		var qty float64
		var at sql.NullTime
		if rows.Scan(&batch, &qty, &at) != nil {
			continue
		}
		covered += qty
		for _, b := range all {
			if b.batch == batch {
				out = append(out, b.mrp)
			}
		}
	}
	return distinctMRPs(toBatchMRPs(out))
}

type batchMRP struct {
	batch string
	mrp   float64
}

func toBatchMRPs(ms []float64) []batchMRP {
	out := make([]batchMRP, len(ms))
	for i, m := range ms {
		out[i] = batchMRP{mrp: m}
	}
	return out
}

// batchMRPsOfSKU is every batch of the SKU that carries an MRP, newest first.
func batchMRPsOfSKU(schema, sku string) []batchMRP {
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT data->>'batch_no', (data->>'mrp')::numeric::float8 FROM %s.documents
		WHERE doctype = 'Batch' AND deleted_at IS NULL AND data->>'item' = $1 AND COALESCE(data->>'mrp', '') <> ''
		  AND (data->>'mrp')::numeric > 0
		ORDER BY created_at DESC LIMIT 200`, schema), sku)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []batchMRP
	for rows.Next() {
		var b batchMRP
		if rows.Scan(&b.batch, &b.mrp) == nil {
			out = append(out, b)
		}
	}
	return out
}

func distinctMRPs(bs []batchMRP) []float64 {
	var out []float64
	for _, b := range bs {
		seen := false
		for _, m := range out {
			if math.Abs(m-b.mrp) < 0.005 {
				seen = true
			}
		}
		if !seen {
			out = append(out, round2(b.mrp))
		}
	}
	return out
}

// mrpForReceiptBarcode is the MRP a receipt-date barcode (Stage 57.7) was
// printed with: the batches of the receipts that code was issued on, when
// they all carry one MRP.
func mrpForReceiptBarcode(tenantID, code, sku string) float64 {
	code = strings.TrimSpace(code)
	if code == "" || code == sku {
		return 0
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT DISTINCT round((b.data->>'mrp')::numeric, 2)::float8
		FROM %s.documents ib
		JOIN %s.documents b ON b.doctype = 'Batch' AND b.deleted_at IS NULL AND b.data->>'item' = ib.data->>'item'
		 AND (b.data->>'grn' = ib.data->>'grn' OR COALESCE(ib.data->'grns', '[]'::jsonb) ? (b.data->>'grn'))
		WHERE ib.doctype = 'ItemBarcode' AND ib.deleted_at IS NULL AND (ib.id = $1 OR ib.data->>'code' = $1)
		  AND COALESCE(b.data->>'mrp', '') <> ''`, schema, schema), code)
	if err != nil {
		return 0
	}
	defer rows.Close()
	var mrps []float64
	for rows.Next() {
		var m float64
		if rows.Scan(&m) == nil && m > 0 {
			mrps = append(mrps, m)
		}
	}
	if len(mrps) == 1 {
		return mrps[0]
	}
	return 0
}

// resolveLineBatchMRP decides the MRP a till line sells at: the cashier's
// choice when it is one on the shelf, else the scanned receipt barcode's,
// else the only one on the shelf. With two or more on the shelf and no
// choice it returns the lowest - never above any printed MRP, so an offline
// sale replayed later is still legal - and the choices, for the till to ask.
func resolveLineBatchMRP(tenantID, location, sku string, in QuoteLineRequest) (float64, []float64) {
	choices := BatchMRPChoices(tenantID, sku, location)
	if len(choices) == 0 {
		return 0, nil
	}
	has := func(m float64) bool {
		for _, c := range choices {
			if math.Abs(c-m) < 0.005 {
				return true
			}
		}
		return false
	}
	if in.MRP > 0 && has(in.MRP) {
		return round2(in.MRP), nil
	}
	if m := mrpForReceiptBarcode(tenantID, in.Barcode, sku); m > 0 && has(m) {
		return m, nil
	}
	if len(choices) == 1 {
		return choices[0], nil
	}
	lowest := choices[0]
	for _, c := range choices {
		lowest = math.Min(lowest, c)
	}
	return lowest, choices
}
