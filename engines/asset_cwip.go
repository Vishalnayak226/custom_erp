package engines

import (
	"custom_erp/db"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// Stage 170.6: capital work in progress.
//
// A Draft asset can be moved to status CWIP. Vendor bill lines naming it (the
// line's cwip_asset) debit 1450 Capital Work in Progress instead of an expense
// account. The asset shows the total collected. Capitalise moves that total
// into the fixed asset (Dr 1400 / Cr 1450) on the chosen date, and depreciation
// starts from that date.

const (
	cwipAccount = "1450"

	cwipStepCode   = "ASSET-1723" // the asset is not in a state for this step
	cwipChargeCode = "ASSET-1724" // a bill line cannot be charged to that asset
)

func cwipRefused(code, sub, format string, args ...interface{}) error {
	return &ValidationError{Code: code, SubFor: sub, Message: fmt.Sprintf(format, args...)}
}

// cwipCollectedQuery sums the posted, not cancelled bill lines charged to
// assets. A blocked-credit line carries its GST into the cost. The second %s
// is an optional extra condition.
const cwipCollectedQuery = `SELECT l->>'cwip_asset', COALESCE(SUM((l->>'net_paise')::bigint +
		CASE WHEN COALESCE((l->>'blocked')::boolean, false) THEN COALESCE((l->>'tax_paise')::bigint, 0) ELSE 0 END), 0)
	FROM %s.documents d,
	     jsonb_array_elements(CASE WHEN jsonb_typeof(d.data->'expense_lines') = 'array' THEN d.data->'expense_lines' ELSE '[]'::jsonb END) l
	WHERE d.doctype = 'VendorInvoice' AND d.deleted_at IS NULL AND d.status <> 'Cancelled'
	  AND d.data->>'expense_posted' = 'Yes' AND COALESCE(l->>'cwip_asset', '') <> ''%s
	GROUP BY 1`

func cwipCollectedByAsset(schema string) (map[string]int64, error) {
	rows, err := db.DB.Query(fmt.Sprintf(cwipCollectedQuery, schema, ""))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var id string
		var n int64
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

// CWIPCollectedPaise is the total charged to one asset so far.
func CWIPCollectedPaise(tenantID, assetID string) (int64, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, err
	}
	var n int64
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(SUM(t), 0) FROM (`+cwipCollectedQuery+`) x(a, t)`, schema,
		` AND l->>'cwip_asset' = $1`), assetID).Scan(&n)
	return n, err
}

// cwipChargeOK says whether a bill line may be charged to this asset.
func cwipChargeOK(tenantID, assetID string, line int) error {
	_, status, err := fetchAssetData(tenantID, assetID)
	if err != nil {
		return cwipRefused(cwipChargeCode, "CWIP asset", "Line %d: asset %s is not on file.", line, assetID)
	}
	if status != "CWIP" {
		return cwipRefused(cwipChargeCode, "CWIP asset", "Line %d: asset %s is not in CWIP (it is %s).", line, assetID, status)
	}
	return nil
}

// cwipLockForChargeTx re-checks, inside the transaction that posts a bill,
// that every CWIP asset its lines charge is still in CWIP, and holds a share
// lock on each until that transaction ends. Capitalisation locks the asset
// FOR UPDATE, so the two queue: a bill that posts first is in the capitalised
// total; one that comes after (or is approved after) is refused. Ask 176.
func cwipLockForChargeTx(tx *sql.Tx, schema string, rows []expenseLineRow) error {
	seen := map[string]bool{}
	for i, r := range rows {
		id := strings.TrimSpace(r.CwipAsset)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		var status string
		err := tx.QueryRow(fmt.Sprintf(`SELECT status FROM %s.documents WHERE doctype = 'Asset' AND id = $1 AND deleted_at IS NULL FOR SHARE`, schema), id).Scan(&status)
		if err == sql.ErrNoRows {
			return cwipRefused(cwipChargeCode, "CWIP asset", "Line %d: asset %s is not on file.", i+1, id)
		}
		if err != nil {
			return err
		}
		if status != "CWIP" {
			return cwipRefused(cwipChargeCode, "CWIP asset", "Line %d: asset %s is not in CWIP (it is %s).", i+1, id, status)
		}
	}
	return nil
}

// MoveAssetToCWIP takes a Draft asset into CWIP, with an optional budget.
func MoveAssetToCWIP(tenantID, assetID, userID string, budget float64) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	if budget < 0 {
		return cwipRefused(cwipStepCode, "Budget", "The budget cannot be negative.")
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	data, status, err := lockDocTx(tx, schema, "Asset", assetID)
	if err != nil {
		return cwipRefused(cwipStepCode, "Asset", "That asset is not on file.")
	}
	if status != "Draft" {
		return cwipRefused(cwipStepCode, "Asset", "Only a Draft asset can move to CWIP (it is %s).", status)
	}
	if budget > 0 {
		data["cwip_budget"] = budget
	}
	if err := writeDocTx(tx, schema, "Asset", assetID, data, "CWIP"); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	LogAuditEvent(tenantID, userID, "ASSET_CWIP_STARTED", "SUCCESS", fmt.Sprintf("Asset %s moved to CWIP", assetID))
	return nil
}

// CapitalizeCWIPResult is what Capitalise moved.
type CapitalizeCWIPResult struct {
	AssetID string  `json:"asset_id"`
	Amount  float64 `json:"amount"`
	Date    string  `json:"capitalisation_date"`
}

// CapitalizeCWIPAsset moves the collected total from 1450 to 1400 and makes
// the asset Capitalised from date. A second call finds it Capitalised and is
// refused; the journal is keyed too.
func CapitalizeCWIPAsset(tenantID, assetID, userID string, usefulLifeYears float64, date string) (*CapitalizeCWIPResult, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	date = strings.TrimSpace(date)
	if date == "" {
		date = BusinessToday()
	} else if _, err := time.Parse("2006-01-02", date); err != nil {
		return nil, cwipRefused(cwipStepCode, "Date", "Use a date like 2026-11-04.")
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	data, status, err := lockDocTx(tx, schema, "Asset", assetID)
	if err != nil {
		return nil, cwipRefused(cwipStepCode, "Asset", "That asset is not on file.")
	}
	if status != "CWIP" {
		return nil, cwipRefused(cwipStepCode, "Asset", "Only a CWIP asset can be capitalised here (it is %s).", status)
	}
	var total int64
	if err := tx.QueryRow(fmt.Sprintf(`SELECT COALESCE(SUM(t), 0) FROM (`+cwipCollectedQuery+`) x(a, t)`, schema,
		` AND l->>'cwip_asset' = $1`), assetID).Scan(&total); err != nil {
		return nil, err
	}
	if total <= 0 {
		return nil, cwipRefused(cwipStepCode, "Asset", "Nothing has been charged to this asset yet.")
	}
	if usefulLifeYears > 0 {
		data["useful_life_years"] = usefulLifeYears
	}
	if life, _ := data["useful_life_years"].(float64); life <= 0 {
		return nil, &ValidationError{Code: "ASSET-0271", Message: "useful_life_years must be a positive number to capitalise - depreciation cannot be calculated without it"}
	}
	data["capitalised_paise"] = total
	data["cost"] = PaiseToRupees(total)
	data["capitalisation_date"] = date
	data["cwip_collected"] = PaiseToRupees(total)
	if err := PostDoubleEntryTx(tx, tenantID, schema, "Asset", assetID,
		map[string]int64{assetAccFixed: total}, map[string]int64{cwipAccount: total}, date,
		fmt.Sprintf("Asset:%s:CWIP-CAPITALIZE", assetID)); err != nil {
		return nil, err
	}
	if err := writeDocTx(tx, schema, "Asset", assetID, data, "Capitalised"); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	LogAuditEvent(tenantID, userID, "ASSET_CWIP_CAPITALISED", "SUCCESS", fmt.Sprintf("Asset %s capitalised from CWIP, %.2f", assetID, PaiseToRupees(total)))
	return &CapitalizeCWIPResult{AssetID: assetID, Amount: PaiseToRupees(total), Date: date}, nil
}
