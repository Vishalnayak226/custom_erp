package engines

import (
	"errors"
	"strings"
	"testing"
)

// Stage 98.3: no mail account means "download instead", a bad address or a
// non-PDF is refused, and the message carries the store as Reply-To and the
// PDF as an attachment.
func TestSendDocumentEmailRulesAndMessage(t *testing.T) {
	d := PrintDocument{Transaction: PrintTxnVendorPO, Title: "Purchase Order", Number: "PO/HO/26-27/00045",
		Brand: map[string]string{"name": "Acme Retail"}, Store: PrintStore{Code: "HO", Name: "Head Office", Email: "ho@acme.example"}}
	pdf := []byte("%PDF-1.4\n%test\n")

	t.Setenv("SMTP_HOST", "")
	if _, _, err := SendDocumentEmail("default", "admin", d, DocumentMail{To: "vendor@example.com", PDF: pdf}); !errors.Is(err, ErrMailNotConfigured) {
		t.Errorf("no SMTP_HOST must answer ErrMailNotConfigured, got %v", err)
	}
	if _, _, err := SendDocumentEmail("default", "admin", d, DocumentMail{To: "not an address", PDF: pdf}); err == nil {
		t.Error("a bad address must be refused")
	}
	if _, _, err := SendDocumentEmail("default", "admin", d, DocumentMail{To: "vendor@example.com", PDF: []byte("hello")}); err == nil {
		t.Error("an attachment that is not a PDF must be refused")
	}
	if to, err := MailRecipients("a@example.com; b@example.com"); err != nil || len(to) != 2 {
		t.Errorf("two addresses separated by ';' = %v, %v", to, err)
	}

	msg, err := buildDocumentMessage(d, "erp@wholeops.test", []string{"vendor@example.com"}, "Purchase Order PO/HO/26-27/00045", DocumentMail{Message: "Dear vendor,\nattached.", PDF: pdf, FileName: "Purchase-Order-PO-HO-26-27-00045.pdf"})
	if err != nil {
		t.Fatal(err)
	}
	s := string(msg)
	for _, want := range []string{`From: "Acme Retail" <erp@wholeops.test>`, "To: vendor@example.com", `Reply-To: "Head Office" <ho@acme.example>`,
		`Content-Disposition: attachment; filename="Purchase-Order-PO-HO-26-27-00045.pdf"`, "JVBERi0"} {
		if !strings.Contains(s, want) {
			t.Errorf("message lacks %q", want)
		}
	}
}
