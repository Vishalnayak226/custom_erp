package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// Stage 47.3.3 - "For payment-provider round trips, define Initiated →
// Authorized → Posted plus Failed/Voided states, provider idempotency,
// inventory-reservation expiry and compensating void/reversal; never hold a DB
// transaction across the network." (audit A-03)
//
// Why this exists as a second path rather than a change to handleCheckout: a
// cash sale has no round trip at all, and forcing every till through a
// two-phase flow to serve the card case would make the common path slower and
// more failure-prone for no gain. So handleCheckout stays one call - it posts
// Initiated → Authorized → Posted internally with nothing between the states,
// which is honest, because for cash nothing genuinely happens between them -
// and this file adds the flow for when a terminal really is involved:
//
//	AuthorizePOSSale  -> the cart exists, stock is RESERVED (not decremented),
//	                     payment_state = Initiated. Returns the amount to
//	                     charge. No transaction is open while the cashier taps
//	                     the card on the terminal.
//	ConfirmPOSSale    -> the provider approved it. payment_state = Authorized,
//	                     then the ordinary atomic finalize posts everything and
//	                     leaves it Posted.
//	VoidPOSSale       -> the provider declined, or the customer walked away.
//	                     The reservation is released and payment_state = Voided
//	                     or Failed. Nothing was posted, so there is nothing to
//	                     reverse in the GL - which is the entire reason the
//	                     posting waits for the authorization instead of racing
//	                     it.
//
// An authorization the cashier never confirms or voids is not a leak: the
// reservation carries a TTL and engines/reservation_sweeper.go's existing
// sweeper reclaims it, which is the "inventory-reservation expiry" the item
// asks for and needed no new mechanism.

// Payment states, in the order a sale moves through them.
const (
	PaymentStateInitiated  = "Initiated"
	PaymentStateAuthorized = "Authorized"
	PaymentStatePosted     = "Posted"
	PaymentStateFailed     = "Failed"
	PaymentStateVoided     = "Voided"
)

// posAuthReservationTTLSeconds is how long an authorization holds stock before
// the sweeper reclaims it. Deliberately short: a card tap that takes longer
// than this has failed in every practical sense, and holding a store's last
// unit for an abandoned transaction costs a real sale.
const posAuthReservationTTLSeconds = 300

// paymentStateTransitions is the whole legal state machine, in one place, so a
// transition cannot be permitted by one call site and refused by another. An
// absent key means the state is terminal.
var paymentStateTransitions = map[string][]string{
	PaymentStateInitiated:  {PaymentStateAuthorized, PaymentStateFailed, PaymentStateVoided},
	PaymentStateAuthorized: {PaymentStatePosted, PaymentStateVoided, PaymentStateFailed},
	// Posted, Failed and Voided are terminal. A posted sale is reversed by a
	// RETURN (Stage 47.4), never by rewinding its payment state - the money
	// and the stock have both moved by then, and pretending otherwise is how
	// a ledger stops reconciling.
}

// CanTransitionPaymentState reports whether from -> to is legal.
func CanTransitionPaymentState(from, to string) bool {
	for _, allowed := range paymentStateTransitions[from] {
		if allowed == to {
			return true
		}
	}
	return false
}

// POSAuthorization is what AuthorizePOSSale produced.
type POSAuthorization struct {
	CartNumber     string   `json:"cart_number"`
	PaymentState   string   `json:"payment_state"`
	AmountDue      float64  `json:"amount_due"`
	Currency       string   `json:"currency"`
	QuoteVersion   string   `json:"quote_version"`
	ReservationIDs []string `json:"reservation_ids"`
}

// AuthorizePOSSale reserves the stock for an already-stored cart and puts it in
// Initiated. The cart itself is written by the caller (handleCheckout's own
// claim path), so this function does exactly one thing: hold the goods and
// stamp the state.
//
// Reservations are created one SKU at a time in sorted order, the same
// deterministic order 47.3.4 applies to the decrement, so an authorization and
// a sale racing on the same SKUs cannot deadlock against each other.
func AuthorizePOSSale(tenantID, cartNumber string) (*POSAuthorization, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	var dataStr string
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT data FROM %s.documents WHERE doctype = 'POSCart' AND id = $1`, schema), cartNumber).Scan(&dataStr); err != nil {
		return nil, fmt.Errorf("cart not found: %v", err)
	}
	var cart posCart
	if err := json.Unmarshal([]byte(dataStr), &cart); err != nil {
		return nil, err
	}

	auth := &POSAuthorization{CartNumber: cartNumber, PaymentState: PaymentStateInitiated}
	lines := append([]struct {
		Sku       string  `json:"sku"`
		Qty       int     `json:"qty"`
		SalePrice float64 `json:"sale_price"`
	}(nil), cart.Items...)
	sortPOSLinesBySKU(lines)
	for _, item := range lines {
		resID, resErr := CreateReservation(tenantID, item.Sku, cart.Location, item.Qty, "POSAuthorization", posAuthReservationTTLSeconds)
		if resErr != nil {
			// Roll back the holds already taken - an authorization that could
			// not reserve everything must hold nothing, or a declined card
			// leaves half a cart's stock stranded until the sweeper runs.
			releasePOSReservations(schema, auth.ReservationIDs)
			return nil, &InsufficientStockError{SKU: item.Sku, Location: cart.Location, Requested: item.Qty}
		}
		auth.ReservationIDs = append(auth.ReservationIDs, resID)
		auth.AmountDue += item.SalePrice * float64(item.Qty)
	}
	auth.AmountDue = round2(auth.AmountDue)

	if err := setPaymentState(schema, cartNumber, "", PaymentStateInitiated, "", auth.ReservationIDs); err != nil {
		releasePOSReservations(schema, auth.ReservationIDs)
		return nil, err
	}
	return auth, nil
}

// ConfirmPOSSale moves an Initiated/Authorized cart to Authorized and then
// posts it. The provider's own reference is recorded on the cart before the
// posting runs, so a sale that then fails to post is still traceable back to
// the money that was taken for it - which is exactly the case that has to be
// reconcilable rather than merely logged.
//
// providerReference doubles as the provider idempotency handle the item asks
// for: a second confirm carrying the same reference on an already-Posted cart
// is a replay, not a second charge, and returns the original outcome.
func ConfirmPOSSale(tenantID, cartNumber, providerReference, correlationID string) (*FinalizeOutcome, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	current, existingRef, err := currentPaymentState(schema, cartNumber)
	if err != nil {
		return nil, err
	}
	if current == PaymentStatePosted {
		// Already posted. If it carries the same provider reference this is
		// the same charge being confirmed twice; if it carries a different
		// one, someone is trying to attach a second payment to a paid sale.
		if existingRef != "" && providerReference != "" && existingRef != providerReference {
			return nil, &ValidationError{Code: "POSOFF-0244", SubFor: "",
				Message: fmt.Sprintf("cart %s was already paid under reference %q; reference %q cannot be applied to it", cartNumber, existingRef, providerReference)}
		}
		var dataStr string
		if err := db.DB.QueryRow(fmt.Sprintf(
			`SELECT data FROM %s.documents WHERE doctype = 'POSCart' AND id = $1`, schema), cartNumber).Scan(&dataStr); err != nil {
			return nil, err
		}
		return outcomeFromStoredCart(dataStr)
	}
	if current != PaymentStateInitiated && current != PaymentStateAuthorized {
		return nil, &ValidationError{Code: "GLOBAL-0002", SubFor: "payment_state",
			Message: fmt.Sprintf("cart %s is %s and cannot be confirmed", cartNumber, displayPaymentState(current))}
	}
	if current == PaymentStateInitiated && !CanTransitionPaymentState(current, PaymentStateAuthorized) {
		return nil, &ValidationError{Code: "GLOBAL-0002", SubFor: "payment_state",
			Message: fmt.Sprintf("cart %s cannot move from %s to Authorized", cartNumber, displayPaymentState(current))}
	}
	if err := setPaymentState(schema, cartNumber, current, PaymentStateAuthorized, providerReference, nil); err != nil {
		return nil, err
	}

	// The reservations are released BEFORE the decrement, in the same breath:
	// finalization decrements availability, and leaving the reservation in
	// place would double-count the same units as both reserved and sold.
	releasePOSReservations(schema, reservationIDsForCart(schema, cartNumber))

	outcome, err := finalizePOSCheckoutTx(tenantID, schema, cartNumber, correlationID, nil, "", nil)
	if err != nil {
		// The posting rolled back entirely, so the sale did not happen - but
		// the money did. Failed is the honest state: it is what the
		// reconciliation report (47.3.6) looks for, and what tells an operator
		// that a refund or a retry is owed rather than leaving the cart
		// looking merely unfinished.
		_ = setPaymentState(schema, cartNumber, PaymentStateAuthorized, PaymentStateFailed, providerReference, nil)
		return nil, err
	}
	return outcome, nil
}

// VoidPOSSale releases an authorization that will never be posted. Nothing was
// posted, so there is nothing to reverse - the compensation is releasing the
// stock hold, which is the whole benefit of authorizing before posting.
func VoidPOSSale(tenantID, cartNumber, reason string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	current, _, err := currentPaymentState(schema, cartNumber)
	if err != nil {
		return err
	}
	if !CanTransitionPaymentState(current, PaymentStateVoided) {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "payment_state",
			Message: fmt.Sprintf("cart %s is %s and cannot be voided%s", cartNumber, displayPaymentState(current), postedVoidHint(current))}
	}
	releasePOSReservations(schema, reservationIDsForCart(schema, cartNumber))
	if err := setPaymentState(schema, cartNumber, current, PaymentStateVoided, "", nil); err != nil {
		return err
	}
	if _, err := db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET status = 'Cancelled', updated_at = CURRENT_TIMESTAMP WHERE doctype = 'POSCart' AND id = $1`, schema),
		cartNumber); err != nil {
		return err
	}
	LogAuditEvent(tenantID, "", "POS_SALE_VOIDED", "Voided", fmt.Sprintf("cart %s voided before posting: %s", cartNumber, reason))
	return nil
}

func postedVoidHint(current string) string {
	if current == PaymentStatePosted {
		return " - a posted sale is reversed with a return, not a void"
	}
	return ""
}

// displayPaymentState renders the state for an operator, including the case of
// a cart written before Stage 47.3 that has no state stamped on it at all.
func displayPaymentState(state string) string {
	if state == "" {
		return "not in the payment flow"
	}
	return state
}

func currentPaymentState(schema, cartNumber string) (state, reference string, err error) {
	var s, ref sql.NullString
	err = db.DB.QueryRow(fmt.Sprintf(
		`SELECT data->>'payment_state', data->>'payment_reference' FROM %s.documents
		 WHERE doctype = 'POSCart' AND id = $1`, schema), cartNumber).Scan(&s, &ref)
	if err != nil {
		return "", "", fmt.Errorf("cart not found: %v", err)
	}
	return s.String, ref.String, nil
}

// setPaymentState writes the transition, refusing it if the cart moved on
// underneath us (`expected` non-empty makes the UPDATE conditional, which is
// what stops two confirms racing to post the same authorization twice).
func setPaymentState(schema, cartNumber, expected, next, reference string, reservationIDs []string) error {
	// Built as one nested jsonb_set expression rather than several SET
	// clauses, because Postgres evaluates every SET against the row as it was
	// BEFORE the statement - two `data = jsonb_set(data, ...)` clauses would
	// not compose, the last one would simply win and silently drop the others.
	expr := `jsonb_set(data, '{payment_state}', to_jsonb($2::text))`
	args := []interface{}{cartNumber, next}
	if reference != "" {
		args = append(args, reference)
		expr = fmt.Sprintf(`jsonb_set(%s, '{payment_reference}', to_jsonb($%d::text))`, expr, len(args))
	}
	if len(reservationIDs) > 0 {
		raw, _ := json.Marshal(reservationIDs)
		args = append(args, string(raw))
		expr = fmt.Sprintf(`jsonb_set(%s, '{reservation_ids}', $%d::jsonb)`, expr, len(args))
	}
	query := fmt.Sprintf(`UPDATE %s.documents SET data = %s, updated_at = CURRENT_TIMESTAMP
		WHERE doctype = 'POSCart' AND id = $1`, schema, expr)
	if expected != "" {
		// The conditional UPDATE is the concurrency guard: two confirms racing
		// to post one authorization both read Initiated, but only one of them
		// writes Authorized, and the loser is told so instead of posting a
		// second sale.
		args = append(args, expected)
		query += fmt.Sprintf(` AND COALESCE(data->>'payment_state', '') = $%d`, len(args))
	}
	result, err := db.DB.Exec(query, args...)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows == 0 && expected != "" {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "payment_state",
			Message: fmt.Sprintf("cart %s is no longer %s - another till or terminal changed it first", cartNumber, expected)}
	}
	return nil
}

func reservationIDsForCart(schema, cartNumber string) []string {
	var raw sql.NullString
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT data->>'reservation_ids' FROM %s.documents WHERE doctype = 'POSCart' AND id = $1`, schema),
		cartNumber).Scan(&raw); err != nil || !raw.Valid {
		return nil
	}
	var ids []string
	_ = json.Unmarshal([]byte(raw.String), &ids)
	return ids
}

func releasePOSReservations(schema string, ids []string) {
	for _, id := range ids {
		if strings.TrimSpace(id) == "" {
			continue
		}
		_, _ = db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.inventory_reservation WHERE id = $1::uuid`, schema), id)
	}
}

// sortPOSLinesBySKU keeps the reservation order identical to the decrement
// order (47.3.4). Written out rather than reaching for a generic helper
// because the anonymous struct the stored cart unmarshals into has no name to
// hang a sort.Interface on.
func sortPOSLinesBySKU(lines []struct {
	Sku       string  `json:"sku"`
	Qty       int     `json:"qty"`
	SalePrice float64 `json:"sale_price"`
}) {
	for i := 1; i < len(lines); i++ {
		for j := i; j > 0 && lines[j].Sku < lines[j-1].Sku; j-- {
			lines[j], lines[j-1] = lines[j-1], lines[j]
		}
	}
}
