package engines

import (
	"custom_erp/db"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Ask 170.21: training records may carry a Valid until date. The list below
// is what expires in the next N days; the reminder goes to HR once per
// certificate and date.

const CertificateExpiringEvent = "Certificate Expiring"

// ExpiringCertificate is one training record whose certificate runs out.
type ExpiringCertificate struct {
	RecordID      string `json:"record_id"`
	EmployeeID    string `json:"employee_id"`
	ProgramID     string `json:"program_id"`
	CertificateNo string `json:"certificate_no"`
	ValidUntil    string `json:"valid_until"`
	DaysLeft      int    `json:"days_left"`
}

// ListExpiringCertificates lists certificates that expire within days (already
// expired ones included, oldest first). days <= 0 uses the setting.
func ListExpiringCertificates(tenantID string, days int) ([]ExpiringCertificate, error) {
	if days <= 0 {
		days = GetSettingInt(tenantID, SettingCertificateReminderDays)
		if days <= 0 {
			days = 30
		}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	today, err := time.Parse("2006-01-02", BusinessToday())
	if err != nil {
		return nil, err
	}
	limit := today.AddDate(0, 0, days).Format("2006-01-02")
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, COALESCE(data->>'employee_id',''), COALESCE(data->>'program_id',''),
		COALESCE(data->>'certificate_no',''), left(data->>'valid_until', 10) FROM %s.documents
		WHERE doctype = 'TrainingRecord' AND deleted_at IS NULL AND COALESCE(data->>'valid_until','') <> ''
		AND left(data->>'valid_until', 10) <= $1 ORDER BY left(data->>'valid_until', 10), id LIMIT 5000`, schema), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ExpiringCertificate{}
	for rows.Next() {
		var c ExpiringCertificate
		if err := rows.Scan(&c.RecordID, &c.EmployeeID, &c.ProgramID, &c.CertificateNo, &c.ValidUntil); err != nil {
			return nil, err
		}
		if d, err := time.Parse("2006-01-02", c.ValidUntil); err == nil {
			c.DaysLeft = int(d.Sub(today).Hours() / 24)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// RunCertificateReminders tells HR once about each certificate that expires
// within the setting's days. The date it warned about is stamped on the record,
// so a renewed certificate (a new Valid until) warns again. Setting 0 = off.
func RunCertificateReminders(tenantID, schema string) error {
	n := GetSettingInt(tenantID, SettingCertificateReminderDays)
	if n <= 0 {
		return nil
	}
	list, err := ListExpiringCertificates(tenantID, n)
	if err != nil {
		return err
	}
	today := BusinessToday()
	for _, c := range list {
		if c.ValidUntil < today {
			continue // already lapsed: the list shows it, no reminder
		}
		res, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || jsonb_build_object('expiry_alerted_for', left(data->>'valid_until', 10))
			WHERE doctype = 'TrainingRecord' AND id = $1 AND COALESCE(data->>'expiry_alerted_for','') <> left(data->>'valid_until', 10)`, schema), c.RecordID)
		if err != nil {
			return err
		}
		if k, _ := res.RowsAffected(); k == 0 {
			continue
		}
		DispatchNotification(tenantID, CertificateExpiringEvent, c.RecordID, map[string]string{
			"employee": c.EmployeeID, "program": c.ProgramID, "certificate": c.CertificateNo,
			"valid_until": c.ValidUntil, "days_left": strconv.Itoa(c.DaysLeft)})
	}
	return nil
}

// certificateExpiryReport is the same list as a report, with the employee's
// name and department and the program's name (170.21).
func certificateExpiryReport(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
	days, _ := strconv.Atoi(strings.TrimSpace(params["days"]))
	list, err := ListExpiringCertificates(tenantID, days)
	if err != nil || len(list) == 0 {
		return []map[string]interface{}{}, err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	names := map[string][2]string{} // Employee id or code -> name, department; TrainingProgram id -> name
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT doctype, id, COALESCE(data->>'code', ''),
		COALESCE(NULLIF(data->>'name', ''), NULLIF(data->>'description', ''), ''), COALESCE(data->>'department', '')
		FROM %s.documents WHERE doctype IN ('Employee', 'TrainingProgram') AND deleted_at IS NULL`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var doctype, id, code, name, dept string
		if err := rows.Scan(&doctype, &id, &code, &name, &dept); err != nil {
			return nil, err
		}
		names[doctype+":"+id] = [2]string{name, dept}
		if code != "" {
			names[doctype+":"+code] = [2]string{name, dept}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]map[string]interface{}, 0, len(list))
	for _, c := range list {
		emp, prog := names["Employee:"+c.EmployeeID], names["TrainingProgram:"+c.ProgramID]
		state := "Expiring"
		if c.DaysLeft < 0 {
			state = "Expired"
		}
		out = append(out, map[string]interface{}{"employee_id": c.EmployeeID, "employee_name": emp[0], "department": emp[1],
			"program": firstNonEmpty(prog[0], c.ProgramID), "certificate_no": c.CertificateNo, "valid_until": c.ValidUntil,
			"days_left": c.DaysLeft, "status": state, "record_id": c.RecordID})
	}
	return out, nil
}

func init() {
	RegisterReport(ReportDefinition{
		ID: "certificate-expiry", Label: "Certificate Expiry", Category: "HR",
		Columns: []ReportColumn{
			{Key: "employee_id", Label: "Employee ID"}, {Key: "employee_name", Label: "Employee"}, {Key: "department", Label: "Department"},
			{Key: "program", Label: "Training"}, {Key: "certificate_no", Label: "Certificate"}, {Key: "valid_until", Label: "Valid until"},
			{Key: "days_left", Label: "Days left"}, {Key: "status", Label: "Status"},
		},
		Params: []ReportParam{
			{Key: "days", Label: "Days ahead (optional)", Type: "text"},
		},
		Run: certificateExpiryReport,
	})
}
