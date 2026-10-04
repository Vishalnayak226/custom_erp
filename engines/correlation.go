package engines

import "context"

// BLD-052: one correlation identity carried from an HTTP request through the
// transaction it runs, into the durable async work it queues (async_jobs,
// integration_event_outbox) and on into whatever that work logs when it
// eventually runs.
//
// Before this, correlation stopped at the request boundary. middleware.go
// minted an id per request, writeAPIError returned it to the user, and
// system_error_logs stored it - so a user quoting their correlation id could
// be matched to the error they saw. But a job enqueued by that request
// carried nothing, so an async failure at 03:00 had no path back to the
// request that caused it, and the outbox event a user's action produced
// could not be connected to the action.
//
// Carried on the context rather than threaded as a parameter through every
// enqueue/outbox call site: the id is ambient request identity, not an
// argument any of those functions make a decision on, and the ctx plumbing
// BLD-046 added for cancellation already reaches the same places. A caller
// with no correlation in its context (a background sweep, a test) writes an
// empty string, which is exactly what the column defaults to.

type correlationIDKey struct{}

// WithCorrelationID returns a context carrying this request's correlation id.
// An empty id is not stored, so a caller cannot accidentally overwrite a real
// correlation with a blank one.
func WithCorrelationID(ctx context.Context, correlationID string) context.Context {
	if correlationID == "" {
		return ctx
	}
	return context.WithValue(ctx, correlationIDKey{}, correlationID)
}

// CorrelationIDFromContext returns the correlation id carried by ctx, or "" if
// there is none. Never panics on a nil or bare context: most callers are
// background workers that legitimately have no request identity.
func CorrelationIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	id, _ := ctx.Value(correlationIDKey{}).(string)
	return id
}

// correlationIDMaxBytes bounds what is stored and logged. Today every id is a
// server-minted UUID (36 bytes): middleware.go calls generateUUID() and does
// not honour an inbound X-Correlation-ID, so nothing here is attacker-
// controlled as the code stands.
//
// SafeCorrelationID exists anyway, because the value now reaches a log line
// and two indexed database columns, and the one plausible future change -
// accepting a caller-supplied id so a client can trace a request across
// systems - would silently turn all three into untrusted sinks. Bounding and
// sanitizing at the single writer is a few lines now; retrofitting it across
// every column and log statement later is not.
const correlationIDMaxBytes = 64

// SafeCorrelationID bounds and sanitizes a correlation id for storage and for
// log output. Keeps only characters that cannot break a log line or smuggle
// terminal control sequences - the same reason alerting.go redacts before it
// sends. This is the single choke point every correlation writer goes
// through, so a hostile X-Correlation-ID is neutralised once rather than at
// each column and each log statement.
func SafeCorrelationID(correlationID string) string {
	if correlationID == "" {
		return ""
	}
	if len(correlationID) > correlationIDMaxBytes {
		correlationID = correlationID[:correlationIDMaxBytes]
	}
	out := make([]rune, 0, len(correlationID))
	for _, r := range correlationID {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			out = append(out, r)
		case r == '-', r == '_', r == '.', r == ':':
			out = append(out, r)
		}
	}
	return string(out)
}
