package engines

import (
	"custom_erp/db"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// Stage 47.12.1 / 47.12.2 / 47.12.3 / 47.12.7 / 49.17.3: the scope answers, the
// structure graph, and the packs with evidence, waivers and sign-off.

func goLiveTestTenant(t *testing.T) (string, string, func()) {
	t.Helper()
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	return tenantID, schema, func() { dropLifecycleTenant(tenantID, schema) }
}

func goLiveCode(err error) string {
	if ve, ok := err.(*ValidationError); ok {
		return ve.Code
	}
	return ""
}

func TestStage47121ScopeRecommendsOnlySupported(t *testing.T) {
	tenantID, _, done := goLiveTestTenant(t)
	defer done()
	s, err := GetGoLiveScope(tenantID)
	if err != nil || s.Saved {
		t.Fatalf("fresh scope: %+v %v", s, err)
	}
	if _, err := SaveGoLiveScope(tenantID, ScopeAnswers{"industry": {"nonsense"}}, "asha"); goLiveCode(err) != "GLOBAL-9110" {
		t.Fatalf("bad answer: %v, want GLOBAL-9110", err)
	}
	good := ScopeAnswers{"industry": {"retail"}, "countries": {"india"}, "entities": {"one"}, "registrations": {"one"}, "channels": {"counter"},
		"locations": {"stores"}, "roles": {"team"}, "inventory": {"quantity"}, "accounting": {"accrual"}, "devices": {"none"}}
	s, err = SaveGoLiveScope(tenantID, good, "asha")
	if err != nil || !s.Saved || s.Version != 1 || s.SavedBy != "asha" {
		t.Fatalf("save: %+v %v", s, err)
	}
	if len(s.Unsupported) != 0 || !s.Recommended {
		t.Errorf("a supported configuration listed unsupported: %+v", s.Unsupported)
	}
	// a choice no register entry covers is never recommended
	bad := ScopeAnswers{}
	for k, v := range good {
		bad[k] = v
	}
	bad["accounting"] = []string{"cash"}
	s, err = SaveGoLiveScope(tenantID, bad, "ravi")
	if err != nil || s.Version != 2 || s.Recommended || len(s.Unsupported) == 0 {
		t.Fatalf("cash basis: %+v %v, want version 2 with an unsupported choice", s, err)
	}
	// built and tested, not yet run live: Preview, not Unsupported
	for q, v := range map[string]string{"inventory": "serial", "entities": "several", "registrations": "many"} {
		built := ScopeAnswers{}
		for k, vals := range good {
			built[k] = vals
		}
		built[q] = []string{v}
		e, err := EvaluateScope(built)
		if err != nil || !e.Recommended {
			t.Fatalf("%s=%s: %+v %v, want it recommended", q, v, e, err)
		}
		found := false
		for _, c := range e.Choices {
			found = found || (c.Question == q && c.Support == ScopePreview)
		}
		if !found {
			t.Errorf("%s=%s: want it to read Preview: %+v", q, v, e.Choices)
		}
	}
	if len(s.Versions) != 2 {
		t.Errorf("versions: %+v", s.Versions)
	}
}

func TestStage47122GraphReadsMastersAndScopeIsReversible(t *testing.T) {
	tenantID, schema, done := goLiveTestTenant(t)
	defer done()
	exec := func(q string, args ...interface{}) {
		t.Helper()
		if _, err := db.DB.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	exec("DELETE FROM " + schema + ".documents WHERE doctype IN ('LegalEntity', 'Location', 'CostCenter')")
	le, _ := json.Marshal(map[string]interface{}{"name": "Acme", "gstin": "BAD-GSTIN"})
	loc, _ := json.Marshal(map[string]interface{}{"name": "Main", "type": "Store", "legal_entity": "LE1"})
	exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ('LE1', 'LegalEntity', $1, 'Active', 'system')", le)
	exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ('S1', 'Location', $1, 'Active', 'system')", loc)
	g, err := GetGoLiveGraph(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	node := func(g *GoLiveGraph, id string) *GraphNode {
		for i := range g.Nodes {
			if g.Nodes[i].ID == id {
				return &g.Nodes[i]
			}
		}
		return nil
	}
	if n := node(g, "Registration:LE1"); n == nil || n.State != "issue" || !strings.Contains(strings.Join(n.Issues, ","), "GSTIN") {
		t.Fatalf("bad GSTIN node: %+v", n)
	}
	if n := node(g, "Location:S1"); n == nil || n.State != "issue" || !strings.Contains(strings.Join(n.Issues, ","), "finance dimension") {
		t.Fatalf("store without a cost centre: %+v", n)
	}
	var linked bool
	for _, e := range g.Edges {
		linked = linked || (e.From == "LegalEntity:LE1" && e.To == "Location:S1")
	}
	if !linked {
		t.Errorf("no edge from the entity to its location: %+v", g.Edges)
	}
	// out of scope needs a reason; it is recorded, reversible, and the master stays
	if err := SetGoLiveNodeScope(tenantID, "Location:S1", true, "", "asha"); goLiveCode(err) != "GLOBAL-9116" {
		t.Fatalf("no reason: %v", err)
	}
	if err := SetGoLiveNodeScope(tenantID, "Location:NOPE", true, "closed shop", "asha"); goLiveCode(err) != "GLOBAL-9116" {
		t.Fatalf("missing record: %v", err)
	}
	if err := SetGoLiveNodeScope(tenantID, "Location:S1", true, "closed shop", "asha"); err != nil {
		t.Fatal(err)
	}
	g, _ = GetGoLiveGraph(tenantID)
	if n := node(g, "Location:S1"); n.State != "out" || n.OutBy != "asha" || n.Reason != "closed shop" {
		t.Fatalf("out of scope: %+v", n)
	}
	if err := SetGoLiveNodeScope(tenantID, "Location:S1", false, "", "ravi"); err != nil {
		t.Fatal(err)
	}
	g, _ = GetGoLiveGraph(tenantID)
	if n := node(g, "Location:S1"); n.State == "out" {
		t.Errorf("not brought back: %+v", n)
	}
	var n int
	_ = db.DB.QueryRow("SELECT COUNT(*) FROM " + schema + ".documents WHERE doctype = 'Location' AND deleted_at IS NULL").Scan(&n)
	if n != 1 {
		t.Errorf("the master changed: %d", n)
	}
}

func goLiveLineByIDIn(p *GoLivePack, id string) *GoLiveLine {
	for i := range p.Lines {
		if p.Lines[i].ID == id {
			return &p.Lines[i]
		}
	}
	return nil
}

func TestStage49173PacksNeverPassWithoutEvidenceAndSignOffRules(t *testing.T) {
	tenantID, _, done := goLiveTestTenant(t)
	defer done()
	p, err := GetGoLivePack(tenantID, PackGoLive)
	if err != nil {
		t.Fatal(err)
	}
	// things only a person can prove are Not evidenced, never Pass
	for _, id := range []string{"customer-acceptance", "incident-contacts", "rto-rpo", "hardened-deploy", "scope-saved"} {
		if l := goLiveLineByIDIn(p, id); l == nil || l.State != LineNotEvidenced {
			t.Fatalf("%s: %+v, want Not evidenced", id, l)
		}
	}
	if _, err := GetGoLivePack(tenantID, "nope"); err == nil {
		t.Errorf("an unknown pack should be refused")
	}
	// evidence: only on a line a person can prove; needs a note or a reference
	if err := AttachGoLiveEvidence(tenantID, "scope-saved", "x", "", "asha"); goLiveCode(err) != "GLOBAL-9111" {
		t.Errorf("evidence on a live-data line: %v", err)
	}
	if err := AttachGoLiveEvidence(tenantID, "incident-contacts", " ", "", "asha"); goLiveCode(err) != "GLOBAL-9112" {
		t.Errorf("empty evidence: %v", err)
	}
	if err := AttachGoLiveEvidence(tenantID, "incident-contacts", "Ops lead Ravi", "rota.pdf", "asha"); err != nil {
		t.Fatal(err)
	}
	p, _ = GetGoLivePack(tenantID, PackGoLive)
	if l := goLiveLineByIDIn(p, "incident-contacts"); l.State != LinePass || l.Evidence == nil || l.Evidence.By != "asha" || l.Evidence.At == "" {
		t.Fatalf("after evidence: %+v", l)
	}
	// a waiver needs a reason and a future date within a year
	tomorrow := time.Now().AddDate(0, 0, 1).Format("2006-01-02")
	if err := WaiveGoLiveLine(tenantID, "rto-rpo", "", tomorrow, "asha"); goLiveCode(err) != "GLOBAL-9113" {
		t.Errorf("no reason: %v", err)
	}
	if err := WaiveGoLiveLine(tenantID, "rto-rpo", "agreed later", time.Now().AddDate(0, 0, -1).Format("2006-01-02"), "asha"); goLiveCode(err) != "GLOBAL-9113" {
		t.Errorf("past expiry: %v", err)
	}
	if err := WaiveGoLiveLine(tenantID, "rto-rpo", "agreed later", time.Now().AddDate(2, 0, 0).Format("2006-01-02"), "asha"); goLiveCode(err) != "GLOBAL-9113" {
		t.Errorf("expiry beyond a year: %v", err)
	}
	if err := WaiveGoLiveLine(tenantID, "incident-contacts", "not needed", tomorrow, "asha"); goLiveCode(err) != "GLOBAL-9113" {
		t.Errorf("waiving a Pass line: %v", err)
	}
	// sign-off is refused while a blocking line is open
	if _, err := SignOffGoLivePack(tenantID, PackGoLive, "", "ravi"); goLiveCode(err) != "GLOBAL-9114" {
		t.Fatalf("sign-off with open lines: %v", err)
	}
	// asha waives every open line, including the Critical ones
	for _, l := range p.Lines {
		if goLiveBlocks(l) {
			if err := WaiveGoLiveLine(tenantID, l.ID, "accepted for the pilot", tomorrow, "asha"); err != nil {
				t.Fatalf("waive %s: %v", l.ID, err)
			}
		}
	}
	p, _ = GetGoLivePack(tenantID, PackGoLive)
	if p.Open != 0 || !p.CanSign {
		t.Fatalf("after waivers: open %d", p.Open)
	}
	// the one who waived a Critical line cannot sign
	if _, err := SignOffGoLivePack(tenantID, PackGoLive, "", "asha"); goLiveCode(err) != "GLOBAL-9115" {
		t.Fatalf("self sign-off: %v", err)
	}
	signed, err := SignOffGoLivePack(tenantID, PackGoLive, "go", "ravi")
	if err != nil || signed.SignOff == nil || signed.SignOff.By != "ravi" || signed.SignOff.At == "" {
		t.Fatalf("sign-off by another: %+v %v", signed, err)
	}
	// a waiver can be ended; the line is open again
	if err := EndGoLiveWaiver(tenantID, "rto-rpo", "ravi"); err != nil {
		t.Fatal(err)
	}
	p, _ = GetGoLivePack(tenantID, PackGoLive)
	if l := goLiveLineByIDIn(p, "rto-rpo"); l.State != LineNotEvidenced {
		t.Errorf("after ending: %+v", l)
	}
	if signed.SignOff.Reopened || p.SignOff == nil || !p.SignOff.Reopened {
		t.Errorf("a sign-off with a check open again must read reopened: at signing %+v, now %+v", signed.SignOff, p.SignOff)
	}
}

// The person who attached the evidence that makes a Critical line pass cannot
// also sign the pack; a waiver's expiry must be after the business day.
func TestStage49173SignerCannotEvidenceTheirOwnCriticalLine(t *testing.T) {
	tenantID, _, done := goLiveTestTenant(t)
	defer done()
	if err := WaiveGoLiveLine(tenantID, "rto-rpo", "agreed later", BusinessToday(), "asha"); goLiveCode(err) != "GLOBAL-9113" {
		t.Errorf("a waiver ending today must be refused: %v", err)
	}
	if err := AttachGoLiveEvidence(tenantID, "hardened-deploy", "Drift run clean", "run-7", "asha"); err != nil {
		t.Fatal(err)
	}
	p, err := GetGoLivePack(tenantID, PackGoLive)
	if err != nil {
		t.Fatal(err)
	}
	if l := goLiveLineByIDIn(p, "hardened-deploy"); l == nil || l.Severity != LineCritical || l.State != LinePass {
		t.Fatalf("the line under test must be a passing Critical line: %+v", l)
	}
	tomorrow := time.Now().AddDate(0, 0, 2).Format("2006-01-02")
	for _, l := range p.Lines {
		if goLiveBlocks(l) {
			if err := WaiveGoLiveLine(tenantID, l.ID, "accepted for the pilot", tomorrow, "ravi"); err != nil {
				t.Fatalf("waive %s: %v", l.ID, err)
			}
		}
	}
	_, err = SignOffGoLivePack(tenantID, PackGoLive, "", "ASHA")
	if goLiveCode(err) != "GLOBAL-9115" || !strings.Contains(err.Error(), "attached the evidence") {
		t.Fatalf("the evidencer signing: %v", err)
	}
	if s, err := SignOffGoLivePack(tenantID, PackGoLive, "go", "meera"); err != nil || s.SignOff == nil || s.SignOff.By != "meera" {
		t.Fatalf("a third person signs: %+v %v", s, err)
	}
}

// A saved answer that can no longer be evaluated must not read as supported.
func TestStage47121RetiredScopeAnswerIsNotAPass(t *testing.T) {
	tenantID, schema, done := goLiveTestTenant(t)
	defer done()
	if err := insertGoLiveRecord(schema, "Scope", "", "", "", "", "", ScopeAnswers{"retired-question": {"x"}}, "asha"); err != nil {
		t.Fatal(err)
	}
	p, err := GetGoLivePack(tenantID, PackGoLive)
	if err != nil {
		t.Fatal(err)
	}
	if l := goLiveLineByIDIn(p, "capability-level"); l == nil || l.State != LineNotEvidenced {
		t.Fatalf("capability line on an unevaluated scope: %+v", l)
	}
}

// The counts are the states the nodes end up in.
func TestStage47122GraphCountsAreFinalStates(t *testing.T) {
	tenantID, s, done := goLiveTestTenant(t)
	defer done()
	if _, err := SaveGoLiveScope(tenantID, ScopeAnswers{"industry": {"retail"}, "countries": {"india"}, "entities": {"one"}, "registrations": {"one"}, "channels": {"counter"},
		"locations": {"stores"}, "roles": {"team"}, "inventory": {"quantity"}, "accounting": {"accrual"}, "devices": {"none"}}, "asha"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"LE-A", "LE-B"} {
		if _, err := db.DB.Exec("INSERT INTO "+s+".documents (id, doctype, data, status, created_by) VALUES ($1, 'LegalEntity', $2, 'Active', 'asha')", id,
			`{"code":"`+id+`","legal_name":"`+id+`","status":"Active"}`); err != nil {
			t.Fatal(err)
		}
	}
	g, err := GetGoLiveGraph(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{}
	issues := 0
	for _, n := range g.Nodes {
		want[n.State]++
		if n.Kind == "entity" && n.State == "issue" {
			issues++
		}
	}
	if issues != 2 {
		t.Fatalf("two entities under a one-entity scope should both show an issue, got %d: %+v", issues, g.Nodes)
	}
	for state, n := range want {
		if g.Counts[state] != n {
			t.Errorf("counts[%s] = %d, nodes say %d (%v)", state, g.Counts[state], n, g.Counts)
		}
	}
}

func TestStage47123FinanceReconciliationAcceptanceNeedsATiedBook(t *testing.T) {
	tenantID, schema, done := goLiveTestTenant(t)
	defer done()
	p, err := GetGoLivePack(tenantID, PackFinance)
	if err != nil {
		t.Fatal(err)
	}
	if l := goLiveLineByIDIn(p, "fin-recon-accepted"); l.State != LineNotEvidenced {
		t.Fatalf("acceptance before anyone accepts: %+v", l)
	}
	if err := AttachGoLiveEvidence(tenantID, "fin-recon-accepted", "Books agree with stock count", "TB-2026-11", "meena"); err != nil {
		t.Fatalf("accept on tied books: %v", err)
	}
	p, _ = GetGoLivePack(tenantID, PackFinance)
	if l := goLiveLineByIDIn(p, "fin-recon-accepted"); l.State != LinePass || !strings.Contains(l.Detail, "meena") {
		t.Fatalf("accepted line: %+v", l)
	}
	// an unbalanced entry makes the books not tie: the line and the tie fail
	if _, err := db.DB.Exec("INSERT INTO " + schema + ".gl_postings (account_code, debit, credit, document_type, document_id) VALUES ('1100', 500, 0, 'TEST', 'X1')"); err != nil {
		t.Skipf("cannot seed an unbalanced posting here: %v", err)
	}
	p, _ = GetGoLivePack(tenantID, PackFinance)
	if l := goLiveLineByIDIn(p, "fin-recon-tied"); l.State != LineFail {
		t.Fatalf("tie with an unbalanced posting: %+v", l)
	}
	if l := goLiveLineByIDIn(p, "fin-recon-accepted"); l.State != LineFail {
		t.Errorf("acceptance should fail once the books no longer tie: %+v", l)
	}
	if err := AttachGoLiveEvidence(tenantID, "fin-recon-accepted", "again", "", "meena"); goLiveCode(err) != "GLOBAL-9117" {
		t.Errorf("accepting untied books: %v", err)
	}
}
