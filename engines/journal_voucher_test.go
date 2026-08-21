package engines

import (
	"custom_erp/db"
	"testing"
	"time"
)

func TestJournalVoucherLifecycle(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}

	cleanup := func() {
		db.DB.Exec("DELETE FROM " + schema + ".gl_postings WHERE document_type = 'JournalVoucher' AND document_id IN (SELECT id FROM " + schema + ".documents WHERE doctype = 'JournalVoucher' AND data->>'narration' LIKE 'TEST-JV%')")
		db.DB.Exec("DELETE FROM " + schema + ".approval_log WHERE doctype = 'JournalVoucher' AND document_id IN (SELECT id FROM " + schema + ".documents WHERE doctype = 'JournalVoucher' AND data->>'narration' LIKE 'TEST-JV%')")
		db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'JournalVoucher' AND data->>'narration' LIKE 'TEST-JV%'")
	}
	cleanup()
	defer cleanup()

	t.Run("validation rejects unbalanced/single-line/negative/zero-amount", func(t *testing.T) {
		if _, err := CreateJournalVoucher(tenantID, "2026-07-25", "TEST-JV bad", []JournalVoucherLine{{AccountCode: "5100", Debit: 100}}, "system"); err == nil {
			t.Fatalf("expected rejection for a single-line voucher")
		}
		if _, err := CreateJournalVoucher(tenantID, "2026-07-25", "TEST-JV bad", []JournalVoucherLine{{AccountCode: "5100", Debit: 100}, {AccountCode: "1100", Credit: 50}}, "system"); err == nil {
			t.Fatalf("expected rejection for an unbalanced voucher")
		}
		if _, err := CreateJournalVoucher(tenantID, "2026-07-25", "TEST-JV bad", []JournalVoucherLine{{AccountCode: "5100", Debit: -100}, {AccountCode: "1100", Credit: -100}}, "system"); err == nil {
			t.Fatalf("expected rejection for negative amounts")
		}
		if _, err := CreateJournalVoucher(tenantID, "2026-07-25", "TEST-JV bad", []JournalVoucherLine{{AccountCode: "5100", Debit: 0}, {AccountCode: "1100", Credit: 0}}, "system"); err == nil {
			t.Fatalf("expected rejection for a zero-amount voucher")
		}
		if _, err := CreateJournalVoucher(tenantID, "2026-07-25", "TEST-JV bad", []JournalVoucherLine{{AccountCode: "5100", Debit: 100, Credit: 100}, {AccountCode: "1100", Credit: 100}}, "system"); err == nil {
			t.Fatalf("expected rejection for a line with both a debit and a credit")
		}
	})

	t.Run("approval decides Approved posts to GL and reversal swaps lines", func(t *testing.T) {
		cleanup()
		voucherID, err := CreateJournalVoucher(tenantID, "2026-07-25", "TEST-JV lifecycle", []JournalVoucherLine{{AccountCode: "5100", Debit: 777}, {AccountCode: "1100", Credit: 777}}, "manager1")
		if err != nil {
			t.Fatalf("CreateJournalVoucher: %v", err)
		}

		if err := SubmitForApproval(tenantID, "JournalVoucher", voucherID, "manager1", "Store Manager"); err != nil {
			t.Fatalf("SubmitForApproval: %v", err)
		}
		if err := DecideApproval(tenantID, "JournalVoucher", voucherID, "admin", "HR/Admin", "HO", "Approved", "ok"); err != nil {
			t.Fatalf("DecideApproval: %v", err)
		}

		var status string
		if err := db.DB.QueryRow("SELECT status FROM "+schema+".documents WHERE id=$1", voucherID).Scan(&status); err != nil {
			t.Fatalf("query status: %v", err)
		}
		if status != "Posted" {
			t.Fatalf("expected status Posted after approval, got %s", status)
		}

		// gl_postings stores paise (Stage 45): a 777-rupee line is 77700 paise.
		var debit, credit int64
		if err := db.DB.QueryRow("SELECT COALESCE(SUM(debit),0), COALESCE(SUM(credit),0) FROM "+schema+".gl_postings WHERE document_type='JournalVoucher' AND document_id=$1", voucherID).Scan(&debit, &credit); err != nil {
			t.Fatalf("query gl_postings: %v", err)
		}
		if debit != 77700 || credit != 77700 {
			t.Fatalf("expected gl_postings debit=77700 credit=77700, got debit=%d credit=%d", debit, credit)
		}

		reversalID, err := ReverseJournalVoucher(tenantID, voucherID, "admin")
		if err != nil {
			t.Fatalf("ReverseJournalVoucher: %v", err)
		}
		if err := db.DB.QueryRow("SELECT status FROM "+schema+".documents WHERE id=$1", voucherID).Scan(&status); err != nil {
			t.Fatalf("query reversed status: %v", err)
		}
		if status != "Reversed" {
			t.Fatalf("expected original status Reversed, got %s", status)
		}
		data, _, err := fetchJournalVoucher(schema, reversalID)
		if err != nil {
			t.Fatalf("fetchJournalVoucher(reversal): %v", err)
		}
		lines, err := journalVoucherLinesFromData(data)
		if err != nil {
			t.Fatalf("journalVoucherLinesFromData: %v", err)
		}
		if len(lines) != 2 || lines[0].Credit != 777 || lines[1].Debit != 777 {
			t.Fatalf("expected reversal lines to be swapped, got %+v", lines)
		}
		if data["reversed_from"] != voucherID {
			t.Fatalf("expected reversed_from=%s, got %v", voucherID, data["reversed_from"])
		}

		if _, err := ReverseJournalVoucher(tenantID, voucherID, "admin"); err == nil {
			t.Fatalf("expected re-reversing an already-Reversed voucher to be rejected")
		}
	})

	t.Run("recurring template spawns a Draft instance and advances next_run_date", func(t *testing.T) {
		cleanup()
		// One day in the past (not far in the past) so a single sweep
		// advances next_run_date by one Monthly period into the future -
		// otherwise a second tick would correctly (this is catch-up
		// behavior, not a bug) spawn another missed-period instance, which
		// isn't what this test is checking.
		yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
		templateID, err := CreateRecurringJournalTemplate(tenantID, "TEST-JV recurring rent", "Monthly", yesterday, []JournalVoucherLine{{AccountCode: "5100", Debit: 500}, {AccountCode: "1100", Credit: 500}}, "manager1")
		if err != nil {
			t.Fatalf("CreateRecurringJournalTemplate: %v", err)
		}

		runRecurringJournalsForSchema(schema)

		var status string
		if err := db.DB.QueryRow("SELECT status FROM "+schema+".documents WHERE id=$1", templateID).Scan(&status); err != nil {
			t.Fatalf("query template status: %v", err)
		}
		if status != "Recurring Template" {
			t.Fatalf("expected template to remain Recurring Template, got %s", status)
		}
		templateData, _, err := fetchJournalVoucher(schema, templateID)
		if err != nil {
			t.Fatalf("fetchJournalVoucher(template): %v", err)
		}
		wantNext, _ := advanceRecurringDate(yesterday, "Monthly")
		if templateData["next_run_date"] != wantNext {
			t.Fatalf("expected next_run_date advanced to %s, got %v", wantNext, templateData["next_run_date"])
		}

		var spawnedCount int
		if err := db.DB.QueryRow("SELECT COUNT(*) FROM "+schema+".documents WHERE doctype='JournalVoucher' AND status='Draft' AND data->>'narration' = 'TEST-JV recurring rent'").Scan(&spawnedCount); err != nil {
			t.Fatalf("query spawned instances: %v", err)
		}
		if spawnedCount != 1 {
			t.Fatalf("expected exactly one spawned Draft instance, got %d", spawnedCount)
		}

		// A second run on the same tick should not spawn a duplicate -
		// next_run_date has already moved past today.
		runRecurringJournalsForSchema(schema)
		if err := db.DB.QueryRow("SELECT COUNT(*) FROM "+schema+".documents WHERE doctype='JournalVoucher' AND status='Draft' AND data->>'narration' = 'TEST-JV recurring rent'").Scan(&spawnedCount); err != nil {
			t.Fatalf("query spawned instances (2nd run): %v", err)
		}
		if spawnedCount != 1 {
			t.Fatalf("expected still exactly one spawned instance after a second tick, got %d", spawnedCount)
		}
	})

	t.Run("advanceRecurringDate covers all four frequencies", func(t *testing.T) {
		cases := map[string]string{"Daily": "2026-01-02", "Weekly": "2026-01-08", "Monthly": "2026-02-01", "Yearly": "2027-01-01"}
		for freq, want := range cases {
			got, err := advanceRecurringDate("2026-01-01", freq)
			if err != nil {
				t.Fatalf("advanceRecurringDate(%s): %v", freq, err)
			}
			if got != want {
				t.Fatalf("advanceRecurringDate(%s): expected %s, got %s", freq, want, got)
			}
		}
		if _, err := advanceRecurringDate("2026-01-01", "Fortnightly"); err == nil {
			t.Fatalf("expected an unknown frequency to error")
		}
	})
}
