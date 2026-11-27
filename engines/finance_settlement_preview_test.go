package engines

import (
	"testing"

	"custom_erp/db"
)

// 264.4: a foreign-currency receipt or payment, settled on a date the user
// names and at the bank's rate or the table's. The preview the dialog shows
// is worked out by the settlement's own engine and posts nothing; what is
// then posted is what the preview said. Invoice balance, payment history, the
// realised FX result and the GL agree. A foreign invoice settles in full
// only (FIN-0518); part-settlement of a base-currency invoice is
// TestStage73PartialSettlement's.
func TestSettlementPreviewForeignCurrency(t *testing.T) {
	tenantID, schema, insert := stage135Tenant(t)
	// A new tenant already has its INR currency.
	insert("CUR-2644-USD", "Currency", "Active", map[string]interface{}{"code": "USD", "name": "USD"})
	insert("FXR-2644-A", "ExchangeRate", "Active", map[string]interface{}{"from_currency": "USD", "to_currency": "INR", "rate": 84.0,
		"rate_type": "Spot", "effective_from": "2026-01-01", "effective_to": "2026-06-30", "source": "RBI"})
	insert("FXR-2644-B", "ExchangeRate", "Active", map[string]interface{}{"from_currency": "USD", "to_currency": "INR", "rate": 86.0,
		"rate_type": "Spot", "effective_from": "2026-07-01", "source": "RBI"})
	periodID, err := CreateAccountingPeriod(tenantID, "T2644-FEB", "2026-02-01", "2026-02-28", "system")
	if err != nil {
		t.Fatalf("period: %v", err)
	}
	if _, err := db.DB.Exec("UPDATE "+schema+".accounting_periods SET status = 'Closed' WHERE id = $1", periodID); err != nil {
		t.Fatalf("close period: %v", err)
	}

	glRows := func(id string) (rows int, debit, credit int64) {
		t.Helper()
		if err := db.DB.QueryRow("SELECT COUNT(*), COALESCE(SUM(debit),0), COALESCE(SUM(credit),0) FROM "+schema+".gl_postings WHERE document_id = $1", id).
			Scan(&rows, &debit, &credit); err != nil {
			t.Fatalf("gl %s: %v", id, err)
		}
		return
	}
	net := func(account, id string) int64 {
		t.Helper()
		var v int64
		if err := db.DB.QueryRow("SELECT COALESCE(SUM(debit),0) - COALESCE(SUM(credit),0) FROM "+schema+".gl_postings WHERE account_code = $1 AND document_id = $2",
			account, id).Scan(&v); err != nil {
			t.Fatalf("net %s %s: %v", account, id, err)
		}
		return v
	}
	code := func(err error) string {
		if verr, ok := err.(*ValidationError); ok {
			return verr.Code
		}
		return ""
	}

	t.Run("receivable: preview, refusals, then a receipt at the bank's rate on a named date and its retry", func(t *testing.T) {
		inv := "SI-2644"
		insert(inv, "SalesInvoice", "Approved", map[string]interface{}{"code": inv, "customer": "T2644", "currency": "USD", "functional_currency": "INR",
			"exchange_rate": 83.0, "total_amount": 1000.0, "base_total_amount": 83000.0})
		// What the invoice booked, so the receivable can be seen to clear.
		if err := PostDoubleEntry(tenantID, "SalesInvoice", inv, map[string]int64{"1300": 8300000}, map[string]int64{"4100": 8300000}, "2026-01-10", "T2644:SI:BOOK"); err != nil {
			t.Fatalf("book invoice: %v", err)
		}
		before, _, _ := glRows(inv)

		p, err := PreviewInvoiceSettlement(tenantID, "SalesInvoice", inv, SettlementOptions{SettlementDate: "2026-03-15"})
		if err != nil || !p.Foreign || p.Currency != "USD" || p.FunctionalCurrency != "INR" || !near(p.Amount, 1000) ||
			!near(p.Rate, 84) || p.RateSource != "RBI" || !near(p.FunctionalAmount, 84000) || !near(p.CarryingAmount, 83000) || !near(p.RealisedGainLoss, 1000) {
			t.Fatalf("table-rate preview: %+v %v", p, err)
		}
		if p, err = PreviewInvoiceSettlement(tenantID, "SalesInvoice", inv, SettlementOptions{SettlementDate: "2026-08-10"}); err != nil || !near(p.Rate, 86) {
			t.Fatalf("a later date takes the later rate: %+v %v", p, err)
		}
		if _, err := PreviewInvoiceSettlement(tenantID, "SalesInvoice", inv, SettlementOptions{SettlementDate: "2025-12-31"}); code(err) != "FIN-0021" {
			t.Fatalf("no rate for the date: want FIN-0021, got %v", err)
		}
		if _, err := PreviewInvoiceSettlement(tenantID, "SalesInvoice", inv, SettlementOptions{SettlementDate: "2026-08-10", ExchangeRate: -86}); code(err) != "FIN-0021" {
			t.Fatalf("a rate below zero: want FIN-0021, got %v", err)
		}
		if _, err := PreviewInvoiceSettlement(tenantID, "SalesInvoice", inv, SettlementOptions{SettlementDate: "2026-02-10", ExchangeRate: 85}); code(err) != "FIN-0260" {
			t.Fatalf("preview into a closed period: want FIN-0260, got %v", err)
		}
		if _, err := SettleSalesInvoiceAmount(tenantID, inv, "system", 0, "RX0", SettlementOptions{SettlementDate: "2026-02-10", ExchangeRate: 85}); code(err) != "FIN-0260" {
			t.Fatalf("receipt into a closed period: want FIN-0260, got %v", err)
		}
		if _, err := SettleSalesInvoiceAmount(tenantID, inv, "system", 0, "RX0", SettlementOptions{SettlementDate: "2026-08-10", ExchangeRate: -1}); code(err) != "FIN-0021" {
			t.Fatalf("receipt at a rate below zero: want FIN-0021, got %v", err)
		}
		if _, err := SettleSalesInvoiceAmount(tenantID, inv, "system", 400, "RX0"); code(err) != "FIN-0518" {
			t.Fatalf("foreign part-receipt: want FIN-0518, got %v", err)
		}
		if rows, _, _ := glRows(inv); rows != before {
			t.Fatalf("previews and refusals posted %d GL rows", rows-before)
		}
		if status, _ := t5510Data(t, schema, inv); status != "Approved" {
			t.Fatalf("after previews and refusals: status %s, want Approved", status)
		}

		opts := SettlementOptions{SettlementDate: "2026-08-10", ExchangeRate: 86.5}
		p, err = PreviewInvoiceSettlement(tenantID, "SalesInvoice", inv, opts)
		if err != nil || p.RateSource != "Operator" || !near(p.FunctionalAmount, 86500) || !near(p.RealisedGainLoss, 3500) {
			t.Fatalf("bank-rate preview: %+v %v", p, err)
		}
		res, err := SettleSalesInvoiceAmount(tenantID, inv, "system", 0, "RX1", opts)
		if err != nil || res.Status != "Paid" || !near(res.Amount, 1000) || !near(res.FunctionalAmount, p.FunctionalAmount) || !near(res.OutstandingAmount, 0) {
			t.Fatalf("receipt: %+v %v", res, err)
		}
		status, data := t5510Data(t, schema, inv)
		hist, _ := readSettlementEntries(data, "settlements")
		if status != "Paid" || len(hist) != 1 || hist[0].Date != "2026-08-10" || !near(hist[0].FunctionalAmount, 86500) || !near(hist[0].Amount, 1000) {
			t.Fatalf("invoice after the receipt: %s %+v", status, hist)
		}
		if n := net("1300", inv); n != 0 {
			t.Fatalf("receivable nets to %d paise, want 0", n)
		}
		if n := net("1100", inv); n != 8650000 {
			t.Fatalf("cash %d paise, want 8650000 (1000 USD at 86.5)", n)
		}
		if n := net(AccountRealisedFXGain, inv); n != -350000 {
			t.Fatalf("realised gain %d paise, want a 350000 credit", n)
		}
		if _, d, c := glRows(inv); d != c {
			t.Fatalf("invoice journals do not balance: %d / %d", d, c)
		}

		// The response was lost and the dialog sends the same reference again.
		rows, _, _ := glRows(inv)
		again, err := SettleSalesInvoiceAmount(tenantID, inv, "system", 0, "RX1", opts)
		if err != nil || !again.Replayed || !near(again.FunctionalAmount, 86500) || again.Status != "Paid" {
			t.Fatalf("retry: %+v %v", again, err)
		}
		if after, _, _ := glRows(inv); after != rows {
			t.Fatalf("retry posted %d more GL rows", after-rows)
		}
	})

	t.Run("payable: a payment at the table rate on a later date, a part-payment and a closed period refused, and its retry", func(t *testing.T) {
		bill := "VI-2644"
		insert(bill, "VendorInvoice", "Matched", map[string]interface{}{"code": bill, "vendor_id": "T2644", "currency": "USD", "functional_currency": "INR",
			"exchange_rate": 83.0, "invoice_amount": 500.0, "base_invoice_amount": 41500.0})
		if err := PostDoubleEntry(tenantID, "VendorInvoice", bill, map[string]int64{"5100": 4150000}, map[string]int64{"2100": 4150000}, "2026-01-10", "T2644:VI:BOOK"); err != nil {
			t.Fatalf("book bill: %v", err)
		}
		before, _, _ := glRows(bill)

		p, err := PreviewInvoiceSettlement(tenantID, "VendorInvoice", bill, SettlementOptions{SettlementDate: "2026-03-15"})
		if err != nil || !p.Foreign || !near(p.Amount, 500) || !near(p.Rate, 84) || !near(p.FunctionalAmount, 42000) || !near(p.RealisedGainLoss, -500) {
			t.Fatalf("payable preview: %+v %v", p, err)
		}
		if _, err := PayVendorInvoiceAmount(tenantID, bill, "system", 200, "PX0"); code(err) != "FIN-0518" {
			t.Fatalf("foreign part-payment: want FIN-0518, got %v", err)
		}
		if _, err := PayVendorInvoiceAmount(tenantID, bill, "system", 0, "PX0", SettlementOptions{SettlementDate: "2026-02-20"}); code(err) != "FIN-0260" {
			t.Fatalf("payment into a closed period: want FIN-0260, got %v", err)
		}
		if rows, _, _ := glRows(bill); rows != before {
			t.Fatalf("refusals posted %d GL rows", rows-before)
		}

		opts := SettlementOptions{SettlementDate: "2026-07-15"}
		p, err = PreviewInvoiceSettlement(tenantID, "VendorInvoice", bill, opts)
		if err != nil || !near(p.Rate, 86) || !near(p.FunctionalAmount, 43000) || !near(p.RealisedGainLoss, -1500) {
			t.Fatalf("payable preview on the payment date: %+v %v", p, err)
		}
		res, err := PayVendorInvoiceAmount(tenantID, bill, "system", 0, "PX1", opts)
		if err != nil || res.Status != "Paid" || !near(res.Amount, 500) || !near(res.FunctionalAmount, p.FunctionalAmount) {
			t.Fatalf("payment: %+v %v", res, err)
		}
		status, data := t5510Data(t, schema, bill)
		hist, _ := readSettlementEntries(data, "payments")
		if status != "Paid" || len(hist) != 1 || hist[0].Date != "2026-07-15" || !near(hist[0].FunctionalAmount, 43000) {
			t.Fatalf("bill after the payment: %s %+v", status, hist)
		}
		if n := net("2100", bill); n != 0 {
			t.Fatalf("payable nets to %d paise, want 0", n)
		}
		if n := net("1100", bill); n != -4300000 {
			t.Fatalf("cash %d paise, want a 4300000 credit (500 USD at 86)", n)
		}
		if n := net(AccountRealisedFXLoss, bill); n != 150000 {
			t.Fatalf("realised loss %d paise, want a 150000 debit", n)
		}
		if _, d, c := glRows(bill); d != c {
			t.Fatalf("bill journals do not balance: %d / %d", d, c)
		}
		if _, err := PreviewInvoiceSettlement(tenantID, "VendorInvoice", bill, opts); err == nil {
			t.Fatalf("a paid bill still previews a payment")
		}

		rows, _, _ := glRows(bill)
		again, err := PayVendorInvoiceAmount(tenantID, bill, "system", 0, "PX1", opts)
		if err != nil || !again.Replayed || !near(again.FunctionalAmount, 43000) {
			t.Fatalf("retry: %+v %v", again, err)
		}
		if after, _, _ := glRows(bill); after != rows {
			t.Fatalf("retry posted %d more GL rows", after-rows)
		}
	})
}
