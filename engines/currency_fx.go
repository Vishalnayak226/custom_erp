package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"
)

// Stage 37.1.3 / 37.1.4 - realised and unrealised foreign exchange.
//
// WHAT 37.1.2 LEFT UNFINISHED, AND WHY IT IS A CORRECTNESS BUG RATHER THAN A
// MISSING FEATURE. That stage taught a document to carry the currency it was
// transacted in and the rate that converts it, and taught gl_postings to carry
// both amounts side by side. It did not touch the settlement engines. So
// PostSalesInvoice read `total_amount` - which for a USD invoice is 1000, in
// dollars - and posted 1000 to Accounts Receivable as if it were rupees. A USD
// 1,000 invoice at 83.00 booked a 1,000-rupee receivable instead of an 83,000
// one: the receivable, the ageing report, the credit exposure and the trial
// balance were all wrong by 98.8%, silently, for any tenant that used the
// feature 37.1.2 shipped. Fixing that is the first thing this file does, and it
// is why the posting sites below now read the base twin rather than the raw
// amount.
//
// THE MODEL, STATED ONCE SO IT IS NOT RE-DERIVED PER CALL SITE:
//
//	A receivable is booked at the rate on the invoice date and carried at that
//	rupee value. When the customer pays, the cash actually received is worth a
//	different number of rupees. The difference is REALISED - it is cash, it has
//	happened, it can never reverse. It goes to 4200 (gain) or 5600 (loss).
//
//	While the invoice sits open across a period end, the carrying value is
//	stale. Revaluing it to the closing rate recognises the movement so far, but
//	the customer has not paid and the rate can move back, so that gain is
//	UNREALISED - 4210 or 5610 - and it is tracked cumulatively on the document.
//
//	At settlement the realised figure is measured against the REVALUED carrying
//	amount, not the original one. That is the part most likely to be
//	"corrected" by a later reader, so: total P&L across every period is exactly
//	(cash received - amount originally booked), whatever the split between
//	periods. Measuring realised gain against the ORIGINAL amount instead would
//	double-count every rupee already recognised at a period end. There is
//	deliberately no reversing entry on the first day of the next period; this
//	codebase has no automatic reversal machinery, and a cumulative carrying
//	amount reaches the identical answer with one fewer moving part.
//
// The sign convention differs between the two sides and that is not a mistake:
// when the foreign currency strengthens, a receivable is worth MORE (gain) and
// a payable costs MORE to settle (loss).
const (
	// AccountRealisedFXGain and friends are the four accounts seeded by
	// db/migrations_stage37_1_fx_revaluation.sql. Constants rather than
	// literals at eight posting sites, so a chart-of-accounts change is one
	// edit and a typo is a build error instead of a silently misfiled rupee.
	AccountRealisedFXGain   = "4200"
	AccountUnrealisedFXGain = "4210"
	AccountRealisedFXLoss   = "5600"
	AccountUnrealisedFXLoss = "5610"

	// AccountAccountsReceivable / AccountAPSuspense are the control accounts
	// the settlement engines already use. Named here because the revaluation
	// engine has to post to the same ones the original booking did, and a
	// revaluation that lands in a different account than the balance it is
	// revaluing is worse than no revaluation.
	AccountAccountsReceivable = "1300"
	AccountAPSuspense         = "2100"
	AccountCashBank           = "1100"
)

// SettingKeyRevaluationRateType is the rate type a period-end revaluation
// quotes at. Closing is the correct default and the one every accounting
// standard names for balance-sheet items; it is configurable because a tenant
// whose rate feed only publishes Spot must still be able to run a close.
const SettingKeyRevaluationRateType = "finance.fx_revaluation_rate_type"

// DocumentFXPosition is everything the FX engines need to know about one
// financial document, read from the stamp 37.1.2 already writes.
type DocumentFXPosition struct {
	Currency   string  // the currency the document was transacted in
	Functional string  // the tenant's reporting currency
	Foreign    bool    // false means every amount below is already functional
	Rate       float64 // the rate the document was booked at
	// TransactionAmount is the amount in Currency - what the customer agreed
	// to pay. CarryingAmount is what the ledger currently holds for it in
	// Functional, INCLUDING any revaluation already posted.
	TransactionAmount float64
	BookedAmount      int // functional value at the original booking rate
	CarryingAmount    int // BookedAmount + CumulativeUnrealised
	// CumulativeUnrealised is the running total of unrealised gain/loss
	// already posted against this document by period-end revaluations.
	// Positive is a gain on a receivable / a loss on a payable, i.e. it always
	// means "the carrying amount moved up by this much".
	CumulativeUnrealised int
	LastRevaluedOn       string
}

// documentFXPosition reads the position off a document payload. amountField is
// named by the caller rather than guessed, because SalesInvoice and
// VendorInvoice spell their total differently and DocumentBaseAmount's
// first-match-wins scan over four field names is fine for an approval
// comparison but not for deciding what to post.
func documentFXPosition(tenantID string, payload map[string]interface{}, amountField string) DocumentFXPosition {
	functional := FunctionalCurrency(tenantID)
	position := DocumentFXPosition{Functional: functional, Currency: functional, Rate: 1}

	transaction := numFromInterface(payload[amountField])
	position.TransactionAmount = transaction

	if code := strings.ToUpper(strings.TrimSpace(fmt.Sprintf("%v", payload["currency"]))); isoCurrencyCodePattern.MatchString(code) {
		position.Currency = code
	}
	position.Foreign = position.Currency != functional

	if rate, ok := parityNumber(payload["exchange_rate"]); ok && rate > 0 {
		position.Rate = rate
	}

	// The booked amount is read from the stored base twin rather than
	// recomputed from rate x amount. What was posted is a fact; recomputing it
	// would silently "correct" the ledger to a number nobody ever posted if a
	// document's rate were ever edited after the fact.
	if base, ok := parityNumber(payload["base_"+amountField]); ok && payload["base_"+amountField] != nil {
		position.BookedAmount = roundToRupees(base)
	} else {
		// A document that predates the 37.1.2 stamp, or a functional-currency
		// one. Both were posted at their face value, which is correct.
		position.BookedAmount = roundToRupees(transaction)
	}

	if cumulative, ok := parityNumber(payload["unrealized_fx_cumulative"]); ok && payload["unrealized_fx_cumulative"] != nil {
		position.CumulativeUnrealised = roundToRupees(cumulative)
	}
	position.CarryingAmount = position.BookedAmount + position.CumulativeUnrealised
	position.LastRevaluedOn = strings.TrimSpace(fmt.Sprintf("%v", payload["last_revalued_on"]))
	if position.LastRevaluedOn == "<nil>" {
		position.LastRevaluedOn = ""
	}
	return position
}

// roundToRupees converts a money float to the whole-rupee integer the GL
// stores. Centralised because gl_postings.debit/credit are integers and every
// site that reaches them must round the same way - int() truncation and
// math.Round() disagree by a rupee often enough to unbalance a journal.
func roundToRupees(value float64) int {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	return int(math.Round(value))
}

// SettlementOptions carries the optional facts a settlement needs beyond the
// document itself. Variadic at every call site (the PostingOptions and
// CreateLogisticsBooking precedent), so all six existing callers of the four
// settlement engines needed no change at all.
type SettlementOptions struct {
	// SettlementDate is the date the rate is quoted on - the date the money
	// actually moved, which is not necessarily today when a receipt is entered
	// a few days late. Blank means today.
	SettlementDate string
	// ExchangeRate, when positive, is the rate actually achieved. A bank
	// confirmation states the rate it converted at, and that beats any table:
	// booking the gain at a table rate when the bank gave a different one just
	// moves the error into the cash account, where it is harder to find.
	ExchangeRate float64
}

func settlementOption(opts []SettlementOptions) SettlementOptions {
	if len(opts) > 0 {
		return opts[0]
	}
	return SettlementOptions{}
}

// FXSettlement is the computed result of settling one foreign-currency
// document: what to post, and what to record on the document afterwards.
type FXSettlement struct {
	Position         DocumentFXPosition
	SettlementRate   float64
	SettlementDate   string
	SettlementAmount int // functional value of the cash that moved
	// RealisedGainLoss is signed from the perspective of profit: positive is a
	// gain (credit 4200), negative is a loss (debit 5600). Already accounts for
	// the receivable/payable sign flip, so callers never re-derive it.
	RealisedGainLoss int
	RateSource       string
	// DateWasExplicit records whether the caller named the settlement date or
	// this engine defaulted it to today. It decides what reaches
	// PostDoubleEntry's closed-period check - see PostingDate.
	DateWasExplicit bool
}

// PostingDate is the transaction date handed to PostDoubleEntry.
//
// Empty when the caller did not name a date, which makes the closed-period
// check use Postgres's own CURRENT_DATE - byte-identical to what every
// settlement did before this stage. That distinction is load-bearing rather
// than pedantic: SettlementDate defaults to the Go process's UTC date, and on
// this Asia/Calcutta deployment that is yesterday's date until 05:30 local. A
// settlement run at 02:00 on the 1st would otherwise be checked against - and
// refused by - the previous month's just-closed period. Reckon a time window
// against one clock, never a mix of two; the same lesson the Stage 14 lockout
// bug and the Stage 35 SLA bug both taught.
//
// A caller that DID name a date means it, and gets it checked.
func (s FXSettlement) PostingDate() string {
	if s.DateWasExplicit {
		return s.SettlementDate
	}
	return ""
}

// resolveSettlementFX works out the rate and the realised difference for one
// settlement. receivable selects the sign convention: true for money coming in
// against an asset, false for money going out against a liability.
func resolveSettlementFX(tenantID string, position DocumentFXPosition, receivable bool, opts SettlementOptions) (FXSettlement, error) {
	settlement := FXSettlement{
		Position:       position,
		SettlementRate: 1,
		SettlementDate: strings.TrimSpace(opts.SettlementDate),
	}
	if settlement.SettlementDate == "" {
		settlement.SettlementDate = time.Now().UTC().Format("2006-01-02")
	} else {
		if err := validateISODate("Settlement Date", settlement.SettlementDate, true); err != nil {
			return settlement, err
		}
		settlement.DateWasExplicit = true
	}

	if !position.Foreign {
		// A functional-currency document settles for exactly what it was
		// booked at. No rate lookup, no FX line, and - importantly - the
		// posting below is byte-identical to what it was before this stage.
		settlement.SettlementAmount = position.CarryingAmount
		settlement.RateSource = "Functional"
		return settlement, nil
	}

	switch {
	case opts.ExchangeRate > 0:
		settlement.SettlementRate = opts.ExchangeRate
		settlement.RateSource = "Operator"
	default:
		resolved, err := ResolveExchangeRate(tenantID, position.Currency, position.Functional, settlement.SettlementDate, "Spot")
		if err != nil {
			return settlement, &ValidationError{Code: "FIN-0021", SubFor: "Exchange Rate",
				Message: fmt.Sprintf("%s cannot be converted to %s on %s: %v. Add an Exchange Rate for that date, or supply the rate the bank actually gave.",
					position.Currency, position.Functional, settlement.SettlementDate, err)}
		}
		settlement.SettlementRate = resolved.Rate
		settlement.RateSource = resolved.Source
	}
	if settlement.SettlementRate <= 0 || math.IsInf(settlement.SettlementRate, 0) || math.IsNaN(settlement.SettlementRate) {
		return settlement, &ValidationError{Code: "FIN-0021", SubFor: "Exchange Rate",
			Message: "settlement exchange rate must be a finite number greater than zero"}
	}

	settlement.SettlementAmount = roundToRupees(position.TransactionAmount * settlement.SettlementRate)

	// The difference against the CARRYING amount, not the booked one - see the
	// file header for why. On a receivable, cash worth more than we carried is
	// a gain; on a payable, cash paid out worth more than we carried is a loss.
	difference := settlement.SettlementAmount - position.CarryingAmount
	if receivable {
		settlement.RealisedGainLoss = difference
	} else {
		settlement.RealisedGainLoss = -difference
	}
	return settlement, nil
}

// applyRealisedFXLine adds the gain or loss line to a debit/credit pair so the
// journal balances. Returns the maps unchanged when there is nothing to book,
// which keeps a single-currency tenant's postings exactly as they were.
func applyRealisedFXLine(debits, credits map[string]int, realised int) {
	switch {
	case realised > 0:
		credits[AccountRealisedFXGain] += realised
	case realised < 0:
		debits[AccountRealisedFXLoss] += -realised
	}
}

// recordSettlementFX writes the audit trail of a settlement onto the document
// payload. The cumulative unrealised balance is cleared because the document is
// now closed - leaving it would let a later revaluation sweep pick up an item
// that has already been settled.
func recordSettlementFX(data map[string]interface{}, settlement FXSettlement) {
	if !settlement.Position.Foreign {
		return
	}
	data["settlement_exchange_rate"] = settlement.SettlementRate
	data["settlement_date"] = settlement.SettlementDate
	data["settlement_rate_source"] = settlement.RateSource
	data["settled_base_amount"] = settlement.SettlementAmount
	data["realized_fx_gain_loss"] = settlement.RealisedGainLoss
	data["unrealized_fx_cumulative"] = 0
	data["unrealized_fx_released"] = settlement.Position.CumulativeUnrealised
}

// settlementAuditDetail writes the audit line for a settlement. A
// functional-currency settlement reads exactly as it always did; a foreign one
// states the rate and the realised movement, because "we received 85,000 for an
// 83,000 receivable" is the sentence an auditor is going to ask for and
// reconstructing it later from two postings is work nobody should have to do.
func settlementAuditDetail(label, documentID string, settlement FXSettlement) string {
	if !settlement.Position.Foreign {
		return fmt.Sprintf("Settled %s %s amount=%d", label, documentID, settlement.SettlementAmount)
	}
	movement := "no FX difference"
	switch {
	case settlement.RealisedGainLoss > 0:
		movement = fmt.Sprintf("realised gain %d to %s", settlement.RealisedGainLoss, AccountRealisedFXGain)
	case settlement.RealisedGainLoss < 0:
		movement = fmt.Sprintf("realised loss %d to %s", -settlement.RealisedGainLoss, AccountRealisedFXLoss)
	}
	return fmt.Sprintf("Settled %s %s: %.2f %s at %v (%s) = %d %s against carrying %d, %s",
		label, documentID, settlement.Position.TransactionAmount, settlement.Position.Currency,
		settlement.SettlementRate, settlement.RateSource, settlement.SettlementAmount,
		settlement.Position.Functional, settlement.Position.CarryingAmount, movement)
}

// postingOptionsFor builds the currency metadata every FX-aware posting
// carries, so gl_postings records what the entry was denominated in as well as
// what it is worth.
//
// EVERY account in the entry must appear in the transaction maps, including the
// ones whose foreign amount is zero. That is not defensive padding - it is
// required for correctness. PostDoubleEntry deliberately derives a missing
// per-account transaction amount as functional / rate, which is a good default
// for a plain foreign posting whose caller supplied only the rate, but on an FX
// line it invents a number that never existed: a 1,000-rupee realised gain at
// 85.00 was recorded as USD 11.76, and summing transaction_debit for the
// document then reported an exposure that was never owed.
//
// An FX gain or loss line has no foreign-currency value BY CONSTRUCTION - it
// exists precisely because the two currencies disagree - so its honest
// transaction amount is 0. Found in live verification, not by the tests, which
// never read those columns back.
//
// transactionByAccount names the accounts that genuinely carry the foreign
// amount; every other account in debits/credits is zeroed.
func postingOptionsFor(position DocumentFXPosition, rate float64, debits, credits map[string]int, transactionByAccount map[string]float64) PostingOptions {
	if !position.Foreign {
		return PostingOptions{}
	}
	transactionDebits := map[string]float64{}
	transactionCredits := map[string]float64{}
	for account := range debits {
		transactionDebits[account] = transactionByAccount[account]
	}
	for account := range credits {
		transactionCredits[account] = transactionByAccount[account]
	}
	return PostingOptions{
		Currency:           position.Currency,
		ExchangeRate:       rate,
		TransactionDebits:  transactionDebits,
		TransactionCredits: transactionCredits,
	}
}

// ---------------------------------------------------------------------------
// 37.1.4 - period-end revaluation
// ---------------------------------------------------------------------------

// RevaluationLine is one document's revaluation outcome.
type RevaluationLine struct {
	DocType            string  `json:"doctype"`
	DocumentID         string  `json:"document_id"`
	Currency           string  `json:"currency"`
	TransactionAmount  float64 `json:"transaction_amount"`
	BookedRate         float64 `json:"booked_rate"`
	ClosingRate        float64 `json:"closing_rate"`
	BookedAmount       int     `json:"booked_amount"`
	CarryingAmount     int     `json:"carrying_amount"`
	RevaluedAmount     int     `json:"revalued_amount"`
	Adjustment         int     `json:"adjustment"`
	CumulativeAfter    int     `json:"cumulative_unrealised_after"`
	Posted             bool    `json:"posted"`
	SkippedReason      string  `json:"skipped_reason,omitempty"`
	AccountRevalued    string  `json:"account_revalued"`
	AccountGainOrLoss  string  `json:"account_gain_or_loss,omitempty"`
	PreviouslyRevalued string  `json:"previously_revalued_on,omitempty"`
}

// RevaluationRun is the whole result of one period-end revaluation.
type RevaluationRun struct {
	AsOfDate         string            `json:"as_of_date"`
	RateType         string            `json:"rate_type"`
	Functional       string            `json:"functional_currency"`
	DryRun           bool              `json:"dry_run"`
	Lines            []RevaluationLine `json:"lines"`
	TotalAdjustment  int               `json:"total_adjustment"`
	NetGain          int               `json:"net_gain"`
	NetLoss          int               `json:"net_loss"`
	DocumentsPosted  int               `json:"documents_posted"`
	DocumentsSkipped int               `json:"documents_skipped"`
}

// openItemQuery describes one class of open foreign-currency balance.
type openItemQuery struct {
	DocType      string
	AmountField  string
	Statuses     []string
	ControlAcct  string
	IsReceivable bool
}

// revaluableOpenItems is the closed list of what gets revalued, and why each
// entry is on it.
//
// A SalesInvoice is revalued only while Approved: PostSalesInvoice is what put
// the balance in 1300, and SettleSalesInvoice is what takes it out. A Draft has
// never been posted, so revaluing it would create a gain on a balance that does
// not exist in the ledger.
//
// A VendorInvoice is revalued while Matched or Pending Approval - the liability
// is recognised and the cash has not gone. Draft is excluded for the same
// reason as above.
//
// Deliberately NOT a scan of "every currency-bearing doctype": PurchaseOrder,
// SalesOrder, RFQ and PurchaseRequisition are commitments, not booked balances.
// Revaluing a commitment posts a gain against a control account that has no
// corresponding balance, which unbalances the very report this feature exists
// to make correct.
var revaluableOpenItems = []openItemQuery{
	{DocType: "SalesInvoice", AmountField: "total_amount", Statuses: []string{"Approved"},
		ControlAcct: AccountAccountsReceivable, IsReceivable: true},
	{DocType: "VendorInvoice", AmountField: "invoice_amount", Statuses: []string{"Matched", "Pending Approval"},
		ControlAcct: AccountAPSuspense, IsReceivable: false},
}

// RevalueOpenForeignItems restates every open foreign-currency balance at the
// closing rate for asOfDate and books the movement to the unrealised accounts.
//
// dryRun runs the entire calculation and returns exactly what would be posted
// without writing anything. A period-end adjustment that a controller cannot
// preview before it hits the ledger will not be trusted, and this one moves
// numbers on the face of the P&L.
//
// Idempotence is by refusal rather than by silent no-op: a document already
// revalued on or after asOfDate is skipped with a named reason. A silent
// no-op would be indistinguishable from "there was nothing to do", and those
// two answers send a controller to completely different places at close.
func RevalueOpenForeignItems(tenantID, asOfDate, rateType, userID string, dryRun bool) (*RevaluationRun, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	asOfDate = strings.TrimSpace(asOfDate)
	if asOfDate == "" {
		asOfDate = time.Now().UTC().Format("2006-01-02")
	}
	if err := validateISODate("As Of Date", asOfDate, true); err != nil {
		return nil, err
	}
	rateType = strings.TrimSpace(rateType)
	if rateType == "" {
		rateType = strings.TrimSpace(GetSettingString(tenantID, SettingKeyRevaluationRateType))
	}
	if rateType == "" {
		rateType = "Closing"
	}
	if rateType != "Spot" && rateType != "Average" && rateType != "Closing" {
		return nil, &ValidationError{Code: "GLOBAL-0002", SubFor: "Rate Type",
			Message: "rate type must be Spot, Average, or Closing"}
	}

	functional := FunctionalCurrency(tenantID)
	run := &RevaluationRun{AsOfDate: asOfDate, RateType: rateType, Functional: functional, DryRun: dryRun, Lines: []RevaluationLine{}}

	// One rate lookup per currency, not per document. A close with 400 open
	// invoices in 3 currencies should hit the rate table 3 times.
	rateCache := map[string]float64{}
	resolveRate := func(currency string) (float64, error) {
		if rate, ok := rateCache[currency]; ok {
			return rate, nil
		}
		resolved, err := ResolveExchangeRate(tenantID, currency, functional, asOfDate, rateType)
		if err != nil {
			return 0, err
		}
		rateCache[currency] = resolved.Rate
		return resolved.Rate, nil
	}

	for _, class := range revaluableOpenItems {
		placeholders := make([]string, 0, len(class.Statuses))
		args := []interface{}{}
		for index, status := range class.Statuses {
			placeholders = append(placeholders, fmt.Sprintf("$%d", index+1))
			args = append(args, status)
		}
		rows, err := db.DB.Query(fmt.Sprintf(`
			SELECT id, data FROM %s.documents
			WHERE doctype = '%s' AND deleted_at IS NULL
			  AND status IN (%s)
			ORDER BY id`, schema, class.DocType, strings.Join(placeholders, ",")), args...)
		if err != nil {
			return nil, err
		}
		type candidate struct {
			id   string
			data map[string]interface{}
		}
		candidates := []candidate{}
		for rows.Next() {
			var id, dataStr string
			if err := rows.Scan(&id, &dataStr); err != nil {
				rows.Close()
				return nil, err
			}
			var data map[string]interface{}
			if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
				continue
			}
			candidates = append(candidates, candidate{id: id, data: data})
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}

		for _, item := range candidates {
			position := documentFXPosition(tenantID, item.data, class.AmountField)
			if !position.Foreign || position.TransactionAmount <= 0 {
				continue
			}

			line := RevaluationLine{
				DocType: class.DocType, DocumentID: item.id, Currency: position.Currency,
				TransactionAmount: position.TransactionAmount, BookedRate: position.Rate,
				BookedAmount: position.BookedAmount, CarryingAmount: position.CarryingAmount,
				AccountRevalued: class.ControlAcct, PreviouslyRevalued: position.LastRevaluedOn,
			}

			if position.LastRevaluedOn != "" && position.LastRevaluedOn >= asOfDate {
				line.SkippedReason = fmt.Sprintf("already revalued on %s", position.LastRevaluedOn)
				run.Lines = append(run.Lines, line)
				run.DocumentsSkipped++
				continue
			}

			closingRate, rateErr := resolveRate(position.Currency)
			if rateErr != nil {
				line.SkippedReason = fmt.Sprintf("no %s rate for %s to %s on %s", rateType, position.Currency, functional, asOfDate)
				run.Lines = append(run.Lines, line)
				run.DocumentsSkipped++
				continue
			}
			line.ClosingRate = closingRate
			line.RevaluedAmount = roundToRupees(position.TransactionAmount * closingRate)
			line.Adjustment = line.RevaluedAmount - position.CarryingAmount
			line.CumulativeAfter = position.CumulativeUnrealised + line.Adjustment

			if line.Adjustment == 0 {
				line.SkippedReason = "carrying amount already equals the revalued amount"
				run.Lines = append(run.Lines, line)
				run.DocumentsSkipped++
				continue
			}

			debits := map[string]int{}
			credits := map[string]int{}
			// The control account always moves in the direction of the
			// adjustment; the gain/loss account is the other side. On a
			// liability the P&L sign flips, because a payable that grew is a
			// loss even though the balance went up.
			gain := line.Adjustment > 0
			if !class.IsReceivable {
				gain = !gain
			}
			magnitude := line.Adjustment
			if magnitude < 0 {
				magnitude = -magnitude
			}
			if class.IsReceivable {
				if line.Adjustment > 0 {
					debits[class.ControlAcct] = magnitude
				} else {
					credits[class.ControlAcct] = magnitude
				}
			} else {
				// A liability increasing is a credit to the control account.
				if line.Adjustment > 0 {
					credits[class.ControlAcct] = magnitude
				} else {
					debits[class.ControlAcct] = magnitude
				}
			}
			if gain {
				line.AccountGainOrLoss = AccountUnrealisedFXGain
				credits[AccountUnrealisedFXGain] = magnitude
			} else {
				line.AccountGainOrLoss = AccountUnrealisedFXLoss
				debits[AccountUnrealisedFXLoss] = magnitude
			}

			if dryRun {
				run.Lines = append(run.Lines, line)
				run.TotalAdjustment += line.Adjustment
				if gain {
					run.NetGain += magnitude
				} else {
					run.NetLoss += magnitude
				}
				continue
			}

			// The as-of date is passed as the transaction date so the closed
			// period check applies: a revaluation dated into a closed period is
			// exactly the write that check exists to refuse.
			postingKey := fmt.Sprintf("%s:%s:FX_REVAL:%s", class.DocType, item.id, asOfDate)
			if err := PostDoubleEntry(tenantID, class.DocType, item.id, PaiseMap(debits), PaiseMap(credits), asOfDate, postingKey,
				// A revaluation moves no foreign currency at all - the customer
				// still owes exactly the same USD - so no account carries one.
				postingOptionsFor(position, closingRate, debits, credits, nil)); err != nil {
				line.SkippedReason = fmt.Sprintf("GL posting refused: %v", err)
				run.Lines = append(run.Lines, line)
				run.DocumentsSkipped++
				continue
			}

			if err := updateDocumentRevaluationState(schema, class.DocType, item.id, line, closingRate, asOfDate); err != nil {
				return nil, fmt.Errorf("revaluation posted for %s but the document could not be stamped: %v", item.id, err)
			}

			line.Posted = true
			run.Lines = append(run.Lines, line)
			run.TotalAdjustment += line.Adjustment
			run.DocumentsPosted++
			if gain {
				run.NetGain += magnitude
			} else {
				run.NetLoss += magnitude
			}
		}
	}

	if !dryRun && run.DocumentsPosted > 0 {
		LogAuditEvent(tenantID, userID, "FX_REVALUATION", "SUCCESS",
			fmt.Sprintf("Revalued %d open foreign-currency item(s) as of %s at %s rates: net gain %d, net loss %d",
				run.DocumentsPosted, asOfDate, rateType, run.NetGain, run.NetLoss))
	}
	return run, nil
}

// updateDocumentRevaluationState stamps the new cumulative position onto the
// document. Written with jsonb_set against the live row rather than by
// re-serialising the payload this function read earlier, so a concurrent edit
// to an unrelated field is not clobbered by a period-end job.
func updateDocumentRevaluationState(schema, doctype, documentID string, line RevaluationLine, rate float64, asOfDate string) error {
	_, err := db.DB.Exec(fmt.Sprintf(`
		UPDATE %s.documents SET data =
			jsonb_set(
				jsonb_set(
					jsonb_set(
						jsonb_set(data::jsonb, '{unrealized_fx_cumulative}', to_jsonb($1::numeric), true),
						'{last_revalued_on}', to_jsonb($2::text), true),
					'{last_revaluation_rate}', to_jsonb($3::numeric), true),
				'{revalued_base_amount}', to_jsonb($4::numeric), true)::json,
			updated_at = CURRENT_TIMESTAMP
		WHERE doctype = $5 AND id = $6`, schema),
		line.CumulativeAfter, asOfDate, rate, line.RevaluedAmount, doctype, documentID)
	return err
}

// ---------------------------------------------------------------------------
// 37.1.5 - multi-currency reporting
// ---------------------------------------------------------------------------

// FXRegisterRow is one posting to an FX gain/loss account.
type FXRegisterRow struct {
	PostedAt     string  `json:"posted_at"`
	AccountCode  string  `json:"account_code"`
	AccountName  string  `json:"account_name"`
	Kind         string  `json:"kind"`
	DocumentType string  `json:"document_type"`
	DocumentID   string  `json:"document_id"`
	Currency     string  `json:"currency"`
	ExchangeRate float64 `json:"exchange_rate"`
	// Gain/Loss/Net are rupees (Stage 45): gl_postings.debit/credit are paise
	// internally, converted at this report's response boundary via
	// PaiseToRupees so the external contract is unchanged in kind.
	Gain float64 `json:"gain"`
	Loss float64 `json:"loss"`
	Net  float64 `json:"net"`
}

// GetFXGainLossRegister lists every posting to the four FX accounts in a date
// window. This is the report a reviewer opens to answer "where did this FX
// number on the P&L come from", and it reads the ledger directly rather than
// any store of its own - the postings ARE the record, and a second one could
// disagree with them.
func GetFXGainLossRegister(tenantID, fromDate, toDate, kind string) ([]FXRegisterRow, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(fromDate) == "" || strings.TrimSpace(toDate) == "" {
		// Same lesson PostingDate's own comment states two functions above:
		// reckon a time window against one clock, never a mix of two. A
		// default window computed from Go's time.Now().UTC() disagrees with
		// the gl_postings rows it is meant to bound, which are stamped by
		// Postgres's own (server-local) clock - on this Asia/Calcutta
		// deployment that mismatch is 5.5 hours wide, so "today" in UTC is
		// still "yesterday" locally until 05:30, and a posting from the first
		// hour of a local day would silently fall outside its own default
		// window. Bug found live: TestFXGainLossRegisterReadsTheLedger failed
		// deterministically when run between local midnight and 05:30,
		// because the just-posted row's created_at was already "tomorrow" by
		// this function's UTC-clock reckoning.
		var today string
		if err := db.DB.QueryRow("SELECT CURRENT_DATE::text").Scan(&today); err != nil {
			return nil, err
		}
		todayT, terr := time.Parse("2006-01-02", today)
		if terr != nil {
			return nil, terr
		}
		if strings.TrimSpace(fromDate) == "" {
			fromDate = todayT.AddDate(0, -3, 0).Format("2006-01-02")
		}
		if strings.TrimSpace(toDate) == "" {
			toDate = today
		}
	}
	if err := validateISODate("From Date", fromDate, true); err != nil {
		return nil, err
	}
	if err := validateISODate("To Date", toDate, true); err != nil {
		return nil, err
	}

	accounts := []string{AccountRealisedFXGain, AccountRealisedFXLoss, AccountUnrealisedFXGain, AccountUnrealisedFXLoss}
	switch strings.TrimSpace(strings.ToLower(kind)) {
	case "realised", "realized":
		accounts = []string{AccountRealisedFXGain, AccountRealisedFXLoss}
	case "unrealised", "unrealized":
		accounts = []string{AccountUnrealisedFXGain, AccountUnrealisedFXLoss}
	}

	// Placeholders built by hand rather than pq.Array, which would mean
	// importing lib/pq into engines for one query - the account list is a
	// fixed, code-defined set of four, so there is no injection surface and no
	// reason to widen this package's imports.
	placeholders := make([]string, 0, len(accounts))
	args := make([]interface{}, 0, len(accounts)+2)
	for index, account := range accounts {
		placeholders = append(placeholders, fmt.Sprintf("$%d", index+1))
		args = append(args, account)
	}
	args = append(args, fromDate, toDate)

	// The half-open upper bound rather than a cast on created_at, per the
	// convention at the top of finance_reports_stage26.go: casting the indexed
	// column can never be a range seek.
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT to_char(p.created_at, 'YYYY-MM-DD'), p.account_code,
		       COALESCE(a.account_name, ''), COALESCE(p.document_type, ''),
		       COALESCE(p.document_id, ''), COALESCE(p.currency, ''),
		       COALESCE(p.exchange_rate, 0), p.debit, p.credit
		FROM %s.gl_postings p
		LEFT JOIN %s.gl_accounts a ON a.account_code = p.account_code
		WHERE p.account_code IN (%s)
		  AND p.created_at >= $%d::date AND p.created_at < ($%d::date + 1)
		ORDER BY p.created_at DESC, p.posting_id DESC`, schema, schema,
		strings.Join(placeholders, ","), len(accounts)+1, len(accounts)+2),
		args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []FXRegisterRow{}
	for rows.Next() {
		var row FXRegisterRow
		var debitPaise, creditPaise int64
		if err := rows.Scan(&row.PostedAt, &row.AccountCode, &row.AccountName, &row.DocumentType,
			&row.DocumentID, &row.Currency, &row.ExchangeRate, &debitPaise, &creditPaise); err != nil {
			return nil, err
		}
		switch row.AccountCode {
		case AccountRealisedFXGain, AccountRealisedFXLoss:
			row.Kind = "Realised"
		default:
			row.Kind = "Unrealised"
		}
		// A gain account is credited and a loss account debited, so the natural
		// balance of each is what the P&L picks up. Presented as two columns
		// plus a net, because a register that shows only a signed number makes
		// the reader work out the sign convention from the account name.
		row.Gain = PaiseToRupees(creditPaise)
		row.Loss = PaiseToRupees(debitPaise)
		row.Net = PaiseToRupees(creditPaise - debitPaise)
		out = append(out, row)
	}
	return out, rows.Err()
}

// PresentationBalanceRow is one account translated into a presentation
// currency.
type PresentationBalanceRow struct {
	AccountCode  string  `json:"account_code"`
	AccountName  string  `json:"account_name"`
	AccountType  string  `json:"account_type"`
	Functional   float64 `json:"functional_balance"`
	Presentation float64 `json:"presentation_balance"`
	Currency     string  `json:"presentation_currency"`
	Rate         float64 `json:"rate_applied"`
	RateType     string  `json:"rate_type"`
}

// GetTrialBalanceInPresentationCurrency is 37.1.5 proper: the trial balance
// translated out of the functional currency into a chosen presentation
// currency at a chosen rate type.
//
// ONE RATE FOR EVERY LINE, DELIBERATELY. A full translation under IAS 21 uses
// the closing rate for balance-sheet items, the average for P&L items and
// historical rates for equity - which needs an equity movement schedule and a
// cumulative translation reserve this codebase does not have, and inventing a
// half-version of that would produce a statement that looks authoritative and
// balances to nothing. What this does is a single-rate convenience translation:
// honest, useful for "roughly what does this look like in dollars", and it says
// so in its own rate_type column on every row. A real consolidation is 37.2's
// job, and it is listed there.
func GetTrialBalanceInPresentationCurrency(tenantID, asOfDate, presentationCurrency, rateType string) (map[string]interface{}, error) {
	if strings.TrimSpace(asOfDate) == "" {
		asOfDate = time.Now().UTC().Format("2006-01-02")
	}
	if err := validateISODate("As Of Date", asOfDate, true); err != nil {
		return nil, err
	}
	functional := FunctionalCurrency(tenantID)
	presentationCurrency = strings.ToUpper(strings.TrimSpace(presentationCurrency))
	if presentationCurrency == "" {
		presentationCurrency = functional
	}
	if !isoCurrencyCodePattern.MatchString(presentationCurrency) {
		return nil, &ValidationError{Code: "GLOBAL-0002", SubFor: "Presentation Currency",
			Message: "presentation currency must be a three-letter ISO code such as INR or USD"}
	}
	if strings.TrimSpace(rateType) == "" {
		rateType = "Closing"
	}

	rate := 1.0
	if presentationCurrency != functional {
		resolved, err := ResolveExchangeRate(tenantID, functional, presentationCurrency, asOfDate, rateType)
		if err != nil {
			return nil, &ValidationError{Code: "FIN-0021", SubFor: "Exchange Rate",
				Message: fmt.Sprintf("cannot translate %s into %s on %s at %s rates: %v", functional, presentationCurrency, asOfDate, rateType, err)}
		}
		rate = resolved.Rate
	}

	balance, err := GetTrialBalance(tenantID, asOfDate)
	if err != nil {
		return nil, err
	}

	raw, _ := json.Marshal(balance["balances"])
	var accounts []struct {
		Code   string  `json:"account_code"`
		Name   string  `json:"account_name"`
		Type   string  `json:"account_type"`
		Debit  float64 `json:"debit"`
		Credit float64 `json:"credit"`
	}
	if err := json.Unmarshal(raw, &accounts); err != nil {
		return nil, err
	}

	translated := []PresentationBalanceRow{}
	var totalFunctional float64
	var totalPresentation float64
	for _, account := range accounts {
		net := account.Debit - account.Credit
		if net == 0 && account.Debit == 0 && account.Credit == 0 {
			continue
		}
		converted := math.Round(net*rate*100) / 100
		translated = append(translated, PresentationBalanceRow{
			AccountCode: account.Code, AccountName: account.Name, AccountType: account.Type,
			Functional: net, Presentation: converted, Currency: presentationCurrency,
			Rate: rate, RateType: rateType,
		})
		totalFunctional += net
		totalPresentation += converted
	}

	return map[string]interface{}{
		"as_of":                 asOfDate,
		"functional_currency":   functional,
		"presentation_currency": presentationCurrency,
		"rate_type":             rateType,
		"rate_applied":          rate,
		"rows":                  translated,
		"total_functional":      totalFunctional,
		"total_presentation":    math.Round(totalPresentation*100) / 100,
		// The functional total is zero on a balanced ledger; the presentation
		// total is reported alongside so a rounding residue from the
		// translation is visible rather than hidden inside the rows.
		"balanced": totalFunctional == 0,
	}, nil
}

// GetOpenFXExposure lists every open foreign-currency balance with what it
// would be worth at the given rate type today. It is the read-only half of the
// revaluation - the report a treasurer opens to see exposure without running a
// close - and it is what report 37.1.5 registers.
func GetOpenFXExposure(tenantID, asOfDate, rateType string) ([]RevaluationLine, error) {
	run, err := RevalueOpenForeignItems(tenantID, asOfDate, rateType, "", true)
	if err != nil {
		return nil, err
	}
	return run.Lines, nil
}
