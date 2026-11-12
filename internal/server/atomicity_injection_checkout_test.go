package server

// Stage 49.5.2 - failure injection through the real HTTP path for the sale.
//
// A trigger raises inside the sale's own transaction at each significant write
// (internal/testfault), and the test asserts: the sale left nothing behind, the
// caller got a plain error with no internals, and a retry under the SAME
// idempotency key then posts exactly once and a further replay moves nothing.
// docs/security/atomicity-failure-injection.md is the command-by-command record.

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"custom_erp/db"
	"custom_erp/internal/testfault"
)

// checkoutSteps are the writes a completed sale makes inside its transaction.
var checkoutSteps = []testfault.Step{
	{Label: "availability", Table: "inventory_availability", On: "UPDATE"},
	{Label: "stock-ledger", Table: "documents", Doctype: "StockLedgerEntry", On: "INSERT"},
	{Label: "gl", Table: "gl_postings", On: "INSERT"},
	{Label: "cart-paid", Table: "documents", Doctype: "POSCart", On: "UPDATE", When: "NEW.status = 'Paid'"},
	{Label: "outbox", Table: "integration_event_outbox", On: "INSERT"},
	{Label: "idempotency-complete", Table: "command_idempotency", On: "UPDATE", When: "NEW.status = 'Completed'"},
}

func (f *posPricingFixture) claimError(key string) string {
	f.t.Helper()
	var msg string
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(response->>'error', '') FROM %s.command_idempotency WHERE idempotency_key LIKE $1`, f.schema),
		"pos.checkout:%"+key).Scan(&msg)
	return msg
}

func TestStage4952CheckoutFailureAtEachWriteRollsBackAndRetries(t *testing.T) {
	for _, step := range checkoutSteps {
		step := step
		t.Run(step.Label, func(t *testing.T) {
			f := newPOSPricingFixture(t, 500)
			key := "FI-" + f.suffix
			cart := "FICART-" + f.suffix
			t.Cleanup(func() {
				db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.command_idempotency WHERE idempotency_key LIKE $1`, f.schema), "%"+key)
			})
			body := map[string]interface{}{
				"cart_number": cart, "idempotency_key": key, "location": f.location, "payment_mode": "Cash",
				"items": []map[string]interface{}{{"sku": f.sku, "qty": 3}},
			}
			before := f.availabilityFor(f.sku)
			// The cart document is written by the handler before the sale starts,
			// outside the sale's transaction, and is expected to remain as Failed.
			pre := testfault.Fingerprint(t, db.DB, f.schema, f.suffix, "documents:POSCart", "documents:Customer", "audit_logs")

			disarm := testfault.Arm(t, db.DB, f.schema, step, f.suffix)
			status, resp := f.checkout(f.cashierToken, body)
			if status < 400 {
				t.Fatalf("step %q: the sale succeeded (%d) - the command never reaches this write, so the step does not belong in the matrix", step.Label, status)
			}
			raw := fmt.Sprint(resp)
			for _, leak := range []string{testfault.Marker, "tenant_default", "pq:", "trigger", "plpgsql"} {
				if strings.Contains(raw, leak) {
					t.Errorf("step %q: the API answer leaks %q: %v", step.Label, leak, resp)
				}
			}
			if status != http.StatusInternalServerError && status != http.StatusConflict {
				t.Errorf("step %q: status %d, want a plain 500/409 refusal", step.Label, status)
			}
			if msg := f.claimError(key); !strings.Contains(msg, testfault.Marker+"["+step.Label+"]") {
				t.Fatalf("step %q: the sale did not fail at the injected write; recorded reason: %q", step.Label, msg)
			}

			// Complete rollback.
			if d := testfault.Diff(pre, testfault.Fingerprint(t, db.DB, f.schema, f.suffix, "documents:POSCart", "documents:Customer", "audit_logs")); d != "" {
				rows, _ := db.DB.Query(fmt.Sprintf(`SELECT doctype||':'||id FROM %s.documents WHERE data::text LIKE '%%'||$1||'%%' OR id LIKE '%%'||$1||'%%'`, f.schema), f.suffix)
				var seen []string
				for rows != nil && rows.Next() {
					var s string
					_ = rows.Scan(&s)
					seen = append(seen, s)
				}
				t.Errorf("step %q: the failed sale left rows behind in: %s (documents now: %v)", step.Label, d, seen)
			}
			if got := f.availabilityFor(f.sku); got != before {
				t.Errorf("step %q: availability %d -> %d after a failed sale", step.Label, before, got)
			}
			var cartStatus string
			_ = db.DB.QueryRow(fmt.Sprintf(`SELECT status FROM %s.documents WHERE doctype='POSCart' AND id=$1`, f.schema), cart).Scan(&cartStatus)
			if cartStatus == "Paid" {
				t.Errorf("step %q: the cart is Paid after a failed sale", step.Label)
			}

			// Retry, same key: succeeds exactly once.
			disarm()
			status, first := f.checkout(f.cashierToken, body)
			if status != http.StatusOK || first["status"] != "completed" {
				t.Fatalf("step %q: retry under the same key failed (%d): %v", step.Label, status, first)
			}
			if got := f.availabilityFor(f.sku); got != before-3 {
				t.Errorf("step %q: after the retry availability is %d, want %d (sold once)", step.Label, got, before-3)
			}
			if rec := f.reconcile(cart); rec.Verdict != "Balanced" {
				t.Errorf("step %q: the retried sale does not reconcile: %s - %s", step.Label, rec.Verdict, rec.Detail)
			}
			posted := testfault.Fingerprint(t, db.DB, f.schema, f.suffix)

			// Replay: same answer, nothing further moved.
			status, again := f.checkout(f.cashierToken, body)
			if status != http.StatusOK || fmt.Sprint(again["bill_number"]) != fmt.Sprint(first["bill_number"]) || again["sale_total"] != first["sale_total"] {
				t.Errorf("step %q: the replay answered differently (%d): %v vs %v", step.Label, status, again, first)
			}
			if d := testfault.Diff(posted, testfault.Fingerprint(t, db.DB, f.schema, f.suffix)); d != "" {
				t.Errorf("step %q: the replay moved: %s", step.Label, d)
			}
		})
	}
}

// Two tills submit the same sale under one key at the same moment: one effect.
func TestStage4952CheckoutDoubleSubmitRaceHasOneEffect(t *testing.T) {
	f := newPOSPricingFixture(t, 500)
	key := "FIRACE-" + f.suffix
	cart := "FIRACE-" + f.suffix
	t.Cleanup(func() {
		db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.command_idempotency WHERE idempotency_key LIKE $1`, f.schema), "%"+key)
	})
	before := f.availabilityFor(f.sku)
	body := map[string]interface{}{
		"cart_number": cart, "idempotency_key": key, "location": f.location, "payment_mode": "Cash",
		"items": []map[string]interface{}{{"sku": f.sku, "qty": 2}},
	}
	var wg sync.WaitGroup
	codes := make([]int, 4)
	for i := range codes {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			codes[i], _ = f.post(handleCheckout, "/api/v1/checkout", f.cashierToken, body)
		}(i)
	}
	wg.Wait()
	ok := 0
	for _, c := range codes {
		if c == http.StatusOK {
			ok++
		} else if c >= 500 {
			t.Errorf("a concurrent duplicate got HTTP %d; want 200 (replay) or a plain 4xx", c)
		}
	}
	if ok < 1 {
		t.Fatalf("no submission succeeded: %v", codes)
	}
	if got := f.availabilityFor(f.sku); got != before-2 {
		t.Fatalf("%d concurrent identical submissions moved availability %d -> %d, want exactly one sale of 2", len(codes), before, got)
	}
	var paid int
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.documents WHERE doctype='POSCart' AND status='Paid' AND data::text LIKE '%%'||$1||'%%'`, f.schema), f.suffix).Scan(&paid)
	if paid != 1 {
		t.Errorf("%d paid carts for one key, want 1", paid)
	}
}
