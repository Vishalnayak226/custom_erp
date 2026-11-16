package engines

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"custom_erp/db"
)

// Item 201.4: changing an item's name on past documents.

const (
	pnOld = "Necklace Antique"
	pnNew = "Antique Necklace"
)

func pastRenameTenant(t *testing.T) (tenantID, schema string) {
	t.Helper()
	tenantID, schema = healTestTenant(t)
	for _, d := range []string{"DeliveryChallan", "PurchaseOrder"} {
		if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.doctype_meta (name, module, module_key, document_type)
			VALUES ($1, 'Test', 'test', 'Transaction') ON CONFLICT (name) DO UPDATE SET document_type = 'Transaction'`, schema), d); err != nil {
			t.Fatalf("doctype %s: %v", d, err)
		}
	}
	if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.doctype_meta (name, module, module_key, document_type)
		VALUES ('Item', 'Test', 'test', 'Master') ON CONFLICT (name) DO NOTHING`, schema)); err != nil {
		t.Fatalf("doctype Item: %v", err)
	}
	return tenantID, schema
}

func pnPrintPayload() PrintDocument {
	return PrintDocument{Transaction: "po", Doctype: "PurchaseOrder", DocumentID: "PR-PO-1", Number: "PR-PO-1", Status: "Approved",
		Lines: []map[string]interface{}{
			{"sr": 1, "sku": "PR-S1", "item_name": pnOld, "description": pnOld + " · Silver / 7", "qty": 2,
				"item": map[string]interface{}{"name": pnOld, "code": "PR-S1"}},
			{"sr": 2, "sku": "", "item_name": pnOld, "description": pnOld}, // a charge line: no item, kept
		}}
}

func pnData(t *testing.T, schema, id string) map[string]interface{} {
	t.Helper()
	var raw string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT data::text FROM %s.documents WHERE id = $1`, schema), id).Scan(&raw); err != nil {
		t.Fatalf("read %s: %v", id, err)
	}
	var d map[string]interface{}
	_ = json.Unmarshal([]byte(raw), &d)
	return d
}

func pnLines(v interface{}) []map[string]interface{} {
	var list []interface{}
	switch x := v.(type) {
	case []interface{}:
		list = x
	case string:
		_ = json.Unmarshal([]byte(x), &list)
	}
	out := []map[string]interface{}{}
	for _, el := range list {
		if m, ok := el.(map[string]interface{}); ok {
			out = append(out, m)
		}
	}
	return out
}

func pnSeed(t *testing.T, schema string) {
	t.Helper()
	healSeed(t, schema, "DeliveryChallan", "PR-DC-1", map[string]interface{}{"note": pnOld + " for the show",
		"items": []interface{}{map[string]interface{}{"sku": "PR-S1", "item_name": pnOld, "qty": 1}, map[string]interface{}{"sku": "PR-S2", "item_name": "Ring Gold"}}})
	inner, _ := json.Marshal([]interface{}{map[string]interface{}{"sku": "PR-S1", "item_name": strings.ToLower(pnOld), "qty": 3}})
	healSeed(t, schema, "DeliveryChallan", "PR-DC-2", map[string]interface{}{"items": string(inner)})
	healSeed(t, schema, "PurchaseOrder", "PR-PO-1", map[string]interface{}{"items": `[{"qty":2,"sku":"PR-S1"}]`})
	healSeed(t, schema, "DeliveryChallan", "PR-DC-FILED", map[string]interface{}{"items": []interface{}{map[string]interface{}{"sku": "PR-S1", "item_name": pnOld}}})
	healSeed(t, schema, "DeliveryChallan", "PR-DC-OLD", map[string]interface{}{"items": []interface{}{map[string]interface{}{"sku": "PR-S1", "item_name": pnOld}}})
	healSeed(t, schema, "Item", "PR-S1", map[string]interface{}{"name": pnOld, "item_name": pnOld})
	if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET created_at = '2026-01-10 12:00' WHERE id = 'PR-DC-OLD'`, schema)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.ewb_documents (doctype, doc_id, doc_number, status, ewb_no) VALUES ('DeliveryChallan', 'PR-DC-FILED', 'PR-DC-FILED', 'Generated', '331000000001')`, schema)); err != nil {
		t.Fatalf("e-way bill: %v", err)
	}
	snap := pnPrintPayload()
	raw, _ := json.Marshal(snap)
	if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.print_snapshots (txn, doc_id, revision, doctype, payload, checksum, frozen_by)
		VALUES ('po', 'PR-PO-1', 1, 'PurchaseOrder', $1, $2, 'tester')`, schema), string(raw), printChecksum(snap)); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
}

func TestPastRenameChangesCopiedNamesAndFrozenPrints(t *testing.T) {
	tenant, schema := pastRenameTenant(t)
	pnSeed(t, schema)
	req := PastRenameRequest{Old: " " + pnOld + " ", New: pnNew, From: "2026-06-01"}

	pv, err := PastRenamePreviewFor(tenant, req)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if pv.Documents != 3 || pv.Lines != 2 || pv.Prints != 1 || pv.Filed != 1 {
		t.Fatalf("preview: want 3 documents, 2 lines, 1 print, 1 filed; got %+v", pv)
	}
	for _, ty := range pv.Types {
		if ty.Doctype == "Item" {
			t.Fatal("a master is never in the change")
		}
		if ty.Doctype == "DeliveryChallan" && (ty.Documents != 2 || ty.Filed != 1 || ty.Label != "Delivery Challan") {
			t.Fatalf("delivery challans: %+v", ty)
		}
	}
	for _, d := range pv.Docs {
		if d.ID == "PR-DC-OLD" {
			t.Fatal("a document created before From is outside the change")
		}
	}

	if _, err := PastRenameApply(tenant, req, "boss"); err == nil || !strings.Contains(err.Error(), "YES") {
		t.Fatalf("Apply without YES should be refused, got %v", err)
	}
	if d := pnData(t, schema, "PR-DC-1"); pnLines(d["items"])[0]["item_name"] != pnOld {
		t.Fatal("a refused Apply changed a document")
	}

	req.Confirm = "YES"
	res, err := PastRenameApply(tenant, req, "boss")
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if res.Documents != 3 || res.Lines != 2 || res.Prints != 1 || res.Filed != 1 {
		t.Fatalf("apply: want 3/2/1/1, got %+v", res)
	}

	dc := pnData(t, schema, "PR-DC-1")
	lines := pnLines(dc["items"])
	if lines[0]["item_name"] != pnNew || lines[1]["item_name"] != "Ring Gold" || dc["note"] != pnOld+" for the show" {
		t.Fatalf("PR-DC-1 after: %v", dc)
	}
	dc2 := pnData(t, schema, "PR-DC-2")
	if _, still := dc2["items"].(string); !still || pnLines(dc2["items"])[0]["item_name"] != pnNew || pnLines(dc2["items"])[0]["qty"] != float64(3) {
		t.Fatalf("a line list kept as text stays text, renamed: %v", dc2["items"])
	}
	for _, id := range []string{"PR-DC-FILED", "PR-DC-OLD"} {
		if pnLines(pnData(t, schema, id)["items"])[0]["item_name"] != pnOld {
			t.Fatalf("%s should be unchanged", id)
		}
	}
	if pnData(t, schema, "PR-S1")["name"] != pnOld {
		t.Fatal("the Item master is not this tool's to change")
	}
	var version int
	var reason, by string
	db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(version, 0), COALESCE(edit_reason, ''), COALESCE(updated_by, '') FROM %s.documents WHERE id = 'PR-DC-1'`, schema)).Scan(&version, &reason, &by)
	if version < 2 || !strings.Contains(reason, pnOld) || by != "boss" {
		t.Fatalf("version %d, reason %q, by %q", version, reason, by)
	}

	var payload, checksum string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT payload::text, checksum FROM %s.print_snapshots WHERE doc_id = 'PR-PO-1' AND revision = 1`, schema)).Scan(&payload, &checksum); err != nil {
		t.Fatal(err)
	}
	var frozen PrintDocument
	_ = json.Unmarshal([]byte(payload), &frozen)
	l0, l1 := frozen.Lines[0], frozen.Lines[1]
	item, _ := l0["item"].(map[string]interface{})
	if l0["item_name"] != pnNew || l0["description"] != pnNew+" · Silver / 7" || item["name"] != pnNew || item["code"] != "PR-S1" {
		t.Fatalf("frozen print line 1: %v", l0)
	}
	if l1["item_name"] != pnOld {
		t.Fatalf("a charge line is not an item line: %v", l1)
	}
	if frozen.Number != "PR-PO-1" || checksum != printChecksum(frozen) || checksum == printChecksum(pnPrintPayload()) {
		t.Fatal("the frozen copy's checksum should be made again over what it now says")
	}

	var perDoc, runs int
	var detail string
	db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.audit_logs WHERE action = 'PAST_NAME_CHANGE'`, schema)).Scan(&perDoc)
	db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.audit_logs WHERE action = 'PAST_NAME_CHANGE_RUN'`, schema)).Scan(&runs)
	db.DB.QueryRow(fmt.Sprintf(`SELECT details FROM %s.audit_logs WHERE action = 'PAST_NAME_CHANGE' AND details LIKE '%%PR-PO-1%%'`, schema)).Scan(&detail)
	if perDoc != 3 || runs != 1 || !strings.Contains(detail, `old="`+pnOld+`"`) || !strings.Contains(detail, "po#1:lines[0].item_name") {
		t.Fatalf("audit: %d per document, %d runs, PO detail %q", perDoc, runs, detail)
	}

	// The same change again finds nothing left to change.
	again, err := PastRenameApply(tenant, req, "boss")
	if err != nil || again.Documents != 0 || again.Filed != 1 {
		t.Fatalf("second apply: %+v, %v", again, err)
	}
}

func TestPastRenameRefusesBadForms(t *testing.T) {
	tenant, _ := pastRenameTenant(t)
	cases := []struct {
		req  PastRenameRequest
		code string
	}{
		{PastRenameRequest{Old: "", New: "X"}, "MASTER-1705"},
		{PastRenameRequest{Old: "Same", New: "Same"}, "MASTER-1705"},
		{PastRenameRequest{Old: strings.Repeat("a", 201), New: "X"}, "MASTER-1705"},
		{PastRenameRequest{Old: "A", New: "B", From: "15-11-2026"}, "MASTER-1706"},
		{PastRenameRequest{Old: "A", New: "B", From: "2026-11-15", To: "2026-11-01"}, "MASTER-1706"},
	}
	for _, c := range cases {
		_, err := PastRenamePreviewFor(tenant, c.req)
		ve, ok := err.(*ValidationError)
		if !ok || ve.Code != c.code {
			t.Fatalf("%+v: want %s, got %v", c.req, c.code, err)
		}
	}
	// A case change alone is a real change.
	if _, err := PastRenamePreviewFor(tenant, PastRenameRequest{Old: "ring gold", New: "Ring Gold"}); err != nil {
		t.Fatalf("a case-only change should be allowed: %v", err)
	}
}

func TestPastRenameTwoAppliesAtOnceChangeEachDocumentOnce(t *testing.T) {
	tenant, schema := pastRenameTenant(t)
	pnSeed(t, schema)
	req := PastRenameRequest{Old: pnOld, New: pnNew, Doctypes: []string{"DeliveryChallan"}, From: "2026-06-01", Confirm: "YES"}
	var wg sync.WaitGroup
	results := make([]PastRenameResult, 2)
	errs := make([]error, 2)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = PastRenameApply(tenant, req, fmt.Sprintf("boss%d", i))
		}(i)
	}
	wg.Wait()
	if errs[0] != nil || errs[1] != nil {
		t.Fatalf("applies: %v / %v", errs[0], errs[1])
	}
	if got := results[0].Documents + results[1].Documents; got != 2 {
		t.Fatalf("two delivery challans change once between both applies, got %d (%+v)", got, results)
	}
	var perDoc int
	db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.audit_logs WHERE action = 'PAST_NAME_CHANGE'`, schema)).Scan(&perDoc)
	if perDoc != 2 {
		t.Fatalf("one audit row per changed document, got %d", perDoc)
	}
	// The purchase order was not picked: its frozen print is unchanged.
	var payload string
	db.DB.QueryRow(fmt.Sprintf(`SELECT payload::text FROM %s.print_snapshots WHERE doc_id = 'PR-PO-1'`, schema)).Scan(&payload)
	if !strings.Contains(payload, pnOld+" · Silver") {
		t.Fatal("a record type not picked is not changed")
	}
}
