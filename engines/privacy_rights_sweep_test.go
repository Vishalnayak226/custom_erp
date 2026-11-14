package engines

import (
	"custom_erp/db"
	"strings"
	"testing"
)

func runRequest(t *testing.T, tenantID, custID, typ string, corr map[string]interface{}) (string, error) {
	t.Helper()
	id, err := RequestDataSubjectCorrection(tenantID, "Customer", custID, typ, "test", "requester_user", corr)
	if err != nil {
		return "", err
	}
	if err := DecideDataSubjectRequest(tenantID, id, "checker_user", true, "ok"); err != nil {
		t.Fatalf("decide: %v", err)
	}
	return id, ExecuteDataSubjectRequest(tenantID, id, "checker_user")
}

// 49.6.8: erasure also removes the subject's files and saved views.
func TestErasureRemovesAttachmentsAndSavedViews(t *testing.T) {
	db.InitDB(testConnStr())
	SetMediaStoreDirForTest(t.TempDir())
	tenantID := "default"
	schema, _ := db.GetTenantSchema(tenantID)
	custID := "CUST-DSR-SWEEP"
	seedTestCustomer(t, schema, custID, nil)
	db.DB.Exec("DELETE FROM " + schema + ".document_attachments WHERE document_id = '" + custID + "'")
	db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id IN ('SC-SWEEP-1','SC-SWEEP-2','SC-SWEEP-3')")
	defer db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id IN ('" + custID + "','SC-SWEEP-1','SC-SWEEP-2','SC-SWEEP-3')")
	defer db.DB.Exec("DELETE FROM " + schema + ".document_attachments WHERE document_id = '" + custID + "'")

	key, _ := newAttachmentStorageKey(schema, ".pdf")
	if err := serverMedia.Write(key, []byte("%PDF-1.4 id"), MediaWrite{Exclusive: true, Private: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(`INSERT INTO `+schema+`.document_attachments (doctype, document_id, file_name, content_type, size_bytes, sha256, storage_key, uploaded_by)
		VALUES ('Customer', $1, 'passport-9999999999.pdf', 'application/pdf', 11, 'x', $2, 'u')`, custID, key); err != nil {
		t.Fatal(err)
	}
	for id, body := range map[string]string{"SC-SWEEP-1": `{"q":"9999999999"}`, "SC-SWEEP-2": `{"q":"shoes"}`, "SC-SWEEP-3": `{"q":"CUST-DSR-SWEEP2"}`} {
		if _, err := db.DB.Exec(`INSERT INTO `+schema+`.documents (id, doctype, data, status, created_by) VALUES ($1,'UserShortcut',$2,'Active','system')`, id, body); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := runRequest(t, tenantID, custID, PrivacyRequestErasure, nil); err != nil {
		t.Fatalf("erase: %v", err)
	}
	if serverMedia.Exists(key) {
		t.Error("file should be gone from the media store")
	}
	var name string
	var deleted bool
	db.DB.QueryRow(`SELECT file_name, deleted_at IS NOT NULL FROM `+schema+`.document_attachments WHERE document_id=$1`, custID).Scan(&name, &deleted)
	if strings.Contains(name, "9999999999") || !deleted {
		t.Errorf("attachment row not erased: %q deleted=%v", name, deleted)
	}
	var n int
	db.DB.QueryRow(`SELECT COUNT(*) FROM ` + schema + `.documents WHERE id IN ('SC-SWEEP-1')`).Scan(&n)
	if n != 0 {
		t.Error("saved view carrying the phone should be deleted")
	}
	db.DB.QueryRow(`SELECT COUNT(*) FROM ` + schema + `.documents WHERE id = 'SC-SWEEP-2'`).Scan(&n)
	if n != 1 {
		t.Error("an unrelated saved view must stay")
	}
	db.DB.QueryRow(`SELECT COUNT(*) FROM ` + schema + `.documents WHERE id = 'SC-SWEEP-3'`).Scan(&n)
	if n != 1 {
		t.Error("a view naming a different id that merely starts with this one must stay")
	}
}

// 49.6.8: an approved correction writes its values to the Customer, with an audit row.
func TestCorrectionAppliesToCustomer(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, _ := db.GetTenantSchema(tenantID)
	custID := "CUST-DSR-CORR"
	seedTestCustomer(t, schema, custID, nil)
	defer db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id = '" + custID + "'")

	// A field outside the personal list, an empty set, and a non-Customer subject are refused at request time.
	if _, err := RequestDataSubjectCorrection(tenantID, "Customer", custID, PrivacyRequestCorrection, "x", "r", map[string]interface{}{"credit_limit": 1}); err == nil {
		t.Error("credit_limit is not a personal field; expected refusal")
	}
	if _, err := RequestDataSubjectCorrection(tenantID, "Customer", custID, PrivacyRequestCorrection, "x", "r", nil); err == nil {
		t.Error("empty correction should be refused")
	}
	if _, err := RequestDataSubjectCorrection(tenantID, "Employee", "E1", PrivacyRequestCorrection, "x", "r", map[string]interface{}{"name": "A"}); err == nil {
		t.Error("only Customer corrections are built")
	}
	if _, err := RequestDataSubjectCorrection(tenantID, "Customer", custID, PrivacyRequestErasure, "x", "r", map[string]interface{}{"name": "A"}); err == nil {
		t.Error("values on a non-correction request should be refused")
	}
	// A corrected value is held to the Customer's own field rules, like any save.
	if _, err := RequestDataSubjectCorrection(tenantID, "Customer", custID, PrivacyRequestCorrection, "x", "r", map[string]interface{}{"email": "not-an-email"}); err == nil {
		t.Error("a badly formed email should be refused by the Customer's field rules")
	}

	id, err := runRequest(t, tenantID, custID, PrivacyRequestCorrection, map[string]interface{}{"name": "Corrected Name", "city": "Pune"})
	if err != nil {
		t.Fatalf("correction: %v", err)
	}
	defer db.DB.Exec("DELETE FROM "+schema+".data_subject_requests WHERE subject_id = $1", custID)
	data, _ := ExportCustomerData(tenantID, custID)
	if data["name"] != "Corrected Name" || data["city"] != "Pune" || data["phone"] != "9999999999" {
		t.Errorf("unexpected record after correction: %v", data)
	}
	var n int
	db.DB.QueryRow(`SELECT COUNT(*) FROM `+schema+`.audit_logs WHERE action = 'data_subject_correction_applied' AND details LIKE '%' || $1 || '%'`, id).Scan(&n)
	if n == 0 {
		t.Error("expected a correction audit row")
	}
}
