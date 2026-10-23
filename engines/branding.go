package engines

import (
	"crypto/sha256"
	"custom_erp/db"
	"encoding/hex"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Stage 134.10 (120.6, user 2026-10-23 "build it now"): a client's own brand
// in the app, as a per-client mode the platform operator switches on (the
// client_branding feature flag - the plan unlocks it, never the client). With
// it on, the client's logo replaces the Wholeops logo on their sign-in page
// and in the header, their accent colour replaces the default, and
// "Powered by Wholeops" stays at the foot. Off (the standard plan), nothing
// changes - the company name beside the logo (130.7) is for everyone.
//
//   - The logo is a raster picture only (PNG, JPEG or WebP - never SVG, which
//     can carry script), at most 300 KB, through the upload rule (134.4). It
//     lives in the media store under the client's schema, served by
//     GET /api/v1/branding/logo with nosniff.
//   - The accent colour is checked on save: a hex colour dark enough for
//     white button text (contrast 4.5:1 or more).

const FeatureClientBranding = "client_branding"

const SettingBrandAccent = "branding.accent_color"

const brandLogoMaxBytes = 300 << 10

var brandAccentPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func init() {
	RegisterSetting(SettingDefinition{
		Key: SettingBrandAccent, Module: "App branding", Label: "Accent colour", Type: SettingTypeString, Default: "",
		Description: "Your brand colour for buttons, links and highlights, as #RRGGBB (for example #0f766e). It must be dark enough for white text on it. Blank keeps the standard colours. Shown only when branding is switched on for your company.",
	})
	settingValidators[SettingBrandAccent] = validateBrandAccent
}

// validateBrandAccent accepts blank or a #RRGGBB dark enough for white text.
func validateBrandAccent(v string) error {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	if !brandAccentPattern.MatchString(v) {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: SettingBrandAccent, Message: "Give the colour as # and six hex digits, for example #0f766e"}
	}
	if c := contrastWithWhite(v); c < 4.5 {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: SettingBrandAccent, Message: fmt.Sprintf("%s is too light for white text on buttons (contrast %.1f:1, needs 4.5:1) - pick a darker shade", v, c)}
	}
	return nil
}

// contrastWithWhite is the WCAG contrast ratio of #RRGGBB against white.
func contrastWithWhite(hexColour string) float64 {
	lin := func(i int) float64 {
		n, _ := strconv.ParseUint(hexColour[1+2*i:3+2*i], 16, 8)
		c := float64(n) / 255
		if c <= 0.03928 {
			return c / 12.92
		}
		return math.Pow((c+0.055)/1.055, 2.4)
	}
	l := 0.2126*lin(0) + 0.7152*lin(1) + 0.0722*lin(2)
	return 1.05 / (l + 0.05)
}

// Branding is what the shell shows; Enabled false means the standard look.
type Branding struct {
	Enabled     bool   `json:"enabled"`
	CompanyName string `json:"company_name,omitempty"`
	Accent      string `json:"accent,omitempty"`
	LogoVersion string `json:"logo_version,omitempty"` // "" = no logo; changes with the file
}

// GetBranding is the client's branding as the shell should show it.
func GetBranding(tenantID string) Branding {
	enabled, _ := IsFeatureEnabled(tenantID, FeatureClientBranding)
	if !enabled {
		return Branding{}
	}
	b := Branding{Enabled: true, CompanyName: GetSettingString(tenantID, "platform.company_name"), Accent: GetSettingString(tenantID, SettingBrandAccent)}
	if validateBrandAccent(b.Accent) != nil {
		b.Accent = ""
	}
	if content, _, err := BrandLogo(tenantID); err == nil {
		sum := sha256.Sum256(content)
		b.LogoVersion = hex.EncodeToString(sum[:6])
	}
	return b
}

func brandLogoPath(tenantID string) (string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	return filepath.Join(mediaStoreDir, "branding", schema, "logo"), nil
}

// SaveBrandLogo stores the client's logo, replacing any earlier one.
func SaveBrandLogo(tenantID string, content []byte) error {
	if len(content) > brandLogoMaxBytes {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "file", Message: fmt.Sprintf("The logo is %d KB; at most 300 KB - save it smaller", len(content)>>10)}
	}
	if err := CheckUpload(UploadImage, content); err != nil {
		return err
	}
	switch http.DetectContentType(content) {
	case "image/png", "image/jpeg", "image/webp":
	default:
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "file", Message: "The logo must be a PNG, JPEG or WebP picture"}
	}
	path, err := brandLogoPath(tenantID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, content, 0o640); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// DeleteBrandLogo removes the logo; the standard one shows again.
func DeleteBrandLogo(tenantID string) error {
	path, err := brandLogoPath(tenantID)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// BrandLogo is the stored logo and its type.
func BrandLogo(tenantID string) ([]byte, string, error) {
	path, err := brandLogoPath(tenantID)
	if err != nil {
		return nil, "", err
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	return content, http.DetectContentType(content), nil
}
