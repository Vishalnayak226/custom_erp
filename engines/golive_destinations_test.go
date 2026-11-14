package engines

import (
	"custom_erp/db"
	"strings"
	"testing"
)

// 49.17.3: evidence on the integration-proof line is about the destinations
// that existed when it was attached; a new destination makes it stale. And a
// real database error on the credential list is an error, not "no credentials".

func TestGoLiveIntegrationProofGoesStaleWhenADestinationIsAdded(t *testing.T) {
	tenantID, _, done := goLiveTestTenant(t)
	defer done()
	if err := SetSetting(tenantID, SettingRequireRehearsal, "false", "system"); err != nil {
		t.Fatal(err)
	}
	if err := SaveChannelCredentialBy(tenantID, "T49173-A", map[string]string{"access_token": "tok-a", "shop_domain": "a.example"}, "owner", nil); err != nil {
		t.Fatal(err)
	}
	line := func() *GoLiveLine {
		p, err := GetGoLivePack(tenantID, PackGoLive)
		if err != nil {
			t.Fatal(err)
		}
		l := goLiveLineByIDIn(p, "integration-proof")
		if l == nil {
			t.Fatal("no integration-proof line")
		}
		return l
	}
	if l := line(); l.State != LineNotEvidenced {
		t.Fatalf("an unused connector is not proven: %+v", l)
	}
	if err := AttachGoLiveEvidence(tenantID, "integration-proof", "Test order sent", "T-1", "asha"); err != nil {
		t.Fatal(err)
	}
	if l := line(); l.State != LinePass {
		t.Fatalf("evidence must pass the line: %+v", l)
	}
	if err := SaveChannelCredentialBy(tenantID, "T49173-B", map[string]string{"access_token": "tok-b", "shop_domain": "b.example"}, "owner", nil); err != nil {
		t.Fatal(err)
	}
	l := line()
	if l.State != LineNotEvidenced || l.Evidence == nil || !l.Evidence.Stale || !strings.Contains(strings.ToLower(l.Detail), "destination") {
		t.Fatalf("a new destination must make the evidence stale: %+v", l)
	}
	if err := AttachGoLiveEvidence(tenantID, "integration-proof", "Both proven", "T-2", "asha"); err != nil {
		t.Fatal(err)
	}
	if l := line(); l.State != LinePass || l.Evidence.Stale {
		t.Fatalf("fresh evidence must pass again: %+v", l)
	}
}

func TestListConnectorCredentialsSurfacesRealErrorsButSkipsMissingTables(t *testing.T) {
	tenantID, schema, done := goLiveTestTenant(t)
	defer done()
	if _, err := db.DB.Exec(`DROP TABLE ` + schema + `.pinelabs_credentials`); err != nil {
		t.Fatal(err)
	}
	if _, err := ListConnectorCredentials(tenantID); err != nil {
		t.Fatalf("a table an older schema lacks is skipped: %v", err)
	}
	if _, err := db.DB.Exec(`ALTER TABLE ` + schema + `.clevertap_credentials RENAME COLUMN active TO active_x`); err != nil {
		t.Fatal(err)
	}
	if _, err := ListConnectorCredentials(tenantID); err == nil {
		t.Fatal("a real database error must not read as an empty list")
	}
	p, err := GetGoLivePack(tenantID, PackGoLive)
	if err != nil {
		t.Fatal(err)
	}
	if l := goLiveLineByIDIn(p, "integration-proof"); l == nil || l.State != LineNotEvidenced || !strings.Contains(l.Detail, "Could not be read") {
		t.Fatalf("the gate line must not read as not applicable on a database error: %+v", l)
	}
}
