package engines

import (
	"context"
	"custom_erp/db"
	"regexp"
	"strings"
	"testing"
	"time"
)

// Stage 49.18.3: one event, one piece of evidence. A detection over many
// audit rows writes ONE bounded SECURITY_ALERT row that points at its source
// rows - by action, window and up to three correlation ids - and copies none
// of their content. The forensic pack and the investigation trail read the
// source rows through those references; nothing is stored twice.
func TestStage49183DetectionReferencesItsEvidenceInsteadOfCopyingIt(t *testing.T) {
	db.InitDB(testConnStr())
	const tenant = "default"
	s := stage4911Suffix()
	actor := "onevidence-" + s
	payload := "PAYLOAD-" + s + "-" + strings.Repeat("x", 1500)
	start := time.Now()
	cleanup := func() {
		db.DB.Exec(`DELETE FROM tenant_default.audit_logs WHERE user_id = $1`, actor)
		db.DB.Exec(`DELETE FROM tenant_default.audit_logs WHERE action = $1 AND details LIKE $2`, SecurityAlertAction, "%"+actor+"%")
	}
	cleanup()
	defer cleanup()

	var rule SecurityDetectionRule
	for _, r := range SecurityDetectionRules() {
		if r.byActor && r.special == nil && len(r.actions) > 0 {
			rule = r
			break
		}
	}
	if rule.Key == "" {
		t.Fatal("no per-actor rule to drive")
	}
	status := "SUCCESS"
	if len(rule.statuses) > 0 {
		status = rule.statuses[0]
	}
	// Twenty times the threshold, each row with its own correlation and a
	// large detail - the alert must stay one small row whatever the volume.
	n := rule.Threshold * 20
	for i := 0; i < n; i++ {
		ctx := WithCorrelationID(context.Background(), stage4911UUID())
		LogAuditEventCtx(ctx, tenant, actor, rule.actions[0], status, payload)
	}
	if _, err := RunSecurityDetections(tenant, time.Now()); err != nil {
		t.Fatal(err)
	}

	rows, err := db.DB.Query(`SELECT details FROM tenant_default.audit_logs
		WHERE action = $1 AND details LIKE $2 AND created_at > $3::timestamp`, SecurityAlertAction, "%"+actor+"%", utcStamp(start.Add(-time.Second)))
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var alerts []string
	for rows.Next() {
		var d string
		rows.Scan(&d)
		alerts = append(alerts, d)
	}
	if len(alerts) != 1 {
		t.Fatalf("%d source rows produced %d alert rows, want exactly 1", n, len(alerts))
	}
	alert := alerts[0]
	if len(alert) > 2048 {
		t.Errorf("alert row is %d bytes; it must stay bounded whatever the volume", len(alert))
	}
	if strings.Contains(alert, "PAYLOAD-"+s) {
		t.Error("the alert copied a source row's content; it must only reference it")
	}
	ids := regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`).FindAllString(alert, -1)
	if len(ids) == 0 || len(ids) > 3 {
		t.Errorf("alert carries %d correlation ids, want 1-3 references: %s", len(ids), alert)
	}
	if !strings.Contains(alert, "Evidence: audit rows "+rule.actions[0]) {
		t.Errorf("alert does not name the rows it rests on: %s", alert)
	}
}
