package engines

import (
	"custom_erp/db"
	"database/sql"
	"fmt"
	"strings"
)

// Every person has a department (Stage 93.9).
//
// The user, 2026-10-18: "How to assign a user to dept? while user creation or
// should be part of Employee creation in HR. If HR, if someone don't take HR
// than? ... People should be mapped. I want to mapped from day one
// automatically." Decided the same day: the department sits on the User,
// beside the home store it already carries, so a client without HR has it;
// with HR, the Employee carries it and a user linked to an employee takes the
// employee's. A department is not under a store - a person's home store gives
// the cost center, their department the team. Postings then carry the
// department of whoever raised the document (93.10, postingDimensionsTx).

// DepartmentReferenceFields are the plain Data fields, per doctype, that hold
// a Department: the generic save resolves each through
// ResolveDepartmentReference and GetDocTypeMeta marks them pick_from =
// Department - the same shape as LocationReferenceFields.
var DepartmentReferenceFields = map[string][]string{
	"Employee":     {"department"},
	"ExpenseClaim": {"department"},
}

// ResolveDepartmentReference returns the Active Department a reference names
// - by id, code or exact name (case-insensitive), the id preferred - so a
// picker that sent the code or a name typed as before still lands on the
// record.
func ResolveDepartmentReference(tenantID, ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	var id, status string
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT id, status FROM %s.documents
		WHERE doctype = 'Department' AND deleted_at IS NULL
		  AND (id = $1 OR data->>'code' = $1 OR lower(trim(data->>'name')) = lower($1))
		ORDER BY (id = $1) DESC, (data->>'code' = $1) DESC, created_at LIMIT 1`, schema), ref).Scan(&id, &status)
	if err == sql.ErrNoRows {
		return "", &ValidationError{Code: "MASTER-0931", Message: fmt.Sprintf("department %q is not on the Department list - pick one, or add it under Setup -> Department", ref)}
	}
	if err != nil {
		return "", err
	}
	if status != "Active" {
		return "", &ValidationError{Code: "MASTER-0931", Message: fmt.Sprintf("department %q is Inactive - pick an Active one", ref)}
	}
	return id, nil
}

// DepartmentNeededForRole: every login belongs to a department except a
// vendor's own supplier-portal account, which is not one of the client's
// people.
func DepartmentNeededForRole(role string) bool {
	return !strings.EqualFold(strings.TrimSpace(role), "Supplier")
}

// LinkedEmployee is the Employee record a user is linked to (HR's user_id),
// if any - when there is one, the employee leads the user's department.
func LinkedEmployee(tenantID, userID string) (employeeID, department string, err error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", "", err
	}
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT id, COALESCE(data->>'department', '') FROM %s.documents
		WHERE doctype = 'Employee' AND deleted_at IS NULL AND data->>'user_id' = $1
		ORDER BY (status = 'Active') DESC, updated_at DESC NULLS LAST LIMIT 1`, schema), userID).Scan(&employeeID, &department)
	if err == sql.ErrNoRows {
		return "", "", nil
	}
	return employeeID, department, err
}

// SetUserDepartment sets a user's department from the Users screen. A user
// linked to an employee is refused: HR leads, so the change belongs on the
// employee, where it reaches the user by itself.
func SetUserDepartment(tenantID, userID, ref string) (string, error) {
	empID, _, err := LinkedEmployee(tenantID, userID)
	if err != nil {
		return "", err
	}
	if empID != "" {
		return "", &ValidationError{Code: "MASTER-0932", Message: fmt.Sprintf("this user is linked to employee %s - change the department on the employee (HR -> Employees) and it follows here", empID)}
	}
	dept, err := ResolveDepartmentReference(tenantID, ref)
	if err != nil {
		return "", err
	}
	if dept == "" {
		return "", &ValidationError{Code: "MASTER-0931", Message: "pick the user's department"}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	_, err = db.DB.Exec(fmt.Sprintf(`UPDATE %s.users SET department = $1 WHERE id = $2`, schema), dept, userID)
	return dept, err
}

// SyncUserDepartmentFromEmployee gives a linked user its employee's
// department on every employee save (the HR side of "the employee leads").
func SyncUserDepartmentFromEmployee(tenantID, userID, department string) error {
	userID, department = strings.TrimSpace(userID), strings.TrimSpace(department)
	if userID == "" || department == "" {
		return nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	_, err = db.DB.Exec(fmt.Sprintf(`UPDATE %s.users SET department = $1 WHERE id = $2 AND department IS DISTINCT FROM $1`, schema), department, userID)
	return err
}
