package engines

import (
	"custom_erp/db"
	"testing"
)

// Stage 49.18.7: a swept log levels off at its retention window; evidence
// kept on purpose keeps growing.
func TestStage49187ForecastLevelsOffSweptTablesAndGrowsEvidence(t *testing.T) {
	db.InitDB(testConnStr())

	const marker = "forecast-test-49187"
	if _, err := db.DB.Exec(`INSERT INTO tenant_default.integration_event_log (id, event_id, channel, status, error_message, created_at)
		SELECT gen_random_uuid(), gen_random_uuid(), $1, 'Sent', '', NOW() - (g % 30) * INTERVAL '1 day' FROM generate_series(1, 300) g`, marker); err != nil {
		t.Fatal(err)
	}
	defer db.DB.Exec(`DELETE FROM tenant_default.integration_event_log WHERE channel = $1`, marker)

	rows, err := ForecastStateGrowth("default")
	if err != nil {
		t.Fatal(err)
	}
	byTable := map[string]StateForecast{}
	for _, r := range rows {
		byTable[r.Table] = r
	}
	log, ok := byTable["integration_event_log"]
	if !ok {
		t.Fatalf("integration_event_log missing from the forecast: %+v", rows)
	}
	if log.PerDay < 10 || log.Retention != "platform.integration_log_retention_days" || log.RetentionDays <= 0 {
		t.Errorf("integration_event_log: %+v - want >= 10/day bounded by its retention setting", log)
	}
	if log.Year1 != log.Year5 || log.Year1 <= 0 {
		t.Errorf("a swept table must level off: year1 %d, year5 %d", log.Year1, log.Year5)
	}
	for _, r := range rows {
		if r.RetentionDays == 0 && r.PerDay > 0 && r.Year5 <= r.Year1 {
			t.Errorf("%s is kept and growing but year5 (%d) <= year1 (%d)", r.Table, r.Year5, r.Year1)
		}
	}
}
