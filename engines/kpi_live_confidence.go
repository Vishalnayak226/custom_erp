package engines

import (
	"fmt"
	"strings"
)

// Stage 47.14.3 (live confidence): a KPI contract's confidence used to stay
// the static basis. Now, when open (undismissed) reconciliation mismatches
// touch the widget's domain, the contract says "N open mismatches" instead.
// Display only: no figure changes.
//
// Domains: "cash" (order -> shipment -> invoice -> payment, Stage 49.5.5) and
// "stock" (stock ownership). A single-store scope counts a stock mismatch only
// when its location is that store; order-to-cash mismatches carry no store.

const (
	confidenceCash  = "cash"
	confidenceStock = "stock"
)

var datasetConfidenceDomains = map[string]string{
	"all_sales": confidenceCash, "sales_lines": confidenceCash, "pos_bills": confidenceCash, "online_orders": confidenceCash, "ledger": confidenceCash,
	"stock_on_hand": confidenceStock, "stock_moves": confidenceStock, "transfers": confidenceStock,
}

// DatasetConfidenceDomain is the reconciliation domain of a dataset, or "".
func DatasetConfidenceDomain(datasetKey string) string { return datasetConfidenceDomains[datasetKey] }

// ReportConfidenceDomain is the reconciliation domain of a report, or "".
func ReportConfidenceDomain(def ReportDefinition) string {
	if def.ID == "order-to-cash" {
		return confidenceCash
	}
	switch def.Category {
	case "Finance":
		return confidenceCash
	case "Inventory", "WMS":
		return confidenceStock
	}
	return ""
}

// WithLiveConfidence returns c with Confidence and OpenMismatches set when
// open mismatches touch domain (and store, when the widget is scoped to one).
// A clean book, no domain or any lookup trouble leaves c as it is.
func WithLiveConfidence(tenantID string, c KPIContract, domain, store string) KPIContract {
	if domain == "" {
		return c
	}
	rows, err := OrderToCashMismatches(tenantID, false)
	if err != nil {
		return c
	}
	n := 0
	for _, r := range rows {
		isStock := strings.HasPrefix(r.Kind, "Stock ")
		if (domain == confidenceStock) != isStock {
			continue
		}
		if isStock && store != "" && r.Ref != store {
			continue
		}
		n++
	}
	if n == 0 {
		return c
	}
	c.OpenMismatches = n
	c.Confidence = fmt.Sprintf("%d open mismatch", n)
	if n != 1 {
		c.Confidence += "es"
	}
	c.BasisNote = c.Confidence + " in the reconciliation list. Dismiss or fix them under Reports, Order to cash."
	return c
}

// ReportStoreParam is the single store a report run was filtered to, or "".
func ReportStoreParam(params map[string]string) string {
	for _, k := range reportStoreKeys {
		if v := strings.TrimSpace(params[k]); v != "" {
			return v
		}
	}
	return ""
}
