package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Ask 170.18: recruitment. A job opening names a position and how many places;
// an applicant moves through stages; accepting the applicant's offer makes a
// draft Employee (status Inactive until HR activates it at joining) and marks
// the applicant Hired. Applicant data is HR's alone: only the HR and admin roles
// hold a permission on these doctypes.

// AcceptJobOfferResult is what accepting an offer made.
type AcceptJobOfferResult struct {
	OfferID     string `json:"offer_id"`
	ApplicantID string `json:"applicant_id"`
	EmployeeID  string `json:"employee_id"`
}

func hrDocByRef(tx *sql.Tx, schema, doctype, ref string, lock bool) (string, map[string]interface{}, string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", nil, "", nil
	}
	suffix := ""
	if lock {
		suffix = " FOR UPDATE"
	}
	var id, raw, status string
	err := tx.QueryRow(fmt.Sprintf(`SELECT id, data, status FROM %s.documents WHERE doctype = $1 AND deleted_at IS NULL
		AND (id = $2 OR data->>'code' = $2) ORDER BY (id = $2) DESC LIMIT 1`+suffix, schema), doctype, ref).Scan(&id, &raw, &status)
	if err == sql.ErrNoRows {
		return "", nil, "", nil
	}
	if err != nil {
		return "", nil, "", err
	}
	var d map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		return "", nil, "", err
	}
	return id, d, status, nil
}

func hrWriteDoc(tx *sql.Tx, schema, doctype, id string, d map[string]interface{}, status string) error {
	d["status"] = status
	b, err := json.Marshal(d)
	if err != nil {
		return err
	}
	_, err = tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $1, status = $2, updated_at = CURRENT_TIMESTAMP, version = version + 1
		WHERE doctype = $3 AND id = $4`, schema), b, status, doctype, id)
	return err
}

// AcceptJobOffer accepts a Sent (or Draft) offer: the applicant must be at the
// Offer stage and the opening must have a place left. One transaction: the
// Employee, the offer and the applicant change together or not at all.
func AcceptJobOffer(tenantID, offerRef, actor string) (*AcceptJobOfferResult, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	offerID, offer, offerStatus, err := hrDocByRef(tx, schema, "JobOffer", offerRef, true)
	if err != nil {
		return nil, err
	}
	if offer == nil {
		return nil, &ValidationError{Code: "HR-1704", Message: "no job offer has this id"}
	}
	if offerStatus != "Draft" && offerStatus != "Sent" {
		return nil, &ValidationError{Code: "HR-1704", Message: fmt.Sprintf("this offer is already %s", strings.ToLower(offerStatus))}
	}
	appID, app, appStage, err := hrDocByRef(tx, schema, "Applicant", strField(offer, "applicant"), true)
	if err != nil {
		return nil, err
	}
	if app == nil || appStage != "Offer" {
		return nil, &ValidationError{Code: "HR-1705", Message: "the applicant must be at the Offer stage to accept an offer"}
	}
	openID, opening, _, err := hrDocByRef(tx, schema, "JobOpening", strField(app, "opening"), true)
	if err != nil {
		return nil, err
	}
	if opening != nil {
		places := int(numFromInterface(opening["openings"]))
		var hired int
		if err := tx.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.documents WHERE doctype = 'Applicant' AND deleted_at IS NULL
			AND status = 'Hired' AND data->>'opening' IN ($1, $2)`, schema), openID, strField(opening, "code")).Scan(&hired); err != nil {
			return nil, err
		}
		if places > 0 && hired >= places {
			return nil, &ValidationError{Code: "HR-1706", Message: "every place on this opening is filled"}
		}
	}

	emp := map[string]interface{}{
		"name": strField(app, "applicant_name"), "status": "Inactive",
		"joining_date": strField(offer, "joining_date"), "source_offer": offerID, "source_applicant": appID,
	}
	if opening != nil {
		emp["location"] = strField(opening, "location")
		if pos := strField(opening, "position"); pos != "" {
			emp["position"] = pos
			if _, p, _, err := hrDocByRef(tx, schema, "Position", pos, false); err == nil && p != nil {
				emp["designation"] = strField(p, "title")
				emp["department"] = strField(p, "department")
			}
		}
	}
	empID := NewDocID("EMP")
	emp["code"] = empID
	b, _ := json.Marshal(emp)
	if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'Employee', $2, 'Inactive', $3)`, schema), empID, b, actor); err != nil {
		return nil, err
	}
	offer["employee_id"] = empID
	if err := hrWriteDoc(tx, schema, "JobOffer", offerID, offer, "Accepted"); err != nil {
		return nil, err
	}
	app["employee_id"] = empID
	if err := hrWriteDoc(tx, schema, "Applicant", appID, app, "Hired"); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	LogAuditEvent(tenantID, actor, "HR_RECRUITMENT", "OFFER_ACCEPTED", fmt.Sprintf("%s -> employee %s", offerID, empID))
	return &AcceptJobOfferResult{OfferID: offerID, ApplicantID: appID, EmployeeID: empID}, nil
}

// --- 170.19: login at onboarding, clearance before exit ---------------------

// OnboardingLoginSource checks that an onboarding checklist is complete and
// its employee has no login yet, and returns the employee.
func OnboardingLoginSource(tenantID, checklistRef string) (employeeID string, employee map[string]interface{}, err error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", nil, err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return "", nil, err
	}
	defer tx.Rollback()
	_, list, status, err := hrDocByRef(tx, schema, onboardingDoctype, checklistRef, false)
	if err != nil {
		return "", nil, err
	}
	if list == nil {
		return "", nil, &ValidationError{Code: "HR-0531", Message: "no onboarding or offboarding checklist has this id"}
	}
	if strField(list, "checklist_type") == "Offboarding" || status != "Completed" {
		return "", nil, &ValidationError{Code: "HR-1710", Message: "finish every onboarding step first"}
	}
	id, emp, _, err := hrDocByRef(tx, schema, "Employee", strField(list, "employee_id"), false)
	if err != nil {
		return "", nil, err
	}
	if emp == nil {
		return "", nil, &ValidationError{Code: "HR-1710", Message: "the checklist's employee was not found"}
	}
	if strings.TrimSpace(strField(emp, "user_id")) != "" {
		return "", nil, &ValidationError{Code: "HR-1711", Message: "this employee already has a login"}
	}
	return id, emp, nil
}

// LinkEmployeeLogin records the new login on the Employee (Employee.user_id).
func LinkEmployeeLogin(tenantID, employeeID, username string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	_, err = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || jsonb_build_object('user_id', $1::text),
		updated_at = CURRENT_TIMESTAMP, version = version + 1 WHERE doctype = 'Employee' AND id = $2`, schema), username, employeeID)
	return err
}

// CheckExitClearance (170.26 setting hr.exit_clearance_required): while the
// setting is on, a leaver with an open offboarding checklist cannot be paid
// the final settlement - the payslip is not posted.
func CheckExitClearance(tenantID, employeeRef string) error {
	if !GetSettingBool(tenantID, SettingExitClearanceRequired) {
		return nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	empID, emp, err := hrEmployeeByRef(schema, employeeRef)
	if err != nil || emp == nil {
		return err
	}
	if !EmployeeHasLeft(strField(emp, "status"), strField(emp, "exit_date"), time.Now()) {
		return nil
	}
	var open int
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.documents WHERE doctype = 'OnboardingChecklist' AND deleted_at IS NULL
		AND data->>'checklist_type' = 'Offboarding' AND COALESCE(data->>'status', status) <> 'Completed'
		AND data->>'employee_id' IN ($1, $2)`, schema), empID, strField(emp, "code")).Scan(&open)
	if err != nil {
		return err
	}
	if open > 0 {
		return &ValidationError{Code: "HR-1720", Message: "the exit checklist is still open - finish it before the final pay"}
	}
	return nil
}
