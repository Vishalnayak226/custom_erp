package engines

import (
	"custom_erp/db"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// TestStage140142DeletedRecordsDoNotBlockDuplicateChecks: a deleted expense
// claim, a deleted employee and a deleted leave no longer make the same new
// record look like a duplicate.
func TestStage140142DeletedRecordsDoNotBlockDuplicateChecks(t *testing.T) {
	db.InitDB(testConnStr())
	const tenantID = "default"
	schema, _ := db.GetTenantSchema(tenantID)
	ids := []string{"S140142-EC", "S140142-EMP", "S140142-LV"}
	clean := func() {
		for _, id := range ids {
			db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", id)
		}
	}
	clean()
	defer clean()
	put := func(id, doctype string, data map[string]interface{}) {
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, 'Draft', 'system')", id, doctype, raw); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
	day := time.Now().AddDate(0, 0, -3).Format("2006-01-02")
	put("S140142-EC", "ExpenseClaim", map[string]interface{}{"employee_id": "S140142-E", "category": "Travel", "expense_date": day, "amount": 500.0})
	put("S140142-EMP", "Employee", map[string]interface{}{"code": "S140142-CODE", "name": "x"})
	put("S140142-LV", "Leave", map[string]interface{}{"employee_id": "S140142-E", "leave_type": "Casual", "from_date": "2031-03-03", "to_date": "2031-03-04", "days": 2.0})

	claim := map[string]interface{}{"employee_id": "S140142-E", "category": "Travel", "expense_date": day, "amount": 500.0}
	emp := map[string]interface{}{"code": "S140142-CODE"}
	leave := map[string]interface{}{"employee_id": "S140142-E", "from_date": "2031-03-03", "to_date": "2031-03-04"}
	dup := func(err error) bool {
		return err != nil && (strings.Contains(err.Error(), "already") || strings.Contains(err.Error(), "overlaps"))
	}

	// Live records still block (the checks keep working).
	if !dup(ValidateExpenseClaimControls(tenantID, claim)) || !dup(validateEmployeeRules(tenantID, "S140142-NEW", emp)) || !dup(validateLeaveRules(tenantID, "S140142-NEW", leave)) {
		t.Fatalf("a live record must still block its duplicate")
	}
	for _, id := range ids {
		db.DB.Exec("UPDATE "+schema+".documents SET deleted_at = CURRENT_TIMESTAMP WHERE id = $1", id)
	}
	if err := ValidateExpenseClaimControls(tenantID, claim); dup(err) {
		t.Errorf("a deleted claim still blocks: %v", err)
	}
	if err := validateEmployeeRules(tenantID, "S140142-NEW", emp); dup(err) {
		t.Errorf("a deleted employee still blocks its code: %v", err)
	}
	if err := validateLeaveRules(tenantID, "S140142-NEW", leave); dup(err) {
		t.Errorf("a deleted leave still blocks overlap: %v", err)
	}
}
