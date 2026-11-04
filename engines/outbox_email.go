package engines

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"log"
	"mime"
	"mime/multipart"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"os"
	"strings"
)

// Stage 146.5.6: the outbox used to log an email and call it delivered. This
// is the one place that sends it: the same mail account the document email
// uses (SMTP_HOST / SMTP_PORT / SMTP_FROM / SMTP_USER / SMTP_PASSWORD). With
// no mail account it sends nothing and says "Not sent" - in the event log and
// in the feed run - instead of pretending.

// OutboxEmail is one email with at most one attachment.
type OutboxEmail struct {
	To, Subject, Body      string
	AttachName, AttachType string
	Attach                 []byte
}

// Email delivery statuses, as shown in the event log and the feed runs.
const (
	EmailSent      = "Sent"
	EmailSimulated = "Simulated"
	EmailNotSent   = "Not sent"
	EmailFailed    = "Failed"
)

// maxOutboxAttachBytes keeps one feed email a size a mail server will take.
const maxOutboxAttachBytes = 5 << 20

// SendOutboxEmail sends m and returns the status and a short note. A client
// that is a sandbox, or has outside calls switched off, gets "Simulated":
// nothing leaves the server.
func SendOutboxEmail(tenantID string, m OutboxEmail) (status, note string) {
	recipients, err := MailRecipients(m.To)
	if err != nil {
		return EmailFailed, err.Error()
	}
	if len(m.Attach) > maxOutboxAttachBytes {
		return EmailFailed, "the file is larger than 5 MB - download it instead"
	}
	host := os.Getenv("SMTP_HOST")
	if host == "" {
		log.Printf("[OUTBOX] [EMAIL] not sent to %s: SMTP_HOST is not set", maskEmailAddress(recipients[0]))
		return EmailNotSent, "mail is not set up on this server (SMTP_HOST)"
	}
	if sandbox, _ := IsSandboxTenant(tenantID); sandbox {
		return EmailSimulated, "sandbox client: nothing was sent"
	}
	if ok, reason := OutsideCallsAllowedForTenant(tenantID); !ok {
		return EmailSimulated, firstNonEmpty(reason, "outside calls are off") + ": nothing was sent"
	}
	port := os.Getenv("SMTP_PORT")
	if port == "" {
		port = "587"
	}
	from := os.Getenv("SMTP_FROM")
	if from == "" {
		from = "no-reply@custom-erp.local"
	}
	msg, err := buildOutboxMessage(from, recipients, m)
	if err != nil {
		return EmailFailed, err.Error()
	}
	var auth smtp.Auth
	if user := os.Getenv("SMTP_USER"); user != "" {
		auth = smtp.PlainAuth("", user, os.Getenv("SMTP_PASSWORD"), host)
	}
	if err := smtp.SendMail(host+":"+port, auth, from, recipients, msg); err != nil {
		return EmailFailed, "the mail server refused the email: " + err.Error()
	}
	return EmailSent, ""
}

func buildOutboxMessage(from string, to []string, m OutboxEmail) ([]byte, error) {
	clean := strings.NewReplacer("\r", " ", "\n", " ")
	subject := strings.TrimSpace(clean.Replace(m.Subject))
	if subject == "" {
		subject = "Message"
	}
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	var head bytes.Buffer
	fmt.Fprintf(&head, "From: %s\r\n", (&mail.Address{Address: from}).String())
	fmt.Fprintf(&head, "To: %s\r\n", strings.Join(to, ", "))
	fmt.Fprintf(&head, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", subject))
	fmt.Fprintf(&head, "MIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=%q\r\n\r\n", w.Boundary())
	text, err := w.CreatePart(textproto.MIMEHeader{"Content-Type": {"text/plain; charset=utf-8"}, "Content-Transfer-Encoding": {"base64"}})
	if err != nil {
		return nil, err
	}
	writeBase64Lines(text, []byte(m.Body))
	if len(m.Attach) > 0 {
		name := strings.NewReplacer(`"`, "", "\\", "", "/", "", "\r", "", "\n", "").Replace(m.AttachName)
		if name == "" {
			name = "attachment"
		}
		ctype := m.AttachType
		if ctype == "" {
			ctype = "application/octet-stream"
		}
		att, err := w.CreatePart(textproto.MIMEHeader{
			"Content-Type":              {mime.FormatMediaType(ctype, map[string]string{"name": name})},
			"Content-Transfer-Encoding": {"base64"},
			"Content-Disposition":       {mime.FormatMediaType("attachment", map[string]string{"filename": name})},
		})
		if err != nil {
			return nil, err
		}
		writeBase64Lines(att, m.Attach)
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return append(head.Bytes(), body.Bytes()...), nil
}

// dispatchOutboxEmailEvent is processOutbox's email handler. ok is false when
// the event is not an email event (or has no recipient), and the caller logs
// it the way it always did.
func dispatchOutboxEmailEvent(tenantID, eventName string, payload map[string]interface{}) (ok bool, status, note string) {
	var m OutboxEmail
	switch eventName {
	case "email.send":
		m = OutboxEmail{To: pimString(payload["to"]), Subject: pimString(payload["subject"]), Body: pimString(payload["body"]),
			AttachName: pimString(payload["attach_name"]), AttachType: pimString(payload["attach_type"])}
		if b64 := pimString(payload["attach_b64"]); b64 != "" {
			raw, err := base64.StdEncoding.DecodeString(b64)
			if err != nil {
				return true, EmailFailed, "the attachment is not valid base64"
			}
			m.Attach = raw
		}
	case "pim.export.scheduled_delivery":
		// A file queued before the export freeze must not leave after it.
		if GetSettingBool(tenantID, SettingFreezeExports) {
			return true, EmailNotSent, "exports are paused for this business"
		}
		m = OutboxEmail{To: pimString(payload["recipient_email"]),
			Subject:    "Feed: " + pimString(payload["export_template"]),
			Body:       fmt.Sprintf("The scheduled feed %s is attached (%v lines).", pimString(payload["export_template"]), payload["line_count"]),
			AttachName: pimString(payload["export_template"]) + ".csv", AttachType: "text/csv", Attach: []byte(pimString(payload["csv"]))}
	default:
		return false, "", ""
	}
	if strings.TrimSpace(m.To) == "" {
		return false, "", ""
	}
	status, note = SendOutboxEmail(tenantID, m)
	return true, status, note
}
