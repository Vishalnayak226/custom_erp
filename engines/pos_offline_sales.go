package engines

// Stage 49.5.6a (user, 2026-10-28: "There will be a validation of offline
// sales screen. Once its validated it will deduct. Validation of erp barcode
// vs scanned. Else won't be posted."): a checkout the till sends as rung up
// offline is held in pos_offline_sales instead of posting. A manager opens it
// on POS > Offline Sales; Validate posts it through the normal checkout (the
// caller supplies that as post) only when every line was scanned with one of
// that item's ERP barcodes, and never by the cashier who rang it up. Reject
// (with a reason) leaves it unposted. This is what gates offline_synced now:
// before, any till could set the flag on a live sale and lift the stock floor
// and the cash limit (safe_overrides_test.go's former Open entry).

import (
	"context"
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	OfflineSalePending   = "Pending"
	OfflineSaleValidated = "Validated"
	OfflineSaleRejected  = "Rejected"
)

var (
	ErrOfflineSaleNotFound = errors.New("offline sale not found")
	// ErrOfflineSaleKeyReused: the same idempotency key arrived with a
	// different sale - the checkout's own ClaimPayloadMismatch answer.
	ErrOfflineSaleKeyReused = errors.New("this idempotency key was already used for a different sale")
)

// OfflineSaleItem is what a held sale's line says: the SKU, how many and the
// code the till scanned for it ("" when the line was searched or typed).
type OfflineSaleItem struct {
	Sku     string `json:"sku"`
	Qty     int    `json:"qty"`
	Scanned string `json:"scanned"`
}

// OfflineSaleLine is one line checked against its item's ERP barcodes.
type OfflineSaleLine struct {
	Sku         string   `json:"sku"`
	Name        string   `json:"name"`
	Qty         int      `json:"qty"`
	Scanned     string   `json:"scanned"`
	ERPBarcodes []string `json:"erp_barcodes"`
	Match       bool     `json:"match"`
}

// OfflineSale is one held offline sale.
type OfflineSale struct {
	ID            string            `json:"id"`
	CartNumber    string            `json:"cart_number"`
	Cashier       string            `json:"cashier"`
	CashierUserID string            `json:"-"`
	CashierRole   string            `json:"-"`
	Location      string            `json:"location"`
	POSSession    string            `json:"pos_session"`
	StoreDevice   string            `json:"store_device,omitempty"`
	SoldAt        *time.Time        `json:"sold_at,omitempty"`
	ReceivedAt    time.Time         `json:"received_at"`
	Total         float64           `json:"total"`
	LinesTotal    int               `json:"lines_total"`
	LinesMatched  int               `json:"lines_matched"`
	Status        string            `json:"status"`
	DecidedBy     string            `json:"decided_by,omitempty"`
	DecidedAt     *time.Time        `json:"decided_at,omitempty"`
	RejectReason  string            `json:"reject_reason,omitempty"`
	BillNumber    string            `json:"bill_number,omitempty"`
	Lines         []OfflineSaleLine `json:"lines,omitempty"`
	Payload       []byte            `json:"-"`
}

// OfflineSaleHold is what the checkout hands over when it holds a sale.
type OfflineSaleHold struct {
	CartNumber     string
	IdempotencyKey string // already namespaced "<cashier user id>:<key>"
	Digest         string
	Payload        []byte
	CashierUserID  string
	Cashier        string
	CashierRole    string
	Location       string
	POSSession     string
	StoreDevice    string
	SoldAt         *time.Time
	Total          float64
	Items          []OfflineSaleItem
}

const offlineSaleColumns = `id, cart_number, payload, cashier_user_id, cashier, cashier_role, location, pos_session,
	store_device, sold_at, received_at, total, lines_total, lines_matched, status, decided_by, decided_at,
	reject_reason, bill_number`

type offlineSaleScanner interface {
	Scan(dest ...interface{}) error
}

func scanOfflineSale(row offlineSaleScanner) (*OfflineSale, error) {
	var s OfflineSale
	var soldAt, decidedAt sql.NullTime
	if err := row.Scan(&s.ID, &s.CartNumber, &s.Payload, &s.CashierUserID, &s.Cashier, &s.CashierRole, &s.Location,
		&s.POSSession, &s.StoreDevice, &soldAt, &s.ReceivedAt, &s.Total, &s.LinesTotal, &s.LinesMatched, &s.Status,
		&s.DecidedBy, &decidedAt, &s.RejectReason, &s.BillNumber); err != nil {
		return nil, err
	}
	if soldAt.Valid {
		s.SoldAt = &soldAt.Time
	}
	if decidedAt.Valid {
		s.DecidedAt = &decidedAt.Time
	}
	return &s, nil
}

// Items is the held sale's lines as the till sent them.
func (s *OfflineSale) Items() []OfflineSaleItem {
	var p struct {
		Items []OfflineSaleItem `json:"items"`
	}
	_ = json.Unmarshal(s.Payload, &p)
	return p.Items
}

// itemERPBarcodes is every code a label of this item can carry: its barcode
// and GTIN (what the till's scan matches, public/view-pos.js posExactBarcode),
// its receipt-date barcodes (the ItemBarcode register, Stage 57.7), and the
// SKU itself when the item has no barcode - the code its sticker prints then
// (sticker-studio.js: barcode || code).
func itemERPBarcodes(schema string, item *ResolvedItem) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	add := func(v interface{}) {
		s, _ := v.(string)
		s = strings.TrimSpace(s)
		if s != "" && !seen[strings.ToLower(s)] {
			seen[strings.ToLower(s)] = true
			out = append(out, s)
		}
	}
	add(item.Data["barcode"])
	add(item.Data["gtin"])
	if b, _ := item.Data["barcode"].(string); strings.TrimSpace(b) == "" {
		add(item.Data["code"])
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT COALESCE(NULLIF(data->>'code', ''), id) FROM %s.documents
		WHERE doctype = 'ItemBarcode' AND deleted_at IS NULL AND status = 'Active' AND data->>'item' = $1
		ORDER BY created_at`, schema), item.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return nil, err
		}
		add(code)
	}
	return out, rows.Err()
}

// CheckOfflineSaleLines checks each line's scanned code against its item's
// ERP barcodes. No scanned code, an unknown item, or a code that is not that
// item's is a mismatch. Returns the lines and how many match.
func CheckOfflineSaleLines(tenantID string, items []OfflineSaleItem) ([]OfflineSaleLine, int, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, 0, err
	}
	lines := make([]OfflineSaleLine, 0, len(items))
	matched := 0
	for _, it := range items {
		line := OfflineSaleLine{Sku: it.Sku, Qty: it.Qty, Scanned: strings.TrimSpace(it.Scanned), ERPBarcodes: []string{}}
		item, errItem := ResolveItemBySKU(tenantID, it.Sku)
		switch {
		case errors.Is(errItem, ErrItemNotFound):
		case errItem != nil:
			return nil, 0, errItem
		default:
			line.Name, _ = item.Data["name"].(string)
			codes, errCodes := itemERPBarcodes(schema, item)
			if errCodes != nil {
				return nil, 0, errCodes
			}
			line.ERPBarcodes = codes
			for _, c := range codes {
				if line.Scanned != "" && strings.EqualFold(line.Scanned, c) {
					line.Match = true
					break
				}
			}
		}
		if line.Match {
			matched++
		}
		lines = append(lines, line)
	}
	return lines, matched, nil
}

// HoldOfflineSale stores an offline sale as Pending. The same key again
// returns the row it made the first time (created false) - a queue replayed
// twice makes one row; the same key with a different sale is refused.
func HoldOfflineSale(tenantID string, h OfflineSaleHold) (*OfflineSale, bool, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, false, err
	}
	if existing, errEx := offlineSaleByKey(schema, h.IdempotencyKey); errEx == nil {
		return heldAgain(existing, schema, h)
	} else if errEx != sql.ErrNoRows {
		return nil, false, errEx
	}
	_, matched, err := CheckOfflineSaleLines(tenantID, h.Items)
	if err != nil {
		return nil, false, err
	}
	var soldAt interface{}
	if h.SoldAt != nil {
		soldAt = *h.SoldAt
	}
	var id string
	err = db.DB.QueryRow(fmt.Sprintf(`
		INSERT INTO %s.pos_offline_sales (id, cart_number, idempotency_key, payload, payload_digest, cashier_user_id,
			cashier, cashier_role, location, pos_session, store_device, sold_at, total, lines_total, lines_matched)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		ON CONFLICT (idempotency_key) DO NOTHING
		RETURNING id`, schema),
		NewDocID("OFS"), h.CartNumber, h.IdempotencyKey, h.Payload, h.Digest, h.CashierUserID, h.Cashier, h.CashierRole,
		h.Location, h.POSSession, h.StoreDevice, soldAt, round2(h.Total), len(h.Items), matched).Scan(&id)
	if err == sql.ErrNoRows {
		// Lost a race with the same key: the other request's row is the sale.
		existing, errEx := offlineSaleByKey(schema, h.IdempotencyKey)
		if errEx != nil {
			return nil, false, errEx
		}
		return heldAgain(existing, schema, h)
	}
	if err != nil {
		return nil, false, err
	}
	sale, err := scanOfflineSale(db.DB.QueryRow(fmt.Sprintf(`SELECT %s FROM %s.pos_offline_sales WHERE id = $1`, offlineSaleColumns, schema), id))
	return sale, true, err
}

func offlineSaleByKey(schema, key string) (*OfflineSale, error) {
	return scanOfflineSale(db.DB.QueryRow(fmt.Sprintf(`SELECT %s FROM %s.pos_offline_sales WHERE idempotency_key = $1`, offlineSaleColumns, schema), key))
}

func heldAgain(existing *OfflineSale, schema string, h OfflineSaleHold) (*OfflineSale, bool, error) {
	var digest string
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT payload_digest FROM %s.pos_offline_sales WHERE id = $1`, schema), existing.ID).Scan(&digest)
	if digest != "" && h.Digest != "" && digest != h.Digest {
		return nil, false, ErrOfflineSaleKeyReused
	}
	return existing, false, nil
}

// ListOfflineSales is the screen's list, newest first; status "" is all.
func ListOfflineSales(tenantID, status string, limit int) ([]OfflineSale, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT %s FROM %s.pos_offline_sales
		WHERE ($1 = '' OR status = $1) ORDER BY received_at DESC LIMIT %d`, offlineSaleColumns, schema, limit), status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []OfflineSale{}
	for rows.Next() {
		s, err := scanOfflineSale(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *s)
	}
	return out, rows.Err()
}

// GetOfflineSale is one held sale with its lines checked now.
func GetOfflineSale(tenantID, id string) (*OfflineSale, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	s, err := scanOfflineSale(db.DB.QueryRow(fmt.Sprintf(`SELECT %s FROM %s.pos_offline_sales WHERE id = $1`, offlineSaleColumns, schema), id))
	if err == sql.ErrNoRows {
		return nil, ErrOfflineSaleNotFound
	}
	if err != nil {
		return nil, err
	}
	lines, matched, err := CheckOfflineSaleLines(tenantID, s.Items())
	if err != nil {
		return nil, err
	}
	s.Lines, s.LinesMatched = lines, matched
	return s, nil
}

// OfflineSaleDecision is a manager's Validate or Reject.
type OfflineSaleDecision struct {
	ID          string
	ActorUserID string
	Actor       string // username
	Validate    bool
	Reason      string
}

// OfflineSalePost posts a validated sale through the checkout and returns the
// bill (or document) number and the checkout's response.
type OfflineSalePost func(sale *OfflineSale) (billNumber string, result []byte, err error)

func offlineDecisionError(msg string) error {
	return &ValidationError{Code: "POSOFF-1362", Message: msg}
}

// DecideOfflineSale validates or rejects one Pending offline sale. The row is
// locked for the whole decision, so a second click waits and then finds it
// decided; the sale posts (post) only while that lock is held.
func DecideOfflineSale(ctx context.Context, tenantID string, d OfflineSaleDecision, post OfflineSalePost) (*OfflineSale, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	tx, err := db.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	sale, err := scanOfflineSale(tx.QueryRow(fmt.Sprintf(`SELECT %s FROM %s.pos_offline_sales WHERE id = $1 FOR UPDATE`, offlineSaleColumns, schema), d.ID))
	if err == sql.ErrNoRows {
		return nil, ErrOfflineSaleNotFound
	}
	if err != nil {
		return nil, err
	}
	if sale.Status != OfflineSalePending {
		return nil, offlineDecisionError(fmt.Sprintf("offline sale %s is already %s", sale.CartNumber, strings.ToLower(sale.Status)))
	}
	if (d.ActorUserID != "" && d.ActorUserID == sale.CashierUserID) || strings.EqualFold(strings.TrimSpace(d.Actor), sale.Cashier) {
		return nil, offlineDecisionError("you rang this sale up - another manager validates or rejects it")
	}
	summary := fmt.Sprintf("offline sale %s (cart %s, cashier %s, store %s, total %.2f)", sale.ID, sale.CartNumber, sale.Cashier, sale.Location, sale.Total)

	if !d.Validate {
		reason := strings.TrimSpace(d.Reason)
		if reason == "" {
			return nil, offlineDecisionError("say why the sale is rejected")
		}
		if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.pos_offline_sales SET status = $2, decided_by = $3, decided_at = NOW(), reject_reason = $4 WHERE id = $1`, schema),
			sale.ID, OfflineSaleRejected, d.Actor, reason); err != nil {
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		LogAuditEventCtx(ctx, tenantID, d.Actor, "POS_OFFLINE_SALE_REJECTED", "SUCCESS", fmt.Sprintf("Rejected %s - not posted: %s", summary, reason))
		return GetOfflineSale(tenantID, sale.ID)
	}

	lines, matched, err := CheckOfflineSaleLines(tenantID, sale.Items())
	if err != nil {
		return nil, err
	}
	if len(lines) == 0 || matched < len(lines) {
		var bad []string
		for _, l := range lines {
			switch {
			case l.Match:
			case l.Scanned == "":
				bad = append(bad, l.Sku+" not scanned")
			default:
				bad = append(bad, fmt.Sprintf("%s scanned %s, not its barcode", l.Sku, l.Scanned))
			}
		}
		return nil, &ValidationError{Code: "POSOFF-1361", Message: fmt.Sprintf("%d of %d lines match: %s", matched, len(lines), strings.Join(bad, "; "))}
	}
	bill, result, err := post(sale)
	if err != nil {
		return nil, err
	}
	if len(result) == 0 || !json.Valid(result) {
		result = []byte("null")
	}
	// 49.5.6d: a sale that posted goes on the till's sale day. One waiting on
	// Approvals (over the discount limit) has not posted yet, and posts on the
	// day it is approved, as any such sale does.
	postedOn := ""
	var checkout struct {
		Status string `json:"status"`
	}
	_ = json.Unmarshal(result, &checkout)
	if checkout.Status == "completed" {
		day, closedDay, err := offlineSalePostingDay(tx, schema, sale.SoldAt)
		if err != nil {
			return nil, err
		}
		if day < BusinessToday() {
			if err := dateOfflineSaleTx(tx, schema, sale.CartNumber, *sale.SoldAt); err != nil {
				return nil, err
			}
		}
		postedOn = " on " + day
		if closedDay != "" {
			postedOn += fmt.Sprintf(" (rung up %s, in a closed period)", closedDay)
		}
	}
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.pos_offline_sales SET status = $2, decided_by = $3, decided_at = NOW(), bill_number = $4,
		result = $5, lines_matched = $6 WHERE id = $1`, schema), sale.ID, OfflineSaleValidated, d.Actor, bill, result, matched); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	LogAuditEventCtx(ctx, tenantID, d.Actor, "POS_OFFLINE_SALE_VALIDATED", "SUCCESS", fmt.Sprintf("Validated %s - %d/%d lines match, posted as %s%s", summary, matched, len(lines), bill, postedOn))
	return GetOfflineSale(tenantID, sale.ID)
}

// offlineSalePostingDay (49.5.6d, user 2026-10-29: "all recommended") is the
// day a validated offline sale posts on: the day the till rang it up. It is
// today when the till sent no time or a time from today on, and today when
// the till's day is inside a Closed accounting period, which takes no new
// postings (rejectIfCurrentPeriodClosed) - closedDay then names that day.
func offlineSalePostingDay(tx *sql.Tx, schema string, soldAt *time.Time) (day, closedDay string, err error) {
	today := BusinessToday()
	if soldAt == nil {
		return today, "", nil
	}
	tillDay := soldAt.In(BusinessLocation()).Format("2006-01-02")
	if tillDay >= today {
		return today, "", nil
	}
	var closed bool
	if err := tx.QueryRow(fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM %s.accounting_periods
		WHERE status = 'Closed' AND $1::date BETWEEN start_date AND end_date)`, schema), tillDay).Scan(&closed); err != nil {
		return "", "", err
	}
	if closed {
		return today, tillDay, nil
	}
	return tillDay, "", nil
}

// dateOfflineSaleTx puts a posted offline sale at the till's sale time: its
// GL rows, its stock ledger lines and the bill, whose created_at is what day
// and period reports read - as a backdated journal voucher's rows take the
// voucher's date (journal_voucher.go, 73.D.16). The bill number still comes
// from the validation day's series.
func dateOfflineSaleTx(tx *sql.Tx, schema, cartNumber string, soldAt time.Time) error {
	at := soldAt.UTC().Format(time.RFC3339Nano)
	for _, q := range []string{
		`UPDATE %s.gl_postings SET created_at = $2::timestamptz WHERE document_type = 'POSCart' AND document_id = $1`,
		`UPDATE %s.documents SET created_at = $2::timestamptz WHERE doctype = 'StockLedgerEntry'
			AND data->>'voucher_id' = $1 AND data->>'voucher_type' IN ('POSInvoice', 'POSConsumption')`,
		`UPDATE %s.documents SET created_at = $2::timestamptz WHERE doctype = 'POSCart' AND id = $1`,
	} {
		if _, err := tx.Exec(fmt.Sprintf(q, schema), cartNumber, at); err != nil {
			return fmt.Errorf("offline sale %s posted but not dated to the till's day: %w", cartNumber, err)
		}
	}
	return nil
}

// sessionHeldOfflineCash (49.5.6d) is the cash a session's drawer took for
// offline sales still waiting on validation. They have not posted, so they
// are not in the session's cash sales; the X-report and the till close show
// them as their own line and hold the drawer to them. Validated, a sale posts
// into the same session and moves to cash sales; rejected, it leaves both.
func sessionHeldOfflineCash(schema, sessionID string) (float64, error) {
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT payload, total FROM %s.pos_offline_sales WHERE pos_session = $1 AND status = $2`, schema),
		sessionID, OfflineSalePending)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	total := 0.0
	for rows.Next() {
		var payload []byte
		var saleTotal float64
		if err := rows.Scan(&payload, &saleTotal); err != nil {
			return 0, err
		}
		var p struct {
			PaymentMode string      `json:"payment_mode"`
			Tenders     []POSTender `json:"tenders"`
		}
		_ = json.Unmarshal(payload, &p)
		if len(p.Tenders) > 0 {
			for _, t := range p.Tenders {
				if strings.EqualFold(t.Mode, "Cash") {
					total += t.Amount
				}
			}
			continue
		}
		if p.PaymentMode == "" || strings.EqualFold(p.PaymentMode, "Cash") {
			total += saleTotal
		}
	}
	return round2(total), rows.Err()
}

// offlineSaleHeld reports whether a cart number arrived as a held offline
// sale - for the queue-gap check, which must not call it missing. False when
// the table is not there yet.
func offlineSaleHeld(schema, cartNumber string) bool {
	var held bool
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM %s.pos_offline_sales WHERE cart_number = $1)`, schema), cartNumber).Scan(&held); err != nil {
		return false
	}
	return held
}
