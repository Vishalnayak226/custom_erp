package engines

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"custom_erp/db"
)

// Background workers with less idle work (283.2).
//
// Every background worker wakes on its own timer, lists the clients, checks
// each one's module switches and then looks in each client for work. With
// nothing to do that still cost about 200 database statements a minute for a
// single idle client (measured 2026-11-26 on a scratch database): about a third
// went on listing clients and module switches again and again, the rest on
// looking into empty queues. Two shared helpers cut both without changing what
// any worker does when there is work:
//
//   - sweepTenantSchemas is listTenantSchemas from a shared view of the client
//     list and each client's module switches, read at most once per
//     AUTH_STATE_CACHE_SECONDS (default 30s, the same window the sign-in check
//     already allows a suspension or module change to take). 0 turns the
//     view off and every call reads afresh, as before. Provisioning, suspension,
//     resumption, package changes and module switches made through the app drop
//     the view at once (forgetSweepView), so those are seen on the next tick.
//   - dueSchemas asks every client in one statement whether its queue has a due
//     row, using the worker's own due condition, so a worker only visits
//     clients with something to do. On any error it returns every client and
//     the worker runs as it did before.
//
// Tests call listTenantSchemas directly; only the Start* worker loops use the
// shared view (TestBLD044WorkerInventoryMatchesTheCode checks this).

type sweepView struct {
	at       time.Time
	schemas  []string
	tenantOf map[string]string
	modules  map[string]map[string]bool // schema -> module key -> enabled
}

var (
	sweepMu  sync.Mutex
	sweepCur *sweepView
	sweepGen uint64 // bumped by forgetSweepView; a read that began before it is not kept
)

// sweepSchemaRe is the schema names dueSchemas and the module read will put
// into SQL text; db.GetTenantSchema allows the same shape.
var sweepSchemaRe = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// forgetSweepView drops the shared view, so the next worker tick reads the
// client list and module switches afresh. Call it after a change to either.
func forgetSweepView() {
	sweepMu.Lock()
	sweepCur = nil
	sweepGen++
	sweepMu.Unlock()
}

// sweepTenantSchemas returns what listTenantSchemas(moduleKeys...) returns,
// read from the shared view. Only background worker loops use it.
func sweepTenantSchemas(moduleKeys ...string) ([]string, error) {
	ttl := authStateCacheTTL()
	if ttl <= 0 {
		return listTenantSchemas(moduleKeys...)
	}
	view, err := currentSweepView(ttl)
	if err != nil {
		return nil, err
	}
	eligible := []string{}
	for _, schema := range view.schemas {
		if len(moduleKeys) == 0 {
			eligible = append(eligible, schema)
			continue
		}
		switches, ok := view.moduleSwitches(schema)
		if !ok {
			continue // as RequireModules: a failed read means not eligible
		}
		all := true
		for _, key := range moduleKeys {
			if !switches[key] {
				all = false
				break
			}
		}
		if all {
			eligible = append(eligible, schema)
		}
	}
	return eligible, nil
}

func currentSweepView(ttl time.Duration) (*sweepView, error) {
	sweepMu.Lock()
	view, gen := sweepCur, sweepGen
	sweepMu.Unlock()
	if view != nil && time.Since(view.at) < ttl {
		return view, nil
	}
	at := time.Now()
	schemas, tenantOf, err := readTenantSchemas(true)
	if err != nil {
		return nil, err
	}
	view = &sweepView{at: at, schemas: schemas, tenantOf: tenantOf, modules: map[string]map[string]bool{}}
	sweepMu.Lock()
	if sweepGen == gen {
		sweepCur = view
	}
	sweepMu.Unlock()
	return view, nil
}

// moduleSwitches reads one client's module switches in one statement, with
// the same rule as IsModuleEnabled: a module is on only when its row exists,
// is registered in public.modules and is enabled. A failed read is not kept.
func (v *sweepView) moduleSwitches(schema string) (map[string]bool, bool) {
	sweepMu.Lock()
	switches, ok := v.modules[schema]
	sweepMu.Unlock()
	if ok {
		return switches, true
	}
	if !sweepSchemaRe.MatchString(schema) {
		return nil, false
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT e.module_key, COALESCE(e.enabled, false)
		FROM %s.module_entitlements e JOIN public.modules m ON m.module_key = e.module_key`, schema))
	if err != nil {
		return nil, false
	}
	defer rows.Close()
	switches = map[string]bool{}
	for rows.Next() {
		var key string
		var enabled bool
		if err := rows.Scan(&key, &enabled); err != nil {
			return nil, false
		}
		switches[key] = switches[key] || enabled
	}
	if rows.Err() != nil {
		return nil, false
	}
	sweepMu.Lock()
	v.modules[schema] = switches
	sweepMu.Unlock()
	return switches, true
}

// dueSchemasChunk keeps one dueSchemas statement to a modest size.
const dueSchemasChunk = 200

// dueSchemas returns the schemas whose table has a row matching where - the
// worker's own due condition, a constant in its file. It is one statement per
// 200 clients. On any error it returns every schema, so the worker looks
// everywhere as it did before.
func dueSchemas(schemas []string, table, where string) []string {
	due, err := findDueSchemas(schemas, table, where)
	if err != nil {
		return schemas
	}
	return due
}

func findDueSchemas(schemas []string, table, where string) ([]string, error) {
	due := []string{}
	for start := 0; start < len(schemas); start += dueSchemasChunk {
		end := start + dueSchemasChunk
		if end > len(schemas) {
			end = len(schemas)
		}
		parts := make([]string, 0, end-start)
		for _, s := range schemas[start:end] {
			if !sweepSchemaRe.MatchString(s) {
				return nil, fmt.Errorf("schema name %q is not a plain identifier", s)
			}
			parts = append(parts, fmt.Sprintf(`SELECT '%[1]s' WHERE EXISTS (SELECT 1 FROM %[1]s.%[2]s WHERE %[3]s)`, s, table, where))
		}
		found, err := queryStrings(strings.Join(parts, "\nUNION ALL\n"))
		if err != nil {
			return nil, err
		}
		due = append(due, found...)
	}
	return due, nil
}

func queryStrings(query string) ([]string, error) {
	rows, err := db.DB.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
