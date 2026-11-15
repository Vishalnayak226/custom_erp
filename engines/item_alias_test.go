package engines

import (
	"custom_erp/db"
	"encoding/json"
	"errors"
	"testing"
	"unicode/utf8"
)

// Item 201.1: the printed alias, at most 14 characters, made from the name
// with the category shortened first - the user's own three examples first.
func TestItemAliasMade(t *testing.T) {
	short := parseItemAliasShortForms(DefaultItemAliasShortForms)
	for _, c := range []struct{ name, lead, want string }{
		{"Necklace Antique", "Antique", "Necklace Antq"},
		{"Earring CZ", "CZ", "Earring CZ"},
		{"Mangalsutra Zircon", "Zircon", "Mangsutra Zirc"},
		{"Managalsutra Zircon", "Zircon", "Mangsutra Zirc"},
		{"Antique Necklace", "Antique", "Antq Necklace"}, // the other order
		{"  Necklace   Antique ", "Antique", "Necklace Antq"},
		{"Kundan Necklace", "Kundan", "Kundn Necklace"}, // no short form: vowels go, the category's first
		{"Bangle", "", "Bangle"},
		{"", "", ""},
	} {
		if got := MakeItemAlias(c.name, c.lead, short); got != c.want {
			t.Errorf("MakeItemAlias(%q, %q) = %q, want %q", c.name, c.lead, got, c.want)
		}
	}
	// However long the name, the alias fits.
	for _, name := range []string{"Temple Jewellery Necklace Set", "Supercalifragilisticexpialidocious", "Oxidised Silver Choker Necklace"} {
		if got := MakeItemAlias(name, "", short); utf8.RuneCountInString(got) > ItemAliasMaxLen || got == "" {
			t.Errorf("MakeItemAlias(%q) = %q (%d characters)", name, got, utf8.RuneCountInString(got))
		}
	}
	// The short-forms list: pairs by comma, semicolon or line, any case; a
	// phrase or a pair without "=" is ignored.
	got := parseItemAliasShortForms("antique=Antq; Zircon = Zirc\nTwo Words=X, NoPair,=Y")
	if len(got) != 2 || got["antique"] != "Antq" || got["zircon"] != "Zirc" {
		t.Errorf("short forms: %v", got)
	}
}

// Items 201.1/201.2 on a throwaway tenant: a save fills the alias, a typed
// one is kept (14 at most), switching the name order and retyping an item
// name's alias move the items that print it, a typed item alias stays, and a
// record saved before 201.1 prints one made on the fly.
func TestItemAliasSaveAndFollow(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schemaName := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schemaName)
	if _, err := ProvisionTenantSchema(tenantID, schemaName, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	insert := func(id, doctype string, data map[string]interface{}) {
		t.Helper()
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schemaName+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, 'Active', 'system')", id, doctype, raw); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	doc := func(id string) map[string]interface{} {
		t.Helper()
		var raw string
		if err := db.DB.QueryRow("SELECT data::text FROM "+schemaName+".documents WHERE id = $1", id).Scan(&raw); err != nil {
			t.Fatalf("read %s: %v", id, err)
		}
		var d map[string]interface{}
		_ = json.Unmarshal([]byte(raw), &d)
		return d
	}
	alias := func(id string) string { t.Helper(); return strField(doc(id), "item_alias") }
	set := func(key, value string) {
		t.Helper()
		if err := SetSetting(tenantID, key, value, "test"); err != nil {
			t.Fatalf("SetSetting %s: %v", key, err)
		}
	}

	insert("CAT-ANT", "Category", map[string]interface{}{"code": "CAT-ANT", "name": "Antique", "status": "Active"})
	insert("PT-NECK", "ProductType", map[string]interface{}{"code": "PT-NECK", "name": "Necklace", "status": "Active"})
	set(SettingItemNameOrder, ItemNameOrderTypeCategory)

	// A new item name: named from Type + Category, its alias made from that.
	in := map[string]interface{}{"code": "IN-AN", "category": "CAT-ANT", "product_type": "PT-NECK"}
	if err := PrepareItemNameFields(tenantID, "ItemName", in); err != nil {
		t.Fatal(err)
	}
	if err := PrepareItemAlias(tenantID, "ItemName", "", in); err != nil || in["name"] != "Necklace Antique" || in["item_alias"] != "Necklace Antq" {
		t.Fatalf("new item name: %v / %v %v", in["name"], in["item_alias"], err)
	}
	insert("IN-AN", "ItemName", in)
	if name, a, _ := PreviewItemName(tenantID, "CAT-ANT", "PT-NECK"); name != "Necklace Antique" || a != "Necklace Antq" {
		t.Errorf("preview: %q / %q", name, a)
	}

	// Items under it take its alias; a typed one is kept; over 14 is refused.
	it := map[string]interface{}{"code": "IT-1", "name": "Necklace Antique", "item_name": "IN-AN"}
	if err := PrepareItemAlias(tenantID, "Item", "", it); err != nil || it["item_alias"] != "Necklace Antq" {
		t.Fatalf("new item: %v %v", it["item_alias"], err)
	}
	insert("IT-1", "Item", it)
	typed := map[string]interface{}{"code": "IT-2", "name": "Necklace Antique", "item_name": "IN-AN", "item_alias": " My Tag "}
	if err := PrepareItemAlias(tenantID, "Item", "", typed); err != nil || typed["item_alias"] != "My Tag" {
		t.Fatalf("typed alias: %v %v", typed["item_alias"], err)
	}
	insert("IT-2", "Item", typed)
	var verr *ValidationError
	if err := PrepareItemAlias(tenantID, "Item", "", map[string]interface{}{"name": "X", "item_alias": "Fifteen chars!!"}); !errors.As(err, &verr) || verr.Code != "PIM-9363" {
		t.Errorf("15 characters: %v", err)
	}
	// A record saved before 201.1 (no alias), under a design, prints one anyway.
	insert("FAM-1", "ProductFamily", map[string]interface{}{"code": "FAM-1", "name": "Necklace Antique D1", "item_name": "IN-AN"})
	insert("IT-3", "Item", map[string]interface{}{"code": "IT-3", "name": "Necklace Antique", "family": "FAM-1"})
	if got := ItemAliasForItem(schemaName, doc("IT-3")); got != "Necklace Antq" {
		t.Errorf("sticker alias of an old item: %q", got)
	}
	// 201.3: its sticker prints the alias; the full name stays as full_name.
	if label, err := resolveStickerLabel(tenantID, schemaName, "IT-3", "", "", "", "", "", ""); err != nil || label.Name != "Necklace Antq" || label.Fields["full_name"] != "Necklace Antique" {
		t.Errorf("sticker of an old item: %q / %q %v", label.Name, label.Fields["full_name"], err)
	}

	// Switching the order renames the master data and its alias follows.
	set(SettingItemNameOrder, ItemNameOrderCategoryType)
	if doc("IN-AN")["name"] != "Antique Necklace" || alias("IN-AN") != "Antq Necklace" || alias("IT-1") != "Antq Necklace" || alias("IT-2") != "My Tag" {
		t.Errorf("order switch: %v / %q / %q / %q", doc("IN-AN")["name"], alias("IN-AN"), alias("IT-1"), alias("IT-2"))
	}
	if got := ItemAliasForItem(schemaName, doc("IT-3")); got != "Antq Necklace" {
		t.Errorf("old item after the switch: %q", got)
	}

	// An item name's alias typed over: kept, and the items that printed the
	// made one print it; the typed item alias stays.
	prior := doc("IN-AN")
	saved := doc("IN-AN")
	saved["item_alias"] = "Antique Neck"
	if err := PrepareItemAlias(tenantID, "ItemName", "IN-AN", saved); err != nil || saved["item_alias"] != "Antique Neck" {
		t.Fatalf("typed item name alias: %v %v", saved["item_alias"], err)
	}
	raw, _ := json.Marshal(saved)
	if _, err := db.DB.Exec("UPDATE "+schemaName+".documents SET data = $2 WHERE id = $1", "IN-AN", raw); err != nil {
		t.Fatal(err)
	}
	FollowItemNameAlias(tenantID, "IN-AN", prior, saved)
	if alias("IT-1") != "Antique Neck" || alias("IT-2") != "My Tag" || ItemAliasForItem(schemaName, doc("IT-3")) != "Antique Neck" {
		t.Errorf("follow a typed item name alias: %q / %q / %q", alias("IT-1"), alias("IT-2"), ItemAliasForItem(schemaName, doc("IT-3")))
	}
	// A typed alias does not follow the next order switch.
	set(SettingItemNameOrder, ItemNameOrderTypeCategory)
	if alias("IN-AN") != "Antique Neck" || alias("IT-1") != "Antique Neck" {
		t.Errorf("typed alias after a switch: %q / %q", alias("IN-AN"), alias("IT-1"))
	}

	// An edit that sends back the made alias, now off its item name and
	// renamed, gets one made from the new name.
	edit := doc("IT-1")
	edit["item_name"], edit["name"] = "", "Choker Antique Set"
	if err := PrepareItemAlias(tenantID, "Item", "IT-1", edit); err != nil || edit["item_alias"] != "Chokr Antq Set" {
		t.Errorf("edit with the made alias sent back: %v %v", edit["item_alias"], err)
	}
}
