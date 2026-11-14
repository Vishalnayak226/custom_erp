package engines

import (
	"custom_erp/db"
	"sync"
	"time"
)

// Stage 47.12.4: the Finance pack's eight live-book checks (47.12.3, golive.go)
// feed the guided setup steps they belong to, so a step cannot show Ready while
// one of them fails, and the reason is on the step.
//
//	entity     the company's books: open period, INR, posting accounts, a bank account
//	parties    opening balances, stock / receivables / payables tie to the ledger, accepted
//
// The eighth line, fin-series, is read from the numbering step's own checks, so it
// already counts there and is not fed back (it would call itself).
//
// A line that fails is a failing check, blocking when the line blocks, so the
// step reads Invalid. A line only a person can prove and nobody has yet is a
// warning: it shows on the step but does not make it Invalid. Evidence and
// waivers on the pack line count here too, since the same record resolves both.
var setupFinanceLines = map[string][]string{
	"entity":  {"fin-calendar", "fin-currency", "fin-chart", "fin-bank"},
	"parties": {"fin-opening", "fin-recon-tied", "fin-recon-accepted"},
}

// Stage 164.2: the Finance lines run a trial balance and three reports, and
// the admin Home card asks for the setup path on every visit. A client's lines
// are therefore kept for setupFinanceCacheTTL. Saving a go-live record, a
// Currency or a BankAccount drops the client's copy at once
// (InvalidateSetupFinanceChecks); a posting, period or chart change shows by
// the TTL at the latest. Memory is one small map per client, cleared when it
// holds setupFinanceCacheMax clients.
const (
	setupFinanceCacheTTL = 60 * time.Second
	setupFinanceCacheMax = 500
)

type setupFinanceCacheEntry struct {
	byStep   map[string][]SetupCheck
	loadedAt time.Time
}

var (
	setupFinanceCacheMu sync.Mutex
	setupFinanceCache   = map[string]setupFinanceCacheEntry{}
)

// InvalidateSetupFinanceChecks forgets a client schema's cached finance checks.
func InvalidateSetupFinanceChecks(schema string) {
	setupFinanceCacheMu.Lock()
	delete(setupFinanceCache, schema)
	setupFinanceCacheMu.Unlock()
}

// cachedSetupFinanceChecks returns every setup step's finance checks for a
// client, running the lines only when the cached copy is missing or stale.
func cachedSetupFinanceChecks(tenantID string) (map[string][]SetupCheck, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	setupFinanceCacheMu.Lock()
	e, ok := setupFinanceCache[schema]
	setupFinanceCacheMu.Unlock()
	if ok && time.Since(e.loadedAt) < setupFinanceCacheTTL {
		return e.byStep, nil
	}
	c, err := newGoLiveCtx(tenantID)
	if err != nil {
		return nil, err
	}
	byStep := map[string][]SetupCheck{}
	for stepID := range setupFinanceLines {
		byStep[stepID] = setupFinanceChecks(c, stepID)
	}
	setupFinanceCacheMu.Lock()
	if len(setupFinanceCache) >= setupFinanceCacheMax {
		setupFinanceCache = map[string]setupFinanceCacheEntry{}
	}
	setupFinanceCache[schema] = setupFinanceCacheEntry{byStep: byStep, loadedAt: time.Now()}
	setupFinanceCacheMu.Unlock()
	return byStep, nil
}

// setupFinanceChecks returns the finance checks of one setup step.
func setupFinanceChecks(c *goLiveCtx, stepID string) []SetupCheck {
	var out []SetupCheck
	for _, id := range setupFinanceLines[stepID] {
		d, ok := goLiveLineByID(id)
		if !ok {
			continue
		}
		l := c.resolve(d)
		chk := SetupCheck{ID: "finance-" + id, Label: l.Label, Severity: SetupSeverityWarning, Detail: l.Detail}
		switch l.State {
		case LineFail:
			if l.Severity == LineBlocking || l.Severity == LineCritical {
				chk.Severity = SetupSeverityBlocking
			}
		case LineNotEvidenced:
		default:
			chk.Passed = true
		}
		out = append(out, chk)
	}
	return out
}
