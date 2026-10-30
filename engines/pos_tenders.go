package engines

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Stage 73.1.6 - split tender at the till, and refunds back to the tender a
// sale was paid with.
//
// A sale used to carry one payment_mode and nothing else: every rupee posted
// to that mode's clearing account, and every refund - even one labelled
// "Store Credit" - credited cash (1100). Now a sale may carry several money
// tenders. The cashier enters the fixed parts (Card 1,200, UPI 300) and the
// cart's payment_mode takes whatever is left, so the parts always add up to
// the bill. Points and gift cards stay their own tenders, as before.
//
// Each tender is posted to its own clearing account, and the sale's allocation
// (engines/sale_allocation.go) splits every tender across the bill's lines, so
// a return refunds each part back the way it was paid: cash to the drawer,
// card and UPI to their clearing accounts (the cashier reverses the card on
// the terminal), gift-card value back onto the card, points back as points.

// POSTender is one fixed money part of a split payment.
type POSTender struct {
	Mode   string  `json:"mode"`
	Amount float64 `json:"amount"`
	// Name (Stage 85.E2) is the store's own tender ("HDFC Card") of kind
	// Mode; Reference is its card slip / UTR when the tender asks for one.
	Name      string `json:"name,omitempty"`
	Reference string `json:"reference,omitempty"`
	// SecondPayment (49.5.4) is the cashier saying this tender repeats one
	// taken minutes ago on purpose - the same card slip / UTR for a second
	// sale - so the duplicate-tender refusal (pos_duplicate_tender.go) lets
	// it through.
	SecondPayment bool `json:"second_payment,omitempty"`
}

// tenderPart is one tender of a settled sale, in paise.
type tenderPart struct {
	Key   string
	Paise int64
}

// posMoneyModes are the payment modes a tender may name; each has a clearing
// account (paymentModeClearingAccount).
var posMoneyModes = []string{"Cash", "Card", "UPI"}

func isPOSMoneyMode(mode string) bool {
	for _, m := range posMoneyModes {
		if m == mode {
			return true
		}
	}
	return false
}

// settlePOSTenders turns a cart's fixed tenders and its payment mode into the
// money tenders that pay moneyDuePaise - the bill after points and gift cards.
// The payment mode takes the rest. Same modes are merged; the order is the
// fixed parts as entered, then the rest. The fixed parts must be Cash, Card
// or UPI; the rest keeps whatever payment_mode the cart always carried (an
// unrecognised one settles to cash clearing, as it always has).
func settlePOSTenders(fixed []POSTender, paymentMode string, moneyDuePaise int64) ([]tenderPart, error) {
	if paymentMode == "" {
		paymentMode = "Cash"
	}
	var parts []tenderPart
	index := map[string]int{}
	add := func(mode string, paise int64) {
		if i, ok := index[mode]; ok {
			parts[i].Paise += paise
			return
		}
		index[mode] = len(parts)
		parts = append(parts, tenderPart{Key: mode, Paise: paise})
	}
	var fixedPaise int64
	for _, t := range fixed {
		mode := strings.TrimSpace(t.Mode)
		if !isPOSMoneyMode(mode) {
			return nil, &ValidationError{Code: "POSOFF-0605", Message: fmt.Sprintf("%q is not a payment mode the till takes (Cash, Card or UPI)", t.Mode)}
		}
		p := RupeesToPaise(t.Amount)
		if p <= 0 {
			continue
		}
		fixedPaise += p
		add(mode, p)
	}
	if fixedPaise > moneyDuePaise {
		return nil, &ValidationError{Code: "POSOFF-0604", Message: fmt.Sprintf(
			"the split payment adds up to %.2f, more than the %.2f the bill comes to - lower one of the parts", PaiseToRupees(fixedPaise), PaiseToRupees(moneyDuePaise))}
	}
	if rest := moneyDuePaise - fixedPaise; rest > 0 {
		add(paymentMode, rest)
	}
	return parts, nil
}

// ValidatePOSTenders is the till's check before a sale is claimed: the split
// must name known modes and must not exceed what is due in money.
func ValidatePOSTenders(fixed []POSTender, paymentMode string, moneyDue float64) error {
	_, err := settlePOSTenders(fixed, paymentMode, RupeesToPaise(moneyDue))
	return err
}

// postTenderReclassTx moves each part paid other than by the primary mode out
// of the primary mode's clearing account, which PostSalesFinanceBookingTx
// debited with the whole money-paid bill. The sale then shows each tender in
// its own account; the gift card leg (redeemGiftCardTendersTx) still settles
// against the primary account, exactly as before.
func postTenderReclassTx(tx *sql.Tx, tenantID, schema, cartNumber, primary string, parts []tenderPart) error {
	from := paymentModeClearingAccount(primary)
	for _, p := range parts {
		to := paymentModeClearingAccount(p.Key)
		if p.Paise <= 0 || to == from {
			continue
		}
		if err := PostDoubleEntryTx(tx, tenantID, schema, "POSCart", cartNumber,
			map[string]int64{to: p.Paise}, map[string]int64{from: p.Paise},
			"", fmt.Sprintf("POSCart:%s:TENDER:%s", cartNumber, p.Key)); err != nil {
			return fmt.Errorf("split tender posting failed: %v", err)
		}
	}
	return nil
}

// refundCreditsByTender is where a refund's money goes, by the tenders the
// returned units were paid with: each money mode to its clearing account, the
// gift-card share to the card's liability account (the card itself is
// credited by refundGiftCardShareTx). Points are handled by the caller.
func refundCreditsByTender(tenders map[string]int64, liabilityAccount string) map[string]int64 {
	credits := map[string]int64{}
	for key, paise := range tenders {
		if paise <= 0 || key == TenderLoyaltyPoints {
			continue
		}
		if key == TenderGiftCard {
			credits[liabilityAccount] += paise
			continue
		}
		credits[paymentModeClearingAccount(key)] += paise
	}
	return credits
}

// saleGiftCardTenders is the gift cards a sale was paid with.
func saleGiftCardTenders(tx *sql.Tx, schema, cartID string) ([]GiftCardTender, error) {
	var raw sql.NullString
	if err := tx.QueryRow(fmt.Sprintf(
		`SELECT data->'gift_card_tenders' FROM %s.documents WHERE doctype = 'POSCart' AND id = $1`, schema), cartID).Scan(&raw); err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	var out []GiftCardTender
	if raw.Valid && raw.String != "" && raw.String != "null" {
		if err := json.Unmarshal([]byte(raw.String), &out); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// paiseMapFrom reads a stored {key: paise} map back from a document.
func paiseMapFrom(v interface{}) map[string]int64 {
	out := map[string]int64{}
	if m, ok := v.(map[string]interface{}); ok {
		for k, x := range m {
			if p := int64(numFromInterface(x)); p != 0 {
				out[k] = p
			}
		}
	}
	return out
}

// SVEventReversal puts value back on a gift card: a returned item that was
// paid for with the card. It restores the balance without counting as new
// funding.
const SVEventReversal = "REVERSAL"

// refundGiftCardShareTx puts the gift-card-paid share of a refund back on the
// card(s) the sale was paid with, split by what each paid. A card that has
// since been blocked or cancelled cannot take it - the refund stops with a
// reason a manager can act on (release the card), rather than turning stored
// value into cash.
func refundGiftCardShareTx(tx *sql.Tx, tenantID, schema, cartNumber, refundRequestID, user string, sharePaise int64, saleTenders []GiftCardTender) (map[string]int64, error) {
	// Stage 130.12: the share is returned per liability account - a bill paid
	// by a gift card (2610) and store credit (2650) credits each its own.
	byAccount := map[string]int64{}
	if sharePaise <= 0 || len(saleTenders) == 0 {
		return byAccount, nil
	}
	sorted := append([]GiftCardTender(nil), saleTenders...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].InstrumentID < sorted[j].InstrumentID })
	weights := make([]int64, len(sorted))
	for i, t := range sorted {
		weights[i] = t.AmountPaise
	}
	today := BusinessToday()
	for i, share := range allocatePaise(sharePaise, weights) {
		if share <= 0 {
			continue
		}
		t := sorted[i]
		idem := "refund|" + refundRequestID + "|" + t.InstrumentID
		var done bool
		if err := tx.QueryRow(fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM %s.stored_value_ledger WHERE idempotency_key = $1)`, schema), idem).Scan(&done); err != nil {
			return nil, err
		}
		c, err := scanSVCard(tx.QueryRow(fmt.Sprintf(`SELECT %s FROM %s.stored_value_instruments WHERE instrument_id = $1 FOR UPDATE`, svCardColumns, schema), t.InstrumentID))
		if err != nil {
			return nil, fmt.Errorf("gift card %s: %v", t.Masked, err)
		}
		p, err := LoadStoredValueProfile(tenantID, c.Profile)
		if err != nil {
			return nil, err
		}
		byAccount[p.LiabilityAccount] += share
		if done {
			continue
		}
		if st := c.effectiveStatus(today); st == SVStatusBlocked || st == SVStatusCancelled {
			return nil, &ValidationError{Code: "POSOFF-0606", SubFor: t.Masked, Message: fmt.Sprintf(
				"%.2f of this refund goes back to gift card %s, which is %s - a manager releases the card first", PaiseToRupees(share), t.Masked, strings.ToLower(st))}
		}
		src := svSource{Doctype: "RefundRequest", ID: refundRequestID, User: user, Reason: "returned against " + cartNumber}
		if _, err := appendSVEvent(tx, schema, &c, SVEventReversal, share, "", src, idem); err != nil {
			return nil, err
		}
	}
	return byAccount, nil
}
