package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Ask 170.27: people analytics with privacy. Headcount, joiners, leavers and
// attrition by department, location or position. A group of fewer people than
// hr.analytics_min_group is folded into one "Other" row, so a small group never
// identifies a person. A session without the global location scope sees its own
// store's people only.

// PeopleAnalyticsRow is one group.
type PeopleAnalyticsRow struct {
	Group        string  `json:"group"`
	Headcount    int     `json:"headcount"`
	Joiners      int     `json:"joiners"`
	Leavers      int     `json:"leavers"`
	AttritionPct float64 `json:"attrition_pct"`
}

type analyticsAcc struct{ head, join, leave int }

// PeopleAnalytics counts by "department", "location" or "position" for the
// period [from, to] (default: the last 12 months). A person counts in the
// headcount if they joined on or before `to` and had not left before it.
func PeopleAnalytics(tenantID string, session SessionScope, groupBy, from, to string) ([]PeopleAnalyticsRow, error) {
	field := map[string]string{"department": "department", "location": "location", "position": "position"}[groupBy]
	if field == "" {
		return nil, &ValidationError{Code: "HR-1734", Message: "group by department, location or position"}
	}
	today := BusinessToday()
	if to == "" {
		to = today
	}
	if from == "" {
		t, _ := time.Parse("2006-01-02", to)
		from = t.AddDate(-1, 0, 0).Format("2006-01-02")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	where := ""
	var args []interface{}
	if !session.HasGlobalScope(ScopeLocation, "Employee") {
		where = ` AND COALESCE(data->>'location', '') = $1`
		args = append(args, session.LocationCode)
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT data FROM %s.documents WHERE doctype = 'Employee' AND deleted_at IS NULL`+where, schema), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	acc := map[string]*analyticsAcc{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var d map[string]interface{}
		if json.Unmarshal([]byte(raw), &d) != nil {
			continue
		}
		key := strings.TrimSpace(strField(d, field))
		if key == "" {
			key = "(none)"
		}
		a := acc[key]
		if a == nil {
			a = &analyticsAcc{}
			acc[key] = a
		}
		join := dateHead(strField(d, "joining_date"))
		exit := dateHead(strField(d, "exit_date"))
		if (join == "" || join <= to) && (exit == "" || exit > to) {
			a.head++
		}
		if join != "" && join >= from && join <= to {
			a.join++
		}
		if exit != "" && exit >= from && exit <= to {
			a.leave++
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return foldSmallGroups(acc, GetSettingInt(tenantID, SettingAnalyticsMinGroup)), nil
}

func dateHead(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 10 {
		return s[:10]
	}
	return ""
}

// foldSmallGroups turns the counts into rows: a group with fewer people than
// min (headcount plus leavers - everyone it ever shows) goes into "Other".
func foldSmallGroups(acc map[string]*analyticsAcc, min int) []PeopleAnalyticsRow {
	if min < 1 {
		min = 5
	}
	var out []PeopleAnalyticsRow
	other := &analyticsAcc{}
	hasOther := false
	for key, a := range acc {
		if a.head+a.leave < min {
			other.head += a.head
			other.join += a.join
			other.leave += a.leave
			hasOther = true
			continue
		}
		out = append(out, analyticsRow(key, a))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Group < out[j].Group })
	if hasOther {
		out = append(out, analyticsRow("Other", other))
	}
	return out
}

func analyticsRow(key string, a *analyticsAcc) PeopleAnalyticsRow {
	r := PeopleAnalyticsRow{Group: key, Headcount: a.head, Joiners: a.join, Leavers: a.leave}
	if base := a.head + a.leave; base > 0 {
		r.AttritionPct = float64(int(float64(a.leave)/float64(base)*10000+0.5)) / 100
	}
	return r
}
