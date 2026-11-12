package engines

import (
	"custom_erp/db"
	"errors"
	"fmt"
	"log"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Stage 47.12.6: a sandbox client runs in the same process as real ones, and
// nothing it does may leave the server.
//
// The one outside-calls gate (engines/outside_calls.go) now knows a sandbox:
// every place that asks it whether a call may go - connectors, couriers,
// extension hooks, webhooks, Web Push, CRM sends, emailed documents,
// dashboard digests - is refused for a sandbox client, reads and loopback
// included. Each refusal writes one row to public.sandbox_effect_log: the
// kind of call, the code place that asked and a short detail, so a rehearsal
// shows what WOULD have gone out. The few places that had their own sandbox
// check first (webhook delivery, notification webhooks, CRM sends, Web Push,
// password-reset mail) record the same row. The image drive, which does not
// ask the gate, is refused for a sandbox in its own request function.
//
// The log holds kinds, code places and hosts only - never a recipient, a
// body or a credential.

// sandboxFlagTTL is how long a client's sandbox answer is remembered. A
// sandbox is made one at creation and never later (migration trigger), so a
// stale "no" can only last this long after a sandbox is created, and both
// creators drop the cache as they flag it.
const sandboxFlagTTL = 5 * time.Second

// sandboxEffectCap bounds one sandbox's log; past it new effects are only
// written to the process log.
const sandboxEffectCap = 5000

type sandboxFlagEntry struct {
	v  bool
	at time.Time
}

var sandboxFlags sync.Map // "s:<schema>" or "t:<tenant id>" -> sandboxFlagEntry

func invalidateSandboxFlag() {
	sandboxFlags.Range(func(k, _ any) bool { sandboxFlags.Delete(k); return true })
}

func sandboxFlagCached(key, query, arg string) bool {
	if arg == "" || db.DB == nil {
		return false
	}
	if e, ok := sandboxFlags.Load(key); ok {
		if ent := e.(sandboxFlagEntry); time.Since(ent.at) < sandboxFlagTTL {
			return ent.v
		}
	}
	var v bool
	if err := db.DB.QueryRow(query, arg).Scan(&v); err != nil {
		return false
	}
	sandboxFlags.Store(key, sandboxFlagEntry{v: v, at: time.Now()})
	return v
}

// schemaIsSandbox reports whether the client with this schema is a sandbox,
// remembered for a few seconds so the outside-calls gate costs a map lookup.
func schemaIsSandbox(schema string) bool {
	return sandboxFlagCached("s:"+schema, `SELECT COALESCE(is_sandbox, FALSE) FROM public.tenants WHERE schema_name = $1`, schema)
}

// tenantIsSandboxCached is schemaIsSandbox for a caller holding a tenant id.
func tenantIsSandboxCached(tenantID string) bool {
	return sandboxFlagCached("t:"+tenantID, `SELECT COALESCE(is_sandbox, FALSE) FROM public.tenants WHERE tenant_id = $1`, tenantID)
}

// sandboxKindByFile names the kind of outside call from the file that asked.
var sandboxKindByFile = []struct{ prefix, kind string }{
	{"document_mail", "email"}, {"outbox_email", "email"}, {"dashboard_digest", "email"}, {"password_reset", "email"},
	{"crm_", "message"}, {"webhook", "webhook"}, {"notifications", "webhook"}, {"web_push", "push"},
	{"shopify_sync", "channel"}, {"oms_", "channel"}, {"connector", "connector"}, {"courier", "connector"},
	{"pinelabs", "connector"}, {"unicommerce", "connector"}, {"clevertap", "connector"}, {"channel_", "connector"},
	{"extensions", "hook"}, {"image_drive", "drive"},
}

// sandboxEffectOrigin names the kind and the function of the code that
// asked: skip 0 is the caller of sandboxEffectOrigin, 1 that caller's caller.
func sandboxEffectOrigin(skip int) (kind, effect string) {
	kind, effect = "other", "unknown"
	pc, file, _, ok := runtime.Caller(skip + 1)
	if !ok {
		return
	}
	if f := runtime.FuncForPC(pc); f != nil {
		effect = f.Name()
		if i := strings.LastIndex(effect, "/"); i >= 0 {
			effect = effect[i+1:]
		}
		effect = strings.TrimPrefix(effect, "engines.")
	}
	base := filepath.Base(file)
	for _, k := range sandboxKindByFile {
		if strings.HasPrefix(base, k.prefix) {
			kind = k.kind
			break
		}
	}
	return
}

func sandboxHostOf(rawURL string) string {
	if u, err := url.Parse(rawURL); err == nil && u.Host != "" {
		return u.Host
	}
	return ""
}

// noteSandboxSuppressed records, for the client with this schema, that the
// calling code place would have made an outside call. detail is a host or an
// event name, never personal data.
func noteSandboxSuppressed(schema, detail string) {
	kind, effect := sandboxEffectOrigin(1)
	insertSandboxEffect("schema_name", schema, kind, effect, detail)
}

// noteSandboxSuppressedForTenant is noteSandboxSuppressed for a caller
// holding a tenant id.
func noteSandboxSuppressedForTenant(tenantID, detail string) {
	kind, effect := sandboxEffectOrigin(1)
	insertSandboxEffect("tenant_id", tenantID, kind, effect, detail)
}

func insertSandboxEffect(column, key, kind, effect, detail string) {
	if db.DB == nil || key == "" || (column != "schema_name" && column != "tenant_id") {
		return
	}
	if len(detail) > 300 {
		detail = detail[:300]
	}
	log.Printf("[SANDBOX] %s %s suppressed (%s)", kind, effect, detail)
	_, err := db.DB.Exec(`INSERT INTO public.sandbox_effect_log (tenant_id, kind, effect, detail)
		SELECT t.tenant_id, $2, $3, $4 FROM public.tenants t
		 WHERE t.`+column+` = $1 AND COALESCE(t.is_sandbox, FALSE)
		   AND (SELECT COUNT(*) FROM public.sandbox_effect_log l WHERE l.tenant_id = t.tenant_id) < $5`,
		key, kind, effect, detail, sandboxEffectCap)
	if err != nil {
		log.Printf("[SANDBOX] could not record a suppressed effect: %v", err)
	}
}

// SandboxEffect is one outside call a sandbox client attempted.
type SandboxEffect struct {
	ID        int64     `json:"id"`
	TenantID  string    `json:"tenant_id"`
	Kind      string    `json:"kind"`
	Effect    string    `json:"effect"`
	Detail    string    `json:"detail"`
	CreatedAt time.Time `json:"created_at"`
}

// ListSandboxEffects returns a sandbox client's suppressed effects, newest
// first. limit <= 0 is 100.
func ListSandboxEffects(tenantID string, limit int) ([]SandboxEffect, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := db.DB.Query(`SELECT id, tenant_id, kind, effect, detail, created_at FROM public.sandbox_effect_log
		WHERE tenant_id = $1 ORDER BY id DESC LIMIT $2`, tenantID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SandboxEffect
	for rows.Next() {
		var e SandboxEffect
		if err := rows.Scan(&e.ID, &e.TenantID, &e.Kind, &e.Effect, &e.Detail, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// SandboxEffectCounts is a sandbox client's suppressed effects by kind.
func SandboxEffectCounts(tenantID string) (map[string]int64, error) {
	rows, err := db.DB.Query(`SELECT kind, COUNT(*) FROM public.sandbox_effect_log WHERE tenant_id = $1 GROUP BY 1`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var k string
		var n int64
		if err := rows.Scan(&k, &n); err != nil {
			return nil, err
		}
		out[k] = n
	}
	return out, rows.Err()
}

// --- rehearsal before a real connector is switched on ---------------------

// SettingRequireRehearsal makes switching on a connector that no sandbox has
// rehearsed a refusal instead of a recorded warning. Off as shipped.
const SettingRequireRehearsal = "platform.require_rehearsal"

// sandboxRehearsalWindow is how recent a sandbox's suppressed connector call
// must be to count as a rehearsal.
const sandboxRehearsalWindow = 90 * 24 * time.Hour

// connectorRehearsed reports whether any sandbox in this database has had a
// connector call stopped within the window, and when the latest was.
func connectorRehearsed() (bool, time.Time) {
	var at *time.Time
	if db.DB == nil {
		return false, time.Time{}
	}
	if err := db.DB.QueryRow(`SELECT MAX(created_at) FROM public.sandbox_effect_log WHERE kind = 'connector' AND created_at > $1`,
		time.Now().Add(-sandboxRehearsalWindow)).Scan(&at); err != nil || at == nil {
		return false, time.Time{}
	}
	return true, *at
}

// checkConnectorRehearsal runs when a client switches a connector credential
// on (a save or a switch-on). A sandbox client is the rehearsal, so it is
// never asked. For a real client it records whether a sandbox has rehearsed a
// connector call - CONNECTOR_REHEARSED, or the warning
// CONNECTOR_ENABLED_WITHOUT_REHEARSAL, in its audit log - and refuses only
// when the client turned platform.require_rehearsal on.
func checkConnectorRehearsal(tenantID, kind, code, actor string) error {
	if tenantID == "" || tenantIsSandboxCached(tenantID) {
		return nil
	}
	if done, at := connectorRehearsed(); done {
		LogAuditEvent(tenantID, actor, "CONNECTOR_REHEARSED", "SUCCESS",
			fmt.Sprintf("kind=%s code=%s last sandbox rehearsal=%s", kind, code, at.UTC().Format("2006-01-02")))
		return nil
	}
	if schema := outsideCallsSchemaOf(tenantID); schema != "" && GetSettingBoolForSchema(schema, SettingRequireRehearsal) {
		LogAuditEvent(tenantID, actor, "CONNECTOR_ENABLE_REFUSED", "FAILURE",
			fmt.Sprintf("kind=%s code=%s no sandbox has rehearsed a connector call in the last 90 days", kind, code))
		return errors.New("rehearse this connector in a sandbox first: no sandbox has stopped a connector call in the last 90 days (Settings > Platform > Require rehearsal)")
	}
	LogAuditEvent(tenantID, actor, "CONNECTOR_ENABLED_WITHOUT_REHEARSAL", "WARNING",
		fmt.Sprintf("kind=%s code=%s no sandbox has rehearsed a connector call in the last 90 days", kind, code))
	return nil
}
