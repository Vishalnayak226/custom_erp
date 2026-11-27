package engines

import (
	"fmt"
	"strings"
)

// 298.1-298.2 (user, 2026-11-27: "I need proper dashbaord for each sales and
// all" ... "Dasbord with number and visulation"): one sales dataset for every
// channel and four ready-made dashboards on it - All sales, Store sales,
// Online sales and B2B sales.
//
// all_sales is one row per sold or returned line:
//   - Store: each line of a paid till bill, at what the customer paid for it
//     after offers (the bill's allocation, else qty x price), with its cost
//     from the bill's sale_costs;
//   - Online: each line of a sales order not cancelled (marketplaces, web,
//     Store app orders), at qty x unit price;
//   - B2B: each line of a sales invoice not raised from an order (an
//     invoice raised from an order is that order's sale, already counted);
//   - returns: each line of a return request not rejected or cancelled,
//     negative, under the channel of the sale it returns.
//
// Values include GST, as the till and every other sales dataset do. Margin is
// only for lines with a known cost (till bills and their returns): the value
// less GST at the item's own rate, less that cost.
//
// Each part shows only to a role that may read its record types, so a role
// that cannot open invoices sees All sales without the B2B lines.

// The B2B line value follows ewbSalesLineValue (the e-way bill's own rule),
// except that a line with only a taxable value and no tax figures counts
// that value without GST.

// salesItems is alias.data->'items' as a JSON array, also when it was stored
// as a JSON string (a package bill's lines).
func salesItems(alias string) string {
	return strings.NewReplacer(`d.data->'items'`, alias+`.data->'items'`, `d.data->>'items'`, alias+`.data->>'items'`).Replace(dsPOSItems)
}

// salesTillB2B: a till bill made out to a business (its GSTIN).
func salesTillB2B(alias string) string {
	return strings.ReplaceAll(`(jsonb_typeof(@.data->'b2b') = 'object' AND COALESCE(@.data->'b2b'->>'gstin', '') <> '')`, "@", alias)
}

// salesAt is the same position's field in a bill's per-line array.
func salesAt(array, field string) string {
	return fmt.Sprintf(`d.data->'%s'->((l.n - 1)::int)->>'%s'`, array, field)
}

const salesNumberRE = `'^[0-9]+(\.[0-9]+)?$'`

// salesFirstPositive is the first of exprs above zero (the invoice line's
// own rule, ewbSalesLineValue).
func salesFirstPositive(exprs ...string) string {
	parts := make([]string, len(exprs))
	for i, e := range exprs {
		parts[i] = fmt.Sprintf(`NULLIF(GREATEST(%s, 0), 0)`, e)
	}
	return `COALESCE(` + strings.Join(parts, ", ") + `, 0)`
}

// The columns every part gives, in this order.
const salesPartColumns = `sold_at, channel, sub_channel, customer_type, store_code, salesman_id, customer, doc_id, sku, kind, qty, value, cost`

var salesStorePart = datasetPart{doctypes: []string{"POSCart"}, sql: `SELECT d.created_at, 'Store'::text,
	CASE WHEN ` + salesTillB2B("d") + ` THEN 'Till B2B bill' ELSE 'Store bill' END,
	CASE WHEN ` + salesTillB2B("d") + ` THEN 'Business' WHEN COALESCE(d.data->>'customer_id', '') <> '' THEN 'Known customer' ELSE 'Walk-in' END,
	d.data->>'location', COALESCE(NULLIF(l.v->>'salesman_id', ''), d.data->>'salesman_id'), NULLIF(d.data->>'customer_id', ''),
	d.id, l.v->>'sku', 'Sale'::text, ` + dsNum(`l.v->>'qty'`) + `,
	CASE WHEN ` + salesAt("allocation", "sku") + ` = l.v->>'sku' AND COALESCE(` + salesAt("allocation", "net_paise") + `, '') ~ '^-?[0-9]+$'
		THEN (` + salesAt("allocation", "net_paise") + `)::numeric / 100
		ELSE ` + dsNum(`l.v->>'qty'`) + ` * ` + dsNum(`l.v->>'sale_price'`) + ` END,
	CASE WHEN ` + salesAt("sale_costs", "sku") + ` = l.v->>'sku' AND COALESCE(` + salesAt("sale_costs", "value_paise") + `, '') ~ ` + salesNumberRE + `
		THEN NULLIF((` + salesAt("sale_costs", "value_paise") + `)::numeric, 0) / 100 END
	FROM %[1]s.documents d CROSS JOIN LATERAL jsonb_array_elements(` + salesItems("d") + `) WITH ORDINALITY l(v, n)
	WHERE d.doctype = 'POSCart' AND d.status = 'Paid' AND d.deleted_at IS NULL`}

var salesOnlinePart = datasetPart{doctypes: []string{"SalesOrder"}, sql: `SELECT so.created_at, 'Online'::text,
	COALESCE(NULLIF(so.data->>'channel', ''), 'Manual'), 'Online buyer'::text,
	ln.data->>'location_code', NULL::text, NULLIF(so.data->>'customer_name', ''),
	so.id, ln.data->>'sku', 'Sale'::text, ` + dsNum(`ln.data->>'qty'`) + `,
	` + dsNum(`ln.data->>'qty'`) + ` * ` + dsNum(`ln.data->>'unit_price'`) + `, NULL::numeric
	FROM %[1]s.documents ln JOIN %[1]s.documents so ON so.doctype = 'SalesOrder' AND so.id = ln.data->>'order_id' AND so.deleted_at IS NULL
	WHERE ln.doctype = 'SalesOrderLine' AND ln.deleted_at IS NULL AND COALESCE(ln.data->>'line_status', '') <> 'Cancelled'
		AND COALESCE(so.data->>'order_status', '') <> 'Cancelled' AND COALESCE(so.status, '') <> 'Cancelled'`}

// salesDirectInvoice: an invoice not raised from an order (or a summary of
// orders).
func salesDirectInvoice(alias string) string {
	return strings.ReplaceAll(`(COALESCE(@.data->>'sales_order_id', '') = '' AND COALESCE(@.data->>'summary_order_ids', '') = '')`, "@", alias)
}

var salesB2BPart = datasetPart{doctypes: []string{"SalesInvoice"}, sql: `SELECT si.created_at, 'B2B'::text, 'Invoice'::text, 'Business'::text,
	si.data->>'location', NULL::text, NULLIF(si.data->>'customer', ''),
	si.id, COALESCE(NULLIF(l.v->>'sku', ''), NULLIF(l.v->>'item', ''), l.v->>'item_code'), 'Sale'::text, ` + dsNum(`l.v->>'qty'`) + `,
	COALESCE(NULLIF(` + salesFirstPositive(dsNum(`l.v->>'net_total'`), dsNum(`l.v->>'amount'`), dsNum(`l.v->>'line_total'`),
	salesFirstPositive(dsNum(`l.v->>'rate'`), dsNum(`l.v->>'unit_price'`), dsNum(`l.v->>'sale_price'`))+` * `+dsNum(`l.v->>'qty'`)) +
	`, 0) - ` + dsNum(`l.v->>'discount'`) + `, ` + dsNum(`l.v->>'taxable'`) + ` + ` + salesFirstPositive(dsNum(`l.v->>'tax'`),
	dsNum(`l.v->>'cgst'`)+` + `+dsNum(`l.v->>'sgst'`)+` + `+dsNum(`l.v->>'igst'`)) + `), NULL::numeric
	FROM %[1]s.documents si CROSS JOIN LATERAL jsonb_array_elements(` + salesItems("si") + `) l(v)
	WHERE si.doctype = 'SalesInvoice' AND si.deleted_at IS NULL AND COALESCE(si.status, '') NOT IN ('Draft', 'Cancelled')
		AND ` + salesDirectInvoice("si")}

// salesReturnPart is the returns of one kind of sale (origDoctype), under
// that sale's channel: negative quantities and values, and the cost the
// return recorded.
func salesReturnPart(origDoctype, channel, subChannel, customerType, customer string) datasetPart {
	return datasetPart{doctypes: []string{"ReturnRequest", origDoctype}, sql: `SELECT rr.created_at, ` + channel + `, ` + subChannel + `, ` + customerType + `,
	COALESCE(NULLIF(rr.data->>'return_location', ''), rr.data->>'original_store'), NULL::text, ` + customer + `,
	rr.id, l.v->>'sku', 'Return'::text, -` + dsNum(`l.v->>'qty'`) + `,
	-(CASE WHEN l.v->>'allocated' = 'true' AND COALESCE(l.v->>'net_paise', '') ~ '^-?[0-9]+$' THEN (l.v->>'net_paise')::numeric / 100
		ELSE ` + dsNum(`l.v->>'original_unit_price'`) + ` * ` + dsNum(`l.v->>'qty'`) + ` END),
	CASE WHEN COALESCE(l.v->>'original_cost_price', '') ~ ` + salesNumberRE + `
		THEN -NULLIF((l.v->>'original_cost_price')::numeric, 0) * ` + dsNum(`l.v->>'qty'`) + ` END
	FROM %[1]s.documents rr JOIN %[1]s.documents od ON od.doctype = '` + origDoctype + `' AND od.id = rr.data->>'original_order_id'
	CROSS JOIN LATERAL jsonb_array_elements(` + salesItems("rr") + `) l(v)
	WHERE rr.doctype = 'ReturnRequest' AND rr.deleted_at IS NULL AND COALESCE(rr.status, '') NOT IN ('Rejected', 'Cancelled')`}
}

// salesGSTRate is the item's own GST rate (0 when it is not taxable).
const salesGSTRate = `(CASE WHEN lower(COALESCE(NULLIF(it.data->>'tax_treatment', ''), 'taxable')) = 'taxable' AND COALESCE(it.data->>'gst_rate', '') ~ ` + salesNumberRE + `
	THEN (it.data->>'gst_rate')::numeric ELSE 0 END)`

func init() {
	RegisterDashboardDataset(DashboardDataset{
		Key: "all_sales", Label: "All sales (every channel)", About: "Every sold and returned line of every channel - store bills, online orders and B2B invoices - with returns as minus lines.",
		Doctype: "POSCart", List: "pos", DateField: "date",
		from: `{parts} s` + dsItemJoinFor(`s.sku`) + dsLocationJoin("lo", `s.store_code`) +
			` LEFT JOIN %[1]s.documents sm ON sm.doctype = 'Salesman' AND sm.id = s.salesman_id` +
			` LEFT JOIN %[1]s.documents cu ON cu.doctype = 'Customer' AND cu.id = s.customer`,
		where:  `TRUE`,
		stores: []string{`s.store_code`},
		parts: []datasetPart{
			named(salesStorePart), named(salesOnlinePart), named(salesB2BPart),
			named(salesReturnPart("POSCart", `'Store'::text`,
				`CASE WHEN `+salesTillB2B("od")+` THEN 'Till B2B bill' ELSE 'Store bill' END`,
				`CASE WHEN `+salesTillB2B("od")+` THEN 'Business' WHEN COALESCE(od.data->>'customer_id', '') <> '' THEN 'Known customer' ELSE 'Walk-in' END`,
				`NULLIF(od.data->>'customer_id', '')`)),
			named(salesReturnPart("SalesOrder", `'Online'::text`, `COALESCE(NULLIF(od.data->>'channel', ''), 'Manual')`, `'Online buyer'::text`,
				`NULLIF(od.data->>'customer_name', '')`)),
			named(salesReturnPart("SalesInvoice",
				`CASE WHEN `+salesDirectInvoice("od")+` THEN 'B2B' ELSE 'Online' END`,
				`CASE WHEN `+salesDirectInvoice("od")+` THEN 'Invoice' ELSE 'Order invoice' END`,
				`CASE WHEN `+salesDirectInvoice("od")+` THEN 'Business' ELSE 'Online buyer' END`,
				`NULLIF(od.data->>'customer', '')`)),
		},
		Fields: []DatasetField{
			dsF("date", "Date", "date", `s.sold_at`),
			dsF("channel", "Channel", "category", `s.channel`),
			dsF("sub_channel", "Sub-channel", "category", `COALESCE(NULLIF(s.sub_channel, ''), '(none)')`),
			dsF("customer_type", "Customer type", "category", `s.customer_type`),
			dsF("store", "Store", "category", dsStoreName("lo", `s.store_code`)),
			dsF("salesman", "Salesman", "category", `COALESCE(NULLIF(sm.data->>'name', ''), NULLIF(s.salesman_id, ''), '(none)')`),
			dsF("customer", "Customer", "category", `COALESCE(NULLIF(cu.data->>'name', ''), NULLIF(s.customer, ''), '(none)')`),
			dsF("item", "Item", "category", fmt.Sprintf(dsItemName, `s.sku`)),
			dsF("sku", "Combination ID", "category", `s.sku`),
			dsF("category", "Category", "category", dsCategoryName),
			dsF("brand", "Brand", "category", dsBrand),
			dsF("design", "Design", "category", dsDesign),
			dsF("hour", "Hour of day", "category", `to_char(s.sold_at, 'HH24')`),
			dsF("weekday", "Day of week", "category", `to_char(s.sold_at, 'ID Dy')`),
			dsF("kind", "Sale or return", "category", `s.kind`),
			dsF("bill", "Bill / order", "category", `CASE WHEN s.kind = 'Sale' THEN s.doc_id END`),
			dsF("qty", "Qty sold", "number", `CASE WHEN s.kind = 'Sale' THEN s.qty ELSE 0 END`),
			dsF("returned_qty", "Qty returned", "number", `CASE WHEN s.kind = 'Return' THEN -s.qty ELSE 0 END`),
			dsF("net_qty", "Net qty", "number", `s.qty`),
			dsMoney("gross", "Sales", `CASE WHEN s.kind = 'Sale' THEN s.value ELSE 0 END`),
			dsMoney("returned", "Returns", `CASE WHEN s.kind = 'Return' THEN -s.value ELSE 0 END`),
			dsMoney("value", "Net sales", `s.value`),
			dsMoney("cost", "Cost", `s.cost`),
			dsMoney("costed_value", "Sales with a cost (ex GST)", `CASE WHEN s.cost IS NOT NULL THEN s.value / (1 + `+salesGSTRate+` / 100) END`),
			dsMoney("margin", "Margin (ex GST)", `CASE WHEN s.cost IS NOT NULL THEN s.value / (1 + `+salesGSTRate+` / 100) - s.cost END`),
		},
	})
}

// named gives a part's SQL its column names (the first SELECT of a UNION
// names the columns; naming every part keeps each one readable alone).
func named(p datasetPart) datasetPart {
	p.sql = `SELECT * FROM (` + p.sql + `) x(` + salesPartColumns + `)`
	return p
}

// --- ready-made sales dashboards (298.2) -------------------------------------

var (
	saleNet       = dsSum("value")
	saleBills     = DatasetMeasure{Field: "bill", Agg: "distinct"}
	saleAvgBill   = DatasetMeasure{Field: "gross", Agg: "per", Of: "bill"}
	saleUnits     = dsSum("qty")
	saleUnitsBill = DatasetMeasure{Field: "qty", Agg: "per", Of: "bill"}
	saleReturns   = dsSum("returned")
	saleReturnPct = DatasetMeasure{Field: "returned", Agg: "pct", Of: "gross"}
	saleMarginPct = DatasetMeasure{Field: "margin", Agg: "pct", Of: "costed_value"}
)

// salesQ is a query of all_sales, for one channel when channel is set.
func salesQ(channel string, q DatasetQuery) DatasetQuery {
	q.Dataset = "all_sales"
	if channel != "" {
		q.Filters = append([]DatasetFilter{{Field: "channel", Op: "eq", Value: channel}}, q.Filters...)
	}
	return q
}

func salesMeasures(m ...DatasetMeasure) []DatasetMeasure { return m }

// salesReadyMades are All sales, Store sales (key "sales", the Sales
// dashboard it replaces), Online sales and B2B sales.
func salesReadyMades(mk func(key, name string, f DashboardFilters, widgets ...DashboardWidget) CustomDashboard) []CustomDashboard {
	mtd := DashboardFilters{Range: "mtd"}
	tile := func(ch, title string, x, y, w int, m DatasetMeasure) DashboardWidget {
		return dsW(title, "number", x, y, w, 1, salesQ(ch, DatasetQuery{Measures: salesMeasures(m), Compare: true}))
	}
	by := func(ch, title, kind string, x, y, w int, group []string, limit int, m ...DatasetMeasure) DashboardWidget {
		q := salesQ(ch, DatasetQuery{GroupBy: group, Measures: m, Limit: limit})
		if len(group) > 0 && (group[0] == "hour" || group[0] == "weekday") {
			q.Sort = "group"
		}
		return dsW(title, kind, x, y, w, 2, q)
	}
	byDay := func(ch, title, kind string, x, y, w int, group ...string) DashboardWidget {
		return dsW(title, kind, x, y, w, 2, salesQ(ch, DatasetQuery{GroupBy: append([]string{"date"}, group...), Grain: "day", Measures: salesMeasures(saleNet), Limit: 400}))
	}
	return []CustomDashboard{
		mk("all_sales", "All sales", mtd,
			tile("", "Net sales", 0, 0, 3, saleNet),
			tile("", "Bills and orders", 3, 0, 3, saleBills),
			tile("", "Average bill", 6, 0, 3, saleAvgBill),
			tile("", "Units sold", 9, 0, 3, saleUnits),
			tile("", "Returns", 0, 1, 3, saleReturns),
			tile("", "Returns %", 3, 1, 3, saleReturnPct),
			tile("", "Margin % (store)", 6, 1, 3, saleMarginPct),
			tile("", "Units per bill", 9, 1, 3, saleUnitsBill),
			byDay("", "Net sales by day and channel", "stacked", 0, 2, 8, "channel"),
			by("", "Net sales by channel", "donut", 8, 2, 4, []string{"channel"}, 6, saleNet),
			by("", "Net sales by store", "bar", 0, 4, 4, []string{"store"}, 12, saleNet),
			by("", "Net sales by category", "donut", 4, 4, 4, []string{"category"}, 8, saleNet),
			by("", "Net sales by sub-channel", "bar", 8, 4, 4, []string{"sub_channel"}, 12, saleNet),
			by("", "Top items", "table", 0, 6, 6, []string{"item"}, 15, saleUnits, saleNet, saleReturns),
			by("", "Brand by channel", "pivot", 6, 6, 6, []string{"brand", "channel"}, 200, saleNet),
		),
		mk("sales", "Store sales", mtd,
			tile("Store", "Net sales", 0, 0, 3, saleNet),
			tile("Store", "Bills", 3, 0, 3, saleBills),
			tile("Store", "Average bill", 6, 0, 3, saleAvgBill),
			tile("Store", "Units per bill", 9, 0, 3, saleUnitsBill),
			tile("Store", "Units sold", 0, 1, 3, saleUnits),
			tile("Store", "Returns", 3, 1, 3, saleReturns),
			tile("Store", "Returns %", 6, 1, 3, saleReturnPct),
			tile("Store", "Margin %", 9, 1, 3, saleMarginPct),
			byDay("Store", "Net sales by day", "line", 0, 2, 8),
			by("Store", "Net sales by store", "bar", 8, 2, 4, []string{"store"}, 12, saleNet),
			by("Store", "Net sales by category", "donut", 0, 4, 4, []string{"category"}, 8, saleNet),
			by("Store", "Top salesmen", "bar", 4, 4, 4, []string{"salesman"}, 10, saleNet, saleBills),
			by("Store", "Net sales by hour", "bar", 8, 4, 4, []string{"hour"}, 24, saleNet),
			by("Store", "Top items", "table", 0, 6, 6, []string{"item"}, 15, saleUnits, saleNet, saleReturns),
			by("Store", "Brand by store", "pivot", 6, 6, 6, []string{"brand", "store"}, 200, saleNet),
			by("Store", "Walk-in and known customers", "donut", 0, 8, 4, []string{"customer_type"}, 6, saleNet),
			by("Store", "Margin % by category", "bar", 4, 8, 8, []string{"category"}, 12, saleMarginPct),
		),
		mk("online_sales", "Online sales", mtd,
			tile("Online", "Net sales", 0, 0, 3, saleNet),
			tile("Online", "Orders", 3, 0, 3, saleBills),
			tile("Online", "Average order", 6, 0, 3, saleAvgBill),
			tile("Online", "Units sold", 9, 0, 3, saleUnits),
			tile("Online", "Returns", 0, 1, 6, saleReturns),
			tile("Online", "Returns %", 6, 1, 6, saleReturnPct),
			byDay("Online", "Net sales by day and marketplace", "stacked", 0, 2, 8, "sub_channel"),
			by("Online", "Net sales by marketplace", "donut", 8, 2, 4, []string{"sub_channel"}, 8, saleNet),
			by("Online", "Returns % by marketplace", "bar", 0, 4, 4, []string{"sub_channel"}, 8, saleReturnPct),
			by("Online", "Net sales by category", "donut", 4, 4, 4, []string{"category"}, 8, saleNet),
			by("Online", "Shipped from (store)", "bar", 8, 4, 4, []string{"store"}, 12, saleNet),
			by("Online", "Top items", "table", 0, 6, 6, []string{"item"}, 15, saleUnits, saleNet, saleReturns),
			by("Online", "Category by marketplace", "pivot", 6, 6, 6, []string{"category", "sub_channel"}, 200, saleNet),
		),
		mk("b2b_sales", "B2B sales", mtd,
			tile("B2B", "Net sales", 0, 0, 3, saleNet),
			tile("B2B", "Invoices", 3, 0, 3, saleBills),
			tile("B2B", "Average invoice", 6, 0, 3, saleAvgBill),
			tile("B2B", "Units sold", 9, 0, 3, saleUnits),
			tile("B2B", "Returns", 0, 1, 6, saleReturns),
			tile("B2B", "Returns %", 6, 1, 6, saleReturnPct),
			byDay("B2B", "Net sales by day", "line", 0, 2, 8),
			by("B2B", "Top customers", "bar", 8, 2, 4, []string{"customer"}, 10, saleNet),
			by("B2B", "Net sales by category", "donut", 0, 4, 4, []string{"category"}, 8, saleNet),
			by("B2B", "Top items", "table", 4, 4, 8, []string{"item"}, 15, saleUnits, saleNet, saleReturns),
			by("B2B", "Customers by store", "pivot", 0, 6, 12, []string{"customer", "store"}, 200, saleNet),
		),
	}
}
