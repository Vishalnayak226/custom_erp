package engines

import (
	"custom_erp/db"
	"fmt"
)

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

	attRows, err := db.DB.Query(fmt.Sprintf(`
		SELECT data->>'employee_id', data->>'status' FROM %s.documents
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
		SELECT data->>'employee_id', COALESCE((data->>'days')::numeric, 0) FROM %s.documents
		WHERE doctype = 'Leave' AND status = 'Approved'
		AND (data->>'from_date') <= $2 AND (data->>'to_date') >= $1`, schema), from, to)
	if err != nil {
		return nil, err
	}
	defer leaveRows.Close()
	for leaveRows.Next() {
		var empID string
		var days float64
		if err := leaveRows.Scan(&empID, &days); err != nil {
			return nil, err
		}
		getEntry(empID).ApprovedLeaveDays += int(days)
	}

	results := make([]PayrollExportEntry, 0, len(byEmployee))
	for _, e := range byEmployee {
		results = append(results, *e)
	}
	return results, nil
}
