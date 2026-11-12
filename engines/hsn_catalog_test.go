package engines

import "testing"

// Stage 128.5.1: a Setup Workbook row that changes only an HSN code's
// description (minn, 2026-10-27, on the Jewelry library's 711719) was refused
// "HSN/SAC code is required" - the check ran before the stored record is
// merged in. An update without the number now passes; a create, or an update
// that sends a blank or malformed number, is still refused.
func TestPrepareHSNCodePartialUpdate(t *testing.T) {
	if err := PrepareHSNCode(false, map[string]interface{}{"description": "Imitation jewellery"}); err != nil {
		t.Fatalf("update without hsn: %v", err)
	}
	if err := PrepareHSNCode(true, map[string]interface{}{"description": "Imitation jewellery"}); err == nil {
		t.Fatal("create without hsn was accepted")
	}
	if err := PrepareHSNCode(false, map[string]interface{}{"hsn": ""}); err == nil {
		t.Fatal("update clearing hsn was accepted")
	}
	if err := PrepareHSNCode(false, map[string]interface{}{"hsn": "71"}); err == nil {
		t.Fatal("update with a 2-digit hsn was accepted")
	}
	p := map[string]interface{}{"hsn": "7117 19"}
	if err := PrepareHSNCode(true, p); err != nil || p["hsn"] != "711719" || p["code"] != "HSN-711719" {
		t.Fatalf("create: err=%v payload=%v", err, p)
	}
}
