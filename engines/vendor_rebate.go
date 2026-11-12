package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// Stage 148.17: volume rebates from vendors.
//
// Off by default (procurement.vendor_rebates). On, the vendor agreement (a
// PriceListVersion with a vendor, 148.6) can carry a target with two tiers on
// the vendor's bills - on the gross taxable value, or net of debit notes. A
// claim is one vendor and one period: Calculated by the Calculate button from
// the bills of the period, then Pending Approval -> Approved through the
// maker-checker engine, then Settled by a debit note against the vendor.
//
//	approve: Dr 1820 Vendor Rebates Receivable / Cr 5150 Purchase Returns & Allowances
//	settle:  Dr 2100 / Cr 1820, as a Posted debit note (basis vendor_rebate)
//
// 5150 is where a debit note already credits, so a rebate lowers purchase cost
// in the same place a return does. The accrual is booked once, at approval; the
// debit note then moves the receivable onto the vendor's account. A claim that
// is Approved or Settled is never touched by a re-run.

const (
	SettingVendorRebates     = "procurement.vendor_rebates"
	VendorRebateClaimDoctype = "VendorRebateClaim"
	AccountRebateReceivable  = "1820"

	rebateCalculated = "Calculated"
	rebatePending    = "Pending Approval"
	rebateApproved   = "Approved"
	rebateRejected   = "Rejected"
	rebateSettled    = "Settled"

	vendorRebateOffCode    = "PURCHA-5020" // the setting is off
	vendorRebateInputCode  = "PURCHA-5021" // period or vendor not usable
	vendorRebateStateCode  = "PURCHA-5022" // claim not in a state for that step
	vendorRebateZeroCode   = "PURCHA-5023" // nothing to claim
	vendorRebateTargetCode = "PURCHA-5024" // tier setup on the agreement is not usable
)

func init() {
	RegisterSetting(SettingDefinition{
		Key: SettingVendorRebates, Module: "Procurement",
		Label: "Volume rebates from vendors", Type: SettingTypeBool, Default: "false",
		Description: "Vendor agreements can carry a purchase target with two rebate tiers; a claim is calculated per vendor and period, approved, then settled by a debit note.",
	})
	RegisterApprovalDecisionHook(VendorRebateClaimDoctype, false, func(tenantID, docID, decision, actor string) {
		if decision != "Approved" {
			return
		}
		if err := accrueVendorRebate(tenantID, docID, actor); err != nil {
			LogSystemError(tenantID, "", "ERROR", "vendor_rebate", fmt.Sprintf("approved claim %s not accrued (settle will retry): %v", docID, err), "")
		}
	})
}

// settingGatedFields / settingGatedDoctypes hide a field or a whole doctype
// until its setting is on, at the two places the app lists them.
var settingGatedFields = map[string]map[string]string{
	"PriceListVersion": {
		"rebate_basis": SettingVendorRebates, "rebate_tier1_target": SettingVendorRebates, "rebate_tier1_pct": SettingVendorRebates,
		"rebate_tier2_target": SettingVendorRebates, "rebate_tier2_pct": SettingVendorRebates,
	},
	// 148.38: the screens show these boxes only while the setting is on.
	"GRN": {
		"received_before_po": SettingGoodsBeforePO, "before_po_reason": SettingGoodsBeforePO,
		"before_po_due": SettingGoodsBeforePO, "retro_po_id": SettingGoodsBeforePO,
	},
	"PurchaseOrder": {"retro_grn_id": SettingGoodsBeforePO, "received_before_po": SettingGoodsBeforePO},
}

var settingGatedDoctypes = map[string]string{VendorRebateClaimDoctype: SettingVendorRebates}

func requireVendorRebates(tenantID string) error {
	if !GetSettingBool(tenantID, SettingVendorRebates) {
		return &ValidationError{Code: vendorRebateOffCode, Message: "Volume rebates are off."}
	}
	return nil
}

func rebateErr(code, format string, args ...interface{}) error {
	return &ValidationError{Code: code, Message: fmt.Sprintf(format, args...)}
}

// RebateTerms is a vendor agreement's target.
type RebateTerms struct {
	AgreementID string
	Basis       string // Gross or Net
	T1, T2      int64  // target in paise; T2 = 0 when there is no second tier
	P1, P2      float64
}

func rebateNum(data map[string]interface{}, key string) float64 {
	switch v := data[key].(type) {
	case string:
		f, _ := strconv.ParseFloat(strings.TrimSpace(v), 64)
		return f
	default:
		return numFromInterface(data[key])
	}
}

// rebateTermsFor reads the target off an agreement; ok is false when the
// agreement carries none.
func rebateTermsFor(id string, data map[string]interface{}) (RebateTerms, bool, error) {
	t := RebateTerms{AgreementID: id, Basis: strField(data, "rebate_basis"),
		T1: RupeesToPaise(rebateNum(data, "rebate_tier1_target")), P1: rebateNum(data, "rebate_tier1_pct"),
		T2: RupeesToPaise(rebateNum(data, "rebate_tier2_target")), P2: rebateNum(data, "rebate_tier2_pct")}
	if t.T1 <= 0 && t.T2 <= 0 && t.P1 <= 0 && t.P2 <= 0 {
		return t, false, nil
	}
	if t.Basis == "" {
		t.Basis = "Gross"
	}
	if t.Basis != "Gross" && t.Basis != "Net" {
		return t, false, rebateErr(vendorRebateTargetCode, "agreement %s: the rebate basis must be Gross or Net", id)
	}
	if t.T1 <= 0 || t.P1 <= 0 || t.P1 > 100 || t.P2 < 0 || t.P2 > 100 {
		return t, false, rebateErr(vendorRebateTargetCode, "agreement %s: tier 1 needs a target and a percentage up to 100", id)
	}
	if t.T2 > 0 && (t.T2 <= t.T1 || t.P2 <= t.P1) {
		return t, false, rebateErr(vendorRebateTargetCode, "agreement %s: tier 2 must have a higher target and a higher percentage than tier 1", id)
	}
	if t.T2 <= 0 {
		t.P2 = 0
	}
	return t, true, nil
}

// rebateFor is the tier reached and the rebate in paise: the percentage of the
// whole qualifying amount at the highest tier reached.
func (t RebateTerms) rebateFor(qualifying int64) (tier int, pct float64, paise int64) {
	switch {
	case t.T2 > 0 && qualifying >= t.T2:
		tier, pct = 2, t.P2
	case qualifying >= t.T1:
		tier, pct = 1, t.P1
	default:
		return 0, 0, 0
	}
	return tier, pct, int64(math.Round(float64(qualifying) * pct / 100))
}

// VendorRebateResult is one claim a Calculate run touched.
type VendorRebateResult struct {
	ID         string  `json:"id,omitempty"`
	Vendor     string  `json:"vendor_id"`
	Status     string  `json:"status"`
	Action     string  `json:"action"` // created, updated, unchanged, skipped
	Qualifying float64 `json:"qualifying_amount"`
	Tier       int     `json:"tier"`
	Rebate     float64 `json:"rebate_amount"`
	Note       string  `json:"note,omitempty"`
}

// CalculateVendorRebates works the claims for [from, to] for every vendor whose
// approved agreement carries a target (or one vendor). A Calculated or Rejected
// claim is brought up to date; an Approved or Settled one is left as it is.
func CalculateVendorRebates(tenantID, userID, from, to, vendorRef string) ([]VendorRebateResult, error) {
	if err := requireVendorRebates(tenantID); err != nil {
		return nil, err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	if !vendorAdvanceValidDate(from) || !vendorAdvanceValidDate(to) || from > to {
		return nil, rebateErr(vendorRebateInputCode, "the period needs a from and a to date (YYYY-MM-DD), from not after to")
	}
	only := ""
	if vendorRef = strings.TrimSpace(vendorRef); vendorRef != "" {
		id, _, ok := findVendorRow(db.DB, schema, vendorRef)
		if !ok {
			return nil, rebateErr(vendorRebateInputCode, "vendor %q was not found", vendorRef)
		}
		only = id
	}

	// the latest approved agreement with a target, per vendor
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, data FROM %s.documents WHERE doctype = 'PriceListVersion' AND status = 'Approved' AND COALESCE(data->>'vendor','') <> '' AND deleted_at IS NULL ORDER BY created_at ASC, id ASC`, schema))
	if err != nil {
		return nil, err
	}
	terms := map[string]RebateTerms{}
	var order []string
	var termErr error
	for rows.Next() {
		var id string
		var raw []byte
		if err := rows.Scan(&id, &raw); err != nil {
			rows.Close()
			return nil, err
		}
		var d map[string]interface{}
		_ = json.Unmarshal(raw, &d)
		vid, _, ok := findVendorRow(db.DB, schema, strField(d, "vendor"))
		if !ok || (only != "" && vid != only) {
			continue
		}
		t, has, terr := rebateTermsFor(id, d)
		if terr != nil {
			termErr = terr
			continue
		}
		if has {
			if _, seen := terms[vid]; !seen {
				order = append(order, vid)
			}
			terms[vid] = t
		}
	}
	rows.Close()
	if termErr != nil && len(terms) == 0 {
		return nil, termErr
	}

	taxable, notes, err := rebateVendorTotals(schema, from, to)
	if err != nil {
		return nil, err
	}
	var out []VendorRebateResult
	for _, vid := range order {
		t := terms[vid]
		qualifying := taxable[vid]
		if t.Basis == "Net" {
			qualifying -= notes[vid]
		}
		if qualifying < 0 {
			qualifying = 0
		}
		tier, pct, rebate := t.rebateFor(qualifying)
		res, err := saveRebateClaim(tenantID, schema, userID, vid, from, to, t, qualifying, tier, pct, rebate)
		if err != nil {
			return out, err
		}
		if res != nil {
			out = append(out, *res)
		}
	}
	return out, nil
}

// rebateVendorTotals sums each vendor's taxable bill value (Matched or Paid
// bills dated in the period) and its debit notes in the period, in paise, keyed
// by the vendor's own id. Rebate debit notes are left out of the net.
func rebateVendorTotals(schema, from, to string) (taxable, notes map[string]int64, err error) {
	taxable, notes = map[string]int64{}, map[string]int64{}
	cache := map[string]string{}
	canon := func(ref string) string {
		if v, ok := cache[ref]; ok {
			return v
		}
		id, _, ok := findVendorRow(db.DB, schema, ref)
		if !ok {
			id = ""
		}
		cache[ref] = id
		return id
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT COALESCE(NULLIF(data->>'vendor_id',''), data->>'vendor', ''),
		COALESCE(NULLIF(data->>'invoice_amount','')::numeric, 0), COALESCE(NULLIF(data->>'gst_amount','')::numeric, 0)
		FROM %s.documents WHERE doctype = 'VendorInvoice' AND status IN ('Matched','Paid') AND deleted_at IS NULL AND COALESCE(data->>'bill_kind','') <> 'Expense'
		AND COALESCE(NULLIF(data->>'invoice_date',''), public.erp_local_date(created_at)::text) BETWEEN $1 AND $2`, schema), from, to)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var ref string
		var amt, gst float64
		if err := rows.Scan(&ref, &amt, &gst); err != nil {
			rows.Close()
			return nil, nil, err
		}
		if v := canon(ref); v != "" {
			taxable[v] += RupeesToPaise(amt) - RupeesToPaise(gst)
		}
	}
	rows.Close()
	rows, err = db.DB.Query(fmt.Sprintf(`SELECT COALESCE(NULLIF(data->>'vendor_id',''), data->>'vendor', ''), COALESCE(NULLIF(data->>'amount','')::numeric, 0)
		FROM %s.documents WHERE doctype = 'DebitNote' AND status = 'Posted' AND deleted_at IS NULL AND COALESCE(data->>'basis','') <> 'vendor_rebate'
		AND public.erp_local_date(created_at)::text BETWEEN $1 AND $2`, schema), from, to)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var ref string
		var amt float64
		if err := rows.Scan(&ref, &amt); err != nil {
			return nil, nil, err
		}
		if v := canon(ref); v != "" {
			notes[v] += RupeesToPaise(amt)
		}
	}
	return taxable, notes, nil
}

func saveRebateClaim(tenantID, schema, userID, vendorID, from, to string, t RebateTerms, qualifying int64, tier int, pct float64, rebate int64) (*VendorRebateResult, error) {
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext($1))`, "vrebate:"+schema+":"+vendorID+":"+from+":"+to); err != nil {
		return nil, err
	}
	res := &VendorRebateResult{Vendor: vendorID, Qualifying: PaiseToRupees(qualifying), Tier: tier, Rebate: PaiseToRupees(rebate)}
	var id string
	var raw []byte
	var status string
	err = tx.QueryRow(fmt.Sprintf(`SELECT id, data, status FROM %s.documents WHERE doctype = 'VendorRebateClaim' AND data->>'vendor_id' = $1 AND data->>'period_from' = $2 AND data->>'period_to' = $3 AND deleted_at IS NULL FOR UPDATE`, schema), vendorID, from, to).Scan(&id, &raw, &status)
	switch {
	case err == sql.ErrNoRows:
		if rebate <= 0 {
			return nil, nil // below the first tier: nothing to claim, nothing filed
		}
		id = NewDocID("VRC")
		data := rebateClaimData(id, vendorID, from, to, t, qualifying, tier, pct, rebate, userID)
		b, _ := json.Marshal(data)
		if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'VendorRebateClaim', $2, $3, $4)`, schema), id, b, rebateCalculated, userID); err != nil {
			return nil, err
		}
		res.ID, res.Status, res.Action = id, rebateCalculated, "created"
	case err != nil:
		return nil, err
	default:
		res.ID, res.Status = id, status
		if status != rebateCalculated && status != rebateRejected {
			res.Action, res.Note = "skipped", "already "+status
			return res, nil // approved / settled / waiting for approval: never touched
		}
		var old map[string]interface{}
		_ = json.Unmarshal(raw, &old)
		if RupeesToPaise(numFromInterface(old["rebate_amount"])) == rebate && RupeesToPaise(numFromInterface(old["qualifying_amount"])) == qualifying && status == rebateCalculated {
			res.Action = "unchanged"
			return res, nil
		}
		data := rebateClaimData(id, vendorID, from, to, t, qualifying, tier, pct, rebate, userID)
		b, _ := json.Marshal(data)
		if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $1, status = $2, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'VendorRebateClaim' AND id = $3`, schema), b, rebateCalculated, id); err != nil {
			return nil, err
		}
		res.Status, res.Action = rebateCalculated, "updated"
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	LogAuditEvent(tenantID, userID, "VENDOR_REBATE_CALCULATED", "SUCCESS", fmt.Sprintf("Rebate claim %s for vendor %s, %s to %s: %.2f", id, vendorID, from, to, PaiseToRupees(rebate)))
	return res, nil
}

func rebateClaimData(id, vendorID, from, to string, t RebateTerms, qualifying int64, tier int, pct float64, rebate int64, userID string) map[string]interface{} {
	return map[string]interface{}{
		"code": id, "vendor_id": vendorID, "agreement_id": t.AgreementID, "basis": t.Basis,
		"period_from": from, "period_to": to, "qualifying_amount": PaiseToRupees(qualifying),
		"tier": float64(tier), "rebate_pct": pct, "rebate_amount": PaiseToRupees(rebate),
		"status": rebateCalculated, "calculated_by": userID,
	}
}

func loadRebateClaimTx(tx *sql.Tx, schema, id string) (map[string]interface{}, error) {
	var raw []byte
	if err := tx.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents WHERE doctype = 'VendorRebateClaim' AND id = $1 AND deleted_at IS NULL FOR UPDATE`, schema), id).Scan(&raw); err != nil {
		if err == sql.ErrNoRows {
			return nil, rebateErr(vendorRebateStateCode, "rebate claim %s was not found", id)
		}
		return nil, err
	}
	var d map[string]interface{}
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, err
	}
	return d, nil
}

func saveRebateClaimTx(tx *sql.Tx, schema, id string, d map[string]interface{}) error {
	b, _ := json.Marshal(d)
	_, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $1, status = $2, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'VendorRebateClaim' AND id = $3`, schema), b, strField(d, "status"), id)
	return err
}

// SubmitVendorRebateClaim sends a Calculated (or Rejected) claim to the
// maker-checker engine. The engine takes a Draft, so the claim is stood up as
// one for the instant of the call and put back if the submit is refused.
func SubmitVendorRebateClaim(tenantID, userID, role, claimID string) error {
	if err := requireVendorRebates(tenantID); err != nil {
		return err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var raw []byte
	var status string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT data, status FROM %s.documents WHERE doctype = 'VendorRebateClaim' AND id = $1 AND deleted_at IS NULL`, schema), claimID).Scan(&raw, &status); err != nil {
		return rebateErr(vendorRebateStateCode, "rebate claim %s was not found", claimID)
	}
	var d map[string]interface{}
	_ = json.Unmarshal(raw, &d)
	if status != rebateCalculated && status != rebateRejected {
		return rebateErr(vendorRebateStateCode, "rebate claim %s is %s - only a Calculated claim goes for approval", claimID, status)
	}
	if RupeesToPaise(numFromInterface(d["rebate_amount"])) <= 0 {
		return rebateErr(vendorRebateZeroCode, "rebate claim %s has nothing to claim", claimID)
	}
	if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET status = 'Draft' WHERE doctype = 'VendorRebateClaim' AND id = $1 AND status = $2`, schema), claimID, status); err != nil {
		return err
	}
	if err := SubmitForApproval(tenantID, VendorRebateClaimDoctype, claimID, userID, role); err != nil {
		_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET status = $2 WHERE doctype = 'VendorRebateClaim' AND id = $1 AND status = 'Draft'`, schema), claimID, status)
		return err
	}
	return nil
}

// accrueVendorRebate books Dr 1820 / Cr 5150 for an approved claim, once (the
// key makes a repeat a no-op, so the hook and Settle can both call it).
func accrueVendorRebate(tenantID, claimID, userID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	d, err := loadRebateClaimTx(tx, schema, claimID)
	if err != nil {
		return err
	}
	if err := accrueClaimTx(tx, tenantID, schema, claimID, d, userID); err != nil {
		return err
	}
	return tx.Commit()
}

func accrueClaimTx(tx *sql.Tx, tenantID, schema, claimID string, d map[string]interface{}, userID string) error {
	if strField(d, "accrued_on") != "" {
		return nil
	}
	if st := strField(d, "status"); st != rebateApproved {
		return rebateErr(vendorRebateStateCode, "rebate claim %s is %s - only an Approved claim is accrued", claimID, st)
	}
	paise := RupeesToPaise(numFromInterface(d["rebate_amount"]))
	today := BusinessToday()
	if err := PostDoubleEntryTx(tx, tenantID, schema, VendorRebateClaimDoctype, claimID,
		map[string]int64{AccountRebateReceivable: paise}, map[string]int64{"5150": paise}, today, "VendorRebateClaim:"+claimID+":ACCRUE"); err != nil {
		return postingError(err, "rebate not accrued")
	}
	d["accrued_on"] = today
	return saveRebateClaimTx(tx, schema, claimID, d)
}

// SettleVendorRebateClaim raises the debit note for an Approved claim and
// moves the accrued receivable onto the vendor's account: Dr 2100 / Cr 1820.
func SettleVendorRebateClaim(tenantID, userID, claimID string) (string, error) {
	if err := requireVendorRebates(tenantID); err != nil {
		return "", err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	noteID := ""
	tx, err := db.DB.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	d, err := loadRebateClaimTx(tx, schema, claimID)
	if err != nil {
		return "", err
	}
	if strField(d, "status") == rebateSettled {
		return strField(d, "debit_note_id"), nil
	}
	if strField(d, "status") != rebateApproved {
		return "", rebateErr(vendorRebateStateCode, "rebate claim %s is %s - only an Approved claim is settled", claimID, strField(d, "status"))
	}
	if err := accrueClaimTx(tx, tenantID, schema, claimID, d, userID); err != nil {
		return "", err
	}
	paise := RupeesToPaise(numFromInterface(d["rebate_amount"]))
	today := BusinessToday()
	noteID, err = GenerateSequence(tenantID, "DN", numberingStore(tenantID, ""), documentFinancialYearOf(today))
	if err != nil {
		return "", fmt.Errorf("could not number the debit note for rebate claim %s: %w", claimID, err)
	}
	note := map[string]interface{}{
		"code": noteID, "note_number": noteID, "vendor_id": strField(d, "vendor_id"),
		"amount": PaiseToRupees(paise), "gst_amount": float64(0), "total_with_gst": PaiseToRupees(paise),
		"reason": fmt.Sprintf("Volume rebate %s to %s (claim %s)", strField(d, "period_from"), strField(d, "period_to"), claimID),
		"status": "Posted", "basis": "vendor_rebate", "source_doctype": VendorRebateClaimDoctype, "source_doc_id": claimID,
	}
	nb, _ := json.Marshal(note)
	if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'DebitNote', $2, 'Posted', $3)`, schema), noteID, nb, userID); err != nil {
		return "", err
	}
	if err := PostDoubleEntryTx(tx, tenantID, schema, "DebitNote", noteID,
		map[string]int64{"2100": paise}, map[string]int64{AccountRebateReceivable: paise}, today, "VendorRebateClaim:"+claimID+":SETTLE"); err != nil {
		return "", postingError(err, "rebate not settled")
	}
	d["status"], d["debit_note_id"], d["settled_on"], d["settled_by"] = rebateSettled, noteID, today, userID
	if err := saveRebateClaimTx(tx, schema, claimID, d); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	LogAuditEvent(tenantID, userID, "VENDOR_REBATE_SETTLED", "SUCCESS", fmt.Sprintf("Rebate claim %s settled by debit note %s for %.2f", claimID, noteID, PaiseToRupees(paise)))
	return noteID, nil
}

// documentFinancialYearOf is the financial year of a YYYY-MM-DD business day.
func documentFinancialYearOf(day string) string {
	t, err := time.Parse("2006-01-02", day)
	if err != nil {
		return ""
	}
	return documentFinancialYear(t)
}
