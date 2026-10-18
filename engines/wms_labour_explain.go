package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"math"
	"time"
)

// Stage 55.8 - labour and billing a supervisor can check by hand.
//
// ExplainLaborStandard shows a standard's time component by component - each
// work element (fixed seconds plus seconds per unit), the travel section, and
// the allowances (a percentage of element + travel time plus fixed seconds) -
// next to the total ResolveLaborStandardTime computes, so the worked example
// and the engine can never quietly disagree: a difference is reported, not
// hidden.
//
// BillingReview lays out one owner's captured charges and storage billing for
// a period with the arithmetic behind each amount, and lists what needs a look
// before an invoice is raised: storage periods with missing daily snapshots,
// charges captured before their rate evidence was recorded, and charges where
// the minimum charge applied. It changes nothing; capturing and invoicing stay
// on their own endpoints.

// LaborPreviewLine is one component of a standard's time.
type LaborPreviewLine struct {
	Component string  `json:"component"`
	Code      string  `json:"code"`
	Basis     string  `json:"basis"`
	Seconds   float64 `json:"seconds"`
}

// LaborPreview is a standard's worked example for a quantity.
type LaborPreview struct {
	LaborStandardTime
	Lines   []LaborPreviewLine `json:"lines"`
	Minutes float64            `json:"minutes"`
	Agrees  bool               `json:"agrees"`
}

func secs(v float64) string {
	if v == math.Trunc(v) {
		return fmt.Sprintf("%.0f s", v)
	}
	return fmt.Sprintf("%.2f s", v)
}

// ExplainLaborStandard resolves a standard for qty units with its working.
func ExplainLaborStandard(tenantID, standardCode string, qty float64) (*LaborPreview, error) {
	if qty <= 0 {
		qty = 1
	}
	total, err := ResolveLaborStandardTime(tenantID, standardCode, qty)
	if err != nil {
		return nil, err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	standard, _ := stage42ActiveDocByCode(schema, "LaborStandard", standardCode)
	out := &LaborPreview{LaborStandardTime: *total}
	base := 0.0
	for _, code := range stage42Codes(stage42DataString(standard, "element_codes")) {
		el, _ := stage42ActiveDocByCode(schema, "LaborElement", code)
		fixed, per := stage42Number(el, "fixed_seconds"), stage42Number(el, "standard_seconds_per_unit")
		s := fixed + per*qty
		base += s
		out.Lines = append(out.Lines, LaborPreviewLine{Component: "Work element", Code: code,
			Basis: fmt.Sprintf("%s fixed + %s × %g unit(s)", secs(fixed), secs(per), qty), Seconds: roundTo2(s)})
	}
	if travelCode := stage42DataString(standard, "travel_section_code"); travelCode != "" {
		tr, _ := stage42ActiveDocByCode(schema, "TravelSection", travelCode)
		perTask, perUnit := stage42Number(tr, "seconds_per_task"), stage42Number(tr, "seconds_per_unit")
		s := perTask + perUnit*qty
		base += s
		out.Lines = append(out.Lines, LaborPreviewLine{Component: "Travel", Code: travelCode,
			Basis: fmt.Sprintf("%s per task + %s × %g unit(s)", secs(perTask), secs(perUnit), qty), Seconds: roundTo2(s)})
	}
	allowance := 0.0
	for _, code := range stage42Codes(stage42DataString(standard, "allowance_codes")) {
		al, _ := stage42ActiveDocByCode(schema, "LaborAllowance", code)
		pct, fixed := stage42Number(al, "allowance_pct"), stage42Number(al, "fixed_seconds")
		s := base*pct/100 + fixed
		allowance += s
		out.Lines = append(out.Lines, LaborPreviewLine{Component: "Allowance", Code: code,
			Basis: fmt.Sprintf("%g%% of %s + %s fixed", pct, secs(roundTo2(base)), secs(fixed)), Seconds: roundTo2(s)})
	}
	out.Minutes = math.Round(total.TotalSeconds/60*100) / 100
	out.Agrees = math.Abs(roundTo2(base+allowance)-total.TotalSeconds) < 0.02
	return out, nil
}

// ChargeReviewLine is a captured charge with its working.
type ChargeReviewLine struct {
	CapturedChargeInfo
	Explanation string `json:"explanation"`
}

// StorageReviewRow is a storage rate's bill for the period with its working.
type StorageReviewRow struct {
	StorageBillingV2Row
	PeriodDays  int    `json:"period_days"`
	Explanation string `json:"explanation"`
}

// BillingException is something to look at before invoicing.
type BillingException struct {
	Kind   string `json:"kind"`
	Ref    string `json:"ref"`
	Detail string `json:"detail"`
}

// BillingReviewResult is one owner's period, explained.
type BillingReviewResult struct {
	Owner         string             `json:"owner"`
	Start         string             `json:"start"`
	End           string             `json:"end"`
	Charges       []ChargeReviewLine `json:"charges"`
	Storage       []StorageReviewRow `json:"storage"`
	Exceptions    []BillingException `json:"exceptions"`
	Captured      float64            `json:"captured_total"`
	NotInvoiced   float64            `json:"not_invoiced_total"`
	StorageToBill float64            `json:"storage_to_capture_total"`
}

func amt2(v float64) string { return fmt.Sprintf("%.2f", v) }

// BillingReview explains an owner's captured charges and storage bill.
func BillingReview(tenantID, owner, start, end string) (*BillingReviewResult, error) {
	s, err1 := time.Parse("2006-01-02", start)
	e, err2 := time.Parse("2006-01-02", end)
	if err1 != nil || err2 != nil || e.Before(s) {
		return nil, &ValidationError{Code: "INVENT-0500", Message: "choose a period: a start date and an end date on or after it"}
	}
	if owner == "" {
		return nil, &ValidationError{Code: "INVENT-0500", Message: "choose the owner (client) to review"}
	}
	out := &BillingReviewResult{Owner: owner, Start: start, End: end, Charges: []ChargeReviewLine{}, Storage: []StorageReviewRow{}, Exceptions: []BillingException{}}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	charges, err := ListCapturedCharges(tenantID, owner, start, end, "")
	if err != nil {
		return nil, err
	}
	for _, c := range charges {
		line := ChargeReviewLine{CapturedChargeInfo: c}
		var raw []byte
		data := map[string]interface{}{}
		if db.DB.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents WHERE id = $1`, schema), c.ID).Scan(&raw) == nil {
			_ = json.Unmarshal(raw, &data)
		}
		if _, ok := data["base_amount"]; ok {
			rate, baseAmt := stage42Number(data, "base_rate"), stage42Number(data, "base_amount")
			line.Explanation = fmt.Sprintf("%g × %s = %s", c.Quantity, amt2(rate), amt2(baseAmt))
			if v := stage42Number(data, "markup_pct"); v != 0 {
				line.Explanation += fmt.Sprintf("; +%g%% markup", v)
			}
			if v := stage42Number(data, "discount_pct"); v != 0 {
				line.Explanation += fmt.Sprintf("; −%g%% discount", v)
			}
			if minimum := stage42Number(data, "minimum_charge"); minimum > 0 && math.Abs(c.NetAmount-minimum) < 0.005 {
				line.Explanation += fmt.Sprintf("; minimum charge %s applies", amt2(minimum))
				out.Exceptions = append(out.Exceptions, BillingException{Kind: "Minimum charge applied", Ref: c.ID,
					Detail: fmt.Sprintf("%s for %s billed at the minimum %s", c.ChargeCode, c.EventKey, amt2(minimum))})
			}
		} else if days := stage42Number(data, "snapshot_days"); days > 0 {
			line.Explanation = fmt.Sprintf("storage: average %g unit(s) over %g day(s)", c.Quantity, days)
		} else {
			line.Explanation = "captured before rate evidence was recorded"
			out.Exceptions = append(out.Exceptions, BillingException{Kind: "No rate evidence", Ref: c.ID,
				Detail: fmt.Sprintf("%s for %s: only the amounts were kept - check against the charge code before invoicing", c.ChargeCode, c.EventKey)})
		}
		line.Explanation += fmt.Sprintf(" → net %s; tax %g%% = %s; total %s", amt2(c.NetAmount), c.TaxRate, amt2(c.TaxAmount), amt2(c.TotalAmount))
		out.Captured += c.TotalAmount
		if c.InvoiceID == "" {
			out.NotInvoiced += c.TotalAmount
		}
		out.Charges = append(out.Charges, line)
	}
	periodDays := int(e.Sub(s).Hours()/24) + 1
	rows, err := GetStorageBillingV2(tenantID, owner, start, end)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		row := StorageReviewRow{StorageBillingV2Row: r, PeriodDays: periodDays}
		row.Explanation = fmt.Sprintf("average %g unit(s) × %s per unit per day × %d day(s) with a snapshot = %s → net %s; tax %g%% = %s; total %s",
			r.AverageUnits, amt2(r.StorageRate), r.SnapshotDays, amt2(r.AverageUnits*r.StorageRate*float64(r.SnapshotDays)), amt2(r.NetAmount), r.TaxRate, amt2(r.TaxAmount), amt2(r.TotalAmount))
		switch {
		case r.SnapshotDays == 0:
			out.Exceptions = append(out.Exceptions, BillingException{Kind: "No storage snapshots", Ref: r.RateCode,
				Detail: fmt.Sprintf("%s at %s has no daily balance snapshot in the period - nothing would be billed", r.RateCode, r.LocationCode)})
		case r.SnapshotDays < periodDays:
			out.Exceptions = append(out.Exceptions, BillingException{Kind: "Missing storage days", Ref: r.RateCode,
				Detail: fmt.Sprintf("%s at %s: %d of %d day(s) have a balance snapshot - the bill covers only those days", r.RateCode, r.LocationCode, r.SnapshotDays, periodDays)})
		}
		out.StorageToBill += r.TotalAmount
		out.Storage = append(out.Storage, row)
	}
	out.Captured, out.NotInvoiced, out.StorageToBill = roundTo2(out.Captured), roundTo2(out.NotInvoiced), roundTo2(out.StorageToBill)
	return out, nil
}
