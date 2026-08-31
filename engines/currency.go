package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var isoCurrencyCodePattern = regexp.MustCompile(`^[A-Z]{3}$`)

type ExchangeRateResolution struct {
	FromCurrency    string  `json:"from_currency"`
	ToCurrency      string  `json:"to_currency"`
	Rate            float64 `json:"rate"`
	RateType        string  `json:"rate_type"`
	EffectiveFrom   string  `json:"effective_from,omitempty"`
	EffectiveTo     string  `json:"effective_to,omitempty"`
	Source          string  `json:"source"`
	SourceReference string  `json:"source_reference,omitempty"`
	RateDocumentID  string  `json:"rate_document_id,omitempty"`
	Inverted        bool    `json:"inverted"`
}

func parityNumber(value interface{}) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		n, err := v.Float64()
		return n, err == nil
	default:
		n, err := strconv.ParseFloat(strings.TrimSpace(fmt.Sprintf("%v", value)), 64)
		return n, err == nil
	}
}

func validateISODate(label, value string, required bool) error {
	value = strings.TrimSpace(value)
	if value == "" && !required {
		return nil
	}
	if value == "" {
		return &ValidationError{Code: "GLOBAL-0001", SubFor: label, Message: label + " is required"}
	}
	if _, err := time.Parse("2006-01-02", value); err != nil {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: label, Message: label + " must use YYYY-MM-DD"}
	}
	return nil
}

func ValidateCurrencyDocument(payload map[string]interface{}) error {
	code := strings.ToUpper(strings.TrimSpace(fmt.Sprintf("%v", payload["code"])))
	payload["code"] = code
	if !isoCurrencyCodePattern.MatchString(code) {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "ISO Currency Code", Message: "currency code must be exactly three uppercase letters, for example INR or USD"}
	}
	decimalPlaces, ok := parityNumber(payload["decimal_places"])
	if !ok || decimalPlaces < 0 || decimalPlaces > 4 || decimalPlaces != math.Trunc(decimalPlaces) {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "Decimal Places", Message: "decimal_places must be a whole number from 0 to 4"}
	}

	// Stage 37.1.3: a Currency's document id MUST be its ISO code, and this is
	// where that becomes self-enforcing rather than folklore.
	//
	// The system already depends on it from two directions that never meet.
	// `SalesInvoice.currency` and friends are declared as Link fields, and the
	// generic Link check (META-0198) resolves against documents.id - while
	// ApplyDocumentCurrency requires the stored value to be a three-letter ISO
	// code. So the only value that satisfies both is one where id == code. The
	// seeded INR row happens to be keyed that way, which is why nothing noticed.
	//
	// Created any other way - "CUR-USD", an auto-generated id, anything - the
	// currency saves happily and then EVERY invoice that selects it fails with
	// "Linked Currency record with ID \"USD\" does not exist", a message that
	// points at the invoice and names a record that is sitting right there.
	// Found over live HTTP while verifying 37.1.3; the unit tests could not have
	// caught it because they never travel the Link check.
	//
	// Refused at authoring time, where the fix is one field, instead of at every
	// future document that references it. Absent id (an engine-generated
	// document) is left alone.
	if id := currencyField(payload, "id"); id != "" && !strings.EqualFold(id, code) {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "ISO Currency Code",
			Message: fmt.Sprintf("a Currency's ID must be its ISO code - use %q as the ID, not %q. Every document that selects a currency links to it by ID, so any other ID makes this currency unusable on invoices and orders.", code, id)}
	}
	return nil
}

// currencyField reads an optional payload key as a trimmed string.
//
// Stage 37.1.3 found the need for this the expensive way, over live HTTP: this
// function used to spell every read as fmt.Sprintf("%v", payload[key]), and
// fmt.Sprintf("%v", nil) is the four-character string "<nil>", not "". So an
// optional field that was simply not sent arrived here as a NON-empty value
// which then failed its format check. An ExchangeRate with no end date - an
// open-ended rate, which is how essentially every rate table is really
// maintained - was impossible to save, and the message blamed a field the user
// had never filled in ("Effective To must use YYYY-MM-DD").
//
// It delegates to the existing payloadString (document_mirror_fields.go) rather
// than defining a second one, and adds the trim this file's checks rely on.
func currencyField(payload map[string]interface{}, key string) string {
	return strings.TrimSpace(payloadString(payload, key))
}

func ValidateExchangeRateDocument(payload map[string]interface{}) error {
	from := currencyField(payload, "from_currency")
	to := currencyField(payload, "to_currency")
	if from != "" && from == to {
		return &ValidationError{Code: "FIN-0021", SubFor: "To Currency", Message: "an exchange rate must convert between two different currencies"}
	}
	rate, ok := parityNumber(payload["rate"])
	if !ok || rate <= 0 || math.IsInf(rate, 0) || math.IsNaN(rate) {
		return &ValidationError{Code: "FIN-0021", SubFor: "Exchange Rate", Message: "exchange rate must be a finite number greater than zero"}
	}
	fromDate := currencyField(payload, "effective_from")
	toDate := currencyField(payload, "effective_to")
	if err := validateISODate("Effective From", fromDate, true); err != nil {
		return err
	}
	if err := validateISODate("Effective To", toDate, false); err != nil {
		return err
	}
	if toDate != "" && toDate < fromDate {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "Effective To", Message: "effective_to cannot be before effective_from"}
	}
	if currencyField(payload, "source") == "Imported" && currencyField(payload, "source_reference") == "" {
		return &ValidationError{Code: "GLOBAL-0001", SubFor: "Source Reference", Message: "an Imported exchange rate needs a source reference for auditability"}
	}
	return nil
}

// ValidateParityFoundationDocument is attached once to ValidateDocument so
// both browser/API saves and BulkImportCSV enforce these rules.
func ValidateParityFoundationDocument(tenantID, doctype string, payload map[string]interface{}) error {
	switch doctype {
	case "PIMProductGroup":
		return ValidatePIMProductGroupDocument(tenantID, payload)
	// Stage 36.2: the task/workflow doctypes join here rather than growing a
	// second validation entry point, so a task or workflow written through the
	// generic document API is subject to the same state machine and condition
	// vocabulary the engine enforces on its own writes.
	case "PIMTask":
		return ValidatePIMTaskDocument(tenantID, payload)
	case "PIMTaskTemplate":
		return ValidatePIMTaskTemplateDocument(tenantID, payload)
	case "PIMWorkflowDefinition":
		return ValidatePIMWorkflowDefinitionDocument(tenantID, payload)
	// Stage 36.5: joins here rather than growing a second validation entry
	// point, so a rule written through the generic document API is subject
	// to the same closed-vocabulary check the engine enforces on its own writes.
	case "PIMTransformRule":
		return ValidatePIMTransformRuleDocument(tenantID, payload)
	case "PIMImportTemplate":
		return ValidatePIMImportTemplateDocument(tenantID, payload)
	case "PIMImportSchedule":
		return ValidatePIMImportScheduleDocument(tenantID, payload)
	// Stage 36.4: joins here for the same reason 36.3's import doctypes do -
	// a template/schedule/catalog written through the generic document API
	// is subject to the same column/channel/token rules the engine enforces
	// on its own writes.
	case "PIMExportTemplate":
		return ValidatePIMExportTemplateDocument(tenantID, payload)
	case "PIMExportSchedule":
		return ValidatePIMExportScheduleDocument(tenantID, payload)
	case "PIMCatalog":
		return ValidatePIMCatalogDocument(tenantID, payload)
	case "Currency":
		return ValidateCurrencyDocument(payload)
	case "ExchangeRate":
		return ValidateExchangeRateDocument(payload)
	// Stage 37.2.2: joins here for the same reason 36.4's PIM doctypes do -
	// a generic-API write to IntercompanyTransaction is subject to the same
	// referential checks CreateIntercompanyTransaction's dedicated path
	// enforces.
	case "IntercompanyTransaction":
		return ValidateIntercompanyTransactionDocument(tenantID, payload)
	default:
		return nil
	}
}

type currencyRef struct {
	ID   string
	Code string
}

func resolveCurrencyRef(tenantID, reference string) (currencyRef, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return currencyRef{}, err
	}
	reference = strings.TrimSpace(reference)
	var result currencyRef
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT id, UPPER(COALESCE(data->>'code',''))
		FROM %s.documents WHERE doctype = 'Currency' AND deleted_at IS NULL
		AND status = 'Active' AND (id = $1 OR UPPER(data->>'code') = UPPER($1))
		ORDER BY CASE WHEN id = $1 THEN 0 ELSE 1 END LIMIT 1`, schema), reference).Scan(&result.ID, &result.Code)
	if err != nil {
		return currencyRef{}, fmt.Errorf("active currency %q not found", reference)
	}
	return result, nil
}

func findExchangeRate(tenantID string, from, to currencyRef, onDate, rateType string) (*ExchangeRateResolution, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	var id, effectiveFrom, effectiveTo, source, sourceRef string
	var rate float64
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT id,
		(data->>'rate')::numeric, data->>'effective_from',
		COALESCE(data->>'effective_to',''), COALESCE(data->>'source',''),
		COALESCE(data->>'source_reference','')
		FROM %s.documents
		WHERE doctype = 'ExchangeRate' AND deleted_at IS NULL AND status = 'Active'
		  AND data->>'from_currency' IN ($1, $2)
		  AND data->>'to_currency' IN ($3, $4)
		  AND COALESCE(data->>'rate_type','Spot') = $5
		  AND data->>'rate' ~ '^[0-9]+([.][0-9]+)?$'
		  AND (data->>'rate')::numeric > 0
		  AND data->>'effective_from' <= $6
		  AND (COALESCE(data->>'effective_to','') = '' OR data->>'effective_to' >= $6)
		ORDER BY data->>'effective_from' DESC, updated_at DESC, id DESC LIMIT 1`, schema),
		from.ID, from.Code, to.ID, to.Code, rateType, onDate).Scan(&id, &rate, &effectiveFrom, &effectiveTo, &source, &sourceRef)
	if err != nil {
		return nil, err
	}
	return &ExchangeRateResolution{
		FromCurrency: from.Code, ToCurrency: to.Code, Rate: rate, RateType: rateType,
		EffectiveFrom: effectiveFrom, EffectiveTo: effectiveTo, Source: source,
		SourceReference: sourceRef, RateDocumentID: id,
	}, nil
}

// ResolveExchangeRate returns the latest active rate whose effective window
// contains onDate. A stored reverse pair is accepted by inversion so tenants
// do not need to maintain duplicate USD->INR and INR->USD rows.
func ResolveExchangeRate(tenantID, fromReference, toReference, onDate, rateType string) (*ExchangeRateResolution, error) {
	from, err := resolveCurrencyRef(tenantID, fromReference)
	if err != nil {
		return nil, err
	}
	to, err := resolveCurrencyRef(tenantID, toReference)
	if err != nil {
		return nil, err
	}
	if from.ID == to.ID {
		return &ExchangeRateResolution{FromCurrency: from.Code, ToCurrency: to.Code, Rate: 1, RateType: "Identity", Source: "System"}, nil
	}
	if strings.TrimSpace(onDate) == "" {
		onDate = time.Now().UTC().Format("2006-01-02")
	}
	if err := validateISODate("Rate Date", onDate, true); err != nil {
		return nil, err
	}
	if strings.TrimSpace(rateType) == "" {
		rateType = "Spot"
	}
	if rateType != "Spot" && rateType != "Average" && rateType != "Closing" {
		return nil, fmt.Errorf("rate_type must be Spot, Average, or Closing")
	}
	if direct, directErr := findExchangeRate(tenantID, from, to, onDate, rateType); directErr == nil {
		return direct, nil
	} else if directErr != sql.ErrNoRows {
		return nil, directErr
	}
	reverse, reverseErr := findExchangeRate(tenantID, to, from, onDate, rateType)
	if reverseErr != nil && reverseErr != sql.ErrNoRows {
		return nil, reverseErr
	}
	if reverseErr == sql.ErrNoRows {
		return nil, &ValidationError{Code: "FIN-0021", Message: fmt.Sprintf("exchange rate is missing for %s to %s on %s (%s)", from.Code, to.Code, onDate, rateType)}
	}
	reverse.FromCurrency = from.Code
	reverse.ToCurrency = to.Code
	reverse.Rate = 1 / reverse.Rate
	reverse.Inverted = true
	return reverse, nil
}
