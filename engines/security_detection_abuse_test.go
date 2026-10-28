package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func stage4954Rule(t *testing.T, key string) SecurityDetectionRule {
	t.Helper()
	for _, r := range SecurityDetectionRules() {
		if r.Key == key {
			return r
		}
	}
	t.Fatalf("no detection rule %q", key)
	return SecurityDetectionRule{}
}

// stage4954Fixture makes user an Active user that documents can name, and
// returns a document writer plus a cleanup that removes everything it wrote.
func stage4954Fixture(t *testing.T, user string, extraDoctypes ...string) (func(id, doctype string, data map[string]interface{}), func()) {
	t.Helper()
	clearRecords := func() {
		db.DB.Exec(`DELETE FROM tenant_default.documents WHERE created_by = $1`, user)
		for _, dt := range extraDoctypes {
			db.DB.Exec(`DELETE FROM tenant_default.approval_rules WHERE doctype = $1`, dt)
			db.DB.Exec(`DELETE FROM tenant_default.doctype_meta WHERE name = $1`, dt)
		}
		db.DB.Exec(`DELETE FROM tenant_default.users WHERE id = $1`, user)
	}
	// Before: records only - the drill has already written its audit rows
	// under this name. After: the audit rows too.
	clearRecords()
	cleanup := func() {
		clearRecords()
		db.DB.Exec(`DELETE FROM tenant_default.audit_logs WHERE user_id = $1`, user)
	}
	if _, err := db.DB.Exec(`INSERT INTO tenant_default.users (id, username, password_hash, email, role, status, location_code)
		VALUES ($1, $1, 'x', $2, 'Cashier', 'Active', 'HO')`, user, user+"@drill.invalid"); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	for _, dt := range extraDoctypes {
		if _, err := db.DB.Exec(`INSERT INTO tenant_default.doctype_meta (name, module, document_type) VALUES ($1, 'Drill', 'Transaction')`, dt); err != nil {
			t.Fatalf("seed doctype %s: %v", dt, err)
		}
	}
	doc := func(id, doctype string, data map[string]interface{}) {
		b, _ := json.Marshal(data)
		if _, err := db.DB.Exec(`INSERT INTO tenant_default.documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, 'Active', $4)`,
			id, doctype, b, user); err != nil {
			t.Fatalf("seed %s %s: %v", doctype, id, err)
		}
	}
	return doc, cleanup
}

// seedAbuseDrill writes, for the 49.11.8 drill, the business records behind
// every 49.5.4 rule that reads documents rather than counting audit rows.
// actor becomes a real user (documents must name one); every subject the
// rules report carries actor, so the drill's dedup pass checks them too.
func seedAbuseDrill(t *testing.T, s, actor string) func() {
	t.Helper()
	const tenant = "default"
	vendor := "V-" + actor
	store := "S-" + actor
	splitDoctype := "Drill49118_" + s
	doc, cleanup := stage4954Fixture(t, actor, splitDoctype)

	for i := 0; i < stage4954Rule(t, "refund_burst").Threshold; i++ {
		doc(fmt.Sprintf("RR-%s-%d", actor, i), "ReturnRequest", map[string]interface{}{"requested_by": actor})
	}
	over := GetSettingFloat(tenant, "pos.manual_discount.reason_above_pct") + 5
	for i := 0; i < stage4954Rule(t, "manual_discount_burst").Threshold; i++ {
		doc(fmt.Sprintf("CART-%s-%d", actor, i), "POSCart", map[string]interface{}{"manual_discount_pct": over})
	}
	// 49.5.6a: an offline claim is a held sale (pos_offline_sales), not a POSCart.
	for i := 0; i < stage4954Rule(t, "offline_sale_burst").Threshold; i++ {
		if _, err := db.DB.Exec(`INSERT INTO tenant_default.pos_offline_sales (id, cart_number, idempotency_key, payload, cashier_user_id, cashier)
			VALUES ($1, $1, $1, '{}', $2, $2)`, fmt.Sprintf("OFF-%s-%d", actor, i), actor); err != nil {
			t.Fatalf("seed held offline sale: %v", err)
		}
	}
	clearDocs := cleanup
	cleanup = func() {
		db.DB.Exec(`DELETE FROM tenant_default.pos_offline_sales WHERE cashier_user_id = $1`, actor)
		clearDocs()
	}
	for i := 0; i < stage4954Rule(t, "negative_stock_burst").Threshold; i++ {
		doc(fmt.Sprintf("VAR-%s-%d", actor, i), "POSOfflineSyncVariance", map[string]interface{}{"location": store, "sku": "X"})
	}
	// vendor_bank_then_paid: bank details changed, then the vendor's bill paid.
	doc("VI-"+actor, "VendorInvoice", map[string]interface{}{"vendor_id": vendor, "invoice_number": "PAID-" + s})
	LogAuditEvent(tenant, actor, "EDIT_Vendor", "SUCCESS", "Edited Vendor "+vendor+": bank_account_number, bank_ifsc - reason: drill49118")
	time.Sleep(5 * time.Millisecond)
	LogAuditEvent(tenant, actor, "PAY_VENDOR_INVOICE", "SUCCESS", "Settled vendor invoice VI-"+actor+" amount=100")
	// duplicate_vendor_invoice: one number keyed two ways.
	doc("VIA-"+actor, "VendorInvoice", map[string]interface{}{"vendor_id": vendor, "invoice_number": "DUP-" + s + "/01"})
	doc("VIB-"+actor, "VendorInvoice", map[string]interface{}{"vendor_id": vendor, "invoice_number": "dup " + s + " 01"})
	// threshold_split: two pieces each under a 1000 tier, 1200 together.
	if _, err := db.DB.Exec(`INSERT INTO tenant_default.approval_rules (doctype, min_amount, max_amount, required_role) VALUES ($1, 1000, NULL, 'HR/Admin')`, splitDoctype); err != nil {
		t.Fatalf("seed approval tier: %v", err)
	}
	doc("SPA-"+actor, splitDoctype, map[string]interface{}{"vendor": vendor, "total_amount": 600})
	doc("SPB-"+actor, splitDoctype, map[string]interface{}{"vendor": vendor, "total_amount": 600})
	return cleanup
}

func stage4954HasSubject(hits []detectionHit, part string) bool {
	for _, h := range hits {
		if strings.Contains(h.Subject, part) {
			return true
		}
	}
	return false
}

// Stage 49.5.4: the business rules fire on the pattern and not on its
// look-alikes - a bank change AFTER the payment, a non-bank vendor edit,
// different invoice numbers, a split whose pieces are too small or too big.
func TestStage4954AbuseRulesIgnoreLookAlikes(t *testing.T) {
	db.InitDB(testConnStr())
	const tenant = "default"
	s := stage4911Suffix()
	user := "abuse4954-" + s
	splitDoctype := "Drill4954_" + s
	doc, cleanup := stage4954Fixture(t, user, splitDoctype)
	defer cleanup()
	now := func() time.Time { return time.Now().Add(time.Second) }

	// Paid first, bank changed after: not the pattern.
	early := "VE-" + user
	doc("VIE-"+user, "VendorInvoice", map[string]interface{}{"vendor_id": early, "invoice_number": "E-" + s})
	LogAuditEvent(tenant, user, "PAY_VENDOR_INVOICE", "SUCCESS", "Paid 10.00 against vendor invoice VIE-"+user+" (reference R1); outstanding now 0.00")
	time.Sleep(5 * time.Millisecond)
	LogAuditEvent(tenant, user, "EDIT_Vendor", "SUCCESS", "Edited Vendor "+early+": bank_account_number")
	// A non-bank field changed, then paid: not the pattern.
	phone := "VP-" + user
	doc("VIP-"+user, "VendorInvoice", map[string]interface{}{"vendor_id": phone, "invoice_number": "P-" + s})
	LogAuditEvent(tenant, user, "EDIT_Vendor", "SUCCESS", "Edited Vendor "+phone+": phone, email")
	time.Sleep(5 * time.Millisecond)
	LogAuditEvent(tenant, user, "PAY_VENDOR_INVOICE", "SUCCESS", "Settled vendor invoice VIP-"+user+" amount=100")
	// Bank changed, then a GRN of the same vendor paid: the pattern.
	grnVendor := "VG-" + user
	doc("GRN-"+user, "GRN", map[string]interface{}{"vendor": grnVendor, "vendor_invoice_no": "G-" + s})
	LogAuditEvent(tenant, user, "EDIT_Vendor", "SUCCESS", "Edited Vendor "+grnVendor+": bank_ifsc")
	time.Sleep(5 * time.Millisecond)
	LogAuditEvent(tenant, user, "GRN_VENDOR_PAYMENT", "SUCCESS", "GRN GRN-"+user+": paid 100.00 by NEFT (UTR1), TDS 0.00")

	hits, err := detectVendorBankThenPaid("tenant_default", now(), stage4954Rule(t, "vendor_bank_then_paid"))
	if err != nil {
		t.Fatal(err)
	}
	if stage4954HasSubject(hits, early) || stage4954HasSubject(hits, phone) {
		t.Fatalf("a payment before the change, or a non-bank edit, fired: %+v", hits)
	}
	if !stage4954HasSubject(hits, grnVendor) {
		t.Fatalf("a GRN payment after a bank change did not fire: %+v", hits)
	}

	// Two different numbers for one vendor; one number for two vendors.
	dv := "VD-" + user
	doc("VDA-"+user, "VendorInvoice", map[string]interface{}{"vendor_id": dv, "invoice_number": "A-" + s + "-1"})
	doc("VDB-"+user, "VendorInvoice", map[string]interface{}{"vendor_id": dv, "invoice_number": "A-" + s + "-2"})
	doc("VDC-"+user, "VendorInvoice", map[string]interface{}{"vendor_id": dv + "X", "invoice_number": "A-" + s + "-1"})
	// A bill raised from a GRN is that GRN's own, not a second copy.
	doc("VDG-"+user, "VendorInvoice", map[string]interface{}{"vendor_id": grnVendor, "invoice_number": "G-" + s, "grn_id": "GRN-" + user})
	dups, err := detectDuplicateVendorInvoice("tenant_default", now(), stage4954Rule(t, "duplicate_vendor_invoice"))
	if err != nil {
		t.Fatal(err)
	}
	if stage4954HasSubject(dups, dv) || stage4954HasSubject(dups, grnVendor) {
		t.Fatalf("different numbers, or a GRN's own bill, counted as a duplicate: %+v", dups)
	}

	// Split: too small (each under half the tier), or one piece over it.
	if _, err := db.DB.Exec(`INSERT INTO tenant_default.approval_rules (doctype, min_amount, max_amount, required_role) VALUES ($1, 1000, NULL, 'HR/Admin')`, splitDoctype); err != nil {
		t.Fatal(err)
	}
	for i, amt := range []float64{300, 300, 300} {
		doc(fmt.Sprintf("SMALL-%s-%d", user, i), splitDoctype, map[string]interface{}{"vendor": "small", "total_amount": amt})
	}
	doc("BIG1-"+user, splitDoctype, map[string]interface{}{"vendor": "big", "total_amount": 1200})
	doc("BIG2-"+user, splitDoctype, map[string]interface{}{"vendor": "big", "total_amount": 600})
	// Percent, not money: discount_amount is what the approval engine reads.
	doc("PCT1-"+user, splitDoctype, map[string]interface{}{"vendor": "pct", "discount_amount": 600, "invoice_amount": 600})
	doc("PCT2-"+user, splitDoctype, map[string]interface{}{"vendor": "pct", "discount_amount": 600, "invoice_amount": 600})
	splits, err := detectThresholdSplit("tenant_default", now(), stage4954Rule(t, "threshold_split"))
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range splits {
		if strings.Contains(h.Subject, splitDoctype) && h.Count >= 2 {
			t.Fatalf("a look-alike split fired: %+v", h)
		}
	}
}
