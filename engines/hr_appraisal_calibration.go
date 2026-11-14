package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// Stage 170.22: appraisal calibration. After the reviews are approved, HR (or the
// cycle's owner) can set a calibrated rating on an appraisal with a mandatory
// reason. The original overall_rating is never overwritten; every change is kept
// in the appraisal's calibration_log and in the audit log.

// RatingSpreadRow is one department's count per rating band (1-5).
type RatingSpreadRow struct {
	Department string `json:"department"`
	Original   [5]int `json:"original"`
	Calibrated [5]int `json:"calibrated"`
	Unrated    int    `json:"unrated"`
	Total      int    `json:"total"`
}

func ratingBand(v float64) int {
	b := int(math.Round(v))
	if b < 1 {
		b = 1
	}
	if b > 5 {
		b = 5
	}
	return b
}

// AppraisalRatingSpread counts the cycle's appraisals per rating band by
// department: "original" by overall_rating, "calibrated" by the calibrated
// rating where one is set, else the original.
func AppraisalRatingSpread(tenantID, cycleID string) ([]RatingSpreadRow, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT a.data, COALESCE(e.data->>'department', '') FROM %s.documents a
		LEFT JOIN %s.documents e ON e.doctype = 'Employee' AND e.id = a.data->>'employee_id'
		WHERE a.doctype = 'Appraisal' AND a.deleted_at IS NULL AND a.data->>'cycle_id' = $1`, schema, schema), cycleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byDept := map[string]*RatingSpreadRow{}
	for rows.Next() {
		var raw, dept string
		var d map[string]interface{}
		if rows.Scan(&raw, &dept) != nil || json.Unmarshal([]byte(raw), &d) != nil {
			continue
		}
		if dept == "" {
			dept = "(none)"
		}
		r := byDept[dept]
		if r == nil {
			r = &RatingSpreadRow{Department: dept}
			byDept[dept] = r
		}
		r.Total++
		orig := numFromInterface(d["overall_rating"])
		if orig <= 0 {
			r.Unrated++
			continue
		}
		r.Original[ratingBand(orig)-1]++
		eff := orig
		if c := numFromInterface(d["calibrated_rating"]); c > 0 {
			eff = c
		}
		r.Calibrated[ratingBand(eff)-1]++
	}
	out := make([]RatingSpreadRow, 0, len(byDept))
	for _, r := range byDept {
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Department < out[j].Department })
	return out, rows.Err()
}

// CalibrateAppraisal sets the calibrated rating. Only a Super Admin / HR, or the
// user named as the cycle's owner, may calibrate; the appraisal must be Approved
// and its cycle not Closed; the reason is mandatory.
func CalibrateAppraisal(tenantID, appraisalID string, rating float64, reason, actorUserID, actorRole string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return &ValidationError{Code: "HR-1740", Message: "Give the reason for the calibrated rating."}
	}
	if rating < 1 || rating > 5 {
		return &ValidationError{Code: "HR-1742", Message: "The calibrated rating must be between 1 and 5."}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var raw, status string
	if err := tx.QueryRow(fmt.Sprintf(`SELECT data, status FROM %s.documents WHERE doctype = 'Appraisal' AND id = $1 AND deleted_at IS NULL FOR UPDATE`, schema), appraisalID).Scan(&raw, &status); err != nil {
		return &ValidationError{Code: "HR-1743", Message: "That appraisal was not found."}
	}
	var d map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		return err
	}
	var cycleRaw string
	_ = tx.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents WHERE doctype = 'AppraisalCycle' AND id = $1 AND deleted_at IS NULL`, schema), strField(d, "cycle_id")).Scan(&cycleRaw)
	var cycle map[string]interface{}
	_ = json.Unmarshal([]byte(cycleRaw), &cycle)
	owner := strings.TrimSpace(strField(cycle, "owner"))
	if !IsSuperAdmin(actorRole) && (owner == "" || !strings.EqualFold(owner, actorUserID)) {
		return &ValidationError{Code: "HR-1741", Message: "Only HR or the cycle owner can calibrate a rating."}
	}
	if status != "Approved" || strField(cycle, "status") == "Closed" {
		return &ValidationError{Code: "HR-1743", Message: "Only an approved appraisal in an open cycle can be calibrated."}
	}
	prev := numFromInterface(d["calibrated_rating"])
	if prev == 0 {
		prev = numFromInterface(d["overall_rating"])
	}
	now := time.Now().UTC().Format(time.RFC3339)
	entry := map[string]interface{}{"from": prev, "to": rating, "reason": reason, "by": actorUserID, "at": now}
	log, _ := d["calibration_log"].([]interface{})
	d["calibration_log"] = append(log, entry)
	d["calibrated_rating"], d["calibration_reason"], d["calibrated_by"], d["calibrated_at"] = rating, reason, actorUserID, now
	out, err := json.Marshal(d)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $1, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'Appraisal' AND id = $2`, schema), out, appraisalID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	LogAuditEvent(tenantID, actorUserID, "APPRAISAL_CALIBRATED", "SUCCESS", fmt.Sprintf("Appraisal %s rating %v -> %v (original %v): %s", appraisalID, prev, rating, d["overall_rating"], reason))
	return nil
}

var calibrationFields = []string{"calibrated_rating", "calibration_reason", "calibrated_by", "calibrated_at", "calibration_log"}

// validateAppraisalCalibrationFields keeps the calibration fields out of the
// generic save: they change only through CalibrateAppraisal, which checks the
// role and demands a reason.
func validateAppraisalCalibrationFields(priorData, payload map[string]interface{}) error {
	for _, f := range calibrationFields {
		nv, has := payload[f]
		if !has {
			continue
		}
		show := func(v interface{}) string {
			if v == nil {
				return ""
			}
			return fmt.Sprint(v)
		}
		if show(nv) != show(priorData[f]) {
			return &ValidationError{Code: "HR-1741", Message: "Use Calibrate to change a calibrated rating: it needs a reason and HR or the cycle owner."}
		}
	}
	return nil
}
