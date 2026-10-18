package engines

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"custom_erp/db"

	"github.com/lib/pq"
)

// Stage 85.C (user, 2026-10-18): "Customer phone number is not asked in bill
// ... Phone number is unque. Ask Always." with the 2026-10-17 list (address,
// DOB, DOA, gender, PAN, GSTIN) and 77.0's per-store walk-in customer.
//
// The till asks for a mobile number on every bill (view-pos-customer.js). A
// known number brings the customer back; a new one opens the customer form.
// A customer who will not give one is the store's walk-in customer: one per
// store, made on first use, with a placeholder number (1111111111,
// 2222222222 ...) that is never printed and never earns points.

// CustomerRef is what the till needs to know about a customer.
type CustomerRef struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Phone    string `json:"phone"`
	Email    string `json:"email"`
	GSTIN    string `json:"gstin"`
	PAN      string `json:"pan"`
	Address  string `json:"address"`
	Line2    string `json:"address_line2"`
	City     string `json:"city"`
	State    string `json:"state"`
	Pincode  string `json:"pincode"`
	WalkIn   bool   `json:"walk_in"`
	Complete bool   `json:"address_complete"`
}

func customerRefFrom(id string, data map[string]interface{}) CustomerRef {
	c := CustomerRef{
		ID: id, Name: strField(data, "name"), Phone: strField(data, "phone"), Email: strField(data, "email"),
		GSTIN: strField(data, "gstin"), PAN: strField(data, "pan"), Address: strField(data, "address"),
		Line2: strField(data, "address_line2"), City: strField(data, "city"), State: strField(data, "state"),
		Pincode: strField(data, "pincode"), WalkIn: strField(data, "walk_in") == "Yes",
	}
	c.Complete = c.Address != "" && c.State != "" && (c.City != "" || c.Pincode != "")
	if c.WalkIn {
		c.Phone = "" // the placeholder is never shown
	}
	return c
}

// LoadCustomerRef reads one customer.
func LoadCustomerRef(tenantID, id string) (CustomerRef, bool, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return CustomerRef{}, false, err
	}
	var raw []byte
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents WHERE doctype = 'Customer' AND id = $1 AND deleted_at IS NULL`, schema), id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return CustomerRef{}, false, nil
	}
	if err != nil {
		return CustomerRef{}, false, err
	}
	var data map[string]interface{}
	_ = json.Unmarshal(raw, &data)
	return customerRefFrom(id, data), true, nil
}

// LookupCustomerByPhone finds the one customer with this mobile number (as
// the master stores it: the national number, dial code resolved). Deleted,
// merged and walk-in records are not candidates.
func LookupCustomerByPhone(tenantID, raw string) (CustomerRef, bool, string, error) {
	parsed := NormalizePhone(raw, TenantPhoneCountry(tenantID))
	if !parsed.Valid {
		return CustomerRef{}, false, "", &ValidationError{Code: "MASTER-0051", Message: parsed.Reason}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return CustomerRef{}, false, parsed.National, err
	}
	var id string
	var data []byte
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT id, data FROM %s.documents
		 WHERE doctype = 'Customer' AND data->>'phone' = $1 AND deleted_at IS NULL AND status != 'Cancelled'
		   AND COALESCE(data->>'merged_into', '') = '' AND COALESCE(data->>'walk_in', '') <> 'Yes'
		 ORDER BY created_at LIMIT 1`, schema), parsed.National).Scan(&id, &data)
	if errors.Is(err, sql.ErrNoRows) {
		return CustomerRef{}, false, parsed.National, nil
	}
	if err != nil {
		return CustomerRef{}, false, parsed.National, err
	}
	var m map[string]interface{}
	_ = json.Unmarshal(data, &m)
	return customerRefFrom(id, m), true, parsed.National, nil
}

// IsWalkInCustomer reports whether a customer is a store's walk-in record.
func IsWalkInCustomer(tenantID, id string) bool {
	if strings.TrimSpace(id) == "" {
		return false
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return false
	}
	var walkIn string
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(data->>'walk_in', '') FROM %s.documents WHERE doctype = 'Customer' AND id = $1`, schema), id).Scan(&walkIn)
	return walkIn == "Yes"
}

// walkInPlaceholder is the n-th store's number: 1111111111 ... 9999999999,
// then 90000000NN - distinct per store, never a real customer's.
func walkInPlaceholder(n int) string {
	if n >= 1 && n <= 9 {
		return strings.Repeat(strconv.Itoa(n), 10)
	}
	return fmt.Sprintf("9%09d", n)
}

var walkInMu sync.Mutex

// EnsureWalkInCustomer is the store's walk-in customer, made on first use.
func EnsureWalkInCustomer(tenantID, store string) (CustomerRef, error) {
	store = strings.TrimSpace(store)
	if store == "" {
		return CustomerRef{}, &ValidationError{Code: "STORE-0851", Message: "Choose the store first."}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return CustomerRef{}, err
	}
	find := func() (CustomerRef, bool, error) {
		var id string
		var raw []byte
		err := db.DB.QueryRow(fmt.Sprintf(`
			SELECT id, data FROM %s.documents
			 WHERE doctype = 'Customer' AND data->>'walk_in' = 'Yes' AND data->>'walk_in_store' = $1 AND deleted_at IS NULL
			 ORDER BY created_at LIMIT 1`, schema), store).Scan(&id, &raw)
		if errors.Is(err, sql.ErrNoRows) {
			return CustomerRef{}, false, nil
		}
		if err != nil {
			return CustomerRef{}, false, err
		}
		var m map[string]interface{}
		_ = json.Unmarshal(raw, &m)
		return customerRefFrom(id, m), true, nil
	}
	if c, ok, err := find(); err != nil || ok {
		return c, err
	}
	walkInMu.Lock()
	defer walkInMu.Unlock()
	if c, ok, err := find(); err != nil || ok {
		return c, err
	}
	var n int
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.documents WHERE doctype = 'Customer' AND data->>'walk_in' = 'Yes'`, schema)).Scan(&n); err != nil {
		return CustomerRef{}, err
	}
	var storeName string
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(NULLIF(data->>'name', ''), id) FROM %s.documents WHERE doctype = 'Location' AND id = $1`, schema), store).Scan(&storeName)
	if storeName == "" {
		storeName = store
	}
	payload := map[string]interface{}{
		"name": "Walk-in - " + storeName, "phone": walkInPlaceholder(n + 1), "status": "Active",
		"walk_in": "Yes", "walk_in_store": store, "consent_sms": false, "consent_whatsapp": false,
	}
	if err := PrepareMasterNumber(tenantID, store, "Customer", true, payload); err != nil || strField(payload, "id") == "" {
		payload["id"] = "WALKIN-" + store
		payload["code"] = payload["id"]
	}
	id := strField(payload, "id")
	raw, _ := json.Marshal(payload)
	if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'Customer', $2, 'Active', 'system')`, schema), id, raw); err != nil {
		return CustomerRef{}, err
	}
	LogAuditEvent(tenantID, "system", "CRM", "WALK_IN_CUSTOMER_CREATED", fmt.Sprintf("Walk-in customer %s for store %s", id, store))
	return customerRefFrom(id, payload), nil
}

// --- 85.C4: high-value sale rules, by country ---------------------------------

// HighValuePolicy is when a bill needs the buyer's address or PAN, and how
// much of it may be paid in cash. 0 = no rule.
type HighValuePolicy struct {
	Country     string  `json:"country"`
	AddressFrom float64 `json:"address_from"` // full address at or above this total
	AddressAll  bool    `json:"address_all"`  // true: every buyer; false: unregistered (no GSTIN) only
	PANAbove    float64 `json:"pan_above"`    // PAN (or a Form 97 declaration) above this total
	CashLimit   float64 `json:"cash_limit"`   // cash of this much or more is refused
}

// countryHighValueDefaults are the statutory defaults (research 2026-10-17,
// Stage 77): India CGST Rule 46(e) (address >= 50,000 to an unregistered
// buyer), Income-tax Rules 2026 r.159 (PAN > 2,00,000; Form 97 if none),
// s.269ST (cash >= 2,00,000 refused); UK / Germany full invoice details above
// the simplified-invoice limit (250), UAE above AED 10,000.
var countryHighValueDefaults = map[string]HighValuePolicy{
	"IN": {AddressFrom: 50000, PANAbove: 200000, CashLimit: 200000},
	"GB": {AddressFrom: 250, AddressAll: true},
	"DE": {AddressFrom: 250, AddressAll: true},
	"AE": {AddressFrom: 10000, AddressAll: true},
}

// HighValuePolicyFor is the tenant's rule: each setting left blank takes the
// home country's default; a value (0 = off) replaces it.
func HighValuePolicyFor(tenantID string) HighValuePolicy {
	country := TenantPhoneCountry(tenantID)
	p := countryHighValueDefaults[country]
	p.Country = country
	override := func(key string, into *float64) {
		v := strings.TrimSpace(GetSettingString(tenantID, key))
		if v == "" {
			return
		}
		if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0 {
			*into = f
		}
	}
	override("pos.high_value.address_from", &p.AddressFrom)
	override("pos.high_value.pan_above", &p.PANAbove)
	override("pos.high_value.cash_limit", &p.CashLimit)
	return p
}

// CheckHighValueSale is checked at checkout, before any side effect, and by
// the till before Complete Sale so the cashier asks first.
func CheckHighValueSale(tenantID, customerID string, total, cash float64, form97 bool) error {
	p := HighValuePolicyFor(tenantID)
	if p.CashLimit > 0 && cash >= p.CashLimit {
		return &ValidationError{Code: "POSOFF-0868", Message: fmt.Sprintf("Cash of %.2f on one bill; the limit is below %.2f. Take the rest by card or UPI.", cash, p.CashLimit)}
	}
	needAddress := p.AddressFrom > 0 && total >= p.AddressFrom
	needPAN := p.PANAbove > 0 && total > p.PANAbove
	if !needAddress && !needPAN {
		return nil
	}
	c, found, err := LoadCustomerRef(tenantID, customerID)
	if err != nil {
		return err
	}
	if !found || c.WalkIn {
		return &ValidationError{Code: "POSOFF-0866", Message: fmt.Sprintf("A bill of %.2f needs the buyer's details - take the customer's mobile number and address.", total)}
	}
	if needAddress && (p.AddressAll || c.GSTIN == "") && !c.Complete {
		return &ValidationError{Code: "POSOFF-0866", Message: fmt.Sprintf("A bill of %.2f needs %s's address (line 1, state and city or PIN).", total, c.Name)}
	}
	if needPAN && c.PAN == "" && !form97 {
		return &ValidationError{Code: "POSOFF-0867", Message: fmt.Sprintf("A bill above %.2f needs %s's PAN, or a signed Form 97 if they have none.", p.PANAbove, c.Name)}
	}
	return nil
}

// --- 85.C1/85.C2: PAN and GSTIN on the customer master -------------------------

// panPattern is field_formats.go's, the one the generic PAN field check uses.

// validateCustomerTaxIDs checks an Indian customer's PAN and GSTIN, and
// fills the PAN from the GSTIN (characters 3-12 are the holder's PAN).
func validateCustomerTaxIDs(tenantID string, payload map[string]interface{}) error {
	if TenantPhoneCountry(tenantID) != "IN" {
		return nil
	}
	gstin := strings.ToUpper(strings.TrimSpace(strField(payload, "gstin")))
	pan := strings.ToUpper(strings.TrimSpace(strField(payload, "pan")))
	if gstin != "" {
		if !gstinPattern.MatchString(gstin) || !ValidGSTINChecksum(gstin) {
			return &ValidationError{Code: "CUSTOM-0869", Message: fmt.Sprintf("GSTIN %q is not a valid GSTIN (15 characters, last one a check digit).", gstin)}
		}
		payload["gstin"] = gstin
		if pan == "" {
			pan = gstin[2:12]
		}
	}
	if pan != "" {
		if !panPattern.MatchString(pan) {
			return &ValidationError{Code: "CUSTOM-0870", Message: fmt.Sprintf("PAN %q is not a valid PAN (5 letters, 4 digits, 1 letter).", pan)}
		}
		payload["pan"] = pan
	}
	return nil
}

// ReceiptCustomerLine is how the customer reads on a receipt (77.3): the name
// and a real mobile number - never the customer code, and nothing at all for
// a store's walk-in customer, whose number is a placeholder.
func ReceiptCustomerLine(tenantID, customerID string) string {
	c, found, err := LoadCustomerRef(tenantID, strings.TrimSpace(customerID))
	if err != nil || !found || c.WalkIn {
		return ""
	}
	if c.Phone != "" {
		return c.Name + " · " + c.Phone
	}
	return c.Name
}

// --- Stage 85.E4 (77.8): find a store's bill to return -------------------------

// StoreBill is one paid bill, as the return search lists it.
type StoreBill struct {
	CartNumber string  `json:"cart_number"`
	BillNumber string  `json:"bill_number"`
	Store      string  `json:"store"`
	Date       string  `json:"date"`
	Customer   string  `json:"customer"`
	Phone      string  `json:"phone"`
	Total      float64 `json:"total"`
}

// SearchStoreBills finds paid bills by bill / cart number, the customer's
// mobile or name, and/or a date - at the given stores (all when nil).
func SearchStoreBills(tenantID string, stores []string, q, date string) ([]StoreBill, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	sqlq := fmt.Sprintf(`SELECT c.id, COALESCE(c.data->>'bill_number', ''), COALESCE(c.data->>'location', ''), to_char(c.created_at, 'YYYY-MM-DD HH24:MI'),
		COALESCE(cu.data->>'name', ''), CASE WHEN COALESCE(cu.data->>'walk_in', '') = 'Yes' THEN '' ELSE COALESCE(cu.data->>'phone', '') END,
		COALESCE((c.data->>'amount_paid')::numeric, 0)
		FROM %[1]s.documents c LEFT JOIN %[1]s.documents cu ON cu.doctype = 'Customer' AND cu.id = c.data->>'customer_id'
		WHERE c.doctype = 'POSCart' AND c.status = 'Paid' AND c.deleted_at IS NULL`, schema)
	args := []interface{}{}
	if stores != nil {
		args = append(args, pq.Array(stores))
		sqlq += fmt.Sprintf(" AND c.data->>'location' = ANY($%d)", len(args))
	}
	if q = strings.TrimSpace(q); q != "" {
		like := "%" + strings.ToLower(q) + "%"
		phone := ""
		if p := NormalizePhone(q, TenantPhoneCountry(tenantID)); p.Valid {
			phone = p.National
		}
		args = append(args, like, phone)
		sqlq += fmt.Sprintf(` AND (lower(c.id) LIKE $%[1]d OR lower(COALESCE(c.data->>'bill_number', '')) LIKE $%[1]d
			OR lower(COALESCE(cu.data->>'name', '')) LIKE $%[1]d OR ($%[2]d <> '' AND cu.data->>'phone' = $%[2]d))`, len(args)-1, len(args))
	}
	if date = strings.TrimSpace(date); date != "" {
		args = append(args, date)
		sqlq += fmt.Sprintf(" AND c.created_at::date = $%d::date", len(args))
	}
	sqlq += " ORDER BY c.created_at DESC LIMIT 50"
	rows, err := db.DB.Query(sqlq, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []StoreBill{}
	for rows.Next() {
		var b StoreBill
		if err := rows.Scan(&b.CartNumber, &b.BillNumber, &b.Store, &b.Date, &b.Customer, &b.Phone, &b.Total); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}
