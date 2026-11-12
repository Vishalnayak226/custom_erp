package engines

import (
	"custom_erp/db"
	"fmt"
	"sort"
	"strings"
)

// Stage 146.4.3 (built inside 73.4.2): the back-order list. Sales lines still
// Pending (no stock reserved) are summed per SKU and location, set against
// what is already in stock or coming, and the rest is what buying has to
// cover. "Send to buying" is the "backorder" source of the existing supply
// proposals: open proposals a buyer approves into a Draft PO, never a PO
// raised on its own. Promise dates are 148.27's, not built here.

// BackorderRow is one SKU waiting at one location.
type BackorderRow struct {
	Sku          string  `json:"sku"`
	ItemName     string  `json:"item_name,omitempty"`
	LocationCode string  `json:"location_code"`
	Waiting      float64 `json:"waiting"`
	Orders       int     `json:"orders"`
	Oldest       string  `json:"oldest,omitempty"`
	Available    float64 `json:"available"`
	Inward       float64 `json:"inward"`
	OnOrder      float64 `json:"on_order"`
	InTransit    float64 `json:"in_transit"`
	ToBuy        float64 `json:"to_buy"`
}

// ListBackorders reads the waiting lines of one location (blank = every
// location that has some), biggest shortfall first.
func ListBackorders(tenantID, location string) ([]BackorderRow, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	location = strings.TrimSpace(location)
	if location != "" {
		if loc, err := ResolveLocationReference(tenantID, location); err == nil {
			location = loc
		}
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT COALESCE(data->>'sku', ''), COALESCE(data->>'location_code', ''),
		       COALESCE(SUM(COALESCE(NULLIF(data->>'qty', '')::numeric, 0)), 0),
		       COUNT(DISTINCT COALESCE(data->>'order_id', id)), COALESCE(to_char(MIN(created_at), 'YYYY-MM-DD'), '')
		  FROM %s.documents
		 WHERE doctype = 'SalesOrderLine' AND deleted_at IS NULL AND status = 'Pending'
		   AND COALESCE(data->>'sku', '') <> '' AND ($1 = '' OR data->>'location_code' = $1)
		 GROUP BY 1, 2 LIMIT %d`, schema, optimizationScanRowCap), location)
	if err != nil {
		return nil, err
	}
	var out []BackorderRow
	for rows.Next() {
		var r BackorderRow
		if rows.Scan(&r.Sku, &r.LocationCode, &r.Waiting, &r.Orders, &r.Oldest) == nil {
			out = append(out, r)
		}
	}
	rows.Close()
	inbound := map[string]reorderInbound{}
	for i := range out {
		r := &out[i]
		in, ok := inbound[r.LocationCode]
		if !ok {
			in = loadReorderInbound(tenantID, schema, r.LocationCode)
			inbound[r.LocationCode] = in
		}
		var inward float64
		_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(available, 0), COALESCE(inward, 0) + COALESCE(qc_hold, 0)
			FROM %s.inventory_availability WHERE sku = $1 AND location_code = $2`, schema), r.Sku, r.LocationCode).Scan(&r.Available, &inward)
		r.Inward = inward
		r.OnOrder, r.InTransit = float64(in.onOrder[r.Sku]), float64(in.inTransit[r.Sku])
		if need := r.Waiting - r.Available - r.Inward - r.OnOrder - r.InTransit; need > 0 {
			r.ToBuy = need
		}
		if item, _, err := fetchDocData(tenantID, "Item", r.Sku); err == nil {
			r.ItemName = firstNonEmpty(str(item, "name"), r.Sku)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ToBuy != out[j].ToBuy {
			return out[i].ToBuy > out[j].ToBuy
		}
		return out[i].Sku < out[j].Sku
	})
	return out, nil
}

func init() {
	RegisterReport(ReportDefinition{ID: "backorder-list", Label: "Back orders", Category: "Procurement",
		Columns: []ReportColumn{
			{Key: "sku", Label: "SKU"}, {Key: "item_name", Label: "Item"}, {Key: "location_code", Label: "Location"},
			{Key: "waiting", Label: "Waiting"}, {Key: "orders", Label: "Orders"}, {Key: "oldest", Label: "Oldest"},
			{Key: "available", Label: "In stock"}, {Key: "inward", Label: "Inward"}, {Key: "on_order", Label: "On PO"},
			{Key: "in_transit", Label: "In transit"}, {Key: "to_buy", Label: "To buy"},
		},
		Params: []ReportParam{{Key: "location_code", Label: "Location", Type: "text"}},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			rows, err := ListBackorders(tenantID, params["location_code"])
			if err != nil {
				return nil, err
			}
			return structsToRows(rows)
		}})
}
