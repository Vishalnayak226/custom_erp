package engines

import (
	"crypto/sha256"
	"custom_erp/db"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
)

// TestStage47133ImportJobKeepsRulesVersionAndLineage: an import run stamps
// the rules version it ran under and the file's lineage (name, size,
// SHA-256, uploader, time); the recorded job carries both, and its control
// totals agree with the rows it accepted and rejected.
func TestStage47133ImportJobKeepsRulesVersionAndLineage(t *testing.T) {
	db.InitDB(testConnStr())
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	var jobID string
	cleanup := func() {
		db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'POSCostingGap' AND id LIKE 'N20D-LIN-%'")
		if jobID != "" {
			db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'ImportJob' AND id = $1", jobID)
		}
	}
	cleanup()
	defer cleanup()

	file := "id,cart_number,sku,qty,sale_value,status\n" +
		"N20D-LIN-1,C1,SKU1,2,100.50,Open\n" +
		"N20D-LIN-2,C1,SKU2,3,200.25,Open\n" +
		"N20D-LIN-3,C1,SKU3,5,10,Bogus\n" // status not allowed: rejected
	sum := sha256.Sum256([]byte(file))

	res, err := BulkImportCSV("default", "POSCostingGap", NamedUpload(strings.NewReader(file), "C:\\Users\\x\\gaps (oct).csv"), "system", "", false)
	if err != nil {
		t.Fatalf("BulkImportCSV: %v", err)
	}
	if res.RulesVersion != ImportRulesVersion || ImportRulesVersion == "" {
		t.Fatalf("rules version %q on the result, want %q", res.RulesVersion, ImportRulesVersion)
	}
	src := res.Source
	if src == nil {
		t.Fatalf("no source lineage on the result")
	}
	if src.FileName != "gaps (oct).csv" || src.SizeBytes != int64(len(file)) || src.SHA256 != hex.EncodeToString(sum[:]) || src.UploadedBy != "system" || src.UploadedAt == "" {
		t.Fatalf("lineage %+v: want name gaps (oct).csv, %d bytes, sha256 %s, by system, with a time", *src, len(file), hex.EncodeToString(sum[:]))
	}

	jobID, err = RecordImportJob("default", "POSCostingGap", res, "system")
	if err != nil {
		t.Fatalf("RecordImportJob: %v", err)
	}
	var raw string
	if err := db.DB.QueryRow("SELECT data FROM "+schema+".documents WHERE doctype = 'ImportJob' AND id = $1", jobID).Scan(&raw); err != nil {
		t.Fatalf("load job: %v", err)
	}
	var job struct {
		RulesVersion string              `json:"rules_version"`
		Source       ImportSource        `json:"source"`
		Totals       ImportControlTotals `json:"control_totals"`
		TotalRows    int                 `json:"total_rows"`
		SuccessRows  int                 `json:"success_rows"`
		FailedRows   int                 `json:"failed_rows"`
	}
	if err := json.Unmarshal([]byte(raw), &job); err != nil {
		t.Fatal(err)
	}
	if job.RulesVersion != ImportRulesVersion || job.Source.SHA256 != src.SHA256 || job.Source.FileName != src.FileName || job.Source.SizeBytes != src.SizeBytes || job.Source.UploadedBy != "system" {
		t.Fatalf("the job did not keep the version and lineage: %s", raw)
	}
	tot := job.Totals
	if tot.RowsRead != 3 || tot.RowsAccepted != 2 || tot.RowsRejected != 1 || !tot.Balanced {
		t.Fatalf("control totals %+v, want 3 read, 2 accepted, 1 rejected, balanced", tot)
	}
	if tot.RowsRead != job.TotalRows || tot.RowsAccepted != job.SuccessRows || tot.RowsRejected != job.FailedRows || tot.RowsRead != tot.RowsAccepted+tot.RowsRejected {
		t.Fatalf("totals %+v do not agree with the job's counts %d/%d/%d", tot, job.TotalRows, job.SuccessRows, job.FailedRows)
	}

	// The Import Job screen reads them from the summary.
	summary, err := ImportJobSummary("default", jobID)
	if err != nil {
		t.Fatalf("ImportJobSummary: %v", err)
	}
	if summary["rules_version"] != ImportRulesVersion || summary["control_totals"] == nil || summary["source"] == nil {
		t.Fatalf("summary lacks the version, totals or source: %v", summary)
	}
	if s, _ := summary["source"].(map[string]interface{}); s["sha256"] != src.SHA256 {
		t.Fatalf("summary source %v, want sha256 %s", summary["source"], src.SHA256)
	}
}
