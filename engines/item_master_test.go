package engines

import (
	"custom_erp/db"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// Stage 89 - the Item master asked once: org defaults and the item name fill,
// the name order (renaming both ways), where each field is asked (GRN-asked
// values), attributes mapped to the type, a free-text field made a list, and
// the admin's treatment correction after stock moves. Own throwaway tenant.
func TestStage89ItemMasterAskedOnce(t *testing.T) {
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
	code := func(err error) string {
		var verr *ValidationError
		if errors.As(err, &verr) {
			return verr.Code
		}
		return ""
	}
	set := func(key, value string) {
		t.Helper()
		if err := SetSetting(tenantID, key, value, "test"); err != nil {
			t.Fatalf("SetSetting %s: %v", key, err)
		}
	}

	// --- 89.2 the item name fills the item; 89.1 org defaults ------------------
	insert("CAT-ANT", "Category", map[string]interface{}{"code": "CAT-ANT", "name": "Antique", "status": "Active"})
	insert("PT-NECK", "ProductType", map[string]interface{}{"code": "PT-NECK", "name": "Necklace", "status": "Active"})
	insert("PT-RING", "ProductType", map[string]interface{}{"code": "PT-RING", "name": "Ring", "status": "Active"})
	nameIn := map[string]interface{}{"category": "CAT-ANT", "product_type": "PT-NECK", "hsn_code": "7117", "gst_rate": 3}
	if err := PrepareItemNameFields(tenantID, "ItemName", nameIn); err != nil || nameIn["name"] != "Antique Necklace" {
		t.Fatalf("item name in Category + Type order: %v %v", nameIn["name"], err)
	}
	nameIn["code"] = "IN-AN"
	insert("IN-AN", "ItemName", nameIn)
	insert("IN-HAND", "ItemName", map[string]interface{}{"code": "IN-HAND", "name": "Temple Set", "category": "CAT-ANT", "product_type": "PT-RING", "hsn_code": "7117", "gst_rate": 3})

	set(SettingDefaultTrackingMode, "Batch")
	set(SettingDefaultShelfLifeDays, "30")
	item := map[string]interface{}{"item_name": "IN-AN"}
	if err := PrepareItemDefaults(tenantID, true, item); err != nil {
		t.Fatalf("PrepareItemDefaults: %v", err)
	}
	for k, want := range map[string]interface{}{"category": "Antique", "product_type": "Necklace", "hsn_code": "7117", "name": "Antique Necklace",
		"tax_treatment": "Taxable", "tracking_mode": "Batch", "shelf_life_days": 30} {
		if item[k] != want {
			t.Errorf("new item %s = %v, want %v", k, item[k], want)
		}
	}
	if g, _ := parityNumber(item["gst_rate"]); g != 3 {
		t.Errorf("gst from the item name: %v", item["gst_rate"])
	}
	// An edit takes nothing from the org defaults; a category given by code reads as its name.
	edit := map[string]interface{}{"category": "CAT-ANT"}
	_ = PrepareItemDefaults(tenantID, false, edit)
	if edit["category"] != "Antique" || edit["tracking_mode"] != nil || edit["tax_treatment"] != nil {
		t.Errorf("edit: %+v", edit)
	}
	set(SettingDefaultTrackingMode, "None")
	set(SettingDefaultShelfLifeDays, "0")

	// --- 89.2.3 name order, switched and back (D2) ------------------------------
	insert("FAM-1", "ProductFamily", map[string]interface{}{"code": "FAM-1", "name": "Antique Necklace D101", "item_name": "IN-AN"})
	insert("IT-1", "Item", map[string]interface{}{"code": "IT-1", "name": "Antique Necklace D101 Red", "family": "FAM-1"})
	insert("IT-2", "Item", map[string]interface{}{"code": "IT-2", "name": "Antique Necklace", "item_name": "IN-AN"})
	insert("IT-3", "Item", map[string]interface{}{"code": "IT-3", "name": "Antique Necklaces Lot", "item_name": "IN-AN"})
	set(SettingItemNameOrder, ItemNameOrderTypeCategory)
	if doc("IN-AN")["name"] != "Necklace Antique" || doc("FAM-1")["name"] != "Necklace Antique D101" ||
		doc("IT-1")["name"] != "Necklace Antique D101 Red" || doc("IT-2")["name"] != "Necklace Antique" {
		t.Errorf("switch to Type + Category: %v / %v / %v / %v", doc("IN-AN")["name"], doc("FAM-1")["name"], doc("IT-1")["name"], doc("IT-2")["name"])
	}
	if doc("IT-3")["name"] != "Antique Necklaces Lot" || doc("IN-HAND")["name"] != "Temple Set" {
		t.Errorf("a name that is not the composed one must stay: %v / %v", doc("IT-3")["name"], doc("IN-HAND")["name"])
	}
	fresh := map[string]interface{}{"category": "CAT-ANT", "product_type": "PT-RING"}
	_ = PrepareItemNameFields(tenantID, "ItemName", fresh)
	if fresh["name"] != "Ring Antique" {
		t.Errorf("new item name in the new order: %v", fresh["name"])
	}
	set(SettingItemNameOrder, ItemNameOrderCategoryType)
	if doc("IN-AN")["name"] != "Antique Necklace" || doc("IT-1")["name"] != "Antique Necklace D101 Red" {
		t.Errorf("switch back: %v / %v", doc("IN-AN")["name"], doc("IT-1")["name"])
	}

	// --- 89.4 where each field is asked; GRN-asked values -----------------------
	insert("IA-COLOR", "ItemAttribute", map[string]interface{}{"code": "COLOR", "label": "Colour", "value_type": "Select", "value_options": "Red,Blue,Gold", "status": "Active"})
	insert("IA-SIZE", "ItemAttribute", map[string]interface{}{"code": "SIZE", "label": "Size", "value_type": "Select", "value_options": "S,M", "status": "Active"})
	rules, err := ItemFieldRules(tenantID)
	if err != nil {
		t.Fatalf("ItemFieldRules: %v", err)
	}
	byKey := map[string]ItemFieldRule{}
	for _, r := range rules {
		byKey[r.Key] = r
	}
	if !byKey["code"].Fixed || !byKey["attr:COLOR"].PO || !byKey["weight"].Item || byKey["weight"].GRN != "" {
		t.Errorf("defaults: code %+v colour %+v weight %+v", byKey["code"], byKey["attr:COLOR"], byKey["weight"])
	}
	if err := SaveItemFieldRules(tenantID, "test", []ItemFieldRule{
		{Key: "weight", Item: false, GRN: GRNAskRequired},
		{Key: "attr:SIZE", Item: true, PO: false, GRN: GRNAskOptional},
		{Key: "code", Hidden: true}, // fixed - ignored
	}); err != nil {
		t.Fatalf("SaveItemFieldRules: %v", err)
	}
	if c := code(SaveItemFieldRules(tenantID, "test", []ItemFieldRule{{Key: "weight", GRN: "sometimes"}})); c != "MASTER-0890" {
		t.Errorf("bad GRN level: %q", c)
	}
	asked, _ := GRNAskedFields(tenantID)
	if len(asked) != 2 || asked[0].Key != "weight" || !asked[0].Required || asked[1].Key != "attr:SIZE" || asked[1].Required {
		t.Errorf("GRN-asked fields: %+v", asked)
	}
	grn := map[string]interface{}{"received_items": `[{"sku":"IT-2","qty":1,"accepted_qty":1}]`}
	if c := code(PrepareGRNItemValues(tenantID, grn)); c != "MASTER-0891" {
		t.Errorf("required weight missing: %q", c)
	}
	grn["received_items"] = `[{"sku":"IT-2","qty":1,"accepted_qty":1,"item_values":{"weight":12.5,"attr:SIZE":"M","not_asked":"x"}}]`
	if err := PrepareGRNItemValues(tenantID, grn); err != nil {
		t.Fatalf("GRN with weight: %v", err)
	}
	if s := grn["received_items"].(string); !strings.Contains(s, `"weight":"12.5"`) || strings.Contains(s, "not_asked") {
		t.Errorf("kept values: %s", s)
	}
	if v := grnLineItemValues(map[string]interface{}{"item_values": map[string]interface{}{"weight": 12.5}}); v["weight"] != "12.5" {
		t.Errorf("line values for the batch: %v", v)
	}
	// D3: the batch keeps them, and its sticker prints them over the item's.
	if _, err := EnsureBatch(tenantID, BatchInfo{BatchNo: "B89", Item: "IT-2", Attributes: map[string]string{"weight": "12.5"}}, 1, "system"); err != nil {
		t.Fatalf("EnsureBatch: %v", err)
	}
	if v := batchItemValues(schemaName, "IT-2", "B89"); v["weight"] != "12.5" {
		t.Errorf("batch values: %v", v)
	}
	label, err := resolveAndLogSticker(tenantID, schemaName, "IT-2", 1, "", "system", "", "GRN", "GRN-89", "B89", "", "", "", 0)
	if err != nil || label.Fields["weight"] != "12.5" {
		t.Errorf("sticker for batch B89 prints its weight: %v / %v", label.Fields["weight"], err)
	}

	// --- 89.3.1 a free-text field made a list ---------------------------------
	insert("IT-4", "Item", map[string]interface{}{"code": "IT-4", "name": "x", "country_of_origin": "India"})
	insert("IT-5", "Item", map[string]interface{}{"code": "IT-5", "name": "y", "country_of_origin": "Italy"})
	r, err := CreateAttributeListFromField(tenantID, "system", "test", "country_of_origin")
	if err != nil || r.List == "" {
		t.Fatalf("make a list: %+v %v", r, err)
	}
	if opts := doc(itemAttributeID(r.List))["value_options"]; opts != "India,Italy" {
		t.Errorf("list from existing values: %v", opts)
	}
	if c := code(func() error {
		_, e := CreateAttributeListFromField(tenantID, "system", "test", "country_of_origin")
		return e
	}()); c != "MASTER-0890" {
		t.Errorf("a field already a list: %q", c)
	}

	// --- 89.5 attributes mapped to the type -----------------------------------
	attrSet, _ := ItemAttributesFor(tenantID, "IN-AN")
	if attrSet.Source != "all" {
		t.Errorf("mapping off: %+v", attrSet)
	}
	if _, err := db.DB.Exec("UPDATE " + schemaName + ".documents SET data = data || '{\"attribute_map\":\"COLOR:Red|Gold\"}' WHERE id = 'PT-NECK'"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec("UPDATE " + schemaName + ".documents SET data = data || '{\"attribute_map\":\"SIZE\"}' WHERE id = 'CAT-ANT'"); err != nil {
		t.Fatal(err)
	}
	SetSetting(tenantID, SettingAttributeTypeMapping, "true", "test")
	attrSet, _ = ItemAttributesFor(tenantID, "IN-AN")
	if attrSet.Source != "type" || len(attrSet.Attributes) != 1 || attrSet.Attributes[0].Code != "COLOR" || strings.Join(attrSet.Attributes[0].Values, ",") != "Red,Gold" {
		t.Errorf("Necklace offers its colours only: %+v", attrSet)
	}
	ring, _ := ItemAttributesFor(tenantID, "IN-HAND")
	if ring.Source != "category" || len(ring.Attributes) != 1 || ring.Attributes[0].Code != "SIZE" {
		t.Errorf("Ring (no map) falls back to its category: %+v", ring)
	}
	m, err := GetDesignMatrixFor(tenantID, "IN-HAND")
	if err != nil || len(m.Attributes) != 0 {
		t.Errorf("SIZE is not asked in the PO, so the ring's PO matrix is empty: %+v %v", m.Attributes, err)
	}
	if err := MapAttributeValueToItemName(tenantID, "system", "IN-AN", "COLOR", "Rose"); err != nil || doc("PT-NECK")["attribute_map"] != "COLOR:Red|Gold|Rose" {
		t.Errorf("a new value joins the type's map: %v %v", doc("PT-NECK")["attribute_map"], err)
	}
	SetSetting(tenantID, SettingAttributeTypeMapping, "false", "test")

	// --- 89.8 treatment correction after stock moves -----------------------------
	const loc = "STORE-89"
	insert(loc, "Location", map[string]interface{}{"code": loc, "name": "Store 89", "type": "Store", "sellable": "Yes"})
	stock := func(sku string, qty int, unitPaise int64) {
		t.Helper()
		if _, err := PostInventoryLedger(tenantID, loc, []interface{}{map[string]interface{}{"sku": sku, "qty": float64(qty)}}, false); err != nil {
			t.Fatalf("stock %s: %v", sku, err)
		}
		if unitPaise > 0 {
			if err := RecordStockReceiptCost(tenantID, sku, float64(qty), unitPaise); err != nil {
				t.Fatalf("cost %s: %v", sku, err)
			}
		}
	}
	onHand := func(sku string) int {
		var v int
		_ = db.DB.QueryRow("SELECT on_hand FROM "+schemaName+".inventory_availability WHERE sku = $1 AND location_code = $2", sku, loc).Scan(&v)
		return v
	}
	glNet := func(docID, acct string) int64 {
		var v int64
		_ = db.DB.QueryRow("SELECT COALESCE(SUM(debit) - SUM(credit), 0) FROM "+schemaName+".gl_postings WHERE document_id = $1 AND account_code = $2", docID, acct).Scan(&v)
		return v
	}
	insert("BOX89", "Item", map[string]interface{}{"code": "BOX89", "name": "Gift box", "item_type": "Product", "hsn_code": "4819", "tax_treatment": "Exempt"})
	stock("BOX89", 10, 10000) // 10 at Rs 100

	// a. Product (in Inventory) -> Consumable - Periodic (expensed): value out of 1200.
	ch := TreatmentChange{Target: "Item", Ref: "BOX89", ItemType: "Consumable - Periodic"}
	pv, err := PreviewTreatmentCorrection(tenantID, ch)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if pv.NoCorrectionNeeded || pv.TotalValue != 1000 || len(pv.Items) != 1 || pv.Items[0].OnHand != 10 || len(pv.Blockers) != 0 || pv.Items[0].WriteOff {
		t.Fatalf("preview a: %+v", pv)
	}
	if len(pv.Journal) != 2 || pv.Journal[0].Account != "1200" || pv.Journal[0].Credit != 1000 || pv.Journal[1].Debit != 1000 {
		t.Errorf("journal a: %+v", pv.Journal)
	}
	req := TreatmentApplyRequest{TreatmentChange: ch, Reason: "set up as a product by mistake", Confirm: "box89", Token: "stale"}
	if c := code(func() error {
		_, e := ApplyTreatmentCorrection(tenantID, "system", "admin", RoleSuperAdmin, req)
		return e
	}()); c != "MASTER-0895" {
		t.Errorf("stale token: %q", c)
	}
	req.Token, req.Confirm = pv.Token, "BOX8"
	if c := code(func() error {
		_, e := ApplyTreatmentCorrection(tenantID, "system", "admin", RoleSuperAdmin, req)
		return e
	}()); c != "MASTER-0893" {
		t.Errorf("code not typed right: %q", c)
	}
	req.Confirm = "box89"
	res, err := ApplyTreatmentCorrection(tenantID, "system", "admin", RoleSuperAdmin, req)
	if err != nil || res.Status != "Applied" {
		t.Fatalf("apply a: %+v %v", res, err)
	}
	if doc("BOX89")["item_type"] != "Consumable - Periodic" || onHand("BOX89") != 10 || glNet(res.ID, "1200") != -100000 || glNet(res.ID, pv.Journal[1].Account) != 100000 {
		t.Errorf("applied a: type %v on hand %d 1200 %d", doc("BOX89")["item_type"], onHand("BOX89"), glNet(res.ID, "1200"))
	}
	if unit, _, _ := GetItemUnitCost(tenantID, "BOX89"); unit != 0 {
		t.Errorf("an expensed item carries no cost: %d", unit)
	}
	if rec := doc(res.ID); rec["reason"] != "set up as a product by mistake" || rec["applied"] == nil {
		t.Errorf("the correction record: %+v", rec)
	}
	if !ItemHasStockHistory(tenantID, "BOX89") {
		t.Errorf("BOX89 has stock history")
	}

	// b. back into Inventory: no cost to propose, so a value per unit is asked.
	ch = TreatmentChange{Target: "Item", Ref: "BOX89", ItemType: "Product"}
	pv, _ = PreviewTreatmentCorrection(tenantID, ch)
	if !pv.NeedsUnitValue || len(pv.Blockers) == 0 {
		t.Errorf("value per unit asked: %+v", pv)
	}
	v := 120.0
	ch.UnitValue = &v
	pv, _ = PreviewTreatmentCorrection(tenantID, ch)
	if pv.NeedsUnitValue || pv.TotalValue != 1200 || len(pv.Blockers) != 0 {
		t.Fatalf("preview b: %+v", pv)
	}
	res, err = ApplyTreatmentCorrection(tenantID, "system", "admin", RoleSuperAdmin, TreatmentApplyRequest{TreatmentChange: ch, Reason: "back", Confirm: "BOX89", Token: pv.Token})
	if err != nil || glNet(res.ID, "1200") != 120000 {
		t.Fatalf("apply b: %v / 1200 %d", err, glNet(res.ID, "1200"))
	}
	if unit, _, _ := GetItemUnitCost(tenantID, "BOX89"); unit != 12000 {
		t.Errorf("moving cost set to the value entered: %d", unit)
	}

	// c. to Service (no stock): the 10 leave the store and their value is expensed.
	ch = TreatmentChange{Target: "Item", Ref: "BOX89", ItemType: "Service"}
	pv, _ = PreviewTreatmentCorrection(tenantID, ch)
	if !pv.Items[0].WriteOff || pv.TotalValue != 1200 || len(pv.Blockers) != 0 {
		t.Fatalf("preview c: %+v", pv)
	}
	if _, err := ApplyTreatmentCorrection(tenantID, "system", "admin", RoleSuperAdmin, TreatmentApplyRequest{TreatmentChange: ch, Reason: "it is a service", Confirm: "BOX89", Token: pv.Token}); err != nil {
		t.Fatalf("apply c: %v", err)
	}
	if onHand("BOX89") != 0 {
		t.Errorf("written off: %d left", onHand("BOX89"))
	}

	// d. reserved stock stops a write-off.
	insert("TAG89", "Item", map[string]interface{}{"code": "TAG89", "name": "Tag", "item_type": "Product", "hsn_code": "4821", "tax_treatment": "Exempt"})
	stock("TAG89", 5, 1000)
	if _, err := db.DB.Exec("UPDATE " + schemaName + ".inventory_availability SET hold_qty = 2 WHERE sku = 'TAG89'"); err != nil {
		t.Fatal(err)
	}
	pv, _ = PreviewTreatmentCorrection(tenantID, TreatmentChange{Target: "Item", Ref: "TAG89", ItemType: "Service"})
	if len(pv.Blockers) == 0 || !strings.Contains(pv.Blockers[0], "on hold") {
		t.Errorf("held stock blocks: %+v", pv.Blockers)
	}
	// e. a gift card never becomes an ordinary item.
	pv, _ = PreviewTreatmentCorrection(tenantID, TreatmentChange{Target: "Item", Ref: "TAG89", ItemType: "Gift Card"})
	if len(pv.Blockers) == 0 {
		t.Errorf("gift card blocked")
	}
	// No treatment change: nothing to correct.
	pv, _ = PreviewTreatmentCorrection(tenantID, TreatmentChange{Target: "Item", Ref: "TAG89", ItemType: "Accessory"})
	if !pv.NoCorrectionNeeded {
		t.Errorf("Product -> Accessory keeps the treatment: %+v", pv)
	}

	// f. an Item Type: its items with stock are corrected together.
	insert("PIN89", "Item", map[string]interface{}{"code": "PIN89", "name": "Pin", "item_type": "Accessory", "hsn_code": "7117", "tax_treatment": "Exempt"})
	stock("PIN89", 4, 5000)
	tch := TreatmentChange{Target: "ItemType", Ref: "Accessory", Valuation: "Non-valuated", Accounting: "Expense"}
	pv, err = PreviewTreatmentCorrection(tenantID, tch)
	if err != nil || len(pv.Items) != 1 || pv.Items[0].Code != "PIN89" || pv.TotalValue != 200 || len(pv.Blockers) != 0 {
		t.Fatalf("item type preview: %+v %v", pv, err)
	}
	if _, err := ApplyTreatmentCorrection(tenantID, "system", "admin", RoleSuperAdmin, TreatmentApplyRequest{TreatmentChange: tch, Reason: "accessories are expensed", Confirm: "accessory", Token: pv.Token}); err != nil {
		t.Fatalf("item type apply: %v", err)
	}
	InvalidateItemTypeCache(tenantID)
	if p, _, _ := ItemPolicyForSKU(tenantID, "PIN89"); p.Valuated() {
		t.Errorf("Accessory items are no longer valuated: %+v", p)
	}

	// g. a second admin approves (maker-checker).
	set(SettingTreatmentSecondAdmin, "true")
	stock("TAG89", 0, 0)
	ch = TreatmentChange{Target: "Item", Ref: "TAG89", ItemType: "Consumable - Periodic"}
	pv, _ = PreviewTreatmentCorrection(tenantID, ch)
	res, err = ApplyTreatmentCorrection(tenantID, "system", "admin", RoleSuperAdmin, TreatmentApplyRequest{TreatmentChange: ch, Reason: "tags are consumables", Confirm: "TAG89", Token: pv.Token})
	if err != nil || res.Status != "Pending Approval" || doc("TAG89")["item_type"] != "Product" {
		t.Fatalf("second admin: %+v %v / %v", res, err, doc("TAG89")["item_type"])
	}
	if err := DecideApproval(tenantID, TreatmentCorrectionDoctype, res.ID, "system", RoleSuperAdmin, "", "Approved", ""); err == nil {
		t.Errorf("the admin who asked cannot approve")
	}
	if err := DecideApproval(tenantID, TreatmentCorrectionDoctype, res.ID, "second-admin", RoleSuperAdmin, "", "Approved", "ok"); err != nil {
		t.Fatalf("second admin approves: %v", err)
	}
	if doc("TAG89")["item_type"] != "Consumable - Periodic" || glNet(res.ID, "1200") != -5000 {
		t.Errorf("applied on approval: %v / 1200 %d", doc("TAG89")["item_type"], glNet(res.ID, "1200"))
	}
}
