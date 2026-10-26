package engines

import (
	"custom_erp/db"
	"fmt"
	"strings"
	"time"
)

// leaveDaysInside (Stage 132.20) is how many of a leave's days fall in
// [from, to]: its own day count when it lies wholly inside (so a half day
// stays a half day), else the calendar days of the overlap.
func leaveDaysInside(days float64, leaveFrom, leaveTo, from, to string) float64 {
	if leaveFrom >= from && leaveTo <= to {
		return days
	}
	lo, hi := leaveFrom, leaveTo
	if from > lo {
		lo = from
	}
	if to < hi {
		hi = to
	}
	a, errA := time.Parse("2006-01-02", lo)
	b, errB := time.Parse("2006-01-02", hi)
	if errA != nil || errB != nil || b.Before(a) {
		return 0
	}
	return float64(int(b.Sub(a).Hours()/24) + 1)
}

// SyncEmployeeAccessLink implements MB 16.3's "Access Link": an Employee's
// active/inactive status controls their linked ERP user account's ability
// to log in. Rather than adding an extra check to the login path, this
// reuses handleLogin's existing "WHERE status = 'Active'" filter - setting
// the linked user's status to match the employee's status is sufficient to
// block (or restore) login with no new login-path code needed. A no-op if
// the employee has no linked user_id.
func SyncEmployeeAccessLink(tenantID, employeeUserID, employeeStatus string) error {
	if employeeUserID == "" {
		return nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	userStatus := "Active"
	if employeeStatus == "Inactive" {
		userStatus = "Inactive"
	}
	_, err = db.DB.Exec(fmt.Sprintf(`UPDATE %s.users SET status = $1 WHERE id = $2`, schema), userStatus, employeeUserID)
	if err == nil {
		// Stage 29.8: drop the cached auth state so deactivating an Employee
		// kills their live session on the next request rather than at the end
		// of the cache window.
		InvalidateLiveUserState(tenantID, employeeUserID)
	}
	return err
}

// PayrollExportEntry is one employee's approved attendance/leave summary
// for a payroll period.
type PayrollExportEntry struct {
	EmployeeID        string `json:"employee_id"`
	PresentDays       int    `json:"present_days"`
	AbsentDays        int    `json:"absent_days"`
	LateDays          int    `json:"late_days"`
	ApprovedLeaveDays int    `json:"approved_leave_days"`
	// Stage 132.20: approved Unpaid leave inside the period (half days kept) - with
	// AbsentDays, the loss-of-pay days RunPayroll deducts.
	UnpaidLeaveDays float64 `json:"unpaid_leave_days"`
}

// GetPayrollExport implements MB 16.3's "Payroll Interface": if payroll is
// external, export approved attendance/leave data for the period. Expense
// data (also named in that requirement) isn't included here - it's added
// once Stage 13.13c's Expense Management exists to pull from, rather than
// being stubbed out early.
func GetPayrollExport(tenantID, from, to string) ([]PayrollExportEntry, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}

	// Stage 30.2.3: COALESCE'd - one Attendance row missing employee_id or
	// status would otherwise fail the scan and take the entire payroll export
	// down with it.
	attRows, err := db.DB.Query(fmt.Sprintf(`
		SELECT COALESCE(data->>'employee_id', ''), COALESCE(data->>'status', '') FROM %s.documents
		WHERE doctype = 'Attendance' AND (data->>'date') BETWEEN $1 AND $2`, schema), from, to)
	if err != nil {
		return nil, err
	}
	defer attRows.Close()

	byEmployee := map[string]*PayrollExportEntry{}
	getEntry := func(empID string) *PayrollExportEntry {
		if e, ok := byEmployee[empID]; ok {
			return e
		}
		e := &PayrollExportEntry{EmployeeID: empID}
		byEmployee[empID] = e
		return e
	}

	for attRows.Next() {
		var empID, status string
		if err := attRows.Scan(&empID, &status); err != nil {
			return nil, err
		}
		e := getEntry(empID)
		switch status {
		case "Present":
			e.PresentDays++
		case "Absent":
			e.AbsentDays++
		case "Late":
			e.LateDays++
		}
	}

	leaveRows, err := db.DB.Query(fmt.Sprintf(`
		SELECT data->>'employee_id', COALESCE((data->>'days')::numeric, 0)::float8,
		       COALESCE(data->>'leave_type', ''), COALESCE(data->>'from_date', ''), COALESCE(data->>'to_date', '')
		FROM %s.documents
		WHERE doctype = 'Leave' AND status = 'Approved'
		AND (data->>'from_date') <= $2 AND (data->>'to_date') >= $1`, schema), from, to)
	if err != nil {
		return nil, err
	}
	defer leaveRows.Close()
	for leaveRows.Next() {
		var empID, leaveType, lf, lt string
		var days float64
		if err := leaveRows.Scan(&empID, &days, &leaveType, &lf, &lt); err != nil {
			return nil, err
		}
		getEntry(empID).ApprovedLeaveDays += int(days)
		if strings.EqualFold(strings.TrimSpace(leaveType), "Unpaid") {
			getEntry(empID).UnpaidLeaveDays += leaveDaysInside(days, lf, lt, from, to)
		}
	}

	results := make([]PayrollExportEntry, 0, len(byEmployee))
	for _, e := range byEmployee {
		results = append(results, *e)
	}
	return results, nil
}
