package server

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Stage 49.4.7a: the commands that create a new money or stock record each
// time carry the checkout's command key. A retry with the same key answers
// the first result and creates nothing twice; a different key is a new
// record; the same key with a different request is refused; a refused
// request frees its key; no key behaves as before.

func keyedRequest4947a(method, path, key, body string) *http.Request {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Resolved-Tenant-ID", "default")
	r.Header.Set("Resolved-User-ID", "u4947a")
	r.Header.Set("Content-Type", "application/json")
	if key != "" {
		r.Header.Set("Idempotency-Key", key)
	}
	return r
}

func TestStage4947aCommandKeyAnswersTheFirstResult(t *testing.T) {
	db.InitDB(testConnStr())
	var runs int64
	h := commandKeyed("t4947a.fake", func(w http.ResponseWriter, r *http.Request) {
		var in map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in["bad"] == true {
			writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "refused")
			return
		}
		n := atomic.AddInt64(&runs, 1)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": fmt.Sprintf("REC-%d", n), "amount": in["amount"]})
	})
	call := func(key, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h(rec, keyedRequest4947a(http.MethodPost, "/api/v1/t4947a", key, body))
		return rec
	}
	stamp := fmt.Sprint(time.Now().UnixNano())

	first := call("k1-"+stamp, `{"amount": 100}`)
	again := call("k1-"+stamp, `{"amount":100}`) // same request, other spacing
	if first.Code != 200 || again.Code != 200 || runs != 1 {
		t.Fatalf("retry ran the command again: codes %d/%d, runs %d", first.Code, again.Code, runs)
	}
	if strings.TrimSpace(first.Body.String()) != strings.TrimSpace(again.Body.String()) || again.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatalf("the retry must answer the first result: %q vs %q (replayed=%q)", first.Body.String(), again.Body.String(), again.Header().Get("Idempotent-Replayed"))
	}
	if other := call("k1-"+stamp, `{"amount": 200}`); other.Code != 409 || !strings.Contains(other.Body.String(), "GLOBAL-4972") || runs != 1 {
		t.Fatalf("same key, different request: %d %s runs %d", other.Code, other.Body.String(), runs)
	}
	if fresh := call("k2-"+stamp, `{"amount": 100}`); fresh.Code != 200 || runs != 2 || !strings.Contains(fresh.Body.String(), "REC-2") {
		t.Fatalf("a new key must create a new record: %d %s", fresh.Code, fresh.Body.String())
	}
	// No key: as before - every call runs.
	call("", `{"amount": 100}`)
	call("", `{"amount": 100}`)
	if runs != 4 {
		t.Fatalf("calls without a key must run each time, runs %d", runs)
	}
	// A refused request frees its key: the corrected request runs under it.
	if bad := call("k3-"+stamp, `{"bad": true}`); bad.Code != 422 {
		t.Fatalf("refusal: %d", bad.Code)
	}
	if fixed := call("k3-"+stamp, `{"amount": 5}`); fixed.Code != 200 || runs != 5 {
		t.Fatalf("the key of a refused request must be free: %d runs %d", fixed.Code, runs)
	}
	// The key may come in the body; the body's own key is not part of the request.
	if b1 := call("", `{"amount": 7, "idempotency_key": "k4-`+stamp+`"}`); b1.Code != 200 || runs != 6 {
		t.Fatalf("body key: %d runs %d", b1.Code, runs)
	}
	if b2 := call("", `{"amount": 7, "idempotency_key": "k4-`+stamp+`"}`); b2.Code != 200 || runs != 6 {
		t.Fatalf("body key retry ran again: %d runs %d", b2.Code, runs)
	}
	if h1 := call("k5-"+stamp, `{"amount": 8, "idempotency_key": "screen-press-1"}`); h1.Code != 200 || runs != 7 {
		t.Fatalf("header key: %d runs %d", h1.Code, runs)
	}
	if h2 := call("k5-"+stamp, `{"amount": 8, "idempotency_key": "screen-press-2"}`); h2.Code != 200 || runs != 7 {
		t.Fatalf("a re-press with the same header key must replay even if the body's key changed: %d %s runs %d", h2.Code, h2.Body.String(), runs)
	}
	// Another user's identical key is another command.
	rec := httptest.NewRecorder()
	r := keyedRequest4947a(http.MethodPost, "/api/v1/t4947a", "k1-"+stamp, `{"amount": 100}`)
	r.Header.Set("Resolved-User-ID", "u4947a-other")
	h(rec, r)
	if rec.Code != 200 || runs != 8 {
		t.Fatalf("keys are per user: %d runs %d", rec.Code, runs)
	}
}

func TestStage4947aConcurrentRetriesRunOnce(t *testing.T) {
	db.InitDB(testConnStr())
	var runs int64
	h := commandKeyed("t4947a.slow", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&runs, 1)
		time.Sleep(150 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "SLOW-1"})
	})
	key := "slow-" + fmt.Sprint(time.Now().UnixNano())
	codes := make([]int, 12)
	bodies := make([]string, 12)
	var wg sync.WaitGroup
	for i := range codes {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rec := httptest.NewRecorder()
			h(rec, keyedRequest4947a(http.MethodPost, "/api/v1/t4947a/slow", key, `{"amount": 1}`))
			codes[i], bodies[i] = rec.Code, rec.Body.String()
		}(i)
	}
	wg.Wait()
	if runs != 1 {
		t.Fatalf("12 simultaneous retries ran the command %d times", runs)
	}
	for i, c := range codes {
		if c != 200 && !(c == 409 && strings.Contains(bodies[i], "GLOBAL-4971")) {
			t.Fatalf("retry %d answered %d %s - want the first result or GLOBAL-4971", i, c, bodies[i])
		}
	}
	rec := httptest.NewRecorder()
	h(rec, keyedRequest4947a(http.MethodPost, "/api/v1/t4947a/slow", key, `{"amount": 1}`))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "SLOW-1") || runs != 1 {
		t.Fatalf("after it finished, a retry answers its result: %d %s", rec.Code, rec.Body.String())
	}
}

// The real journal voucher route: a retried create makes one voucher.
func TestStage4947aJournalVoucherRetryCreatesOneVoucher(t *testing.T) {
	db.InitDB(testConnStr())
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatal(err)
	}
	narration := "TEST-JV4947a " + fmt.Sprint(time.Now().UnixNano())
	defer db.DB.Exec(`DELETE FROM `+schema+`.documents WHERE doctype = 'JournalVoucher' AND data->>'narration' LIKE $1`, narration+"%")
	h := commandKeyed("finance.journal_voucher.create", handleCreateJournalVoucher)
	body := `{"voucher_date":"2026-07-25","narration":"` + narration + `","lines":[{"account_code":"5100","debit":250},{"account_code":"1100","credit":250}]}`
	post := func(key, b string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		r := keyedRequest4947a(http.MethodPost, "/api/v1/finance/journal-voucher", key, b)
		r.Header.Set("Resolved-User-ID", "admin") // documents.created_by names a real user
		h(rec, r)
		return rec
	}
	key := "jv-" + narration
	first, second := post(key, body), post(key, body)
	if first.Code != 200 || second.Code != 200 || first.Body.String() != second.Body.String() {
		t.Fatalf("create %d %s / retry %d %s", first.Code, first.Body.String(), second.Code, second.Body.String())
	}
	count := func() int {
		var n int
		_ = db.DB.QueryRow(`SELECT COUNT(*) FROM `+schema+`.documents WHERE doctype = 'JournalVoucher' AND data->>'narration' = $1`, narration).Scan(&n)
		return n
	}
	if n := count(); n != 1 {
		t.Fatalf("a retried create made %d vouchers", n)
	}
	if third := post("jv2-"+narration, body); third.Code != 200 || count() != 2 {
		t.Fatalf("a new key must make a new voucher: %d, count %d", third.Code, count())
	}
}

// Every command 49.4.7a names is wrapped in routes.go, so the audit shows it keyed.
func TestStage4947aMoneyAndStockCreatorsAreKeyed(t *testing.T) {
	rows, err := IdempotencyAuditRows("routes.go", ".")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"POST /api/v1/finance/journal-voucher":               false,
		"POST /api/v1/finance/payment-proposal/{id}/execute": false,
		"POST /api/v1/hr/run-payroll":                        false,
		"POST /api/v1/hr/disburse-loan":                      false,
		"POST /api/v1/inventory/adjustments":                 false,
		"POST /api/v1/pos/advances":                          false,
		"POST /api/v1/pos/gift-card/issue":                   false,
		"POST /api/v1/loyalty/redeem":                        false,
		"POST /api/v1/crm/voucher/redeem":                    false,
	}
	for _, row := range rows {
		if _, ok := want[row.Route]; ok {
			want[row.Route] = strings.HasPrefix(row.Key, "yes (command key")
		}
	}
	for route, keyed := range want {
		if !keyed {
			t.Errorf("%s is not keyed", route)
		}
	}
}
