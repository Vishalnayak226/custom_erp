package engines

import (
	"custom_erp/db"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"time"
)

// Ask 176 item 176.6: instrument calibration.
//
// An Instrument (a gauge, scale or meter) is calibrated every N days. A
// CalibrationRecord against it stores the result and its next due date,
// which the server sets on every save: the calibration date plus the
// interval on a Pass, the calibration date itself on a Fail (a failed
// instrument is due again at once). Only a Recorded record counts, and a
// Recorded record is not edited. The "Instruments due for calibration"
// report reads the latest Recorded record of each instrument in use.

const (
	InstrumentDoctype        = "Instrument"
	CalibrationRecordDoctype = "CalibrationRecord"
	instrumentCodeInvalid    = "QUALIT-1766"
	calibrationCodeInvalid   = "QUALIT-1767"
	calibrationDueSoonDays   = 30
)

func init() {
	RegisterReport(ReportDefinition{
		ID: "calibration-due", Label: "Instruments due for calibration", Category: "Quality",
		Columns: []ReportColumn{
			{Key: "code", Label: "Instrument"}, {Key: "name", Label: "Name"}, {Key: "kept_at", Label: "Kept at"},
			{Key: "last_calibrated", Label: "Last calibrated"}, {Key: "result", Label: "Result"},
			{Key: "next_due", Label: "Next due"}, {Key: "days_overdue", Label: "Days overdue"},
			{Key: "state", Label: "State"},
		},
		Params: []ReportParam{{Key: "show", Label: "Show", Type: "select", Options: "Due and overdue,All"}},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			return CalibrationDue(tenantID, BusinessToday(), params["show"] == "All")
		},
	})
}

// validateInstrumentSave: the interval is a whole number of days, 1 to 3650.
func validateInstrumentSave(payload map[string]interface{}) error {
	days := numFromInterface(payload["calibration_interval_days"])
	if days < 1 || days > 3650 || days != math.Trunc(days) {
		return &ValidationError{Code: instrumentCodeInvalid, Message: "Give a calibration interval of 1 to 3650 days."}
	}
	return nil
}

// validateCalibrationRecordSave checks a calibration and sets next_due_on.
func validateCalibrationRecordSave(tenantID, priorStatus string, payload map[string]interface{}) error {
	bad := func(msg string) error { return &ValidationError{Code: calibrationCodeInvalid, Message: msg} }
	if priorStatus == "Recorded" {
		return bad("A recorded calibration cannot be changed. Record a new one.")
	}
	inst, instStatus, err := fetchDocData(tenantID, InstrumentDoctype, strField(payload, "instrument"))
	if errors.Is(err, sql.ErrNoRows) {
		return bad("Pick the instrument.")
	}
	if err != nil {
		return err
	}
	if instStatus == "Retired" {
		return bad("That instrument is retired.")
	}
	on, err := time.Parse("2006-01-02", strField(payload, "calibrated_on"))
	if err != nil {
		return bad("Enter the date it was calibrated.")
	}
	if on.Format("2006-01-02") > BusinessToday() {
		return bad("The calibration date cannot be later than today.")
	}
	switch strField(payload, "result") {
	case "Pass":
		days := int(numFromInterface(inst["calibration_interval_days"]))
		if days < 1 {
			return bad("The instrument has no calibration interval. Set one on the instrument first.")
		}
		payload["next_due_on"] = on.AddDate(0, 0, days).Format("2006-01-02")
	case "Fail":
		payload["next_due_on"] = on.Format("2006-01-02")
	default:
		return bad("Pick Pass or Fail.")
	}
	return nil
}

// CalibrationDue is one row per instrument not retired, read from its latest
// Recorded calibration as of today (YYYY-MM-DD). all=false keeps only the
// rows that need action: overdue, due within 30 days, failed, never done.
func CalibrationDue(tenantID, today string, all bool) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	day, err := time.Parse("2006-01-02", today)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT COALESCE(NULLIF(i.data->>'code', ''), i.id), COALESCE(i.data->>'name', ''), COALESCE(i.data->>'kept_at', ''),
		       COALESCE(last.cal_on, ''), COALESCE(last.result, ''), COALESCE(last.next_due, '')
		  FROM %[1]s.documents i
		  LEFT JOIN LATERAL (
		       SELECT r.data->>'calibrated_on' AS cal_on, r.data->>'result' AS result, r.data->>'next_due_on' AS next_due
		         FROM %[1]s.documents r
		        WHERE r.doctype = 'CalibrationRecord' AND r.deleted_at IS NULL AND r.status = 'Recorded'
		          AND r.data->>'instrument' IN (i.id, i.data->>'code')
		        ORDER BY r.data->>'calibrated_on' DESC, r.created_at DESC
		        LIMIT 1) last ON TRUE
		 WHERE i.doctype = 'Instrument' AND i.deleted_at IS NULL AND i.status <> 'Retired'
		 ORDER BY 1`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]interface{}{}
	for rows.Next() {
		var code, name, keptAt, calOn, result, nextDue string
		if err := rows.Scan(&code, &name, &keptAt, &calOn, &result, &nextDue); err != nil {
			return nil, err
		}
		state, overdue := calibrationState(day, calOn, result, nextDue)
		if !all && state == "OK" {
			continue
		}
		out = append(out, map[string]interface{}{
			"code": code, "name": name, "kept_at": keptAt, "last_calibrated": calOn, "result": result,
			"next_due": nextDue, "days_overdue": overdue, "state": state,
		})
	}
	return out, rows.Err()
}

// calibrationState names what an instrument needs as of day, and how many
// days its calibration is overdue (0 when it is not).
func calibrationState(day time.Time, calOn, result, nextDue string) (string, int) {
	if calOn == "" {
		return "Never calibrated", 0
	}
	overdue := 0
	due, err := time.Parse("2006-01-02", nextDue)
	if err == nil && due.Before(day) {
		overdue = int(day.Sub(due).Hours() / 24)
	}
	switch {
	case result == "Fail":
		return "Failed - recalibrate", overdue
	case err != nil || overdue > 0:
		return "Overdue", overdue
	case !due.After(day.AddDate(0, 0, calibrationDueSoonDays)):
		return "Due within 30 days", 0
	}
	return "OK", 0
}
