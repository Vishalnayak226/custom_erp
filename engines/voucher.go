package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Stage 26.7.2: Voucher/coupon issuance + redemption. Voucher is a flat-
// schema Master doctype (db/migrations_stage26_7_crm_loyalty.sql) - create/
// list/edit/bulk-CSV-issuance all come free from the existing generic
// doctype/BulkImportCSV machinery, zero bespoke code needed for those.
// Redemption is the one thing that needs real logic (expiry/max-uses/
// customer-restriction checks, and an atomic used_count increment), mirroring
// the shape of the existing loyalty ledger's RedeemLoyaltyPoints (Stage
// 13.13d).
//
// Stage 73.2.10: issued vouchers work at the till. The coupon box the POS
// already had resolves a code no offer claims as a voucher (one per bill),
// priced in the server's quote like an offer, and the voucher is redeemed
// inside the sale's own transaction (redeemVoucherTx) - so it is spent if and
// only if the sale posts. Vouchers gain an optional start date and channel
// (Any / POS / Online).

// Voucher mirrors the doctype's own JSON shape.
type Voucher struct {
	Code          string `json:"code"`
	DiscountType  string `json:"discount_type"`
	DiscountValue int    `json:"discount_value"`
	ValidFrom     string `json:"valid_from,omitempty"`
	ExpiryDate    string `json:"expiry_date"`
	MaxUses       int    `json:"max_uses"`
	UsedCount     int    `json:"used_count"`
	CustomerID    string `json:"customer_id"`
	Channel       string `json:"channel,omitempty"`
	Status        string `json:"status"`
}

// Voucher channels; blank means Any.
const (
	VoucherChannelPOS    = "POS"
	VoucherChannelOnline = "Online"
)

func voucherFromData(data map[string]interface{}) Voucher {
	return Voucher{
		Code:          stringField(data, "code"),
		DiscountType:  stringField(data, "discount_type"),
		DiscountValue: int(numFromInterface(data["discount_value"])),
		ValidFrom:     stringField(data, "valid_from"),
		ExpiryDate:    stringField(data, "expiry_date"),
		MaxUses:       int(numFromInterface(data["max_uses"])),
		UsedCount:     int(numFromInterface(data["used_count"])),
		CustomerID:    stringField(data, "customer_id"),
		Channel:       stringField(data, "channel"),
		Status:        stringField(data, "status"),
	}
}

func stringField(data map[string]interface{}, key string) string {
	s, _ := data[key].(string)
	return s
}

// checkVoucherUsable is every rule but the amount: status, dates, uses, the
// customer it is restricted to, and (when channel is given) where it may be
// used.
func checkVoucherUsable(v Voucher, customerID, channel string) error {
	today := time.Now().Format("2006-01-02")
	if v.Status != "Active" {
		return fmt.Errorf("voucher %q is not active (status: %s)", v.Code, v.Status)
	}
	if v.ValidFrom != "" && v.ValidFrom > today {
		return fmt.Errorf("voucher %q can be used from %s", v.Code, v.ValidFrom)
	}
	if v.ExpiryDate != "" && v.ExpiryDate < today {
		return fmt.Errorf("voucher %q expired on %s", v.Code, v.ExpiryDate)
	}
	if v.MaxUses > 0 && v.UsedCount >= v.MaxUses {
		return fmt.Errorf("voucher %q has reached its maximum uses", v.Code)
	}
	if v.CustomerID != "" && v.CustomerID != customerID {
		return fmt.Errorf("voucher %q is restricted to a different customer", v.Code)
	}
	if channel != "" && v.Channel != "" && !strings.EqualFold(v.Channel, "Any") && !strings.EqualFold(v.Channel, channel) {
		return fmt.Errorf("voucher %q is for %s use only", v.Code, v.Channel)
	}
	return nil
}

// validateVoucher checks a voucher (already loaded) is usable by customerID
// against cartAmount, and returns the discount it's worth. Shared by
// ValidateVoucher (a plain read, for a checkout screen to preview the
// discount) and RedeemVoucher (a row-locked read, to actually apply it).
func validateVoucher(v Voucher, customerID string, cartAmount int) (discountAmount int, err error) {
	if err := checkVoucherUsable(v, customerID, ""); err != nil {
		return 0, err
	}
	switch v.DiscountType {
	case "Percentage":
		discountAmount = cartAmount * v.DiscountValue / 100
	case "Flat":
		discountAmount = v.DiscountValue
	default:
		return 0, fmt.Errorf("voucher %q has an unknown discount_type %q", v.Code, v.DiscountType)
	}
	if discountAmount > cartAmount {
		discountAmount = cartAmount
	}
	if discountAmount < 0 {
		discountAmount = 0
	}
	return discountAmount, nil
}

// voucherDiscountPaise is what a voucher takes off a till bill of totalPaise
// (after offers), in paise - the same rules, at the till's channel.
func voucherDiscountPaise(v Voucher, customerID string, totalPaise int64) (int64, error) {
	if err := checkVoucherUsable(v, customerID, VoucherChannelPOS); err != nil {
		return 0, err
	}
	var d int64
	switch v.DiscountType {
	case "Percentage":
		d = totalPaise * int64(v.DiscountValue) / 100
	case "Flat":
		d = RupeesToPaise(float64(v.DiscountValue))
	default:
		return 0, fmt.Errorf("voucher %q has an unknown discount_type %q", v.Code, v.DiscountType)
	}
	if d > totalPaise {
		d = totalPaise
	}
	if d < 0 {
		d = 0
	}
	return d, nil
}

func fetchVoucherByCode(schema, code string) (id string, data map[string]interface{}, err error) {
	var dataStr string
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT id, data FROM %s.documents WHERE doctype = 'Voucher' AND UPPER(data->>'code') = UPPER($1)`, schema), code).
		Scan(&id, &dataStr); err != nil {
		return "", nil, fmt.Errorf("voucher %q not found", code)
	}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return "", nil, fmt.Errorf("voucher %q has corrupt stored data: %v", code, err)
	}
	return id, data, nil
}

// ValidateVoucher previews the discount a voucher is worth for customerID/
// cartAmount without applying it - for a checkout screen to show the
// discount before the sale is finalized.
func ValidateVoucher(tenantID, code, customerID string, cartAmount int) (*Voucher, int, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, 0, err
	}
	_, data, err := fetchVoucherByCode(schema, code)
	if err != nil {
		return nil, 0, err
	}
	v := voucherFromData(data)
	discount, err := validateVoucher(v, customerID, cartAmount)
	if err != nil {
		return nil, 0, err
	}
	return &v, discount, nil
}

// RedeemVoucher re-validates and atomically applies one use of a voucher -
// row-locked (SELECT ... FOR UPDATE) the same way DecideApproval locks a
// document, so two concurrent redemptions of a max_uses=1 voucher can't
// both succeed.
func RedeemVoucher(tenantID, code, customerID, referenceID string, cartAmount int) (discountAmount int, err error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	discount := 0
	v, err := redeemVoucherTx(tx, schema, code, customerID, referenceID, func(v Voucher) error {
		d, err := validateVoucher(v, customerID, cartAmount)
		discount = d
		return err
	})
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	LogAuditEvent(tenantID, customerID, "REDEEM_VOUCHER", "SUCCESS",
		fmt.Sprintf("Voucher %s redeemed against %s for a discount of %d (use %d/%s)", code, referenceID, discount, v.UsedCount, voucherMaxUsesLabel(v.MaxUses)))
	return discount, nil
}

// redeemVoucherTx takes one use of a voucher inside the caller's transaction:
// the row is locked, check (the caller's rules for this redemption) runs
// against the locked row, and the use is counted (Exhausted at max_uses). The
// returned voucher carries the new count.
func redeemVoucherTx(tx *sql.Tx, schema, code, customerID, referenceID string, check func(Voucher) error) (Voucher, error) {
	var id, dataStr string
	if err := tx.QueryRow(fmt.Sprintf(
		`SELECT id, data FROM %s.documents WHERE doctype = 'Voucher' AND UPPER(data->>'code') = UPPER($1) FOR UPDATE`, schema), code).
		Scan(&id, &dataStr); err != nil {
		return Voucher{}, fmt.Errorf("voucher %q not found", code)
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return Voucher{}, fmt.Errorf("voucher %q has corrupt stored data: %v", code, err)
	}
	v := voucherFromData(data)
	if err := check(v); err != nil {
		return v, err
	}
	// Stage 101.11.2: another checkout's live hold on a single-use code wins;
	// this sale's own hold is captured below.
	if held, err := crmVoucherHeldElsewhereTx(tx, schema, v, referenceID); err != nil {
		return v, err
	} else if held {
		return v, crmErr(CRMCodeVoucher, "this code is being used at another checkout right now - it can be used once")
	}
	if err := crmCaptureVoucherTx(tx, schema, v.Code, referenceID); err != nil {
		return v, err
	}
	v.UsedCount++
	data["used_count"] = v.UsedCount
	if v.MaxUses > 0 && v.UsedCount >= v.MaxUses {
		v.Status = "Exhausted"
	}
	data["status"] = v.Status
	data["last_used_on"] = referenceID
	marshaled, err := json.Marshal(data)
	if err != nil {
		return v, err
	}
	if _, err := tx.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = $1, status = $2, updated_at = CURRENT_TIMESTAMP WHERE id = $3`, schema),
		marshaled, v.Status, id); err != nil {
		return v, err
	}
	return v, nil
}

// TillVoucher is a voucher the till resolved for a bill: what it takes off.
type TillVoucher struct {
	Code          string
	DiscountPaise int64
}

// resolveTillVoucher is the quote's look-up: the first of codes (those no
// offer claimed) that is an issued voucher usable on this bill. Codes that
// are vouchers but cannot be used come back with the reason, so the cashier
// can tell the customer why.
func resolveTillVoucher(tenantID, customerID string, codes []string, totalPaise int64) (*TillVoucher, map[string]string) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, nil
	}
	reasons := map[string]string{}
	var found *TillVoucher
	for _, code := range codes {
		_, data, err := fetchVoucherByCode(schema, code)
		if err != nil {
			continue // not a voucher either: stays "did not match"
		}
		if found != nil {
			reasons[code] = "one voucher per bill"
			continue
		}
		v := voucherFromData(data)
		d, err := voucherDiscountPaise(v, customerID, totalPaise)
		if err != nil {
			reasons[code] = err.Error()
			continue
		}
		found = &TillVoucher{Code: v.Code, DiscountPaise: d}
	}
	return found, reasons
}

func voucherMaxUsesLabel(maxUses int) string {
	if maxUses <= 0 {
		return "unlimited"
	}
	return fmt.Sprintf("%d", maxUses)
}
