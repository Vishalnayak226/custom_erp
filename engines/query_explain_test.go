package engines

import (
	"context"
	"custom_erp/db"
	"errors"
	"strings"
	"testing"
)

// Stage 47.9.3d: the operator's EXPLAIN (ANALYZE, BUFFERS) runs one read-only
// SELECT and nothing else, and its failures never quote a stored value.
func TestStage4793dExplainIsOneReadOnlySelect(t *testing.T) {
	for _, bad := range []string{
		"",
		"DELETE FROM documents",
		"select 1; select 2",
		"select 1 -- note",
		"select /* x */ 1",
		"select * from documents where id = $1",
		"select pg_terminate_backend(1)",
		`select "pg_terminate_backend"(1)`,
		"select pg_catalog.pg_advisory_lock(7)",
		"select set_config('search_path', 'public', false)",
		"select query_to_xml('select 1', true, true, '')",
		`select * from U&"d\0061ta"`,
	} {
		_, err := checkExplainStatement(bad)
		var r *ExplainRefusal
		if !errors.As(err, &r) {
			t.Errorf("%q was not refused (err %v)", bad, err)
		}
	}
	if s, err := checkExplainStatement("  SELECT 1;  "); err != nil || s != "SELECT 1" {
		t.Fatalf("a trailing semicolon should be dropped, got %q %v", s, err)
	}

	db.InitDB(testConnStr())
	ctx := context.Background()

	p, err := ExplainStatement(ctx, "default", "SELECT count(*) FROM tenant_?.documents")
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	all := strings.Join(p.Plan, "\n")
	if !strings.Contains(all, "actual time") || !strings.Contains(all, "Execution Time") {
		t.Fatalf("not an ANALYZE plan:\n%s", all)
	}

	// The search path is the client's: an unqualified table resolves.
	if _, err := ExplainStatement(ctx, "default", "SELECT 1 FROM documents LIMIT 1"); err != nil {
		t.Fatalf("unqualified table in the client's schema: %v", err)
	}

	// A data-changing CTE is stopped by the read-only transaction.
	_, err = ExplainStatement(ctx, "default", "WITH d AS (DELETE FROM public.ops_run_log WHERE false RETURNING 1) SELECT * FROM d")
	var r *ExplainRefusal
	if !errors.As(err, &r) || !strings.Contains(r.Error(), "read-only") {
		t.Fatalf("a DELETE inside WITH ran or failed oddly: %v", err)
	}

	// A failed cast quotes the value in Postgres's message; here only its code.
	_, err = ExplainStatement(ctx, "default", "SELECT 'card4111111111111111'::int")
	if err == nil || strings.Contains(err.Error(), "4111") || !strings.Contains(err.Error(), "SQLSTATE 22P02") {
		t.Fatalf("a failed cast leaked or was not reported by code: %v", err)
	}

	// A naming error keeps its message, so the operator can fix it.
	_, err = ExplainStatement(ctx, "default", "SELECT 1 FROM no_such_table_4793")
	if err == nil || !strings.Contains(err.Error(), "no_such_table_4793") {
		t.Fatalf("a naming error lost its message: %v", err)
	}
}
