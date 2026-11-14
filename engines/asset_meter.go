package engines

import (
	"custom_erp/db"
	"fmt"
	"strings"
	"time"
)

// Stage 170.9: meter-based maintenance.
//
// An asset has meter readings (AssetMeterReading: date, reading, unit). A
// reading below the one before it is refused. A MaintenanceSchedule can carry
// interval_meter as well as interval_days; it falls due at whichever comes
// first, and the existing scheduling worker raises the order. The schedule
// remembers the reading at the last service (meter_baseline).

const meterReadingCode = "ASSET-1727"

func meterRefused(sub, format string, args ...interface{}) error {
	return &ValidationError{Code: meterReadingCode, SubFor: sub, Message: fmt.Sprintf(format, args...)}
}

// ValidateAssetMeterReadingDocument keeps an asset's readings in order: not
// below the reading before it (by date), not above the one after it.
func ValidateAssetMeterReadingDocument(tenantID string, payload map[string]interface{}) error {
	asset := strings.TrimSpace(strField(payload, "asset"))
	date := strings.TrimSpace(strField(payload, "reading_date"))
	reading, ok := parityNumber(payload["reading"])
	if asset == "" || date == "" || !ok {
		return nil // the field rules report what is missing
	}
	if reading < 0 {
		return meterRefused("Reading", "The reading cannot be negative.")
	}
	if _, err := time.Parse("2006-01-02", date); err != nil {
		return meterRefused("Date", "Use a date like 2026-11-04.")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	self := strField(payload, "id")
	var prev, next *float64
	q := func(query string) (*float64, error) {
		var v *float64
		err := db.DB.QueryRow(fmt.Sprintf(query, schema), asset, date, self).Scan(&v)
		return v, err
	}
	if prev, err = q(`SELECT MAX((data->>'reading')::numeric)::float8 FROM %s.documents
		WHERE doctype = 'AssetMeterReading' AND deleted_at IS NULL AND data->>'asset' = $1 AND data->>'reading_date' <= $2 AND id <> $3`); err != nil {
		return err
	}
	if next, err = q(`SELECT MIN((data->>'reading')::numeric)::float8 FROM %s.documents
		WHERE doctype = 'AssetMeterReading' AND deleted_at IS NULL AND data->>'asset' = $1 AND data->>'reading_date' > $2 AND id <> $3`); err != nil {
		return err
	}
	if prev != nil && reading < *prev {
		return meterRefused("Reading", "The reading cannot be below the last one, %.2f.", *prev)
	}
	if next != nil && reading > *next {
		return meterRefused("Reading", "A later reading, %.2f, is lower than this one.", *next)
	}
	return nil
}

// latestMeterReading is the asset's newest reading (highest on a tie of date).
func latestMeterReading(schema, asset string) (float64, bool) {
	var v *float64
	err := db.DB.QueryRow(fmt.Sprintf(`SELECT (data->>'reading')::numeric::float8 FROM %s.documents
		WHERE doctype = 'AssetMeterReading' AND deleted_at IS NULL AND data->>'asset' = $1
		ORDER BY data->>'reading_date' DESC, (data->>'reading')::numeric DESC LIMIT 1`, schema), asset).Scan(&v)
	if err != nil || v == nil {
		return 0, false
	}
	return *v, true
}
