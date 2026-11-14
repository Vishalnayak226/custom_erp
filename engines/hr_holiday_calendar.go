package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Ask 170.28: holiday calendar and weekly offs per location. With no active
// calendar for the person's location (or a blank-location one), nothing here
// changes a count: leave days and attendance statuses stay as they were.

var weekdayByName = map[string]time.Weekday{
	"sun": time.Sunday, "mon": time.Monday, "tue": time.Tuesday, "wed": time.Wednesday,
	"thu": time.Thursday, "fri": time.Friday, "sat": time.Saturday,
}

// HolidayCalendarView is one calendar read for counting.
type HolidayCalendarView struct {
	Name      string
	WeeklyOff map[time.Weekday]bool
	Holidays  map[string]string // date -> name
}

// IsOff says whether a date is a weekly off or a holiday, and which.
func (c *HolidayCalendarView) IsOff(d time.Time) (off bool, kind string) {
	if c == nil {
		return false, ""
	}
	if _, ok := c.Holidays[d.Format("2006-01-02")]; ok {
		return true, "Holiday"
	}
	if c.WeeklyOff[d.Weekday()] {
		return true, "WeeklyOff"
	}
	return false, ""
}

func parseWeeklyOff(s string) (map[time.Weekday]bool, []string) {
	out := map[time.Weekday]bool{}
	var bad []string
	for _, p := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ';' || r == ' ' }) {
		k := strings.ToLower(p)
		if len(k) > 3 {
			k = k[:3]
		}
		if wd, ok := weekdayByName[k]; ok {
			out[wd] = true
		} else {
			bad = append(bad, p)
		}
	}
	return out, bad
}

func parseHolidayRows(v interface{}) []map[string]interface{} {
	switch t := v.(type) {
	case string:
		var rows []map[string]interface{}
		if strings.TrimSpace(t) != "" && json.Unmarshal([]byte(t), &rows) == nil {
			return rows
		}
	case []interface{}:
		var rows []map[string]interface{}
		for _, x := range t {
			if m, ok := x.(map[string]interface{}); ok {
				rows = append(rows, m)
			}
		}
		return rows
	}
	return nil
}

func validateHolidayCalendar(payload map[string]interface{}) error {
	if _, bad := parseWeeklyOff(strField(payload, "weekly_off")); len(bad) > 0 {
		return &ValidationError{Code: "HR-1731", Message: "weekly off days are written Sun, Mon, Tue, Wed, Thu, Fri, Sat"}
	}
	for _, row := range parseHolidayRows(payload["holidays"]) {
		d := strings.TrimSpace(fmt.Sprintf("%v", row["date"]))
		if _, err := time.Parse("2006-01-02", d); err != nil {
			return &ValidationError{Code: "HR-1732", Message: "every holiday needs a date as YYYY-MM-DD"}
		}
	}
	return nil
}

// HolidayCalendarFor returns the active calendar for a location: its own, else
// the one with a blank location, else nil (no calendar - nothing changes).
func HolidayCalendarFor(tenantID, location string) (*HolidayCalendarView, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT data FROM %s.documents WHERE doctype = 'HolidayCalendar' AND deleted_at IS NULL
		AND COALESCE(data->>'status', 'Active') = 'Active' AND COALESCE(data->>'location', '') IN ($1, '')
		ORDER BY (COALESCE(data->>'location', '') = $1) DESC, id LIMIT 1`, schema), strings.TrimSpace(location))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, rows.Err()
	}
	var raw string
	if err := rows.Scan(&raw); err != nil {
		return nil, err
	}
	var d map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		return nil, err
	}
	view := &HolidayCalendarView{Name: strField(d, "code"), Holidays: map[string]string{}}
	view.WeeklyOff, _ = parseWeeklyOff(strField(d, "weekly_off"))
	for _, row := range parseHolidayRows(d["holidays"]) {
		date := strings.TrimSpace(fmt.Sprintf("%v", row["date"]))
		if len(date) >= 10 {
			view.Holidays[date[:10]] = strings.TrimSpace(fmt.Sprintf("%v", row["name"]))
		}
	}
	return view, nil
}

// WorkingDays counts the days in [from, to] that are not weekly offs or
// holidays on the calendar. A nil calendar counts every day.
func (c *HolidayCalendarView) WorkingDays(from, to time.Time) float64 {
	n := 0
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		if off, _ := c.IsOff(d); !off {
			n++
		}
	}
	return float64(n)
}

func employeeLocation(tenantID, ref string) string {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return ""
	}
	if _, doc, err := hrEmployeeByRef(schema, ref); err == nil && doc != nil {
		return strField(doc, "location")
	}
	return ""
}

// applyCalendarToLeave counts a whole-day leave in working days when the
// person's location has a calendar. A half day or a count the person typed
// that is not the calendar span is left as typed.
func applyCalendarToLeave(tenantID string, payload map[string]interface{}) error {
	from, errA := time.Parse("2006-01-02", strField(payload, "from_date"))
	to, errB := time.Parse("2006-01-02", strField(payload, "to_date"))
	if errA != nil || errB != nil || to.Before(from) {
		return nil
	}
	cal, err := HolidayCalendarFor(tenantID, employeeLocation(tenantID, strField(payload, "employee_id")))
	if err != nil || cal == nil {
		return err
	}
	span := float64(int(to.Sub(from).Hours()/24) + 1)
	if days := numFromInterface(payload["days"]); days != 0 && days != span {
		return nil
	}
	working := cal.WorkingDays(from, to)
	if working == 0 {
		return &ValidationError{Code: "HR-1733", Message: "these dates are all holidays or weekly offs"}
	}
	payload["days"] = working
	return nil
}

// applyCalendarToAttendance files an absence on a holiday or weekly off as
// that, not as an absence.
func applyCalendarToAttendance(tenantID string, payload map[string]interface{}) error {
	date, err := time.Parse("2006-01-02", strField(payload, "date"))
	if err != nil {
		return nil
	}
	if st := strField(payload, "status"); st != "" && st != "Absent" {
		return nil
	}
	loc := strField(payload, "location")
	if loc == "" {
		loc = employeeLocation(tenantID, strField(payload, "employee_id"))
	}
	cal, err := HolidayCalendarFor(tenantID, loc)
	if err != nil || cal == nil {
		return err
	}
	if off, kind := cal.IsOff(date); off {
		payload["status"] = kind
	}
	return nil
}
