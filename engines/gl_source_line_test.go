package engines

import (
	"encoding/json"
	"testing"

	"custom_erp/db"
)

// Stage 170.1: with finance.gl_source_line on, a sales invoice posts one row
// per account per line, each naming its line, with the same totals; off, it
// posts one row per account with no line. GL Drill-down shows the line.
func TestStage170GLSourceLine(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	ids := []string{"T1701-ON", "T1701-OFF"}
	cleanup := func() {
		for _, id := range ids {
			db.DB.Exec("DELETE FROM "+schema+".gl_postings WHERE document_id = $1", id)
			db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", id)
		}
		_ = SetSetting(tenantID, SettingGLSourceLine, "false", "test")
		db.DB.Exec("DELETE FROM "+schema+".system_settings WHERE key = $1", SettingGLSourceLine)
	}
	cleanup()
	defer cleanup()
	insert := func(id string) {
		raw, _ := json.Marshal(map[string]interface{}{"invoice_number": id, "total_amount": 1000, "location": "HO",
			"items": []map[string]interface{}{
				{"sku": "T1701-NOSTOCK-A", "qty": 1, "amount": 300},
				{"sku": "T1701-NOSTOCK-B", "qty": 1, "amount": 700},
			}})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'SalesInvoice', $2, 'Draft', 'admin')", id, raw); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	type row struct {
		account, line string
		debit, credit int64
	}
	rowsOf := func(id string) []row {
		rs, err := db.DB.Query("SELECT account_code, COALESCE(source_line_id,''), debit, credit FROM "+schema+".gl_postings WHERE document_id = $1 ORDER BY account_code, source_line_id", id)
		if err != nil {
			t.Fatalf("rows: %v", err)
		}
		defer rs.Close()
		var out []row
		for rs.Next() {
			var r row
			rs.Scan(&r.account, &r.line, &r.debit, &r.credit)
			out = append(out, r)
		}
		return out
	}

	// Off (the default): one row per account, no line named.
	insert("T1701-OFF")
	if _, err := PostSalesInvoice(tenantID, "T1701-OFF", "admin"); err != nil {
		t.Fatalf("post with the setting off: %v", err)
	}
	off := rowsOf("T1701-OFF")
	if len(off) != 2 {
		t.Fatalf("setting off: %d rows, want 2 (one per account): %+v", len(off), off)
	}
	for _, r := range off {
		if r.line != "" {
			t.Errorf("setting off: row %+v names a line", r)
		}
	}

	// On: one row per account per line, split 300 / 700, same totals.
	if err := SetSetting(tenantID, SettingGLSourceLine, "true", "test"); err != nil {
		t.Fatalf("turn the setting on: %v", err)
	}
	insert("T1701-ON")
	if _, err := PostSalesInvoice(tenantID, "T1701-ON", "admin"); err != nil {
		t.Fatalf("post with the setting on: %v", err)
	}
	on := rowsOf("T1701-ON")
	if len(on) != 4 {
		t.Fatalf("setting on: %d rows, want 4 (2 accounts x 2 lines): %+v", len(on), on)
	}
	byLine := map[string]int64{}
	var sumD, sumC int64
	for _, r := range on {
		sumD += r.debit
		sumC += r.credit
		if r.account == "1300" {
			byLine[r.line] += r.debit
		}
	}
	if byLine["1"] != 30000 || byLine["2"] != 70000 {
		t.Errorf("receivable by line = %v paise, want 1: 30000, 2: 70000", byLine)
	}
	if sumD != 100000 || sumC != 100000 {
		t.Errorf("totals debit %d / credit %d paise, want 100000 each", sumD, sumC)
	}

	// GL Drill-down shows the line on the invoice's rows.
	book, err := getGLAccountBook(tenantID, "1300")
	if err != nil {
		t.Fatalf("drill: %v", err)
	}
	seen := map[string]bool{}
	for _, r := range book {
		if r["document_id"] == "T1701-ON" {
			seen[r["source_line_id"].(string)] = true
		}
	}
	if !seen["1"] || !seen["2"] {
		t.Errorf("GL drill-down lines for the invoice = %v, want 1 and 2", seen)
	}
}

// spreadByShares always adds up to the total, the last weighted share taking
// the remainder, and skips lines with no weight.
func TestStage170SpreadByShares(t *testing.T) {
	got := spreadByShares(100, []sourceLineShare{{ID: "1", Weight: 1}, {ID: "2", Weight: 0}, {ID: "3", Weight: 2}})
	if got[0] != 33 || got[1] != 0 || got[2] != 67 {
		t.Errorf("spread = %v, want [33 0 67]", got)
	}
	if got := spreadByShares(100, []sourceLineShare{{ID: "1", Weight: 0}}); got[0] != 0 {
		t.Errorf("no weight: %v, want [0]", got)
	}
}
