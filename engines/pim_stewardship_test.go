package engines

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"image"
	"image/color"
	"image/png"
	"strings"
	"sync"
	"testing"
	"time"

	"custom_erp/db"
)

// Stage 47.13.4 (stewardship register, survivorship, provenance) and 146.5.9
// (image order per channel), through the doors the app uses: the document
// save (ValidateDocument), the bulk design-value writer, the approval engine,
// the channel publish payload and the feed export.

type stewardInsert = func(id, doctype, status, createdBy string, data map[string]interface{})

func stewardFixture(t *testing.T) (string, string, stewardInsert) {
	tenantID, schema, insert := stage140Tenant(t)
	for _, u := range []string{"owner1", "steward1"} {
		if _, err := db.DB.Exec("INSERT INTO "+schema+".users (id, username, password_hash, role) VALUES ($1, $1, 'x', 'Store Manager')", u); err != nil {
			t.Fatal(err)
		}
	}
	insert("COLOR", "ProductAttributeDef", "Active", "system", map[string]interface{}{"code": "COLOR", "label": "Colour", "value_type": "Data", "status": "Active"})
	insert("AMZ", "Channel", "Active", "system", map[string]interface{}{"code": "AMZ", "name": "Amazon"})
	insert("ITM-1", "Item", "Active", "system", map[string]interface{}{"code": "ITM-1", "name": "Shirt"})
	return tenantID, schema, insert
}

func stewardChange(t *testing.T, tenantID string, insert stewardInsert, id string, data map[string]interface{}) {
	t.Helper()
	if err := validateStewardshipChange(tenantID, data); err != nil {
		t.Fatalf("%s should save: %v", id, err)
	}
	data["code"], data["status"] = id, "Draft"
	insert(id, StewardshipChangeDoctype, "Draft", "maker1", data)
	if err := SubmitForApproval(tenantID, StewardshipChangeDoctype, id, "maker1", "Store Manager"); err != nil {
		t.Fatalf("submit %s: %v", id, err)
	}
	if err := DecideApproval(tenantID, StewardshipChangeDoctype, id, "admin", "HR/Admin", "HO", "Approved", "ok"); err != nil {
		t.Fatalf("approve %s: %v", id, err)
	}
}

func stewardSet(attr, sor, ref, surv string) map[string]interface{} {
	return map[string]interface{}{"action": "Set", "attribute": attr, "system_of_record": sor, "sor_ref": ref,
		"data_owner": "owner1", "data_steward": "steward1", "confidence": "High", "survivorship": surv, "channels": "AMZ"}
}

func TestStewardshipRegisterNeedsApproval(t *testing.T) {
	tenantID, _, insert := stewardFixture(t)

	var ve *ValidationError
	for label, bad := range map[string]map[string]interface{}{
		"unknown steward": {"action": "Set", "attribute": "COLOR", "system_of_record": "ERP", "data_owner": "owner1", "data_steward": "nobody", "confidence": "High", "survivorship": stewardLatestWins},
		"unknown attr":    {"action": "Set", "attribute": "NOPE", "system_of_record": "ERP", "data_owner": "owner1", "data_steward": "steward1", "confidence": "High", "survivorship": stewardLatestWins},
		"channel no ref":  {"action": "Set", "attribute": "COLOR", "system_of_record": "Channel", "data_owner": "owner1", "data_steward": "steward1", "confidence": "High", "survivorship": stewardLatestWins},
		"remove nothing":  {"action": "Remove", "attribute": "COLOR"},
	} {
		if err := validateStewardshipChange(tenantID, bad); !errors.As(err, &ve) || ve.Code != "PIM-9150" {
			t.Errorf("%s should be refused with PIM-9150, got %v", label, err)
		}
	}

	good := stewardSet("COLOR", "Channel", "AMZ", stewardSurvivorWins)
	good["code"], good["status"] = "STC-1", "Draft"
	insert("STC-1", StewardshipChangeDoctype, "Draft", "maker1", good)
	if err := SubmitForApproval(tenantID, StewardshipChangeDoctype, "STC-1", "maker1", "Store Manager"); err != nil {
		t.Fatal(err)
	}
	if loadStewardship(tenantID, "COLOR") != nil {
		t.Fatal("the entry exists before the change was approved")
	}
	second := stewardSet("COLOR", "ERP", "", stewardSurvivorWins)
	if err := ValidateTransactionalRules(tenantID, StewardshipChangeDoctype, "", "", nil, second); !errors.As(err, &ve) || ve.Code != "PIM-9150" {
		t.Fatalf("a second request while one waits should be refused with PIM-9150, got %v", err)
	}
	if err := DecideApproval(tenantID, StewardshipChangeDoctype, "STC-1", "maker1", "Store Manager", "HO", "Approved", "self"); err == nil {
		t.Fatal("the person who asked must not approve their own change")
	}
	if err := DecideApproval(tenantID, StewardshipChangeDoctype, "STC-1", "admin", "HR/Admin", "HO", "Approved", "ok"); err != nil {
		t.Fatal(err)
	}
	e := loadStewardship(tenantID, "COLOR")
	if e == nil || e.SystemOfRecord != "Channel" || e.SORRef != "AMZ" || e.DataSteward != "steward1" || e.Survivorship != stewardSurvivorWins {
		t.Fatalf("approved entry: %+v", e)
	}
	rows, err := StewardshipOverview(tenantID)
	if err != nil || len(rows) != 1 || rows[0].Entry == nil || rows[0].Entry.Attribute != "COLOR" {
		t.Fatalf("overview: %+v %v", rows, err)
	}
	stewardChange(t, tenantID, insert, "STC-2", map[string]interface{}{"action": "Remove", "attribute": "COLOR"})
	if loadStewardship(tenantID, "COLOR") != nil {
		t.Fatal("the entry was not removed")
	}
}

func TestStewardshipSurvivorshipAndProvenanceAtTheDoors(t *testing.T) {
	tenantID, schema, insert := stewardFixture(t)
	save := func(source, value string) (map[string]interface{}, error) {
		p := map[string]interface{}{"code": "ITM-1::COLOR", "item": "ITM-1", "attribute": "COLOR", "value": value, "status": "Active"}
		if source != "" {
			p["source"] = source
		}
		return p, ValidateDocument(tenantID, "ProductAttributeValue", p)
	}

	// No entry: nothing changes, nothing is stamped.
	if p, err := save("Import", "Red"); err != nil || p["confidence"] != nil {
		t.Fatalf("with no entry: %v %v", p, err)
	}

	// The system of record is the AMZ channel and it wins.
	stewardChange(t, tenantID, insert, "STC-3", stewardSet("COLOR", "Channel", "AMZ", stewardSurvivorWins))
	var ve *ValidationError
	if _, err := save("Import", "Blue"); !errors.As(err, &ve) || ve.Code != "PIM-9151" {
		t.Fatalf("an import must be refused with PIM-9151, got %v", err)
	}
	if _, err := save("", "Blue"); !errors.As(err, &ve) || ve.Code != "PIM-9151" {
		t.Fatalf("an ERP write must be refused too, got %v", err)
	}
	p := map[string]interface{}{"code": "ITM-1::COLOR", "item": "ITM-1", "attribute": "COLOR", "value": "Blue", "status": "Active", "source": "Channel", "source_ref": "AMZ"}
	if err := ValidateDocument(tenantID, "ProductAttributeValue", p); err != nil || p["confidence"] != "High" {
		t.Fatalf("the system of record writes and its value is stamped: %v %v", p, err)
	}
	// The bulk design-value writer is held to the same rule.
	_, err := SetDesignAttributeValues(tenantID, "u1", DesignValueRequest{Designs: []string{"ITM-1"}, Attribute: "COLOR", Value: "Green"})
	if !errors.As(err, &ve) || ve.Code != "PIM-9151" {
		t.Fatalf("the design-value writer must be refused with PIM-9151, got %v", err)
	}

	// Latest write wins: any source writes, and is stamped with where it came from.
	stewardChange(t, tenantID, insert, "STC-4", stewardSet("COLOR", "Channel", "AMZ", stewardLatestWins))
	if p, err := save("Import", "Blue"); err != nil || p["source"] != "Import" || p["confidence"] != "High" {
		t.Fatalf("latest-write-wins: %v %v", p, err)
	}
	if _, err := SetDesignAttributeValues(tenantID, "u1", DesignValueRequest{Designs: []string{"ITM-1"}, Attribute: "COLOR", Value: "Green"}); err != nil {
		t.Fatalf("design-value writer under latest-write-wins: %v", err)
	}
	var by, src string
	if err := db.DB.QueryRow("SELECT COALESCE(updated_by,''), COALESCE(data->>'source','') FROM "+schema+".documents WHERE doctype='ProductAttributeValue' AND id='ITM-1::COLOR'").Scan(&by, &src); err != nil || src != "ERP" {
		t.Fatalf("provenance of the stored value: by %q source %q err %v", by, src, err)
	}

	// An entry not yet in force does not hold writes.
	future := stewardSet("COLOR", "Channel", "AMZ", stewardSurvivorWins)
	future["effective_from"] = "2999-01-01"
	stewardChange(t, tenantID, insert, "STC-5", future)
	if _, err := save("Import", "Pink"); err != nil {
		t.Fatalf("a future-dated entry must not hold writes: %v", err)
	}

	// Downstream impact lists what reads the attribute.
	insert("CFM-1", "ChannelFieldMap", "Active", "system", map[string]interface{}{"channel": "AMZ", "source_field": "COLOR", "target_field": "color_name", "mandatory": "Yes"})
	d, err := StewardshipDetail(tenantID, "COLOR")
	if err != nil {
		t.Fatal(err)
	}
	var feed bool
	for _, c := range d.Consumers {
		if c.Kind == "Channel feed" && c.Name == "AMZ" && strings.Contains(c.Detail, "color_name") {
			feed = true
		}
	}
	if !feed || d.ItemsWithValue != 1 {
		t.Fatalf("impact: %+v", d)
	}
}

func channelOrderPNG(shade uint8) []byte {
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for x := 0; x < 4; x++ {
		img.Set(x, 0, color.RGBA{shade, 0, 0, 255})
	}
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}

func TestChannelImageOrderReachesPublishAndFeed(t *testing.T) {
	tenantID, schema, insert := stage140Tenant(t)
	insert("AMZ", "Channel", "Active", "system", map[string]interface{}{"code": "AMZ", "name": "Amazon"})
	insert("EBAY", "Channel", "Active", "system", map[string]interface{}{"code": "EBAY", "name": "eBay"})
	insert("ITM-9", "Item", "Active", "system", map[string]interface{}{"code": "ITM-9", "name": "Shirt"})
	var ids []string
	for i := 0; i < 3; i++ {
		m, err := SaveMediaFile(tenantID, channelOrderPNG(uint8(10+i*40)), "pic"+string(rune('a'+i))+".png", "ITM-9", "Gallery", "u1", "", "")
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, m.ID)
	}
	// The publish door: the payload's images, read back by their file names.
	payloadNames := func(ch string) []string {
		p, err := BuildChannelPayload(tenantID, "ITM-9", ch)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, im := range p.Images {
			out = append(out, im.Filename)
		}
		return out
	}
	idNames := map[string]string{}
	for _, id := range ids {
		_, _, n, _ := ReadMediaFile(tenantID, id)
		idNames[id] = n
	}
	join := func(s []string) string { return strings.Join(s, ",") }
	today := join(payloadNames("AMZ"))
	if len(ids) != 3 || len(payloadNames("AMZ")) != 3 || join(payloadNames("EBAY")) != today {
		t.Fatalf("baseline payload: %s", today)
	}

	// Order the third image first and the first second, on AMZ only.
	if _, err := SetChannelImageOrder(tenantID, "u1", "ITM-9", "AMZ", []string{ids[2], ids[0]}); err != nil {
		t.Fatal(err)
	}
	want := join([]string{idNames[ids[2]], idNames[ids[0]], idNames[ids[1]]})
	if got := join(payloadNames("AMZ")); got != want {
		t.Fatalf("publish order for AMZ: %s want %s", got, want)
	}
	if join(payloadNames("EBAY")) != today {
		t.Fatalf("another channel must keep today's order, got %v", payloadNames("EBAY"))
	}

	// The feed: the file path column carries an address; point it and read it back.
	for _, id := range ids {
		if _, err := db.DB.Exec("UPDATE "+schema+".documents SET data = jsonb_set(data, '{file_path}', to_jsonb('https://img.example/'||id)) WHERE id = $1", id); err != nil {
			t.Fatal(err)
		}
	}
	for key, w := range map[string]string{"image_url": ids[2], "image_url_2": ids[0], "image_url_3": ids[1]} {
		if v, _ := channelExportExtras(tenantID, "AMZ")("ITM-9", key); v != "https://img.example/"+w {
			t.Errorf("feed %s for AMZ = %q want %s", key, v, w)
		}
	}
	if v, _ := channelExportExtras(tenantID, "EBAY")("ITM-9", "image_url"); v != "https://img.example/"+ids[0] {
		t.Errorf("feed for EBAY changed: %q", v)
	}

	// The same image listed twice, a foreign image and an unknown channel are refused.
	var ve *ValidationError
	for label, f := range map[string]func() error{
		"duplicate": func() error {
			_, e := SetChannelImageOrder(tenantID, "u1", "ITM-9", "AMZ", []string{ids[0], ids[0]})
			return e
		},
		"foreign": func() error { _, e := SetChannelImageOrder(tenantID, "u1", "ITM-9", "AMZ", []string{"NOPE"}); return e },
		"channel": func() error { _, e := SetChannelImageOrder(tenantID, "u1", "ITM-9", "NOPE", nil); return e },
	} {
		if err := f(); !errors.As(err, &ve) || ve.Code != "PIM-9152" {
			t.Errorf("%s should be refused with PIM-9152, got %v", label, err)
		}
	}

	// Clearing returns the channel to today's order.
	if _, err := SetChannelImageOrder(tenantID, "u1", "ITM-9", "AMZ", nil); err != nil {
		t.Fatal(err)
	}
	list, _, err := ListMediaForItemChannel(tenantID, "ITM-9", "AMZ")
	if err != nil || len(list) != 3 || list[0].ID != ids[0] || list[1].ID != ids[1] || list[2].ID != ids[2] {
		t.Fatalf("after clearing: %v %v", list, err)
	}
	if v, _ := channelExportExtras(tenantID, "AMZ")("ITM-9", "image_url"); v != "https://img.example/"+ids[0] {
		t.Errorf("feed after clearing: %q", v)
	}
}

// Review of 47.13.4: the register has no side door, a blank or a delete is a
// write, a second request cannot reach the queue by the draft route, and an
// approved change that did not apply says so.
func TestStewardshipRegisterHasNoSideDoor(t *testing.T) {
	tenantID, _, insert := stewardFixture(t)
	stewardChange(t, tenantID, insert, "STC-S1", stewardSet("COLOR", "Channel", "AMZ", stewardSurvivorWins))
	var ve *ValidationError
	if err := ValidateDocument(tenantID, StewardshipDoctype, map[string]interface{}{"code": "x", "attribute": "COLOR", "system_of_record": "ERP"}); !errors.As(err, &ve) || ve.Code != "PIM-9150" {
		t.Fatalf("a direct save of the register must be refused, got %v", err)
	}
	if err := CheckStewardshipDelete(tenantID, StewardshipDoctype, stewardshipID("COLOR"), "u1"); !errors.As(err, &ve) || ve.Code != "PIM-9150" {
		t.Fatalf("a direct delete of the register must be refused, got %v", err)
	}
}

func TestStewardshipBlankAndDeleteAreWrites(t *testing.T) {
	tenantID, _, insert := stewardFixture(t)
	insert(attributeValueID("ITM-1", "COLOR", "", ""), "ProductAttributeValue", "Active", "system", map[string]interface{}{"item": "ITM-1", "attribute": "COLOR", "value": "Red"})
	stewardChange(t, tenantID, insert, "STC-B1", stewardSet("COLOR", "Channel", "AMZ", stewardSurvivorWins))
	var ve *ValidationError
	if err := ValidateProductAttributeValueDocument(tenantID, map[string]interface{}{"item": "ITM-1", "attribute": "COLOR", "value": ""}); !errors.As(err, &ve) || ve.Code != "PIM-9151" {
		t.Fatalf("a blank must not wipe a stewarded value, got %v", err)
	}
	if _, err := SetDesignAttributeValues(tenantID, "u1", DesignValueRequest{Designs: []string{"ITM-1"}, Attribute: "COLOR", Value: ""}); !errors.As(err, &ve) || ve.Code != "PIM-9151" {
		t.Fatalf("the design-value writer must not wipe it either, got %v", err)
	}
	if err := CheckStewardshipDelete(tenantID, "ProductAttributeValue", attributeValueID("ITM-1", "COLOR", "", ""), "u1"); !errors.As(err, &ve) || ve.Code != "PIM-9151" {
		t.Fatalf("a delete must not remove it either, got %v", err)
	}
	if err := ValidateProductAttributeValueDocument(tenantID, map[string]interface{}{"item": "ITM-NEW", "attribute": "COLOR", "value": ""}); err != nil {
		t.Fatalf("a blank where nothing is stored removes nothing: %v", err)
	}
	if err := StewardshipGuard("no-such-tenant-47134", map[string]interface{}{"item": "ITM-1", "attribute": "COLOR", "value": "Blue"}); err == nil {
		t.Fatal("a register that cannot be read must refuse, not wave the write through")
	}
}

// The product sheet writes the same values by its own SQL, so it answers to the
// same register: preview and commit refuse the same cell, a refusal saves
// nothing, and what it does write says it came from an import.
func TestStewardshipProductSheetIsADoorToo(t *testing.T) {
	tenantID, schema, insert := stewardFixture(t)
	insert("SIZE", "ProductAttributeDef", "Active", "system", map[string]interface{}{"code": "SIZE", "label": "Size", "value_type": "Data", "status": "Active"})
	colorID, sizeID := attributeValueID("ITM-1", "COLOR", "", ""), attributeValueID("ITM-1", "SIZE", "", "")
	insert(colorID, "ProductAttributeValue", "Active", "system", map[string]interface{}{"item": "ITM-1", "attribute": "COLOR", "value": "Red"})
	stewardChange(t, tenantID, insert, "STC-S1", stewardSet("COLOR", "Channel", "AMZ", stewardSurvivorWins))

	sheet := func(color string) *strings.Reader {
		return strings.NewReader("id,name,hsn_code,gst_rate,attr:COLOR,attr:SIZE\nITM-1,Shirt,6204,5," + color + ",L\n")
	}
	stored := func(id, key string) string {
		var v sql.NullString
		if err := db.DB.QueryRow("SELECT data->>$2 FROM "+schema+".documents WHERE doctype='ProductAttributeValue' AND id=$1 AND deleted_at IS NULL", id, key).Scan(&v); err != nil && err != sql.ErrNoRows {
			t.Fatal(err)
		}
		return v.String
	}
	for _, commit := range []bool{false, true} {
		for _, color := range []string{"Blue", ImportClearMarker} {
			res, err := ImportProductSheet(context.Background(), tenantID, sheet(color), "u1", "HR/Admin", commit)
			if err != nil {
				t.Fatalf("commit=%v %q: %v", commit, color, err)
			}
			if res.Committed || len(res.Errors) != 1 || res.Errors[0].RowNumber != 2 || !strings.Contains(res.Errors[0].Message, "COLOR") {
				t.Fatalf("commit=%v %q must be refused on row 2 by name, got %+v", commit, color, res)
			}
		}
	}
	if got := stored(colorID, "value"); got != "Red" {
		t.Fatalf("a refused sheet changed the stewarded value to %q", got)
	}
	if got := stored(sizeID, "value"); got != "" {
		t.Fatalf("a refused sheet saved the other attribute on its row: %q", got)
	}

	// Leaving the stored value as it is passes; the unstewarded attribute beside it saves with its source.
	res, err := ImportProductSheet(context.Background(), tenantID, sheet("Red"), "u1", "HR/Admin", true)
	if err != nil || !res.Committed {
		t.Fatalf("an unchanged stewarded value must not block the sheet: %v %+v", err, res)
	}
	if got := stored(sizeID, "value"); got != "L" {
		t.Fatalf("size = %q", got)
	}
	if src, ref := stored(sizeID, "source"), stored(sizeID, "source_ref"); src != "Import" || ref != "u1" {
		t.Fatalf("the sheet must say where the value came from, got %q %q", src, ref)
	}

	// An entry that names Import as the system of record lets the sheet change it, with the entry's confidence.
	stewardChange(t, tenantID, insert, "STC-S2", stewardSet("COLOR", "Import", "", stewardSurvivorWins))
	res, err = ImportProductSheet(context.Background(), tenantID, sheet("Blue"), "u1", "HR/Admin", true)
	if err != nil || !res.Committed {
		t.Fatalf("Import is the system of record here: %v %+v", err, res)
	}
	if got, conf := stored(colorID, "value"), stored(colorID, "confidence"); got != "Blue" || conf != "High" {
		t.Fatalf("value %q confidence %q", got, conf)
	}
}

func TestStewardshipPersonIsTheUserNotTheClaim(t *testing.T) {
	tenantID, _, insert := stewardFixture(t)
	stewardChange(t, tenantID, insert, "STC-P1", stewardSet("COLOR", "Person", "owner1", stewardSurvivorWins))
	var ve *ValidationError
	spoof := map[string]interface{}{"code": "c", "item": "ITM-1", "attribute": "COLOR", "value": "Blue", "status": "Active", "source": "Person", "source_ref": "owner1"}
	StampStewardshipSource("ProductAttributeValue", spoof, "ERP", "someone-else")
	if err := ValidateDocument(tenantID, "ProductAttributeValue", spoof); !errors.As(err, &ve) || ve.Code != "PIM-9151" {
		t.Fatalf("a claimed source must not pass, got %v", err)
	}
	real := map[string]interface{}{"code": "c", "item": "ITM-1", "attribute": "COLOR", "value": "Blue", "status": "Active"}
	StampStewardshipSource("ProductAttributeValue", real, "ERP", "owner1")
	if err := ValidateDocument(tenantID, "ProductAttributeValue", real); err != nil {
		t.Fatalf("the named person writes from any screen: %v", err)
	}
	other := map[string]interface{}{"source": "Channel"}
	StampStewardshipSource("Item", other, "ERP", "u1")
	if other["source"] != "Channel" {
		t.Fatalf("only a value is stamped: %v", other)
	}
}

func TestStewardshipSecondDraftCannotReachTheQueue(t *testing.T) {
	tenantID, _, insert := stewardFixture(t)
	for _, id := range []string{"STC-D1", "STC-D2"} {
		d := stewardSet("COLOR", "ERP", "", stewardLatestWins)
		d["code"], d["status"] = id, "Draft"
		insert(id, StewardshipChangeDoctype, "Draft", "maker1", d)
	}
	if err := SubmitForApproval(tenantID, StewardshipChangeDoctype, "STC-D1", "maker1", "Store Manager"); err != nil {
		t.Fatal(err)
	}
	err := SubmitForApproval(tenantID, StewardshipChangeDoctype, "STC-D2", "maker1", "Store Manager")
	if err == nil || !strings.Contains(err.Error(), "waiting for approval") {
		t.Fatalf("the second draft must be refused at submit, got %v", err)
	}
}

func TestStewardshipFailedApplyIsShownAndCleared(t *testing.T) {
	tenantID, schema, insert := stewardFixture(t)
	bad := stewardSet("COLOR", "ERP", "", stewardLatestWins)
	bad["code"], bad["status"] = "STC-F1", "Draft"
	insert("STC-F1", StewardshipChangeDoctype, "Draft", "maker1", bad)
	if err := SubmitForApproval(tenantID, StewardshipChangeDoctype, "STC-F1", "maker1", "Store Manager"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec("UPDATE " + schema + ".users SET status = 'Inactive' WHERE id = 'owner1'"); err != nil {
		t.Fatal(err)
	}
	if err := DecideApproval(tenantID, StewardshipChangeDoctype, "STC-F1", "admin", "HR/Admin", "HO", "Approved", "ok"); err != nil {
		t.Fatal(err)
	}
	row := func() StewardshipRow {
		t.Helper()
		rows, err := StewardshipOverview(tenantID)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range rows {
			if r.Attribute == "COLOR" {
				return r
			}
		}
		t.Fatal("COLOR not listed")
		return StewardshipRow{}
	}
	if r := row(); r.ApplyError == "" || r.Entry != nil {
		t.Fatalf("a failed apply must show: %+v", r)
	}
	if _, err := db.DB.Exec("UPDATE " + schema + ".users SET status = 'Active' WHERE id = 'owner1'"); err != nil {
		t.Fatal(err)
	}
	stewardChange(t, tenantID, insert, "STC-F2", stewardSet("COLOR", "ERP", "", stewardLatestWins))
	if r := row(); r.ApplyError != "" || r.Entry == nil {
		t.Fatalf("a later good request clears it: %+v", r)
	}
}

// A hand-edited position that is not a number must not blank the image column.
func TestChannelImageOrderSurvivesABadPosition(t *testing.T) {
	tenantID, _, insert := stewardFixture(t)
	insert("MED-BAD", "ProductMedia", "Active", "system", map[string]interface{}{"item": "ITM-1", "file_path": "https://img.example/a.jpg", "media_role": "Main",
		"channel_positions": map[string]interface{}{"AMZ": "first"}})
	if v, _ := channelExportExtras(tenantID, "AMZ")("ITM-1", "image_url"); v != "https://img.example/a.jpg" {
		t.Fatalf("image_url = %q", v)
	}
}

// 47.13.4: two requests for one attribute submitted at the same moment - one
// waits, the other is refused with the "already waiting" error; a request
// for another attribute is not held up by either.
func TestStewardshipConcurrentSubmitsOneWaits(t *testing.T) {
	tenantID, schema, insert := stewardFixture(t)
	insert("SIZE", "ProductAttributeDef", "Active", "system", map[string]interface{}{"code": "SIZE", "label": "Size", "value_type": "Data", "status": "Active"})
	ids := []string{"CHG-RACE-1", "CHG-RACE-2", "CHG-RACE-3", "CHG-RACE-4"}
	for _, id := range ids {
		data := stewardSet("COLOR", "Person", "owner1", "Source")
		data["code"], data["status"] = id, "Draft"
		insert(id, StewardshipChangeDoctype, "Draft", "maker1", data)
	}
	other := stewardSet("SIZE", "Person", "owner1", "Source")
	other["code"], other["status"] = "CHG-RACE-SIZE", "Draft"
	insert("CHG-RACE-SIZE", StewardshipChangeDoctype, "Draft", "maker1", other)

	all := append(append([]string{}, ids...), "CHG-RACE-SIZE")
	errs := make([]error, len(all))
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i, id := range all {
		wg.Add(1)
		go func(i int, id string) {
			defer wg.Done()
			<-start
			errs[i] = SubmitForApproval(tenantID, StewardshipChangeDoctype, id, "maker1", "Store Manager")
		}(i, id)
	}
	close(start)
	wg.Wait()
	won := 0
	for i, id := range ids {
		var ve *ValidationError
		switch {
		case errs[i] == nil:
			won++
		case errors.As(errs[i], &ve) && ve.Code == stewardInvalidCode && strings.Contains(ve.Message, "waiting for approval"):
		default:
			t.Errorf("%s: want the already-waiting refusal, got %v", id, errs[i])
		}
	}
	if won != 1 {
		t.Fatalf("%d of %d requests for one attribute went to approval, want exactly 1", won, len(ids))
	}
	if errs[len(ids)] != nil {
		t.Fatalf("a request for another attribute was held up: %v", errs[len(ids)])
	}
	var waiting int
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM `+schema+`.documents WHERE doctype = $1 AND status = 'Pending Approval' AND data->>'attribute' = 'COLOR'`, StewardshipChangeDoctype).Scan(&waiting); err != nil || waiting != 1 {
		t.Fatalf("COLOR requests waiting: %d (%v), want 1", waiting, err)
	}
}

func init() {
	// Widens the gap between the "is one waiting?" check and the move to
	// Pending Approval, so the race test above is not left to luck.
	RegisterApprovalSubmitCheck(StewardshipChangeDoctype, func(tenantID, docID string, data map[string]interface{}) error {
		if strings.HasPrefix(docID, "CHG-RACE") {
			time.Sleep(300 * time.Millisecond)
		}
		return nil
	})
}
