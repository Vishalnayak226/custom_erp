package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
)

// Stage 146.1.4: the reorder suggestion compares a stock position, not just
// what is on the shelf. Counting only "available" re-proposed the same
// shortfall on every run while an open PO already covered it.

// reorderInbound is the stock already coming to (or owed from) one location,
// by SKU.
type reorderInbound struct {
	onOrder    map[string]int // open PO qty not yet received here
	inTransit  map[string]int // dispatched transfers not yet received here
	backOrders map[string]int // sales lines waiting for stock here
}

// loadReorderInbound reads the three open-document sums once per run.
func loadReorderInbound(tenantID, schema, location string) reorderInbound {
	in := reorderInbound{onOrder: map[string]int{}, inTransit: map[string]int{}, backOrders: map[string]int{}}

	if rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id FROM %s.documents
		 WHERE doctype = 'PurchaseOrder' AND deleted_at IS NULL AND status IN ('Approved', 'Partially Received')
		   AND COALESCE(NULLIF(data->>'target_warehouse', ''), data->>'location', '') = $1
		 ORDER BY created_at DESC LIMIT %d`, schema, optimizationScanRowCap), location); err == nil {
		var pos []string
		for rows.Next() {
			var id string
			if rows.Scan(&id) == nil {
				pos = append(pos, id)
			}
		}
		rows.Close()
		for _, id := range pos {
			ordered, err := fetchPOItemQuantities(tenantID, id)
			if err != nil {
				continue
			}
			received, _ := fetchGRNAcceptedQuantities(tenantID, id, "")
			for sku, q := range ordered {
				if open := q - received[sku]; open > 1e-9 {
					in.onOrder[sku] += int(open)
				}
			}
		}
	}

	if rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT data FROM %s.documents
		 WHERE doctype = 'TransferOrder' AND deleted_at IS NULL AND data->>'to_warehouse' = $1
		   AND status IN ('Dispatched', 'In Transit', 'Partially Received')
		 LIMIT %d`, schema, optimizationScanRowCap), location); err == nil {
		for rows.Next() {
			var raw []byte
			if rows.Scan(&raw) != nil {
				continue
			}
			var data map[string]interface{}
			if json.Unmarshal(raw, &data) != nil {
				continue
			}
			if bal, err := loadTransferBalances(data); err == nil {
				for _, b := range bal {
					if open := b.Open(); open > 0 {
						in.inTransit[b.Sku] += open
					}
				}
			}
		}
		rows.Close()
	}

	if rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT data->>'sku', COALESCE(SUM(COALESCE((data->>'qty')::numeric, 0)), 0)::int FROM %s.documents
		 WHERE doctype = 'SalesOrderLine' AND deleted_at IS NULL AND status = 'Pending' AND data->>'location_code' = $1
		 GROUP BY data->>'sku'`, schema), location); err == nil {
		for rows.Next() {
			var sku string
			var q int
			if rows.Scan(&sku, &q) == nil && sku != "" {
				in.backOrders[sku] = q
			}
		}
		rows.Close()
	}
	return in
}

// reorderConfigLimits are a ReorderPointConfig's order limits; zero = unset.
type reorderConfigLimits struct{ minOrder, maxStock, multiple int }

func resolveReorderLimits(schema, sku, location string) reorderConfigLimits {
	var mn, mx, mul float64
	if err := db.DB.QueryRow(fmt.Sprintf(`
		SELECT COALESCE(NULLIF(data->>'min_order_qty', '')::numeric, 0), COALESCE(NULLIF(data->>'max_stock_qty', '')::numeric, 0),
		       COALESCE(NULLIF(data->>'order_multiple', '')::numeric, 0)
		  FROM %s.documents
		 WHERE doctype = 'ReorderPointConfig' AND status = 'Active' AND deleted_at IS NULL
		   AND UPPER(data->>'item') = UPPER($1) AND UPPER(data->>'location_code') = UPPER($2) LIMIT 1`, schema),
		sku, location).Scan(&mn, &mx, &mul); err != nil {
		return reorderConfigLimits{}
	}
	return reorderConfigLimits{minOrder: int(mn), maxStock: int(mx), multiple: int(mul)}
}

// shapeReorderQty turns the shortfall into an order quantity: up to the max
// stock when one is set, at least the minimum, in whole multiples, and never
// past the max when a smaller whole multiple still covers the shortfall.
func shapeReorderQty(need, position int, l reorderConfigLimits) int {
	qty := need
	if l.maxStock > 0 && l.maxStock-position > qty {
		qty = l.maxStock - position
	}
	mul := l.multiple
	if mul < 1 {
		mul = 1
	}
	up := func(n int) int { return (n + mul - 1) / mul * mul }
	qty = up(qty)
	if l.minOrder > qty {
		qty = up(l.minOrder)
	}
	if l.maxStock > 0 && position+qty > l.maxStock {
		if fit := (l.maxStock - position) / mul * mul; fit >= need && fit > 0 {
			qty = fit
		}
	}
	return qty
}
