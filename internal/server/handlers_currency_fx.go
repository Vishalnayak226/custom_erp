package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
	"strings"
)

// Stage 37.1.4 / 37.1.5 - the FX revaluation and multi-currency reporting
// endpoints.
//
// Only two routes are added. The three reports 37.1.5 registers need no
// endpoint at all - the generic report catalog already serves, exports and
// schedules anything registered with RegisterReport, which is exactly why they
// were built as ReportDefinitions.

// handleFXRevaluation runs (or previews) a period-end revaluation of every open
// foreign-currency receivable and payable.
//
// GET is the preview and POST is the commit, deliberately split along the HTTP
// verb rather than hidden behind a `dry_run` flag on one POST. A GET cannot
// change the ledger however it is called, which is the property that makes it
// safe to put behind a button a controller will press repeatedly while
// checking the numbers - and safe against a retry, a prefetch or a double-click
// posting a second adjustment.
func handleFXRevaluation(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")

	dryRun := r.Method == http.MethodGet
	asOf := strings.TrimSpace(r.URL.Query().Get("as_of"))
	rateType := strings.TrimSpace(r.URL.Query().Get("rate_type"))

	if r.Method == http.MethodPost {
		// A body is optional: posting with no body revalues as of today at the
		// tenant's configured rate type, which is the common case at a close.
		var req struct {
			AsOf     string `json:"as_of"`
			RateType string `json:"rate_type"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err == nil {
			if strings.TrimSpace(req.AsOf) != "" {
				asOf = strings.TrimSpace(req.AsOf)
			}
			if strings.TrimSpace(req.RateType) != "" {
				rateType = strings.TrimSpace(req.RateType)
			}
		}
	} else if r.Method != http.MethodGet {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	run, err := engines.RevalueOpenForeignItems(tenantID, asOf, rateType, userID, dryRun)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(run)
}

// settlementOptionsFromRequest reads the optional settlement facts off a
// request body. Shared by the AR and AP settlement handlers so the two cannot
// spell the same two fields differently.
//
// Everything is optional and a malformed or absent body yields the zero value,
// which reproduces the pre-37.1.3 behaviour exactly: settle at today's rate.
// That is deliberate rather than lax - these endpoints are already called by
// the existing frontend with no body at all, and a settlement that started
// refusing requests it used to accept would be a regression dressed as a
// feature.
func settlementOptionsFromRequest(r *http.Request) engines.SettlementOptions {
	var body struct {
		SettlementDate string  `json:"settlement_date"`
		ExchangeRate   float64 `json:"exchange_rate"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	return engines.SettlementOptions{
		SettlementDate: strings.TrimSpace(body.SettlementDate),
		ExchangeRate:   body.ExchangeRate,
	}
}

// handleTrialBalancePresentationCurrency serves the translated trial balance.
// It exists alongside the registered report because the existing
// /finance/trial-balance endpoint is what the Finance screen already calls, and
// a currency-aware sibling keeps that screen's shape rather than forcing it
// through the generic report runner for one extra column.
func handleTrialBalancePresentationCurrency(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	result, err := engines.GetTrialBalanceInPresentationCurrency(tenantID,
		strings.TrimSpace(r.URL.Query().Get("as_of")),
		strings.TrimSpace(r.URL.Query().Get("presentation_currency")),
		strings.TrimSpace(r.URL.Query().Get("rate_type")))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(result)
}
