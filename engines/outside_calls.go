package engines

import (
	"context"
	"custom_erp/db"
	"net/http"
	"net/url"
	"sync"
)

// Stage 49.8.7a: "Outside calls" - one answer to whether a call that leaves
// this server may be made for a client. Every place that makes one asks it:
// connectors and couriers (doConnectorRequest), extension hooks, webhooks,
// notification webhooks, Web Push, CRM sends, emailed documents and
// dashboard digests. Three switches, strongest first:
//
//   - ERP_DISABLE_EXTERNAL_SIDE_EFFECTS=1, the emergency switch, any
//     environment (ExternalSideEffectsKilled).
//   - The client's own Platform > Outside calls setting (SettingOutsideCalls,
//     on as shipped). Off stops that client's outside calls and no other's.
//   - The environment: off unless ENV=production or
//     ERP_ENABLE_EXTERNAL_SIDE_EFFECTS=1, so a developer's copy never reaches
//     a real platform, courier or recipient.
//
// Not covered, on purpose: platform security mail (password reset, sign-in
// codes) and the operator's ops alerts keep ExternalSideEffectsEnabled()
// alone - a client cannot switch off its own users' account recovery, nor
// the operator's alarms.

// SettingOutsideCalls is the per-client switch.
const SettingOutsideCalls = "platform.outside_calls"

type outsideCallsState int

const (
	outsideCallsOn outsideCallsState = iota
	outsideCallsKilled
	outsideCallsClientOff
	outsideCallsDevelopment
)

func (s outsideCallsState) reason() string {
	switch s {
	case outsideCallsKilled:
		return "outside calls are switched off on this server (emergency switch)"
	case outsideCallsClientOff:
		return "outside calls are off for this client (Settings > Configuration > Platform > Outside calls)"
	case outsideCallsDevelopment:
		return "outside calls are off in development (ERP_ENABLE_EXTERNAL_SIDE_EFFECTS=1 turns them on)"
	}
	return ""
}

// outsideCallsClientIsOff reads the client's setting - cached per schema by
// the settings registry, so asking on every call costs a map lookup. ""
// (no client known) is never off.
func outsideCallsClientIsOff(schema string) bool {
	return schema != "" && !GetSettingBoolForSchema(schema, SettingOutsideCalls)
}

func outsideCallsFor(schema string) outsideCallsState {
	switch {
	case ExternalSideEffectsKilled():
		return outsideCallsKilled
	case outsideCallsClientIsOff(schema):
		return outsideCallsClientOff
	case !externalSideEffectsByEnvironment():
		return outsideCallsDevelopment
	}
	return outsideCallsOn
}

// OutsideCallsAllowed reports whether an outside call may be made for the
// client whose schema this is ("" = no client known: the server's switches
// only) and, when not, why - one sentence for the log, a job's result or the
// person. A caller that is refused simulates or refuses the call the way it
// already does for a sandbox client.
func OutsideCallsAllowed(schema string) (bool, string) {
	s := outsideCallsFor(schema)
	return s == outsideCallsOn, s.reason()
}

// OutsideCallsAllowedForTenant is OutsideCallsAllowed for a caller holding a
// tenant id.
func OutsideCallsAllowedForTenant(tenantID string) (bool, string) {
	return OutsideCallsAllowed(outsideCallsSchemaOf(tenantID))
}

func outsideCallsSchemaOf(tenantID string) string {
	if db.DB == nil {
		return ""
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return ""
	}
	return schema
}

// outsideCallRefusal is the gate for a call to one address - connectors and
// couriers (doConnectorRequest) and extension hooks. "" lets it go;
// otherwise it is why not.
//
//   - Under the emergency switch or the client's Off, a call that could
//     change something (anything but GET/HEAD) is refused, a local one too;
//     reads still go, so orders and tracking can be reconciled (49.8.7).
//   - In development every call to another machine is refused, reads too: a
//     copy of a client's data must not pull its live orders or spend its API
//     budget. Loopback (localhost, 127.0.0.1, ::1) is not an outside call -
//     test doubles and local sandboxes are still reached.
func outsideCallRefusal(schema, method, rawURL string) string {
	write := method != http.MethodGet && method != http.MethodHead
	switch {
	case write && ExternalSideEffectsKilled():
		return outsideCallsKilled.reason()
	case write && outsideCallsClientIsOff(schema):
		return outsideCallsClientOff.reason()
	case !externalSideEffectsByEnvironment() && !loopbackURL(rawURL):
		return outsideCallsDevelopment.reason()
	}
	return ""
}

func loopbackURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	return isLoopbackHost(u.Hostname())
}

// The client an outbound call is made for travels on its context: a
// connector or courier adapter is handed a context and credentials, not a
// schema. The API middleware names the signed-in client on every request,
// the job runner the schema a job belongs to, and the background loops that
// call a platform themselves (channel sync, PIM publish, return pickups)
// their own. A context naming no client gets the server's switches only.
type outsideCallsClientKey struct{}

type outsideCallsClient struct {
	tenantID string
	once     sync.Once
	schema   string
}

// WithOutsideCallsTenant names the client (by tenant id) the outside calls
// made under ctx are for. The schema is looked up once, at the first call.
func WithOutsideCallsTenant(ctx context.Context, tenantID string) context.Context {
	if tenantID == "" {
		return ctx
	}
	return context.WithValue(ctx, outsideCallsClientKey{}, &outsideCallsClient{tenantID: tenantID})
}

// withOutsideCallsSchema is WithOutsideCallsTenant for a caller holding the
// schema.
func withOutsideCallsSchema(ctx context.Context, schema string) context.Context {
	if schema == "" {
		return ctx
	}
	c := &outsideCallsClient{schema: schema}
	c.once.Do(func() {})
	return context.WithValue(ctx, outsideCallsClientKey{}, c)
}

func outsideCallsSchemaFrom(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	c, _ := ctx.Value(outsideCallsClientKey{}).(*outsideCallsClient)
	if c == nil {
		return ""
	}
	c.once.Do(func() { c.schema = outsideCallsSchemaOf(c.tenantID) })
	return c.schema
}
