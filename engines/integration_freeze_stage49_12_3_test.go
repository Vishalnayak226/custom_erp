package engines

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Stage 49.12.3 (a): the per-client integration freeze. One switch stops this
// client's connector / courier calls (reads too), webhook deliveries and hook
// calls, with a reason; another client is not touched; unfreezing restores
// them.
func TestStage49123IntegrationFreezeStopsOutboundCallsForOneClient(t *testing.T) {
	tenantID, schema := outsideCallsTenant(t)
	outsideCallsEnv(t, "production", "", "")
	const key = "integration-freeze-49-12-3"
	resetBreaker(key)
	t.Cleanup(func() { resetBreaker(key) })
	var hits atomic.Int32
	local := countingServer(t, &hits)
	ctx := WithOutsideCallsTenant(context.Background(), tenantID)

	if ok, _ := OutsideCallsAllowed(schema); !ok {
		t.Fatal("a client that is not frozen must be allowed")
	}
	if err := SetIntegrationFreeze(tenantID, true, "tester", "x"); err == nil {
		t.Fatal("a freeze needs a reason of at least 5 characters")
	}
	if err := SetIntegrationFreeze(tenantID, true, "tester", "suspected key leak"); err != nil {
		t.Fatalf("freeze: %v", err)
	}

	if ok, reason := OutsideCallsAllowed(schema); ok || !strings.Contains(reason, "frozen") {
		t.Fatalf("frozen: OutsideCallsAllowed = %v %q", ok, reason)
	}
	// A connector write and read are both refused; nothing reaches the server.
	for _, method := range []string{http.MethodPost, http.MethodGet} {
		_, _, err := doConnectorRequest(ctx, 2*time.Second, method, local.URL, nil, []byte(`{}`), key)
		wantConn0225(t, "frozen "+method, err, "frozen")
	}
	if hits.Load() != 0 {
		t.Fatalf("frozen: a call reached the server (hits %d)", hits.Load())
	}
	// A webhook delivery job is not sent.
	job := Job{ID: "freeze-49123", JobType: "webhook_delivery", Payload: map[string]interface{}{"url": local.URL, "event_name": "order.created", "event_payload": map[string]interface{}{}}}
	res, err := deliverWebhook(context.Background(), schema, job)
	if err != nil || res["simulated"] != true || !strings.Contains(res["reason"].(string), "frozen") {
		t.Fatalf("frozen: webhook want simulated naming the freeze, got %v %v", res, err)
	}
	// Another client is unaffected.
	if ok, _ := OutsideCallsAllowed("tenant_default"); !ok {
		t.Fatal("another client must not be frozen with this one")
	}
	if status, _, err := doConnectorRequest(WithOutsideCallsTenant(context.Background(), "default"), 2*time.Second, http.MethodPost, local.URL, nil, []byte(`{}`), key); err != nil || status != http.StatusOK || hits.Load() != 1 {
		t.Fatalf("another client: want the write to go, got %d %v (hits %d)", status, err, hits.Load())
	}

	// Unfrozen, the client's calls go again.
	if err := SetIntegrationFreeze(tenantID, false, "tester", "all clear again"); err != nil {
		t.Fatalf("unfreeze: %v", err)
	}
	if status, _, err := doConnectorRequest(ctx, 2*time.Second, http.MethodPost, local.URL, nil, []byte(`{}`), key); err != nil || status != http.StatusOK {
		t.Fatalf("unfrozen: want the write to go, got %d %v", status, err)
	}
}
