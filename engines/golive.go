package engines

import (
	"context"
	"crypto/sha256"
	"custom_erp/db"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Stage 47.12.3 / 47.12.7 / 49.17.3 - the finance start, the cutover pack and
// the go-live gate.
//
// One engine, three packs. A pack is a list of lines; a line is a check with
// a state: Pass, Fail, Not evidenced, Waived or Not applicable. A line is read
// from real data where the ERP holds the data (accounting periods, the chart,
// the control totals, the SoD catalog, credentials, the host's backup log) and
// is Not evidenced where only a person or the host can prove it and nothing
// was recorded - never Pass on a guess. A person can attach evidence to a line
// that is theirs to prove (a note and a reference, recorded with who and when),
// or waive a failing one (a reason and an expiry). Sign-off records the named
// person and is refused while a Critical or Blocking line is Fail or Not
// evidenced, and a Critical line cannot be waived by the one who signs.
//
// It is a record and a sign-off. Nothing else in the ERP reads it, so it
// blocks no sale, receipt or posting.

// Line states.
const (
	LinePass         = "Pass"
	LineFail         = "Fail"
	LineNotEvidenced = "Not evidenced"
	LineWaived       = "Waived"
	LineNotApplic    = "Not applicable"
)

// Line severities: Critical and Blocking keep a pack from being signed;
// Warning does not. Critical cannot be waived by the signer.
const (
	LineCritical = "Critical"
	LineBlocking = "Blocking"
	LineWarning  = "Warning"
)

// Packs.
const (
	PackFinance = "finance"
	PackCutover = "cutover"
	PackGoLive  = "golive"
)

var goLivePackTitles = map[string]string{PackFinance: "Finance start", PackCutover: "Cutover", PackGoLive: "Go-live gate"}

const (
	maxWaiverDays  = 365
	backupFreshDay = 7
	drillFreshDay  = 35
)

func goLiveErr(code, msg string) error { return &ValidationError{Code: code, Message: msg} }

// lineResult is a check's own answer, before evidence and waivers.
type lineResult struct{ State, Detail, Source string }

func pass(source, format string, a ...interface{}) lineResult {
	return lineResult{LinePass, fmt.Sprintf(format, a...), source}
}
func fail(source, format string, a ...interface{}) lineResult {
	return lineResult{LineFail, fmt.Sprintf(format, a...), source}
}
func notEvidenced(source, format string, a ...interface{}) lineResult {
	return lineResult{LineNotEvidenced, fmt.Sprintf(format, a...), source}
}
func notApplicable(source, format string, a ...interface{}) lineResult {
	return lineResult{LineNotApplic, fmt.Sprintf(format, a...), source}
}

type goLiveLineDef struct {
	ID       string
	Label    string
	Packs    []string
	Severity string
	Manual   bool   // a person can attach evidence
	Fix      string // a view for renderView, or doctype:<Name>
	check    func(*goLiveCtx) lineResult
	// stamp, when set, fingerprints what the evidence is about; evidence
	// attached under a different stamp is stale (49.17.3).
	stamp func(*goLiveCtx) (string, error)
}

// GoLiveEvidence is what a person attached to a line.
type GoLiveEvidence struct {
	Note      string `json:"note"`
	Reference string `json:"reference"`
	By        string `json:"by"`
	At        string `json:"at"`
	Stale     bool   `json:"stale,omitempty"` // what it covered has changed since
}

// GoLiveWaiver is a line's waiver: why, until when, and who.
type GoLiveWaiver struct {
	Reason    string `json:"reason"`
	ExpiresOn string `json:"expires_on"`
	By        string `json:"by"`
	At        string `json:"at"`
	Active    bool   `json:"active"`
}

// GoLiveLine is a line as the screen shows it.
type GoLiveLine struct {
	ID       string          `json:"id"`
	Label    string          `json:"label"`
	Severity string          `json:"severity"`
	State    string          `json:"state"`
	Detail   string          `json:"detail"`
	Source   string          `json:"source"`
	Fix      string          `json:"fix,omitempty"`
	Manual   bool            `json:"manual"`
	Raw      string          `json:"raw_state"` // the check's own answer, before evidence or a waiver
	Evidence *GoLiveEvidence `json:"evidence,omitempty"`
	Waiver   *GoLiveWaiver   `json:"waiver,omitempty"`
}

// GoLiveSignOff is a pack's latest sign-off.
type GoLiveSignOff struct {
	By       string `json:"by"`
	At       string `json:"at"`
	Note     string `json:"note"`
	Reopened bool   `json:"reopened,omitempty"` // a check has opened since it was signed
}

// GoLivePack is one pack with its lines, counts and sign-off.
type GoLivePack struct {
	Pack    string         `json:"pack"`
	Title   string         `json:"title"`
	Lines   []GoLiveLine   `json:"lines"`
	Counts  map[string]int `json:"counts"`
	Open    int            `json:"open"` // Critical and Blocking lines Fail or Not evidenced
	SignOff *GoLiveSignOff `json:"sign_off,omitempty"`
	CanSign bool           `json:"can_sign"`
}

type goLiveRecord struct {
	ID                                             int64
	Kind, Pack, LineID, Note, Reference, ExpiresOn string
	Data, By, At                                   string
}

// goLiveCtx carries one pack read: the shared inputs are loaded once.
type goLiveCtx struct {
	tenantID, schema string
	today            time.Time
	scope            *GoLiveScope
	recs             map[string][]goLiveRecord // by line id, newest first
	steps            []SetupPathStep
	stepsErr         error
	stepsDone        bool
	raw              map[string]lineResult
	ties             *goLiveTies
}

func (c *goLiveCtx) todayStr() string { return c.today.Format("2006-01-02") }

func (c *goLiveCtx) setupSteps() ([]SetupPathStep, error) {
	if !c.stepsDone {
		c.steps, c.stepsErr = GetSetupPath(c.tenantID)
		c.stepsDone = true
	}
	return c.steps, c.stepsErr
}

func newGoLiveCtx(tenantID string) (*goLiveCtx, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	scope, err := GetGoLiveScope(tenantID)
	if err != nil {
		return nil, err
	}
	c := &goLiveCtx{tenantID: tenantID, schema: schema, today: time.Now(), scope: scope, raw: map[string]lineResult{}}
	c.recs, err = loadGoLiveRecords(schema)
	return c, err
}

func loadGoLiveRecords(schema string) (map[string][]goLiveRecord, error) {
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, kind, pack, line_id, note, reference, COALESCE(expires_on::text, ''), data::text, created_by,
		to_char(created_at, 'YYYY-MM-DD HH24:MI') FROM %s.golive_records WHERE kind <> 'Scope' ORDER BY id DESC LIMIT 5000`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]goLiveRecord{}
	for rows.Next() {
		var r goLiveRecord
		if err := rows.Scan(&r.ID, &r.Kind, &r.Pack, &r.LineID, &r.Note, &r.Reference, &r.ExpiresOn, &r.Data, &r.By, &r.At); err != nil {
			return nil, err
		}
		out[r.LineID] = append(out[r.LineID], r)
	}
	return out, rows.Err()
}

func insertGoLiveRecord(schema, kind, pack, lineID, note, ref, expires string, data interface{}, actor string) error {
	raw, _ := json.Marshal(data)
	var exp interface{}
	if expires != "" {
		exp = expires
	}
	defer InvalidateSetupFinanceChecks(schema) // 164.2
	_, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.golive_records (kind, pack, line_id, note, reference, expires_on, data, created_by)
		VALUES ($1, $2, $3, $4, $5, $6::date, $7::jsonb, $8)`, schema), kind, pack, lineID, note, ref, exp, string(raw), actor)
	return err
}

// latest returns the newest record of one of the kinds on a line.
func (c *goLiveCtx) latest(lineID string, kinds ...string) *goLiveRecord {
	for i := range c.recs[lineID] {
		r := &c.recs[lineID][i]
		for _, k := range kinds {
			if r.Kind == k {
				return r
			}
		}
	}
	return nil
}

// ---- the lines --------------------------------------------------------

var goLiveLines []goLiveLineDef

func goLiveLineByID(id string) (goLiveLineDef, bool) {
	for _, l := range goLiveLines {
		if l.ID == id {
			return l, true
		}
	}
	return goLiveLineDef{}, false
}

func (d goLiveLineDef) inPack(p string) bool {
	for _, x := range d.Packs {
		if x == p {
			return true
		}
	}
	return false
}

// rawLine runs (once) a line's own check.
func (c *goLiveCtx) rawLine(id string) lineResult {
	if r, ok := c.raw[id]; ok {
		return r
	}
	d, ok := goLiveLineByID(id)
	if !ok {
		return lineResult{LineNotEvidenced, "unknown line", ""}
	}
	r := d.check(c)
	c.raw[id] = r
	return r
}

// resolve folds evidence and waiver into the line.
func (c *goLiveCtx) resolve(d goLiveLineDef) GoLiveLine {
	raw := c.rawLine(d.ID)
	l := GoLiveLine{ID: d.ID, Label: d.Label, Severity: d.Severity, State: raw.State, Detail: raw.Detail, Source: raw.Source, Fix: d.Fix, Manual: d.Manual, Raw: raw.State}
	if ev := c.latest(d.ID, "Evidence"); ev != nil && d.Manual {
		l.Evidence = &GoLiveEvidence{Note: ev.Note, Reference: ev.Reference, By: ev.By, At: ev.At}
		l.Evidence.Stale = c.evidenceStale(d, ev)
		if raw.State == LineNotEvidenced {
			if l.Evidence.Stale {
				l.Detail = strings.TrimSpace(l.Detail + " The evidence from " + ev.At + " is stale: the destinations changed since. Attach it again.")
			} else {
				l.State, l.Source = LinePass, "attached by "+ev.By
				l.Detail = strings.TrimSpace(ev.Reference + " " + ev.Note)
			}
		}
	}
	if w := c.latest(d.ID, "Waiver", "WaiverEnd"); w != nil && w.Kind == "Waiver" {
		active := w.ExpiresOn >= c.todayStr()
		l.Waiver = &GoLiveWaiver{Reason: w.Note, ExpiresOn: w.ExpiresOn, By: w.By, At: w.At, Active: active}
		if active && (l.State == LineFail || l.State == LineNotEvidenced) {
			l.State = LineWaived
		}
	}
	return l
}

func goLiveBlocks(l GoLiveLine) bool {
	return (l.Severity == LineCritical || l.Severity == LineBlocking) && (l.State == LineFail || l.State == LineNotEvidenced)
}

// GetGoLivePack reads one pack: every line, its counts and its sign-off.
func GetGoLivePack(tenantID, pack string) (*GoLivePack, error) {
	title, ok := goLivePackTitles[pack]
	if !ok {
		return nil, goLiveErr("GLOBAL-0004", fmt.Sprintf("pack %q not found", pack))
	}
	c, err := newGoLiveCtx(tenantID)
	if err != nil {
		return nil, err
	}
	out := &GoLivePack{Pack: pack, Title: title, Lines: []GoLiveLine{}, Counts: map[string]int{}}
	for _, d := range goLiveLines {
		if !d.inPack(pack) {
			continue
		}
		l := c.resolve(d)
		out.Lines = append(out.Lines, l)
		out.Counts[l.State]++
		if goLiveBlocks(l) {
			out.Open++
		}
	}
	out.CanSign = out.Open == 0
	if so := c.latest("signoff:"+pack, "SignOff"); so != nil {
		out.SignOff = &GoLiveSignOff{By: so.By, At: so.At, Note: so.Note, Reopened: out.Open > 0}
	}
	return out, nil
}

func trimLen(s string, max int) (string, bool) {
	s = strings.TrimSpace(s)
	return s, len(s) <= max
}

// AttachGoLiveEvidence records a note and a reference against a line a person
// can prove. A line read from live data takes no evidence: fix the data or
// waive it.
func AttachGoLiveEvidence(tenantID, lineID, note, reference, actor string) error {
	d, ok := goLiveLineByID(lineID)
	if !ok {
		return goLiveErr("GLOBAL-9111", fmt.Sprintf("Unknown check %q", lineID))
	}
	if !d.Manual {
		return goLiveErr("GLOBAL-9111", "This check is read from live data. Fix the data, or waive it.")
	}
	note, ok1 := trimLen(note, 2000)
	reference, ok2 := trimLen(reference, 500)
	if note == "" && reference == "" {
		return goLiveErr("GLOBAL-9112", "Add a note or a reference.")
	}
	if !ok1 || !ok2 {
		return goLiveErr("GLOBAL-9112", "The note or reference is too long.")
	}
	c, err := newGoLiveCtx(tenantID)
	if err != nil {
		return err
	}
	if lineID == "fin-recon-accepted" {
		if r := c.rawLine("fin-recon-tied"); r.State != LinePass {
			return goLiveErr("GLOBAL-9117", "The books do not tie yet: "+r.Detail)
		}
	}
	var data interface{}
	if d.stamp != nil {
		st, err := d.stamp(c)
		if err != nil {
			return err
		}
		data = map[string]string{"stamp": st}
	}
	return insertGoLiveRecord(c.schema, "Evidence", firstPack(d), lineID, note, reference, "", data, actor)
}

// evidenceStale is true when the line fingerprints what its evidence covers and
// that has changed since the evidence was attached. Evidence recorded before
// the stamp existed carries none and is honoured.
func (c *goLiveCtx) evidenceStale(d goLiveLineDef, ev *goLiveRecord) bool {
	if d.stamp == nil {
		return false
	}
	var rec struct {
		Stamp string `json:"stamp"`
	}
	if json.Unmarshal([]byte(ev.Data), &rec) != nil || rec.Stamp == "" {
		return false
	}
	now, err := d.stamp(c)
	return err != nil || now != rec.Stamp
}

func firstPack(d goLiveLineDef) string { return d.Packs[0] }

// WaiveGoLiveLine waives a line that is failing or not evidenced: a reason and
// an expiry within a year are required.
func WaiveGoLiveLine(tenantID, lineID, reason, expiresOn, actor string) error {
	d, ok := goLiveLineByID(lineID)
	if !ok {
		return goLiveErr("GLOBAL-9111", fmt.Sprintf("Unknown check %q", lineID))
	}
	reason, okLen := trimLen(reason, 1000)
	if len(reason) < 5 || !okLen {
		return goLiveErr("GLOBAL-9113", "Give the reason, in a few words to 1000 characters.")
	}
	c, err := newGoLiveCtx(tenantID)
	if err != nil {
		return err
	}
	exp, perr := time.Parse("2006-01-02", strings.TrimSpace(expiresOn))
	if perr != nil || exp.Format("2006-01-02") <= c.todayStr() || exp.After(c.today.AddDate(0, 0, maxWaiverDays)) {
		return goLiveErr("GLOBAL-9113", fmt.Sprintf("Give an expiry date after today and within %d days.", maxWaiverDays))
	}
	l := c.resolve(d)
	if l.State != LineFail && l.State != LineNotEvidenced {
		return goLiveErr("GLOBAL-9113", "Only a failing or not evidenced check can be waived.")
	}
	return insertGoLiveRecord(c.schema, "Waiver", firstPack(d), lineID, reason, "", exp.Format("2006-01-02"), nil, actor)
}

// EndGoLiveWaiver withdraws a waiver before it expires.
func EndGoLiveWaiver(tenantID, lineID, actor string) error {
	d, ok := goLiveLineByID(lineID)
	if !ok {
		return goLiveErr("GLOBAL-9111", fmt.Sprintf("Unknown check %q", lineID))
	}
	c, err := newGoLiveCtx(tenantID)
	if err != nil {
		return err
	}
	if w := c.latest(lineID, "Waiver", "WaiverEnd"); w == nil || w.Kind != "Waiver" {
		return goLiveErr("GLOBAL-9113", "This check has no waiver.")
	}
	return insertGoLiveRecord(c.schema, "WaiverEnd", firstPack(d), lineID, "", "", "", nil, actor)
}

// SignOffGoLivePack records the named person's sign-off. It is refused while a
// Critical or Blocking line is Fail or Not evidenced, and a Critical line
// waived by the signer does not count as waived.
func SignOffGoLivePack(tenantID, pack, note, actor string) (*GoLivePack, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	// Stage 47.12.7: one sign-off at a time per pack. The lock is held until
	// the sign-off commits, and the pack is read again under it, so of two
	// people signing at once the second sees the first one's record and is
	// refused (GLOBAL-9118) instead of landing a second sign-off.
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext($1))`, "golive-signoff:"+schema+":"+pack); err != nil {
		return nil, err
	}
	p, err := GetGoLivePack(tenantID, pack)
	if err != nil {
		return nil, err
	}
	if p.SignOff != nil && !p.SignOff.Reopened {
		return nil, goLiveErr("GLOBAL-9118", fmt.Sprintf("Already signed by %s at %s.", p.SignOff.By, p.SignOff.At))
	}
	var open, self, selfEv []string
	snapshot := []map[string]string{}
	for _, l := range p.Lines {
		snapshot = append(snapshot, map[string]string{"id": l.ID, "state": l.State})
		if goLiveBlocks(l) {
			open = append(open, l.Label)
		}
		if l.State == LineWaived && l.Severity == LineCritical && l.Waiver != nil && strings.EqualFold(l.Waiver.By, actor) {
			self = append(self, l.Label)
		}
		if l.State == LinePass && l.Severity == LineCritical && l.Manual && l.Raw == LineNotEvidenced && l.Evidence != nil && strings.EqualFold(l.Evidence.By, actor) {
			selfEv = append(selfEv, l.Label)
		}
	}
	if len(open) > 0 {
		return nil, goLiveErr("GLOBAL-9114", fmt.Sprintf("%d check(s) are still open: %s", len(open), setupNamed(open)))
	}
	if len(self) > 0 {
		return nil, goLiveErr("GLOBAL-9115", "You waived "+setupNamed(self)+". Another person must sign.")
	}
	if len(selfEv) > 0 {
		return nil, goLiveErr("GLOBAL-9115", "You attached the evidence for "+setupNamed(selfEv)+". Another person must sign.")
	}
	note, _ = trimLen(note, 2000)
	raw, _ := json.Marshal(map[string]interface{}{"lines": snapshot})
	if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.golive_records (kind, pack, line_id, note, reference, expires_on, data, created_by)
		VALUES ('SignOff', $1, $2, $3, '', NULL, $4::jsonb, $5)`, schema), pack, "signoff:"+pack, note, string(raw), actor); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	InvalidateSetupFinanceChecks(schema) // 164.2
	return GetGoLivePack(tenantID, pack)
}

// ---- shared reads -----------------------------------------------------

func (c *goLiveCtx) scopeHas(q, v string) bool {
	return c.scope != nil && c.scope.Saved && c.scope.Answers.Has(q, v)
}

// goLiveTies is the three subledger-to-ledger ties plus the trial balance.
type goLiveTies struct {
	ok     bool
	detail string
	err    error
}

func (c *goLiveCtx) subledgerTies() *goLiveTies {
	if c.ties != nil {
		return c.ties
	}
	t := &goLiveTies{ok: true}
	c.ties = t
	var bad []string
	for _, r := range [][2]string{{"inventory-valuation", "Inventory"}, {"receivables-ageing", "Receivables"}, {"payables-ageing", "Payables"}} {
		_, _, _, totals, err := RunReportWithControlTotals(context.Background(), c.tenantID, r[0], RoleSuperAdmin, "system", map[string]string{})
		if err != nil || totals == nil {
			t.err = fmt.Errorf("%s: could not be totalled", r[1])
			if err != nil {
				t.err = fmt.Errorf("%s: %v", r[1], err)
			}
			return t
		}
		if !totals.Tied {
			bad = append(bad, fmt.Sprintf("%s (%s)", r[1], ledgerDiffSummary(totals)))
		}
	}
	tb, err := GetTrialBalance(c.tenantID, c.todayStr())
	if err != nil {
		t.err = fmt.Errorf("trial balance: %v", err)
		return t
	}
	if b, _ := tb["balanced"].(bool); !b {
		bad = append(bad, "Trial balance (debits and credits differ)")
	}
	if len(bad) > 0 {
		t.ok, t.detail = false, strings.Join(bad, "; ")
	}
	return t
}

func ledgerDiffSummary(t *ReportControlTotals) string {
	if t.SummaryVsLedger != nil && *t.SummaryVsLedger != 0 {
		return fmt.Sprintf("books %.2f, ledger differs by %.2f", t.SummaryTotal, *t.SummaryVsLedger)
	}
	return fmt.Sprintf("detail differs from summary by %.2f", t.DetailVsSummary)
}

func (c *goLiveCtx) count(q string, args ...interface{}) (int, error) {
	var n int
	err := db.DB.QueryRow(fmt.Sprintf(q, c.schema), args...).Scan(&n)
	return n, err
}

func (c *goLiveCtx) strings(q string, args ...interface{}) ([]string, error) {
	rows, err := db.DB.Query(fmt.Sprintf(q, c.schema), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func errLine(source string, err error) lineResult {
	return notEvidenced(source, "Could not be read: %v", err)
}

// opsRun is the host's record of a backup or restore drill.
type opsRun struct {
	Finished time.Time
	Started  time.Time
	Env      string
}

func lastOpsRun(runType string) (*opsRun, error) {
	var r opsRun
	err := db.DB.QueryRow(`SELECT finished_at, started_at, environment FROM public.ops_run_log
		WHERE run_type = $1 AND status = 'success' ORDER BY finished_at DESC LIMIT 1`, runType).Scan(&r.Finished, &r.Started, &r.Env)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// manual is the check of a line only a person or the host can prove.
func manualLine(source, what string) func(*goLiveCtx) lineResult {
	return func(*goLiveCtx) lineResult {
		return notEvidenced(source, "%s. Attach it.", what)
	}
}

func init() {
	fin, cut, live := PackFinance, PackCutover, PackGoLive
	goLiveLines = []goLiveLineDef{
		// ---- 47.12.3 finance foundation -------------------------------
		{ID: "fin-calendar", Label: "Fiscal calendar: a period is open today", Packs: []string{fin}, Severity: LineBlocking, Fix: "finance", check: checkFinCalendar},
		{ID: "fin-currency", Label: "Currency: INR is active", Packs: []string{fin}, Severity: LineBlocking, Fix: "doctype:Currency", check: checkFinCurrency},
		{ID: "fin-chart", Label: "Chart of accounts: the posting accounts exist", Packs: []string{fin}, Severity: LineBlocking, Fix: "finance", check: checkFinChart},
		{ID: "fin-series", Label: "Invoice series: tax documents numbered", Packs: []string{fin}, Severity: LineBlocking, Fix: "prefix-configs", check: checkFinSeries},
		{ID: "fin-bank", Label: "Banks and cash: a bank account posts to the ledger", Packs: []string{fin}, Severity: LineBlocking, Fix: "doctype:BankAccount", check: checkFinBank},
		{ID: "fin-opening", Label: "Opening date and balances", Packs: []string{fin, cut}, Severity: LineBlocking, Manual: true, Fix: "journal-vouchers", check: checkFinOpening},
		{ID: "fin-recon-tied", Label: "Stock, receivables and payables tie to the ledger", Packs: []string{fin, cut}, Severity: LineCritical, Fix: "finance", check: checkReconTied},
		{ID: "fin-recon-accepted", Label: "Opening reconciliation accepted by a named person", Packs: []string{fin, cut}, Severity: LineBlocking, Manual: true, Fix: "finance", check: checkReconAccepted},
		// ---- 47.12.7 cutover ------------------------------------------
		{ID: "cut-import-totals", Label: "Migration control totals tie to what was loaded", Packs: []string{cut}, Severity: LineBlocking, Fix: "doctype:ImportBatch", check: checkCutImportTotals},
		{ID: "cut-exceptions", Label: "No open exceptions", Packs: []string{cut}, Severity: LineBlocking, Fix: "configuration", check: checkCutExceptions},
		{ID: "backup-restore", Label: "Backup and restore drill proven by the host", Packs: []string{cut, live}, Severity: LineCritical, Fix: "system-status", check: checkBackupRestore},
		{ID: "sod-review", Label: "Roles and separation of duties reviewed", Packs: []string{cut, live}, Severity: LineBlocking, Fix: "roles", check: checkSoD},
		{ID: "integration-proof", Label: "Connectors proven in use", Packs: []string{cut, live}, Severity: LineBlocking, Manual: true, Fix: "configuration:credentials", check: checkIntegrationProof, stamp: destinationStamp},
		{ID: "device-proof", Label: "Devices proven on site", Packs: []string{cut, live}, Severity: LineBlocking, Manual: true, check: checkDeviceProof},
		// ---- 49.17.3 go-live gate -------------------------------------
		{ID: "scope-saved", Label: "Scope answered", Packs: []string{live}, Severity: LineBlocking, Fix: "configuration:Go-live scope", check: checkScopeSaved},
		{ID: "capability-level", Label: "Every chosen capability is supported", Packs: []string{live}, Severity: LineBlocking, Fix: "configuration:Go-live scope", check: checkCapabilityLevel},
		{ID: "hardened-deploy", Label: "Hardened deployment and drift result", Packs: []string{live}, Severity: LineCritical, Manual: true, check: manualLine("host", "The host's deployment check result is not recorded here")},
		{ID: "credential-ownership", Label: "Credential owner, expiry and rotation", Packs: []string{live}, Severity: LineBlocking, Fix: "configuration:credentials", check: checkCredentialOwnership},
		{ID: "external-destinations", Label: "External destinations confirmed", Packs: []string{live}, Severity: LineBlocking, Manual: true, Fix: "configuration:credentials", check: checkExternalDestinations, stamp: destinationStamp},
		{ID: "rto-rpo", Label: "Recovery time and point targets stated", Packs: []string{live}, Severity: LineBlocking, Manual: true, check: manualLine("person", "No recovery time or point target is recorded")},
		{ID: "incident-contacts", Label: "Incident contacts named", Packs: []string{live}, Severity: LineBlocking, Manual: true, check: manualLine("person", "No incident contact is recorded")},
		{ID: "retention-legal", Label: "Data, retention and legal basis agreed", Packs: []string{live}, Severity: LineBlocking, Manual: true, check: manualLine("person", "No retention or legal basis decision is recorded")},
		{ID: "customer-acceptance", Label: "Customer acceptance by a named person", Packs: []string{live}, Severity: LineCritical, Manual: true, check: manualLine("customer", "The customer's acceptance is not recorded")},
	}
}

// ---- checks -----------------------------------------------------------

func checkFinCalendar(c *goLiveCtx) lineResult {
	const src = "Accounting periods"
	periods, err := ListAccountingPeriods(c.tenantID)
	if err != nil {
		return errLine(src, err)
	}
	if len(periods) == 0 {
		return fail(src, "No accounting period exists.")
	}
	today := c.todayStr()
	for _, p := range periods {
		if p.Status == "Open" && p.StartDate <= today && today <= p.EndDate {
			return pass(src, "%s, %s to %s, is open.", p.PeriodName, p.StartDate, p.EndDate)
		}
	}
	return fail(src, "%d period(s), none open for today.", len(periods))
}

func checkFinCurrency(c *goLiveCtx) lineResult {
	const src = "Currency records"
	codes, err := c.strings(`SELECT UPPER(COALESCE(data->>'code', '')) FROM %s.documents WHERE doctype = 'Currency' AND deleted_at IS NULL AND status = 'Active'`)
	if err != nil {
		return errLine(src, err)
	}
	for _, code := range codes {
		if code == "INR" {
			return pass(src, "INR is active; %d currency record(s).", len(codes))
		}
	}
	if len(codes) == 0 {
		return fail(src, "No active currency.")
	}
	return fail(src, "Active: %s. INR is not.", strings.Join(codes, ", "))
}

var postingAccounts = []string{"1100", "1200", "1300", "2100", "2200", "4100"}

func checkFinChart(c *goLiveCtx) lineResult {
	const src = "Chart of accounts"
	have, err := c.strings(`SELECT account_code FROM %s.gl_accounts`)
	if err != nil {
		return errLine(src, err)
	}
	set := map[string]bool{}
	for _, a := range have {
		set[a] = true
	}
	var missing []string
	for _, a := range postingAccounts {
		if !set[a] {
			missing = append(missing, a)
		}
	}
	if len(missing) > 0 {
		return fail(src, "Missing: %s.", strings.Join(missing, ", "))
	}
	return pass(src, "%d accounts; the posting accounts %s exist.", len(have), strings.Join(postingAccounts, ", "))
}

func checkFinSeries(c *goLiveCtx) lineResult {
	const src = "Guided setup · Document numbering"
	steps, err := c.setupSteps()
	if err != nil {
		return errLine(src, err)
	}
	for _, s := range steps {
		if s.ID != "numbering" {
			continue
		}
		var bad []string
		for _, ch := range s.Checks {
			if !ch.Passed && ch.Severity == SetupSeverityBlocking {
				bad = append(bad, ch.Label)
			}
		}
		if len(bad) > 0 {
			return fail(src, "%s", setupNamed(bad))
		}
		return pass(src, "%d series; sales invoice, credit note, debit note and OMS bill numbers fit.", s.Count)
	}
	return notEvidenced(src, "The numbering step is not in the setup path.")
}

func checkFinBank(c *goLiveCtx) lineResult {
	const src = "Bank accounts"
	ids, err := c.strings(`SELECT b.id FROM %[1]s.documents b JOIN %[1]s.gl_accounts g ON g.account_code = b.data->>'gl_account_code'
		WHERE b.doctype = 'BankAccount' AND b.deleted_at IS NULL AND COALESCE(b.status, 'Active') NOT IN ('Inactive', 'Cancelled', 'Archived') ORDER BY b.id`)
	if err != nil {
		return errLine(src, err)
	}
	if len(ids) == 0 {
		return fail(src, "No active bank account posts to a ledger account.")
	}
	return pass(src, "%s.", setupNamed(ids))
}

func checkFinOpening(c *goLiveCtx) lineResult {
	const src = "Ledger postings"
	n, err := c.count(`SELECT COUNT(*) FROM %s.gl_postings`)
	if err != nil {
		return errLine(src, err)
	}
	if n == 0 {
		return notEvidenced(src, "No opening balances are posted. Attach the opening date, or that the books start empty.")
	}
	tb, err := GetTrialBalance(c.tenantID, c.todayStr())
	if err != nil {
		return errLine(src, err)
	}
	if b, _ := tb["balanced"].(bool); !b {
		return fail(src, "%d postings; debits and credits differ.", n)
	}
	return pass(src, "%d postings; the trial balance balances.", n)
}

func checkReconTied(c *goLiveCtx) lineResult {
	const src = "Control totals · stock, receivables, payables, trial balance"
	t := c.subledgerTies()
	switch {
	case t.err != nil:
		return errLine(src, t.err)
	case !t.ok:
		return fail(src, "%s", t.detail)
	}
	return pass(src, "Inventory, receivables and payables tie to the ledger; the trial balance balances.")
}

func checkReconAccepted(c *goLiveCtx) lineResult {
	const src = "Acceptance record"
	ev := c.latest("fin-recon-accepted", "Evidence")
	if ev == nil {
		return notEvidenced(src, "Nobody has accepted the reconciliation.")
	}
	if tied := c.rawLine("fin-recon-tied"); tied.State != LinePass {
		return fail(src, "Accepted by %s on %s; the books no longer tie.", ev.By, ev.At)
	}
	return pass(src, "Accepted by %s on %s. %s", ev.By, ev.At, strings.TrimSpace(ev.Reference+" "+ev.Note))
}

func checkCutExceptions(c *goLiveCtx) lineResult {
	const src = "Guided setup checks · receivables vs ledger"
	var parts []string
	steps, err := c.setupSteps()
	if err != nil {
		return errLine(src, err)
	}
	invalid := 0
	for _, s := range steps {
		if s.State == SetupStateInvalid {
			invalid++
		}
	}
	if invalid > 0 {
		parts = append(parts, fmt.Sprintf("%d setup step(s) Invalid", invalid))
	}
	rows, err := ReceivablesVsLedger(c.tenantID, true)
	if err != nil {
		return errLine(src, err)
	}
	if len(rows) > 0 {
		parts = append(parts, fmt.Sprintf("%d invoice(s) differ from the ledger", len(rows)))
	}
	if len(parts) > 0 {
		return fail(src, "%s.", strings.Join(parts, "; "))
	}
	return pass(src, "No Invalid setup step; every invoice agrees with the ledger.")
}

func checkBackupRestore(c *goLiveCtx) lineResult {
	const src = "Host log (ops_run_log)"
	backup, err := lastOpsRun("backup")
	if err != nil {
		return errLine(src, err)
	}
	drill, err := lastOpsRun("restore_drill")
	if err != nil {
		return errLine(src, err)
	}
	if backup == nil || drill == nil {
		return notEvidenced(src, "The host has not recorded a successful backup and restore drill.")
	}
	took := drill.Finished.Sub(drill.Started).Round(time.Second)
	detail := fmt.Sprintf("Backup %s; restore drill %s took %s.", backup.Finished.Format("2006-01-02 15:04"), drill.Finished.Format("2006-01-02 15:04"), took)
	if c.today.Sub(backup.Finished) > backupFreshDay*24*time.Hour || c.today.Sub(drill.Finished) > drillFreshDay*24*time.Hour {
		return fail(src, "Too old. %s", detail)
	}
	return pass(src, "%s", detail)
}

func checkSoD(c *goLiveCtx) lineResult {
	const src = "Roles · SoD catalog"
	roles, err := RolesWithStoredGrants(c.tenantID)
	if err != nil {
		return errLine(src, err)
	}
	var found []string
	for _, role := range roles {
		ex, err := ExplainAccess(c.tenantID, role, "")
		if err != nil {
			return errLine(src, err)
		}
		for _, f := range ex.Conflicts {
			if f.Severity == "High" {
				found = append(found, role+": "+f.Label)
			}
		}
	}
	if len(found) > 0 {
		return fail(src, "%s", setupNamed(found))
	}
	return pass(src, "%d role(s) checked; no High-severity conflict.", len(roles))
}

func (c *goLiveCtx) usedChannelChosen() bool {
	return c.scopeHas("channels", "website") || c.scopeHas("channels", "shopify") || c.scopeHas("channels", "courier_api")
}

func checkIntegrationProof(c *goLiveCtx) lineResult {
	const src = "Connector credentials"
	creds, err := ListConnectorCredentials(c.tenantID)
	if err != nil {
		return errLine(src, err)
	}
	var live, unused []string
	for _, cr := range creds {
		if cr.Active {
			live = append(live, cr.Kind+" "+cr.Code)
			if cr.LastUsedAt == "" {
				unused = append(unused, cr.Kind+" "+cr.Code)
			}
		}
	}
	if len(live) == 0 {
		if c.usedChannelChosen() {
			return notEvidenced(src, "The scope names a connected channel; no connector is active.")
		}
		return notApplicable(src, "No connector is active and none is in scope.")
	}
	if len(unused) > 0 {
		return notEvidenced(src, "Never used: %s.", setupNamed(unused))
	}
	return pass(src, "%d connector(s), each used.", len(live))
}

func checkDeviceProof(c *goLiveCtx) lineResult {
	const src = "Scope · devices"
	if c.scope == nil || !c.scope.Saved {
		return notEvidenced(src, "The scope is not answered, so devices are unknown.")
	}
	if c.scopeHas("devices", "none") {
		return notApplicable(src, "No devices in scope.")
	}
	return notEvidenced(src, "No device test is recorded. Attach it.")
}

func checkScopeSaved(c *goLiveCtx) lineResult {
	const src = "Scope questionnaire"
	if c.scope == nil || !c.scope.Saved {
		return notEvidenced(src, "The scope questions are not answered.")
	}
	return pass(src, "Version %d, saved by %s on %s.", c.scope.Version, c.scope.SavedBy, c.scope.SavedAt)
}

func checkCapabilityLevel(c *goLiveCtx) lineResult {
	const src = "Capability maturity register"
	if c.scope == nil || !c.scope.Saved {
		return notEvidenced(src, "The scope is not answered.")
	}
	if c.scope.Unevaluated {
		return notEvidenced(src, "A scope answer is out of date. Answer the scope again.")
	}
	if len(c.scope.Unsupported) > 0 {
		var names []string
		for _, u := range c.scope.Unsupported {
			names = append(names, u.Label+" ("+u.Detail+")")
		}
		return fail(src, "Unsupported: %s.", setupNamed(names))
	}
	var lv []string
	for _, m := range c.scope.Modules {
		lv = append(lv, m.Module+" "+m.Level)
	}
	return pass(src, "%s.", strings.Join(lv, ", "))
}

func checkCredentialOwnership(c *goLiveCtx) lineResult {
	const src = "Connector credentials"
	creds, err := ListConnectorCredentials(c.tenantID)
	if err != nil {
		return errLine(src, err)
	}
	var bad []string
	n := 0
	cutoff := c.today.AddDate(-1, 0, 0).Format("2006-01-02")
	for _, cr := range creds {
		if !cr.Active {
			continue
		}
		n++
		name := cr.Kind + " " + cr.Code
		switch {
		case cr.Owner == "":
			bad = append(bad, name+": no owner")
		case cr.Status == "Expired":
			bad = append(bad, name+": expired")
		case len(cr.ChangedAt) >= 10 && cr.ChangedAt[:10] < cutoff:
			bad = append(bad, name+": not rotated in a year")
		}
	}
	if n == 0 {
		return notApplicable(src, "No credential is active.")
	}
	if len(bad) > 0 {
		return fail(src, "%s", setupNamed(bad))
	}
	return pass(src, "%d credential(s), each with an owner, in date and changed within a year.", n)
}

func checkExternalDestinations(c *goLiveCtx) lineResult {
	const src = "Credentials and webhook subscriptions"
	dest, err := externalDestinations(c)
	if err != nil {
		return errLine(src, err)
	}
	if len(dest) == 0 {
		return notApplicable(src, "No external destination is set up.")
	}
	return notEvidenced(src, "%d destination(s): %s. Confirm them.", len(dest), setupNamed(dest))
}

// externalDestinations is every active connector credential and webhook
// subscription, sorted.
func externalDestinations(c *goLiveCtx) ([]string, error) {
	creds, err := ListConnectorCredentials(c.tenantID)
	if err != nil {
		return nil, err
	}
	var dest []string
	for _, cr := range creds {
		if cr.Active {
			dest = append(dest, cr.Kind+" "+cr.Code)
		}
	}
	hooks, err := c.strings(`SELECT COALESCE(NULLIF(data->>'url', ''), id) FROM %s.documents WHERE doctype = 'WebhookSubscription' AND deleted_at IS NULL AND COALESCE(status, 'Active') = 'Active'`)
	if err != nil {
		return nil, err
	}
	dest = append(dest, hooks...)
	sort.Strings(dest)
	return dest, nil
}

// destinationStamp fingerprints the set of destinations (49.17.3): evidence that
// they are proven or confirmed goes stale when the set changes.
func destinationStamp(c *goLiveCtx) (string, error) {
	dest, err := externalDestinations(c)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(strings.Join(dest, "|")))
	return hex.EncodeToString(sum[:]), nil
}
