package engines

// Stage 37.1.5 - multi-currency reporting by selected rate type.
//
// All three are ReportDefinitions rather than bespoke endpoints, for the same
// reason 42.1's traceability reports are: registering here means they inherit
// the generic report screen, CSV/PDF export, scheduled delivery, column-level
// masking and the role gate with no new frontend code at all.
//
// Between them they answer the three questions multi-currency actually raises:
// what am I exposed to right now (exposure), where did the FX number on my P&L
// come from (register), and what does my ledger look like in someone else's
// currency (presentation trial balance).

func init() {
	// 37.1.4's read-only half. Runs the revaluation calculation in dry-run and
	// reports what it WOULD post - so a treasurer can watch exposure daily
	// without anybody posting anything, and a controller can preview a close.
	RegisterReport(ReportDefinition{
		ID: "fx-open-item-exposure", Label: "Open FX Exposure", Category: "Finance",
		Columns: []ReportColumn{
			{Key: "doctype", Label: "Document Type"},
			{Key: "document_id", Label: "Document"},
			{Key: "currency", Label: "Currency"},
			{Key: "transaction_amount", Label: "Amount (txn)"},
			{Key: "booked_rate", Label: "Booked Rate"},
			{Key: "booked_amount", Label: "Booked (functional)"},
			{Key: "carrying_amount", Label: "Carrying"},
			{Key: "closing_rate", Label: "Rate Now"},
			{Key: "revalued_amount", Label: "Revalued"},
			{Key: "adjustment", Label: "Unrealised Movement"},
			{Key: "previously_revalued_on", Label: "Last Revalued"},
			{Key: "skipped_reason", Label: "Note"},
		},
		Params: []ReportParam{
			{Key: "as_of", Label: "As Of (YYYY-MM-DD, default today)", Type: "text"},
			{Key: "rate_type", Label: "Rate Type (Closing/Spot/Average)", Type: "text"},
		},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			rows, err := GetOpenFXExposure(tenantID, params["as_of"], params["rate_type"])
			if err != nil {
				return nil, err
			}
			return structsToRows(rows)
		},
	})

	// 37.1.3 + 37.1.4's audit trail. Reads gl_postings directly: the postings
	// are the record, and a separate FX log could only ever disagree with them.
	RegisterReport(ReportDefinition{
		ID: "fx-gain-loss-register", Label: "FX Gain/Loss Register", Category: "Finance",
		Columns: []ReportColumn{
			{Key: "posted_at", Label: "Posted"},
			{Key: "kind", Label: "Kind"},
			{Key: "account_code", Label: "Account"},
			{Key: "account_name", Label: "Account Name"},
			{Key: "document_type", Label: "Document Type"},
			{Key: "document_id", Label: "Document"},
			{Key: "currency", Label: "Currency"},
			{Key: "exchange_rate", Label: "Rate"},
			{Key: "gain", Label: "Gain"},
			{Key: "loss", Label: "Loss"},
			{Key: "net", Label: "Net"},
		},
		Params: []ReportParam{
			{Key: "from_date", Label: "From (YYYY-MM-DD, default 3 months back)", Type: "text"},
			{Key: "to_date", Label: "To (YYYY-MM-DD, default today)", Type: "text"},
			{Key: "kind", Label: "Kind (realised / unrealised, blank for both)", Type: "text"},
		},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			rows, err := GetFXGainLossRegister(tenantID, params["from_date"], params["to_date"], params["kind"])
			if err != nil {
				return nil, err
			}
			return structsToRows(rows)
		},
	})

	// 37.1.5 proper - the ledger restated into another currency at a chosen
	// rate type. Its single-rate limitation is stated in the engine's own
	// comment and carried onto every row via the Rate Type column, so the
	// report cannot be mistaken for an IAS 21 translation.
	RegisterReport(ReportDefinition{
		ID: "trial-balance-presentation-currency", Label: "Trial Balance in Presentation Currency", Category: "Finance",
		Columns: []ReportColumn{
			{Key: "account_code", Label: "Account"},
			{Key: "account_name", Label: "Account Name"},
			{Key: "account_type", Label: "Type"},
			{Key: "functional_balance", Label: "Balance (functional)"},
			{Key: "presentation_balance", Label: "Balance (presentation)"},
			{Key: "presentation_currency", Label: "Currency"},
			{Key: "rate_applied", Label: "Rate"},
			{Key: "rate_type", Label: "Rate Type"},
		},
		Params: []ReportParam{
			{Key: "as_of", Label: "As Of (YYYY-MM-DD, default today)", Type: "text"},
			{Key: "presentation_currency", Label: "Present In (ISO code, e.g. USD)", Type: "text"},
			{Key: "rate_type", Label: "Rate Type (Closing/Spot/Average)", Type: "text"},
		},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			result, err := GetTrialBalanceInPresentationCurrency(tenantID, params["as_of"],
				params["presentation_currency"], params["rate_type"])
			if err != nil {
				return nil, err
			}
			return structsToRows(result["rows"])
		},
	})
}
