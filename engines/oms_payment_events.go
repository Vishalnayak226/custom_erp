package engines

// Stage 73.4.2.3: payment states on an order. payment_status stays the one
// field the validate chain reads (Confirmed or COD lets an order reserve);
// this adds the moves a paid order goes through after it is taken:
//
//	capture         Pending / Authorized / COD -> Confirmed, and a
//	                PAYMENT_PENDING hold is retried at once
//	refund          part or all of what was paid; all of it -> Refunded
//	chargeback      the bank reverses a payment: chargeback Open
//	chargeback_won  Open -> Won, the payment stands
//	chargeback_lost Open -> Lost, payment_status -> Charged back
//
// Every move is a row in payment_events (type, amount, reference, reason, by,
// at). A move sent twice with the same reference is recorded once. The order
// row is locked while a move is checked and saved, so two refunds at once
// cannot pass more than was paid. Money is not posted to the ledger here: the
// refund itself goes out through the payment provider or a credit note.

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"custom_erp/db"
)

const (
	PaymentCapture        = "capture"
	PaymentRefund         = "refund"
	PaymentChargeback     = "chargeback"
	PaymentChargebackWon  = "chargeback_won"
	PaymentChargebackLost = "chargeback_lost"

	PaymentStatusRefunded    = "Refunded"
	PaymentStatusChargedBack = "Charged back"
)

// OrderPaymentInput is one payment move on an order.
type OrderPaymentInput struct {
	Action    string  `json:"action"`
	Amount    float64 `json:"amount"`
	Reference string  `json:"reference"`
	Reason    string  `json:"reason"`
}

func paymentErr(format string, a ...interface{}) error {
	return &ValidationError{Code: "ORDERM-0470", SubFor: "payment", Message: fmt.Sprintf(format, a...)}
}

// RecordOrderPayment applies one payment move and returns the order's data.
func RecordOrderPayment(tenantID, orderID string, in OrderPaymentInput, userID string) (map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	action := strings.ToLower(strings.TrimSpace(in.Action))
	ref := strings.TrimSpace(in.Reference)
	reason := strings.TrimSpace(in.Reason)
	amount := round2(in.Amount)
	if amount < 0 {
		return nil, paymentErr("amount cannot be negative")
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var raw []byte
	err = tx.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents
		WHERE doctype = 'SalesOrder' AND id = $1 AND deleted_at IS NULL FOR UPDATE`, schema), orderID).Scan(&raw)
	if err == sql.ErrNoRows {
		return nil, &ValidationError{Code: "GLOBAL-0004", Message: fmt.Sprintf("order %s not found", orderID)}
	} else if err != nil {
		return nil, err
	}
	var order map[string]interface{}
	if err := json.Unmarshal(raw, &order); err != nil {
		return nil, err
	}

	events, _ := order["payment_events"].([]interface{})
	if ref != "" {
		for _, e := range events {
			if m, ok := e.(map[string]interface{}); ok && strField(m, "type") == action && strField(m, "reference") == ref {
				return order, nil // the same move sent again
			}
		}
	}

	status := strField(order, "payment_status")
	total := round2(numFromInterface(order["total_amount"]))
	paid := round2(numFromInterface(order["captured_amount"]))
	if paid == 0 && status == "Confirmed" {
		paid = total // paid before this record existed, or taken as Confirmed
	}
	refunded := round2(numFromInterface(order["refunded_amount"]))
	chargeback := strField(order, "chargeback_status")

	switch action {
	case PaymentCapture:
		if strField(order, "order_status") == "Cancelled" {
			return nil, paymentErr("order %s is cancelled", orderID)
		}
		if status == "Confirmed" || status == PaymentStatusRefunded || status == PaymentStatusChargedBack {
			return nil, paymentErr("payment is already %s", status)
		}
		if amount == 0 {
			amount = total
		}
		order["payment_status"] = "Confirmed"
		order["captured_amount"] = amount
	case PaymentRefund:
		if status != "Confirmed" {
			return nil, paymentErr("only a paid order can be refunded (payment is %s)", firstNonEmpty(status, "not recorded"))
		}
		left := round2(paid - refunded)
		if amount <= 0 || amount > left {
			return nil, paymentErr("refund must be more than 0 and at most %.2f", left)
		}
		order["captured_amount"] = paid
		order["refunded_amount"] = round2(refunded + amount)
		if round2(refunded+amount) >= paid {
			order["payment_status"] = PaymentStatusRefunded
		}
	case PaymentChargeback:
		if status != "Confirmed" && status != PaymentStatusRefunded {
			return nil, paymentErr("only a paid order can have a chargeback (payment is %s)", firstNonEmpty(status, "not recorded"))
		}
		if chargeback == "Open" {
			return nil, paymentErr("a chargeback is already open on this order")
		}
		if amount <= 0 || amount > paid {
			return nil, paymentErr("chargeback must be more than 0 and at most %.2f", paid)
		}
		if reason == "" {
			return nil, paymentErr("give the bank's reason for the chargeback")
		}
		order["captured_amount"] = paid
		order["chargeback_status"] = "Open"
		order["chargeback_amount"] = amount
		order["chargeback_reason"] = reason
		order["chargeback_ref"] = ref
	case PaymentChargebackWon, PaymentChargebackLost:
		if chargeback != "Open" {
			return nil, paymentErr("there is no open chargeback on this order")
		}
		amount = round2(numFromInterface(order["chargeback_amount"]))
		if action == PaymentChargebackWon {
			order["chargeback_status"] = "Won"
		} else {
			order["chargeback_status"] = "Lost"
			order["payment_status"] = PaymentStatusChargedBack
		}
	default:
		return nil, paymentErr("unknown payment action %q", in.Action)
	}

	order["payment_events"] = append(events, map[string]interface{}{
		"type": action, "amount": amount, "reference": ref, "reason": reason,
		"by": userID, "at": time.Now().UTC().Format(time.RFC3339),
	})
	out, err := json.Marshal(order)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $1, updated_at = CURRENT_TIMESTAMP
		WHERE doctype = 'SalesOrder' AND id = $2`, schema), out, orderID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	LogAuditEvent(tenantID, userID, "ORDER_PAYMENT_"+strings.ToUpper(action), "SUCCESS",
		fmt.Sprintf("%s: %.2f %s", orderID, amount, ref))

	// A captured payment lets a payment-held order go on at once. If the
	// retry finds another reason to hold it, the order keeps that hold.
	if action == PaymentCapture && strField(order, "order_status") == "On Hold" && strField(order, "hold_reason") == HoldPaymentPending {
		_ = ReleaseOrderHoldAs(tenantID, orderID, userID)
		if _, fresh, err := fetchSalesOrder(tenantID, orderID); err == nil {
			order = fresh
		}
	}
	return order, nil
}
