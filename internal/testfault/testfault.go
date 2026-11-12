// Package testfault is the failure-injection harness for tests (Stage 49.5.2).
//
// It makes a command die at a chosen write INSIDE its own transaction: Arm
// puts a BEFORE INSERT/UPDATE trigger on one tenant table that raises an
// exception for a row carrying the test's unique marker, so the statement
// fails exactly where the engine would have written it. Nothing else can trip
// it:
//
//   - the trigger fires only for a row whose text contains the marker (a unique
//     SKU, location or key the test minted), so another test's rows, or a
//     concurrent session's, never match;
//   - the trigger and its function are created per call under a unique name and
//     removed by the returned disarm (and by t.Cleanup), so there is nothing to
//     leak into another test;
//   - it lives in one schema only (the one passed in).
//
// The raised message carries INJECTED_FAILURE[label]. A test asserts the error
// it got back contains that label: that proves the command failed at the step
// the test meant, not at some other step or for an unrelated reason. A test
// whose command succeeds with the trigger armed has found a step the command
// never reaches, and says so.
//
// This is test support. No non-test package imports it.
package testfault

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/lib/pq"
)

// Marker is the text an injected failure message always contains.
const Marker = "INJECTED_FAILURE"

var identRe = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

// Step names one write of a command: the table, the statement kind and, for the
// shared documents table, the doctype.
type Step struct {
	Label   string // the name the test uses for this step; appears in the error
	Table   string // e.g. "gl_postings"
	Doctype string // optional: only documents of this doctype
	On      string // "INSERT", "UPDATE" or "INSERT OR UPDATE" (default)
	// When is an optional SQL condition on NEW (test-written, never user input),
	// e.g. "NEW.status = 'Paid'", to pick the one UPDATE of a row that is
	// written more than once by a command.
	When string
}

// Arm installs the fault for step in schema; a row of step.Table whose JSON text
// contains rowMarker makes the statement raise. It returns the disarm, which is
// also registered with t.Cleanup (so calling it is optional, and calling it
// twice is safe).
func Arm(t testing.TB, conn *sql.DB, schema string, step Step, rowMarker string) (disarm func()) {
	t.Helper()
	if !identRe.MatchString(schema) || !identRe.MatchString(step.Table) {
		t.Fatalf("testfault: %q.%q is not a usable table", schema, step.Table)
	}
	if strings.TrimSpace(rowMarker) == "" {
		t.Fatal("testfault: a row marker is required, or the fault would hit every row")
	}
	on := step.On
	if on == "" {
		on = "INSERT OR UPDATE"
	}
	if on != "INSERT" && on != "UPDATE" && on != "INSERT OR UPDATE" {
		t.Fatalf("testfault: unknown statement kind %q", on)
	}
	b := make([]byte, 5)
	_, _ = rand.Read(b)
	name := "zz_tf_" + hex.EncodeToString(b)
	fn := schema + "." + name

	create := fmt.Sprintf(`CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $f$
BEGIN
  IF to_jsonb(NEW)::text LIKE '%%' || TG_ARGV[0] || '%%'
     AND (TG_ARGV[1] = '' OR to_jsonb(NEW)->>'doctype' = TG_ARGV[1]) THEN
    RAISE EXCEPTION '%s[%%]', TG_ARGV[2];
  END IF;
  RETURN NEW;
END $f$`, fn, Marker)
	if _, err := conn.Exec(create); err != nil {
		t.Fatalf("testfault: create function: %v", err)
	}
	// $-quoting is not usable for trigger arguments; they are string literals,
	// so single quotes are doubled.
	q := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
	when := ""
	if strings.TrimSpace(step.When) != "" {
		when = " WHEN (" + step.When + ")"
	}
	trig := fmt.Sprintf(`CREATE TRIGGER %s BEFORE %s ON %s.%s FOR EACH ROW%s EXECUTE FUNCTION %s(%s, %s, %s)`,
		name, on, schema, step.Table, when, fn, q(rowMarker), q(step.Doctype), q(step.Label))
	if _, err := conn.Exec(trig); err != nil {
		_, _ = conn.Exec(`DROP FUNCTION IF EXISTS ` + fn + `()`)
		t.Fatalf("testfault: create trigger: %v", err)
	}
	done := false
	disarm = func() {
		if done {
			return
		}
		done = true
		_, _ = conn.Exec(fmt.Sprintf(`DROP TRIGGER IF EXISTS %s ON %s.%s`, name, schema, step.Table))
		_, _ = conn.Exec(`DROP FUNCTION IF EXISTS ` + fn + `()`)
	}
	t.Cleanup(disarm)
	return disarm
}

// Hit reports whether err is an injected failure for label.
func Hit(err error, label string) bool {
	return err != nil && strings.Contains(err.Error(), Marker+"["+label+"]")
}

// FingerprintTables are the tables a command's effects land in. documents is
// every doctype; command_idempotency is left out on purpose (a failed attempt
// legitimately leaves a Failed claim) and checked on its own.
var FingerprintTables = []string{
	"documents", "gl_postings", "inventory_availability", "integration_event_outbox",
	"audit_logs", "loyalty_point_ledger", "sequence_counters", "bin_stock",
	"stored_value_ledger", "inventory_reservation",
}

// Fingerprint captures, per table, the rows whose JSON text contains rowMarker:
// a row count and a hash of the rows. Two fingerprints are equal when nothing
// the marker reaches has changed. updated_at is ignored, and so are the
// post_error / gl_error fields engines write to say why a post failed (that
// note is a deliberate record, checked separately by tests that care).
//
// skip names tables to leave out (e.g. audit_logs where the engine records the
// refusal on purpose); "documents:Doctype" leaves out that one doctype.
func Fingerprint(t testing.TB, conn *sql.DB, schema, rowMarker string, skip ...string) map[string]string {
	t.Helper()
	skipped := map[string]bool{}
	skipDoctypes := []string{}
	for _, s := range skip {
		if strings.HasPrefix(s, "documents:") {
			skipDoctypes = append(skipDoctypes, strings.TrimPrefix(s, "documents:"))
			continue
		}
		skipped[s] = true
	}
	out := map[string]string{}
	for _, table := range FingerprintTables {
		if skipped[table] {
			continue
		}
		var exists bool
		if err := conn.QueryRow(`SELECT to_regclass($1) IS NOT NULL`, schema+"."+table).Scan(&exists); err != nil || !exists {
			continue
		}
		var n int
		var h string
		q := fmt.Sprintf(`SELECT COUNT(*), COALESCE(md5(string_agg(r, '|' ORDER BY r)), '') FROM (
			SELECT ((to_jsonb(x) - 'updated_at' - 'created_at') #- '{data,post_error}' #- '{data,gl_error}')::text AS r
			FROM %s.%s x WHERE to_jsonb(x)::text LIKE '%%' || $1 || '%%' AND NOT (COALESCE(to_jsonb(x)->>'doctype', '') = ANY($2))) s`, schema, table)
		if err := conn.QueryRow(q, rowMarker, pq.Array(skipDoctypes)).Scan(&n, &h); err != nil {
			t.Fatalf("testfault: fingerprint %s: %v", table, err)
		}
		out[table] = fmt.Sprintf("%d:%s", n, h)
	}
	return out
}

// Diff lists the tables whose fingerprints differ ("" = identical).
func Diff(a, b map[string]string) string {
	var diffs []string
	for k, v := range a {
		if b[k] != v {
			diffs = append(diffs, fmt.Sprintf("%s (%s -> %s)", k, strings.SplitN(v, ":", 2)[0], strings.SplitN(b[k], ":", 2)[0]))
		}
	}
	for k := range b {
		if _, ok := a[k]; !ok {
			diffs = append(diffs, k)
		}
	}
	return strings.Join(diffs, ", ")
}
