package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Stage 148.23: customer-vendor set-off.
//
// A customer that is also a vendor owes us on invoices and is owed by us on
// bills. A PartyPair says "these two are one business" - suggested when the PAN
// or GSTIN matches, created only by a person, never by the suggestion. A
// SetOffVoucher between the pair, once approved (maker-checker), posts two legs
// through the clearing account the admin picked (finance.setoff_clearing_account):
//
//	customer side: Dr clearing / Cr 1300   (against the customer's oldest open invoices)
//	vendor side:   Dr 2100+1500 / Cr clearing (against the vendor's oldest open bills)
//
// Each leg is posted under the invoice or bill it settles, with the settlement
// recorded on that document, so both open balances fall and both party ledgers
// show it exactly as a receipt or a payment would. The amount cannot exceed
// either balance. A reversal posts the mirror legs on a date the user gives and
// gives both balances back. No account row is written by the migration: the
// clearing account is a setting.

const (
	PartyPairDoctype     = "PartyPair"
	SetOffVoucherDoctype = "SetOffVoucher"

	setOffPairCode     = "FIN-0973" // a pair that cannot be made
	setOffInputCode    = "FIN-0974" // a voucher with bad input
	setOffClearingCode = "FIN-0975" // no usable clearing account
	setOffCapCode      = "FIN-0976" // more than a balance holds
	setOffReverseCode  = "FIN-0977" // a reversal that cannot be made
	setOffForgedCode   = "FIN-0978" // a typed-in or edited-after-approval voucher

	settlementKindSetOff         = "Set-off"
	settlementKindSetOffReversed = "Set-off reversed"
)

type setOffRows interface {
	Query(query string, args ...interface{}) (*sql.Rows, error)
	QueryRow(query string, args ...interface{}) *sql.Row
}

func setOffErr(code, format string, args ...interface{}) error {
	return &ValidationError{Code: code, Message: fmt.Sprintf(format, args...)}
}

// setOffParty finds a Customer or Vendor row by id or code.
func setOffParty(q docQuerier, schema, doctype, ref string) (id string, data map[string]interface{}, ok bool) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", nil, false
	}
	var raw []byte
	err := q.QueryRow(fmt.Sprintf(`SELECT id, data FROM %s.documents
		WHERE doctype = $2 AND deleted_at IS NULL AND (id = $1 OR data->>'code' = $1)
		ORDER BY CASE WHEN id = $1 THEN 0 ELSE 1 END LIMIT 1`, schema), ref, doctype).Scan(&id, &raw)
	if err != nil || json.Unmarshal(raw, &data) != nil {
		return "", nil, false
	}
	return id, data, true
}

// SetOffEnabled: the Finance page shows its set-off links once a clearing account is picked.
func SetOffEnabled(tenantID string) bool {
	return strings.TrimSpace(GetSettingString(tenantID, SettingSetOffClearingAccount)) != ""
}

// partyTaxIDs are the PAN and GSTIN a party row carries; a PAN is also the
// middle ten characters of a GSTIN.
func partyTaxIDs(data map[string]interface{}) (pan, gstin string) {
	pan = strings.ToUpper(strings.TrimSpace(strField(data, "pan")))
	gstin = strings.ToUpper(strings.TrimSpace(strField(data, "gstin")))
	if pan == "" && len(gstin) == 15 {
		pan = gstin[2:12]
	}
	return pan, gstin
}

// partyRefs are the values an invoice or bill may name a party by.
func partyRefs(id string, data map[string]interface{}) []string {
	refs := []string{id}
	if c := strings.TrimSpace(strField(data, "code")); c != "" && c != id {
		refs = append(refs, c)
	}
	return refs
}

// PairSuggestion is a customer and a vendor that look like one business.
type PairSuggestion struct {
	CustomerID   string `json:"customer_id"`
	CustomerName string `json:"customer_name"`
	VendorID     string `json:"vendor_id"`
	VendorName   string `json:"vendor_name"`
	Basis        string `json:"basis"` // "GSTIN" or "PAN"
	TaxID        string `json:"tax_id"`
}

// PartyPairSuggestions lists customers and vendors sharing a GSTIN or PAN that
// are not already paired. It writes nothing: a pair is made by a person.
func PartyPairSuggestions(tenantID string) ([]PairSuggestion, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	type party struct {
		id, name, pan, gstin string
	}
	load := func(doctype string) ([]party, error) {
		rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, data FROM %s.documents WHERE doctype = $1 AND deleted_at IS NULL ORDER BY id`, schema), doctype)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []party
		for rows.Next() {
			var id string
			var raw []byte
			var d map[string]interface{}
			if err := rows.Scan(&id, &raw); err != nil {
				return nil, err
			}
			if json.Unmarshal(raw, &d) != nil {
				continue
			}
			pan, gstin := partyTaxIDs(d)
			if pan == "" {
				continue
			}
			out = append(out, party{id, firstNonEmpty(strField(d, "name"), id), pan, gstin})
		}
		return out, rows.Err()
	}
	customers, err := load("Customer")
	if err != nil {
		return nil, err
	}
	vendors, err := load("Vendor")
	if err != nil {
		return nil, err
	}
	paired := map[string]bool{}
	prows, err := db.DB.Query(fmt.Sprintf(`SELECT COALESCE(data->>'customer_id',''), COALESCE(data->>'vendor_id','') FROM %s.documents
		WHERE doctype = $1 AND deleted_at IS NULL AND COALESCE(data->>'status','Active') = 'Active'`, schema), PartyPairDoctype)
	if err != nil {
		return nil, err
	}
	for prows.Next() {
		var c, v string
		if err := prows.Scan(&c, &v); err == nil {
			paired[c+"|"+v] = true
		}
	}
	prows.Close()
	out := []PairSuggestion{}
	for _, c := range customers {
		for _, v := range vendors {
			if c.pan != v.pan || paired[c.id+"|"+v.id] {
				continue
			}
			basis, tax := "PAN", c.pan
			if c.gstin != "" && c.gstin == v.gstin {
				basis, tax = "GSTIN", c.gstin
			}
			out = append(out, PairSuggestion{c.id, c.name, v.id, v.name, basis, tax})
		}
	}
	return out, nil
}

// validatePartyPair is the save check for a PartyPair: both parties exist and
// no other active pair already joins them or uses either as its customer or
// vendor side. It stamps the parties' ids and the basis they matched on.
func validatePartyPair(tenantID, docID string, priorData, payload map[string]interface{}) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	cid, cdata, cok := setOffParty(db.DB, schema, "Customer", strField(payload, "customer_id"))
	vid, vdata, vok := setOffParty(db.DB, schema, "Vendor", strField(payload, "vendor_id"))
	if !cok {
		return setOffErr(setOffPairCode, "choose a customer from the list")
	}
	if !vok {
		return setOffErr(setOffPairCode, "choose a vendor from the list")
	}
	status := strings.TrimSpace(strField(payload, "status"))
	if status == "" {
		status = "Active"
	}
	if status != "Active" && status != "Ended" {
		return setOffErr(setOffPairCode, "a pair is Active or Ended")
	}
	if priorData != nil && (strField(priorData, "customer_id") != cid || strField(priorData, "vendor_id") != vid) {
		return setOffErr(setOffPairCode, "a pair's customer and vendor cannot be changed - end it and make a new one")
	}
	payload["customer_id"], payload["vendor_id"], payload["status"] = cid, vid, status
	cpan, cg := partyTaxIDs(cdata)
	vpan, vg := partyTaxIDs(vdata)
	switch {
	case cg != "" && cg == vg:
		payload["basis"] = "GSTIN"
	case cpan != "" && cpan == vpan:
		payload["basis"] = "PAN"
	default:
		// A pair of two names that share no tax id is allowed (a person decides),
		// but it says so.
		payload["basis"] = "Chosen by user"
	}
	if status == "Active" {
		var n int
		err := db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.documents WHERE doctype = $1 AND deleted_at IS NULL AND id <> $2
			AND COALESCE(data->>'status','Active') = 'Active' AND (data->>'customer_id' = $3 OR data->>'vendor_id' = $4)`, schema),
			PartyPairDoctype, docID, cid, vid).Scan(&n)
		if err != nil {
			return err
		}
		if n > 0 {
			return setOffErr(setOffPairCode, "that customer or vendor is already in an active pair")
		}
	}
	return nil
}

// ---- open balances -------------------------------------------------------

type setOffOpen struct {
	DocType, ID string
	Date        string
	Paise       int64
}

func sortOpen(open []setOffOpen) {
	sort.SliceStable(open, func(i, j int) bool {
		if open[i].Date != open[j].Date {
			return open[i].Date < open[j].Date
		}
		return open[i].ID < open[j].ID
	})
}

// customerOpenInvoices are the customer's posted rupee invoices with a balance.
func customerOpenInvoices(q setOffRows, tenantID, schema string, refs []string) ([]setOffOpen, error) {
	rows, err := q.Query(fmt.Sprintf(`SELECT id, data FROM %s.documents WHERE doctype = 'SalesInvoice' AND status = 'Approved' AND deleted_at IS NULL
		AND (data->>'customer' = ANY($1::text[]) OR data->>'customer_id' = ANY($1::text[]))`, schema), pqStringArray(refs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []setOffOpen
	for rows.Next() {
		var id string
		var raw []byte
		var data map[string]interface{}
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, err
		}
		if json.Unmarshal(raw, &data) != nil {
			continue
		}
		position := documentFXPosition(tenantID, data, "total_amount")
		if position.Foreign {
			continue
		}
		entries, err := readSettlementEntries(data, "settlements")
		if err != nil {
			continue
		}
		if p := int64(position.CarryingAmount)*100 - sumSettledPaise(entries); p > 0 {
			out = append(out, setOffOpen{"SalesInvoice", id, firstNonEmpty(strField(data, "invoice_date"), "9999"), p})
		}
	}
	sortOpen(out)
	return out, rows.Err()
}

// vendorOpenBills are the vendor's Matched rupee bills with a balance.
func vendorOpenBills(q setOffRows, tenantID, schema string, refs []string) ([]setOffOpen, error) {
	rows, err := q.Query(fmt.Sprintf(`SELECT id, data FROM %s.documents WHERE doctype = 'VendorInvoice' AND status = 'Matched' AND deleted_at IS NULL
		AND (data->>'vendor_id' = ANY($1::text[]) OR data->>'vendor' = ANY($1::text[]))`, schema), pqStringArray(refs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []setOffOpen
	for rows.Next() {
		var id string
		var raw []byte
		var data map[string]interface{}
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, err
		}
		if json.Unmarshal(raw, &data) != nil {
			continue
		}
		position := documentFXPosition(tenantID, data, "invoice_amount")
		if position.Foreign {
			continue
		}
		if _, total := vendorOpenLegs(data, position); total > 0 {
			out = append(out, setOffOpen{"VendorInvoice", id, firstNonEmpty(strField(data, "invoice_date"), strField(data, "bill_date"), "9999"), total})
		}
	}
	sortOpen(out)
	return out, rows.Err()
}

func sumOpen(open []setOffOpen) (n int64) {
	for _, o := range open {
		n += o.Paise
	}
	return n
}

// SetOffBalances is what each side of a pair owes.
type SetOffBalances struct {
	PairID        string  `json:"pair_id"`
	CustomerID    string  `json:"customer_id"`
	CustomerName  string  `json:"customer_name"`
	VendorID      string  `json:"vendor_id"`
	VendorName    string  `json:"vendor_name"`
	CustomerOwes  float64 `json:"customer_owes"`
	WeOweVendor   float64 `json:"we_owe_vendor"`
	MaxSetOff     float64 `json:"max_set_off"`
	CustomerCount int     `json:"customer_invoices"`
	VendorCount   int     `json:"vendor_bills"`
}

type setOffSides struct {
	pairID, cid, vid, cname, vname string
	cOpen, vOpen                   []setOffOpen
}

func loadSetOffSides(q setOffRows, tenantID, schema, pairRef string) (*setOffSides, error) {
	_, pair, ok := setOffParty(q, schema, PartyPairDoctype, pairRef)
	if !ok {
		return nil, setOffErr(setOffInputCode, "choose the pair")
	}
	if st := strField(pair, "status"); st != "" && st != "Active" {
		return nil, setOffErr(setOffInputCode, "that pair has ended")
	}
	cid, cdata, cok := setOffParty(q, schema, "Customer", strField(pair, "customer_id"))
	vid, vdata, vok := setOffParty(q, schema, "Vendor", strField(pair, "vendor_id"))
	if !cok || !vok {
		return nil, setOffErr(setOffInputCode, "the pair's customer or vendor is no longer there")
	}
	s := &setOffSides{pairID: pairRef, cid: cid, vid: vid, cname: firstNonEmpty(strField(cdata, "name"), cid), vname: firstNonEmpty(strField(vdata, "name"), vid)}
	var err error
	if s.cOpen, err = customerOpenInvoices(q, tenantID, schema, partyRefs(cid, cdata)); err != nil {
		return nil, err
	}
	if s.vOpen, err = vendorOpenBills(q, tenantID, schema, partyRefs(vid, vdata)); err != nil {
		return nil, err
	}
	return s, nil
}

// GetSetOffBalances reads both open balances for a pair.
func GetSetOffBalances(tenantID, pairID string) (*SetOffBalances, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	s, err := loadSetOffSides(db.DB, tenantID, schema, pairID)
	if err != nil {
		return nil, err
	}
	c, v := sumOpen(s.cOpen), sumOpen(s.vOpen)
	m := c
	if v < m {
		m = v
	}
	return &SetOffBalances{PairID: pairID, CustomerID: s.cid, CustomerName: s.cname, VendorID: s.vid, VendorName: s.vname,
		CustomerOwes: PaiseToRupees(c), WeOweVendor: PaiseToRupees(v), MaxSetOff: PaiseToRupees(m), CustomerCount: len(s.cOpen), VendorCount: len(s.vOpen)}, nil
}

// setOffClearingAccount is the admin's clearing account, checked to exist and
// not to be one of the party control accounts.
func setOffClearingAccount(q docQuerier, schema string) (string, error) {
	acct := strings.TrimSpace(GetSettingStringForSchema(schema, SettingSetOffClearingAccount))
	if acct == "" {
		return "", setOffErr(setOffClearingCode, "choose the set-off clearing account in Settings first")
	}
	if acct == "1300" || acct == "2100" || acct == "1500" {
		return "", setOffErr(setOffClearingCode, "the clearing account cannot be a receivable or payable account")
	}
	var blocked bool
	if err := q.QueryRow(fmt.Sprintf(`SELECT blocked FROM %s.gl_accounts WHERE account_code = $1`, schema), acct).Scan(&blocked); err != nil {
		return "", setOffErr(setOffClearingCode, "the set-off clearing account %s is not in the chart of accounts", acct)
	}
	if blocked {
		return "", setOffErr(setOffClearingCode, "the set-off clearing account %s is blocked", acct)
	}
	return acct, nil
}

// ---- save check ----------------------------------------------------------

var setOffServerFields = []string{"posted_at", "posted_by", "reversed_on", "reversed_by", "allocations", "post_error", "clearing_account"}

// validateSetOffVoucher is the save check for a SetOffVoucher. A voucher is
// saved as a Draft only; sending it for approval and the decision go through
// the approval engine, and posting happens on approval. Everything the system
// sets is taken from the stored voucher (or dropped on a create), so a generic
// save cannot forge it.
func validateSetOffVoucher(tenantID, docID, priorStatus string, priorData, payload map[string]interface{}) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	if priorData != nil && priorStatus != "Draft" {
		return setOffErr(setOffForgedCode, "a set-off that has been sent for approval cannot be edited")
	}
	if st := strings.TrimSpace(strField(payload, "status")); st != "" && st != "Draft" {
		return setOffErr(setOffForgedCode, "a set-off is approved through approval, not typed in")
	}
	payload["status"] = "Draft"
	for _, k := range setOffServerFields {
		if priorData != nil {
			if v, ok := priorData[k]; ok {
				payload[k] = v
				continue
			}
		}
		delete(payload, k)
	}
	s, err := loadSetOffSides(db.DB, tenantID, schema, strField(payload, "pair_id"))
	if err != nil {
		return err
	}
	amount := numFromInterface(payload["amount"])
	pay := RupeesToPaise(amount)
	if pay <= 0 {
		return setOffErr(setOffInputCode, "enter an amount above zero")
	}
	if PaiseToRupees(pay) != amount {
		return setOffErr(setOffInputCode, "an amount has two decimals at most")
	}
	c, v := sumOpen(s.cOpen), sumOpen(s.vOpen)
	if pay > c {
		return setOffErr(setOffCapCode, "%.2f is more than %s owes (%.2f)", amount, s.cname, PaiseToRupees(c))
	}
	if pay > v {
		return setOffErr(setOffCapCode, "%.2f is more than we owe %s (%.2f)", amount, s.vname, PaiseToRupees(v))
	}
	if _, err := setOffClearingAccount(db.DB, schema); err != nil {
		return err
	}
	date := strings.TrimSpace(strField(payload, "voucher_date"))
	if date == "" {
		date = BusinessToday()
	}
	if _, err := time.Parse("2006-01-02", date); err != nil {
		return setOffErr(setOffInputCode, "enter the date as a day")
	}
	payload["voucher_date"] = date
	if rev := strings.TrimSpace(strField(payload, "reversal_date")); rev != "" {
		if _, err := time.Parse("2006-01-02", rev); err != nil {
			return setOffErr(setOffInputCode, "enter the reversal date as a day")
		}
		if rev < date {
			return setOffErr(setOffInputCode, "the reversal date cannot be before the set-off date")
		}
	}
	payload["customer_id"], payload["vendor_id"] = s.cid, s.vid
	payload["customer_balance"], payload["vendor_balance"] = PaiseToRupees(c), PaiseToRupees(v)
	return nil
}

func init() {
	// Sending for approval re-checks the caps and the clearing account against
	// today's balances.
	RegisterApprovalSubmitCheck(SetOffVoucherDoctype, func(tenantID, docID string, data map[string]interface{}) error {
		schema, err := db.GetTenantSchema(tenantID)
		if err != nil {
			return err
		}
		if _, err := setOffClearingAccount(db.DB, schema); err != nil {
			return err
		}
		s, err := loadSetOffSides(db.DB, tenantID, schema, strField(data, "pair_id"))
		if err != nil {
			return err
		}
		pay := RupeesToPaise(numFromInterface(data["amount"]))
		if pay > sumOpen(s.cOpen) || pay > sumOpen(s.vOpen) {
			return setOffErr(setOffCapCode, "the balances are now smaller than this set-off")
		}
		return nil
	})
	RegisterApprovalDecisionHook(SetOffVoucherDoctype, true, func(tenantID, docID, decision, actor string) {
		if decision != "Approved" {
			return
		}
		if err := postSetOff(tenantID, docID, actor); err != nil {
			LogSystemError(tenantID, "", "ERROR", "set_off", fmt.Sprintf("approved set-off %s was not posted: %v", docID, err), "")
			failSetOff(tenantID, docID, err)
		}
	})
}

// failSetOff turns an approved voucher that could not post into a Rejected one
// carrying the reason, so it is never left looking posted.
func failSetOff(tenantID, docID string, cause error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return
	}
	_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET status = 'Rejected',
		data = jsonb_set(jsonb_set(data, '{status}', '"Rejected"'), '{post_error}', to_jsonb($3::text)), updated_at = CURRENT_TIMESTAMP
		WHERE doctype = $1 AND id = $2 AND status = 'Approved' AND data->>'posted_at' IS NULL`, schema), SetOffVoucherDoctype, docID, cause.Error())
}

// ---- posting -------------------------------------------------------------

type setOffAllocation struct {
	Side    string             `json:"side"` // "customer" or "vendor"
	DocType string             `json:"doctype"`
	ID      string             `json:"id"`
	Amount  float64            `json:"amount"`
	Key     string             `json:"key"`
	Legs    map[string]float64 `json:"legs,omitempty"`
}

func readSetOffAllocations(data map[string]interface{}) []setOffAllocation {
	raw, err := json.Marshal(data["allocations"])
	if err != nil {
		return nil
	}
	var out []setOffAllocation
	_ = json.Unmarshal(raw, &out)
	return out
}

func loadSetOffTx(tx *sql.Tx, schema, id string) (map[string]interface{}, string, error) {
	var raw []byte
	var status string
	if err := tx.QueryRow(fmt.Sprintf(`SELECT data, status FROM %s.documents WHERE doctype = $1 AND id = $2 AND deleted_at IS NULL FOR UPDATE`, schema), SetOffVoucherDoctype, id).Scan(&raw, &status); err != nil {
		return nil, "", setOffErr(setOffInputCode, "that set-off was not found")
	}
	var data map[string]interface{}
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, "", err
	}
	return data, status, nil
}

func saveSetOffTx(tx *sql.Tx, schema, id, status string, data map[string]interface{}) error {
	data["status"] = status
	raw, _ := json.Marshal(data)
	_, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $1, status = $2, updated_at = CURRENT_TIMESTAMP WHERE doctype = $3 AND id = $4`, schema), raw, status, SetOffVoucherDoctype, id)
	return err
}

// postSetOff books an approved voucher in one transaction: both legs, the
// settlement lines on the invoices and bills it clears, and the voucher's own
// record of what it cleared. Running it twice posts once.
func postSetOff(tenantID, docID, actor string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	v, status, err := loadSetOffTx(tx, schema, docID)
	if err != nil {
		return err
	}
	if status != "Approved" || strField(v, "posted_at") != "" {
		return nil
	}
	clearing, err := setOffClearingAccount(tx, schema)
	if err != nil {
		return err
	}
	s, err := loadSetOffSides(tx, tenantID, schema, strField(v, "pair_id"))
	if err != nil {
		return err
	}
	total := RupeesToPaise(numFromInterface(v["amount"]))
	if total <= 0 || total > sumOpen(s.cOpen) || total > sumOpen(s.vOpen) {
		return setOffErr(setOffCapCode, "the balances are now smaller than this set-off")
	}
	date := firstNonEmpty(strField(v, "voucher_date"), BusinessToday())
	var allocs []setOffAllocation

	// Customer side: oldest invoices first.
	left := total
	for _, o := range s.cOpen {
		if left <= 0 {
			break
		}
		take := o.Paise
		if take > left {
			take = left
		}
		key := fmt.Sprintf("SalesInvoice:%s:SETOFF:%s", o.ID, docID)
		if err := PostDoubleEntryTx(tx, tenantID, schema, "SalesInvoice", o.ID, map[string]int64{clearing: take}, map[string]int64{"1300": take}, date, key); err != nil {
			return postingError(err, "set-off not posted")
		}
		applied, err := noteInvoiceSettlementTx(tx, tenantID, schema, invoiceSettlement{InvoiceID: o.ID, Ref: docID, Kind: settlementKindSetOff,
			User: actor, PostingKey: key, Customer: "", Date: date, Paise: take, Cap: false})
		if err != nil {
			return err
		}
		if applied != take {
			return setOffErr(setOffCapCode, "invoice %s changed while posting", o.ID)
		}
		allocs = append(allocs, setOffAllocation{Side: "customer", DocType: "SalesInvoice", ID: o.ID, Amount: PaiseToRupees(take), Key: key})
		left -= take
	}

	// Vendor side: oldest bills first, split between 2100 and 1500 as a payment is.
	left = total
	for _, o := range s.vOpen {
		if left <= 0 {
			break
		}
		var raw []byte
		if err := tx.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents WHERE doctype = 'VendorInvoice' AND id = $1 AND status = 'Matched' FOR UPDATE`, schema), o.ID).Scan(&raw); err != nil {
			return setOffErr(setOffCapCode, "bill %s changed while posting", o.ID)
		}
		var bill map[string]interface{}
		if err := json.Unmarshal(raw, &bill); err != nil {
			return err
		}
		position := documentFXPosition(tenantID, bill, "invoice_amount")
		open, outstanding := vendorOpenLegs(bill, position)
		take := outstanding
		if take > left {
			take = left
		}
		if take <= 0 {
			continue
		}
		entries, err := readSettlementEntries(bill, "payments")
		if err != nil {
			return err
		}
		legs := splitVendorPayment(open, outstanding, take)
		key := fmt.Sprintf("VendorInvoice:%s:SETOFF:%s", o.ID, docID)
		if err := PostDoubleEntryTx(tx, tenantID, schema, "VendorInvoice", o.ID, legs, map[string]int64{clearing: take}, date, key); err != nil {
			return postingError(err, "set-off not posted")
		}
		remaining := noteVendorPayment(bill, entries, settlementKindSetOff, docID, key, actor, date, legs, position, PaiseToRupees(take))
		newStatus := "Matched"
		if remaining <= 0 {
			newStatus = "Paid"
		}
		bill["status"] = newStatus
		bb, _ := json.Marshal(bill)
		if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $1, status = $2, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'VendorInvoice' AND id = $3`, schema), bb, newStatus, o.ID); err != nil {
			return err
		}
		legRupees := map[string]float64{}
		for k, p := range legs {
			legRupees[k] = PaiseToRupees(p)
		}
		allocs = append(allocs, setOffAllocation{Side: "vendor", DocType: "VendorInvoice", ID: o.ID, Amount: PaiseToRupees(take), Key: key, Legs: legRupees})
		left -= take
	}
	if left != 0 {
		return setOffErr(setOffCapCode, "the balances are now smaller than this set-off")
	}
	v["allocations"] = allocs
	v["clearing_account"] = clearing
	v["customer_balance"], v["vendor_balance"] = PaiseToRupees(sumOpen(s.cOpen)), PaiseToRupees(sumOpen(s.vOpen))
	v["posted_at"], v["posted_by"] = time.Now().UTC().Format(time.RFC3339), actor
	if err := saveSetOffTx(tx, schema, docID, "Approved", v); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	LogAuditEvent(tenantID, actor, "SETOFF_POSTED", "SUCCESS", fmt.Sprintf("Set-off %s of %.2f between %s and %s posted through %s", docID, PaiseToRupees(total), s.cname, s.vname, clearing))
	return nil
}

// ---- reversal ------------------------------------------------------------

// ReverseSetOff posts the mirror of a posted voucher on the given day (today if
// blank) and gives both balances back. It can be done once.
func ReverseSetOff(tenantID, userID, voucherID, date string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	date = strings.TrimSpace(date)
	if date == "" {
		date = BusinessToday()
	}
	if _, err := time.Parse("2006-01-02", date); err != nil {
		return setOffErr(setOffReverseCode, "enter the date as a day")
	}
	if date > BusinessToday() {
		return setOffErr(setOffReverseCode, "the reversal cannot be dated in the future")
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	v, status, err := loadSetOffTx(tx, schema, voucherID)
	if err != nil {
		return err
	}
	if status == "Reversed" {
		return setOffErr(setOffReverseCode, "that set-off is already reversed")
	}
	if status != "Approved" || strField(v, "posted_at") == "" {
		return setOffErr(setOffReverseCode, "only a posted set-off can be reversed")
	}
	if d := strField(v, "voucher_date"); d != "" && date < d {
		return setOffErr(setOffReverseCode, "the reversal cannot be dated before the set-off")
	}
	clearing := strField(v, "clearing_account")
	for _, a := range readSetOffAllocations(v) {
		p := RupeesToPaise(a.Amount)
		key := strings.Replace(a.Key, ":SETOFF:", ":SETOFFREV:", 1)
		if a.Side == "customer" {
			if err := PostDoubleEntryTx(tx, tenantID, schema, a.DocType, a.ID, map[string]int64{"1300": p}, map[string]int64{clearing: p}, date, key); err != nil {
				return postingError(err, "set-off not reversed")
			}
			if err := reopenSalesInvoiceTx(tx, tenantID, schema, a.ID, voucherID, userID, key, date, p); err != nil {
				return err
			}
			continue
		}
		legs := map[string]int64{}
		for k, r := range a.Legs {
			legs[k] = RupeesToPaise(r)
		}
		if err := PostDoubleEntryTx(tx, tenantID, schema, a.DocType, a.ID, map[string]int64{clearing: p}, legs, date, key); err != nil {
			return postingError(err, "set-off not reversed")
		}
		if err := reopenVendorBillTx(tx, schema, a.ID, voucherID, userID, key, date, legs); err != nil {
			return err
		}
	}
	v["reversed_on"], v["reversed_by"] = date, userID
	v["reversal_date"] = date
	if err := saveSetOffTx(tx, schema, voucherID, "Reversed", v); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	LogAuditEvent(tenantID, userID, "SETOFF_REVERSED", "SUCCESS", fmt.Sprintf("Set-off %s reversed on %s", voucherID, date))
	return nil
}

func reopenSalesInvoiceTx(tx *sql.Tx, tenantID, schema, invoiceID, voucherID, userID, key, date string, paise int64) error {
	var raw []byte
	if err := tx.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents WHERE doctype = 'SalesInvoice' AND id = $1 FOR UPDATE`, schema), invoiceID).Scan(&raw); err != nil {
		return err
	}
	var data map[string]interface{}
	if err := json.Unmarshal(raw, &data); err != nil {
		return err
	}
	entries, err := readSettlementEntries(data, "settlements")
	if err != nil {
		return err
	}
	entries = append(entries, SettlementEntry{Reference: voucherID, Kind: settlementKindSetOffReversed, Amount: -PaiseToRupees(paise),
		Date: date, By: userID, PostingKey: key, RecordedAt: time.Now().UTC().Format(time.RFC3339)})
	already := sumSettledPaise(entries)
	total := int64(documentFXPosition(tenantID, data, "total_amount").CarryingAmount) * 100 // the whole-rupee figure the ledger carries
	data["settlements"] = entries
	data["settled_amount"] = PaiseToRupees(already)
	data["outstanding_amount"] = PaiseToRupees(total - already)
	status := "Approved"
	data["status"] = status
	bb, _ := json.Marshal(data)
	_, err = tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $1, status = $2, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'SalesInvoice' AND id = $3`, schema), bb, status, invoiceID)
	return err
}

func reopenVendorBillTx(tx *sql.Tx, schema, billID, voucherID, userID, key, date string, legs map[string]int64) error {
	var raw []byte
	if err := tx.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents WHERE doctype = 'VendorInvoice' AND id = $1 FOR UPDATE`, schema), billID).Scan(&raw); err != nil {
		return err
	}
	var bill map[string]interface{}
	if err := json.Unmarshal(raw, &bill); err != nil {
		return err
	}
	entries, err := readSettlementEntries(bill, "payments")
	if err != nil {
		return err
	}
	basis := paiseLegs(bill["payment_basis_paise"])
	cleared := paiseLegs(bill["payment_cleared_paise"])
	neg := map[string]float64{}
	var sum int64
	for k, p := range legs {
		cleared[k] -= p
		if cleared[k] < 0 {
			cleared[k] = 0
		}
		neg[k] = -PaiseToRupees(p)
		sum += p
	}
	paid := sumLegs(cleared)
	outstanding := sumLegs(basis) - paid
	bill["payment_cleared_paise"] = cleared
	bill["paid_amount"] = PaiseToRupees(paid)
	bill["outstanding_amount"] = PaiseToRupees(outstanding)
	bill["payments"] = append(entries, SettlementEntry{Reference: voucherID, Kind: settlementKindSetOffReversed, Amount: -PaiseToRupees(sum), Legs: neg,
		Date: date, By: userID, PostingKey: key, RecordedAt: time.Now().UTC().Format(time.RFC3339)})
	bill["status"] = "Matched"
	bb, _ := json.Marshal(bill)
	_, err = tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $1, status = 'Matched', updated_at = CURRENT_TIMESTAMP WHERE doctype = 'VendorInvoice' AND id = $2`, schema), bb, billID)
	return err
}

// ---- statement -----------------------------------------------------------

// SetOffStatementLine is one invoice or bill a set-off cleared.
type SetOffStatementLine struct {
	DocType string  `json:"doctype"`
	ID      string  `json:"id"`
	Amount  float64 `json:"amount"`
}

// SetOffStatement is the printable record of a set-off.
type SetOffStatement struct {
	ID              string                `json:"id"`
	Status          string                `json:"status"`
	Date            string                `json:"date"`
	ReversedOn      string                `json:"reversed_on,omitempty"`
	Amount          float64               `json:"amount"`
	CustomerName    string                `json:"customer_name"`
	VendorName      string                `json:"vendor_name"`
	ClearingAccount string                `json:"clearing_account"`
	CustomerBefore  float64               `json:"customer_before"`
	CustomerAfter   float64               `json:"customer_after"`
	VendorBefore    float64               `json:"vendor_before"`
	VendorAfter     float64               `json:"vendor_after"`
	Invoices        []SetOffStatementLine `json:"invoices"`
	Bills           []SetOffStatementLine `json:"bills"`
	PostedBy        string                `json:"posted_by"`
}

// GetSetOffStatement reads the statement of a posted (or reversed) voucher. The
// balances are the ones at posting, kept on the voucher.
func GetSetOffStatement(tenantID, voucherID string) (*SetOffStatement, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	var raw []byte
	var status string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT data, status FROM %s.documents WHERE doctype = $1 AND id = $2 AND deleted_at IS NULL`, schema), SetOffVoucherDoctype, voucherID).Scan(&raw, &status); err != nil {
		return nil, setOffErr(setOffInputCode, "that set-off was not found")
	}
	var v map[string]interface{}
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	if strField(v, "posted_at") == "" {
		return nil, setOffErr(setOffInputCode, "that set-off is not posted yet")
	}
	_, cdata, _ := setOffParty(db.DB, schema, "Customer", strField(v, "customer_id"))
	_, vdata, _ := setOffParty(db.DB, schema, "Vendor", strField(v, "vendor_id"))
	amount := numFromInterface(v["amount"])
	cb, vb := numFromInterface(v["customer_balance"]), numFromInterface(v["vendor_balance"])
	st := &SetOffStatement{ID: voucherID, Status: status, Date: strField(v, "voucher_date"), ReversedOn: strField(v, "reversed_on"), Amount: amount,
		CustomerName: firstNonEmpty(strField(cdata, "name"), strField(v, "customer_id")), VendorName: firstNonEmpty(strField(vdata, "name"), strField(v, "vendor_id")),
		ClearingAccount: strField(v, "clearing_account"), CustomerBefore: cb, CustomerAfter: PaiseToRupees(RupeesToPaise(cb) - RupeesToPaise(amount)),
		VendorBefore: vb, VendorAfter: PaiseToRupees(RupeesToPaise(vb) - RupeesToPaise(amount)), PostedBy: strField(v, "posted_by"),
		Invoices: []SetOffStatementLine{}, Bills: []SetOffStatementLine{}}
	for _, a := range readSetOffAllocations(v) {
		line := SetOffStatementLine{DocType: a.DocType, ID: a.ID, Amount: a.Amount}
		if a.Side == "customer" {
			st.Invoices = append(st.Invoices, line)
		} else {
			st.Bills = append(st.Bills, line)
		}
	}
	return st, nil
}
