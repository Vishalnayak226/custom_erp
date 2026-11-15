package engines

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"custom_erp/db"
)

// 50.12 / BLD-030: the portable export, its import into a new client and the
// reconcile, in the three variants the line asks for.

func portableNewTenant(t *testing.T) (string, string) {
	t.Helper()
	id, schema := uniqueLifecycleTenant(t)
	t.Cleanup(func() { dropLifecycleTenant(id, schema) })
	return id, schema
}

func portableSource(t *testing.T) (tenantID, schema string) {
	t.Helper()
	tenantID, schema, insert := stage140Tenant(t)
	for i := 1; i <= 25; i++ {
		insert(fmt.Sprintf("SO-PX-%03d", i), "SalesOrder", "Draft", "admin", map[string]interface{}{"customer": "C1", "note": "line \"quoted\" é " + fmt.Sprint(i), "amount": float64(i) * 10.5})
	}
	return tenantID, schema
}

func portableTenantExists(tenantID, schema string) bool {
	var n int
	_ = db.DB.QueryRow(`SELECT (SELECT count(*) FROM public.tenants WHERE tenant_id = $1) + (SELECT count(*) FROM information_schema.schemata WHERE schema_name = $2)`, tenantID, schema).Scan(&n)
	return n > 0
}

func portableRewriteManifest(t *testing.T, dir string, edit func(m *PortableManifest)) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m PortableManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	edit(&m)
	m.Digest = m.computeDigest()
	out, _ := json.MarshalIndent(&m, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), out, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestPortableExportRoundTripSameVersion(t *testing.T) {
	src, _ := portableSource(t)
	dir := filepath.Join(t.TempDir(), "export")
	m, err := ExportTenantPortable(src, dir, "tester", false)
	if err != nil {
		t.Fatal(err)
	}
	if m.FormatVersion != PortableExportFormat || len(m.Migrations) == 0 || len(m.Tables) < 20 {
		t.Fatalf("manifest looks wrong: format %d, %d migrations, %d tables", m.FormatVersion, len(m.Migrations), len(m.Tables))
	}
	for _, secret := range []string{"api_credentials", "async_jobs", "channel_credentials"} {
		if _, ok := m.Excluded[secret]; !ok {
			t.Errorf("%s must be listed as excluded", secret)
		}
	}
	if _, err := ExportTenantPortable(src, dir, "tester", false); err == nil || !strings.Contains(err.Error(), "not empty") {
		t.Fatalf("exporting into a used folder must be refused, got %v", err)
	}

	dst, dstSchema := portableNewTenant(t)
	res, err := ImportTenantPortable(dir, dst, dstSchema, "tester", "round trip test")
	if err != nil {
		t.Fatal(err)
	}
	if res.Rows == 0 || res.Digest != m.Digest {
		t.Fatalf("import result: %+v", res)
	}
	var n int
	if err := db.DB.QueryRow(`SELECT count(*) FROM ` + dstSchema + `.documents WHERE id LIKE 'SO-PX-%'`).Scan(&n); err != nil || n != 25 {
		t.Fatalf("documents in the new client: %d (%v)", n, err)
	}
	rep, err := ReconcileTenantPortable(dir, dst)
	if err != nil || !rep.OK {
		t.Fatalf("reconcile after import: %+v %v", rep, err)
	}

	// A second import into the same client or schema is refused and changes nothing.
	if _, err := ImportTenantPortable(dir, dst, dstSchema, "tester", "again"); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("a re-run must be refused cleanly, got %v", err)
	}
	if rep, _ := ReconcileTenantPortable(dir, dst); rep == nil || !rep.OK {
		t.Fatalf("the refused re-run changed the client: %+v", rep)
	}

	// Reconcile never claims a pass on a difference.
	if _, err := db.DB.Exec(`UPDATE ` + dstSchema + `.documents SET data = jsonb_set(data, '{note}', '"changed"') WHERE id = 'SO-PX-001'`); err != nil {
		t.Fatal(err)
	}
	rep, err = ReconcileTenantPortable(dir, dst)
	if err != nil || rep.OK || !strings.Contains(strings.Join(rep.Differences, ";"), "documents") {
		t.Fatalf("a changed row must fail reconcile: %+v %v", rep, err)
	}
	if _, err := db.DB.Exec(`DELETE FROM ` + dstSchema + `.documents WHERE id = 'SO-PX-002'`); err != nil {
		t.Fatal(err)
	}
	rep, _ = ReconcileTenantPortable(dir, dst)
	if rep.OK || !strings.Contains(strings.Join(rep.Differences, ";"), "rows in the client") {
		t.Fatalf("a missing row must fail reconcile: %+v", rep)
	}
}

func TestPortableExportRefusesOutstandingQueuedWork(t *testing.T) {
	src, schema := portableSource(t)
	if _, err := db.DB.Exec(`INSERT INTO ` + schema + `.async_jobs (job_type, status) VALUES ('portable.test', 'Pending')`); err != nil {
		t.Skipf("async_jobs shape differs here: %v", err)
	}
	dir := filepath.Join(t.TempDir(), "export")
	if _, err := ExportTenantPortable(src, dir, "tester", false); err == nil || !strings.Contains(err.Error(), "queued work") {
		t.Fatalf("pending queued work must stop the export, got %v", err)
	}
	if _, err := os.Stat(dir); err == nil {
		t.Fatal("a refused export must leave no folder behind")
	}
	m, err := ExportTenantPortable(src, dir, "tester", true)
	if err != nil {
		t.Fatal(err)
	}
	if m.PendingQueue["async_jobs"] != 1 {
		t.Fatalf("manifest must record what was left out: %v", m.PendingQueue)
	}
}

func TestPortableImportUpgradeAndFormatRules(t *testing.T) {
	src, _ := portableSource(t)
	dir := filepath.Join(t.TempDir(), "export")
	if _, err := ExportTenantPortable(src, dir, "tester", false); err != nil {
		t.Fatal(err)
	}

	// (b) an export made under an older migration set, imported after newer additive migrations.
	var dropped string
	portableRewriteManifest(t, dir, func(m *PortableManifest) {
		dropped = m.Migrations[len(m.Migrations)-1]
		m.Migrations = m.Migrations[:len(m.Migrations)-1]
	})
	dst, dstSchema := portableNewTenant(t)
	res, err := ImportTenantPortable(dir, dst, dstSchema, "tester", "upgrade test")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.NewerBy) != 1 || res.NewerBy[0] != dropped {
		t.Fatalf("the upgrade must be reported: %+v", res)
	}

	// a column this version lacks cannot be carried
	portableRewriteManifest(t, dir, func(m *PortableManifest) {
		m.Tables[0].Columns = append(m.Tables[0].Columns, "column_from_the_future")
	})
	dst2, dst2Schema := portableNewTenant(t)
	if _, err := ImportTenantPortable(dir, dst2, dst2Schema, "tester", "future column"); err == nil || !strings.Contains(err.Error(), "column_from_the_future") {
		t.Fatalf("an unknown column must be refused, got %v", err)
	}
	if portableTenantExists(dst2, dst2Schema) {
		t.Fatal("a refused import left a client behind")
	}

	// an export made under migrations this database has not got
	portableRewriteManifest(t, dir, func(m *PortableManifest) {
		m.Tables[0].Columns = m.Tables[0].Columns[:len(m.Tables[0].Columns)-1]
		m.Migrations = append(m.Migrations, "migrations_stage999_9_not_yet.sql")
	})
	if _, err := ImportTenantPortable(dir, dst2, dst2Schema, "tester", "target older"); err == nil || !strings.Contains(err.Error(), "older than the export") {
		t.Fatalf("a newer export must be refused with the reason, got %v", err)
	}

	// a newer format
	portableRewriteManifest(t, dir, func(m *PortableManifest) { m.FormatVersion = PortableExportFormat + 1 })
	if _, err := ImportTenantPortable(dir, dst2, dst2Schema, "tester", "newer format"); err == nil || !strings.Contains(err.Error(), "upgrade this system first") {
		t.Fatalf("a newer format must be refused, got %v", err)
	}
	if portableTenantExists(dst2, dst2Schema) {
		t.Fatal("a refused import left a client behind")
	}
}

func TestPortableImportRefusesDamagedAndPartialExports(t *testing.T) {
	src, _ := portableSource(t)
	base := filepath.Join(t.TempDir(), "export")
	if _, err := ExportTenantPortable(src, base, "tester", false); err != nil {
		t.Fatal(err)
	}
	copyDir := func(name string) string {
		d := filepath.Join(t.TempDir(), name)
		_ = filepath.Walk(base, func(p string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(base, p)
			if info.IsDir() {
				return os.MkdirAll(filepath.Join(d, rel), 0o700)
			}
			b, _ := os.ReadFile(p)
			return os.WriteFile(filepath.Join(d, rel), b, 0o600)
		})
		return d
	}
	docs := filepath.Join("tables", "documents.jsonl")
	cases := []struct {
		name, want string
		damage     func(d string)
	}{
		{"no manifest (a partial export)", "manifest.json is missing", func(d string) { os.Remove(filepath.Join(d, "manifest.json")) }},
		{"a table file is missing", "is missing", func(d string) { os.Remove(filepath.Join(d, docs)) }},
		{"a table file is cut short", "rows", func(d string) {
			b, _ := os.ReadFile(filepath.Join(d, docs))
			lines := strings.SplitAfter(string(b), "\n")
			os.WriteFile(filepath.Join(d, docs), []byte(strings.Join(lines[:len(lines)/2], "")), 0o600)
		}},
		{"a row is altered", "sha256", func(d string) {
			b, _ := os.ReadFile(filepath.Join(d, docs))
			os.WriteFile(filepath.Join(d, docs), []byte(strings.Replace(string(b), "SO-PX-001", "SO-PX-9X1", 1)), 0o600)
		}},
		{"the manifest is damaged", "damaged", func(d string) { os.WriteFile(filepath.Join(d, "manifest.json"), []byte("{not json"), 0o600) }},
		{"the manifest is edited", "digest", func(d string) {
			b, _ := os.ReadFile(filepath.Join(d, "manifest.json"))
			var m PortableManifest
			_ = json.Unmarshal(b, &m)
			m.Tables[0].Rows++
			out, _ := json.Marshal(&m)
			os.WriteFile(filepath.Join(d, "manifest.json"), out, 0o600)
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := copyDir("bad")
			c.damage(d)
			id, schema := uniqueLifecycleTenant(t)
			t.Cleanup(func() { dropLifecycleTenant(id, schema) })
			_, err := ImportTenantPortable(d, id, schema, "tester", "damage test")
			if err == nil || !strings.Contains(err.Error(), c.want) || !strings.Contains(err.Error(), "nothing was written") {
				t.Fatalf("want a refusal mentioning %q and that nothing was written, got %v", c.want, err)
			}
			if portableTenantExists(id, schema) {
				t.Fatal("a refused import left a client behind")
			}
		})
	}

	// A file that passes every check but cannot be loaded (a duplicate primary key) rolls the new client back.
	t.Run("load fails midway", func(t *testing.T) {
		d := copyDir("dup")
		path := filepath.Join(d, docs)
		b, _ := os.ReadFile(path)
		first := strings.SplitN(string(b), "\n", 2)[0]
		b = append(b, []byte(first+"\n")...)
		os.WriteFile(path, b, 0o600)
		lines, sum, err := portableFileSum(path)
		if err != nil {
			t.Fatal(err)
		}
		portableRewriteManifest(t, d, func(m *PortableManifest) {
			for i := range m.Tables {
				if m.Tables[i].Name == "documents" {
					m.Tables[i].Rows, m.Tables[i].SHA256 = lines, sum
				}
			}
		})
		id, schema := uniqueLifecycleTenant(t)
		t.Cleanup(func() { dropLifecycleTenant(id, schema) })
		_, err = ImportTenantPortable(d, id, schema, "tester", "rollback test")
		if err == nil || !strings.Contains(err.Error(), "nothing was kept") {
			t.Fatalf("want a rollback message, got %v", err)
		}
		if portableTenantExists(id, schema) {
			t.Fatal("a failed load left a client behind")
		}
	})
}
