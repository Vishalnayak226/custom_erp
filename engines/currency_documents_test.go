package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"testing"
)

func TestApplyDocumentCurrencyStampsAndConverts(t *testing.T) {
	t.Run("a non-financial doctype is untouched", func(t *testing.T) {
		payload := map[string]interface{}{"total_amount": 100.0}
		if err := ApplyDocumentCurrency("default", "Item", payload); err != nil {
			t.Fatalf("Item: %v", err)
		}
		if _, stamped := payload["currency"]; stamped {
			t.Fatal("an Item was stamped with a currency; only financial documents participate")
		}
	})

	t.Run("a functional-currency document gets rate 1 and a base twin", func(t *testing.T) {
		payload := map[string]interface{}{"total_amount": 1234.56}
		if err := ApplyDocumentCurrency("default", "SalesInvoice", payload); err != nil {
			t.Fatalf("SalesInvoice: %v", err)
		}
		if payload["currency"] != "INR" || payload["functional_currency"] != "INR" {
			t.Fatalf("currency stamp = %v/%v, want INR/INR", payload["currency"], payload["functional_currency"])
		}
		if rate, _ := parityNumber(payload["exchange_rate"]); rate != 1 {
			t.Fatalf("exchange_rate = %v, want 1 for a document already in the functional currency", payload["exchange_rate"])
		}
		if base, _ := parityNumber(payload["base_total_amount"]); base != 1234.56 {
			t.Fatalf("base_total_amount = %v, want the amount unchanged", payload["base_total_amount"])
		}
	})

	t.Run("an explicit rate is honoured and converts the base amount", func(t *testing.T) {
		payload := map[string]interface{}{"currency": "usd", "exchange_rate": 83.5, "total_amount": 100.0}
		if err := ApplyDocumentCurrency("default", "PurchaseOrder", payload); err != nil {
			t.Fatalf("PurchaseOrder: %v", err)
		}
		if payload["currency"] != "USD" {
			t.Fatalf("currency = %v, want it normalised to USD", payload["currency"])
		}
		if base, _ := parityNumber(payload["base_total_amount"]); base != 8350 {
			t.Fatalf("base_total_amount = %v, want 8350", payload["base_total_amount"])
		}
		// The document keeps what was agreed; only the base twin is converted.
		if amount, _ := parityNumber(payload["total_amount"]); amount != 100 {
			t.Fatalf("total_amount = %v, want the transaction amount left alone", payload["total_amount"])
		}
	})

	t.Run("an invalid currency code is refused", func(t *testing.T) {
		payload := map[string]interface{}{"currency": "dollars", "exchange_rate": 80.0, "total_amount": 10.0}
		if err := ApplyDocumentCurrency("default", "SalesInvoice", payload); err == nil {
			t.Fatal("a non-ISO currency code was accepted")
		}
	})

	t.Run("approval slabs compare the base amount", func(t *testing.T) {
		payload := map[string]interface{}{"currency": "USD", "exchange_rate": 83.0, "total_amount": 100.0}
		if err := ApplyDocumentCurrency("default", "VendorInvoice", payload); err != nil {
			t.Fatalf("VendorInvoice: %v", err)
		}
		if got := DocumentBaseAmount(payload); got != 8300 {
			t.Fatalf("DocumentBaseAmount = %v, want 8300 - a slab written in INR must not be compared against 100", got)
		}
		// A document saved before the stamp existed has no base twin and was,
		// by definition, already in the functional currency.
		if got := DocumentBaseAmount(map[string]interface{}{"total_amount": 500.0}); got != 500 {
			t.Fatalf("legacy DocumentBaseAmount = %v, want the raw amount", got)
		}
	})
}

func TestConvertPostingToFunctionalStaysBalanced(t *testing.T) {
	// Chosen so naive per-line rounding pulls the two sides apart: each side
	// rounds in a different direction.
	debits := map[string]int{"1100": 333, "1200": 334}
	credits := map[string]int{"4100": 667}
	functionalDebits, functionalCredits, transactionDebits, transactionCredits := ConvertPostingToFunctional(debits, credits, 83.335)

	sum := func(m map[string]int) int {
		total := 0
		for _, value := range m {
			total += value
		}
		return total
	}
	if sum(functionalDebits) != sum(functionalCredits) {
		t.Fatalf("converted journal is unbalanced: debits %d, credits %d - PostDoubleEntry would refuse it",
			sum(functionalDebits), sum(functionalCredits))
	}
	if transactionDebits["1100"] != 333 || transactionCredits["4100"] != 667 {
		t.Fatalf("transaction amounts were altered: %v / %v", transactionDebits, transactionCredits)
	}
	// The functional total must still be the transaction total times the rate,
	// within the one-unit rounding adjustment.
	transactionTotal, rate := 667.0, 83.335
	expected := int(transactionTotal * rate)
	if difference := sum(functionalDebits) - expected; difference < -2 || difference > 2 {
		t.Fatalf("converted total = %d, want ~%d", sum(functionalDebits), expected)
	}

	// Rate 1 must be a pure pass-through, so a single-currency tenant's
	// postings are byte-identical to what they were before Stage 37.1.2.
	sameDebits, sameCredits, _, _ := ConvertPostingToFunctional(debits, credits, 1)
	if fmt.Sprint(sameDebits) != fmt.Sprint(debits) || fmt.Sprint(sameCredits) != fmt.Sprint(credits) {
		t.Fatalf("rate 1 altered the posting: %v / %v", sameDebits, sameCredits)
	}
}

func TestForeignCurrencyPostingRecordsBothAmounts(t *testing.T) {
	db.InitDB(testConnStr())
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	var hasColumns bool
	if err := db.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM information_schema.columns
		WHERE table_schema = $1 AND table_name = 'gl_postings' AND column_name = 'transaction_debit')`, schema).Scan(&hasColumns); err != nil {
		t.Fatalf("inspect gl_postings: %v", err)
	}
	if !hasColumns {
		t.Skip("db/migrations_stage37_1_2_multicurrency_documents.sql has not been applied to this database")
	}

	const docID = "TEST-FX-JV-0001"
	cleanup := func() {
		_, _ = db.DB.Exec("DELETE FROM "+schema+".gl_postings WHERE document_id = $1", docID)
	}
	cleanup()
	defer cleanup()

	debits, credits, transactionDebits, transactionCredits := ConvertPostingToFunctional(
		map[string]int{"1100": 100}, map[string]int{"4100": 100}, 83.0)
	err = PostDoubleEntry("default", "JournalVoucher", docID, PaiseMap(debits), PaiseMap(credits), "", "",
		PostingOptions{Currency: "USD", ExchangeRate: 83.0,
			TransactionDebits: transactionDebits, TransactionCredits: transactionCredits})
	if err != nil {
		t.Fatalf("post foreign-currency entry: %v", err)
	}

	rows, err := db.DB.Query("SELECT account_code, debit, credit, currency, exchange_rate, COALESCE(transaction_debit,0), COALESCE(transaction_credit,0) FROM "+
		schema+".gl_postings WHERE document_id = $1 ORDER BY account_code", docID)
	if err != nil {
		t.Fatalf("read postings: %v", err)
	}
	defer rows.Close()
	seen := 0
	for rows.Next() {
		var account, currency string
		var debit, credit int64
		var rate, transactionDebit, transactionCredit float64
		if err := rows.Scan(&account, &debit, &credit, &currency, &rate, &transactionDebit, &transactionCredit); err != nil {
			t.Fatalf("scan: %v", err)
		}
		seen++
		if currency != "USD" || rate != 83.0 {
			t.Fatalf("row %s carries currency %q rate %v, want USD/83", account, currency, rate)
		}
		// The functional amount is what every existing report sums (paise,
		// Stage 45: 100 rupees at 83.0 = 8300 rupees = 830000 paise); the
		// transaction amount is what the document said, still rupees.
		if debit+credit != 830000 {
			t.Fatalf("row %s functional amount = %d, want 830000", account, debit+credit)
		}
		if transactionDebit+transactionCredit != 100 {
			t.Fatalf("row %s transaction amount = %v, want 100", account, transactionDebit+transactionCredit)
		}
	}
	if seen != 2 {
		t.Fatalf("wrote %d postings, want 2", seen)
	}
}

func TestFunctionalCurrencyFallsBackSafely(t *testing.T) {
	// A misconfigured setting must never silently reinterpret an existing
	// ledger as some other currency.
	if got := FunctionalCurrency("no-such-tenant"); got != "INR" {
		t.Fatalf("FunctionalCurrency for an unknown tenant = %q, want the INR fallback", got)
	}
	var probe map[string]interface{}
	if err := json.Unmarshal([]byte(`{"total_amount": 10}`), &probe); err != nil {
		t.Fatalf("probe: %v", err)
	}
	// json.Number-free float from encoding/json must still be recognised.
	if err := ApplyDocumentCurrency("default", "SalesOrder", probe); err != nil {
		t.Fatalf("JSON-sourced document: %v", err)
	}
	if base, ok := parityNumber(probe["base_total_amount"]); !ok || base != 10 {
		t.Fatalf("base_total_amount = %v, want 10", probe["base_total_amount"])
	}
}
