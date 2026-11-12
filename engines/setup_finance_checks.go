package engines

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
