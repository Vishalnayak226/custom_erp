package engines

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/smtp"
	"net/url"
	"os"
	"strings"
	"time"
)

// 140.12.26: WhatsApp and SMS were not ready on app and nothing said so
// before a campaign's pre-send check. Three things now, without a provider
// account of anyone's being assumed:
//
//   - CRMChannelReadiness: one check per channel - an active sender with
//     nothing missing, a template ready to send (Meta-approved for WhatsApp,
//     with its DLT template ID for SMS), and the test allowlist a test send
//     (or any send from a server that is not production) is held to;
//   - TestCRMSender: "test connection" for one sender - WhatsApp asks Meta for
//     the phone number, email logs in to the mail server, SMS sends one
//     message to a number the user gives (an SMS gateway has no other test),
//     a webhook gets a test event; a sandbox client or a server with outside
//     calls off says so instead of calling;
//   - CRMSenderPresets: MSG91, Gupshup and Twilio filled into the SMS
//     gateway sender's parameter names. Check them against the provider's
//     current API documentation when the account is opened.

// CRMReadinessCheck is one line of the readiness check.
type CRMReadinessCheck struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

// CRMReadiness is a channel's whole answer.
type CRMReadiness struct {
	Channel string              `json:"channel"`
	Ready   bool                `json:"ready"`
	Checks  []CRMReadinessCheck `json:"checks"`
}

// CRMChannelReadiness checks a channel end to end.
func CRMChannelReadiness(tenantID, channel string) (*CRMReadiness, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	out := &CRMReadiness{Channel: channel, Ready: true}
	add := func(name string, ok bool, detail string) {
		out.Checks = append(out.Checks, CRMReadinessCheck{Name: name, OK: ok, Detail: detail})
		out.Ready = out.Ready && ok
	}
	s, err := crmActiveSender(schema, channel)
	switch {
	case err != nil || s == nil:
		add("Sender", false, fmt.Sprintf("no active %s sender - add one under CRM > Senders", channel))
	default:
		problems := crmSenderProblems(s.Provider, channel, s.Config, s.secret)
		add("Sender", len(problems) == 0, firstNonEmpty(strings.Join(problems, "; "), fmt.Sprintf("%s (%s)", s.Name, s.Provider)))
	}
	templates, err := ListCRMTemplates(tenantID, channel)
	if err != nil {
		return nil, err
	}
	ready := 0
	var why string
	for _, t := range templates {
		if t.Ready {
			ready++
		} else if why == "" {
			why = t.Name + ": " + t.NotReady
		}
	}
	detail := fmt.Sprintf("%d of %d template(s) ready", ready, len(templates))
	if ready == 0 && why != "" {
		detail += " - " + why
	}
	add("Template", ready > 0, detail)
	list := strings.TrimSpace(GetSettingStringForSchema(schema, "crm.test_allowlist"))
	n := 0
	for _, a := range strings.Split(list, ",") {
		if strings.TrimSpace(a) != "" {
			n++
		}
	}
	if os.Getenv("ENV") != "production" {
		add("Allowlist", n > 0, fmt.Sprintf("this server is not production, so it sends only to the test allowlist (%d contact(s)) - Settings > CRM", n))
	} else {
		add("Allowlist", true, fmt.Sprintf("test sends reach only the %d allowlisted contact(s)", n))
	}
	return out, nil
}

// CRMSenderTest is what a test connection found.
type CRMSenderTest struct {
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

// TestCRMSender checks one sender against its provider. to is needed only
// for SMS (and is held to the test allowlist like any test send).
func TestCRMSender(ctx context.Context, tenantID, senderID, to string) (*CRMSenderTest, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	s, err := crmLoadSender(schema, senderID)
	if err != nil {
		return nil, err
	}
	if p := crmSenderProblems(s.Provider, s.Channel, s.Config, s.secret); len(p) > 0 {
		return &CRMSenderTest{Detail: "fill these first: " + strings.Join(p, "; ")}, nil
	}
	if s.Provider == CRMProviderMock {
		return &CRMSenderTest{OK: true, Detail: "the mock sender answers without calling anyone"}, nil
	}
	if sb, _ := IsSandboxSchema(schema); sb {
		return &CRMSenderTest{Detail: "a sandbox client does not call real providers"}, nil
	}
	if ok, reason := OutsideCallsAllowed(schema); !ok {
		return &CRMSenderTest{Detail: "not called - " + reason}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	switch s.Provider {
	case CRMProviderWhatsApp:
		base := strings.TrimRight(firstNonEmpty(s.cfg("graph_base"), "https://graph.facebook.com"), "/")
		ver := firstNonEmpty(s.cfg("api_version"), "v19.0")
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/%s/%s?fields=display_phone_number,verified_name", base, ver, url.PathEscape(s.cfg("phone_number_id"))), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+s.secret["access_token"])
		code, body, raw, err := crmDoJSON(ctx, req)
		if err != nil {
			return &CRMSenderTest{Detail: "Meta did not answer: " + err.Error()}, nil
		}
		if code >= 300 {
			return &CRMSenderTest{Detail: fmt.Sprintf("Meta refused (%d): %s", code, truncateRunes(string(raw), 200))}, nil
		}
		return &CRMSenderTest{OK: true, Detail: fmt.Sprintf("connected - %s (%s)", crmJSONPath(body, "display_phone_number"), crmJSONPath(body, "verified_name"))}, nil
	case CRMProviderEmail:
		host := firstNonEmpty(s.cfg("host"), os.Getenv("SMTP_HOST"))
		port := firstNonEmpty(s.cfg("port"), os.Getenv("SMTP_PORT"), "587")
		dial := (&net.Dialer{Timeout: 10 * time.Second}).DialContext
		if s.cfg("host") != "" {
			dial = outboundDial // as crmSendEmail: a sender's own mail server is client-typed; SMTP_HOST is the operator's
		}
		conn, err := dial(ctx, "tcp", net.JoinHostPort(host, port))
		if err != nil {
			return &CRMSenderTest{Detail: "mail server not reachable: " + err.Error()}, nil
		}
		c, err := smtp.NewClient(conn, host)
		if err != nil {
			conn.Close()
			return &CRMSenderTest{Detail: "mail server did not greet: " + err.Error()}, nil
		}
		defer c.Close()
		if ok, _ := c.Extension("STARTTLS"); ok {
			if err := c.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}); err != nil {
				return &CRMSenderTest{Detail: "TLS failed: " + err.Error()}, nil
			}
		}
		if user := s.cfg("username"); user != "" {
			if err := c.Auth(smtp.PlainAuth("", user, s.secret["password"], host)); err != nil {
				return &CRMSenderTest{Detail: "login refused: " + err.Error()}, nil
			}
		}
		_ = c.Quit()
		return &CRMSenderTest{OK: true, Detail: "connected and logged in to " + host}, nil
	case CRMProviderSMS:
		to = strings.TrimSpace(to)
		if to == "" {
			return &CRMSenderTest{Detail: "an SMS gateway is tested by sending one message - give a number on the test allowlist"}, nil
		}
		if !crmAllowlisted(schema, to) {
			return &CRMSenderTest{Detail: to + " is not on the test allowlist (Settings > CRM)"}, nil
		}
		r, err := crmSendSMS(ctx, s, crmDispatch{To: to, Body: "Test message from your ERP - please ignore.", SendID: fmt.Sprintf("test-%d", time.Now().UnixNano()), DLTTemplateID: s.cfg("test_template_id")})
		if err != nil {
			return &CRMSenderTest{Detail: "the gateway did not answer: " + err.Error()}, nil
		}
		return &CRMSenderTest{OK: r.State != "Failed", Detail: firstNonEmpty(r.Detail, r.State)}, nil
	case CRMProviderWebhook:
		r, err := crmSendWebhook(ctx, s, crmDispatch{To: firstNonEmpty(to, "test"), Body: "test", SendID: fmt.Sprintf("test-%d", time.Now().UnixNano())})
		if err != nil {
			return &CRMSenderTest{Detail: "the webhook did not answer: " + err.Error()}, nil
		}
		return &CRMSenderTest{OK: r.State != "Failed", Detail: firstNonEmpty(r.Detail, r.State)}, nil
	}
	return &CRMSenderTest{Detail: "no test for provider " + s.Provider}, nil
}

// CRMSenderPreset fills a sender form for a known provider.
type CRMSenderPreset struct {
	Key        string                 `json:"key"`
	Label      string                 `json:"label"`
	Channel    string                 `json:"channel"`
	Provider   string                 `json:"provider"`
	Config     map[string]interface{} `json:"config"`
	SecretHint string                 `json:"secret_hint"`
	Note       string                 `json:"note"`
}

// CRMSenderPresets are MSG91, Gupshup and Twilio as SMS gateway senders.
func CRMSenderPresets() []CRMSenderPreset {
	return []CRMSenderPreset{
		{Key: "msg91", Label: "MSG91", Channel: CRMChannelSMS, Provider: CRMProviderSMS,
			Config: map[string]interface{}{"url": "https://api.msg91.com/api/sendhttp.php", "method": "GET", "to_param": "mobiles", "to_format": "digits",
				"message_param": "message", "sender_param": "sender", "entity_param": "PE_ID", "template_param": "DLT_TE_ID", "auth_param": "authkey",
				"extra": map[string]interface{}{"route": "4", "country": "91"}},
			SecretHint: "your MSG91 auth key", Note: "DLT: register the sender header and templates on the DLT portal and in MSG91."},
		{Key: "gupshup", Label: "Gupshup (Enterprise SMS)", Channel: CRMChannelSMS, Provider: CRMProviderSMS,
			Config: map[string]interface{}{"url": "https://enterprise.smsgupshup.com/GatewayAPI/rest", "method": "GET", "to_param": "send_to", "to_format": "digits",
				"message_param": "msg", "sender_param": "mask", "entity_param": "principalEntityId", "template_param": "dltTemplateId", "auth_param": "password",
				"extra": map[string]interface{}{"method": "SendMessage", "msg_type": "TEXT", "v": "1.1", "auth_scheme": "plain", "format": "json", "userid": ""}},
			SecretHint: "your Gupshup password", Note: "Fill your Gupshup user ID under extra > userid."},
		{Key: "twilio", Label: "Twilio", Channel: CRMChannelSMS, Provider: CRMProviderSMS,
			Config: map[string]interface{}{"url": "https://api.twilio.com/2010-04-01/Accounts/<AccountSid>/Messages.json", "method": "POST", "format": "form",
				"to_param": "To", "message_param": "Body", "sender_param": "From", "entity_param": "", "template_param": "", "auth_header": "Authorization",
				"id_field": "sid"},
			SecretHint: "Basic <base64 of AccountSid:AuthToken>", Note: "Put your Account SID in the URL. For India, register DLT on the Twilio console; the sender is your Twilio number."},
	}
}
