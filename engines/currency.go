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
	return nil
}

func ValidateExchangeRateDocument(payload map[string]interface{}) error {
	from := strings.TrimSpace(fmt.Sprintf("%v", payload["from_currency"]))
	to := strings.TrimSpace(fmt.Sprintf("%v", payload["to_currency"]))
	if from != "" && from == to {
		return &ValidationError{Code: "FIN-0021", SubFor: "To Currency", Message: "an exchange rate must convert between two different currencies"}
	}
	rate, ok := parityNumber(payload["rate"])
	if !ok || rate <= 0 || math.IsInf(rate, 0) || math.IsNaN(rate) {
		return &ValidationError{Code: "FIN-0021", SubFor: "Exchange Rate", Message: "exchange rate must be a finite number greater than zero"}
	}
	fromDate := strings.TrimSpace(fmt.Sprintf("%v", payload["effective_from"]))
	toDate := strings.TrimSpace(fmt.Sprintf("%v", payload["effective_to"]))
	if err := validateISODate("Effective From", fromDate, true); err != nil {
		return err
	}
	if err := validateISODate("Effective To", toDate, false); err != nil {
		return err
	}
	if toDate != "" && toDate < fromDate {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "Effective To", Message: "effective_to cannot be before effective_from"}
	}
	if strings.TrimSpace(fmt.Sprintf("%v", payload["source"])) == "Imported" && strings.TrimSpace(fmt.Sprintf("%v", payload["source_reference"])) == "" {
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
	case "Currency":
		return ValidateCurrencyDocument(payload)
	case "ExchangeRate":
		return ValidateExchangeRateDocument(payload)
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
