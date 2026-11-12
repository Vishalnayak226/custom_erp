package engines

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"custom_erp/db"
)

// Stage 146.5.9: signed public image addresses.

const (
	pmKeyA = "0123456789abcdef0123456789abcdef-key-A"
	pmKeyB = "fedcba9876543210fedcba9876543210-key-B"
)

type pmFixture struct {
	*poLCFixture
	media *ProductMediaAsset
	img   []byte
}

func newPMFixture(t *testing.T, item string) *pmFixture {
	t.Helper()
	t.Setenv("PUBLIC_MEDIA_SIGNING_KEY", pmKeyA)
	t.Setenv("PUBLIC_MEDIA_SIGNING_KEY_PREVIOUS", "")
	f := &pmFixture{poLCFixture: newPOLCFixture(t)}
	f.img = tinyTestJPEG(t, 40, 40)
	a, err := SaveMediaFile(f.tenantID, f.img, item+".jpg", item, "Main", "system", "", "")
	if err != nil {
		t.Fatalf("save media: %v", err)
	}
	f.media = a
	if err := SetSetting(f.tenantID, SettingPublicMediaURLs, "true", "test"); err != nil {
		t.Fatalf("switch on: %v", err)
	}
	return f
}

// sigOf pulls the signature out of an address built by PublicMediaPath.
func pmSig(path string) string { return path[strings.LastIndex(path, "/")+1:] }

func pmExpect404(t *testing.T, what, tenant, id, sig string) {
	t.Helper()
	b, _, err := ReadPublicMedia(tenant, id, sig)
	if !errors.Is(err, ErrPublicMediaNotFound) || b != nil {
		t.Errorf("%s: want ErrPublicMediaNotFound, got %v (%d bytes)", what, err, len(b))
	}
}

func TestPublicMediaServesOnlyWhenItShould(t *testing.T) {
	f := newPMFixture(t, "PM-ITEM-1")
	path := PublicMediaPath(f.tenantID, f.media.ID)
	sig := pmSig(path)
	if !strings.HasPrefix(path, "/media/"+f.tenantID+"/") {
		t.Fatalf("path %q", path)
	}

	b, ft, err := ReadPublicMedia(f.tenantID, f.media.ID, sig)
	if err != nil || ft != "image/jpeg" || !bytes.Equal(b, f.img) {
		t.Fatalf("a valid address must serve the stored bytes: %v %q %d", err, ft, len(b))
	}

	// Tampered, empty, truncated and wrong-case signatures.
	pmExpect404(t, "tampered sig", f.tenantID, f.media.ID, sig[:len(sig)-1]+map[bool]string{true: "A", false: "B"}[sig[len(sig)-1] != 'A'])
	pmExpect404(t, "empty sig", f.tenantID, f.media.ID, "")
	pmExpect404(t, "truncated sig", f.tenantID, f.media.ID, sig[:10])
	pmExpect404(t, "unknown media id", f.tenantID, "NOPE::main::abc", pmSig(PublicMediaPath(f.tenantID, "NOPE::main::abc")))

	// A signature is for one client and one picture.
	other := newPMFixture(t, "PM-ITEM-2")
	pmExpect404(t, "client A's signature on client B's path", other.tenantID, f.media.ID, sig)
	pmExpect404(t, "client A's id under client B, signed for B", other.tenantID, f.media.ID, pmSig(PublicMediaPath(other.tenantID, f.media.ID)))
	pmExpect404(t, "B's signature, A's media", f.tenantID, other.media.ID, pmSig(PublicMediaPath(other.tenantID, other.media.ID)))
	if _, _, err := ReadPublicMedia(other.tenantID, other.media.ID, pmSig(PublicMediaPath(other.tenantID, other.media.ID))); err != nil {
		t.Fatalf("client B's own address must serve: %v", err)
	}

	// A client that did not switch it on serves nothing.
	if err := SetSetting(f.tenantID, SettingPublicMediaURLs, "false", "test"); err != nil {
		t.Fatal(err)
	}
	pmExpect404(t, "setting off", f.tenantID, f.media.ID, sig)
	if err := SetSetting(f.tenantID, SettingPublicMediaURLs, "true", "test"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReadPublicMedia(f.tenantID, f.media.ID, sig); err != nil {
		t.Fatalf("serves again once back on: %v", err)
	}

	// Deactivated and deleted media.
	if _, err := db.DB.Exec("UPDATE "+f.schema+".documents SET deleted_at = now() WHERE id = $1", f.media.ID); err != nil {
		t.Fatal(err)
	}
	pmExpect404(t, "deleted media", f.tenantID, f.media.ID, sig)
	if _, err := db.DB.Exec("UPDATE "+f.schema+".documents SET deleted_at = NULL, status = 'Inactive' WHERE id = $1", f.media.ID); err != nil {
		t.Fatal(err)
	}
	pmExpect404(t, "inactive media", f.tenantID, f.media.ID, sig)
	if _, err := db.DB.Exec("UPDATE "+f.schema+".documents SET status = 'Active' WHERE id = $1", f.media.ID); err != nil {
		t.Fatal(err)
	}

	// A picture of another file type is never served here.
	if _, err := db.DB.Exec("UPDATE "+f.schema+".documents SET data = jsonb_set(data, '{file_type}', '\"application/pdf\"') WHERE id = $1", f.media.ID); err != nil {
		t.Fatal(err)
	}
	pmExpect404(t, "non-image type", f.tenantID, f.media.ID, sig)

	// An unregistered client id (a purged one would fall to the default schema).
	pmExpect404(t, "unregistered client", "nosuchclient9", f.media.ID, pmSig(PublicMediaPath("nosuchclient9", f.media.ID)))
}

func TestPublicMediaKeyUnsetAndRotation(t *testing.T) {
	f := newPMFixture(t, "PM-ITEM-3")
	sigA := pmSig(PublicMediaPath(f.tenantID, f.media.ID))

	// Previous key alone never serves.
	t.Setenv("PUBLIC_MEDIA_SIGNING_KEY", "")
	t.Setenv("PUBLIC_MEDIA_SIGNING_KEY_PREVIOUS", pmKeyA)
	pmExpect404(t, "key unset", f.tenantID, f.media.ID, sigA)
	if PublicMediaEnabled(f.tenantID) || PublicMediaPath(f.tenantID, f.media.ID) != "" {
		t.Error("with no key the feature must be off and sign nothing")
	}
	// A key under 32 characters counts as unset.
	t.Setenv("PUBLIC_MEDIA_SIGNING_KEY", "too-short")
	pmExpect404(t, "short key", f.tenantID, f.media.ID, sigA)

	// Rotation: new key signs, the previous one still verifies, nothing else does.
	t.Setenv("PUBLIC_MEDIA_SIGNING_KEY", pmKeyB)
	t.Setenv("PUBLIC_MEDIA_SIGNING_KEY_PREVIOUS", pmKeyA)
	sigB := pmSig(PublicMediaPath(f.tenantID, f.media.ID))
	if sigA == sigB {
		t.Fatal("a new key must sign differently")
	}
	for name, sig := range map[string]string{"old address during overlap": sigA, "new address": sigB} {
		if _, _, err := ReadPublicMedia(f.tenantID, f.media.ID, sig); err != nil {
			t.Errorf("%s must serve: %v", name, err)
		}
	}
	t.Setenv("PUBLIC_MEDIA_SIGNING_KEY_PREVIOUS", "")
	pmExpect404(t, "old address after the previous key is removed", f.tenantID, f.media.ID, sigA)
	t.Setenv("PUBLIC_MEDIA_SIGNING_KEY_PREVIOUS", "an-unrelated-key-an-unrelated-key-12345")
	pmExpect404(t, "unknown previous key", f.tenantID, f.media.ID, sigA)
	if _, _, err := ReadPublicMedia(f.tenantID, f.media.ID, sigB); err != nil {
		t.Errorf("current key must still serve: %v", err)
	}
}

func TestPublicMediaSettingNeedsKey(t *testing.T) {
	f := newPOLCFixture(t)
	t.Setenv("PUBLIC_MEDIA_SIGNING_KEY", "")
	if err := SetSetting(f.tenantID, SettingPublicMediaURLs, "true", "test"); err == nil || !strings.Contains(err.Error(), "PUBLIC_MEDIA_SIGNING_KEY") {
		t.Fatalf("switching on without a key must say why, got %v", err)
	}
	if GetSettingBool(f.tenantID, SettingPublicMediaURLs) {
		t.Fatal("off is the default and stays off after a refused change")
	}
	t.Setenv("PUBLIC_MEDIA_SIGNING_KEY", pmKeyA)
	if err := SetSetting(f.tenantID, SettingPublicMediaURLs, "true", "test"); err != nil {
		t.Fatalf("with a key it switches on: %v", err)
	}
}

func TestPublicMediaFeedImageURL(t *testing.T) {
	f := newPMFixture(t, "PM-ITEM-4")
	if err := SetSetting(f.tenantID, "crm.public_link_base", "https://erp.example.com/", "test"); err != nil {
		t.Fatal(err)
	}
	feed := func() string { v, _ := channelExportExtras(f.tenantID, "AMZ")("PM-ITEM-4", "image_url"); return v }

	want := "https://erp.example.com" + PublicMediaPath(f.tenantID, f.media.ID)
	if got := feed(); got != want || strings.Contains(got, "com//") {
		t.Fatalf("on, with key and address: got %q want %q", got, want)
	}
	if _, _, err := ReadPublicMedia(f.tenantID, f.media.ID, pmSig(feed())); err != nil {
		t.Fatalf("the address in the feed must serve: %v", err)
	}

	if err := SetSetting(f.tenantID, SettingPublicMediaURLs, "false", "test"); err != nil {
		t.Fatal(err)
	}
	if got := feed(); got != "" {
		t.Errorf("setting off: feed must stay as today (empty for ERP-hosted media), got %q", got)
	}
	if err := SetSetting(f.tenantID, SettingPublicMediaURLs, "true", "test"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PUBLIC_MEDIA_SIGNING_KEY", "")
	if got := feed(); got != "" {
		t.Errorf("key unset: got %q", got)
	}
	t.Setenv("PUBLIC_MEDIA_SIGNING_KEY", pmKeyA)
	if err := SetSetting(f.tenantID, "crm.public_link_base", "", "test"); err != nil {
		t.Fatal(err)
	}
	if got := feed(); got != "" {
		t.Errorf("no web address known: got %q", got)
	}
	if err := SetSetting(f.tenantID, "crm.public_link_base", "https://erp.example.com", "test"); err != nil {
		t.Fatal(err)
	}

	// Media that already has a public address keeps it, with the feature on or off.
	if _, err := db.DB.Exec("UPDATE "+f.schema+".documents SET data = jsonb_set(data, '{file_path}', '\"https://img.example/a.jpg\"') WHERE id = $1", f.media.ID); err != nil {
		t.Fatal(err)
	}
	if got := feed(); got != "https://img.example/a.jpg" {
		t.Errorf("an existing public path must win, got %q", got)
	}
	// Another client's media never shows in this client's feed.
	other := newPMFixture(t, "PM-ITEM-4")
	if v, _ := channelExportExtras(other.tenantID, "AMZ")("PM-ITEM-4", "image_url"); v != "" && !strings.Contains(v, "/media/"+other.tenantID+"/") {
		t.Errorf("wrong-tenant feed address: %q", v)
	}
}
