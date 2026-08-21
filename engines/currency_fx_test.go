package engines

import (
	"custom_erp/db"
	"strings"
	"testing"
)

// Stage 37.1.3 / 37.1.4.
//
// The bulk of these are pure-arithmetic tests with no database, deliberately:
// the sign conventions and the carrying-amount model are where this feature is
// actually hard to get right, and a test that needs a live rate table to prove
// "a payable settled at a higher rate is a LOSS" is a test nobody will run
// while changing the sign. The one integration test at the bottom reuses an
// already-open pool rather than calling db.InitDB again, per the connection
// ceiling recorded in Stage 35.4's notes.

func fxPosition(currency string, amount float64, rate float64, booked, cumulative int) DocumentFXPosition {
	return DocumentFXPosition{
		Currency: currency, Functional: "INR", Foreign: currency != "INR", Rate: rate,
		TransactionAmount: amount, BookedAmount: booked,
		CumulativeUnrealised: cumulative, CarryingAmount: booked + cumulative,
	}
}

func TestRealisedFXSignConventions(t *testing.T) {
	// USD 1,000 booked at 83.00 = 83,000. The rate moves to 85.00.
	position := fxPosition("USD", 1000, 83, 83000, 0)

	t.Run("a receivable collected at a higher rate is a gain", func(t *testing.T) {
		settlement, err := resolveSettlementFX("default", position, true, SettlementOptions{ExchangeRate: 85})
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if settlement.SettlementAmount != 85000 {
			t.Fatalf("cash received = %d, want 85000", settlement.SettlementAmount)
		}
		if settlement.RealisedGainLoss != 2000 {
			t.Fatalf("realised = %d, want +2000 (a gain)", settlement.RealisedGainLoss)
		}
	})

	t.Run("a payable settled at a higher rate is a loss", func(t *testing.T) {
		// Same numbers, opposite side. This is the assertion that catches a
		// sign flip, and the reason resolveSettlementFX is TOLD which side it
		// is on rather than inferring it from the amounts.
		settlement, err := resolveSettlementFX("default", position, false, SettlementOptions{ExchangeRate: 85})
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if settlement.RealisedGainLoss != -2000 {
			t.Fatalf("realised = %d, want -2000 (a loss - the payable cost 2,000 more rupees than booked)", settlement.RealisedGainLoss)
		}
	})

	t.Run("a receivable collected at a lower rate is a loss", func(t *testing.T) {
		settlement, err := resolveSettlementFX("default", position, true, SettlementOptions{ExchangeRate: 81})
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if settlement.RealisedGainLoss != -2000 {
			t.Fatalf("realised = %d, want -2000", settlement.RealisedGainLoss)
		}
	})
}

func TestRealisedFXMeasuresAgainstCarryingNotBookedAmount(t *testing.T) {
	// The invoice was booked at 83.00 (83,000) and already revalued at a
	// period end to 84.00, so +1,000 of unrealised gain is ALREADY in the P&L
	// and the ledger carries 84,000. It is then collected at 85.00.
	//
	// The realised gain must be 1,000 - the movement since the revaluation -
	// not 2,000. Measuring against the booked amount would recognise the first
	// 1,000 twice, which is the single most likely way for this feature to be
	// "simplified" into being wrong.
	position := fxPosition("USD", 1000, 83, 83000, 1000)
	settlement, err := resolveSettlementFX("default", position, true, SettlementOptions{ExchangeRate: 85})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if settlement.SettlementAmount != 85000 {
		t.Fatalf("cash = %d, want 85000", settlement.SettlementAmount)
	}
	if settlement.RealisedGainLoss != 1000 {
		t.Fatalf("realised = %d, want 1000 - the 1,000 already recognised as unrealised must not be counted again", settlement.RealisedGainLoss)
	}
	// Total recognised across both periods is 2,000, which is the whole
	// movement from 83 to 85. That identity is the point.
	if position.CumulativeUnrealised+settlement.RealisedGainLoss != 2000 {
		t.Fatalf("total recognised = %d, want 2000", position.CumulativeUnrealised+settlement.RealisedGainLoss)
	}
}

func TestSettlementPostingsAlwaysBalance(t *testing.T) {
	// Every combination of side and rate direction, asserted through the same
	// helper the engines use. An unbalanced journal is refused by
	// PostDoubleEntry, so a sign error here surfaces as a settlement that
	// cannot post at all.
	cases := []struct {
		name       string
		receivable bool
		rate       float64
	}{
		{"receivable, rate up", true, 85},
		{"receivable, rate down", true, 81},
		{"receivable, rate flat", true, 83},
		{"payable, rate up", false, 85},
		{"payable, rate down", false, 81},
		{"payable, rate flat", false, 83},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			position := fxPosition("USD", 1000, 83, 83000, 0)
			settlement, err := resolveSettlementFX("default", position, testCase.receivable, SettlementOptions{ExchangeRate: testCase.rate})
			if err != nil {
				t.Fatalf("resolve: %v", err)
			}
			var debits, credits map[string]int
			if testCase.receivable {
				debits = map[string]int{AccountCashBank: settlement.SettlementAmount}
				credits = map[string]int{AccountAccountsReceivable: position.CarryingAmount}
			} else {
				debits = map[string]int{AccountAPSuspense: position.CarryingAmount}
				credits = map[string]int{AccountCashBank: settlement.SettlementAmount}
			}
			applyRealisedFXLine(debits, credits, settlement.RealisedGainLoss)

			sumDebits, sumCredits := 0, 0
			for _, value := range debits {
				sumDebits += value
			}
			for _, value := range credits {
				sumCredits += value
			}
			if sumDebits != sumCredits {
				t.Fatalf("unbalanced: debits %d (%v) vs credits %d (%v)", sumDebits, debits, sumCredits, credits)
			}
			// A flat rate must produce no FX line at all, or a single-currency
			// tenant's ledger would sprout zero-value FX rows.
			if testCase.rate == 83 {
				if _, gain := credits[AccountRealisedFXGain]; gain {
					t.Fatal("a flat rate produced a gain line")
				}
				if _, loss := debits[AccountRealisedFXLoss]; loss {
					t.Fatal("a flat rate produced a loss line")
				}
			}
		})
	}
}

func TestFunctionalCurrencySettlementIsUnchanged(t *testing.T) {
	// The guarantee that matters most: a tenant that never uses multi-currency
	// must post exactly what it posted before this stage.
	position := fxPosition("INR", 5000, 1, 5000, 0)
	settlement, err := resolveSettlementFX("default", position, true, SettlementOptions{})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if settlement.SettlementAmount != 5000 || settlement.RealisedGainLoss != 0 {
		t.Fatalf("functional settlement = %d with FX %d, want 5000 / 0",
			settlement.SettlementAmount, settlement.RealisedGainLoss)
	}
	debits := map[string]int{AccountCashBank: settlement.SettlementAmount}
	credits := map[string]int{AccountAccountsReceivable: position.CarryingAmount}
	applyRealisedFXLine(debits, credits, settlement.RealisedGainLoss)
	if len(debits) != 1 || len(credits) != 1 {
		t.Fatalf("a functional settlement added extra lines: %v / %v", debits, credits)
	}
	// And no rate lookup was attempted - the ExchangeRate table may be empty.
	if settlement.RateSource != "Functional" {
		t.Fatalf("rate source = %q, want Functional (no lookup)", settlement.RateSource)
	}
}

func TestSettlementPostingDateOnlyBindsWhenExplicit(t *testing.T) {
	position := fxPosition("USD", 100, 83, 8300, 0)

	implicit, err := resolveSettlementFX("default", position, true, SettlementOptions{ExchangeRate: 83})
	if err != nil {
		t.Fatalf("implicit: %v", err)
	}
	// Left to default, the closed-period check must fall back to Postgres's
	// CURRENT_DATE rather than the Go process's UTC date - see PostingDate's
	// comment for the 05:30 IST trap this avoids.
	if implicit.PostingDate() != "" {
		t.Fatalf("PostingDate = %q for a defaulted date, want empty so CURRENT_DATE is used", implicit.PostingDate())
	}

	explicit, err := resolveSettlementFX("default", position, true, SettlementOptions{ExchangeRate: 83, SettlementDate: "2026-03-31"})
	if err != nil {
		t.Fatalf("explicit: %v", err)
	}
	if explicit.PostingDate() != "2026-03-31" {
		t.Fatalf("PostingDate = %q, want the date the caller named", explicit.PostingDate())
	}

	if _, err := resolveSettlementFX("default", position, true, SettlementOptions{ExchangeRate: 83, SettlementDate: "31/03/2026"}); err == nil {
		t.Fatal("a malformed settlement date was accepted")
	}
}

func TestDocumentFXPositionReadsTheStamp(t *testing.T) {
	t.Run("a stamped foreign document", func(t *testing.T) {
		payload := map[string]interface{}{
			"currency": "USD", "exchange_rate": 83.0,
			"total_amount": 1000.0, "base_total_amount": 83000.0,
			"unrealized_fx_cumulative": 1000.0, "last_revalued_on": "2026-07-31",
		}
		position := documentFXPosition("default", payload, "total_amount")
		if !position.Foreign || position.Currency != "USD" {
			t.Fatalf("position = %+v, want a foreign USD position", position)
		}
		if position.BookedAmount != 83000 || position.CarryingAmount != 84000 {
			t.Fatalf("booked/carrying = %d/%d, want 83000/84000", position.BookedAmount, position.CarryingAmount)
		}
		if position.LastRevaluedOn != "2026-07-31" {
			t.Fatalf("last revalued = %q", position.LastRevaluedOn)
		}
	})

	t.Run("a pre-37.1.2 document has no stamp and is functional by definition", func(t *testing.T) {
		position := documentFXPosition("default", map[string]interface{}{"total_amount": 500.0}, "total_amount")
		if position.Foreign {
			t.Fatal("an unstamped document was treated as foreign")
		}
		if position.BookedAmount != 500 || position.CarryingAmount != 500 {
			t.Fatalf("booked/carrying = %d/%d, want 500/500 - the raw amount", position.BookedAmount, position.CarryingAmount)
		}
		if position.LastRevaluedOn != "" {
			t.Fatalf("last revalued = %q, want empty rather than the string \"<nil>\"", position.LastRevaluedOn)
		}
	})

	t.Run("the booked amount comes from the stamp, not from rate x amount", func(t *testing.T) {
		// A rate edited after posting must NOT retroactively restate what the
		// ledger holds. 82 x 1000 is 82,000, but 83,000 was posted.
		payload := map[string]interface{}{
			"currency": "USD", "exchange_rate": 82.0,
			"total_amount": 1000.0, "base_total_amount": 83000.0,
		}
		position := documentFXPosition("default", payload, "total_amount")
		if position.BookedAmount != 83000 {
			t.Fatalf("booked = %d, want the 83000 that was actually posted", position.BookedAmount)
		}
	})
}

func TestRevaluationAdjustmentAndSignBySide(t *testing.T) {
	// Verified through the same arithmetic RevalueOpenForeignItems runs: a
	// USD 1,000 item booked at 83, revalued at 84.
	position := fxPosition("USD", 1000, 83, 83000, 0)
	revalued := roundToRupees(position.TransactionAmount * 84)
	adjustment := revalued - position.CarryingAmount
	if adjustment != 1000 {
		t.Fatalf("adjustment = %d, want 1000", adjustment)
	}

	// A receivable that grew is a gain; a payable that grew is a loss.
	for _, testCase := range []struct {
		name       string
		receivable bool
		wantGain   bool
	}{
		{"receivable up is a gain", true, true},
		{"payable up is a loss", false, false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			gain := adjustment > 0
			if !testCase.receivable {
				gain = !gain
			}
			if gain != testCase.wantGain {
				t.Fatalf("gain = %v, want %v", gain, testCase.wantGain)
			}
		})
	}

	// A second revaluation at the same rate must move nothing - the carrying
	// amount already equals the revalued amount, which is what makes the run
	// idempotent without needing to remember it ran.
	after := fxPosition("USD", 1000, 83, 83000, 1000)
	if second := roundToRupees(after.TransactionAmount*84) - after.CarryingAmount; second != 0 {
		t.Fatalf("second revaluation at the same rate moved %d, want 0", second)
	}
}

func TestFXLinesCarryNoInventedForeignAmount(t *testing.T) {
	// A regression test for a defect live verification caught and the unit
	// tests could not have: PostDoubleEntry derives a missing per-account
	// transaction amount as functional/rate, so leaving an FX gain line out of
	// the transaction maps made a 1,000-rupee gain at 85.00 record itself as
	// USD 11.76. Summing transaction_debit for the document then reported an
	// exposure that was never owed.
	position := fxPosition("USD", 1000, 83, 83000, 0)
	settlement, err := resolveSettlementFX("default", position, true, SettlementOptions{ExchangeRate: 85})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	debits := map[string]int{AccountCashBank: settlement.SettlementAmount}
	credits := map[string]int{AccountAccountsReceivable: position.CarryingAmount}
	applyRealisedFXLine(debits, credits, settlement.RealisedGainLoss)
	if _, present := credits[AccountRealisedFXGain]; !present {
		t.Fatal("expected a realised gain line to exist for this fixture")
	}

	options := postingOptionsFor(position, settlement.SettlementRate, debits, credits, map[string]float64{
		AccountCashBank:           position.TransactionAmount,
		AccountAccountsReceivable: position.TransactionAmount,
	})

	// Every account in the entry must be present in the maps - a missing key
	// is exactly what triggers the derive-it fallback.
	for account := range debits {
		if _, present := options.TransactionDebits[account]; !present {
			t.Fatalf("debit account %s has no transaction amount; PostDoubleEntry will invent one", account)
		}
	}
	for account := range credits {
		if _, present := options.TransactionCredits[account]; !present {
			t.Fatalf("credit account %s has no transaction amount; PostDoubleEntry will invent one", account)
		}
	}
	if got := options.TransactionCredits[AccountRealisedFXGain]; got != 0 {
		t.Fatalf("the realised gain line carries %v USD, want 0 - it has no foreign value by construction", got)
	}
	if got := options.TransactionDebits[AccountCashBank]; got != 1000 {
		t.Fatalf("the cash line carries %v USD, want the full 1000", got)
	}

	// A revaluation moves no foreign currency at all, so every line is zero.
	revalDebits := map[string]int{AccountAccountsReceivable: 1000}
	revalCredits := map[string]int{AccountUnrealisedFXGain: 1000}
	revalOptions := postingOptionsFor(position, 84, revalDebits, revalCredits, nil)
	for account, amount := range revalOptions.TransactionDebits {
		if amount != 0 {
			t.Fatalf("revaluation debit %s carries %v USD, want 0", account, amount)
		}
	}
	for account, amount := range revalOptions.TransactionCredits {
		if amount != 0 {
			t.Fatalf("revaluation credit %s carries %v USD, want 0", account, amount)
		}
	}
	if revalOptions.Currency != "USD" || revalOptions.ExchangeRate != 84 {
		t.Fatalf("revaluation lost its currency/rate metadata: %+v", revalOptions)
	}
}

func TestRoundToRupeesIsHalfAwayFromZero(t *testing.T) {
	// int() truncation and math.Round() disagree by a rupee, and an
	// unbalanced journal is the symptom. Pinned so nobody "simplifies" it.
	cases := map[float64]int{83.4: 83, 83.5: 84, 83.6: 84, -83.5: -84, 0: 0}
	for input, want := range cases {
		if got := roundToRupees(input); got != want {
			t.Fatalf("roundToRupees(%v) = %d, want %d", input, got, want)
		}
	}
}

func TestRevaluationRefusesUnknownRateType(t *testing.T) {
	if _, err := RevalueOpenForeignItems("default", "2026-08-16", "Nonsense", "system", true); err == nil {
		t.Fatal("an unknown rate type was accepted")
	}
	if _, err := RevalueOpenForeignItems("default", "16-08-2026", "Closing", "system", true); err == nil {
		t.Fatal("a malformed as-of date was accepted")
	}
}

func TestFXSettlementAuditDetailStatesTheMovement(t *testing.T) {
	position := fxPosition("USD", 1000, 83, 83000, 0)
	settlement, err := resolveSettlementFX("default", position, true, SettlementOptions{ExchangeRate: 85})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	detail := settlementAuditDetail("sales invoice", "INV-1", settlement)
	for _, want := range []string{"USD", "85000", "83000", "realised gain 2000"} {
		if !strings.Contains(detail, want) {
			t.Fatalf("audit detail %q is missing %q", detail, want)
		}
	}

	// A functional settlement's audit line must read exactly as it always did.
	functional := fxPosition("INR", 500, 1, 500, 0)
	plain, err := resolveSettlementFX("default", functional, true, SettlementOptions{})
	if err != nil {
		t.Fatalf("resolve functional: %v", err)
	}
	if got := settlementAuditDetail("sales invoice", "INV-2", plain); got != "Settled sales invoice INV-2 amount=500" {
		t.Fatalf("functional audit detail = %q, want the pre-37.1.3 wording", got)
	}
}

// TestFXGainLossRegisterReadsTheLedger is the one database-backed test here. It
// reuses whatever pool is already open rather than calling db.InitDB again -
// see the connection-ceiling note in Stage 35.4's handover entry.
func TestFXGainLossRegisterReadsTheLedger(t *testing.T) {
	if db.DB == nil {
		db.InitDB(testConnStr())
	}
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	var hasAccount bool
	if err := db.DB.QueryRow("SELECT EXISTS(SELECT 1 FROM "+schema+".gl_accounts WHERE account_code = $1)",
		AccountRealisedFXGain).Scan(&hasAccount); err != nil {
		t.Fatalf("inspect gl_accounts: %v", err)
	}
	if !hasAccount {
		t.Skip("db/migrations_stage37_1_fx_revaluation.sql has not been applied to this database")
	}

	const docID = "TEST-FX-REG-0001"
	cleanup := func() {
		_, _ = db.DB.Exec("DELETE FROM "+schema+".gl_postings WHERE document_id = $1", docID)
	}
	cleanup()
	defer cleanup()

	// A realised gain: cash 8,500 against a receivable of 8,300.
	if err := PostDoubleEntry("default", "SalesInvoice", docID,
		map[string]int64{AccountCashBank: 850000},
		map[string]int64{AccountAccountsReceivable: 830000, AccountRealisedFXGain: 20000},
		"", "TEST-FX-REG-0001:SETTLE"); err != nil {
		t.Fatalf("post: %v", err)
	}

	rows, err := GetFXGainLossRegister("default", "", "", "realised")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	found := false
	for _, row := range rows {
		if row.DocumentID != docID {
			continue
		}
		found = true
		if row.Kind != "Realised" {
			t.Fatalf("kind = %q, want Realised", row.Kind)
		}
		if row.Gain != 200 || row.Loss != 0 || row.Net != 200 {
			t.Fatalf("gain/loss/net = %v/%v/%v, want 200/0/200", row.Gain, row.Loss, row.Net)
		}
	}
	if !found {
		t.Fatal("the realised gain posting did not appear in the register")
	}

	// The unrealised filter must exclude it - the whole point of four accounts
	// rather than two is that these two answers never merge.
	unrealised, err := GetFXGainLossRegister("default", "", "", "unrealised")
	if err != nil {
		t.Fatalf("register (unrealised): %v", err)
	}
	for _, row := range unrealised {
		if row.DocumentID == docID {
			t.Fatal("a realised posting appeared under the unrealised filter")
		}
	}
}
