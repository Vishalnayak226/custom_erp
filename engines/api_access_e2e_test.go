package engines

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"custom_erp/db"
)

// 205.16 (ask 226): a document event reaches a key's webhook end to end - the
// outbox row, the delivery job for the key's subscription, the signed POST the
// receiver can check with the secret it was shown once, and the delivery log.
func TestAPIAccessE2EKeyWebhookDelivery(t *testing.T) {
	db.InitDB(testConnStr())
	modA, docA, _, _ := apiAccessPick(t)
	owner := fmt.Sprintf("__api226_%d__", time.Now().UnixNano())
	seedLifecycleUser(t, owner, RoleSuperAdmin, "Active")
	cred, _ := makeAPIKeyOwnedBy(t, owner, RoleSuperAdmin, map[string]string{modA: "write"})
	hook, err := CreateKeyWebhook("default", cred.ID, owner, "https://example.com/t226", nil)
	if err != nil || !strings.HasPrefix(hook.Secret, "whsec_") {
		t.Fatalf("subscribe: %+v %v", hook, err)
	}
	docID := fmt.Sprintf("T226-DOC-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		db.DB.Exec(`DELETE FROM tenant_default.async_jobs WHERE payload->>'subscription_id' = $1`, hook.ID)
		db.DB.Exec(`DELETE FROM tenant_default.integration_event_outbox WHERE payload->>'id' = $1`, docID)
		db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id = $1`, hook.ID)
		forgetSubscriptionProbe("tenant_default")
	})

	if err := PublishDocumentEventNow("default", docA, docID, "submitted", "Pending Approval"); err != nil {
		t.Fatal(err)
	}
	var evID, evName string
	var raw []byte
	if err := db.DB.QueryRow(`SELECT id, event_name, payload FROM tenant_default.integration_event_outbox WHERE payload->>'id' = $1`, docID).Scan(&evID, &evName, &raw); err != nil {
		t.Fatalf("outbox row: %v", err)
	}
	if want := modA + "." + docA + ".submitted"; evName != want {
		t.Fatalf("event %q, want %q", evName, want)
	}
	var payload map[string]interface{}
	_ = json.Unmarshal(raw, &payload)

	// What processOutbox does for this row: one job per matching subscription.
	dispatchWebhooksForEvent("tenant_default", evID, evName, payload, "")
	var jobID string
	var jobRaw []byte
	if err := db.DB.QueryRow(`SELECT id, payload FROM tenant_default.async_jobs WHERE job_type = $1 AND payload->>'subscription_id' = $2`, webhookDeliveryJobType, hook.ID).Scan(&jobID, &jobRaw); err != nil {
		t.Fatalf("no delivery job for the key's subscription: %v", err)
	}
	var job struct {
		URL          string                 `json:"url"`
		EventName    string                 `json:"event_name"`
		EventPayload map[string]interface{} `json:"event_payload"`
		Secret       string                 `json:"secret"`
	}
	_ = json.Unmarshal(jobRaw, &job)
	if job.Secret != "" || bytes.Contains(jobRaw, []byte(hook.Secret)) {
		t.Error("the job row carries the signing secret")
	}
	if job.EventName != evName || job.EventPayload["id"] != docID {
		t.Fatalf("job payload: %s", jobRaw)
	}

	// The signed POST, as the receiver sees it.
	var got struct {
		header http.Header
		body   []byte
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.header = r.Header.Clone()
		got.body, _ = io.ReadAll(r.Body)
	}))
	defer srv.Close()
	oldPrivate := outboundAllowPrivate
	outboundAllowPrivate = true // the receiver is on loopback
	t.Cleanup(func() { outboundAllowPrivate = oldPrivate })
	res, err := sendWebhookHTTPContext(context.Background(), srv.URL, hook.Secret, job.EventName, job.EventPayload, jobID)
	if err != nil || res["status_code"] != http.StatusOK {
		t.Fatalf("send: %v %v", res, err)
	}
	sign := func(msg string) string {
		m := hmac.New(sha256.New, []byte(hook.Secret))
		m.Write([]byte(msg))
		return hex.EncodeToString(m.Sum(nil))
	}
	if h := got.header.Get("X-Webhook-Signature"); h != "sha256="+sign(string(got.body)) {
		t.Errorf("X-Webhook-Signature %q does not match the body", h)
	}
	ts := got.header.Get("X-Webhook-Timestamp")
	if h := got.header.Get("X-Webhook-Signature-256"); h != "t="+ts+",v1="+sign(ts+"."+string(got.body)) {
		t.Errorf("X-Webhook-Signature-256 %q does not match t.body", h)
	}
	if got.header.Get("X-Webhook-Event") != evName || got.header.Get("X-Webhook-Delivery") != jobID {
		t.Errorf("event/delivery headers: %v", got.header)
	}
	var body struct {
		Event   string                 `json:"event"`
		Payload map[string]interface{} `json:"payload"`
	}
	if err := json.Unmarshal(got.body, &body); err != nil || body.Event != evName || body.Payload["id"] != docID ||
		body.Payload["record_url"] != "/api/public/v1/records/"+docA+"/"+docID {
		t.Errorf("body: %s", got.body)
	}

	// The key page's delivery log lists the job.
	list, err := ListKeyWebhookDeliveries("default", cred.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range list {
		found = found || (d.ID == jobID && d.Event == evName)
	}
	if !found {
		t.Errorf("delivery log lacks job %s: %+v", jobID, list)
	}
}

// Ask 226: a key "until 10 Mar" ends at the end of 10 Mar where the business
// is, not at the end of 10 Mar UTC (05:29 the next morning in India).
func TestAPIKeyExpiryDateEndsThatLocalDay(t *testing.T) {
	got, err := parseAPIKeyExpiry("2030-03-10")
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2030, 3, 10, 23, 59, 59, 0, BusinessLocation())
	if !got.Equal(want) {
		t.Errorf("expiry %v, want %v", got.In(BusinessLocation()), want)
	}
}

// 205.20 (ask 226): a key ending within 30 days is on the attention list (the
// screen's banner and the Home tile); once revoked it is not.
func TestAPIKeyAttentionListsAnEndingKey(t *testing.T) {
	db.InitDB(testConnStr())
	modA, _, _, _ := apiAccessPick(t)
	owner := fmt.Sprintf("__api226att_%d__", time.Now().UnixNano())
	seedLifecycleUser(t, owner, RoleSuperAdmin, "Active")
	cred, _ := makeAPIKeyOwnedBy(t, owner, RoleSuperAdmin, map[string]string{modA: "read"})
	actor := APIKeyActor{UserID: owner, Role: RoleSuperAdmin}
	kinds := func() []string {
		list, err := APIKeyAttentionList("default", actor, false)
		if err != nil {
			t.Fatal(err)
		}
		out := []string{}
		for _, a := range list {
			if a.KeyID == cred.ID {
				out = append(out, a.Kind)
			}
		}
		return out
	}
	if got := kinds(); len(got) != 1 || got[0] != "expiring" {
		t.Fatalf("a 30-day key: attention %v, want [expiring]", got)
	}
	if _, err := db.DB.Exec(`UPDATE tenant_default.api_credentials SET status = 'Revoked' WHERE id = $1`, cred.ID); err != nil {
		t.Fatal(err)
	}
	if got := kinds(); len(got) != 0 {
		t.Errorf("a revoked key: attention %v, want none", got)
	}
}

// 227.3 (owner, ask 228): *Share module API* stays Admin only. Admin and Super
// Admin reach the API access routes through LevelAdmin; no role template holds
// api.share, so giving it to another role is a decision made here, not by a
// template edit that slips through.
func TestShareModuleAPIStaysAdminOnly(t *testing.T) {
	for _, tpl := range RoleTemplates() {
		for _, c := range tpl.Capabilities {
			if c == "api.share" {
				t.Errorf("role template %q holds api.share; 227.3 keeps it Admin only - change this test only with the owner's say-so", tpl.Name)
			}
		}
	}
}

// 205.22 (ask 226): a public-API server error tells operations once in ten
// minutes for a company, not once per failing call.
func TestAPIAccessE2EFaultAlertOncePerTenWithoutFlood(t *testing.T) {
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(prev)
	tenant := fmt.Sprintf("t226-%d", time.Now().UnixNano())
	other := tenant + "-b"
	AlertPublicAPIFault(tenant, "/api/public/v1/records/Item", 500, "corr-1")
	AlertPublicAPIFault(tenant, "/api/public/v1/records/Item", 502, "corr-2")
	AlertPublicAPIFault(other, "/api/public/v1/records/Item", 500, "corr-3")
	out := buf.String()
	if n := strings.Count(out, "company "+tenant+":"); n != 1 {
		t.Errorf("%d alerts for one company in ten minutes, want 1:\n%s", n, out)
	}
	if !strings.Contains(out, "corr-1") || strings.Contains(out, "corr-2") {
		t.Errorf("the first fault is the one told:\n%s", out)
	}
	if !strings.Contains(out, "company "+other+":") {
		t.Errorf("another company's fault was held back:\n%s", out)
	}
}
