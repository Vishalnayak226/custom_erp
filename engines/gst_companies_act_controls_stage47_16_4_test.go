package engines

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"custom_erp/db"
)

// Stage 47.16.4: a posted tax document is reversed, never un-posted. The
// rule sits in validatePostedAmountsFrozen, which every generic save runs
// through (ValidateTransactionalRules), so it holds whatever an
// administrator's status-transition map allows.
func TestStage47164PostedDocumentCannotBeUnpostedBySave(t *testing.T) {
	code := func(err error) string {
		var v *ValidationError
		if errors.As(err, &v) {
			return v.Code
		}
		return ""
	}
	invoice := map[string]interface{}{"invoice_number": "SI-1", "total_amount": 1180.0}
	with := func(status string) map[string]interface{} {
		p := map[string]interface{}{"status": status}
		for k, v := range invoice {
			p[k] = v
		}
		return p
	}
	cases := []struct {
		doctype, prior, to string
		refused            bool
	}{
		{"SalesInvoice", "Approved", "Cancelled", true},
		{"SalesInvoice", "Approved", "Draft", true},
		{"SalesInvoice", "Paid", "Cancelled", true},
		{"SalesInvoice", "Approved", "Paid", false},     // a settlement's own status
		{"SalesInvoice", "Approved", "Approved", false}, // an ordinary edit
		{"SalesInvoice", "Draft", "Cancelled", false},   // nothing posted yet
		{"CreditNote", "Posted", "Cancelled", true},
		{"DebitNote", "Posted", "Draft", true},
		{"JournalVoucher", "Posted", "Reversed", true}, // only ReverseJournalVoucher writes it
	}
	for _, c := range cases {
		err := validatePostedAmountsFrozen(c.doctype, "X-1", c.prior, invoice, with(c.to))
		if c.refused && code(err) != "FIN-0844" {
			t.Errorf("%s %s -> %s by a save: %v, want FIN-0844", c.doctype, c.prior, c.to, err)
		}
		if !c.refused && err != nil {
			t.Errorf("%s %s -> %s by a save: refused (%v), want allowed", c.doctype, c.prior, c.to, err)
		}
	}
}

// Stage 47.16.4: the platform's scale test used to begin by deleting the
// client's whole general ledger. It now leaves every existing posting where
// it is, and the books still balance after it.
func TestStage47164ScaleTestNeverDeletesTheLedger(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	// Stage 47.16.4b: the scale test runs only in a sandbox client.
	if _, err := db.DB.Exec(`UPDATE public.tenants SET is_sandbox = TRUE WHERE tenant_id = $1`, tenantID); err != nil {
		t.Fatal(err)
	}
	if err := PostDoubleEntry(tenantID, "JournalVoucher", "JV-KEEP", map[string]int64{"1100": 10000}, map[string]int64{"4100": 10000}, "", "JV-KEEP:POST"); err != nil {
		t.Fatalf("seed posting: %v", err)
	}
	if err := SeedScaleTestData(tenantID, 2, "BAR-SCALE", 50); err != nil {
		t.Fatalf("SeedScaleTestData: %v", err)
	}
	if _, err := RunScaleSimulation(tenantID, 2, 4, "BAR-SCALE", 2); err != nil {
		t.Fatalf("RunScaleSimulation: %v", err)
	}
	var kept int
	if err := db.DB.QueryRow("SELECT COUNT(*) FROM " + schema + ".gl_postings WHERE document_id = 'JV-KEEP'").Scan(&kept); err != nil {
		t.Fatal(err)
	}
	if kept != 2 {
		t.Fatalf("the journal posted before the scale test has %d of its 2 lines left", kept)
	}
	tb, err := GetTrialBalance(tenantID, allPostingsAsOf())
	if err != nil {
		t.Fatal(err)
	}
	if balanced, _ := tb["balanced"].(bool); !balanced {
		t.Errorf("the books do not balance after the scale test: %+v", tb)
	}
}

// Stage 47.16.4a (G1): a newly provisioned client keeps its own edit log - one
// unsigned row per save in its own audit_logs, naming the editor, the reason
// and the old and new value of every changed field - which the audit verifier
// and checkpoints accept, the change reports read, and the schema sync puts
// back if it goes missing. tenant_default writes the same one row per save.
func TestStage47164aEditLogOnEveryClient(t *testing.T) {
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
	maxSeq := func(s string) (n int64) {
		db.DB.QueryRow("SELECT COALESCE(MAX(seq), 0) FROM " + s + ".audit_logs").Scan(&n)
		return
	}
	const id = "EL47164A-1"
	db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id = $1`, id)
	defer db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id = $1`, id)
	defaultSince := maxSeq("tenant_default")

	exec(`INSERT INTO `+schema+`.documents (id, doctype, data, status, created_by) VALUES ($1, 'Brand', $2, 'Active', 'admin')`,
		id, `{"code": "`+id+`", "name": "Red", "mrp": 100, "tags": ["a"]}`)
	since := maxSeq(schema)
	exec(`UPDATE `+schema+`.documents SET data = $2, updated_by = 'manager1', edit_reason = 'price fix' WHERE id = $1`,
		id, `{"code": "`+id+`", "name": "Crimson \"deep\"", "mrp": 120, "colour": "red"}`)

	var user, action, details, entType, entID string
	var signed bool
	var rows int
	db.DB.QueryRow(`SELECT COUNT(*) FROM `+schema+`.audit_logs WHERE seq > $1`, since).Scan(&rows)
	if rows != 1 {
		t.Fatalf("one save wrote %d edit-log rows, want exactly 1", rows)
	}
	if err := db.DB.QueryRow(`SELECT user_id, action, details, COALESCE(entity_type, ''), COALESCE(entity_id, ''), signature IS NOT NULL
		FROM `+schema+`.audit_logs WHERE seq > $1`, since).Scan(&user, &action, &details, &entType, &entID, &signed); err != nil {
		t.Fatal(err)
	}
	if user != "manager1" || action != "UPDATE_Brand" || entType != "Brand" || entID != id || signed {
		t.Errorf("row: user %q, action %q, entity %s/%s, signed %v", user, action, entType, entID, signed)
	}
	if !strings.HasSuffix(details, " for Document ID: "+id+" (reason: price fix)") {
		t.Errorf("details do not name the record and the reason: %s", details)
	}
	docID, changes := parseAuditFieldChanges(details)
	want := []auditFieldChange{{"colour", "", "red"}, {"mrp", "100", "120"}, {"name", "Red", `Crimson "deep"`}, {"tags", `["a"]`, ""}}
	if docID != id || len(changes) != len(want) {
		t.Fatalf("parsed %q %+v, want %+v", docID, changes, want)
	}
	for i := range want {
		if changes[i] != want[i] {
			t.Errorf("change %d: %+v, want %+v", i, changes[i], want[i])
		}
	}

	// Nothing in data changed: no row.
	exec(`UPDATE `+schema+`.documents SET status = 'Inactive', updated_at = CURRENT_TIMESTAMP WHERE id = $1`, id)
	exec(`UPDATE `+schema+`.documents SET data = data WHERE id = $1`, id)
	if n := maxSeq(schema); n != since+1 {
		t.Errorf("updates that change no field wrote %d more row(s)", n-since-1)
	}
	if n := maxSeq("tenant_default"); n != defaultSince {
		var leaked int
		db.DB.QueryRow(`SELECT COUNT(*) FROM tenant_default.audit_logs WHERE seq > $1 AND details LIKE '%' || $2 || '%'`, defaultSince, id).Scan(&leaked)
		if leaked > 0 {
			t.Errorf("the client's edit landed in tenant_default.audit_logs (%d rows)", leaked)
		}
	}

	// The change reports read the new shape.
	mrp, err := auditChangeRows([]string{"Brand"}, []string{"mrp"})(tenantID, schema, map[string]string{})
	if err != nil || len(mrp) != 1 || mrp[0].H["old"] != "100" || mrp[0].H["new"] != "120" || mrp[0].H["doc_id"] != id {
		t.Errorf("MRP change rows: %v %+v", err, mrp)
	}
	// ...and so do a service ticket's status history and the taxonomy history.
	exec(`INSERT INTO ` + schema + `.documents (id, doctype, data, status, created_by) VALUES ('ST47164A', 'ServiceTicket', '{"status": "Draft"}', 'Draft', 'admin'),
		('PF47164A', 'ProductFamily', '{"name": "Kurta"}', 'Active', 'admin')`)
	exec(`UPDATE ` + schema + `.documents SET data = '{"status": "Assigned", "assigned_to": "tech1"}', status = 'Assigned' WHERE id = 'ST47164A'`)
	exec(`UPDATE ` + schema + `.documents SET data = '{"name": "Kurta Set"}' WHERE id = 'PF47164A'`)
	if h := serviceStatusHistory(schema, "ST47164A"); len(h) != 1 || h[0]["change"] != `from "Draft" to "Assigned"` {
		t.Errorf("ticket status history: %+v", h)
	}
	if h, err := GetTaxonomyHistory(tenantID, "ProductFamily", "PF47164A"); err != nil || len(h) != 1 || !strings.Contains(h[0].Details, `"Kurta Set"`) {
		t.Errorf("taxonomy history: %v %+v", err, h)
	}

	// Unsigned trigger rows are counted, sealed by a checkpoint, never called tampered.
	if _, err := WriteAuditCheckpoint(tenantID, "Periodic"); err != nil {
		t.Fatalf("checkpoint: %v", err)
	}
	v, err := VerifyAuditEvidence(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.TamperedRows) > 0 || len(v.CheckpointsBad) > 0 || v.UnsignedLegacy < 1 || v.CheckpointsOK < 1 {
		t.Errorf("verification: %+v", v)
	}

	// A client that lost the trigger gets it back from the schema sync.
	exec(`DROP TRIGGER ` + db.DocumentEditLogTrigger + ` ON ` + schema + `.documents`)
	tx, err := db.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	rep, err := db.SyncTenantSchemaTx(tx, schema)
	if err == nil {
		err = tx.Commit()
	} else {
		tx.Rollback()
	}
	if err != nil || !slices.Contains(rep.TriggersAdded, "documents."+db.DocumentEditLogTrigger) {
		t.Fatalf("sync: %v, triggers added %v, warnings %v", err, rep.TriggersAdded, rep.Warnings)
	}
	since = maxSeq(schema)
	exec(`UPDATE `+schema+`.documents SET data = data || '{"mrp": 130}' WHERE id = $1`, id)
	var again int
	db.DB.QueryRow(`SELECT COUNT(*) FROM `+schema+`.audit_logs WHERE seq > $1 AND action = 'UPDATE_Brand' AND entity_id = $2`, since, id).Scan(&again)
	if again != 1 {
		t.Errorf("the re-attached trigger wrote %d rows, want 1", again)
	}

	// tenant_default: one row per save too.
	exec(`INSERT INTO tenant_default.documents (id, doctype, data, status, created_by) VALUES ($1, 'Brand', $2, 'Active', 'admin')`,
		id, `{"code": "`+id+`", "name": "Red", "mrp": 100}`)
	defaultSince = maxSeq("tenant_default")
	exec(`UPDATE tenant_default.documents SET data = $2 WHERE id = $1`, id, `{"code": "`+id+`", "name": "Blue", "mrp": 90}`)
	var defaultRows int
	db.DB.QueryRow(`SELECT COUNT(*) FROM tenant_default.audit_logs WHERE seq > $1 AND action = 'UPDATE_Brand' AND entity_id = $2`, defaultSince, id).Scan(&defaultRows)
	if defaultRows != 1 {
		t.Errorf("tenant_default wrote %d rows for one two-field save, want 1", defaultRows)
	}
}

// Stage 47.16.4c (G3): the audit trail is kept at least eight years (Rule 3(5)).
// The database refuses a delete_after_days below 2,922; an older, smaller value
// is read as the floor; and the retention delete never removes an audit row,
// checkpoint or archive younger than the floor, while an older one still goes.
func TestStage47164cAuditRetentionFloor(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	exec := func(q string) {
		t.Helper()
		if _, err := db.DB.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	if AuditLogMinRetentionDays != 2922 {
		t.Fatalf("the floor is eight years, 2,922 days")
	}
	for _, s := range []string{"tenant_default", schema} {
		var has bool
		db.DB.QueryRow(`SELECT EXISTS (SELECT 1 FROM pg_constraint c JOIN pg_class t ON t.oid = c.conrelid JOIN pg_namespace n ON n.oid = t.relnamespace
			WHERE n.nspname = $1 AND t.relname = 'audit_retention_policy' AND c.conname = 'audit_retention_policy_delete_floor')`, s).Scan(&has)
		if !has {
			t.Errorf("%s.audit_retention_policy has no 8-year floor", s)
		}
	}
	policy := `INSERT INTO ` + schema + `.audit_retention_policy (version, hot_window_days, archive_after_days, delete_after_days) VALUES `
	if _, err := db.DB.Exec(policy + `(2, 365, 365, 2921)`); err == nil || !strings.Contains(err.Error(), "audit_retention_policy_delete_floor") {
		t.Errorf("delete_after_days 2921 was accepted (%v)", err)
	}
	exec(policy + `(2, 365, 365, 2922)`)
	exec(policy + `(3, 365, 365, NULL)`)

	// A value written before the floor existed reads as the floor.
	exec(`ALTER TABLE ` + schema + `.audit_retention_policy DROP CONSTRAINT audit_retention_policy_delete_floor`)
	exec(policy + `(4, 365, 365, 30)`)
	p, err := currentRetentionPolicy(schema)
	if err != nil || p == nil || p.Version != 4 || !p.DeleteAfterDays.Valid || p.DeleteAfterDays.Int64 != AuditLogMinRetentionDays {
		t.Errorf("policy read: %v %+v", err, p)
	}

	for _, age := range []string{"100 days", "3000 days"} {
		ago := `now() - interval '` + age + `'`
		exec(`INSERT INTO ` + schema + `.audit_logs (user_id, action, status, details, created_at) VALUES ('t', 'T47164C', 'SUCCESS', 'floor', ` + ago + `)`)
		exec(`INSERT INTO ` + schema + `.audit_checkpoints (from_seq, to_seq, row_count, row_digest, signature, sig_version, created_at)
			VALUES (900000001, 900000001, 1, 'd', 's', 'v', ` + ago + `)`)
		exec(`INSERT INTO ` + schema + `.audit_archives (checkpoint_id, from_seq, to_seq, row_count, row_digest, file_path, file_sha256, manifest_signature, sig_version, compressed_bytes, created_at)
			VALUES (gen_random_uuid(), 900000001, 900000001, 1, 'd', '/nowhere/t47164c.enc', 'h', 's', 'v', 1, ` + ago + `)`)
	}
	count := func(table, cond string) (n int) {
		db.DB.QueryRow(`SELECT COUNT(*) FROM ` + schema + `.` + table + ` WHERE created_at ` + cond).Scan(&n)
		return
	}
	for _, table := range []string{"audit_logs", "audit_checkpoints", "audit_archives"} {
		young := count(table, `> now() - interval '2922 days'`)
		if _, err := retentionDelete(tenantID, schema, table, "TRUE"); err != nil {
			t.Fatalf("%s: %v", table, err)
		}
		if n := count(table, `> now() - interval '2922 days'`); n != young || n == 0 {
			t.Errorf("%s: %d of %d rows younger than eight years survived a delete-everything sweep", table, n, young)
		}
		if n := count(table, `< now() - interval '2922 days'`); n != 0 {
			t.Errorf("%s: the 3000-day-old row is past the floor and goes, %d left", table, n)
		}
	}
	if got := withAuditLogFloor("api_request_log", "x"); got != "x" {
		t.Errorf("a table off the audit list keeps its own predicate, got %q", got)
	}
}

// Stage 47.16.4: the control map (docs/security/gst-companies-act-controls.md)
// stays honest - every control row names a test that exists, or says Gap.
func TestStage47164ControlMapNamesLiveTests(t *testing.T) {
	root := filepath.Join("..")
	raw, err := os.ReadFile(filepath.Join(root, "docs", "security", "gst-companies-act-controls.md"))
	if err != nil {
		t.Fatal(err)
	}
	defined := map[string]bool{}
	fn := regexp.MustCompile(`(?m)^func (Test[A-Za-z0-9_]+)\(`)
	for _, dir := range []string{"engines", "internal", "cmd"} {
		filepath.Walk(filepath.Join(root, dir), func(path string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() && strings.HasSuffix(path, "_test.go") {
				b, _ := os.ReadFile(path)
				for _, m := range fn.FindAllSubmatch(b, -1) {
					defined[string(m[1])] = true
				}
			}
			return nil
		})
	}
	named := regexp.MustCompile("`(Test[A-Za-z0-9_]+)`")
	rows := 0
	for _, line := range strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n") {
		if !strings.HasPrefix(line, "| C") {
			continue // control rows are "| C1 | ..."
		}
		rows++
		tests := named.FindAllStringSubmatch(line, -1)
		if len(tests) == 0 && !strings.Contains(line, "Gap") {
			t.Errorf("control row names no test and is not marked Gap: %s", line)
		}
		for _, m := range tests {
			if !defined[m[1]] {
				t.Errorf("control map names %s, which no test defines", m[1])
			}
		}
	}
	if rows < 10 {
		t.Fatalf("read only %d control rows", rows)
	}
}
