package engines

import (
	"context"
	"custom_erp/db"
	"fmt"
	"strings"
	"testing"
	"time"
)

func stage4982Report(t *testing.T) string {
	t.Helper()
	reportID := fmt.Sprintf("t4982-report-%d", time.Now().UnixNano())
	RegisterReport(ReportDefinition{
		ID: reportID, Label: "49.8.2 export test", Category: "Admin",
		Columns: []ReportColumn{{Key: "customer", Label: "Customer"}, {Key: "account_number", Label: "Account", Sensitive: true}},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			return []map[string]interface{}{{"customer": "Asha", "account_number": "ACCT-4982-SECRET"}}, nil
		},
	})
	return reportID
}

// TestStage4982ExportLinkIsOwnerBoundShortLivedAndRevocable: a completed
// export is stamped with an expiry on the export retention clock; another
// user's GET of its link is not found; past the expiry the link is not
// found; and the owner can revoke it, which drops the CSV and makes the
// link not found from then on.
func TestStage4982ExportLinkIsOwnerBoundShortLivedAndRevocable(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	reportID := stage4982Report(t)
	jobID, err := CreateReportExportJob(tenantID, reportID, "HR/Admin", nil, "cashier1")
	if err != nil {
		t.Fatalf("CreateReportExportJob: %v", err)
	}
	defer db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'ReportExportJob' AND id = $1", jobID)
	processReportExportJobs(context.Background(), schema)

	status, csvBytes, _, err := GetReportExportJob(tenantID, jobID)
	if err != nil || status != "Completed" || !strings.Contains(string(csvBytes), "Asha") {
		t.Fatalf("after the worker: status %s, csv %q, err %v", status, csvBytes, err)
	}
	var expiresAt string
	if err := db.DB.QueryRow("SELECT data->>'expires_at' FROM "+schema+".documents WHERE id = $1", jobID).Scan(&expiresAt); err != nil {
		t.Fatal(err)
	}
	at, err := time.Parse(time.RFC3339, expiresAt)
	if err != nil {
		t.Fatalf("expires_at %q is not a time: %v", expiresAt, err)
	}
	wantDays := reportExportRetentionDays(tenantID)
	if until := time.Until(at); until < time.Duration(wantDays)*24*time.Hour-time.Hour || until > time.Duration(wantDays)*24*time.Hour+time.Hour {
		t.Fatalf("expires in %s, want about %d day(s)", until, wantDays)
	}
	// "My exports" shows the expiry.
	mine, err := ListMyReportExportJobs(tenantID, "cashier1", 50)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, j := range mine {
		if j.ID == jobID {
			found = j.ExpiresAt == expiresAt
		}
	}
	if !found {
		t.Fatalf("the owner's list does not show the export with its expiry")
	}

	// Owner-bound: another user, not found; the owner, visible.
	if visible, _ := ReportExportJobVisible(tenantID, jobID, "manager1", "Cashier"); visible {
		t.Fatalf("another user can see the export link")
	}
	if visible, _ := ReportExportJobVisible(tenantID, jobID, "cashier1", "Cashier"); !visible {
		t.Fatalf("the owner cannot see their own export")
	}
	if status, _ := CancelReportExportJob(tenantID, jobID, "manager1", "Cashier"); status != "" {
		t.Fatalf("another user revoked the export")
	}

	// Short-lived: past the expiry the link is not found, before the sweep.
	if _, err := db.DB.Exec("UPDATE "+schema+".documents SET data = jsonb_set(data, '{expires_at}', to_jsonb($2::text)) WHERE id = $1", jobID, time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := GetReportExportJob(tenantID, jobID); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("an expired export still answers: %v", err)
	}
	if _, err := db.DB.Exec("UPDATE "+schema+".documents SET data = jsonb_set(data, '{expires_at}', to_jsonb($2::text)) WHERE id = $1", jobID, expiresAt); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := GetReportExportJob(tenantID, jobID); err != nil {
		t.Fatalf("unexpired again, but: %v", err)
	}

	// Revocable: the owner revokes a completed export; the CSV is gone.
	newStatus, err := CancelReportExportJob(tenantID, jobID, "cashier1", "Cashier")
	if err != nil || newStatus != "Revoked" {
		t.Fatalf("revoke: status %q, err %v", newStatus, err)
	}
	if _, _, _, err := GetReportExportJob(tenantID, jobID); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("a revoked export still answers: %v", err)
	}
	var hasCSV bool
	db.DB.QueryRow("SELECT data ? 'csv' FROM "+schema+".documents WHERE id = $1", jobID).Scan(&hasCSV)
	if hasCSV {
		t.Fatalf("the revoked export still stores its CSV")
	}
	if _, err := CancelReportExportJob(tenantID, jobID, "cashier1", "Cashier"); err == nil {
		t.Fatalf("revoking twice should say so")
	}
}

// TestStage4982SensitiveColumnsNeverReachAnExport: the export CSV a role
// without full visibility gets never carries a sensitive column's value -
// the same masking the screen applies runs before the CSV is written - and
// every registered report's column named like a sensitive bank, secret or
// personal field is flagged Sensitive.
func TestStage4982SensitiveColumnsNeverReachAnExport(t *testing.T) {
	db.InitDB(testConnStr())
	reportID := stage4982Report(t)
	def := reportRegistry[reportID]
	run := func(role string) string {
		t.Helper()
		rows, _ := def.Run("default", nil)
		csv, err := reportRowsToCSV(def, maskSensitiveColumns(def.Columns, rows, role))
		if err != nil {
			t.Fatal(err)
		}
		return csv
	}
	if csv := run("Cashier"); strings.Contains(csv, "ACCT-4982-SECRET") || !strings.Contains(csv, redactedReportValue) {
		t.Fatalf("a Cashier's export carries the sensitive value:\n%s", csv)
	}
	if csv := run(RoleSuperAdmin); !strings.Contains(csv, "ACCT-4982-SECRET") {
		t.Fatalf("a Super Admin's export lost the value:\n%s", csv)
	}
	// The async path is the same function on the same masked rows.
	schema, _ := db.GetTenantSchema("default")
	jobID, err := CreateReportExportJob("default", reportID, "Cashier", nil, "cashier1")
	if err != nil {
		t.Fatal(err)
	}
	defer db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'ReportExportJob' AND id = $1", jobID)
	processReportExportJobs(context.Background(), schema)
	if _, csvBytes, _, err := GetReportExportJob("default", jobID); err != nil || strings.Contains(string(csvBytes), "ACCT-4982-SECRET") {
		t.Fatalf("the background export for a Cashier carries the sensitive value (err %v):\n%s", err, csvBytes)
	}

	// Hidden-column review over the catalog: a column keyed like a sensitive
	// bank, secret or personal field must be flagged.
	// A personal field's name is guarded only when no record type keeps a
	// field of that name as ordinary data: Salesman.phone is personal, but a
	// customer's or supplier's phone is not, so a bare "phone" column says
	// nothing about which one a report shows. Bank and secret names are never
	// ordinary data.
	ordinaryElsewhere := func(field string) bool {
		rows, err := db.DB.Query("SELECT DISTINCT doctype_name FROM "+schema+".doctype_fields WHERE fieldname = $1", field)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		for rows.Next() {
			var dt string
			if err := rows.Scan(&dt); err != nil {
				t.Fatal(err)
			}
			if _, sensitive := sensitiveFields[dt][field]; !sensitive {
				return true
			}
		}
		return false
	}
	guarded := map[string]bool{}
	for _, doctype := range SensitiveFieldDoctypes() {
		for field, rule := range sensitiveFields[doctype] {
			switch rule.Category {
			case SensitiveCategoryBank, SensitiveCategorySecret:
				guarded[field] = true
			case SensitiveCategoryPersonal:
				if !ordinaryElsewhere(field) {
					guarded[field] = true
				}
			}
		}
	}
	for _, id := range reportRegistryOrder {
		for _, c := range reportRegistry[id].Columns {
			if guarded[c.Key] && !c.Sensitive {
				t.Errorf("report %s: column %s is a sensitive field but is not flagged Sensitive", id, c.Key)
			}
		}
	}
}
