package engines

import (
	"custom_erp/db"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Stage 148.27 - Promise date on order lines.
//
// A line's promise is stock on hand shipping today; what stock cannot cover
// waits for confirmed PO lines (148.11) and transfers in transit (the expected
// arrivals, 148.34). The dispatch day is today, or the next day after the
// store/channel cut-off hour, never a closed day; the courier days then count
// open days only. It reads and writes nothing else: no stock is reserved. All
// settings off or empty means no promise and no cut-off.

const (
	SettingPromiseDates   = "orders.promise_dates"
	SettingPromiseCourier = "orders.promise_courier_days"
	SettingPromiseClosed  = "orders.promise_closed_days"
	SettingPromiseCutoffs = "orders.promise_cutoff_hours"
)

func init() {
	zero, thirty := 0.0, 30.0
	RegisterSetting(SettingDefinition{Key: SettingPromiseDates, Module: "Orders", Label: "Promise date on order lines",
		Type: SettingTypeBool, Default: "false",
		Description: "On: each new order line gets a promise date and reason from stock, confirmed purchase orders and transfers in transit. Nothing is reserved."})
	RegisterSetting(SettingDefinition{Key: SettingPromiseCourier, Module: "Orders", Label: "Courier days", Type: SettingTypeInt,
		Default: "0", Unit: "days", Min: &zero, Max: &thirty, Description: "Open days added after dispatch."})
	RegisterSetting(SettingDefinition{Key: SettingPromiseClosed, Module: "Orders", Label: "Closed days", Type: SettingTypeString,
		Default: "", Description: "Weekdays and dates, for example: Sun, 2026-11-12. No dispatch or courier day lands on one."})
	RegisterSetting(SettingDefinition{Key: SettingPromiseCutoffs, Module: "Orders", Label: "Cut-off hours", Type: SettingTypeString,
		Default: "", Description: "Hour (0-23) per store or store/channel, for example: HO=15, HO/Shopify=12. After it, dispatch is the next day. Empty: no cut-off."})
	RegisterReport(ReportDefinition{ID: "late-promise-lines", Label: "Late promise lines", Category: "Sales",
		Columns: []ReportColumn{{Key: "sku", Label: "Item"}, {Key: "customer_name", Label: "Customer"}, {Key: "order_id", Label: "Order", Type: "doclink"},
			{Key: "qty", Label: "Qty"}, {Key: "promise_date", Label: "Promised"}, {Key: "days_late", Label: "Days late"}, {Key: "reason", Label: "Reason"}},
		Run: func(tenantID string, _ map[string]string) ([]map[string]interface{}, error) {
			return LatePromiseLines(tenantID)
		}})
}

type promiseArrival struct {
	Day   string
	Qty   float64
	Doc   string
	Kind  string
	Store string
}

type promiseInput struct {
	Qty         float64
	Stock       []LocationATS
	Arrivals    []promiseArrival
	Now         time.Time
	CourierDays int
	Closed      closedDays
	CutoffHour  int // -1: none
}

// PromiseResult is the date (empty when it cannot be given) and why.
type PromiseResult struct {
	Date   string `json:"promise_date"`
	Reason string `json:"promise_reason"`
}

type closedDays struct {
	weekdays map[time.Weekday]bool
	dates    map[string]bool
}

func parseClosedDays(s string) closedDays {
	c := closedDays{weekdays: map[time.Weekday]bool{}, dates: map[string]bool{}}
	for _, tok := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ';' || r == ' ' }) {
		if len(tok) == 10 {
			c.dates[tok] = true
			continue
		}
		for d := time.Sunday; d <= time.Saturday; d++ {
			if len(tok) >= 3 && strings.EqualFold(tok[:3], d.String()[:3]) {
				c.weekdays[d] = true
			}
		}
	}
	return c
}

func (c closedDays) closed(d time.Time) bool {
	return c.weekdays[d.Weekday()] || c.dates[d.Format("2006-01-02")]
}

// open moves a day forward to the first open one (bounded, so an all-closed setting ends).
func (c closedDays) open(d time.Time) time.Time {
	for i := 0; i < 400 && c.closed(d); i++ {
		d = d.AddDate(0, 0, 1)
	}
	return d
}

// cutoffFor reads "HO=15, HO/Shopify=12": store/channel first, then the store, then "/channel".
func cutoffFor(setting, store, channel string) int {
	m := map[string]int{}
	for _, p := range strings.FieldsFunc(setting, func(r rune) bool { return r == ',' || r == ';' }) {
		kv := strings.SplitN(p, "=", 2)
		if len(kv) != 2 {
			continue
		}
		if h, err := strconv.Atoi(strings.TrimSpace(kv[1])); err == nil && h >= 0 && h <= 23 {
			m[strings.ToLower(strings.TrimSpace(kv[0]))] = h
		}
	}
	s, c := strings.ToLower(store), strings.ToLower(channel)
	for _, k := range []string{s + "/" + c, s, "/" + c} {
		if k == "" || k == "/" {
			continue
		}
		if h, ok := m[k]; ok {
			return h
		}
	}
	return -1
}

func qtyText(q float64) string { return strconv.FormatFloat(q, 'f', -1, 64) }

// computePromise is the whole rule, on plain inputs.
func computePromise(in promiseInput) PromiseResult {
	today := time.Date(in.Now.Year(), in.Now.Month(), in.Now.Day(), 0, 0, 0, 0, in.Now.Location())
	dispatch := today
	if in.CutoffHour >= 0 && in.Now.Hour() >= in.CutoffHour {
		dispatch = dispatch.AddDate(0, 0, 1)
	}
	dispatch = in.Closed.open(dispatch)
	need := in.Qty
	var parts []string
	last := time.Time{}
	for _, s := range in.Stock {
		if need <= 0 {
			break
		}
		take := float64(s.ATS)
		if take > need {
			take = need
		}
		if take <= 0 {
			continue
		}
		need -= take
		parts = append(parts, fmt.Sprintf("%s in %s", qtyText(take), s.LocationCode))
		last = dispatch
	}
	arr := append([]promiseArrival(nil), in.Arrivals...)
	sort.SliceStable(arr, func(i, j int) bool { return arr[i].Day < arr[j].Day })
	for _, a := range arr {
		if need <= 0 {
			break
		}
		d, err := time.ParseInLocation("2006-01-02", a.Day, in.Now.Location())
		if err != nil || a.Qty <= 0 {
			continue
		}
		take := a.Qty
		if take > need {
			take = need
		}
		need -= take
		parts = append(parts, fmt.Sprintf("%s on %s due %s", qtyText(take), a.Doc, d.Format("2 Jan")))
		if d.Before(dispatch) {
			d = dispatch
		}
		if d = in.Closed.open(d); d.After(last) {
			last = d
		}
	}
	if need > 1e-9 {
		parts = append(parts, fmt.Sprintf("short %s, none due", qtyText(need)))
		return PromiseResult{Reason: strings.Join(parts, ", ")}
	}
	if last.IsZero() {
		return PromiseResult{}
	}
	for i := 0; i < in.CourierDays; i++ {
		last = in.Closed.open(last.AddDate(0, 0, 1))
	}
	return PromiseResult{Date: last.Format("2006-01-02"), Reason: strings.Join(parts, ", ")}
}

// PromiseForLine is the promise for a quantity of an item, from one store
// (location) or the whole network. Empty while the setting is off.
func PromiseForLine(tenantID, sku string, qty float64, location, channel string) (PromiseResult, error) {
	if !GetSettingBool(tenantID, SettingPromiseDates) || strings.TrimSpace(sku) == "" || qty <= 0 {
		return PromiseResult{}, nil
	}
	net, err := GetNetworkAvailableToSell(tenantID, sku)
	if err != nil {
		return PromiseResult{}, err
	}
	var stock []LocationATS
	if ls, ok := net["locations"].([]LocationATS); ok {
		for _, l := range ls {
			if location == "" || l.LocationCode == location {
				stock = append(stock, l)
			}
		}
	}
	rows, err := ExpectedArrivals(tenantID, "")
	if err != nil {
		return PromiseResult{}, err
	}
	var arrivals []promiseArrival
	for _, r := range rows {
		if strField(r, "sku") != sku || strField(r, "due") == "" {
			continue
		}
		if strField(r, "kind") == "Purchase order" && strField(r, "confirmation") != "Confirmed" {
			continue
		}
		if location != "" && strField(r, "store") != location {
			continue
		}
		pend, _ := r["pending"].(float64)
		arrivals = append(arrivals, promiseArrival{Day: strField(r, "due"), Qty: pend, Doc: strField(r, "doc_no"), Kind: strField(r, "kind"), Store: strField(r, "store")})
	}
	store := location
	if store == "" && len(stock) > 0 {
		store = stock[0].LocationCode
	}
	return computePromise(promiseInput{Qty: qty, Stock: stock, Arrivals: arrivals, Now: time.Now().In(BusinessLocation()),
		CourierDays: GetSettingInt(tenantID, SettingPromiseCourier),
		Closed:      parseClosedDays(GetSettingString(tenantID, SettingPromiseClosed)),
		CutoffHour:  cutoffFor(GetSettingString(tenantID, SettingPromiseCutoffs), store, channel)}), nil
}

// OrderPromise is an order's promise from its lines' promises (73.4.2.1): the
// latest line date, and only when every line has one. A line that cannot be
// promised leaves the order without a date and names the item. Empty while the
// setting is off (every line promise empty).
func OrderPromise(skus []string, lines []PromiseResult) PromiseResult {
	latest, any := "", false
	var short []string
	for i, p := range lines {
		if p.Date == "" && p.Reason == "" {
			continue
		}
		any = true
		if p.Date == "" {
			if i < len(skus) {
				short = append(short, skus[i])
			}
			continue
		}
		if p.Date > latest {
			latest = p.Date
		}
	}
	switch {
	case !any:
		return PromiseResult{}
	case len(short) > 0:
		return PromiseResult{Reason: "no date for " + strings.Join(short, ", ")}
	}
	return PromiseResult{Date: latest, Reason: "latest line"}
}

// LatePromiseLines lists open order lines whose stored promise day has passed.
func LatePromiseLines(tenantID string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rs, err := db.DB.Query(fmt.Sprintf(`SELECT l.data->>'sku', COALESCE(c.data->>'name', o.data->>'customer_id', ''), l.data->>'order_id',
			COALESCE((l.data->>'qty')::numeric, 0), l.data->>'promise_date', COALESCE(l.data->>'promise_reason', '')
		FROM %[1]s.documents l
		LEFT JOIN %[1]s.documents o ON o.id = l.data->>'order_id' AND o.doctype = 'SalesOrder'
		LEFT JOIN %[1]s.documents c ON c.id = o.data->>'customer_id' AND c.doctype = 'Customer'
		WHERE l.doctype = 'SalesOrderLine' AND l.deleted_at IS NULL
		  AND COALESCE(l.data->>'promise_date', '') <> '' AND l.data->>'promise_date' < $1
		  AND COALESCE(l.data->>'line_status', '') NOT IN ('Dispatched', 'Cancelled', 'Returned', 'Delivered', 'Shipped')
		ORDER BY l.data->>'promise_date', l.id`, schema), BusinessToday())
	if err != nil {
		return nil, err
	}
	defer rs.Close()
	today, _ := time.Parse("2006-01-02", BusinessToday())
	out := []map[string]interface{}{}
	for rs.Next() {
		var sku, cust, order, pd, reason string
		var qty float64
		if err := rs.Scan(&sku, &cust, &order, &qty, &pd, &reason); err != nil {
			return nil, err
		}
		late := 0
		if d, err := time.Parse("2006-01-02", pd); err == nil {
			late = int(today.Sub(d).Hours() / 24)
		}
		out = append(out, map[string]interface{}{"sku": sku, "customer_name": cust, "order_id": order, "order_id_doctype": "SalesOrder", "order_id_id": order,
			"qty": qty, "promise_date": pd, "days_late": late, "reason": reason})
	}
	return out, rs.Err()
}

// KeepServerSetFields148 stops a generic save from writing, or wiping, fields only
// the server sets: a line's promise (148.27) and a piece's hold (148.41). A save
// keeps what is stored; a new document carries none.
func KeepServerSetFields148(tenantID, doctype, id string, payload map[string]interface{}) {
	var keys []string
	switch doctype {
	case "SalesOrderLine":
		keys = []string{"promise_date", "promise_reason"}
	case "SerialNumber":
		keys = []string{"hold_customer", "hold_until", "hold_advance", "hold_note", "hold_by", "hold_at"}
	default:
		return
	}
	var prior map[string]interface{}
	if id != "" {
		prior, _, _ = fetchDocData(tenantID, doctype, id)
	}
	for _, k := range keys {
		delete(payload, k)
		if v, ok := prior[k]; ok {
			payload[k] = v
		}
	}
}
