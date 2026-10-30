package engines

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"custom_erp/db"
)

// Stage 47.16.4b / 4e / 4f: the G2, G5 and G6 gaps of
// docs/security/gst-companies-act-controls.md.

// G2, step 1: the scale test posts synthetic sales, so it runs only in a
// sandbox client and touches nothing of a real one.
func TestStage47164bScaleTestOnlyInASandbox(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	if err := SeedScaleTestData(tenantID, 2, "BAR-SCALE", 50); validationCode(err) != "PLATFORM-0318" {
		t.Fatalf("seeding a real client: want PLATFORM-0318, got %v", err)
	}
	if _, err := RunScaleSimulation(tenantID, 1, 2, "BAR-SCALE", 2); validationCode(err) != "PLATFORM-0318" {
		t.Fatalf("running in a real client: want PLATFORM-0318, got %v", err)
	}
	if err := SeedScaleTestData("default", 2, "BAR-SCALE", 50); validationCode(err) != "PLATFORM-0318" {
		t.Fatalf("seeding the default client: want PLATFORM-0318, got %v", err)
	}
	var stock, postings int
	db.DB.QueryRow(`SELECT COUNT(*) FROM ` + schema + `.inventory_availability WHERE sku = 'BAR-SCALE'`).Scan(&stock)
	db.DB.QueryRow(`SELECT COUNT(*) FROM ` + schema + `.gl_postings`).Scan(&postings)
	if stock != 0 || postings != 0 {
		t.Fatalf("a refused scale test left %d stock row(s) and %d posting(s)", stock, postings)
	}
}

// G2, steps 2 and 3: a posted ledger row is never deleted, and its account,
// money and date never change - on a connection as production has it. Dates
// are written when the rows are inserted; the match mark, a renumber's
// reference columns and a client's data reset are the sanctioned writes.
func TestStage47164bLedgerRowsTheDatabaseWillNotChange(t *testing.T) {
	db.InitDB(testConnStrUnsanctioned())
	defer db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	defer db.DB.Exec(`DELETE FROM public.backup_runs WHERE tenant_schema = $1`, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	exec := func(q string, args ...interface{}) {
		t.Helper()
		if _, err := db.DB.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	var guarded bool
	db.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM pg_trigger WHERE tgrelid = to_regclass($1) AND tgname = 'gl_postings_guard')`, schema+".gl_postings").Scan(&guarded)
	if !guarded {
		t.Fatal("a new client's gl_postings has no guard - the schema sync must add it")
	}

	if err := PostDoubleEntry(tenantID, "JournalVoucher", "JV-G2", map[string]int64{"1100": 10000}, map[string]int64{"4100": 10000}, "", "JV-G2:POST"); err != nil {
		t.Fatalf("post: %v", err)
	}
	refused := func(what, q string) {
		t.Helper()
		if _, err := db.DB.Exec(q); err == nil || !strings.Contains(err.Error(), "append-only") {
			t.Errorf("%s: want refused as append-only, got %v", what, err)
		}
	}
	rows := schema + ".gl_postings WHERE document_id = 'JV-G2'"
	refused("changing an amount", `UPDATE `+schema+`.gl_postings SET debit = debit + 1 WHERE document_id = 'JV-G2'`)
	refused("re-dating a posting", `UPDATE `+schema+`.gl_postings SET created_at = created_at - INTERVAL '40 days' WHERE document_id = 'JV-G2'`)
	refused("moving it to another account", `UPDATE `+schema+`.gl_postings SET account_code = '1000' WHERE document_id = 'JV-G2'`)
	refused("deleting it", `DELETE FROM `+rows)
	exec(`UPDATE ` + schema + `.gl_postings SET matched_statement_line_id = 'BSL-G2' WHERE document_id = 'JV-G2'`) // bank reconciliation

	sanctioned := func(kind, q string) error {
		tx, err := db.DB.Begin()
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		if err := sanctionWriteTx(tx, kind); err != nil {
			t.Fatal(err)
		}
		_, err = tx.Exec(q)
		return err
	}
	if err := sanctioned(sanctionRecode, `UPDATE `+schema+`.gl_postings SET cost_center = 'CC-X' WHERE document_id = 'JV-G2'`); err != nil {
		t.Errorf("a renumber carrying a cost center: %v", err)
	}
	if err := sanctioned(sanctionRecode, `UPDATE `+schema+`.gl_postings SET credit = 0, debit = 0 WHERE document_id = 'JV-G2'`); err == nil {
		t.Error("a renumber changed money")
	}
	if err := sanctioned(sanctionPurge, `UPDATE `+schema+`.gl_postings SET created_at = NOW() WHERE document_id = 'JV-G2'`); err == nil {
		t.Error("a purge re-dated a posting")
	}
	if err := sanctioned(sanctionPurge, `DELETE FROM `+rows); err != nil {
		t.Errorf("a sanctioned purge: %v", err)
	}

	// The date a posting belongs to is written on insert.
	dateOf := func(doc string) string {
		var d string
		db.DB.QueryRow(`SELECT MIN(created_at)::text FROM `+schema+`.gl_postings WHERE document_id = $1`, doc).Scan(&d)
		return d
	}
	post := func(doc string, opt PostingOptions) error {
		return PostDoubleEntry(tenantID, "JournalVoucher", doc, map[string]int64{"1100": 500}, map[string]int64{"4100": 500}, "", doc+":POST", opt)
	}
	if err := post("JV-G2-DAY", PostingOptions{PostedAt: "2026-04-15"}); err != nil || !strings.HasPrefix(dateOf("JV-G2-DAY"), "2026-04-15 ") {
		t.Errorf("a backdated voucher: %v, rows dated %q", err, dateOf("JV-G2-DAY"))
	}
	if err := post("JV-G2-EOM", PostingOptions{PostedAt: "2026-04-30 23:59:59"}); err != nil || dateOf("JV-G2-EOM") != "2026-04-30 23:59:59" {
		t.Errorf("a month's depreciation: %v, rows dated %q", err, dateOf("JV-G2-EOM"))
	}
	release := BindPostingTime("2026-04-10T05:30:00Z")
	err := post("JV-G2-TILL", PostingOptions{})
	release()
	var atTill bool
	db.DB.QueryRow(`SELECT bool_and(created_at = '2026-04-10T05:30:00Z'::timestamptz::timestamp) FROM ` + schema + `.gl_postings WHERE document_id = 'JV-G2-TILL'`).Scan(&atTill)
	if err != nil || !atTill {
		t.Errorf("an offline sale at the till's time: %v, rows dated %q", err, dateOf("JV-G2-TILL"))
	}
	if err := post("JV-G2-NOW", PostingOptions{}); err != nil || !strings.HasPrefix(dateOf("JV-G2-NOW"), time.Now().Format("2006-01")) {
		t.Errorf("a binding must end with its release: %v, rows dated %q", err, dateOf("JV-G2-NOW"))
	}
	if err := post("JV-G2-BAD", PostingOptions{PostedAt: "yesterday"}); err == nil {
		t.Error("a posting time that is not a date was taken")
	}

	// A master renumber carries the new code onto the ledger's references.
	exec(`INSERT INTO ` + schema + `.documents (id, doctype, data, status, created_by) VALUES ('CC-G2OLD', 'CostCenter', '{"code":"CC-G2OLD","name":"G2"}', 'Active', 'admin')`)
	if err := post("JV-G2-CC", PostingOptions{CostCenter: "CC-G2OLD"}); err != nil {
		t.Fatalf("post with a cost center: %v", err)
	}
	tx, err := db.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := recodeInSchemaTx(tx, schema, "CostCenter", []MasterRecodeRow{{Old: "CC-G2OLD", New: "CC-G2NEW", Name: "G2"}}, "test"); err != nil {
		tx.Rollback()
		t.Fatalf("renumber: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var cc string
	db.DB.QueryRow(`SELECT MIN(cost_center) FROM ` + schema + `.gl_postings WHERE document_id = 'JV-G2-CC'`).Scan(&cc)
	if cc != "CC-G2NEW" {
		t.Errorf("after the renumber the postings name cost center %q, want CC-G2NEW", cc)
	}

	// A client's data reset is the one sanctioned delete.
	if err := SuspendTenant(tenantID, "test", "reset"); err != nil {
		t.Fatalf("suspend: %v", err)
	}
	exec(`INSERT INTO public.backup_runs (scope, tenant_schema, outcome, finished_at) VALUES ('tenant', $1, 'Success', NOW())`, schema)
	if _, err := ResetTenantData(tenantID, "test", "G2 reset"); err != nil {
		t.Fatalf("reset: %v", err)
	}
	var left int
	db.DB.QueryRow(`SELECT COUNT(*) FROM ` + schema + `.gl_postings`).Scan(&left)
	if left != 0 {
		t.Fatalf("the data reset left %d posting(s)", left)
	}
}

// G5: Rule 138 counts a consignment's value with its tax. An invoice just
// under Rs 50,000 before tax and over it with tax needs an e-way bill,
// whichever way its lines carry their value.
func TestStage47164eEWayValueIncludesTax(t *testing.T) {
	db.InitDB(testConnStr())
	const item = "T47164E-ITEM"
	ids := []string{"T47164E-TAXABLE", "T47164E-AMOUNT", "T47164E-PACKAGE", "T47164E-ITEMRATE", "T47164E-UNDER"}
	cleanup := func() {
		db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id = ANY($1)`, pqStringArray(append([]string{item}, ids...)))
	}
	cleanup()
	defer cleanup()
	insert := func(id, doctype string, data map[string]interface{}) {
		t.Helper()
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec(`INSERT INTO tenant_default.documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, 'Approved', 'admin')`, id, doctype, raw); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
	insert(item, "Item", map[string]interface{}{"code": item, "name": "Gold bangle", "hsn_code": "7113", "gst_rate": 3})
	invoice := func(id string, line map[string]interface{}) {
		line["sku"] = item
		insert(id, "SalesInvoice", map[string]interface{}{"invoice_number": id, "total_amount": 50264, "items": []interface{}{line}})
	}
	invoice("T47164E-TAXABLE", map[string]interface{}{"hsn_code": "7113", "qty": 1, "taxable": 48800, "gst_rate": 3}) // 48,800 + 3% = 50,264
	invoice("T47164E-AMOUNT", map[string]interface{}{"hsn_code": "7113", "qty": 1, "rate": 50264, "amount": 50264})   // the line's amount includes GST
	invoice("T47164E-PACKAGE", map[string]interface{}{"qty": 2, "unit_price": 25132, "line_total": 50264})            // a package bill's line
	invoice("T47164E-ITEMRATE", map[string]interface{}{"qty": 1, "taxable": 48800})                                   // the item's 3%
	invoice("T47164E-UNDER", map[string]interface{}{"hsn_code": "7113", "qty": 1, "taxable": 48000, "gst_rate": 3})   // 49,440 with tax

	threshold := GetSettingFloat("default", SettingEWBThreshold)
	if threshold != 50000 {
		t.Skipf("gst.ewb_threshold is %.2f on this database; the cases are built around 50,000", threshold)
	}
	for _, c := range []struct {
		id       string
		value    float64
		required bool
	}{
		{"T47164E-TAXABLE", 50264, true},
		{"T47164E-AMOUNT", 50264, true},
		{"T47164E-PACKAGE", 50264, true},
		{"T47164E-ITEMRATE", 50264, true},
		{"T47164E-UNDER", 49440, false},
	} {
		d, _, err := EWBDecisionFor("default", "SalesInvoice", c.id)
		if err != nil {
			t.Fatalf("%s: %v", c.id, err)
		}
		if d["value"] != c.value || d["required"] != c.required || d["rule_version"] != ewbDecisionVersion {
			t.Errorf("%s: value %v required %v (%v), want %.2f %v", c.id, d["value"], d["required"], d["reason"], c.value, c.required)
		}
	}
}

// G6: the IRP's raw response is kept with the attempt, with nothing secret in
// it, and an acknowledgement never changes once the IRP has given it.
func TestStage47164fIRPResponseKeptAndAckFrozen(t *testing.T) {
	db.InitDB(testConnStrUnsanctioned())
	defer db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	for _, f := range []string{"../db/migrations_stage75_2_einvoice.sql", "../db/migrations_stage75_2_irp_evidence_47_16_4f.sql"} {
		sql, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.DB.Exec(string(sql)); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
	}
	kick := irpKickWorker
	irpKickWorker = func(string) {}
	defer func() { irpKickWorker = kick }()
	FakeIRP.Reset()
	defer FakeIRP.Reset()

	exec := func(q string, args ...interface{}) {
		t.Helper()
		if _, err := db.DB.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	doc := func(id, doctype, status string, data map[string]interface{}) {
		t.Helper()
		b, _ := json.Marshal(data)
		exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1,$2,$3,$4,'tester')", id, doctype, b, status)
	}
	const seller, buyer = "29AAACE7520K1ZI", "29BBBCF1234L1ZK"
	doc("LE-T", "LegalEntity", "Active", map[string]interface{}{"code": "LE-T", "name": "Test Seller Pvt Ltd", "gstin": seller, "state": "Karnataka", "address": "1 MG Road", "city": "Bengaluru", "pincode": "560001"})
	doc("LOC-T", "Location", "Active", map[string]interface{}{"code": "LOC-T", "name": "Test Store", "legal_entity": "LE-T", "address": "1 MG Road", "city": "Bengaluru", "pincode": "560001", "state": "Karnataka"})
	doc("C-B2B", "Customer", "Active", map[string]interface{}{"code": "C-B2B", "name": "Buyer", "gstin": buyer, "address": "2 Brigade Road", "city": "Bengaluru", "pincode": "560025", "state": "Karnataka"})
	doc("SKU-T1", "Item", "Active", map[string]interface{}{"code": "SKU-T1", "name": "Cotton shirt", "hsn_code": "610910", "gst_rate": 5, "uom": "PCS"})
	doc("SI-G6", "SalesInvoice", "Approved", map[string]interface{}{"invoice_number": "SI/G6/2627/0001", "customer": "C-B2B", "location": "LOC-T",
		"items": []map[string]interface{}{{"sku": "SKU-T1", "qty": 2, "rate": 1050}}, "total_amount": 2100, "taxable_amount": 2000, "cgst": 50, "sgst": 50, "total_tax": 100})
	if _, err := SaveIRPProfile(tenantID, IRPProfile{GSTIN: seller, Enabled: true, Provider: "fake", InterGSTIN: "NOT_REQUIRED",
		EffectiveFrom: time.Now().AddDate(0, 0, -1).Format("2006-01-02")}, nil, "admin"); err != nil {
		t.Fatalf("profile: %v", err)
	}
	if err := RunEInvoiceWorkerOnce(tenantID); err != nil {
		t.Fatal(err)
	}
	r, err := GetIRPRecordFor(tenantID, "SalesInvoice", "SI-G6")
	if err != nil || r == nil || r.State != IRPStateReady {
		t.Fatalf("record: %+v %v", r, err)
	}
	if _, err := SubmitEInvoice(tenantID, r.ID, "acc"); err != nil {
		t.Fatal(err)
	}
	RunEInvoiceWorkerOnce(tenantID)
	if r, _ = GetIRPRecordFor(tenantID, "SalesInvoice", "SI-G6"); r == nil || r.State != IRPStateAcknowledged || r.IRN == "" {
		t.Fatalf("after the generate: %+v", r)
	}

	// The raw response is on the attempt, as the IRP sent it.
	var body string
	db.DB.QueryRow(`SELECT COALESCE(response_body, '') FROM `+schema+`.irp_attempts WHERE document_id = $1 AND action = 'generate' AND outcome = 'acknowledged'`, r.ID).Scan(&body)
	var resp map[string]interface{}
	if json.Unmarshal([]byte(body), &resp) != nil || resp["Irn"] != r.IRN || resp["SignedQRCode"] != r.SignedQR || resp["AckNo"] != r.AckNo || strings.Contains(body, "fake-token") {
		t.Fatalf("kept response: %q", body)
	}

	// The acknowledgement is frozen; the record's other columns still move.
	var frozen bool
	db.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM pg_trigger WHERE tgrelid = to_regclass($1) AND tgname = 'irp_documents_ack_immutable')`, schema+".irp_documents").Scan(&frozen)
	if !frozen {
		t.Fatal("irp_documents has no acknowledgement guard")
	}
	for col, set := range map[string]string{
		"irn": "irn = repeat('0', 64)", "ack_no": "ack_no = '999'", "ack_date": "ack_date = ack_date - INTERVAL '1 day'",
		"signed_qr": "signed_qr = 'x'", "signed_invoice": "signed_invoice = 'x'", "payload_hash": "payload_hash = repeat('a', 64)",
	} {
		if _, err := db.DB.Exec(`UPDATE `+schema+`.irp_documents SET `+set+` WHERE id = $1`, r.ID); err == nil || !strings.Contains(err.Error(), col) {
			t.Errorf("changing %s of an acknowledged e-invoice: want refused, got %v", col, err)
		}
	}
	exec(`UPDATE `+schema+`.irp_documents SET last_error = 'checked', updated_at = NOW() WHERE id = $1`, r.ID)
	if _, err := db.DB.Exec(`DELETE FROM `+schema+`.irp_documents WHERE id = $1`, r.ID); err == nil {
		t.Error("an acknowledged e-invoice record was deleted")
	}

	// Nothing secret is ever kept.
	kept := irpEvidenceBody([]byte(`{"Status":1,"Data":{"AuthToken":"tok-123","Sek":"key-456","nested":[{"client_secret":"s-789","Irn":"abc"}]},"Password":"pw"}`))
	for _, secret := range []string{"tok-123", "key-456", "s-789", `"pw"`} {
		if strings.Contains(kept, secret) {
			t.Errorf("a secret survived in %s", kept)
		}
	}
	if !strings.Contains(kept, `"Irn":"abc"`) {
		t.Errorf("the response itself was lost: %s", kept)
	}
	if kept := irpEvidenceBody([]byte("Authorization: Bearer abc.def password=hunter2")); strings.Contains(kept, "abc.def") || strings.Contains(kept, "hunter2") {
		t.Errorf("a text response kept a secret: %s", kept)
	}
	if kept := irpEvidenceBody(make([]byte, irpResponseBodyMax+1)); !strings.HasPrefix(kept, "[response of") {
		t.Errorf("an oversized response was kept: %d bytes", len(kept))
	}
}
