package engines

import (
	"context"
	"strings"
	"testing"

	"custom_erp/db"
)

// 146.5.6c / 170.43: the export freeze is enforced on requests, so every
// scheduled export has to honour it itself - a dashboard digest, a scheduled
// report, and a webhook carrying either that was queued before the freeze.
// Nothing leaves, the schedule still moves on (no backlog), and the next run
// after unfreezing delivers.
func TestExportFreezeStopsDigestsScheduledReportsAndTheirWebhooks(t *testing.T) {
	tenantID, schema := outsideCallsTenant(t)
	ctx := context.Background()
	if err := SetExportFreeze(tenantID, true, "tester", "suspected leak"); err != nil {
		t.Fatalf("freeze: %v", err)
	}
	t.Cleanup(func() { _ = SetSetting(tenantID, SettingFreezeExports, "false", "test") })

	outbox := func(event, key, id string) (n int) {
		if err := db.DB.QueryRow("SELECT COUNT(*) FROM "+schema+".integration_event_outbox WHERE event_name = $1 AND payload->>'"+key+"' = $2", event, id).Scan(&n); err != nil {
			t.Fatalf("count outbox: %v", err)
		}
		return n
	}

	report := map[string]interface{}{"report_id": "current-stock", "frequency": "Daily", "requested_role": "HR/Admin",
		"recipient_email": "ops@example.test", "next_run_date": "2026-01-01"}
	runScheduledReport(ctx, schema, tenantID, "SR-FREEZE-170", "", report)
	if report["last_run_status"] != exportsPausedRunStatus {
		t.Errorf("scheduled report under the freeze: last_run_status = %v, want %q", report["last_run_status"], exportsPausedRunStatus)
	}
	if _, has := report["last_run_rows"]; has {
		t.Error("a refused run must not report a row count")
	}
	if report["next_run_date"] == "2026-01-01" {
		t.Error("next_run_date did not move on, so the report would pile up for the unfreeze")
	}
	if n := outbox("report.scheduled_delivery", "schedule_id", "SR-FREEZE-170"); n != 0 {
		t.Errorf("scheduled report under the freeze queued %d deliveries, want 0", n)
	}

	digest := map[string]interface{}{"dashboard_layout_id": "NO-SUCH-LAYOUT", "frequency": "Weekly",
		"recipient_email": "ops@example.test", "next_run_date": "2026-01-01"}
	runDashboardDigest(ctx, schema, tenantID, "DD-FREEZE-170", "", digest)
	if digest["last_run_status"] != exportsPausedRunStatus {
		t.Errorf("digest under the freeze: last_run_status = %v, want %q", digest["last_run_status"], exportsPausedRunStatus)
	}
	if digest["next_run_date"] == "2026-01-01" {
		t.Error("digest next_run_date did not move on")
	}
	if n := outbox("dashboard.digest_delivery", "digest_id", "DD-FREEZE-170"); n != 0 {
		t.Errorf("digest under the freeze queued %d deliveries, want 0", n)
	}

	// A webhook queued before the freeze: an export is held back, any other
	// event goes on to the usual outside-call checks.
	for _, event := range []string{"report.scheduled_delivery", "dashboard.digest_delivery", "pim.export.scheduled_delivery"} {
		res, err := deliverWebhook(ctx, schema, Job{ID: "freeze-170-" + event, JobType: webhookDeliveryJobType,
			Payload: map[string]interface{}{"url": "https://example.test/hook", "event_name": event, "event_payload": map[string]interface{}{"csv": "a,b\n"}}})
		if err != nil || res["skipped"] != true || !strings.Contains(res["reason"].(string), "paused") {
			t.Errorf("%s webhook under the freeze = %v %v, want skipped as paused", event, res, err)
		}
	}
	if res, _ := deliverWebhook(ctx, schema, Job{ID: "freeze-170-order", JobType: webhookDeliveryJobType,
		Payload: map[string]interface{}{"url": "https://example.test/hook", "event_name": "order.created", "event_payload": map[string]interface{}{}}}); res["skipped"] == true {
		t.Errorf("an ordinary event was held back by the export freeze: %v", res)
	}

	if err := SetExportFreeze(tenantID, false, "tester", "all clear now"); err != nil {
		t.Fatalf("unfreeze: %v", err)
	}
	report["next_run_date"] = "2026-01-01"
	runScheduledReport(ctx, schema, tenantID, "SR-FREEZE-170", "", report)
	if report["last_run_status"] != "Delivered" {
		t.Errorf("after unfreezing: last_run_status = %v, want Delivered", report["last_run_status"])
	}
	if n := outbox("report.scheduled_delivery", "schedule_id", "SR-FREEZE-170"); n != 1 {
		t.Errorf("after unfreezing: %d deliveries queued, want 1", n)
	}
}
