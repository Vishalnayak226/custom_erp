package engines

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// Stage 148.29 - quote expiry. Setting crm.quote_expiry (off): past
// valid_until the quote expires and cannot convert; the owner is reminded 3
// days before, once per quote. Off: exactly today.

func quoteExpiryDeal(t *testing.T, e *crmTestEnv, validUntil string) (opp, quoteID, won string) {
	t.Helper()
	e.exec(`INSERT INTO {s}.documents (id, doctype, data, status, created_by) VALUES ('SKU-QE', 'Item', '{"code":"SKU-QE","name":"Uniform shirt"}', 'Active', 'test') ON CONFLICT DO NOTHING`)
	l, err := SaveCRMLead(e.tenantID, "", CRMLead{Name: "Ramesh", Company: "Acme School", Phone: fmt.Sprintf("97%08d", time.Now().UnixNano()%100000000), Source: "Exhibition"}, "test")
	if err != nil {
		t.Fatal(err)
	}
	conv, err := ConvertCRMLead(e.tenantID, l.ID, "Uniforms", 0, "", "test")
	if err != nil {
		t.Fatal(err)
	}
	opp = conv.OpportunityID
	stages, _ := ListCRMPipelineStages(e.tenantID)
	for _, s := range stages {
		if s.Kind == "won" {
			won = s.ID
		}
	}
	if _, err := SaveCRMQuotation(e.tenantID, opp, []CRMQuotationLine{{SKU: "SKU-QE", Qty: 10, Rate: 350}}, validUntil, "", "test"); err != nil {
		t.Fatal(err)
	}
	if err := e.approvalRule("SalesQuotation"); err != nil {
		t.Fatal(err)
	}
	if _, err := SubmitCRMQuotation(e.tenantID, opp, "seller", "Store Manager"); err != nil {
		t.Fatal(err)
	}
	o, _ := GetCRMOpportunity(e.tenantID, opp)
	quoteID = o.QuotationID
	if err := DecideApproval(e.tenantID, "SalesQuotation", quoteID, "boss", "Admin", "", "Approved", "ok"); err != nil {
		t.Fatal(err)
	}
	e.exec(`UPDATE {s}.crm_opportunities SET owner = 'seller1' WHERE id = $1`, opp)
	return opp, quoteID, won
}

func TestStage14829QuoteExpiryOffIsToday(t *testing.T) {
	e := newCRMTestEnv(t)
	past := time.Now().AddDate(0, 0, -5).Format("2006-01-02")
	opp, quoteID, won := quoteExpiryDeal(t, e, past)
	if n, r, err := RunQuoteExpirySweep(e.tenantID, e.schema); err != nil || n != 0 || r != 0 {
		t.Fatalf("setting off: sweep must do nothing, got %d %d %v", n, r, err)
	}
	if st := e.str(`SELECT status FROM {s}.documents WHERE id = $1`, quoteID); st != "Approved" {
		t.Fatalf("setting off: the quote stays %s", st)
	}
	o, err := MoveCRMOpportunity(e.tenantID, opp, won, "", "test")
	if err != nil || o.Status != "Won" || o.SalesOrderID == "" {
		t.Fatalf("setting off: a past-dated approved quote still converts: %+v %v", o, err)
	}
}

func TestStage14829ExpiredQuoteCannotConvert(t *testing.T) {
	e := newCRMTestEnv(t)
	past := time.Now().AddDate(0, 0, -5).Format("2006-01-02")
	opp, quoteID, won := quoteExpiryDeal(t, e, past)
	e.set("crm.quote_expiry", "true")
	_, err := MoveCRMOpportunity(e.tenantID, opp, won, "", "test")
	if err == nil || !strings.Contains(err.Error(), "CRMPLT-1130") && codeOf(err) != "CRMPLT-1130" {
		t.Fatalf("an expired quote must not convert (CRMPLT-1130), got %v", err)
	}
	if n := e.count(`SELECT COUNT(*) FROM {s}.documents WHERE doctype = 'SalesOrder'`); n != 0 {
		t.Fatalf("%d orders raised from an expired quote", n)
	}
	// the sweep marks it Expired, and revising it returns the same quote to Draft
	n, _, err := RunQuoteExpirySweep(e.tenantID, e.schema)
	if err != nil || n != 1 {
		t.Fatalf("sweep expired %d (%v)", n, err)
	}
	if st := e.str(`SELECT status FROM {s}.documents WHERE id = $1`, quoteID); st != "Expired" {
		t.Fatalf("quote status %q", st)
	}
	future := time.Now().AddDate(0, 0, 30).Format("2006-01-02")
	if _, err := SaveCRMQuotation(e.tenantID, opp, []CRMQuotationLine{{SKU: "SKU-QE", Qty: 10, Rate: 340}}, future, "", "test"); err != nil {
		t.Fatal(err)
	}
	o, _ := GetCRMOpportunity(e.tenantID, opp)
	if o.QuotationID != quoteID || o.QuotationStatus != "Draft" {
		t.Fatalf("revising an expired quote reuses it as Draft: %+v", o)
	}
}

func TestStage14829ReminderThreeDaysBeforeOncePerQuote(t *testing.T) {
	e := newCRMTestEnv(t)
	e.set("crm.quote_expiry", "true")
	soon := time.Now().AddDate(0, 0, 3).Format("2006-01-02")
	_, quoteID, _ := quoteExpiryDeal(t, e, soon)
	if _, r, err := RunQuoteExpirySweep(e.tenantID, e.schema); err != nil || r != 1 {
		t.Fatalf("first sweep reminded %d (%v)", r, err)
	}
	if _, r, _ := RunQuoteExpirySweep(e.tenantID, e.schema); r != 0 {
		t.Fatalf("second sweep reminded %d - once per quote", r)
	}
	if n := e.count(`SELECT COUNT(*) FROM {s}.crm_tasks WHERE ref_doctype = 'SalesQuotation' AND ref_id = $1 AND owner = 'seller1'`, quoteID); n != 1 {
		t.Fatalf("%d reminder tasks for the owner", n)
	}
	if st := e.str(`SELECT status FROM {s}.documents WHERE id = $1`, quoteID); st != "Approved" {
		t.Fatalf("a quote 3 days from expiry is still %s", st)
	}
}

func TestStage14829FarQuoteGetsNoReminder(t *testing.T) {
	e := newCRMTestEnv(t)
	e.set("crm.quote_expiry", "true")
	far := time.Now().AddDate(0, 0, 20).Format("2006-01-02")
	_, quoteID, _ := quoteExpiryDeal(t, e, far)
	n, r, _ := RunQuoteExpirySweep(e.tenantID, e.schema)
	if n != 0 || r != 0 || e.count(`SELECT COUNT(*) FROM {s}.crm_tasks WHERE ref_id = $1`, quoteID) != 0 {
		t.Fatalf("a quote 20 days out gets nothing: %d %d", n, r)
	}
}
