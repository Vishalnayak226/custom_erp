package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// Stage 146.2.6 - an approved transfer of usable points from one customer to
// another (decision D12).
//
// Off unless loyalty.points_transfer is on. The source customer confirms with
// a one-time code (the redemption code mechanism, challenge reference = the
// transfer); above the approval value (LoyaltyPointTransfer approval rule) a
// manager other than the requester approves it under Approvals. Only usable
// points move - not pending (return window), cancelled or expired ones - and
// the target gets one ledger row per source lot, with the source lot's expiry
// date, taken earliest-expiring first. A daily per-customer cap limits what
// one customer can send. Nothing is shared: each customer keeps their own
// balance, and the ledger is only ever appended to. Value-neutral, so no GL.

const (
	loyaltyTransferDoctype = "LoyaltyPointTransfer"
	// Ledger reference on both sides of a transfer.
	loyaltyTransferRef = loyaltyTransferDoctype
)

// LoyaltyTransfer is one transfer, as the screen shows it.
type LoyaltyTransfer struct {
	ID           string  `json:"id"`
	FromCustomer string  `json:"from_customer"`
	ToCustomer   string  `json:"to_customer"`
	Points       int     `json:"points"`
	PointsValue  float64 `json:"points_value"`
	Status       string  `json:"status"`
	RequiredRole string  `json:"required_role,omitempty"`
	CreatedAt    string  `json:"created_at,omitempty"`
}

// LoyaltyTransfersEnabled reports whether the tenant has turned transfers on.
func LoyaltyTransfersEnabled(tenantID string) bool {
	return GetSettingBool(tenantID, "loyalty.points_transfer")
}

type loyaltyLot struct {
	points  int
	expires sql.NullTime
}

type ledgerQuerier interface {
	QueryRow(query string, args ...interface{}) *sql.Row
	Query(query string, args ...interface{}) (*sql.Rows, error)
}

// transferableLots is what the customer can send: usable, unexpired points,
// per lot, earliest expiry first (no expiry last). Earlier use of points (any
// non-Earn row) is taken off the front lots first, the same first-expiring-
// first order, so what is left is what is really still there.
func transferableLots(q ledgerQuerier, schema, customerID string) ([]loyaltyLot, error) {
	var consumed int
	if err := q.QueryRow(fmt.Sprintf(`SELECT COALESCE(SUM(points), 0) FROM %s.loyalty_point_ledger
		WHERE customer_id = $1 AND transaction_type <> 'Earn' AND `+crmUsablePointsSQL, schema), customerID).Scan(&consumed); err != nil {
		return nil, err
	}
	rows, err := q.Query(fmt.Sprintf(`SELECT points, expires_at, (expires_at IS NOT NULL AND expires_at <= NOW()) FROM %s.loyalty_point_ledger
		WHERE customer_id = $1 AND transaction_type = 'Earn' AND `+crmUsablePointsSQL+`
		ORDER BY expires_at NULLS LAST, id`, schema), customerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var lots []loyaltyLot
	for rows.Next() {
		var l loyaltyLot
		var expired bool
		if err := rows.Scan(&l.points, &l.expires, &expired); err != nil {
			return nil, err
		}
		take := consumed
		if take > l.points {
			take = l.points
		}
		consumed -= take
		l.points -= take
		if l.points > 0 && !expired {
			lots = append(lots, l)
		}
	}
	return lots, rows.Err()
}

func lotsTotal(lots []loyaltyLot) int {
	n := 0
	for _, l := range lots {
		n += l.points
	}
	return n
}

func loyaltyCustomerExists(schema, customerID string) bool {
	var exists bool
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM %s.documents WHERE doctype = 'Customer' AND (id = $1 OR data->>'code' = $1) AND deleted_at IS NULL)`, schema), customerID).Scan(&exists)
	return exists
}

// transferCheckCap refuses a transfer that takes the customer over the daily
// cap (confirmed transfers sent today - waiting, approved or posted).
func transferCheckCap(tenantID, schema, from, exceptID string, points int, q ledgerQuerier) error {
	limit := GetSettingInt(tenantID, "loyalty.points_transfer_daily_cap")
	if limit <= 0 {
		return nil
	}
	var sent int
	if err := q.QueryRow(fmt.Sprintf(`SELECT COALESCE(SUM((data->>'points')::int), 0) FROM %s.documents
		WHERE doctype = $1 AND data->>'from_customer' = $2 AND id <> $3 AND status IN ('Pending Approval', 'Approved', 'Posted')
		  AND created_at >= public.erp_day_start($4::date)`, schema), loyaltyTransferDoctype, from, exceptID, BusinessToday()).Scan(&sent); err != nil {
		return err
	}
	if sent+points > limit {
		return &ValidationError{Code: "CUSTOM-0623", Message: fmt.Sprintf("%s may send %d point(s) a day; %d already sent today, so %d more is over the limit", from, limit, sent, points)}
	}
	return nil
}

// RequestLoyaltyPointsTransfer starts a transfer and sends the source customer
// a one-time code. Nothing moves until ConfirmLoyaltyPointsTransfer.
func RequestLoyaltyPointsTransfer(tenantID, fromCustomer, toCustomer string, points int, userID string) (*LoyaltyTransfer, error) {
	fromCustomer, toCustomer = strings.TrimSpace(fromCustomer), strings.TrimSpace(toCustomer)
	if !LoyaltyTransfersEnabled(tenantID) {
		return nil, &ValidationError{Code: "CUSTOM-0621", Message: "points transfer is not turned on (Configuration, Loyalty)"}
	}
	if fromCustomer == "" || toCustomer == "" || points <= 0 || fromCustomer == toCustomer {
		return nil, &ValidationError{Code: "CUSTOM-0621", Message: "a transfer needs two different customers and a number of points above zero"}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	for _, c := range []string{fromCustomer, toCustomer} {
		if !loyaltyCustomerExists(schema, c) {
			return nil, &ValidationError{Code: "CUSTOM-0621", Message: fmt.Sprintf("no customer %q", c)}
		}
	}
	lots, err := transferableLots(db.DB, schema, fromCustomer)
	if err != nil {
		return nil, err
	}
	if have := lotsTotal(lots); points > have {
		return nil, &ValidationError{Code: "CUSTOM-0622", Message: fmt.Sprintf("%s has %d point(s) that can be sent (pending, expired and used points cannot) - %d is too many", fromCustomer, have, points)}
	}
	if err := transferCheckCap(tenantID, schema, fromCustomer, "", points, db.DB); err != nil {
		return nil, err
	}
	value := PaiseToRupees(loyaltyPointsPaise(tenantID, points))
	t := &LoyaltyTransfer{ID: NewDocID("LPT"), FromCustomer: fromCustomer, ToCustomer: toCustomer, Points: points, PointsValue: value, Status: "Awaiting OTP"}
	challenge, err := InitiateSecureLoyaltyRedemption(tenantID, fromCustomer, points, t.ID, userID)
	if err != nil {
		return nil, err
	}
	data, _ := json.Marshal(map[string]interface{}{"code": t.ID, "from_customer": fromCustomer, "to_customer": toCustomer, "points": points,
		"points_value": value, "challenge_id": challenge, "status": t.Status})
	if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, $5)`, schema),
		t.ID, loyaltyTransferDoctype, data, t.Status, userID); err != nil {
		return nil, err
	}
	return t, nil
}

// ConfirmLoyaltyPointsTransfer checks the code the source customer read out.
// Below the approval value the transfer posts at once; at or above it, it goes
// to a manager (never the requester).
func ConfirmLoyaltyPointsTransfer(tenantID, transferID, otpCode, userID, role string) (*LoyaltyTransfer, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	var dataStr, status string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT data, status FROM %s.documents WHERE doctype = $1 AND id = $2`, schema), loyaltyTransferDoctype, transferID).Scan(&dataStr, &status); err != nil {
		return nil, &ValidationError{Code: "CUSTOM-0621", Message: fmt.Sprintf("no transfer %s", transferID)}
	}
	if status != "Awaiting OTP" {
		return nil, &ValidationError{Code: "CUSTOM-0621", Message: fmt.Sprintf("transfer %s is %s - it can no longer be confirmed", transferID, status)}
	}
	var data map[string]interface{}
	_ = json.Unmarshal([]byte(dataStr), &data)
	customer, _, ref, _, err := verifyLoyaltyOTPChallenge(tenantID, strField(data, "challenge_id"), strings.TrimSpace(otpCode))
	if err != nil {
		return nil, &ValidationError{Code: "CUSTOM-0624", Message: err.Error()}
	}
	if customer != strField(data, "from_customer") || ref != transferID {
		return nil, &ValidationError{Code: "CUSTOM-0624", Message: "that code was not sent for this transfer"}
	}
	t := &LoyaltyTransfer{ID: transferID, FromCustomer: customer, ToCustomer: strField(data, "to_customer"),
		Points: int(numFromInterface(data["points"])), PointsValue: numFromInterface(data["points_value"])}
	approver, err := RequiredApproverRoleForAmount(tenantID, loyaltyTransferDoctype, t.PointsValue)
	if err != nil {
		return nil, err
	}
	data["status"] = "Draft"
	marshaled, _ := json.Marshal(data)
	if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $1, status = 'Draft', updated_at = CURRENT_TIMESTAMP WHERE id = $2 AND doctype = $3`, schema), marshaled, transferID, loyaltyTransferDoctype); err != nil {
		return nil, err
	}
	if approver != "" {
		if err := SubmitForApproval(tenantID, loyaltyTransferDoctype, transferID, userID, role); err != nil {
			return nil, err
		}
		t.Status, t.RequiredRole = "Pending Approval", approver
		return t, nil
	}
	if err := postLoyaltyPointsTransfer(tenantID, transferID); err != nil {
		return nil, err
	}
	t.Status = "Posted"
	return t, nil
}

// postLoyaltyPointsTransfer moves the points, once: the transfer moves to
// Posted in the same transaction. The usable points and the daily cap are
// checked again under both customers' locks.
func postLoyaltyPointsTransfer(tenantID, transferID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var dataStr, status string
	if err := tx.QueryRow(fmt.Sprintf(`SELECT data, status FROM %s.documents WHERE doctype = $1 AND id = $2 FOR UPDATE`, schema), loyaltyTransferDoctype, transferID).Scan(&dataStr, &status); err != nil {
		return err
	}
	if status == "Posted" {
		return nil
	}
	var data map[string]interface{}
	_ = json.Unmarshal([]byte(dataStr), &data)
	from, to, points := strField(data, "from_customer"), strField(data, "to_customer"), int(numFromInterface(data["points"]))
	// Locks in a fixed order, so two opposite transfers cannot wait on each other.
	first, second := from, to
	if second < first {
		first, second = second, first
	}
	for _, c := range []string{first, second} {
		if _, err := lockedLoyaltyBalanceTx(tx, schema, c); err != nil {
			return err
		}
	}
	lots, err := transferableLots(tx, schema, from)
	if err != nil {
		return err
	}
	if have := lotsTotal(lots); points > have {
		return &ValidationError{Code: "CUSTOM-0622", Message: fmt.Sprintf("%s now has %d point(s) that can be sent - %d is too many", from, have, points)}
	}
	if err := transferCheckCap(tenantID, schema, from, transferID, points, tx); err != nil {
		return err
	}
	if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.loyalty_point_ledger (customer_id, transaction_type, points, reference_doctype, reference_id)
		VALUES ($1, $2, $3, $4, $5)`, schema), from, LoyaltyTxnAdjustment, points, loyaltyTransferRef, transferID); err != nil {
		return err
	}
	need := points
	for _, l := range lots {
		if need == 0 {
			break
		}
		n := l.points
		if n > need {
			n = need
		}
		var exp interface{}
		if l.expires.Valid {
			exp = l.expires.Time
		}
		if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.loyalty_point_ledger (customer_id, transaction_type, points, reference_doctype, reference_id, expires_at)
			VALUES ($1, 'Earn', $2, $3, $4, $5)`, schema), to, n, loyaltyTransferRef, transferID, exp); err != nil {
			return err
		}
		need -= n
	}
	data["status"] = "Posted"
	marshaled, _ := json.Marshal(data)
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $1, status = 'Posted', updated_at = CURRENT_TIMESTAMP WHERE id = $2 AND doctype = $3`, schema), marshaled, transferID, loyaltyTransferDoctype); err != nil {
		return err
	}
	return tx.Commit()
}

// postApprovedLoyaltyPointsTransfer is the approval hook.
func postApprovedLoyaltyPointsTransfer(tenantID, transferID string) {
	if err := postLoyaltyPointsTransfer(tenantID, transferID); err != nil {
		LogSystemError(tenantID, "", "ERROR", "postApprovedLoyaltyPointsTransfer", fmt.Sprintf("transfer %s approved but not posted: %v", transferID, err), "")
	}
}

// ListLoyaltyPointsTransfers lists the transfers a customer sent or received,
// newest first.
func ListLoyaltyPointsTransfers(tenantID, customerID string) ([]LoyaltyTransfer, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, data, status, to_char(created_at, 'YYYY-MM-DD HH24:MI') FROM %s.documents
		WHERE doctype = $1 AND (data->>'from_customer' = $2 OR data->>'to_customer' = $2) ORDER BY created_at DESC LIMIT 200`, schema), loyaltyTransferDoctype, customerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LoyaltyTransfer{}
	for rows.Next() {
		var t LoyaltyTransfer
		var raw []byte
		if err := rows.Scan(&t.ID, &raw, &t.Status, &t.CreatedAt); err != nil {
			return nil, err
		}
		var d map[string]interface{}
		_ = json.Unmarshal(raw, &d)
		t.FromCustomer, t.ToCustomer = strField(d, "from_customer"), strField(d, "to_customer")
		t.Points, t.PointsValue = int(numFromInterface(d["points"])), numFromInterface(d["points_value"])
		out = append(out, t)
	}
	return out, rows.Err()
}
