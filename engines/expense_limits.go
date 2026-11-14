package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"strings"
)

// Ask 170 item 170.29: expense limits by category (and employee grade).
//
// An ExpenseCategory names a limit per claim, optionally a different limit per
// employee grade (Employee.grade). A claim whose category matches an Active
// category and whose amount is over the limit that applies to its claimant is
// either refused (EXP-1700) or sent to a higher approver, as the setting
// says. A claim whose category has no limit - or no category master at all -
// is untouched, so with nothing configured nothing changes.

const (
	SettingExpenseLimitAction       = "expense.limit_action"
	SettingExpenseOverLimitApprover = "expense.over_limit_approver_role"
	expenseLimitRefuse              = "refuse"
	expenseLimitApproval            = "approval"
)

func init() {
	RegisterSetting(SettingDefinition{
		Key: SettingExpenseLimitAction, Module: "Expenses",
		Label: "Claim over its category limit", Type: SettingTypeSelect, Default: expenseLimitApproval,
		Options:     []SettingOption{{Value: expenseLimitApproval, Label: "Send for approval"}, {Value: expenseLimitRefuse, Label: "Refuse the claim"}},
		Description: "Ask 170.29: what happens to an expense claim over the limit set on its category (or on its claimant's grade). approval: it goes to the approver role named below. refuse: it cannot be saved. Categories with no limit are not affected.",
	})
	RegisterSetting(SettingDefinition{
		Key: SettingExpenseOverLimitApprover, Module: "Expenses",
		Label: "Approver for an over-limit claim", Type: SettingTypeString, Default: "Super Admin",
		Description: "Ask 170.29: the role that approves a claim over its category limit when the setting above is approval.",
	})
}

// expenseLimitFor returns the limit that applies to a claim (false when the
// category has no limit). A grade limit beats the category's plain limit.
func expenseLimitFor(tenantID string, claim map[string]interface{}) (float64, string, bool) {
	cat := strings.ToLower(strField(claim, "category"))
	if cat == "" {
		return 0, "", false
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, "", false
	}
	var raw string
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT data::text FROM %s.documents
		 WHERE doctype = 'ExpenseCategory' AND deleted_at IS NULL
		   AND COALESCE(data->>'status','Active') = 'Active'
		   AND (lower(data->>'name') = $1 OR lower(data->>'code') = $1)
		 ORDER BY created_at LIMIT 1`, schema), cat).Scan(&raw)
	if err != nil {
		return 0, "", false
	}
	var c map[string]interface{}
	if json.Unmarshal([]byte(raw), &c) != nil {
		return 0, "", false
	}
	name := strField(c, "name")
	if emp := strField(claim, "employee_id"); emp != "" {
		if e, _, err := fetchDocData(tenantID, "Employee", emp); err == nil && e != nil {
			grade := strings.ToLower(strField(e, "grade"))
			if rows, ok := c["grade_limits"].([]interface{}); ok && grade != "" {
				for _, r := range rows {
					m, _ := r.(map[string]interface{})
					if strings.ToLower(strField(m, "grade")) == grade {
						return numFromInterface(m["limit"]), name, true
					}
				}
			}
		}
	}
	if l := numFromInterface(c["limit_per_claim"]); l > 0 {
		return l, name, true
	}
	return 0, name, false
}

// expenseClaimOverLimit reports whether the claim is over its limit.
func expenseClaimOverLimit(tenantID string, claim map[string]interface{}) (bool, float64, string) {
	limit, name, ok := expenseLimitFor(tenantID, claim)
	if !ok {
		return false, 0, ""
	}
	return numFromInterface(claim["amount"]) > limit+0.004, limit, name
}

// validateExpenseClaimLimit runs on every claim save (refuse mode only; in
// approval mode the claim saves and is routed up at submit).
func validateExpenseClaimLimit(tenantID string, payload map[string]interface{}) error {
	if GetSettingString(tenantID, SettingExpenseLimitAction) != expenseLimitRefuse {
		return nil
	}
	if over, limit, name := expenseClaimOverLimit(tenantID, payload); over {
		return &ValidationError{Code: "EXP-1700", Message: fmt.Sprintf("The claim is over the %s limit of %.2f.", name, limit)}
	}
	return nil
}

// expenseOverLimitApproverRole is the role an over-limit claim is routed to,
// or "" when the claim is within its limit (or the setting is not approval).
func expenseOverLimitApproverRole(tenantID string, claim map[string]interface{}) string {
	if GetSettingString(tenantID, SettingExpenseLimitAction) == expenseLimitRefuse {
		return ""
	}
	if over, _, _ := expenseClaimOverLimit(tenantID, claim); !over {
		return ""
	}
	return strings.TrimSpace(GetSettingString(tenantID, SettingExpenseOverLimitApprover))
}
