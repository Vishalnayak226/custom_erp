package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"time"
)

// Stage 26.7.2: Voucher/coupon issuance + redemption. Voucher is a flat-
// schema Master doctype (db/migrations_stage26_7_crm_loyalty.sql) - create/
// list/edit/bulk-CSV-issuance all come free from the existing generic
// doctype/BulkImportCSV machinery, zero bespoke code needed for those.
// Redemption is the one thing that needs real logic (expiry/max-uses/
// customer-restriction checks, and an atomic used_count increment), mirroring
// the shape of the existing loyalty ledger's RedeemLoyaltyPoints (Stage
// 13.13d) - a standalone validate-then-apply function, not deep POS
// checkout integration (the same shallow-integration precedent
// handleRedeemLoyaltyPoints already set).

// Voucher mirrors the doctype's own JSON shape.
type Voucher struct {
	Code          string `json:"code"`
	DiscountType  string `json:"discount_type"`
	DiscountValue int    `json:"discount_value"`
	ExpiryDate    string `json:"expiry_date"`
	MaxUses       int    `json:"max_uses"`
	UsedCount     int    `json:"used_count"`
	CustomerID    string `json:"customer_id"`
	Status        string `json:"status"`
}

func voucherFromData(data map[string]interface{}) Voucher {
	return Voucher{
		Code:          stringField(data, "code"),
		DiscountType:  stringField(data, "discount_type"),
		DiscountValue: int(numFromInterface(data["discount_value"])),
		ExpiryDate:    stringField(data, "expiry_date"),
		MaxUses:       int(numFromInterface(data["max_uses"])),
		UsedCount:     int(numFromInterface(data["used_count"])),
		CustomerID:    stringField(data, "customer_id"),
		Status:        stringField(data, "status"),
	}
}

func stringField(data map[string]interface{}, key string) string {
	s, _ := data[key].(string)
	return s
}

// validateVoucher checks a voucher (already loaded) is usable by customerID
// against cartAmount, and returns the discount it's worth. Shared by
// ValidateVoucher (a plain read, for a checkout screen to preview the
// discount) and RedeemVoucher (a row-locked read, to actually apply it).
func validateVoucher(v Voucher, customerID string, cartAmount int) (discountAmount int, err error) {
	if v.Status != "Active" {
		return 0, fmt.Errorf("voucher %q is not active (status: %s)", v.Code, v.Status)
	}
	if v.ExpiryDate != "" && v.ExpiryDate < time.Now().Format("2006-01-02") {
		return 0, fmt.Errorf("voucher %q expired on %s", v.Code, v.ExpiryDate)
	}
	if v.MaxUses > 0 && v.UsedCount >= v.MaxUses {
		return 0, fmt.Errorf("voucher %q has reached its maximum uses", v.Code)
	}
	if v.CustomerID != "" && v.CustomerID != customerID {
		return 0, fmt.Errorf("voucher %q is restricted to a different customer", v.Code)
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

func fetchVoucherByCode(schema, code string) (id string, data map[string]interface{}, err error) {
	var dataStr string
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT id, data FROM %s.documents WHERE doctype = 'Voucher' AND data->>'code' = $1`, schema), code).
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

	var id, dataStr string
	if err := tx.QueryRow(fmt.Sprintf(
		`SELECT id, data FROM %s.documents WHERE doctype = 'Voucher' AND data->>'code' = $1 FOR UPDATE`, schema), code).
		Scan(&id, &dataStr); err != nil {
		return 0, fmt.Errorf("voucher %q not found", code)
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return 0, fmt.Errorf("voucher %q has corrupt stored data: %v", code, err)
	}
	v := voucherFromData(data)
	discount, err := validateVoucher(v, customerID, cartAmount)
	if err != nil {
		return 0, err
	}

	newUsedCount := v.UsedCount + 1
	data["used_count"] = newUsedCount
	newStatus := v.Status
	if v.MaxUses > 0 && newUsedCount >= v.MaxUses {
		newStatus = "Exhausted"
	}
	data["status"] = newStatus
	marshaled, err := json.Marshal(data)
	if err != nil {
		return 0, err
	}
	if _, err := tx.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = $1, status = $2, updated_at = CURRENT_TIMESTAMP WHERE id = $3`, schema),
		marshaled, newStatus, id); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	LogAuditEvent(tenantID, customerID, "REDEEM_VOUCHER", "SUCCESS",
		fmt.Sprintf("Voucher %s redeemed against %s for a discount of %d (use %d/%s)", code, referenceID, discount, newUsedCount, voucherMaxUsesLabel(v.MaxUses)))
	return discount, nil
}

func voucherMaxUsesLabel(maxUses int) string {
	if maxUses <= 0 {
		return "unlimited"
	}
	return fmt.Sprintf("%d", maxUses)
}
