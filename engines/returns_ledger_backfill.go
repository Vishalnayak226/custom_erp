package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Stage 132.20 (user decision 2026-10-26 - a client data change): a customer
// return whose QC ran before Stage 132.13 (p) put its units back on hand
// without a stock ledger line, so the Stock Ledger's running balance stayed
// behind on hand by every returned unit (Guide, on app). This writes each
// missing line once - the voucher, bucket and idempotency key ApplyReturnQC
// writes today, so it can never count twice and a re-run is a no-op - stamped
// at the return's last update (its QC) and marked `backfilled`. Stock levels
// are not touched: those units were already counted back.

// BackfillReturnStockLedgerAllTenants runs the backfill for every client
// schema (cmd/server -migrate), the BackfillLocationCostCentersAllTenants way.
func BackfillReturnStockLedgerAllTenants() (map[string]int, error) {
	rows, err := db.DB.Query(`SELECT MIN(tenant_id) FROM public.tenants GROUP BY schema_name ORDER BY 1`)
	if err != nil {
		return nil, err
	}
	var tenants []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			rows.Close()
			return nil, err
		}
		tenants = append(tenants, t)
	}
	rows.Close()
	out := map[string]int{}
	for _, t := range tenants {
		n, err := BackfillReturnStockLedger(t)
		if n > 0 {
			out[t] = n
		}
		if err != nil {
			return out, fmt.Errorf("tenant %s: %v", t, err)
		}
	}
	return out, nil
}

// BackfillReturnStockLedger writes the missing return lines for one client
// and reports how many it wrote.
func BackfillReturnStockLedger(tenantID string) (int, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, err
	}
	var exists bool
	if err := db.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_schema = $1 AND table_name = 'documents')`, schema).Scan(&exists); err != nil || !exists {
		return 0, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, data::text, COALESCE(updated_at, created_at) FROM %s.documents
		WHERE doctype = 'ReturnRequest' AND deleted_at IS NULL AND data ? 'items'`, schema))
	if err != nil {
		return 0, err
	}
	type pending struct {
		id, location string
		at           time.Time
		items        []returnItemRecord
	}
	var todo []pending
	for rows.Next() {
		var id, raw string
		var at time.Time
		if err := rows.Scan(&id, &raw, &at); err != nil {
			rows.Close()
			return 0, err
		}
		var d struct {
			Items          []returnItemRecord `json:"items"`
			ReturnLocation string             `json:"return_location"`
		}
		if json.Unmarshal([]byte(raw), &d) != nil || d.ReturnLocation == "" {
			continue
		}
		todo = append(todo, pending{id: id, location: d.ReturnLocation, at: at, items: d.Items})
	}
	rows.Close()

	written := 0
	for _, r := range todo {
		for _, it := range r.items {
			rule, ok := returnDispositionRule[strings.TrimSpace(it.Disposition)]
			if !ok || !rule.ReceivesStock || it.Qty <= 0 || it.SKU == "" {
				continue // not QC'd yet, or nothing came back on hand
			}
			key := fmt.Sprintf("ReturnRequest:%s:%s:QC", r.id, it.SKU)
			var have bool
			if err := db.DB.QueryRow(fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM %s.documents WHERE doctype = 'StockLedgerEntry' AND data->>'idempotency_key' = $1)`, schema), key).Scan(&have); err != nil {
				return written, err
			}
			if have {
				continue
			}
			tx, err := db.DB.Begin()
			if err != nil {
				return written, err
			}
			if err := insertStockLedgerEntry(tx, schema, StockLedgerEntry{
				ItemID: it.SKU, WarehouseID: r.location, Qty: float64(it.Qty),
				VoucherType: "SalesReturn", VoucherID: r.id, IdempotencyKey: key,
				ToStatus: rule.Bucket, UserID: "backfill-132.20",
			}); err != nil {
				tx.Rollback()
				return written, fmt.Errorf("return %s %s: %v", r.id, it.SKU, err)
			}
			if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET created_at = $2, data = data || '{"backfilled":"132.20"}'::jsonb
				WHERE doctype = 'StockLedgerEntry' AND data->>'idempotency_key' = $1`, schema), key, r.at); err != nil {
				tx.Rollback()
				return written, err
			}
			if err := tx.Commit(); err != nil {
				return written, err
			}
			written++
		}
	}
	return written, nil
}
