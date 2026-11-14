package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// Stage 148.13: the vendor scorecard. Every number is worked out from the
// purchase orders, receipts and bills already on file, at the moment it is
// asked for; nothing is stored. A measure with nothing to measure is nil
// (shown as "No data"), never 0%.
//
// The period is the orders placed in it (default: the last 12 months). Receipts
// and bills are those against those orders.

// VendorScorecard is one vendor's track record for a period.
type VendorScorecard struct {
	Vendor string `json:"vendor"`
	From   string `json:"from"`
	To     string `json:"to"`
	Orders int    `json:"orders"`
	// Receipts and Bills are the counts behind the percentages.
	Receipts int `json:"receipts"`
	Bills    int `json:"bills"`
	// OnTimePct: receipts on or before the order's expected date, of receipts
	// whose order has one.
	OnTimePct *float64 `json:"on_time_pct"`
	// FillPct: quantity received (never more than ordered) of quantity ordered,
	// over orders that have a receipt or are past their expected date.
	FillPct *float64 `json:"fill_pct"`
	// QCRejectPct: quantity received but not accepted, of quantity received.
	QCRejectPct *float64 `json:"qc_reject_pct"`
	// DefectsPerMillion (170.16): rejected quantity per million received.
	DefectsPerMillion *float64 `json:"defects_per_million"`
	// OpenSCARs: supplier corrective action requests not yet closed.
	OpenSCARs int `json:"open_scars"`
	// PriceVariancePct: billed rate against ordered rate, weighted by accepted
	// quantity; positive = dearer than ordered.
	PriceVariancePct *float64 `json:"price_variance_pct"`
	// BillMismatches: bills now held for a mismatch; MismatchPct is their share
	// of the bills in the period.
	BillMismatches int      `json:"bill_mismatches"`
	MismatchPct    *float64 `json:"bill_mismatch_pct"`
}

func pctOf(num, den float64) *float64 {
	if den <= 0 {
		return nil
	}
	v := math.Round(num/den*1000) / 10
	return &v
}

// scorecardPeriod fills a blank from/to: to = today, from = 12 months before.
func scorecardPeriod(from, to string) (string, string) {
	to = strings.TrimSpace(to)
	if len(to) < 10 {
		to = BusinessToday()
	}
	from = strings.TrimSpace(from)
	if len(from) < 10 {
		if t, err := time.Parse("2006-01-02", to[:10]); err == nil {
			from = t.AddDate(-1, 0, 1).Format("2006-01-02")
		} else {
			from = to
		}
	}
	return from[:10], to[:10]
}

type scPO struct {
	id, vendor, expected string
	lines                []map[string]interface{}
	ordered              map[string]float64
	rate                 map[string]float64
}

type scGRN struct {
	po, date string
	lines    []map[string]interface{}
}

// VendorScorecards computes the scorecard of every vendor that had an order in
// the period, or of one vendor when vendor is given.
func VendorScorecards(tenantID, vendor, from, to string) ([]VendorScorecard, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	from, to = scorecardPeriod(from, to)
	vendor = strings.TrimSpace(vendor)
	today := BusinessToday()

	pos := map[string]*scPO{}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, COALESCE(NULLIF(data->>'vendor_id',''), data->>'vendor', ''),
		COALESCE(left(data->>'expected_delivery_date', 10), ''), COALESCE(data->>'items', '')
		FROM %s.documents WHERE doctype = 'PurchaseOrder' AND deleted_at IS NULL
		AND COALESCE(status, '') NOT IN ('Draft', 'Cancelled', 'Rejected')
		AND public.erp_local_date(created_at) BETWEEN $1::date AND $2::date
		AND ($3 = '' OR COALESCE(NULLIF(data->>'vendor_id',''), data->>'vendor', '') = $3)
		LIMIT 20001`, schema), from, to, vendor)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		p := &scPO{ordered: map[string]float64{}, rate: map[string]float64{}}
		var items string
		if err := rows.Scan(&p.id, &p.vendor, &p.expected, &items); err != nil {
			rows.Close()
			return nil, err
		}
		if p.vendor == "" {
			continue
		}
		var ls []map[string]interface{}
		_ = json.Unmarshal([]byte(items), &ls)
		for _, l := range ls {
			sku := strField(l, "sku")
			p.ordered[sku] += numFromInterface(l["qty"])
			if r := numFromInterface(l["rate"]); r > 0 {
				p.rate[sku] = r
			}
		}
		pos[p.id] = p
	}
	rows.Close()
	if len(pos) == 0 {
		return []VendorScorecard{}, nil
	}

	var grns []scGRN
	rows, err = db.DB.Query(fmt.Sprintf(`SELECT data, public.erp_local_date(created_at)::text FROM %s.documents
		WHERE doctype = 'GRN' AND deleted_at IS NULL AND COALESCE(status, '') NOT IN ('Draft', 'Cancelled', 'Rejected')
		AND public.erp_local_date(created_at) >= $1::date LIMIT 50001`, schema), from)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var raw []byte
		var date string
		if err := rows.Scan(&raw, &date); err != nil {
			rows.Close()
			return nil, err
		}
		var data map[string]interface{}
		if json.Unmarshal(raw, &data) != nil {
			continue
		}
		for _, poID := range GRNPOIDs(data) {
			if pos[poID] != nil {
				grns = append(grns, scGRN{po: poID, date: date, lines: grnLineMaps(data)})
				break
			}
		}
	}
	rows.Close()

	type bill struct {
		po, status string
		lines      []billLine
	}
	var bills []bill
	rows, err = db.DB.Query(fmt.Sprintf(`SELECT data FROM %s.documents WHERE doctype = 'VendorInvoice' AND deleted_at IS NULL
		AND COALESCE(status, '') NOT IN ('Draft', 'Cancelled') AND COALESCE(data->>'po_id', '') <> '' LIMIT 50001`, schema))
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			rows.Close()
			return nil, err
		}
		var data map[string]interface{}
		if json.Unmarshal(raw, &data) != nil {
			continue
		}
		if po := strField(data, "po_id"); pos[po] != nil {
			bills = append(bills, bill{po: po, status: strField(data, "status"), lines: vendorBillLines(data)})
		}
	}
	rows.Close()

	type acc struct {
		sc                                    VendorScorecard
		onTime, timed                         float64
		ordered, filled                       float64
		recv, accepted                        float64
		varNum, varDen                        float64
		mismatched                            float64
		received                              map[string]map[string]float64 // po -> sku -> qty
		billRate                              map[string]map[string]float64 // po -> sku -> billed rate
		billQty                               map[string]map[string]float64
		acceptedBySKU                         map[string]map[string]float64
		invoiceRateBySKU, invoiceRateWeightBy map[string]map[string]float64
	}
	by := map[string]*acc{}
	get := func(v string) *acc {
		a := by[v]
		if a == nil {
			a = &acc{received: map[string]map[string]float64{}, billRate: map[string]map[string]float64{}, billQty: map[string]map[string]float64{},
				acceptedBySKU: map[string]map[string]float64{}, invoiceRateBySKU: map[string]map[string]float64{}, invoiceRateWeightBy: map[string]map[string]float64{}}
			a.sc = VendorScorecard{Vendor: v, From: from, To: to}
			by[v] = a
		}
		return a
	}
	add := func(m map[string]map[string]float64, po, sku string, v float64) {
		if m[po] == nil {
			m[po] = map[string]float64{}
		}
		m[po][sku] += v
	}
	for _, p := range pos {
		get(p.vendor).sc.Orders++
	}
	hasReceipt := map[string]bool{}
	for _, g := range grns {
		p := pos[g.po]
		a := get(p.vendor)
		a.sc.Receipts++
		hasReceipt[g.po] = true
		if len(p.expected) == 10 {
			a.timed++
			if g.date <= p.expected {
				a.onTime++
			}
		}
		for _, l := range g.lines {
			sku := strField(l, "sku")
			q, acq := numFromInterface(l["qty"]), grnLineAcceptedQty(l)
			a.recv += q
			a.accepted += acq
			add(a.received, p.id, sku, q)
			add(a.acceptedBySKU, p.id, sku, acq)
			if r := numFromInterface(l["invoice_rate"]); r > 0 {
				add(a.invoiceRateBySKU, p.id, sku, r*acq)
				add(a.invoiceRateWeightBy, p.id, sku, acq)
			}
		}
	}
	for _, b := range bills {
		p := pos[b.po]
		a := get(p.vendor)
		a.sc.Bills++
		if b.status == "MismatchHold" {
			a.mismatched++
		}
		for _, l := range b.lines {
			add(a.billRate, b.po, l.SKU, l.Rate*l.Qty)
			add(a.billQty, b.po, l.SKU, l.Qty)
		}
	}
	for _, p := range pos {
		if !hasReceipt[p.id] && !(len(p.expected) == 10 && p.expected < today) {
			continue
		}
		a := get(p.vendor)
		for sku, q := range p.ordered {
			a.ordered += q
			a.filled += math.Min(a.received[p.id][sku], q)
		}
	}
	out := make([]VendorScorecard, 0, len(by))
	openScars := openSCARCounts(schema)
	for _, a := range by {
		for poID, skus := range a.acceptedBySKU {
			p := pos[poID]
			for sku, accQty := range skus {
				poRate := p.rate[sku]
				if poRate <= 0 || accQty <= 0 {
					continue
				}
				billed := 0.0
				if q := a.billQty[poID][sku]; q > 0 {
					billed = a.billRate[poID][sku] / q
				} else if w := a.invoiceRateWeightBy[poID][sku]; w > 0 {
					billed = a.invoiceRateBySKU[poID][sku] / w
				}
				if billed <= 0 {
					continue
				}
				a.varNum += (billed - poRate) * accQty
				a.varDen += poRate * accQty
			}
		}
		sc := a.sc
		sc.OnTimePct = pctOf(a.onTime, a.timed)
		sc.FillPct = pctOf(a.filled, a.ordered)
		sc.QCRejectPct = pctOf(a.recv-a.accepted, a.recv)
		sc.DefectsPerMillion = defectsPerMillion(a.recv, a.accepted)
		sc.OpenSCARs = openScars[sc.Vendor]
		if a.varDen > 0 {
			v := math.Round(a.varNum/a.varDen*1000) / 10
			sc.PriceVariancePct = &v
		}
		sc.BillMismatches = int(a.mismatched)
		sc.MismatchPct = pctOf(a.mismatched, float64(sc.Bills))
		out = append(out, sc)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Vendor < out[j].Vendor })
	return out, nil
}

// VendorScorecardFor is one vendor's scorecard; a vendor with no orders in the
// period gets an empty card (every measure "No data").
func VendorScorecardFor(tenantID, vendor, from, to string) (VendorScorecard, error) {
	list, err := VendorScorecards(tenantID, vendor, from, to)
	if err != nil {
		return VendorScorecard{}, err
	}
	if len(list) > 0 {
		return list[0], nil
	}
	f, t := scorecardPeriod(from, to)
	return VendorScorecard{Vendor: vendor, From: f, To: t}, nil
}

func scText(p *float64) string {
	if p == nil {
		return "No data"
	}
	return fmt.Sprintf("%.1f%%", *p)
}

func init() {
	RegisterReport(ReportDefinition{
		ID: "vendor-scorecard", Label: "Vendor scorecard", Category: "Procurement", ModuleKey: "procurement", StoreKey: "-",
		Columns: []ReportColumn{
			{Key: "vendor", Label: "Vendor"}, {Key: "orders", Label: "Orders"}, {Key: "receipts", Label: "Receipts"},
			{Key: "on_time", Label: "On time"}, {Key: "fill", Label: "Fill rate"}, {Key: "qc_reject", Label: "QC reject"}, {Key: "dpm", Label: "Defects per million"}, {Key: "open_scars", Label: "Open SCARs"},
			{Key: "price_variance", Label: "Price variance"}, {Key: "bills", Label: "Bills"}, {Key: "mismatches", Label: "Bill mismatches"},
		},
		Params: []ReportParam{
			{Key: "from", Label: "From", Type: "date"}, {Key: "to", Label: "To", Type: "date"},
			{Key: "vendor", Label: "Vendor", Type: "link", Options: "Vendor"},
		},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			list, err := VendorScorecards(tenantID, params["vendor"], params["from"], params["to"])
			if err != nil {
				return nil, err
			}
			rows := make([]map[string]interface{}, 0, len(list))
			for _, s := range list {
				rows = append(rows, map[string]interface{}{
					"vendor": s.Vendor, "orders": s.Orders, "receipts": s.Receipts, "on_time": scText(s.OnTimePct),
					"fill": scText(s.FillPct), "qc_reject": scText(s.QCRejectPct), "dpm": dpmText(s.DefectsPerMillion), "open_scars": s.OpenSCARs, "price_variance": scText(s.PriceVariancePct),
					"bills": s.Bills, "mismatches": s.BillMismatches,
				})
			}
			return rows, nil
		},
	})
}
