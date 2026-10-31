package engines

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"custom_erp/db"
)

// stateLargeColumn is a column of a state table whose size is not fixed by
// its type: jsonb / json / bytea, or unbounded text named like a body.
var stateLargeColumn = regexp.MustCompile(`(payload|body|response|result|detail|error|stack|message|snapshot|signed|comment|reason|note)`)

// TestStage49182StatePayloadAndQuerySLO (Stage 49.18.2, the remaining part):
// every bounded store states its maximum payload and the index and SLO of
// its lookups or its sweep, held to the schema and the code - a store with
// neither, a large column its payload does not name, a cap it names that the
// code does not have, or an index its table does not have fails.
func TestStage49182StatePayloadAndQuerySLO(t *testing.T) {
	db.InitDB(testConnStr())

	stores := SecurityStateStores()
	names := map[string]bool{}
	for _, s := range stores {
		names[s.Name] = true
		if s.MaxPayload == "" || s.QuerySLO == "" {
			t.Errorf("%s: maximum payload and query SLO must both be stated (stateStoreLimits)", s.Name)
		}
		if (s.Kind == "table") != (s.Index != "") {
			t.Errorf("%s (%s): a table names the index its queries read; memory and files name none - got %q", s.Name, s.Kind, s.Index)
		}
	}
	for name := range stateStoreLimits {
		if !names[name] {
			t.Errorf("stateStoreLimits has %q, which is not a store in SecurityStateStores", name)
		}
	}

	// The caps a payload names by identifier exist in the code.
	var src strings.Builder
	for _, pattern := range []string{"*.go", filepath.Join("..", "internal", "server", "*.go")} {
		files, _ := filepath.Glob(pattern)
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") {
				continue
			}
			b, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			src.Write(b)
		}
	}
	code := src.String()
	capName := regexp.MustCompile(`\(([A-Za-z][A-Za-z0-9]*)\)`)
	for _, s := range stores {
		for _, m := range capName.FindAllStringSubmatch(s.MaxPayload, -1) {
			if !regexp.MustCompile(`\b` + m[1] + `\b\s*(=|:=|\(|int|string)`).MatchString(code) {
				t.Errorf("%s: its payload names cap %s, which the code does not define", s.Name, m[1])
			}
		}
	}

	checked := 0
	for _, s := range stores {
		if s.Kind != "table" {
			continue
		}
		schema, table := "tenant_default", s.Name
		if strings.HasPrefix(table, "public.") {
			schema, table = "public", strings.TrimPrefix(table, "public.")
		}
		shared := strings.Contains(table, " (") // a part of a shared table (documents, tenants)
		table, _, _ = strings.Cut(table, " (")
		var exists bool
		if err := db.DB.QueryRow(`SELECT to_regclass($1) IS NOT NULL`, schema+"."+table).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if !exists {
			t.Logf("%s: not in this schema, skipped", s.Name)
			continue
		}
		checked++
		var indexed bool
		if err := db.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM pg_indexes WHERE schemaname = $1 AND tablename = $2 AND indexname = $3)`,
			schema, table, s.Index).Scan(&indexed); err != nil {
			t.Fatal(err)
		}
		if !indexed {
			t.Errorf("%s: index %s is not on %s.%s", s.Name, s.Index, schema, table)
		}
		if shared {
			continue
		}
		rows, err := db.DB.Query(`SELECT column_name FROM information_schema.columns
			WHERE table_schema = $1 AND table_name = $2
			  AND (data_type IN ('jsonb', 'json', 'bytea')
			       OR ((data_type = 'text' OR (data_type = 'character varying' AND COALESCE(character_maximum_length, 1000000) > 255)) AND column_name ~ $3))`,
			schema, table, stateLargeColumn.String())
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var col string
			if err := rows.Scan(&col); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(s.MaxPayload, col) {
				t.Errorf("%s: large column %s is not named in its maximum payload %q - say what caps it", s.Name, col, s.MaxPayload)
			}
		}
		rows.Close()
	}
	if checked < 30 {
		t.Fatalf("only %d state tables were checked; the registry or the schema changed", checked)
	}

	// The swept tables' sweeps have their index (db/migrations_stage140_13_state_sweep_indexes_49_18_2.sql).
	for table, index := range map[string]string{
		"async_jobs": "idx_async_jobs_completed", "system_error_logs": "idx_system_error_logs_created",
		"extension_hook_log": "idx_extension_hook_log_called", "crm_events": "crm_events_occurred_idx",
	} {
		var leads string
		db.DB.QueryRow(`SELECT a.attname FROM pg_index i JOIN pg_class c ON c.oid = i.indexrelid JOIN pg_namespace n ON n.oid = c.relnamespace
			JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = i.indkey[0] WHERE n.nspname = 'tenant_default' AND c.relname = $1`, index).Scan(&leads)
		if leads == "" {
			t.Errorf("%s: sweep index %s is missing", table, index)
		}
	}
}
