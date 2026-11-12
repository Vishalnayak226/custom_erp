package engines

import (
	"encoding/json"
	"reflect"
	"testing"
)

// Stage 49.5.7 (adversarial invariants): changing a document's currency or
// rate, or an item's unit, after lines exist or after posting must never
// silently revalue posted documents, stock or the ledger. Each case replays
// the generic save the way the route does: the stored record is the base, the
// change is laid over it, ValidateTransactionalRules judges it (prior and
// payload) and ApplyDocumentCurrency stamps what is left. The invariant is
// "refused, or nothing about the money moved".

// withStatus is a stored record as the app saves it: the status is in its data too.
func withStatus(insert stewardInsert) stewardInsert {
	return func(id, doctype, status, createdBy string, data map[string]interface{}) {
		data["status"] = status
		insert(id, doctype, status, createdBy, data)
	}
}

func savedLikeTheRoute(t *testing.T, tenantID, doctype, id string, change map[string]interface{}) (map[string]interface{}, map[string]interface{}, error) {
	t.Helper()
	prior, status, _, err := fetchDocument(tenantID, doctype, id)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(prior)
	var payload map[string]interface{}
	_ = json.Unmarshal(raw, &payload)
	for k, v := range change {
		payload[k] = v
	}
	if err := ValidateTransactionalRules(tenantID, doctype, id, status, prior, payload); err != nil {
		return prior, payload, err
	}
	if err := ApplyDocumentCurrency(tenantID, doctype, payload); err != nil {
		return prior, payload, err
	}
	return prior, payload, nil
}

func assertMoneyDidNotMove(t *testing.T, label string, prior, after map[string]interface{}) {
	t.Helper()
	for _, f := range []string{"currency", "exchange_rate", "total_amount", "invoice_amount", "base_total_amount", "base_invoice_amount", "lines"} {
		if _, had := prior[f]; !had {
			continue
		}
		if !reflect.DeepEqual(comparablePostedValue(prior[f]), comparablePostedValue(after[f])) {
			t.Errorf("%s: %s moved from %v to %v and the save was accepted", label, f, prior[f], after[f])
		}
	}
}

var currencyChanges = map[string]map[string]interface{}{
	"currency to INR":      {"currency": "INR"},
	"currency blanked":     {"currency": ""},
	"currency to EUR":      {"currency": "EUR", "exchange_rate": 90.0},
	"rate to 1":            {"exchange_rate": 1.0},
	"rate to 90":           {"exchange_rate": 90.0},
	"rate as text":         {"exchange_rate": "90"},
	"rate zero":            {"exchange_rate": 0.0},
	"rate removed":         {"exchange_rate": nil},
	"date moved":           {"invoice_date": "2020-01-01", "transaction_date": "2020-01-01"},
	"amount lowered":       {"total_amount": 1.0, "invoice_amount": 1.0},
	"lines rewritten":      {"lines": "[{\"account_code\":\"5100\",\"debit\":1}]"},
	"base amount tampered": {"base_total_amount": 1.0, "base_invoice_amount": 1.0},
}

func TestStage4957PostedDocumentsKeepTheirCurrencyAndRate(t *testing.T) {
	tenantID, _, rawInsert := stage140Tenant(t)
	insert := withStatus(rawInsert)
	usd := func(extra map[string]interface{}) map[string]interface{} {
		d := map[string]interface{}{"currency": "USD", "exchange_rate": 80.0, "total_amount": 100.0, "base_total_amount": 8000.0,
			"invoice_amount": 100.0, "base_invoice_amount": 8000.0, "invoice_date": "2026-11-01", "transaction_date": "2026-11-01", "functional_currency": "INR"}
		for k, v := range extra {
			d[k] = v
		}
		return d
	}
	insert("SI-ADV-APPROVED", "SalesInvoice", "Approved", "maker1", usd(nil))
	insert("SI-ADV-PAID", "SalesInvoice", "Paid", "maker1", usd(nil))
	insert("VI-ADV-PAID", "VendorInvoice", "Paid", "maker1", usd(nil))
	insert("JV-ADV-POSTED", "JournalVoucher", "Posted", "maker1", usd(map[string]interface{}{"lines": "[]"}))
	cases := []struct{ doctype, id string }{
		{"SalesInvoice", "SI-ADV-APPROVED"}, {"SalesInvoice", "SI-ADV-PAID"},
		{"VendorInvoice", "VI-ADV-PAID"}, {"JournalVoucher", "JV-ADV-POSTED"},
	}
	for _, c := range cases {
		for name, change := range currencyChanges {
			if c.doctype == "JournalVoucher" && (name == "amount lowered" || name == "base amount tampered") {
				continue // a voucher's money is its lines
			}
			prior, after, err := savedLikeTheRoute(t, tenantID, c.doctype, c.id, change)
			if err != nil {
				continue // refused: the invariant holds
			}
			assertMoneyDidNotMove(t, c.doctype+" "+c.id+" "+name, prior, after)
		}
	}
}

func TestStage4957PurchaseOrderWithReceiptsKeepsItsCurrencyAndRate(t *testing.T) {
	tenantID, _, rawInsert := stage140Tenant(t)
	insert := withStatus(rawInsert)
	line := `[{"sku":"S1","qty":10,"unit_price":10}]`
	po := map[string]interface{}{"vendor": "V1", "currency": "USD", "exchange_rate": 80.0, "total_amount": 100.0, "base_total_amount": 8000.0,
		"items": line, "functional_currency": "INR"}
	insert("PO-ADV-1", "PurchaseOrder", "Approved", "maker1", po)
	insert("PO-ADV-NOGRN", "PurchaseOrder", "Approved", "maker1", po)
	insert("GRN-ADV-1", "GRN", "Approved", "maker1", map[string]interface{}{"po_id": "PO-ADV-1", "vendor": "V1",
		"received_items": `[{"po_id":"PO-ADV-1","sku":"S1","qty":4,"accepted_qty":4}]`})
	for name, change := range currencyChanges {
		if name == "amount lowered" || name == "base amount tampered" {
			continue // the PO's own amendment rules judge these
		}
		prior, after, err := savedLikeTheRoute(t, tenantID, "PurchaseOrder", "PO-ADV-1", change)
		if err == nil {
			assertMoneyDidNotMove(t, "PO with a receipt, "+name, prior, after)
		}
	}
	// With nothing received yet, a rate correction is still the buyer's to make.
	if _, _, err := savedLikeTheRoute(t, tenantID, "PurchaseOrder", "PO-ADV-NOGRN", map[string]interface{}{"exchange_rate": 82.0}); err != nil {
		t.Errorf("a rate correction before any receipt was refused: %v", err)
	}
}

func TestStage4957StockUnitCannotChangeAfterMovementWhateverTheSetting(t *testing.T) {
	packOn(t, false)
	m := newMC1481(t)
	moved := m.item(map[string]interface{}{"uom": "pc"})
	m.receive(moved, 4, 10)
	if c := t14835Code(PrepareItemPackUnits(m.tenant, moved, map[string]interface{}{"uom": "dozen"})); c != "INVENT-1401" {
		t.Errorf("moved item, unit changed with pack units off: %q, want INVENT-1401", c)
	}
	still := m.item(map[string]interface{}{"uom": "pc"})
	if err := PrepareItemPackUnits(m.tenant, still, map[string]interface{}{"uom": "dozen"}); err != nil {
		t.Errorf("no movement yet, unit change: %v", err)
	}
	if err := PrepareItemPackUnits(m.tenant, moved, map[string]interface{}{"uom": "PC", "name": "renamed"}); err != nil {
		t.Errorf("same unit, other edit: %v", err)
	}
}
