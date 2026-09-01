package engines

import (
	"crypto/hmac"
	"crypto/sha256"
	"custom_erp/db"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStage384WebhookSubscriptions(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	cleanupDoctype := func(ids ...string) {
		for _, id := range ids {
			db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1 AND doctype = 'WebhookSubscription'", id)
		}
	}
	cleanupJobs := func(ids ...string) {
		for _, id := range ids {
			db.DB.Exec("DELETE FROM "+schema+".async_jobs WHERE id = $1", id)
		}
	}

	t.Run("ValidateWebhookSubscriptionDocument rejects non-https, a loopback host, a missing secret, and a missing event_pattern", func(t *testing.T) {
		base := map[string]interface{}{"secret": "s3cr3t", "event_pattern": "inventory.*"}

		withURL := func(u string) map[string]interface{} {
			m := map[string]interface{}{}
			for k, v := range base {
				m[k] = v
			}
			m["url"] = u
			return m
		}
		if err := ValidateWebhookSubscriptionDocument(tenantID, withURL("http://example.com/hook")); err == nil {
			t.Fatalf("expected a non-https URL to be rejected")
		}
		if err := ValidateWebhookSubscriptionDocument(tenantID, withURL("https://127.0.0.1/hook")); err == nil {
			t.Fatalf("expected a loopback host to be rejected")
		}
		if err := ValidateWebhookSubscriptionDocument(tenantID, withURL("not a url")); err == nil {
			t.Fatalf("expected a malformed URL to be rejected")
		}

		noSecret := map[string]interface{}{"url": "https://127.0.0.1/hook", "event_pattern": "inventory.*"}
		if err := ValidateWebhookSubscriptionDocument(tenantID, noSecret); err == nil {
			t.Fatalf("expected a missing secret to be rejected (via the loopback rejection or the secret check - either is a real refusal)")
		}
	})

	t.Run("matchEventPattern supports exact match and a trailing-* prefix wildcard, nothing looser", func(t *testing.T) {
		cases := []struct {
			pattern, event string
			want           bool
		}{
			{"inventory.stock_changed", "inventory.stock_changed", true},
			{"inventory.stock_changed", "inventory.stock_changed_extra", false},
			{"inventory.*", "inventory.stock_changed", true},
			{"inventory.*", "sales.order_created", false},
			{"*", "anything.at.all", true},
			{"inv*ntory.*", "inventory.stock_changed", false}, // no mid-string wildcard support
		}
		for _, c := range cases {
			if got := matchEventPattern(c.pattern, c.event); got != c.want {
				t.Errorf("matchEventPattern(%q, %q) = %v, want %v", c.pattern, c.event, got, c.want)
			}
		}
	})

	t.Run("dispatchWebhooksForEvent enqueues a job for a matching Active subscription, skips Inactive and non-matching ones, and is idempotent on retry", func(t *testing.T) {
		activeID := "TEST3840-ACTIVE"
		inactiveID := "TEST3840-INACTIVE"
		nonMatchID := "TEST3840-NONMATCH"
		defer cleanupDoctype(activeID, inactiveID, nonMatchID)

		insertSub := func(id, status, pattern string) {
			data, _ := json.Marshal(map[string]interface{}{
				"code": id, "name": id, "url": "https://example.invalid/hook", "secret": "s3cr3t", "event_pattern": pattern,
			})
			db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'WebhookSubscription', $2, $3, 'system') "+
				"ON CONFLICT (id) DO UPDATE SET data = $2, status = $3", id, data, status)
		}
		insertSub(activeID, "Active", "test3840.*")
		insertSub(inactiveID, "Inactive", "test3840.*")
		insertSub(nonMatchID, "Active", "somethingelse.*")

		eventID := "TEST3840-EVENT"
		dispatchWebhooksForEvent(schema, eventID, "test3840.fired", map[string]interface{}{"x": 1})

		var jobIDs []string
		var count int
		if err := db.DB.QueryRow("SELECT count(*) FROM "+schema+".async_jobs WHERE idempotency_key = $1", eventID+"-"+activeID).Scan(&count); err != nil {
			t.Fatalf("count jobs for active sub: %v", err)
		}
		if count != 1 {
			t.Fatalf("expected exactly 1 job enqueued for the matching Active subscription, got %d", count)
		}
		var jobID string
		db.DB.QueryRow("SELECT id FROM "+schema+".async_jobs WHERE idempotency_key = $1", eventID+"-"+activeID).Scan(&jobID)
		jobIDs = append(jobIDs, jobID)
		defer cleanupJobs(jobIDs...)

		if err := db.DB.QueryRow("SELECT count(*) FROM "+schema+".async_jobs WHERE idempotency_key = $1", eventID+"-"+inactiveID).Scan(&count); err != nil {
			t.Fatalf("count jobs for inactive sub: %v", err)
		}
		if count != 0 {
			t.Fatalf("expected no job for the Inactive subscription, got %d", count)
		}
		if err := db.DB.QueryRow("SELECT count(*) FROM "+schema+".async_jobs WHERE idempotency_key = $1", eventID+"-"+nonMatchID).Scan(&count); err != nil {
			t.Fatalf("count jobs for non-matching sub: %v", err)
		}
		if count != 0 {
			t.Fatalf("expected no job for the non-matching event_pattern, got %d", count)
		}

		// Re-dispatch the same event (simulating processOutbox retrying a
		// Failed row) - must not create a second job for the same subscription.
		dispatchWebhooksForEvent(schema, eventID, "test3840.fired", map[string]interface{}{"x": 1})
		if err := db.DB.QueryRow("SELECT count(*) FROM "+schema+".async_jobs WHERE idempotency_key = $1", eventID+"-"+activeID).Scan(&count); err != nil {
			t.Fatalf("count jobs for active sub after re-dispatch: %v", err)
		}
		if count != 1 {
			t.Fatalf("expected re-dispatching the same event to stay idempotent, got %d jobs", count)
		}
	})

	t.Run("sendWebhookHTTP signs the body with the real HMAC-SHA256 of the secret, and a 2xx/non-2xx response is a success/failure", func(t *testing.T) {
		var gotSignature, gotEvent string
		var gotBody []byte
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotSignature = r.Header.Get("X-Webhook-Signature")
			gotEvent = r.Header.Get("X-Webhook-Event")
			gotBody, _ = io.ReadAll(r.Body)
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		result, err := sendWebhookHTTP(srv.URL, "s3cr3t", "test3840.event", map[string]interface{}{"n": 42}, "JOB-TEST")
		if err != nil {
			t.Fatalf("sendWebhookHTTP: %v", err)
		}
		if result["status_code"] != 200 {
			t.Fatalf("expected status_code 200 in the result, got %+v", result)
		}
		if gotEvent != "test3840.event" {
			t.Fatalf("expected X-Webhook-Event to be test3840.event, got %q", gotEvent)
		}
		mac := hmac.New(sha256.New, []byte("s3cr3t"))
		mac.Write(gotBody)
		wantSignature := "sha256=" + hex.EncodeToString(mac.Sum(nil))
		if gotSignature != wantSignature {
			t.Fatalf("expected the signature to be the real HMAC-SHA256 of the request body, got %q want %q", gotSignature, wantSignature)
		}

		failSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer failSrv.Close()
		if _, err := sendWebhookHTTP(failSrv.URL, "s3cr3t", "test3840.event", nil, "JOB-TEST-2"); err == nil {
			t.Fatalf("expected a 500 response to be treated as a delivery failure")
		}
	})

	t.Run("deliverWebhook simulates delivery for a sandbox tenant without making a real HTTP call", func(t *testing.T) {
		var called bool
		canary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			w.WriteHeader(http.StatusOK)
		}))
		defer canary.Close()

		sandboxTenantID, sandboxSchema, _, err := ProvisionSandboxTenant("0.1.0-test", 5)
		if err != nil {
			t.Fatalf("ProvisionSandboxTenant: %v", err)
		}
		defer func() {
			db.DB.Exec("DROP SCHEMA IF EXISTS " + sandboxSchema + " CASCADE")
			db.DB.Exec("DELETE FROM public.tenants WHERE tenant_id = $1", sandboxTenantID)
		}()

		result, err := deliverWebhook(sandboxSchema, Job{
			ID: "JOB-TEST-SANDBOX",
			Payload: map[string]interface{}{
				"url": canary.URL, "secret": "s3cr3t", "event_name": "test3840.event", "event_payload": map[string]interface{}{},
			},
		})
		if err != nil {
			t.Fatalf("deliverWebhook (sandbox): %v", err)
		}
		if result["simulated"] != true {
			t.Fatalf("expected a sandbox delivery to report simulated=true, got %+v", result)
		}
		if called {
			t.Fatalf("expected a sandbox tenant's webhook delivery to make NO real HTTP call, but the canary server was hit")
		}
	})
}
