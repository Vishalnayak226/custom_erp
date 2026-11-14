package engines

import (
	"fmt"
	"strings"

	"custom_erp/db"
)

// Stage 170.38: how long each record type keeps its personal data lives in
// one table, retention_policies, seeded to the numbers the code always used.
// The hourly personal-data sweep and the erasure rules read it; a row that is
// missing (a tenant migrated before the table, say) falls back to the code
// default, so nothing changes until an admin edits a row. A tenant legal hold
// still wins over every row (retentionClearField checks it).

// RetentionPolicy is one retention_policies row.
type RetentionPolicy struct {
	RecordType  string `json:"record_type"`
	FieldName   string `json:"field_name"`
	KeepFor     int    `json:"keep_for"`
	KeepUnit    string `json:"keep_unit"`   // days | months | years
	ThenAction  string `json:"then_action"` // clear field | purge | anonymise
	LegalBasis  string `json:"legal_basis"`
	Note        string `json:"note"`
	UpdatedBy   string `json:"updated_by,omitempty"`
	UpdatedAt   string `json:"updated_at,omitempty"`
	KeepForDays int    `json:"keep_for_days"`
}

// retentionFloorDays is the shortest keep period a row may be given: the
// security-event address is audit evidence for 180 days.
var retentionFloorDays = map[string]int{
	"audit_logs/source_ip":  auditSourceRetentionDays,
	"users/last_sign_in_ip": auditSourceRetentionDays,
}

// RetentionToDays turns keep_for and its unit into days (30-day month,
// 365-day year).
func RetentionToDays(n int, unit string) int {
	switch unit {
	case "years":
		return n * 365
	case "months":
		return n * 30
	}
	return n
}

// RetentionDays is the keep period in days for one record type and field, or
// def when the table has no row for it (or is missing).
func RetentionDays(tenantID, recordType, field string, def int) int {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return def
	}
	var n int
	var unit string
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT keep_for, keep_unit FROM %s.retention_policies WHERE record_type = $1 AND field_name = $2`, schema),
		recordType, field).Scan(&n, &unit)
	if err != nil {
		return def
	}
	return RetentionToDays(n, unit)
}

// ListRetentionPolicies returns every row, oldest rule first by record type.
func ListRetentionPolicies(tenantID string) ([]RetentionPolicy, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT record_type, field_name, keep_for, keep_unit, then_action, legal_basis, note,
		COALESCE(updated_by,''), COALESCE(updated_at::text,'') FROM %s.retention_policies ORDER BY record_type, field_name`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RetentionPolicy{}
	for rows.Next() {
		var p RetentionPolicy
		if err := rows.Scan(&p.RecordType, &p.FieldName, &p.KeepFor, &p.KeepUnit, &p.ThenAction, &p.LegalBasis, &p.Note, &p.UpdatedBy, &p.UpdatedAt); err != nil {
			return nil, err
		}
		p.KeepForDays = RetentionToDays(p.KeepFor, p.KeepUnit)
		out = append(out, p)
	}
	return out, rows.Err()
}

// SaveRetentionPolicy inserts or updates one row after checking it.
func SaveRetentionPolicy(tenantID, actor string, p RetentionPolicy) error {
	p.RecordType = strings.TrimSpace(p.RecordType)
	p.FieldName = strings.TrimSpace(p.FieldName)
	if p.KeepUnit == "" {
		p.KeepUnit = "days"
	}
	if p.ThenAction == "" {
		p.ThenAction = "clear field"
	}
	if p.RecordType == "" || p.KeepFor < 0 ||
		(p.KeepUnit != "days" && p.KeepUnit != "months" && p.KeepUnit != "years") ||
		(p.ThenAction != "clear field" && p.ThenAction != "purge" && p.ThenAction != "anonymise") ||
		(p.ThenAction == "clear field" && p.FieldName == "") {
		return &ValidationError{Code: "SEC-1700", Message: "Retention rule is not valid."}
	}
	if floor, ok := retentionFloorDays[p.RecordType+"/"+p.FieldName]; ok && p.KeepFor > 0 && RetentionToDays(p.KeepFor, p.KeepUnit) < floor {
		return &ValidationError{Code: "SEC-1701", Message: fmt.Sprintf("This record must be kept at least %d days.", floor)}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	_, err = db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.retention_policies (record_type, field_name, keep_for, keep_unit, then_action, legal_basis, note, updated_by, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,CURRENT_TIMESTAMP)
		ON CONFLICT (record_type, field_name) DO UPDATE SET keep_for=EXCLUDED.keep_for, keep_unit=EXCLUDED.keep_unit,
		  then_action=EXCLUDED.then_action, legal_basis=EXCLUDED.legal_basis, note=EXCLUDED.note, updated_by=EXCLUDED.updated_by, updated_at=CURRENT_TIMESTAMP`, schema),
		p.RecordType, p.FieldName, p.KeepFor, p.KeepUnit, p.ThenAction, p.LegalBasis, p.Note, actor)
	if err != nil {
		return err
	}
	LogAuditEvent(tenantID, actor, "RETENTION_POLICY_SAVED", "Success",
		fmt.Sprintf("%s/%s keep %d %s then %s (170.38)", p.RecordType, p.FieldName, p.KeepFor, p.KeepUnit, p.ThenAction))
	return nil
}

// extraFieldClearings are 'clear field' rows an admin added for a doctype the
// code does not list; the sweep clears them like personalFieldClearings.
func extraFieldClearings(tenantID string) []personalFieldClearing {
	pols, err := ListRetentionPolicies(tenantID)
	if err != nil {
		return nil
	}
	var out []personalFieldClearing
	for _, p := range pols {
		if p.ThenAction != "clear field" || p.FieldName == "" || p.KeepForDays <= 0 {
			continue
		}
		if p.RecordType == "audit_logs" || p.RecordType == "users" {
			continue
		}
		known := false
		for _, c := range personalFieldClearings {
			if c.Doctype == p.RecordType && c.Field == p.FieldName {
				known = true
			}
		}
		if !known {
			out = append(out, personalFieldClearing{Doctype: p.RecordType, Field: p.FieldName, Days: p.KeepForDays})
		}
	}
	return out
}
