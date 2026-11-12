package engines

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"custom_erp/db"
)

// Stage 47.16.4h.1 (user 2026-10-30, recommended answer): the e-invoice
// turnover warning counts the till's sales too, not only posted sales
// invoices - same April-March financial year, same GSTIN, before tax. A paid
// or settled bill counts on the day it is dated; an open cart, a deleted
// bill and a bill in the next year do not; a bill with no GST breakdown
// counts at its net amount.
func TestStage47164h1EInvoiceTurnoverCountsTillSales(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	exec := func(q string, args ...interface{}) {
		t.Helper()
		if _, err := db.DB.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	doc := func(id, doctype, status, createdAt string, data map[string]interface{}) {
		t.Helper()
		raw, _ := json.Marshal(data)
		exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by, created_at) VALUES ($1, $2, $3, $4, 'system', $5::timestamp)",
			id, doctype, raw, status, createdAt)
	}
	turnover := func() SetupCheck {
		t.Helper()
		steps, err := GetSetupPath(tenantID)
		if err != nil {
			t.Fatalf("GetSetupPath: %v", err)
		}
		for _, s := range steps {
			for _, c := range s.Checks {
				if c.ID == "tax-einvoice-turnover" {
					return c
				}
			}
		}
		t.Fatal("no e-invoice turnover check")
		return SetupCheck{}
	}

	const crore = 1_00_00_000
	gstin := gstinWithCheckDigit(t, "29PQRSX1234F1Z")
	now := "2026-01-15 12:00:00"
	exec("DELETE FROM " + schema + ".documents WHERE doctype IN ('LegalEntity', 'Location', 'SalesInvoice', 'POSCart')")
	doc("LE-T", "LegalEntity", "Active", now, map[string]interface{}{"code": "LE-T", "name": "Till Co", "gstin": gstin, "state_code": "29"})
	doc("ST-T", "Location", "Active", now, map[string]interface{}{"code": "ST-T", "name": "Store T", "legal_entity": "LE-T"})

	// FY 2025-26: ₹3 crore of invoices and ₹2 crore of till sales before
	// tax (₹2.36 crore with ₹0.36 crore GST) make exactly ₹5 crore - not
	// over. An open cart, a deleted bill and an April 2026 bill count for
	// nothing.
	doc("SI-T1", "SalesInvoice", "Approved", now, map[string]interface{}{"location": "ST-T", "invoice_date": "2025-06-10", "total_amount": 3 * crore})
	doc("CART-T1", "POSCart", "Paid", "2025-09-20 12:00:00", map[string]interface{}{"location": "ST-T",
		"gst_breakdown": map[string]interface{}{"total_amount": 2.36 * crore, "total_tax": 0.36 * crore}, "amount_paid": 2.36 * crore})
	doc("CART-T2", "POSCart", "Open", "2025-10-01 12:00:00", map[string]interface{}{"location": "ST-T", "amount_paid": 4 * crore})
	doc("CART-T3", "POSCart", "Paid", "2025-10-02 12:00:00", map[string]interface{}{"location": "ST-T", "amount_paid": 4 * crore})
	exec("UPDATE "+schema+".documents SET deleted_at = NOW() WHERE id = $1", "CART-T3")
	doc("CART-T4", "POSCart", "Paid", "2026-04-02 12:00:00", map[string]interface{}{"location": "ST-T", "amount_paid": 4 * crore})
	if c := turnover(); !c.Passed {
		t.Fatalf("₹5 crore before tax is not over the limit: %+v", c)
	}

	// A settled bill on 31 March 2026 with no GST breakdown counts at its net
	// ₹0.2 crore: FY 2025-26 reaches ₹5.2 crore and the warning names it.
	doc("CART-T5", "POSCart", "Settled", "2026-03-31 12:00:00", map[string]interface{}{"location": "ST-T", "amount_paid": 0.2 * crore})
	c := turnover()
	if c.Passed || c.Severity != SetupSeverityWarning || !strings.Contains(c.Detail, gstin) || !strings.Contains(c.Detail, "₹5.20 crore in 2025-26") {
		t.Fatalf("invoices plus till sales over ₹5 crore: a warning naming the GSTIN, got %+v", c)
	}

	// Till sales alone are enough: with the invoice gone, ₹6 crore of bills
	// in FY 2026-27 (₹4 crore above plus ₹2 crore more) still warn.
	exec("DELETE FROM "+schema+".documents WHERE id = $1", "SI-T1")
	doc("CART-T6", "POSCart", "Paid", "2026-05-02 12:00:00", map[string]interface{}{"location": "ST-T", "amount_paid": 2 * crore})
	if c := turnover(); c.Passed || !strings.Contains(c.Detail, "₹6.00 crore in 2026-27") {
		t.Fatalf("till sales alone over ₹5 crore: a warning for the latest year, got %+v", c)
	}
}

// Stage 47.16.4h.2 (47.12.4's screen part): the guided setup on the
// Configuration screen shows each step's failing readiness checks - a
// warning never holds the step. The keys the screen reads are on the wire,
// and view-admin.js renders them under each step and counts them on the
// folded summary.
func TestStage47164h2SetupScreenShowsReadinessChecks(t *testing.T) {
	step := SetupPathStep{ID: "tax", Count: 1, State: SetupStateReady, Severity: SetupSeverityWarning,
		Checks: []SetupCheck{setupCheck("tax-einvoice-turnover", "E-invoicing is on", SetupSeverityWarning, []string{"29X: ₹5.20 crore in 2025-26"})}}
	raw, err := json.Marshal(step)
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		State  string                   `json:"state"`
		Checks []map[string]interface{} `json:"checks"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil || wire.State != SetupStateReady || len(wire.Checks) != 1 {
		t.Fatalf("state and checks on the wire: %s (%v)", raw, err)
	}
	for _, key := range []string{"id", "label", "severity", "passed", "detail"} {
		if _, ok := wire.Checks[0][key]; !ok {
			t.Errorf("check.%s is not on the wire: %s", key, raw)
		}
	}

	src, err := os.ReadFile(filepath.Join("..", "public", "view-admin.js"))
	if err != nil {
		t.Fatal(err)
	}
	js := string(src)
	start := strings.Index(js, "async function renderSetupPathPanel(")
	end := strings.Index(js, "async function mountSetupGuideCard(")
	if start < 0 || end < start {
		t.Fatal("renderSetupPathPanel / mountSetupGuideCard not found in view-admin.js")
	}
	panel := js[start:end]
	for _, want := range []string{"${setupCheckLines(s)}", "setupFailingChecks(s).length", "to check</span>",
		"c.passed", "c.severity !== 'Blocking'", "c.label", "c.detail", "s.state", "'Awaiting Approval'"} {
		if !strings.Contains(panel, want) {
			t.Errorf("the guided setup panel does not render %q", want)
		}
	}
}
