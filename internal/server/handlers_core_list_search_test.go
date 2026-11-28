package server

import (
	"custom_erp/db"
	"custom_erp/engines"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Stage 47.10 (ask 192): the big-master search rules - exact match first,
// identity fields only over 100k rows unless the caller names fields, and an
// estimated total past the count cap.
func TestStage47MasterSearchRules(t *testing.T) {
	db.InitDB(testConnStr())
	tag := fmt.Sprintf("MSRCH%d", time.Now().UnixNano())
	clean := func() {
		db.DB.Exec(`DELETE FROM tenant_default.documents WHERE doctype = 'Customer' AND id LIKE $1`, "%"+tag+"%")
	}
	clean()
	defer clean()
	insert := func(id string, data map[string]interface{}) {
		t.Helper()
		data["id"] = id
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec(`INSERT INTO tenant_default.documents (id, doctype, data, status, created_by) VALUES ($1, 'Customer', $2, 'Active', 'admin')`, id, raw); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
	for i := 1; i <= 5; i++ {
		id := fmt.Sprintf("%s-%d", tag, i)
		insert(id, map[string]interface{}{"code": id, "name": tag + " customer", "city": "Pune"})
	}
	// Its id sorts last; its code is the whole search term.
	insert("Z"+tag, map[string]interface{}{"code": tag, "name": "Exact customer", "city": "Pune"})
	// Holds a second term only in city, a field outside the identity set.
	insert(tag+"-C", map[string]interface{}{"code": "C" + tag, "name": "City customer", "city": tag + "CITY"})

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/doc/{doctype}", apiMiddleware(handleGenericDoc))
	token := engines.SignToken("admin", "admin", engines.RoleSuperAdmin, "default", "HO", currentCredentialVersion("admin"))
	list := func(query string) ([]map[string]interface{}, http.Header) {
		t.Helper()
		rec := doRequest(t, mux.ServeHTTP, http.MethodGet, "/api/v1/doc/Customer?"+query, token, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("list %s: %d %s", query, rec.Code, rec.Body.String())
		}
		var rows []map[string]interface{}
		json.Unmarshal(rec.Body.Bytes(), &rows)
		return rows, rec.Header()
	}
	ids := func(rows []map[string]interface{}) []string {
		out := make([]string, len(rows))
		for i, r := range rows {
			out[i] = fmt.Sprint(r["id"])
		}
		return out
	}

	big := false
	restoreBig := masterSearchIsBig
	masterSearchIsBig = func(schema, doctype string) bool { return big && doctype == "Customer" }
	defer func() { masterSearchIsBig = restoreBig }()

	// (1) The exact code match leads, though its id sorts last, and pinning
	// it adds no row: the seven matches stay seven.
	rows, h := list("q=" + url.QueryEscape(tag) + "&count=true&limit=25")
	if got := ids(rows); len(got) != 7 || got[0] != "Z"+tag {
		t.Fatalf("search %s: rows %v; want 7 with Z%s first", tag, got, tag)
	}
	if h.Get("X-Total-Count") != "7" || h.Get("X-Total-Count-Estimated") != "" {
		t.Fatalf("search %s: total %q estimated %q; want an exact 7", tag, h.Get("X-Total-Count"), h.Get("X-Total-Count-Estimated"))
	}
	// Newest-first keeps the exact match first too.
	if rows, _ = list("q=" + url.QueryEscape(tag) + "&sort=recent&limit=25"); len(rows) != 7 || fmt.Sprint(rows[0]["id"]) != "Z"+tag {
		t.Fatalf("sort=recent search: rows %v; want Z%s first", ids(rows), tag)
	}
	// 297.4: a later page (ids picked first) keeps the exact-first order.
	rows, _ = list("q=" + url.QueryEscape(tag) + "&limit=25")
	whole := ids(rows)
	if paged, _ := list("q=" + url.QueryEscape(tag) + "&limit=3&offset=1"); strings.Join(ids(paged), ",") != strings.Join(whole[1:4], ",") {
		t.Fatalf("offset 1: rows %v; want %v", ids(paged), whole[1:4])
	}

	// (3) A small master searches every field; a big one only its identity
	// fields, unless the caller names the field.
	cityTerm := url.QueryEscape(tag + "CITY")
	if rows, _ = list("q=" + cityTerm); len(rows) != 1 {
		t.Fatalf("small master, city search: rows %v; want the city customer", ids(rows))
	}
	big = true
	if rows, _ = list("q=" + cityTerm); len(rows) != 0 {
		t.Fatalf("big master, city search: rows %v; a non-identity field must not be searched", ids(rows))
	}
	if rows, _ = list("q=" + cityTerm + "&q_fields=city"); len(rows) != 1 {
		t.Fatalf("big master, q_fields=city: rows %v; a named field is searched", ids(rows))
	}
	// The big-master identity search still finds by name and keeps exact first.
	if rows, _ = list("q=" + url.QueryEscape(tag) + "&limit=25"); len(rows) != 7 || fmt.Sprint(rows[0]["id"]) != "Z"+tag {
		t.Fatalf("big master identity search: rows %v; want 7 with Z%s first", ids(rows), tag)
	}
	big = false

	// (4) Past the cap the total is an estimate, never below the cap, and
	// the page itself is unchanged.
	restoreCap := listCountCap
	listCountCap = 3
	defer func() { listCountCap = restoreCap }()
	rows, h = list("q=" + url.QueryEscape(tag) + "&count=true&limit=25")
	total, _ := strconv.ParseInt(h.Get("X-Total-Count"), 10, 64)
	if h.Get("X-Total-Count-Estimated") != "true" || total <= 3 || len(rows) != 7 {
		t.Fatalf("over the cap: total %d estimated %q rows %d; want an estimate above 3 and all 7 rows", total, h.Get("X-Total-Count-Estimated"), len(rows))
	}
	if _, h = list("q=" + url.QueryEscape("Z"+tag) + "&count=true"); h.Get("X-Total-Count") != "1" || h.Get("X-Total-Count-Estimated") != "" {
		t.Fatalf("under the cap: total %q estimated %q; want an exact 1", h.Get("X-Total-Count"), h.Get("X-Total-Count-Estimated"))
	}
}

// The pre-filter only appears when the trigram index can serve it.
func TestStage47MasterSearchPrefilterScope(t *testing.T) {
	if masterSearchPrefilter("Item", masterSearchFields["Item"], "kurta", 4) == "" {
		t.Fatal("an Item search over its identity fields gets no pre-filter")
	}
	for _, c := range []struct {
		why, doctype string
		fields       []string
		term         string
	}{
		{"not a master", "SalesOrder", []string{"id"}, "kurta"},
		{"term under three characters", "Item", []string{"name"}, " ku "},
		{"a field the index lacks", "Customer", []string{"name", "city"}, "pune"},
	} {
		if got := masterSearchPrefilter(c.doctype, c.fields, c.term, 4); got != "" {
			t.Errorf("%s: pre-filter %q; want none", c.why, got)
		}
	}
}

// The migration writes the index expression by hand; the list endpoint builds
// it in Go. If the two drift apart the planner silently stops using the index,
// so this compares the definitions Postgres derives from each.
func TestStage47MasterSearchUsesTrigramIndex(t *testing.T) {
	db.InitDB(testConnStr())
	var opclass *string
	db.DB.QueryRow(`SELECT format('%I.gin_trgm_ops', n.nspname) FROM pg_extension e JOIN pg_namespace n ON n.oid = e.extnamespace WHERE e.extname = 'pg_trgm'`).Scan(&opclass)
	var migrated *string
	db.DB.QueryRow(`SELECT pg_get_indexdef(to_regclass('tenant_default.idx_documents_master_search_trgm'))`).Scan(&migrated)
	if opclass == nil || migrated == nil {
		t.Skip("pg_trgm or the Stage 47.10 index is absent on this database; the search runs without it")
	}
	tx, err := db.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`CREATE TEMP TABLE stage47_trgm_probe (LIKE tenant_default.documents) ON COMMIT DROP`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(fmt.Sprintf(`CREATE INDEX stage47_trgm_probe_idx ON stage47_trgm_probe USING gin (%s %s) WHERE doctype IN ('Item', 'Customer', 'Vendor') AND deleted_at IS NULL`, masterSearchIndexExpr, *opclass)); err != nil {
		t.Fatalf("index from masterSearchIndexExpr: %v", err)
	}
	var built string
	if err := tx.QueryRow(`SELECT pg_get_indexdef('stage47_trgm_probe_idx'::regclass)`).Scan(&built); err != nil {
		t.Fatal(err)
	}
	using := func(def string) string { return def[strings.Index(def, " USING "):] }
	if using(*migrated) != using(built) {
		t.Fatalf("the migration's index and masterSearchIndexExpr differ:\nmigration: %s\ngo:        %s", using(*migrated), using(built))
	}
}

// Ask 297.4: migrations_stage297_4_item_identity_search.sql indexes the text
// an item box searches (q_fields=@identity: fieldsSearchClause over
// engines.SearchIdentityFields, in that order). If either side changes, the
// index stops serving the box and a 10-lakh-item search walks every item.
func TestAsk297ItemIdentityIndexMatchesThePicker(t *testing.T) {
	db.InitDB(testConnStr())
	var opclass *string
	db.DB.QueryRow(`SELECT format('%I.gin_trgm_ops', n.nspname) FROM pg_extension e JOIN pg_namespace n ON n.oid = e.extnamespace WHERE e.extname = 'pg_trgm'`).Scan(&opclass)
	var migrated *string
	db.DB.QueryRow(`SELECT pg_get_indexdef(to_regclass('tenant_default.idx_documents_item_identity_trgm'))`).Scan(&migrated)
	if opclass == nil || migrated == nil {
		t.Skip("pg_trgm or the 297.4 item identity index is absent on this database; the item box runs without it")
	}
	fields := engines.SearchIdentityFields("default", "Item")
	parts := make([]string, len(fields))
	for i, f := range fields {
		parts[i] = fmt.Sprintf("coalesce(data->>'%s', '')", f)
	}
	expr := "(" + strings.Join(parts, " || "+searchFieldSepSQL+" || ") + ")"
	tx, err := db.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`CREATE TEMP TABLE ask297_identity_probe (LIKE tenant_default.documents) ON COMMIT DROP`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(fmt.Sprintf(`CREATE INDEX ask297_identity_probe_idx ON ask297_identity_probe USING gin (%s %s) WHERE doctype = 'Item' AND deleted_at IS NULL`, expr, *opclass)); err != nil {
		t.Fatalf("index from the @identity fields: %v", err)
	}
	var built string
	if err := tx.QueryRow(`SELECT pg_get_indexdef('ask297_identity_probe_idx'::regclass)`).Scan(&built); err != nil {
		t.Fatal(err)
	}
	using := func(def string) string { return def[strings.Index(def, " USING "):] }
	if using(*migrated) != using(built) {
		t.Fatalf("the migration's index and the item box's search differ:\nmigration: %s\ngo:        %s", using(*migrated), using(built))
	}
	var stats int
	db.DB.QueryRow(`SELECT count(*) FROM pg_statistic_ext WHERE stxnamespace = 'tenant_default'::regnamespace AND stxname = 'documents_item_identity_stats'`).Scan(&stats)
	if stats != 1 {
		t.Fatalf("tenant_default lacks documents_item_identity_stats: the planner cannot tell a rare term from a common one")
	}
}

// Ask 297.4: a big transaction list is searched by number, and a deep page
// picks its ids first. Both run against a few rows, with the "big" check
// switched on by hand.
func TestAsk297BigListSearchAndDeepPage(t *testing.T) {
	db.InitDB(testConnStr())
	tag := fmt.Sprintf("BIGTXN%d", time.Now().UnixNano())
	clean := func() {
		db.DB.Exec(`DELETE FROM tenant_default.documents WHERE doctype = 'POSCart' AND id LIKE $1`, "%"+tag+"%")
	}
	clean()
	defer clean()
	for i := 1; i <= 6; i++ {
		id := fmt.Sprintf("%s-%d", tag, i)
		raw, _ := json.Marshal(map[string]interface{}{"id": id, "location": "HO", "note": "for " + strings.ToLower(tag) + "-note",
			"bill_number": fmt.Sprintf("B-%s-%d", tag, i)})
		if _, err := db.DB.Exec(`INSERT INTO tenant_default.documents (id, doctype, data, status, created_by) VALUES ($1, 'POSCart', $2, 'Open', 'admin')`, id, raw); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/doc/{doctype}", apiMiddleware(handleGenericDoc))
	token := engines.SignToken("admin", "admin", engines.RoleSuperAdmin, "default", "HO", currentCredentialVersion("admin"))
	list := func(doctype, query string) ([]string, http.Header) {
		t.Helper()
		rec := doRequest(t, mux.ServeHTTP, http.MethodGet, "/api/v1/doc/"+doctype+"?"+query, token, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("list %s %s: %d %s", doctype, query, rec.Code, rec.Body.String())
		}
		var rows []map[string]interface{}
		json.Unmarshal(rec.Body.Bytes(), &rows)
		out := make([]string, len(rows))
		for i, r := range rows {
			out[i] = fmt.Sprint(r["id"])
		}
		return out, rec.Header()
	}
	big := false
	restore := txnSearchByNumber
	txnSearchByNumber = func(schema, doctype string) bool { return big && doctype == "POSCart" }
	defer func() { txnSearchByNumber = restore }()

	noteTerm := url.QueryEscape(strings.ToLower(tag) + "-note")
	if got, h := list("POSCart", "q="+noteTerm+"&count=true"); len(got) != 6 || h.Get("X-Search-Scope") != "" {
		t.Fatalf("small list, any-field search: rows %v scope %q; want all 6, no scope", got, h.Get("X-Search-Scope"))
	}
	big = true
	if got, h := list("POSCart", "q="+noteTerm+"&count=true"); len(got) != 0 || h.Get("X-Search-Scope") != "number" || h.Get("X-Total-Count") != "0" {
		t.Fatalf("big list, word search: rows %v scope %q total %q; want none, scope number, total 0", got, h.Get("X-Search-Scope"), h.Get("X-Total-Count"))
	}
	for _, term := range []string{tag + "-3", " " + strings.ToLower(tag) + "-3 "} {
		if got, h := list("POSCart", "q="+url.QueryEscape(term)+"&count=true"); len(got) != 1 || got[0] != tag+"-3" || h.Get("X-Total-Count") != "1" {
			t.Fatalf("big list, number %q: rows %v total %q; want %s-3 alone", term, got, h.Get("X-Total-Count"), tag)
		}
	}
	if got, _ := list("POSCart", "q="+url.QueryEscape(strings.ToLower("B-"+tag+"-4"))); len(got) != 1 || got[0] != tag+"-4" {
		t.Fatalf("big list, printed bill number: rows %v; want %s-4", got, tag)
	}
	if got, _ := list("POSCart", "q="+url.QueryEscape(tag+"-3")+"&q_fields=@identity"); len(got) != 1 {
		t.Fatalf("big list, @identity picker: rows %v; want the numbered bill", got)
	}
	if got, h := list("POSCart", "q="+noteTerm+"&q_fields=note"); len(got) != 6 || h.Get("X-Search-Scope") != "" {
		t.Fatalf("big list, named field: rows %v scope %q; a field the caller names is still searched", got, h.Get("X-Search-Scope"))
	}
	big = false

	// A deep page is the same rows, in the same order, as that slice of
	// the whole list - with a search's arguments and without.
	whole, _ := list("POSCart", "q="+noteTerm+"&limit=6")
	for _, c := range []struct{ limit, offset int }{{2, 2}, {3, 3}, {4, 5}} {
		got, _ := list("POSCart", fmt.Sprintf("q=%s&limit=%d&offset=%d", noteTerm, c.limit, c.offset))
		want := whole[c.offset:min(c.offset+c.limit, len(whole))]
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("offset %d limit %d: rows %v; want %v", c.offset, c.limit, got, want)
		}
	}
}
