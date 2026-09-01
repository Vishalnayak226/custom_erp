package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
	"time"
)

func TestStage376DeferredPrepaidRecurringPriceList(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}

	insert := func(id, doctype, status string, data map[string]interface{}) {
		raw, _ := json.Marshal(data)
		db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system') "+
			"ON CONFLICT (id) DO UPDATE SET doctype = $2, data = $3, status = $4", id, doctype, raw, status)
	}
	cleanupIDs := func(ids ...string) {
		for _, id := range ids {
			db.DB.Exec("DELETE FROM "+schema+".gl_postings WHERE document_id = $1", id)
			db.DB.Exec("DELETE FROM "+schema+".approval_log WHERE document_id = $1", id)
			db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", id)
		}
	}

	t.Run("a deferred_revenue invoice credits 2600 not 4100, and its schedule recognizes correctly over time", func(t *testing.T) {
		const invID = "TEST376A-INV"
		cleanupIDs(invID)
		defer cleanupIDs(invID)
		insert(invID, "SalesInvoice", "Draft", map[string]interface{}{
			"code": invID, "customer": "", "total_amount": 1200,
			"deferred_revenue": "Yes", "deferred_term_months": 12,
		})

		amount, err := PostSalesInvoice(tenantID, invID, "system")
		if err != nil {
			t.Fatalf("PostSalesInvoice: %v", err)
		}
		if amount != 1200 {
			t.Fatalf("expected posted amount 1200, got %d", amount)
		}

		var debit1300, credit2600, credit4100 int
		db.DB.QueryRow("SELECT COALESCE(debit,0) FROM "+schema+".gl_postings WHERE document_id=$1 AND account_code='1300'", invID).Scan(&debit1300)
		db.DB.QueryRow("SELECT COALESCE(credit,0) FROM "+schema+".gl_postings WHERE document_id=$1 AND account_code='2600'", invID).Scan(&credit2600)
		db.DB.QueryRow("SELECT COALESCE(credit,0) FROM "+schema+".gl_postings WHERE document_id=$1 AND account_code='4100'", invID).Scan(&credit4100)
		if debit1300 != 120000 {
			t.Fatalf("expected 1300 debited 120000 paise, got %d", debit1300)
		}
		if credit2600 != 120000 {
			t.Fatalf("expected 2600 credited 120000 paise, got %d", credit2600)
		}
		if credit4100 != 0 {
			t.Fatalf("expected 4100 to receive NOTHING from a deferred invoice, got %d", credit4100)
		}

		var scheduleID string
		var termMonths int
		if err := db.DB.QueryRow("SELECT id, (data->>'term_months')::int FROM "+schema+".documents WHERE doctype='DeferredRevenueSchedule' AND data->>'sales_invoice_id'=$1", invID).Scan(&scheduleID, &termMonths); err != nil {
			t.Fatalf("expected a DeferredRevenueSchedule to have been created: %v", err)
		}
		defer cleanupIDs(scheduleID)
		if termMonths != 12 {
			t.Fatalf("expected term_months=12, got %d", termMonths)
		}

		// Force the schedule's next_recognition_date into the past so the
		// worker's due-date filter picks it up on this run.
		db.DB.Exec("UPDATE "+schema+".documents SET data = jsonb_set(data, '{next_recognition_date}', to_jsonb('2020-01-01'::text)) WHERE id=$1", scheduleID)

		recognized, err := runDeferredRevenueRecognition(tenantID, schema)
		if err != nil {
			t.Fatalf("runDeferredRevenueRecognition: %v", err)
		}
		if recognized < 1 {
			t.Fatalf("expected at least 1 schedule recognized, got %d", recognized)
		}

		var debit2600, credit4100Recognized, recognizedMonths int
		db.DB.QueryRow("SELECT COALESCE(SUM(debit),0) FROM "+schema+".gl_postings WHERE document_id=$1 AND account_code='2600'", scheduleID).Scan(&debit2600)
		db.DB.QueryRow("SELECT COALESCE(SUM(credit),0) FROM "+schema+".gl_postings WHERE document_id=$1 AND account_code='4100'", scheduleID).Scan(&credit4100Recognized)
		db.DB.QueryRow("SELECT (data->>'recognized_months')::int FROM "+schema+".documents WHERE id=$1", scheduleID).Scan(&recognizedMonths)
		if debit2600 != 10000 {
			t.Fatalf("expected 2600 debited 10000 paise (1/12 of 120000), got %d", debit2600)
		}
		if credit4100Recognized != 10000 {
			t.Fatalf("expected 4100 credited 10000 paise, got %d", credit4100Recognized)
		}
		if recognizedMonths != 1 {
			t.Fatalf("expected recognized_months=1, got %d", recognizedMonths)
		}
	})

	t.Run("PrepaidExpenseSchedule validation, approval posting, and recognition", func(t *testing.T) {
		const scheduleID = "TEST376B-SCHED"
		cleanupIDs(scheduleID)
		defer cleanupIDs(scheduleID)

		if err := ValidatePrepaidExpenseScheduleDocument(tenantID, map[string]interface{}{"expense_account_code": "1300", "total_amount": 1200.0, "term_months": 12.0}); err == nil {
			t.Fatalf("expected an Asset account (1300) to be rejected as an expense_account_code")
		}
		if err := ValidatePrepaidExpenseScheduleDocument(tenantID, map[string]interface{}{"expense_account_code": "5400", "total_amount": 0.0, "term_months": 12.0}); err == nil {
			t.Fatalf("expected a non-positive total_amount to be rejected")
		}
		if err := ValidatePrepaidExpenseScheduleDocument(tenantID, map[string]interface{}{"expense_account_code": "5400", "total_amount": 1200.0, "term_months": 12.0}); err != nil {
			t.Fatalf("expected a valid schedule to be accepted, got %v", err)
		}

		insert(scheduleID, "PrepaidExpenseSchedule", "Approved", map[string]interface{}{
			"code": scheduleID, "description": "Annual insurance", "total_amount": 1200.0,
			"expense_account_code": "5400", "term_months": 12,
		})
		postApprovedPrepaidExpenseSchedule(tenantID, scheduleID)

		var debit1800, credit1100 int
		db.DB.QueryRow("SELECT COALESCE(debit,0) FROM "+schema+".gl_postings WHERE document_id=$1 AND account_code='1800'", scheduleID).Scan(&debit1800)
		db.DB.QueryRow("SELECT COALESCE(credit,0) FROM "+schema+".gl_postings WHERE document_id=$1 AND account_code='1100'", scheduleID).Scan(&credit1100)
		if debit1800 != 120000 || credit1100 != 120000 {
			t.Fatalf("expected the upfront posting Dr 1800/Cr 1100 for 120000 paise, got debit1800=%d credit1100=%d", debit1800, credit1100)
		}

		db.DB.Exec("UPDATE "+schema+".documents SET data = jsonb_set(data, '{next_recognition_date}', to_jsonb('2020-01-01'::text)) WHERE id=$1", scheduleID)
		recognized, err := runPrepaidExpenseRecognition(tenantID, schema)
		if err != nil {
			t.Fatalf("runPrepaidExpenseRecognition: %v", err)
		}
		if recognized < 1 {
			t.Fatalf("expected at least 1 schedule recognized, got %d", recognized)
		}
		var debit5400, credit1800 int
		db.DB.QueryRow("SELECT COALESCE(SUM(debit),0) FROM "+schema+".gl_postings WHERE document_id=$1 AND account_code='5400'", scheduleID).Scan(&debit5400)
		db.DB.QueryRow("SELECT COALESCE(SUM(credit),0) FROM "+schema+".gl_postings WHERE document_id=$1 AND account_code='1800'", scheduleID).Scan(&credit1800)
		if debit5400 != 10000 || credit1800 != 10000 {
			t.Fatalf("expected the recognition posting Dr 5400/Cr 1800 for 10000 paise, got debit5400=%d credit1800=%d", debit5400, credit1800)
		}
	})

	t.Run("RecurringSalesContract validation and the worker spawning a Draft SalesInvoice", func(t *testing.T) {
		const contractID = "TEST376C-CONTRACT"
		cleanupIDs(contractID)
		defer cleanupIDs(contractID)

		if err := ValidateRecurringSalesContractDocument(tenantID, map[string]interface{}{"billing_frequency": "Fortnightly", "amount": 500.0}); err == nil {
			t.Fatalf("expected an invalid billing_frequency to be rejected")
		}
		if err := ValidateRecurringSalesContractDocument(tenantID, map[string]interface{}{"billing_frequency": "Monthly", "amount": 0.0}); err == nil {
			t.Fatalf("expected a non-positive amount to be rejected")
		}

		yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
		insert(contractID, "RecurringSalesContract", "Active", map[string]interface{}{
			"code": contractID, "customer": "TEST376C-CUST", "description": "Monthly retainer",
			"amount": 999.0, "billing_frequency": "Monthly", "next_billing_date": yesterday,
		})

		runRecurringBillingForSchema(schema)

		var invoiceID string
		var invoiceAmount float64
		if err := db.DB.QueryRow("SELECT id, (data->>'total_amount')::numeric FROM "+schema+".documents WHERE doctype='SalesInvoice' AND data->>'recurring_contract_id'=$1", contractID).Scan(&invoiceID, &invoiceAmount); err != nil {
			t.Fatalf("expected the worker to have spawned a SalesInvoice: %v", err)
		}
		defer cleanupIDs(invoiceID)
		if invoiceAmount != 999 {
			t.Fatalf("expected the spawned invoice's total_amount=999, got %v", invoiceAmount)
		}

		var newNextBillingDate string
		db.DB.QueryRow("SELECT data->>'next_billing_date' FROM "+schema+".documents WHERE id=$1", contractID).Scan(&newNextBillingDate)
		if newNextBillingDate <= yesterday {
			t.Fatalf("expected next_billing_date to have advanced past %s, got %s", yesterday, newNextBillingDate)
		}
	})

	t.Run("PriceListVersion: validation, supersession on approval, and ResolvePriceForSKU", func(t *testing.T) {
		const plCode, v1, v2 = "TEST376D-PL", "TEST376D-V1", "TEST376D-V2"
		cleanupIDs(v1, v2)
		defer cleanupIDs(v1, v2)

		if err := ValidatePriceListVersionDocument(tenantID, map[string]interface{}{"effective_from": "2026-06-01", "effective_to": "2026-01-01"}); err == nil {
			t.Fatalf("expected effective_to before effective_from to be rejected")
		}

		itemsV1, _ := json.Marshal([]map[string]interface{}{{"sku": "TEST376D-SKU", "price": 100.0}})
		insert(v1, "PriceListVersion", "Approved", map[string]interface{}{
			"code": v1, "price_list_code": plCode, "effective_from": "2026-01-01", "effective_to": "", "items": string(itemsV1),
		})
		postApprovedPriceListVersion(tenantID, v1)

		price, found, err := ResolvePriceForSKU(tenantID, plCode, "TEST376D-SKU", "2026-03-15")
		if err != nil {
			t.Fatalf("ResolvePriceForSKU: %v", err)
		}
		if !found || price != 100 {
			t.Fatalf("expected price=100 found=true, got price=%v found=%v", price, found)
		}

		// A second version, effective later, should supersede the first's
		// open-ended window once approved.
		itemsV2, _ := json.Marshal([]map[string]interface{}{{"sku": "TEST376D-SKU", "price": 150.0}})
		insert(v2, "PriceListVersion", "Approved", map[string]interface{}{
			"code": v2, "price_list_code": plCode, "effective_from": "2026-07-01", "effective_to": "", "items": string(itemsV2),
		})
		postApprovedPriceListVersion(tenantID, v2)

		var v1EffectiveTo, v1Status string
		db.DB.QueryRow("SELECT data->>'effective_to', status FROM "+schema+".documents WHERE id=$1", v1).Scan(&v1EffectiveTo, &v1Status)
		if v1EffectiveTo != "2026-06-30" {
			t.Fatalf("expected v1's effective_to to be superseded to 2026-06-30, got %q", v1EffectiveTo)
		}
		if v1Status != "Superseded" {
			t.Fatalf("expected v1's status to be Superseded, got %q", v1Status)
		}

		priceInMarch, found, err := ResolvePriceForSKU(tenantID, plCode, "TEST376D-SKU", "2026-03-15")
		if err != nil {
			t.Fatalf("ResolvePriceForSKU (March, still v1's window): %v", err)
		}
		if !found || priceInMarch != 100 {
			t.Fatalf("expected March to still resolve to v1's price 100, got price=%v found=%v", priceInMarch, found)
		}
		priceInAugust, found, err := ResolvePriceForSKU(tenantID, plCode, "TEST376D-SKU", "2026-08-01")
		if err != nil {
			t.Fatalf("ResolvePriceForSKU (August, v2's window): %v", err)
		}
		if !found || priceInAugust != 150 {
			t.Fatalf("expected August to resolve to v2's price 150, got price=%v found=%v", priceInAugust, found)
		}

		_, found, err = ResolvePriceForSKU(tenantID, "TEST376D-NO-SUCH-LIST", "TEST376D-SKU", "2026-08-01")
		if err != nil {
			t.Fatalf("ResolvePriceForSKU (no such list): %v", err)
		}
		if found {
			t.Fatalf("expected an unregistered price list to resolve found=false")
		}
	})
}
