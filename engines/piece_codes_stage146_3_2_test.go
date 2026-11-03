package engines

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"

	"custom_erp/db"
)

// Stage 146.3.2: codes per piece minted at the goods receipt. An item type
// opts in ("Code for every piece received"), a store setting switches the
// store on, and posting the GRN then gives every accepted piece its own
// serial code from the PieceCode number series.

type pcFixture struct {
	tenantID, schema string
	insert           func(id, doctype, status, createdBy string, data map[string]interface{})
}

func pcSetup(t *testing.T, optIn bool, stores string) pcFixture {
	t.Helper()
	tenantID, schema, insert := stage140Tenant(t)
	typ := "No"
	if optIn {
		typ = "Yes"
	}
	insert("PCType", "ItemType", "Active", "system", map[string]interface{}{"name": "PCType", "piece_codes": typ})
	insert("ITEM-PC1", "Item", "Active", "system", map[string]interface{}{"code": "PC1", "name": "Ring", "item_type": "PCType"})
	insert("ITEM-PC2", "Item", "Active", "system", map[string]interface{}{"code": "PC2", "name": "Chain", "item_type": "PCType", "tracking_mode": "Serial"})
	InvalidateItemTypeCache(tenantID)
	if stores != "" {
		if err := SetSetting(tenantID, "inventory.piece_codes_stores", stores, "test"); err != nil {
			t.Fatal(err)
		}
	}
	return pcFixture{tenantID, schema, insert}
}

func (f pcFixture) grn(t *testing.T, id string, lines []map[string]interface{}) []interface{} {
	t.Helper()
	raw, _ := json.Marshal(lines)
	f.insert(id, "GRN", "Posted", "system", map[string]interface{}{"location": "PCS", "received_items": string(raw)})
	out := make([]interface{}, len(lines))
	for i, l := range lines {
		out[i] = l
	}
	return out
}

func (f pcFixture) serials(t *testing.T, sku string) []string {
	t.Helper()
	rows, err := db.DB.Query("SELECT data->>'serial_no' FROM "+f.schema+".documents WHERE doctype = 'SerialNumber' AND data->>'item' = $1 ORDER BY 1", sku)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		_ = rows.Scan(&s)
		out = append(out, s)
	}
	return out
}

func (f pcFixture) storedCodes(t *testing.T, grn string, idx int) []string {
	t.Helper()
	var raw string
	if err := db.DB.QueryRow("SELECT data->>'received_items' FROM "+f.schema+".documents WHERE doctype = 'GRN' AND id = $1", grn).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var lines []grnReceivedLine
	if err := json.Unmarshal([]byte(raw), &lines); err != nil {
		t.Fatal(err)
	}
	return lines[idx].SerialNumbers
}

func TestStage14632OptedInGRNMintsOnePerAcceptedPiece(t *testing.T) {
	f := pcSetup(t, true, "all")
	items := f.grn(t, "GRN-PC-A", []map[string]interface{}{{"sku": "PC1", "qty": 6.0, "accepted_qty": 5.0, "rejected_qty": 1.0, "rejection_reason": "bent"}})
	if _, err := PostGRNReceiptWithQC(f.tenantID, "PCS", items, "system", "GRN-PC-A"); err != nil {
		t.Fatal(err)
	}
	got := f.serials(t, "PC1")
	if len(got) != 5 {
		t.Fatalf("minted %d serials %v; want 5 (the accepted pieces, not the 6 received)", len(got), got)
	}
	seen := map[string]bool{}
	for _, c := range got {
		if seen[c] || !strings.HasPrefix(c, "PC") {
			t.Fatalf("codes must be unique and from the PieceCode series: %v", got)
		}
		seen[c] = true
	}
	stored := f.storedCodes(t, "GRN-PC-A", 0)
	sort.Strings(stored)
	if strings.Join(stored, ",") != strings.Join(got, ",") {
		t.Fatalf("the GRN line keeps %v; the serial register has %v", stored, got)
	}
}

func TestStage14632NotOptedInMintsNone(t *testing.T) {
	for name, f := range map[string]pcFixture{
		"type says no":     pcSetup(t, false, "all"),
		"store switch off": pcSetup(t, true, ""),
		"other store only": pcSetup(t, true, "ELSEWHERE"),
	} {
		items := f.grn(t, "GRN-PC-N", []map[string]interface{}{{"sku": "PC1", "qty": 3.0}})
		if _, err := PostGRNReceiptWithQC(f.tenantID, "PCS", items, "system", "GRN-PC-N"); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got := f.serials(t, "PC1"); len(got) != 0 {
			t.Fatalf("%s: minted %v; want none", name, got)
		}
	}
}

func TestStage14632RepostAndReprintDoNotMintAgain(t *testing.T) {
	f := pcSetup(t, true, "PCS,OTHER")
	items := f.grn(t, "GRN-PC-R", []map[string]interface{}{{"sku": "PC1", "qty": 3.0}})
	if _, err := PostGRNReceiptWithQC(f.tenantID, "PCS", items, "system", "GRN-PC-R"); err != nil {
		t.Fatal(err)
	}
	first := f.serials(t, "PC1")
	if len(first) != 3 {
		t.Fatalf("first post minted %v", first)
	}
	// The same receipt posted again (the caller still holds the line as it
	// first came, without codes): the codes already on the GRN are reused.
	again := []interface{}{map[string]interface{}{"sku": "PC1", "qty": 3.0}}
	_, _ = PostGRNReceiptWithQC(f.tenantID, "PCS", again, "system", "GRN-PC-R")
	if got := f.serials(t, "PC1"); len(got) != 3 {
		t.Fatalf("a re-post minted more: %v", got)
	}

	// Reprint: the sticker run is one label per stored code, twice the same.
	lines, err := ResolveDocumentStickerLines(f.tenantID, "GRN", "GRN-PC-R")
	if err != nil {
		t.Fatal(err)
	}
	var codes []string
	for _, l := range lines {
		if l.Qty != 1 || l.PieceCode == "" {
			t.Fatalf("each piece prints its own label: %+v", l)
		}
		codes = append(codes, l.PieceCode)
	}
	sort.Strings(codes)
	if strings.Join(codes, ",") != strings.Join(first, ",") {
		t.Fatalf("stickers print %v; the pieces are %v", codes, first)
	}
	lines2, _ := ResolveDocumentStickerLines(f.tenantID, "GRN", "GRN-PC-R")
	if len(lines2) != 3 || lines2[0].PieceCode != lines[0].PieceCode {
		t.Fatalf("a second resolve changed the codes: %+v", lines2)
	}
	if got := f.serials(t, "PC1"); len(got) != 3 {
		t.Fatalf("resolving stickers minted: %v", got)
	}
}

func TestStage14632ManualAndMintedCodesMix(t *testing.T) {
	f := pcSetup(t, true, "all")
	// PC2 is also serial-tracked: fewer codes than pieces used to be refused.
	items := f.grn(t, "GRN-PC-M", []map[string]interface{}{{"sku": "PC2", "qty": 4.0, "serial_numbers": []string{"MAN-1", "MAN-2"}}})
	if err := ValidateReceiptPieceLine(f.tenantID, "PCS", "PC2", []string{"MAN-1", "MAN-2"}, 4); err != nil {
		t.Fatalf("two typed codes for four pieces on an opted-in item: %v", err)
	}
	if err := ValidateReceiptPieceLine(f.tenantID, "PCS", "PC2", []string{"A", "B", "C", "D", "E"}, 4); err == nil {
		t.Fatal("five codes for four pieces was accepted")
	}
	if err := ValidateReceiptPieceLine(f.tenantID, "PCS", "PC2", []string{"A", "A"}, 4); err == nil {
		t.Fatal("a code listed twice was accepted")
	}
	if _, err := PostGRNReceiptWithQC(f.tenantID, "PCS", items, "system", "GRN-PC-M"); err != nil {
		t.Fatal(err)
	}
	got := f.serials(t, "PC2")
	if len(got) != 4 {
		t.Fatalf("serials = %v; want the 2 typed + 2 minted", got)
	}
	have := map[string]bool{}
	for _, c := range got {
		have[c] = true
	}
	if !have["MAN-1"] || !have["MAN-2"] {
		t.Fatalf("typed codes were dropped: %v", got)
	}
	if st := f.storedCodes(t, "GRN-PC-M", 0); len(st) != 4 || st[0] != "MAN-1" || st[1] != "MAN-2" {
		t.Fatalf("the GRN line keeps %v; want typed first, then the 2 minted", st)
	}
}

func TestStage14632ScanExistingCodeOrMint(t *testing.T) {
	f := pcSetup(t, true, "all")
	// RF receiving: a scanned code is taken as typed; "mint" asks for the next.
	c1, err := MintPieceCodes(f.tenantID, 2)
	if err != nil || len(c1) != 2 || c1[0] == c1[1] {
		t.Fatalf("mint = %v, %v", c1, err)
	}
	c2, _ := MintPieceCodes(f.tenantID, 1)
	if c2[0] == c1[0] || c2[0] == c1[1] {
		t.Fatalf("the series repeated a code: %v then %v", c1, c2)
	}
	if !PieceCodesOn(f.tenantID, "PC1", "PCS") || PieceCodesOn(f.tenantID, "NOSUCH", "PCS") {
		t.Fatal("PieceCodesOn: want true for the opted-in item, false for an unknown one")
	}
}

// The RF menu shows "Piece codes", and the mint route answers, only when the
// store setting is not blank.
func TestStage14632RFPieceCodesOfferedOnlyWhenSettingSet(t *testing.T) {
	f := pcSetup(t, true, "")
	if PieceCodesSwitchedOn(f.tenantID) {
		t.Fatal("blank setting reported as on")
	}
	if err := SetSetting(f.tenantID, "inventory.piece_codes_stores", "PCS", "test"); err != nil {
		t.Fatal(err)
	}
	if !PieceCodesSwitchedOn(f.tenantID) {
		t.Fatal("a store list reported as off")
	}
}
