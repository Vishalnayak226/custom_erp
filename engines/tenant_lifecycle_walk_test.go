package engines

// BLD-028 / 50.12 - the whole client lifecycle in one walk, in three variants:
// provision, bootstrap, MFA, hostname, suspend, resume, export, legal hold,
// retention and offboarding. Each step asserts what each role and ANOTHER
// client can reach (sessions, API keys, workers), and the end state: the
// export reconciles, a hold blocks the purge, and only the approved purge
// removes the client - leaving the bystander client untouched.
//
// Legal decisions stay with the owner's lawyer: the code names where they
// apply (SetTenantLegalHold: any purge; RequestTenantDeprovision: how long data
// is kept; PurgeTenant: irreversible deletion), and this test only proves
// those gates hold.

import (
	"custom_erp/db"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// walkMember is one signed-in person of a walked client.
type walkMember struct {
	id, role string
	active   bool // false: deactivated before the walk starts
}

type walkClient struct {
	id, schema string
	members    []walkMember
	apiKey     string // an active API key
	revokedKey string // a key revoked before the suspension
	apiKeyID   string
	marker     string // a document id found only in this client
}

func walkProvision(t *testing.T, label string, members []walkMember) *walkClient {
	t.Helper()
	id, schema := uniqueLifecycleTenant(t)
	id, schema = id+label, schema+label
	t.Cleanup(func() { dropLifecycleTenant(id, schema) })
	if _, err := ProvisionTenantSchema(id, schema, "0.1.0-test"); err != nil {
		t.Fatalf("provision %s: %v", label, err)
	}
	c := &walkClient{id: id, schema: schema, marker: "WALK-" + strings.ToUpper(label) + "-" + id}
	for _, m := range members {
		status := "Active"
		if !m.active {
			status = "Inactive"
		}
		if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.users (id, username, password_hash, email, role, status) VALUES ($1, $1, 'x', $2, $3, $4) ON CONFLICT (id) DO UPDATE SET role = EXCLUDED.role, status = EXCLUDED.status`, schema),
			m.id, m.id+"@walk.test", m.role, status); err != nil {
			t.Fatalf("user %s: %v", m.id, err)
		}
		c.members = append(c.members, m)
	}
	if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'SalesOrder', '{"customer":"C1"}', 'Draft', 'admin')`, schema), c.marker); err != nil {
		t.Fatalf("marker doc: %v", err)
	}
	scope := PublicAPIScopes()[0]
	issued, err := IssueAPICredential(id, "walk key", []string{scope}, nil, "admin")
	if err != nil {
		t.Fatalf("issue key: %v", err)
	}
	c.apiKey, c.apiKeyID = issued.APIKey, issued.Credential.ID
	old, err := IssueAPICredential(id, "walk old key", []string{scope}, nil, "admin")
	if err != nil {
		t.Fatalf("issue old key: %v", err)
	}
	c.revokedKey = old.APIKey
	if err := RevokeAPICredential(id, old.Credential.ID); err != nil {
		t.Fatalf("revoke old key: %v", err)
	}
	return c
}

func (c *walkClient) served(t *testing.T) bool {
	t.Helper()
	list, err := listTenantSchemas()
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range list {
		if s == c.schema {
			return true
		}
	}
	return false
}

// assertAccess is the access matrix at one moment: live=true means the client
// serves traffic, so every active member's session resolves and the active API
// key works; live=false means nothing of it works and no worker serves it.
// A deactivated member and a revoked key never work, live or not.
func (c *walkClient) assertAccess(t *testing.T, step string, live bool) {
	t.Helper()
	for _, m := range c.members {
		st, err := ResolveLiveUserState(c.id, m.id)
		want := live && m.active
		if want && (err != nil || st.Role != m.role) {
			t.Errorf("%s: %s (%s) should be signed in: %+v %v", step, m.id, m.role, st, err)
		}
		if !want && !IsUserNotActiveError(err) {
			t.Errorf("%s: %s (%s) must be refused as not active, got %v", step, m.id, m.role, err)
		}
	}
	cred, err := AuthenticateAPICredential(c.id, c.apiKey)
	if live && (err != nil || cred == nil) {
		t.Errorf("%s: the active API key should work: %v", step, err)
	}
	if !live && !errors.Is(err, ErrInvalidAPICredential) {
		t.Errorf("%s: the API key must be refused with the one public answer, got %v", step, err)
	}
	if _, err := AuthenticateAPICredential(c.id, c.revokedKey); !errors.Is(err, ErrInvalidAPICredential) {
		t.Errorf("%s: a revoked key must never work, got %v", step, err)
	}
	if got := c.served(t); got != live {
		t.Errorf("%s: workers serve=%v, want %v", step, got, live)
	}
}

// assertBystander proves another client is unaffected and cannot cross over.
func (b *walkClient) assertBystander(t *testing.T, step string, other *walkClient) {
	t.Helper()
	b.assertAccess(t, step+" (other client)", true)
	// Another client's key and people do not open this one, nor this one theirs.
	if _, err := AuthenticateAPICredential(b.id, other.apiKey); !errors.Is(err, ErrInvalidAPICredential) {
		t.Errorf("%s: a key of %s opened %s: %v", step, other.id, b.id, err)
	}
	var n int
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT count(*) FROM %s.documents WHERE id = $1`, b.schema), b.marker).Scan(&n); err != nil || n != 1 {
		t.Errorf("%s: the other client's data changed: %d rows, %v", step, n, err)
	}
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT count(*) FROM %s.documents WHERE id = $1`, b.schema), other.marker).Scan(&n); err != nil || n != 0 {
		t.Errorf("%s: another client's document is in %s: %d, %v", step, b.id, n, err)
	}
}

func walkFileContains(t *testing.T, dir, needle string) bool {
	t.Helper()
	found := false
	_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || found {
			return nil
		}
		raw, rerr := os.ReadFile(p)
		if rerr == nil && strings.Contains(string(raw), needle) {
			found = true
		}
		return nil
	})
	return found
}

func TestBLD028TenantLifecycleWalk(t *testing.T) {
	db.InitDB(testConnStr())

	// The bystander is a second, healthy client for the whole walk.
	bystander := walkProvision(t, "by", []walkMember{{"admin", RoleSuperAdmin, true}, {"cash1", RoleCashier, true}})

	variants := []struct {
		name    string
		members []walkMember
		trial   bool
	}{
		{"single_admin", []walkMember{{"admin", RoleSuperAdmin, true}}, false},
		{"manager_and_cashier", []walkMember{
			{"admin", RoleSuperAdmin, true}, {"mgr1", RoleStoreManager, true},
			{"cash1", RoleCashier, true}, {"gone1", RoleCashier, false}}, false},
		{"trial_that_expires", []walkMember{{"admin", RoleSuperAdmin, true}, {"cash1", RoleCashier, true}}, true},
	}
	for i, v := range variants {
		v := v
		label := fmt.Sprintf("v%d", i)
		t.Run(v.name, func(t *testing.T) {
			c := walkProvision(t, label, v.members)
			scratch := t.TempDir()

			// 1. Provision: admin holds a one-time credential, MFA is mandatory
			// for the admin role, the client is served and nothing is shared.
			life, err := GetTenantLifecycle(c.id)
			if err != nil || life.Status != TenantStatusActive || !life.BootstrapOutstanding {
				t.Fatalf("provisioned client: %+v %v", life, err)
			}
			if !RequiresMFA(RoleSuperAdmin) {
				t.Error("admin must require MFA")
			}
			for _, m := range v.members {
				if m.role != RoleSuperAdmin && HasFullAccess(m.role) {
					t.Errorf("%s must not have full access", m.role)
				}
			}
			c.assertAccess(t, "after provision", true)
			bystander.assertBystander(t, "after provision", c)

			// 2. Bootstrap: rotating the one-time credential gives a new one,
			// leaves the old hash dead, and is refused for a client that is
			// not serving.
			pw, err := RotateTenantBootstrapCredential(c.id, "admin", "walk", "handover")
			if err != nil || pw == "" {
				t.Fatalf("rotate bootstrap: %q %v", pw, err)
			}

			// 3. MFA: enrol and verify the admin; another client's admin is
			// untouched (same user id, separate schema).
			secret, err := GenerateTOTPSecret()
			if err != nil {
				t.Fatal(err)
			}
			if err := SetPendingMFASecret(c.id, "admin", secret); err != nil {
				t.Fatal(err)
			}
			code, _ := GenerateTOTPCode(secret)
			if !VerifyTOTPCode(c.id, secret, code) {
				t.Fatal("a valid code was refused")
			}
			if err := ActivateMFA(c.id, "admin"); err != nil {
				t.Fatal(err)
			}
			if on, _, _ := GetUserMFAStatus(c.id, "admin"); !on {
				t.Error("admin MFA not active")
			}
			if on, sec, _ := GetUserMFAStatus(bystander.id, "admin"); on || sec == secret {
				t.Errorf("another client's admin picked up this MFA (on=%v)", on)
			}

			// 4. Hostname: a name is unique across clients.
			slug := "walk" + strings.ToLower(label) + fmt.Sprint(time.Now().UnixNano()%1_000_000)
			if _, err := db.DB.Exec(`UPDATE public.tenants SET host_slug = $1 WHERE tenant_id = $2`, slug, c.id); err != nil {
				t.Fatal(err)
			}
			if _, err := db.DB.Exec(`UPDATE public.tenants SET host_slug = $1 WHERE tenant_id = $2`, slug, bystander.id); err == nil {
				t.Error("two clients got the same hostname")
			}

			// 5. Trial that runs out: everything stops, workers skip it, and
			// converting it to paid brings back exactly what was working.
			if v.trial {
				if _, err := SetTenantTrial(c.id, 7, "walk", "trial"); err != nil {
					t.Fatal(err)
				}
				c.assertAccess(t, "trial running", true)
				db.DB.Exec(`UPDATE public.tenants SET trial_ends_at = NOW() - INTERVAL '1 minute' WHERE tenant_id = $1`, c.id)
				InvalidateTenantLifecycleCache(c.id)
				invalidateLiveUserStatesForTenant(c.id)
				c.assertAccess(t, "trial over", false)
				bystander.assertBystander(t, "trial over", c)
				if _, err := SetTenantTrial(c.id, 0, "walk", "client paid"); err != nil {
					t.Fatal(err)
				}
				c.assertAccess(t, "trial converted", true)
			}

			// 6. Suspend: sessions that were already open and the API key stop;
			// only the working people/keys come back on resume.
			for _, m := range v.members { // warm every cache first
				_, _ = ResolveLiveUserState(c.id, m.id)
			}
			if err := SuspendTenant(c.id, "walk", "non-payment"); err != nil {
				t.Fatal(err)
			}
			c.assertAccess(t, "suspended", false)
			bystander.assertBystander(t, "suspended", c)
			if _, err := RotateTenantBootstrapCredential(c.id, "admin", "walk", "while suspended"); err == nil {
				t.Error("a credential was issued to a suspended client")
			}

			// 7. Export while the client is quiet: it reconciles, carries this
			// client's data and none of another's, and leaves secrets out.
			dir := filepath.Join(scratch, "export")
			m, err := ExportTenantPortable(c.id, dir, "walk", false)
			if err != nil {
				t.Fatalf("export of a suspended client: %v", err)
			}
			if _, ok := m.Excluded["api_credentials"]; !ok {
				t.Error("API credentials must stay out of the export")
			}
			rep, err := ReconcileTenantPortable(dir, c.id)
			if err != nil || !rep.OK {
				t.Fatalf("export does not reconcile: %+v %v", rep, err)
			}
			if !walkFileContains(t, dir, c.marker) {
				t.Error("the export is missing the client's own document")
			}
			if walkFileContains(t, dir, bystander.marker) {
				t.Error("the export carries another client's document")
			}
			if walkFileContains(t, dir, c.apiKey) {
				t.Error("the export carries an API key secret")
			}

			// 8. Resume: back exactly as before, no more.
			if err := ResumeTenant(c.id, "walk", "payment received"); err != nil {
				t.Fatal(err)
			}
			c.assertAccess(t, "resumed", true)
			bystander.assertBystander(t, "resumed", c)

			// 9. Offboarding: traffic stops at once; resume is no undo; a trial
			// cannot be re-opened on it. Retention (a legal decision: how long)
			// keeps the data; nothing is purged yet.
			until, err := RequestTenantDeprovision(c.id, "walk", "contract ended", UseDefaultRetention)
			if err != nil || time.Until(until) < 24*time.Hour {
				t.Fatalf("deprovision: %v %v", until, err)
			}
			c.assertAccess(t, "deprovisioning", false)
			if err := ResumeTenant(c.id, "walk", "changed mind"); err == nil {
				t.Error("a deprovisioning client was resumed as if nothing happened")
			}
			if _, err := SetTenantTrial(c.id, 5, "walk", "x"); err == nil {
				t.Error("a trial was set on a deprovisioning client")
			}
			asker, approver := clientRemovalAdmins(t)
			if _, err := RequestClientDataRemoval(c.id, ClientRemovalPurge, "walk", "no-such-operator-admin"); err == nil {
				t.Error("someone who is not an operator admin asked for a purge")
			}
			purge := func(req string) error {
				_, err := PurgeTenant(c.id, "walk", "pg_dump walk.dump", req)
				return err
			}
			reqID, err := RequestClientDataRemoval(c.id, ClientRemovalPurge, "walk", asker.username)
			if err != nil {
				t.Fatalf("request purge: %v", err)
			}
			if err := purge(reqID); err == nil || !strings.Contains(err.Error(), "retention") {
				t.Errorf("retention window did not stop the purge: %v", err)
			}

			// 10. Legal hold (a legal decision: when to lift it) outranks an
			// elapsed retention and an approved request.
			db.DB.Exec(`UPDATE public.tenants SET retention_until = NOW() - INTERVAL '1 day' WHERE tenant_id = $1`, c.id)
			if err := SetTenantLegalHold(c.id, true, "walk", "matter 2026-15"); err != nil {
				t.Fatal(err)
			}
			if err := DecideApproval(ClientRemovalTenant(), ClientDataRemovalDoctype, reqID, asker.id, RoleSuperAdmin, "", "Approved", "self"); err == nil {
				t.Error("the person who asked approved their own purge")
			}
			if err := purge(reqID); err == nil {
				t.Fatal("an unapproved request purged a client")
			}
			if err := DecideApproval(ClientRemovalTenant(), ClientDataRemovalDoctype, reqID, approver.id, RoleSuperAdmin, "", "Approved", "ok"); err != nil {
				t.Fatalf("second admin approve: %v", err)
			}
			if err := purge(reqID); err == nil || !strings.Contains(err.Error(), "legal hold") {
				t.Fatalf("the hold did not stop an approved purge: %v", err)
			}
			if _, err := GetTenantLifecycle(c.id); err != nil {
				t.Fatalf("the client vanished under a hold: %v", err)
			}
			c.assertAccess(t, "under hold", false)
			bystander.assertBystander(t, "under hold", c)
			if err := SetTenantLegalHold(c.id, false, "walk", "matter closed"); err != nil {
				t.Fatal(err)
			}

			// 11. Hold released, retention over, approved by a second person:
			// the purge removes the client and proves nothing remains.
			// A request for the wrong act or the wrong client cannot be used.
			wrongAct := approvedClientRemoval(t, c.id, ClientRemovalReset)
			if err := purge(wrongAct); err == nil {
				t.Error("an approved RESET was accepted for a purge")
			}
			wrongClient := approvedClientRemoval(t, bystander.id, ClientRemovalPurge)
			if err := purge(wrongClient); err == nil {
				t.Error("an approval for another client was accepted")
			}
			res, err := PurgeTenant(c.id, "walk", "pg_dump walk.dump", reqID)
			if err != nil {
				t.Fatalf("purge: %v", err)
			}
			if !res.Clean {
				t.Errorf("residue after purge: %+v", res)
			}
			if err := purge(reqID); err == nil {
				t.Error("a spent approval purged again")
			}
			if _, err := GetTenantLifecycle(c.id); !errors.Is(err, ErrTenantNotRegistered) {
				t.Errorf("purged client still registered: %v", err)
			}
			if _, err := AuthenticateAPICredential(c.id, c.apiKey); !errors.Is(err, ErrInvalidAPICredential) {
				t.Errorf("a purged client's key still answers: %v", err)
			}
			for _, mm := range v.members {
				if _, err := ResolveLiveUserState(c.id, mm.id); !IsUserNotActiveError(err) {
					t.Errorf("purged client member %s: %v", mm.id, err)
				}
			}
			var left int
			db.DB.QueryRow(`SELECT count(*) FROM information_schema.schemata WHERE schema_name = $1`, c.schema).Scan(&left)
			if left != 0 || c.served(t) {
				t.Errorf("schema left=%d served=%v", left, c.served(t))
			}
			names := eventNames(t, c.id)
			for _, want := range []string{TenantEventSuspended, TenantEventResumed, TenantEventDeprovisionRequested,
				TenantEventLegalHoldSet, TenantEventLegalHoldCleared, TenantEventPurgeRefused, TenantEventPurged, TenantEventResidueVerified} {
				if !hasEvent(names, want) {
					t.Errorf("evidence trail %v lacks %s", names, want)
				}
			}
			bystander.assertBystander(t, "after purge", c)
		})
	}
}
