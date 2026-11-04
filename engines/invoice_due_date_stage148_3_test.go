package engines

import "testing"

// Stage 148.3 (left): a manual invoice that arrives with no due date gets the
// customer's terms through the save choke point; a typed date always wins; no
// terms leaves it blank.
func TestStage1483ManualInvoiceDueDateFromTerms(t *testing.T) {
	tenantID, _, insert := stage140Tenant(t)
	insert("NET30", "PaymentTerm", "Active", "system", map[string]interface{}{"code": "NET30", "name": "Net 30", "net_days": 30.0})
	insert("C1483T", "Customer", "Active", "system", map[string]interface{}{"code": "C1483T", "name": "Terms Buyer", "payment_term": "NET30"})
	insert("C1483N", "Customer", "Active", "system", map[string]interface{}{"code": "C1483N", "name": "Plain Buyer"})

	check := func(label string, data map[string]interface{}, wantDue, wantTerm string) {
		t.Helper()
		_ = ValidateDocument(tenantID, "SalesInvoice", data) // other fields may fail; the due date is filled first
		if got := strField(data, "due_date"); got != wantDue {
			t.Errorf("%s: due_date %q, want %q", label, got, wantDue)
		}
		if got := strField(data, "payment_term"); got != wantTerm {
			t.Errorf("%s: payment_term %q, want %q", label, got, wantTerm)
		}
	}
	check("by customer id", map[string]interface{}{"customer": "C1483T", "customer_id": "C1483T", "invoice_date": "2026-11-10"}, "2026-12-10", "NET30")
	check("by typed code only", map[string]interface{}{"customer": "C1483T", "invoice_date": "2026-11-10"}, "2026-12-10", "NET30")
	check("by name", map[string]interface{}{"customer": "Terms Buyer", "invoice_date": "2026-11-10"}, "2026-12-10", "NET30")
	check("typed date wins", map[string]interface{}{"customer": "C1483T", "invoice_date": "2026-11-10", "due_date": "2026-11-15"}, "2026-11-15", "")
	check("no terms stays blank", map[string]interface{}{"customer": "C1483N", "invoice_date": "2026-11-10"}, "", "")
	check("walk-in stays blank", map[string]interface{}{"customer": "Someone", "invoice_date": "2026-11-10"}, "", "")

	// A second way in: the import path validates through the same function.
	data := map[string]interface{}{"customer": "C1483T", "invoice_date": "2026-11-10"}
	fields, _ := GetDocTypeMeta(tenantID, "SalesInvoice")
	_ = validateDocumentWith(tenantID, "SalesInvoice", data, &docValidationLookups{fields: fields, fieldMaxLength: -1})
	if got := strField(data, "due_date"); got != "2026-12-10" {
		t.Errorf("import path: due_date %q, want 2026-12-10", got)
	}
}
