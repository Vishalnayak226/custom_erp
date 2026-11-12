package engines

import (
	"custom_erp/db"
	"encoding/json"
	"strings"
	"testing"
)

// 49.8.5 (screen): one list of courier / channel / CleverTap credentials, with
// rotate and switch-off. The list never carries a secret; rotate replaces the
// secret and stamps who and when; a switched-off credential is refused.

func credential4985cSetup(t *testing.T) string {
	t.Helper()
	schema := credential4985Setup(t)
	cleanup := func() {
		_, _ = db.DB.Exec(`DELETE FROM ` + schema + `.channel_credentials WHERE channel_code LIKE 'T4985C-%' OR channel_code = 'courier:t4985c'`)
		_, _ = db.DB.Exec(`DELETE FROM ` + schema + `.clevertap_credentials WHERE account_id LIKE 'T4985C-%'`)
		_, _ = db.DB.Exec(`DELETE FROM ` + schema + `.audit_logs WHERE action IN ('CONNECTOR_ROTATED','CONNECTOR_SWITCHED') AND details LIKE '%T4985C-%'`)
	}
	cleanup()
	t.Cleanup(cleanup)
	return schema
}

func TestStage4985cListNeverReturnsASecret(t *testing.T) {
	credential4985cSetup(t)
	const secret = "t4985c-very-secret-token"
	if err := SaveChannelCredentialBy("default", "T4985C-CH", map[string]string{"access_token": secret, "webhook_secret": secret + "-wh"}, "owner4985c", nil); err != nil {
		t.Fatal(err)
	}
	if err := SaveCleverTapCredentialBy("default", "T4985C-CT", secret+"-passcode", "in1", "owner4985c", ""); err != nil {
		t.Fatal(err)
	}
	rows, err := ListConnectorCredentials("default")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(rows)
	if strings.Contains(string(raw), secret) {
		t.Fatalf("the list carries a secret: %s", raw)
	}
	seen := map[string]ConnectorCredentialRow{}
	for _, r := range rows {
		seen[r.Kind+"/"+r.Code] = r
	}
	ch, ct := seen["channel/T4985C-CH"], seen["clevertap/T4985C-CT"]
	if ch.Owner != "owner4985c" || ch.Status != "Active" || ct.Owner != "owner4985c" || ct.Status != "Active" {
		t.Fatalf("rows: %+v / %+v", ch, ct)
	}
}

func TestStage4985cRotateReplacesAndStamps(t *testing.T) {
	schema := credential4985cSetup(t)
	const code = "T4985C-ROT"
	if err := SaveChannelCredentialBy("default", code, map[string]string{"access_token": "old-token", "shop_domain": "x.example"}, "first4985c", nil); err != nil {
		t.Fatal(err)
	}
	var before string
	_ = db.DB.QueryRow(`SELECT updated_at::text FROM `+schema+`.channel_credentials WHERE channel_code=$1`, code).Scan(&before)
	// An empty replacement is refused and changes nothing.
	if _, err := RotateConnectorCredential("default", "channel", code, map[string]string{"access_token": " "}, "second4985c"); err == nil {
		t.Fatal("a blank secret must be refused")
	}
	generated, err := RotateConnectorCredential("default", "channel", code, map[string]string{"access_token": "new-token", "webhook_secret": ""}, "second4985c")
	if err != nil {
		t.Fatal(err)
	}
	got, err := getChannelCredentialRaw("default", code)
	if err != nil || got["access_token"] != "new-token" || got["shop_domain"] != "x.example" {
		t.Fatalf("after rotate: %v %v", got, err)
	}
	// A blank webhook_secret is generated, handed back once, and stored.
	if len(generated["webhook_secret"]) < 32 || got["webhook_secret"] != generated["webhook_secret"] {
		t.Fatalf("generated %v stored %q", generated, got["webhook_secret"])
	}
	if _, leaked := generated["access_token"]; leaked {
		t.Fatal("a supplied secret must not be echoed back")
	}
	var owner, after string
	_ = db.DB.QueryRow(`SELECT saved_by, updated_at::text FROM `+schema+`.channel_credentials WHERE channel_code=$1`, code).Scan(&owner, &after)
	if owner != "second4985c" || after == before {
		t.Fatalf("the change is not stamped: owner %q updated %s -> %s", owner, before, after)
	}
	var audits int
	_ = db.DB.QueryRow(`SELECT count(*) FROM ` + schema + `.audit_logs WHERE action='CONNECTOR_ROTATED' AND details LIKE '%` + code + `%'`).Scan(&audits)
	if audits != 1 {
		t.Fatalf("rotate audited %d times", audits)
	}
	// CleverTap: the passcode is replaced.
	if err := SaveCleverTapCredentialBy("default", "T4985C-CT", "old-pass", "in1", "first4985c", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := RotateConnectorCredential("default", "clevertap", "T4985C-CT", map[string]string{"passcode": "new-pass"}, "second4985c"); err != nil {
		t.Fatal(err)
	}
	var pass string
	_ = db.DB.QueryRow(`SELECT passcode FROM ` + schema + `.clevertap_credentials WHERE account_id='T4985C-CT'`).Scan(&pass)
	if pass != "new-pass" {
		t.Fatalf("passcode %q", pass)
	}
	if _, err := RotateConnectorCredential("default", "channel", "T4985C-NONE", map[string]string{"access_token": "x"}, "a"); err == nil {
		t.Fatal("rotating a missing credential must fail")
	}
}

func TestStage4985cSwitchOffRefusesTheCredential(t *testing.T) {
	credential4985cSetup(t)
	const code = "T4985C-OFF"
	if err := SaveChannelCredentialBy("default", code, map[string]string{"access_token": "tok"}, "o", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := getChannelCredential("default", code); err != nil {
		t.Fatal(err)
	}
	if err := SetConnectorCredentialActive("default", "channel", code, false, "admin4985c"); err != nil {
		t.Fatal(err)
	}
	if _, err := getChannelCredential("default", code); err == nil || !strings.Contains(err.Error(), "switched off") {
		t.Fatalf("a switched-off credential must be refused, got %v", err)
	}
	rows, _ := ListConnectorCredentials("default")
	for _, r := range rows {
		if r.Code == code && (r.Status != "Off" || r.Active) {
			t.Fatalf("row %+v", r)
		}
	}
	if err := SetConnectorCredentialActive("default", "channel", code, true, "admin4985c"); err != nil {
		t.Fatal(err)
	}
	if _, err := getChannelCredential("default", code); err != nil {
		t.Fatalf("switched back on: %v", err)
	}
	// Saving new fields switches it on, like the other kinds do.
	_ = SetConnectorCredentialActive("default", "channel", code, false, "admin4985c")
	if err := SaveChannelCredentialBy("default", code, map[string]string{"access_token": "tok2"}, "o", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := getChannelCredential("default", code); err != nil {
		t.Fatalf("a fresh save must switch it on: %v", err)
	}
}

func TestStage4985cSecretFieldsAreNamesOnly(t *testing.T) {
	credential4985cSetup(t)
	if err := SaveChannelCredentialBy("default", "T4985C-NAMES", map[string]string{"access_token": "v1", "shop_domain": "x.example", "webhook_secret": "v2"}, "o", nil); err != nil {
		t.Fatal(err)
	}
	rows, _ := ListConnectorCredentials("default")
	for _, r := range rows {
		if r.Code == "T4985C-NAMES" {
			if strings.Join(r.SecretFields, ",") != "access_token,webhook_secret" {
				t.Fatalf("secret fields %v", r.SecretFields)
			}
			return
		}
	}
	t.Fatal("row missing")
}
