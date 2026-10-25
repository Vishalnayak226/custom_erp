package engines

import (
	"context"
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"strings"
	"time"
)

// Stage 55.16 (Batch D, the user's decision "post depreciation monthly"):
// depreciation is no longer only calculated for the register - a monthly job
// posts it to the books, and a disposal records its proceeds and the gain or
// loss.
//
//   * Straight line by month: cost / (useful life x 12) a month, the last
//     month taking the paise remainder so the months add up to cost exactly.
//     Month 1 is the calendar month the asset was capitalised in; a month is
//     posted once it has ended. Dr 5310 Depreciation Expense / Cr 1410
//     Accumulated Depreciation, document_type "Asset", one posting per month
//     under its own key Asset:<id>:DEP:<YYYY-MM>, so a re-run, a catch-up for
//     an asset capitalised long ago, and the job racing a manual run can never
//     post a month twice.
//   * Each month is posted with its month-end as the transaction date, so a
//     month inside a closed accounting period is refused by the same
//     rejectIfCurrentPeriodClosed every posting goes through. It is skipped and
//     reported - never re-dated to today - and posts on the first run after
//     the period is reopened. Its row is stamped with the month-end, so period
//     reports (which read gl_postings.created_at) carry each month's charge in
//     its own month rather than in the month the job happened to run.
//   * Disposal: depreciation due is posted first, then Dr 1410 (everything
//     posted), Dr the cash/bank account (proceeds), Cr 1400 (cost), and the
//     difference as Dr 5300 Loss or Cr 4300 Gain on Asset Disposal. 1400 and
//     1410 end at zero for the asset. Assets disposed before this change keep
//     their old entry; nothing re-posts them.

const (
	assetAccFixed       = "1400"
	assetAccAccumulated = "1410"
	assetAccDepExpense  = "5310"
	assetAccLoss        = "5300"
	assetAccGain        = "4300"
	assetAccCash        = "1100"

	// AssetDepreciationJobType is the async job the monthly scheduler queues.
	AssetDepreciationJobType = "asset_depreciation"
)

// assetDepPlan is one asset's straight-line monthly plan.
type assetDepPlan struct {
	start     time.Time // first day of month 1 (the capitalisation month)
	months    int       // useful life in months
	costPaise int64     // what capitalisation posted to 1400
}

// assetCostPaise is what capitalisation debits (or debited) to 1400, the
// figure depreciation and disposal must clear exactly. Stage 132.17: it is
// now the cost to the paisa, recorded as capitalised_paise - int(cost) had
// dropped the paise. An asset capitalised before then posted whole rupees and
// has no record, so it keeps that figure.
func assetCostPaise(data map[string]interface{}) int64 {
	if p, ok := data["capitalised_paise"].(float64); ok && p > 0 {
		return int64(p)
	}
	cost, _ := data["cost"].(float64)
	if s, _ := data["status"].(string); s == "" || s == "Draft" {
		return RupeesToPaise(cost)
	}
	return int64(int(cost)) * 100
}

// assetDepreciationPlan reads the plan from an asset's data. capDate
// overrides the stored capitalisation date (a Draft's preview).
func assetDepreciationPlan(data map[string]interface{}, capDate string) (assetDepPlan, bool) {
	life, _ := data["useful_life_years"].(float64)
	if capDate == "" {
		capDate, _ = data["capitalisation_date"].(string)
	}
	plan := assetDepPlan{costPaise: assetCostPaise(data), months: int(math.Round(life * 12))}
	t, err := time.Parse("2006-01-02", capDate)
	if err != nil || plan.months <= 0 || plan.costPaise <= 0 {
		return plan, false
	}
	plan.start = time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
	return plan, true
}

func (p assetDepPlan) monthStart(i int) time.Time { return p.start.AddDate(0, i-1, 0) }
func (p assetDepPlan) monthEnd(i int) time.Time   { return p.start.AddDate(0, i, -1) }
func (p assetDepPlan) monthKey(i int) string      { return p.monthStart(i).Format("2006-01") }

// amount is month i's charge in paise; the last month takes the remainder.
func (p assetDepPlan) amount(i int) int64 {
	per := p.costPaise / int64(p.months)
	if i == p.months {
		return p.costPaise - per*int64(p.months-1)
	}
	return per
}

// monthsDue is how many of the plan's months have ended before now's month.
func (p assetDepPlan) monthsDue(now time.Time) int {
	due := (now.Year()-p.start.Year())*12 + int(now.Month()) - int(p.start.Month())
	if due < 0 {
		return 0
	}
	if due > p.months {
		return p.months
	}
	return due
}

func assetDepreciationKey(assetID, month string) string {
	return fmt.Sprintf("Asset:%s:DEP:%s", assetID, month)
}

type assetDepPosting struct {
	amount   int64
	postedAt string
	postedBy string
}

type assetRowsQuerier interface {
	Query(query string, args ...interface{}) (*sql.Rows, error)
}

// loadAssetDepreciationPostings returns the asset's posted months
// (YYYY-MM -> its 1410 credit).
func loadAssetDepreciationPostings(q assetRowsQuerier, schema, assetID string) (map[string]assetDepPosting, error) {
	rows, err := q.Query(fmt.Sprintf(`
		SELECT idempotency_key, credit - debit, to_char(created_at, 'YYYY-MM-DD'), COALESCE(posted_by, '')
		FROM %s.gl_postings
		WHERE document_type = 'Asset' AND document_id = $1 AND account_code = '%s' AND idempotency_key IS NOT NULL`,
		schema, assetAccAccumulated), assetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	posted := map[string]assetDepPosting{}
	for rows.Next() {
		var key, at, by string
		var amount int64
		if err := rows.Scan(&key, &amount, &at, &by); err != nil {
			return nil, err
		}
		if i := strings.LastIndex(key, ":DEP:"); i >= 0 {
			month := key[i+len(":DEP:"):]
			prev := posted[month]
			posted[month] = assetDepPosting{amount: prev.amount + amount, postedAt: at, postedBy: by}
		}
	}
	return posted, rows.Err()
}

// postedAccumulatedDepreciation is every asset's 1410 balance from its own
// depreciation postings (asset id -> paise), for the register.
func postedAccumulatedDepreciation(schema string) (map[string]int64, error) {
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT document_id, COALESCE(SUM(credit - debit), 0) FROM %s.gl_postings
		WHERE document_type = 'Asset' AND account_code = '%s' GROUP BY document_id`, schema, assetAccAccumulated))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var id string
		var paise int64
		if err := rows.Scan(&id, &paise); err != nil {
			return nil, err
		}
		out[id] = paise
	}
	return out, rows.Err()
}

// AssetDepreciationSkip is a due month that was not posted, and why.
type AssetDepreciationSkip struct {
	Month  string `json:"month"`
	Reason string `json:"reason"`
}

// AssetDepreciationOutcome is one asset's part of a run.
type AssetDepreciationOutcome struct {
	AssetID      string                  `json:"asset_id"`
	Code         string                  `json:"code"`
	MonthsPosted []string                `json:"months_posted"`
	AmountPosted float64                 `json:"amount_posted"`
	Skipped      []AssetDepreciationSkip `json:"months_skipped"`
	Note         string                  `json:"note,omitempty"`
}

// AssetDepreciationRunResult is what one run did.
type AssetDepreciationRunResult struct {
	ThroughMonth  string                     `json:"through_month"`
	AssetsChecked int                        `json:"assets_checked"`
	MonthsPosted  int                        `json:"months_posted"`
	MonthsSkipped int                        `json:"months_skipped"`
	AmountPosted  float64                    `json:"amount_posted"`
	Assets        []AssetDepreciationOutcome `json:"assets"`
}

func closedPeriodReason(err error) (string, bool) {
	var verr *ValidationError
	if errors.As(err, &verr) && verr.Code == "FIN-0260" {
		return strings.TrimPrefix(verr.Message, "cannot post: ") + " - posts after the period is reopened", true
	}
	return "", false
}

// closedDepreciationMonths asks the posting's own period rule
// (rejectIfCurrentPeriodClosed, read-only) which of these months would be
// refused, so a preview says exactly what a run would skip.
func closedDepreciationMonths(schema, assetID string, months []string) (map[string]string, error) {
	closed := map[string]string{}
	if len(months) == 0 {
		return closed, nil
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	for _, month := range months {
		start, err := time.Parse("2006-01", month)
		if err != nil {
			continue
		}
		monthEnd := start.AddDate(0, 1, -1).Format("2006-01-02")
		checkErr := rejectIfCurrentPeriodClosed(tx, schema, "Asset", assetID, monthEnd)
		if reason, isClosed := closedPeriodReason(checkErr); isClosed {
			closed[month] = reason
		} else if checkErr != nil {
			return nil, checkErr
		}
	}
	return closed, nil
}

// postAssetDepreciationMonth posts one month in its own transaction, holding
// the asset row so a concurrent run or a disposal cannot interleave. It
// reports false when the month was already posted or the asset is no longer
// Capitalised.
func postAssetDepreciationMonth(tenantID, schema, assetID string, plan assetDepPlan, i int) (bool, error) {
	tx, err := db.DB.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var status string
	if err := tx.QueryRow(fmt.Sprintf(`SELECT status FROM %s.documents WHERE doctype = 'Asset' AND id = $1 FOR UPDATE`, schema), assetID).Scan(&status); err != nil {
		return false, err
	}
	if status != "Capitalised" {
		return false, nil
	}
	key := assetDepreciationKey(assetID, plan.monthKey(i))
	var exists bool
	if err := tx.QueryRow(fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM %s.gl_postings WHERE idempotency_key = $1)`, schema), key).Scan(&exists); err != nil {
		return false, err
	}
	if exists {
		return false, nil
	}
	monthEnd := plan.monthEnd(i).Format("2006-01-02")
	amount := plan.amount(i)
	if err := PostDoubleEntryTx(tx, tenantID, schema, "Asset", assetID,
		map[string]int64{assetAccDepExpense: amount}, map[string]int64{assetAccAccumulated: amount}, monthEnd, key); err != nil {
		return false, err
	}
	// The month's charge belongs to the month: period reports read created_at.
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.gl_postings SET created_at = $1::date + interval '23:59:59' WHERE idempotency_key = $2`, schema), monthEnd, key); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

// depreciateAsset posts every ended, unposted month of one Capitalised asset.
// A month in a closed period is skipped and reported; any other failure stops
// this asset and is returned.
func depreciateAsset(tenantID, schema, assetID string, data map[string]interface{}, now time.Time) (AssetDepreciationOutcome, error) {
	out := AssetDepreciationOutcome{AssetID: assetID, MonthsPosted: []string{}, Skipped: []AssetDepreciationSkip{}}
	out.Code, _ = data["code"].(string)
	plan, ok := assetDepreciationPlan(data, "")
	if !ok {
		out.Note = "no useful life, cost or capitalisation date - depreciation cannot be worked out"
		return out, nil
	}
	due := plan.monthsDue(now)
	if due == 0 {
		return out, nil
	}
	posted, err := loadAssetDepreciationPostings(db.DB, schema, assetID)
	if err != nil {
		return out, err
	}
	for i := 1; i <= due; i++ {
		month := plan.monthKey(i)
		if _, done := posted[month]; done {
			continue
		}
		didPost, err := postAssetDepreciationMonth(tenantID, schema, assetID, plan, i)
		if reason, closed := closedPeriodReason(err); closed {
			out.Skipped = append(out.Skipped, AssetDepreciationSkip{Month: month, Reason: reason})
			continue
		}
		if err != nil {
			out.Skipped = append(out.Skipped, AssetDepreciationSkip{Month: month, Reason: "could not be posted - see System Errors"})
			return out, fmt.Errorf("asset %s month %s: %v", assetID, month, err)
		}
		if didPost {
			out.MonthsPosted = append(out.MonthsPosted, month)
			out.AmountPosted += PaiseToRupees(plan.amount(i))
		}
	}
	out.AmountPosted = math.Round(out.AmountPosted*100) / 100
	return out, nil
}

func lastCompletedMonth(now time.Time) string {
	return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -1, 0).Format("2006-01")
}

// RunAssetDepreciation posts every Capitalised asset's ended, unposted
// months. Safe to run any number of times.
func RunAssetDepreciation(ctx context.Context, tenantID string) (*AssetDepreciationRunResult, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, data FROM %s.documents WHERE doctype = 'Asset' AND status = 'Capitalised' ORDER BY id`, schema))
	if err != nil {
		return nil, err
	}
	type assetRow struct{ id, data string }
	var assets []assetRow
	for rows.Next() {
		var a assetRow
		if err := rows.Scan(&a.id, &a.data); err != nil {
			rows.Close()
			return nil, err
		}
		assets = append(assets, a)
	}
	rows.Close()

	now := time.Now()
	result := &AssetDepreciationRunResult{ThroughMonth: lastCompletedMonth(now), Assets: []AssetDepreciationOutcome{}}
	for _, a := range assets {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		var data map[string]interface{}
		if err := json.Unmarshal([]byte(a.data), &data); err != nil {
			log.Printf("[ASSET-DEPRECIATION] corrupt Asset %s: %v", a.id, err)
			continue
		}
		result.AssetsChecked++
		outcome, err := depreciateAsset(tenantID, schema, a.id, data, now)
		if err != nil {
			LogSystemError(tenantID, "", "ERROR", "RunAssetDepreciation", err.Error(), "")
		}
		result.MonthsPosted += len(outcome.MonthsPosted)
		result.MonthsSkipped += len(outcome.Skipped)
		result.AmountPosted += outcome.AmountPosted
		if len(outcome.MonthsPosted) > 0 || len(outcome.Skipped) > 0 || outcome.Note != "" {
			result.Assets = append(result.Assets, outcome)
		}
	}
	result.AmountPosted = math.Round(result.AmountPosted*100) / 100
	return result, nil
}

// ---------------------------------------------------------------------------
// Disposal with proceeds.
// ---------------------------------------------------------------------------

// AssetDisposalLine is one line of the disposal entry.
type AssetDisposalLine struct {
	Account string  `json:"account_code"`
	Name    string  `json:"account_name"`
	Debit   float64 `json:"debit"`
	Credit  float64 `json:"credit"`
}

// AssetReceiptAccount is a cash/bank account proceeds can be received into.
type AssetReceiptAccount struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// AssetDisposal is a disposal's figures and entry - the preview, or what was
// posted.
type AssetDisposal struct {
	AssetID                 string                  `json:"asset_id"`
	Code                    string                  `json:"code"`
	DisposalType            string                  `json:"disposal_type,omitempty"`
	Cost                    float64                 `json:"cost"`
	AccumulatedDepreciation float64                 `json:"accumulated_depreciation"`
	BookValue               float64                 `json:"book_value"`
	Proceeds                float64                 `json:"proceeds"`
	ReceivedInto            string                  `json:"received_into"`
	GainLoss                float64                 `json:"gain_loss"`
	Lines                   []AssetDisposalLine     `json:"lines"`
	DepreciationFirst       []string                `json:"depreciation_first"`
	DepreciationSkipped     []AssetDepreciationSkip `json:"depreciation_skipped"`
	ReceiptAccounts         []AssetReceiptAccount   `json:"receipt_accounts,omitempty"`
}

// assetReceiptAccounts is 1100 Cash/Bank plus every BankAccount's GL code
// (bankGLAccountsSQL, the payroll reconciliation's own list).
func assetReceiptAccounts(schema string) ([]AssetReceiptAccount, error) {
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT account_code, account_name FROM %s.gl_accounts WHERE account_code IN (%s) ORDER BY account_code`,
		schema, bankGLAccountsSQL(schema)))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AssetReceiptAccount{}
	for rows.Next() {
		var a AssetReceiptAccount
		if err := rows.Scan(&a.Code, &a.Name); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func validateAssetDisposalInputs(schema string, proceeds float64, receivedInto string) (string, error) {
	if math.IsNaN(proceeds) || math.IsInf(proceeds, 0) || proceeds < 0 {
		return "", &ValidationError{Code: "ASSET-0537", SubFor: "Proceeds", Message: "proceeds must be zero or more"}
	}
	receivedInto = strings.TrimSpace(receivedInto)
	if receivedInto == "" {
		receivedInto = assetAccCash
	}
	accounts, err := assetReceiptAccounts(schema)
	if err != nil {
		return "", err
	}
	for _, a := range accounts {
		if a.Code == receivedInto {
			return receivedInto, nil
		}
	}
	return "", &ValidationError{Code: "ASSET-0538", SubFor: "Received Into", Message: fmt.Sprintf("account '%s' is not 1100 Cash/Bank or a bank account's GL account", receivedInto)}
}

// assetDisposalEntry builds the disposal entry in paise.
func assetDisposalEntry(costPaise, accumulatedPaise, proceedsPaise int64, receivedInto string) (debits, credits map[string]int64, gainLoss int64) {
	debits = map[string]int64{assetAccAccumulated: accumulatedPaise}
	credits = map[string]int64{assetAccFixed: costPaise}
	if proceedsPaise > 0 {
		debits[receivedInto] += proceedsPaise
	}
	gainLoss = proceedsPaise - (costPaise - accumulatedPaise)
	if gainLoss < 0 {
		debits[assetAccLoss] += -gainLoss
	} else if gainLoss > 0 {
		credits[assetAccGain] += gainLoss
	}
	return debits, credits, gainLoss
}

// assetDisposalLines lists the entry in reading order: 1410, the cash/bank
// account, a loss, then 1400 and a gain.
func assetDisposalLines(schema, receivedInto string, debits, credits map[string]int64) []AssetDisposalLine {
	names := map[string]string{}
	if rows, err := db.DB.Query(fmt.Sprintf(`SELECT account_code, account_name FROM %s.gl_accounts WHERE account_code IN ($1, $2, $3, $4, $5)`, schema),
		assetAccAccumulated, receivedInto, assetAccLoss, assetAccFixed, assetAccGain); err == nil {
		for rows.Next() {
			var code, name string
			if rows.Scan(&code, &name) == nil {
				names[code] = name
			}
		}
		rows.Close()
	}
	lines := []AssetDisposalLine{}
	for _, code := range []string{assetAccAccumulated, receivedInto, assetAccLoss} {
		if debits[code] > 0 {
			lines = append(lines, AssetDisposalLine{Account: code, Name: names[code], Debit: PaiseToRupees(debits[code])})
		}
	}
	for _, code := range []string{assetAccFixed, assetAccGain} {
		if credits[code] > 0 {
			lines = append(lines, AssetDisposalLine{Account: code, Name: names[code], Credit: PaiseToRupees(credits[code])})
		}
	}
	return lines
}

// PreviewAssetDisposal works out what disposing now would post, without
// posting anything - including the depreciation that would be posted first.
func PreviewAssetDisposal(tenantID, assetID string, proceeds float64, receivedInto string) (*AssetDisposal, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	receivedInto, err = validateAssetDisposalInputs(schema, proceeds, receivedInto)
	if err != nil {
		return nil, err
	}
	data, status, err := fetchAssetData(tenantID, assetID)
	if err != nil {
		return nil, fmt.Errorf("asset not found: %v", err)
	}
	if status != "Capitalised" {
		return nil, &ValidationError{Code: "ASSET-0270", Message: fmt.Sprintf("only a Capitalised asset can be disposed (current status: %s)", status)}
	}
	posted, err := loadAssetDepreciationPostings(db.DB, schema, assetID)
	if err != nil {
		return nil, err
	}
	var accumulated int64
	for _, p := range posted {
		accumulated += p.amount
	}
	preview := &AssetDisposal{AssetID: assetID, DepreciationFirst: []string{}, DepreciationSkipped: []AssetDepreciationSkip{}}
	preview.Code, _ = data["code"].(string)
	if plan, ok := assetDepreciationPlan(data, ""); ok {
		var unposted []string
		for i := 1; i <= plan.monthsDue(time.Now()); i++ {
			if _, done := posted[plan.monthKey(i)]; !done {
				unposted = append(unposted, plan.monthKey(i))
			}
		}
		closed, err := closedDepreciationMonths(schema, assetID, unposted)
		if err != nil {
			return nil, err
		}
		for i := 1; i <= plan.monthsDue(time.Now()); i++ {
			month := plan.monthKey(i)
			if _, done := posted[month]; done {
				continue
			}
			if reason, isClosed := closed[month]; isClosed {
				preview.DepreciationSkipped = append(preview.DepreciationSkipped, AssetDepreciationSkip{Month: month, Reason: reason})
				continue
			}
			preview.DepreciationFirst = append(preview.DepreciationFirst, month)
			accumulated += plan.amount(i)
		}
	}
	costPaise := assetCostPaise(data)
	debits, credits, gainLoss := assetDisposalEntry(costPaise, accumulated, RupeesToPaise(proceeds), receivedInto)
	preview.Cost = PaiseToRupees(costPaise)
	preview.AccumulatedDepreciation = PaiseToRupees(accumulated)
	preview.BookValue = PaiseToRupees(costPaise - accumulated)
	preview.Proceeds = PaiseToRupees(RupeesToPaise(proceeds))
	preview.ReceivedInto = receivedInto
	preview.GainLoss = PaiseToRupees(gainLoss)
	preview.Lines = assetDisposalLines(schema, receivedInto, debits, credits)
	if preview.ReceiptAccounts, err = assetReceiptAccounts(schema); err != nil {
		return nil, err
	}
	return preview, nil
}

// DisposeAssetWithProceeds disposes of a Capitalised asset: posts the
// depreciation due up to the disposal month, then the disposal entry with
// its proceeds and gain or loss, and closes the asset.
func DisposeAssetWithProceeds(tenantID, assetID, disposalType string, proceeds float64, receivedInto string) (*AssetDisposal, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	receivedInto, err = validateAssetDisposalInputs(schema, proceeds, receivedInto)
	if err != nil {
		return nil, err
	}
	data, status, err := fetchAssetData(tenantID, assetID)
	if err != nil {
		return nil, fmt.Errorf("asset not found: %v", err)
	}
	if status != "Capitalised" {
		return nil, &ValidationError{Code: "ASSET-0270", Message: fmt.Sprintf("only a Capitalised asset can be disposed (current status: %s)", status)}
	}
	outcome, err := depreciateAsset(tenantID, schema, assetID, data, time.Now())
	if err != nil {
		LogSystemError(tenantID, "", "ERROR", "DisposeAssetWithProceeds", err.Error(), "")
		return nil, &ValidationError{Code: "ASSET-0539", Message: "the depreciation due on this asset could not be posted, so it was not disposed"}
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var dataStr string
	if err := tx.QueryRow(fmt.Sprintf(`SELECT data, status FROM %s.documents WHERE doctype = 'Asset' AND id = $1 FOR UPDATE`, schema), assetID).Scan(&dataStr, &status); err != nil {
		return nil, fmt.Errorf("asset not found: %v", err)
	}
	if status != "Capitalised" {
		return nil, &ValidationError{Code: "ASSET-0270", Message: fmt.Sprintf("only a Capitalised asset can be disposed (current status: %s)", status)}
	}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return nil, err
	}
	posted, err := loadAssetDepreciationPostings(tx, schema, assetID)
	if err != nil {
		return nil, err
	}
	var accumulated int64
	for _, p := range posted {
		accumulated += p.amount
	}
	costPaise := assetCostPaise(data)
	proceedsPaise := RupeesToPaise(proceeds)
	debits, credits, gainLoss := assetDisposalEntry(costPaise, accumulated, proceedsPaise, receivedInto)
	if err := PostDoubleEntryTx(tx, tenantID, schema, "Asset-Disposal", assetID, debits, credits, "", fmt.Sprintf("Asset:%s:DISPOSE", assetID)); err != nil {
		return nil, err
	}
	today := time.Now().Format("2006-01-02")
	data["disposal_type"] = disposalType
	data["disposal_date"] = today
	data["disposal_proceeds"] = PaiseToRupees(proceedsPaise)
	data["disposal_received_into"] = receivedInto
	data["disposal_book_value"] = PaiseToRupees(costPaise - accumulated)
	data["disposal_gain_loss"] = PaiseToRupees(gainLoss)
	data["status"] = "Disposed"
	marshaled, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $1, status = 'Disposed', updated_at = CURRENT_TIMESTAMP WHERE doctype = 'Asset' AND id = $2`, schema), marshaled, assetID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	result := &AssetDisposal{
		AssetID: assetID, DisposalType: disposalType, Cost: PaiseToRupees(costPaise),
		AccumulatedDepreciation: PaiseToRupees(accumulated), BookValue: PaiseToRupees(costPaise - accumulated),
		Proceeds: PaiseToRupees(proceedsPaise), ReceivedInto: receivedInto, GainLoss: PaiseToRupees(gainLoss),
		Lines: assetDisposalLines(schema, receivedInto, debits, credits), DepreciationFirst: outcome.MonthsPosted, DepreciationSkipped: outcome.Skipped,
	}
	result.Code, _ = data["code"].(string)
	return result, nil
}

// ---------------------------------------------------------------------------
// The monthly job: Stage 38.6's runner, queued once per tenant per calendar
// month by an hourly scheduler (StartAuditArchiveScheduler's enqueue-only
// split). Each run posts the month that just ended plus anything still
// missing, so a month skipped for a closed period posts on the first run
// after it is reopened.
// ---------------------------------------------------------------------------

func init() {
	RegisterJobHandler(AssetDepreciationJobType, runAssetDepreciationJob)
}

func runAssetDepreciationJob(ctx context.Context, schema string, job Job) (map[string]interface{}, error) {
	tenantID := schemaToTenantID(schema)
	if tenantID == "" {
		return nil, fmt.Errorf("no tenant for schema %s", schema)
	}
	result, err := RunAssetDepreciation(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"through_month": result.ThroughMonth, "assets_checked": result.AssetsChecked,
		"months_posted": result.MonthsPosted, "months_skipped": result.MonthsSkipped, "amount_posted": result.AmountPosted,
	}, nil
}

// StartAssetDepreciationScheduler queues the month's depreciation job for
// every tenant with Fixed Assets switched on. The key carries the month, so
// the hourly tick queues it once a month.
func StartAssetDepreciationScheduler(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if !IsSchedulerLeader() { // 134.7: one server runs each sweep
					continue
				}
				if db.DB == nil {
					continue
				}
				schemas, err := listTenantSchemas("assets")
				if err != nil {
					log.Printf("[ASSET-DEPRECIATION] cannot list tenant schemas: %v", err)
					continue
				}
				month := time.Now().Format("2006-01")
				for _, schema := range schemas {
					tenantID := schemaToTenantID(schema)
					if tenantID == "" {
						continue
					}
					if _, err := EnqueueJob(tenantID, AssetDepreciationJobType, nil,
						fmt.Sprintf("asset-depreciation:%s:%s", tenantID, month)); err != nil {
						log.Printf("[ASSET-DEPRECIATION] %s: could not enqueue: %v", schema, err)
					}
				}
			}
		}
	}()
}
