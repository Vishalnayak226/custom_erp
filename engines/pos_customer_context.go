package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Stage 73.3.2 - customer context at the till. A cashier serving a known
// customer sees what helps the sale: their points and what those are worth,
// their tier, what it gives and how far the next one is, their last few
// bills and returns, and the vouchers issued to them that they can use now.
// It reads the same figures the Loyalty screen and the tier engine use. The
// phone number is masked to its last four digits.

// TillCustomerBill is one recent bill.
type TillCustomerBill struct {
	CartNumber string  `json:"cart_number"`
	Date       string  `json:"date"`
	Amount     float64 `json:"amount"`
	Lines      int     `json:"lines"`
}

// TillCustomerReturn is one recent return.
type TillCustomerReturn struct {
	ID     string  `json:"id"`
	Bill   string  `json:"bill"`
	Date   string  `json:"date"`
	Status string  `json:"status"`
	Refund float64 `json:"refund"`
}

// TillCustomerVoucher is a voucher issued to the customer that can be used.
type TillCustomerVoucher struct {
	Code     string `json:"code"`
	Offer    string `json:"offer"`
	Expires  string `json:"expires,omitempty"`
	UsesLeft int    `json:"uses_left,omitempty"`
}

// TillCustomerContext is everything the till shows about the customer.
type TillCustomerContext struct {
	CustomerID    string                `json:"customer_id"`
	Name          string                `json:"name"`
	Phone         string                `json:"phone,omitempty"`
	Balance       int                   `json:"balance"`
	PointsValue   float64               `json:"points_value"`
	ConsentSMS    bool                  `json:"consent_sms"`
	ConsentWA     bool                  `json:"consent_whatsapp"`
	Tier          string                `json:"tier,omitempty"`
	Benefits      string                `json:"benefits,omitempty"`
	Spend         float64               `json:"spend"`
	NextTier      string                `json:"next_tier,omitempty"`
	ToNextTier    float64               `json:"to_next_tier,omitempty"`
	BelowSince    string                `json:"below_tier_since,omitempty"`
	RecentBills   []TillCustomerBill    `json:"recent_bills"`
	RecentReturns []TillCustomerReturn  `json:"recent_returns"`
	Vouchers      []TillCustomerVoucher `json:"vouchers"`
}

func maskPhone(p string) string {
	digits := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, p)
	if len(digits) <= 4 {
		return digits
	}
	return strings.Repeat("*", len(digits)-4) + digits[len(digits)-4:]
}

// TillCustomerContextFor gathers the customer's till view.
func TillCustomerContextFor(tenantID, customerID string) (*TillCustomerContext, error) {
	customerID = strings.TrimSpace(customerID)
	if customerID == "" {
		return nil, &ValidationError{Code: "GLOBAL-0002", Message: "a customer is required"}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	out := &TillCustomerContext{CustomerID: customerID, RecentBills: []TillCustomerBill{}, RecentReturns: []TillCustomerReturn{}, Vouchers: []TillCustomerVoucher{}}
	var dataStr string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents WHERE doctype = 'Customer' AND id = $1`, schema), customerID).Scan(&dataStr); err == nil {
		var data map[string]interface{}
		_ = json.Unmarshal([]byte(dataStr), &data)
		out.Name, out.Phone = strField(data, "name"), maskPhone(strField(data, "phone"))
		out.Tier, out.BelowSince = strField(data, "loyalty_tier"), strField(data, "loyalty_tier_below_since")
		// Stage 55.17a: whether offers may be sent to them, and by what.
		out.ConsentSMS, out.ConsentWA = HasMarketingConsent(data, ConsentChannelSMS), HasMarketingConsent(data, ConsentChannelWhatsApp)
	}
	if out.Balance, err = GetLoyaltyBalance(tenantID, customerID); err != nil {
		return nil, err
	}
	out.PointsValue = PaiseToRupees(int64(out.Balance) * loyaltyPointValuePaise(tenantID))
	if out.Spend, err = customerTierSpend(tenantID, schema, customerID); err != nil {
		return nil, err
	}
	out.Spend = round2(out.Spend)
	if rules, err := GetLoyaltyTierRules(tenantID); err == nil {
		for _, r := range rules {
			if r.Tier == out.Tier {
				out.Benefits = r.Benefits
			}
			if out.NextTier == "" && r.MinSpend > out.Spend && r.MinSpend > tierMinSpend(rules, out.Tier) {
				out.NextTier, out.ToNextTier = r.Tier, round2(r.MinSpend-out.Spend)
			}
		}
	}

	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, to_char(created_at, 'YYYY-MM-DD'), `+posCartSpendSQL+`, jsonb_array_length(COALESCE(data->'items', '[]'::jsonb))
		FROM %s.documents WHERE doctype = 'POSCart' AND status = 'Paid' AND data->>'customer_id' = $1 ORDER BY created_at DESC LIMIT 5`, schema), customerID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var b TillCustomerBill
		if err := rows.Scan(&b.CartNumber, &b.Date, &b.Amount, &b.Lines); err == nil {
			b.Amount = round2(b.Amount)
			out.RecentBills = append(out.RecentBills, b)
		}
	}
	rows.Close()

	rows, err = db.DB.Query(fmt.Sprintf(`SELECT r.id, COALESCE(r.data->>'original_order_id', ''), to_char(r.created_at, 'YYYY-MM-DD'), r.status,
		COALESCE((r.data->>'total_refund_eligible')::numeric, 0)
		FROM %s.documents r JOIN %s.documents c ON c.id = r.data->>'original_order_id' AND c.doctype = 'POSCart'
		WHERE r.doctype = 'ReturnRequest' AND c.data->>'customer_id' = $1 ORDER BY r.created_at DESC LIMIT 5`, schema, schema), customerID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var rr TillCustomerReturn
		if err := rows.Scan(&rr.ID, &rr.Bill, &rr.Date, &rr.Status, &rr.Refund); err == nil {
			out.RecentReturns = append(out.RecentReturns, rr)
		}
	}
	rows.Close()

	rows, err = db.DB.Query(fmt.Sprintf(`SELECT data FROM %s.documents WHERE doctype = 'Voucher' AND status = 'Active' AND data->>'customer_id' = $1
		AND deleted_at IS NULL ORDER BY COALESCE(data->>'expiry_date', '9999') LIMIT 10`, schema), customerID)
	if err != nil {
		return nil, err
	}
	today := time.Now().Format("2006-01-02")
	for rows.Next() {
		var vs string
		if err := rows.Scan(&vs); err != nil {
			continue
		}
		var data map[string]interface{}
		_ = json.Unmarshal([]byte(vs), &data)
		v := voucherFromData(data)
		if checkVoucherUsable(v, customerID, VoucherChannelPOS) != nil {
			continue
		}
		offer := fmt.Sprintf("%d%% off", v.DiscountValue)
		if v.DiscountType == "Flat" {
			offer = fmt.Sprintf("%d off", v.DiscountValue)
		}
		tv := TillCustomerVoucher{Code: v.Code, Offer: offer, Expires: v.ExpiryDate}
		if v.MaxUses > 0 {
			tv.UsesLeft = v.MaxUses - v.UsedCount
		}
		if v.ValidFrom == "" || v.ValidFrom <= today {
			out.Vouchers = append(out.Vouchers, tv)
		}
	}
	rows.Close()
	return out, nil
}
