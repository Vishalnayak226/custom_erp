package engines

import (
	"fmt"
	"sort"
	"strings"

	"custom_erp/db"
)

// Stage 85 reports: stock by the store's own locations (85.B), and customers
// sharing one mobile number (85.C2 - the duplicates the one-phone-one-customer
// rule refuses from now on, but which may already be on file).
func init() {
	RegisterReport(ReportDefinition{
		ID: "stock-by-location", Label: "Stock by Location", Category: "Inventory",
		Columns: []ReportColumn{
			{Key: "location_code", Label: "Store"}, {Key: "sku", Label: "SKU"},
			{Key: "main", Label: "Main"}, {Key: "inward", Label: "Inward"}, {Key: "in_transit", Label: "In-Transit"},
			{Key: "qc_hold", Label: "QC Hold"}, {Key: "damaged", Label: "Damage"}, {Key: "on_hold", Label: "On Hold"},
			{Key: "purchase_return", Label: "Purchase Return"}, {Key: "own_locations", Label: "Own locations"},
			{Key: "sellable", Label: "Sellable"},
		},
		Params: []ReportParam{{Key: "store", Label: "Store code (blank = all)", Type: "text"}},
		Run:    stockByLocationReport,
	})
	RegisterReport(ReportDefinition{
		ID: "advance-register", Label: "Customer Advance Register", Category: "Sales",
		Columns: []ReportColumn{
			{Key: "number", Label: "Receipt voucher"}, {Key: "taken_at", Label: "Taken"}, {Key: "location", Label: "Store"},
			{Key: "customer_name", Label: "Customer"}, {Key: "purpose", Label: "For"}, {Key: "expected_date", Label: "Expected"},
			{Key: "amount", Label: "Taken"}, {Key: "applied", Label: "Used"}, {Key: "refunded", Label: "Refunded"},
			{Key: "balance", Label: "Balance"}, {Key: "status", Label: "Status"}, {Key: "payment_mode", Label: "Paid by"},
		},
		Params: []ReportParam{{Key: "store", Label: "Store code (blank = all)", Type: "text"}, {Key: "open", Label: "Open only", Type: "select", Options: ",Yes"}},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			list, err := ListCustomerAdvances(tenantID, "", params["store"], params["open"] == "Yes")
			if err != nil {
				return nil, err
			}
			out := make([]map[string]interface{}, 0, len(list))
			for _, a := range list {
				out = append(out, map[string]interface{}{"number": a.Number, "taken_at": a.TakenAt, "location": a.Location, "customer_name": a.CustomerName,
					"purpose": a.Purpose, "expected_date": a.ExpectedDate, "amount": a.Amount, "applied": a.Applied, "refunded": a.Refunded,
					"balance": a.Balance, "status": a.Status, "payment_mode": a.PaymentMode})
			}
			return out, nil
		},
	})
	RegisterReport(ReportDefinition{
		ID: "tender-sales", Label: "Tender-wise Sales", Category: "Sales",
		Columns: []ReportColumn{{Key: "tender", Label: "Tender"}, {Key: "amount", Label: "Amount"}},
		Params:  []ReportParam{{Key: "store", Label: "Store code (blank = all)", Type: "text"}, {Key: "from", Label: "From", Type: "date"}, {Key: "to", Label: "To", Type: "date"}},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			totals, err := TenderTotals(tenantID, "", params["store"], params["from"], params["to"])
			if err != nil {
				return nil, err
			}
			out := []map[string]interface{}{}
			for k, v := range totals {
				out = append(out, map[string]interface{}{"tender": k, "amount": v})
			}
			sort.Slice(out, func(i, j int) bool { return out[i]["amount"].(float64) > out[j]["amount"].(float64) })
			return out, nil
		},
	})
	RegisterReport(ReportDefinition{
		ID: "salesman-sales", Label: "Salesman-wise Sales", Category: "Sales",
		Columns: []ReportColumn{{Key: "salesman", Label: "Salesman"}, {Key: "bills", Label: "Bills"}, {Key: "amount", Label: "Sales"}},
		Params:  []ReportParam{{Key: "store", Label: "Store code (blank = all)", Type: "text"}, {Key: "from", Label: "From", Type: "date"}, {Key: "to", Label: "To", Type: "date"}},
		Run:     salesmanSalesReport,
	})
	RegisterReport(ReportDefinition{
		ID: "customer-duplicate-phones", Label: "Customers Sharing a Mobile Number", Category: "CRM",
		Columns: []ReportColumn{
			{Key: "phone", Label: "Mobile", Sensitive: true}, {Key: "count", Label: "Customers"},
			{Key: "customers", Label: "Customer codes and names"},
		},
		Run: customerDuplicatePhonesReport,
	})
}

func stockByLocationReport(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	q := fmt.Sprintf(`SELECT location_code, sku, available, blocked, hold_qty, inward, in_transit, qc_hold, damaged, rtv,
		reserved, safety_stock, channel_buffer FROM %s.inventory_availability`, schema)
	args := []interface{}{}
	if store := strings.TrimSpace(params["store"]); store != "" {
		q += " WHERE location_code = $1"
		args = append(args, store)
	}
	q += " ORDER BY location_code, sku"
	rows, err := db.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	policy := atsPolicyFor(schema)
	out := []map[string]interface{}{}
	for rows.Next() {
		var store, sku string
		var available, blocked, hold, inward, inTransit, qc, damaged, rtv, reserved, safety, buffer int
		if err := rows.Scan(&store, &sku, &available, &blocked, &hold, &inward, &inTransit, &qc, &damaged, &rtv, &reserved, &safety, &buffer); err != nil {
			return nil, err
		}
		out = append(out, map[string]interface{}{
			"location_code": store, "sku": sku, "main": available - blocked - hold, "inward": inward, "in_transit": inTransit,
			"qc_hold": qc, "damaged": damaged, "on_hold": hold, "purchase_return": rtv, "own_locations": blocked,
			"sellable": computeATS(policy, available, reserved, safety, blocked, qc, damaged, buffer, hold),
		})
	}
	return out, rows.Err()
}

func customerDuplicatePhonesReport(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT data->>'phone', COUNT(*), string_agg(id || ' ' || COALESCE(data->>'name', ''), '; ' ORDER BY created_at)
		  FROM %s.documents
		 WHERE doctype = 'Customer' AND deleted_at IS NULL AND status != 'Cancelled'
		   AND COALESCE(data->>'phone', '') <> '' AND COALESCE(data->>'merged_into', '') = ''
		   AND COALESCE(data->>'walk_in', '') <> 'Yes'
		 GROUP BY data->>'phone' HAVING COUNT(*) > 1
		 ORDER BY COUNT(*) DESC, data->>'phone'`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]interface{}{}
	for rows.Next() {
		var phone, list string
		var n int
		if err := rows.Scan(&phone, &n, &list); err != nil {
			return nil, err
		}
		out = append(out, map[string]interface{}{"phone": phone, "count": n, "customers": list})
	}
	return out, rows.Err()
}

func salesmanSalesReport(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	q := fmt.Sprintf(`SELECT COALESCE(NULLIF(s.data->>'name', ''), NULLIF(c.data->>'salesman_id', ''), '(none)'), COUNT(*),
		COALESCE(SUM(COALESCE((c.data->>'amount_paid')::numeric, 0)), 0)
		FROM %[1]s.documents c LEFT JOIN %[1]s.documents s ON s.doctype = 'Salesman' AND s.id = c.data->>'salesman_id'
		WHERE c.doctype = 'POSCart' AND c.status = 'Paid' AND c.deleted_at IS NULL`, schema)
	args := []interface{}{}
	if v := strings.TrimSpace(params["store"]); v != "" {
		args = append(args, v)
		q += fmt.Sprintf(" AND c.data->>'location' = $%d", len(args))
	}
	if v := strings.TrimSpace(params["from"]); v != "" {
		args = append(args, v)
		q += fmt.Sprintf(" AND c.created_at >= $%d::date", len(args))
	}
	if v := strings.TrimSpace(params["to"]); v != "" {
		args = append(args, v)
		q += fmt.Sprintf(" AND c.created_at < ($%d::date + 1)", len(args))
	}
	q += " GROUP BY 1 ORDER BY 3 DESC"
	rows, err := db.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]interface{}{}
	for rows.Next() {
		var name string
		var n int
		var amount float64
		if err := rows.Scan(&name, &n, &amount); err != nil {
			return nil, err
		}
		out = append(out, map[string]interface{}{"salesman": name, "bills": n, "amount": round2(amount)})
	}
	return out, rows.Err()
}
