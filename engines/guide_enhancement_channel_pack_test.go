package engines

import (
	"bytes"
	"context"
	"custom_erp/db"
	"strings"
	"testing"
	"time"
)

// Stage 140.12, second half: PIM (.22-.25), CRM (.26, .27), warehouse setup (.21).

// 140.12.22 / .23 / .24: a Shopify pack on a channel makes its field maps,
// the attributes it lacks and a feed in Shopify's own columns; the feed
// takes one field in two columns, filters by category, gives XLSX, and an
// item the channel cannot take is skipped and listed instead of stopping it.
func TestStage140ChannelPackAndFeed(t *testing.T) {
	tenantID, schema, insert := stage140Tenant(t)
	insert("SHOP-140", "Channel", "Active", "system", map[string]interface{}{"code": "SHOP-140", "name": "Shop", "platform": "Shopify", "enabled": "No", "default_locale": "en"})
	insert("TEE-A", "Item", "Active", "system", map[string]interface{}{"code": "TEE-A", "name": "Tee A", "category": "Tops", "sale_price": 499.0, "mrp": 599.0})
	insert("CAP-B", "Item", "Active", "system", map[string]interface{}{"code": "CAP-B", "name": "Cap B", "category": "Caps", "sale_price": 199.0})
	res, err := ApplyChannelPack(tenantID, "SHOP-140", "merch1")
	if err != nil {
		t.Fatalf("apply pack: %v", err)
	}
	if res.FieldMaps == 0 || len(res.AttributesCreated) == 0 || res.Template == "" {
		t.Fatalf("pack = %+v; want field maps, new attributes (brand, size) and a template", res)
	}
	again, err := ApplyChannelPack(tenantID, "SHOP-140", "merch1")
	if err != nil || again.FieldMaps != 0 || len(again.AttributesCreated) != 0 {
		t.Fatalf("a second apply changed things: %+v (%v)", again, err)
	}
	// The stored template passes the template rules (one field, two columns).
	var raw string
	_ = db.DB.QueryRow("SELECT data::text FROM "+schema+".documents WHERE id = $1", res.Template).Scan(&raw)
	if !strings.Contains(raw, "Handle") || !strings.Contains(raw, "Variant SKU") {
		t.Fatalf("template columns = %s", raw)
	}
	out, err := RunPIMExport(tenantID, res.Template, PIMExportFilter{Category: "Tops"})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(out.Data)), "\n")
	if !strings.HasPrefix(lines[0], "Handle,Title,Body (HTML)") || len(lines) != 2 || !strings.HasPrefix(lines[1], "TEE-A,") || !strings.Contains(lines[1], "TEE-A") {
		t.Fatalf("Tops feed = %q", out.Data)
	}
	// A transform rule that cannot apply makes CAP-B one the channel cannot take.
	insert("BAD-MAP", "ChannelFieldMap", "Active", "system", map[string]interface{}{"code": "BAD-MAP", "channel": "SHOP-140", "source_field": "sale_price", "target_field": "bad_col", "transform_rule": "NO-SUCH-RULE"})
	out, err = RunPIMExport(tenantID, res.Template, PIMExportFilter{Format: "xlsx"})
	if err != nil {
		t.Fatalf("export with a bad map: %v", err)
	}
	if len(out.Skipped) != 2 || out.Extension != "xlsx" || !bytes.HasPrefix(out.Data, []byte("PK")) {
		t.Fatalf("xlsx feed: %d skipped, ext %s; want both items listed as skipped (bad rule) and a sheet", len(out.Skipped), out.Extension)
	}
	if err := ValidatePIMExportTemplateDocument(tenantID, map[string]interface{}{"name": "x", "channel": "SHOP-140",
		"column_mappings": `[{"field_key":"item_code","column_header":"Handle"},{"field_key":"item_code","column_header":"Variant SKU 2"}]`}); err != nil {
		t.Fatalf("one field in two columns refused: %v", err)
	}
}

// 140.12.25: the Merchandiser template sets up PIM and fills items, nothing else.
func TestStage140MerchandiserTemplate(t *testing.T) {
	var m *RoleTemplate
	for i := range roleTemplates {
		if roleTemplates[i].Name == "Merchandiser" {
			m = &roleTemplates[i]
		}
	}
	if m == nil {
		t.Fatal("no Merchandiser role template")
	}
	_, finance := m.Modules["Finance"]
	if m.Modules["PIM"] != AccessManage || m.Doctypes["Item"] != AccessOperate || finance || len(m.Capabilities) != 0 {
		t.Fatalf("Merchandiser = %+v", *m)
	}
}

// 140.12.26: the readiness check names what is missing; presets fill the SMS
// gateway sender; a mock sender's test connection works without a call.
func TestStage140CRMReadiness(t *testing.T) {
	tenantID, _, _ := stage140Tenant(t)
	r, err := CRMChannelReadiness(tenantID, CRMChannelWhatsApp)
	if err != nil {
		t.Fatal(err)
	}
	if r.Ready || len(r.Checks) != 3 || r.Checks[0].OK {
		t.Fatalf("readiness with nothing set up = %+v", r)
	}
	presets := CRMSenderPresets()
	if len(presets) != 3 || presets[0].Provider != CRMProviderSMS {
		t.Fatalf("presets = %+v", presets)
	}
	s, err := SaveCRMSender(tenantID, "", CRMSenderInput{Channel: CRMChannelSMS, Provider: CRMProviderMock, Name: "Mock", Status: "Active", Config: map[string]interface{}{}}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	test, err := TestCRMSender(context.Background(), tenantID, s.ID, "")
	if err != nil || !test.OK {
		t.Fatalf("mock test connection = %+v (%v)", test, err)
	}
}

// 140.12.27: the customer pulse, affinity and suggested segments read the
// till's bills of known customers.
func TestStage140CRMInsights(t *testing.T) {
	tenantID, _, insert := stage140Tenant(t)
	insert("CU-1", "Customer", "Active", "system", map[string]interface{}{"code": "CU-1", "name": "Asha"})
	insert("TEE-A", "Item", "Active", "system", map[string]interface{}{"code": "TEE-A", "name": "Tee A", "category": "Tops"})
	insert("TEE-B", "Item", "Active", "system", map[string]interface{}{"code": "TEE-B", "name": "Tee B", "category": "Tops"})
	insert("CU-2", "Customer", "Active", "system", map[string]interface{}{"code": "CU-2", "name": "Ravi"})
	insert("CART-1", "POSCart", "Paid", "system", map[string]interface{}{"customer_id": "CU-1", "amount_paid": 500.0, "items": []interface{}{map[string]interface{}{"sku": "TEE-A", "qty": 1, "sale_price": 500.0}}})
	insert("CART-2", "POSCart", "Paid", "system", map[string]interface{}{"customer_id": "CU-2", "amount_paid": 900.0, "items": []interface{}{map[string]interface{}{"sku": "TEE-B", "qty": 3, "sale_price": 300.0}}})
	pulse, err := crmPulse(tenantID, time.Now())
	if err != nil || pulse[0]["value"] != 2 {
		t.Fatalf("pulse = %+v (%v); want 2 active customers", pulse, err)
	}
	aff, err := crmAffinity(tenantID, time.Now())
	if err != nil || len(aff) != 2 {
		t.Fatalf("affinity = %+v (%v)", aff, err)
	}
	for _, row := range aff {
		if row["customer_id"] == "CU-1" && (row["top_category"] != "Tops" || row["next_offer"] != "TEE-B") {
			t.Fatalf("CU-1 affinity = %+v; want Tops and an offer of TEE-B (the best seller not yet bought)", row)
		}
	}
	segs, err := crmSuggestedSegments(tenantID, time.Now())
	if err != nil || len(segs) < 4 {
		t.Fatalf("suggested segments = %+v (%v)", segs, err)
	}
	found := false
	for _, d := range DefaultDashboardTiles() {
		found = found || d.ReportID == "crm-pulse"
	}
	if !found {
		t.Fatal("the main dashboard has no customer tile")
	}
}

// 140.12.21: a bin's new zone is made with it.
func TestStage140BinMakesItsZone(t *testing.T) {
	tenantID, schema, _ := stage140Tenant(t)
	payload := map[string]interface{}{"bin_code": "B-140", "location": "WH140", "zone": "ZN-140"}
	EnsureBinZone(tenantID, "wh1", payload)
	EnsureBinZone(tenantID, "wh1", payload)
	var n int
	_ = db.DB.QueryRow("SELECT COUNT(*) FROM " + schema + ".documents WHERE doctype = 'Zone' AND data->>'code' = 'ZN-140' AND status = 'Active'").Scan(&n)
	if n != 1 {
		t.Fatalf("zones made = %d, want 1", n)
	}
	if err := ValidateMasterDataRules(tenantID, "", "Bin", payload); err != nil {
		t.Fatalf("the bin is still refused: %v", err)
	}
}
