package engines

import (
	"bytes"
	"custom_erp/db"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// Stage 148.18 - the vendor's signed PO link: off by default, scoped to one
// PO, expiring and revocable; qty / date proposals applied by the buyer line
// by line (at once for a trusted vendor); per-vendor unit names; an uploaded
// invoice that waits for review; reminders for overdue lines.

type vlFixture struct {
	*poLCFixture
}

func newVLFixture(t *testing.T) *vlFixture {
	t.Helper()
	f := &vlFixture{newPOLCFixture(t)}
	f.base()
	f.setting(SettingVendorLink, "true")
	return f
}

func (f *vlFixture) setting(key, val string) {
	f.t.Helper()
	if err := SetSetting(f.tenantID, key, val, "test"); err != nil {
		f.t.Fatalf("setting %s: %v", key, err)
	}
}

// an Approved PO with two lines, due in the past so it is overdue.
func (f *vlFixture) openPO(id string) {
	f.t.Helper()
	raw, _ := json.Marshal([]map[string]interface{}{
		{"sku": "LC-A", "qty": 10, "rate": 100.0, "confirmation_no": "C-1", "confirmation_date": "2030-01-01"},
		{"sku": "LC-B", "qty": 5, "rate": 50.0},
	})
	f.insert(id, "PurchaseOrder", "Approved", map[string]interface{}{"code": id, "vendor": "LC-V", "location": "LC-LOC",
		"gst_mode": GSTModeExclusive, "items": string(raw), "expected_delivery_date": "2020-01-01"})
}

func (f *vlFixture) vendorSet(patch map[string]interface{}) {
	f.t.Helper()
	b, _ := json.Marshal(patch)
	if _, err := db.DB.Exec("UPDATE "+f.schema+".documents SET data = data || $1::jsonb WHERE doctype = 'Vendor' AND id = 'LC-V'", b); err != nil {
		f.t.Fatalf("vendor: %v", err)
	}
}

func (f *vlFixture) link(po string) string {
	f.t.Helper()
	info, err := CreateVendorPOLink(f.tenantID, "buyer1", po)
	if err != nil {
		f.t.Fatalf("CreateVendorPOLink: %v", err)
	}
	return info.Token
}

func (f *vlFixture) poLines(id string) []map[string]interface{} {
	f.t.Helper()
	po, _, err := fetchDocData(f.tenantID, "PurchaseOrder", id)
	if err != nil {
		f.t.Fatal(err)
	}
	lines, _ := parsePOLineMaps(strField(po, "items"))
	return lines
}

func qty(n float64) *float64 { return &n }

func TestStage14818OffByDefaultAndNothingStored(t *testing.T) {
	f := newPOLCFixture(t)
	f.base()
	f.insert("VL-OFF", "PurchaseOrder", "Approved", map[string]interface{}{"code": "VL-OFF", "vendor": "LC-V", "items": `[{"sku":"LC-A","qty":1,"rate":1}]`})
	if VendorLinkOn(f.tenantID) {
		t.Fatal("procurement.vendor_link must be off by default")
	}
	if _, err := CreateVendorPOLink(f.tenantID, "buyer1", "VL-OFF"); lcCode(err) != "PURCHA-5060" {
		t.Fatalf("link while off: want PURCHA-5060, got %v", err)
	}
	var n int
	_ = db.DB.QueryRow("SELECT count(*) FROM " + f.schema + ".vendor_po_links").Scan(&n)
	if n != 0 {
		t.Fatalf("off stores nothing, got %d rows", n)
	}
}

func TestStage14818LinkScopeExpiryRevoke(t *testing.T) {
	f := newVLFixture(t)
	f.openPO("VL-1")
	f.openPO("VL-2")
	tok := f.link("VL-1")
	v, err := VendorLinkPublicView(f.tenantID, tok)
	if err != nil {
		t.Fatalf("view: %v", err)
	}
	if v.PO != "VL-1" || len(v.Lines) != 2 || v.Vendor != "LC Vendor" {
		t.Fatalf("view = %+v", v)
	}
	raw, _ := json.Marshal(v)
	for _, secret := range []string{"LC-V", "VL-2", "buyer1", strings.SplitN(tok, ".", 2)[0]} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("the public page leaks %q: %s", secret, raw)
		}
	}
	// a forged or cut token, and another tenant's, are all the same refusal.
	for name, bad := range map[string]string{"empty": "", "garbage": "abc.def", "cut": tok[:len(tok)-3], "swapped": strings.SplitN(tok, ".", 2)[0] + ".AAAAAAAAAAAAAAAAAAAAAA"} {
		if _, err := VendorLinkPublicView(f.tenantID, bad); lcCode(err) != "PURCHA-5061" {
			t.Fatalf("%s token: want PURCHA-5061, got %v", name, err)
		}
	}
	if _, err := VendorLinkPublicView("default", tok); lcCode(err) != "PURCHA-5061" {
		t.Fatalf("another client's token: want PURCHA-5061, got %v", err)
	}
	// expires after 14 days.
	var exp time.Time
	_ = db.DB.QueryRow("SELECT expires_at FROM " + f.schema + ".vendor_po_links").Scan(&exp)
	if d := time.Until(exp); d < 13*24*time.Hour || d > 14*24*time.Hour+time.Minute {
		t.Fatalf("expiry in %v, want 14 days", d)
	}
	db.DB.Exec("UPDATE " + f.schema + ".vendor_po_links SET expires_at = now() - interval '1 minute'")
	if _, err := VendorLinkPublicView(f.tenantID, tok); lcCode(err) != "PURCHA-5061" {
		t.Fatalf("expired: want PURCHA-5061, got %v", err)
	}
	// a new link revokes the old one for that PO; revoke kills the new one.
	tok2 := f.link("VL-1")
	if _, err := VendorLinkPublicView(f.tenantID, tok2); err != nil {
		t.Fatalf("fresh link: %v", err)
	}
	if err := RevokeVendorPOLink(f.tenantID, "buyer1", "VL-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := VendorLinkPublicView(f.tenantID, tok2); lcCode(err) != "PURCHA-5061" {
		t.Fatalf("revoked: want PURCHA-5061, got %v", err)
	}
	// turning the setting off kills every link without touching them.
	tok3 := f.link("VL-1")
	f.setting(SettingVendorLink, "false")
	if _, err := VendorLinkPublicView(f.tenantID, tok3); lcCode(err) != "PURCHA-5061" {
		t.Fatalf("setting off: want PURCHA-5061, got %v", err)
	}
	f.setting(SettingVendorLink, "true")
	// only an Approved PO gets a link.
	f.insert("VL-DRAFT", "PurchaseOrder", "Draft", map[string]interface{}{"code": "VL-DRAFT", "vendor": "LC-V", "items": `[{"sku":"LC-A","qty":1,"rate":1}]`})
	if _, err := CreateVendorPOLink(f.tenantID, "buyer1", "VL-DRAFT"); lcCode(err) != "PURCHA-5062" {
		t.Fatalf("draft PO: want PURCHA-5062, got %v", err)
	}
}

func TestStage14818ProposalValidation(t *testing.T) {
	f := newVLFixture(t)
	f.openPO("VL-3")
	tok := f.link("VL-3")
	bad := map[string][]VendorProposalLine{
		"no line":      {{LineNo: 9, Qty: qty(3)}},
		"zero qty":     {{LineNo: 1, Qty: qty(0)}},
		"negative qty": {{LineNo: 1, Qty: qty(-2)}},
		"past date":    {{LineNo: 1, Date: "2020-01-01"}},
		"bad date":     {{LineNo: 1, Date: "next week"}},
		"nothing":      {{LineNo: 1}},
		"empty":        {},
	}
	for name, lines := range bad {
		if _, err := SubmitVendorProposal(f.tenantID, tok, lines); lcCode(err) != "PURCHA-5063" {
			t.Fatalf("%s: want PURCHA-5063, got %v", name, err)
		}
	}
	if _, err := SubmitVendorProposal(f.tenantID, "x.y", []VendorProposalLine{{LineNo: 1, Qty: qty(3)}}); lcCode(err) != "PURCHA-5061" {
		t.Fatalf("bad link: want PURCHA-5061, got %v", err)
	}
	// a closed PO takes no proposal.
	db.DB.Exec("UPDATE " + f.schema + ".documents SET status = 'Closed' WHERE id = 'VL-3'")
	if _, err := SubmitVendorProposal(f.tenantID, tok, []VendorProposalLine{{LineNo: 1, Qty: qty(3)}}); lcCode(err) != "PURCHA-5062" {
		t.Fatalf("closed PO: want PURCHA-5062, got %v", err)
	}
}

func TestStage14818BuyerAppliesDateAndQty(t *testing.T) {
	f := newVLFixture(t)
	f.openPO("VL-4")
	tok := f.link("VL-4")
	future := time.Now().AddDate(0, 0, 20).Format("2006-01-02")
	res, err := SubmitVendorProposal(f.tenantID, tok, []VendorProposalLine{
		{LineNo: 1, Date: future, Note: "stock arrives late"},
		{LineNo: 2, Qty: qty(4)},
	})
	if err != nil || res.Pending != 2 || res.Applied != 0 {
		t.Fatalf("submit: %+v %v", res, err)
	}
	// nothing moved on the PO yet: the vendor only proposes.
	if l := f.poLines("VL-4"); numFromInterface(l[1]["qty"]) != 5 || strField(l[0], "confirmation_no") != "C-1" {
		t.Fatalf("the PO changed before the buyer applied: %v", l)
	}
	st, err := VendorLinkAdminView(f.tenantID, "VL-4")
	if err != nil || len(st.Proposals) != 2 {
		t.Fatalf("admin view: %+v %v", st, err)
	}
	var dateP, qtyP int64
	for _, p := range st.Proposals {
		if p.LineNo == 1 {
			dateP = p.ID
		} else {
			qtyP = p.ID
		}
	}
	// date: line 1 gets one instalment on that date and its confirmation re-opens.
	if err := ApplyVendorProposal(f.tenantID, "buyer1", "Purchase Manager", dateP, false); err != nil {
		t.Fatalf("apply date: %v", err)
	}
	l := f.poLines("VL-4")
	if strField(l[0], "confirmation_no") != "" || strField(l[0], "confirmation_reopened_at") == "" {
		t.Fatalf("applied date must re-open confirmation: %v", l[0])
	}
	if b, _ := json.Marshal(l[0]["instalments"]); !strings.Contains(string(b), future) {
		t.Fatalf("instalment on %s expected: %s", future, b)
	}
	if numFromInterface(l[0]["qty"]) != 10 {
		t.Fatalf("a date change must not change the qty: %v", l[0])
	}
	// the same proposal cannot be applied twice.
	if err := ApplyVendorProposal(f.tenantID, "buyer1", "Purchase Manager", dateP, false); lcCode(err) != "PURCHA-5064" {
		t.Fatalf("second apply: want PURCHA-5064, got %v", err)
	}
	// qty: an amendment of the PO's line, with the reason, the total follows.
	if err := ApplyVendorProposal(f.tenantID, "buyer1", "Purchase Manager", qtyP, false); err != nil {
		t.Fatalf("apply qty: %v", err)
	}
	l = f.poLines("VL-4")
	if numFromInterface(l[1]["qty"]) != 4 {
		t.Fatalf("line 2 qty = %v, want 4", l[1]["qty"])
	}
	po, _, _ := fetchDocData(f.tenantID, "PurchaseOrder", "VL-4")
	if got := numFromInterface(po["total_amount"]); got != 10*100+4*50 {
		t.Fatalf("total = %v, want 1200", got)
	}
	// reject leaves the PO alone.
	res, _ = SubmitVendorProposal(f.tenantID, tok, []VendorProposalLine{{LineNo: 2, Qty: qty(9)}})
	st, _ = VendorLinkAdminView(f.tenantID, "VL-4")
	var pend int64
	for _, p := range st.Proposals {
		if p.Status == "Pending" {
			pend = p.ID
		}
	}
	if err := RejectVendorProposal(f.tenantID, "buyer1", pend, "too many"); err != nil {
		t.Fatal(err)
	}
	if numFromInterface(f.poLines("VL-4")[1]["qty"]) != 4 {
		t.Fatal("a rejected proposal must not change the PO")
	}
	// a line with several instalments cannot take a date from a proposal.
	f.openPO("VL-4B")
	multi := []map[string]interface{}{{"sku": "LC-A", "qty": 10, "rate": 100.0, "instalments": []map[string]interface{}{{"qty": 5, "date": "2030-02-01"}, {"qty": 5, "date": "2030-03-01"}}}}
	mb, _ := json.Marshal(multi)
	db.DB.Exec("UPDATE "+f.schema+".documents SET data = data || jsonb_build_object('items', $1::text) WHERE id = 'VL-4B'", string(mb))
	tok2 := f.link("VL-4B")
	_, _ = SubmitVendorProposal(f.tenantID, tok2, []VendorProposalLine{{LineNo: 1, Date: future}})
	st, _ = VendorLinkAdminView(f.tenantID, "VL-4B")
	if err := ApplyVendorProposal(f.tenantID, "buyer1", "Purchase Manager", st.Proposals[0].ID, false); lcCode(err) != "PURCHA-5065" {
		t.Fatalf("several instalments: want PURCHA-5065, got %v", err)
	}
}

func TestStage14818TrustedVendorAndUnitNames(t *testing.T) {
	f := newVLFixture(t)
	f.openPO("VL-5")
	tok := f.link("VL-5")
	future := time.Now().AddDate(0, 0, 9).Format("2006-01-02")
	// not trusted: waits.
	res, err := SubmitVendorProposal(f.tenantID, tok, []VendorProposalLine{{LineNo: 2, Date: future}})
	if err != nil || res.Applied != 0 || res.Pending != 1 {
		t.Fatalf("untrusted: %+v %v", res, err)
	}
	// trusted: applied at once, marked auto.
	f.vendorSet(map[string]interface{}{"vendor_link_trusted": "Yes"})
	res, err = SubmitVendorProposal(f.tenantID, tok, []VendorProposalLine{{LineNo: 1, Date: future}})
	if err != nil || res.Applied != 1 {
		t.Fatalf("trusted: %+v %v", res, err)
	}
	if b, _ := json.Marshal(f.poLines("VL-5")[0]["instalments"]); !strings.Contains(string(b), future) {
		t.Fatalf("trusted proposal not applied: %s", b)
	}
	// a unit the buyer has not mapped waits, even for a trusted vendor.
	res, _ = SubmitVendorProposal(f.tenantID, tok, []VendorProposalLine{{LineNo: 2, Date: future, Unit: "pieces"}})
	if res.Applied != 0 || res.Pending != 1 {
		t.Fatalf("unknown unit must wait: %+v", res)
	}
	st, _ := VendorLinkAdminView(f.tenantID, "VL-5")
	var id int64
	for _, p := range st.Proposals {
		if p.Status == "Pending" && p.VendorUnit == "pieces" {
			id = p.ID
			if p.UnitKnown {
				t.Fatal("pieces is not yet known")
			}
		}
	}
	if err := ApplyVendorProposal(f.tenantID, "buyer1", "Purchase Manager", id, true); err != nil {
		t.Fatal(err)
	}
	var item string
	_ = db.DB.QueryRow("SELECT item_unit FROM " + f.schema + ".vendor_unit_names WHERE vendor_id = 'LC-V' AND vendor_unit = 'pieces'").Scan(&item)
	if item == "" {
		t.Fatal("the vendor's unit name was not remembered")
	}
	// now it is known: the trusted vendor's next proposal in "Pieces" applies at once.
	res, _ = SubmitVendorProposal(f.tenantID, tok, []VendorProposalLine{{LineNo: 2, Date: time.Now().AddDate(0, 0, 11).Format("2006-01-02"), Unit: "Pieces"}})
	if res.Applied != 1 {
		t.Fatalf("known unit should apply for a trusted vendor: %+v", res)
	}
}

func TestStage14818InvoiceWaitsForReview(t *testing.T) {
	f := newVLFixture(t)
	f.openPO("VL-6")
	tok := f.link("VL-6")
	pdf := append([]byte("%PDF-1.4\n"), bytes.Repeat([]byte("x"), 100)...)
	var docsBefore int
	_ = db.DB.QueryRow("SELECT count(*) FROM " + f.schema + ".documents WHERE doctype = 'VendorInvoice'").Scan(&docsBefore)
	if err := SubmitVendorInvoice(f.tenantID, tok, "bill 17.pdf", pdf, "INV-17", "2026-11-01", qty(1234.5)); err != nil {
		t.Fatalf("upload: %v", err)
	}
	var docsAfter int
	_ = db.DB.QueryRow("SELECT count(*) FROM " + f.schema + ".documents WHERE doctype = 'VendorInvoice'").Scan(&docsAfter)
	if docsAfter != docsBefore {
		t.Fatal("an uploaded invoice must never post")
	}
	for name, c := range map[string]struct {
		file string
		data []byte
	}{
		"exe":      {"run.exe", append([]byte("MZ"), bytes.Repeat([]byte("x"), 50)...)},
		"renamed":  {"fake.pdf", []byte("<html>not a pdf</html>")},
		"too big":  {"big.pdf", append([]byte("%PDF-"), bytes.Repeat([]byte("x"), 2<<20)...)},
		"empty":    {"none.pdf", nil},
		"no name":  {"", pdf},
		"path":     {"../../x.pdf", pdf},
		"wrongext": {"image.png", pdf},
	} {
		err := SubmitVendorInvoice(f.tenantID, tok, c.file, c.data, "", "", nil)
		if name == "path" {
			if err != nil {
				t.Fatalf("path name is cleaned, not refused: %v", err)
			}
			continue
		}
		if lcCode(err) != "PURCHA-5066" {
			t.Fatalf("%s: want PURCHA-5066, got %v", name, err)
		}
	}
	var stored string
	_ = db.DB.QueryRow("SELECT string_agg(file_name, '|') FROM " + f.schema + ".vendor_po_files WHERE file_name LIKE '%x.pdf'").Scan(&stored)
	if strings.Contains(stored, "/") || strings.Contains(stored, "..") {
		t.Fatalf("stored name %q", stored)
	}
	// five per link.
	for i := 0; i < 5; i++ {
		_ = SubmitVendorInvoice(f.tenantID, tok, "more.pdf", pdf, "", "", nil)
	}
	if err := SubmitVendorInvoice(f.tenantID, tok, "last.pdf", pdf, "", "", nil); lcCode(err) != "PURCHA-5066" {
		t.Fatalf("sixth file: want PURCHA-5066, got %v", err)
	}
	st, _ := VendorLinkAdminView(f.tenantID, "VL-6")
	if len(st.Files) == 0 || st.Files[0].Status != "Pending review" || st.Files[0].InvoiceNo == "" && st.Files[len(st.Files)-1].InvoiceNo != "INV-17" {
		t.Fatalf("files: %+v", st.Files)
	}
	first := st.Files[len(st.Files)-1]
	name, ctype, data, err := VendorLinkFile(f.tenantID, first.ID)
	if err != nil || ctype != "application/pdf" || !bytes.Equal(data, pdf) || name == "" {
		t.Fatalf("download: %q %q %v", name, ctype, err)
	}
	if err := ReviewVendorFile(f.tenantID, "buyer1", first.ID, "Reviewed", "keyed as a bill"); err != nil {
		t.Fatal(err)
	}
	if err := ReviewVendorFile(f.tenantID, "buyer1", first.ID, "Posted", ""); lcCode(err) != "PURCHA-5067" {
		t.Fatalf("odd status: want PURCHA-5067, got %v", err)
	}
}

func TestStage14818OverdueReminderCarriesLink(t *testing.T) {
	f := newVLFixture(t)
	f.openPO("VL-7")
	f.vendorSet(map[string]interface{}{"contact_email": "vendor@example.com"})
	f.setting("crm.public_link_base", "https://erp.example.com")
	count := func() (n int) {
		_ = db.DB.QueryRow("SELECT count(*) FROM " + f.schema + ".integration_event_outbox WHERE event_name = 'email.send' AND payload->>'to' = 'vendor@example.com'").Scan(&n)
		return
	}
	// switch off: nothing is sent.
	f.setting(SettingVendorLink, "false")
	if err := RunVendorLinkReminders(f.tenantID, f.schema); err != nil || count() != 0 {
		t.Fatalf("off: %v %d", err, count())
	}
	f.setting(SettingVendorLink, "true")
	if err := RunVendorLinkReminders(f.tenantID, f.schema); err != nil {
		t.Fatal(err)
	}
	if count() != 1 {
		t.Fatalf("one reminder expected, got %d", count())
	}
	var body string
	_ = db.DB.QueryRow("SELECT payload->>'body' FROM " + f.schema + ".integration_event_outbox WHERE payload->>'to' = 'vendor@example.com'").Scan(&body)
	if !strings.Contains(body, "https://erp.example.com/p/vendor.html?t=") || !strings.Contains(body, "VL-7") {
		t.Fatalf("body = %q", body)
	}
	// the link in the mail is a working one.
	i := strings.Index(body, "?t=")
	token := strings.Fields(body[i+3:])[0]
	token = strings.SplitN(token, "&", 2)[0]
	if _, err := VendorLinkPublicView(f.tenantID, token); err != nil {
		t.Fatalf("the reminder's link does not open: %v", err)
	}
	// not again within a week.
	_ = RunVendorLinkReminders(f.tenantID, f.schema)
	if count() != 1 {
		t.Fatalf("a second reminder inside 7 days: %d", count())
	}
	// no email on the vendor: nothing, no error.
	f.openPO("VL-8")
	f.vendorSet(map[string]interface{}{"contact_email": ""})
	if err := RunVendorLinkReminders(f.tenantID, f.schema); err != nil {
		t.Fatal(err)
	}
}
