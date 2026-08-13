package engines

import (
	"custom_erp/db"
	"strings"
	"testing"
	"time"
)

// Stage 38.5's store is exercised directly rather than through a route, because
// the first slice of the public API is deliberately read-only - the guarantee
// has to be proven before the first mutating endpoint relies on it, not after.
func TestPublicAPIIdempotencyAndTraffic(t *testing.T) {
	db.InitDB(testConnStr())
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	var tablesExist bool
	if err := db.DB.QueryRow(`SELECT to_regclass($1) IS NOT NULL AND to_regclass($2) IS NOT NULL`,
		schema+".api_idempotency_keys", schema+".api_request_log").Scan(&tablesExist); err != nil {
		t.Fatalf("inspect Stage 38.3 tables: %v", err)
	}
	if !tablesExist {
		t.Skip("db/migrations_stage38_3_5_9_public_api_spine.sql has not been applied to this database")
	}

	const credentialID = "00000000-0000-4000-8000-0000000038a5"
	const key = "TEST-IDEMPOTENCY-KEY"
	cleanup := func() {
		_, _ = db.DB.Exec("DELETE FROM "+schema+".api_idempotency_keys WHERE credential_id = $1", credentialID)
		_, _ = db.DB.Exec("DELETE FROM "+schema+".api_request_log WHERE credential_id = $1", credentialID)
	}
	cleanup()
	defer cleanup()

	body := []byte(`{"order":"A-1"}`)
	replay, err := BeginIdempotentRequest("default", credentialID, key, "POST", "/api/public/v1/orders", body)
	if err != nil || replay != nil {
		t.Fatalf("first claim = %v, %v; want a clean claim with no replay", replay, err)
	}

	// A retry while the first attempt is still running must not run twice.
	if _, err := BeginIdempotentRequest("default", credentialID, key, "POST", "/api/public/v1/orders", body); err != ErrIdempotencyInProgress {
		t.Fatalf("concurrent retry error = %v, want ErrIdempotencyInProgress", err)
	}

	if err := CompleteIdempotentRequest("default", credentialID, key, 201, `{"id":"SO-1"}`); err != nil {
		t.Fatalf("complete: %v", err)
	}
	replay, err = BeginIdempotentRequest("default", credentialID, key, "POST", "/api/public/v1/orders", body)
	if err != nil {
		t.Fatalf("replay claim: %v", err)
	}
	if replay == nil || replay.StatusCode != 201 || replay.Body != `{"id":"SO-1"}` {
		t.Fatalf("replay = %#v, want the stored 201 response", replay)
	}

	// The same key with a different body is a caller bug, not a retry. Replaying
	// would answer a question they did not ask.
	if _, err := BeginIdempotentRequest("default", credentialID, key, "POST", "/api/public/v1/orders", []byte(`{"order":"A-2"}`)); err != ErrIdempotencyKeyReused {
		t.Fatalf("reused-key error = %v, want ErrIdempotencyKeyReused", err)
	}
	if _, err := BeginIdempotentRequest("default", credentialID, "  ", "POST", "/api/public/v1/orders", body); err != ErrIdempotencyKeyRequired {
		t.Fatalf("blank-key error = %v, want ErrIdempotencyKeyRequired", err)
	}

	// A 5xx must not be cached: the caller retrying after a server error needs a
	// real second attempt, not a permanently stored failure.
	const failKey = "TEST-IDEMPOTENCY-KEY-5XX"
	if _, err := BeginIdempotentRequest("default", credentialID, failKey, "POST", "/api/public/v1/orders", body); err != nil {
		t.Fatalf("claim before failure: %v", err)
	}
	if err := CompleteIdempotentRequest("default", credentialID, failKey, 500, `{"error":"boom"}`); err != nil {
		t.Fatalf("complete with 500: %v", err)
	}
	replay, err = BeginIdempotentRequest("default", credentialID, failKey, "POST", "/api/public/v1/orders", body)
	if err != nil || replay != nil {
		t.Fatalf("after a 500 the key = %v, %v; want it released for a genuine retry", replay, err)
	}

	// 38.9: the traffic log records metadata and the reader returns it.
	RecordPublicAPIRequest("default", PublicAPILogEntry{
		CredentialID: credentialID, KeyPrefix: "testprefix", Method: "GET",
		Path: "/api/public/v1/items", RequiredScope: "items:read", StatusCode: 200,
		DurationMS: 12, CorrelationID: "test-correlation", ClientIP: "127.0.0.1", Outcome: "handled",
	})
	entries, err := ListPublicAPITraffic("default", credentialID, 10)
	if err != nil {
		t.Fatalf("list traffic: %v", err)
	}
	if len(entries) != 1 || entries[0].Path != "/api/public/v1/items" || entries[0].Outcome != "handled" {
		t.Fatalf("traffic entries = %#v, want the single recorded call", entries)
	}
	if entries[0].RequiredScope != "items:read" || entries[0].StatusCode != 200 {
		t.Fatalf("traffic entry lost its metadata: %#v", entries[0])
	}

	// The sweeper is bounded by the configured retention, so a row recorded a
	// moment ago must survive it.
	if _, _, err := SweepPublicAPIRuntime("default"); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	entries, err = ListPublicAPITraffic("default", credentialID, 10)
	if err != nil || len(entries) != 1 {
		t.Fatalf("after sweep entries = %d, %v; want the fresh row kept", len(entries), err)
	}
}

func TestPublicAPIBudgetAdmission(t *testing.T) {
	limiter := &publicAPIBurstLimiter{windows: make(map[string][]time.Time)}
	for i := 0; i < 3; i++ {
		allowed, remaining, _ := limiter.allow("cred", 3, time.Minute)
		if !allowed {
			t.Fatalf("request %d was rejected inside the budget", i+1)
		}
		if want := 2 - i; remaining != want {
			t.Fatalf("remaining after request %d = %d, want %d", i+1, remaining, want)
		}
	}
	allowed, remaining, retryAfter := limiter.allow("cred", 3, time.Minute)
	if allowed || remaining != 0 || retryAfter <= 0 {
		t.Fatalf("over-budget request = allowed:%v remaining:%d retryAfter:%v; want a rejection with a wait", allowed, remaining, retryAfter)
	}
	// A different credential has its own bucket - one noisy integration must
	// never draw down another's budget.
	if allowed, _, _ := limiter.allow("other-cred", 3, time.Minute); !allowed {
		t.Fatal("a second credential was rejected by the first credential's spent budget")
	}
	limiter.forget("cred")
	if allowed, _, _ := limiter.allow("cred", 3, time.Minute); !allowed {
		t.Fatal("forget() did not release the credential's window")
	}
}

func TestPublicAPIPagingIsBounded(t *testing.T) {
	if limit, offset := NormalizePublicPaging(0, -5); limit != 50 || offset != 0 {
		t.Fatalf("defaults = %d/%d, want 50/0", limit, offset)
	}
	if limit, _ := NormalizePublicPaging(100000, 0); limit != 200 {
		t.Fatalf("limit = %d, want it clamped to 200 - an unbounded public read is a denial-of-service invitation", limit)
	}
}

func TestPublicItemProjectionExcludesInternalFields(t *testing.T) {
	// The projection is the contract. This asserts the negative case that
	// matters: a public product read must not carry cost or margin, whatever
	// gets added to the Item doctype later.
	for _, forbidden := range []string{"cost", "margin", "purchase_price", "supplier", "vendor"} {
		if strings.Contains(strings.ToLower(publicItemSelect), forbidden) {
			t.Fatalf("the public item projection selects %q - internal commercial data must not reach an integration key", forbidden)
		}
	}
}
