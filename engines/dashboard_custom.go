package engines

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"custom_erp/db"
)

// Stage 100.2-100.3: custom dashboards - a grid of widgets, each a chart or
// a number over a dataset (dashboard_datasets.go) or a registered report,
// with dashboard-wide filters (D3), shared with users, roles or stores (D4)
// and refreshed while open (D5). Built on DashboardLayout (Stage 37.11) with
// additive fields - kind "custom" plus widgets / filters / shares - so the
// report-tile layouts and their digests keep working unchanged.

// DashboardWidget is one tile of a custom dashboard.
type DashboardWidget struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// Kind is how it is drawn: number, line, bar, stacked, donut, table, pivot.
	Kind string `json:"kind"`
	// Source is "dataset" (Query) or "report" (ReportID + ReportParams).
	Source       string            `json:"source"`
	Query        *DatasetQuery     `json:"query,omitempty"`
	ReportID     string            `json:"report_id,omitempty"`
	ReportParams map[string]string `json:"report_params,omitempty"`
	// OwnFilters: the widget keeps its own dates and store instead of the
	// dashboard's (D3, "a widget may fix its own").
	OwnFilters bool `json:"own_filters,omitempty"`
	// Grid placement: 12 columns; W 3-12, H 1-4 rows.
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

// DashboardFilters are a dashboard's own filters (D3). Range is today,
// week, mtd, last30, month (last month) or custom (From / To).
type DashboardFilters struct {
	Range    string `json:"range,omitempty"`
	From     string `json:"from,omitempty"`
	To       string `json:"to,omitempty"`
	Store    string `json:"store,omitempty"`
	Brand    string `json:"brand,omitempty"`
	Category string `json:"category,omitempty"`
	Channel  string `json:"channel,omitempty"`
}

// CustomDashboard is one saved (or ready-made) dashboard.
type CustomDashboard struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Owner       string            `json:"owner"`
	OwnerName   string            `json:"owner_name,omitempty"`
	Widgets     []DashboardWidget `json:"widgets"`
	Filters     DashboardFilters  `json:"filters"`
	ShareUsers  []string          `json:"share_users"`
	ShareRoles  []string          `json:"share_roles"`
	ShareStores []string          `json:"share_stores"`
	HomeRoles   []string          `json:"home_roles"`
	// RefreshMinutes: reloaded this often while open (D5; 0 = the setting).
	RefreshMinutes int    `json:"refresh_minutes,omitempty"`
	ReadyMade      string `json:"ready_made,omitempty"` // the ready-made key, for a shipped one or a copy
	Mine           bool   `json:"mine"`
	CanEdit        bool   `json:"can_edit"`
}

const customDashboardKind = "custom"

var dashboardWidgetKinds = map[string]bool{"number": true, "line": true, "bar": true, "stacked": true, "donut": true, "table": true, "pivot": true}

func dashboardError(msg string) error {
	return &ValidationError{Code: "REPORT-0989", Message: msg}
}

// validateCustomDashboard checks a dashboard before it is saved.
func validateCustomDashboard(cd *CustomDashboard) error {
	cd.Name = strings.TrimSpace(cd.Name)
	if cd.Name == "" {
		return dashboardError("give the dashboard a name")
	}
	if len(cd.Widgets) == 0 {
		return dashboardError("add at least one widget")
	}
	if len(cd.Widgets) > 40 {
		return dashboardError("a dashboard holds at most 40 widgets")
	}
	for i := range cd.Widgets {
		w := &cd.Widgets[i]
		if w.ID == "" {
			w.ID = fmt.Sprintf("w%d", i+1)
		}
		if !dashboardWidgetKinds[w.Kind] {
			return dashboardError(fmt.Sprintf("%q is not a widget type", w.Kind))
		}
		switch w.Source {
		case "dataset":
			if w.Query == nil {
				return dashboardError(fmt.Sprintf("widget %q has no data", w.Title))
			}
			if _, ok := DashboardDatasetByKey(w.Query.Dataset); !ok {
				return dashboardError(fmt.Sprintf("widget %q reads %q, which is not a dataset", w.Title, w.Query.Dataset))
			}
		case "report":
			if _, ok := reportRegistry[w.ReportID]; !ok {
				return dashboardError(fmt.Sprintf("widget %q reads report %q, which does not exist", w.Title, w.ReportID))
			}
		default:
			return dashboardError(fmt.Sprintf("widget %q needs a dataset or a report", w.Title))
		}
		w.W = clampInt(w.W, 3, 12, 6)
		w.H = clampInt(w.H, 1, 4, 2)
		w.X = clampInt(w.X, 0, 12-w.W, 0)
		if w.Y < 0 {
			w.Y = 0
		}
	}
	if cd.RefreshMinutes < 0 || cd.RefreshMinutes > 120 {
		cd.RefreshMinutes = 0
	}
	cd.ShareUsers, cd.ShareRoles, cd.ShareStores, cd.HomeRoles = cleanList(cd.ShareUsers), cleanList(cd.ShareRoles), cleanList(cd.ShareStores), cleanList(cd.HomeRoles)
	return nil
}

func clampInt(v, lo, hi, def int) int {
	if v == 0 && def != 0 {
		v = def
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func cleanList(in []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" && !seen[strings.ToLower(s)] {
			seen[strings.ToLower(s)] = true
			out = append(out, s)
		}
	}
	return out
}

// SaveCustomDashboard creates (no ID) or updates (the owner's, or any for a
// role with full access) a dashboard.
func SaveCustomDashboard(tenantID, userID, username, role string, cd CustomDashboard) (string, error) {
	if err := validateCustomDashboard(&cd); err != nil {
		return "", err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	widgets, _ := json.Marshal(cd.Widgets)
	filters, _ := json.Marshal(cd.Filters)
	data := map[string]interface{}{
		"kind": customDashboardKind, "name": cd.Name, "role": "", "tiles_json": "[]",
		"widgets_json": string(widgets), "filters_json": string(filters),
		"share_users": cd.ShareUsers, "share_roles": cd.ShareRoles, "share_stores": cd.ShareStores, "home_roles": cd.HomeRoles,
		"refresh_minutes": cd.RefreshMinutes, "ready_made": cd.ReadyMade,
	}
	if cd.ID == "" || strings.HasPrefix(cd.ID, readyMadePrefix) {
		id := NewDocID("DBL")
		data["code"], data["owner"], data["owner_name"] = id, userID, username
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'DashboardLayout', $2, 'Active', 'system')`, schema), id, raw); err != nil {
			return "", err
		}
		LogAuditEvent(tenantID, username, "DASHBOARD_SAVED", "Success", fmt.Sprintf("dashboard %s %q created", id, cd.Name))
		return id, nil
	}
	cur, err := getCustomDashboard(schema, cd.ID)
	if err != nil {
		return "", err
	}
	if cur.Owner != userID && !HasFullAccess(role) {
		return "", dashboardError("only the person who made this dashboard can change it - copy it to make your own")
	}
	data["code"], data["owner"], data["owner_name"] = cd.ID, cur.Owner, cur.OwnerName
	raw, _ := json.Marshal(data)
	if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $2, updated_at = CURRENT_TIMESTAMP WHERE id = $1 AND doctype = 'DashboardLayout' AND deleted_at IS NULL`, schema), cd.ID, raw); err != nil {
		return "", err
	}
	LogAuditEvent(tenantID, username, "DASHBOARD_SAVED", "Success", fmt.Sprintf("dashboard %s %q changed", cd.ID, cd.Name))
	return cd.ID, nil
}

func strList(v interface{}) []string {
	out := []string{}
	switch x := v.(type) {
	case []interface{}:
		for _, s := range x {
			if str, ok := s.(string); ok && str != "" {
				out = append(out, str)
			}
		}
	case string:
		for _, s := range strings.Split(x, ",") {
			if s = strings.TrimSpace(s); s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

func customDashboardFromData(id string, data map[string]interface{}) CustomDashboard {
	cd := CustomDashboard{ID: id, Name: strField(data, "name"), Owner: strField(data, "owner"), OwnerName: strField(data, "owner_name"),
		ShareUsers: strList(data["share_users"]), ShareRoles: strList(data["share_roles"]), ShareStores: strList(data["share_stores"]),
		HomeRoles: strList(data["home_roles"]), ReadyMade: strField(data, "ready_made"), RefreshMinutes: int(numFromInterface(data["refresh_minutes"]))}
	_ = json.Unmarshal([]byte(strField(data, "widgets_json")), &cd.Widgets)
	_ = json.Unmarshal([]byte(strField(data, "filters_json")), &cd.Filters)
	if cd.Widgets == nil {
		cd.Widgets = []DashboardWidget{}
	}
	return cd
}

func getCustomDashboard(schema, id string) (CustomDashboard, error) {
	var raw string
	err := db.DB.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents WHERE id = $1 AND doctype = 'DashboardLayout' AND deleted_at IS NULL AND data->>'kind' = $2`, schema), id, customDashboardKind).Scan(&raw)
	if err != nil {
		return CustomDashboard{}, dashboardError(fmt.Sprintf("dashboard %s was not found", id))
	}
	var data map[string]interface{}
	_ = json.Unmarshal([]byte(raw), &data)
	return customDashboardFromData(id, data), nil
}

func containsFold(list []string, v string) bool {
	for _, s := range list {
		if strings.EqualFold(strings.TrimSpace(s), strings.TrimSpace(v)) {
			return true
		}
	}
	return false
}

// dashboardVisibleTo: the owner's, shared with the user, their role or one of
// their stores, or set as their role's home.
func dashboardVisibleTo(cd CustomDashboard, userID, username, role string, access StoreAccess) bool {
	if cd.Owner == userID || HasFullAccess(role) {
		return true
	}
	if containsFold(cd.ShareUsers, username) || containsFold(cd.ShareUsers, userID) || containsFold(cd.ShareRoles, role) || containsFold(cd.HomeRoles, role) {
		return true
	}
	for _, s := range cd.ShareStores {
		if access.Allows(s) {
			return true
		}
	}
	return false
}

// ListCustomDashboards is every dashboard the viewer may open, the
// ready-made ones last.
func ListCustomDashboards(tenantID, userID, username, role string) ([]CustomDashboard, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	access, _ := UserStoreAccess(tenantID, userID)
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, data FROM %s.documents WHERE doctype = 'DashboardLayout' AND deleted_at IS NULL
		AND status = 'Active' AND data->>'kind' = $1 ORDER BY data->>'name'`, schema), customDashboardKind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CustomDashboard{}
	for rows.Next() {
		var id, raw string
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, err
		}
		var data map[string]interface{}
		_ = json.Unmarshal([]byte(raw), &data)
		cd := customDashboardFromData(id, data)
		if !dashboardVisibleTo(cd, userID, username, role, access) {
			continue
		}
		cd.Mine = cd.Owner == userID
		cd.CanEdit = cd.Mine || HasFullAccess(role)
		out = append(out, cd)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, rm := range ReadyMadeDashboards() {
		out = append(out, rm)
	}
	return out, nil
}

// GetCustomDashboardFor is one dashboard (or a ready-made one) if the viewer
// may open it.
func GetCustomDashboardFor(tenantID, userID, username, role, id string) (CustomDashboard, error) {
	if strings.HasPrefix(id, readyMadePrefix) {
		for _, rm := range ReadyMadeDashboards() {
			if rm.ID == id {
				return rm, nil
			}
		}
		return CustomDashboard{}, dashboardError(fmt.Sprintf("dashboard %s was not found", id))
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return CustomDashboard{}, err
	}
	cd, err := getCustomDashboard(schema, id)
	if err != nil {
		return cd, err
	}
	access, _ := UserStoreAccess(tenantID, userID)
	if !dashboardVisibleTo(cd, userID, username, role, access) {
		return CustomDashboard{}, dashboardError(fmt.Sprintf("dashboard %s was not found", id))
	}
	cd.Mine = cd.Owner == userID
	cd.CanEdit = cd.Mine || HasFullAccess(role)
	return cd, nil
}

// CopyCustomDashboard saves a copy the viewer owns (unshared, not a home).
func CopyCustomDashboard(tenantID, userID, username, role, id string) (string, error) {
	cd, err := GetCustomDashboardFor(tenantID, userID, username, role, id)
	if err != nil {
		return "", err
	}
	cd.ID, cd.Name = "", "Copy of "+cd.Name
	cd.ShareUsers, cd.ShareRoles, cd.ShareStores, cd.HomeRoles = nil, nil, nil, nil
	return SaveCustomDashboard(tenantID, userID, username, role, cd)
}

// DeleteCustomDashboard removes the owner's dashboard (or any, for full access).
func DeleteCustomDashboard(tenantID, userID, role, id string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	cd, err := getCustomDashboard(schema, id)
	if err != nil {
		return err
	}
	if cd.Owner != userID && !HasFullAccess(role) {
		return dashboardError("only the person who made this dashboard can delete it")
	}
	_, err = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET deleted_at = CURRENT_TIMESTAMP WHERE id = $1 AND doctype = 'DashboardLayout'`, schema), id)
	return err
}

// HomeDashboardFor is the dashboard set as the viewer's role's home screen
// (100.3.2), if any - the newest one when several name the role.
func HomeDashboardFor(tenantID, userID, username, role string) (*CustomDashboard, error) {
	list, err := ListCustomDashboards(tenantID, userID, username, role)
	if err != nil {
		return nil, err
	}
	for i := range list {
		if containsFold(list[i].HomeRoles, role) {
			return &list[i], nil
		}
	}
	return nil, nil
}

// ResolveDashboardRange turns a dashboard's range into dates (YYYY-MM-DD).
func ResolveDashboardRange(f DashboardFilters, now time.Time) (from, to string) {
	day := func(t time.Time) string { return t.Format("2006-01-02") }
	switch f.Range {
	case "today":
		return day(now), day(now)
	case "week":
		wd := (int(now.Weekday()) + 6) % 7 // Monday first
		return day(now.AddDate(0, 0, -wd)), day(now)
	case "mtd", "":
		return day(time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())), day(now)
	case "last30":
		return day(now.AddDate(0, 0, -29)), day(now)
	case "month":
		first := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		return day(first.AddDate(0, -1, 0)), day(first.AddDate(0, 0, -1))
	case "custom":
		return f.From, f.To
	case "all":
		return "", ""
	}
	return "", ""
}

// ApplyDashboardFilters puts a dashboard's filters on one widget's query,
// unless the widget keeps its own (D3). A filter the dataset has no field
// for is skipped.
func ApplyDashboardFilters(q DatasetQuery, f DashboardFilters, own bool, now time.Time) DatasetQuery {
	if own {
		return q
	}
	ds, ok := DashboardDatasetByKey(q.Dataset)
	if !ok {
		return q
	}
	if ds.DateField != "" {
		q.DateFrom, q.DateTo = ResolveDashboardRange(f, now)
	}
	if f.Store != "" && len(ds.stores) > 0 {
		q.Store = f.Store
	}
	add := func(field, value string) {
		if value == "" {
			return
		}
		if _, ok := ds.field(field); ok {
			q.Filters = append(append([]DatasetFilter{}, q.Filters...), DatasetFilter{Field: field, Op: "eq", Value: value})
		}
	}
	add("brand", f.Brand)
	add("category", f.Category)
	add("channel", f.Channel)
	return q
}

// --- ready-made dashboards (100.3.3) ----------------------------------------

const readyMadePrefix = "ready:"

func dsW(title, kind string, x, y, w, h int, q DatasetQuery) DashboardWidget {
	return DashboardWidget{ID: strings.ToLower(strings.ReplaceAll(title, " ", "-")), Title: title, Kind: kind, Source: "dataset", Query: &q, X: x, Y: y, W: w, H: h}
}

func dsSum(field string) DatasetMeasure { return DatasetMeasure{Field: field, Agg: "sum"} }

var countRows = DatasetMeasure{Agg: "count"}

// ReadyMadeDashboards are the shipped ones: Sales, Stock, Buying, Customers
// and Money. Each opens as it is and copies into an editable one of your own.
func ReadyMadeDashboards() []CustomDashboard {
	mk := func(key, name string, f DashboardFilters, widgets ...DashboardWidget) CustomDashboard {
		return CustomDashboard{ID: readyMadePrefix + key, Name: name, Owner: "", ReadyMade: key, Filters: f, Widgets: widgets,
			ShareUsers: []string{}, ShareRoles: []string{}, ShareStores: []string{}, HomeRoles: []string{}}
	}
	mtd := DashboardFilters{Range: "mtd"}
	return []CustomDashboard{
		mk("sales", "Sales", mtd,
			dsW("Sales", "number", 0, 0, 3, 1, DatasetQuery{Dataset: "pos_bills", Measures: []DatasetMeasure{dsSum("value")}, Compare: true}),
			dsW("Bills", "number", 3, 0, 3, 1, DatasetQuery{Dataset: "pos_bills", Measures: []DatasetMeasure{countRows}, Compare: true}),
			dsW("Average bill", "number", 6, 0, 3, 1, DatasetQuery{Dataset: "pos_bills", Measures: []DatasetMeasure{{Field: "value", Agg: "avg"}}, Compare: true}),
			dsW("Items per bill", "number", 9, 0, 3, 1, DatasetQuery{Dataset: "pos_bills", Measures: []DatasetMeasure{{Field: "items", Agg: "avg"}}, Compare: true}),
			dsW("Sales by day", "line", 0, 1, 8, 2, DatasetQuery{Dataset: "pos_bills", GroupBy: []string{"date"}, Grain: "day", Measures: []DatasetMeasure{dsSum("value")}, Limit: 92}),
			dsW("Sales by store", "bar", 8, 1, 4, 2, DatasetQuery{Dataset: "pos_bills", GroupBy: []string{"store"}, Measures: []DatasetMeasure{dsSum("value")}, Limit: 12}),
			dsW("Sales by category", "donut", 0, 3, 4, 2, DatasetQuery{Dataset: "sales_lines", GroupBy: []string{"category"}, Measures: []DatasetMeasure{dsSum("value")}, Limit: 8}),
			dsW("Top salesmen", "bar", 4, 3, 4, 2, DatasetQuery{Dataset: "pos_bills", GroupBy: []string{"salesman"}, Measures: []DatasetMeasure{dsSum("value"), countRows}, Limit: 10}),
			dsW("Sales by hour", "bar", 8, 3, 4, 2, DatasetQuery{Dataset: "pos_bills", GroupBy: []string{"hour"}, Measures: []DatasetMeasure{dsSum("value")}, Sort: "group", Limit: 24}),
			dsW("Top items", "table", 0, 5, 6, 2, DatasetQuery{Dataset: "sales_lines", GroupBy: []string{"item"}, Measures: []DatasetMeasure{dsSum("qty"), dsSum("value")}, Limit: 15}),
			dsW("Brand by store", "pivot", 6, 5, 6, 2, DatasetQuery{Dataset: "sales_lines", GroupBy: []string{"brand", "store"}, Measures: []DatasetMeasure{dsSum("value")}, Limit: 200}),
		),
		mk("stock", "Stock", DashboardFilters{},
			dsW("Stock value (cost)", "number", 0, 0, 4, 1, DatasetQuery{Dataset: "stock_on_hand", Measures: []DatasetMeasure{dsSum("value")}}),
			dsW("Units on hand", "number", 4, 0, 4, 1, DatasetQuery{Dataset: "stock_on_hand", Measures: []DatasetMeasure{dsSum("on_hand")}}),
			dsW("Items low or out", "number", 8, 0, 4, 1, DatasetQuery{Dataset: "stock_on_hand", Measures: []DatasetMeasure{countRows},
				Filters: []DatasetFilter{{Field: "stock_level", Op: "in", Value: "Low,Out of stock"}}}),
			dsW("Stock value by store", "bar", 0, 1, 6, 2, DatasetQuery{Dataset: "stock_on_hand", GroupBy: []string{"store"}, Measures: []DatasetMeasure{dsSum("value")}, Limit: 15}),
			dsW("Stock by category", "donut", 6, 1, 6, 2, DatasetQuery{Dataset: "stock_on_hand", GroupBy: []string{"category"}, Measures: []DatasetMeasure{dsSum("on_hand")}, Limit: 8}),
			dsW("Stock level", "stacked", 0, 3, 6, 2, DatasetQuery{Dataset: "stock_on_hand", GroupBy: []string{"store", "stock_level"}, Measures: []DatasetMeasure{countRows}, Limit: 200}),
			dsW("Slowest stock (days since it last moved)", "table", 6, 3, 6, 2, DatasetQuery{Dataset: "stock_on_hand", GroupBy: []string{"item"},
				Measures: []DatasetMeasure{{Field: "days_still", Agg: "max"}, dsSum("on_hand")}, Filters: []DatasetFilter{{Field: "on_hand", Op: "gte", Value: "1"}}, Limit: 15}),
			dsW("Sell-through: units sold by category (this month)", "bar", 0, 5, 12, 2, DatasetQuery{Dataset: "sales_lines", GroupBy: []string{"category"}, Measures: []DatasetMeasure{dsSum("qty")}, Limit: 12}),
		),
		mk("buying", "Buying", mtd,
			dsW("Purchase orders", "number", 0, 0, 4, 1, DatasetQuery{Dataset: "purchase_orders", Measures: []DatasetMeasure{countRows}, Compare: true}),
			dsW("Ordered value", "number", 4, 0, 4, 1, DatasetQuery{Dataset: "purchase_orders", Measures: []DatasetMeasure{dsSum("value")}, Compare: true}),
			dsW("Received value", "number", 8, 0, 4, 1, DatasetQuery{Dataset: "goods_receipts", Measures: []DatasetMeasure{dsSum("value")}, Compare: true}),
			dsW("Open purchase orders by vendor", "bar", 0, 1, 6, 2, DatasetQuery{Dataset: "purchase_orders", GroupBy: []string{"vendor"}, Measures: []DatasetMeasure{dsSum("value")},
				Filters: []DatasetFilter{{Field: "status", Op: "in", Value: "Approved,Pending Approval,Draft,Partially Received"}}, Limit: 12}),
			dsW("Orders by status", "donut", 6, 1, 6, 2, DatasetQuery{Dataset: "purchase_orders", GroupBy: []string{"status"}, Measures: []DatasetMeasure{countRows}, Limit: 8}),
			dsW("Ordered and received by vendor (fill)", "table", 0, 3, 12, 2, DatasetQuery{Dataset: "goods_receipts", GroupBy: []string{"vendor"}, Measures: []DatasetMeasure{countRows, dsSum("value")}, Limit: 20}),
		),
		mk("customers", "Customers", mtd,
			dsW("New customers", "number", 0, 0, 4, 1, DatasetQuery{Dataset: "customers", Measures: []DatasetMeasure{countRows}, Compare: true}),
			dsW("Bills by new and repeat customers", "donut", 4, 0, 4, 2, DatasetQuery{Dataset: "pos_bills", GroupBy: []string{"customer_type"}, Measures: []DatasetMeasure{countRows}}),
			dsW("Points earned and used", "bar", 8, 0, 4, 2, DatasetQuery{Dataset: "loyalty", GroupBy: []string{"type"}, Measures: []DatasetMeasure{dsSum("points")}}),
			dsW("New customers by store", "bar", 0, 1, 4, 2, DatasetQuery{Dataset: "customers", GroupBy: []string{"store"}, Measures: []DatasetMeasure{countRows}, Limit: 12}),
			dsW("Customers by tier", "table", 0, 3, 12, 2, DatasetQuery{Dataset: "customers", GroupBy: []string{"tier"}, Measures: []DatasetMeasure{countRows}, Limit: 12}),
		),
		// Balances to date (Range all): cash / bank / card / UPI clearing, what
		// customers owe (1300) and what is owed to vendors for goods received
		// (2100, cleared by their bills and payments).
		mk("money", "Money", DashboardFilters{Range: "all"},
			dsW("Cash and bank", "number", 0, 0, 4, 1, DatasetQuery{Dataset: "ledger", Measures: []DatasetMeasure{dsSum("net")},
				Filters: []DatasetFilter{{Field: "account_code", Op: "in", Value: "1100,1101,1102"}}}),
			dsW("Owed by customers", "number", 4, 0, 4, 1, DatasetQuery{Dataset: "ledger", Measures: []DatasetMeasure{dsSum("net")},
				Filters: []DatasetFilter{{Field: "account_code", Op: "eq", Value: "1300"}}}),
			dsW("Owed to vendors", "number", 8, 0, 4, 1, DatasetQuery{Dataset: "ledger", Measures: []DatasetMeasure{dsSum("credit_net")},
				Filters: []DatasetFilter{{Field: "account_code", Op: "eq", Value: "2100"}}}),
			dsW("Postings by account type", "bar", 0, 1, 6, 2, DatasetQuery{Dataset: "ledger", GroupBy: []string{"account_type"}, Measures: []DatasetMeasure{dsSum("debit"), dsSum("credit")}, Limit: 12}),
			dsW("Expenses by category", "donut", 6, 1, 6, 2, DatasetQuery{Dataset: "expenses", GroupBy: []string{"category"}, Measures: []DatasetMeasure{dsSum("amount")}, Limit: 8}),
			dsW("Accounts", "table", 0, 3, 12, 2, DatasetQuery{Dataset: "ledger", GroupBy: []string{"account"}, Measures: []DatasetMeasure{dsSum("debit"), dsSum("credit"), dsSum("net")}, Limit: 30}),
		),
	}
}

// ReadyMadeKeys is the shipped keys in order (tests).
func ReadyMadeKeys() []string {
	out := []string{}
	for _, d := range ReadyMadeDashboards() {
		out = append(out, d.ReadyMade)
	}
	sort.Strings(out)
	return out
}

// SettingDashboardRefresh is how often an open dashboard reloads (D5).
const SettingDashboardRefresh = "reports.dashboard_refresh_minutes"

func init() {
	RegisterSetting(SettingDefinition{
		Key: SettingDashboardRefresh, Module: "Reports",
		Label: "Dashboard refresh", Type: SettingTypeInt, Default: "5", Unit: "minutes",
		Min: settingBound(1), Max: settingBound(120),
		Description: "How often an open custom dashboard reloads its figures (Stage 100). Each result is also kept 60 seconds on the server, so many people opening one dashboard run it once.",
	})
}

// ScheduleCustomDashboardDigest emails a dashboard on a schedule (100.3.4):
// a DashboardDigest run as the caller - their stores, their role's money
// rule - starting on firstRun (default tomorrow).
func ScheduleCustomDashboardDigest(tenantID, userID, username, role, dashboardID, frequency, email, firstRun string) (string, error) {
	if _, err := GetCustomDashboardFor(tenantID, userID, username, role, dashboardID); err != nil {
		return "", err
	}
	if strings.HasPrefix(dashboardID, readyMadePrefix) {
		return "", dashboardError("copy the ready-made dashboard first, then email your copy")
	}
	switch frequency {
	case "Daily", "Weekly", "Monthly":
	default:
		return "", dashboardError("send it Daily, Weekly or Monthly")
	}
	if _, err := MailRecipients(email); err != nil {
		return "", err
	}
	if firstRun == "" {
		firstRun = time.Now().AddDate(0, 0, 1).Format("2006-01-02")
	} else if _, err := time.Parse("2006-01-02", firstRun); err != nil {
		return "", dashboardError("the first date must be YYYY-MM-DD")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	id := NewDocID("DGT")
	raw, _ := json.Marshal(map[string]interface{}{"code": id, "dashboard_layout_id": dashboardID, "frequency": frequency,
		"requested_role": role, "recipient_email": strings.TrimSpace(email), "next_run_date": firstRun, "status": "Active"})
	if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'DashboardDigest', $2, 'Active', $3)`, schema), id, raw, userID); err != nil {
		return "", err
	}
	LogAuditEvent(tenantID, username, "DASHBOARD_DIGEST_SCHEDULED", "Success", fmt.Sprintf("dashboard %s emailed %s from %s", dashboardID, frequency, firstRun))
	return id, nil
}
