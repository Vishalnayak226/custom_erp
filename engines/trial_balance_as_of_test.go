package engines

import (
	"custom_erp/db"
	"fmt"
	"reflect"
	"testing"
	"time"
)

// allPostingsAsOf is the as-of date for a test that means "every posting this
// test made", now that GetTrialBalance requires one (Stage 29.7.4). It is
// tomorrow rather than today on purpose: created_at is written by the
// database, so on a box whose local timezone is behind the database's, a
// posting made "now" can carry tomorrow's date and silently drop out of a
// today-bounded trial balance - a flaky failure that would look like a
// broken posting engine.
func allPostingsAsOf() string {
	return time.Now().AddDate(0, 0, 1).Format("2006-01-02")
}

// lenOfSlice reports the length of a slice held in an interface{} whose
// element type is a function-local struct and therefore cannot be named from
// a test (GetTrialBalance's "balances" is exactly that).
func lenOfSlice(v interface{}) int {
	rv := reflect.ValueOf(v)
	if !rv.IsValid() || rv.Kind() != reflect.Slice {
		return -1
	}
	return rv.Len()
}

// TestTrialBalanceAsOfDate covers the three things 29.7.4 actually changed:
// the parameter is mandatory, it genuinely bounds the aggregate, and the
// answer is reported back so a screen can label what it is showing.
//
// Wipes gl_postings for the tenant schema first - it asserts on absolute
// totals, and this is the shared dev DB, so anything already in the ledger
// would contaminate it. That is the same thing the pre-existing trial-balance
// subtest in engines_test.go does, and for the same reason.
func TestTrialBalanceAsOfDate(t *testing.T) {
	db.InitDB(testConnStr())

	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("get schema: %v", err)
	}

	const docID = "TB-ASOF-TEST"

	cleanup := func() {
		db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.gl_postings WHERE document_id = $1`, schema), docID)
	}
	if _, err := db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.gl_postings`, schema)); err != nil {
		t.Fatalf("failed to clear gl_postings: %v", err)
	}
	defer cleanup()

	// Two balanced pairs, one dated well in the past and one today, so an
	// as-of between them must see exactly one of them.
	seed := func(code string, debit, credit, daysAgo int) {
		if _, err := db.DB.Exec(fmt.Sprintf(
			`INSERT INTO %s.gl_postings (account_code, debit, credit, document_type, document_id, created_at)
			 VALUES ($1, $2, $3, 'TestJournal', $4, $5)`, schema),
			code, debit, credit, docID, time.Now().AddDate(0, 0, -daysAgo)); err != nil {
			t.Fatalf("failed to seed gl_posting: %v", err)
		}
	}
	seed("1100", 1000, 0, 40)
	seed("4100", 0, 1000, 40)
	seed("1100", 500, 0, 0)
	seed("4100", 0, 500, 0)

	t.Run("as_of is mandatory", func(t *testing.T) {
		_, err := GetTrialBalance(tenantID, "")
		if err == nil {
			t.Fatal("expected an error for an empty as_of, got nil")
		}
		verr, ok := err.(*ValidationError)
		if !ok {
			t.Fatalf("expected *ValidationError so the API can code it, got %T: %v", err, err)
		}
		if verr.Code != "GLOBAL-0001" {
			t.Errorf("expected GLOBAL-0001 (required value missing), got %q", verr.Code)
		}
	})

	t.Run("bounds the aggregate to the as-of date", func(t *testing.T) {
		cutoff := time.Now().AddDate(0, 0, -20).Format("2006-01-02")
		tb, err := GetTrialBalance(tenantID, cutoff)
		if err != nil {
			t.Fatalf("trial balance failed: %v", err)
		}
		if got := tb["total_debits"].(int); got != 1000 {
			t.Errorf("as of %s only the 40-day-old posting should count: want 1000 debits, got %d", cutoff, got)
		}
		if got := tb["total_credits"].(int); got != 1000 {
			t.Errorf("as of %s: want 1000 credits, got %d", cutoff, got)
		}
		if !tb["balanced"].(bool) {
			t.Error("a bounded trial balance of two balanced pairs must still be balanced")
		}
	})

	t.Run("includes postings made on the as-of date itself", func(t *testing.T) {
		// The half-open `created_at < ($1::date + 1)` spelling exists so that
		// a posting timestamped 23:59 on the as-of date is still inside it.
		// A naive `created_at <= $1::date` would drop every posting made
		// after midnight on the last day of the range.
		tb, err := GetTrialBalance(tenantID, allPostingsAsOf())
		if err != nil {
			t.Fatalf("trial balance failed: %v", err)
		}
		if got := tb["total_debits"].(int); got != 1500 {
			t.Errorf("today's own postings must be included: want 1500 debits, got %d", got)
		}
	})

	t.Run("an as-of before every posting is empty, not an error", func(t *testing.T) {
		tb, err := GetTrialBalance(tenantID, "2000-01-01")
		if err != nil {
			t.Fatalf("trial balance failed: %v", err)
		}
		if got := tb["total_debits"].(int); got != 0 {
			t.Errorf("want 0 debits before any posting existed, got %d", got)
		}
		if !tb["balanced"].(bool) {
			t.Error("an empty ledger is trivially balanced")
		}
		// The LEFT JOIN must still list every account, so Chart of Accounts -
		// which reuses this endpoint purely for the account rows - keeps
		// working at any as-of date. balances is a function-local struct type
		// that cannot be named here, so assert on its length reflectively.
		if n := lenOfSlice(tb["balances"]); n == 0 {
			t.Error("expected every gl_account still listed at a zero balance")
		}
	})

	t.Run("reports the as_of it used", func(t *testing.T) {
		tb, err := GetTrialBalance(tenantID, "2026-03-31")
		if err != nil {
			t.Fatalf("trial balance failed: %v", err)
		}
		if tb["as_of"] != "2026-03-31" {
			t.Errorf("expected as_of echoed back so the screen can label the statement, got %v", tb["as_of"])
		}
	})
}
