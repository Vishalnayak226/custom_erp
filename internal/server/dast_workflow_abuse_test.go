package server

import (
	"custom_erp/db"
	"custom_erp/engines"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// Stage 49.10.4 - workflow abuse as a test. The sibling sweep of
// TestStage49104AuthenticatedDAST sends hostile values to every route; this one
// plays a hostile user against the approval workflow through the same
// handlers: skipping states, deciding twice, replaying a decision, deciding
// your own document, deciding a document at another site, submitting an
// already-decided document, and racing transitions. Each refusal must leave
// the document and its approval log exactly as they were. The deployed proxy
// and WebSocket/SSE paths are outside what a unit test can reach and stay
// with the host checks.

type workflowAbuseFixture struct {
	t            *testing.T
	maker        string
	makerTok     string
	checkerTok   string
	otherSiteTok string
	submit       http.HandlerFunc
	decide       http.HandlerFunc
}

func newWorkflowAbuseFixture(t *testing.T) *workflowAbuseFixture {
	t.Helper()
	db.InitDB(testConnStr())
	f := &workflowAbuseFixture{t: t, submit: apiMiddleware(handleSubmitApproval), decide: apiMiddleware(handleDecideApproval)}
	suffix := contractUniqueID()
	seed := func(prefix, role, loc string) string {
		id := prefix + suffix
		if _, err := db.DB.Exec(`INSERT INTO tenant_default.users (id, username, password_hash, email, role, status, location_code) VALUES ($1, $1, 'x', $2, $3, 'Active', $4)`, id, id+"@dast.invalid", role, loc); err != nil {
			t.Fatalf("seed %s: %v", role, err)
		}
		t.Cleanup(func() { db.DB.Exec(`DELETE FROM tenant_default.users WHERE id = $1`, id) })
		return id
	}
	f.maker = seed("wf-maker-", engines.RoleSuperAdmin, "HO")
	checker := seed("wf-checker-", engines.RoleSuperAdmin, "HO")
	otherSite := seed("wf-site-", engines.RoleStoreManager, "ZZ")
	engines.ResetLiveUserStateCache()
	f.makerTok = engines.SignToken(f.maker, f.maker, engines.RoleSuperAdmin, "default", "HO", 1)
	f.checkerTok = engines.SignToken(checker, checker, engines.RoleSuperAdmin, "default", "HO", 1)
	f.otherSiteTok = engines.SignToken(otherSite, otherSite, engines.RoleStoreManager, "default", "ZZ", 1)

	for _, q := range []string{
		`INSERT INTO tenant_default.doctype_meta (name, module, document_type) VALUES ('WorkflowDrill', 'Drill', 'Transaction') ON CONFLICT DO NOTHING`,
		`INSERT INTO tenant_default.approval_rules (doctype, min_amount, required_role) VALUES ('WorkflowDrill', 0, 'Store Manager')`,
	} {
		if _, err := db.DB.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		db.DB.Exec(`DELETE FROM tenant_default.approval_log WHERE doctype = 'WorkflowDrill'`)
		db.DB.Exec(`DELETE FROM tenant_default.documents WHERE doctype = 'WorkflowDrill'`)
		db.DB.Exec(`DELETE FROM tenant_default.approval_rules WHERE doctype = 'WorkflowDrill'`)
		db.DB.Exec(`DELETE FROM tenant_default.doctype_meta WHERE name = 'WorkflowDrill'`)
	})
	old := engines.GetSettingString("default", engines.SettingSelfApproval)
	if err := engines.SetSetting("default", engines.SettingSelfApproval, engines.SelfApprovalNobody, "test"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { engines.SetSetting("default", engines.SettingSelfApproval, old, "test") })
	return f
}

func (f *workflowAbuseFixture) draft() string {
	id := "WFD-" + contractUniqueID()
	if _, err := db.DB.Exec(`INSERT INTO tenant_default.documents (id, doctype, data, status, created_by) VALUES ($1, 'WorkflowDrill', $2, 'Draft', $3)`,
		id, `{"total_amount": 500, "location": "HO", "status": "Draft"}`, f.maker); err != nil {
		f.t.Fatal(err)
	}
	return id
}

func (f *workflowAbuseFixture) submitAs(tok, id string) int {
	return doRequest(f.t, f.submit, http.MethodPost, "/api/v1/approval/submit", tok,
		map[string]string{"doctype": "WorkflowDrill", "document_id": id}).Code
}

func (f *workflowAbuseFixture) decideAs(tok, id, decision string) int {
	return doRequest(f.t, f.decide, http.MethodPost, "/api/v1/approval/decide", tok,
		map[string]string{"doctype": "WorkflowDrill", "document_id": id, "decision": decision, "comment": "drill"}).Code
}

func (f *workflowAbuseFixture) state(id string) (status string, logged map[string]int) {
	f.t.Helper()
	if err := db.DB.QueryRow(`SELECT status FROM tenant_default.documents WHERE id = $1`, id).Scan(&status); err != nil {
		f.t.Fatal(err)
	}
	rows, err := db.DB.Query(`SELECT action, count(*) FROM tenant_default.approval_log WHERE document_id = $1 GROUP BY action`, id)
	if err != nil {
		f.t.Fatal(err)
	}
	defer rows.Close()
	logged = map[string]int{}
	for rows.Next() {
		var a string
		var n int
		rows.Scan(&a, &n)
		logged[a] = n
	}
	return
}

func (f *workflowAbuseFixture) expect(id, wantStatus string, wantLog map[string]int, what string) {
	f.t.Helper()
	status, logged := f.state(id)
	if status != wantStatus {
		f.t.Errorf("%s: status %q, want %q", what, status, wantStatus)
	}
	for a, n := range wantLog {
		if logged[a] != n {
			f.t.Errorf("%s: %d %q log rows, want %d (%v)", what, logged[a], a, n, logged)
		}
	}
}

func TestStage49104WorkflowAbuse(t *testing.T) {
	f := newWorkflowAbuseFixture(t)

	t.Run("a decision cannot skip the submit step", func(t *testing.T) {
		id := f.draft()
		if code := f.decideAs(f.checkerTok, id, "Approved"); code == http.StatusOK {
			t.Fatalf("approving a Draft was accepted")
		}
		f.expect(id, "Draft", map[string]int{"Approved": 0}, "decide on a Draft")
	})

	t.Run("the maker cannot decide their own document", func(t *testing.T) {
		id := f.draft()
		f.submitAs(f.makerTok, id)
		if code := f.decideAs(f.makerTok, id, "Approved"); code == http.StatusOK {
			t.Fatalf("self-approval accepted")
		}
		f.expect(id, "Pending Approval", map[string]int{"Approved": 0}, "self approval")
	})

	t.Run("a second approval, a late rejection and a replay change nothing", func(t *testing.T) {
		id := f.draft()
		f.submitAs(f.makerTok, id)
		if code := f.decideAs(f.checkerTok, id, "Approved"); code != http.StatusOK {
			t.Fatalf("the real approval was refused: %d", code)
		}
		for _, d := range []string{"Approved", "Rejected", "Approved"} {
			if code := f.decideAs(f.checkerTok, id, d); code == http.StatusOK {
				t.Errorf("a repeated %s was accepted", d)
			}
		}
		f.expect(id, "Approved", map[string]int{"Approved": 1, "Rejected": 0}, "replay after approval")
	})

	t.Run("an approved document cannot be submitted again", func(t *testing.T) {
		id := f.draft()
		f.submitAs(f.makerTok, id)
		f.decideAs(f.checkerTok, id, "Approved")
		if code := f.submitAs(f.makerTok, id); code == http.StatusOK {
			t.Errorf("submitting an Approved document was accepted")
		}
		f.expect(id, "Approved", map[string]int{"Submitted": 1}, "re-submit")
	})

	t.Run("another site's approver and made-up ids decide nothing", func(t *testing.T) {
		id := f.draft()
		f.submitAs(f.makerTok, id)
		if code := f.decideAs(f.otherSiteTok, id, "Approved"); code == http.StatusOK {
			t.Errorf("a different site's approver decided it")
		}
		for _, bad := range []string{"WFD-does-not-exist", "../x", strings.Repeat("A", 300)} {
			if code := f.decideAs(f.checkerTok, bad, "Approved"); code >= 500 || code == http.StatusOK {
				t.Errorf("decide on %q: status %d", bad, code)
			}
		}
		f.expect(id, "Pending Approval", map[string]int{"Approved": 0}, "other site")
	})

	t.Run("a rejected document cannot be approved afterwards", func(t *testing.T) {
		id := f.draft()
		f.submitAs(f.makerTok, id)
		f.decideAs(f.checkerTok, id, "Rejected")
		if code := f.decideAs(f.checkerTok, id, "Approved"); code == http.StatusOK {
			t.Errorf("a rejected document was approved")
		}
		f.expect(id, "Rejected", map[string]int{"Approved": 0}, "approve after reject")
	})

	t.Run("racing submits and racing decisions leave one outcome", func(t *testing.T) {
		id := f.draft()
		var wg sync.WaitGroup
		for i := 0; i < 6; i++ {
			wg.Add(1)
			go func() { defer wg.Done(); f.submitAs(f.makerTok, id) }()
		}
		wg.Wait()
		f.expect(id, "Pending Approval", map[string]int{"Submitted": 1}, "six racing submits")
		won := make(chan string, 8)
		for i := 0; i < 8; i++ {
			d := []string{"Approved", "Rejected"}[i%2]
			wg.Add(1)
			go func() {
				defer wg.Done()
				if f.decideAs(f.checkerTok, id, d) == http.StatusOK {
					won <- d
				}
			}()
		}
		wg.Wait()
		close(won)
		var winners []string
		for d := range won {
			winners = append(winners, d)
		}
		if len(winners) != 1 {
			t.Fatalf("%d racing decisions were accepted: %v", len(winners), winners)
		}
		status, logged := f.state(id)
		if status != winners[0] || logged["Approved"]+logged["Rejected"] != 1 {
			t.Errorf("status %q, log %v, winner %s", status, logged, winners[0])
		}
	})
}
