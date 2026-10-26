package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Stage 101.11 - loyalty and offers depth (the blueprint's M27, M28).
//
// 101.11.1 Pending points (D11): with loyalty.pending_points on, points
// earned on a bill wait for the bill's return window (sales.return_window_days)
// before they can be used. The earn row and any return reversal of it carry
// the same pending_until, so a return inside the window simply cancels them
// out, and nothing has to "release" them: once the time passes they count.
//
// 101.11.2 One-use codes everywhere: a checkout reserves a code (the till
// when the code is entered, the web shop when its checkout starts) and the
// sale captures it. While one checkout holds a single-use code no other can
// take it, and the redemption itself re-checks under the voucher's row lock,
// so two simultaneous uses give one success (M28-A02). A cancelled or fully
// returned bill's code follows crm.voucher_on_cancel.
//
// 101.11.3 Referrals: a customer's own code; the friend's first bill earns
// the referrer crm.referral_reward_points once that bill has passed the
// return window and was not fully returned. A referral of oneself (same
// phone, email or record) is flagged, not rewarded.

// crmPendingUntilSQL is when points earned now become usable, or NULL.
func crmPendingUntil(tenantID string) *time.Time {
	if !GetSettingBool(tenantID, "loyalty.pending_points") {
		return nil
	}
	days := GetSettingInt(tenantID, "sales.return_window_days")
	if days <= 0 {
		return nil
	}
	t := time.Now().AddDate(0, 0, days)
	return &t
}

// crmUsablePointsSQL is the filter for ledger rows that count towards a
// usable balance.
const crmUsablePointsSQL = `(pending_until IS NULL OR pending_until <= NOW()) AND cancelled_at IS NULL`

// --- one-use codes -----------------------------------------------------------------

var crmReservationsReady sync.Map // schema -> bool

func crmHasReservations(q interface {
	QueryRow(string, ...interface{}) *sql.Row
}, schema string) bool {
	if v, ok := crmReservationsReady.Load(schema); ok {
		return v.(bool)
	}
	var ok bool
	_ = q.QueryRow(`SELECT to_regclass($1) IS NOT NULL`, schema+".crm_voucher_reservations").Scan(&ok)
	if ok {
		crmReservationsReady.Store(schema, true)
	}
	return ok
}

// CRMVoucherReservation is a code held for one checkout.
type CRMVoucherReservation struct {
	ID        string    `json:"id"`
	Code      string    `json:"code"`
	Channel   string    `json:"channel"`
	RefID     string    `json:"ref_id"`
	Status    string    `json:"status"`
	ExpiresAt time.Time `json:"expires_at"`
	Discount  string    `json:"discount"`
}

// ReserveCRMVoucher holds a code for one checkout (a till cart or a web-shop
// checkout). Reserving again for the same checkout extends the hold.
func ReserveCRMVoucher(tenantID, code, channel, refID, customerID string) (*CRMVoucherReservation, error) {
	code, refID = strings.TrimSpace(code), strings.TrimSpace(refID)
	if code == "" || refID == "" {
		return nil, crmErr(CRMCodeVoucher, "a code and the checkout it is for are needed")
	}
	if channel != VoucherChannelPOS && channel != VoucherChannelOnline {
		channel = VoucherChannelPOS
	}
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var docID string
	var raw []byte
	if err := tx.QueryRow(fmt.Sprintf(`SELECT id, data FROM %s.documents WHERE doctype = 'Voucher' AND UPPER(data->>'code') = UPPER($1) AND deleted_at IS NULL FOR UPDATE`, schema), code).
		Scan(&docID, &raw); err != nil {
		return nil, crmErr(CRMCodeVoucher, "no voucher %q", code)
	}
	var data map[string]interface{}
	_ = json.Unmarshal(raw, &data)
	v := voucherFromData(data)
	if err := checkVoucherUsable(v, customerID, channel); err != nil {
		return nil, crmErr(CRMCodeVoucher, "%v", err)
	}
	if held, err := crmVoucherHeldElsewhereTx(tx, schema, v, refID); err != nil {
		return nil, err
	} else if held {
		return nil, crmErr(CRMCodeVoucher, "this code is being used at another checkout right now - it can be used once")
	}
	mins := GetSettingInt(tenantID, "crm.voucher_reservation_minutes")
	if mins <= 0 {
		mins = 30
	}
	id := NewDocID("VRS")
	if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.crm_voucher_reservations (id, code, channel, ref_id, customer_id, status, expires_at)
		VALUES ($1, $2, $3, $4, NULLIF($5, ''), 'Reserved', LOCALTIMESTAMP + make_interval(mins => $6))
		ON CONFLICT (code, ref_id) DO UPDATE SET status = 'Reserved', expires_at = EXCLUDED.expires_at, closed_at = NULL`, schema),
		id, strings.ToUpper(v.Code), channel, refID, customerID, mins); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	r := &CRMVoucherReservation{Code: strings.ToUpper(v.Code), Channel: channel, RefID: refID, Status: "Reserved"}
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT id, expires_at FROM %s.crm_voucher_reservations WHERE code = $1 AND ref_id = $2`, schema), r.Code, refID).Scan(&r.ID, &r.ExpiresAt)
	r.Discount = fmt.Sprintf("%d%% off", v.DiscountValue)
	if v.DiscountType == "Flat" {
		r.Discount = fmt.Sprintf("%d off", v.DiscountValue)
	}
	return r, nil
}

// ReleaseCRMVoucher lets go of a hold (the checkout was abandoned).
func ReleaseCRMVoucher(tenantID, code, refID string) error {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return err
	}
	_, err = db.DB.Exec(fmt.Sprintf(`UPDATE %s.crm_voucher_reservations SET status = 'Released', closed_at = LOCALTIMESTAMP
		WHERE UPPER(code) = UPPER($1) AND ref_id = $2 AND status = 'Reserved'`, schema), strings.TrimSpace(code), strings.TrimSpace(refID))
	return err
}

// RedeemCRMVoucherForChannelOrder takes one use of an ERP code that a
// web-shop order carried, capturing the hold its checkout took
// (ReserveCRMVoucher with the checkout token). The shop has already given the
// discount, so a code that was not usable - used up at the till, expired,
// another customer's - comes back as a problem for a person, never dropped.
// A code that is not an ERP voucher (one made in the shop) is not ours.
func RedeemCRMVoucherForChannelOrder(tenantID, code, customerID, checkoutRef, orderID string) (problem string) {
	schema, err := db.GetTenantSchema(tenantID)
	code = strings.TrimSpace(code)
	if err != nil || code == "" {
		return ""
	}
	var n int
	if db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.documents WHERE doctype = 'Voucher' AND UPPER(data->>'code') = UPPER($1)`, schema), code).Scan(&n) != nil || n == 0 {
		return ""
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return "the voucher code " + strings.ToUpper(code) + " could not be checked: " + err.Error()
	}
	defer tx.Rollback()
	if checkoutRef != "" {
		if err := crmCaptureVoucherTx(tx, schema, code, checkoutRef); err != nil {
			return "the voucher code " + strings.ToUpper(code) + " could not be checked: " + err.Error()
		}
	}
	v, err := redeemVoucherTx(tx, schema, code, customerID, orderID, func(v Voucher) error { return checkVoucherUsable(v, customerID, VoucherChannelOnline) })
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		return fmt.Sprintf("the web shop gave a discount for voucher code %s, but %v - recover it or accept the loss", strings.ToUpper(code), err)
	}
	LogAuditEvent(tenantID, customerID, "REDEEM_VOUCHER", "SUCCESS", fmt.Sprintf("Voucher %s used on web-shop order %s (use %d/%s)", v.Code, orderID, v.UsedCount, voucherMaxUsesLabel(v.MaxUses)))
	return ""
}

// crmVoucherHeldElsewhereTx reports whether other checkouts' live holds
// already take every use the voucher has left.
func crmVoucherHeldElsewhereTx(tx *sql.Tx, schema string, v Voucher, refID string) (bool, error) {
	if v.MaxUses <= 0 || !crmHasReservations(tx, schema) {
		return false, nil
	}
	var others int
	if err := tx.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.crm_voucher_reservations WHERE UPPER(code) = UPPER($1) AND ref_id <> $2
		AND status = 'Reserved' AND expires_at > LOCALTIMESTAMP`, schema), v.Code, refID).Scan(&others); err != nil {
		return false, err
	}
	return v.UsedCount+others >= v.MaxUses, nil
}

// crmCaptureVoucherTx marks this checkout's hold as used, in the redemption's
// transaction.
func crmCaptureVoucherTx(tx *sql.Tx, schema, code, refID string) error {
	if !crmHasReservations(tx, schema) {
		return nil
	}
	_, err := tx.Exec(fmt.Sprintf(`UPDATE %s.crm_voucher_reservations SET status = 'Captured', closed_at = LOCALTIMESTAMP
		WHERE UPPER(code) = UPPER($1) AND ref_id = $2 AND status = 'Reserved'`, schema), code, refID)
	return err
}

// RecordCRMBillReturned tells the CRM a refund was processed on a bill.
func RecordCRMBillReturned(tenantID, cartID, refundID string) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil || cartID == "" {
		return
	}
	var customerID string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(data->>'customer_id', '') FROM %s.documents WHERE id = $1 AND doctype = 'POSCart'`, schema), cartID).Scan(&customerID); err != nil || customerID == "" {
		return
	}
	RecordCRMEventForTenant(tenantID, customerID, CRMEventReturn, refundID, map[string]interface{}{"bill": cartID})
}

// crmBillFullyReturned: every rupee of the bill's lines has been refunded.
func crmBillFullyReturned(schema, cartID string) bool {
	var amount, refunded float64
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT %s FROM %s.documents b WHERE b.id = $1`, crmBillAmountSQL("b"), schema), cartID).Scan(&amount)
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(SUM(%s), 0) FROM %s.documents r WHERE r.doctype = 'ReturnRequest' AND r.data->>'original_order_id' = $1
		AND r.status IN ('Approved', 'Received', 'QC Complete', 'Closed')`, crmNumSQL("r.data->>'total_refund_eligible'"), schema), cartID).Scan(&refunded)
	return amount > 0 && refunded+0.01 >= amount
}

func init() {
	registerCRMEventHandler(CRMEventReturn, func(schema string, ev crmEvent) {
		bill := crmString(ev.Payload["bill"])
		if bill == "" || !crmBillFullyReturned(schema, bill) {
			return
		}
		// 101.11.2: the bill's single-use code, per the client's rule.
		if GetSettingStringForSchema(schema, "crm.voucher_on_cancel") == "reinstate" {
			_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || jsonb_build_object('used_count', GREATEST(COALESCE((data->>'used_count')::int, 1) - 1, 0),
				'status', 'Active', 'reinstated_from', $1::text), status = 'Active', updated_at = CURRENT_TIMESTAMP
				WHERE doctype = 'Voucher' AND COALESCE(data->>'last_used_on', '') = $1 AND COALESCE((data->>'max_uses')::int, 0) = 1
				  AND COALESCE(data->>'expiry_date', '9999-12-31') >= to_char(public.erp_today(), 'YYYY-MM-DD')`, schema), bill)
		}
		// 101.11.3: a friend's first bill fully returned earns nothing.
		_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.crm_referrals SET status = 'Cancelled', flag_reason = 'The first bill was returned' WHERE first_bill_id = $1 AND status = 'Waiting'`, schema), bill)
	})
	registerCRMEventHandler(CRMEventBillPaid, crmReferralOnBill)
	registerCRMDaily("referral rewards", crmReleaseReferrals)
}

// --- referrals --------------------------------------------------------------------------

// CRMReferralCode returns (creating once) a customer's referral code.
func CRMReferralCode(tenantID, customerID string) (string, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return "", err
	}
	c, err := crmLoadCustomer(schema, customerID)
	if err != nil {
		return "", crmNotFound("customer", customerID)
	}
	var code string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT code FROM %s.crm_referral_codes WHERE customer_id = $1`, schema), c.ID).Scan(&code); err == nil {
		return code, nil
	}
	for i := 0; i < 5; i++ {
		if code, err = crmCode("R", 7); err != nil {
			return "", err
		}
		res, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.crm_referral_codes (code, customer_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, schema), code, c.ID)
		if err != nil {
			return "", err
		}
		if n, _ := res.RowsAffected(); n == 1 {
			return code, nil
		}
		if err := db.DB.QueryRow(fmt.Sprintf(`SELECT code FROM %s.crm_referral_codes WHERE customer_id = $1`, schema), c.ID).Scan(&code); err == nil {
			return code, nil
		}
	}
	return "", crmErr(CRMCodeReferral, "could not make a referral code - try again")
}

// CRMReferral is one referral.
type CRMReferral struct {
	ID           string     `json:"id"`
	Code         string     `json:"code"`
	ReferrerID   string     `json:"referrer_id"`
	ReferrerName string     `json:"referrer_name"`
	FriendID     string     `json:"friend_id"`
	FriendName   string     `json:"friend_name"`
	Status       string     `json:"status"`
	FirstBillID  string     `json:"first_bill_id,omitempty"`
	QualifiesOn  string     `json:"qualifies_on,omitempty"`
	RewardPoints int        `json:"reward_points"`
	FlagReason   string     `json:"flag_reason,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	ReleasedAt   *time.Time `json:"released_at,omitempty"`
}

// RecordCRMReferral records that a (new) customer was referred by a code.
func RecordCRMReferral(tenantID, friendID, code, actor string) (*CRMReferral, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	code = strings.ToUpper(strings.TrimSpace(code))
	var referrer string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT customer_id FROM %s.crm_referral_codes WHERE code = $1`, schema), code).Scan(&referrer); err != nil {
		return nil, crmErr(CRMCodeReferral, "no referral code %q", code)
	}
	friend, err := crmLoadCustomer(schema, friendID)
	if err != nil {
		return nil, crmNotFound("customer", friendID)
	}
	ref, _ := crmLoadCustomer(schema, referrer)
	status, flag := "Waiting", ""
	switch {
	case friend.ID == ref.ID:
		status, flag = "Flagged", "A customer cannot refer themselves"
	case strField(friend.Data, "email") != "" && strings.EqualFold(strField(friend.Data, "email"), strField(ref.Data, "email")):
		status, flag = "Flagged", "Same email as the referrer"
	case strField(friend.Data, "address") != "" && strings.EqualFold(strings.TrimSpace(strField(friend.Data, "address")), strings.TrimSpace(strField(ref.Data, "address"))):
		status, flag = "Flagged", "Same address as the referrer - check before rewarding"
	}
	var bills int
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.documents WHERE doctype = 'POSCart' AND status = 'Paid' AND data->>'customer_id' = $1`, schema), friend.ID).Scan(&bills)
	if bills > 1 && status == "Waiting" {
		return nil, crmErr(CRMCodeReferral, "%s already shops here - a referral is for a new customer", friend.Name())
	}
	id := NewDocID("REF")
	res, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.crm_referrals (id, code, referrer_id, friend_id, status, reward_points, flag_reason, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, ''), $8) ON CONFLICT (friend_id) DO NOTHING`, schema),
		id, code, ref.ID, friend.ID, status, GetSettingInt(tenantID, "crm.referral_reward_points"), flag, actor)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, crmErr(CRMCodeReferral, "%s was already referred", friend.Name())
	}
	if bills == 1 {
		// already made the first bill (the referral was keyed in at the till)
		var bill string
		if err := db.DB.QueryRow(fmt.Sprintf(`SELECT id FROM %s.documents WHERE doctype = 'POSCart' AND status = 'Paid' AND data->>'customer_id' = $1 LIMIT 1`, schema), friend.ID).Scan(&bill); err == nil {
			crmReferralOnBill(schema, crmEvent{CustomerID: friend.ID, RefID: bill})
		}
	}
	list, err := ListCRMReferrals(tenantID, "")
	if err != nil {
		return nil, err
	}
	for i := range list {
		if list[i].ID == id {
			return &list[i], nil
		}
	}
	return nil, crmNotFound("referral", id)
}

// crmReferralOnBill notes the friend's first bill and when it qualifies.
func crmReferralOnBill(schema string, ev crmEvent) {
	tid, _ := tenantIDForSchema(schema)
	days := GetSettingInt(tid, "sales.return_window_days")
	_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.crm_referrals SET first_bill_id = $2, qualifies_on = public.erp_today() + $3::int
		WHERE friend_id = $1 AND status = 'Waiting' AND first_bill_id IS NULL`, schema), ev.CustomerID, ev.RefID, days)
}

// crmReleaseReferrals rewards referrals whose first bill passed the return
// window, once each (the reward's ledger reference is the referral id).
func crmReleaseReferrals(schema string) {
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, referrer_id, first_bill_id, reward_points FROM %s.crm_referrals
		WHERE status = 'Waiting' AND first_bill_id IS NOT NULL AND qualifies_on <= public.erp_today()`, schema))
	if err != nil {
		return
	}
	type due struct {
		id, referrer, bill string
		points             int
	}
	var list []due
	for rows.Next() {
		var d due
		if rows.Scan(&d.id, &d.referrer, &d.bill, &d.points) == nil {
			list = append(list, d)
		}
	}
	rows.Close()
	for _, d := range list {
		if crmBillFullyReturned(schema, d.bill) {
			_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.crm_referrals SET status = 'Cancelled', flag_reason = 'The first bill was returned' WHERE id = $1`, schema), d.id)
			continue
		}
		tx, err := db.DB.Begin()
		if err != nil {
			return
		}
		res, err := tx.Exec(fmt.Sprintf(`UPDATE %s.crm_referrals SET status = 'Released', released_at = LOCALTIMESTAMP WHERE id = $1 AND status = 'Waiting'`, schema), d.id)
		if err == nil {
			if n, _ := res.RowsAffected(); n == 1 && d.points > 0 {
				_, err = tx.Exec(fmt.Sprintf(`INSERT INTO %s.loyalty_point_ledger (customer_id, transaction_type, points, reference_doctype, reference_id, expires_at)
					VALUES ($1, 'Earn', $2, 'CRMReferral', $3, NOW() + make_interval(days => $4))`, schema), d.referrer, d.points, d.id, GetSettingIntForSchema(schema, "loyalty.point_expiry_days"))
			}
		}
		if err != nil {
			tx.Rollback()
			continue
		}
		_ = tx.Commit()
	}
}

// ListCRMReferrals lists referrals, optionally for one customer (either side).
func ListCRMReferrals(tenantID, customerID string) ([]CRMReferral, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT r.id, r.code, r.referrer_id, COALESCE(a.data->>'name', ''), r.friend_id, COALESCE(b.data->>'name', ''), r.status,
		COALESCE(r.first_bill_id, ''), COALESCE(to_char(r.qualifies_on, 'YYYY-MM-DD'), ''), r.reward_points, COALESCE(r.flag_reason, ''), r.created_at, r.released_at
		FROM %s.crm_referrals r LEFT JOIN %s.documents a ON a.id = r.referrer_id AND a.doctype = 'Customer' LEFT JOIN %s.documents b ON b.id = r.friend_id AND b.doctype = 'Customer'
		WHERE ($1 = '' OR r.referrer_id = $1 OR r.friend_id = $1) ORDER BY r.created_at DESC LIMIT 300`, schema, schema, schema), customerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CRMReferral{}
	for rows.Next() {
		var r CRMReferral
		var rel sql.NullTime
		if err := rows.Scan(&r.ID, &r.Code, &r.ReferrerID, &r.ReferrerName, &r.FriendID, &r.FriendName, &r.Status, &r.FirstBillID, &r.QualifiesOn, &r.RewardPoints, &r.FlagReason, &r.CreatedAt, &rel); err != nil {
			return nil, err
		}
		if rel.Valid {
			r.ReleasedAt = &rel.Time
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ApproveCRMReferral lets a manager clear a flagged referral (it then waits
// for the first bill like any other) or cancel it.
func ApproveCRMReferral(tenantID, id string, approve bool, actor string) error {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return err
	}
	status := "Cancelled"
	if approve {
		status = "Waiting"
	}
	res, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.crm_referrals SET status = $2, flag_reason = COALESCE(flag_reason, '') || ' - decided by ' || $3 WHERE id = $1 AND status = 'Flagged'`, schema), id, status, actor)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return crmErr(CRMCodeReferral, "only a flagged referral can be decided")
	}
	if approve {
		var friend, bill string
		_ = db.DB.QueryRow(fmt.Sprintf(`SELECT friend_id, COALESCE((SELECT id FROM %s.documents WHERE doctype = 'POSCart' AND status = 'Paid' AND data->>'customer_id' = r.friend_id ORDER BY created_at LIMIT 1), '')
			FROM %s.crm_referrals r WHERE id = $1`, schema, schema), id).Scan(&friend, &bill)
		if bill != "" {
			crmReferralOnBill(schema, crmEvent{CustomerID: friend, RefID: bill})
		}
	}
	return nil
}

var errCRMNoVoucher = errors.New("no voucher")
