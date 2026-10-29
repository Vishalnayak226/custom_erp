package engines

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

// Stage 73.D.8: once a document has posted to the ledger, the figures it
// posted from are fixed. A record-form save may still change what it says - a
// note, a due date, a reference - but not its money: that is corrected with a
// credit or debit note, or a reversal, each of which posts its own entry.
// Before this, a generic save could change a posted invoice's total_amount
// and leave the ledger saying something else.
type postedAmountRule struct {
	posted func(status string, data map[string]interface{}) bool
	fields []string
}

func postedIn(statuses ...string) func(string, map[string]interface{}) bool {
	return func(status string, _ map[string]interface{}) bool {
		for _, s := range statuses {
			if status == s {
				return true
			}
		}
		return false
	}
}

var postedAmountRules = map[string]postedAmountRule{
	"SalesInvoice": {postedIn("Approved", "Paid"), []string{"total_amount", "taxable_amount", "non_taxable", "cgst", "sgst", "igst", "total_tax", "currency", "exchange_rate", "deferred_revenue"}},
	// A supplier bill posts when it is paid (in part or whole).
	"VendorInvoice": {func(status string, data map[string]interface{}) bool {
		return status == "Paid" || vendorInvoiceHasPayments(data)
	}, []string{"invoice_amount", "gst_amount", "currency", "exchange_rate"}},
	"CreditNote":       {postedIn("Posted"), []string{"amount", "sales_invoice_id", "taxable_amount", "cgst", "sgst", "igst"}},
	"DebitNote":        {postedIn("Posted"), []string{"amount", "gst_amount"}},
	"VendorCreditNote": {postedIn("Posted"), []string{"amount", "gst_amount"}},
	"JournalVoucher":   {postedIn("Posted"), []string{"lines", "voucher_date", "currency", "exchange_rate"}},
}

// unpostingStatuses are the statuses that would say a posted document never
// counted. Only an engine's own reversal may write them (in SQL, not here).
var unpostingStatuses = map[string]bool{"Cancelled": true, "Draft": true, "Rejected": true, "Void": true, "Reversed": true}

// validatePostedAmountsFrozen refuses a change to a posted document's money.
func validatePostedAmountsFrozen(doctype, docID, priorStatus string, priorData, payload map[string]interface{}) error {
	rule, ok := postedAmountRules[doctype]
	if !ok || priorData == nil || !rule.posted(priorStatus, priorData) {
		return nil
	}
	// Stage 47.16.4: nor can a save take it back out of the accounts. A
	// posted invoice saved as Cancelled left its receivable and the GST it
	// declared in the ledger while the invoice said it was gone - the
	// instrument for that is a credit or debit note (CGST s.34) or the
	// document's own reversal, each posting its own entry.
	if s, sent := payload["status"]; sent && s != nil {
		if status := strings.TrimSpace(fmt.Sprintf("%v", s)); status != priorStatus && unpostingStatuses[status] {
			return &ValidationError{Code: "FIN-0844", Message: fmt.Sprintf(
				"%s %s has posted to the accounts, so a save cannot make it %s - reverse it with a credit or debit note, or its own reversal", doctype, docID, status)}
		}
	}
	for _, field := range rule.fields {
		after, sent := payload[field]
		before := comparablePostedValue(priorData[field])
		// Only what the document posted with is frozen: a field the stored
		// record never had (an older invoice saved before the currency stamp,
		// which every save now adds) is the server filling it in, not a change.
		if !sent || before == "" {
			continue
		}
		if !reflect.DeepEqual(before, comparablePostedValue(after)) {
			return &ValidationError{Code: "FIN-0844", Message: fmt.Sprintf(
				"%s %s has posted to the accounts, so its %s cannot change - correct it with a credit or debit note, or reverse it", doctype, docID, field)}
		}
	}
	return nil
}

// comparablePostedValue is normaliseEditValue (numbers sent as text, blank
// and missing are the same), plus a line table stored as JSON text compared
// by content, so re-saving unchanged lines is not a change.
func comparablePostedValue(v interface{}) interface{} {
	if s, ok := v.(string); ok {
		if t := strings.TrimSpace(s); strings.HasPrefix(t, "[") || strings.HasPrefix(t, "{") {
			var parsed interface{}
			if json.Unmarshal([]byte(t), &parsed) == nil {
				return normaliseEditValue(parsed)
			}
		}
	}
	return normaliseEditValue(v)
}
