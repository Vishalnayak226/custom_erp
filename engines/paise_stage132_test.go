package engines

import (
	"custom_erp/db"
	"testing"
)

// Stage 132.17: money is posted to the paisa. Guide paid an expense claim of
// 1,250.75 and the books held 1,250; an asset's cost was capitalised the same
// way.
func TestStage132ExpenseAndAssetKeepPaise(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, _ := db.GetTenantSchema(tenantID)
	cleanup := func() {
		db.DB.Exec("DELETE FROM " + schema + ".gl_postings WHERE document_id LIKE 'T13217-%'")
		db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id LIKE 'T13217-%'")
	}
	cleanup()
	defer cleanup()
	leg := func(docID, account string) (debit, credit int64) {
		db.DB.QueryRow("SELECT COALESCE(SUM(debit),0), COALESCE(SUM(credit),0) FROM "+schema+".gl_postings WHERE document_id = $1 AND account_code = $2", docID, account).Scan(&debit, &credit)
		return
	}

	insertTestDoc(t, schema, "T13217-CL", "ExpenseClaim", map[string]interface{}{"employee_id": "T13217-E", "amount": 1250.75, "status": "Verified"})
	db.DB.Exec("UPDATE " + schema + ".documents SET status = 'Verified' WHERE id = 'T13217-CL'")
	payable, err := PayExpenseClaim(tenantID, "T13217-CL")
	if err != nil || payable != 1250.75 {
		t.Fatalf("PayExpenseClaim = %v, %v (want 1250.75)", payable, err)
	}
	if d, _ := leg("T13217-CL", "5400"); d != 125075 {
		t.Errorf("expense booked %d paise to 5400, want 125075", d)
	}
	if _, c := leg("T13217-CL", "1100"); c != 125075 {
		t.Errorf("expense paid %d paise from 1100, want 125075", c)
	}

	insertTestDoc(t, schema, "T13217-AS", "Asset", map[string]interface{}{"cost": 75000.5, "useful_life_years": 5.0, "status": "Draft"})
	db.DB.Exec("UPDATE " + schema + ".documents SET status = 'Draft' WHERE id = 'T13217-AS'")
	if err := CapitalizeAsset(tenantID, "T13217-AS"); err != nil {
		t.Fatalf("CapitalizeAsset: %v", err)
	}
	if d, _ := leg("T13217-AS", "1400"); d != 7500050 {
		t.Errorf("asset capitalised at %d paise, want 7500050", d)
	}
	data, _, _ := fetchAssetData(tenantID, "T13217-AS")
	if plan, ok := assetDepreciationPlan(data, ""); !ok || plan.costPaise != 7500050 {
		t.Errorf("depreciation plan clears %d paise, want what was capitalised (7500050)", plan.costPaise)
	}

	// An asset capitalised before the fix posted whole rupees; depreciation
	// and disposal keep clearing exactly that.
	legacy := map[string]interface{}{"cost": 75000.5, "status": "Capitalised"}
	if got := assetCostPaise(legacy); got != 7500000 {
		t.Errorf("an older capitalised asset clears %d paise, want 7500000 (what it posted)", got)
	}
}
