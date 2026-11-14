package engines

import (
	"custom_erp/db"
	"fmt"
	"strconv"
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
