package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Stage 72.13: the item & material master blueprint's section 20 reports,
// registered in the ordinary Report Catalog (engines/report_registry.go).
// Gift Card Liability / Ledger live with the engine in stored_value.go.

func init() {
	RegisterReport(ReportDefinition{
		ID: "item-classification-register", Label: "Item Classification Register", Category: "Inventory",
		Columns: []ReportColumn{
			{Key: "sku", Label: "SKU"}, {Key: "name", Label: "Item"}, {Key: "profile", Label: "Item Type"},
			{Key: "commercial_class", Label: "Class"}, {Key: "stock_tracking", Label: "Stock"}, {Key: "valuation", Label: "Valuation"},
			{Key: "accounting_treatment", Label: "Accounting"}, {Key: "sellability", Label: "Sellability"}, {Key: "bill_mode", Label: "Bill"},
			{Key: "capture_method", Label: "Capture"}, {Key: "consumption_method", Label: "Consumption"}, {Key: "overrides", Label: "Item overrides"}, {Key: "status", Label: "Status"},
		},
		Params: []ReportParam{{Key: "item_type", Label: "Item Type", Type: "text"}, {Key: "sellability", Label: "Sellability", Type: "select", Options: ",Normal sale,Promo only,Not sellable,Stored-value issue only"}},
		Run:    itemClassificationRegister,
	})
	RegisterReport(ReportDefinition{
		ID: "item-master-completeness", Label: "Item Master Completeness", Category: "Exceptions",
		Columns: []ReportColumn{{Key: "sku", Label: "SKU"}, {Key: "name", Label: "Item"}, {Key: "profile", Label: "Item Type"}, {Key: "missing", Label: "Missing"}},
		Params:  []ReportParam{},
		Run:     itemMasterCompleteness,
	})
	RegisterReport(ReportDefinition{
		ID: "backflush-consumption", Label: "Backflush Consumption", Category: "Inventory",
		Columns: []ReportColumn{
			{Key: "date", Label: "Date"}, {Key: "bill", Label: "Bill"}, {Key: "store", Label: "Store"}, {Key: "sold_sku", Label: "Sold item"},
			{Key: "group", Label: "Group"}, {Key: "component", Label: "Consumed"}, {Key: "qty", Label: "Qty"}, {Key: "default_component", Label: "Default"},
			{Key: "overridden", Label: "Changed"}, {Key: "reason", Label: "Reason"}, {Key: "rule", Label: "Rule (version)"},
		},
		Params: []ReportParam{{Key: "from", Label: "From", Type: "date"}, {Key: "to", Label: "To", Type: "date"}, {Key: "store", Label: "Store", Type: "text"}},
		Run:    backflushConsumptionReport,
	})
	RegisterReport(ReportDefinition{
		ID: "gwp-issue-register", Label: "Gift With Purchase Register", Category: "Sales",
		Columns: []ReportColumn{
			{Key: "date", Label: "Date"}, {Key: "bill", Label: "Bill"}, {Key: "store", Label: "Store"}, {Key: "customer", Label: "Customer"},
			{Key: "gift", Label: "Gift"}, {Key: "qty", Label: "Qty"}, {Key: "offer", Label: "Offer"},
		},
		Params: []ReportParam{{Key: "from", Label: "From", Type: "date"}, {Key: "to", Label: "To", Type: "date"}},
		Run:    gwpIssueRegister,
	})
	RegisterReport(ReportDefinition{
		ID: "non-valuated-stock", Label: "Non-Valuated Stock", Category: "Inventory",
		Columns: []ReportColumn{{Key: "sku", Label: "SKU"}, {Key: "name", Label: "Item"}, {Key: "profile", Label: "Item Type"}, {Key: "store", Label: "Store"}, {Key: "on_hand", Label: "On hand"}, {Key: "available", Label: "Available"}},
		Params:  []ReportParam{},
		Run:     nonValuatedStockReport,
	})
	RegisterReport(ReportDefinition{
		ID: "inactive-items-with-stock", Label: "Inactive Items With Stock", Category: "Exceptions",
		Columns: []ReportColumn{{Key: "sku", Label: "SKU"}, {Key: "name", Label: "Item"}, {Key: "status", Label: "Status"}, {Key: "store", Label: "Store"}, {Key: "on_hand", Label: "On hand"}},
		Params:  []ReportParam{},
		Run:     inactiveItemsWithStock,
	})
	RegisterReport(ReportDefinition{
		ID: "item-exceptions", Label: "Item Exceptions", Category: "Exceptions",
		Columns: []ReportColumn{
			{Key: "date", Label: "Date"}, {Key: "type", Label: "Type"}, {Key: "source", Label: "Source"}, {Key: "sku", Label: "SKU"},
			{Key: "store", Label: "Store"}, {Key: "code", Label: "Code"}, {Key: "message", Label: "What happened"}, {Key: "qty", Label: "Qty"}, {Key: "status", Label: "Status"},
		},
		Params: []ReportParam{{Key: "status", Label: "Status", Type: "select", Options: ",Open,Logged,Resolved"}},
		Run:    itemExceptionsReport,
	})
}

type reportItem struct {
	id, sku, status string
	data            map[string]interface{}
}

func loadReportItems(tenantID string, includeDeleted bool) ([]reportItem, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	where := "AND deleted_at IS NULL"
	if includeDeleted {
		where = ""
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, data::text, COALESCE(status, ''), deleted_at IS NOT NULL FROM %s.documents WHERE doctype = 'Item' %s ORDER BY COALESCE(data->>'code', id)`, schema, where))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []reportItem
	for rows.Next() {
		var id, raw, status string
		var deleted bool
		if err := rows.Scan(&id, &raw, &status, &deleted); err != nil {
			return nil, err
		}
		var d map[string]interface{}
		if json.Unmarshal([]byte(raw), &d) != nil {
			continue
		}
		sku := strings.TrimSpace(pimString(d["code"]))
		if sku == "" {
			sku = id
		}
		if deleted {
			status = "Deleted"
		}
		out = append(out, reportItem{id: id, sku: sku, status: status, data: d})
	}
	return out, rows.Err()
}

func itemClassificationRegister(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
	items, err := loadReportItems(tenantID, false)
	if err != nil {
		return nil, err
	}
	wantType, wantSell := strings.TrimSpace(params["item_type"]), policyKey(params["sellability"])
	out := []map[string]interface{}{}
	for _, it := range items {
		p := ItemPolicyFor(tenantID, it.data)
		if wantType != "" && !strings.EqualFold(wantType, p.Profile) {
			continue
		}
		if wantSell != "" && policyKey(p.Sellability) != wantSell {
			continue
		}
		var overrides []string
		for _, dim := range policyDimensions {
			if p.Source[dim.field] == "item" {
				overrides = append(overrides, dim.label)
			}
		}
		out = append(out, map[string]interface{}{
			"sku": it.sku, "name": pimString(it.data["name"]), "profile": p.Profile,
			"commercial_class": policyLabel(p.Class), "stock_tracking": policyLabel(p.StockTracking), "valuation": policyLabel(p.Valuation),
			"accounting_treatment": policyLabel(p.Accounting), "sellability": policyLabel(p.Sellability), "bill_mode": policyLabel(p.BillMode),
			"capture_method": policyLabel(p.Capture), "consumption_method": policyLabel(p.Consumption),
			"overrides": strings.Join(overrides, ", "), "status": it.status,
		})
	}
	return out, nil
}

// itemMasterCompleteness lists what each item still lacks for the way it is
// classified (blueprint 20 "Master Completeness Exceptions").
func itemMasterCompleteness(tenantID string, _ map[string]string) ([]map[string]interface{}, error) {
	items, err := loadReportItems(tenantID, false)
	if err != nil {
		return nil, err
	}
	out := []map[string]interface{}{}
	for _, it := range items {
		p := ItemPolicyFor(tenantID, it.data)
		var missing []string
		if strings.TrimSpace(pimString(it.data["name"])) == "" {
			missing = append(missing, "name")
		}
		if strings.TrimSpace(pimString(it.data["category"])) == "" && (p.Class == ClassMerchandise || p.Class == ClassAccessory) {
			missing = append(missing, "category")
		}
		sold := p.Sellability == SellNormal
		if sold && strings.TrimSpace(pimString(it.data["hsn_code"])) == "" {
			missing = append(missing, "HSN / SAC code")
		}
		if sold && numFromInterface(it.data["sale_price"]) <= 0 && numFromInterface(it.data["mrp"]) <= 0 {
			missing = append(missing, "sale price or MRP")
		}
		if p.Valuated() && numFromInterface(it.data["standard_cost"]) <= 0 {
			if _, ok := ResolveQuoteUnitCostPaise(tenantID, it.sku); !ok {
				missing = append(missing, "a cost (no receipt yet and no standard cost)")
			}
		}
		if len(missing) == 0 {
			continue
		}
		out = append(out, map[string]interface{}{"sku": it.sku, "name": pimString(it.data["name"]), "profile": p.Profile, "missing": strings.Join(missing, ", ")})
	}
	return out, nil
}

// paidCarts reads Paid POS bills in a date range.
func paidCarts(tenantID string, params map[string]string, extra string) ([]struct {
	id, date string
	data     map[string]interface{}
}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, to_char(created_at, 'YYYY-MM-DD'), data::text FROM %s.documents
		WHERE doctype = 'POSCart' AND status = 'Paid' %s
		  AND ($1 = '' OR created_at >= public.erp_day_start($1::date)) AND ($2 = '' OR created_at < public.erp_day_start($2::date + 1))
		ORDER BY created_at DESC LIMIT 5000`, schema, extra), strings.TrimSpace(params["from"]), strings.TrimSpace(params["to"]))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []struct {
		id, date string
		data     map[string]interface{}
	}
	for rows.Next() {
		var id, date, raw string
		if err := rows.Scan(&id, &date, &raw); err != nil {
			return nil, err
		}
		var d map[string]interface{}
		if json.Unmarshal([]byte(raw), &d) != nil {
			continue
		}
		out = append(out, struct {
			id, date string
			data     map[string]interface{}
		}{id, date, d})
	}
	return out, rows.Err()
}

func backflushConsumptionReport(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
	// CASE, not AND: Postgres may evaluate either side of an AND first, and
	// jsonb_array_length on a non-array consumption crashed the whole report
	// ("cannot get array length of a scalar") - Guide, Stage 132.13.
	carts, err := paidCarts(tenantID, params, `AND CASE WHEN jsonb_typeof(data->'consumption') = 'array' THEN jsonb_array_length(data->'consumption') > 0 ELSE FALSE END`)
	if err != nil {
		return nil, err
	}
	store := strings.TrimSpace(params["store"])
	out := []map[string]interface{}{}
	for _, c := range carts {
		loc := pimString(c.data["location"])
		if store != "" && !strings.EqualFold(store, loc) {
			continue
		}
		arr, _ := c.data["consumption"].([]interface{})
		for _, raw := range arr {
			m, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			changed := ""
			if m["overridden"] == true {
				changed = "Yes"
			}
			comp := pimString(m["component_name"])
			if comp == "" {
				comp = pimString(m["component"])
			}
			if comp == "" {
				comp = "None"
			}
			out = append(out, map[string]interface{}{
				"date": c.date, "bill": c.id, "store": loc, "sold_sku": pimString(m["for_sku"]), "group": pimString(m["group"]),
				"component": comp, "qty": numFromInterface(m["qty"]), "default_component": pimString(m["default_name"]),
				"overridden": changed, "reason": pimString(m["reason"]), "rule": pimString(m["rule_version"]),
			})
		}
	}
	return out, nil
}

func gwpIssueRegister(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
	carts, err := paidCarts(tenantID, params, `AND data::text LIKE '%gwp_offer%'`)
	if err != nil {
		return nil, err
	}
	out := []map[string]interface{}{}
	for _, c := range carts {
		arr, _ := c.data["items"].([]interface{})
		for _, raw := range arr {
			m, ok := raw.(map[string]interface{})
			if !ok || pimString(m["price_source"]) != PriceSourceGWP {
				continue
			}
			out = append(out, map[string]interface{}{
				"date": c.date, "bill": c.id, "store": pimString(c.data["location"]), "customer": pimString(c.data["customer_id"]),
				"gift": pimString(m["sku"]), "qty": numFromInterface(m["qty"]), "offer": pimString(m["promotion_ref"]),
			})
		}
	}
	return out, nil
}

// stockByItem reads every store's stock row.
func stockRows(tenantID string) (map[string][][3]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT sku, location_code, on_hand, available FROM %s.inventory_availability WHERE on_hand <> 0 OR available <> 0`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][][3]interface{}{}
	for rows.Next() {
		var sku, loc string
		var onHand, avail int
		if err := rows.Scan(&sku, &loc, &onHand, &avail); err != nil {
			return nil, err
		}
		out[sku] = append(out[sku], [3]interface{}{loc, onHand, avail})
	}
	return out, rows.Err()
}

func nonValuatedStockReport(tenantID string, _ map[string]string) ([]map[string]interface{}, error) {
	items, err := loadReportItems(tenantID, false)
	if err != nil {
		return nil, err
	}
	stock, err := stockRows(tenantID)
	if err != nil {
		return nil, err
	}
	out := []map[string]interface{}{}
	for _, it := range items {
		p := ItemPolicyFor(tenantID, it.data)
		if !p.Tracked() || p.Valuated() {
			continue
		}
		for _, s := range stock[it.sku] {
			out = append(out, map[string]interface{}{"sku": it.sku, "name": pimString(it.data["name"]), "profile": p.Profile, "store": s[0], "on_hand": s[1], "available": s[2]})
		}
	}
	return out, nil
}

func inactiveItemsWithStock(tenantID string, _ map[string]string) ([]map[string]interface{}, error) {
	items, err := loadReportItems(tenantID, true)
	if err != nil {
		return nil, err
	}
	stock, err := stockRows(tenantID)
	if err != nil {
		return nil, err
	}
	out := []map[string]interface{}{}
	for _, it := range items {
		st := it.status
		if st == "" || strings.EqualFold(st, "Active") {
			if s := pimString(it.data["status"]); s != "" && !strings.EqualFold(s, "Active") {
				st = s
			} else {
				continue
			}
		}
		for _, s := range stock[it.sku] {
			if n, _ := s[1].(int); n == 0 {
				continue
			}
			out = append(out, map[string]interface{}{"sku": it.sku, "name": pimString(it.data["name"]), "status": st, "store": s[0], "on_hand": s[1]})
		}
	}
	return out, nil
}

func itemExceptionsReport(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT to_char(created_at, 'YYYY-MM-DD HH24:MI'), data::text, COALESCE(status, '') FROM %s.documents
		WHERE doctype = 'ItemException' AND deleted_at IS NULL AND ($1 = '' OR status = $1) ORDER BY created_at DESC LIMIT 5000`, schema), strings.TrimSpace(params["status"]))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]interface{}{}
	for rows.Next() {
		var at, raw, status string
		if err := rows.Scan(&at, &raw, &status); err != nil {
			return nil, err
		}
		var d map[string]interface{}
		if json.Unmarshal([]byte(raw), &d) != nil {
			continue
		}
		out = append(out, map[string]interface{}{
			"date": at, "type": pimString(d["process_type"]), "source": strings.TrimSpace(pimString(d["source_doctype"]) + " " + pimString(d["source_id"])),
			"sku": pimString(d["sku"]), "store": pimString(d["location"]), "code": pimString(d["error_code"]),
			"message": pimString(d["message"]), "qty": numFromInterface(d["qty"]), "status": status,
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return pimString(out[i]["date"]) > pimString(out[j]["date"]) })
	return out, rows.Err()
}
