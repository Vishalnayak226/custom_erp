package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Customer receipts (140.12.5 / 140.12.6). A receipt was one route per
// invoice that always debited cash; an advance needed an open till. A Receipt
// is now one voucher for money a customer paid at the back office:
//
//   - Dr the account the money went to - the chosen bank account's GL, or
//     cash / card / UPI clearing - and Dr 1310 TDS Receivable for tax the
//     customer deducted;
//   - Cr 1300 for each invoice it settles (one receipt across several), each
//     invoice's payment history and balance moved exactly as a receipt from
//     the invoice list moves them;
//   - what is not allocated becomes a customer advance (Cr 2640) the
//     customer's later invoices or bills can use - an on-account receipt;
//   - the customer's open advances can pay invoices on the same voucher
//     (Dr 2640 / Cr 1300, the output tax of a services advance given back).

const AccountTDSReceivable = "1310"

// ReceiptModes are how a back-office receipt can arrive.
var ReceiptModes = []string{"Bank Transfer", "Cheque", "UPI", "Card", "Cash"}

// ReceiptLine is one invoice the receipt settles, or one advance it uses.
type ReceiptLine struct {
	Ref    string  `json:"ref"`
	Amount float64 `json:"amount"`
}

// CustomerReceiptInput is one receipt as the Receipt screen sends it.
type CustomerReceiptInput struct {
	Customer       string        `json:"customer"`
	Location       string        `json:"location"`
	Date           string        `json:"date"`
	Mode           string        `json:"mode"`
	BankAccount    string        `json:"bank_account"`
	Reference      string        `json:"reference"`
	Amount         float64       `json:"amount"`
	TDS            float64       `json:"tds"`
	Invoices       []ReceiptLine `json:"invoices"`
	Advances       []ReceiptLine `json:"advances"`
	Purpose        string        `json:"purpose"`
	IdempotencyKey string        `json:"idempotency_key"`
}

// CustomerReceipt is a recorded receipt.
type CustomerReceipt struct {
	Number        string        `json:"number"`
	Customer      string        `json:"customer"`
	Location      string        `json:"location"`
	Date          string        `json:"date"`
	Mode          string        `json:"mode"`
	BankAccount   string        `json:"bank_account,omitempty"`
	Reference     string        `json:"reference,omitempty"`
	Amount        float64       `json:"amount"`
	TDS           float64       `json:"tds"`
	Invoices      []ReceiptLine `json:"invoices"`
	Advances      []ReceiptLine `json:"advances"`
	Allocated     float64       `json:"allocated"`
	OnAccount     string        `json:"on_account_advance,omitempty"`
	OnAccountAmt  float64       `json:"on_account_amount"`
	CreatedBy     string        `json:"created_by"`
	CreatedAt     string        `json:"created_at"`
	IdempotencyID string        `json:"idempotency_key,omitempty"`
}

func receiptFail(format string, args ...interface{}) error {
	return &ValidationError{Code: "FIN-0517", Message: fmt.Sprintf(format, args...)}
}

// receiptDebitAccount is where the money landed: the bank account's GL when
// one is chosen (required for a cheque or bank transfer), else the mode's
// clearing account.
func receiptDebitAccount(tenantID, mode, bankAccount string) (string, error) {
	if bankAccount = strings.TrimSpace(bankAccount); bankAccount != "" {
		schema, err := db.GetTenantSchema(tenantID)
		if err != nil {
			return "", err
		}
		var gl string
		_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(data->>'gl_account_code', '') FROM %s.documents
			WHERE doctype = 'BankAccount' AND deleted_at IS NULL AND (id = $1 OR data->>'account_number' = $1) LIMIT 1`, schema), bankAccount).Scan(&gl)
		if strings.TrimSpace(gl) == "" {
			return "", receiptFail("bank account %s was not found, or has no GL account", bankAccount)
		}
		return strings.TrimSpace(gl), nil
	}
	if mode == "Cheque" || mode == "Bank Transfer" {
		return "", receiptFail("choose the bank account a %s went into", strings.ToLower(mode))
	}
	return paymentModeClearingAccount(mode), nil
}

// RecordCustomerReceipt posts one receipt voucher in one transaction.
func RecordCustomerReceipt(tenantID, user string, in CustomerReceiptInput) (*CustomerReceipt, error) {
	in.Customer, in.Location, in.Mode = strings.TrimSpace(in.Customer), strings.TrimSpace(in.Location), strings.TrimSpace(in.Mode)
	if in.Mode == "" {
		in.Mode = "Bank Transfer"
	}
	known := false
	for _, m := range ReceiptModes {
		known = known || m == in.Mode
	}
	if !known {
		return nil, receiptFail("%q is not a way a receipt arrives (%s)", in.Mode, strings.Join(ReceiptModes, ", "))
	}
	if in.Location == "" {
		return nil, receiptFail("choose the store or office the receipt is booked at")
	}
	money, tds := RupeesToPaise(in.Amount), RupeesToPaise(in.TDS)
	if money < 0 || tds < 0 {
		return nil, receiptFail("the amount and TDS cannot be negative")
	}
	c, found, err := LoadCustomerRef(tenantID, in.Customer)
	if err != nil {
		return nil, err
	}
	if !found || c.WalkIn {
		return nil, receiptFail("choose the customer who paid")
	}
	in.Customer = c.ID
	debit := ""
	if money > 0 {
		if debit, err = receiptDebitAccount(tenantID, in.Mode, in.BankAccount); err != nil {
			return nil, err
		}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	key := strings.TrimSpace(in.IdempotencyKey)
	if key != "" {
		var number string
		if db.DB.QueryRow(fmt.Sprintf(`SELECT id FROM %s.documents WHERE doctype = 'CustomerReceipt' AND data->>'idempotency_key' = $1`, schema), key).Scan(&number) == nil {
			return LoadCustomerReceipt(tenantID, number)
		}
	}
	if in.Date == "" {
		in.Date = BusinessToday()
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := db.SetSearchPath(tx, schema); err != nil {
		return nil, err
	}
	number, err := issueCompactNumberTx(tx, schema, "CustomerReceipt", "RCT", in.Location, time.Now())
	if err != nil {
		return nil, err
	}
	postingKey := "CustomerReceipt:" + number

	// The customer's own advances first, so the money left over is what
	// becomes a new advance.
	debits := map[string]int64{}
	var advUsed int64
	seen := map[string]bool{}
	for _, a := range in.Advances {
		advNo := strings.TrimSpace(a.Ref)
		amount := RupeesToPaise(a.Amount)
		if advNo == "" || amount <= 0 {
			continue
		}
		if seen[advNo] {
			return nil, receiptFail("advance %s is on the receipt twice", advNo)
		}
		seen[advNo] = true
		share, err := useAdvanceTx(tx, tenantID, schema, advNo, in.Customer, amount, svSource{Doctype: "CustomerReceipt", ID: number, Location: in.Location, User: user})
		if err != nil {
			return nil, err
		}
		debits[AccountCustomerAdvLia] += amount - share.tax
		addLegs(debits, gstSplit(share.tax, share.interstate))
		advUsed += amount
	}

	available := money + tds + advUsed
	var allocated int64
	var invoices []ReceiptLine
	for _, l := range in.Invoices {
		id := strings.TrimSpace(l.Ref)
		want := RupeesToPaise(l.Amount)
		if id == "" || want <= 0 {
			continue
		}
		if allocated+want > available {
			return nil, receiptFail("the invoices take %.2f but the receipt brings %.2f", PaiseToRupees(allocated+want), PaiseToRupees(available))
		}
		got, err := noteInvoiceSettlementTx(tx, tenantID, schema, invoiceSettlement{InvoiceID: id, Ref: number, Kind: settlementKindReceipt,
			User: user, PostingKey: postingKey, Customer: in.Customer, Date: in.Date, Paise: want})
		if err != nil {
			return nil, err
		}
		allocated += got
		invoices = append(invoices, ReceiptLine{Ref: id, Amount: PaiseToRupees(got)})
	}
	if advUsed > allocated {
		return nil, receiptFail("advances pay invoices - allocate the %.2f of advance used to the customer's invoices", PaiseToRupees(advUsed))
	}
	if tds > allocated-advUsed {
		return nil, receiptFail("TDS is deducted on invoices - allocate at least the TDS to the invoices it was deducted on")
	}
	onAccount := available - allocated // only money is ever left over
	moneyAllocated := money - onAccount
	if available == 0 {
		return nil, receiptFail("enter the amount received, or the advance to use")
	}
	if moneyAllocated > 0 {
		debits[debit] += moneyAllocated
	}
	if tds > 0 {
		debits[AccountTDSReceivable] += tds
	}
	if allocated > 0 {
		if err := PostDoubleEntryTx(tx, tenantID, schema, "CustomerReceipt", number, debits, map[string]int64{"1300": allocated}, in.Date, postingKey); err != nil {
			return nil, postingError(err, "receipt not recorded")
		}
	}
	advance := ""
	if onAccount > 0 {
		purpose := strings.TrimSpace(in.Purpose)
		if purpose == "" {
			purpose = "On account, receipt " + number
		}
		advance, err = takeCustomerAdvanceTx(tx, tenantID, schema, user, "", TakeAdvanceRequest{Location: in.Location, CustomerID: in.Customer,
			Amount: PaiseToRupees(onAccount), PaymentMode: in.Mode, Purpose: purpose, SupplyType: "Goods", IdempotencyKey: "receipt|" + number}, c, debit)
		if err != nil {
			return nil, err
		}
	}
	var used []ReceiptLine
	for _, a := range in.Advances {
		if RupeesToPaise(a.Amount) > 0 {
			used = append(used, ReceiptLine{Ref: strings.TrimSpace(a.Ref), Amount: a.Amount})
		}
	}
	rec := CustomerReceipt{Number: number, Customer: in.Customer, Location: in.Location, Date: in.Date, Mode: in.Mode, BankAccount: strings.TrimSpace(in.BankAccount),
		Reference: strings.TrimSpace(in.Reference), Amount: PaiseToRupees(money), TDS: PaiseToRupees(tds), Invoices: invoices, Advances: used,
		Allocated: PaiseToRupees(allocated), OnAccount: advance, OnAccountAmt: PaiseToRupees(onAccount), CreatedBy: user, IdempotencyID: key}
	raw, err := json.Marshal(rec)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'CustomerReceipt', $2, 'Posted', $3)`, schema), number, raw, user); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	LogAuditEvent(tenantID, user, "FINANCE", "CUSTOMER_RECEIPT", fmt.Sprintf("%s: %.2f from %s by %s (TDS %.2f), %.2f to %d invoice(s), %.2f on account",
		number, PaiseToRupees(money), in.Customer, in.Mode, PaiseToRupees(tds), PaiseToRupees(allocated), len(invoices), PaiseToRupees(onAccount)))
	return LoadCustomerReceipt(tenantID, number)
}

type advanceShare struct {
	tax        int64
	interstate bool
}

// useAdvanceTx takes amount off one of the customer's own open advances.
func useAdvanceTx(tx *sql.Tx, tenantID, schema, number, customer string, amount int64, src svSource) (advanceShare, error) {
	var owner string
	err := tx.QueryRow(fmt.Sprintf(`SELECT customer_id FROM %s.customer_advances WHERE instrument_id = $1`, schema), number).Scan(&owner)
	if errors.Is(err, sql.ErrNoRows) {
		return advanceShare{}, receiptFail("%s is not a customer advance", number)
	}
	if err != nil {
		return advanceShare{}, err
	}
	if owner != customer {
		return advanceShare{}, receiptFail("advance %s was paid by another customer", number)
	}
	c, err := scanSVCard(tx.QueryRow(fmt.Sprintf(`SELECT %s FROM %s.stored_value_instruments WHERE instrument_id = $1 FOR UPDATE`, svCardColumns, schema), number))
	if err != nil {
		return advanceShare{}, err
	}
	p, err := LoadStoredValueProfile(tenantID, c.Profile)
	if err != nil {
		return advanceShare{}, err
	}
	if err := checkRedeemable(c, p, amount, BusinessToday()); err != nil {
		return advanceShare{}, err
	}
	if _, err := appendSVEvent(tx, schema, &c, SVEventRedeem, -amount, "", src, "receipt|"+src.ID+"|"+number); err != nil {
		return advanceShare{}, err
	}
	tax, interstate, err := advanceTaxShareTx(tx, schema, number, amount)
	return advanceShare{tax: tax, interstate: interstate}, err
}

// LoadCustomerReceipt reads one receipt.
func LoadCustomerReceipt(tenantID, number string) (*CustomerReceipt, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	var raw []byte
	var at time.Time
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT data, created_at FROM %s.documents WHERE doctype = 'CustomerReceipt' AND id = $1`, schema), number).Scan(&raw, &at); err != nil {
		return nil, fmt.Errorf("receipt %s not found", number)
	}
	var rec CustomerReceipt
	if err := json.Unmarshal(raw, &rec); err != nil {
		return nil, err
	}
	rec.CreatedAt = at.Format(time.RFC3339)
	return &rec, nil
}

// ListCustomerReceipts is the receipt register, newest first.
func ListCustomerReceipts(tenantID, customer string, limit int) ([]CustomerReceipt, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT data, created_at FROM %s.documents WHERE doctype = 'CustomerReceipt' AND deleted_at IS NULL
		AND ($1 = '' OR data->>'customer' = $1) ORDER BY created_at DESC LIMIT $2`, schema), strings.TrimSpace(customer), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CustomerReceipt{}
	for rows.Next() {
		var raw []byte
		var at time.Time
		if err := rows.Scan(&raw, &at); err != nil {
			return nil, err
		}
		var rec CustomerReceipt
		if json.Unmarshal(raw, &rec) == nil {
			rec.CreatedAt = at.Format(time.RFC3339)
			out = append(out, rec)
		}
	}
	return out, rows.Err()
}

// OpenReceivable is one invoice the customer still owes on.
type OpenReceivable struct {
	ID          string  `json:"id"`
	Number      string  `json:"number"`
	Date        string  `json:"date"`
	DueDate     string  `json:"due_date"`
	Total       float64 `json:"total"`
	Outstanding float64 `json:"outstanding"`
}

// CustomerOpenItems is what a receipt can be allocated to: the customer's
// posted invoices with a balance (oldest first) and open advances.
func CustomerOpenItems(tenantID, customer string) ([]OpenReceivable, []CustomerAdvance, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, nil, err
	}
	customer = strings.TrimSpace(customer)
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, COALESCE(data->>'invoice_number', id), COALESCE(data->>'invoice_date', public.erp_local_date(created_at)::text),
		COALESCE(data->>'due_date', ''), COALESCE((data->>'total_amount')::numeric, 0), %s
		FROM %s.documents WHERE doctype = 'SalesInvoice' AND status = 'Approved' AND deleted_at IS NULL AND data->>'customer' = $1
		ORDER BY 3, created_at`, salesInvoiceOutstandingSQL, schema), customer)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	invoices := []OpenReceivable{}
	for rows.Next() {
		var o OpenReceivable
		if err := rows.Scan(&o.ID, &o.Number, &o.Date, &o.DueDate, &o.Total, &o.Outstanding); err != nil {
			return nil, nil, err
		}
		if o.Outstanding > 0 {
			invoices = append(invoices, o)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	advances, err := ListCustomerAdvances(tenantID, customer, "", true)
	if err != nil {
		return nil, nil, err
	}
	sort.SliceStable(advances, func(i, j int) bool { return advances[i].TakenAt < advances[j].TakenAt })
	return invoices, advances, nil
}
