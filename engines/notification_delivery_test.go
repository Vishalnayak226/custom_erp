package engines

import (
	"context"
	"custom_erp/db"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// Stage 73.2.13: a notification is never "Sent" without evidence. Each
// provider answer maps to one state; a retryable failure retries, the last
// one says Failed; a delivery receipt can confirm but never overturn
// Delivered; and a message the provider already has is never sent again.
func TestNotificationDeliveryStateFromProviderEvidence(t *testing.T) {
	t.Run("provider answers map to states", func(t *testing.T) {
		cases := []struct {
			code      int
			body      string
			wantState string
			wantRef   string
			retryable bool
		}{
			{200, `{"status":"delivered","message_id":"P-1"}`, "Delivered", "P-1", false},
			{202, ``, "Accepted", "", false},
			{200, `{"status":"queued","id":"P-2"}`, "Accepted", "P-2", false},
			{200, `{"status":"bounced"}`, "Failed", "", false},
			{400, `{"error":"bad number"}`, "Failed", "", false},
			{503, ``, "", "", true},
			{429, ``, "", "", true},
		}
		for _, c := range cases {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("X-Notification-Delivery") != "NL-T" {
					t.Errorf("delivery id header missing")
				}
				w.WriteHeader(c.code)
				_, _ = w.Write([]byte(c.body))
			}))
			result, err := sendNotificationHTTP(context.Background(), srv.URL, notificationWebhookPayload{DeliveryID: "NL-T", Event: "Order Placed"})
			srv.Close()
			_, isRetryable := err.(*errNotificationRetryable)
			if isRetryable != c.retryable {
				t.Errorf("HTTP %d: retryable=%v, want %v (err %v)", c.code, isRetryable, c.retryable, err)
				continue
			}
			if !c.retryable && (result.State != c.wantState || result.ProviderRef != c.wantRef) {
				t.Errorf("HTTP %d %s: got state %q ref %q, want %q %q", c.code, c.body, result.State, result.ProviderRef, c.wantState, c.wantRef)
			}
		}
	})

	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	logData := func(id string) map[string]interface{} {
		t.Helper()
		var raw []byte
		var status string
		if err := db.DB.QueryRow("SELECT data, status FROM "+schema+".documents WHERE id = $1", id).Scan(&raw, &status); err != nil {
			t.Fatalf("read log %s: %v", id, err)
		}
		var data map[string]interface{}
		_ = json.Unmarshal(raw, &data)
		if data["dispatch_status"] != status {
			t.Fatalf("row status %q and dispatch_status %v disagree", status, data["dispatch_status"])
		}
		return data
	}
	queue := func() (string, Job) {
		t.Helper()
		if err := queueNotificationDelivery(schema, "NT-1", "https://example.invalid/hook", notificationWebhookPayload{Event: "Order Placed", Channel: "SMS", OrderID: "SO-1", Body: "hi"}); err != nil {
			t.Fatalf("queue: %v", err)
		}
		var id, jobID string
		if err := db.DB.QueryRow("SELECT id, data->>'job_id' FROM "+schema+".documents WHERE doctype = 'NotificationLog' ORDER BY created_at DESC, id DESC LIMIT 1").Scan(&id, &jobID); err != nil {
			t.Fatalf("find log: %v", err)
		}
		var payloadRaw []byte
		var idem string
		if err := db.DB.QueryRow("SELECT payload, idempotency_key FROM "+schema+".async_jobs WHERE id = $1", jobID).Scan(&payloadRaw, &idem); err != nil {
			t.Fatalf("queued log %s has no delivery job %q: %v", id, jobID, err)
		}
		if idem != id {
			t.Fatalf("job idempotency key %q, want the log id %q", idem, id)
		}
		var payload map[string]interface{}
		_ = json.Unmarshal(payloadRaw, &payload)
		return id, Job{ID: jobID, JobType: notificationDeliveryJobType, Payload: payload, MaxAttempts: 3}
	}

	t.Run("queued with its job, retries, then fails on the last attempt", func(t *testing.T) {
		id, _ := queue()
		if got := logData(id)["dispatch_status"]; got != "Queued" {
			t.Fatalf("new notification is %v, want Queued", got)
		}
		retry := &errNotificationRetryable{msg: "provider answered HTTP 503"}
		if _, err := applyNotificationAttempt(schema, id, 1, 3, map[string]interface{}{"attempts": 1}, notificationProviderResult{HTTPStatus: 503}, retry); err == nil {
			t.Fatal("a retryable failure must return its error so the runner backs off")
		}
		if d := logData(id); d["dispatch_status"] != "Retrying" || d["attempts"].(float64) != 1 {
			t.Fatalf("after attempt 1: %v / %v, want Retrying / 1", d["dispatch_status"], d["attempts"])
		}
		_, _ = applyNotificationAttempt(schema, id, 3, 3, map[string]interface{}{"attempts": 3}, notificationProviderResult{}, retry)
		d := logData(id)
		if d["dispatch_status"] != "Failed" || d["final_at"] == nil {
			t.Fatalf("after the last attempt: %v, want Failed with final_at", d["dispatch_status"])
		}
		if h, _ := d["history"].([]interface{}); len(h) != 3 {
			t.Fatalf("history has %d entries, want Queued, Retrying, Failed", len(h))
		}
		// A provider that delivered after all is better evidence than our give-up.
		if res, err := RecordNotificationDeliveryStatus(tenantID, id, "delivered", "P-9", "", "provider:test"); err != nil || res["changed"] != true {
			t.Fatalf("Failed -> Delivered receipt: %v %v", res, err)
		}
		if d := logData(id); d["dispatch_status"] != "Delivered" || d["provider_ref"] != "P-9" {
			t.Fatalf("after receipt: %v ref %v", d["dispatch_status"], d["provider_ref"])
		}
		if res, err := RecordNotificationDeliveryStatus(tenantID, id, "DELIVERED", "", "", "provider:test"); err != nil || res["changed"] != false {
			t.Fatalf("a repeated receipt must be a no-op: %v %v", res, err)
		}
		_, err := RecordNotificationDeliveryStatus(tenantID, id, "failed", "", "", "provider:test")
		if verr, ok := err.(*ValidationError); !ok || verr.Code != "NOTIFI-0463" {
			t.Fatalf("Delivered must be final, got %v", err)
		}
	})

	t.Run("a message the provider has is never sent again", func(t *testing.T) {
		id, job := queue()
		if _, err := applyNotificationAttempt(schema, id, 1, 3, map[string]interface{}{"attempts": 1}, notificationProviderResult{HTTPStatus: 202, State: "Accepted", Detail: "accepted"}, nil); err != nil {
			t.Fatal(err)
		}
		res, err := deliverNotification(context.Background(), schema, job)
		if err != nil || res["skipped"] == nil {
			t.Fatalf("replaying an Accepted notification must skip the send: %v %v", res, err)
		}
	})

	t.Run("no real call when side effects are off, and no false Sent", func(t *testing.T) {
		old := os.Getenv("ERP_DISABLE_EXTERNAL_SIDE_EFFECTS")
		defer os.Setenv("ERP_DISABLE_EXTERNAL_SIDE_EFFECTS", old)
		os.Setenv("ERP_DISABLE_EXTERNAL_SIDE_EFFECTS", "1")
		if ExternalSideEffectsEnabled() {
			t.Skip("side effects cannot be switched off in this environment")
		}
		id, job := queue()
		if _, err := deliverNotification(context.Background(), schema, job); err != nil {
			t.Fatal(err)
		}
		if got := logData(id)["dispatch_status"]; got != "Skipped-SideEffectsOff" {
			t.Fatalf("got %v, want Skipped-SideEffectsOff", got)
		}
		_, err := RecordNotificationDeliveryStatus(tenantID, id, "delivered", "", "", "provider:test")
		if verr, ok := err.(*ValidationError); !ok || verr.Code != "NOTIFI-0463" {
			t.Fatalf("a never-sent notification cannot be confirmed, got %v", err)
		}
		_, err = RecordNotificationDeliveryStatus(tenantID, "NL-NOPE", "delivered", "", "", "provider:test")
		if verr, ok := err.(*ValidationError); !ok || verr.Code != "NOTIFI-0462" {
			t.Fatalf("unknown id: got %v", err)
		}
	})
}
