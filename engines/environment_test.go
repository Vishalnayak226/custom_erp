package engines

import (
	"custom_erp/db"
	"os"
	"testing"
)

// Stage 47.0.5/47.11.6 Gate 0: ExternalSideEffectsEnabled must default to
// OFF everywhere except ENV=production, ERP_ENABLE_EXTERNAL_SIDE_EFFECTS=1
// must opt a non-production run in deliberately, and
// ERP_DISABLE_EXTERNAL_SIDE_EFFECTS=1 must win even over ENV=production -
// the emergency kill switch the acceptance criteria names explicitly.
func TestExternalSideEffectsEnabled(t *testing.T) {
	for _, key := range []string{"ENV", "ERP_ENABLE_EXTERNAL_SIDE_EFFECTS", "ERP_DISABLE_EXTERNAL_SIDE_EFFECTS"} {
		old := os.Getenv(key)
		defer os.Setenv(key, old)
	}

	cases := []struct {
		name    string
		env     string
		enable  string
		disable string
		want    bool
	}{
		{"unset ENV defaults off", "", "", "", false},
		{"dev ENV without opt-in stays off", "development", "", "", false},
		{"dev ENV with explicit opt-in turns on", "development", "1", "", true},
		{"ENV=production turns on without any override", "production", "", "", true},
		{"kill switch wins over ENV=production", "production", "", "1", false},
		{"kill switch wins over explicit opt-in", "development", "1", "1", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			os.Setenv("ENV", c.env)
			os.Setenv("ERP_ENABLE_EXTERNAL_SIDE_EFFECTS", c.enable)
			os.Setenv("ERP_DISABLE_EXTERNAL_SIDE_EFFECTS", c.disable)
			if got := ExternalSideEffectsEnabled(); got != c.want {
				t.Fatalf("ExternalSideEffectsEnabled() = %v, want %v", got, c.want)
			}
		})
	}
}

// TestQuarantineStaleExternalDispatchJobs confirms the startup sweep moves a
// Pending webhook_delivery job to Quarantined (inert - outside the runner's
// claim query) and that ReplayJob is the deliberate way back to Pending, the
// same Retry action the Async Jobs tab already exposes for Failed/
// DeadLettered/Cancelled jobs.
func TestQuarantineStaleExternalDispatchJobs(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}

	jobID, err := enqueueJobInSchema(schema, webhookDeliveryJobType, map[string]interface{}{"url": "https://example.invalid/hook"}, "")
	if err != nil {
		t.Fatalf("enqueueJobInSchema: %v", err)
	}
	defer db.DB.Exec("DELETE FROM "+schema+".async_jobs WHERE id = $1", jobID)

	// A job of a different type must not be touched by the sweep.
	otherID, err := enqueueJobInSchema(schema, "test-4700-unrelated", map[string]interface{}{}, "")
	if err != nil {
		t.Fatalf("enqueueJobInSchema (unrelated): %v", err)
	}
	defer db.DB.Exec("DELETE FROM "+schema+".async_jobs WHERE id = $1", otherID)

	n, err := quarantineStaleExternalDispatchJobsInSchema(schema)
	if err != nil {
		t.Fatalf("quarantineStaleExternalDispatchJobsInSchema: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected exactly 1 job quarantined, got %d", n)
	}

	var status string
	if err := db.DB.QueryRow("SELECT status FROM "+schema+".async_jobs WHERE id = $1", jobID).Scan(&status); err != nil {
		t.Fatalf("query status: %v", err)
	}
	if status != "Quarantined" {
		t.Fatalf("expected webhook_delivery job to be Quarantined, got %q", status)
	}
	if err := db.DB.QueryRow("SELECT status FROM "+schema+".async_jobs WHERE id = $1", otherID).Scan(&status); err != nil {
		t.Fatalf("query status (unrelated): %v", err)
	}
	if status != "Pending" {
		t.Fatalf("expected the unrelated job type to be left Pending, got %q", status)
	}

	// A second sweep must be a no-op - the job is already Quarantined, not Pending.
	if n, err := quarantineStaleExternalDispatchJobsInSchema(schema); err != nil || n != 0 {
		t.Fatalf("expected a repeated sweep to quarantine 0 more jobs, got n=%d err=%v", n, err)
	}

	if err := ReplayJob(tenantID, jobID); err != nil {
		t.Fatalf("ReplayJob on a Quarantined job: %v", err)
	}
	if err := db.DB.QueryRow("SELECT status FROM "+schema+".async_jobs WHERE id = $1", jobID).Scan(&status); err != nil {
		t.Fatalf("query status after replay: %v", err)
	}
	if status != "Pending" {
		t.Fatalf("expected ReplayJob to move the job back to Pending, got %q", status)
	}
}
