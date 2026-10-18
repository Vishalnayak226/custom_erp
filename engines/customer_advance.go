package engines

import (
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"custom_erp/db"
)

// Stage 85.D3 (74.2.1): Customer Advance - money a customer pays now for a
// purchase later (a custom order, a hold, an item not yet in stock).
//
// Built on stored value (stored_value.go): a receipt voucher (CA, at most 16
// characters, drawn in the same transaction) funds one instrument of the
// "Customer Advance" profile for one customer - Dr the tender's clearing
// account, Cr 2640 Customer Advance Liability. At the till it pays part or all
// of a later bill of the SAME customer through the gift-card tender path, so
// it is taken off inside the sale's transaction under a row lock. A refund
// needs a reason, goes back to the tender the advance was paid with, gets its
// own voucher (AVR), and is an appended ledger event like every other change.
//
// GST (DEC-03, the BRD default): an advance for goods carries none at receipt
// (Notification 66/2017-CT); an advance for services is taxed when received -
// the tax is split out of the amount (Cr output CGST/SGST, or IGST) and given
// back when the advance is used on the bill (which charges the full tax) or
// refunded, so the tax is paid once.

const (
	AdvanceProfileID      = "Customer Advance"
	AccountCustomerAdvLia = "2640"
	SVEventRefund         = "REFUND"
)

// TakeAdvanceRequest is one advance taken at the till.
type TakeAdvanceRequest struct {
	Location       string  `json:"location"`
	CustomerID     string  `json:"customer_id"`
	Amount         float64 `json:"amount"`
	PaymentMode    string  `json:"payment_mode"`
	Purpose        string  `json:"purpose"`
	ExpectedDate   string  `json:"expected_date"`
	SupplyType     string  `json:"supply_type"`
	GSTRate        float64 `json:"gst_rate"`
	IdempotencyKey string  `json:"idempotency_key"`
}

// CustomerAdvance is an advance as the till and the register show it.
type CustomerAdvance struct {
	Number       string  `json:"number"`
	CustomerID   string  `json:"customer_id"`
	CustomerName string  `json:"customer_name"`
	Location     string  `json:"location"`
	Amount       float64 `json:"amount"`
	Balance      float64 `json:"balance"`
	Applied      float64 `json:"applied"`
	Refunded     float64 `json:"refunded"`
	PaymentMode  string  `json:"payment_mode"`
	Purpose      string  `json:"purpose"`
	ExpectedDate string  `json:"expected_date"`
	SupplyType   string  `json:"supply_type"`
	GSTRate      float64 `json:"gst_rate"`
	Tax          float64 `json:"tax"`
	Status       string  `json:"status"`
	TakenAt      string  `json:"taken_at"`
	TakenBy      string  `json:"taken_by"`
}

// advanceStatus reads a stored-value status the way an advance is talked about.
func advanceStatus(svStatus string, refunded int64) string {
	switch svStatus {
	case SVStatusActive:
		return "Open"
	case SVStatusPartial:
		return "Partially used"
	case SVStatusFull:
		if refunded > 0 {
			return "Refunded"
		}
		return "Fully used"
	}
	return svStatus
}

func ensureAdvanceSetupTx(tx *sql.Tx, schema string) error {
	if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.gl_accounts (account_code, account_name, account_type) VALUES ('2640', 'Customer Advance Liability', 'Liability') ON CONFLICT (account_code) DO NOTHING`, schema)); err != nil {
		return err
	}
	_, err := tx.Exec(fmt.Sprintf(`
		INSERT INTO %s.documents (id, doctype, data, status, created_by)
		SELECT 'Customer Advance', 'StoredValueProfile', jsonb_build_object(
		         'code', 'Customer Advance', 'instrument_type', 'Customer advance', 'number_length', 16, 'number_format', 'Alphanumeric',
		         'denomination_mode', 'Variable', 'min_amount', 1, 'max_amount', 0, 'validity_months', 0,
		         'reload_allowed', 'No', 'partial_redemption', 'Yes', 'liability_account', '2640', 'status', 'Active'),
		       'Active', 'system'
		WHERE NOT EXISTS (SELECT 1 FROM %s.documents WHERE id = 'Customer Advance')`, schema, schema))
	return err
}

// gstSplit is the output-tax legs for tax paise, intra- or inter-state.
func gstSplit(tax int64, interstate bool) map[string]int64 {
	if tax <= 0 {
		return map[string]int64{}
	}
	if interstate {
		return map[string]int64{"2202": tax}
	}
	c := tax / 2
	return map[string]int64{"2200": c, "2201": tax - c}
}

func addLegs(into map[string]int64, legs map[string]int64) map[string]int64 {
	for k, v := range legs {
		into[k] += v
	}
	return into
}

// TakeCustomerAdvance records an advance: voucher, funding and posting in one
// transaction. A repeat with the same idempotency key returns the first.
func TakeCustomerAdvance(tenantID, user, session string, req TakeAdvanceRequest) (*CustomerAdvance, error) {
	req.Location, req.CustomerID = strings.TrimSpace(req.Location), strings.TrimSpace(req.CustomerID)
	req.PaymentMode = strings.TrimSpace(req.PaymentMode)
	if req.PaymentMode == "" {
		req.PaymentMode = "Cash"
	}
	if !isPOSMoneyMode(req.PaymentMode) {
		return nil, &ValidationError{Code: "POSOFF-0871", Message: fmt.Sprintf("%q is not a payment mode the till takes (Cash, Card or UPI).", req.PaymentMode)}
	}
	amount := RupeesToPaise(req.Amount)
	if amount <= 0 {
		return nil, &ValidationError{Code: "POSOFF-0871", Message: "Enter the amount the customer is paying in advance."}
	}
	c, found, err := LoadCustomerRef(tenantID, req.CustomerID)
	if err != nil {
		return nil, err
	}
	if !found || c.WalkIn {
		return nil, &ValidationError{Code: "POSOFF-0871", Message: "An advance belongs to a customer - take their mobile number first (not Walk-in)."}
	}
	supply := "Goods"
	if strings.EqualFold(strings.TrimSpace(req.SupplyType), "Services") {
		supply = "Services"
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	key := strings.TrimSpace(req.IdempotencyKey)
	if key != "" {
		var number string
		if db.DB.QueryRow(fmt.Sprintf(`SELECT instrument_id FROM %s.stored_value_ledger WHERE idempotency_key = $1`, schema), "advance|"+key).Scan(&number) == nil {
			return LoadCustomerAdvance(tenantID, number)
		}
	}
	tax := int64(0)
	rate := 0.0
	interstate := false
	if supply == "Services" {
		rate = req.GSTRate
		if rate <= 0 {
			rate = 18
		}
		tax = int64(math.Round(float64(amount) * rate / (100 + rate)))
		store := LoadPrintStore(tenantID, req.Location)
		interstate = c.State != "" && store.State != "" && !strings.EqualFold(c.State, store.State)
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := ensureAdvanceSetupTx(tx, schema); err != nil {
		return nil, err
	}
	number, err := issueCompactNumberTx(tx, schema, "CA", "CA", req.Location, time.Now())
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.stored_value_instruments (instrument_id, card_number, profile, status, customer_id, created_by)
		VALUES ($1, $1, $2, 'Inactive', $3, $4)`, schema), number, AdvanceProfileID, c.ID, user); err != nil {
		return nil, err
	}
	card := svCard{ID: number, Number: number, Profile: AdvanceProfileID, Status: SVStatusInactive}
	idem := ""
	if key != "" {
		idem = "advance|" + key
	}
	src := svSource{Doctype: "CustomerAdvance", ID: number, Location: req.Location, PaymentMode: req.PaymentMode, User: user, Reason: strings.TrimSpace(req.Purpose)}
	if _, err := appendSVEvent(tx, schema, &card, SVEventActivate, amount, "", src, idem); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.stored_value_instruments SET activated_at = CURRENT_TIMESTAMP, issued_location = $2, issue_payment_mode = $3 WHERE instrument_id = $1`, schema),
		number, req.Location, req.PaymentMode); err != nil {
		return nil, err
	}
	var expected interface{}
	if d := strings.TrimSpace(req.ExpectedDate); d != "" {
		expected = d
	}
	if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.customer_advances (instrument_id, customer_id, location, amount_paise, payment_mode, purpose, expected_date, supply_type, gst_rate, tax_paise, interstate, pos_session, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, NULLIF($12, ''), $13)`, schema),
		number, c.ID, req.Location, amount, req.PaymentMode, strings.TrimSpace(req.Purpose), expected, supply, rate, tax, interstate, session, user); err != nil {
		return nil, err
	}
	credits := addLegs(map[string]int64{AccountCustomerAdvLia: amount - tax}, gstSplit(tax, interstate))
	if err := PostDoubleEntryTx(tx, tenantID, schema, "CustomerAdvance", number,
		map[string]int64{paymentModeClearingAccount(req.PaymentMode): amount}, credits, "", "CustomerAdvance:"+number+":RECEIPT"); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	LogAuditEvent(tenantID, user, "POS", "CUSTOMER_ADVANCE_TAKEN", fmt.Sprintf("%s: %.2f from %s by %s (%s)", number, PaiseToRupees(amount), c.ID, req.PaymentMode, supply))
	return LoadCustomerAdvance(tenantID, number)
}

const advanceSelect = `SELECT a.instrument_id, a.customer_id, COALESCE(c.data->>'name', a.customer_id), a.location, a.amount_paise, i.balance_paise,
	COALESCE((SELECT SUM(r.amount_paise) FROM %[1]s.customer_advance_refunds r WHERE r.instrument_id = a.instrument_id), 0),
	a.payment_mode, a.purpose, COALESCE(to_char(a.expected_date, 'YYYY-MM-DD'), ''), a.supply_type, a.gst_rate, a.tax_paise,
	i.status, to_char(a.created_at, 'YYYY-MM-DD HH24:MI'), COALESCE(a.created_by, '')
	FROM %[1]s.customer_advances a
	JOIN %[1]s.stored_value_instruments i ON i.instrument_id = a.instrument_id
	LEFT JOIN %[1]s.documents c ON c.doctype = 'Customer' AND c.id = a.customer_id`

func scanAdvance(scan func(...interface{}) error) (CustomerAdvance, error) {
	var a CustomerAdvance
	var amount, balance, refunded, tax int64
	var status string
	err := scan(&a.Number, &a.CustomerID, &a.CustomerName, &a.Location, &amount, &balance, &refunded, &a.PaymentMode, &a.Purpose,
		&a.ExpectedDate, &a.SupplyType, &a.GSTRate, &tax, &status, &a.TakenAt, &a.TakenBy)
	if err != nil {
		return a, err
	}
	a.Amount, a.Balance, a.Refunded, a.Tax = PaiseToRupees(amount), PaiseToRupees(balance), PaiseToRupees(refunded), PaiseToRupees(tax)
	a.Applied = PaiseToRupees(amount - balance - refunded)
	a.Status = advanceStatus(status, refunded)
	return a, nil
}

// LoadCustomerAdvance reads one advance by its voucher number.
func LoadCustomerAdvance(tenantID, number string) (*CustomerAdvance, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	a, err := scanAdvance(db.DB.QueryRow(fmt.Sprintf(advanceSelect+` WHERE a.instrument_id = $1`, schema), strings.TrimSpace(number)).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, &ValidationError{Code: "POSOFF-0872", Message: fmt.Sprintf("%s is not a customer advance.", number)}
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// ListCustomerAdvances is the advances of one customer (open only when
// openOnly), or of one store, newest first.
func ListCustomerAdvances(tenantID, customerID, store string, openOnly bool) ([]CustomerAdvance, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	q := fmt.Sprintf(advanceSelect+` WHERE TRUE`, schema)
	args := []interface{}{}
	if strings.TrimSpace(customerID) != "" {
		args = append(args, strings.TrimSpace(customerID))
		q += fmt.Sprintf(" AND a.customer_id = $%d", len(args))
	}
	if strings.TrimSpace(store) != "" {
		args = append(args, strings.TrimSpace(store))
		q += fmt.Sprintf(" AND a.location = $%d", len(args))
	}
	if openOnly {
		q += " AND i.balance_paise > 0"
	}
	q += " ORDER BY a.created_at DESC LIMIT 200"
	rows, err := db.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CustomerAdvance{}
	for rows.Next() {
		a, err := scanAdvance(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// AdvanceTenderRequest is an advance paying part of a bill.
type AdvanceTenderRequest struct {
	Number string  `json:"number"`
	Amount float64 `json:"amount"`
}

// PrepareAdvanceTenders checks the advances paying a bill - each the bill's
// own customer's, open, enough left - and returns them as stored-value
// tenders for the sale to redeem (DEC-04: same customer only).
func PrepareAdvanceTenders(tenantID, customerID string, reqs []AdvanceTenderRequest, duePaise int64) ([]GiftCardTender, error) {
	if len(reqs) == 0 {
		return nil, nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	p, err := LoadStoredValueProfile(tenantID, AdvanceProfileID)
	if err != nil {
		return nil, err
	}
	today := time.Now().Format("2006-01-02")
	seen := map[string]bool{}
	var total int64
	out := []GiftCardTender{}
	for _, r := range reqs {
		number := strings.TrimSpace(r.Number)
		if seen[number] {
			return nil, &ValidationError{Code: "POSOFF-0872", Message: fmt.Sprintf("Advance %s is on the bill twice.", number)}
		}
		seen[number] = true
		var owner string
		err := db.DB.QueryRow(fmt.Sprintf(`SELECT customer_id FROM %s.customer_advances WHERE instrument_id = $1`, schema), number).Scan(&owner)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, &ValidationError{Code: "POSOFF-0872", Message: fmt.Sprintf("%s is not a customer advance.", number)}
		}
		if err != nil {
			return nil, err
		}
		if owner != strings.TrimSpace(customerID) {
			return nil, &ValidationError{Code: "POSOFF-0873", Message: fmt.Sprintf("Advance %s was paid by another customer.", number)}
		}
		c, err := scanSVCard(db.DB.QueryRow(fmt.Sprintf(`SELECT %s FROM %s.stored_value_instruments WHERE instrument_id = $1`, svCardColumns, schema), number))
		if err != nil {
			return nil, err
		}
		amount := RupeesToPaise(r.Amount)
		if err := checkRedeemable(c, p, amount, today); err != nil {
			return nil, err
		}
		total += amount
		out = append(out, GiftCardTender{InstrumentID: number, Masked: "Advance " + number, AmountPaise: amount})
	}
	if total > duePaise {
		return nil, &ValidationError{Code: "POSOFF-0872", Message: fmt.Sprintf("Advances cover %.2f but the bill is %.2f - use less of the advance.", PaiseToRupees(total), PaiseToRupees(duePaise))}
	}
	return out, nil
}

// advanceTaxShare is the GST taken at receipt on part of an advance.
func advanceTaxShareTx(tx *sql.Tx, schema, number string, partPaise int64) (int64, bool, error) {
	var amount, tax int64
	var interstate bool
	err := tx.QueryRow(fmt.Sprintf(`SELECT amount_paise, tax_paise, interstate FROM %s.customer_advances WHERE instrument_id = $1`, schema), number).Scan(&amount, &tax, &interstate)
	if errors.Is(err, sql.ErrNoRows) || amount == 0 || tax == 0 {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return int64(math.Round(float64(tax) * float64(partPaise) / float64(amount))), interstate, nil
}

// postAdvanceTaxOnUseTx gives back the GST paid at receipt on the part of a
// services advance a bill uses - the bill charges the full tax itself. A
// goods advance (no tax at receipt) posts nothing.
func postAdvanceTaxOnUseTx(tx *sql.Tx, tenantID, schema, cartNumber, number string, usedPaise int64) error {
	share, interstate, err := advanceTaxShareTx(tx, schema, number, usedPaise)
	if err != nil || share == 0 {
		return err
	}
	return PostDoubleEntryTx(tx, tenantID, schema, "POSCart", cartNumber, gstSplit(share, interstate),
		map[string]int64{AccountCustomerAdvLia: share}, "", fmt.Sprintf("POSCart:%s:ADVANCETAX:%s", cartNumber, number))
}

// RefundCustomerAdvance pays back what is left (or part of it) to the tender
// the advance was paid with, with a reason, under its own voucher.
func RefundCustomerAdvance(tenantID, user, session, number string, amountRupees float64, reason, location, idemKey string) (*CustomerAdvance, string, error) {
	number, reason = strings.TrimSpace(number), strings.TrimSpace(reason)
	if reason == "" {
		return nil, "", &ValidationError{Code: "POSOFF-0874", Message: "Say why the advance is being refunded."}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, "", err
	}
	if k := strings.TrimSpace(idemKey); k != "" {
		var voucher string
		if db.DB.QueryRow(fmt.Sprintf(`SELECT source_id FROM %s.stored_value_ledger WHERE idempotency_key = $1`, schema), "advance-refund|"+k).Scan(&voucher) == nil {
			a, err := LoadCustomerAdvance(tenantID, number)
			return a, voucher, err
		}
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, "", err
	}
	defer tx.Rollback()
	c, err := scanSVCard(tx.QueryRow(fmt.Sprintf(`SELECT %s FROM %s.stored_value_instruments WHERE instrument_id = $1 AND profile = $2 FOR UPDATE`, svCardColumns, schema), number, AdvanceProfileID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", &ValidationError{Code: "POSOFF-0872", Message: fmt.Sprintf("%s is not a customer advance.", number)}
	}
	if err != nil {
		return nil, "", err
	}
	amount := RupeesToPaise(amountRupees)
	if amount <= 0 {
		amount = c.Balance
	}
	if amount <= 0 || amount > c.Balance {
		return nil, "", &ValidationError{Code: "POSOFF-0874", Message: fmt.Sprintf("Advance %s has %.2f left to refund.", number, PaiseToRupees(c.Balance))}
	}
	store := strings.TrimSpace(location)
	if store == "" {
		_ = tx.QueryRow(fmt.Sprintf(`SELECT location FROM %s.customer_advances WHERE instrument_id = $1`, schema), number).Scan(&store)
	}
	voucher, err := issueCompactNumberTx(tx, schema, "AVR", "AVR", store, time.Now())
	if err != nil {
		return nil, "", err
	}
	mode := c.IssuePaymentMode
	if mode == "" {
		mode = "Cash"
	}
	idem := ""
	if k := strings.TrimSpace(idemKey); k != "" {
		idem = "advance-refund|" + k
	}
	src := svSource{Doctype: "CustomerAdvanceRefund", ID: voucher, Location: store, PaymentMode: mode, User: user, Reason: reason}
	if _, err := appendSVEvent(tx, schema, &c, SVEventRefund, -amount, "", src, idem); err != nil {
		return nil, "", err
	}
	if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.customer_advance_refunds (voucher_number, instrument_id, amount_paise, payment_mode, reason, location, pos_session, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, ''), $8)`, schema), voucher, number, amount, mode, reason, store, session, user); err != nil {
		return nil, "", err
	}
	share, interstate, err := advanceTaxShareTx(tx, schema, number, amount)
	if err != nil {
		return nil, "", err
	}
	debits := addLegs(map[string]int64{AccountCustomerAdvLia: amount - share}, gstSplit(share, interstate))
	if err := PostDoubleEntryTx(tx, tenantID, schema, "CustomerAdvanceRefund", voucher, debits,
		map[string]int64{paymentModeClearingAccount(mode): amount}, "", "CustomerAdvanceRefund:"+voucher); err != nil {
		return nil, "", err
	}
	if err := tx.Commit(); err != nil {
		return nil, "", err
	}
	LogAuditEvent(tenantID, user, "POS", "CUSTOMER_ADVANCE_REFUNDED", fmt.Sprintf("%s: %.2f of %s back by %s - %s", voucher, PaiseToRupees(amount), number, mode, reason))
	a, err := LoadCustomerAdvance(tenantID, number)
	return a, voucher, err
}

// sessionAdvanceCash is the cash a till session took in as advances and paid
// back as advance refunds.
func sessionAdvanceCash(schema, sessionID string) (in, out float64) {
	var a, b int64
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(SUM(amount_paise), 0) FROM %s.customer_advances WHERE pos_session = $1 AND payment_mode = 'Cash'`, schema), sessionID).Scan(&a)
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(SUM(amount_paise), 0) FROM %s.customer_advance_refunds WHERE pos_session = $1 AND payment_mode = 'Cash'`, schema), sessionID).Scan(&b)
	return PaiseToRupees(a), PaiseToRupees(b)
}
