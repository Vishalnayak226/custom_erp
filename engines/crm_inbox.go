package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Stage 101.5.3 - the WhatsApp inbox (the blueprint's M33), and the provider
// callbacks every sender reports through.
//
// A customer's reply lands in one conversation per phone, linked to the
// customer and, when there is one, a ticket. The conversation is "auto"
// until a person takes it over (then automatic replies stop) and hands it
// back. Replies by staff go through the send gate: WhatsApp allows free text
// only within 24 hours of the customer's last message, after that a template.
// A reply is never consent; "STOP" withdraws marketing on that channel.

// CRMConversation is one thread.
type CRMConversation struct {
	ID            string                   `json:"id"`
	Channel       string                   `json:"channel"`
	Phone         string                   `json:"phone"`
	CustomerID    string                   `json:"customer_id,omitempty"`
	CustomerName  string                   `json:"customer_name,omitempty"`
	Mode          string                   `json:"mode"`
	AssignedTo    string                   `json:"assigned_to,omitempty"`
	TicketID      string                   `json:"ticket_id,omitempty"`
	Status        string                   `json:"status"`
	Unread        int                      `json:"unread"`
	LastInboundAt *time.Time               `json:"last_inbound_at,omitempty"`
	WindowOpen    bool                     `json:"window_open"`
	UpdatedAt     time.Time                `json:"updated_at"`
	Messages      []CRMConversationMessage `json:"messages,omitempty"`
}

// CRMConversationMessage is one message in a thread.
type CRMConversationMessage struct {
	ID        int64     `json:"id"`
	Direction string    `json:"direction"`
	Body      string    `json:"body"`
	SentBy    string    `json:"sent_by,omitempty"`
	SendID    string    `json:"send_id,omitempty"`
	Status    string    `json:"status,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

func init() {
	RegisterSetting(SettingDefinition{Key: "crm.whatsapp_auto_reply", Module: "CRM", Label: "Automatic WhatsApp reply", Type: SettingTypeString, Default: "",
		Description: "Sent once a day to a customer who writes in while nobody has taken the conversation over. Blank sends nothing. Say when someone will answer."})
}

// crmRecordInbound records a customer's message and returns its conversation.
func crmRecordInbound(schema, channel, e164, body, providerRef string) (string, error) {
	e164 = "+" + strings.TrimPrefix(strings.TrimSpace(e164), "+")
	hash := crmHash("phone", e164)
	customers, _ := crmCustomersByPhone(schema, e164)
	customerID := ""
	if len(customers) > 0 {
		customerID = customers[0]
	}
	var convID, mode string
	var lastAuto sql.NullTime
	err := db.DB.QueryRow(fmt.Sprintf(`INSERT INTO %s.crm_conversations (id, channel, phone_hash, phone, customer_id, last_inbound_at, unread)
		VALUES ($1, $2, $3, $4, NULLIF($5, ''), LOCALTIMESTAMP, 1)
		ON CONFLICT (channel, phone_hash) DO UPDATE SET last_inbound_at = LOCALTIMESTAMP, unread = %s.crm_conversations.unread + 1, updated_at = LOCALTIMESTAMP,
		  status = 'Open', phone = EXCLUDED.phone, customer_id = COALESCE(%s.crm_conversations.customer_id, EXCLUDED.customer_id)
		RETURNING id, mode, last_outbound_at`, schema, schema, schema), NewDocID("CNV"), channel, hash, e164, customerID).Scan(&convID, &mode, &lastAuto)
	if err != nil {
		return "", err
	}
	res, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.crm_conversation_messages (conversation_id, direction, body, provider_ref) VALUES ($1, 'in', $2, NULLIF($3, ''))
		ON CONFLICT (provider_ref) WHERE provider_ref IS NOT NULL AND direction = 'in' DO NOTHING`, schema), convID, truncateRunes(body, 4000), providerRef)
	if err != nil {
		return "", err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// the provider sent the same message twice: counted once
		_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.crm_conversations SET unread = GREATEST(unread - 1, 0) WHERE id = $1`, schema), convID)
		return convID, nil
	}
	if crmIsStopWord(body) {
		_, _ = crmHandleStop(schema, channel, e164, "Replied "+strings.ToUpper(strings.TrimSpace(body))+" on "+channel)
	}
	if customerID != "" {
		_ = RecordCRMEvent(schema, customerID, CRMEventMessageReply, firstNonEmpty(providerRef, convID+":"+time.Now().Format(time.RFC3339Nano)), map[string]interface{}{"channel": channel})
		if mode == "auto" && !crmIsStopWord(body) {
			if text := strings.TrimSpace(GetSettingStringForSchema(schema, "crm.whatsapp_auto_reply")); text != "" && channel == CRMChannelWhatsApp {
				// once a day per conversation, as a service message
				_, _, _ = crmQueueSend(db.DB, schema, CRMSendRequest{RunKind: "auto_reply", RunID: convID + ":" + time.Now().In(crmLocation(schema)).Format("2006-01-02"),
					CustomerID: customerID, Channel: channel, Purpose: CRMPurposeService, Body: text, ExpiresInHours: 2, CreatedBy: "auto reply"})
			}
		}
	}
	return convID, nil
}

// ListCRMConversations lists threads, the most recent first.
func ListCRMConversations(tenantID, status, assignedTo string) ([]CRMConversation, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT v.id, v.channel, COALESCE(v.phone, ''), COALESCE(v.customer_id, ''), COALESCE(c.data->>'name', ''), v.mode, COALESCE(v.assigned_to, ''),
		COALESCE(v.ticket_id, ''), v.status, v.unread, v.last_inbound_at, v.updated_at, (v.last_inbound_at > LOCALTIMESTAMP - interval '24 hours')
		FROM %s.crm_conversations v LEFT JOIN %s.documents c ON c.id = v.customer_id AND c.doctype = 'Customer'
		WHERE ($1 = '' OR v.status = $1) AND ($2 = '' OR v.assigned_to = $2) ORDER BY v.updated_at DESC LIMIT 200`, schema, schema), status, assignedTo)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CRMConversation{}
	for rows.Next() {
		var c CRMConversation
		var last sql.NullTime
		var open sql.NullBool
		if err := rows.Scan(&c.ID, &c.Channel, &c.Phone, &c.CustomerID, &c.CustomerName, &c.Mode, &c.AssignedTo, &c.TicketID, &c.Status, &c.Unread, &last, &c.UpdatedAt, &open); err != nil {
			return nil, err
		}
		if last.Valid {
			c.LastInboundAt = &last.Time
		}
		c.WindowOpen = open.Bool
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetCRMConversation returns a thread with its messages and marks it read.
func GetCRMConversation(tenantID, role, id string) (*CRMConversation, error) {
	list, err := ListCRMConversations(tenantID, "", "")
	if err != nil {
		return nil, err
	}
	var c *CRMConversation
	for i := range list {
		if list[i].ID == id {
			c = &list[i]
		}
	}
	if c == nil {
		return nil, crmNotFound("conversation", id)
	}
	schema, _ := crmSchema(tenantID)
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT m.id, m.direction, COALESCE(m.body, ''), COALESCE(m.sent_by, ''), COALESCE(m.send_id, ''), COALESCE(s.status, ''), m.created_at
		FROM %s.crm_conversation_messages m LEFT JOIN %s.crm_sends s ON s.id = m.send_id WHERE m.conversation_id = $1 ORDER BY m.id DESC LIMIT 200`, schema, schema), id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	c.Messages = []CRMConversationMessage{}
	for rows.Next() {
		var m CRMConversationMessage
		if err := rows.Scan(&m.ID, &m.Direction, &m.Body, &m.SentBy, &m.SendID, &m.Status, &m.CreatedAt); err != nil {
			return nil, err
		}
		c.Messages = append([]CRMConversationMessage{m}, c.Messages...)
	}
	_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.crm_conversations SET unread = 0 WHERE id = $1`, schema), id)
	if !RoleSeesContacts(tenantID, role) {
		c.Phone = crmMaskPhone(c.Phone)
	}
	return c, rows.Err()
}

// ReplyCRMConversation sends a person's reply (through the gate) and keeps it
// in the thread. Inside the 24-hour window it is free text; outside, pass a
// template.
func ReplyCRMConversation(tenantID, id, text, templateID, actor string) (*CRMSend, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	var customerID, channel string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(customer_id, ''), channel FROM %s.crm_conversations WHERE id = $1`, schema), id).Scan(&customerID, &channel); err != nil {
		return nil, crmNotFound("conversation", id)
	}
	if customerID == "" {
		return nil, crmErr(CRMCodeMessage, "this number is not a customer yet - add them (Customers > New) to reply")
	}
	send, err := SendCRMStaffMessage(tenantID, actor, CRMStaffMessage{CustomerID: customerID, Channel: channel, Purpose: CRMPurposeService, TemplateID: templateID, Text: text})
	if err != nil {
		return nil, err
	}
	body := firstNonEmpty(send.Body, text)
	_, _ = db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.crm_conversation_messages (conversation_id, direction, body, send_id, sent_by) VALUES ($1, 'out', $2, $3, $4)`, schema), id, body, send.ID, actor)
	_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.crm_conversations SET last_outbound_at = LOCALTIMESTAMP, updated_at = LOCALTIMESTAMP,
		mode = 'human', assigned_to = COALESCE(assigned_to, $2) WHERE id = $1`, schema), id, actor)
	return send, nil
}

// SetCRMConversation takes a thread over, hands it back, closes it, or links
// it to a ticket.
func SetCRMConversation(tenantID, id, action, value, actor string) error {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return err
	}
	q := map[string]string{
		"take_over": `UPDATE %s.crm_conversations SET mode = 'human', assigned_to = $2, updated_at = LOCALTIMESTAMP WHERE id = $1`,
		"hand_back": `UPDATE %s.crm_conversations SET mode = 'auto', assigned_to = NULL, updated_at = LOCALTIMESTAMP WHERE id = $1 AND $2::text IS NOT NULL`,
		"close":     `UPDATE %s.crm_conversations SET status = 'Closed', unread = 0, updated_at = LOCALTIMESTAMP WHERE id = $1 AND $2::text IS NOT NULL`,
		"reopen":    `UPDATE %s.crm_conversations SET status = 'Open', updated_at = LOCALTIMESTAMP WHERE id = $1 AND $2::text IS NOT NULL`,
		"ticket":    `UPDATE %s.crm_conversations SET ticket_id = NULLIF($2, ''), updated_at = LOCALTIMESTAMP WHERE id = $1`,
	}[action]
	if q == "" {
		return crmErr(CRMCodeMessage, "unknown action %q", action)
	}
	arg := actor
	if action == "ticket" {
		arg = strings.TrimSpace(value)
	}
	res, err := db.DB.Exec(fmt.Sprintf(q, schema), id, arg)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return crmNotFound("conversation", id)
	}
	return nil
}

// --- provider callbacks ----------------------------------------------------------

// CRMCallbackResult is what a provider callback did.
type CRMCallbackResult struct {
	Statuses  int    `json:"statuses"`
	Messages  int    `json:"messages"`
	Templates int    `json:"templates"`
	Challenge string `json:"-"`
}

// VerifyCRMWhatsAppSubscription answers WhatsApp's webhook check (GET with
// hub.mode, hub.verify_token, hub.challenge).
func VerifyCRMWhatsAppSubscription(tenantID, key string, q url.Values) (string, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return "", crmErr(CRMCodeLink, "unknown callback")
	}
	s, err := crmSenderByCallbackKey(schema, key)
	if err != nil || s.Provider != CRMProviderWhatsApp {
		return "", crmErr(CRMCodeLink, "unknown callback")
	}
	want := firstNonEmpty(s.cfg("verify_token"), key)
	if q.Get("hub.mode") != "subscribe" || q.Get("hub.verify_token") != want {
		return "", crmErr(CRMCodeLink, "the verify token does not match")
	}
	return q.Get("hub.challenge"), nil
}

// HandleCRMWhatsAppWebhook takes WhatsApp's signed callback: delivery
// statuses, customer messages and template approval changes.
func HandleCRMWhatsAppWebhook(tenantID, key string, body []byte, signature string) (*CRMCallbackResult, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, crmErr(CRMCodeLink, "unknown callback")
	}
	s, err := crmSenderByCallbackKey(schema, key)
	if err != nil || s.Provider != CRMProviderWhatsApp {
		return nil, crmErr(CRMCodeLink, "unknown callback")
	}
	if !crmVerifyWhatsAppSignature(s.secret["app_secret"], body, signature) {
		return nil, crmErr(CRMCodeLink, "the signature does not match")
	}
	var payload struct {
		Entry []struct {
			Changes []struct {
				Field string `json:"field"`
				Value struct {
					Statuses []struct {
						ID        string `json:"id"`
						Status    string `json:"status"`
						BizOpaque string `json:"biz_opaque_callback_data"`
						Errors    []struct {
							Title   string `json:"title"`
							Message string `json:"message"`
						} `json:"errors"`
						Pricing *struct {
							Billable bool   `json:"billable"`
							Category string `json:"category"`
						} `json:"pricing"`
					} `json:"statuses"`
					Messages []struct {
						From string `json:"from"`
						ID   string `json:"id"`
						Type string `json:"type"`
						Text struct {
							Body string `json:"body"`
						} `json:"text"`
						Button struct {
							Text string `json:"text"`
						} `json:"button"`
						Interactive struct {
							ButtonReply struct {
								Title string `json:"title"`
							} `json:"button_reply"`
						} `json:"interactive"`
					} `json:"messages"`
					Event                   string `json:"event"`
					MessageTemplateName     string `json:"message_template_name"`
					MessageTemplateLanguage string `json:"message_template_language"`
				} `json:"value"`
			} `json:"changes"`
		} `json:"entry"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, crmErr(CRMCodeLink, "the callback is not JSON")
	}
	out := &CRMCallbackResult{}
	for _, e := range payload.Entry {
		for _, ch := range e.Changes {
			v := ch.Value
			for _, st := range v.Statuses {
				detail := ""
				if len(st.Errors) > 0 {
					detail = firstNonEmpty(st.Errors[0].Message, st.Errors[0].Title)
				}
				if err := crmApplyStatusCallback(schema, st.ID, st.BizOpaque, st.Status, detail, nil, "whatsapp"); err == nil {
					out.Statuses++
				}
			}
			for _, m := range v.Messages {
				text := firstNonEmpty(m.Text.Body, m.Button.Text, m.Interactive.ButtonReply.Title)
				if text == "" {
					text = "[" + firstNonEmpty(m.Type, "message") + "]"
				}
				if _, err := crmRecordInbound(schema, CRMChannelWhatsApp, m.From, text, m.ID); err == nil {
					out.Messages++
				}
			}
			if ch.Field == "message_template_status_update" && v.MessageTemplateName != "" {
				res, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.crm_template_versions v SET wa_status = $3, wa_status_at = NOW() FROM %s.crm_templates t
					WHERE t.id = v.template_id AND v.version = t.current_version AND v.wa_template_name = $1 AND ($2 = '' OR COALESCE(v.wa_language, '') IN ('', $2))`, schema, schema),
					v.MessageTemplateName, v.MessageTemplateLanguage, strings.ToUpper(v.Event))
				if err == nil {
					n, _ := res.RowsAffected()
					out.Templates += int(n)
				}
			}
		}
	}
	return out, nil
}

// HandleCRMProviderCallback takes a delivery report, a bounce or complaint,
// or an inbound SMS from an SMS gateway, a mail service or the client's own
// webhook. The callback URL's key names the sender; the parameter names are
// the sender's own (id_param, status_param ... in its settings), with common
// defaults.
func HandleCRMProviderCallback(tenantID, kind, key string, fields map[string]string) (*CRMCallbackResult, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, crmErr(CRMCodeLink, "unknown callback")
	}
	s, err := crmSenderByCallbackKey(schema, key)
	if err != nil {
		return nil, err
	}
	get := func(cfgKey string, defaults ...string) string {
		names := append([]string{s.cfg(cfgKey)}, defaults...)
		for _, n := range names {
			if n == "" {
				continue
			}
			if v := strings.TrimSpace(fields[n]); v != "" {
				return v
			}
		}
		return ""
	}
	out := &CRMCallbackResult{}
	ref := get("dlr_id_param", "provider_ref", "message_id", "messageId", "id", "sid", "request_id")
	sendID := get("dlr_ref_param", "send_id", "delivery_id", "client_ref", "reference")
	status := get("dlr_status_param", "status", "delivery_status", "event", "type")
	if from, text := get("in_from_param", "from", "sender", "mobile"), get("in_text_param", "text", "message", "body"); from != "" && text != "" && ref == "" {
		if s.Channel == CRMChannelSMS || s.Channel == CRMChannelWhatsApp {
			p := NormalizePhone(from, "IN")
			if tid, err := tenantIDForSchema(schema); err == nil {
				p = NormalizePhone(from, TenantPhoneCountry(tid))
			}
			if p.Valid {
				if _, err := crmRecordInbound(schema, s.Channel, p.E164, text, get("in_id_param", "message_id", "id")); err == nil {
					out.Messages++
				}
			}
		}
		return out, nil
	}
	if ref == "" && sendID == "" {
		return nil, crmErr(CRMCodeLink, "the callback names no message")
	}
	var cost *float64
	if c := get("dlr_cost_param", "cost", "price", "charge"); c != "" {
		if f, err := strconv.ParseFloat(c, 64); err == nil {
			cost = &f
		}
	}
	low := strings.ToLower(status)
	if s.Channel == CRMChannelEmail && (strings.Contains(low, "bounce") || strings.Contains(low, "complain") || strings.Contains(low, "spam")) {
		if id, err := crmFindSend(schema, ref, sendID); err == nil {
			var hash string
			_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(contact_hash, '') FROM %s.crm_sends WHERE id = $1`, schema), id).Scan(&hash)
			reason := "bounced"
			if !strings.Contains(low, "bounce") {
				reason = "complained"
			}
			_ = crmSuppress(db.DB, schema, "email", hash, reason)
			_ = crmRecordOutcome(schema, id, crmSendFailed, ref, "the address "+reason, cost, kind)
			out.Statuses++
		}
		return out, nil
	}
	if status == "" {
		status = "delivered"
	}
	if err := crmApplyStatusCallback(schema, ref, sendID, status, get("dlr_detail_param", "detail", "description", "error"), cost, kind); err != nil {
		return nil, err
	}
	out.Statuses++
	return out, nil
}

// errCRMCallback is returned for an unknown sender key (so handlers can 404).
var errCRMCallback = errors.New("unknown callback")
