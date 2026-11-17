package engines

import (
	"custom_erp/db"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/lib/pq"
)

// 205.16, 205.19-205.21: events, share link, rotation, reminders, failure alert.

func TestEventForStatusChange(t *testing.T) {
	cases := []struct {
		existed      bool
		prior, newer string
		want         string
	}{
		{false, "", "Draft", "created"},
		{true, "Draft", "Submitted", "submitted"},
		{true, "Submitted", "Cancelled", "cancelled"},
		{true, "Submitted", "Approved", "approved"},
		{true, "Draft", "Draft", "updated"},
		{true, "Submitted", "Submitted", "updated"},
	}
	for _, c := range cases {
		if got := EventForStatusChange(c.existed, c.prior, c.newer); got != c.want {
			t.Errorf("%v %q->%q = %q, want %q", c.existed, c.prior, c.newer, got, c.want)
		}
	}
}

// A company-wide subscription made before 205.16 must not start receiving
// document events after the release; only a key's subscription does.
func TestDocumentEventsOnlyForKeySubscriptions(t *testing.T) {
	doc := map[string]interface{}{"event": "inventory.Item.updated", "record_url": "/api/public/v1/records/Item/IT-1"}
	if !isDocumentEvent(doc) {
		t.Fatal("a document event was not recognised")
	}
	for _, old := range []map[string]interface{}{
		{"event": "inventory.stock_changed", "sku": "A1"},
		{"event": "order.created", "record_url": "https://shop.example/orders/1"},
		{},
	} {
		if isDocumentEvent(old) {
			t.Fatalf("an older platform event was taken for a document event: %v", old)
		}
	}
}

func TestAPIAccessShareDocsRotationAndWatch(t *testing.T) {
	db.InitDB(testConnStr())
	modA, _, _, _ := apiAccessPick(t)
	owner := fmt.Sprintf("__api205w2_%d__", time.Now().UnixNano())
	seedLifecycleUser(t, owner, RoleSuperAdmin, "Active")
	cred, _ := makeAPIKeyOwnedBy(t, owner, RoleSuperAdmin, map[string]string{modA: "write"})

	t.Run("share link: days are capped, a bad token is not ok, withdraw ends it", func(t *testing.T) {
		if _, _, err := SetShareDocs("default", cred.ID, 500); err == nil {
			t.Error("a link of 500 days was allowed")
		}
		token, exp, err := SetShareDocs("default", cred.ID, 90)
		if err != nil {
			t.Fatal(err)
		}
		if exp.After(time.Now().Add(91 * 24 * time.Hour)) {
			t.Errorf("link lasts beyond 90 days: %v", exp)
		}
		if tenant, id, ok := ResolveShareDocsToken(token); !ok || tenant != "default" || id != cred.ID {
			t.Fatalf("fresh token: %v %v %v", tenant, id, ok)
		}
		for _, bad := range []string{"", "default.", "default.00", "nobody." + token[len("default."):], "x"} {
			if _, _, ok := ResolveShareDocsToken(bad); ok {
				t.Errorf("token %q resolved", bad)
			}
		}
		db.DB.Exec(`UPDATE tenant_default.api_credentials SET docs_expires_at = CURRENT_TIMESTAMP - INTERVAL '1 minute' WHERE id=$1`, cred.ID)
		if _, _, ok := ResolveShareDocsToken(token); ok {
			t.Error("an expired link resolved")
		}
		token, _, _ = SetShareDocs("default", cred.ID, 1)
		if err := WithdrawShareDocs("default", cred.ID); err != nil {
			t.Fatal(err)
		}
		if _, _, ok := ResolveShareDocsToken(token); ok {
			t.Error("a withdrawn link resolved")
		}
	})

	t.Run("a webhook of one key receives only the events it asked for", func(t *testing.T) {
		hook, err := CreateKeyWebhook("default", cred.ID, owner, "https://example.com/h", []string{modA + ".PurchaseOrder.submitted"})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id=$1`, hook.ID) })
		if hook.Secret == "" {
			t.Fatal("the secret must be shown once")
		}
		if !keySubscriptionMayReceive("tenant_default", cred.ID, []string{modA + ".PurchaseOrder.submitted"}, modA+".PurchaseOrder.submitted") {
			t.Error("a matching event was not delivered")
		}
		if keySubscriptionMayReceive("tenant_default", cred.ID, []string{modA + ".PurchaseOrder.submitted"}, modA+".PurchaseOrder.cancelled") {
			t.Error("an event outside the list was delivered")
		}
	})

	t.Run("rotation under concurrent calls refuses nothing", func(t *testing.T) {
		out, err := CreateAPIKey("default", APIKeyActor{UserID: owner, Role: RoleSuperAdmin}, CreateAPIKeyInput{
			Name: "api205w2 rotate", Grant: APIKeyGrant{Modules: map[string]string{modA: "read"}}, Expires: "30d",
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			db.DB.Exec(`DELETE FROM tenant_default.api_credentials WHERE id=$1 OR rotated_from=$1`, out.Credential.ID)
		})
		oldKey := out.APIKey
		stop := make(chan struct{})
		var wg sync.WaitGroup
		var mu sync.Mutex
		failures := 0
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					select {
					case <-stop:
						return
					default:
					}
					if c, err := AuthenticateAPICredential("default", oldKey); err != nil || c == nil {
						mu.Lock()
						failures++
						mu.Unlock()
					}
				}
			}()
		}
		time.Sleep(50 * time.Millisecond)
		rotated, err := RotateAPIKey("default", out.Credential.ID, owner, 24*time.Hour)
		time.Sleep(100 * time.Millisecond)
		close(stop)
		wg.Wait()
		if err != nil || rotated == nil || rotated.APIKey == "" {
			t.Fatalf("rotate: %v", err)
		}
		if failures != 0 {
			t.Errorf("%d calls with the old key were refused during the overlap", failures)
		}
		if c, err := AuthenticateAPICredential("default", rotated.APIKey); err != nil || c == nil {
			t.Errorf("the new key does not work: %v", err)
		}
	})

	t.Run("reminders go once per step and the failure alert once an hour", func(t *testing.T) {
		step := func() int {
			var s int
			db.DB.QueryRow(`SELECT COALESCE(expiry_reminded_days,0) FROM tenant_default.api_credentials WHERE id=$1`, cred.ID).Scan(&s)
			return s
		}
		sweep := func() {
			if _, err := SweepAPIKeyWatch("default", time.Now()); err != nil {
				t.Fatal(err)
			}
		}
		db.DB.Exec(`UPDATE tenant_default.api_credentials SET expires_at = CURRENT_TIMESTAMP + INTERVAL '6 days', expiry_reminded_days=NULL, failure_alerted_at=NULL WHERE id=$1`, cred.ID)
		sweep()
		if step() != 7 {
			t.Errorf("reminder step = %d, want 7", step())
		}
		sweep()
		if step() != 7 {
			t.Errorf("step moved to %d on a second sweep", step())
		}
		db.DB.Exec(`UPDATE tenant_default.api_credentials SET expires_at = CURRENT_TIMESTAMP + INTERVAL '20 hours' WHERE id=$1`, cred.ID)
		sweep()
		if step() != 1 {
			t.Errorf("reminder step = %d, want 1", step())
		}

		insert := func(n int) {
			for i := 0; i < n; i++ {
				db.DB.Exec(`INSERT INTO tenant_default.api_request_log (credential_id, method, path, status_code, duration_ms, outcome)
					VALUES ($1, 'GET', '/api/public/v1/records/PurchaseOrder', 403, 1, 'refused:APIKEY-0503')`, cred.ID)
			}
		}
		t.Cleanup(func() { db.DB.Exec(`DELETE FROM tenant_default.api_request_log WHERE credential_id=$1`, cred.ID) })
		insert(APIKeyFailureThreshold - 1)
		if _, failing := apiRecentFailure("default", "tenant_default", cred.ID); failing {
			t.Fatal("alert below the threshold")
		}
		insert(1)
		if _, failing := apiRecentFailure("default", "tenant_default", cred.ID); !failing {
			t.Fatal("no alert at the threshold")
		}
		sweep()
		var at1, at2 time.Time
		if err := db.DB.QueryRow(`SELECT failure_alerted_at FROM tenant_default.api_credentials WHERE id=$1`, cred.ID).Scan(&at1); err != nil || at1.IsZero() {
			t.Fatalf("the owner was not alerted: %v", err)
		}
		sweep()
		db.DB.QueryRow(`SELECT failure_alerted_at FROM tenant_default.api_credentials WHERE id=$1`, cred.ID).Scan(&at2)
		if !at1.Equal(at2) {
			t.Error("the alert repeated within the hour")
		}
	})
}

// 227.1: a module's own route finds the document by id alone and sends the
// moved status as an event; a never-shared document type sends nothing.
// 227.2: a key still waiting for its developer link cannot be rotated.
func TestModuleRouteEventsAndPendingRotate(t *testing.T) {
	db.InitDB(testConnStr())
	modA, docA, _, _ := apiAccessPick(t)
	owner := fmt.Sprintf("__api227_%d__", time.Now().UnixNano())
	seedLifecycleUser(t, owner, RoleSuperAdmin, "Active")
	cred, _ := makeAPIKeyOwnedBy(t, owner, RoleSuperAdmin, map[string]string{modA: "write"})
	hook, err := CreateKeyWebhook("default", cred.ID, owner, "https://example.com/h", []string{modA + "." + docA + ".cancelled"})
	if err != nil {
		t.Fatal(err)
	}
	forgetSubscriptionProbe("tenant_default")

	id := fmt.Sprintf("API227-%d", time.Now().UnixNano())
	never := id + "-PR"
	t.Cleanup(func() {
		db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id = ANY($1)`, pq.Array([]string{id, never, hook.ID}))
		db.DB.Exec(`DELETE FROM tenant_default.integration_event_outbox WHERE payload->>'id' = ANY($1)`, pq.Array([]string{id, never}))
	})
	if _, err := db.DB.Exec(`INSERT INTO tenant_default.documents (id, doctype, data, status, created_by) VALUES ($1, $2, '{}', 'Submitted', $3)`, id, docA, owner); err != nil {
		t.Fatal(err)
	}
	if dt, st := DocumentByID("default", id); dt != docA || st != "Submitted" {
		t.Fatalf("DocumentByID = %q %q, want %q Submitted", dt, st, docA)
	}
	if dt, _ := DocumentByID("default", id+"-none"); dt != "" {
		t.Fatalf("a missing id found %q", dt)
	}
	if err := PublishDocumentEventNow("default", docA, id, EventForStatusChange(true, "Submitted", "Cancelled"), "Cancelled"); err != nil {
		t.Fatal(err)
	}
	var n int
	db.DB.QueryRow(`SELECT COUNT(*) FROM tenant_default.integration_event_outbox WHERE payload->>'id'=$1 AND event_name=$2`, id, modA+"."+docA+".cancelled").Scan(&n)
	if n != 1 {
		t.Errorf("cancelled events queued = %d, want 1", n)
	}

	if APIDoctypeClass("PasswordResetRequest") != APIDoctypeNever {
		t.Fatal("PasswordResetRequest is expected to be never-shared")
	}
	if err := PublishDocumentEventNow("default", "PasswordResetRequest", never, "cancelled", "Cancelled"); err != nil {
		t.Fatal(err)
	}
	db.DB.QueryRow(`SELECT COUNT(*) FROM tenant_default.integration_event_outbox WHERE payload->>'id'=$1`, never).Scan(&n)
	if n != 0 {
		t.Errorf("a never-shared document type queued %d events", n)
	}

	out, err := CreateAPIKey("default", APIKeyActor{UserID: owner, Role: RoleSuperAdmin}, CreateAPIKeyInput{
		Name: "api227 pending", Grant: APIKeyGrant{Modules: map[string]string{modA: "read"}}, Expires: "30d", AuthMode: "api_key", Deliver: "developer_link",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.DB.Exec(`DELETE FROM tenant_default.api_credentials WHERE id=$1 OR rotated_from=$1`, out.Credential.ID) })
	if _, err := RotateAPIKey("default", out.Credential.ID, owner, 24*time.Hour); validationCode(err) != APIKeyPendingClaim {
		t.Errorf("rotating a key waiting for its link: %v, want %s", err, APIKeyPendingClaim)
	}
}
