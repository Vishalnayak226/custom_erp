package engines

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"custom_erp/db"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/http"
	"net/smtp"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Stage 101.3.1 - senders (D2): the accounts messages leave by. Plain HTTPS
// and SMTP, no provider library (first principle). One active sender per
// channel; its secrets are encrypted with the channel-credential keyring and
// never returned by the API.
//
//	email_smtp     the client's mail account, or this server's (SMTP_HOST);
//	               List-Unsubscribe and one-click unsubscribe headers
//	sms_http       a DLT-registered SMS gateway, described by its parameter
//	               names (entity ID, header, DLT template ID are checked
//	               before anything is sent)
//	whatsapp_cloud the WhatsApp Business Cloud API (templates, signed callbacks)
//	webhook        the client's own gateway (the 26.12.10 shape, now with the
//	               address, since the CRM must say whom to send to)
//	mock           tests and walk-throughs: accept, reject, time out, deliver
//
// A delivery outcome is evidence-based: Accepted when the provider took it,
// Delivered / Read / Failed when it says so (in its answer or a callback),
// Unknown when the request went out and no answer came back - never resent
// by itself (101.9.3).

// CRM sender providers.
const (
	CRMProviderEmail    = "email_smtp"
	CRMProviderSMS      = "sms_http"
	CRMProviderWhatsApp = "whatsapp_cloud"
	CRMProviderWebhook  = "webhook"
	CRMProviderMock     = "mock"
)

var crmProviderChannels = map[string][]string{
	CRMProviderEmail:    {CRMChannelEmail},
	CRMProviderSMS:      {CRMChannelSMS},
	CRMProviderWhatsApp: {CRMChannelWhatsApp},
	CRMProviderWebhook:  {CRMChannelSMS, CRMChannelWhatsApp, CRMChannelEmail},
	CRMProviderMock:     {CRMChannelSMS, CRMChannelWhatsApp, CRMChannelEmail},
}

// crmSecretFields are the write-only fields per provider.
var crmSecretFields = map[string][]string{
	CRMProviderEmail:    {"password"},
	CRMProviderSMS:      {"auth_value"},
	CRMProviderWhatsApp: {"access_token", "app_secret"},
	CRMProviderWebhook:  {"signing_secret"},
}

// crmProviderURLCheck guards a configured URL (https, public address only);
// a test points it at a no-op to use an httptest server.
var crmProviderURLCheck = validateWebhookURL

var crmHTTPClient = &http.Client{Timeout: 15 * time.Second}

// CRMSender is a sender as the screen sees it.
type CRMSender struct {
	ID          string                 `json:"id"`
	Channel     string                 `json:"channel"`
	Provider    string                 `json:"provider"`
	Name        string                 `json:"name"`
	Status      string                 `json:"status"`
	Config      map[string]interface{} `json:"config"`
	Secrets     map[string]bool        `json:"secrets_set"`
	CallbackURL string                 `json:"callback_url,omitempty"`
	UpdatedAt   time.Time              `json:"updated_at"`
	Problems    []string               `json:"problems,omitempty"`
}

// CRMSenderInput is a save from the screen; a blank secret keeps the stored one.
type CRMSenderInput struct {
	Channel  string                 `json:"channel"`
	Provider string                 `json:"provider"`
	Name     string                 `json:"name"`
	Status   string                 `json:"status"`
	Config   map[string]interface{} `json:"config"`
	Secrets  map[string]string      `json:"secrets"`
}

type crmSenderRow struct {
	CRMSender
	secret      map[string]string
	callbackKey string
}

func (s *crmSenderRow) cfg(key string) string { return crmString(s.Config[key]) }

func crmSenderProblems(provider, channel string, cfg map[string]interface{}, secret map[string]string) []string {
	get := func(k string) string { return crmString(cfg[k]) }
	var p []string
	switch provider {
	case CRMProviderEmail:
		if get("host") == "" && os.Getenv("SMTP_HOST") == "" {
			p = append(p, "no mail server: fill Host, or set SMTP_HOST on the server")
		}
		if get("from_email") == "" && os.Getenv("SMTP_FROM") == "" {
			p = append(p, "From address is missing (it must be a verified sender at your mail provider, with SPF and DKIM set)")
		}
	case CRMProviderSMS:
		if get("url") == "" {
			p = append(p, "the gateway's send URL is missing")
		}
		if get("sender_value") == "" {
			p = append(p, "the DLT sender header (6 letters) is missing")
		}
		if get("entity_value") == "" {
			p = append(p, "the DLT principal entity ID is missing")
		}
	case CRMProviderWhatsApp:
		if get("phone_number_id") == "" {
			p = append(p, "the WhatsApp phone number ID is missing")
		}
		if secret["access_token"] == "" {
			p = append(p, "the access token is missing")
		}
		if secret["app_secret"] == "" {
			p = append(p, "the app secret is missing - callbacks cannot be checked without it")
		}
	case CRMProviderWebhook:
		if get("url") == "" {
			p = append(p, "the webhook URL is missing")
		}
	}
	return p
}

// SaveCRMSender creates (id "") or updates a sender. Making one Active
// switches off the channel's other senders, so one sender is active per channel.
func SaveCRMSender(tenantID, id string, in CRMSenderInput, actor string) (*CRMSender, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	channel := crmChannel(in.Channel)
	provider := strings.TrimSpace(in.Provider)
	allowed, ok := crmProviderChannels[provider]
	if !ok || channel == "" {
		return nil, crmErr(CRMCodeSender, "choose a channel and a provider")
	}
	fits := false
	for _, c := range allowed {
		fits = fits || c == channel
	}
	if !fits {
		return nil, crmErr(CRMCodeSender, "%s cannot send %s", provider, channel)
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = channel + " sender"
	}
	status := "Active"
	if strings.EqualFold(in.Status, "Inactive") {
		status = "Inactive"
	}
	if in.Config == nil {
		in.Config = map[string]interface{}{}
	}
	for _, k := range []string{"url", "status_url", "graph_base"} {
		if u := crmString(in.Config[k]); u != "" {
			if err := crmProviderURLCheck(u); err != nil {
				return nil, crmErr(CRMCodeSender, "%s: %v", k, err)
			}
		}
	}
	secret := map[string]string{}
	var cbKey string
	if id != "" {
		cur, err := crmLoadSender(schema, id)
		if err != nil {
			return nil, err
		}
		if cur.Provider != provider || cur.Channel != channel {
			return nil, crmErr(CRMCodeSender, "a sender's channel and provider cannot change - add a new sender")
		}
		secret, cbKey = cur.secret, cur.callbackKey
	}
	for _, k := range crmSecretFields[provider] {
		if v := strings.TrimSpace(in.Secrets[k]); v != "" {
			secret[k] = v
		}
	}
	if cbKey == "" {
		if cbKey, err = crmToken(18); err != nil {
			return nil, err
		}
	}
	var enc []byte
	if len(secret) > 0 {
		if enc, err = encryptChannelCredential(secret); err != nil {
			return nil, err
		}
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if id == "" {
		id = NewDocID("SNDR")
		_, err = tx.Exec(fmt.Sprintf(`INSERT INTO %s.crm_senders (id, channel, provider, name, config, secret, callback_key, status, created_by) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`, schema),
			id, channel, provider, name, crmJSON(in.Config), enc, cbKey, status, actor)
	} else {
		_, err = tx.Exec(fmt.Sprintf(`UPDATE %s.crm_senders SET name = $2, config = $3, secret = $4, status = $5, callback_key = $6, updated_at = NOW() WHERE id = $1`, schema),
			id, name, crmJSON(in.Config), enc, status, cbKey)
	}
	if err != nil {
		return nil, err
	}
	if status == "Active" {
		if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.crm_senders SET status = 'Inactive', updated_at = NOW() WHERE channel = $1 AND id <> $2 AND status = 'Active'`, schema), channel, id); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	LogAuditEvent(tenantID, actor, "CRM_SENDER_SAVED", "SUCCESS", fmt.Sprintf("%s %s (%s) %s", channel, provider, id, status))
	s, err := crmLoadSender(schema, id)
	if err != nil {
		return nil, err
	}
	return &s.CRMSender, nil
}

func crmLoadSender(schema, id string) (*crmSenderRow, error) {
	var s crmSenderRow
	var cfg, enc []byte
	var cb sql.NullString
	err := db.DB.QueryRow(fmt.Sprintf(`SELECT id, channel, provider, name, status, config, secret, callback_key, updated_at FROM %s.crm_senders WHERE id = $1`, schema), id).
		Scan(&s.ID, &s.Channel, &s.Provider, &s.Name, &s.Status, &cfg, &enc, &cb, &s.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, crmNotFound("sender", id)
	}
	if err != nil {
		return nil, err
	}
	return crmFinishSender(schema, &s, cfg, enc, cb.String)
}

func crmFinishSender(schema string, s *crmSenderRow, cfg, enc []byte, cb string) (*crmSenderRow, error) {
	_ = json.Unmarshal(cfg, &s.Config)
	if s.Config == nil {
		s.Config = map[string]interface{}{}
	}
	s.secret = map[string]string{}
	if len(enc) > 0 {
		sec, err := decryptChannelCredential(enc)
		if err != nil {
			return nil, err
		}
		s.secret = sec
	}
	s.callbackKey = cb
	s.Secrets = map[string]bool{}
	for _, k := range crmSecretFields[s.Provider] {
		s.Secrets[k] = s.secret[k] != ""
	}
	if cb != "" {
		path := map[string]string{CRMProviderWhatsApp: "whatsapp", CRMProviderSMS: "sms", CRMProviderEmail: "email", CRMProviderWebhook: "webhook", CRMProviderMock: "webhook"}[s.Provider]
		tid, _ := tenantIDForSchema(schema)
		base := crmPublicBase(schema)
		s.CallbackURL = fmt.Sprintf("%s/api/v1/crm/hooks/%s?k=%s", base, path, cb)
		if tid != "" && tid != "default" {
			s.CallbackURL += "&tenant_id=" + tid
		}
	}
	s.Problems = crmSenderProblems(s.Provider, s.Channel, s.Config, s.secret)
	return s, nil
}

// ListCRMSenders lists senders.
func ListCRMSenders(tenantID string) ([]CRMSender, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id FROM %s.crm_senders ORDER BY channel, status, created_at`, schema))
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	out := []CRMSender{}
	for _, id := range ids {
		s, err := crmLoadSender(schema, id)
		if err != nil {
			return nil, err
		}
		out = append(out, s.CRMSender)
	}
	return out, nil
}

// crmActiveSender is the channel's active sender, or nil.
func crmActiveSender(schema, channel string) (*crmSenderRow, error) {
	var id string
	err := db.DB.QueryRow(fmt.Sprintf(`SELECT id FROM %s.crm_senders WHERE channel = $1 AND status = 'Active' ORDER BY updated_at DESC LIMIT 1`, schema), channel).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return crmLoadSender(schema, id)
}

// crmSenderByCallbackKey finds the sender a provider callback is for.
func crmSenderByCallbackKey(schema, key string) (*crmSenderRow, error) {
	key = strings.TrimSpace(key)
	if len(key) < 16 {
		return nil, crmErr(CRMCodeLink, "unknown callback")
	}
	var id string
	err := db.DB.QueryRow(fmt.Sprintf(`SELECT id FROM %s.crm_senders WHERE callback_key = $1`, schema), key).Scan(&id)
	if err != nil {
		return nil, crmErr(CRMCodeLink, "unknown callback")
	}
	return crmLoadSender(schema, id)
}

// --- one outgoing message -----------------------------------------------------

type crmDispatch struct {
	SendID         string
	Channel        string
	Purpose        string
	To             string
	Subject        string
	Body           string
	Params         []string
	WATemplate     string
	WALanguage     string
	DLTTemplateID  string
	FreeForm       bool
	UnsubscribeURL string
	FromName       string
}

type crmProviderResult struct {
	State       string // Accepted, Delivered or Failed
	ProviderRef string
	Detail      string
	Cost        *float64
}

// errCRMNotSent: the provider was never reached; safe to try again.
type errCRMNotSent struct{ msg string }

func (e *errCRMNotSent) Error() string { return e.msg }

// errCRMUnknown: the request went out and no answer came back.
type errCRMUnknown struct{ msg string }

func (e *errCRMUnknown) Error() string { return e.msg }

// crmMockHook replaces the mock provider's behaviour in tests.
var crmMockHook func(d crmDispatch) (crmProviderResult, error)

func crmDeliver(ctx context.Context, s *crmSenderRow, d crmDispatch) (crmProviderResult, error) {
	switch s.Provider {
	case CRMProviderMock:
		if crmMockHook != nil {
			return crmMockHook(d)
		}
		switch s.cfg("mode") {
		case "reject":
			return crmProviderResult{State: "Failed", Detail: "the mock provider refused it"}, nil
		case "timeout":
			return crmProviderResult{}, &errCRMUnknown{msg: "the mock provider did not answer"}
		case "deliver":
			return crmProviderResult{State: "Delivered", ProviderRef: "mock-" + d.SendID, Detail: "the mock provider delivered it"}, nil
		}
		return crmProviderResult{State: "Accepted", ProviderRef: "mock-" + d.SendID, Detail: "the mock provider accepted it"}, nil
	case CRMProviderEmail:
		return crmSendEmail(ctx, s, d)
	case CRMProviderSMS:
		return crmSendSMS(ctx, s, d)
	case CRMProviderWhatsApp:
		return crmSendWhatsApp(ctx, s, d)
	case CRMProviderWebhook:
		return crmSendWebhook(ctx, s, d)
	}
	return crmProviderResult{State: "Failed", Detail: "unknown provider " + s.Provider}, nil
}

// crmClassifyHTTPError turns a transport error into not-sent (never reached)
// or unknown (may have arrived).
func crmClassifyHTTPError(err error) error {
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() || errors.Is(err, context.DeadlineExceeded) {
		return &errCRMUnknown{msg: "no answer from the provider: " + err.Error()}
	}
	var op *net.OpError
	if errors.As(err, &op) && op.Op == "dial" {
		return &errCRMNotSent{msg: "provider not reached: " + err.Error()}
	}
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return &errCRMNotSent{msg: "provider not reached: " + err.Error()}
	}
	return &errCRMUnknown{msg: "the request failed part-way: " + err.Error()}
}

func crmDoJSON(ctx context.Context, req *http.Request) (int, map[string]interface{}, []byte, error) {
	resp, err := crmHTTPClient.Do(req.WithContext(ctx))
	if err != nil {
		return 0, nil, nil, crmClassifyHTTPError(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	var m map[string]interface{}
	_ = json.Unmarshal(raw, &m)
	return resp.StatusCode, m, raw, nil
}

// crmJSONPath reads a dotted path ("data.0.id") from decoded JSON.
func crmJSONPath(v interface{}, path string) string {
	if path == "" {
		return ""
	}
	cur := v
	for _, part := range strings.Split(path, ".") {
		switch t := cur.(type) {
		case map[string]interface{}:
			cur = t[part]
		case []interface{}:
			i, err := strconv.Atoi(part)
			if err != nil || i < 0 || i >= len(t) {
				return ""
			}
			cur = t[i]
		default:
			return ""
		}
	}
	return crmString(cur)
}

func crmStatusResult(code int, body map[string]interface{}, raw []byte, refPath string) (crmProviderResult, error) {
	switch {
	case code >= 200 && code < 300:
		r := crmProviderResult{State: "Accepted", Detail: fmt.Sprintf("provider accepted it (HTTP %d)", code)}
		r.ProviderRef = crmJSONPath(body, refPath)
		if r.ProviderRef == "" {
			for _, k := range []string{"message_id", "messageId", "id", "sid", "request_id"} {
				if v := crmString(body[k]); v != "" {
					r.ProviderRef = v
					break
				}
			}
		}
		if st := crmString(body["status"]); st != "" {
			r.State = normalizeProviderDeliveryState(st)
			if r.State == "Failed" {
				r.Detail = "provider status: " + st
			}
		}
		if c := crmString(body["cost"]); c != "" {
			if f, err := strconv.ParseFloat(c, 64); err == nil {
				r.Cost = &f
			}
		}
		return r, nil
	case code == http.StatusRequestTimeout || code == http.StatusTooManyRequests || code >= 500:
		return crmProviderResult{}, &errCRMNotSent{msg: fmt.Sprintf("provider answered HTTP %d - will try again", code)}
	}
	detail := fmt.Sprintf("provider refused it (HTTP %d)", code)
	if len(raw) > 0 {
		detail += ": " + truncateRunes(strings.TrimSpace(string(raw)), 200)
	}
	return crmProviderResult{State: "Failed", Detail: detail}, nil
}

// --- webhook ------------------------------------------------------------------

func crmSendWebhook(ctx context.Context, s *crmSenderRow, d crmDispatch) (crmProviderResult, error) {
	u := s.cfg("url")
	if err := crmProviderURLCheck(u); err != nil {
		return crmProviderResult{State: "Failed", Detail: "webhook URL not allowed: " + err.Error()}, nil
	}
	body, _ := json.Marshal(map[string]interface{}{
		"send_id": d.SendID, "channel": d.Channel, "purpose": d.Purpose, "to": d.To, "subject": d.Subject, "body": d.Body,
		"template": d.WATemplate, "language": d.WALanguage, "params": d.Params, "dlt_template_id": d.DLTTemplateID, "unsubscribe_url": d.UnsubscribeURL,
	})
	req, err := http.NewRequest(http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return crmProviderResult{State: "Failed", Detail: err.Error()}, nil
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CRM-Send-ID", d.SendID)
	if sec := s.secret["signing_secret"]; sec != "" {
		mac := hmac.New(sha256.New, []byte(sec))
		mac.Write(body)
		req.Header.Set("X-CRM-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}
	code, m, raw, err := crmDoJSON(ctx, req)
	if err != nil {
		return crmProviderResult{}, err
	}
	return crmStatusResult(code, m, raw, s.cfg("id_field"))
}

// --- SMS gateway (DLT) ----------------------------------------------------------

func crmSendSMS(ctx context.Context, s *crmSenderRow, d crmDispatch) (crmProviderResult, error) {
	u := s.cfg("url")
	if err := crmProviderURLCheck(u); err != nil {
		return crmProviderResult{State: "Failed", Detail: "gateway URL not allowed: " + err.Error()}, nil
	}
	to := d.To
	switch s.cfg("to_format") {
	case "digits":
		to = strings.TrimPrefix(to, "+")
	case "national":
		if strings.HasPrefix(to, "+91") && len(to) == 13 {
			to = to[3:]
		} else {
			to = strings.TrimPrefix(to, "+")
		}
	}
	params := map[string]string{}
	set := func(nameKey, def, value string) {
		name := firstNonEmpty(s.cfg(nameKey), def)
		if name != "" && value != "" {
			params[name] = value
		}
	}
	set("to_param", "to", to)
	set("message_param", "message", d.Body)
	set("sender_param", "sender", s.cfg("sender_value"))
	set("entity_param", "entity_id", s.cfg("entity_value"))
	set("template_param", "template_id", d.DLTTemplateID)
	set("ref_param", "", d.SendID)
	if extra, ok := s.Config["extra"].(map[string]interface{}); ok {
		for k, v := range extra {
			params[k] = crmString(v)
		}
	}
	var req *http.Request
	var err error
	method := strings.ToUpper(firstNonEmpty(s.cfg("method"), "POST"))
	if method == "GET" {
		q := url.Values{}
		for k, v := range params {
			q.Set(k, v)
		}
		sep := "?"
		if strings.Contains(u, "?") {
			sep = "&"
		}
		req, err = http.NewRequest(http.MethodGet, u+sep+q.Encode(), nil)
	} else if s.cfg("format") == "form" {
		q := url.Values{}
		for k, v := range params {
			q.Set(k, v)
		}
		req, err = http.NewRequest(http.MethodPost, u, strings.NewReader(q.Encode()))
		if req != nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
	} else {
		b, _ := json.Marshal(params)
		req, err = http.NewRequest(http.MethodPost, u, bytes.NewReader(b))
		if req != nil {
			req.Header.Set("Content-Type", "application/json")
		}
	}
	if err != nil {
		return crmProviderResult{State: "Failed", Detail: err.Error()}, nil
	}
	if h := s.cfg("auth_header"); h != "" && s.secret["auth_value"] != "" {
		req.Header.Set(h, s.secret["auth_value"])
	}
	code, m, raw, err := crmDoJSON(ctx, req)
	if err != nil {
		return crmProviderResult{}, err
	}
	r, err := crmStatusResult(code, m, raw, s.cfg("id_field"))
	if err == nil && r.State != "Failed" && s.cfg("success_field") != "" {
		want := firstNonEmpty(s.cfg("success_value"), "success")
		if got := crmJSONPath(m, s.cfg("success_field")); !strings.EqualFold(got, want) {
			r.State, r.Detail = "Failed", fmt.Sprintf("gateway said %q", truncateRunes(string(raw), 200))
		}
	}
	return r, err
}

// --- WhatsApp Business Cloud API -------------------------------------------------

func crmSendWhatsApp(ctx context.Context, s *crmSenderRow, d crmDispatch) (crmProviderResult, error) {
	base := strings.TrimRight(firstNonEmpty(s.cfg("graph_base"), "https://graph.facebook.com"), "/")
	ver := firstNonEmpty(s.cfg("api_version"), "v21.0")
	endpoint := fmt.Sprintf("%s/%s/%s/messages", base, ver, url.PathEscape(s.cfg("phone_number_id")))
	msg := map[string]interface{}{"messaging_product": "whatsapp", "recipient_type": "individual", "to": strings.TrimPrefix(d.To, "+"),
		"biz_opaque_callback_data": d.SendID}
	if d.FreeForm || d.WATemplate == "" {
		msg["type"] = "text"
		msg["text"] = map[string]interface{}{"body": d.Body, "preview_url": true}
	} else {
		params := []map[string]string{}
		for _, p := range d.Params {
			params = append(params, map[string]string{"type": "text", "text": p})
		}
		tpl := map[string]interface{}{"name": d.WATemplate, "language": map[string]string{"code": firstNonEmpty(d.WALanguage, "en")}}
		if len(params) > 0 {
			tpl["components"] = []map[string]interface{}{{"type": "body", "parameters": params}}
		}
		msg["type"] = "template"
		msg["template"] = tpl
	}
	body, _ := json.Marshal(msg)
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return crmProviderResult{State: "Failed", Detail: err.Error()}, nil
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.secret["access_token"])
	code, m, raw, err := crmDoJSON(ctx, req)
	if err != nil {
		return crmProviderResult{}, err
	}
	if code >= 200 && code < 300 {
		return crmProviderResult{State: "Accepted", ProviderRef: crmJSONPath(m, "messages.0.id"), Detail: "WhatsApp accepted it"}, nil
	}
	r, err := crmStatusResult(code, m, raw, "")
	if msgErr := crmJSONPath(m, "error.message"); msgErr != "" && r.State == "Failed" {
		r.Detail = "WhatsApp refused it: " + msgErr
	}
	return r, err
}

// crmVerifyWhatsAppSignature checks X-Hub-Signature-256 against the app secret.
func crmVerifyWhatsAppSignature(appSecret string, body []byte, header string) bool {
	if appSecret == "" || !strings.HasPrefix(header, "sha256=") {
		return false
	}
	mac := hmac.New(sha256.New, []byte(appSecret))
	mac.Write(body)
	want := mac.Sum(nil)
	got, err := hex.DecodeString(strings.TrimPrefix(header, "sha256="))
	return err == nil && hmac.Equal(want, got)
}

// SyncCRMWhatsAppTemplate asks WhatsApp whether a template version is
// approved and records the answer.
func SyncCRMWhatsAppTemplate(tenantID, templateID string) (*CRMTemplate, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	t, err := crmGetTemplate(schema, templateID)
	if err != nil {
		return nil, err
	}
	if t.Channel != CRMChannelWhatsApp || t.Current == nil || t.Current.WATemplateName == "" {
		return nil, crmErr(CRMCodeTemplate, "only a WhatsApp template with its WhatsApp template name can be synced")
	}
	s, err := crmActiveSender(schema, CRMChannelWhatsApp)
	if err != nil {
		return nil, err
	}
	if s == nil || s.Provider != CRMProviderWhatsApp {
		return nil, crmErr(CRMCodeSender, "set up the WhatsApp sender first (CRM > Settings > Senders)")
	}
	if s.cfg("waba_id") == "" {
		return nil, crmErr(CRMCodeSender, "the WhatsApp sender needs its business account (WABA) ID to read template approvals")
	}
	if !ExternalSideEffectsEnabled() {
		return nil, crmErr(CRMCodeSender, "this server does not call outside services (external side effects are off), so WhatsApp cannot be asked")
	}
	base := strings.TrimRight(firstNonEmpty(s.cfg("graph_base"), "https://graph.facebook.com"), "/")
	endpoint := fmt.Sprintf("%s/%s/%s/message_templates?name=%s", base, firstNonEmpty(s.cfg("api_version"), "v21.0"),
		url.PathEscape(s.cfg("waba_id")), url.QueryEscape(t.Current.WATemplateName))
	req, _ := http.NewRequest(http.MethodGet, endpoint, nil)
	req.Header.Set("Authorization", "Bearer "+s.secret["access_token"])
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	code, m, raw, err := crmDoJSON(ctx, req)
	if err != nil {
		return nil, crmErr(CRMCodeSender, "WhatsApp could not be reached: %v", err)
	}
	if code < 200 || code >= 300 {
		return nil, crmErr(CRMCodeSender, "WhatsApp answered HTTP %d: %s", code, truncateRunes(string(raw), 200))
	}
	status := "NOT FOUND"
	if list, ok := m["data"].([]interface{}); ok {
		for _, it := range list {
			row, _ := it.(map[string]interface{})
			if crmString(row["name"]) == t.Current.WATemplateName && (t.Current.WALanguage == "" || strings.EqualFold(crmString(row["language"]), t.Current.WALanguage)) {
				status = strings.ToUpper(crmString(row["status"]))
				break
			}
		}
	}
	if err := setCRMTemplateWAStatus(schema, t.ID, t.CurrentVersion, status); err != nil {
		return nil, err
	}
	return crmGetTemplate(schema, t.ID)
}

// --- email ----------------------------------------------------------------------

func crmSendEmail(ctx context.Context, s *crmSenderRow, d crmDispatch) (crmProviderResult, error) {
	host := firstNonEmpty(s.cfg("host"), os.Getenv("SMTP_HOST"))
	port := firstNonEmpty(s.cfg("port"), os.Getenv("SMTP_PORT"), "587")
	user := s.cfg("username")
	pass := s.secret["password"]
	if s.cfg("host") == "" {
		user, pass = os.Getenv("SMTP_USER"), os.Getenv("SMTP_PASSWORD")
	}
	from := firstNonEmpty(s.cfg("from_email"), os.Getenv("SMTP_FROM"))
	if host == "" || from == "" {
		return crmProviderResult{State: "Failed", Detail: "the email sender has no mail server or From address"}, nil
	}
	fromName := firstNonEmpty(s.cfg("from_name"), d.FromName)
	msg := crmBuildEmail(from, fromName, s.cfg("reply_to"), d)
	addr := net.JoinHostPort(host, port)
	conn, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return crmProviderResult{}, &errCRMNotSent{msg: "mail server not reached: " + err.Error()}
	}
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		conn.Close()
		return crmProviderResult{}, &errCRMNotSent{msg: "mail server did not greet: " + err.Error()}
	}
	defer c.Close()
	if ok, _ := c.Extension("STARTTLS"); ok {
		if err := c.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}); err != nil {
			return crmProviderResult{}, &errCRMNotSent{msg: "mail server TLS failed: " + err.Error()}
		}
	}
	if user != "" {
		if err := c.Auth(smtp.PlainAuth("", user, pass, host)); err != nil {
			return crmProviderResult{State: "Failed", Detail: "the mail server refused the sign-in: " + err.Error()}, nil
		}
	}
	if err := c.Mail(from); err != nil {
		return crmProviderResult{State: "Failed", Detail: "the mail server refused the sender: " + err.Error()}, nil
	}
	if err := c.Rcpt(d.To); err != nil {
		return crmProviderResult{State: "Failed", Detail: "the mail server refused the address: " + err.Error()}, nil
	}
	w, err := c.Data()
	if err != nil {
		return crmProviderResult{State: "Failed", Detail: "the mail server refused the message: " + err.Error()}, nil
	}
	if _, err := w.Write(msg); err != nil {
		return crmProviderResult{}, &errCRMUnknown{msg: "the connection broke while sending: " + err.Error()}
	}
	if err := w.Close(); err != nil {
		// After DATA the server may or may not have queued it.
		return crmProviderResult{}, &errCRMUnknown{msg: "no confirmation from the mail server: " + err.Error()}
	}
	_ = c.Quit()
	return crmProviderResult{State: "Accepted", ProviderRef: "<" + d.SendID + "@crm>", Detail: "the mail server accepted it"}, nil
}

func crmBuildEmail(from, fromName, replyTo string, d crmDispatch) []byte {
	var b bytes.Buffer
	hdr := func(k, v string) { b.WriteString(k + ": " + v + "\r\n") }
	clean := func(s string) string { return strings.NewReplacer("\r", " ", "\n", " ").Replace(s) }
	if fromName != "" {
		hdr("From", mime.QEncoding.Encode("utf-8", clean(fromName))+" <"+from+">")
	} else {
		hdr("From", from)
	}
	hdr("To", d.To)
	if replyTo != "" {
		hdr("Reply-To", clean(replyTo))
	}
	hdr("Subject", mime.QEncoding.Encode("utf-8", clean(d.Subject)))
	hdr("Date", time.Now().Format(time.RFC1123Z))
	hdr("Message-ID", "<"+d.SendID+"@crm>")
	hdr("MIME-Version", "1.0")
	hdr("X-CRM-Send-ID", d.SendID)
	if d.UnsubscribeURL != "" {
		hdr("List-Unsubscribe", "<"+d.UnsubscribeURL+">")
		hdr("List-Unsubscribe-Post", "List-Unsubscribe=One-Click")
	}
	hdr("Content-Type", "text/plain; charset=utf-8")
	hdr("Content-Transfer-Encoding", "quoted-printable")
	b.WriteString("\r\n")
	qp := quotedprintable.NewWriter(&b)
	_, _ = qp.Write([]byte(strings.ReplaceAll(d.Body, "\n", "\r\n")))
	_ = qp.Close()
	return b.Bytes()
}
