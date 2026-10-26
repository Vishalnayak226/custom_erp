package engines

import (
	"custom_erp/db"
	"fmt"
	"sort"
	"strings"
)

// Stage 55.14: what an employee has left of each leave type this year, so a
// leave form can explain the balance before HRPAYR-0151 refuses the save.
//
// Available is exactly validateLeaveBalance's figure (transactional_validation.go):
// the flat yearly entitlement per type, less the days already Applied or
// Approved in the same calendar year (by from_date, a plain date string - no
// time zone involved). A user without HR's global self scope can read only
// their own employee's balance, the same rule the Leave list itself follows.

// Stage 132.20 (user decision 2026-10-26): Remaining drops on approval -
// entitlement less approved days - and Available (Remaining less what is
// awaiting approval) is what a new application may still take; the save
// check refuses beyond it, as before.
type LeaveBalanceLine struct {
	LeaveType   string   `json:"leave_type"`
	Entitlement *float64 `json:"entitlement"`
	Approved    float64  `json:"approved"`
	Pending     float64  `json:"pending"`
	Remaining   *float64 `json:"remaining"`
	Available   *float64 `json:"available"`
}

// GetLeaveBalance returns one line per leave type for employeeID in year.
func GetLeaveBalance(tenantID, employeeID, year string, session SessionScope) ([]LeaveBalanceLine, error) {
	employeeID = strings.TrimSpace(employeeID)
	if employeeID == "" {
		return nil, fmt.Errorf("choose an employee")
	}
	if len(year) != 4 {
		return nil, fmt.Errorf("year must be four digits")
	}
	if !session.HasGlobalScope(ScopeSelf, "Leave") {
		own, err := EmployeeCodeForUser(tenantID, session.UserID)
		if err != nil {
			return nil, err
		}
		if own == "" || own != employeeID {
			// The same answer as for an unknown employee: another person's
			// leave is not disclosed, not even that it exists.
			return nil, &ValidationError{Code: "GLOBAL-0011", Message: "you can see only your own leave balance"}
		}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT COALESCE(data->>'leave_type', ''), status, COALESCE(SUM((data->>'days')::numeric), 0)::float8
		FROM %s.documents WHERE doctype = 'Leave' AND data->>'employee_id' = $1
		  AND status IN ('Applied', 'Approved') AND (data->>'from_date') LIKE $2
		GROUP BY 1, 2`, schema), employeeID, year+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	used := map[string]*LeaveBalanceLine{}
	line := func(t string) *LeaveBalanceLine {
		if l, ok := used[t]; ok {
			return l
		}
		l := &LeaveBalanceLine{LeaveType: t}
		used[t] = l
		return l
	}
	for rows.Next() {
		var t, status string
		var days float64
		if err := rows.Scan(&t, &status, &days); err != nil {
			return nil, err
		}
		if status == "Approved" {
			line(t).Approved += days
		} else {
			line(t).Pending += days
		}
	}
	for t := range leaveAnnualEntitlement {
		line(t)
	}
	line("Unpaid")
	out := make([]LeaveBalanceLine, 0, len(used))
	for t, l := range used {
		if ent, ok := leaveAnnualEntitlement[t]; ok {
			e := ent
			r := ent - l.Approved
			a := r - l.Pending
			l.Entitlement, l.Remaining, l.Available = &e, &r, &a
		}
		out = append(out, *l)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LeaveType < out[j].LeaveType })
	return out, nil
}
