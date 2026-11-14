package engines

// Stage 73.4.2.4: the order risk check. A new order that trips one of the
// tenant's rules is held with HoldFraudRisk until a person releases it. Off by
// default; with the switch off nothing here runs and orders behave as before.
//
// The rules are deliberately few and plain - a cash-on-delivery or order value
// ceiling, repeat orders from one phone, and a block list of phones and PIN
// codes - because each one is something a store owner can explain to a
// customer. No score, no outside service.

import (
	"database/sql"
	"fmt"
	"regexp"
	"strings"

	"custom_erp/db"
)

// HoldFraudRisk is the hold code for an order stopped by the risk check.
const HoldFraudRisk = "FRAUD_RISK"

const (
	SettingRiskHold           = "orders.risk_hold"
	SettingRiskCODAbove       = "orders.risk_cod_above"
	SettingRiskValueAbove     = "orders.risk_value_above"
	SettingRiskOrdersPerPhone = "orders.risk_orders_per_phone"
	SettingRiskBlocked        = "orders.risk_blocked"
)

func init() {
	zero, maxAmount, maxOrders := 0.0, 100000000.0, 100.0
	RegisterSetting(SettingDefinition{Key: SettingRiskHold, Module: "Orders", Label: "Risk check on new orders",
		Type: SettingTypeBool, Default: "false",
		Description: "On: a new order that trips a rule below is held as Risk check until someone releases it."})
	RegisterSetting(SettingDefinition{Key: SettingRiskCODAbove, Module: "Orders", Label: "Risk: COD order above",
		Type: SettingTypeInt, Default: "0", Unit: "rupees", Min: &zero, Max: &maxAmount,
		Description: "Hold a cash-on-delivery order worth more than this. 0 = no limit."})
	RegisterSetting(SettingDefinition{Key: SettingRiskValueAbove, Module: "Orders", Label: "Risk: order above",
		Type: SettingTypeInt, Default: "0", Unit: "rupees", Min: &zero, Max: &maxAmount,
		Description: "Hold any order worth more than this. 0 = no limit."})
	RegisterSetting(SettingDefinition{Key: SettingRiskOrdersPerPhone, Module: "Orders", Label: "Risk: orders per phone in 24 hours",
		Type: SettingTypeInt, Default: "0", Unit: "orders", Min: &zero, Max: &maxOrders,
		Description: "Hold an order when the same phone already placed this many in the last 24 hours. 0 = off."})
	RegisterSetting(SettingDefinition{Key: SettingRiskBlocked, Module: "Orders", Label: "Risk: blocked phones and PIN codes",
		Type: SettingTypeString, Default: "",
		Description: "Phones and PIN codes, comma separated. An order from one is held."})
}

var riskPincodeRe = regexp.MustCompile(`\b\d{6}\b`)

// orderRiskReasons returns why a new order should be held, or nil. lines are
// the order's lines after bundle expansion (the same ones its total is made of).
func orderRiskReasons(tenantID, schema string, in SalesOrderInput, shippingAddress string, lines []SalesOrderLineInput) ([]string, error) {
	if !GetSettingBool(tenantID, SettingRiskHold) {
		return nil, nil
	}
	var why []string
	var total float64
	for _, l := range lines {
		total += l.UnitPrice * float64(l.Qty)
	}
	if limit := GetSettingInt(tenantID, SettingRiskCODAbove); limit > 0 && strings.EqualFold(in.PaymentStatus, "COD") && total > float64(limit) {
		why = append(why, fmt.Sprintf("COD above %d", limit))
	}
	if limit := GetSettingInt(tenantID, SettingRiskValueAbove); limit > 0 && total > float64(limit) {
		why = append(why, fmt.Sprintf("order above %d", limit))
	}

	phone := ""
	if strings.TrimSpace(in.CustomerPhone) != "" {
		phone = NormalizeTenantPhone(tenantID, in.CustomerPhone).National
	}
	if n := GetSettingInt(tenantID, SettingRiskOrdersPerPhone); n > 0 && phone != "" {
		var count int
		// NOW() and created_at are both the database's own clock (see the
		// age note in oms_console.go), so the 24 hours are right in any zone.
		err := db.DB.QueryRow(fmt.Sprintf(
			`SELECT COUNT(*) FROM %s.documents WHERE doctype = 'SalesOrder' AND deleted_at IS NULL
			   AND data->>'customer_phone' = $1 AND created_at > NOW() - INTERVAL '24 hours'`, schema),
			phone).Scan(&count)
		if err != nil && err != sql.ErrNoRows {
			return nil, err
		}
		if count >= n {
			why = append(why, fmt.Sprintf("%d orders from this phone in 24 h", count+1))
		}
	}

	if blocked := strings.TrimSpace(GetSettingString(tenantID, SettingRiskBlocked)); blocked != "" {
		pin := strings.TrimSpace(in.ShippingPincode)
		if pin == "" {
			if all := riskPincodeRe.FindAllString(shippingAddress, -1); len(all) > 0 {
				pin = all[len(all)-1]
			}
		}
		phoneDigits := digitsOnly(phone)
		var badPhone, badPin bool
		for _, entry := range strings.Split(blocked, ",") {
			d := digitsOnly(entry)
			if d == "" {
				continue
			}
			if phoneDigits != "" && (d == phoneDigits || digitsOnly(NormalizeTenantPhone(tenantID, entry).National) == phoneDigits) {
				badPhone = true
			} else if pin != "" && d == pin {
				badPin = true
			}
		}
		if badPhone {
			why = append(why, "blocked phone")
		}
		if badPin {
			why = append(why, "blocked PIN code")
		}
	}
	return why, nil
}
