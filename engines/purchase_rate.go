package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
)

// Stage 148.12: the purchase rate is kept current by the matched bill. The
// purchase rate of an item is Item.standard_cost (the planned cost per unit a
// PO design and the cost fallbacks read). The GRN's price is provisional; the
// bill, once it has matched, sets it. Every change is kept in
// item_purchase_rate_history. A printed MRP is never touched: only
// standard_cost is written here.

const SettingPriceJumpPercent = "procurement.price_jump_percent"

func init() {
	RegisterSetting(SettingDefinition{Key: SettingPriceJumpPercent, Module: "Procurement & GRN",
		Label: "Ask for a reason when a bill changes an item's rate by more than", Type: SettingTypeFloat, Default: "0", Unit: "%",
		Min: settingBound(0), Max: settingBound(1000),
		Description: "A matched bill sets the item's purchase rate. A change (up or down) above this percent holds the bill until it carries a reason. 0 = never asks."})
}

// PurchaseRateChange is one row of an item's rate history.
type PurchaseRateChange struct {
	ID            int64   `json:"id"`
	SKU           string  `json:"sku"`
	OldRate       float64 `json:"old_rate"`
	NewRate       float64 `json:"new_rate"`
	ChangedBy     string  `json:"changed_by"`
	ChangedAt     string  `json:"changed_at"`
	SourceDoctype string  `json:"source_doctype"`
	SourceID      string  `json:"source_id"`
	SourceRef     string  `json:"source_ref"` // the bill's own number, for the screen
	Vendor        string  `json:"vendor,omitempty"`
	Reason        string  `json:"reason,omitempty"`
}

// ItemPurchaseRateHistory reads an item's rate changes, newest first.
func ItemPurchaseRateHistory(tenantID, sku string, limit int) ([]PurchaseRateChange, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT h.id, h.sku, h.old_rate_paise, h.new_rate_paise, h.changed_by,
		to_char(h.changed_at, 'YYYY-MM-DD HH24:MI'), h.source_doctype, h.source_id, h.vendor, h.reason,
		COALESCE(NULLIF(d.data->>'invoice_number', ''), h.source_id)
		FROM %[1]s.item_purchase_rate_history h
		LEFT JOIN %[1]s.documents d ON d.id = h.source_id AND d.doctype = h.source_doctype
		WHERE h.sku = $1 ORDER BY h.changed_at DESC, h.id DESC LIMIT %[2]d`, schema, limit), strings.TrimSpace(sku))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PurchaseRateChange{}
	for rows.Next() {
		var c PurchaseRateChange
		var oldP, newP int64
		if err := rows.Scan(&c.ID, &c.SKU, &oldP, &newP, &c.ChangedBy, &c.ChangedAt, &c.SourceDoctype, &c.SourceID, &c.Vendor, &c.Reason, &c.SourceRef); err != nil {
			return nil, err
		}
		c.OldRate, c.NewRate = PaiseToRupees(oldP), PaiseToRupees(newP)
		out = append(out, c)
	}
	return out, rows.Err()
}

// matchedBillRates is the rate, ex-GST, the matched bill carries for each item:
// the bill's own lines when it lists them, else the rate it was matched at (the
// GRN's invoice rate, or the PO's). Items not received on the GRN are left out.
func matchedBillRates(tenantID string, invData map[string]interface{}, billing grnBilling, grnID string) map[string]float64 {
	out := map[string]float64{}
	if lines := vendorBillLines(invData); len(lines) > 0 {
		value, qty := map[string]float64{}, map[string]float64{}
		for _, l := range lines {
			value[l.SKU] += l.Rate * l.Qty
			qty[l.SKU] += l.Qty
		}
		for sku, q := range qty {
			if r := value[sku] / q; r > 0 {
				out[sku] = r
			}
		}
		return out
	}
	grn, _, err := fetchDocData(tenantID, "GRN", grnID)
	if err != nil {
		return out
	}
	for _, l := range grnLineMaps(grn) {
		sku := strings.TrimSpace(strField(l, "sku"))
		if sku == "" || grnLineAcceptedQty(l) <= 0 {
			continue
		}
		if r := billing.RateBySku[sku]; r > 0 {
			out[sku] = r
		}
	}
	return out
}

type itemRateRow struct {
	id       string
	data     map[string]interface{}
	oldPaise int64
}

func loadItemRateTx(tx *sql.Tx, schema, sku string) (*itemRateRow, error) {
	var id, raw string
	err := tx.QueryRow(fmt.Sprintf(`SELECT id, data FROM %s.documents WHERE doctype = 'Item' AND deleted_at IS NULL
		AND (id = $1 OR data->>'code' = $1) ORDER BY (id = $1) DESC LIMIT 1 FOR UPDATE`, schema), sku).Scan(&id, &raw)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r := &itemRateRow{id: id, data: map[string]interface{}{}}
	if err := json.Unmarshal([]byte(raw), &r.data); err != nil {
		return nil, err
	}
	r.oldPaise = RupeesToPaise(numFromInterface(r.data["standard_cost"]))
	return r, nil
}

// billRateJumpReasons says which items the bill would move by more than the
// jump setting, unless the bill already carries a reason. Read-only.
func billRateJumpReasons(tx *sql.Tx, tenantID, schema string, invData map[string]interface{}, rates map[string]float64) []string {
	pct := GetSettingFloat(tenantID, SettingPriceJumpPercent)
	if pct <= 0 || strings.TrimSpace(strField(invData, "price_jump_reason")) != "" {
		return nil
	}
	var out []string
	for _, sku := range costKeysF(rates) {
		row, err := loadItemRateTx(tx, schema, sku)
		if err != nil || row == nil || row.oldPaise <= 0 {
			continue
		}
		newP := RupeesToPaise(rates[sku])
		if newP <= 0 || newP == row.oldPaise {
			continue
		}
		if change := math.Abs(float64(newP-row.oldPaise)) / float64(row.oldPaise) * 100; change > pct {
			out = append(out, fmt.Sprintf("The rate of %s moves %.1f%% (%.2f to %.2f), above the %.1f%% limit; enter a reason on the bill and match again.",
				sku, change, PaiseToRupees(row.oldPaise), PaiseToRupees(newP), pct))
		}
	}
	return out
}

func costKeysF(m map[string]float64) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// applyBillPurchaseRatesTx sets the items' purchase rates from a matched bill
// and writes one history row per change, in the match's own transaction. A bill
// sets an item once: a later match of the same bill never undoes a newer bill's
// rate. Nothing here touches mrp.
func applyBillPurchaseRatesTx(tx *sql.Tx, schema, invoiceID, userID string, invData map[string]interface{}, rates map[string]float64) error {
	done := map[string]interface{}{}
	if m, ok := invData["purchase_rate_set"].(map[string]interface{}); ok {
		done = m
	}
	reason := strings.TrimSpace(strField(invData, "price_jump_reason"))
	vendor := strField(invData, "vendor_id")
	for _, sku := range costKeysF(rates) {
		if _, ok := done[sku]; ok {
			continue
		}
		row, err := loadItemRateTx(tx, schema, sku)
		if err != nil {
			return err
		}
		if row == nil {
			continue
		}
		newP := RupeesToPaise(rates[sku])
		if newP <= 0 {
			continue
		}
		done[sku] = PaiseToRupees(newP)
		if newP == row.oldPaise {
			continue
		}
		patch, _ := json.Marshal(map[string]interface{}{"standard_cost": PaiseToRupees(newP)})
		if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || $1::jsonb, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'Item' AND id = $2`, schema), string(patch), row.id); err != nil {
			return err
		}
		if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.item_purchase_rate_history
			(sku, old_rate_paise, new_rate_paise, changed_by, source_doctype, source_id, vendor, reason) VALUES ($1,$2,$3,$4,'VendorInvoice',$5,$6,$7)`, schema),
			sku, row.oldPaise, newP, userID, invoiceID, vendor, reason); err != nil {
			return err
		}
	}
	if len(done) > 0 {
		invData["purchase_rate_set"] = done
	}
	return nil
}
