package engines

import (
	"custom_erp/db"
	"fmt"
	"math"
	"strings"
	"time"
)

// Stage 37.1.2 - transaction currency and functional currency on financial
// documents.
//
// THE RULE THIS FILE EXISTS TO ENFORCE: a document records what it was
// transacted in, and every number the rest of the system reasons about is in
// the tenant's functional currency. Those are two different facts about the
// same document, and a system that keeps only one of them cannot answer either
// "what did the customer agree to pay?" or "what is this worth to us?".
//
// It is attached at ValidateDocument, the one choke point every generic API
// save, CSV import and engine write already passes through, rather than at each
// financial doctype's own code. A doctype added next year gets this by being
// listed in currencyBearingDoctypes - not by someone remembering to call a
// function.

// SettingKeyFunctionalCurrency is the tenant's reporting currency. Referenced
// by name from here rather than typed as a literal at each site, so the key
// cannot drift between where it is declared and where it is read.
const SettingKeyFunctionalCurrency = "finance.functional_currency"

// currencyBearingDoctypes are the documents that carry a monetary amount which
// could legitimately be agreed in a currency other than the tenant's own.
//
// Deliberately a list, not "every doctype with an amount field": stamping a
// currency onto a POS receipt or a stock adjustment would imply a multi-currency
// capability those paths do not have, and an implied capability is worse than an
// absent one.
var currencyBearingDoctypes = map[string]bool{
	"SalesInvoice":        true,
	"SalesOrder":          true,
	"PurchaseOrder":       true,
	"VendorInvoice":       true,
	"JournalVoucher":      true,
	"ExpenseClaim":        true,
	"PaymentProposal":     true,
	"RFQ":                 true,
	"PurchaseRequisition": true,
}

// IsCurrencyBearingDoctype reports whether a doctype participates in the
// multi-currency stamp.
func IsCurrencyBearingDoctype(doctype string) bool {
	return currencyBearingDoctypes[doctype]
}

// FunctionalCurrency returns the tenant's reporting currency, always uppercase.
func FunctionalCurrency(tenantID string) string {
	code := strings.ToUpper(strings.TrimSpace(GetSettingString(tenantID, SettingKeyFunctionalCurrency)))
	if !isoCurrencyCodePattern.MatchString(code) {
		// A misconfigured setting must not silently reinterpret every existing
		// amount as some other currency. INR is what every existing row in every
		// existing deployment already is.
		return "INR"
	}
	return code
}

// documentTransactionDate finds the date a document's rate should be quoted on.
// The document's own date, not today: back-dating an invoice and having it
// picked up at today's rate is how a month-end close stops reconciling.
func documentTransactionDate(payload map[string]interface{}) string {
	for _, key := range []string{"transaction_date", "invoice_date", "posting_date", "date", "order_date", "voucher_date"} {
		value := strings.TrimSpace(fmt.Sprintf("%v", payload[key]))
		if value == "" || value == "<nil>" {
			continue
		}
		if len(value) >= 10 {
			if _, err := time.Parse("2006-01-02", value[:10]); err == nil {
				return value[:10]
			}
		}
	}
	return time.Now().UTC().Format("2006-01-02")
}

// documentAmountFields are the monetary fields a base-currency twin is written
// for. Kept in step with extractAmount's own list, since that is what approval
// slabs compare against.
var documentAmountFields = []string{"total_amount", "amount", "invoice_amount", "discount_amount"}

// ApplyDocumentCurrency stamps currency, exchange_rate, functional_currency and
// the base_* amounts onto a financial document. It is idempotent: re-saving a
// document that already carries an explicit rate keeps that rate rather than
// re-resolving it, so an invoice's agreed rate does not quietly change because
// someone opened and saved it a week later.
func ApplyDocumentCurrency(tenantID, doctype string, payload map[string]interface{}) error {
	if !currencyBearingDoctypes[doctype] {
		return nil
	}
	functional := FunctionalCurrency(tenantID)
	payload["functional_currency"] = functional

	currency := strings.ToUpper(strings.TrimSpace(fmt.Sprintf("%v", payload["currency"])))
	if currency == "" || currency == "<NIL>" {
		currency = functional
	}
	if !isoCurrencyCodePattern.MatchString(currency) {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "Currency",
			Message: "currency must be a three-letter ISO code such as INR or USD"}
	}
	payload["currency"] = currency

	rate := 1.0
	if currency != functional {
		explicit, hasExplicit := parityNumber(payload["exchange_rate"])
		switch {
		case hasExplicit && explicit > 0:
			// An operator-agreed rate wins. Contract rates exist, and refusing
			// to honour one would make the feature unusable for the case that
			// most often needs it.
			rate = explicit
		case db.DB != nil:
			resolved, err := ResolveExchangeRate(tenantID, currency, functional, documentTransactionDate(payload), "Spot")
			if err != nil {
				return &ValidationError{Code: "FIN-0021", SubFor: "Exchange Rate",
					Message: fmt.Sprintf("%s cannot be converted to %s on this document's date: %v. Add an Exchange Rate, or enter one on the document.", currency, functional, err)}
			}
			rate = resolved.Rate
			payload["exchange_rate_source"] = resolved.Source
			if resolved.RateDocumentID != "" {
				payload["exchange_rate_document"] = resolved.RateDocumentID
			}
		default:
			return &ValidationError{Code: "FIN-0021", SubFor: "Exchange Rate",
				Message: "a foreign-currency document needs an exchange rate"}
		}
		if rate <= 0 || math.IsInf(rate, 0) || math.IsNaN(rate) {
			return &ValidationError{Code: "FIN-0021", SubFor: "Exchange Rate",
				Message: "exchange rate must be a finite number greater than zero"}
		}
	}
	payload["exchange_rate"] = rate

	for _, field := range documentAmountFields {
		value, ok := parityNumber(payload[field])
		if !ok || payload[field] == nil {
			continue
		}
		// Rounded to 2 dp because it is money, and an unrounded base amount
		// makes two documents that agree to the paisa look like they disagree.
		payload["base_"+field] = math.Round(value*rate*100) / 100
	}
	return nil
}

// ConvertPostingToFunctional scales a transaction-currency debit/credit set
// into the functional currency and returns both, ready for PostDoubleEntry.
//
// The subtle part is rounding. Converting each line independently and rounding
// each one can leave the two sides differing by a rupee, which PostDoubleEntry
// correctly refuses as an unbalanced journal - so a legitimate foreign-currency
// voucher would fail on an arithmetic artifact. The rounding difference is
// absorbed into the largest line on the short side, which is the conventional
// treatment and keeps the entry balanced without distorting any line materially.
func ConvertPostingToFunctional(debits, credits map[string]int, rate float64) (map[string]int, map[string]int, map[string]float64, map[string]float64) {
	transactionDebits := map[string]float64{}
	transactionCredits := map[string]float64{}
	for code, value := range debits {
		transactionDebits[code] = float64(value)
	}
	for code, value := range credits {
		transactionCredits[code] = float64(value)
	}
	if rate <= 0 || rate == 1 {
		return debits, credits, transactionDebits, transactionCredits
	}

	convert := func(source map[string]int) (map[string]int, int) {
		out := map[string]int{}
		total := 0
		for code, value := range source {
			converted := int(math.Round(float64(value) * rate))
			out[code] = converted
			total += converted
		}
		return out, total
	}
	functionalDebits, debitTotal := convert(debits)
	functionalCredits, creditTotal := convert(credits)

	if difference := debitTotal - creditTotal; difference != 0 {
		target := functionalCredits
		if difference < 0 {
			target = functionalDebits
			difference = -difference
		}
		largest := ""
		for code, value := range target {
			if largest == "" || value > target[largest] {
				largest = code
			}
		}
		if largest != "" {
			target[largest] += difference
		}
	}
	return functionalDebits, functionalCredits, transactionDebits, transactionCredits
}

// DocumentBaseAmount returns a document's amount in the functional currency -
// the number an approval slab, a credit limit or a report must compare. It
// falls back to the raw amount for a document that predates this stamp, which
// is correct: those documents were all in the functional currency.
func DocumentBaseAmount(payload map[string]interface{}) float64 {
	for _, field := range documentAmountFields {
		if value, ok := parityNumber(payload["base_"+field]); ok && payload["base_"+field] != nil {
			return value
		}
	}
	return extractAmount(payload)
}
