package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
	"time"
)

func TestStage372MultiEntityIntercompany(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}

	const entityA = "TEST-ICE-ENTITY-A"
	const entityB = "TEST-ICE-ENTITY-B"

	seedEntity := func(id, status string) {
		data, _ := json.Marshal(map[string]interface{}{"id": id, "code": id, "name": id, "status": status})
		db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'LegalEntity', $2, $3, 'system') "+
			"ON CONFLICT (id) DO UPDATE SET status=$3, data=$2", id, data, status)
	}

	cleanup := func() {
		db.DB.Exec("DELETE FROM " + schema + ".gl_postings WHERE document_type IN ('IntercompanyTransaction','JournalVoucher') AND document_id IN (SELECT id FROM " + schema + ".documents WHERE (doctype = 'IntercompanyTransaction' OR (doctype = 'JournalVoucher' AND data->>'narration' LIKE 'TEST-ICE%')))")
		db.DB.Exec("DELETE FROM " + schema + ".approval_log WHERE document_id IN (SELECT id FROM " + schema + ".documents WHERE doctype IN ('IntercompanyTransaction','JournalVoucher') AND (data->>'narration' LIKE 'TEST-ICE%' OR doctype = 'IntercompanyTransaction'))")
		db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'IntercompanyTransaction'")
		db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'JournalVoucher' AND data->>'narration' LIKE 'TEST-ICE%'")
		db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'LegalEntity' AND id IN ($1, $2)", entityA, entityB)
	}
	cleanup()
	defer cleanup()

	t.Run("validation: distinct Active entities and real GL accounts are required", func(t *testing.T) {
		cleanup()
		seedEntity(entityA, "Active")
		seedEntity(entityB, "Inactive")

		if _, err := CreateIntercompanyTransaction(tenantID, "2026-08-01", "TEST-ICE bad", entityA, "4100", entityA, "5400", 100, "admin"); err == nil {
			t.Fatalf("expected same from/to entity to be rejected")
		}
		if _, err := CreateIntercompanyTransaction(tenantID, "2026-08-01", "TEST-ICE bad", entityA, "4100", entityB, "5400", 100, "admin"); err == nil {
			t.Fatalf("expected an Inactive to_entity to be rejected")
		}
		if _, err := CreateIntercompanyTransaction(tenantID, "2026-08-01", "TEST-ICE bad", entityA, "4100", "TEST-ICE-NOPE", "5400", 100, "admin"); err == nil {
			t.Fatalf("expected an unregistered entity to be rejected")
		}
		seedEntity(entityB, "Active")
		if _, err := CreateIntercompanyTransaction(tenantID, "2026-08-01", "TEST-ICE bad", entityA, "NOPE-ACCOUNT", entityB, "5400", 100, "admin"); err == nil {
			t.Fatalf("expected an unregistered account code to be rejected")
		}
		if _, err := CreateIntercompanyTransaction(tenantID, "2026-08-01", "TEST-ICE bad", entityA, "4100", entityB, "5400", 0, "admin"); err == nil {
			t.Fatalf("expected a non-positive amount to be rejected")
		}
		if err := ValidateIntercompanyTransactionDocument(tenantID, map[string]interface{}{
			"from_entity": entityA, "to_entity": entityA, "from_account_code": "4100", "to_account_code": "5400", "amount": 100,
		}); err == nil {
			t.Fatalf("expected the generic-API choke point to reject same from/to entity too")
		}
	})

	t.Run("an approved intercompany transaction posts two mirrored, entity-tagged legs and both books' trial balances see them", func(t *testing.T) {
		cleanup()
		seedEntity(entityA, "Active")
		seedEntity(entityB, "Active")

		today := time.Now().Format("2006-01-02")
		id, err := CreateIntercompanyTransaction(tenantID, today, "TEST-ICE management fee", entityA, "4100", entityB, "5400", 1000, "manager1")
		if err != nil {
			t.Fatalf("CreateIntercompanyTransaction: %v", err)
		}
		if err := SubmitForApproval(tenantID, "IntercompanyTransaction", id, "manager1", "Store Manager"); err != nil {
			t.Fatalf("SubmitForApproval: %v", err)
		}
		if err := DecideApproval(tenantID, "IntercompanyTransaction", id, "admin", "HR/Admin", "HO", "Approved", "ok"); err != nil {
			t.Fatalf("DecideApproval: %v", err)
		}

		var status string
		if err := db.DB.QueryRow("SELECT status FROM "+schema+".documents WHERE id=$1", id).Scan(&status); err != nil {
			t.Fatalf("query status: %v", err)
		}
		if status != "Posted" {
			t.Fatalf("expected Posted, got %s", status)
		}

		type leg struct {
			entity              string
			account             string
			wantDebit, wantCred int
		}
		for _, l := range []leg{
			{entityA, "1700", 100000, 0},
			{entityA, "4100", 0, 100000},
			{entityB, "5400", 100000, 0},
			{entityB, "2500", 0, 100000},
		} {
			var debit, credit int
			var gotEntity string
			if err := db.DB.QueryRow("SELECT debit, credit, entity FROM "+schema+".gl_postings WHERE document_type='IntercompanyTransaction' AND document_id=$1 AND account_code=$2", id, l.account).
				Scan(&debit, &credit, &gotEntity); err != nil {
				t.Fatalf("query gl_postings for account %s: %v", l.account, err)
			}
			if debit != l.wantDebit || credit != l.wantCred {
				t.Fatalf("account %s: expected debit=%d credit=%d, got debit=%d credit=%d", l.account, l.wantDebit, l.wantCred, debit, credit)
			}
			if gotEntity != l.entity {
				t.Fatalf("account %s: expected entity=%s, got %s", l.account, l.entity, gotEntity)
			}
		}

		rangeStart := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
		rangeEnd := time.Now().AddDate(0, 0, 1).Format("2006-01-02")
		rows, err := GetEntityTrialBalance(tenantID, rangeStart, rangeEnd)
		if err != nil {
			t.Fatalf("GetEntityTrialBalance: %v", err)
		}
		foundA, foundB := false, false
		for _, r := range rows {
			if r["entity"] == entityA && r["account_code"] == "1700" {
				foundA = true
				if amt, _ := r["total_debit"].(float64); amt != 1000 {
					t.Fatalf("entity A 1700 debit: expected 1000, got %v", amt)
				}
			}
			if r["entity"] == entityB && r["account_code"] == "2500" {
				foundB = true
				if amt, _ := r["total_credit"].(float64); amt != 1000 {
					t.Fatalf("entity B 2500 credit: expected 1000, got %v", amt)
				}
			}
		}
		if !foundA || !foundB {
			t.Fatalf("expected GetEntityTrialBalance to show both entities' legs, got %+v", rows)
		}

		t.Run("reconciliation shows the pair in balance", func(t *testing.T) {
			asOf := time.Now().AddDate(0, 0, 1).Format("2006-01-02")
			recon, err := GetIntercompanyReconciliation(tenantID, asOf)
			if err != nil {
				t.Fatalf("GetIntercompanyReconciliation: %v", err)
			}
			found := false
			for _, r := range recon {
				if r["from_entity"] == entityA && r["to_entity"] == entityB {
					found = true
					if inBalance, _ := r["in_balance"].(bool); !inBalance {
						t.Fatalf("expected the pair to be in balance, got %+v", r)
					}
					if amt, _ := r["ic_ledger_amount"].(float64); amt != 1000 {
						t.Fatalf("expected ic_ledger_amount=1000, got %v", amt)
					}
				}
			}
			if !found {
				t.Fatalf("expected a reconciliation row for (%s, %s), got %+v", entityA, entityB, recon)
			}
		})

		t.Run("consolidation eliminates the intercompany balance sheet and P&L pair", func(t *testing.T) {
			asOf := time.Now().AddDate(0, 0, 1).Format("2006-01-02")
			eliminations, err := ComputeIntercompanyEliminations(tenantID, asOf)
			if err != nil {
				t.Fatalf("ComputeIntercompanyEliminations: %v", err)
			}
			byAccount := map[string]IntercompanyElimination{}
			for _, e := range eliminations {
				byAccount[e.AccountCode] = e
			}
			if e, ok := byAccount["1700"]; !ok || e.Credit != 1000 {
				t.Fatalf("expected a 1700 elimination crediting 1000, got %+v (ok=%v)", e, ok)
			}
			if e, ok := byAccount["2500"]; !ok || e.Debit != 1000 {
				t.Fatalf("expected a 2500 elimination debiting 1000, got %+v (ok=%v)", e, ok)
			}
			if e, ok := byAccount["4100"]; !ok || e.Debit != 1000 {
				t.Fatalf("expected the Revenue account 4100 to be eliminated (debited) by 1000, got %+v (ok=%v)", e, ok)
			}
			if e, ok := byAccount["5400"]; !ok || e.Credit != 1000 {
				t.Fatalf("expected the Expense account 5400 to be eliminated (credited) by 1000, got %+v (ok=%v)", e, ok)
			}

			consolidated, err := GetConsolidatedTrialBalance(tenantID, asOf)
			if err != nil {
				t.Fatalf("GetConsolidatedTrialBalance: %v", err)
			}
			accounts, _ := consolidated["accounts"].([]map[string]interface{})
			for _, row := range accounts {
				code, _ := row["account_code"].(string)
				if code != "1700" && code != "2500" {
					continue
				}
				preDebit, _ := row["pre_elimination_debit"].(float64)
				preCredit, _ := row["pre_elimination_credit"].(float64)
				postDebit, _ := row["consolidated_debit"].(float64)
				postCredit, _ := row["consolidated_credit"].(float64)
				netPre := preDebit - preCredit
				netPost := postDebit - postCredit
				if code == "1700" && (netPre == 0 || netPost != 0) {
					t.Fatalf("expected 1700's net balance to go from non-zero (%v) to zero after elimination, got %v", netPre, netPost)
				}
				if code == "2500" && (netPre == 0 || netPost != 0) {
					t.Fatalf("expected 2500's net balance to go from non-zero (%v) to zero after elimination, got %v", netPre, netPost)
				}
			}
		})
	})

	t.Run("retry refuses a Draft transaction and is a safe no-op on an already-Posted one", func(t *testing.T) {
		cleanup()
		seedEntity(entityA, "Active")
		seedEntity(entityB, "Active")
		today := time.Now().Format("2006-01-02")

		draftID, err := CreateIntercompanyTransaction(tenantID, today, "TEST-ICE draft", entityA, "4100", entityB, "5400", 500, "admin")
		if err != nil {
			t.Fatalf("CreateIntercompanyTransaction: %v", err)
		}
		if err := RetryPostApprovedIntercompanyTransaction(tenantID, draftID); err == nil {
			t.Fatalf("expected retry on a Draft transaction to be refused")
		}

		postedID, err := CreateIntercompanyTransaction(tenantID, today, "TEST-ICE posted", entityA, "4100", entityB, "5400", 500, "manager1")
		if err != nil {
			t.Fatalf("CreateIntercompanyTransaction: %v", err)
		}
		if err := SubmitForApproval(tenantID, "IntercompanyTransaction", postedID, "manager1", "Store Manager"); err != nil {
			t.Fatalf("SubmitForApproval: %v", err)
		}
		if err := DecideApproval(tenantID, "IntercompanyTransaction", postedID, "admin", "HR/Admin", "HO", "Approved", "ok"); err != nil {
			t.Fatalf("DecideApproval: %v", err)
		}
		// Matches RetryPostApprovedJournalVoucher's own precedent exactly:
		// retry is for an Approved/Partially Posted document stuck short of
		// Posted, not a re-run of an already-Posted one.
		if err := RetryPostApprovedIntercompanyTransaction(tenantID, postedID); err == nil {
			t.Fatalf("expected retry on an already-Posted transaction to be refused")
		}
	})

	t.Run("PostingOptions.Entity tags a plain journal voucher's postings, and rejects an unregistered entity", func(t *testing.T) {
		cleanup()
		seedEntity(entityA, "Active")
		today := time.Now().Format("2006-01-02")

		if _, err := CreateJournalVoucher(tenantID, today, "TEST-ICE JV bad entity", []JournalVoucherLine{{AccountCode: "5100", Debit: 10}, {AccountCode: "1100", Credit: 10}}, "manager1", JournalVoucherOptions{Entity: "TEST-ICE-NOPE"}); err == nil {
			t.Fatalf("expected an unregistered entity to reject voucher creation")
		}

		voucherID, err := CreateJournalVoucher(tenantID, today, "TEST-ICE JV", []JournalVoucherLine{{AccountCode: "5100", Debit: 700}, {AccountCode: "1100", Credit: 700}}, "manager1", JournalVoucherOptions{Entity: entityA})
		if err != nil {
			t.Fatalf("CreateJournalVoucher: %v", err)
		}
		if err := SubmitForApproval(tenantID, "JournalVoucher", voucherID, "manager1", "Store Manager"); err != nil {
			t.Fatalf("SubmitForApproval: %v", err)
		}
		if err := DecideApproval(tenantID, "JournalVoucher", voucherID, "admin", "HR/Admin", "HO", "Approved", "ok"); err != nil {
			t.Fatalf("DecideApproval: %v", err)
		}
		var gotEntity string
		if err := db.DB.QueryRow("SELECT entity FROM "+schema+".gl_postings WHERE document_type='JournalVoucher' AND document_id=$1 AND account_code='5100'", voucherID).Scan(&gotEntity); err != nil {
			t.Fatalf("query gl_postings entity: %v", err)
		}
		if gotEntity != entityA {
			t.Fatalf("expected gl_postings.entity=%s, got %s", entityA, gotEntity)
		}
	})
}
