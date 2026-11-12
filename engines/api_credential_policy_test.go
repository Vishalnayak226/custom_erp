package engines

import (
	"custom_erp/db"
	"strings"
	"testing"
)

// Stage 49.2.6 - purpose per API credential, one active per purpose (setting),
// rotation policy (setting).

func issuePurposeCred(t *testing.T, name, purpose string) *IssuedAPICredential {
	t.Helper()
	c, err := IssueAPICredential("default", name, []string{PublicAPIScopes()[0]}, nil, "t49-2-6")
	if err != nil {
		t.Fatal(err)
	}
	id := c.Credential.ID
	t.Cleanup(func() {
		db.DB.Exec(`DELETE FROM tenant_default.api_oauth_tokens WHERE credential_id = $1::uuid`, id)
		db.DB.Exec(`DELETE FROM tenant_default.api_credentials WHERE id = $1::uuid`, id)
		db.DB.Exec(`DELETE FROM tenant_default.audit_logs WHERE details LIKE $1`, "credential="+id+"%")
	})
	if purpose != "" {
		if err := SetAPICredentialPurpose("default", id, purpose); err != nil {
			t.Fatalf("set purpose: %v", err)
		}
	}
	return c
}

func TestStage4926PurposeOffIsOnlyALabel(t *testing.T) {
	db.InitDB(testConnStr())
	s := stage4911Suffix()
	p := "Shopify " + s
	a := issuePurposeCred(t, "t4926a-"+s, p)
	if err := CheckAPICredentialPurposeFree("default", p); err != nil {
		t.Fatalf("setting off must not refuse: %v", err)
	}
	b := issuePurposeCred(t, "t4926b-"+s, p) // second, same purpose: allowed while off
	list, err := ListAPICredentials("default")
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, c := range list {
		if (c.ID == a.Credential.ID || c.ID == b.Credential.ID) && c.Purpose == p {
			seen++
		}
	}
	if seen != 2 {
		t.Errorf("purpose listed on %d of 2 credentials", seen)
	}
	if _, err := NormalizeAPICredentialPurpose("bad;purpose"); err == nil {
		t.Error("odd characters accepted in a purpose")
	}
}

func TestStage4926OnePerPurposeWhenOn(t *testing.T) {
	db.InitDB(testConnStr())
	s := stage4911Suffix()
	p := "ERP sync " + s
	if err := SetSetting("default", SettingAPICredentialOnePerPurpose, "true", "t49-2-6"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = SetSetting("default", SettingAPICredentialOnePerPurpose, "false", "t49-2-6")
		db.DB.Exec(`DELETE FROM tenant_default.system_settings WHERE key = $1`, SettingAPICredentialOnePerPurpose)
	})
	a := issuePurposeCred(t, "t4926c-"+s, p)
	err := CheckAPICredentialPurposeFree("default", strings.ToUpper(p))
	var ve *ValidationError
	if err == nil || !asValidationError(err, &ve) || ve.Code != "SEC-0290" {
		t.Fatalf("second active credential for purpose: %v, want SEC-0290", err)
	}
	// A racing issue that got past the pre-check is still refused by the write.
	b, ierr := IssueAPICredential("default", "t4926d-"+s, []string{PublicAPIScopes()[0]}, nil, "t49-2-6")
	if ierr != nil {
		t.Fatal(ierr)
	}
	t.Cleanup(func() {
		db.DB.Exec(`DELETE FROM tenant_default.api_credentials WHERE id = $1::uuid`, b.Credential.ID)
	})
	if err := SetAPICredentialPurpose("default", b.Credential.ID, p); err == nil {
		t.Error("write-time guard let a second active credential take the purpose")
	}
	// Rotation keeps the purpose on the new credential and frees the old.
	rot, err := RotateAPICredential("default", a.Credential.ID, "t49-2-6")
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	t.Cleanup(func() {
		db.DB.Exec(`DELETE FROM tenant_default.api_credentials WHERE id = $1::uuid`, rot.Credential.ID)
	})
	var got string
	db.DB.QueryRow(`SELECT COALESCE(purpose,'') FROM tenant_default.api_credentials WHERE id = $1::uuid`, rot.Credential.ID).Scan(&got)
	if got != p {
		t.Errorf("rotated credential purpose = %q, want %q", got, p)
	}
	// Revoking frees the purpose.
	if err := RevokeAPICredential("default", rot.Credential.ID); err != nil {
		t.Fatal(err)
	}
	if err := CheckAPICredentialPurposeFree("default", p); err != nil {
		t.Errorf("revoked credential still holds the purpose: %v", err)
	}
}

func TestStage4926RotationPolicySweep(t *testing.T) {
	db.InitDB(testConnStr())
	s := stage4911Suffix()
	old := issuePurposeCred(t, "t4926-old-"+s, "")
	soon := issuePurposeCred(t, "t4926-soon-"+s, "")
	fresh := issuePurposeCred(t, "t4926-new-"+s, "")
	age := func(id, days string) {
		db.DB.Exec(`UPDATE tenant_default.api_credentials SET created_at = NOW() - $2::interval WHERE id = $1::uuid`, id, days+" days")
	}
	age(old.Credential.ID, "200")
	age(soon.Credential.ID, "85")
	age(fresh.Credential.ID, "5")
	status := func(id string) string {
		var st string
		db.DB.QueryRow(`SELECT status FROM tenant_default.api_credentials WHERE id = $1::uuid`, id).Scan(&st)
		return st
	}
	if n, err := SweepAgedAPICredentials("default"); err != nil || n != 0 || status(old.Credential.ID) != "Active" {
		t.Fatalf("policy off: revoked %d, err %v, old %s", n, err, status(old.Credential.ID))
	}
	setLifecyclePolicy(t, SettingAPICredentialMaxAgeDays, "90", false)
	if n, err := SweepAgedAPICredentials("default"); err != nil || n != 0 || status(old.Credential.ID) != "Active" {
		t.Fatalf("policy just turned on must give every key a full period: revoked %d, err %v", n, err)
	}
	setLifecyclePolicy(t, SettingAPICredentialMaxAgeDays, "90", true)
	for i := 0; i < 2; i++ { // second run: warning not repeated, nothing more revoked
		n, err := SweepAgedAPICredentials("default")
		want := 0
		if i == 0 {
			want = 1
		}
		if err != nil || n != want {
			t.Fatalf("run %d: revoked %d, err %v, want %d", i, n, err, want)
		}
	}
	if status(old.Credential.ID) != "Revoked" || status(soon.Credential.ID) != "Active" || status(fresh.Credential.ID) != "Active" {
		t.Errorf("old %s soon %s fresh %s; want Revoked Active Active", status(old.Credential.ID), status(soon.Credential.ID), status(fresh.Credential.ID))
	}
	var warned, revoked int
	db.DB.QueryRow(`SELECT COUNT(*) FROM tenant_default.audit_logs WHERE action = 'API_CREDENTIAL_ROTATION_DUE' AND details LIKE $1`, "credential="+soon.Credential.ID+"%").Scan(&warned)
	db.DB.QueryRow(`SELECT COUNT(*) FROM tenant_default.audit_logs WHERE action = 'API_CREDENTIAL_REVOKED' AND details LIKE $1`, "credential="+old.Credential.ID+"%rotation policy%").Scan(&revoked)
	if warned != 1 || revoked != 1 {
		t.Errorf("audit rows: warned %d (want 1), revoked %d (want 1)", warned, revoked)
	}
	if _, err := AuthenticateAPICredential("default", old.APIKey); err != ErrInvalidAPICredential {
		t.Errorf("revoked key still authenticates: %v", err)
	}
}

func TestStage4926TokenIssuerAndAudience(t *testing.T) {
	sess := SignToken("u1", "u1", "Cashier", "default", "HO", 1)
	purp := SignPurposeToken("u1", "u1", "default", "mfa", 5*60*1e9)
	ext := SignExtensionToken("default", "Item", 5*60*1e9)
	want := map[string]string{"session": sess, "purpose": purp, "extension": ext}
	for aud, tok := range want {
		c, err := ParseToken(tok)
		if err != nil {
			t.Fatalf("%s token: %v", aud, err)
		}
		if c["iss"] != "erp" || c["aud"] != aud {
			t.Errorf("%s token claims iss=%q aud=%q", aud, c["iss"], c["aud"])
		}
	}
	forge := func(claims string) string { return signClaims(claims + "&exp=9999999999") }
	cases := map[string]bool{
		"id=u1&tenant=default&iss=other&aud=session":           false, // wrong issuer
		"id=u1&tenant=default&iss=erp&aud=session&purpose=mfa": false, // purpose token passed off as a session
		"id=u1&tenant=default&iss=erp&aud=purpose":             false, // session passed off as a purpose token
		"tenant=default&iss=erp&aud=extension&purpose=mfa":     false,
		"id=u1&tenant=default&iss=erp&aud=somethingelse":       false,
		"id=u1&tenant=default&role=Cashier":                    true, // minted before the claims existed
		"id=u1&tenant=default&purpose=mfa":                     true,
	}
	for claims, ok := range cases {
		_, err := ParseToken(forge(claims))
		if (err == nil) != ok {
			t.Errorf("%q accepted=%v, want %v", claims, err == nil, ok)
		}
	}
}
