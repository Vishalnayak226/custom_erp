package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// Stage 73.2.6 - exchange at the till with net settlement.
//
// An exchange is a return and a sale that settle against each other. The
// returned lines go through the ordinary return - raised, received and
// inspected at the counter in one step - so their tax is reversed and their
// points restored and reversed exactly as for a refund (73.1.1-73.1.3). Only
// the money differs: instead of going back to the tenders the items were
// paid with, the refund's money part becomes an ExchangeCredit, posted to
// 2620 Exchange Credit Clearing. The new sale takes that credit as a tender
// (after points and gift cards, before money), so the customer pays only the
// difference; if the new items cost less, the rest is paid back out of the
// till in the same transaction. The new sale earns points on everything it
// was paid with, the credit included, so a like-for-like swap leaves the
// customer's points as they were.

// RefundMethodExchangeCredit is the refund method that turns a refund's money
// into an ExchangeCredit rather than paying it back.
const RefundMethodExchangeCredit = "Exchange Credit"

// TenderExchangeCredit is the allocation's tender key for it.
const TenderExchangeCredit = "Exchange Credit"

// ExchangeCredit is the value an exchange's returned items carry to its new sale.
type ExchangeCredit struct {
	ID              string  `json:"id"`
	OriginalOrderID string  `json:"original_order_id"`
	ReturnRequestID string  `json:"return_request_id"`
	Location        string  `json:"location"`
	CustomerID      string  `json:"customer_id,omitempty"`
	Amount          float64 `json:"amount"`
	PointsRestored  int     `json:"points_restored,omitempty"`
	Status          string  `json:"status"`
}

// ExchangeReturnRequest is the returned half of an exchange.
type ExchangeReturnRequest struct {
	OriginalOrderID string            `json:"original_order_id"`
	Location        string            `json:"location"`
	Lines           []ReturnItemInput `json:"lines"`
	Dispositions    map[string]string `json:"dispositions"`
	IdempotencyKey  string            `json:"idempotency_key"`
}

// StartTillExchange takes the returned items back at the counter and holds
// their value as an ExchangeCredit for the new sale. Each step is the
// ordinary return's own, in order; a step that fails leaves the return where
// it stopped, visible on the Returns screen, and nothing is credited.
func StartTillExchange(tenantID, cashier string, req ExchangeReturnRequest) (*ExchangeCredit, error) {
	if strings.TrimSpace(req.OriginalOrderID) == "" || strings.TrimSpace(req.Location) == "" || len(req.Lines) == 0 {
		return nil, &ValidationError{Code: "POSOFF-0611", Message: "an exchange needs the original bill, the store and at least one item coming back"}
	}
	key := strings.TrimSpace(req.IdempotencyKey)
	if key == "" {
		key = "exchange:" + req.OriginalOrderID + ":" + NewDocIDCompact("X")
	}
	cmd, err := CreateReturnRequestCommand(tenantID, "Customer Return", req.Location, req.OriginalOrderID, "", cashier, key, "", req.Lines, nil)
	if err != nil {
		return nil, err
	}
	returnID := cmd.ReturnRequestID
	if credit, err := exchangeCreditForReturn(tenantID, returnID); err != nil || credit != nil {
		return credit, err // a replayed request: the credit it already made
	}
	// The counter's own approval: an exchange is one transaction at the till,
	// so the second-person rule (55.5a) does not apply to it.
	if err := approveReturnRequest(tenantID, returnID, cashier, false); err != nil {
		return nil, err
	}
	if err := ReceiveReturnRequest(tenantID, returnID, cashier); err != nil {
		return nil, err
	}
	dispositions := map[string]string{}
	for _, l := range req.Lines {
		d := strings.TrimSpace(req.Dispositions[l.SKU])
		if d == "" {
			d = "Sellable"
		}
		dispositions[l.SKU] = d
	}
	_, refundID, err := ApplyReturnQC(tenantID, returnID, dispositions, cashier)
	if err != nil {
		return nil, err
	}
	if refundID == "" {
		return nil, &ValidationError{Code: "POSOFF-0611", Message: "nothing coming back is refundable (check the condition chosen), so there is no credit to exchange against"}
	}
	if err := approveRefundRequest(tenantID, refundID, cashier, false); err != nil {
		return nil, err
	}
	if err := ProcessRefundRequest(tenantID, refundID, cashier, RefundMethodExchangeCredit); err != nil {
		return nil, err
	}
	credit, err := exchangeCreditForReturn(tenantID, returnID)
	if err == nil && credit == nil {
		err = fmt.Errorf("exchange credit for return %s was not created", returnID)
	}
	return credit, err
}

func exchangeCreditForReturn(tenantID, returnID string) (*ExchangeCredit, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	var id, dataStr, status string
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT id, data, status FROM %s.documents WHERE doctype = 'ExchangeCredit' AND data->>'return_request_id' = $1`, schema), returnID).Scan(&id, &dataStr, &status)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var c ExchangeCredit
	_ = json.Unmarshal([]byte(dataStr), &c)
	c.ID, c.Status = id, status
	return &c, nil
}

// createExchangeCreditTx is ProcessRefundRequest's exchange branch: the
// refund's money part is credited to 2620 (the caller posts it) and recorded
// here as an Open credit for the new sale.
func createExchangeCreditTx(tx *sql.Tx, schema, refundRequestID, returnRequestID, originalOrderID string, amountPaise int64, points int) error {
	var location, customer string
	_ = tx.QueryRow(fmt.Sprintf(`SELECT COALESCE(data->>'return_location', '') FROM %s.documents WHERE doctype = 'ReturnRequest' AND id = $1`, schema), returnRequestID).Scan(&location)
	_ = tx.QueryRow(fmt.Sprintf(`SELECT COALESCE(data->>'customer_id', '') FROM %s.documents WHERE doctype = 'POSCart' AND id = $1`, schema), originalOrderID).Scan(&customer)
	id := "EXC-" + refundRequestID
	data, _ := json.Marshal(map[string]interface{}{
		"code": id, "refund_request_id": refundRequestID, "return_request_id": returnRequestID, "original_order_id": originalOrderID,
		"location": location, "customer_id": customer, "amount": PaiseToRupees(amountPaise), "points_restored": points, "status": "Open",
	})
	_, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'ExchangeCredit', $2, 'Open', 'system')
		ON CONFLICT (id) DO NOTHING`, schema), id, data)
	return err
}

// LoadOpenExchangeCredit is the till's check before a sale uses a credit.
func LoadOpenExchangeCredit(tenantID, creditID, location string) (*ExchangeCredit, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	var dataStr, status string
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT data, status FROM %s.documents WHERE doctype = 'ExchangeCredit' AND id = $1`, schema), creditID).Scan(&dataStr, &status)
	if err == sql.ErrNoRows || status != "Open" {
		return nil, &ValidationError{Code: "POSOFF-0612", Message: "this exchange credit has already been used, or does not exist"}
	}
	if err != nil {
		return nil, err
	}
	var c ExchangeCredit
	_ = json.Unmarshal([]byte(dataStr), &c)
	c.ID, c.Status = creditID, status
	if location != "" && c.Location != location {
		return nil, &ValidationError{Code: "POSOFF-0612", Message: "this exchange credit belongs to another store"}
	}
	return &c, nil
}

// useExchangeCreditTx spends a sale's exchange credit inside the sale's
// transaction: up to owedPaise is the sale's tender (Dr 2620 / Cr the primary
// clearing account, which the sale debited with everything owed); whatever is
// left is paid back from the till in the payment mode (Dr 2620 / Cr that
// clearing account). Returns the tender part and the payout.
func useExchangeCreditTx(tx *sql.Tx, tenantID, schema, cartNumber, creditID, paymentMode, session string, owedPaise int64) (int64, int64, error) {
	var dataStr, status string
	if err := tx.QueryRow(fmt.Sprintf(`SELECT data, status FROM %s.documents WHERE doctype = 'ExchangeCredit' AND id = $1 FOR UPDATE`, schema), creditID).Scan(&dataStr, &status); err != nil {
		return 0, 0, &ValidationError{Code: "POSOFF-0612", Message: "this exchange credit does not exist"}
	}
	var data map[string]interface{}
	_ = json.Unmarshal([]byte(dataStr), &data)
	if status != "Open" {
		if strField(data, "used_on") == cartNumber { // a replay of the same sale
			return RupeesToPaise(numFromInterface(data["used_amount"])), RupeesToPaise(numFromInterface(data["paid_out"])), nil
		}
		return 0, 0, &ValidationError{Code: "POSOFF-0612", Message: "this exchange credit has already been used"}
	}
	credit := RupeesToPaise(numFromInterface(data["amount"]))
	use := credit
	if owedPaise < use {
		use = owedPaise
	}
	if use < 0 {
		use = 0
	}
	payout := credit - use
	primary := paymentModeClearingAccount(paymentMode)
	if use > 0 {
		if err := PostDoubleEntryTx(tx, tenantID, schema, "POSCart", cartNumber, map[string]int64{"2620": use}, map[string]int64{primary: use},
			"", fmt.Sprintf("POSCart:%s:EXCHANGE_CREDIT", cartNumber)); err != nil {
			return 0, 0, err
		}
	}
	if payout > 0 {
		if err := PostDoubleEntryTx(tx, tenantID, schema, "ExchangeCredit", creditID, map[string]int64{"2620": payout}, map[string]int64{primary: payout},
			"", fmt.Sprintf("ExchangeCredit:%s:PAYOUT", creditID)); err != nil {
			return 0, 0, err
		}
	}
	data["status"], data["used_on"], data["used_amount"], data["paid_out"] = "Used", cartNumber, PaiseToRupees(use), PaiseToRupees(payout)
	if session != "" {
		data["pos_session"] = session
	}
	marshaled, _ := json.Marshal(data)
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $1, status = 'Used', updated_at = CURRENT_TIMESTAMP WHERE id = $2`, schema), marshaled, creditID); err != nil {
		return 0, 0, err
	}
	return use, payout, nil
}
