package engines

import (
	"custom_erp/db"
	"testing"
)

// TestStage74_1_6GRNEvidence: a goods receipt takes the vendor's invoice copy
// while it is worked in steps (Pending) and refuses a change once posted.
func TestStage74_1_6GRNEvidence(t *testing.T) {
	db.InitDB(testConnStr())
	old := mediaStoreDir
	SetMediaStoreDirForTest(t.TempDir())
	defer func() { mediaStoreDir = old }()
	schema, _ := db.GetTenantSchema("default")
	const grnID = "TEST7416-GRN"
	cleanup := func() {
		db.DB.Exec("DELETE FROM "+schema+".document_attachments WHERE document_id = $1", grnID)
		db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", grnID)
	}
	cleanup()
	defer cleanup()
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'GRN', '{\"flow\":\"steps\"}', 'Pending', 'system')", grnID); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if !AttachmentsEnabled("GRN") || !AttachmentStatusAllowsChange("GRN", "Pending") || AttachmentStatusAllowsChange("GRN", "Approved") {
		t.Fatalf("a GRN takes files while Pending only")
	}
	png := tinyTestPNG(t, 4, 4)
	if _, err := AddAttachment("default", "GRN", grnID, "invoice.png", png, "system"); err != nil {
		t.Fatalf("attach to a Pending GRN: %v", err)
	}
	db.DB.Exec("UPDATE "+schema+".documents SET status = 'Approved' WHERE id = $1", grnID)
	if _, err := AddAttachment("default", "GRN", grnID, "late.png", tinyTestPNG(t, 5, 5), "system"); attachmentCode(err) != "EXP-0545" {
		t.Fatalf("a posted GRN must refuse a new file, got %v", err)
	}
}
