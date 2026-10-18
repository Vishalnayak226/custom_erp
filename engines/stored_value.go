package engines

import (
	"crypto/rand"
	"custom_erp/db"
	"database/sql"
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Gift cards and vouchers as stored value (Stage 72.8, the user's blueprint
// section 11). The physical card is only the carrier; what matters is the
// unique number and its balance, which is a liability to the holder - not
// merchandise revenue.
//
//   - Register: a manager loads pre-printed numbers (or has the ERP generate
//     them) as Inactive cards of a StoredValueProfile.
//   - Issue: the cashier scans the card, takes payment, and only then is the
//     card funded - activation, the ledger event and the liability posting
//     (Dr payment clearing / Cr 2610) commit together or not at all.
//   - Redeem: at checkout a card pays part or all of a bill, inside the sale's
//     own transaction - a sale that fails never moves a balance.
//   - Every balance change is an appended ledger event with the balance after
//     it and a unique idempotency key; the table refuses UPDATE and DELETE.
//
// Numbers never leave this file unmasked except to the manager who generated
// them, once, for printing.

const (
	SVStatusInactive  = "Inactive"
	SVStatusActive    = "Active"
	SVStatusPartial   = "Partially redeemed"
	SVStatusFull      = "Fully redeemed"
	SVStatusBlocked   = "Blocked"
	SVStatusExpired   = "Expired"
	SVStatusCancelled = "Cancelled"

	SVEventActivate = "ACTIVATE"
	SVEventReload   = "RELOAD"
	SVEventRedeem   = "REDEEM"
	SVEventBlock    = "BLOCK"
	SVEventUnblock  = "UNBLOCK"
	SVEventCancel   = "CANCEL"
)

// StoredValueProfile is the policy a card follows.
type StoredValueProfile struct {
	ID               string
	InstrumentType   string
	NumberLength     int
	Alphanumeric     bool
	Fixed            bool
	Denominations    []int64 // paise
	MinPaise         int64
	MaxPaise         int64
	ValidityMonths   int
	ReloadAllowed    bool
	PartialAllowed   bool
	LiabilityAccount string
	Active           bool
}

// LoadStoredValueProfile reads one profile.
func LoadStoredValueProfile(tenantID, id string) (StoredValueProfile, error) {
	data, status, err := fetchDocData(tenantID, "StoredValueProfile", id)
	if err != nil || data == nil {
		return StoredValueProfile{}, &ValidationError{Code: "POSOFF-0434", SubFor: id, Message: fmt.Sprintf("gift card profile %q not found", id)}
	}
	p := StoredValueProfile{
		ID:               id,
		InstrumentType:   pimString(data["instrument_type"]),
		NumberLength:     int(numFromInterface(data["number_length"])),
		Alphanumeric:     strings.EqualFold(pimString(data["number_format"]), "Alphanumeric"),
		Fixed:            strings.EqualFold(pimString(data["denomination_mode"]), "Fixed"),
		MinPaise:         RupeesToPaise(numFromInterface(data["min_amount"])),
		MaxPaise:         RupeesToPaise(numFromInterface(data["max_amount"])),
		ValidityMonths:   int(numFromInterface(data["validity_months"])),
		ReloadAllowed:    yesValue(data["reload_allowed"]),
		PartialAllowed:   !strings.EqualFold(strings.TrimSpace(pimString(data["partial_redemption"])), "No"),
		LiabilityAccount: strings.TrimSpace(pimString(data["liability_account"])),
		Active:           !strings.EqualFold(status, "Inactive") && !strings.EqualFold(pimString(data["status"]), "Inactive"),
	}
	for _, part := range strings.Split(pimString(data["denominations"]), ",") {
		if v, err := strconv.ParseFloat(strings.TrimSpace(part), 64); err == nil && v > 0 {
			p.Denominations = append(p.Denominations, RupeesToPaise(v))
		}
	}
	if p.LiabilityAccount == "" {
		p.LiabilityAccount = AccountStoredValueLiab
	}
	if p.NumberLength <= 0 {
		p.NumberLength = 16
	}
	return p, nil
}

// NormalizeCardNumber strips spaces and dashes and upper-cases.
func NormalizeCardNumber(n string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(n) {
		if (r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// MaskCardNumber shows only the last four characters.
func MaskCardNumber(n string) string {
	n = NormalizeCardNumber(n)
	if len(n) <= 4 {
		return "****"
	}
	return "**** " + n[len(n)-4:]
}

func validateCardNumberFormat(p StoredValueProfile, n string) error {
	if len(n) != p.NumberLength {
		return &ValidationError{Code: "POSOFF-0431", SubFor: MaskCardNumber(n), Message: fmt.Sprintf("a %s number has %d characters, this one has %d", p.ID, p.NumberLength, len(n))}
	}
	if !p.Alphanumeric {
		for _, r := range n {
			if r < '0' || r > '9' {
				return &ValidationError{Code: "POSOFF-0431", SubFor: MaskCardNumber(n), Message: fmt.Sprintf("a %s number is digits only", p.ID)}
			}
		}
	}
	return nil
}

// --- card registration --------------------------------------------------------

// RegisterCardsRequest loads cards: either the printed numbers, or Generate N.
type RegisterCardsRequest struct {
	Profile  string   `json:"profile"`
	Numbers  []string `json:"numbers"`
	Generate int      `json:"generate"`
}

// RegisterCardsResult lists what was registered. Numbers is filled only for
// generated cards - the one time they are shown, for printing.
type RegisterCardsResult struct {
	Registered int      `json:"registered"`
	Numbers    []string `json:"numbers,omitempty"`
	Masked     []string `json:"masked"`
}

// RegisterStoredValueCards creates Inactive cards. A number already known -
// in any state - is refused (blueprint 7.3: duplicate stored-value numbers
// are rejected globally).
func RegisterStoredValueCards(tenantID, user string, req RegisterCardsRequest) (*RegisterCardsResult, error) {
	p, err := LoadStoredValueProfile(tenantID, strings.TrimSpace(req.Profile))
	if err != nil {
		return nil, err
	}
	if !p.Active {
		return nil, &ValidationError{Code: "POSOFF-0434", SubFor: p.ID, Message: fmt.Sprintf("profile %s is Inactive", p.ID)}
	}
	var numbers []string
	generated := req.Generate > 0
	switch {
	case generated && len(req.Numbers) > 0:
		return nil, &ValidationError{Code: "POSOFF-0431", Message: "either list the printed numbers or ask for generated ones, not both"}
	case generated:
		if req.Generate > 1000 {
			return nil, &ValidationError{Code: "POSOFF-0431", Message: "generate at most 1,000 cards at a time"}
		}
		for len(numbers) < req.Generate {
			numbers = append(numbers, generateCardNumber(p))
		}
	default:
		if len(req.Numbers) == 0 || len(req.Numbers) > 5000 {
			return nil, &ValidationError{Code: "POSOFF-0431", Message: "list between 1 and 5,000 card numbers"}
		}
		seen := map[string]bool{}
		for _, raw := range req.Numbers {
			n := NormalizeCardNumber(raw)
			if n == "" {
				continue
			}
			if err := validateCardNumberFormat(p, n); err != nil {
				return nil, err
			}
			if seen[n] {
				return nil, &ValidationError{Code: "POSOFF-0432", SubFor: MaskCardNumber(n), Message: fmt.Sprintf("card %s is listed twice", MaskCardNumber(n))}
			}
			seen[n] = true
			numbers = append(numbers, n)
		}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	out := &RegisterCardsResult{}
	for i := 0; i < len(numbers); i++ {
		n := numbers[i]
		res, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.stored_value_instruments (instrument_id, card_number, profile, status, created_by)
			VALUES ($1, $2, $3, 'Inactive', $4) ON CONFLICT (card_number) DO NOTHING`, schema), NewDocIDCompact("SVI"), n, p.ID, user)
		if err != nil {
			return nil, err
		}
		if affected, _ := res.RowsAffected(); affected == 0 {
			if generated { // a random collision: draw again
				numbers[i] = generateCardNumber(p)
				i--
				continue
			}
			return nil, &ValidationError{Code: "POSOFF-0432", SubFor: MaskCardNumber(n), Message: fmt.Sprintf("card %s is already registered", MaskCardNumber(n))}
		}
		out.Registered++
		out.Masked = append(out.Masked, MaskCardNumber(n))
		if generated {
			out.Numbers = append(out.Numbers, n)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}

// generateCardNumber draws a random number; a numeric one ends in a Luhn
// check digit so a mistyped digit is caught at the till.
func generateCardNumber(p StoredValueProfile) string {
	const alnum = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	var b strings.Builder
	if p.Alphanumeric {
		for b.Len() < p.NumberLength {
			i, _ := rand.Int(rand.Reader, big.NewInt(int64(len(alnum))))
			b.WriteByte(alnum[i.Int64()])
		}
		return b.String()
	}
	for b.Len() < p.NumberLength-1 {
		d, _ := rand.Int(rand.Reader, big.NewInt(10))
		if b.Len() == 0 && d.Int64() == 0 {
			continue
		}
		b.WriteByte(byte('0' + d.Int64()))
	}
	body := b.String()
	return body + strconv.Itoa(luhnCheckDigit(body))
}

func luhnCheckDigit(body string) int {
	sum := 0
	double := true
	for i := len(body) - 1; i >= 0; i-- {
		d := int(body[i] - '0')
		if double {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
		double = !double
	}
	return (10 - sum%10) % 10
}

// --- cards and events ---------------------------------------------------------

type svCard struct {
	ID, Number, Profile, Status string
	Balance, Funded             int64
	ExpiresOn                   string
	Seq                         int
	IssuePaymentMode            string
}

const svCardColumns = `instrument_id, card_number, profile, status, balance_paise, funded_paise, COALESCE(to_char(expires_on, 'YYYY-MM-DD'), ''), last_event_seq, COALESCE(issue_payment_mode, '')`

func scanSVCard(row *sql.Row) (svCard, error) {
	var c svCard
	err := row.Scan(&c.ID, &c.Number, &c.Profile, &c.Status, &c.Balance, &c.Funded, &c.ExpiresOn, &c.Seq, &c.IssuePaymentMode)
	return c, err
}

func lockSVCardByNumber(tx *sql.Tx, schema, number string) (svCard, error) {
	c, err := scanSVCard(tx.QueryRow(fmt.Sprintf(`SELECT %s FROM %s.stored_value_instruments WHERE card_number = $1 FOR UPDATE`, svCardColumns, schema), number))
	if err == sql.ErrNoRows {
		return c, &ValidationError{Code: "POSOFF-0431", SubFor: MaskCardNumber(number), Message: fmt.Sprintf("card %s is not a registered gift card", MaskCardNumber(number))}
	}
	return c, err
}

// effectiveStatus turns a funded card past its expiry date into Expired.
func (c svCard) effectiveStatus(today string) string {
	if (c.Status == SVStatusActive || c.Status == SVStatusPartial) && c.ExpiresOn != "" && c.ExpiresOn < today {
		return SVStatusExpired
	}
	return c.Status
}

// statusFromBalance is a funded card's status after a balance change.
func (c svCard) statusFromBalance() string {
	switch {
	case c.Funded == 0:
		return SVStatusInactive
	case c.Balance == 0:
		return SVStatusFull
	case c.Balance < c.Funded:
		return SVStatusPartial
	}
	return SVStatusActive
}

// svSource is where an event came from.
type svSource struct {
	Doctype, ID, Location, PaymentMode, User, Reason string
}

// svLedgerGuarded remembers the schemas whose ledger already carries the
// append-only trigger. A client provisioned after the 72.8 migration gets its
// tables cloned with LIKE ... INCLUDING ALL, which copies no triggers, so the
// guard is put in place before the first event is written there.
var svLedgerGuarded sync.Map

func ensureSVLedgerGuard(tx *sql.Tx, schema string) error {
	if _, ok := svLedgerGuarded.Load(schema); ok {
		return nil
	}
	var exists bool
	if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM pg_trigger t JOIN pg_class c ON c.oid = t.tgrelid JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = $1 AND c.relname = 'stored_value_ledger' AND t.tgname = 'stored_value_ledger_append_only')`, schema).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		if _, err := tx.Exec(fmt.Sprintf(`CREATE TRIGGER stored_value_ledger_append_only BEFORE UPDATE OR DELETE ON %s.stored_value_ledger
			FOR EACH ROW EXECUTE FUNCTION public.stored_value_ledger_append_only()`, schema)); err != nil {
			return fmt.Errorf("gift card ledger guard: %v", err)
		}
	}
	svLedgerGuarded.Store(schema, true)
	return nil
}

// appendSVEvent writes one ledger event and moves the card to its new
// balance and status, in the caller's transaction.
func appendSVEvent(tx *sql.Tx, schema string, c *svCard, event string, amount int64, status string, src svSource, idemKey string) (int64, error) {
	if err := ensureSVLedgerGuard(tx, schema); err != nil {
		return 0, err
	}
	newBalance := c.Balance + amount
	if newBalance < 0 {
		return 0, &ValidationError{Code: "POSOFF-0433", SubFor: MaskCardNumber(c.Number), Message: fmt.Sprintf("card %s has %.2f, not %.2f", MaskCardNumber(c.Number), PaiseToRupees(c.Balance), PaiseToRupees(-amount))}
	}
	c.Seq++
	var key interface{}
	if idemKey != "" {
		key = idemKey
	}
	var entryID int64
	if err := tx.QueryRow(fmt.Sprintf(`INSERT INTO %s.stored_value_ledger
		(instrument_id, event_seq, event_type, amount_paise, balance_after_paise, source_doctype, source_id, location, payment_mode, user_name, reason, idempotency_key)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), NULLIF($7, ''), NULLIF($8, ''), NULLIF($9, ''), $10, NULLIF($11, ''), $12) RETURNING entry_id`, schema),
		c.ID, c.Seq, event, amount, newBalance, src.Doctype, src.ID, src.Location, src.PaymentMode, src.User, src.Reason, key).Scan(&entryID); err != nil {
		return 0, err
	}
	c.Balance = newBalance
	if amount > 0 && event != SVEventReversal {
		// A REVERSAL (Stage 73.1.6, a returned item paid by card) gives back
		// value already funded; it is not new funding.
		c.Funded += amount
	}
	if status == "" {
		status = c.statusFromBalance()
	}
	c.Status = status
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.stored_value_instruments SET balance_paise = $2, funded_paise = $3, status = $4,
		last_event_seq = $5, updated_at = CURRENT_TIMESTAMP WHERE instrument_id = $1`, schema), c.ID, c.Balance, c.Funded, c.Status, c.Seq); err != nil {
		return 0, err
	}
	return entryID, nil
}

// --- issue / add value --------------------------------------------------------

// StoredValueIssueRequest funds a card at the till.
type StoredValueIssueRequest struct {
	CardNumber     string  `json:"card_number"`
	Amount         float64 `json:"amount"`
	PaymentMode    string  `json:"payment_mode"`
	Location       string  `json:"location"`
	CustomerID     string  `json:"customer_id"`
	IdempotencyKey string  `json:"idempotency_key"`
}

// StoredValueReceipt is what the issue receipt prints.
type StoredValueReceipt struct {
	EntryID     int64   `json:"entry_id"`
	Event       string  `json:"event"`
	Masked      string  `json:"card"`
	Profile     string  `json:"profile"`
	Amount      float64 `json:"amount"`
	Balance     float64 `json:"balance"`
	ExpiresOn   string  `json:"expires_on,omitempty"`
	Location    string  `json:"location"`
	PaymentMode string  `json:"payment_mode,omitempty"`
	User        string  `json:"user"`
	At          string  `json:"at"`
	Replayed    bool    `json:"replayed,omitempty"`
}

func checkIssueAmount(p StoredValueProfile, amount int64) error {
	if amount <= 0 {
		return &ValidationError{Code: "POSOFF-0435", Message: "enter the amount to load on the card"}
	}
	if p.Fixed {
		for _, d := range p.Denominations {
			if d == amount {
				return nil
			}
		}
		opts := make([]string, 0, len(p.Denominations))
		for _, d := range p.Denominations {
			opts = append(opts, fmt.Sprintf("%.0f", PaiseToRupees(d)))
		}
		return &ValidationError{Code: "POSOFF-0435", Message: fmt.Sprintf("%s cards come in %s only", p.ID, strings.Join(opts, ", "))}
	}
	if (p.MinPaise > 0 && amount < p.MinPaise) || (p.MaxPaise > 0 && amount > p.MaxPaise) {
		return &ValidationError{Code: "POSOFF-0435", Message: fmt.Sprintf("a %s takes %.2f to %.2f", p.ID, PaiseToRupees(p.MinPaise), PaiseToRupees(p.MaxPaise))}
	}
	return nil
}

// IssueStoredValue activates (or, where the profile allows, adds value to) a
// card once the customer has paid. Idempotent on IdempotencyKey: a retry
// returns the original receipt; the same key for another card is refused.
func IssueStoredValue(tenantID, user string, req StoredValueIssueRequest) (*StoredValueReceipt, error) {
	number := NormalizeCardNumber(req.CardNumber)
	key := strings.TrimSpace(req.IdempotencyKey)
	if number == "" || key == "" {
		return nil, &ValidationError{Code: "POSOFF-0431", Message: "scan or type the card number"}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	idem := "issue|" + key
	if r, err := svReceiptByKey(tenantID, schema, idem, number); r != nil || err != nil {
		return r, err
	}
	amount := RupeesToPaise(req.Amount)
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	c, err := lockSVCardByNumber(tx, schema, number)
	if err != nil {
		return nil, err
	}
	p, err := LoadStoredValueProfile(tenantID, c.Profile)
	if err != nil {
		return nil, err
	}
	if !p.Active {
		return nil, &ValidationError{Code: "POSOFF-0434", SubFor: MaskCardNumber(number), Message: fmt.Sprintf("cards of profile %s are no longer issued", p.ID)}
	}
	if err := checkIssueAmount(p, amount); err != nil {
		return nil, err
	}
	today := time.Now().Format("2006-01-02")
	event := SVEventActivate
	switch c.effectiveStatus(today) {
	case SVStatusInactive:
	case SVStatusActive, SVStatusPartial, SVStatusFull:
		if !p.ReloadAllowed {
			return nil, &ValidationError{Code: "POSOFF-0432", SubFor: MaskCardNumber(number), Message: fmt.Sprintf("card %s is already issued and %s cards cannot have value added", MaskCardNumber(number), p.ID)}
		}
		event = SVEventReload
	default:
		return nil, &ValidationError{Code: "POSOFF-0434", SubFor: MaskCardNumber(number), Message: fmt.Sprintf("card %s is %s", MaskCardNumber(number), strings.ToLower(c.effectiveStatus(today)))}
	}
	src := svSource{Doctype: "GiftCardIssue", ID: key, Location: req.Location, PaymentMode: req.PaymentMode, User: user}
	entryID, err := appendSVEvent(tx, schema, &c, event, amount, "", src, idem)
	if err != nil {
		return nil, err
	}
	if event == SVEventActivate {
		expires := interface{}(nil)
		if p.ValidityMonths > 0 {
			expires = time.Now().AddDate(0, p.ValidityMonths, 0).Format("2006-01-02")
			c.ExpiresOn = expires.(string)
		}
		if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.stored_value_instruments SET activated_at = CURRENT_TIMESTAMP, expires_on = $2,
			issued_location = NULLIF($3, ''), issue_payment_mode = NULLIF($4, ''), customer_id = NULLIF($5, '') WHERE instrument_id = $1`, schema),
			c.ID, expires, req.Location, req.PaymentMode, req.CustomerID); err != nil {
			return nil, err
		}
	}
	if err := PostDoubleEntryTx(tx, tenantID, schema, "StoredValue", c.ID,
		map[string]int64{paymentModeClearingAccount(req.PaymentMode): amount}, map[string]int64{p.LiabilityAccount: amount},
		"", fmt.Sprintf("StoredValue:%d:FUND", entryID)); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &StoredValueReceipt{EntryID: entryID, Event: event, Masked: MaskCardNumber(number), Profile: p.ID,
		Amount: PaiseToRupees(amount), Balance: PaiseToRupees(c.Balance), ExpiresOn: c.ExpiresOn,
		Location: req.Location, PaymentMode: req.PaymentMode, User: user, At: time.Now().UTC().Format(time.RFC3339)}, nil
}

// svReceiptByKey replays an already-processed event (ITM-016).
func svReceiptByKey(tenantID, schema, idem, number string) (*StoredValueReceipt, error) {
	var r StoredValueReceipt
	var card, profile, location, mode, at, expires string
	var amount, balance int64
	err := db.DB.QueryRow(fmt.Sprintf(`SELECT l.entry_id, l.event_type, i.card_number, i.profile, l.amount_paise, l.balance_after_paise,
		COALESCE(to_char(i.expires_on, 'YYYY-MM-DD'), ''), COALESCE(l.location, ''), COALESCE(l.payment_mode, ''), COALESCE(l.user_name, ''), l.created_at::text
		FROM %[1]s.stored_value_ledger l JOIN %[1]s.stored_value_instruments i ON i.instrument_id = l.instrument_id WHERE l.idempotency_key = $1`, schema), idem).
		Scan(&r.EntryID, &r.Event, &card, &profile, &amount, &balance, &expires, &location, &mode, &r.User, &at)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if card != number {
		return nil, &ValidationError{Code: "POSOFF-0436", Message: "this request was already used for another card - start a new issue"}
	}
	r.Masked, r.Profile, r.Amount, r.Balance, r.ExpiresOn, r.Location, r.PaymentMode, r.At, r.Replayed =
		MaskCardNumber(card), profile, PaiseToRupees(amount), PaiseToRupees(balance), expires, location, mode, at, true
	return &r, nil
}

// --- balance -----------------------------------------------------------------

// StoredValueCardView is what Balance shows - never the full number.
type StoredValueCardView struct {
	Masked         string  `json:"card"`
	Profile        string  `json:"profile"`
	Status         string  `json:"status"`
	Balance        float64 `json:"balance"`
	Funded         float64 `json:"loaded"`
	ExpiresOn      string  `json:"expires_on,omitempty"`
	PartialAllowed bool    `json:"part_use_allowed"`
	ReloadAllowed  bool    `json:"add_value_allowed"`
}

// StoredValueBalance looks a card up by number.
func StoredValueBalance(tenantID, number string) (*StoredValueCardView, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	number = NormalizeCardNumber(number)
	c, err := scanSVCard(db.DB.QueryRow(fmt.Sprintf(`SELECT %s FROM %s.stored_value_instruments WHERE card_number = $1`, svCardColumns, schema), number))
	if err == sql.ErrNoRows {
		return nil, &ValidationError{Code: "POSOFF-0431", SubFor: MaskCardNumber(number), Message: fmt.Sprintf("card %s is not a registered gift card", MaskCardNumber(number))}
	}
	if err != nil {
		return nil, err
	}
	p, _ := LoadStoredValueProfile(tenantID, c.Profile)
	return &StoredValueCardView{Masked: MaskCardNumber(number), Profile: c.Profile, Status: c.effectiveStatus(time.Now().Format("2006-01-02")),
		Balance: PaiseToRupees(c.Balance), Funded: PaiseToRupees(c.Funded), ExpiresOn: c.ExpiresOn,
		PartialAllowed: p.PartialAllowed, ReloadAllowed: p.ReloadAllowed}, nil
}

// --- redemption as a tender ------------------------------------------------------

// GiftCardTenderRequest is a card the cashier is paying with.
type GiftCardTenderRequest struct {
	CardNumber string  `json:"card_number"`
	Amount     float64 `json:"amount"`
}

// GiftCardTender is a checked tender, stored on the POSCart - masked, with
// the internal card id, never the number.
type GiftCardTender struct {
	InstrumentID string `json:"instrument_id"`
	Masked       string `json:"card"`
	AmountPaise  int64  `json:"amount_paise"`
}

func checkRedeemable(c svCard, p StoredValueProfile, amount int64, today string) error {
	masked := MaskCardNumber(c.Number)
	switch st := c.effectiveStatus(today); st {
	case SVStatusActive, SVStatusPartial:
	case SVStatusInactive:
		return &ValidationError{Code: "POSOFF-0434", SubFor: masked, Message: fmt.Sprintf("card %s has not been issued (it is not funded)", masked)}
	case SVStatusFull:
		return &ValidationError{Code: "POSOFF-0433", SubFor: masked, Message: fmt.Sprintf("card %s has no balance left", masked)}
	default:
		return &ValidationError{Code: "POSOFF-0434", SubFor: masked, Message: fmt.Sprintf("card %s is %s", masked, strings.ToLower(st))}
	}
	if amount <= 0 {
		return &ValidationError{Code: "POSOFF-0435", SubFor: masked, Message: "enter how much to take from the card"}
	}
	if amount > c.Balance {
		return &ValidationError{Code: "POSOFF-0433", SubFor: masked, Message: fmt.Sprintf("card %s has %.2f, not %.2f", masked, PaiseToRupees(c.Balance), PaiseToRupees(amount))}
	}
	if !p.PartialAllowed && amount != c.Balance {
		return &ValidationError{Code: "POSOFF-0435", SubFor: masked, Message: fmt.Sprintf("card %s must be used in full (%.2f) in one bill", masked, PaiseToRupees(c.Balance))}
	}
	return nil
}

// PrepareGiftCardTenders checks the cards a cashier is paying with before
// anything posts, and returns what the cart stores. The authoritative check
// runs again under a row lock when the sale posts.
func PrepareGiftCardTenders(tenantID string, reqs []GiftCardTenderRequest, duePaise int64) ([]GiftCardTender, error) {
	if len(reqs) == 0 {
		return nil, nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	today := time.Now().Format("2006-01-02")
	seen := map[string]bool{}
	var total int64
	out := make([]GiftCardTender, 0, len(reqs))
	for _, r := range reqs {
		number := NormalizeCardNumber(r.CardNumber)
		if seen[number] {
			return nil, &ValidationError{Code: "POSOFF-0435", SubFor: MaskCardNumber(number), Message: fmt.Sprintf("card %s is on the bill twice", MaskCardNumber(number))}
		}
		seen[number] = true
		c, err := scanSVCard(db.DB.QueryRow(fmt.Sprintf(`SELECT %s FROM %s.stored_value_instruments WHERE card_number = $1`, svCardColumns, schema), number))
		if err == sql.ErrNoRows {
			return nil, &ValidationError{Code: "POSOFF-0431", SubFor: MaskCardNumber(number), Message: fmt.Sprintf("card %s is not a registered gift card", MaskCardNumber(number))}
		}
		if err != nil {
			return nil, err
		}
		p, err := LoadStoredValueProfile(tenantID, c.Profile)
		if err != nil {
			return nil, err
		}
		amount := RupeesToPaise(r.Amount)
		if err := checkRedeemable(c, p, amount, today); err != nil {
			return nil, err
		}
		total += amount
		out = append(out, GiftCardTender{InstrumentID: c.ID, Masked: MaskCardNumber(number), AmountPaise: amount})
	}
	if total > duePaise {
		return nil, &ValidationError{Code: "POSOFF-0435", Message: fmt.Sprintf("gift cards cover %.2f but the bill is %.2f - take less from the card", PaiseToRupees(total), PaiseToRupees(duePaise))}
	}
	return out, nil
}

// redeemGiftCardTendersTx takes each tender off its card inside the sale's
// transaction and moves it from the liability to the payment clearing account
// the sale was debited to. A replayed sale finds its events by key and posts
// nothing twice.
func redeemGiftCardTendersTx(tx *sql.Tx, schema, tenantID, cartNumber, location, cashier, paymentMode string, tenders []GiftCardTender) error {
	sorted := append([]GiftCardTender(nil), tenders...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].InstrumentID < sorted[j].InstrumentID })
	today := time.Now().Format("2006-01-02")
	for _, t := range sorted {
		idem := "redeem|" + cartNumber + "|" + t.InstrumentID
		var done bool
		if err := tx.QueryRow(fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM %s.stored_value_ledger WHERE idempotency_key = $1)`, schema), idem).Scan(&done); err != nil {
			return err
		}
		if done {
			continue
		}
		c, err := scanSVCard(tx.QueryRow(fmt.Sprintf(`SELECT %s FROM %s.stored_value_instruments WHERE instrument_id = $1 FOR UPDATE`, svCardColumns, schema), t.InstrumentID))
		if err != nil {
			return fmt.Errorf("gift card %s: %v", t.Masked, err)
		}
		p, err := LoadStoredValueProfile(tenantID, c.Profile)
		if err != nil {
			return err
		}
		if err := checkRedeemable(c, p, t.AmountPaise, today); err != nil {
			return err
		}
		src := svSource{Doctype: "POSCart", ID: cartNumber, Location: location, PaymentMode: paymentMode, User: cashier}
		if _, err := appendSVEvent(tx, schema, &c, SVEventRedeem, -t.AmountPaise, "", src, idem); err != nil {
			return err
		}
		if err := PostDoubleEntryTx(tx, tenantID, schema, "POSCart", cartNumber,
			map[string]int64{p.LiabilityAccount: t.AmountPaise}, map[string]int64{paymentModeClearingAccount(paymentMode): t.AmountPaise},
			"", fmt.Sprintf("POSCart:%s:GIFTCARD:%s", cartNumber, t.InstrumentID)); err != nil {
			return err
		}
		// Stage 85.D3: a customer advance for services paid its GST when it
		// was taken; the bill charges the full tax, so that part comes back.
		if c.Profile == AdvanceProfileID {
			if err := postAdvanceTaxOnUseTx(tx, tenantID, schema, cartNumber, t.InstrumentID, t.AmountPaise); err != nil {
				return err
			}
		}
	}
	return nil
}

// --- block / unblock / cancel -----------------------------------------------------

// SetStoredValueBlocked holds a card (lost, suspected misuse) or releases it.
// Both need a reason (blueprint 19.2: unblock requires approval/reason).
func SetStoredValueBlocked(tenantID, user, number, reason string, block bool) (*StoredValueCardView, error) {
	number = NormalizeCardNumber(number)
	if strings.TrimSpace(reason) == "" {
		return nil, &ValidationError{Code: "POSOFF-0434", SubFor: MaskCardNumber(number), Message: "say why the card is being blocked or released"}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	c, err := lockSVCardByNumber(tx, schema, number)
	if err != nil {
		return nil, err
	}
	src := svSource{Doctype: "GiftCard", ID: c.ID, User: user, Reason: strings.TrimSpace(reason)}
	if block {
		if c.Status == SVStatusBlocked || c.Status == SVStatusCancelled {
			return nil, &ValidationError{Code: "POSOFF-0434", SubFor: MaskCardNumber(number), Message: fmt.Sprintf("card %s is already %s", MaskCardNumber(number), strings.ToLower(c.Status))}
		}
		if _, err := appendSVEvent(tx, schema, &c, SVEventBlock, 0, SVStatusBlocked, src, ""); err != nil {
			return nil, err
		}
	} else {
		if c.Status != SVStatusBlocked {
			return nil, &ValidationError{Code: "POSOFF-0434", SubFor: MaskCardNumber(number), Message: fmt.Sprintf("card %s is not blocked", MaskCardNumber(number))}
		}
		if _, err := appendSVEvent(tx, schema, &c, SVEventUnblock, 0, "", src, ""); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return StoredValueBalance(tenantID, number)
}

// CancelStoredValueIssue reverses an issue that was never used (blueprint
// 18: allowed only while the balance is unused): the balance goes to zero,
// the card is Cancelled, and the liability goes back to the payment mode it
// was paid by - the refund the cashier hands back.
func CancelStoredValueIssue(tenantID, user, number, reason, location string) (*StoredValueReceipt, error) {
	number = NormalizeCardNumber(number)
	if strings.TrimSpace(reason) == "" {
		return nil, &ValidationError{Code: "POSOFF-0434", SubFor: MaskCardNumber(number), Message: "say why the issue is being cancelled"}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	c, err := lockSVCardByNumber(tx, schema, number)
	if err != nil {
		return nil, err
	}
	if c.Status != SVStatusActive || c.Balance != c.Funded || c.Balance == 0 {
		return nil, &ValidationError{Code: "POSOFF-0434", SubFor: MaskCardNumber(number), Message: fmt.Sprintf("card %s can be cancelled only while its whole balance is unused (it is %s, %.2f of %.2f left)",
			MaskCardNumber(number), strings.ToLower(c.Status), PaiseToRupees(c.Balance), PaiseToRupees(c.Funded))}
	}
	p, err := LoadStoredValueProfile(tenantID, c.Profile)
	if err != nil {
		return nil, err
	}
	refund := c.Balance
	src := svSource{Doctype: "GiftCard", ID: c.ID, Location: location, PaymentMode: c.IssuePaymentMode, User: user, Reason: strings.TrimSpace(reason)}
	entryID, err := appendSVEvent(tx, schema, &c, SVEventCancel, -refund, SVStatusCancelled, src, "cancel|"+c.ID)
	if err != nil {
		return nil, err
	}
	if err := PostDoubleEntryTx(tx, tenantID, schema, "StoredValue", c.ID,
		map[string]int64{p.LiabilityAccount: refund}, map[string]int64{paymentModeClearingAccount(c.IssuePaymentMode): refund},
		"", fmt.Sprintf("StoredValue:%d:CANCEL", entryID)); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &StoredValueReceipt{EntryID: entryID, Event: SVEventCancel, Masked: MaskCardNumber(number), Profile: c.Profile,
		Amount: PaiseToRupees(refund), Balance: 0, Location: location, PaymentMode: c.IssuePaymentMode, User: user, At: time.Now().UTC().Format(time.RFC3339)}, nil
}

// --- reports (blueprint 20: Stored Value Liability, Stored Value Audit Ledger) ---

func init() {
	RegisterReport(ReportDefinition{
		ID: "stored-value-liability", Label: "Gift Card Liability", Category: "Finance",
		Columns: []ReportColumn{
			{Key: "card", Label: "Card"}, {Key: "profile", Label: "Profile"}, {Key: "status", Label: "Status"},
			{Key: "loaded", Label: "Loaded"}, {Key: "redeemed", Label: "Used / cancelled"}, {Key: "balance", Label: "Outstanding"},
			{Key: "expires_on", Label: "Expires"}, {Key: "issued_at", Label: "Store issued"},
		},
		Params: []ReportParam{{Key: "status", Label: "Status", Type: "select", Options: ",Inactive,Active,Partially redeemed,Fully redeemed,Blocked,Cancelled"}},
		Run:    storedValueLiabilityReport,
	})
	RegisterReport(ReportDefinition{
		ID: "stored-value-ledger", Label: "Gift Card Ledger", Category: "Finance",
		Columns: []ReportColumn{
			{Key: "at", Label: "When"}, {Key: "card", Label: "Card"}, {Key: "seq", Label: "#"}, {Key: "event", Label: "Event"},
			{Key: "amount", Label: "Amount"}, {Key: "balance_after", Label: "Balance after"}, {Key: "source", Label: "Source"},
			{Key: "location", Label: "Store"}, {Key: "user", Label: "User"}, {Key: "reason", Label: "Reason"},
		},
		Params: []ReportParam{{Key: "card", Label: "Last 4 digits", Type: "text"}, {Key: "from", Label: "From", Type: "date"}, {Key: "to", Label: "To", Type: "date"}},
		Run:    storedValueLedgerReport,
	})
}

func storedValueLiabilityReport(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT card_number, profile, status, balance_paise, funded_paise,
		COALESCE(to_char(expires_on, 'YYYY-MM-DD'), ''), COALESCE(issued_location, '')
		FROM %s.stored_value_instruments ORDER BY created_at, card_number`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	today := time.Now().Format("2006-01-02")
	want := strings.TrimSpace(params["status"])
	out := []map[string]interface{}{}
	for rows.Next() {
		var c svCard
		var loc string
		if err := rows.Scan(&c.Number, &c.Profile, &c.Status, &c.Balance, &c.Funded, &c.ExpiresOn, &loc); err != nil {
			return nil, err
		}
		st := c.effectiveStatus(today)
		if want != "" && !strings.EqualFold(want, st) {
			continue
		}
		out = append(out, map[string]interface{}{
			"card": MaskCardNumber(c.Number), "profile": c.Profile, "status": st,
			"loaded": PaiseToRupees(c.Funded), "redeemed": PaiseToRupees(c.Funded - c.Balance), "balance": PaiseToRupees(c.Balance),
			"expires_on": c.ExpiresOn, "issued_at": loc,
		})
	}
	return out, rows.Err()
}

func storedValueLedgerReport(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT l.created_at, i.card_number, l.event_seq, l.event_type, l.amount_paise, l.balance_after_paise,
		COALESCE(l.source_doctype, '') || CASE WHEN l.source_id IS NULL THEN '' ELSE ' ' || l.source_id END,
		COALESCE(l.location, ''), COALESCE(l.user_name, ''), COALESCE(l.reason, '')
		FROM %[1]s.stored_value_ledger l JOIN %[1]s.stored_value_instruments i ON i.instrument_id = l.instrument_id
		WHERE ($1 = '' OR right(i.card_number, 4) = $1)
		  AND ($2 = '' OR l.created_at >= $2::date) AND ($3 = '' OR l.created_at < $3::date + 1)
		ORDER BY l.created_at DESC, l.entry_id DESC LIMIT 5000`, schema),
		strings.TrimSpace(params["card"]), strings.TrimSpace(params["from"]), strings.TrimSpace(params["to"]))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]interface{}{}
	for rows.Next() {
		var at time.Time
		var card, event, source, location, user, reason string
		var seq int
		var amount, balance int64
		if err := rows.Scan(&at, &card, &seq, &event, &amount, &balance, &source, &location, &user, &reason); err != nil {
			return nil, err
		}
		out = append(out, map[string]interface{}{
			"at": at.Format("2006-01-02 15:04"), "card": MaskCardNumber(card), "seq": seq, "event": event,
			"amount": PaiseToRupees(amount), "balance_after": PaiseToRupees(balance), "source": source,
			"location": location, "user": user, "reason": reason,
		})
	}
	return out, rows.Err()
}
