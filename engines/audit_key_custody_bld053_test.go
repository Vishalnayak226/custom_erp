package engines

import (
	"custom_erp/db"
	"testing"
	"time"
)

// BLD-053: a lost audit signing key is a key-custody failure, not tampering.
// Rows signed by a key the server no longer holds are reported as
// unverifiable with that key's fingerprint; a row whose content changed under
// a key the server does hold is still reported as tampered.
func TestBLD053RowSignedByAMissingKeyIsUnverifiableNotTampered(t *testing.T) {
	db.InitDB(testConnStr())
	actor := "bld053-" + stage4911Suffix()
	defer db.DB.Exec(`DELETE FROM tenant_default.audit_logs WHERE user_id = $1`, actor)

	for i := 0; i < 3; i++ {
		LogAuditEvent("default", actor, "BLD053_TEST", "SUCCESS", "custody drill")
	}
	var fp string
	if err := db.DB.QueryRow(`SELECT COALESCE(sig_key,'') FROM tenant_default.audit_logs WHERE user_id = $1 LIMIT 1`, actor).Scan(&fp); err != nil {
		t.Fatal(err)
	}
	if fp != auditSigningKeyFingerprint() || len(fp) != 8 {
		t.Fatalf("new rows must carry the signing key's fingerprint: got %q, want %q", fp, auditSigningKeyFingerprint())
	}

	// Two rows "signed under a key since lost": re-signed with a key this
	// process does not hold, and stamped with that key's fingerprint.
	lost := []byte("bld053-lost-key-not-in-keyring!!")
	lostFP := auditKeyFingerprint(lost)
	rows, err := db.DB.Query(`SELECT id, user_id, action, status, COALESCE(details,''), COALESCE(correlation_id,''), created_at
		FROM tenant_default.audit_logs WHERE user_id = $1 ORDER BY seq`, actor)
	if err != nil {
		t.Fatal(err)
	}
	type row struct {
		id, user, action, status, details, corr string
		at                                      time.Time
	}
	var all []row
	for rows.Next() {
		var r row
		rows.Scan(&r.id, &r.user, &r.action, &r.status, &r.details, &r.corr, &r.at)
		all = append(all, r)
	}
	rows.Close()
	if len(all) != 3 {
		t.Fatalf("wrote %d rows, want 3", len(all))
	}
	for _, r := range all[:2] {
		sig := signAuditRowWith(AuditSigVersion, lost, "default", r.user, r.action, r.status, r.details, "", "", r.corr, r.at)
		if _, err := db.DB.Exec(`UPDATE tenant_default.audit_logs SET signature = $1, sig_key = $2 WHERE id = $3::uuid`, sig, lostFP, r.id); err != nil {
			t.Fatal(err)
		}
	}
	// The third: content changed after signing, under the key we hold.
	tampered := all[2].id
	if _, err := db.DB.Exec(`UPDATE tenant_default.audit_logs SET details = 'changed after signing' WHERE id = $1::uuid`, tampered); err != nil {
		t.Fatal(err)
	}

	v, err := VerifyAuditEvidence("default")
	if err != nil {
		t.Fatal(err)
	}
	if v.Intact {
		t.Error("evidence with unverifiable and tampered rows must not be intact")
	}
	if v.UnverifiableRows < 2 || !containsString(v.MissingKeys, lostFP) {
		t.Errorf("rows signed by a missing key: unverifiable=%d missing=%v, want >= 2 and %s", v.UnverifiableRows, v.MissingKeys, lostFP)
	}
	for _, id := range v.TamperedRows {
		if id == all[0].id || id == all[1].id {
			t.Errorf("row %s was signed by a missing key and must not be called tampered", id)
		}
	}
	if !containsString(v.TamperedRows, tampered) && len(v.TamperedRows) < 50 {
		t.Errorf("the row changed under a loaded key must be reported as tampered: %v", v.TamperedRows)
	}
	if want := lostFP; !containsSubstring(v.CoverageStatement, want) {
		t.Errorf("the coverage statement must name the missing key %s: %s", want, v.CoverageStatement)
	}
}

func containsSubstring(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
