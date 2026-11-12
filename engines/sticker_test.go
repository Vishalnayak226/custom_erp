package engines

import (
	"custom_erp/db"
	"encoding/json"
	"strings"
	"testing"
)

// TestStage134StickerSourceDocs (134.1b): the sticker screen's Document list
// finds a GRN by part of its number, newest first, without cancelled ones,
// and within the session's store - a store's staff never see another store's
// receipts, a full-access role sees all.
func TestStage134StickerSourceDocs(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	for _, g := range []struct{ id, loc, status string }{
		{"GRN/HO/26-27/00050", "HO", "Posted"},
		{"GRN/BKC/26-27/00051", "BKC", "Posted"},
		{"GRN/HO/26-27/00052", "HO", "Cancelled"},
		{"GRN/HO/26-27/00053", "HO", "Draft"},
	} {
		raw, _ := json.Marshal(map[string]interface{}{"location": g.loc, "posting_date": "2026-10-23"})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'GRN', $2, $3, 'system')", g.id, raw, g.status); err != nil {
			t.Fatal(err)
		}
	}
	ids := func(docs []StickerSourceDoc) string {
		out := []string{}
		for _, d := range docs {
			out = append(out, d.ID)
		}
		return strings.Join(out, ",")
	}
	store := SessionScope{Role: "Store Manager", UserID: "sm1", LocationCode: "HO"}
	docs, err := SearchStickerSourceDocs(tenantID, "GRN", "00050", 10, store)
	if err != nil || ids(docs) != "GRN/HO/26-27/00050" || docs[0].Location != "HO" || docs[0].Date != "2026-10-23" {
		t.Fatalf("a typed part of the number finds the store's receipt: %+v %v", docs, err)
	}
	if docs, _ = SearchStickerSourceDocs(tenantID, "GRN", "", 10, store); strings.Contains(ids(docs), "BKC") || strings.Contains(ids(docs), "00052") || len(docs) != 2 {
		t.Fatalf("a store sees its own receipts, none cancelled: %s", ids(docs))
	}
	if docs, _ = SearchStickerSourceDocs(tenantID, "GRN", "00051", 10, store); len(docs) != 0 {
		t.Fatalf("another store's receipt is not listed: %s", ids(docs))
	}
	if docs, _ = SearchStickerSourceDocs(tenantID, "GRN", "", 10, SessionScope{Role: "Admin", UserID: "admin"}); len(docs) != 3 {
		t.Fatalf("a full-access role sees every store's receipts: %s", ids(docs))
	}
}

// TestStage134ReprintReasonCodeWithNote (134.1c): the sticker screen sends
// "CODE: note" when a Reprint code is picked beside a note; it is kept as
// "CODE: label - note" and passes when the client requires codes, where a
// note alone is refused.
func TestStage134ReprintReasonCodeWithNote(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	raw, _ := json.Marshal(map[string]interface{}{"code": "T134-TORN", "description": "Label torn", "category": ReasonReprint})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ('T134-TORN', 'ReasonCode', $1, 'Active', 'system')", raw); err != nil {
		t.Fatal(err)
	}
	if err := SetSetting(tenantID, SettingReasonCodesRequired, "true", "system"); err != nil {
		t.Fatal(err)
	}
	for in, want := range map[string]string{
		"T134-TORN":              "T134-TORN: Label torn",
		"T134-TORN: second roll": "T134-TORN: Label torn - second roll",
		"T134-TORN: Label torn":  "T134-TORN: Label torn",
		"":                       "",
	} {
		if got, err := normalizeReprintReason(tenantID, in); err != nil || got != want {
			t.Fatalf("normalizeReprintReason(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"torn", "MRP changed: 100 to 120"} {
		if _, err := normalizeReprintReason(tenantID, in); err == nil {
			t.Fatalf("a note alone (%q) is refused when codes are required", in)
		}
	}
}
