package db

import "testing"

// Stage 64.1: a client schema provisioned before later migrations (as
// tenant_minn was on production) is brought up to tenant_default, and a
// second run changes nothing.
func TestSyncTenantSchemaBringsStaleClientUpToTemplate(t *testing.T) {
	InitDB(ConnStringFromEnv())
	const schema = "tenant_synctest641"
	mustExec := func(q string) {
		t.Helper()
		if _, err := DB.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	mustExec(`DROP SCHEMA IF EXISTS ` + schema + ` CASCADE`)
	t.Cleanup(func() { DB.Exec(`DROP SCHEMA IF EXISTS ` + schema + ` CASCADE`) })

	// A stale client: a handful of tables cloned the old way (sharing
	// tenant_default's sequences), one column missing, half the record types.
	// A serial-id template table with a row to copy, to check the per-client
	// sequence starts after the client's existing ids.
	candidates, err := DB.Query(`SELECT c.table_name FROM information_schema.columns c
		JOIN pg_tables p ON p.schemaname = c.table_schema AND p.tablename = c.table_name
		WHERE c.table_schema = 'tenant_default' AND c.column_name = 'id' AND c.column_default LIKE 'nextval%'
		  AND c.data_type IN ('integer', 'bigint') ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for candidates.Next() {
		var n string
		candidates.Scan(&n)
		names = append(names, n)
	}
	candidates.Close()
	var serialTable string
	for _, n := range names {
		var has bool
		if DB.QueryRow(`SELECT EXISTS (SELECT 1 FROM tenant_default.`+n+`)`).Scan(&has) == nil && has &&
			n != "doctype_fields" && n != "role_permissions" && n != "prefix_configs" {
			serialTable = n
			break
		}
	}
	if serialTable == "" {
		t.Skip("no serial-id template table with a row to copy")
	}

	mustExec(`CREATE SCHEMA ` + schema)
	for _, tbl := range []string{"doctype_meta", "doctype_fields", "role_permissions", "prefix_configs", "documents", "audit_logs", serialTable} {
		mustExec(`CREATE TABLE IF NOT EXISTS ` + schema + `.` + tbl + ` (LIKE tenant_default.` + tbl + ` INCLUDING ALL)`)
	}
	mustExec(`ALTER TABLE ` + schema + `.audit_logs DROP COLUMN IF EXISTS signature`)
	mustExec(`INSERT INTO ` + schema + `.doctype_meta SELECT * FROM tenant_default.doctype_meta WHERE name < 'M'`)
	// An existing record type whose permission an admin removed must not get it back.
	mustExec(`INSERT INTO ` + schema + `.doctype_meta SELECT * FROM tenant_default.doctype_meta WHERE name = 'Item' ON CONFLICT DO NOTHING`)
	mustExec(`INSERT INTO ` + schema + `.` + serialTable + ` SELECT * FROM tenant_default.` + serialTable + ` LIMIT 1`)
	mustExec(`UPDATE ` + schema + `.` + serialTable + ` SET id = 900000001`)

	run := func() TenantSchemaSyncReport {
		t.Helper()
		tx, err := DB.Begin()
		if err != nil {
			t.Fatal(err)
		}
		rep, err := SyncTenantSchemaTx(tx, schema)
		if err != nil {
			tx.Rollback()
			t.Fatalf("sync: %v", err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		return rep
	}
	rep := run()
	for _, w := range rep.Warnings {
		t.Logf("warning: %s", w)
	}
	t.Logf("first run: %s", rep.Summary())

	count := func(q string) int {
		t.Helper()
		var n int
		if err := DB.QueryRow(q).Scan(&n); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		return n
	}
	if n := count(`SELECT count(*) FROM pg_tables d WHERE d.schemaname = 'tenant_default'
		AND to_regclass('` + schema + `.' || quote_ident(d.tablename)) IS NULL`); n != 0 {
		t.Errorf("%d tenant_default tables still missing", n)
	}
	if n := count(`SELECT count(*) FROM information_schema.columns c WHERE c.table_schema = 'tenant_default'
		AND NOT EXISTS (SELECT 1 FROM information_schema.columns m WHERE m.table_schema = '` + schema + `'
		AND m.table_name = c.table_name AND m.column_name = c.column_name)
		AND EXISTS (SELECT 1 FROM pg_tables p WHERE p.schemaname='tenant_default' AND p.tablename=c.table_name)`); n != 0 {
		t.Errorf("%d columns still missing", n)
	}
	if n := count(`SELECT count(*) FROM information_schema.columns WHERE table_schema = '` + schema + `'
		AND column_default LIKE '%tenant_default.%'`); n != 0 {
		t.Errorf("%d columns still draw from tenant_default's sequences", n)
	}
	if n := count(`SELECT count(*) FROM (SELECT name FROM tenant_default.doctype_meta EXCEPT SELECT name FROM ` + schema + `.doctype_meta) x`); n != 0 {
		t.Errorf("%d record types missing", n)
	}
	if n := count(`SELECT count(*) FROM (SELECT doctype_name, fieldname FROM tenant_default.doctype_fields EXCEPT SELECT doctype_name, fieldname FROM ` + schema + `.doctype_fields) x`); n != 0 {
		t.Errorf("%d fields missing", n)
	}
	if n := count(`SELECT count(*) FROM (SELECT id FROM tenant_default.documents WHERE doctype = 'StatusTransitionRule' EXCEPT SELECT id FROM ` + schema + `.documents) x`); n != 0 {
		t.Errorf("%d status-change rules missing", n)
	}
	// Permissions: added for the record types the sync created, not for 'Item',
	// which the client already had.
	if n := count(`SELECT count(*) FROM ` + schema + `.role_permissions WHERE doctype_name = 'Item'`); n != 0 {
		t.Errorf("permissions were handed back for a record type the client already had (%d rows)", n)
	}
	if count(`SELECT count(*) FROM tenant_default.role_permissions WHERE doctype_name >= 'M'`) > 0 &&
		count(`SELECT count(*) FROM `+schema+`.role_permissions WHERE doctype_name >= 'M'`) == 0 {
		t.Errorf("no permissions copied for the record types the sync added")
	}
	// Own sequence, started after the existing rows.
	var next int64
	var seqSchema string
	if err := DB.QueryRow(`SELECT split_part(pg_get_serial_sequence('`+schema+`.`+serialTable+`', 'id'), '.', 1),
		nextval(pg_get_serial_sequence('`+schema+`.`+serialTable+`', 'id'))`).Scan(&seqSchema, &next); err != nil {
		t.Fatalf("own sequence of %s.id: %v", serialTable, err)
	}
	if seqSchema != schema {
		t.Errorf("%s.id sequence lives in %q, want %q", serialTable, seqSchema, schema)
	}
	if next <= 900000001 {
		t.Errorf("own sequence starts at %d, want after 900000001", next)
	}
	// The audit write that failed on production now has its column.
	mustExec(`INSERT INTO ` + schema + `.audit_logs (user_id, action, status, details, signature) VALUES ('t', 'TEST', 'ok', '', 'x')`)

	if again := run(); again.Changed() {
		t.Errorf("second run changed something: %s", again.Summary())
	}
}
