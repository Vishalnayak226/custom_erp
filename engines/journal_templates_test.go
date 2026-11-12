package engines

import (
	"custom_erp/db"
	"testing"
)

// Stage 148.21: a saved journal layout is checked when it is saved, and it
// is a record of its own that nothing posts.

func TestStage14821TemplateSaveChecksTheSplit(t *testing.T) {
	db.InitDB(testConnStr())
	pct := `[{"account_code":"5100","debit_pct":60},{"account_code":"5200","debit_pct":40},{"account_code":"1100","credit_pct":100}]`
	if err := ValidateDocument("default", "JournalTemplate", map[string]interface{}{"name": "T1", "split_mode": "Percent", "lines": pct, "status": "Active"}); err != nil {
		t.Fatalf("a 100/100 split must save: %v", err)
	}
	bad := `[{"account_code":"5100","debit_pct":60},{"account_code":"1100","credit_pct":100}]`
	err := ValidateDocument("default", "JournalTemplate", map[string]interface{}{"name": "T2", "split_mode": "Percent", "lines": bad, "status": "Active"})
	ve, ok := err.(*ValidationError)
	if !ok || ve.Code != "FIN-0953" {
		t.Fatalf("want FIN-0953 for a split that is not 100, got %v", err)
	}
	amt := `[{"account_code":"5100","debit":500},{"account_code":"1100","credit":400}]`
	err = ValidateDocument("default", "JournalTemplate", map[string]interface{}{"name": "T3", "split_mode": "Amount", "lines": amt, "status": "Active"})
	if ve, ok := err.(*ValidationError); !ok || ve.Code != "FIN-0953" {
		t.Fatalf("want FIN-0953 for unbalanced amounts, got %v", err)
	}
	if err := ValidateDocument("default", "JournalTemplate", map[string]interface{}{"name": "T4", "split_mode": "Amount", "lines": `[{"account_code":"5100"}]`, "status": "Active"}); err == nil {
		t.Fatal("a template with one line must be refused")
	}
}

func TestStage14821RuleSaveChecksTheRange(t *testing.T) {
	db.InitDB(testConnStr())
	ok := map[string]interface{}{"kind": "Required", "account_from": "5000", "account_to": "5999", "dimension": "department", "status": "Active"}
	if err := ValidateDocument("default", "JournalDimensionRule", ok); err != nil {
		t.Fatalf("valid rule: %v", err)
	}
	for name, p := range map[string]map[string]interface{}{
		"no dimension":   {"kind": "Required", "account_from": "5000", "status": "Active"},
		"reversed range": {"kind": "Exempt", "account_from": "5999", "account_to": "5000", "status": "Active"},
	} {
		err := ValidateDocument("default", "JournalDimensionRule", p)
		if ve, isVE := err.(*ValidationError); !isVE || ve.Code != "FIN-0954" {
			t.Fatalf("%s: want FIN-0954, got %v", name, err)
		}
	}
}

func TestStage14821TemplateIsNeverAVoucher(t *testing.T) {
	f := newJD20(t)
	f.doc(t, "JD20-TPL", "JournalTemplate", "Active", map[string]interface{}{"name": "Rent", "split_mode": "Amount", "lines": `[{"account_code":"5100","debit":100},{"account_code":"1100","credit":100}]`})
	runRecurringJournalsForSchema(f.schema)
	var n int
	db.DB.QueryRow("SELECT count(*) FROM " + f.schema + ".documents WHERE doctype='JournalVoucher' AND data->>'narration' LIKE 'Rent%'").Scan(&n)
	var posted int
	db.DB.QueryRow("SELECT count(*) FROM " + f.schema + ".gl_postings WHERE document_id='JD20-TPL'").Scan(&posted)
	if posted != 0 {
		t.Fatalf("a template posted %d rows", posted)
	}
}
