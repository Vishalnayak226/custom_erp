package engines

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"custom_erp/db"
)

// Stage 130.12 (74.2.2, user decision 2026-10-22: DEC-11 / DEC-12 at the BRD
// default). A return's money goes back the way it was paid (73.1.6); the
// customer may instead choose store credit - the refund method "Store
// Credit" on Returns. Store credit is a stored-value instrument (72.8) bound
// to the customer, on its own profile and liability account 2650: no card,
// no number to keep, no expiry, it cannot move to another customer and it is
// never paid out as cash. It pays a later bill of the same customer at the
// till, after points and gift cards, like an advance; above the client's
// limit only a store manager or an administrator may use it, and at most the
// set number of uses a day. A bill paid with store credit is refunded back to
// it (refundGiftCardShareTx, as for a gift card).

const (
	StoreCreditProfileID            = "Store Credit"
	AccountStoreCreditLia           = "2650"
	RefundMethodStoreCredit         = "Store Credit"
	SettingStoreCreditApprovalAbove = "pos.store_credit_approval_above"
	SettingStoreCreditDailyUses     = "pos.store_credit_daily_uses"
	// StoreCreditTenderRef is how the till names store credit among a bill's
	// advances (the customer panel offers both behind one Use button).
	StoreCreditTenderRef = "STORE-CREDIT"
)

func init() {
	RegisterSetting(SettingDefinition{Key: SettingStoreCreditApprovalAbove, Module: "Point of Sale", Label: "Store credit above this needs a store manager",
		Type: SettingTypeInt, Default: "0", Unit: "rupees", Min: settingBound(0), Max: settingBound(10000000),
		Description: "Using more store credit than this on one bill needs a store manager or an administrator at the till (a cashier switches them in with their PIN). 0 = anyone at the till."})
	RegisterSetting(SettingDefinition{Key: SettingStoreCreditDailyUses, Module: "Point of Sale", Label: "Store credit uses a day",
		Type: SettingTypeInt, Default: "0", Unit: "bills", Min: settingBound(0), Max: settingBound(100),
		Description: "How many bills one customer's store credit may pay in a day. 0 = no limit."})
	RegisterReport(ReportDefinition{ID: "store-credit-liability", Label: "Store credit by customer", Category: "Finance",
		Columns: []ReportColumn{{Key: "customer", Label: "Customer"}, {Key: "issued", Label: "Credited"}, {Key: "used", Label: "Used"}, {Key: "balance", Label: "Balance"}, {Key: "gl_2650", Label: "Store Credit Liability (GL)"}},
		Run:     storeCreditLiabilityReport})
	RegisterReport(ReportDefinition{ID: "refund-methods", Label: "Refunds by method", Category: "Finance",
		Params:  []ReportParam{{Key: "from", Label: "From", Type: "date"}, {Key: "to", Label: "To", Type: "date"}},
		Columns: []ReportColumn{{Key: "method", Label: "Refund method"}, {Key: "refunds", Label: "Refunds"}, {Key: "amount", Label: "Amount"}},
		Run:     refundMethodsReport})
}

// storeCreditLiabilityReport (R-TX-07) is each customer's store credit, and
// on every row the 2650 balance it must add up to.
func storeCreditLiabilityReport(tenantID string, _ map[string]string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	var gl int64
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(SUM(credit - debit), 0) FROM %s.gl_postings WHERE account_code = $1`, schema), AccountStoreCreditLia).Scan(&gl)
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT COALESCE(i.customer_id, ''), COALESCE(MAX(c.data->>'name'), ''), SUM(i.funded_paise), SUM(i.funded_paise - i.balance_paise), SUM(i.balance_paise)
		FROM %[1]s.stored_value_instruments i LEFT JOIN %[1]s.documents c ON c.doctype = 'Customer' AND c.id = i.customer_id
		WHERE i.profile = $1 GROUP BY i.customer_id ORDER BY SUM(i.balance_paise) DESC`, schema), StoreCreditProfileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]interface{}{}
	for rows.Next() {
		var id, name string
		var issued, used, bal int64
		if err := rows.Scan(&id, &name, &issued, &used, &bal); err != nil {
			return nil, err
		}
		out = append(out, map[string]interface{}{"customer": strings.TrimSpace(name + " " + id), "issued": PaiseToRupees(issued), "used": PaiseToRupees(used),
			"balance": PaiseToRupees(bal), "gl_2650": PaiseToRupees(gl)})
	}
	return out, rows.Err()
}

// refundMethodsReport (R-TX-08) is the processed refunds of a period by how
// they were paid back.
func refundMethodsReport(tenantID string, p map[string]string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT CASE
			WHEN COALESCE(data->>'refund_method', '') <> '' THEN data->>'refund_method'
			WHEN data ? 'tenders' THEN 'Back the way it was paid'
			ELSE 'Not recorded' END AS method,
		COUNT(*), COALESCE(SUM((data->>'amount')::numeric), 0)::float8
		FROM %s.documents WHERE doctype = 'RefundRequest' AND status = 'Processed' AND deleted_at IS NULL
		  AND ($1 = '' OR updated_at >= public.erp_day_start($1::date)) AND ($2 = '' OR updated_at < public.erp_day_start($2::date + 1))
		GROUP BY 1 ORDER BY 3 DESC`, schema), strings.TrimSpace(p["from"]), strings.TrimSpace(p["to"]))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]interface{}{}
	for rows.Next() {
		var method string
		var n int
		var amount float64
		if err := rows.Scan(&method, &n, &amount); err != nil {
			return nil, err
		}
		out = append(out, map[string]interface{}{"method": method, "refunds": n, "amount": round2(amount)})
	}
	return out, rows.Err()
}

func ensureStoreCreditSetupTx(tx *sql.Tx, schema string) error {
	if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.gl_accounts (account_code, account_name, account_type) VALUES ('2650', 'Store Credit Liability', 'Liability') ON CONFLICT (account_code) DO NOTHING`, schema)); err != nil {
		return err
	}
	_, err := tx.Exec(fmt.Sprintf(`
		INSERT INTO %s.documents (id, doctype, data, status, created_by)
		SELECT 'Store Credit', 'StoredValueProfile', jsonb_build_object(
		         'code', 'Store Credit', 'instrument_type', 'Store credit', 'number_length', 16, 'number_format', 'Alphanumeric',
		         'denomination_mode', 'Variable', 'min_amount', 0, 'max_amount', 0, 'validity_months', 0,
		         'reload_allowed', 'Yes', 'partial_redemption', 'Yes', 'liability_account', '2650', 'status', 'Active'),
		       'Active', 'system'
		WHERE NOT EXISTS (SELECT 1 FROM %s.documents WHERE id = 'Store Credit')`, schema, schema))
	return err
}

// storeCreditInstrumentTx is the customer's store credit, locked; found=false
// when they have none yet.
func storeCreditInstrumentTx(tx *sql.Tx, schema, customerID string) (svCard, bool, error) {
	c, err := scanSVCard(tx.QueryRow(fmt.Sprintf(`SELECT %s FROM %s.stored_value_instruments WHERE profile = $1 AND customer_id = $2
		ORDER BY created_at LIMIT 1 FOR UPDATE`, svCardColumns, schema), StoreCreditProfileID, customerID))
	if errors.Is(err, sql.ErrNoRows) {
		return c, false, nil
	}
	return c, err == nil, err
}

// issueStoreCreditTx credits amount to the customer's store credit (making it
// on first use), as event idem. Returns the instrument id.
func issueStoreCreditTx(tx *sql.Tx, tenantID, schema, customerID string, amount int64, src svSource, idem string) (string, error) {
	if amount <= 0 {
		return "", nil
	}
	if err := ensureStoreCreditSetupTx(tx, schema); err != nil {
		return "", err
	}
	var done string
	if tx.QueryRow(fmt.Sprintf(`SELECT instrument_id FROM %s.stored_value_ledger WHERE idempotency_key = $1`, schema), idem).Scan(&done) == nil {
		return done, nil
	}
	c, found, err := storeCreditInstrumentTx(tx, schema, customerID)
	if err != nil {
		return "", err
	}
	if !found {
		p, err := LoadStoredValueProfile(tenantID, StoreCreditProfileID)
		if err != nil {
			p = StoredValueProfile{ID: StoreCreditProfileID, NumberLength: 16, Alphanumeric: true}
		}
		key, last4, enc, err := svSealCard(generateCardNumber(p))
		if err != nil {
			return "", err
		}
		id := NewDocIDCompact("SC")
		if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.stored_value_instruments (instrument_id, card_number, profile, status, customer_id, created_by, card_last4, card_enc)
			VALUES ($1, $2, $3, 'Inactive', $4, $5, $6, $7)`, schema), id, key, StoreCreditProfileID, customerID, src.User, last4, enc); err != nil {
			return "", err
		}
		c = svCard{ID: id, Number: key, Profile: StoreCreditProfileID, Status: SVStatusInactive, Last4: last4}
	}
	event := SVEventReload
	if c.Status == SVStatusInactive {
		event = SVEventActivate
	}
	if _, err := appendSVEvent(tx, schema, &c, event, amount, "", src, idem); err != nil {
		return "", err
	}
	return c.ID, nil
}

// StoreCreditBalance is a customer's store credit, for the till and Returns.
func StoreCreditBalance(tenantID, customerID string) (float64, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, err
	}
	var bal int64
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(SUM(balance_paise), 0) FROM %s.stored_value_instruments WHERE profile = $1 AND customer_id = $2`, schema),
		StoreCreditProfileID, strings.TrimSpace(customerID)).Scan(&bal)
	return PaiseToRupees(bal), err
}

// PrepareStoreCreditTender checks store credit paying part of a bill - the
// bill's own customer's, enough left, within the manager limit and the daily
// uses - and returns it as a stored-value tender.
func PrepareStoreCreditTender(tenantID, role, customerID string, amountRupees float64, duePaise int64) ([]GiftCardTender, error) {
	amount := RupeesToPaise(amountRupees)
	if amount <= 0 {
		return nil, nil
	}
	customerID = strings.TrimSpace(customerID)
	if customerID == "" {
		return nil, &ValidationError{Code: "POSOFF-1309", Message: "store credit belongs to a customer - take their mobile number first"}
	}
	if amount > duePaise {
		return nil, &ValidationError{Code: "POSOFF-1309", Message: fmt.Sprintf("store credit of %.2f is more than the %.2f still due - use less", PaiseToRupees(amount), PaiseToRupees(duePaise))}
	}
	if limit := GetSettingInt(tenantID, SettingStoreCreditApprovalAbove); limit > 0 && amount > int64(limit)*100 && role != RoleStoreManager && !HasFullAccess(role) {
		return nil, &ValidationError{Code: "POSOFF-1309", Message: fmt.Sprintf("using more than %d of store credit needs a store manager - switch them in (More… → Switch staff)", limit)}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	c, err := scanSVCard(db.DB.QueryRow(fmt.Sprintf(`SELECT %s FROM %s.stored_value_instruments WHERE profile = $1 AND customer_id = $2 ORDER BY created_at LIMIT 1`, svCardColumns, schema),
		StoreCreditProfileID, customerID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, &ValidationError{Code: "POSOFF-1309", Message: "this customer has no store credit"}
	}
	if err != nil {
		return nil, err
	}
	if c.Balance < amount {
		return nil, &ValidationError{Code: "POSOFF-1309", Message: fmt.Sprintf("this customer has %.2f of store credit, not %.2f", PaiseToRupees(c.Balance), PaiseToRupees(amount))}
	}
	if uses := GetSettingInt(tenantID, SettingStoreCreditDailyUses); uses > 0 {
		var today int
		_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.stored_value_ledger WHERE instrument_id = $1 AND event_type = $2 AND public.erp_local_date(created_at) = public.erp_today()`, schema),
			c.ID, SVEventRedeem).Scan(&today)
		if today >= uses {
			return nil, &ValidationError{Code: "POSOFF-1309", Message: fmt.Sprintf("this customer's store credit has paid %d bill(s) today - the most allowed", today)}
		}
	}
	return []GiftCardTender{{InstrumentID: c.ID, Masked: "Store credit", AmountPaise: amount}}, nil
}

// storeCreditRefundCustomer is the customer a refund's store credit goes to:
// the original sale's. Store credit is never anonymous (DEC-13).
func storeCreditRefundCustomer(tx *sql.Tx, tenantID, schema, originalOrderID string) (string, error) {
	if originalOrderID != "" {
		if _, customerID, found, err := loadSaleAllocation(tx, tenantID, schema, originalOrderID); err == nil && found && customerID != "" {
			return customerID, nil
		}
		var cust string
		_ = tx.QueryRow(fmt.Sprintf(`SELECT COALESCE(data->>'customer_id', '') FROM %s.documents WHERE doctype = 'POSCart' AND id = $1`, schema), originalOrderID).Scan(&cust)
		if c := strings.TrimSpace(cust); c != "" && !strings.EqualFold(c, "walk-in") {
			return c, nil
		}
	}
	return "", &ValidationError{Code: "SALESR-1310", Message: "store credit belongs to a customer, and this bill has none - refund it the way it was paid"}
}
