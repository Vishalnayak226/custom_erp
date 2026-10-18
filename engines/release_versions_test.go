package engines

import (
	"testing"
	"time"

	"custom_erp/db"
)

// Stage 90: the release numbering rule.
func TestNextReleaseVersion(t *testing.T) {
	cases := []struct {
		previous string
		subjects []string
		want     string
	}{
		{"0.1.0", []string{"docs: x", "feat: y", "fix: z"}, "0.2.0"},
		{"0.2.3", []string{"fix(pos): a", "docs: b"}, "0.2.4"},
		{"0.2.3", []string{"feat(grn): batch rules"}, "0.3.0"},
		{"1.4.2", []string{"feat!: breaking"}, "1.5.0"}, // MAJOR is never automatic
		{"0.2.0", nil, "0.2.0"},                         // a redeploy of the live commit
		{"0.2.0", []string{"chore: feature flag", "refactor: featured"}, "0.2.1"},
	}
	for _, c := range cases {
		got, _, err := NextReleaseVersion(c.previous, c.subjects)
		if err != nil || got != c.want {
			t.Errorf("NextReleaseVersion(%q, %v) = %q, %v; want %q", c.previous, c.subjects, got, err, c.want)
		}
	}
	for _, bad := range []string{"", "0.1", "dev", "0.1.x", "1.2.3.4"} {
		if _, _, err := NextReleaseVersion(bad, []string{"feat: x"}); err == nil {
			t.Errorf("NextReleaseVersion(%q) should be refused", bad)
		}
	}
}

// Recording a release writes one ledger row (a re-run adds none) and stamps
// every unpinned client; a backfilled row leaves the clients alone; a pinned
// client keeps its pin.
func TestRecordReleaseLedgerAndClientStamp(t *testing.T) {
	db.InitDB(testConnStr())
	const env = "test-release-85"
	const version = "9.8.7"
	type tenantVersion struct{ app, pinned *string }
	saved := map[string]tenantVersion{}
	rows, err := db.DB.Query(`SELECT tenant_id, app_version, pinned_version FROM public.tenants`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id string
		var tv tenantVersion
		if err := rows.Scan(&id, &tv.app, &tv.pinned); err != nil {
			t.Fatal(err)
		}
		saved[id] = tv
	}
	rows.Close()
	if len(saved) == 0 {
		t.Skip("no clients in public.tenants to stamp")
	}
	t.Cleanup(func() {
		db.DB.Exec(`DELETE FROM public.deployments WHERE environment = $1`, env)
		for id, tv := range saved {
			db.DB.Exec(`UPDATE public.tenants SET app_version = $2, pinned_version = $3 WHERE tenant_id = $1`, id, tv.app, tv.pinned)
		}
	})
	var pinnedID string
	for id := range saved {
		pinnedID = id
		break
	}
	if _, err := db.DB.Exec(`UPDATE public.tenants SET pinned_version = '0.0.1' WHERE tenant_id = $1`, pinnedID); err != nil {
		t.Fatal(err)
	}

	if _, err := RecordRelease(ReleaseRecord{Environment: env, Version: "9.8.6", Commit: "abc1234", At: time.Now().Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	var stampedByBackfill int
	db.DB.QueryRow(`SELECT COUNT(*) FROM public.tenants WHERE app_version = '9.8.6'`).Scan(&stampedByBackfill)
	if stampedByBackfill != 0 {
		t.Fatalf("a backfilled release stamped %d clients", stampedByBackfill)
	}
	for i := 0; i < 2; i++ {
		if _, err := RecordRelease(ReleaseRecord{Environment: env, Version: version, Commit: "def5678", By: "test", StampClients: true}); err != nil {
			t.Fatal(err)
		}
	}
	var ledgerRows int
	db.DB.QueryRow(`SELECT COUNT(*) FROM public.deployments WHERE environment = $1 AND app_version = $2`, env, version).Scan(&ledgerRows)
	if ledgerRows != 1 {
		t.Fatalf("recording the same release twice left %d ledger rows, want 1", ledgerRows)
	}
	var unstamped, pinnedStamped int
	db.DB.QueryRow(`SELECT COUNT(*) FROM public.tenants WHERE COALESCE(pinned_version, '') = '' AND COALESCE(app_version, '') <> $1`, version).Scan(&unstamped)
	db.DB.QueryRow(`SELECT COUNT(*) FROM public.tenants WHERE tenant_id = $1 AND app_version = $2`, pinnedID, version).Scan(&pinnedStamped)
	if unstamped != 0 || pinnedStamped != 0 {
		t.Fatalf("unpinned clients left unstamped = %d, pinned client stamped = %d; want 0 and 0", unstamped, pinnedStamped)
	}
	if _, err := RecordRelease(ReleaseRecord{Environment: env, Version: version, Commit: "dev"}); err == nil {
		t.Fatal("an unstamped development build was recorded as a release")
	}
}
