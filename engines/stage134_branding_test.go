package engines

import (
	"bytes"
	"custom_erp/db"
	"image"
	"image/png"
	"testing"
)

// TestStage134Branding (120.6): branding shows only when the operator has
// switched it on; the accent must be a hex colour dark enough for white text;
// the logo is a raster picture of at most 300 KB, never SVG.
func TestStage134Branding(t *testing.T) {
	for v, ok := range map[string]bool{"": true, "#0f766e": true, "#1E3A8A": true, "#ffd700": false, "#fff": false, "red": false, "#12345g": false} {
		if err := validateBrandAccent(v); (err == nil) != ok {
			t.Errorf("validateBrandAccent(%q) = %v, want ok %v", v, err, ok)
		}
	}
	if c := contrastWithWhite("#000000"); c < 20.9 || c > 21.1 {
		t.Errorf("black on white contrast = %.2f, want 21", c)
	}

	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	var logo bytes.Buffer
	_ = png.Encode(&logo, image.NewRGBA(image.Rect(0, 0, 120, 40)))
	if err := SaveBrandLogo(tenantID, []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)); err == nil {
		t.Fatal("an SVG logo is refused")
	}
	if err := SaveBrandLogo(tenantID, make([]byte, 301<<10)); err == nil {
		t.Fatal("a logo over 300 KB is refused")
	}
	if err := SaveBrandLogo(tenantID, logo.Bytes()); err != nil {
		t.Fatalf("a PNG logo is kept: %v", err)
	}
	if err := SetSetting(tenantID, SettingBrandAccent, "#ffd700", "test"); err == nil {
		t.Fatal("a light accent is refused on save")
	}
	if err := SetSetting(tenantID, SettingBrandAccent, "#0f766e", "test"); err != nil {
		t.Fatal(err)
	}
	_ = SetFeatureFlag(tenantID, FeatureClientBranding, false) // a new client may copy the default's flags
	if b := GetBranding(tenantID); b.Enabled || b.LogoVersion != "" || b.Accent != "" {
		t.Fatalf("off (the standard plan), nothing shows: %+v", b)
	}
	if err := SetFeatureFlag(tenantID, FeatureClientBranding, true); err != nil {
		t.Fatal(err)
	}
	b := GetBranding(tenantID)
	if !b.Enabled || b.Accent != "#0f766e" || b.LogoVersion == "" {
		t.Fatalf("on, the logo and accent show: %+v", b)
	}
	if content, ct, err := BrandLogo(tenantID); err != nil || ct != "image/png" || !bytes.Equal(content, logo.Bytes()) {
		t.Fatalf("the stored logo comes back as a PNG: %s %v", ct, err)
	}
	if err := DeleteBrandLogo(tenantID); err != nil || GetBranding(tenantID).LogoVersion != "" {
		t.Fatalf("a removed logo is gone: %v", err)
	}
}
