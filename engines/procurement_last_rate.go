package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/lib/pq"
)

// Item 273.3 - the last price paid, on the PO line.
//
// A buyer keying a rate cannot see what the vendor charged last time without
// leaving the PO. The PO screen's preview now says it under each line: the
// rate on this vendor's last issued PO for the item (the last year), how far
// the typed rate is from it, and a cheaper rate another vendor gave in the
// last 180 days. A rise above procurement.last_rate_alert_pct shows in red.
// It only informs: nothing is blocked and nothing is saved.
//
// Rates are compared like for like: the paid price of one unit (after the
// line's discounts, poLineUnitRate) and in this PO's GST mode - a past PO in
// the other mode is converted at this line's GST rate.

const SettingLastRateAlertPct = "procurement.last_rate_alert_pct"

func init() {
	RegisterSetting(SettingDefinition{Key: SettingLastRateAlertPct, Module: "Procurement & GRN",
		Label: "Show a PO rate in red when it is above the vendor's last rate by more than", Type: SettingTypeFloat, Default: "5", Unit: "%",
		Min: settingBound(0), Max: settingBound(1000),
		Description: "Each PO line shows what the vendor was last paid for the item. A rate higher than that by more than this percent shows in red. 0 = never red."})
}

const (
	lastRateDays   = 365 // this vendor's last rate
	betterRateDays = 180 // another vendor's cheaper rate
)

// lastRatePOStatuses are the POs whose rates the vendor accepted.
var lastRatePOStatuses = []string{"Approved", "Partially Received", "Received", "Closed"}

type pastPOLine struct {
	vendor, poID, ref, date, mode string
	recent                        bool
	line                          POLineInput
}

// StampPOLastRates fills the last-rate fields of a PO preview's lines. The
// payload's last_rate_exclude is the PO being edited, which must not be its
// own last rate.
func StampPOLastRates(tenantID string, payload map[string]interface{}, pv *POPreview) error {
	var skus []string
	seen := map[string]bool{}
	for _, l := range pv.Lines {
		if l.SKU != "" && !seen[l.SKU] {
			seen[l.SKU] = true
			skus = append(skus, l.SKU)
		}
	}
	if len(skus) == 0 {
		return nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT DISTINCT ON (x.sku, x.vendor) x.sku, x.vendor, x.id, x.ref, x.dt, x.mode, x.recent, x.line::text
		  FROM (SELECT li->>'sku' AS sku,
		               COALESCE(NULLIF(d.data->>'vendor', ''), d.data->>'vendor_id', '') AS vendor,
		               d.id,
		               COALESCE(NULLIF(d.data->>'po_number', ''), NULLIF(d.data->>'code', ''), d.id) AS ref,
		               left(COALESCE(NULLIF(d.data->>'order_date', ''), to_char(d.created_at, 'YYYY-MM-DD')), 10) AS dt,
		               COALESCE(d.data->>'gst_mode', '') AS mode,
		               d.created_at >= now() - interval '%[2]d days' AS recent,
		               d.created_at, li AS line
		          FROM %[1]s.documents d
		         CROSS JOIN LATERAL jsonb_array_elements(CASE
		                 WHEN jsonb_typeof(d.data->'items') = 'array' THEN d.data->'items'
		                 WHEN jsonb_typeof(d.data->'items') = 'string' AND d.data->>'items' LIKE '[%%' THEN (d.data->>'items')::jsonb
		                 ELSE '[]'::jsonb END) li
		         WHERE d.doctype = 'PurchaseOrder' AND d.deleted_at IS NULL AND d.status = ANY($2)
		           AND d.id <> $3 AND d.created_at >= now() - interval '%[3]d days'
		           AND EXISTS (SELECT 1 FROM unnest($1::text[]) s WHERE strpos(d.data->>'items', '"' || s || '"') > 0)) x
		 WHERE x.sku = ANY($1) AND x.vendor <> ''
		 ORDER BY x.sku, x.vendor, x.dt DESC, x.created_at DESC`, schema, betterRateDays, lastRateDays),
		pq.Array(skus), pq.Array(lastRatePOStatuses), strings.TrimSpace(pimString(payload["last_rate_exclude"])))
	if err != nil {
		return err
	}
	bySKU := map[string][]pastPOLine{}
	for rows.Next() {
		var sku, text string
		var p pastPOLine
		if rows.Scan(&sku, &p.vendor, &p.poID, &p.ref, &p.date, &p.mode, &p.recent, &text) != nil {
			continue
		}
		if json.Unmarshal([]byte(text), &p.line) != nil || poLineUnitRate(p.line) <= 0 {
			continue
		}
		bySKU[sku] = append(bySKU[sku], p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	vendor := strings.TrimSpace(pimString(payload["vendor"]))
	if vendor == "" {
		vendor = strings.TrimSpace(pimString(payload["vendor_id"]))
	}
	inclusive := pv.GSTMode != GSTModeExclusive
	blankInclusive := ResolvePOGSTMode(tenantID, map[string]interface{}{}) != GSTModeExclusive
	alertPct := GetSettingFloat(tenantID, SettingLastRateAlertPct)
	names := map[string]string{} // vendor -> name, "" when it cannot be bought from
	nameOf := func(v string) string {
		if n, ok := names[v]; ok {
			return n
		}
		n, ok := vendorNameOf(tenantID, v)
		if !ok {
			n = ""
		}
		names[v] = n
		return n
	}

	for i := range pv.Lines {
		l := &pv.Lines[i]
		past := bySKU[l.SKU]
		if len(past) == 0 {
			continue
		}
		// One unit's paid price in this PO's GST mode.
		inMode := func(p pastPOLine) float64 {
			u := poLineUnitRate(p.line)
			was := blankInclusive
			switch p.mode {
			case GSTModeExclusive:
				was = false
			case GSTModeInclusive:
				was = true
			}
			switch {
			case was == inclusive:
				return round2(u)
			case was:
				return round2(u / (1 + l.GSTRate/100))
			default:
				return round2(u * (1 + l.GSTRate/100))
			}
		}
		cur := round2(poLineUnitRate(l.POLineInput))
		basis := cur
		var others []pastPOLine
		for _, p := range past {
			if vendor != "" && p.vendor == vendor {
				l.LastRate, l.LastRatePO, l.LastRateDate = inMode(p), p.ref, p.date
				note := "Last " + rupeeText(l.LastRate) + " · " + p.ref + ", " + lastRateDay(p.date)
				if cur > 0 && l.LastRate > 0 {
					l.LastRateUpPct = math.Round((cur-l.LastRate)/l.LastRate*1000) / 10
					if l.LastRateUpPct != 0 {
						note += fmt.Sprintf(" · now %+.1f%%", l.LastRateUpPct)
					}
					l.LastRateAlert = alertPct > 0 && l.LastRateUpPct > alertPct
				}
				if basis <= 0 {
					basis = l.LastRate
				}
				l.LastRateNote = note
			} else if p.recent {
				others = append(others, p)
			}
		}
		if basis <= 0 || len(others) == 0 {
			continue
		}
		sort.SliceStable(others, func(a, b int) bool {
			ra, rb := inMode(others[a]), inMode(others[b])
			return ra < rb || (ra == rb && others[a].vendor < others[b].vendor)
		})
		for _, p := range others {
			r := inMode(p)
			if r >= basis {
				break
			}
			if name := nameOf(p.vendor); name != "" {
				l.BetterRate, l.BetterVendor = r, name
				l.BetterRateNote = "Cheaper: " + name + " " + rupeeText(r) + ", " + lastRateDay(p.date)
				break
			}
		}
	}
	return nil
}

// lastRateDay shows a PO date as "12 Oct 2026".
func lastRateDay(iso string) string {
	if t, err := time.Parse("2006-01-02", iso); err == nil {
		return t.Format("2 Jan 2006")
	}
	return iso
}
