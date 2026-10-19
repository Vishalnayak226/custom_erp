package engines

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Catalog reports - MIS & Management, Master Data & Audit, E-commerce &
// Integration (Stage 75.3.6, Wave B).
//
// MIS folds the same till lines, returns and stock the Wave A reports read.
// Sales figures for a stock report are the period asked (From / To; blank =
// the last 90 days). The ERP keeps no sales targets, so Sales vs Target ranks
// the stores and leaves the target blank. Audit reports read the
// approval_log, the documents trigger's audit_logs, the users and roles
// tables and the report export jobs. E-commerce reads SalesOrder /
// SalesOrderLine, ShippingPackage, LogisticsBooking (AWB, courier, manifest),
// the Unicommerce and channel mapping tables and the integration logs.

// --- period sales for stock reports -------------------------------------------

func catPeriod(p map[string]string) (from, to string, days float64) {
	to = strings.TrimSpace(p["to"])
	if to == "" {
		to = time.Now().Format("2006-01-02")
	}
	from = strings.TrimSpace(p["from"])
	if from == "" {
		t, _ := time.Parse("2006-01-02", to)
		from = t.AddDate(0, 0, -89).Format("2006-01-02")
	}
	f, _ := time.Parse("2006-01-02", from)
	t, _ := time.Parse("2006-01-02", to)
	days = math.Max(1, t.Sub(f).Hours()/24+1)
	return
}

type catSale struct{ qty, net float64 }

// catSold: paid till sales per store and SKU in the period, once per run.
func catSold(r *catRow) catSale {
	m, ok := r.lk.extra["sold"].(map[string]catSale)
	if !ok {
		m = map[string]catSale{}
		from, to, _ := catPeriod(r.p)
		rows, err := dbQuery(fmt.Sprintf(`SELECT d.data->>'location', i->>'sku', SUM(COALESCE(NULLIF(i->>'qty', '')::numeric, 0)),
			SUM(COALESCE(NULLIF(i->>'net_total', '')::numeric, COALESCE(NULLIF(i->>'sale_price', '')::numeric, 0) * COALESCE(NULLIF(i->>'qty', '')::numeric, 0)))
			FROM %s.documents d, jsonb_array_elements(CASE WHEN jsonb_typeof(d.data->'items') = 'array' THEN d.data->'items' ELSE '[]'::jsonb END) i
			WHERE d.doctype = 'POSCart' AND d.status = 'Paid' AND d.deleted_at IS NULL AND d.created_at >= $1::date AND d.created_at < $2::date + 1
			GROUP BY 1, 2`, r.lk.schema), from, to)
		if err == nil {
			for rows.Next() {
				var store, sku string
				var s catSale
				if rows.Scan(&store, &sku, &s.qty, &s.net) == nil {
					m[store+"/"+sku] = s
				}
			}
			rows.Close()
		}
		r.lk.extra["sold"] = m
	}
	return m[r.storeID()+"/"+r.sku()]
}

type catStockTotal struct{ qty, mrp float64 }

// catStockBy: on-hand stock and its MRP value per store, per category and per
// store + category, once per run.
func catStockBy(r *catRow, key string) catStockTotal {
	m, ok := r.lk.extra["stockby"].(map[string]catStockTotal)
	if !ok {
		m = map[string]catStockTotal{}
		rows, err := dbQuery(fmt.Sprintf(`SELECT ia.location_code, COALESCE(i.data->>'category', ''), SUM(ia.on_hand),
			SUM(ia.on_hand * COALESCE(NULLIF(i.data->>'mrp', '')::numeric, 0))
			FROM %[1]s.inventory_availability ia LEFT JOIN %[1]s.documents i ON i.doctype = 'Item' AND i.id = ia.sku GROUP BY 1, 2`, r.lk.schema))
		if err == nil {
			for rows.Next() {
				var store, cat string
				var t catStockTotal
				if rows.Scan(&store, &cat, &t.qty, &t.mrp) == nil {
					for _, k := range []string{"s:" + store, "c:" + cat, "sc:" + store + "/" + cat} {
						v := m[k]
						v.qty, v.mrp = v.qty+t.qty, v.mrp+t.mrp
						m[k] = v
					}
				}
			}
			rows.Close()
		}
		r.lk.extra["stockby"] = m
	}
	return m[key]
}

func catCategoryOf(r *catRow) string { return catStr(r.item()["category"]) }

var misSalesCols = map[string]catFn{
	"Margin Amount": func(r *catRow) interface{} { return posLine(r).taxable - catItemCost(r)*r.ln("qty") },
	"Cost Value":    func(r *catRow) interface{} { return catItemCost(r) * r.ln("qty") },
	"_taxable":      func(r *catRow) interface{} { return posLine(r).taxable },
	"_cost":         func(r *catRow) interface{} { return catItemCost(r) * r.ln("qty") },
}

func misMarginPct(row map[string]interface{}) {
	row["Margin %"] = catPct(catNum(row["_taxable"])-catNum(row["_cost"]), catNum(row["_taxable"]))
}

func misRank(header string) func(rows []map[string]interface{}) {
	return func(rows []map[string]interface{}) {
		idx := make([]int, len(rows))
		for i := range idx {
			idx[i] = i
		}
		sort.SliceStable(idx, func(a, b int) bool { return catNum(rows[idx[a]][header]) > catNum(rows[idx[b]][header]) })
		for rank, i := range idx {
			rows[i]["Rank"] = float64(rank + 1)
		}
	}
}

var misStockCols = map[string]catFn{
	"Sold Qty":         func(r *catRow) interface{} { return catSold(r).qty },
	"Net Sales Amount": func(r *catRow) interface{} { return catSold(r).net },
	"Last Sale Date":   func(r *catRow) interface{} { _, s := invLastMoves(r); return s },
	"_taxable": func(r *catRow) interface{} {
		return catSold(r).net / (1 + catNum(r.item()["gst_rate"])/100)
	},
	"_cost": func(r *catRow) interface{} { return catSold(r).qty * catItemCost(r) },
	"Stock Cover Days": func(r *catRow) interface{} {
		_, _, days := catPeriod(r.p)
		if s := catSold(r).qty; s > 0 {
			return r.hn("available") / (s / days)
		}
		return nil
	},
}

func misCoverPost(row map[string]interface{}, days float64) {
	if s := catNum(row["Sold Qty"]); s > 0 {
		row["Stock Cover Days"] = math.Round(catNum(row["Closing Stock Qty"])/(s/days)*10) / 10
	}
}

var catMISLines = &catSource{Label: "POS Bill", Doctype: "POSCart", Where: "status = 'Paid'", Lines: "items", Number: "bill_number",
	SKU: "sku", Qty: "qty", MRP: "reference_price", Cols: mergeCols(posSaleCols, posLineCols, misSalesCols)}
var catMISStock = &catSource{Label: "Stock", Rows: invRows, SKU: "sku", Qty: "on_hand", Cols: mergeCols(invCols, misStockCols)}

func misSoldOnly(r *catRow) interface{} {
	if r.src == catReturnLines {
		return 0.0
	}
	return r.ln("qty")
}

func misReturnOnly(r *catRow) interface{} {
	if r.src == catReturnLines {
		return r.ln("qty")
	}
	return 0.0
}

func misBillNo(r *catRow) interface{} {
	if r.src == catReturnLines {
		return nil
	}
	return posBillNo(r)
}

func misABVUPT(row map[string]interface{}) {
	bills := catNum(row["Bill Count"])
	row["ABV"], row["UPT"] = catDiv(catNum(row["Net Sales Amount"]), bills), catDiv(catNum(row["Sold Qty"]), bills)
}

var _ = catWire("cat-daily-business-summary", &catReport{Union: []*catSource{catPOSLines, catReturnLines},
	Group: []string{"Document Date", "Store Code", "Store Name"},
	Agg:   map[string]string{"Bill Count": "countd", "Closing Stock Qty": "first", "MRP Value": "first"},
	Cols: map[string]catFn{
		"Sold Qty":          misSoldOnly,
		"Return Qty":        misReturnOnly,
		"Bill Count":        misBillNo,
		"Closing Stock Qty": func(r *catRow) interface{} { return catStockBy(r, "s:"+r.storeID()).qty },
		"MRP Value":         func(r *catRow) interface{} { return catStockBy(r, "s:"+r.storeID()).mrp },
		"ABV":               catPost,
		"UPT":               catPost,
	},
	Post: misABVUPT})
var _ = catWire("cat-sales-vs-target", &catReport{Src: catMISLines,
	Group:   []string{"From Date", "To Date", "Store Code", "Store Name"},
	Cols:    map[string]catFn{"Target Amount": catNotHeld, "Achievement %": catNotHeld, "Rank": catPost},
	PostAll: misRank("Net Sales Amount")})
var _ = catWire("cat-store-performance-ranking", &catReport{Src: catMISLines, Hidden: []string{"_taxable", "_cost"},
	Cols:  map[string]catFn{"ABV": catPost, "UPT": catPost, "Rank": catPost, "Margin %": catPost},
	Group: []string{"From Date", "To Date", "Store Code", "Store Name"}, Agg: map[string]string{"Bill Count": "countd"},
	Post: func(row map[string]interface{}) { misABVUPT(row); misMarginPct(row) }, PostAll: misRank("Net Sales Amount")})
var _ = catWire("cat-category-performance-summary", &catReport{Src: catMISLines, Hidden: []string{"_taxable", "_cost"},
	Group: []string{"From Date", "To Date", "Category Name"},
	Agg:   map[string]string{"Closing Stock Qty": "first", "MRP Value": "first"},
	Cols: map[string]catFn{
		"Closing Stock Qty": func(r *catRow) interface{} { return catStockBy(r, "c:"+catCategoryOf(r)).qty },
		"MRP Value":         func(r *catRow) interface{} { return catStockBy(r, "c:"+catCategoryOf(r)).mrp },
		"Sell Through %":    catPost,
		"Margin %":          catPost,
	},
	Post: func(row map[string]interface{}) {
		misMarginPct(row)
		row["Sell Through %"] = catPct(catNum(row["Sold Qty"]), catNum(row["Sold Qty"])+catNum(row["Closing Stock Qty"]))
	}})
var _ = catWire("cat-gross-margin-summary", &catReport{Src: catMISLines, Hidden: []string{"_taxable", "_cost"},
	Group: []string{"From Date", "To Date", "Store Name", "Category Name"}, Post: misMarginPct})
var _ = catWire("cat-fast-slow-moving-stock", &catReport{Src: catMISStock,
	Group: []string{"As Of Date", "SKU", "Design No", "Item Name", "Category Name"},
	Agg:   map[string]string{"Stock Age Days": "max", "Last Sale Date": "max"},
	Post: func(row map[string]interface{}) {
		sold, avail := catNum(row["Sold Qty"]), catNum(row["Available Qty"])
		switch {
		case sold == 0:
			row["Status"] = "Non-moving"
		case sold >= avail:
			row["Status"] = "Fast"
		default:
			row["Status"] = "Slow"
		}
	}})
var _ = catWire("cat-stock-cover-report", &catReport{Src: catMISStock,
	Keep: func(r *catRow) bool { return r.hn("available") > 0 },
	Cols: map[string]catFn{"Status": func(r *catRow) interface{} {
		d, ok := misStockCols["Stock Cover Days"](r).(float64)
		switch {
		case !ok:
			return "No sales in the period"
		case d < 15:
			return "Reorder"
		case d > 120:
			return "Overstock"
		}
		return "Healthy"
	}}})
var _ = catWire("cat-ageing-inventory-value", &catReport{Src: catMISStock, Keep: func(r *catRow) bool { return r.hn("on_hand") > 0 },
	Group: []string{"As Of Date", "Store Name", "Category Name", "Age Bucket"},
	Cols: map[string]catFn{"Age Bucket": func(r *catRow) interface{} {
		if d, ok := invAgeDays(r).(float64); ok {
			return invAgeBucket(d)
		}
		return "No receipt on record"
	}}})
var _ = catWire("cat-inventory-turnover-summary", &catReport{Src: catMISStock,
	Cols:  map[string]catFn{"Inventory Turnover": catPost, "Sell Through %": catPost},
	Group: []string{"From Date", "To Date", "Store Name", "Category Name"},
	Post: func(row map[string]interface{}) {
		row["Inventory Turnover"] = catDiv(catNum(row["Sold Qty"]), catNum(row["Closing Stock Qty"]))
		row["Sell Through %"] = catPct(catNum(row["Sold Qty"]), catNum(row["Sold Qty"])+catNum(row["Closing Stock Qty"]))
	}})
var _ = catWire("cat-management-stock-and-sales-summary", &catReport{Src: catMISStock, Hidden: []string{"_taxable", "_cost", "_days"},
	Group: []string{"From Date", "To Date", "Store Name", "Category Name"},
	Agg:   map[string]string{"_days": "first"},
	Cols:  map[string]catFn{"_days": func(r *catRow) interface{} { _, _, d := catPeriod(r.p); return d }, "Margin %": catPost, "Stock Cover Days": catPost},
	Post:  func(row map[string]interface{}) { misMarginPct(row); misCoverPost(row, catNum(row["_days"])) }})

// --- master data and audit ------------------------------------------------------

var catItems = &catSource{Label: "Item", Doctype: "Item", StoreField: "-", SKU: "id", Statuses: []string{"Active", "Inactive", "Draft"},
	Cols: map[string]catFn{
		"Subcategory": func(r *catRow) interface{} {
			return firstNonEmpty(r.hs("sub_category"), r.hs("subcategory"), r.hs("product_type"))
		},
		"Status": func(r *catRow) interface{} { return r.Status },
	}}

var catStores = &catSource{Label: "Location", Doctype: "Location", StoreOf: func(r *catRow) string { return r.ID },
	Statuses: []string{"Active", "Inactive"},
	Cols: map[string]catFn{
		"Outlet ID":     func(r *catRow) interface{} { return firstNonEmpty(r.hs("code"), r.ID) },
		"Location Code": func(r *catRow) interface{} { return firstNonEmpty(r.hs("parent"), r.hs("code"), r.ID) },
		"Location Name": func(r *catRow) interface{} {
			if p := r.hs("parent"); p != "" {
				return r.nameOf("Location", p)
			}
			return r.hs("name")
		},
		"Store Code": func(r *catRow) interface{} { return firstNonEmpty(r.hs("code"), r.ID) },
		"Store Name": catH("name"),
	}}

var catVendors = &catSource{Label: "Vendor", Doctype: "Vendor", StoreField: "-", Vendor: "id", Statuses: []string{"Active", "Inactive"}}

// The master data that the change audit covers.
var auditMasterDoctypes = []string{"Item", "Vendor", "Customer", "Location", "LegalEntity", "ProductFamily", "Category", "Brand", "Employee",
	"CostCenter", "Department", "HSNCode", "UOM", "Salesman", "POSTender", "TDSSection", "BankAccount", "Bin", "ItemAttribute"}

// auditNoise are fields every save rewrites - not a change anyone made.
var auditNoise = map[string]bool{"version": true, "updated_at": true, "modified": true, "modified_by": true, "_created_at": true, "last_synced_at": true}

func auditDocNo(r *catRow) interface{} {
	if d := r.master(r.hs("doctype"), r.hs("doc_id")); d != nil {
		return firstNonEmpty(catStr(d["code"]), catStr(d["name"]), r.hs("doc_id"))
	}
	return r.hs("doc_id")
}

var catMasterChanges = &catSource{Label: "Master change", Rows: auditChangeRows(auditMasterDoctypes, nil), StoreField: "-",
	Cols: map[string]catFn{
		"Audit ID":      func(r *catRow) interface{} { return r.ID },
		"Document Type": catH("doctype"),
		"Document No":   auditDocNo,
		"Field Changed": catH("field"),
		"Old Value":     catH("old"),
		"New Value":     catH("new"),
		"Edit Reason":   catNotHeld,
		"Edited By":     func(r *catRow) interface{} { return r.CreatedBy },
		"Edited At":     func(r *catRow) interface{} { return catStamp(r.Created) },
	}}

// usersRows: one row per user and store scope (the users table).
func usersRows(tenantID, schema string, p map[string]string) ([]*catRow, error) {
	rows, err := dbQuery(fmt.Sprintf(`SELECT id, username, COALESCE(role, ''), COALESCE(status, ''), COALESCE(location_code, ''), created_at,
		COALESCE(store_access, ''), COALESCE(stores::text, '') FROM %s.users ORDER BY username`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*catRow
	for rows.Next() {
		var id, user, role, status, home, access, stores string
		var created time.Time
		if rows.Scan(&id, &user, &role, &status, &home, &created, &access, &stores) != nil {
			continue
		}
		scopes := []string{home}
		if list := catLineListStrings(stores); len(list) > 0 && !strings.EqualFold(access, "All") {
			scopes = list
		}
		for _, s := range scopes {
			out = append(out, &catRow{ID: id, Status: status, Created: created, H: map[string]interface{}{"user": user, "role": role, "location": s,
				"access": access}})
		}
	}
	return out, rows.Err()
}

func catLineListStrings(raw string) []string {
	var out []string
	for _, s := range strings.Split(strings.Trim(raw, "[]{} "), ",") {
		if s = strings.Trim(strings.TrimSpace(s), "\""); s != "" {
			out = append(out, s)
		}
	}
	return out
}

var catUsers = &catSource{Label: "User", Rows: usersRows,
	Cols: map[string]catFn{
		"User ID":    func(r *catRow) interface{} { return r.ID },
		"User Name":  func(r *catRow) interface{} { return r.hs("user") + " (" + r.hs("role") + ")" },
		"Created At": func(r *catRow) interface{} { return catStamp(r.Created) },
		"Store Name": func(r *catRow) interface{} {
			if strings.EqualFold(r.hs("access"), "All") {
				return "All stores"
			}
			return r.nameOf("Location", r.storeID())
		},
		"Approved By": catNotHeld,
	}}

// rolePermissionRows: one row per role and record type it may touch.
func rolePermissionRows(tenantID, schema string, p map[string]string) ([]*catRow, error) {
	rows, err := dbQuery(fmt.Sprintf(`SELECT rp.id::text, rp.role, rp.doctype_name, rp.allow_read, rp.allow_create, rp.allow_update, rp.allow_delete,
		COALESCE(r.created_by, ''), r.created_at FROM %[1]s.role_permissions rp LEFT JOIN %[1]s.roles r ON r.name = rp.role
		ORDER BY rp.role, rp.doctype_name`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*catRow
	for rows.Next() {
		var id, role, doctype, by string
		var read, create, update, del bool
		var created *time.Time
		if rows.Scan(&id, &role, &doctype, &read, &create, &update, &del, &by, &created) != nil {
			continue
		}
		var rights []string
		for _, x := range []struct {
			on   bool
			name string
		}{{read, "Read"}, {create, "Create"}, {update, "Update"}, {del, "Delete"}} {
			if x.on {
				rights = append(rights, x.name)
			}
		}
		r := &catRow{ID: id, CreatedBy: by, H: map[string]interface{}{"role": role, "doctype": doctype, "rights": strings.Join(rights, ", ")}}
		if created != nil {
			r.Created = *created
		}
		r.Status = map[bool]string{true: "Granted", false: "No access"}[len(rights) > 0]
		out = append(out, r)
	}
	return out, rows.Err()
}

var catRolePerms = &catSource{Label: "Role permission", Rows: rolePermissionRows, StoreField: "-",
	Cols: map[string]catFn{
		"Document Date": func(r *catRow) interface{} { return catDay(r.Created) },
		"Document No":   func(r *catRow) interface{} { return r.hs("role") + " / " + r.hs("doctype") },
		"Approved By":   catNotHeld,
		"Remarks":       catH("rights"),
	}}

// approvalRows: every maker-checker action (approval_log).
func approvalRows(tenantID, schema string, p map[string]string) ([]*catRow, error) {
	q := fmt.Sprintf(`SELECT id::text, doctype, document_id, action, COALESCE(actor_user_id, ''), COALESCE(actor_role, ''), COALESCE(comment, ''), created_at
		FROM %s.approval_log WHERE TRUE`, schema)
	var args []interface{}
	if v := p["from"]; v != "" {
		args = append(args, v)
		q += fmt.Sprintf(" AND created_at >= $%d::date", len(args))
	}
	if v := p["to"]; v != "" {
		args = append(args, v)
		q += fmt.Sprintf(" AND created_at < $%d::date + 1", len(args))
	}
	rows, err := dbQuery(q+" ORDER BY created_at DESC LIMIT 20000", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*catRow
	for rows.Next() {
		var id, doctype, doc, action, actor, role, comment string
		var at time.Time
		if rows.Scan(&id, &doctype, &doc, &action, &actor, &role, &comment, &at) != nil {
			continue
		}
		out = append(out, &catRow{ID: id, Status: action, CreatedBy: actor, Created: at, H: map[string]interface{}{"doctype": doctype, "doc_id": doc,
			"role": role, "comment": comment}})
	}
	return out, rows.Err()
}

// catDocNumber is a document's printed number, whatever its type calls it.
func catDocNumber(r *catRow, doctype, id string) string {
	if d := r.master(doctype, id); d != nil {
		for _, f := range []string{"bill_number", "po_number", "invoice_number", "note_number", "transfer_number", "voucher_number", "code"} {
			if v := catStr(d[f]); v != "" {
				return v
			}
		}
	}
	return id
}

var catApprovals = &catSource{Label: "Approval", Rows: approvalRows, StoreOf: func(r *catRow) string {
	d := r.master(r.hs("doctype"), r.hs("doc_id"))
	return firstNonEmpty(catStr(d["location"]), catStr(d["location_code"]), catStr(d["from_warehouse"]))
},
	Cols: map[string]catFn{
		"Document Type":   catH("doctype"),
		"Document No":     func(r *catRow) interface{} { return catDocNumber(r, r.hs("doctype"), r.hs("doc_id")) },
		"Approval Status": func(r *catRow) interface{} { return r.Status },
		"Approved By":     func(r *catRow) interface{} { return r.CreatedBy + " (" + r.hs("role") + ")" },
		"Approved At":     func(r *catRow) interface{} { return catStamp(r.Created) },
		"Reason":          catH("comment"),
		"Remarks": func(r *catRow) interface{} {
			if a := catNum(r.master(r.hs("doctype"), r.hs("doc_id"))["grand_total"]); a > 0 {
				return fmt.Sprintf("Amount %.2f", a)
			}
			return nil
		},
	}}

var auditDocIDPattern = regexp.MustCompile(`Document ID: (\S+)`)

// activityNoise are records the system writes for itself on every run.
var activityNoise = map[string]bool{"ReportRunLog": true, "NotificationLog": true, "TaskCompletionLog": true, "StorageBalanceSnapshot": true,
	"StockLedgerEntry": true, "DocumentEditHistory": true, "CourierTrackingEvent": true, "ContentAssistLog": true, "ReportExportJob": true}

// activityRows: material user actions from the audit log - sign-ins and
// document creates, edits and deletes (not system errors).
func activityRows(tenantID, schema string, p map[string]string) ([]*catRow, error) {
	q := fmt.Sprintf(`SELECT id::text, COALESCE(user_id, ''), action, COALESCE(details, ''), created_at FROM %s.audit_logs
		WHERE (action LIKE 'CREATE\_%%' OR action LIKE 'UPDATE\_%%' OR action LIKE 'DELETE\_%%' OR action IN ('LOGIN', 'LOGOUT', 'LOGIN_FAILED')
		       OR action ~ '^[A-Z_]+$') AND action !~ '^[A-Z]+-[0-9]{4}$' AND COALESCE(user_id, '') NOT IN ('', 'system')`, schema)
	var args []interface{}
	if v := p["from"]; v != "" {
		args = append(args, v)
		q += fmt.Sprintf(" AND created_at >= $%d::date", len(args))
	}
	if v := p["to"]; v != "" {
		args = append(args, v)
		q += fmt.Sprintf(" AND created_at < $%d::date + 1", len(args))
	}
	rows, err := dbQuery(q+" ORDER BY created_at DESC LIMIT 20000", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*catRow
	for rows.Next() {
		var id, user, action, details string
		var at time.Time
		if rows.Scan(&id, &user, &action, &details, &at) != nil {
			continue
		}
		doctype := ""
		for _, pre := range []string{"CREATE_", "UPDATE_", "DELETE_"} {
			if strings.HasPrefix(action, pre) {
				doctype = strings.TrimPrefix(action, pre)
			}
		}
		docID := ""
		if m := auditDocIDPattern.FindStringSubmatch(details); m != nil {
			docID = m[1]
		}
		if activityNoise[doctype] {
			continue // the system's own bookkeeping, not something a person did
		}
		if len(details) > 160 {
			details = details[:160] + "..."
		}
		out = append(out, &catRow{ID: id, CreatedBy: user, Created: at, H: map[string]interface{}{"action": action, "doctype": doctype, "doc_id": docID,
			"details": details}})
	}
	return out, rows.Err()
}

var catActivity = &catSource{Label: "User action", Rows: activityRows, StoreField: "-",
	Cols: map[string]catFn{
		"Edit Action":   catH("action"),
		"Document Type": catH("doctype"),
		"Document No":   catH("doc_id"),
		"IP Address":    catNotHeld,
		"Remarks":       catH("details"),
	}}

var catReportExports = &catSource{Label: "Report export", Doctype: "ReportExportJob", StoreField: "-",
	Cols: map[string]catFn{
		"Report Name": func(r *catRow) interface{} {
			if d, ok := reportRegistry[r.hs("report_id")]; ok {
				return d.Label
			}
			return r.hs("report_id")
		},
		"From Date": func(r *catRow) interface{} { return reportExportParam(r, "from", "start", "start_date") },
		"To Date":   func(r *catRow) interface{} { return reportExportParam(r, "to", "end", "end_date") },
		"User ID":   func(r *catRow) interface{} { return firstNonEmpty(r.hs("requested_by"), r.CreatedBy) },
		"User Name": func(r *catRow) interface{} {
			return firstNonEmpty(r.hs("requested_by"), r.CreatedBy) + " (" + r.hs("requested_role") + ")"
		},
		"Export Format": catConst("CSV"),
		"Download At":   func(r *catRow) interface{} { return catStamp(r.Created) },
		"IP Address":    catNotHeld,
	}}

func reportExportParam(r *catRow, keys ...string) interface{} {
	params := map[string]interface{}{}
	switch v := r.H["params"].(type) {
	case map[string]interface{}:
		params = v
	case string:
		if l := catLineList("[" + v + "]"); len(l) == 1 {
			params = l[0]
		}
	}
	for _, k := range keys {
		if s := catStr(params[k]); s != "" {
			return s
		}
	}
	return nil
}

var catBackdated = &catSource{Label: "Backdated entry", Doctype: "BackdatedPostingRequest", StoreOf: func(r *catRow) string {
	d := r.master(r.hs("target_doctype"), r.hs("target_document_id"))
	return firstNonEmpty(catStr(d["location"]), catStr(d["location_code"]))
},
	Statuses: []string{"Pending", "Approved", "Rejected"},
	Cols: map[string]catFn{
		"Document Type": catH("target_doctype"),
		"Document No": func(r *catRow) interface{} {
			return catDocNumber(r, r.hs("target_doctype"), r.hs("target_document_id"))
		},
		"Document Date": catHD("transaction_date"),
		"Created At":    func(r *catRow) interface{} { return catStamp(r.Created) },
	}}

var catCancellations = &catSource{Label: "Cancellation", Rows: auditChangeRows(nil, []string{"status"}), StoreOf: func(r *catRow) string {
	d := r.master(r.hs("doctype"), r.hs("doc_id"))
	return firstNonEmpty(catStr(d["location"]), catStr(d["location_code"]), catStr(d["from_warehouse"]), catStr(d["return_location"]))
},
	Cols: map[string]catFn{
		"Document Type": catH("doctype"),
		"Document No":   func(r *catRow) interface{} { return catDocNumber(r, r.hs("doctype"), r.hs("doc_id")) },
		"Document Date": func(r *catRow) interface{} { return catDay(r.master(r.hs("doctype"), r.hs("doc_id"))["_created_at"]) },
		"Edit Action":   func(r *catRow) interface{} { return firstNonEmpty(r.hs("old"), "?") + " → " + r.hs("new") },
		"Reason": func(r *catRow) interface{} {
			d := r.master(r.hs("doctype"), r.hs("doc_id"))
			return firstNonEmpty(catStr(d["cancel_reason"]), catStr(d["cancel_reason_code"]), catStr(d["void_reason"]), catStr(d["cancel_note"]))
		},
		"Approved By": func(r *catRow) interface{} { return r.master(r.hs("doctype"), r.hs("doc_id"))["cancelled_by"] },
		"Approved At": func(r *catRow) interface{} { return r.master(r.hs("doctype"), r.hs("doc_id"))["cancelled_at"] },
	}}

var _ = catWire("cat-item-master-register", &catReport{Src: catItems})
var _ = catWire("cat-store-location-master-register", &catReport{Src: catStores})
var _ = catWire("cat-vendor-master-register", &catReport{Src: catVendors})
var _ = catWire("cat-master-data-change-audit", &catReport{Src: catMasterChanges, Keep: func(r *catRow) bool { return !auditNoise[r.hs("field")] }})
var _ = catWire("cat-user-access-matrix-report", &catReport{Src: catUsers, NoDates: true})
var _ = catWire("cat-role-permission-audit", &catReport{Src: catRolePerms, NoDates: true})
var _ = catWire("cat-approval-history-report", &catReport{Src: catApprovals})
var _ = catWire("cat-user-activity-log", &catReport{Src: catActivity})
var _ = catWire("cat-report-download-audit", &catReport{Src: catReportExports})
var _ = catWire("cat-backdated-entry-report", &catReport{Src: catBackdated})
var _ = catWire("cat-transaction-cancellation-audit", &catReport{Src: catCancellations,
	Keep: func(r *catRow) bool {
		switch r.hs("new") {
		case "Cancelled", "Void", "Voided", "Rejected":
			return true
		}
		return false
	}})

// --- e-commerce and integration ---------------------------------------------------

func ecomOrder(r *catRow, field string) map[string]interface{} {
	return r.master("SalesOrder", r.hs(field))
}

// ecomBooking is the courier booking of a shipping package (or of an order).
func ecomBooking(r *catRow, pkg, order string) map[string]interface{} {
	m, ok := r.lk.extra["bookings"].(map[string]map[string]interface{})
	if !ok {
		m = map[string]map[string]interface{}{}
		rows, err := dbQuery(fmt.Sprintf(`SELECT COALESCE(data->>'shipping_package_id', ''), COALESCE(data->>'order_id', ''), COALESCE(data->>'tracking_number', ''),
			COALESCE(data->>'awb_number', ''), COALESCE(data->>'carrier', ''), COALESCE(data->>'manifest_id', '') FROM %s.documents
			WHERE doctype = 'LogisticsBooking' AND deleted_at IS NULL ORDER BY created_at`, r.lk.schema))
		if err == nil {
			for rows.Next() {
				var p, o, tr, awb, carrier, manifest string
				if rows.Scan(&p, &o, &tr, &awb, &carrier, &manifest) == nil {
					b := map[string]interface{}{"tracking": firstNonEmpty(tr, awb), "carrier": carrier, "manifest": manifest}
					if p != "" {
						m["p:"+p] = b
					}
					if o != "" {
						m["o:"+o] = b
					}
				}
			}
			rows.Close()
		}
		r.lk.extra["bookings"] = m
	}
	if b := m["p:"+pkg]; b != nil {
		return b
	}
	return m["o:"+order]
}

// ecomUC is the Unicommerce status of an order (unicommerce_order_mapping).
func ecomUC(r *catRow, order string) string {
	m, ok := r.lk.extra["ucorders"].(map[string]string)
	if !ok {
		m = map[string]string{}
		rows, err := dbQuery(fmt.Sprintf(`SELECT order_id, COALESCE(status, '') FROM %s.unicommerce_order_mapping`, r.lk.schema))
		if err == nil {
			for rows.Next() {
				var o, s string
				if rows.Scan(&o, &s) == nil {
					m[o] = s
				}
			}
			rows.Close()
		}
		r.lk.extra["ucorders"] = m
	}
	return m[order]
}

func ecomChannelSKU(r *catRow, channel string) string {
	m, ok := r.lk.extra["chansku"].(map[string]string)
	if !ok {
		m = map[string]string{}
		rows, err := dbQuery(fmt.Sprintf(`SELECT sku, channel, COALESCE(channel_sku, '') FROM %s.channel_product_mapping`, r.lk.schema))
		if err == nil {
			for rows.Next() {
				var sku, ch, cs string
				if rows.Scan(&sku, &ch, &cs) == nil {
					m[strings.ToLower(ch)+"/"+sku] = cs
				}
			}
			rows.Close()
		}
		r.lk.extra["chansku"] = m
	}
	return m[strings.ToLower(channel)+"/"+r.sku()]
}

// ecomPackages lists each order's packages in the order they were made.
func ecomPackageSeq(r *catRow, order, pkg string) interface{} {
	m, ok := r.lk.extra["pkgseq"].(map[string]int)
	if !ok {
		m = map[string]int{}
		rows, err := dbQuery(fmt.Sprintf(`SELECT id, COALESCE(data->>'order_id', ''), ROW_NUMBER() OVER (PARTITION BY data->>'order_id' ORDER BY created_at, id)
			FROM %s.documents WHERE doctype = 'ShippingPackage' AND deleted_at IS NULL`, r.lk.schema))
		if err == nil {
			for rows.Next() {
				var id, o string
				var n int
				if rows.Scan(&id, &o, &n) == nil {
					m[o+"/"+id] = n
				}
			}
			rows.Close()
		}
		r.lk.extra["pkgseq"] = m
	}
	if n := m[order+"/"+pkg]; n > 0 {
		return float64(n)
	}
	return nil
}

var ecomCols = map[string]catFn{
	"Order Date": func(r *catRow) interface{} {
		return catDay(firstNonEmpty(catStr(ecomOrder(r, "order_id")["_created_at"]), catStamp(r.Created)))
	},
	"Order ID": func(r *catRow) interface{} {
		o := ecomOrder(r, "order_id")
		return firstNonEmpty(catStr(o["channel_order_id"]), catStr(o["code"]), r.hs("order_id"))
	},
	"Sales Channel": func(r *catRow) interface{} { return ecomOrder(r, "order_id")["channel"] },
	"Outlet ID":     func(r *catRow) interface{} { return r.storeID() },
	"Outlet Name":   func(r *catRow) interface{} { return r.nameOf("Location", r.storeID()) },
	"UC Status":     func(r *catRow) interface{} { return ecomUC(r, r.hs("order_id")) },
	"POS Bill Status": func(r *catRow) interface{} {
		return nil // marketplace orders are invoiced, not billed at a till
	},
	"POS Invoice No": catNotHeld,
	"Order Status":   func(r *catRow) interface{} { return ecomOrder(r, "order_id")["order_status"] },
}

var catEcomPackages = &catSource{Label: "Shipment", Doctype: "ShippingPackage", StoreField: "location_code", Lines: "",
	Cols: mergeCols(ecomCols, map[string]catFn{
		"Shipment ID":       func(r *catRow) interface{} { return firstNonEmpty(r.hs("code"), r.ID) },
		"Shipment Sequence": func(r *catRow) interface{} { return ecomPackageSeq(r, r.hs("order_id"), r.ID) },
		"Invoice Code": func(r *catRow) interface{} {
			return firstNonEmpty(catStr(r.master("SalesInvoice", r.hs("sales_invoice_id"))["invoice_number"]), r.hs("sales_invoice_id"))
		},
		"Invoice Date": func(r *catRow) interface{} {
			return catDay(r.master("SalesInvoice", r.hs("sales_invoice_id"))["_created_at"])
		},
		"Invoice Status": func(r *catRow) interface{} { return r.master("SalesInvoice", r.hs("sales_invoice_id"))["_status"] },
		"Manifest Code": func(r *catRow) interface{} {
			b := ecomBooking(r, r.ID, r.hs("order_id"))
			return firstNonEmpty(catStr(r.master("Manifest", catStr(b["manifest"]))["code"]), catStr(b["manifest"]))
		},
		"Qty": catSumLines("items", "qty"),
		"Package Amount": func(r *catRow) interface{} {
			return firstPositive(catNum(r.master("SalesInvoice", r.hs("sales_invoice_id"))["total_amount"]), catSumLines("items", "amount")(r).(float64))
		},
		"Tracking Number":   func(r *catRow) interface{} { return ecomBooking(r, r.ID, r.hs("order_id"))["tracking"] },
		"Shipping Provider": func(r *catRow) interface{} { return ecomBooking(r, r.ID, r.hs("order_id"))["carrier"] },
	})}

var catEcomLines = &catSource{Label: "Order line", Doctype: "SalesOrderLine", StoreField: "location_code", SKU: "sku", Qty: "qty",
	Cols: mergeCols(ecomCols, map[string]catFn{
		"Shipment ID":    func(r *catRow) interface{} { return ecomOrderPackage(r) },
		"Invoice Code":   func(r *catRow) interface{} { return ecomOrderInvoice(r)["invoice_number"] },
		"Invoice Status": func(r *catRow) interface{} { return ecomOrderInvoice(r)["_status"] },
		"Channel SKU": func(r *catRow) interface{} {
			return ecomChannelSKU(r, catStr(ecomOrder(r, "order_id")["channel"]))
		},
		"Line Status":       catH("line_status"),
		"Package Amount":    func(r *catRow) interface{} { return r.hn("unit_price") * r.hn("qty") },
		"Tracking Number":   func(r *catRow) interface{} { return ecomBooking(r, "", r.hs("order_id"))["tracking"] },
		"Shipping Provider": func(r *catRow) interface{} { return ecomBooking(r, "", r.hs("order_id"))["carrier"] },
		"Available Qty": func(r *catRow) interface{} {
			return ecomAvailable(r)
		},
		"Status": func(r *catRow) interface{} {
			if ecomAvailable(r) >= r.hn("qty") {
				return "Stock available"
			}
			return "Short of stock"
		},
	})}

// ecomOrderPackage / ecomOrderInvoice: the order's first package and invoice.
func ecomOrderPackage(r *catRow) string {
	m := ecomOrderDocs(r)
	return m["p:"+r.hs("order_id")]
}

func ecomOrderInvoice(r *catRow) map[string]interface{} {
	return r.master("SalesInvoice", ecomOrderDocs(r)["i:"+r.hs("order_id")])
}

func ecomOrderDocs(r *catRow) map[string]string {
	if m, ok := r.lk.extra["orderdocs"].(map[string]string); ok {
		return m
	}
	m := map[string]string{}
	rows, err := dbQuery(fmt.Sprintf(`SELECT doctype, id, COALESCE(data->>'order_id', data->>'sales_order_id', '') FROM %s.documents
		WHERE doctype IN ('ShippingPackage', 'SalesInvoice') AND deleted_at IS NULL ORDER BY created_at DESC`, r.lk.schema))
	if err == nil {
		for rows.Next() {
			var dt, id, o string
			if rows.Scan(&dt, &id, &o) == nil && o != "" {
				m[map[string]string{"ShippingPackage": "p:", "SalesInvoice": "i:"}[dt]+o] = id
			}
		}
		rows.Close()
	}
	r.lk.extra["orderdocs"] = m
	return m
}

func ecomAvailable(r *catRow) float64 {
	m, ok := r.lk.extra["avail"].(map[string]float64)
	if !ok {
		m = map[string]float64{}
		rows, err := dbQuery(fmt.Sprintf(`SELECT location_code, sku, available FROM %s.inventory_availability`, r.lk.schema))
		if err == nil {
			for rows.Next() {
				var s, sku string
				var a float64
				if rows.Scan(&s, &sku, &a) == nil {
					m[s+"/"+sku] = a
				}
			}
			rows.Close()
		}
		r.lk.extra["avail"] = m
	}
	return m[r.storeID()+"/"+r.sku()]
}

func ecomOrderOpen(r *catRow) bool {
	switch catStr(ecomOrder(r, "order_id")["order_status"]) {
	case "Shipped", "Delivered", "Cancelled", "Returned", "RTO", "Closed", "Completed":
		return false
	}
	switch r.hs("line_status") {
	case "Shipped", "Delivered", "Cancelled":
		return false
	}
	return true
}

// ucSyncRows: the stock last pushed to Unicommerce per SKU and outlet, beside
// what the ERP has now.
func ucSyncRows(tenantID, schema string, p map[string]string) ([]*catRow, error) {
	q := fmt.Sprintf(`SELECT s.store_code, s.sku, s.quantity, s.last_synced_at, COALESCE(ia.available, 0),
		EXISTS (SELECT 1 FROM %[1]s.channel_product_mapping m WHERE m.sku = s.sku)
		FROM %[1]s.unicommerce_inventory_sync s LEFT JOIN %[1]s.inventory_availability ia ON ia.sku = s.sku AND ia.location_code = s.store_code`, schema)
	var args []interface{}
	if v := strings.TrimSpace(p["location"]); v != "" {
		args = append(args, pqStringArray(strings.Split(v, ",")))
		q += " WHERE s.store_code = ANY($1)"
	}
	rows, err := dbQuery(q+" ORDER BY s.sku, s.store_code LIMIT 50000", args...)
	if err != nil {
		return nil, nil // no Unicommerce on this client
	}
	defer rows.Close()
	var out []*catRow
	for rows.Next() {
		var store, sku string
		var uc, erp float64
		var at *time.Time
		var mapped bool
		if rows.Scan(&store, &sku, &uc, &at, &erp, &mapped) != nil {
			continue
		}
		r := &catRow{ID: store + "/" + sku, H: map[string]interface{}{"location": store, "sku": sku, "uc": uc, "erp": erp, "mapped": mapped}}
		if at != nil {
			r.Created = *at
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

var ucCols = map[string]catFn{
	"ERP Sync Time":             func(r *catRow) interface{} { return catStamp(r.Created) },
	"Latest ERP Sync Time":      func(r *catRow) interface{} { return catStamp(r.Created) },
	"UC Facility Code":          func(r *catRow) interface{} { return r.storeID() },
	"Barcode":                   func(r *catRow) interface{} { return r.item()["barcode"] },
	"ERP Inventory Qty":         catHN("erp"),
	"Unicommerce Inventory Qty": catHN("uc"),
	"Variance Qty":              func(r *catRow) interface{} { return r.hn("erp") - r.hn("uc") },
	"Variance Status": func(r *catRow) interface{} {
		if r.hn("erp") == r.hn("uc") {
			return "Matched"
		}
		return "Mismatch"
	},
	"Mapping Status": func(r *catRow) interface{} {
		if r.hv("mapped") == true {
			return "Mapped"
		}
		return "Not mapped"
	},
	"Mismatch Outlet Count": func(r *catRow) interface{} {
		if r.hn("erp") != r.hn("uc") {
			return r.storeID()
		}
		return nil
	},
	"Integration Status": catConst("Synced"),
}

var catUCSync = &catSource{Label: "Inventory sync", Rows: ucSyncRows, SKU: "sku", Cols: mergeCols(ucCols, map[string]catFn{
	"Outlet ID": func(r *catRow) interface{} { return r.storeID() }, "Outlet Name": func(r *catRow) interface{} { return r.nameOf("Location", r.storeID()) }})}

// integrationRows: failed or retried integration events (event log + outbox).
func integrationRows(onlyOrders bool) func(tenantID, schema string, p map[string]string) ([]*catRow, error) {
	return func(tenantID, schema string, p map[string]string) ([]*catRow, error) {
		var out []*catRow
		q := fmt.Sprintf(`SELECT id::text, COALESCE(event_id, ''), COALESCE(channel, ''), COALESCE(status, ''), COALESCE(error_message, ''), created_at
			FROM %s.integration_event_log WHERE COALESCE(error_message, '') <> '' OR status NOT IN ('processed', 'success', 'Processed', 'Success')`, schema)
		if rows, err := dbQuery(q + " ORDER BY created_at DESC LIMIT 10000"); err == nil {
			for rows.Next() {
				var id, ev, ch, st, msg string
				var at time.Time
				if rows.Scan(&id, &ev, &ch, &st, &msg, &at) == nil {
					out = append(out, &catRow{ID: id, Status: st, Created: at, H: map[string]interface{}{"source": "Inbound event", "ref": ev,
						"channel": ch, "error": msg, "attempts": 1.0}})
				}
			}
			rows.Close()
		}
		if !onlyOrders {
			q := fmt.Sprintf(`SELECT id::text, COALESCE(event_name, ''), COALESCE(status, ''), attempts, created_at, COALESCE(correlation_id, '')
				FROM %s.integration_event_outbox WHERE attempts > 1 OR status NOT IN ('delivered', 'sent', 'Delivered', 'Sent')`, schema)
			if rows, err := dbQuery(q + " ORDER BY created_at DESC LIMIT 10000"); err == nil {
				for rows.Next() {
					var id, name, st, corr string
					var attempts int
					var at time.Time
					if rows.Scan(&id, &name, &st, &attempts, &at, &corr) == nil {
						out = append(out, &catRow{ID: id, Status: st, Created: at, H: map[string]interface{}{"source": "Outbound: " + name, "ref": corr,
							"attempts": float64(attempts)}})
					}
				}
				rows.Close()
			}
		}
		from, to := p["from"], p["to"]
		kept := out[:0]
		for _, r := range out {
			d := catDay(r.Created)
			if (from == "" || d >= from) && (to == "" || d <= to) {
				kept = append(kept, r)
			}
		}
		return kept, nil
	}
}

var integrationCols = map[string]catFn{
	"Channel":              catH("channel"),
	"Created At":           func(r *catRow) interface{} { return catStamp(r.Created) },
	"Order Date":           func(r *catRow) interface{} { return catDay(r.Created) },
	"Order ID":             catH("ref"),
	"Shipment ID":          catNotHeld,
	"Sales Channel":        catH("channel"),
	"Source Document Type": catH("source"),
	"Source Document No":   catH("ref"),
	"Integration Status":   func(r *catRow) interface{} { return r.Status },
	"Error Code": func(r *catRow) interface{} {
		if r.hs("error") == "" {
			return nil
		}
		return "FAILED"
	},
	"Error Message": catH("error"),
	"Retry Count":   catHN("attempts"),
	"Last Retry At": func(r *catRow) interface{} { return catStamp(r.Created) },
	"User Name":     catConst("system"),
}

var catIntegrationErrors = &catSource{Label: "Integration", Rows: integrationRows(true), StoreField: "-", Cols: integrationCols}
var catIntegrationAll = &catSource{Label: "Integration", Rows: integrationRows(false), StoreField: "-", Cols: integrationCols}

// orderSyncRows: each channel order the ERP took in (channel + Unicommerce mappings).
func orderSyncRows(tenantID, schema string, p map[string]string) ([]*catRow, error) {
	var out []*catRow
	for _, q := range []string{
		fmt.Sprintf(`SELECT order_id, COALESCE(channel, ''), COALESCE(channel_order_id, ''), 'Mapped', updated_at FROM %s.channel_order_mapping`, schema),
		fmt.Sprintf(`SELECT order_id, 'Unicommerce', COALESCE(channel_order_id, ''), COALESCE(status, ''), created_at FROM %s.unicommerce_order_mapping`, schema),
	} {
		rows, err := dbQuery(q + " LIMIT 20000")
		if err != nil {
			continue
		}
		for rows.Next() {
			var order, ch, chOrder, st string
			var at time.Time
			if rows.Scan(&order, &ch, &chOrder, &st, &at) == nil {
				out = append(out, &catRow{ID: order, Status: st, Created: at, H: map[string]interface{}{"order_id": order, "channel": ch, "channel_order": chOrder}})
			}
		}
		rows.Close()
	}
	return out, nil
}

var catOrderSync = &catSource{Label: "Order sync", Rows: orderSyncRows, StoreOf: func(r *catRow) string {
	return catStr(r.master("SalesOrder", r.hs("order_id"))["location_code"])
},
	Cols: mergeCols(ecomCols, map[string]catFn{
		"Created At":         func(r *catRow) interface{} { return catStamp(r.Created) },
		"Order ID":           func(r *catRow) interface{} { return firstNonEmpty(r.hs("channel_order"), r.hs("order_id")) },
		"Shipment ID":        func(r *catRow) interface{} { return ecomOrderPackage(r) },
		"Sales Channel":      catH("channel"),
		"Integration Status": func(r *catRow) interface{} { return r.Status },
		"Error Code":         catNotHeld,
		"Error Message":      catNotHeld,
		"Retry Count":        catConst(0.0),
		"Last Retry At":      func(r *catRow) interface{} { return catStamp(r.Created) },
	})}

var catOnlineReturns = &catSource{Label: "Online return", Doctype: "ReturnRequest", StoreField: "return_location", Lines: "items", Number: "code",
	SKU: "sku", Qty: "qty", Where: "status NOT IN ('Rejected', 'Cancelled')",
	Cols: mergeCols(returnCols, map[string]catFn{
		"Order ID": func(r *catRow) interface{} {
			o := r.master("SalesOrder", r.hs("original_order_id"))
			return firstNonEmpty(catStr(o["channel_order_id"]), r.hs("original_order_id"))
		},
		"Shipment ID": func(r *catRow) interface{} { return ecomOrderDocs(r)["p:"+r.hs("original_order_id")] },
		"Outlet Name": func(r *catRow) interface{} { return r.nameOf("Location", r.storeID()) },
		"Return Qty":  catLN("qty"),
		"Net Amount":  func(r *catRow) interface{} { n, _, _, _ := returnLineMoney(r); return n },
		"Status":      func(r *catRow) interface{} { return r.Status },
	})}

var catChannels = &catSource{Label: "Channel", Doctype: "Channel", StoreField: "location_code",
	Cols: map[string]catFn{
		"Outlet ID":        func(r *catRow) interface{} { return r.storeID() },
		"Outlet Name":      func(r *catRow) interface{} { return r.nameOf("Location", r.storeID()) },
		"UC Facility Code": func(r *catRow) interface{} { return firstNonEmpty(r.hs("facility_code"), r.hs("code")) },
		"Mapping Status": func(r *catRow) interface{} {
			if r.storeID() == "" {
				return "No outlet"
			}
			return "Mapped"
		},
		"Status": func(r *catRow) interface{} {
			if r.hv("enabled") == false || r.hs("enabled") == "No" {
				return "Disabled"
			}
			return r.Status
		},
	}}

var catOrders = &catSource{Label: "Sales order", Doctype: "SalesOrder", StoreField: "location_code",
	Cols: map[string]catFn{
		"Sales Channel":   catH("channel"),
		"UC Status":       func(r *catRow) interface{} { return ecomUC(r, r.ID) },
		"POS Bill Status": catNotHeld,
		"Invoice Status": func(r *catRow) interface{} {
			if inv := r.master("SalesInvoice", ecomOrderDocs(r)["i:"+r.ID]); inv != nil {
				return inv["_status"]
			}
			return "Not invoiced"
		},
		"Bill Count": catConst(1.0),
		"Qty":        func(r *catRow) interface{} { return ecomOrderQty(r) },
		"Net Amount": catHN("total_amount"),
	}}

func ecomOrderQty(r *catRow) float64 {
	m, ok := r.lk.extra["orderqty"].(map[string]float64)
	if !ok {
		m = map[string]float64{}
		rows, err := dbQuery(fmt.Sprintf(`SELECT data->>'order_id', SUM(COALESCE(NULLIF(data->>'qty', '')::numeric, 0)) FROM %s.documents
			WHERE doctype = 'SalesOrderLine' AND deleted_at IS NULL GROUP BY 1`, r.lk.schema))
		if err == nil {
			for rows.Next() {
				var o string
				var q float64
				if rows.Scan(&o, &q) == nil {
					m[o] = q
				}
			}
			rows.Close()
		}
		r.lk.extra["orderqty"] = m
	}
	return m[r.ID]
}

var _ = catWire("cat-e-commerce-sales-order-summary", &catReport{Src: catEcomPackages})
var _ = catWire("cat-e-commerce-sales-order-item-detail", &catReport{Src: catEcomLines})
var _ = catWire("cat-pending-sales-order-stock", &catReport{Src: catEcomLines, Keep: ecomOrderOpen})
var _ = catWire("cat-sales-order-integration-error", &catReport{Src: catIntegrationErrors})
var _ = catWire("cat-erp-vs-unicommerce-inventory-reconciliation-itemwise", &catReport{Src: catUCSync, NoDates: true})
var _ = catWire("cat-erp-vs-unicommerce-inventory-reconciliation-summary", &catReport{Src: catUCSync, NoDates: true,
	Group: []string{"As Of Date", "SKU", "Item Name"},
	Agg:   map[string]string{"Mismatch Outlet Count": "countd", "Latest ERP Sync Time": "max", "Mapping Status": "first"},
	Post: func(row map[string]interface{}) {
		if catNum(row["Variance Qty"]) == 0 && catNum(row["Mismatch Outlet Count"]) == 0 {
			row["Variance Status"] = "Matched"
		} else {
			row["Variance Status"] = "Mismatch"
		}
	}})
var _ = catWire("cat-inventory-sync-log", &catReport{Src: catUCSync, NoDates: true})
var _ = catWire("cat-order-sync-log", &catReport{Src: catOrderSync})
var _ = catWire("cat-marketplace-order-status-summary", &catReport{Src: catOrders,
	Group: []string{"From Date", "To Date", "Sales Channel", "UC Status", "POS Bill Status", "Invoice Status"}})
var _ = catWire("cat-online-return-register", &catReport{Src: catOnlineReturns,
	Keep: func(r *catRow) bool { return r.master("SalesOrder", r.hs("original_order_id")) != nil }})
var _ = catWire("cat-integration-retry-failure-audit", &catReport{Src: catIntegrationAll})
var _ = catWire("cat-outlet-facility-mapping-summary", &catReport{Src: catChannels, NoDates: true})
