package engines

import (
	"bytes"
	"encoding/base64"
	"errors"
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

// Stage 98.3 (user, 2026-10-19: "download in vendor template and Sent it to
// Vendor. Same for PO, GRN, TO and Sales Bill"; decision D2: the ERP emails it
// itself, and a PDF download is always there to send yourself).
//
// The PDF is the one the browser made from the print (98.2), so what the party
// receives is what the store printed. It goes out through the server's mail
// account - the SMTP_* settings account notices already use - with the
// store's email as Reply-To, so the vendor's answer reaches the store.

// ErrMailNotConfigured means this server has no mail account (SMTP_HOST);
// the caller downloads the PDF instead.
var ErrMailNotConfigured = errors.New("this server has no mail account set up")

// maxDocumentPDFBytes bounds an attachment; a long PO with pictures is a few MB.
const maxDocumentPDFBytes = 15 << 20

// DocumentMail is one document sent by email.
type DocumentMail struct {
	To       string // one or more addresses, comma-separated
	Subject  string
	Message  string
	FileName string
	PDF      []byte
}

// MailRecipients parses the To box: up to ten valid addresses.
func MailRecipients(to string) ([]string, error) {
	list, err := mail.ParseAddressList(strings.ReplaceAll(strings.TrimSpace(to), ";", ","))
	if err != nil || len(list) == 0 {
		return nil, &ValidationError{Code: "GLOBAL-0002", SubFor: "Email", Message: fmt.Sprintf("%q is not an email address - check the To box", strings.TrimSpace(to))}
	}
	if len(list) > 10 {
		return nil, &ValidationError{Code: "GLOBAL-0002", SubFor: "Email", Message: "send to at most 10 addresses at once"}
	}
	out := make([]string, len(list))
	for i, a := range list {
		out[i] = a.Address
	}
	return out, nil
}

// SendDocumentEmail mails a print's PDF. It returns simulated=true when the
// tenant is a sandbox or external side effects are off (nothing leaves the
// server), and ErrMailNotConfigured when there is no mail account.
func SendDocumentEmail(tenantID, userID string, d PrintDocument, m DocumentMail) (recipients []string, simulated bool, err error) {
	recipients, err = MailRecipients(m.To)
	if err != nil {
		return nil, false, err
	}
	if len(m.PDF) == 0 || !bytes.HasPrefix(m.PDF, []byte("%PDF-")) {
		return nil, false, &ValidationError{Code: "GLOBAL-0002", SubFor: "PDF", Message: "the attachment is not a PDF"}
	}
	if len(m.PDF) > maxDocumentPDFBytes {
		return nil, false, &ValidationError{Code: "GLOBAL-0002", SubFor: "PDF", Message: "the PDF is larger than 15 MB - send it in parts or as a download"}
	}
	host := os.Getenv("SMTP_HOST")
	if host == "" {
		return recipients, false, ErrMailNotConfigured
	}
	subject := strings.TrimSpace(strings.NewReplacer("\r", " ", "\n", " ").Replace(m.Subject))
	if subject == "" {
		subject = strings.TrimSpace(d.Title + " " + d.Number)
	}
	audit := fmt.Sprintf("txn=%s number=%s to=%s", d.Transaction, d.Number, strings.Join(recipients, ","))
	ok, reason := OutsideCallsAllowedForTenant(tenantID) // 49.8.7a
	if sandbox, _ := IsSandboxTenant(tenantID); sandbox || !ok {
		log.Printf("[DOCUMENT-MAIL] simulated (%s): %s", firstNonEmpty(reason, "sandbox client"), audit)
		LogAuditEvent(tenantID, userID, "DOCUMENT_SENT", "SIMULATED", audit)
		return recipients, true, nil
	}

	port := os.Getenv("SMTP_PORT")
	if port == "" {
		port = "587"
	}
	from := os.Getenv("SMTP_FROM")
	if from == "" {
		from = "no-reply@custom-erp.local"
	}
	msg, err := buildDocumentMessage(d, from, recipients, subject, m)
	if err != nil {
		return nil, false, err
	}
	var auth smtp.Auth
	if user := os.Getenv("SMTP_USER"); user != "" {
		auth = smtp.PlainAuth("", user, os.Getenv("SMTP_PASSWORD"), host)
	}
	if err := smtp.SendMail(host+":"+port, auth, from, recipients, msg); err != nil {
		LogAuditEvent(tenantID, userID, "DOCUMENT_SENT", "FAILED", audit+" error="+err.Error())
		return nil, false, fmt.Errorf("the mail server refused the email: %v", err)
	}
	LogAuditEvent(tenantID, userID, "DOCUMENT_SENT", "SUCCESS", audit)
	return recipients, false, nil
}

// buildDocumentMessage is the MIME email: the message as text, the PDF
// attached. From shows the store's (or brand's) name; Reply-To is the store's
// email when it has one.
func buildDocumentMessage(d PrintDocument, from string, to []string, subject string, m DocumentMail) ([]byte, error) {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	name := firstNonEmpty(d.Brand["name"], d.Store.Name)
	var head bytes.Buffer
	fmt.Fprintf(&head, "From: %s\r\n", (&mail.Address{Name: name, Address: from}).String())
	fmt.Fprintf(&head, "To: %s\r\n", strings.Join(to, ", "))
	if reply, err := mail.ParseAddress(d.Store.Email); err == nil {
		fmt.Fprintf(&head, "Reply-To: %s\r\n", (&mail.Address{Name: d.Store.Name, Address: reply.Address}).String())
	}
	fmt.Fprintf(&head, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", subject))
	fmt.Fprintf(&head, "MIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=%q\r\n\r\n", w.Boundary())

	text, err := w.CreatePart(textproto.MIMEHeader{"Content-Type": {"text/plain; charset=utf-8"}, "Content-Transfer-Encoding": {"base64"}})
	if err != nil {
		return nil, err
	}
	writeBase64Lines(text, []byte(strings.ReplaceAll(m.Message, "\n", "\r\n")))
	file := exportFileUnsafe.ReplaceAllString(strings.TrimSuffix(firstNonEmpty(m.FileName, ExportFileName(d, "pdf")), ".pdf"), "-") + ".pdf"
	att, err := w.CreatePart(textproto.MIMEHeader{
		"Content-Type":              {fmt.Sprintf("application/pdf; name=%q", file)},
		"Content-Disposition":       {fmt.Sprintf("attachment; filename=%q", file)},
		"Content-Transfer-Encoding": {"base64"},
	})
	if err != nil {
		return nil, err
	}
	writeBase64Lines(att, m.PDF)
	if err := w.Close(); err != nil {
		return nil, err
	}
	return append(head.Bytes(), body.Bytes()...), nil
}

// writeBase64Lines writes base64 in 76-character lines (RFC 2045).
func writeBase64Lines(w interface{ Write([]byte) (int, error) }, b []byte) {
	enc := base64.StdEncoding.EncodeToString(b)
	for len(enc) > 76 {
		_, _ = w.Write([]byte(enc[:76] + "\r\n"))
		enc = enc[76:]
	}
	_, _ = w.Write([]byte(enc + "\r\n"))
}
