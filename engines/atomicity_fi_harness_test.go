package engines

// Stage 49.5.2 - the table-driven failure-injection runner for the engine
// commands (docs/security/atomicity-failure-injection.md is the record).
//
// For every command and every significant write inside its transaction the
// runner (1) arms a trigger on that write (internal/testfault), (2) runs the
// command and checks it failed AT that write, (3) checks nothing the command
// owns changed, (4) disarms and retries, (5) checks the effect happened
// exactly once, (6) replays and checks nothing moved further, and (7) races
// several callers and checks the effect is still exactly once.

import (
	"custom_erp/db"
	"custom_erp/internal/testfault"
	"fmt"
	"sync"
	"testing"
)

// fiEnv is a unique slice of the shared tenant: every id carries marker, so a
// trigger armed for it can never touch another test's rows.
type fiEnv struct {
	t      *testing.T
	schema string
	marker string
	sku    string
	loc    string
}

func fiNewEnv(t *testing.T) *fiEnv {
	t.Helper()
	db.InitDB(testConnStr())
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatal(err)
	}
	e := &fiEnv{t: t, schema: schema}
	e.marker = "FI" + fiUnique()
	e.sku = "FISKU-" + e.marker
	e.loc = "FILOC-" + e.marker
	t.Cleanup(e.purge)
	return e
}

func fiUnique() string {
	id := NewDocIDCompact("X")
	if len(id) > 14 {
		id = id[len(id)-14:]
	}
	return id
}

// exec runs a seed statement and fails the test on error.
func (e *fiEnv) exec(q string, args ...interface{}) {
	e.t.Helper()
	if _, err := db.DB.Exec(q, args...); err != nil {
		e.t.Fatalf("fixture: %s: %v", q, err)
	}
}

// doc seeds a document row.
func (e *fiEnv) doc(doctype, id, status string, data string) {
	e.t.Helper()
	e.exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3::jsonb, $4, 'system')`, e.schema), id, doctype, data, status)
}

// seedItem puts the item (costed at 40), its location and qty in stock.
func (e *fiEnv) seedItem(qty int) {
	e.t.Helper()
	e.doc("Item", e.sku, "Active", fmt.Sprintf(`{"code":%q,"name":"FI item","hsn_code":"6109","gst_rate":18,"mrp":100,"sale_price":100,"cost_price":40,"standard_cost":40}`, e.sku))
	e.doc("Location", e.loc, "Active", fmt.Sprintf(`{"code":%q,"name":"FI store"}`, e.loc))
	e.stock(e.sku, e.loc, qty)
}

func (e *fiEnv) stock(sku, loc string, qty int) {
	e.t.Helper()
	e.exec(fmt.Sprintf(`INSERT INTO %s.inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, $3, $3) ON CONFLICT (sku, location_code) DO UPDATE SET on_hand = $3, available = $3`, e.schema), sku, loc, qty)
}

func (e *fiEnv) avail(sku, loc string) (onHand, available, inTransit int) {
	e.t.Helper()
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT on_hand, available, in_transit FROM %s.inventory_availability WHERE sku = $1 AND location_code = $2`, e.schema), sku, loc).Scan(&onHand, &available, &inTransit)
	return
}

func (e *fiEnv) count(table, where string, args ...interface{}) int {
	e.t.Helper()
	var n int
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.%s WHERE %s`, e.schema, table, where), args...).Scan(&n); err != nil {
		e.t.Fatalf("count %s: %v", table, err)
	}
	return n
}

func (e *fiEnv) docStatus(doctype, id string) string {
	var s string
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT status FROM %s.documents WHERE doctype = $1 AND id = $2`, e.schema), doctype, id).Scan(&s)
	return s
}

// purge removes everything that carries the marker.
func (e *fiEnv) purge() {
	for _, table := range []string{"gl_postings", "integration_event_outbox", "loyalty_point_ledger", "bin_stock", "inventory_reservation", "inventory_availability", "command_idempotency", "documents"} {
		_, _ = db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.%s x WHERE to_jsonb(x)::text LIKE '%%'||$1||'%%'`, e.schema, table), e.marker)
	}
}

// fiScenario is one command, ready to run against its own fixture.
type fiScenario struct {
	env *fiEnv
	// run is the command. It is called for the failing attempt, the retry, the
	// replay and every racer; it carries its own idempotency (a key, or the
	// document's status), as the real caller does.
	run func() (string, error)
	// effect asserts the command's whole effect happened exactly once.
	effect func(t *testing.T)
	// skip are tables (and "documents:Doctype") the command legitimately
	// changes outside its transaction, or a record of the failure.
	skip []string
	// tolerateReplayError: a replay may answer with a plain refusal ("already
	// posted") instead of the first answer, as long as nothing moves.
	tolerateReplayError bool
}

// fiMatrix runs every step of one command.
func fiMatrix(t *testing.T, steps []testfault.Step, build func(t *testing.T) *fiScenario) {
	t.Helper()
	for _, step := range steps {
		step := step
		t.Run(step.Label, func(t *testing.T) {
			s := build(t)
			e := s.env
			// A failed attempt may write an audit row saying it failed - a record,
			// not a leak - so the rollback check leaves audit_logs out. A replay
			// that did nothing must not add one: that check keeps it in.
			failSkip := append(append([]string{}, s.skip...), "audit_logs")
			pre := testfault.Fingerprint(t, db.DB, e.schema, e.marker, failSkip...)

			disarm := testfault.Arm(t, db.DB, e.schema, step, e.marker)
			_, err := s.run()
			if err == nil {
				t.Fatalf("step %q: the command succeeded - it never reaches this write, so the step does not belong in the matrix", step.Label)
			}
			if !testfault.Hit(err, step.Label) {
				t.Fatalf("step %q: the command failed, but not at the injected write: %v", step.Label, err)
			}
			if d := testfault.Diff(pre, testfault.Fingerprint(t, db.DB, e.schema, e.marker, failSkip...)); d != "" {
				t.Errorf("step %q: the failed command left changes behind in: %s", step.Label, d)
			}

			disarm()
			first, err := s.run()
			if err != nil {
				t.Fatalf("step %q: the retry failed: %v", step.Label, err)
			}
			s.effect(t)
			posted := testfault.Fingerprint(t, db.DB, e.schema, e.marker, s.skip...)

			second, err := s.run()
			if err != nil && !s.tolerateReplayError {
				t.Errorf("step %q: the replay failed: %v", step.Label, err)
			}
			if err == nil && second != first {
				t.Errorf("step %q: the replay answered %q, the first answer was %q", step.Label, second, first)
			}
			if d := testfault.Diff(posted, testfault.Fingerprint(t, db.DB, e.schema, e.marker, s.skip...)); d != "" {
				t.Errorf("step %q: the replay moved: %s", step.Label, d)
			}
			s.effect(t)
		})
	}
}

// fiRace calls the command from several goroutines at once; the effect must
// still be there exactly once.
func fiRace(t *testing.T, build func(t *testing.T) *fiScenario) {
	t.Helper()
	s := build(t)
	const n = 4
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = s.run()
		}(i)
	}
	wg.Wait()
	ok := 0
	for _, err := range errs {
		if err == nil {
			ok++
		}
	}
	if ok == 0 {
		t.Fatalf("no concurrent caller succeeded: %v", errs)
	}
	s.effect(t)
}

// TestFaultHarnessSeesANonAtomicWrite is the negative control: two writes in
// separate transactions, the second failed by injection, leave the first
// behind, and the fingerprint says so. Without this a harness that could never
// see a leak would pass everything.
func TestFaultHarnessSeesANonAtomicWrite(t *testing.T) {
	e := fiNewEnv(t)
	e.seedItem(10)
	pre := testfault.Fingerprint(t, db.DB, e.schema, e.marker)
	testfault.Arm(t, db.DB, e.schema, testfault.Step{Label: "second", Table: "gl_postings", On: "INSERT"}, e.marker)

	write := func(atomic bool) error {
		step1 := fmt.Sprintf(`UPDATE %s.inventory_availability SET available = available - 1 WHERE sku = $1 AND location_code = $2`, e.schema)
		step2 := fmt.Sprintf(`INSERT INTO %s.gl_postings (document_type, document_id, account_code, debit, credit, idempotency_key) VALUES ('FI', $1, '1200', 1, 0, $1)`, e.schema)
		if !atomic {
			if _, err := db.DB.Exec(step1, e.sku, e.loc); err != nil {
				return err
			}
			_, err := db.DB.Exec(step2, "FIDOC-"+e.marker)
			return err
		}
		tx, err := db.DB.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if _, err := tx.Exec(step1, e.sku, e.loc); err != nil {
			return err
		}
		if _, err := tx.Exec(step2, "FIDOC-"+e.marker); err != nil {
			return err
		}
		return tx.Commit()
	}

	if err := write(true); !testfault.Hit(err, "second") {
		t.Fatalf("the atomic write did not fail at the injected step: %v", err)
	}
	if d := testfault.Diff(pre, testfault.Fingerprint(t, db.DB, e.schema, e.marker)); d != "" {
		t.Fatalf("the atomic write left a change behind (%s): the harness or the rollback is wrong", d)
	}
	if err := write(false); !testfault.Hit(err, "second") {
		t.Fatalf("the non-atomic write did not fail at the injected step: %v", err)
	}
	if d := testfault.Diff(pre, testfault.Fingerprint(t, db.DB, e.schema, e.marker)); d == "" {
		t.Fatal("a write outside the transaction left its first half behind, and the fingerprint did not see it")
	}
}
