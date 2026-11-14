package engines

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"custom_erp/db"
)

// Markdown calendar: a MarkdownPlan lowers the price of some items (or a whole
// category) between two dates, for some stores or all of them. The item master
// price is never rewritten: the plan is read at price time, so the price goes
// back by itself the day after the plan ends, with no job to run.
//
// Stored status is Draft, Scheduled or Cancelled; Running and Ended are what a
// Scheduled plan looks like inside and after its dates (MarkdownPlanState), so
// nothing has to flip a status at midnight. Dates are the client's local day
// (BusinessToday), not the database's UTC day.
//
// Overlap rule: when several plans cover the same item and store on the same
// day, the lowest resulting price wins. Overlaps are allowed, never refused.

// Error codes PIM-9250 to PIM-9259.
const markdownCodeShape = "PIM-9250"

const (
	MarkdownDraft     = "Draft"
	MarkdownScheduled = "Scheduled"
	MarkdownRunning   = "Running"
	MarkdownEnded     = "Ended"
	MarkdownCancelled = "Cancelled"
)

// MarkdownPlanState is the status a person sees on a given local day.
func MarkdownPlanState(status, from, to, day string) string {
	switch status {
	case MarkdownDraft, MarkdownCancelled:
		return status
	}
	switch {
	case to != "" && to < day:
		return MarkdownEnded
	case from != "" && from > day:
		return MarkdownScheduled
	}
	return MarkdownRunning
}

func csvHas(list []string, v string) bool {
	for _, p := range list {
		if strings.EqualFold(p, v) {
			return true
		}
	}
	return false
}

type markdownPlan struct {
	ID, Name, Category, From, To, Status string
	Items, Stores                        []string
	Percent, Fixed                       float64
}

func loadMarkdownPlans(tenantID string) ([]markdownPlan, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rs, err := db.DB.Query(fmt.Sprintf(`SELECT id, COALESCE(data->>'name',''), COALESCE(data->>'items',''), COALESCE(data->>'category',''),
		COALESCE(data->>'percent_off',''), COALESCE(data->>'fixed_price',''), COALESCE(data->>'start_date',''), COALESCE(data->>'end_date',''),
		COALESCE(data->>'stores',''), COALESCE(data->>'status','')
		FROM %s.documents WHERE doctype = 'MarkdownPlan' AND deleted_at IS NULL`, schema))
	if err != nil {
		return nil, err
	}
	defer rs.Close()
	var out []markdownPlan
	for rs.Next() {
		var p markdownPlan
		var items, stores, pct, fixed string
		if err := rs.Scan(&p.ID, &p.Name, &items, &p.Category, &pct, &fixed, &p.From, &p.To, &stores, &p.Status); err != nil {
			return nil, err
		}
		p.Items, p.Stores = csvList(items), csvList(stores)
		p.Percent, _ = strconv.ParseFloat(pct, 64)
		p.Fixed, _ = strconv.ParseFloat(fixed, 64)
		out = append(out, p)
	}
	return out, rs.Err()
}

// price is what the plan makes of base, or 0 when it does not lower it.
func (p markdownPlan) price(base float64) float64 {
	var v float64
	switch {
	case p.Fixed > 0:
		v = p.Fixed
	case p.Percent > 0 && p.Percent < 100:
		v = base * (1 - p.Percent/100)
	default:
		return 0
	}
	if v = round2(v); v > 0 && v < base {
		return v
	}
	return 0
}

// MarkdownPrice is the marked-down price of sku at store on day (YYYY-MM-DD)
// when a Scheduled plan covers it, or ok=false and the base stays. store may be
// empty (a channel order): only plans with no store list apply then.
func MarkdownPrice(tenantID, sku, store, day string, base float64) (price float64, ok bool) {
	if base <= 0 || strings.TrimSpace(sku) == "" {
		return 0, false
	}
	plans, err := loadMarkdownPlans(tenantID)
	if err != nil || len(plans) == 0 {
		return 0, false
	}
	var category string
	var keys []string
	for _, p := range plans {
		if p.Status != MarkdownScheduled || (p.From != "" && p.From > day) || (p.To != "" && p.To < day) {
			continue
		}
		if len(p.Stores) > 0 {
			if keys == nil {
				keys = storeKeys(tenantID, store)
			}
			hit := false
			for _, k := range keys {
				if csvHas(p.Stores, k) {
					hit = true
					break
				}
			}
			if !hit {
				continue
			}
		}
		covered := csvHas(p.Items, sku)
		if !covered && p.Category != "" {
			if category == "" {
				if r, rerr := ResolveItemBySKU(tenantID, sku); rerr == nil {
					category = strField(r.Data, "category")
				}
			}
			covered = category != "" && strings.EqualFold(category, p.Category)
		}
		if !covered {
			continue
		}
		if v := p.price(base); v > 0 && (!ok || v < price) {
			price, ok = v, true
		}
	}
	return price, ok
}

// fillMarkdownRates gives a sales order line with no rate its marked-down item
// price, when a plan covers it. A line that already has a rate is untouched.
func fillMarkdownRates(tenantID string, lines []SalesOrderLineInput) {
	day := BusinessToday()
	for i := range lines {
		if lines[i].UnitPrice > 0 {
			continue
		}
		sale, mrp := itemMasterPrices(tenantID, lines[i].SKU)
		base := sale
		if base <= 0 {
			base = mrp
		}
		if p, ok := MarkdownPrice(tenantID, lines[i].SKU, "", day, base); ok {
			lines[i].UnitPrice = p
		}
	}
}

// ValidateMarkdownPlan is the save check of a MarkdownPlan.
func ValidateMarkdownPlan(payload map[string]interface{}) error {
	bad := func(field, msg string) error {
		return &ValidationError{Code: markdownCodeShape, SubFor: field, Message: msg}
	}
	if strField(payload, "name") == "" {
		return bad("name", "give the plan a name")
	}
	if (strField(payload, "items") == "") == (strField(payload, "category") == "") {
		return bad("items", "name items or a category, not both and not neither")
	}
	pct, _ := parityNumber(payload["percent_off"])
	fixed, _ := parityNumber(payload["fixed_price"])
	if (pct > 0) == (fixed > 0) {
		return bad("percent_off", "give a percent off or a fixed price, not both and not neither")
	}
	if pct >= 100 || pct < 0 || fixed < 0 {
		return bad("percent_off", "the percent is between 0 and 100 and the price is positive")
	}
	from, to := strField(payload, "start_date"), strField(payload, "end_date")
	for _, d := range []string{from, to} {
		if _, err := time.Parse("2006-01-02", d); err != nil {
			return bad("start_date", "dates are written YYYY-MM-DD")
		}
	}
	if to < from {
		return bad("end_date", "the end date is before the start date")
	}
	return nil
}

// MarkdownCalendarRow is one plan as the calendar shows it.
type MarkdownCalendarRow struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Month    string `json:"month"`
	Start    string `json:"start_date"`
	End      string `json:"end_date"`
	State    string `json:"state"`
	Scope    string `json:"scope"`
	Stores   string `json:"stores"`
	Discount string `json:"discount"`
}

// MarkdownCalendar lists every plan, upcoming and running first, then ended.
func MarkdownCalendar(tenantID string) ([]MarkdownCalendarRow, error) {
	plans, err := loadMarkdownPlans(tenantID)
	if err != nil {
		return nil, err
	}
	day := BusinessToday()
	rows := []MarkdownCalendarRow{}
	rank := func(s string) int {
		switch s {
		case MarkdownRunning:
			return 0
		case MarkdownScheduled:
			return 1
		case MarkdownDraft:
			return 2
		}
		return 3
	}
	for _, p := range plans {
		r := MarkdownCalendarRow{ID: p.ID, Name: p.Name, Start: p.From, End: p.To, Stores: strings.Join(p.Stores, ", "),
			State: MarkdownPlanState(p.Status, p.From, p.To, day)}
		if len(p.From) >= 7 {
			r.Month = p.From[:7]
		}
		if r.Stores == "" {
			r.Stores = "All"
		}
		if p.Category != "" {
			r.Scope = "Category " + p.Category
		} else {
			r.Scope = strings.Join(p.Items, ", ")
		}
		if p.Fixed > 0 {
			r.Discount = fmt.Sprintf("Rs %.2f", p.Fixed)
		} else {
			r.Discount = strconv.FormatFloat(p.Percent, 'f', -1, 64) + "% off"
		}
		rows = append(rows, r)
	}
	for i := 1; i < len(rows); i++ {
		for j := i; j > 0; j-- {
			a, b := rows[j-1], rows[j]
			if rank(a.State) < rank(b.State) || (rank(a.State) == rank(b.State) && a.Start <= b.Start) {
				break
			}
			rows[j-1], rows[j] = b, a
		}
	}
	return rows, nil
}
