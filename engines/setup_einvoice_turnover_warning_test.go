package engines

import (
	"encoding/json"
	"strings"
	"testing"

	"custom_erp/db"
)

// Stage 47.16.4d (gap G4): e-invoicing stays the administrator's switch on a
// GSTIN's IRP profile, but the tax step warns when that GSTIN's posted sales
// invoices, before tax, pass ₹5 crore in a financial year (April-March) while
// the profile is off or missing. Above and off warns; on does not; below does
// not.
func TestStage47164dEInvoiceTurnoverWarning(t *testing.T) {
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
	doc := func(id, doctype, status string, data map[string]interface{}) {
		t.Helper()
		raw, _ := json.Marshal(data)
		exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system')", id, doctype, raw, status)
	}
	turnover := func() SetupCheck {
		t.Helper()
		steps, err := GetSetupPath(tenantID)
		if err != nil {
			t.Fatalf("GetSetupPath: %v", err)
		}
		for _, s := range steps {
			if s.ID != "tax" {
				continue
			}
			for _, c := range s.Checks {
				if c.ID == "tax-einvoice-turnover" {
					if c.Severity != SetupSeverityWarning || c.Label == "" {
						t.Fatalf("the turnover check is a labelled warning, never blocking: %+v", c)
					}
					return c
				}
			}
			t.Fatalf("the tax step has no e-invoice turnover check: %+v", s.Checks)
		}
		t.Fatal("no tax step")
		return SetupCheck{}
	}

	const crore = 1_00_00_000
	gstin := gstinWithCheckDigit(t, "29ABCDE1234F1Z")
	exec("DELETE FROM " + schema + ".documents WHERE doctype IN ('LegalEntity', 'Location', 'SalesInvoice')")
	doc("LE-E", "LegalEntity", "Active", map[string]interface{}{"code": "LE-E", "name": "Acme Pvt Ltd", "gstin": gstin, "state_code": "29"})
	doc("ST-E", "Location", "Active", map[string]interface{}{"code": "ST-E", "name": "Store E", "legal_entity": "LE-E"})
	doc("ST-F", "Location", "Active", map[string]interface{}{"code": "ST-F", "name": "Store F", "legal_entity": "LE-E"})

	// Below: FY 2025-26 holds ₹5.5 crore with ₹0.9 crore of it tax (₹4.6
	// crore before tax); April 2026 is the next year; a draft and a deleted
	// invoice count for nothing.
	doc("SI-E1", "SalesInvoice", "Approved", map[string]interface{}{"location": "ST-E", "invoice_date": "2025-05-10", "total_amount": 5.5 * crore, "total_tax": 0.9 * crore})
	doc("SI-E2", "SalesInvoice", "Paid", map[string]interface{}{"location": "ST-E", "invoice_date": "2026-04-02", "total_amount": 3 * crore, "total_tax": 0})
	doc("SI-F1", "SalesInvoice", "Draft", map[string]interface{}{"location": "ST-F", "invoice_date": "2025-06-01", "total_amount": 9 * crore})
	doc("SI-F2", "SalesInvoice", "Approved", map[string]interface{}{"location": "ST-F", "invoice_date": "2025-07-01", "total_amount": 9 * crore})
	exec("UPDATE "+schema+".documents SET deleted_at = NOW() WHERE id = $1", "SI-F2")
	if c := turnover(); !c.Passed {
		t.Fatalf("₹4.6 crore before tax is below the limit: %+v", c)
	}

	// Above and off: ₹0.5 crore more on 31 March 2026 from the GSTIN's other
	// store takes FY 2025-26 to ₹5.1 crore, with no IRP profile at all.
	doc("SI-F3", "SalesInvoice", "Approved", map[string]interface{}{"location": "ST-F", "invoice_date": "2026-03-31", "total_amount": 0.5 * crore})
	c := turnover()
	if c.Passed || !strings.Contains(c.Detail, gstin) || !strings.Contains(c.Detail, "₹5.10 crore in 2025-26") {
		t.Fatalf("₹5.1 crore across two stores of one GSTIN, no profile: a warning naming it, got %+v", c)
	}

	// A profile that is off still warns.
	exec("INSERT INTO "+schema+".irp_profiles (environment, gstin, enabled) VALUES ($1, $2, FALSE)", IRPEnvironment(), gstin)
	if c := turnover(); c.Passed || !strings.Contains(c.Detail, gstin) {
		t.Fatalf("profile off: still a warning, got %+v", c)
	}

	// On: no warning.
	exec("UPDATE "+schema+".irp_profiles SET enabled = TRUE WHERE gstin = $1", gstin)
	if c := turnover(); !c.Passed {
		t.Fatalf("profile on: no warning, got %+v", c)
	}

	// The warning never holds the step: a tax step that was Ready stays so.
	exec("UPDATE "+schema+".irp_profiles SET enabled = FALSE WHERE gstin = $1", gstin)
	doc("HSN-E", "HSNCode", "Active", map[string]interface{}{"code": "HSN-E", "hsn": "6109"})
	steps, err := GetSetupPath(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range steps {
		if s.ID == "tax" && (s.State != SetupStateReady || s.Severity != SetupSeverityWarning) {
			t.Fatalf("over the limit with e-invoicing off: tax step Ready with a warning, got %s / %s", s.State, s.Severity)
		}
	}
}
