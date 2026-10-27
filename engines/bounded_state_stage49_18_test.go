package engines

// Stage 49.18.2 / 49.18.7 - bounded state, held to the schema and the code.

import (
	"custom_erp/db"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// stateTablePattern is what makes a table "state" rather than a business
// record: logs, tokens, idempotency and replay keys, jobs, audit and security
// stores. A new table matching it fails the test until SecurityStateStores
// says what bounds it.
const stateTablePattern = `(login|session|revoc|token|csrf|replay|idempot|nonce|signature|^async_jobs|alert|support_access|mfa|recovery|reset|webhook_receipt|attempt|api_key|^audit|security|outbox|_log$|_logs$|backup_run|lifecycle_event|_seen$|irp_)`

func TestStage49182EveryStateTableIsBounded(t *testing.T) {
	db.InitDB(testConnStr())

	registered := map[string]StateStore{}
	for _, s := range SecurityStateStores() {
		if _, dup := registered[s.Name]; dup {
			t.Errorf("%s is listed twice", s.Name)
		}
		registered[s.Name] = s
		if s.Bound == "" || s.LegalHold == "" || s.Key == "" {
			t.Errorf("%s: key, bound and legal-hold behaviour must all be stated", s.Name)
		}
	}

	rows, err := db.DB.Query(`SELECT CASE WHEN table_schema = 'public' THEN 'public.' ELSE '' END || table_name
		FROM information_schema.tables
		WHERE table_schema IN ('public', 'tenant_default') AND table_type = 'BASE TABLE' AND table_name ~ $1`, stateTablePattern)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	seen := 0
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		seen++
		if _, ok := registered[name]; !ok {
			t.Errorf("state table %s has no entry in SecurityStateStores - say what bounds it (a retention sweep through retentionDelete, a fixed size, or evidence kept on purpose)", name)
		}
	}
	if seen < 20 {
		t.Fatalf("only %d state tables matched; the pattern or the schema changed", seen)
	}

	// Every named cleanup exists and is referenced from somewhere other than
	// its own definition (a call, or a function value handed to a scheduler),
	// so the bound is enforced, not just written down.
	defined, referenced := map[string]bool{}, map[string]int{}
	fset := token.NewFileSet()
	files, _ := filepath.Glob("*.go")
	serverFiles, _ := filepath.Glob(filepath.Join("..", "internal", "server", "*.go"))
	for _, f := range append(files, serverFiles...) {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", f, err)
		}
		declNames := map[*ast.Ident]bool{}
		ast.Inspect(file, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.FuncDecl:
				if !strings.Contains(f, "server") {
					defined[x.Name.Name] = true
				}
				declNames[x.Name] = true
			case *ast.Ident:
				if !declNames[x] {
					referenced[x.Name]++
				}
			}
			return true
		})
	}
	for _, s := range registered {
		if s.Cleanup == "" {
			continue
		}
		if !defined[s.Cleanup] {
			t.Errorf("%s names cleanup %s, which does not exist", s.Name, s.Cleanup)
		} else if referenced[s.Cleanup] == 0 {
			t.Errorf("%s: cleanup %s exists but nothing calls it", s.Name, s.Cleanup)
		}
	}

	// In-memory stores: their caps exist where the registry says.
	for file, want := range map[string]string{
		"auth_livestate.go": "authStateCacheMaxEntries",
		filepath.Join("..", "internal", "server", "middleware.go"):  "rateLimitMaxKeys",
		filepath.Join("..", "internal", "server", "login_abuse.go"): "func pruneLoginAbuse",
	} {
		b, err := os.ReadFile(file)
		if err != nil || !strings.Contains(string(b), want) {
			t.Errorf("%s no longer holds %q - update SecurityStateStores", file, want)
		}
	}
}

func TestStage49187RetentionDeletesInBatchesAndHonoursLegalHold(t *testing.T) {
	db.InitDB(testConnStr())

	suffix := fmt.Sprintf("%d", time.Now().UnixNano()%1_000_000_000)
	tenantID, schema := "bst"+suffix, "bst_schema"+suffix
	if _, err := db.DB.Exec(`INSERT INTO public.tenants (tenant_id, name, schema_name, legal_hold, legal_hold_reason) VALUES ($1, $1, $2, TRUE, 'investigation')`, tenantID, schema); err != nil {
		t.Fatal(err)
	}
	defer db.DB.Exec(`DELETE FROM public.tenants WHERE tenant_id = $1`, tenantID)
	defer db.DB.Exec(`DROP SCHEMA IF EXISTS ` + schema + ` CASCADE`)
	for _, q := range []string{
		`CREATE SCHEMA ` + schema,
		`CREATE TABLE ` + schema + `.log (id SERIAL PRIMARY KEY, created_at TIMESTAMP NOT NULL)`,
		// More old rows than two batches, so the loop has to run three times.
		fmt.Sprintf(`INSERT INTO %s.log (created_at) SELECT NOW() - INTERVAL '400 days' FROM generate_series(1, %d)`, schema, 2*retentionBatchRows+1000),
		`INSERT INTO ` + schema + `.log (created_at) SELECT NOW() FROM generate_series(1, 300)`,
	} {
		if _, err := db.DB.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	count := func() (n int) {
		db.DB.QueryRow(`SELECT COUNT(*) FROM ` + schema + `.log`).Scan(&n)
		return
	}
	where := `created_at < CURRENT_TIMESTAMP - ($1 || ' days')::interval`
	total := count()

	// Under legal hold nothing goes.
	if n, err := retentionDelete(tenantID, schema, "log", where, 90); err != nil || n != 0 || count() != total {
		t.Fatalf("legal hold: deleted %d (err %v), %d of %d rows left", n, err, count(), total)
	}

	// Hold lifted: every old row goes across batches; the recent ones stay.
	db.DB.Exec(`UPDATE public.tenants SET legal_hold = FALSE WHERE tenant_id = $1`, tenantID)
	n, err := retentionDelete(tenantID, schema, "log", where, 90)
	if err != nil {
		t.Fatal(err)
	}
	if want := int64(2*retentionBatchRows + 1000); n != want || count() != 300 {
		t.Fatalf("deleted %d (want %d), %d rows left (want 300)", n, want, count())
	}
}
