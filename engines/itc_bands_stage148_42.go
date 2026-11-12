package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Stage 148.42: GST credit eligibility on a purchase line, and value bands.
//
// Off by default (finance.itc_eligibility). On:
//   - a purchase line is Eligible or Blocked; blank is Eligible. Blank on the
//     line falls back to the HSN row, then the vendor, then Eligible.
//   - the GST on a Blocked line is not input credit: when the bill is matched
//     its gst_amount leaves out the blocked share, so the 1500 debit is the
//     eligible tax only, and the blocked share stays in the cost the bill
//     books (the 148.1 bill-difference entry moves it into stock cost).
//   - an HSN row can carry "Rate by price" bands, "1000:5,2500:12,:18": a unit
//     price up to 1000 takes 5 percent, up to 2500 takes 12, the rest 18. A
//     row with no bands keeps the rate on the row.
//
// Purchase side only: a sale keeps the rate on the HSN row.

const SettingITC = "finance.itc_eligibility"

const (
	itcEligible = "Eligible"
	itcBlocked  = "Blocked"
)

func init() {
	RegisterSetting(SettingDefinition{
		Key: SettingITC, Module: "Finance",
		Label: "GST credit eligibility and rate by price", Type: SettingTypeBool, Default: "false",
		Description: "A purchase line can be marked Blocked so its GST stays in cost, not input credit; an HSN row can set the GST rate by unit price.",
	})
	settingGatedFields["HSNCode"] = map[string]string{"itc": SettingITC, "value_bands": SettingITC}
	settingGatedFields["Vendor"] = map[string]string{"itc": SettingITC}
}

// ValueBand is one step of "Rate by price": prices up to UpTo take Rate.
type ValueBand struct {
	UpTo float64 // 0 on the open last band
	Open bool
	Rate float64
}

// ParseValueBands reads "1000:5,2500:12,:18". Steps must rise; the last may
// be open (blank limit). Blank text is no bands.
func ParseValueBands(s string) ([]ValueBand, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	parts := strings.Split(s, ",")
	var out []ValueBand
	prev := 0.0
	for i, p := range parts {
		kv := strings.SplitN(strings.TrimSpace(p), ":", 2)
		if len(kv) != 2 {
			return nil, fmt.Errorf("band %d must look like 1000:5", i+1)
		}
		rate, err := strconv.ParseFloat(strings.TrimSpace(kv[1]), 64)
		if err != nil || rate < 0 || rate > 100 {
			return nil, fmt.Errorf("band %d rate must be 0 to 100", i+1)
		}
		lim := strings.TrimSpace(kv[0])
		if lim == "" {
			if i != len(parts)-1 {
				return nil, fmt.Errorf("only the last band can be open")
			}
			out = append(out, ValueBand{Open: true, Rate: rate})
			continue
		}
		up, err := strconv.ParseFloat(lim, 64)
		if err != nil || up <= prev {
			return nil, fmt.Errorf("band %d limit must be above the one before", i+1)
		}
		prev = up
		out = append(out, ValueBand{UpTo: up, Rate: rate})
	}
	return out, nil
}

// validateHSNBands is the PrepareHSNCode hook.
func validateHSNBands(payload map[string]interface{}) error {
	raw, _ := payload["value_bands"].(string)
	if _, err := ParseValueBands(raw); err != nil {
		return &ValidationError{Code: "TAXGST-1330", SubFor: "Rate by price", Message: fmt.Sprintf("Rate by price: %v", err)}
	}
	if v, _ := payload["itc"].(string); v != "" && v != itcEligible && v != itcBlocked {
		return &ValidationError{Code: "TAXGST-1331", SubFor: "GST credit", Message: "GST credit must be Eligible or Blocked"}
	}
	return nil
}

func itcDocData(tenantID, doctype, id string) map[string]interface{} {
	if id == "" {
		return nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil
	}
	var raw []byte
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents WHERE id=$1 AND doctype=$2 AND deleted_at IS NULL`, schema), id, doctype).Scan(&raw); err != nil {
		return nil
	}
	m := map[string]interface{}{}
	_ = json.Unmarshal(raw, &m)
	return m
}

// bandedGSTRate is the rate for a unit price: the band on the HSN row, or the
// fallback when the setting is off, the row has no bands, or none fits.
func bandedGSTRate(tenantID, hsn string, price, fallback float64) float64 {
	if !GetSettingBool(tenantID, SettingITC) {
		return fallback
	}
	row := itcDocData(tenantID, "HSNCode", HSNCatalogID(NormalizeHSN(hsn)))
	raw, _ := row["value_bands"].(string)
	bands, err := ParseValueBands(raw)
	if err != nil || len(bands) == 0 {
		return fallback
	}
	for _, b := range bands {
		if b.Open || price <= b.UpTo {
			return b.Rate
		}
	}
	return fallback
}

// purchaseLineITCInto marks a computed line as banded and/or blocked. Does
// nothing while the setting is off.
func purchaseLineITCInto(tenantID string, payload map[string]interface{}, l POLineInput, g *GSTLineInput) {
	if !GetSettingBool(tenantID, SettingITC) {
		return
	}
	g.Bands = true
	choice := l.ITC
	if choice == "" {
		if info, err := GetItemTaxInfo(tenantID, l.SKU); err == nil {
			row := itcDocData(tenantID, "HSNCode", HSNCatalogID(NormalizeHSN(info.HSNCode)))
			choice, _ = row["itc"].(string)
		}
	}
	if choice == "" {
		v, _ := payload["vendor"].(string)
		if v == "" {
			v, _ = payload["vendor_id"].(string)
		}
		vd := itcDocData(tenantID, "Vendor", v)
		choice, _ = vd["itc"].(string)
	}
	g.Blocked = choice == itcBlocked
}

// stampPOBlockedTax records the blocked GST on the PO so the bill can leave
// it out of input credit. Cleared when there is none.
func stampPOBlockedTax(payload map[string]interface{}, b GSTBreakdown) {
	if b.BlockedTax > 0 {
		payload["itc_blocked_tax"] = b.BlockedTax
	} else {
		delete(payload, "itc_blocked_tax")
	}
}

// blockedGSTShare is the part of a bill's goods GST that is blocked credit:
// the PO's blocked share of its tax, applied to the bill's GST.
func blockedGSTShare(tenantID string, poData map[string]interface{}, gstPart float64) float64 {
	if !GetSettingBool(tenantID, SettingITC) || gstPart <= 0 {
		return 0
	}
	blocked, _ := poData["itc_blocked_tax"].(float64)
	if blocked <= 0 {
		return 0
	}
	var total float64
	switch bd := poData["gst_breakdown"].(type) {
	case map[string]interface{}:
		total, _ = bd["total_tax"].(float64)
	}
	if total <= 0 {
		return 0
	}
	if blocked > total {
		blocked = total
	}
	return round2(gstPart * blocked / total)
}
