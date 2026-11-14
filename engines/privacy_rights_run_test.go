package engines

import (
	"custom_erp/db"
	"testing"
)

// 49.6.8: an export run hands back the subject's copy (the screen saves it as
// JSON); before, the run gathered it and threw it away. An erasure run hands
// back nothing, and a completed request cannot be run twice.
func TestStage4968ExportRunHandsBackTheCopy(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	custID := "CUST-DSR-4968"
	seedTestCustomer(t, schema, custID, map[string]interface{}{"city": "Pune"})
	defer db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", custID)

	run := func(typ string) (map[string]interface{}, string) {
		t.Helper()
		id, err := RequestDataSubjectAction(tenantID, "Customer", custID, typ, "4968", "requester_user")
		if err != nil {
			t.Fatalf("request %s: %v", typ, err)
		}
		t.Cleanup(func() { db.DB.Exec("DELETE FROM "+schema+".data_subject_requests WHERE id = $1", id) })
		if err := DecideDataSubjectRequest(tenantID, id, "checker_user", true, "ok"); err != nil {
			t.Fatalf("decide %s: %v", typ, err)
		}
		data, err := RunDataSubjectRequest(tenantID, id, "checker_user")
		if err != nil {
			t.Fatalf("run %s: %v", typ, err)
		}
		return data, id
	}

	data, exportID := run(PrivacyRequestExport)
	if data == nil || data["name"] != "Original Name" || data["city"] != "Pune" {
		t.Fatalf("export run handed back %v, want the customer's record", data)
	}
	if _, err := RunDataSubjectRequest(tenantID, exportID, "checker_user"); err == nil {
		t.Fatal("a completed export ran a second time")
	}
	if data, _ := run(PrivacyRequestErasure); data != nil {
		t.Fatalf("erasure run handed back %v, want nothing", data)
	}
}
