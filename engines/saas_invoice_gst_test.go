package engines

import (
	"os"
	"testing"
	"time"

	"custom_erp/db"
)

// saasInvoiceSetup points platform billing at the test client with a supplier
// GSTIN (env) and a SAC (setting). The SAC here is a placeholder for tests, not
// a claim about the right code.
func saasInvoiceSetup(t *testing.T, tenantID, supplierGSTIN, sac string) {
	t.Helper()
	oldTenant := saasBillingTenantOverride
	saasBillingTenantOverride = tenantID
	oldEnv, hadEnv := os.LookupEnv("SAAS_SUPPLIER_GSTIN")
	os.Setenv("SAAS_SUPPLIER_GSTIN", supplierGSTIN)
	os.Setenv("SAAS_SUPPLIER_NAME", "Platform Pvt Ltd")
	os.Setenv("SAAS_SUPPLIER_ADDRESS", "1 MG Road, Bengaluru")
	if err := SetSetting(tenantID, SettingSaaSInvoiceSAC, sac, "system"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		saasBillingTenantOverride = oldTenant
		if hadEnv {
			os.Setenv("SAAS_SUPPLIER_GSTIN", oldEnv)
		} else {
			os.Unsetenv("SAAS_SUPPLIER_GSTIN")
		}
		os.Unsetenv("SAAS_SUPPLIER_NAME")
		os.Unsetenv("SAAS_SUPPLIER_ADDRESS")
		_ = SetSetting(tenantID, SettingSaaSInvoiceSAC, "", "system")
	})
}

func issueForTest(t *testing.T, tenantID, pay string, gross int64) (string, bool) {
	t.Helper()
	tx, err := db.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	no, _, err := issueSaaSInvoiceTx(tx, tenantID, pay, gross, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var status string
	_ = db.DB.QueryRow(`SELECT status FROM public.saas_invoices WHERE razorpay_payment_id = $1`, pay).Scan(&status)
	return no, status == "issued"
}

func TestBLD026InvoiceGST(t *testing.T) {
	tenantID, _, insert := stage140Tenant(t)
	saasCleanup(t, tenantID)
	saasInvoiceSetup(t, tenantID, "29AAACE7520K1ZI", "999999") // supplier in Karnataka (29)
	oldNow := saasNow
	saasNow = func() time.Time { return time.Date(2026, 11, 15, 10, 0, 0, 0, istZone) }
	t.Cleanup(func() { saasNow = oldNow })

	// No legal entity on record: place of supply is the supplier's State, so CGST + SGST.
	c := computeSaaSInvoice(tenantID, 118000)
	if c.Pending || c.PosCode != "29" || c.Taxable != 100000 || c.CGST != 9000 || c.SGST != 9000 || c.IGST != 0 {
		t.Fatalf("intra-state (no address on record): %+v", c)
	}

	// A recipient without a GSTIN but with an address in Maharashtra: the address on record decides, so IGST.
	insert("LE-A8", "LegalEntity", "Active", "system", map[string]interface{}{"code": "LE-A8", "name": "Buyer Retail", "state": "Maharashtra", "address": "5 Marine Drive", "city": "Mumbai", "pincode": "400020"})
	c = computeSaaSInvoice(tenantID, 118000)
	if c.PosCode != "27" || c.Recipient.GSTIN != "" || c.IGST != 18000 || c.CGST != 0 || c.SGST != 0 || c.Taxable != 100000 || c.Recipient.Name != "Buyer Retail" {
		t.Fatalf("inter-state, recipient without GSTIN: %+v", c)
	}

	// A registered recipient: the GSTIN's State wins over the address (same State here, so CGST + SGST).
	if _, err := db.DB.Exec(`UPDATE `+mustSchema(t, tenantID)+`.documents SET data = data || '{"gstin":"29BBBCF1234L1ZK"}'::jsonb WHERE id = 'LE-A8'`); err != nil {
		t.Fatal(err)
	}
	c = computeSaaSInvoice(tenantID, 118000)
	if c.PosCode != "29" || c.PosName != "Karnataka" || c.CGST != 9000 || c.SGST != 9000 || c.IGST != 0 {
		t.Fatalf("registered recipient in the supplier's State: %+v", c)
	}

	// The numbered invoice: at most 16 characters, a serial per financial year, one per payment.
	no, issued := issueForTest(t, tenantID, "pay_gst_1", 118000)
	if !issued || len(no) > 16 || no != "SA/26-27/"+no[len(no)-6:] {
		t.Fatalf("issued = %q %v", no, issued)
	}
	var cg, sg, ig, tx, gr int64
	var sac, pos, supGSTIN string
	_ = db.DB.QueryRow(`SELECT cgst_paise, sgst_paise, igst_paise, taxable_paise, gross_paise, sac, pos_state_name, supplier_gstin FROM public.saas_invoices WHERE invoice_no = $1`, no).Scan(&cg, &sg, &ig, &tx, &gr, &sac, &pos, &supGSTIN)
	if cg+sg+ig+tx != gr || sac != "999999" || pos != "Karnataka" || supGSTIN != "29AAACE7520K1ZI" {
		t.Fatalf("row = %d %d %d %d %d %s %s %s", cg, sg, ig, tx, gr, sac, pos, supGSTIN)
	}
	var desc string
	_ = db.DB.QueryRow(`SELECT description FROM public.saas_invoices WHERE invoice_no = $1`, no).Scan(&desc)
	if desc != "Software subscription" {
		t.Fatalf("description without a plan or period = %q", desc)
	}
	ps, pe := time.Date(2026, 11, 1, 0, 0, 0, 0, istZone), time.Date(2026, 12, 1, 0, 0, 0, 0, istZone)
	dtx, err := db.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if d := saasInvoiceDescription(dtx, tenantID, &ps, &pe); d != "Software subscription, 2026-11-01 to 2026-12-01" {
		t.Fatalf("description with a period = %q", d)
	}
	dtx.Rollback()
	if again, _ := issueForTest(t, tenantID, "pay_gst_1", 118000); again != no {
		t.Fatalf("replayed payment got %q; want %q", again, no)
	}
	no2, _ := issueForTest(t, tenantID, "pay_gst_2", 118000)
	if no2 == no || len(no2) > 16 {
		t.Fatalf("second serial %q after %q", no2, no)
	}

	// Rupee/paise rounding: the three heads and the taxable value always add up to the money received.
	for _, gross := range []int64{1, 100, 101, 12345, 99999, 118001, 5900000, 1234567} {
		c := computeSaaSInvoice(tenantID, gross)
		if c.Taxable+c.GSTPaid != gross {
			t.Fatalf("gross %d: taxable %d + tax %d != gross", gross, c.Taxable, c.GSTPaid)
		}
		if c.CGST+c.SGST != c.GSTPaid || c.CGST-c.SGST < -1 || c.CGST-c.SGST > 1 {
			t.Fatalf("gross %d: CGST %d SGST %d vs tax %d; want an even split, the odd paisa on either head", gross, c.CGST, c.SGST, c.GSTPaid)
		}
	}
	if c := computeSaaSInvoice(tenantID, 100); c.Taxable != 85 || c.GSTPaid != 15 {
		t.Fatalf("Rs 1.00 = taxable %d tax %d; want 85 / 15", c.Taxable, c.GSTPaid)
	}
	if _, err := db.DB.Exec(`UPDATE `+mustSchema(t, tenantID)+`.documents SET data = data || '{"gstin":"27BBBCF1234L1ZO"}'::jsonb WHERE id = 'LE-A8'`); err != nil {
		t.Fatal(err)
	}
	if c := computeSaaSInvoice(tenantID, 12345); c.IGST != c.GSTPaid || c.CGST != 0 || c.SGST != 0 || c.Taxable+c.IGST != 12345 {
		t.Fatalf("inter-state rounding: %+v", c)
	}

	// A blank supplier GSTIN: the payment is recorded, no tax invoice number is used, and it is numbered once set.
	os.Setenv("SAAS_SUPPLIER_GSTIN", "")
	var before int64
	_ = db.DB.QueryRow(`SELECT COALESCE(last_no,0) FROM public.saas_invoice_counters WHERE fiscal_year = '2026-27'`).Scan(&before)
	pno, issued := issueForTest(t, tenantID, "pay_gst_3", 118000)
	var after int64
	_ = db.DB.QueryRow(`SELECT COALESCE(last_no,0) FROM public.saas_invoice_counters WHERE fiscal_year = '2026-27'`).Scan(&after)
	var note string
	_ = db.DB.QueryRow(`SELECT note FROM public.saas_invoices WHERE razorpay_payment_id = 'pay_gst_3'`).Scan(&note)
	if issued || pno != "PENDING-pay_gst_3" || after != before || note != "supplier GSTIN not set" {
		t.Fatalf("blank GSTIN: no %q issued %v counter %d->%d note %q", pno, issued, before, after, note)
	}
	if n, err := IssuePendingSaaSInvoices(); err != nil || n != 0 {
		t.Fatalf("numbered %d (%v) while the GSTIN is still blank", n, err)
	}
	os.Setenv("SAAS_SUPPLIER_GSTIN", "29AAACE7520K1ZI")
	if n, err := IssuePendingSaaSInvoices(); err != nil || n < 1 {
		t.Fatalf("numbered %d (%v) after the GSTIN was set", n, err)
	}
	var st, fno string
	_ = db.DB.QueryRow(`SELECT status, invoice_no FROM public.saas_invoices WHERE razorpay_payment_id = 'pay_gst_3'`).Scan(&st, &fno)
	if st != "issued" || len(fno) > 16 || fno == pno {
		t.Fatalf("after setting: %s %s", st, fno)
	}

	// A blank SAC also holds the invoice: no code is assumed.
	if err := SetSetting(tenantID, SettingSaaSInvoiceSAC, "", "system"); err != nil {
		t.Fatal(err)
	}
	if c := computeSaaSInvoice(tenantID, 118000); !c.Pending || c.Note != "SAC not set" {
		t.Fatalf("blank SAC: %+v", c)
	}
}

func mustSchema(t *testing.T, tenantID string) string {
	t.Helper()
	s, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
