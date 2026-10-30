package engines

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// 49.5.4 (a): a duplicate tender at checkout. The same store, the same kind
// of tender and the same reference (card slip, UTR, cheque number) - and for
// a split part the same amount - within the window is the classic double
// ring of one payment: the card was charged once, the bill was made twice.
// Checked at the one place every sale posts (finalizePOSCheckoutTx) against
// the Paid carts of the window. A tender with no reference (cash) is never a
// duplicate, and the cashier can say it is a second payment on purpose
// (POSTender.SecondPayment on a split part, posCart.SecondPayment for the
// Payment Mode part).

// DuplicateTenderCode: the same referenced tender was taken minutes ago.
const DuplicateTenderCode = "POSOFF-1363"

// SettingDuplicateTenderWindow is the window in minutes; 0 switches the
// check off.
const SettingDuplicateTenderWindow = "pos.duplicate_tender_window_minutes"

func init() {
	RegisterSetting(SettingDefinition{
		Key: SettingDuplicateTenderWindow, Module: "Point of Sale",
		Label: "Duplicate tender window", Type: SettingTypeInt, Default: "10", Unit: "minutes (0 = off)",
		Min:         settingBound(0),
		Description: "A card / UPI / cheque tender with the same reference (and, for a split part, the same amount) as one taken at the store within this many minutes is refused at checkout unless the cashier marks it a second payment.",
	})
}

func refuseDuplicateTenderTx(tx *sql.Tx, schema, cartNumber string, cart posCart) error {
	window := GetSettingIntForSchema(schema, SettingDuplicateTenderWindow)
	if window <= 0 {
		return nil
	}
	find := func(where string, args ...interface{}) (string, error) {
		var other string
		err := tx.QueryRow(fmt.Sprintf(`SELECT id FROM %s.documents
			WHERE doctype = 'POSCart' AND status = 'Paid' AND deleted_at IS NULL AND id <> $1
			  AND data->>'location' = $2 AND updated_at > CURRENT_TIMESTAMP - make_interval(mins => $3)
			  AND %s
			ORDER BY updated_at DESC LIMIT 1`, schema, where), append([]interface{}{cartNumber, cart.Location, window}, args...)...).Scan(&other)
		if err == sql.ErrNoRows {
			return "", nil
		}
		return other, err
	}
	refuse := func(what, ref, other string) error {
		return &ValidationError{Code: DuplicateTenderCode, SubFor: "tenders",
			Message: fmt.Sprintf("%s with reference %s was already taken on bill %s in the last %d minutes - if this is a second payment, mark it so", what, ref, other, window)}
	}
	for _, t := range cart.Tenders {
		ref := strings.TrimSpace(t.Reference)
		if ref == "" || t.SecondPayment || t.Amount <= 0 {
			continue
		}
		probe, _ := json.Marshal([]map[string]interface{}{{"mode": t.Mode, "amount": t.Amount, "reference": ref}})
		other, err := find(`data->'tenders' @> $4::jsonb`, string(probe))
		if err != nil {
			return err
		}
		if other != "" {
			return refuse(fmt.Sprintf("%s %.2f", t.Mode, t.Amount), ref, other)
		}
	}
	// The Payment Mode part: its slip / UTR is unique to one transaction, so
	// the same reference on the same kind of tender is a repeat whatever the
	// amount.
	if ref := strings.TrimSpace(cart.PaymentReference); ref != "" && !cart.SecondPayment {
		mode := cart.PaymentMode
		if mode == "" {
			mode = "Cash"
		}
		other, err := find(`data->>'payment_mode' = $4 AND data->>'payment_reference' = $5`, mode, ref)
		if err != nil {
			return err
		}
		if other != "" {
			return refuse(mode, ref, other)
		}
	}
	return nil
}
