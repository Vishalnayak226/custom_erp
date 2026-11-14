package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
)

// Ask 170 item 170.30: employee advances settled against claims.
//
// An StaffAdvance is paid once (Dr 1600 Staff Loans & Advances / Cr 1100
// Bank, key StaffAdvance:<id>:PAY). With expense.net_employee_advance on,
// paying an expense claim clears the claimant's open advances, oldest first,
// against the claim (the 1600 credit instead of bank cash) and records how
// much on the advance (settled_amount) and the claim (advance_netted). With
// it off, claim payment is exactly as before.

const (
	EmployeeAdvanceDoctype       = "StaffAdvance"
	SettingExpenseNetAdvance     = "expense.net_employee_advance"
	employeeAdvanceStatusPaid    = "Paid"
	employeeAdvanceStatusSettled = "Settled"
)

func init() {
	RegisterSetting(SettingDefinition{
		Key: SettingExpenseNetAdvance, Module: "Expenses",
		Label: "Net open advance when paying a claim", Type: SettingTypeBool, Default: "false",
		Description: "Ask 170.30: paying an expense claim first clears the employee's open advance (Dr expense / Cr advance) and pays only the rest from the bank. Off: claims are paid in full from the bank, as before.",
	})
	RegisterReport(ReportDefinition{
		ID: "employee-advance-balance", Label: "Employee advance balance", Category: "Expenses",
		Columns: []ReportColumn{
			{Key: "employee_id", Label: "Employee"}, {Key: "advanced", Label: "Advanced", Sensitive: true},
			{Key: "settled", Label: "Settled", Sensitive: true}, {Key: "balance", Label: "Balance", Sensitive: true},
		},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			return EmployeeAdvanceBalances(tenantID)
		},
	})
}

// EmployeeAdvanceBalances is the open advance per employee, read from the
// advance documents (never a stored counter).
func EmployeeAdvanceBalances(tenantID string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT data->>'employee_id',
		       SUM((data->>'amount')::numeric),
		       SUM(COALESCE((data->>'settled_amount')::numeric, 0))
		  FROM %s.documents
		 WHERE doctype = 'StaffAdvance' AND deleted_at IS NULL AND status IN ('Paid','Settled')
		 GROUP BY 1 ORDER BY 1`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]interface{}{}
	for rows.Next() {
		var emp string
		var adv, set float64
		if err := rows.Scan(&emp, &adv, &set); err != nil {
			return nil, err
		}
		out = append(out, map[string]interface{}{"employee_id": emp, "advanced": adv, "settled": set, "balance": adv - set})
	}
	return out, rows.Err()
}

// PayEmployeeAdvance pays a Draft advance: posts Dr 1600 / Cr 1100 once and
// marks it Paid, in one transaction under the row lock.
func PayEmployeeAdvance(tenantID, advanceID string) error {
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
	err = tx.QueryRow(fmt.Sprintf(`SELECT data::text, status FROM %s.documents WHERE doctype = 'StaffAdvance' AND id = $1 AND deleted_at IS NULL FOR UPDATE`, schema), advanceID).Scan(&raw, &status)
	if err == sql.ErrNoRows {
		return &ValidationError{Code: "EXP-1701", Message: "Advance not found."}
	}
	if err != nil {
		return err
	}
	if status != "Draft" {
		return &ValidationError{Code: "EXP-1702", Message: fmt.Sprintf("The advance is %s; only a Draft advance can be paid.", status)}
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return err
	}
	amt := RupeesToPaise(numFromInterface(data["amount"]))
	emp := strField(data, "employee_id")
	if amt <= 0 || emp == "" {
		return &ValidationError{Code: "EXP-1701", Message: "An advance needs an employee and an amount above zero."}
	}
	if e, _, err := fetchDocData(tenantID, "Employee", emp); err != nil || e == nil {
		return &ValidationError{Code: "EXP-1701", Message: "The employee on the advance was not found."}
	}
	if err := PostDoubleEntryTx(tx, tenantID, schema, EmployeeAdvanceDoctype, advanceID,
		map[string]int64{"1600": amt}, map[string]int64{"1100": amt}, "", fmt.Sprintf("StaffAdvance:%s:PAY", advanceID), PostingOptions{}); err != nil {
		return fmt.Errorf("failed to post advance: %v", err)
	}
	data["status"] = employeeAdvanceStatusPaid
	data["settled_amount"] = 0
	b, _ := json.Marshal(data)
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $1, status = 'Paid', updated_at = CURRENT_TIMESTAMP WHERE doctype = 'StaffAdvance' AND id = $2`, schema), b, advanceID); err != nil {
		return err
	}
	return tx.Commit()
}

// netEmployeeAdvancesTx clears up to duePaise of the employee's open advances
// (oldest first, rows locked) inside the caller's transaction and returns the
// paise cleared. Called from PayExpenseClaim when the setting is on.
func netEmployeeAdvancesTx(tx *sql.Tx, schema, employeeID string, duePaise int64) (int64, error) {
	if duePaise <= 0 || employeeID == "" {
		return 0, nil
	}
	rows, err := tx.Query(fmt.Sprintf(`
		SELECT id, data::text FROM %s.documents
		 WHERE doctype = 'StaffAdvance' AND deleted_at IS NULL AND status = 'Paid'
		   AND data->>'employee_id' = $1
		 ORDER BY created_at, id FOR UPDATE`, schema), employeeID)
	if err != nil {
		return 0, err
	}
	type adv struct {
		id   string
		data map[string]interface{}
	}
	var advs []adv
	for rows.Next() {
		var id, raw string
		if err := rows.Scan(&id, &raw); err != nil {
			rows.Close()
			return 0, err
		}
		var d map[string]interface{}
		if json.Unmarshal([]byte(raw), &d) == nil {
			advs = append(advs, adv{id, d})
		}
	}
	rows.Close()
	var cleared int64
	for _, a := range advs {
		if duePaise <= 0 {
			break
		}
		open := RupeesToPaise(numFromInterface(a.data["amount"])) - RupeesToPaise(numFromInterface(a.data["settled_amount"]))
		if open <= 0 {
			continue
		}
		take := open
		if take > duePaise {
			take = duePaise
		}
		settled := RupeesToPaise(numFromInterface(a.data["settled_amount"])) + take
		a.data["settled_amount"] = PaiseToRupees(settled)
		st := employeeAdvanceStatusPaid
		if settled >= RupeesToPaise(numFromInterface(a.data["amount"])) {
			st = employeeAdvanceStatusSettled
		}
		a.data["status"] = st
		b, _ := json.Marshal(a.data)
		if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $1, status = $2, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'StaffAdvance' AND id = $3`, schema), b, st, a.id); err != nil {
			return 0, err
		}
		cleared += take
		duePaise -= take
	}
	return cleared, nil
}
