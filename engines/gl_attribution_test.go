package engines

import (
	"custom_erp/db"
	"strings"
	"sync"
	"testing"
	"time"
)

// Stage 73.1.9: every GL row names who posted it.
func TestGLPostingAttributionStampsActorAndReportsFilterOnIt(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	cleanup := func() {
		db.DB.Exec("DELETE FROM " + schema + ".gl_postings WHERE document_type = 'TestGLAttribution'")
	}
	cleanup()
	defer cleanup()
	today := time.Now().Format("2006-01-02")
	post := func(id string) {
		t.Helper()
		if err := PostDoubleEntry(tenantID, "TestGLAttribution", id, map[string]int64{"1100": 500}, map[string]int64{"4100": 500}, "", ""); err != nil {
			t.Fatalf("PostDoubleEntry %s: %v", id, err)
		}
	}
	attribution := func(id string) (string, string) {
		t.Helper()
		var by, command string
		if err := db.DB.QueryRow("SELECT COALESCE(posted_by,''), COALESCE(source_command,'') FROM "+schema+
			".gl_postings WHERE document_type = 'TestGLAttribution' AND document_id = $1 LIMIT 1", id).Scan(&by, &command); err != nil {
			t.Fatalf("read attribution %s: %v", id, err)
		}
		return by, command
	}

	// Nothing bound: still attributed, to the engine function that posted.
	post("UNBOUND")
	by, command := attribution("UNBOUND")
	if by != "service:background" || !strings.HasPrefix(command, "func:") || !strings.Contains(command, "TestGLPostingAttribution") {
		t.Fatalf("unbound posting attributed to %q / %q", by, command)
	}

	// A request binding: the user and route.
	release := BindPostingActor(RequestPostingActor("u-1", "manager1", "", "POST /api/v1/finance/journal-voucher"))
	post("REQUEST-1")
	// Nested (a request running a job inline): the inner binding wins, and
	// releasing it restores the request's own.
	inner := BindPostingActor(PostingActor{PostedBy: "service:job", SourceCommand: "job:test"})
	post("NESTED-JOB")
	inner()
	post("REQUEST-2")
	release()

	for id, want := range map[string]string{"REQUEST-1": "manager1", "NESTED-JOB": "service:job", "REQUEST-2": "manager1"} {
		if by, _ := attribution(id); by != want {
			t.Fatalf("%s posted_by = %q, want %q", id, by, want)
		}
	}
	if _, command := attribution("REQUEST-1"); command != "POST /api/v1/finance/journal-voucher" {
		t.Fatalf("REQUEST-1 source_command = %q", command)
	}

	// A binding is per goroutine: another goroutine posting at the same time
	// does not see this one's user.
	release = BindPostingActor(PostingActor{PostedBy: "cashier1", SourceCommand: "POST /x"})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); post("OTHER-GOROUTINE") }()
	wg.Wait()
	release()
	if by, _ := attribution("OTHER-GOROUTINE"); by != "service:background" {
		t.Fatalf("a binding leaked across goroutines: %q", by)
	}

	// A row from before 73.1.9 (NULL attribution).
	if _, err := db.DB.Exec("INSERT INTO " + schema + ".gl_postings (account_code, debit, credit, document_type, document_id) VALUES ('1100', 300, 0, 'TestGLAttribution', 'LEGACY'), ('4100', 0, 300, 'TestGLAttribution', 'LEGACY')"); err != nil {
		t.Fatalf("legacy insert: %v", err)
	}

	count := func(rows []map[string]interface{}, pred func(map[string]interface{}) bool) int {
		n := 0
		for _, r := range rows {
			if pred(r) {
				n++
			}
		}
		return n
	}
	isTest := func(r map[string]interface{}) bool { return r["document_type"] == "TestGLAttribution" }

	all, err := GetGLJournal(tenantID, today, today, "", "", "TestGLAttribution")
	if err != nil {
		t.Fatalf("GetGLJournal: %v", err)
	}
	if got := count(all, isTest); got != 12 { // 6 documents x 2 lines
		t.Fatalf("journal lines = %d, want 12", got)
	}
	before, _ := GetGLJournal(tenantID, today, today, "", "Before attribution", "TestGLAttribution")
	if len(before) != 2 || before[0]["posted_by"] != glBeforeAttribution {
		t.Fatalf("before-attribution filter = %v", before)
	}
	attributed, _ := GetGLJournal(tenantID, today, today, "", "Attributed", "TestGLAttribution")
	if len(attributed) != 10 {
		t.Fatalf("attributed filter = %d lines, want 10", len(attributed))
	}
	mine, _ := GetGLJournal(tenantID, today, today, "manager1", "", "TestGLAttribution")
	if len(mine) != 4 {
		t.Fatalf("posted_by=manager1 filter = %d lines, want 4", len(mine))
	}

	// Review: the poster's row count equals what its drill-down returns.
	review, err := GetGLPostingReview(tenantID, today, today)
	if err != nil {
		t.Fatalf("GetGLPostingReview: %v", err)
	}
	for _, r := range review {
		if r["posted_by"] != "manager1" && r["posted_by"] != glBeforeAttribution {
			continue
		}
		drill, err := GetGLJournal(tenantID, today, today, r["posted_by"].(string), "", "")
		if err != nil {
			t.Fatalf("drill: %v", err)
		}
		if len(drill) != r["lines"].(int) {
			t.Fatalf("review row %v counts %d lines but drills to %d", r["posted_by"], r["lines"], len(drill))
		}
	}

	export, err := GetStatutoryGLExport(tenantID, today, today)
	if err != nil {
		t.Fatalf("GetStatutoryGLExport: %v", err)
	}
	if count(export, func(r map[string]interface{}) bool { return isTest(r) && r["posted_by"] == "manager1" }) != 4 {
		t.Fatalf("statutory export does not carry posted_by")
	}
}
