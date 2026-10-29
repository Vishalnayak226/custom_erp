package engines

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"custom_erp/db"

	"github.com/lib/pq"
)

// Stage 100.1 (user, 2026-10-19: "Data Custom Dasboard"; D1-D5 approved as
// recommended 2026-10-21): the data behind custom dashboards.
//
// D1: a widget reads a registered report or a dataset from this fixed list -
// sales lines, POS bills, stock on hand, stock movements, purchase orders,
// goods receipts, transfers, customers, loyalty, online orders, expenses,
// ledger. A query names fields, filters, a group-by, totals, a sort, a row
// limit and a time grain; the SQL is built from this registry only (every
// identifier and expression below; every value a parameter), never from a
// typed query - another client's data and the database's safety are why a
// SQL box was rejected.
// D4: each viewer sees their own stores' rows (reportStoreScope, the reports'
// rule) and money only if their role may (reportFullVisibilityRoles), so one
// dashboard serves every store manager.
// D5: a result is kept 60 seconds, so 100 stores opening one dashboard do not
// each run it.

// DatasetField is one field a widget may group by, filter on or total.
type DatasetField struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	// Type is "category" (group / filter by it), "number" (total it) or
	// "date" (filter by a range, group by day / week / month).
	Type  string `json:"type"`
	Money bool   `json:"money,omitempty"`
	expr  string
}

// DashboardDataset is one dataset of the fixed list.
type DashboardDataset struct {
	Key    string         `json:"key"`
	Label  string         `json:"label"`
	About  string         `json:"about"`
	Fields []DatasetField `json:"fields"`
	// DateField is the field date filters and the dashboard's date range
	// apply to ("" = a snapshot, such as stock on hand).
	DateField string `json:"date_field,omitempty"`
	// Doctype is the record type a viewer must be allowed to read, and what
	// a click through opens (with DocField's value as the record id).
	Doctype  string `json:"doctype"`
	DocField string `json:"doc_field,omitempty"`
	// List is the screen a click through falls back to.
	List  string `json:"list,omitempty"`
	from  string // %[1]s is the client's schema
	where string
	// stores are the expressions carrying the row's store code: a row is the
	// viewer's when any of them is one of their stores.
	stores []string
}

func dsF(key, label, typ, expr string) DatasetField {
	return DatasetField{Key: key, Label: label, Type: typ, expr: expr}
}

func dsMoney(key, label, expr string) DatasetField {
	return DatasetField{Key: key, Label: label, Type: "number", Money: true, expr: expr}
}

const (
	dsItemJoin     = ` LEFT JOIN %[1]s.documents it ON it.doctype = 'Item' AND it.id = %[2]s LEFT JOIN %[1]s.documents ca ON ca.doctype = 'Category' AND ca.id = it.data->>'category'`
	dsItemName     = `COALESCE(NULLIF(it.data->>'name', ''), %s)`
	dsCategoryName = `COALESCE(NULLIF(ca.data->>'name', ''), NULLIF(it.data->>'category', ''), '(none)')`
	dsBrand        = `COALESCE(NULLIF(it.data->>'brand', ''), '(none)')`
	dsDesign       = `COALESCE(NULLIF(it.data->>'parent_product_code', ''), NULLIF(it.data->>'family', ''), '(none)')`
	dsPOSItems     = `CASE WHEN jsonb_typeof(d.data->'items') = 'array' THEN d.data->'items' WHEN COALESCE(d.data->>'items', '') ~ '^\s*\[' THEN (d.data->>'items')::jsonb ELSE '[]'::jsonb END`
)

func dsStoreName(alias, codeExpr string) string {
	return fmt.Sprintf(`COALESCE(NULLIF(%s.data->>'name', ''), NULLIF(%s, ''), '(none)')`, alias, codeExpr)
}

func dsLocationJoin(alias, codeExpr string) string {
	return fmt.Sprintf(` LEFT JOIN %%[1]s.documents %s ON %s.doctype = 'Location' AND %s.id = %s`, alias, alias, alias, codeExpr)
}

func dsNum(expr string) string {
	return fmt.Sprintf(`COALESCE(NULLIF(%s, '')::numeric, 0)`, expr)
}

func dsItemJoinFor(skuExpr string) string {
	return strings.ReplaceAll(dsItemJoin, "%[2]s", skuExpr)
}

var dashboardDatasets = []DashboardDataset{
	{
		Key: "sales_lines", Label: "Sales lines (POS)", About: "Every line of every paid POS bill: what sold, where, by whom, for how much.",
		Doctype: "POSCart", DocField: "bill_id", List: "pos", DateField: "date",
		from: `%[1]s.documents d CROSS JOIN LATERAL jsonb_array_elements(` + dsPOSItems + `) l` +
			dsItemJoinFor(`l->>'sku'`) + dsLocationJoin("lo", `d.data->>'location'`) +
			` LEFT JOIN %[1]s.documents sm ON sm.doctype = 'Salesman' AND sm.id = COALESCE(NULLIF(l->>'salesman_id', ''), d.data->>'salesman_id')`,
		where:  `d.doctype = 'POSCart' AND d.status = 'Paid' AND d.deleted_at IS NULL`,
		stores: []string{`d.data->>'location'`},
		Fields: []DatasetField{
			dsF("date", "Date", "date", `d.created_at`),
			dsF("store", "Store", "category", dsStoreName("lo", `d.data->>'location'`)),
			dsF("item", "Item", "category", fmt.Sprintf(dsItemName, `l->>'sku'`)),
			dsF("sku", "Combination ID", "category", `l->>'sku'`),
			dsF("category", "Category", "category", dsCategoryName),
			dsF("brand", "Brand", "category", dsBrand),
			dsF("design", "Design", "category", dsDesign),
			dsF("salesman", "Salesman", "category", `COALESCE(NULLIF(sm.data->>'name', ''), NULLIF(COALESCE(NULLIF(l->>'salesman_id', ''), d.data->>'salesman_id'), ''), '(none)')`),
			dsF("hour", "Hour of day", "category", `to_char(d.created_at, 'HH24')`),
			dsF("weekday", "Day of week", "category", `to_char(d.created_at, 'ID Dy')`),
			dsF("bill_id", "Bill", "category", `d.id`),
			dsF("qty", "Qty sold", "number", dsNum(`l->>'qty'`)),
			dsMoney("value", "Sales value", dsNum(`l->>'qty'`)+` * `+dsNum(`l->>'sale_price'`)),
		},
	},
	{
		Key: "pos_bills", Label: "POS bills", About: "Each paid bill: store, salesman, payment, customer and value.",
		Doctype: "POSCart", DocField: "bill_id", List: "pos", DateField: "date",
		from: `%[1]s.documents d` + dsLocationJoin("lo", `d.data->>'location'`) +
			` LEFT JOIN %[1]s.documents sm ON sm.doctype = 'Salesman' AND sm.id = d.data->>'salesman_id'`,
		where:  `d.doctype = 'POSCart' AND d.status = 'Paid' AND d.deleted_at IS NULL`,
		stores: []string{`d.data->>'location'`},
		Fields: []DatasetField{
			dsF("date", "Date", "date", `d.created_at`),
			dsF("store", "Store", "category", dsStoreName("lo", `d.data->>'location'`)),
			dsF("salesman", "Salesman", "category", `COALESCE(NULLIF(sm.data->>'name', ''), NULLIF(d.data->>'salesman_id', ''), '(none)')`),
			dsF("payment_mode", "Payment", "category", `COALESCE(NULLIF(d.data->>'payment_mode', ''), '(none)')`),
			dsF("customer_type", "New or repeat", "category", `CASE WHEN COALESCE(d.data->>'customer_id', '') = '' THEN 'Walk-in'
				WHEN NOT EXISTS (SELECT 1 FROM %[1]s.documents p WHERE p.doctype = 'POSCart' AND p.status = 'Paid' AND p.deleted_at IS NULL
					AND p.data->>'customer_id' = d.data->>'customer_id' AND p.created_at < d.created_at) THEN 'New' ELSE 'Repeat' END`),
			dsF("hour", "Hour of day", "category", `to_char(d.created_at, 'HH24')`),
			dsF("weekday", "Day of week", "category", `to_char(d.created_at, 'ID Dy')`),
			dsF("bill_id", "Bill", "category", `d.id`),
			dsF("items", "Items on the bill", "number", `jsonb_array_length(`+dsPOSItems+`)`),
			dsMoney("value", "Bill value", dsNum(`d.data->>'amount_paid'`)),
		},
	},
	{
		Key: "stock_on_hand", Label: "Stock on hand", About: "What each store holds now, item by item, and what it is worth.",
		Doctype: "Item", List: "inventory",
		from:   `%[1]s.inventory_availability s` + dsItemJoinFor(`s.sku`) + dsLocationJoin("lo", `s.location_code`),
		where:  `TRUE`,
		stores: []string{`s.location_code`},
		Fields: []DatasetField{
			dsF("store", "Store", "category", dsStoreName("lo", `s.location_code`)),
			dsF("item", "Item", "category", fmt.Sprintf(dsItemName, `s.sku`)),
			dsF("sku", "Combination ID", "category", `s.sku`),
			dsF("category", "Category", "category", dsCategoryName),
			dsF("brand", "Brand", "category", dsBrand),
			dsF("design", "Design", "category", dsDesign),
			dsF("stock_level", "Stock level", "category", `CASE WHEN s.available <= 0 THEN 'Out of stock' WHEN s.available <= GREATEST(COALESCE(s.safety_stock, 0), 0) THEN 'Low' ELSE 'In stock' END`),
			dsF("on_hand", "On hand", "number", `s.on_hand`),
			dsF("available", "Available", "number", `s.available`),
			dsF("damaged", "Damaged", "number", `COALESCE(s.damaged, 0)`),
			dsF("in_transit", "In transit", "number", `COALESCE(s.in_transit, 0)`),
			dsF("days_still", "Days since it last moved", "number", `GREATEST(date_part('day', LOCALTIMESTAMP - s.updated_at), 0)`),
			dsMoney("value", "Stock value (cost)", `s.on_hand * `+dsNum(`it.data->>'standard_cost'`)),
			dsMoney("mrp_value", "Stock value (MRP)", `s.on_hand * `+dsNum(`it.data->>'mrp'`)),
		},
	},
	{
		Key: "stock_moves", Label: "Stock movements", About: "Every stock ledger entry: receipts, sales, transfers, adjustments.",
		Doctype: "StockLedgerEntry", DocField: "voucher", DateField: "date",
		from:   `%[1]s.documents d` + dsItemJoinFor(`d.data->>'item_id'`) + dsLocationJoin("lo", `d.data->>'warehouse_id'`),
		where:  `d.doctype = 'StockLedgerEntry' AND d.deleted_at IS NULL`,
		stores: []string{`d.data->>'warehouse_id'`},
		Fields: []DatasetField{
			dsF("date", "Date", "date", `d.created_at`),
			dsF("store", "Store", "category", dsStoreName("lo", `d.data->>'warehouse_id'`)),
			dsF("item", "Item", "category", fmt.Sprintf(dsItemName, `d.data->>'item_id'`)),
			dsF("category", "Category", "category", dsCategoryName),
			dsF("voucher_type", "Movement", "category", `COALESCE(NULLIF(d.data->>'voucher_type', ''), '(none)')`),
			dsF("direction", "In or out", "category", `CASE WHEN `+dsNum(`d.data->>'qty'`)+` >= 0 THEN 'In' ELSE 'Out' END`),
			dsF("voucher", "Document", "category", `COALESCE(NULLIF(d.data->>'voucher_id', ''), d.id)`),
			dsF("qty_in", "Qty in", "number", `GREATEST(`+dsNum(`d.data->>'qty'`)+`, 0)`),
			dsF("qty_out", "Qty out", "number", `GREATEST(-`+dsNum(`d.data->>'qty'`)+`, 0)`),
			dsF("qty", "Net qty", "number", dsNum(`d.data->>'qty'`)),
		},
	},
	{
		Key: "purchase_orders", Label: "Purchase orders", About: "Orders to vendors: who, for which store, their status and value.",
		Doctype: "PurchaseOrder", DocField: "po_id", List: "purchase-orders", DateField: "date",
		from: `%[1]s.documents d` + dsLocationJoin("lo", `d.data->>'location'`) +
			` LEFT JOIN %[1]s.documents ve ON ve.doctype = 'Vendor' AND ve.id = COALESCE(NULLIF(d.data->>'vendor_id', ''), d.data->>'vendor')`,
		where:  `d.doctype = 'PurchaseOrder' AND d.deleted_at IS NULL`,
		stores: []string{`d.data->>'location'`, `d.data->>'target_warehouse'`},
		Fields: []DatasetField{
			dsF("date", "Date", "date", `d.created_at`),
			dsF("store", "Billing store", "category", dsStoreName("lo", `d.data->>'location'`)),
			dsF("vendor", "Vendor", "category", `COALESCE(NULLIF(ve.data->>'name', ''), NULLIF(d.data->>'vendor', ''), '(none)')`),
			dsF("status", "Status", "category", `COALESCE(NULLIF(d.status, ''), '(none)')`),
			dsF("po_id", "Purchase order", "category", `d.id`),
			dsMoney("value", "Order value", `COALESCE(NULLIF(d.data->>'grand_total', '')::numeric, `+dsNum(`d.data->>'total_amount'`)+`)`),
		},
	},
	{
		Key: "goods_receipts", Label: "Goods receipts", About: "Receipts from vendors: store, vendor, step and invoice value.",
		Doctype: "GRN", DocField: "grn_id", List: "grn", DateField: "date",
		from: `%[1]s.documents d` + dsLocationJoin("lo", `d.data->>'location'`) +
			` LEFT JOIN %[1]s.documents ve ON ve.doctype = 'Vendor' AND ve.id = d.data->>'vendor'`,
		where:  `d.doctype = 'GRN' AND d.deleted_at IS NULL`,
		stores: []string{`d.data->>'location'`},
		Fields: []DatasetField{
			dsF("date", "Date", "date", `d.created_at`),
			dsF("store", "Store", "category", dsStoreName("lo", `d.data->>'location'`)),
			dsF("vendor", "Vendor", "category", `COALESCE(NULLIF(ve.data->>'name', ''), NULLIF(d.data->>'vendor', ''), '(none)')`),
			dsF("step", "Step", "category", `COALESCE(NULLIF(d.data->>'grn_step', ''), NULLIF(d.status, ''), '(none)')`),
			dsF("grn_id", "Goods receipt", "category", `d.id`),
			dsMoney("value", "Invoice value", dsNum(`d.data->>'vendor_invoice_amount'`)),
		},
	},
	{
		Key: "transfers", Label: "Transfers", About: "Stock sent between stores: from, to, status and value.",
		Doctype: "TransferOrder", DocField: "to_id", List: "transfers", DateField: "date",
		from:   `%[1]s.documents d` + dsLocationJoin("lf", `d.data->>'from_warehouse'`) + dsLocationJoin("lt", `d.data->>'to_warehouse'`),
		where:  `d.doctype = 'TransferOrder' AND d.deleted_at IS NULL`,
		stores: []string{`d.data->>'from_warehouse'`, `d.data->>'to_warehouse'`},
		Fields: []DatasetField{
			dsF("date", "Date", "date", `d.created_at`),
			dsF("from_store", "From store", "category", dsStoreName("lf", `d.data->>'from_warehouse'`)),
			dsF("to_store", "To store", "category", dsStoreName("lt", `d.data->>'to_warehouse'`)),
			dsF("status", "Status", "category", `COALESCE(NULLIF(d.status, ''), '(none)')`),
			dsF("to_id", "Transfer", "category", `d.id`),
			dsMoney("value", "Value at cost", dsNum(`d.data->>'total_amount'`)),
		},
	},
	{
		Key: "customers", Label: "Customers", About: "Customers by when they joined, their home store, tier and place.",
		Doctype: "Customer", DocField: "customer_id", DateField: "date",
		from:   `%[1]s.documents d` + dsLocationJoin("lo", `d.data->>'walk_in_store'`),
		where:  `d.doctype = 'Customer' AND d.deleted_at IS NULL AND COALESCE(d.data->>'merged_into', '') = ''`,
		stores: []string{`d.data->>'walk_in_store'`},
		Fields: []DatasetField{
			dsF("date", "Joined", "date", `d.created_at`),
			dsF("store", "Home store", "category", dsStoreName("lo", `d.data->>'walk_in_store'`)),
			dsF("tier", "Loyalty tier", "category", `COALESCE(NULLIF(d.data->>'loyalty_tier', ''), '(none)')`),
			dsF("gender", "Gender", "category", `COALESCE(NULLIF(d.data->>'gender', ''), '(none)')`),
			dsF("city", "City", "category", `COALESCE(NULLIF(d.data->>'city', ''), '(none)')`),
			dsF("state", "State", "category", `COALESCE(NULLIF(d.data->>'state', ''), '(none)')`),
			dsF("customer_id", "Customer", "category", `d.id`),
		},
	},
	{
		Key: "loyalty", Label: "Loyalty points", About: "Points earned, redeemed and expired, by store and tier.",
		Doctype: "Customer", DateField: "date",
		from: `%[1]s.loyalty_point_ledger lp LEFT JOIN %[1]s.documents cu ON cu.doctype = 'Customer' AND cu.id = lp.customer_id
			LEFT JOIN %[1]s.documents rc ON rc.doctype = 'POSCart' AND rc.id = lp.reference_id` + dsLocationJoin("lo", `rc.data->>'location'`),
		where:  `lp.cancelled_at IS NULL`,
		stores: []string{`rc.data->>'location'`},
		Fields: []DatasetField{
			dsF("date", "Date", "date", `lp.created_at`),
			dsF("store", "Store", "category", dsStoreName("lo", `rc.data->>'location'`)),
			dsF("type", "Earned or used", "category", `COALESCE(NULLIF(lp.transaction_type, ''), '(none)')`),
			dsF("tier", "Loyalty tier", "category", `COALESCE(NULLIF(cu.data->>'loyalty_tier', ''), '(none)')`),
			dsF("customer", "Customer", "category", `COALESCE(NULLIF(cu.data->>'name', ''), lp.customer_id)`),
			dsF("points", "Points", "number", `lp.points`),
		},
	},
	{
		Key: "online_orders", Label: "Online orders", About: "Marketplace and web order lines: channel, status, store and value.",
		Doctype: "SalesOrder", DocField: "order_id", List: "oms", DateField: "date",
		from: `%[1]s.documents ln JOIN %[1]s.documents so ON so.doctype = 'SalesOrder' AND so.id = ln.data->>'order_id' AND so.deleted_at IS NULL` +
			dsItemJoinFor(`ln.data->>'sku'`) + dsLocationJoin("lo", `ln.data->>'location_code'`),
		where:  `ln.doctype = 'SalesOrderLine' AND ln.deleted_at IS NULL`,
		stores: []string{`ln.data->>'location_code'`},
		Fields: []DatasetField{
			dsF("date", "Date", "date", `so.created_at`),
			dsF("channel", "Channel", "category", `COALESCE(NULLIF(so.data->>'channel', ''), '(none)')`),
			dsF("status", "Order status", "category", `COALESCE(NULLIF(so.data->>'order_status', ''), NULLIF(so.status, ''), '(none)')`),
			dsF("payment", "Payment", "category", `COALESCE(NULLIF(so.data->>'payment_status', ''), '(none)')`),
			dsF("state", "Ship-to state", "category", `COALESCE(NULLIF(so.data->>'shipping_state', ''), '(none)')`),
			dsF("store", "Store", "category", dsStoreName("lo", `ln.data->>'location_code'`)),
			dsF("item", "Item", "category", fmt.Sprintf(dsItemName, `ln.data->>'sku'`)),
			dsF("category", "Category", "category", dsCategoryName),
			dsF("order_id", "Order", "category", `so.id`),
			dsF("qty", "Qty", "number", dsNum(`ln.data->>'qty'`)),
			dsMoney("value", "Line value", dsNum(`ln.data->>'qty'`)+` * `+dsNum(`ln.data->>'unit_price'`)),
		},
	},
	{
		Key: "expenses", Label: "Expenses", About: "Expense claims by store, category and department.",
		Doctype: "ExpenseClaim", DocField: "claim_id", DateField: "date",
		from:   `%[1]s.documents d` + dsLocationJoin("lo", `d.data->>'location'`),
		where:  `d.doctype = 'ExpenseClaim' AND d.deleted_at IS NULL`,
		stores: []string{`d.data->>'location'`},
		Fields: []DatasetField{
			dsF("date", "Expense date", "date", `CASE WHEN COALESCE(d.data->>'expense_date', '') ~ '^\d{4}-\d{2}-\d{2}' THEN (substr(d.data->>'expense_date', 1, 10))::timestamp ELSE d.created_at END`),
			dsF("store", "Store", "category", dsStoreName("lo", `d.data->>'location'`)),
			dsF("category", "Category", "category", `COALESCE(NULLIF(d.data->>'category', ''), '(none)')`),
			dsF("department", "Department", "category", `COALESCE(NULLIF(d.data->>'department', ''), '(none)')`),
			dsF("status", "Status", "category", `COALESCE(NULLIF(d.status, ''), '(none)')`),
			dsF("claim_id", "Claim", "category", `d.id`),
			dsMoney("amount", "Amount", dsNum(`d.data->>'amount'`)),
			dsMoney("gst", "GST", dsNum(`d.data->>'gst_amount'`)),
		},
	},
	{
		Key: "ledger", Label: "Ledger postings", About: "Every posting to the books: account, type, cost centre, debit and credit.",
		Doctype: "JournalVoucher", DateField: "date",
		from: `%[1]s.gl_postings g LEFT JOIN %[1]s.gl_accounts a ON a.account_code = g.account_code
			LEFT JOIN LATERAL (SELECT x.id, x.data FROM %[1]s.documents x WHERE x.doctype = 'Location' AND x.deleted_at IS NULL
				AND COALESCE(g.cost_center, '') <> '' AND x.data->>'cost_center' = g.cost_center LIMIT 1) lo ON TRUE`,
		where:  `TRUE`,
		stores: []string{`lo.id`},
		Fields: []DatasetField{
			dsF("date", "Date", "date", `g.created_at`),
			dsF("account", "Account", "category", `g.account_code || COALESCE(' ' || NULLIF(a.account_name, ''), '')`),
			dsF("account_code", "Account code", "category", `g.account_code`),
			dsF("account_type", "Account type", "category", `COALESCE(NULLIF(a.account_type, ''), '(none)')`),
			dsF("cost_center", "Cost centre", "category", `COALESCE(NULLIF(g.cost_center, ''), '(none)')`),
			dsF("store", "Store", "category", dsStoreName("lo", `lo.id`)),
			dsF("department", "Department", "category", `COALESCE(NULLIF(g.department, ''), '(none)')`),
			dsF("document_type", "From", "category", `COALESCE(NULLIF(g.document_type, ''), '(none)')`),
			dsMoney("debit", "Debit", `g.debit / 100.0`),
			dsMoney("credit", "Credit", `g.credit / 100.0`),
			dsMoney("net", "Debit - credit", `(g.debit - g.credit) / 100.0`),
			dsMoney("credit_net", "Credit - debit", `(g.credit - g.debit) / 100.0`),
		},
	},
}

// RegisterDashboardDataset adds a dataset from another module's own file (an
// init() there, as Stage 101's CRM datasets do), built with dsF / dsMoney and
// the dsLocationJoin / dsStoreName helpers above. A duplicate key is a
// programmer error.
func RegisterDashboardDataset(d DashboardDataset) {
	if _, ok := DashboardDatasetByKey(d.Key); ok {
		panic("dashboard dataset " + d.Key + " already registered")
	}
	dashboardDatasets = append(dashboardDatasets, d)
}

// DashboardDatasetByKey is a dataset of the fixed list, or false.
func DashboardDatasetByKey(key string) (DashboardDataset, bool) {
	for _, d := range dashboardDatasets {
		if d.Key == key {
			return d, true
		}
	}
	return DashboardDataset{}, false
}

// ListDashboardDatasets is the list as a viewer may see it: datasets whose
// record type they may read (canRead, the caller's permission check), money
// fields marked hidden when their role may not see money.
func ListDashboardDatasets(role string, canRead func(doctype string) bool) []DashboardDataset {
	seesMoney := RoleSeesMoney(role)
	out := []DashboardDataset{}
	for _, d := range dashboardDatasets {
		if canRead != nil && !canRead(d.Doctype) {
			continue
		}
		c := d
		c.Fields = nil
		for _, fl := range d.Fields {
			if fl.Money && !seesMoney {
				continue
			}
			c.Fields = append(c.Fields, fl)
		}
		out = append(out, c)
	}
	return out
}

// RoleSeesMoney is the reports' money rule (reportFullVisibilityRoles).
func RoleSeesMoney(role string) bool {
	return reportFullVisibilityRoles[CanonicalRole(role)]
}

// DatasetMeasure is one total: sum / count / avg / min / max / distinct of
// a field ("" field with count = rows).
type DatasetMeasure struct {
	Field string `json:"field"`
	Agg   string `json:"agg"`
}

// DatasetFilter narrows rows: eq / ne / in (a comma list) / contains on a
// category; gte / lte on a number or date.
type DatasetFilter struct {
	Field string `json:"field"`
	Op    string `json:"op"`
	Value string `json:"value"`
}

// DatasetQuery is what a widget asks a dataset.
type DatasetQuery struct {
	Dataset  string           `json:"dataset"`
	GroupBy  []string         `json:"group_by,omitempty"`
	Grain    string           `json:"grain,omitempty"` // day / week / month, for a date in group_by
	Measures []DatasetMeasure `json:"measures"`
	Filters  []DatasetFilter  `json:"filters,omitempty"`
	DateFrom string           `json:"date_from,omitempty"` // YYYY-MM-DD, inclusive
	DateTo   string           `json:"date_to,omitempty"`
	Store    string           `json:"store,omitempty"` // the dashboard's store filter (a code)
	Sort     string           `json:"sort,omitempty"`  // "" / "desc" first measure high to low, "asc", "group"
	Limit    int              `json:"limit,omitempty"`
	// Compare asks for the same totals over the period just before
	// DateFrom-DateTo (the number widget's change).
	Compare bool `json:"compare,omitempty"`
}

// DatasetColumn is one column of a result.
type DatasetColumn struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Type  string `json:"type"`
	Money bool   `json:"money,omitempty"`
}

// DatasetResult is a query's answer.
type DatasetResult struct {
	Columns   []DatasetColumn          `json:"columns"`
	Rows      []map[string]interface{} `json:"rows"`
	Previous  map[string]interface{}   `json:"previous,omitempty"`
	Truncated bool                     `json:"truncated,omitempty"`
	CachedAt  time.Time                `json:"cached_at"`
	// Contract (Stage 47.14.3): what the figures mean, cover and rest on.
	Contract KPIContract `json:"contract"`
}

// DatasetViewer is who asks: the role (money) and user (stores).
type DatasetViewer struct {
	Role   string
	UserID string
}

const (
	datasetDefaultLimit = 50
	datasetMaxLimit     = 1000
	datasetCacheTTL     = 60 * time.Second
	datasetCacheMax     = 2000
)

var datasetAggs = map[string]string{"sum": "SUM", "count": "COUNT", "avg": "AVG", "min": "MIN", "max": "MAX", "distinct": "COUNT(DISTINCT"}

func datasetError(msg string) error {
	return &ValidationError{Code: "REPORT-0988", Message: msg}
}

func (d DashboardDataset) field(key string) (DatasetField, bool) {
	for _, fl := range d.Fields {
		if fl.Key == key {
			return fl, true
		}
	}
	return DatasetField{}, false
}

// MeasureKey is the result column a measure fills ("count" alone, else
// agg_field).
func MeasureKey(m DatasetMeasure) string {
	if m.Field == "" {
		return m.Agg
	}
	return m.Agg + "_" + m.Field
}

type datasetCacheEntry struct {
	at  time.Time
	res DatasetResult
}

var (
	datasetCacheMu sync.Mutex
	datasetCache   = map[string]datasetCacheEntry{}
)

// RunDatasetQuery answers q for v, cut to v's stores and money. Results are
// kept 60 seconds per (client, the viewer's store set, money rule, query).
func RunDatasetQuery(tenantID string, v DatasetViewer, q DatasetQuery) (DatasetResult, error) {
	ds, ok := DashboardDatasetByKey(q.Dataset)
	if !ok {
		return DatasetResult{}, datasetError(fmt.Sprintf("%q is not a dataset", q.Dataset))
	}
	access, limited := reportStoreScope(tenantID, v.Role, v.UserID)
	if q.Store != "" && limited && !access.Allows(q.Store) {
		return DatasetResult{}, &ValidationError{Code: "REPORT-0987", SubFor: "Store", Message: fmt.Sprintf("store %s is not one of the stores you may see", q.Store)}
	}
	seesMoney := RoleSeesMoney(v.Role)
	scopeKey := "all"
	if limited {
		scopeKey = strings.Join(access.List(), ",")
	}
	raw, _ := json.Marshal(q)
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%v|%s", tenantID, scopeKey, seesMoney, raw)))
	key := hex.EncodeToString(sum[:])
	datasetCacheMu.Lock()
	if e, ok := datasetCache[key]; ok && time.Since(e.at) < datasetCacheTTL {
		datasetCacheMu.Unlock()
		return e.res, nil
	}
	datasetCacheMu.Unlock()

	res, err := runDatasetQuery(tenantID, ds, q, limited, access, seesMoney)
	if err != nil {
		return res, err
	}
	if q.Compare && q.DateFrom != "" && q.DateTo != "" && len(q.GroupBy) == 0 {
		from, e1 := time.Parse("2006-01-02", q.DateFrom)
		to, e2 := time.Parse("2006-01-02", q.DateTo)
		if e1 == nil && e2 == nil && !to.Before(from) {
			days := int(to.Sub(from).Hours()/24) + 1
			pq := q
			pq.Compare = false
			pq.DateTo = from.AddDate(0, 0, -1).Format("2006-01-02")
			pq.DateFrom = from.AddDate(0, 0, -days).Format("2006-01-02")
			if prev, err := runDatasetQuery(tenantID, ds, pq, limited, access, seesMoney); err == nil && len(prev.Rows) > 0 {
				res.Previous = prev.Rows[0]
			}
		}
	}
	res.CachedAt = time.Now()
	var viewerStores []string
	if limited {
		viewerStores = access.List()
	}
	res.Contract = buildKPIContract(ds, q, limited, viewerStores, res.CachedAt)
	datasetCacheMu.Lock()
	if len(datasetCache) >= datasetCacheMax {
		datasetCache = map[string]datasetCacheEntry{}
	}
	datasetCache[key] = datasetCacheEntry{at: time.Now(), res: res}
	datasetCacheMu.Unlock()
	return res, nil
}

func runDatasetQuery(tenantID string, ds DashboardDataset, q DatasetQuery, limited bool, access StoreAccess, seesMoney bool) (DatasetResult, error) {
	res := DatasetResult{Rows: []map[string]interface{}{}}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return res, err
	}
	sch := pq.QuoteIdentifier(schema)
	expr := func(e string) string { return strings.ReplaceAll(e, "%[1]s", sch) }
	var args []interface{}
	param := func(v interface{}) string { args = append(args, v); return fmt.Sprintf("$%d", len(args)) }

	if len(q.GroupBy) > 2 {
		return res, datasetError("group by at most two fields")
	}
	if len(q.Measures) == 0 {
		q.Measures = []DatasetMeasure{{Agg: "count"}}
	}
	if len(q.Measures) > 6 {
		return res, datasetError("at most six totals")
	}
	var selects, groups []string
	for i, g := range q.GroupBy {
		fl, ok := ds.field(g)
		if !ok || fl.Type == "number" {
			return res, datasetError(fmt.Sprintf("%s cannot be grouped by", g))
		}
		e := expr(fl.expr)
		col := DatasetColumn{Key: fl.Key, Label: fl.Label, Type: "category"}
		if fl.Type == "date" {
			grain := map[string]string{"day": "day", "week": "week", "month": "month"}[q.Grain]
			if grain == "" {
				grain = "day"
			}
			e = fmt.Sprintf("to_char(date_trunc('%s', %s), 'YYYY-MM-DD')", grain, e)
			col.Type = "date"
		}
		selects = append(selects, fmt.Sprintf("%s AS g%d", e, i))
		groups = append(groups, fmt.Sprintf("g%d", i))
		res.Columns = append(res.Columns, col)
	}
	for i, m := range q.Measures {
		agg, ok := datasetAggs[m.Agg]
		if !ok {
			return res, datasetError(fmt.Sprintf("%q is not a total (sum, count, avg, min, max, distinct)", m.Agg))
		}
		col := DatasetColumn{Key: MeasureKey(m), Type: "number"}
		var e string
		switch {
		case m.Field == "" && m.Agg == "count":
			e, col.Label = "COUNT(*)", "Count"
		case m.Field == "":
			return res, datasetError(m.Agg + " needs a field")
		default:
			fl, ok := ds.field(m.Field)
			if !ok {
				return res, datasetError(fmt.Sprintf("%s is not a field of %s", m.Field, ds.Label))
			}
			if fl.Type != "number" && m.Agg != "count" && m.Agg != "distinct" {
				return res, datasetError(fmt.Sprintf("%s can only be counted", fl.Label))
			}
			col.Label, col.Money = datasetMeasureLabel(m.Agg, fl.Label), fl.Money
			if fl.Money && !seesMoney {
				e = "NULL" // D4: money only for roles that may see it
				break
			}
			if m.Agg == "distinct" {
				e = fmt.Sprintf("COUNT(DISTINCT %s)", expr(fl.expr))
			} else {
				e = fmt.Sprintf("%s(%s)", agg, expr(fl.expr))
			}
		}
		selects = append(selects, fmt.Sprintf("%s AS m%d", e, i))
		res.Columns = append(res.Columns, col)
	}

	where := []string{expr(ds.where)}
	if limited {
		if len(ds.stores) == 0 {
			return res, datasetError(ds.Label + " has no store, so it shows for users of every store only")
		}
		list := param(pq.Array(access.List()))
		var any []string
		for _, s := range ds.stores {
			any = append(any, fmt.Sprintf("%s = ANY(%s::text[])", expr(s), list))
		}
		where = append(where, "("+strings.Join(any, " OR ")+")")
	}
	if q.Store != "" {
		if len(ds.stores) == 0 {
			return res, datasetError(ds.Label + " cannot be filtered by store")
		}
		p := param(q.Store)
		var any []string
		for _, s := range ds.stores {
			any = append(any, fmt.Sprintf("%s = %s", expr(s), p))
		}
		where = append(where, "("+strings.Join(any, " OR ")+")")
	}
	if q.DateFrom != "" || q.DateTo != "" {
		fl, ok := ds.field(ds.DateField)
		if ok {
			if q.DateFrom != "" {
				if _, err := time.Parse("2006-01-02", q.DateFrom); err != nil {
					return res, datasetError("date_from must be YYYY-MM-DD")
				}
				where = append(where, fmt.Sprintf("%s >= %s::date", expr(fl.expr), param(q.DateFrom)))
			}
			if q.DateTo != "" {
				if _, err := time.Parse("2006-01-02", q.DateTo); err != nil {
					return res, datasetError("date_to must be YYYY-MM-DD")
				}
				where = append(where, fmt.Sprintf("%s < (%s::date + 1)", expr(fl.expr), param(q.DateTo)))
			}
		}
	}
	for _, fi := range q.Filters {
		fl, ok := ds.field(fi.Field)
		if !ok {
			return res, datasetError(fmt.Sprintf("%s is not a field of %s", fi.Field, ds.Label))
		}
		if fl.Money && !seesMoney {
			return res, datasetError(fl.Label + " is not shown to your role")
		}
		e := expr(fl.expr)
		switch fi.Op {
		case "eq", "":
			where = append(where, fmt.Sprintf("(%s)::text = %s", e, param(fi.Value)))
		case "ne":
			where = append(where, fmt.Sprintf("(%s)::text <> %s", e, param(fi.Value)))
		case "in":
			vals := []string{}
			for _, s := range strings.Split(fi.Value, ",") {
				if s = strings.TrimSpace(s); s != "" {
					vals = append(vals, s)
				}
			}
			where = append(where, fmt.Sprintf("(%s)::text = ANY(%s::text[])", e, param(pq.Array(vals))))
		case "contains":
			where = append(where, fmt.Sprintf("(%s)::text ILIKE %s", e, param("%"+strings.NewReplacer(`%`, `\%`, `_`, `\_`).Replace(fi.Value)+"%")))
		case "gte", "lte":
			cmp := map[string]string{"gte": ">=", "lte": "<="}[fi.Op]
			switch fl.Type {
			case "number":
				where = append(where, fmt.Sprintf("(%s) %s %s::numeric", e, cmp, param(fi.Value)))
			case "date":
				where = append(where, fmt.Sprintf("(%s)::date %s %s::date", e, cmp, param(fi.Value)))
			default:
				return res, datasetError(fl.Label + " is not a number or a date")
			}
		default:
			return res, datasetError(fmt.Sprintf("%q is not a filter (eq, ne, in, contains, gte, lte)", fi.Op))
		}
	}

	limit := q.Limit
	if limit <= 0 {
		limit = datasetDefaultLimit
	}
	if limit > datasetMaxLimit {
		limit = datasetMaxLimit
	}
	order := ""
	if len(groups) > 0 {
		m0 := fmt.Sprintf("m0 DESC NULLS LAST, %s", strings.Join(groups, ", "))
		switch q.Sort {
		case "asc":
			m0 = fmt.Sprintf("m0 ASC NULLS LAST, %s", strings.Join(groups, ", "))
		case "group":
			m0 = strings.Join(groups, ", ")
		}
		if len(q.GroupBy) > 0 {
			if fl, _ := ds.field(q.GroupBy[0]); fl.Type == "date" && q.Sort == "" {
				m0 = strings.Join(groups, ", ") // a time line reads left to right
			}
		}
		order = " ORDER BY " + m0
	}
	sqlText := fmt.Sprintf("SELECT %s FROM %s WHERE %s", strings.Join(selects, ", "), expr(ds.from), strings.Join(where, " AND "))
	if len(groups) > 0 {
		sqlText += " GROUP BY " + strings.Join(groups, ", ")
	}
	sqlText += order + fmt.Sprintf(" LIMIT %d", limit+1)

	rows, err := db.DB.Query(sqlText, args...)
	if err != nil {
		return res, fmt.Errorf("the dashboard query failed: %v", err)
	}
	defer rows.Close()
	n := len(res.Columns)
	for rows.Next() {
		vals := make([]interface{}, n)
		ptrs := make([]interface{}, n)
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return res, err
		}
		row := map[string]interface{}{}
		for i, c := range res.Columns {
			row[c.Key] = datasetValue(vals[i])
		}
		res.Rows = append(res.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return res, err
	}
	if len(res.Rows) > limit {
		res.Rows, res.Truncated = res.Rows[:limit], true
	}
	return res, nil
}

func datasetMeasureLabel(agg, label string) string {
	switch agg {
	case "sum":
		return label
	case "count":
		return "Count of " + strings.ToLower(label)
	case "distinct":
		return "Different " + strings.ToLower(label)
	case "avg":
		return "Average " + strings.ToLower(label)
	case "min":
		return "Lowest " + strings.ToLower(label)
	case "max":
		return "Highest " + strings.ToLower(label)
	}
	return label
}

// datasetValue makes a scanned value JSON-friendly: numerics come back as
// []byte from lib/pq.
func datasetValue(v interface{}) interface{} {
	switch x := v.(type) {
	case []byte:
		s := string(x)
		var num json.Number = json.Number(s)
		if _, err := num.Float64(); err == nil {
			fv, _ := num.Float64()
			return round2(fv)
		}
		return s
	case float64:
		return round2(x)
	case time.Time:
		return x.Format("2006-01-02")
	}
	return v
}

// DatasetKeys lists the dataset keys in order (tests, ready-made checks).
func DatasetKeys() []string {
	out := make([]string, 0, len(dashboardDatasets))
	for _, d := range dashboardDatasets {
		out = append(out, d.Key)
	}
	sort.Strings(out)
	return out
}
