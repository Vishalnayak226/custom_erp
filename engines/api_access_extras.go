package engines

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"custom_erp/db"
)

// 205.14-205.22: what sits around an API key beyond its calls - the module check
// for routes that name no document type, the share-docs link, webhooks per
// document type, the attention list, the reminders and the failure alert.

// ---- module check for actions and reports (205.14, 205.15) -----------------

// CheckAPIKeyModule judges a call that names a module rather than a document
// type (an action, a report): the module must be in the plan and on the key at
// the level the call needs. A key limited to some document types of a module is
// refused here, because an action or report reaches the whole module.
func CheckAPIKeyModule(tenantID string, cred *APICredential, access *APIKeyAccess, module string, write bool) (*APIRefusal, error) {
	enabled, err := IsModuleEnabled(tenantID, module)
	if err != nil {
		return nil, err
	}
	if !enabled {
		return &APIRefusal{APIKeyModuleNotInPlan, "module " + module + " is not in this plan"}, nil
	}
	if !credentialHoldsModule(cred, module, write) {
		return &APIRefusal{APIKeyModuleNotOnKey, "the key has no " + map[bool]string{true: "Change", false: "View"}[write] + " access to module " + module}, nil
	}
	if access != nil && len(access.Doctypes[module]) > 0 {
		return &APIRefusal{APIKeyDoctypeNotOnKey, "the key was limited to some document types of " + module + "; actions and reports need the whole module"}, nil
	}
	return nil, nil
}

// ---- share docs (205.19) ------------------------------------------------------

const apiShareDocsMaxDays = 90

// SetShareDocs makes (or replaces) the docs link for a key. The token carries
// the company code; only a digest of its random part is kept.
func SetShareDocs(tenantID, credentialID string, days int) (token string, expires time.Time, err error) {
	if days <= 0 {
		days = 7
	}
	if days > apiShareDocsMaxDays {
		return "", time.Time{}, apiBad("Days", fmt.Sprintf("a docs link lasts up to %d days", apiShareDocsMaxDays))
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", time.Time{}, err
	}
	secret, hash, err := newClaimToken()
	if err != nil {
		return "", time.Time{}, err
	}
	expires = time.Now().Add(time.Duration(days) * 24 * time.Hour)
	res, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.api_credentials SET docs_token_hash=$2, docs_expires_at=$3 WHERE id=$1 AND status='Active'`, schema),
		credentialID, hash, expires)
	if err != nil {
		return "", time.Time{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return "", time.Time{}, &ValidationError{Code: "GLOBAL-0004", Message: "active API key not found"}
	}
	return tenantID + "." + secret, expires, nil
}

// ShareDocsExpiry is when the key's current docs link ends, nil when there is none.
func ShareDocsExpiry(tenantID, credentialID string) (*time.Time, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	var exp sql.NullTime
	var hash sql.NullString
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT docs_expires_at, docs_token_hash FROM %s.api_credentials WHERE id=$1`, schema), credentialID).Scan(&exp, &hash); err != nil {
		return nil, err
	}
	if !exp.Valid || !hash.Valid || hash.String == "" || !exp.Time.After(time.Now()) {
		return nil, nil
	}
	t := exp.Time
	return &t, nil
}

// WithdrawShareDocs ends the link at once.
func WithdrawShareDocs(tenantID, credentialID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	_, err = db.DB.Exec(fmt.Sprintf(`UPDATE %s.api_credentials SET docs_token_hash=NULL, docs_expires_at=NULL WHERE id=$1`, schema), credentialID)
	return err
}

// ResolveShareDocsToken finds the key a docs link belongs to. Every reason it
// can fail (bad shape, unknown company, unknown token, expired, withdrawn, key
// revoked or expired) answers ok=false, so the caller gives one identical 404.
func ResolveShareDocsToken(token string) (tenantID, credentialID string, ok bool) {
	tenantID, secret, found := SplitClaimToken(token)
	if !found {
		return "", "", false
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", "", false
	}
	d := sha256.Sum256([]byte(secret))
	var id string
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT id::text FROM %s.api_credentials
		WHERE docs_token_hash=$1 AND docs_expires_at > CURRENT_TIMESTAMP AND status='Active'
		  AND (expires_at IS NULL OR expires_at > CURRENT_TIMESTAMP)`, schema), hex.EncodeToString(d[:])).Scan(&id)
	if err != nil {
		return "", "", false
	}
	return tenantID, id, true
}

// ---- document events and key webhooks (205.16) ---------------------------------

// The five events a document can publish.
var APIDocumentEvents = []string{"created", "updated", "submitted", "cancelled", "approved"}

func apiEventKnown(verb string) bool {
	for _, v := range APIDocumentEvents {
		if v == verb {
			return true
		}
	}
	return false
}

var apiSubscriptionProbe = struct {
	sync.Mutex
	at  map[string]time.Time
	has map[string]bool
}{at: map[string]time.Time{}, has: map[string]bool{}}

// anyActiveWebhookSubscription is the cheap gate in front of every document
// save: with no key's subscription, a save pays one cached boolean. Only a
// key's subscription counts - document events never go to company-wide
// subscriptions (see isDocumentEvent), so those alone must not make every
// save write an outbox row.
func anyActiveWebhookSubscription(schema string) bool {
	apiSubscriptionProbe.Lock()
	if at, seen := apiSubscriptionProbe.at[schema]; seen && time.Since(at) < 5*time.Second {
		v := apiSubscriptionProbe.has[schema]
		apiSubscriptionProbe.Unlock()
		return v
	}
	apiSubscriptionProbe.Unlock()
	var has bool
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM %s.documents WHERE doctype='WebhookSubscription' AND status='Active' AND deleted_at IS NULL AND COALESCE(data->>'api_credential_id','') <> '')`, schema)).Scan(&has)
	apiSubscriptionProbe.Lock()
	apiSubscriptionProbe.at[schema], apiSubscriptionProbe.has[schema] = time.Now(), has
	apiSubscriptionProbe.Unlock()
	return has
}

func forgetSubscriptionProbe(schema string) {
	apiSubscriptionProbe.Lock()
	delete(apiSubscriptionProbe.at, schema)
	apiSubscriptionProbe.Unlock()
}

// PublishDocumentEvent announces that a document was created, updated,
// submitted, cancelled or approved, once, from the one place the change is
// made. It runs in the background so a save never waits on it, and it does
// nothing unless some webhook subscription exists. The event carries the id,
// the status and the address of the record - never the fields - so a key's
// owner's field rules still decide what the receiver can read.
func PublishDocumentEvent(tenantID, doctype, docID, event, status string) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[WEBHOOK] document event %s %s panicked: %v", doctype, event, r)
			}
		}()
		if err := PublishDocumentEventNow(tenantID, doctype, docID, event, status); err != nil {
			log.Printf("[WEBHOOK] document event %s.%s for %s not queued: %v", doctype, event, docID, err)
		}
	}()
}

// PublishDocumentEventNow is PublishDocumentEvent without the goroutine.
func PublishDocumentEventNow(tenantID, doctype, docID, event, status string) error {
	if doctype == "WebhookSubscription" || !apiEventKnown(event) || APIDoctypeClass(doctype) == APIDoctypeNever {
		return nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	if !anyActiveWebhookSubscription(schema) {
		return nil
	}
	module, err := ModuleForDoctype(tenantID, doctype)
	if err != nil || module == "" {
		return err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	name := module + "." + doctype + "." + event
	payload := map[string]interface{}{
		"event": name, "tenant_code": tenantID, "module": module, "doctype": doctype, "id": docID,
		"status": status, "occurred_at": time.Now().UTC().Format(time.RFC3339),
		"record_url": "/api/public/v1/records/" + doctype + "/" + docID,
	}
	if err := PublishEvent(tx, schema, name, payload); err != nil {
		return err
	}
	return tx.Commit()
}

// KeyWebhook is one subscription a key owns. Secret is set only on create.
type KeyWebhook struct {
	ID        string    `json:"id"`
	URL       string    `json:"url"`
	Events    []string  `json:"events"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	Secret    string    `json:"secret,omitempty"`
}

// KeyWebhookDelivery is one try to reach a subscription, from the job log.
type KeyWebhookDelivery struct {
	ID           string    `json:"id"`
	Event        string    `json:"event"`
	Status       string    `json:"status"`
	ResponseCode int       `json:"response_code"`
	Attempts     int       `json:"attempts"`
	LastError    string    `json:"last_error"`
	CreatedAt    time.Time `json:"created_at"`
}

// ValidateKeyWebhookEvents checks each name is <module>.<Doctype>.<event> (or
// ends in *) and that the key can view the module. An empty list means all.
func ValidateKeyWebhookEvents(cred *APICredential, events []string) ([]string, error) {
	out := make([]string, 0, len(events))
	for _, e := range events {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		parts := strings.Split(e, ".")
		if strings.HasSuffix(e, "*") {
			if len(parts) < 1 || !credentialHoldsModule(cred, strings.TrimSuffix(parts[0], "*"), false) && parts[0] != "*" {
				if parts[0] != "*" {
					return nil, apiBad("Events", e+": the key cannot view that module")
				}
			}
			out = append(out, e)
			continue
		}
		if len(parts) != 3 || !apiEventKnown(parts[2]) {
			return nil, apiBad("Events", e+": use <module>.<Doctype>.<created|updated|submitted|cancelled|approved>")
		}
		if !credentialHoldsModule(cred, parts[0], false) {
			return nil, apiBad("Events", e+": the key cannot view module "+parts[0])
		}
		out = append(out, e)
	}
	return out, nil
}

// CreateKeyWebhook adds a subscription to a key and returns its secret once.
func CreateKeyWebhook(tenantID, credentialID, actor, rawURL string, events []string) (*KeyWebhook, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	row, err := loadAPIKeyRow(schema, credentialID)
	if err != nil {
		return nil, err
	}
	if row.Status != "Active" {
		return nil, &ValidationError{Code: "GLOBAL-0004", Message: "active API key not found"}
	}
	rawURL = strings.TrimSpace(rawURL)
	if err := validateWebhookURL(rawURL); err != nil {
		return nil, &ValidationError{Code: APIKeyWebhookURL, Message: err.Error()}
	}
	cred := &APICredential{Scopes: row.Scopes}
	events, err = ValidateKeyWebhookEvents(cred, events)
	if err != nil {
		return nil, err
	}
	var count int
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.documents WHERE doctype='WebhookSubscription' AND status='Active' AND deleted_at IS NULL
		AND data->>'api_credential_id' = $1`, schema), credentialID).Scan(&count)
	if count >= 10 {
		return nil, apiBad("Url", "a key can have up to 10 webhooks; remove one first")
	}
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	secret := "whsec_" + hex.EncodeToString(b)
	id := NewDocID("WHSUB")
	data, _ := json.Marshal(map[string]interface{}{
		"url": rawURL, "secret": secret, "event_pattern": "*", "events": events,
		"api_credential_id": credentialID, "name": "API key " + row.Name,
	})
	if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'WebhookSubscription', $2, 'Active', $3)`, schema),
		id, data, actor); err != nil {
		return nil, err
	}
	forgetSubscriptionProbe(schema)
	return &KeyWebhook{ID: id, URL: rawURL, Events: events, Status: "Active", CreatedAt: time.Now(), Secret: secret}, nil
}

// ListKeyWebhooks lists a key's subscriptions, without secrets.
func ListKeyWebhooks(tenantID, credentialID string) ([]KeyWebhook, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, COALESCE(data->>'url',''), COALESCE(data->'events','[]'::jsonb), status, created_at
		FROM %s.documents WHERE doctype='WebhookSubscription' AND deleted_at IS NULL AND status='Active' AND data->>'api_credential_id' = $1
		ORDER BY created_at DESC`, schema), credentialID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []KeyWebhook{}
	for rows.Next() {
		var w KeyWebhook
		var raw []byte
		if err := rows.Scan(&w.ID, &w.URL, &raw, &w.Status, &w.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(raw, &w.Events)
		if w.Events == nil {
			w.Events = []string{}
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// DeleteKeyWebhook switches a subscription off; deliveries already queued skip.
func DeleteKeyWebhook(tenantID, credentialID, subscriptionID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	res, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET status='Disabled', deleted_at=CURRENT_TIMESTAMP, updated_at=CURRENT_TIMESTAMP
		WHERE id=$1 AND doctype='WebhookSubscription' AND data->>'api_credential_id' = $2 AND deleted_at IS NULL`, schema), subscriptionID, credentialID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return &ValidationError{Code: "GLOBAL-0004", Message: "webhook not found"}
	}
	forgetSubscriptionProbe(schema)
	return nil
}

func deliveryStatusName(jobStatus string) string {
	switch jobStatus {
	case "Succeeded":
		return "delivered"
	case "Failed":
		return "retrying"
	case "DeadLettered", "Cancelled", "Quarantined":
		return "failed"
	}
	return "pending"
}

// ListKeyWebhookDeliveries reads the delivery log (the job rows) of the key's
// subscriptions, newest first.
func ListKeyWebhookDeliveries(tenantID, credentialID string, limit int) ([]KeyWebhookDelivery, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT j.id, COALESCE(j.payload->>'event_name',''), j.status, COALESCE((j.result->>'status_code')::int, 0),
		j.attempts, j.last_error, j.created_at
		FROM %s.async_jobs j
		WHERE j.job_type = $1 AND j.payload->>'subscription_id' IN (
			SELECT id FROM %s.documents WHERE doctype='WebhookSubscription' AND data->>'api_credential_id' = $2)
		ORDER BY j.created_at DESC LIMIT %d`, schema, schema, limit), webhookDeliveryJobType, credentialID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []KeyWebhookDelivery{}
	for rows.Next() {
		var d KeyWebhookDelivery
		var status string
		if err := rows.Scan(&d.ID, &d.Event, &status, &d.ResponseCode, &d.Attempts, &d.LastError, &d.CreatedAt); err != nil {
			return nil, err
		}
		d.Status = deliveryStatusName(status)
		out = append(out, d)
	}
	return out, rows.Err()
}

// ResendKeyWebhookDelivery queues the same delivery again as a new job.
func ResendKeyWebhookDelivery(tenantID, credentialID, jobID string) (string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	var raw []byte
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT j.payload FROM %s.async_jobs j
		WHERE j.id=$1 AND j.job_type=$2 AND j.payload->>'subscription_id' IN (
			SELECT id FROM %s.documents WHERE doctype='WebhookSubscription' AND data->>'api_credential_id' = $3 AND deleted_at IS NULL)`, schema, schema),
		jobID, webhookDeliveryJobType, credentialID).Scan(&raw)
	if err != nil {
		return "", &ValidationError{Code: "GLOBAL-0004", Message: "delivery not found"}
	}
	payload := map[string]interface{}{}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", err
	}
	return enqueueJobInSchema(SystemJobContext("api-access"), schema, webhookDeliveryJobType, payload,
		fmt.Sprintf("resend-%s-%d", jobID, time.Now().UnixNano()), "")
}

// isDocumentEvent tells a document event (PublishDocumentEventNow) from the
// platform's older events. Only a key's subscription receives document
// events: a company-wide subscription made before 205.16 on "*" or
// "inventory.*" keeps receiving exactly what it did before the release.
func isDocumentEvent(payload map[string]interface{}) bool {
	u, _ := payload["record_url"].(string)
	return strings.HasPrefix(u, "/api/public/v1/records/")
}

// keySubscriptionMayReceive is the extra test a key's subscription passes on
// every event: the key is active and unexpired, the event's module is on the
// key, and the event is one the subscription asked for.
func keySubscriptionMayReceive(schema, credentialID string, events []string, eventName string) bool {
	if len(events) > 0 {
		matched := false
		for _, e := range events {
			if matchEventPattern(e, eventName) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	module := eventName
	if i := strings.Index(eventName, "."); i > 0 {
		module = eventName[:i]
	}
	row, err := loadAPIKeyRow(schema, credentialID)
	if err != nil || row.Status != "Active" {
		return false
	}
	if exp := parseAPITime(row.ExpiresAt); exp != nil && !exp.After(time.Now()) {
		return false
	}
	return credentialHoldsModule(&APICredential{Scopes: row.Scopes}, module, false)
}

// ---- attention, fix, reminders, failure alert (205.20-205.22) -----------------

// Refusals that make a key "failing": this many in this long.
const (
	APIKeyFailureThreshold = 20
	APIKeyFailureWindow    = 10 * time.Minute
)

// APIAttention is one thing about a key that needs a person.
type APIAttention struct {
	KeyID      string     `json:"key_id"`
	Name       string     `json:"name"`
	Kind       string     `json:"kind"` // failing | expiring | rotating | owner_inactive
	Message    string     `json:"message"`
	Module     string     `json:"module,omitempty"`
	ModuleName string     `json:"module_name,omitempty"`
	Level      string     `json:"level,omitempty"` // read | write
	CanFix     bool       `json:"can_fix"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
}

type apiFailureCause struct {
	Code   string
	Module string
	Level  string
	Count  int
	Total  int
}

// apiRecentFailure reads the key's refusals in the window; ok is false below
// the threshold. The cause is the most frequent refusal code.
func apiRecentFailure(tenantID, schema, credentialID string) (apiFailureCause, bool) {
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT outcome, method, path, COUNT(*) FROM %s.api_request_log
		WHERE credential_id::text = $1 AND created_at > CURRENT_TIMESTAMP - ($2 * INTERVAL '1 second') AND outcome LIKE 'refused:%%'
		GROUP BY outcome, method, path ORDER BY COUNT(*) DESC`, schema), credentialID, int(APIKeyFailureWindow.Seconds()))
	if err != nil {
		return apiFailureCause{}, false
	}
	defer rows.Close()
	var cause apiFailureCause
	first := true
	for rows.Next() {
		var outcome, method, path string
		var n int
		if err := rows.Scan(&outcome, &method, &path, &n); err != nil {
			continue
		}
		cause.Total += n
		if first {
			first = false
			cause.Code = strings.TrimPrefix(outcome, "refused:")
			cause.Count = n
			cause.Level = "read"
			if method != "GET" && method != "HEAD" {
				cause.Level = "write"
			}
			cause.Module = apiModuleOfPath(tenantID, path)
		}
	}
	if cause.Total < APIKeyFailureThreshold {
		return apiFailureCause{}, false
	}
	return cause, true
}

// apiModuleOfPath names the module a public path reached: records by document
// type, actions by their module segment.
func apiModuleOfPath(tenantID, path string) string {
	const records, actions = "/api/public/v1/records/", "/api/public/v1/actions/"
	switch {
	case strings.HasPrefix(path, records):
		doctype := strings.SplitN(strings.TrimPrefix(path, records), "/", 2)[0]
		m, _ := ModuleForDoctype(tenantID, doctype)
		return m
	case strings.HasPrefix(path, actions):
		return strings.SplitN(strings.TrimPrefix(path, actions), "/", 2)[0]
	}
	return ""
}

func apiModuleName(tenantID, role, module string) string {
	mods, err := APIAccessModules(tenantID, role)
	if err == nil {
		for _, m := range mods {
			if m.Key == module {
				return m.Name
			}
		}
	}
	return module
}

// APIKeyAttentionList is what the screen's banner shows: keys that are failing,
// about to end, in a rotation overlap, or whose owner left.
func APIKeyAttentionList(tenantID string, actor APIKeyActor, all bool) ([]APIAttention, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	keys, err := ListAPIKeys(tenantID, actor.UserID, all, "")
	if err != nil {
		return nil, err
	}
	held, _ := APIAccessModules(tenantID, actor.Role)
	mayHold := func(module, level string) bool {
		for _, m := range held {
			if m.Key == module && m.InPlan {
				return m.CanChange || (level == "read" && m.CanView)
			}
		}
		return false
	}
	now := time.Now()
	out := []APIAttention{}
	for _, k := range keys {
		// Status is apiKeyStatus's computed value; a key still in use is one
		// of active, expiring, rotating or owner_inactive (ask 226: this once
		// compared with "Active", so the banner and Home tile never showed).
		switch k.Status {
		case "revoked", "expired", "pending_claim":
			continue
		}
		if cause, failing := apiRecentFailure(tenantID, schema, k.ID); failing {
			item := APIAttention{KeyID: k.ID, Name: k.Name, Kind: "failing", Module: cause.Module, Level: cause.Level}
			msg := fmt.Sprintf("%s was refused %d times in %d minutes", k.Name, cause.Total, int(APIKeyFailureWindow.Minutes()))
			if cause.Code != "" {
				msg += ": " + apiRefusalCause(cause.Code)
			}
			item.Message = msg
			if cause.Code == APIKeyModuleNotOnKey && cause.Module != "" {
				item.ModuleName = apiModuleName(tenantID, actor.Role, cause.Module)
				item.CanFix = mayHold(cause.Module, cause.Level)
			}
			out = append(out, item)
		}
		if k.ExpiresAt != nil {
			days := int(k.ExpiresAt.Sub(now).Hours() / 24)
			if k.ExpiresAt.After(now) && days <= 30 {
				exp := *k.ExpiresAt
				out = append(out, APIAttention{KeyID: k.ID, Name: k.Name, Kind: "expiring", ExpiresAt: &exp,
					Message: fmt.Sprintf("%s ends in %d day(s). Rotate it before then.", k.Name, days)})
			}
		}
		if k.OldKeyEndsAt != nil && k.OldKeyEndsAt.After(now) {
			exp := *k.OldKeyEndsAt
			out = append(out, APIAttention{KeyID: k.ID, Name: k.Name, Kind: "rotating", ExpiresAt: &exp,
				Message: fmt.Sprintf("%s: the old secret stops working on %s.", k.Name, k.OldKeyEndsAt.In(BusinessLocation()).Format("2 Jan 15:04"))})
		}
		if k.OwnerID != "" && !k.OwnerActive {
			out = append(out, APIAttention{KeyID: k.ID, Name: k.Name, Kind: "owner_inactive",
				Message: fmt.Sprintf("%s: its owner %s is no longer active, so every call is refused. Take it over.", k.Name, k.OwnerName)})
		}
	}
	return out, nil
}

// apiRefusalCause is the plain cause for a refusal code.
func apiRefusalCause(code string) string {
	switch code {
	case APIKeyOwnerInactive:
		return "the key's owner is no longer active"
	case APIKeyModuleNotInPlan:
		return "the module is not in the plan"
	case APIKeyModuleNotOnKey:
		return "the key does not have that module"
	case APIKeyOwnerLostModule:
		return "the owner no longer holds that module"
	case APIKeyDoctypeNotShown, APIKeyDoctypeNotOnKey:
		return "the document type is not given to the key"
	case APIKeyNoApproval:
		return "the key may not approve or reject"
	case APIKeyIPNotAllowed:
		return "the caller's address is not on the key's list"
	case APIKeyExpired:
		return "the key has expired"
	case APIKeyOldSecretEnded:
		return "the old secret has ended"
	case APIKeyStoreNotAllowed:
		return "the owner may not work at that store"
	case APIKeyPendingClaim:
		return "the developer link was not opened"
	}
	return code
}

// FixAPIKey widens a key by one module, only to a level the asker holds.
func FixAPIKey(tenantID string, actor APIKeyActor, credentialID, module, level string) (*APIKeyRow, error) {
	module, level = strings.TrimSpace(module), strings.TrimSpace(level)
	if module == "" || (level != "read" && level != "write") {
		return nil, &ValidationError{Code: APIKeyInvalidRequest, Message: "name a module and a level (read or write)"}
	}
	mods, err := APIAccessModules(tenantID, actor.Role)
	if err != nil {
		return nil, err
	}
	allowed := false
	for _, m := range mods {
		if m.Key == module && m.InPlan && (m.CanChange || (level == "read" && m.CanView)) {
			allowed = true
		}
	}
	if !allowed {
		return nil, &ValidationError{Code: APIKeyActionNotAllowed, Message: "you can give a key only what you hold yourself"}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	row, err := loadAPIKeyRow(schema, credentialID)
	if err != nil {
		return nil, err
	}
	g := APIKeyGrant{Modules: map[string]string{}, Doctypes: row.Doctypes, CanApprove: row.CanApprove}
	for k, v := range row.Modules {
		g.Modules[k] = v
	}
	if g.Modules[module] != "write" {
		g.Modules[module] = level
	}
	if err := UpdateAPIKeyModules(tenantID, credentialID, g); err != nil {
		return nil, err
	}
	return GetAPIKey(tenantID, derefStr(row.OwnerUserID), true, credentialID)
}

// ---- reminders and the failure alert (the sweep) ---------------------------------

var apiReminderSteps = []int{30, 7, 1}

// apiKeyEmails are the owner's address and every Admin's.
func apiKeyEmails(schema, ownerID string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(e string) {
		e = strings.TrimSpace(e)
		if e != "" && !seen[strings.ToLower(e)] {
			seen[strings.ToLower(e)] = true
			out = append(out, e)
		}
	}
	var email string
	if ownerID != "" {
		_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(email,'') FROM %s.users WHERE id = $1 OR username = $1 LIMIT 1`, schema), ownerID).Scan(&email)
		add(email)
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT COALESCE(email,''), COALESCE(role,'') FROM %s.users WHERE COALESCE(status,'Active') = 'Active'`, schema))
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var e, role string
			if rows.Scan(&e, &role) == nil && HasFullAccess(role) {
				add(e)
			}
		}
	}
	return out
}

func queueAPIKeyEmail(schema string, to []string, subject, body string) {
	if len(to) == 0 {
		return
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return
	}
	defer tx.Rollback()
	for _, addr := range to {
		if err := PublishEvent(tx, schema, "email.send", map[string]interface{}{"to": addr, "subject": subject, "body": body}); err != nil {
			log.Printf("[APIKEY-WATCH] could not queue an email: %v", err)
			return
		}
	}
	_ = tx.Commit()
}

// SweepAPIKeyWatch sends the expiry reminders (30, 7 and 1 days before) and the
// failure alert for one company. Each is sent once: the key remembers the
// smallest reminder step sent and the time of the last failure alert. Returns
// how many it sent.
func SweepAPIKeyWatch(tenantID string, now time.Time) (int, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id::text, name, COALESCE(owner_user_id,''), expires_at, expiry_reminded_days, failure_alerted_at
		FROM %s.api_credentials WHERE status='Active'`, schema))
	if err != nil {
		return 0, err
	}
	type keyRow struct {
		id, name, owner string
		expires         sql.NullTime
		reminded        sql.NullInt64
		alerted         sql.NullTime
	}
	var keys []keyRow
	for rows.Next() {
		var k keyRow
		if err := rows.Scan(&k.id, &k.name, &k.owner, &k.expires, &k.reminded, &k.alerted); err == nil {
			keys = append(keys, k)
		}
	}
	rows.Close()
	sent := 0
	for _, k := range keys {
		if k.expires.Valid && k.expires.Time.After(now) {
			days := k.expires.Time.Sub(now).Hours() / 24
			step := 0
			for _, s := range apiReminderSteps { // 30, 7, 1: the smallest step the key is inside
				if days <= float64(s) {
					step = s
				}
			}
			if step > 0 && (!k.reminded.Valid || int(k.reminded.Int64) > step) {
				subject := fmt.Sprintf("API key %q ends in %d day(s)", k.name, step)
				body := fmt.Sprintf("The API key %q ends on %s. Rotate it on the API access screen before then, so the apps using it keep working.", k.name, k.expires.Time.In(BusinessLocation()).Format("2 Jan 2006"))
				queueAPIKeyEmail(schema, apiKeyEmails(schema, k.owner), subject, body)
				_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.api_credentials SET expiry_reminded_days=$2 WHERE id=$1`, schema), k.id, step)
				sent++
			}
		}
		if cause, failing := apiRecentFailure(tenantID, schema, k.id); failing {
			if !k.alerted.Valid || now.Sub(k.alerted.Time) >= time.Hour {
				subject := fmt.Sprintf("API key %q is being refused", k.name)
				body := fmt.Sprintf("The API key %q was refused %d times in the last %d minutes: %s. Open the API access screen to fix it.",
					k.name, cause.Total, int(APIKeyFailureWindow.Minutes()), apiRefusalCause(cause.Code))
				queueAPIKeyEmail(schema, apiKeyEmails(schema, k.owner), subject, body)
				_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.api_credentials SET failure_alerted_at=CURRENT_TIMESTAMP WHERE id=$1`, schema), k.id)
				LogAuditEvent(tenantID, "system", "API_KEY_FAILING", "ALERT", fmt.Sprintf("credential=%s refused=%d cause=%s", k.id, cause.Total, cause.Code))
				sent++
			}
		}
	}
	return sent, nil
}

// StartAPIKeyWatchWorker runs the sweep for every company; the scheduler leader
// only, so two servers never both send.
func StartAPIKeyWatchWorker(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if !IsSchedulerLeader() || db.DB == nil {
					continue
				}
				schemas, err := sweepTenantSchemas()
				if err != nil {
					log.Printf("[APIKEY-WATCH] could not list companies: %v", err)
					continue
				}
				for _, schema := range schemas {
					tenantID, err := tenantIDForSchema(schema)
					if err != nil {
						continue
					}
					if n, err := SweepAPIKeyWatch(tenantID, time.Now()); err != nil {
						log.Printf("[APIKEY-WATCH] %s: %v", schema, err)
					} else if n > 0 {
						log.Printf("[APIKEY-WATCH] %s: sent %d alert(s)", schema, n)
					}
				}
			}
		}
	}()
}

// ---- a 5xx on the public API raises an ops alert (205.22) -------------------------

var apiFaultAlerts = struct {
	sync.Mutex
	last map[string]time.Time
}{last: map[string]time.Time{}}

// AlertPublicAPIFault tells operations about a server fault on the public API,
// at most once in ten minutes for each company.
func AlertPublicAPIFault(tenantID, path string, status int, correlationID string) {
	apiFaultAlerts.Lock()
	if last, seen := apiFaultAlerts.last[tenantID]; seen && time.Since(last) < 10*time.Minute {
		apiFaultAlerts.Unlock()
		return
	}
	apiFaultAlerts.last[tenantID] = time.Now()
	apiFaultAlerts.Unlock()
	SendOpsAlert("high", "public-api", fmt.Sprintf("company %s: %s answered %d (correlation %s)", tenantID, path, status, correlationID))
}

// SortedDocumentEvents lists the event verbs in a stable order for the spec.
func SortedDocumentEvents() []string {
	out := append([]string(nil), APIDocumentEvents...)
	sort.Strings(out)
	return out
}

// DocumentEventsWanted is the cheap gate in front of the document handler: it
// is true only when some webhook subscription exists in the company.
func DocumentEventsWanted(tenantID string) bool {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return false
	}
	return anyActiveWebhookSubscription(schema)
}

// DocumentStatusOf is a document's stored status, "" when there is none.
func DocumentStatusOf(tenantID, doctype, id string) string {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil || id == "" {
		return ""
	}
	var status string
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT status FROM %s.documents WHERE doctype=$1 AND id=$2 AND deleted_at IS NULL`, schema), doctype, id).Scan(&status)
	return status
}

// DocumentByID finds a document by its id alone (ids are unique across document
// types): its type and stored status, "" when there is none. A module's own
// route (cancel, submit, approve) uses it to send the same event the document
// route does (226.1).
func DocumentByID(tenantID, id string) (doctype, status string) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil || id == "" || len(id) > 200 {
		return "", ""
	}
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT doctype, COALESCE(status,'') FROM %s.documents WHERE id=$1 AND deleted_at IS NULL LIMIT 1`, schema), id).Scan(&doctype, &status)
	return doctype, status
}

// EventForStatusChange names the event a save made: a new record is created;
// a status that moved to Submitted, Cancelled or Approved says so; anything
// else is an update.
func EventForStatusChange(existed bool, priorStatus, newStatus string) string {
	if !existed {
		return "created"
	}
	if newStatus != priorStatus {
		switch newStatus {
		case "Submitted":
			return "submitted"
		case "Cancelled", "Canceled":
			return "cancelled"
		case "Approved":
			return "approved"
		}
	}
	return "updated"
}

// GetReportDefinition finds one report in the catalogue by its id.
func GetReportDefinition(id string) (ReportDefinition, bool) {
	def, ok := reportRegistry[id]
	if !ok {
		return ReportDefinition{}, false
	}
	def.Number = reportNumbers[id]
	return def, true
}
