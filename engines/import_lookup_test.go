package engines

import (
	"context"
	"custom_erp/db"
	"fmt"
	"strings"
	"testing"

	"github.com/lib/pq"
)

// TestImportLookupReadsTheKeyOnStaleFigures (313.2) builds the 297.2.5
// slowdown: the table's figures are taken while it holds only other records,
// then an import adds 200,000 Items and nothing refreshes them. The lookup
// must still read documents_pkey, with or without data and the row lock.
func TestImportLookupReadsTheKeyOnStaleFigures(t *testing.T) {
	db.InitDB(testConnStr())
	tx, err := db.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	exec := func(q string) {
		t.Helper()
		if _, err := tx.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	// A temp table: autovacuum never visits one, so the figures stay stale.
	exec(`CREATE TEMP TABLE documents (id VARCHAR(100) PRIMARY KEY, doctype VARCHAR(100) NOT NULL, data JSONB,
		status VARCHAR(50) DEFAULT 'Active', deleted_at TIMESTAMP) ON COMMIT DROP`)
	exec(`CREATE INDEX documents_doctype_status ON pg_temp.documents (doctype, status)`)
	exec(`CREATE INDEX documents_active_doctype ON pg_temp.documents (doctype, deleted_at) WHERE deleted_at IS NULL`)
	exec(`INSERT INTO pg_temp.documents (id, doctype, data) SELECT 'C-' || g, 'Customer', '{}' FROM generate_series(1, 2000) g`)
	exec(`ANALYZE pg_temp.documents`)
	exec(`INSERT INTO pg_temp.documents (id, doctype, data) SELECT 'IT-' || g, 'Item', '{"n":1}' FROM generate_series(1, 200000) g`)

	ids := make([]string, 200)
	for i := range ids {
		ids[i] = fmt.Sprintf("IT-%d", 997*(i+1))
	}
	plan := func(q string, args ...interface{}) string {
		t.Helper()
		var out string
		if err := tx.QueryRow("EXPLAIN (FORMAT JSON) "+q, args...).Scan(&out); err != nil {
			t.Fatalf("explain %s: %v", q, err)
		}
		return out
	}
	old := plan(`SELECT id FROM pg_temp.documents WHERE doctype = $1 AND deleted_at IS NULL AND id = ANY($2)`, "Item", pq.Array(ids))
	t.Logf("the old lookup reads documents_pkey: %v", strings.Contains(old, `"documents_pkey"`))

	for _, c := range []struct{ withData, forUpdate bool }{{false, false}, {true, false}, {true, true}} {
		p := plan(importDocsByIDSQL("pg_temp.documents", c.withData, c.forUpdate), pq.Array(ids))
		if !strings.Contains(p, `"Index Name": "documents_pkey"`) || strings.Contains(p, "documents_active_doctype") || strings.Contains(p, "documents_doctype_status") {
			t.Errorf("withData=%v forUpdate=%v: the lookup must read documents_pkey only, plan:\n%s", c.withData, c.forUpdate, p)
		}
	}

	// What comes back: the doctype, status and deletion of every id, whatever
	// its doctype, and data only when asked.
	exec(`UPDATE pg_temp.documents SET deleted_at = NOW() WHERE id = 'IT-997'`)
	exec(`UPDATE pg_temp.documents SET status = NULL WHERE id = 'C-7'`)
	docs, err := importDocsByID(context.Background(), tx, "pg_temp.documents", append([]string{"C-7", "NOPE"}, ids...), true, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != len(ids)+1 {
		t.Fatalf("want %d records back, got %d", len(ids)+1, len(docs))
	}
	if d := docs["IT-997"]; d.live("Item") || !d.deleted || d.doctype != "Item" {
		t.Errorf("a deleted Item must come back deleted: %+v", d)
	}
	if d := docs["IT-1994"]; !d.live("Item") || d.status != "Active" || !d.hasStatus || string(d.data) != `{"n": 1}` {
		t.Errorf("a live Item: %+v (%s)", d, d.data)
	}
	if d := docs["C-7"]; d.live("Item") || d.doctype != "Customer" || d.hasStatus {
		t.Errorf("another doctype's id with no status: %+v", d)
	}
	bare, err := importDocsByID(context.Background(), tx, "pg_temp.documents", []string{"IT-1994"}, false, false)
	if err != nil || bare["IT-1994"].data != nil {
		t.Errorf("no data unless asked: %+v (%v)", bare, err)
	}
}

// TestImportRefusesADeletedRecordsID (313.2): a row naming a deleted record
// of its own doctype used to be written into that record, which stayed
// deleted, and counted as an update. It is refused, in a preview and a write.
func TestImportRefusesADeletedRecordsID(t *testing.T) {
	tenantID, schema := importPerfTenant(t)
	if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by, deleted_at)
		VALUES ('DL-00001', 'Item', '{"code":"DL-00001","name":"Gone"}', 'Active', 'test', NOW())`, schema)); err != nil {
		t.Fatal(err)
	}
	for _, dryRun := range []bool{true, false} {
		res, err := BulkImportCSV(tenantID, "Item", strings.NewReader(itemImportFixture("DL", 3, nil)), "test", RoleSuperAdmin, dryRun)
		if err != nil {
			t.Fatalf("dryRun=%v: %v", dryRun, err)
		}
		if res.FailedRows != 1 || len(res.CreatedIDs) != 2 || len(res.UpdatedIDs) != 0 || len(res.Errors) != 1 ||
			res.Errors[0].RowNumber != 3 || !strings.Contains(res.Errors[0].Message, "deleted Item") {
			t.Fatalf("dryRun=%v: want row 3 refused and two created, got %+v", dryRun, res)
		}
	}
	var name string
	var deleted bool
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT data->>'name', deleted_at IS NOT NULL FROM %s.documents WHERE id = 'DL-00001'`, schema)).Scan(&name, &deleted); err != nil || name != "Gone" || !deleted {
		t.Fatalf("the deleted record was changed: %q deleted=%v (%v)", name, deleted, err)
	}
}
