package engines

import (
	"custom_erp/db"
	"fmt"
	"strings"
	"testing"
	"time"
)

// 49.4.4a: Item import preview ran at ~5.5 ms/row on tenant_default and
// ~17 ms/row on any other tenant (every lookup re-resolved the tenant's
// schema), because ValidateDocument and importBatch re-read the doctype's
// fields, a setting per field, field permissions, the Master flag, each Link
// target and an EXISTS check for every row. A 20,000-row file - the
// platform.max_import_rows default - could never finish inside the 55 s
// request deadline. These tests hold the batched path to the same answers
// and to a time well inside that deadline.

// itemImportFixture builds an n-row Item CSV with the doctype's real
// columns, so a run exercises the same mandatory/Select/Number checks a
// user's file does. family, when set, adds the Link column.
func itemImportFixture(prefix string, n int, family func(i int) string) string {
	var b strings.Builder
	b.WriteString("code,name,barcode,hsn_code,category,gst_rate,tax_treatment,mrp,sale_price,standard_cost,tracking_mode,item_type")
	if family != nil {
		b.WriteString(",family")
	}
	b.WriteString("\n")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "%s-%05d,Cotton Shirt Slim Fit %d,8901234%06d,62052000,Apparel,12,Taxable,1499,1199,640,None,Stock", prefix, i, i, i)
		if family != nil {
			b.WriteString("," + family(i))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func importPerfTenant(t *testing.T) (string, string) {
	t.Helper()
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	t.Cleanup(func() { dropLifecycleTenant(tenantID, schema) })
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	return tenantID, schema
}

// TestItemImportPreviewFitsTheDeadlineAtTheRowCap is the speed regression:
// a 20,000-row preview on a non-default tenant must finish in under half the
// 55 s request deadline. Before 49.4.4a it took about 330 s.
func TestItemImportPreviewFitsTheDeadlineAtTheRowCap(t *testing.T) {
	tenantID, _ := importPerfTenant(t)
	rows := GetSettingInt(tenantID, "platform.max_import_rows")
	if rows != 20000 {
		t.Fatalf("this test is sized for the 20,000-row default; platform.max_import_rows is %d", rows)
	}
	start := time.Now()
	res, err := BulkImportCSV(tenantID, "Item", strings.NewReader(itemImportFixture("CAP", rows, nil)), "perf", RoleSuperAdmin, true)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	elapsed := time.Since(start)
	if res.SuccessRows != rows || res.FailedRows != 0 {
		t.Fatalf("want %d valid rows, got ok=%d failed=%d first=%v", rows, res.SuccessRows, res.FailedRows, res.Errors)
	}
	if budget := 27500 * time.Millisecond; elapsed > budget {
		t.Fatalf("a %d-row Item preview took %v; it must stay under %v (half the 55 s request deadline)", rows, elapsed, budget)
	}
	t.Logf("%d-row Item preview in %v (%.2f ms/row)", rows, elapsed, float64(elapsed.Microseconds())/1000/float64(rows))
}

// TestBatchedImportKeepsPerRowAnswers checks that batching changed no
// answer: create/update classification across batch boundaries, Link
// checks found and missing, restricted fields, and an id held by another
// record type.
func TestBatchedImportKeepsPerRowAnswers(t *testing.T) {
	tenantID, schema := importPerfTenant(t)
	const n = 1203 // crosses two 500-row batch boundaries
	exec := func(q string, args ...interface{}) {
		t.Helper()
		if _, err := db.DB.Exec(fmt.Sprintf(q, schema), args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ('FAM-OK', 'ProductFamily', '{"code":"FAM-OK","name":"Shirts"}', 'Active', 'test')`)
	exec(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ('FAM-GONE', 'ProductFamily', '{"code":"FAM-GONE","name":"Old"}', 'Cancelled', 'test')`)

	// Every 100th row links a missing family, every 150th a cancelled one.
	family := func(i int) string {
		switch {
		case i%100 == 7:
			return "FAM-NOPE"
		case i%150 == 9:
			return "FAM-GONE"
		default:
			return "FAM-OK"
		}
	}
	badRows := map[int]bool{}
	for i := 0; i < n; i++ {
		if f := family(i); f != "FAM-OK" {
			badRows[i+2] = true // +2: header row, then 1-based
		}
	}
	file := itemImportFixture("ANS", n, family)

	first, err := BulkImportCSV(tenantID, "Item", strings.NewReader(file), "test", RoleSuperAdmin, false)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if first.FailedRows != len(badRows) || len(first.CreatedIDs) != n-len(badRows) || len(first.UpdatedIDs) != 0 {
		t.Fatalf("first import: want %d created and %d failed, got created=%d updated=%d failed=%d",
			n-len(badRows), len(badRows), len(first.CreatedIDs), len(first.UpdatedIDs), first.FailedRows)
	}
	for _, e := range first.Errors {
		if !badRows[e.RowNumber] || !strings.Contains(e.Message, "does not exist") {
			t.Errorf("unexpected failure on row %d: %s", e.RowNumber, e.Message)
		}
	}
	if first.CreatedIDs[0] != "ANS-00000" || first.CreatedIDs[len(first.CreatedIDs)-1] != fmt.Sprintf("ANS-%05d", n-1) {
		t.Errorf("created ids must stay in file order, got %s ... %s", first.CreatedIDs[0], first.CreatedIDs[len(first.CreatedIDs)-1])
	}
	var stored int
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT count(*) FROM %s.documents WHERE doctype = 'Item' AND id LIKE 'ANS-%%'`, schema)).Scan(&stored); err != nil || stored != n-len(badRows) {
		t.Fatalf("want %d stored Items, got %d (%v)", n-len(badRows), stored, err)
	}

	// The same file again, previewed: every valid row is now an update and
	// nothing is written.
	again, err := BulkImportCSV(tenantID, "Item", strings.NewReader(strings.ReplaceAll(file, "Cotton", "Linen")), "test", RoleSuperAdmin, true)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if len(again.UpdatedIDs) != n-len(badRows) || len(again.CreatedIDs) != 0 || again.FailedRows != len(badRows) {
		t.Fatalf("re-preview: want %d updates, got created=%d updated=%d failed=%d", n-len(badRows), len(again.CreatedIDs), len(again.UpdatedIDs), again.FailedRows)
	}
	var name string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT data->>'name' FROM %s.documents WHERE id = 'ANS-00000'`, schema)).Scan(&name); err != nil || !strings.HasPrefix(name, "Cotton") {
		t.Fatalf("a preview must not write: name=%q (%v)", name, err)
	}

	t.Run("AnIdHeldByAnotherRecordTypeIsRefused", func(t *testing.T) {
		exec(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ('XT-00001', 'Brand', '{"name":"Keep Me"}', 'Active', 'test')`)
		res, err := BulkImportCSV(tenantID, "Item", strings.NewReader(itemImportFixture("XT", 3, nil)), "test", RoleSuperAdmin, false)
		if err != nil {
			t.Fatalf("import: %v", err)
		}
		if res.FailedRows != 1 || len(res.CreatedIDs) != 2 || len(res.Errors) != 1 || res.Errors[0].RowNumber != 3 ||
			!strings.Contains(res.Errors[0].Message, "different record type") {
			t.Fatalf("want row 3 refused and the other two created, got %+v", res)
		}
		var doctype, brand string
		if err := db.DB.QueryRow(fmt.Sprintf(`SELECT doctype, data->>'name' FROM %s.documents WHERE id = 'XT-00001'`, schema)).Scan(&doctype, &brand); err != nil || doctype != "Brand" || brand != "Keep Me" {
			t.Fatalf("the Brand record was changed: %s %q (%v)", doctype, brand, err)
		}
	})

	t.Run("RestrictedFieldsStillRefuseEveryRow", func(t *testing.T) {
		exec(`INSERT INTO %s.field_permissions (role, doctype_name, fieldname, allow_read, allow_write) VALUES ($1, 'Item', 'standard_cost', true, false)`, RoleCashier)
		res, err := BulkImportCSV(tenantID, "Item", strings.NewReader(itemImportFixture("RF", 3, nil)), "test", RoleCashier, true)
		if err != nil {
			t.Fatalf("preview: %v", err)
		}
		// Which restricted field gets named depends on map order (Cashier's
		// seeded restrictions cover more than standard_cost).
		if res.FailedRows != 3 || res.SuccessRows != 0 || !strings.Contains(res.Errors[0].Message, "is not writable for role "+RoleCashier) {
			t.Fatalf("want all 3 rows refused as not writable, got %+v", res)
		}
	})
}
