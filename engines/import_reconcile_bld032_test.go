package engines

// BLD-032 - a representative customer import reconciles, survives a retry
// and is corrected by a non-developer through the failed-rows file. 2,000
// rows with 100 bad ones of three kinds (an invalid status, a missing name,
// a code repeated inside the file): counts add up exactly, the run stays
// inside its time budget, a full re-run - what a user does after an
// interrupted upload - creates nothing twice and keeps who created each row,
// and fixing only the failed rows in the downloaded file and resuming the
// same job brings every row in exactly once.

import (
	"bytes"
	"context"
	"custom_erp/db"
	"encoding/csv"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestBLD032RepresentativeImportReconciles(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	count := func() (n int) {
		db.DB.QueryRow("SELECT COUNT(*) FROM " + schema + ".documents WHERE doctype = 'Brand' AND data->>'code' LIKE 'B32-%' AND deleted_at IS NULL").Scan(&n)
		return
	}

	const total = 2000
	var file bytes.Buffer
	file.WriteString("code,name,status\n")
	bad := 0
	good := map[string]bool{} // codes whose row imports
	for i := 1; i <= total; i++ {
		code, name, status := fmt.Sprintf("B32-%04d", i), fmt.Sprintf("Brand %d", i), "Active"
		ok := true
		switch {
		case i%60 == 0 && bad < 100: // invalid status
			status, ok = "Bogus", false
		case i%61 == 0 && bad < 100: // missing name
			name, ok = "", false
		case i%62 == 0 && bad < 100: // repeats an earlier row's code
			code = fmt.Sprintf("B32-%04d", i-1)
			// Only a repeat of a row that imported is a duplicate; a repeat
			// of a failed row is simply that code's first good row.
			ok = !good[code]
		}
		if ok {
			good[code] = true
		} else {
			bad++
		}
		fmt.Fprintf(&file, "%s,%s,%s\n", code, name, status)
	}

	began := time.Now()
	res, err := BulkImportCSV(tenantID, "Brand", bytes.NewReader(file.Bytes()), "importer", RoleSuperAdmin, false)
	took := time.Since(began)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("BLD-032 %d rows: %d imported, %d failed in %v", res.TotalRows, res.SuccessRows, res.FailedRows, took)
	if res.TotalRows != total || res.SuccessRows+res.FailedRows != total {
		t.Fatalf("counts do not add up: total %d, ok %d, failed %d", res.TotalRows, res.SuccessRows, res.FailedRows)
	}
	if res.FailedRows != bad {
		t.Errorf("failed %d rows, the file has %d bad ones: %v", res.FailedRows, bad, res.Errors[:min(5, len(res.Errors))])
	}
	if got := count(); got != res.SuccessRows {
		t.Errorf("database holds %d rows, the import reported %d imported", got, res.SuccessRows)
	}
	if took > 90*time.Second {
		t.Errorf("a %d-row import took %v - over the 90 s budget", total, took)
	}
	jobID, err := RecordImportJob(tenantID, "Brand", res, "importer")
	if err != nil {
		t.Fatal(err)
	}

	// A retry after an interrupted upload: the whole file again.
	again, err := BulkImportCSV(tenantID, "Brand", bytes.NewReader(file.Bytes()), "retrier", RoleSuperAdmin, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := count(); got != res.SuccessRows {
		t.Errorf("re-running the file changed the row count from %d to %d - rows were duplicated", res.SuccessRows, got)
	}
	if len(again.CreatedIDs) != 0 {
		t.Errorf("re-running the file created %d new rows; every good row already existed", len(again.CreatedIDs))
	}
	var creator string
	db.DB.QueryRow("SELECT created_by FROM " + schema + ".documents WHERE doctype = 'Brand' AND data->>'code' = 'B32-0001'").Scan(&creator)
	if creator != "importer" {
		t.Errorf("a retry must keep who created the row: created_by = %q", creator)
	}

	// Correction: download the failed rows, fix only what the error names,
	// resume the same job.
	failedCSV, err := ImportJobFailedRowsCSV(tenantID, jobID)
	if err != nil {
		t.Fatal(err)
	}
	records, err := csv.NewReader(bytes.NewReader(failedCSV)).ReadAll()
	if err != nil || len(records) != bad+1 {
		t.Fatalf("failed-rows file has %d lines (want %d + header): %v", len(records), bad, err)
	}
	header := records[0]
	col := map[string]int{}
	for i, h := range header {
		col[h] = i
	}
	var fixed bytes.Buffer
	w := csv.NewWriter(&fixed)
	w.Write(header)
	for _, r := range records[1:] {
		if r[col["status"]] == "Bogus" {
			r[col["status"]] = "Active"
		}
		if r[col["name"]] == "" {
			r[col["name"]] = "Named on correction"
		}
		if strings.Contains(strings.ToLower(r[col["error"]]), "duplicate") || strings.Contains(strings.ToLower(r[col["error"]]), "already") {
			r[col["code"]] = r[col["code"]] + "-R" + r[col["original_row"]]
		}
		w.Write(r)
		// A corrected row whose code another row already imported updates
		// that record (the importer's upsert rule), so the final count is
		// the number of distinct codes, not of rows.
		good[r[col["code"]]] = true
	}
	w.Flush()
	r2, child, err := ResumeImportJob(context.Background(), tenantID, jobID, bytes.NewReader(fixed.Bytes()), "importer", RoleSuperAdmin, false)
	if err != nil {
		t.Fatal(err)
	}
	if r2.FailedRows != 0 || child == "" {
		t.Fatalf("resume after correction: %d failed (%v), child %q", r2.FailedRows, r2.Errors[:min(5, len(r2.Errors))], child)
	}
	if got, want := count(), len(good); got != want {
		t.Errorf("after correction the database holds %d rows, want %d - every code exactly once", got, want)
	}
	sum, _ := ImportJobSummary(tenantID, jobID)
	if sum["open_failed_rows"] != 0 || sum["status"] != "Completed" {
		t.Errorf("job after correction: %v", sum)
	}
}
