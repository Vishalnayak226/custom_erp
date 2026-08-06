package engines

import (
	"custom_erp/db"
	"encoding/json"
	"strings"
	"testing"
)

// Stage 26.4.11. The properties worth asserting here are the governance ones,
// not the prose: that the generator never writes ProductContent, that it logs
// every run, that it is deterministic, and that supplier-sourced markup cannot
// survive into the draft.
func TestGenerateContentSuggestion(t *testing.T) {
	db.InitDB(testConnStr())
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatalf("resolve default tenant schema: %v", err)
	}

	const itemCode = "CA-ASSIST-FIXTURE"
	const namelessItem = "CA-ASSIST-NONAME"

	cleanup := func() {
		_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE id IN ($1, $2)", itemCode, namelessItem)
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'ContentAssistLog' AND data->>'item_code' LIKE 'CA-ASSIST-%'")
		_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'ProductContent' AND data->>'product_id' = $1", itemCode)
	}
	cleanup()
	defer cleanup()

	insertDoc := func(id, doctype, status string, data map[string]interface{}) {
		t.Helper()
		encoded, err := json.Marshal(data)
		if err != nil {
			t.Fatalf("marshal %s: %v", id, err)
		}
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system')", id, doctype, encoded, status); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}

	// The name deliberately already contains the brand, to exercise the
	// dedupe branch - "Acme Acme Cotton Shirt" is the bug being guarded.
	insertDoc(itemCode, "Item", "Active", map[string]interface{}{
		"code": itemCode, "name": "Acme Cotton Shirt", "brand": "Acme", "category": "Apparel",
	})

	sug, err := GenerateContentSuggestion("default", itemCode, "en", "manager1")
	if err != nil {
		t.Fatalf("GenerateContentSuggestion failed: %v", err)
	}

	// --- Brand must not be doubled up ------------------------------------
	if got := strings.Count(strings.ToLower(sug.Title), "acme"); got != 1 {
		t.Errorf("title %q should mention the brand exactly once, got %d", sug.Title, got)
	}

	// --- SEO title stays within budget and is not cut mid-word ------------
	if len(sug.SEOTitle) > assistMaxSEOTitle {
		t.Errorf("seo_title %q is %d chars, over the %d budget", sug.SEOTitle, len(sug.SEOTitle), assistMaxSEOTitle)
	}

	// --- Provenance is populated -----------------------------------------
	if len(sug.SourceFields) == 0 {
		t.Error("expected source_fields to name what the draft was built from")
	}

	// --- The human-in-the-loop guarantee: NO ProductContent was written ---
	// This is the assertion that actually matters. If a future refactor makes
	// the engine helpfully persist a draft, this fails.
	var contentRows int
	if err := db.DB.QueryRow("SELECT COUNT(*) FROM "+schema+".documents WHERE doctype = 'ProductContent' AND data->>'product_id' = $1", itemCode).Scan(&contentRows); err != nil {
		t.Fatalf("count ProductContent: %v", err)
	}
	if contentRows != 0 {
		t.Errorf("content assist must never write ProductContent, found %d row(s)", contentRows)
	}

	// --- Every generation is logged --------------------------------------
	var logRows int
	if err := db.DB.QueryRow("SELECT COUNT(*) FROM "+schema+".documents WHERE doctype = 'ContentAssistLog' AND data->>'item_code' = $1", itemCode).Scan(&logRows); err != nil {
		t.Fatalf("count ContentAssistLog: %v", err)
	}
	if logRows != 1 {
		t.Errorf("expected exactly 1 ContentAssistLog row, got %d", logRows)
	}

	// --- Deterministic: same item, same draft ------------------------------
	// Two reviewers comparing notes must see identical text; attribute
	// ordering upstream comes from a map, so this is a real risk.
	again, err := GenerateContentSuggestion("default", itemCode, "en", "manager1")
	if err != nil {
		t.Fatalf("second GenerateContentSuggestion failed: %v", err)
	}
	if again.Title != sug.Title || again.LongDesc != sug.LongDesc || again.Tags != sug.Tags {
		t.Error("generator is not deterministic - the same item produced two different drafts")
	}

	// --- An item with no name is refused, not given a SKU-shaped title ----
	insertDoc(namelessItem, "Item", "Active", map[string]interface{}{"code": namelessItem})
	if _, err := GenerateContentSuggestion("default", namelessItem, "en", "manager1"); err == nil {
		t.Error("expected an error for an item with no name")
	}

	// --- Unknown item is an error, not an empty draft ---------------------
	if _, err := GenerateContentSuggestion("default", "CA-ASSIST-DOES-NOT-EXIST", "en", "manager1"); err == nil {
		t.Error("expected an error for an unknown item")
	}
}

// sanitizeAssistInput is the defence against supplier-submitted markup reaching
// published copy (26.4.10 feeds attribute values). Worth testing directly since
// the path through a real supplier submission is long.
func TestSanitizeAssistInput(t *testing.T) {
	cases := []struct{ in, want string }{
		{"<script>alert(1)</script>", "scriptalert(1)/script"},
		{"Cotton   \n  Blend", "Cotton Blend"},
		{"Red\x00Shirt", "Red Shirt"},
		{"  padded  ", "padded"},
		{"<b>bold</b> claim", "bbold/b claim"},
	}
	for _, c := range cases {
		if got := sanitizeAssistInput(c.in); got != c.want {
			t.Errorf("sanitizeAssistInput(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	// The property that actually matters, stated independently of the exact
	// output: no angle bracket survives, so nothing downstream can be coerced
	// into treating the text as markup.
	for _, c := range cases {
		if strings.ContainsAny(sanitizeAssistInput(c.in), "<>") {
			t.Errorf("sanitizeAssistInput(%q) left an angle bracket in", c.in)
		}
	}
}

func TestTruncateOnWord(t *testing.T) {
	if got := truncateOnWord("short", 60); got != "short" {
		t.Errorf("under-budget string should pass through, got %q", got)
	}
	got := truncateOnWord("the quick brown fox jumps", 13)
	if strings.HasSuffix(got, " ") || len(got) > 13 {
		t.Errorf("truncateOnWord returned %q", got)
	}
	if strings.Contains(got, "brow") && !strings.Contains(got, "brown") {
		t.Errorf("truncateOnWord split a word: %q", got)
	}
}
