package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
)

// Stage 37.2: Multi-entity & intercompany. LegalEntity (Stage 17.9) already
// exists as a Master, linked from Location.legal_entity, but nothing
// transacted across entities before this. Five pieces:
//
//  37.2.1 Entity-scoped posting: PostingOptions.Entity (engines/finance.go)
//         + JournalVoucherOptions.Entity, following the exact
//         CostCenter/Department precedent (Stage 26.6.8).
//  37.2.2 IntercompanyTransaction: a Draft->Approved->Posted doctype (the
//         JournalVoucher maker-checker shape) whose approval posts TWO
//         balanced PostDoubleEntry calls, one per entity's book, sharing
//         the new 1700/2500 control account pair.
//  37.2.3 GetIntercompanyReconciliation: per entity-pair, the IC ledger's
//         own claim vs. the GL control-account balance it should equal.
//  37.2.4 ComputeIntercompanyEliminations: the consolidation-worksheet
//         adjustment that nets 1700 against 2500, and the revenue/expense
//         pair too when the chosen accounts are typed that way.
//  37.2.5 GetConsolidatedTrialBalance: the tenant-wide trial balance before
//         and after 37.2.4's eliminations, plus a per-entity trial balance.

// validateLegalEntityReferenceInSchema/validateLegalEntityReference mirror
// validateCostCenterReferenceInSchema's exact shape (engines/gl_cost_center.go)
// - empty is always valid (Entity is optional on every existing posting
// path), a non-empty value must be a registered, Active LegalEntity.
func validateLegalEntityReferenceInSchema(schema, entity string) error {
	if entity == "" {
		return nil
	}
	var status string
	err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT status FROM %s.documents WHERE doctype = 'LegalEntity' AND id = $1 AND deleted_at IS NULL`, schema),
		entity).Scan(&status)
	if err == sql.ErrNoRows {
		return fmt.Errorf("entity '%s' is not a registered LegalEntity", entity)
	}
	if err != nil {
		return err
	}
	if status != "Active" {
		return fmt.Errorf("entity '%s' is not Active", entity)
	}
	return nil
}

func validateLegalEntityReference(tenantID, entity string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	return validateLegalEntityReferenceInSchema(schema, entity)
}

// validateGLAccountCodeInSchema confirms an account code is real before an
// IntercompanyTransaction is allowed to name it - PostDoubleEntry's own
// gl_postings.account_code foreign key would eventually reject a bad code
// too, but only at posting time (after approval), which is a far worse place
// to discover a typo than at creation. Returns the account's type so callers
// (37.2.4's elimination) can tell a Revenue/Expense account from an
// Asset/Liability one without a second query.
func validateGLAccountCodeInSchema(schema, code string) (accountType string, err error) {
	err = db.DB.QueryRow(fmt.Sprintf(
		`SELECT account_type FROM %s.gl_accounts WHERE account_code = $1`, schema), code).Scan(&accountType)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("account code '%s' is not a registered GL account", code)
	}
	return accountType, err
}

// ValidateIntercompanyTransactionDocument runs at ValidateDocument's shared
// exit (engines/currency.go's dispatcher) so a document written through the
// generic API - not just CreateIntercompanyTransaction below - cannot skip
// the referential checks that make the mirrored posting meaningful. It
// cannot enforce the balance/posting itself (that only happens on approval),
// the same boundary JournalVoucher's own generic-create path already
// accepts; see CreateIntercompanyTransaction's comment for why creation goes
// through the dedicated engine function.
func ValidateIntercompanyTransactionDocument(tenantID string, payload map[string]interface{}) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	fromEntity := pimString(payload["from_entity"])
	toEntity := pimString(payload["to_entity"])
	if fromEntity == "" || toEntity == "" {
		return nil // GLOBAL-0001 (mandatory-field check) already covers a blank Link
	}
	if fromEntity == toEntity {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "To Entity", Message: "an intercompany transaction's from_entity and to_entity must be different"}
	}
	if err := validateLegalEntityReferenceInSchema(schema, fromEntity); err != nil {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "From Entity", Message: err.Error()}
	}
	if err := validateLegalEntityReferenceInSchema(schema, toEntity); err != nil {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "To Entity", Message: err.Error()}
	}
	if fromAccount := pimString(payload["from_account_code"]); fromAccount != "" {
		if _, err := validateGLAccountCodeInSchema(schema, fromAccount); err != nil {
			return &ValidationError{Code: "GLOBAL-0002", SubFor: "From Entity Account", Message: err.Error()}
		}
	}
	if toAccount := pimString(payload["to_account_code"]); toAccount != "" {
		if _, err := validateGLAccountCodeInSchema(schema, toAccount); err != nil {
			return &ValidationError{Code: "GLOBAL-0002", SubFor: "To Entity Account", Message: err.Error()}
		}
	}
	if amount, ok := parityNumber(payload["amount"]); ok && amount <= 0 {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "Amount", Message: "an intercompany transaction's amount must be greater than zero"}
	}
	return nil
}

// CreateIntercompanyTransaction creates a Draft IntercompanyTransaction. Like
// JournalVoucher (engines/journal_voucher.go), it must be routed through the
// existing SubmitForApproval/DecideApproval before it posts - see
// postApprovedIntercompanyTransaction. A dedicated function/endpoint rather
// than the generic document-create path for the same reason JournalVoucher
// has one: real business validation (both entities distinct and Active, both
// account codes real) that a generic create's field-type checks alone
// cannot express, even though ValidateIntercompanyTransactionDocument above
// also runs it at the shared choke point as a second line of defence.
func CreateIntercompanyTransaction(tenantID, transactionDate, narration, fromEntity, fromAccountCode, toEntity, toAccountCode string, amount float64, userID string) (string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	if transactionDate == "" {
		return "", fmt.Errorf("transaction_date is required")
	}
	if narration == "" {
		return "", fmt.Errorf("narration is required")
	}
	if fromEntity == "" || toEntity == "" {
		return "", fmt.Errorf("from_entity and to_entity are both required")
	}
	if fromEntity == toEntity {
		return "", fmt.Errorf("from_entity and to_entity must be different")
	}
	if err := validateLegalEntityReferenceInSchema(schema, fromEntity); err != nil {
		return "", fmt.Errorf("from_entity: %v", err)
	}
	if err := validateLegalEntityReferenceInSchema(schema, toEntity); err != nil {
		return "", fmt.Errorf("to_entity: %v", err)
	}
	if _, err := validateGLAccountCodeInSchema(schema, fromAccountCode); err != nil {
		return "", fmt.Errorf("from_account_code: %v", err)
	}
	if _, err := validateGLAccountCodeInSchema(schema, toAccountCode); err != nil {
		return "", fmt.Errorf("to_account_code: %v", err)
	}
	if amount <= 0 {
		return "", fmt.Errorf("amount must be greater than zero")
	}

	id := NewDocID("ICT")
	docData := map[string]interface{}{
		"id": id, "code": id,
		"transaction_date": transactionDate, "narration": narration,
		"from_entity": fromEntity, "from_account_code": fromAccountCode,
		"to_entity": toEntity, "to_account_code": toAccountCode,
		"amount": amount, "status": "Draft",
	}
	marshaled, err := json.Marshal(docData)
	if err != nil {
		return "", err
	}
	if _, err := db.DB.Exec(fmt.Sprintf(
		`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'IntercompanyTransaction', $2, 'Draft', $3)`, schema),
		id, marshaled, userID); err != nil {
		return "", err
	}
	return id, nil
}

func fetchIntercompanyTransaction(schema, id string) (data map[string]interface{}, status string, err error) {
	var dataStr string
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT data, status FROM %s.documents WHERE doctype = 'IntercompanyTransaction' AND id = $1`, schema), id).
		Scan(&dataStr, &status); err != nil {
		return nil, "", fmt.Errorf("intercompany transaction not found: %v", err)
	}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return nil, "", fmt.Errorf("intercompany transaction %s has corrupt stored data: %v", id, err)
	}
	return data, status, nil
}

func updateIntercompanyTransactionData(schema, id, status string, data map[string]interface{}) error {
	data["status"] = status
	marshaled, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = $1, status = $2, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'IntercompanyTransaction' AND id = $3`, schema),
		marshaled, status, id)
	return err
}

// postApprovedIntercompanyTransaction posts an Approved IntercompanyTransaction
// as two mirrored, independently-balanced PostDoubleEntry calls:
//
//	FROM entity's book (tagged Entity=from_entity): Dr 1700 Due from
//	  Intercompany, Cr from_account_code - from_entity gave value and is
//	  now owed for it.
//	TO entity's book (tagged Entity=to_entity):     Dr to_account_code,
//	  Cr 2500 Due to Intercompany - to_entity received value and now owes
//	  for it.
//
// These MUST be two separate calls: PostingOptions.Entity is a whole-posting
// dimension (like CostCenter/Department), and the two legs need two
// different entities. That makes the pair non-atomic across one database
// transaction - PostDoubleEntry owns its own tx.Begin()/Commit() per call.
// Rather than invent compensating-reversal machinery, this follows
// JournalVoucher's own accepted shape for a failed post: log loudly, leave
// the document in a named, retryable, non-silent state, and let
// RetryPostApprovedIntercompanyTransaction re-attempt. Both legs' postingKey
// is scoped to this document+leg, so a retry after the FROM leg already
// succeeded is a no-op on that leg (idempotency check inside
// PostDoubleEntry) and only the TO leg is genuinely re-attempted -
// "Partially Posted" is a real, visible state, not a silent gap.
func postApprovedIntercompanyTransaction(tenantID, id string) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		LogSystemError(tenantID, "", "ERROR", "postApprovedIntercompanyTransaction", fmt.Sprintf("transaction %s: %v", id, err), "")
		return
	}
	data, status, err := fetchIntercompanyTransaction(schema, id)
	if err != nil {
		LogSystemError(tenantID, "", "ERROR", "postApprovedIntercompanyTransaction", err.Error(), "")
		return
	}
	if status != "Approved" && status != "Partially Posted" {
		return
	}

	transactionDate, _ := data["transaction_date"].(string)
	fromEntity := pimString(data["from_entity"])
	fromAccount := pimString(data["from_account_code"])
	toEntity := pimString(data["to_entity"])
	toAccount := pimString(data["to_account_code"])
	amountRupees, _ := parityNumber(data["amount"])
	amountPaise := RupeesToPaise(amountRupees)
	if amountPaise <= 0 {
		LogSystemError(tenantID, "", "ERROR", "postApprovedIntercompanyTransaction", fmt.Sprintf("transaction %s has a non-positive amount, not posted", id), "")
		return
	}

	fromErr := PostDoubleEntry(tenantID, "IntercompanyTransaction", id,
		map[string]int64{"1700": amountPaise},
		map[string]int64{fromAccount: amountPaise},
		transactionDate, fmt.Sprintf("IntercompanyTransaction:%s:FROM", id),
		PostingOptions{Entity: fromEntity})
	if fromErr != nil {
		LogSystemError(tenantID, "", "ERROR", "postApprovedIntercompanyTransaction",
			fmt.Sprintf("transaction %s: FROM leg (entity %s) failed: %v", id, fromEntity, fromErr), "")
		return
	}

	toErr := PostDoubleEntry(tenantID, "IntercompanyTransaction", id,
		map[string]int64{toAccount: amountPaise},
		map[string]int64{"2500": amountPaise},
		transactionDate, fmt.Sprintf("IntercompanyTransaction:%s:TO", id),
		PostingOptions{Entity: toEntity})
	if toErr != nil {
		LogSystemError(tenantID, "", "ERROR", "postApprovedIntercompanyTransaction",
			fmt.Sprintf("transaction %s: FROM leg posted but TO leg (entity %s) failed: %v - retry via RetryPostApprovedIntercompanyTransaction", id, toEntity, toErr), "")
		if err := updateIntercompanyTransactionData(schema, id, "Partially Posted", data); err != nil {
			LogSystemError(tenantID, "", "ERROR", "postApprovedIntercompanyTransaction", fmt.Sprintf("transaction %s: failed to record Partially Posted: %v", id, err), "")
		}
		return
	}

	if err := updateIntercompanyTransactionData(schema, id, "Posted", data); err != nil {
		LogSystemError(tenantID, "", "ERROR", "postApprovedIntercompanyTransaction", fmt.Sprintf("transaction %s posted to GL but status update failed: %v", id, err), "")
	}
}

// RetryPostApprovedIntercompanyTransaction re-attempts posting an Approved or
// Partially Posted transaction, the RetryPostApprovedJournalVoucher precedent.
func RetryPostApprovedIntercompanyTransaction(tenantID, id string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	_, status, err := fetchIntercompanyTransaction(schema, id)
	if err != nil {
		return err
	}
	if status != "Approved" && status != "Partially Posted" {
		return fmt.Errorf("only an Approved or Partially Posted intercompany transaction can be retried (current status: %s)", status)
	}
	postApprovedIntercompanyTransaction(tenantID, id)
	_, newStatus, err := fetchIntercompanyTransaction(schema, id)
	if err != nil {
		return err
	}
	if newStatus != "Posted" {
		return fmt.Errorf("posting is still incomplete (status: %s)", newStatus)
	}
	return nil
}

// GetEntityTrialBalance groups gl_postings by entity (Stage 37.2.1's report
// counterpart to GetCostCenterPL) between [startDate, endDate] - untagged
// rows land under "Unassigned" so nothing silently vanishes for a tenant
// that has never used the entity dimension.
func GetEntityTrialBalance(tenantID, startDate, endDate string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	if startDate == "" || endDate == "" {
		return nil, fmt.Errorf("start and end are required")
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT COALESCE(p.entity, 'Unassigned') AS entity, p.account_code, a.account_name, a.account_type,
		       COALESCE(SUM(p.debit), 0) AS total_debit, COALESCE(SUM(p.credit), 0) AS total_credit
		FROM %s.gl_postings p JOIN %s.gl_accounts a ON a.account_code = p.account_code
		WHERE p.created_at >= $1::date AND p.created_at < ($2::date + 1)
		GROUP BY COALESCE(p.entity, 'Unassigned'), p.account_code, a.account_name, a.account_type
		ORDER BY entity, p.account_code`, schema, schema), startDate, endDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []map[string]interface{}
	for rows.Next() {
		var entity, code, name, accType string
		var debitPaise, creditPaise int64
		if err := rows.Scan(&entity, &code, &name, &accType, &debitPaise, &creditPaise); err != nil {
			return nil, err
		}
		out = append(out, map[string]interface{}{
			"entity": entity, "account_code": code, "account_name": name, "account_type": accType,
			"total_debit": PaiseToRupees(debitPaise), "total_credit": PaiseToRupees(creditPaise),
		})
	}
	if out == nil {
		out = []map[string]interface{}{}
	}
	return out, nil
}

// intercompanyPairTotal is one (from_entity, to_entity) pair's claimed
// amount, from the IntercompanyTransaction ledger itself.
type intercompanyPairTotal struct {
	FromEntity, ToEntity string
	Amount               float64
}

// postedIntercompanyPairTotals sums Posted IntercompanyTransaction.amount by
// (from_entity, to_entity), up to and including asOfDate - reading the
// documents directly rather than trying to re-derive "who owes whom" purely
// from the 1700/2500 control account balances, which carry no counterparty
// dimension of their own (see this file's header comment: one dimension,
// entity, is enough for per-book trial balances; the IntercompanyTransaction
// documents are the one and only source of truth for pairwise ownership).
func postedIntercompanyPairTotals(schema, asOfDate string) ([]intercompanyPairTotal, error) {
	rows, err := db.DB.Query(fmt.Sprintf(
		`SELECT data FROM %s.documents WHERE doctype = 'IntercompanyTransaction' AND status = 'Posted' AND deleted_at IS NULL
		   AND (data->>'transaction_date') <= $1
		 ORDER BY (data->>'transaction_date')`, schema), asOfDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	totals := map[[2]string]float64{}
	for rows.Next() {
		var dataStr string
		if err := rows.Scan(&dataStr); err != nil {
			return nil, err
		}
		var data map[string]interface{}
		if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
			continue
		}
		key := [2]string{pimString(data["from_entity"]), pimString(data["to_entity"])}
		amount, _ := parityNumber(data["amount"])
		totals[key] += amount
	}
	out := make([]intercompanyPairTotal, 0, len(totals))
	for key, amount := range totals {
		out = append(out, intercompanyPairTotal{FromEntity: key[0], ToEntity: key[1], Amount: amount})
	}
	return out, nil
}

// entityControlAccountBalance sums one entity's own postings to a control
// account up to and including asOfDate - the GL's own claim, to compare
// against the IC ledger's claim above. normalSide is "debit" for an Asset
// control account (1700) and "credit" for a Liability one (2500), so both
// come back as a positive "how much is owed" figure rather than one of them
// reading negative purely because of which side of the balance sheet its
// account sits on.
func entityControlAccountBalance(schema, entity, accountCode, asOfDate, normalSide string) (float64, error) {
	var debitPaise, creditPaise int64
	err := db.DB.QueryRow(fmt.Sprintf(`
		SELECT COALESCE(SUM(debit), 0), COALESCE(SUM(credit), 0) FROM %s.gl_postings
		WHERE entity = $1 AND account_code = $2 AND created_at < ($3::date + 1)`, schema),
		entity, accountCode, asOfDate).Scan(&debitPaise, &creditPaise)
	if err != nil {
		return 0, err
	}
	if normalSide == "credit" {
		return PaiseToRupees(creditPaise - debitPaise), nil
	}
	return PaiseToRupees(debitPaise - creditPaise), nil
}

// GetIntercompanyReconciliation (Stage 37.2.3) compares, for every entity
// pair with any posted intercompany activity, what the IntercompanyTransaction
// ledger claims is owed against what each side's own 1700/2500 control
// account actually shows. They can only diverge if something posted to 1700
// or 2500 outside this mechanism (a manual JournalVoucher hitting a control
// account directly) or if a pair is stuck Partially Posted - exactly the
// drift this report exists to surface, the same "compare two independently-
// derived numbers, flag variance" shape Stage 35.8's settlement
// reconciliation already established.
func GetIntercompanyReconciliation(tenantID, asOfDate string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	if asOfDate == "" {
		return nil, &ValidationError{Code: "GLOBAL-0001", SubFor: "As Of Date", Message: "as_of date is required"}
	}
	pairs, err := postedIntercompanyPairTotals(schema, asOfDate)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]interface{}, 0, len(pairs))
	for _, pair := range pairs {
		fromBalance, err := entityControlAccountBalance(schema, pair.FromEntity, "1700", asOfDate, "debit")
		if err != nil {
			return nil, err
		}
		toBalance, err := entityControlAccountBalance(schema, pair.ToEntity, "2500", asOfDate, "credit")
		if err != nil {
			return nil, err
		}
		out = append(out, map[string]interface{}{
			"from_entity": pair.FromEntity, "to_entity": pair.ToEntity,
			"ic_ledger_amount":       pair.Amount,
			"from_entity_1700_total": fromBalance,
			"to_entity_2500_total":   toBalance,
			"variance":               fromBalance - toBalance,
			"in_balance":             fromBalance == toBalance,
		})
	}
	return out, nil
}

// IntercompanyElimination is one consolidation-worksheet adjustment line.
type IntercompanyElimination struct {
	FromEntity, ToEntity string  `json:"-"`
	AccountCode          string  `json:"account_code"`
	AccountName          string  `json:"account_name"`
	Debit                float64 `json:"debit"`
	Credit               float64 `json:"credit"`
	Reason               string  `json:"reason"`
}

// ComputeIntercompanyEliminations (Stage 37.2.4) is deliberately read-only:
// it never posts anything to any entity's own book (an elimination is a
// consolidation-worksheet construct, not a transaction any single legal
// entity actually entered into - inventing a fictitious "Consolidation
// entity" to hold these as real postings would be new scope well beyond
// this stage, the same class of call 37.1.5's presentation trial balance
// made about not modelling full IAS 21 translation). It always eliminates
// the matched 1700/2500 balance per pair (the intercompany receivable/
// payable is internal to the group and must not appear on a consolidated
// balance sheet). It ALSO eliminates the revenue/expense pair when
// from_account_code/to_account_code are actually typed Revenue and Expense
// (the common "internal billing" shape) - stated, not silently assumed:
// any other account-type combination (e.g. an inventory transfer posted to
// two Asset accounts) is intentionally left as an honest gap here, the same
// "single-rate convenience" posture 37.1.5 already documents, because a
// correct general elimination of arbitrary intercompany asset transfers
// needs unrealised-profit-in-inventory tracking this codebase does not have.
func ComputeIntercompanyEliminations(tenantID, asOfDate string) ([]IntercompanyElimination, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	if asOfDate == "" {
		return nil, &ValidationError{Code: "GLOBAL-0001", SubFor: "As Of Date", Message: "as_of date is required"}
	}
	pairs, err := postedIntercompanyPairTotals(schema, asOfDate)
	if err != nil {
		return nil, err
	}

	var out []IntercompanyElimination
	var totalDueFrom, totalDueTo float64
	for _, pair := range pairs {
		totalDueFrom += pair.Amount
		totalDueTo += pair.Amount
	}
	if totalDueFrom > 0 {
		out = append(out,
			IntercompanyElimination{AccountCode: "2500", AccountName: "Due to Intercompany", Debit: totalDueTo, Reason: "Eliminate intercompany payable/receivable at consolidation"},
			IntercompanyElimination{AccountCode: "1700", AccountName: "Due from Intercompany", Credit: totalDueFrom, Reason: "Eliminate intercompany payable/receivable at consolidation"},
		)
	}

	// P&L-side elimination, only for pairs whose chosen accounts are the
	// Revenue/Expense shape - grouped and summed per distinct account pair
	// so two different account-code choices don't collapse into one line.
	rows, err := db.DB.Query(fmt.Sprintf(
		`SELECT data->>'from_account_code', data->>'to_account_code', data->>'amount' FROM %s.documents
		 WHERE doctype = 'IntercompanyTransaction' AND status = 'Posted' AND deleted_at IS NULL
		   AND (data->>'transaction_date') <= $1`, schema), asOfDate)
	if err != nil {
		return nil, err
	}
	type accountPairKey struct{ from, to string }
	plTotals := map[accountPairKey]float64{}
	for rows.Next() {
		var fromAccount, toAccount, amountStr string
		if err := rows.Scan(&fromAccount, &toAccount, &amountStr); err != nil {
			rows.Close()
			return nil, err
		}
		var amount float64
		fmt.Sscanf(amountStr, "%f", &amount)
		plTotals[accountPairKey{fromAccount, toAccount}] += amount
	}
	rows.Close()

	for key, amount := range plTotals {
		fromType, err := validateGLAccountCodeInSchema(schema, key.from)
		if err != nil {
			continue
		}
		toType, err := validateGLAccountCodeInSchema(schema, key.to)
		if err != nil {
			continue
		}
		if fromType != "Revenue" || toType != "Expense" {
			continue
		}
		var fromName, toName string
		db.DB.QueryRow(fmt.Sprintf(`SELECT account_name FROM %s.gl_accounts WHERE account_code = $1`, schema), key.from).Scan(&fromName)
		db.DB.QueryRow(fmt.Sprintf(`SELECT account_name FROM %s.gl_accounts WHERE account_code = $1`, schema), key.to).Scan(&toName)
		out = append(out,
			IntercompanyElimination{AccountCode: key.from, AccountName: fromName, Debit: amount, Reason: "Eliminate intercompany revenue against the counterparty's expense"},
			IntercompanyElimination{AccountCode: key.to, AccountName: toName, Credit: amount, Reason: "Eliminate intercompany revenue against the counterparty's expense"},
		)
	}
	return out, nil
}

// GetConsolidatedTrialBalance (Stage 37.2.5) is the tenant-wide trial balance
// (identical figures GetTrialBalance already reports - one schema is one
// group here, see this file's header) shown alongside 37.2.4's eliminations
// and the resulting consolidated figure per account, so a user sees exactly
// what got removed and why rather than a number they have to trust blind.
func GetConsolidatedTrialBalance(tenantID, asOfDate string) (map[string]interface{}, error) {
	base, err := GetTrialBalance(tenantID, asOfDate)
	if err != nil {
		return nil, err
	}
	eliminations, err := ComputeIntercompanyEliminations(tenantID, asOfDate)
	if err != nil {
		return nil, err
	}
	// An elimination is itself a balanced Dr/Cr entry - its columns add
	// directly onto the pre-elimination columns for the account it names,
	// exactly as posting it to a consolidation worksheet would. Tracked as
	// two separate totals (not a signed net) so a debit-side and a
	// credit-side elimination on the SAME account in the same run - not
	// possible today given 37.2.4's own two elimination shapes, but not
	// something this function should assume - could never cancel out
	// silently.
	eliminationDebitByAccount := map[string]float64{}
	eliminationCreditByAccount := map[string]float64{}
	for _, e := range eliminations {
		eliminationDebitByAccount[e.AccountCode] += e.Debit
		eliminationCreditByAccount[e.AccountCode] += e.Credit
	}

	accounts, _ := base["balances"].([]AccountBalance)
	consolidated := make([]map[string]interface{}, 0, len(accounts))
	for _, acc := range accounts {
		consolidated = append(consolidated, map[string]interface{}{
			"account_code": acc.Code, "account_name": acc.Name, "account_type": acc.Type,
			"pre_elimination_debit":  acc.Debit,
			"pre_elimination_credit": acc.Credit,
			"consolidated_debit":     acc.Debit + eliminationDebitByAccount[acc.Code],
			"consolidated_credit":    acc.Credit + eliminationCreditByAccount[acc.Code],
		})
	}
	return map[string]interface{}{
		"as_of_date":   asOfDate,
		"accounts":     consolidated,
		"eliminations": eliminations,
	}, nil
}

func init() {
	RegisterReport(ReportDefinition{
		ID: "entity-trial-balance", Label: "Entity Trial Balance", Category: "Finance",
		Columns: []ReportColumn{
			{Key: "entity", Label: "Entity"}, {Key: "account_code", Label: "Account"}, {Key: "account_name", Label: "Account Name"},
			{Key: "account_type", Label: "Type"}, {Key: "total_debit", Label: "Debit", Sensitive: true}, {Key: "total_credit", Label: "Credit", Sensitive: true},
		},
		Params: []ReportParam{
			{Key: "start", Label: "From", Type: "date", Required: true},
			{Key: "end", Label: "To", Type: "date", Required: true},
		},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			return GetEntityTrialBalance(tenantID, params["start"], params["end"])
		},
	})

	RegisterReport(ReportDefinition{
		ID: "intercompany-reconciliation", Label: "Intercompany Reconciliation", Category: "Finance",
		Columns: []ReportColumn{
			{Key: "from_entity", Label: "From Entity"}, {Key: "to_entity", Label: "To Entity"},
			{Key: "ic_ledger_amount", Label: "IC Ledger Amount", Sensitive: true},
			{Key: "from_entity_1700_total", Label: "From Entity 1700 Balance", Sensitive: true},
			{Key: "to_entity_2500_total", Label: "To Entity 2500 Balance", Sensitive: true},
			{Key: "variance", Label: "Variance", Sensitive: true}, {Key: "in_balance", Label: "In Balance"},
		},
		Params: []ReportParam{
			{Key: "as_of", Label: "As Of", Type: "date", Required: true},
		},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			return GetIntercompanyReconciliation(tenantID, params["as_of"])
		},
	})

	RegisterReport(ReportDefinition{
		ID: "consolidated-trial-balance", Label: "Consolidated Trial Balance", Category: "Finance",
		Columns: []ReportColumn{
			{Key: "account_code", Label: "Account"}, {Key: "account_name", Label: "Account Name"}, {Key: "account_type", Label: "Type"},
			{Key: "pre_elimination_debit", Label: "Debit (pre-elimination)", Sensitive: true},
			{Key: "pre_elimination_credit", Label: "Credit (pre-elimination)", Sensitive: true},
			{Key: "consolidated_debit", Label: "Debit (consolidated)", Sensitive: true},
			{Key: "consolidated_credit", Label: "Credit (consolidated)", Sensitive: true},
		},
		Params: []ReportParam{
			{Key: "as_of", Label: "As Of", Type: "date", Required: true},
		},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			result, err := GetConsolidatedTrialBalance(tenantID, params["as_of"])
			if err != nil {
				return nil, err
			}
			rows, _ := result["accounts"].([]map[string]interface{})
			return rows, nil
		},
	})
}
