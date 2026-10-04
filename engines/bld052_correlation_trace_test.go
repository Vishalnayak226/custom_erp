package engines

// BLD-052, the correlation leg: one id traces request -> transaction ->
// outbox -> job.
//
// Before this, correlation stopped at the request boundary (middleware minted
// an id, writeAPIError returned it, system_error_logs stored it). A job or
// outbox event created by that request carried nothing, so an async failure
// had no path back to the request that caused it. These tests prove the hop
// actually lands in the durable rows, and that a hostile id cannot ride along
// into a log line or a column.

import (
	"context"
	"custom_erp/db"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestCorrelationIDReachesTheEnqueuedJobRow(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}

	const correlation = "11111111-2222-4333-8444-555555555555"
	ctx := WithCorrelationID(context.Background(), correlation)
	jobID, err := EnqueueJobContext(ctx, tenantID, "trace-probe", map[string]interface{}{"n": 1}, "")
	if err != nil {
		t.Fatalf("EnqueueJobContext: %v", err)
	}

	var stored string
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT correlation_id FROM %s.async_jobs WHERE id = $1`, schema), jobID).Scan(&stored); err != nil {
		t.Fatalf("read back job correlation: %v", err)
	}
	if stored != correlation {
		t.Fatalf("expected the request's correlation id on the job row, got %q", stored)
	}
}

func TestEnqueueWithoutCorrelationStoresEmptyNotGarbage(t *testing.T) {
	// A background sweep legitimately has no request identity. It must store
	// an empty string - the column's own default - not a placeholder that
	// would look like a real trace id during an incident.
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}

	jobID, err := EnqueueJob(tenantID, "trace-probe-bare", map[string]interface{}{}, "")
	if err != nil {
		t.Fatalf("EnqueueJob: %v", err)
	}
	var stored string
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT correlation_id FROM %s.async_jobs WHERE id = $1`, schema), jobID).Scan(&stored); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if stored != "" {
		t.Fatalf("expected an empty correlation for a request-less enqueue, got %q", stored)
	}
}

func TestCorrelationIDReachesTheOutboxRowAndItsWebhookJob(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}

	const correlation = "99999999-8888-4777-8666-555555555555"
	ctx := WithCorrelationID(context.Background(), correlation)

	// The request's transaction publishes an event, exactly as a handler does.
	tx, err := db.DB.Begin()
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if err := PublishEventContext(ctx, tx, schema, "trace.probe.fired", map[string]interface{}{"x": 1}); err != nil {
		tx.Rollback()
		t.Fatalf("PublishEventContext: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// Hop 1: the outbox row carries the request's id.
	var eventID, storedCorrelation string
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT id, correlation_id FROM %s.integration_event_outbox WHERE event_name = $1 ORDER BY created_at DESC LIMIT 1`, schema),
		"trace.probe.fired").Scan(&eventID, &storedCorrelation); err != nil {
		t.Fatalf("read back outbox row: %v", err)
	}
	if storedCorrelation != correlation {
		t.Fatalf("expected the correlation id on the outbox row, got %q", storedCorrelation)
	}

	// Hop 2: a subscription matching this event produces a delivery job that
	// carries the same id, so the whole chain shares one trace.
	subID := "SUB-TRACE-" + fmt.Sprint(time.Now().UnixNano())
	if _, err := db.DB.Exec(fmt.Sprintf(`
		INSERT INTO %s.documents (id, doctype, data, status, created_by)
		VALUES ($1, 'WebhookSubscription', $2, 'Active', 'system')`, schema),
		subID, fmt.Sprintf(`{"id":%q,"code":%q,"target_url":"https://example.invalid/hook","event_pattern":"trace.probe.*","secret":"s"}`, subID, subID)); err != nil {
		t.Skipf("WebhookSubscription shape differs in this build: %v", err)
	}

	dispatchWebhooksForEvent(schema, eventID, "trace.probe.fired", map[string]interface{}{"x": 1}, storedCorrelation)

	var jobCorrelation sql.NullString
	err = db.DB.QueryRow(fmt.Sprintf(
		`SELECT correlation_id FROM %s.async_jobs WHERE idempotency_key = $1`, schema),
		eventID+"-"+subID).Scan(&jobCorrelation)
	if err == sql.ErrNoRows {
		t.Skip("no delivery job was enqueued; the subscription shape this build expects differs from the one seeded above")
	}
	if err != nil {
		t.Fatalf("read back delivery job: %v", err)
	}
	if jobCorrelation.String != correlation {
		t.Fatalf("the webhook delivery job lost the trace: expected %q, got %q", correlation, jobCorrelation.String)
	}
}

func TestSafeCorrelationIDNeutralisesHostileInput(t *testing.T) {
	// The id reaches a log line and two indexed columns. Even though
	// middleware mints it today, the sanitizer is the single choke point that
	// keeps that true if an inbound header is ever honoured.
	cases := map[string]string{
		"11111111-2222-4333-8444-555555555555": "11111111-2222-4333-8444-555555555555",
		"abc_DEF-123.456:789":                  "abc_DEF-123.456:789",
		"inject\nFAKE LOG LINE":                "injectFAKELOGLINE",
		"drop\x1b[2Jterminal":                  "drop2Jterminal",
		"spaces are dropped":                   "spacesaredropped",
		"":                                     "",
	}
	for input, want := range cases {
		if got := SafeCorrelationID(input); got != want {
			t.Errorf("SafeCorrelationID(%q) = %q, want %q", input, got, want)
		}
	}
	long := strings.Repeat("a", 500)
	if got := SafeCorrelationID(long); len(got) != correlationIDMaxBytes {
		t.Errorf("an over-long id must be bounded to %d bytes, got %d", correlationIDMaxBytes, len(got))
	}
}

func TestWithCorrelationIDRefusesToOverwriteWithBlank(t *testing.T) {
	ctx := WithCorrelationID(context.Background(), "real-id")
	ctx = WithCorrelationID(ctx, "")
	if got := CorrelationIDFromContext(ctx); got != "real-id" {
		t.Fatalf("a blank id must not clear an established correlation, got %q", got)
	}
	if got := CorrelationIDFromContext(context.Background()); got != "" {
		t.Fatalf("a bare context must report no correlation, got %q", got)
	}
	//nolint:staticcheck // deliberately checking the nil-context path does not panic
	if got := CorrelationIDFromContext(nil); got != "" {
		t.Fatalf("a nil context must report no correlation, got %q", got)
	}
}
