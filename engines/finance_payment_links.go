package engines

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"custom_erp/db"
)

// 185.3 (ask 185, owner 2026-11-14): Razorpay payment links for a sales
// invoice.
//
//   - The keys are a channel credential, code "razorpay": key_id, key_secret
//     and webhook_secret, encrypted like every other connector's; a changed
//     webhook secret keeps the old one for a week (134.3).
//   - A link is made for what the invoice still owes, in rupees only, and
//     kept on the invoice (data.payment_link). Asking again for the same
//     amount gives the same link; a different amount cancels the old link
//     first, so one invoice never has two links open.
//   - Razorpay's payment_link.paid webhook books a customer receipt, mode
//     Card: the money waits in clearing account 1101 until Razorpay's payout
//     is matched like a card payout (acquirer settlement). The receipt
//     settles the invoice up to what it owes; anything over is the
//     customer's advance. The payment id is the receipt's idempotency key,
//     so a repeated webhook books nothing twice.
//   - Nothing reaches Razorpay from development, a sandbox or a client whose
//     outside calls are off (doConnectorRequest).
const RazorpayCredentialCode = "razorpay"

// razorpayAPIBase is Razorpay's API; a test points it at a local fake.
var razorpayAPIBase = "https://api.razorpay.com"

const paymentLinkErrorCode = "FIN-1726"

func paymentLinkFail(format string, args ...interface{}) error {
	return &ValidationError{Code: paymentLinkErrorCode, Message: fmt.Sprintf(format, args...)}
}

// PaymentLink is what an invoice keeps of its payment link.
type PaymentLink struct {
	ID        string  `json:"id"`
	URL       string  `json:"url"`
	Amount    float64 `json:"amount"`
	Status    string  `json:"status"`
	Reference string  `json:"reference_id"`
	CreatedAt string  `json:"created_at"`
	CreatedBy string  `json:"created_by"`
	Payment   string  `json:"payment,omitempty"`
	Receipt   string  `json:"receipt,omitempty"`
}

// SaveRazorpayCredential stores the keys an administrator copies from the
// Razorpay dashboard. Every field is required; a blank expiry clears it.
func SaveRazorpayCredential(tenantID, keyID, keySecret, webhookSecret, savedBy, expiresAt string) error {
	keyID, keySecret, webhookSecret = strings.TrimSpace(keyID), strings.TrimSpace(keySecret), strings.TrimSpace(webhookSecret)
	if keyID == "" || keySecret == "" || webhookSecret == "" {
		return paymentLinkFail("the key ID, key secret and webhook secret are all required")
	}
	if !strings.HasPrefix(keyID, "rzp_") {
		return paymentLinkFail("a Razorpay key ID starts with rzp_ (rzp_test_ or rzp_live_)")
	}
	return SaveChannelCredentialBy(tenantID, RazorpayCredentialCode, map[string]string{
		"key_id": keyID, "key_secret": keySecret, "webhook_secret": webhookSecret,
	}, savedBy, &expiresAt)
}

// GetRazorpayCredentials lists the saved keys for the Integrations screen:
// the key ID (Razorpay shows it openly) and only whether the secrets are set.
func GetRazorpayCredentials(tenantID string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	var encrypted []byte
	var active bool
	var owner string
	var lastUsed, expires sql.NullTime
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT encrypted_payload, active, `+credentialMetaColumns+` FROM %s.channel_credentials WHERE channel_code = $1`, schema),
		RazorpayCredentialCode).Scan(&encrypted, &active, &owner, &lastUsed, &expires)
	if err == sql.ErrNoRows {
		return []map[string]interface{}{}, nil
	}
	if err != nil {
		return nil, err
	}
	fields, err := decryptChannelCredential(encrypted)
	if err != nil {
		return nil, err
	}
	set := func(k string) string {
		if fields[k] != "" {
			return "set"
		}
		return ""
	}
	row := map[string]interface{}{
		"code": RazorpayCredentialCode, "key_id": fields["key_id"],
		"key_secret": set("key_secret"), "webhook_secret": set("webhook_secret"), "active": active,
	}
	credentialMetaFields(row, owner, lastUsed, expires)
	return []map[string]interface{}{row}, nil
}

// invoiceOpenPaise is what a posted rupee invoice still owes, worked out as
// noteInvoiceSettlementTx works it out.
func invoiceOpenPaise(tenantID, invoiceID string, data map[string]interface{}) (int64, error) {
	position := documentFXPosition(tenantID, data, "total_amount")
	if position.Foreign {
		return 0, paymentLinkFail("sales invoice %s is in %s - a payment link takes rupees only", invoiceID, position.Currency)
	}
	entries, err := readSettlementEntries(data, "settlements")
	if err != nil {
		return 0, fmt.Errorf("sales invoice %s has an unreadable settlement history: %v", invoiceID, err)
	}
	return int64(position.CarryingAmount)*100 - sumSettledPaise(entries), nil
}

func storedPaymentLink(data map[string]interface{}) (PaymentLink, bool) {
	var link PaymentLink
	raw, ok := data["payment_link"]
	if !ok || raw == nil {
		return link, false
	}
	b, err := json.Marshal(raw)
	if err != nil || json.Unmarshal(b, &link) != nil || link.ID == "" {
		return link, false
	}
	return link, true
}

// razorpayCall makes one call to Razorpay with the client's keys and reads
// Razorpay's own error text into the refusal.
func razorpayCall(ctx context.Context, tenantID, method, path string, payload interface{}) (map[string]interface{}, error) {
	creds, err := getChannelCredential(tenantID, RazorpayCredentialCode)
	if err != nil || creds["key_id"] == "" || creds["key_secret"] == "" {
		return nil, paymentLinkFail("Razorpay is not set up - an administrator saves its keys under Settings → Integrations")
	}
	var body []byte
	if payload != nil {
		if body, err = json.Marshal(payload); err != nil {
			return nil, err
		}
	}
	headers := map[string]string{
		"Authorization": "Basic " + base64.StdEncoding.EncodeToString([]byte(creds["key_id"]+":"+creds["key_secret"])),
		"Content-Type":  "application/json",
	}
	status, resp, err := doConnectorRequest(WithOutsideCallsTenant(ctx, tenantID), 20*time.Second, method, razorpayAPIBase+path, headers, body, "razorpay")
	if err != nil {
		if _, ok := err.(*ValidationError); ok {
			return nil, err
		}
		return nil, paymentLinkFail("Razorpay could not be reached: %v", err)
	}
	var out map[string]interface{}
	_ = json.Unmarshal(resp, &out)
	if status >= 300 {
		reason := fmt.Sprintf("HTTP %d", status)
		if e, ok := out["error"].(map[string]interface{}); ok && strings.TrimSpace(fmt.Sprint(e["description"])) != "" {
			reason = strings.TrimSpace(fmt.Sprint(e["description"]))
		}
		return nil, paymentLinkFail("Razorpay refused: %s", reason)
	}
	if out == nil {
		return nil, paymentLinkFail("Razorpay's answer could not be read")
	}
	return out, nil
}

// CreateInvoicePaymentLink makes (or gives back) the Razorpay link for what
// a posted sales invoice still owes. The invoice row stays locked while
// Razorpay answers, so two clicks cannot make two links.
func CreateInvoicePaymentLink(ctx context.Context, tenantID, invoiceID, actor string) (*PaymentLink, error) {
	invoiceID = strings.TrimSpace(invoiceID)
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var dataStr, status string
	err = tx.QueryRow(fmt.Sprintf(`SELECT data, status FROM %s.documents WHERE doctype = 'SalesInvoice' AND id = $1 AND deleted_at IS NULL FOR UPDATE`, schema),
		invoiceID).Scan(&dataStr, &status)
	if err == sql.ErrNoRows {
		return nil, paymentLinkFail("sales invoice %s was not found", invoiceID)
	}
	if err != nil {
		return nil, err
	}
	if status != "Approved" {
		return nil, paymentLinkFail("sales invoice %s is %s - only a posted invoice with a balance takes a payment link", invoiceID, status)
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return nil, err
	}
	open, err := invoiceOpenPaise(tenantID, invoiceID, data)
	if err != nil {
		return nil, err
	}
	if open <= 0 {
		return nil, paymentLinkFail("sales invoice %s has nothing outstanding", invoiceID)
	}
	if old, ok := storedPaymentLink(data); ok && old.Status == "created" {
		if RupeesToPaise(old.Amount) == open {
			return &old, nil
		}
		// The balance moved since: the old link would take the wrong amount.
		if _, err := razorpayCall(ctx, tenantID, "POST", "/v1/payment_links/"+old.ID+"/cancel", nil); err != nil {
			return nil, paymentLinkFail("the earlier link for %s could not be cancelled, so no new one was made: %v", invoiceID, err)
		}
	}
	count := int(numFromInterface(data["payment_link_count"])) + 1
	suffix := fmt.Sprintf("-%d", count)
	ref := invoiceID
	if len(ref)+len(suffix) > 40 { // Razorpay's limit
		ref = ref[len(ref)+len(suffix)-40:]
	}
	ref += suffix
	number := strField(data, "invoice_number")
	if number == "" {
		number = invoiceID
	}
	out, err := razorpayCall(ctx, tenantID, "POST", "/v1/payment_links", map[string]interface{}{
		"amount": open, "currency": "INR", "accept_partial": false,
		"reference_id": ref, "description": "Invoice " + number,
		"notify":          map[string]bool{"sms": false, "email": false},
		"reminder_enable": false,
		"notes":           map[string]string{"invoice": invoiceID},
	})
	if err != nil {
		return nil, err
	}
	link := PaymentLink{
		ID: strings.TrimSpace(fmt.Sprint(out["id"])), URL: strings.TrimSpace(fmt.Sprint(out["short_url"])),
		Amount: PaiseToRupees(open), Status: "created", Reference: ref,
		CreatedAt: time.Now().UTC().Format(time.RFC3339), CreatedBy: actor,
	}
	if link.ID == "" || link.URL == "" || !strings.HasPrefix(link.ID, "plink_") {
		return nil, paymentLinkFail("Razorpay's answer had no link")
	}
	linkJSON, _ := json.Marshal(link)
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || jsonb_build_object('payment_link', $1::jsonb, 'payment_link_count', $2::int),
		updated_at = CURRENT_TIMESTAMP WHERE doctype = 'SalesInvoice' AND id = $3`, schema), string(linkJSON), count, invoiceID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	LogAuditEvent(tenantID, actor, "razorpay.payment_link", "Created", fmt.Sprintf("%s %s for %.2f", invoiceID, link.ID, link.Amount))
	return &link, nil
}

// razorpayEvent is the part of a Razorpay webhook this reads.
type razorpayEvent struct {
	Event   string `json:"event"`
	Payload struct {
		PaymentLink struct {
			Entity struct {
				ID     string `json:"id"`
				Status string `json:"status"`
			} `json:"entity"`
		} `json:"payment_link"`
		Payment struct {
			Entity struct {
				ID       string `json:"id"`
				Amount   int64  `json:"amount"`
				Currency string `json:"currency"`
				Method   string `json:"method"`
			} `json:"entity"`
		} `json:"payment"`
	} `json:"payload"`
}

// HandleRazorpayWebhook acts on one verified Razorpay webhook: a paid link
// books the receipt; an expired or cancelled one is noted on the invoice.
// Anything else, or a link this client did not make, is answered and left.
func HandleRazorpayWebhook(tenantID string, body []byte) (string, error) {
	var ev razorpayEvent
	if err := json.Unmarshal(body, &ev); err != nil {
		return "", paymentLinkFail("the webhook body could not be read")
	}
	linkID := strings.TrimSpace(ev.Payload.PaymentLink.Entity.ID)
	switch ev.Event {
	case "payment_link.paid", "payment_link.expired", "payment_link.cancelled":
	default:
		return "ignored", nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	var invoiceID, dataStr, status string
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT id, data, status FROM %s.documents WHERE doctype = 'SalesInvoice' AND deleted_at IS NULL AND data->'payment_link'->>'id' = $1`, schema),
		linkID).Scan(&invoiceID, &dataStr, &status)
	if err == sql.ErrNoRows || linkID == "" {
		LogAuditEvent(tenantID, "razorpay", "razorpay.webhook", "Ignored", fmt.Sprintf("%s for link %q, which no invoice holds", ev.Event, linkID))
		return "ignored", nil
	}
	if err != nil {
		return "", err
	}
	if ev.Event != "payment_link.paid" {
		state := strings.TrimPrefix(ev.Event, "payment_link.")
		if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = jsonb_set(data, '{payment_link,status}', to_jsonb($1::text)), updated_at = CURRENT_TIMESTAMP
			WHERE doctype = 'SalesInvoice' AND id = $2 AND data->'payment_link'->>'id' = $3 AND data->'payment_link'->>'status' = 'created'`, schema), state, invoiceID, linkID); err != nil {
			return "", err
		}
		return state, nil
	}

	pay := ev.Payload.Payment.Entity
	if pay.ID == "" || pay.Amount <= 0 {
		return "", paymentLinkFail("the paid webhook for %s names no payment", linkID)
	}
	if pay.Currency != "" && pay.Currency != "INR" {
		return "", paymentLinkFail("payment %s is in %s - a payment link takes rupees only", pay.ID, pay.Currency)
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return "", err
	}
	in := CustomerReceiptInput{
		Customer: strField(data, "customer"), Location: strField(data, "location"),
		Mode: "Card", Reference: pay.ID, Amount: PaiseToRupees(pay.Amount),
		Purpose: "Razorpay payment link " + linkID, IdempotencyKey: "razorpay:" + pay.ID,
	}
	// What a posted invoice still owes is settled; anything over, or all of
	// it when the invoice was settled some other way meanwhile, is the
	// customer's advance.
	if open, err := invoiceOpenPaise(tenantID, invoiceID, data); err == nil && open > 0 && status == "Approved" {
		if open > pay.Amount {
			open = pay.Amount
		}
		in.Invoices = []ReceiptLine{{Ref: invoiceID, Amount: PaiseToRupees(open)}}
	}
	receipt, err := RecordCustomerReceipt(tenantID, "razorpay", in)
	if err != nil {
		return "", err
	}
	b, _ := json.Marshal(map[string]string{"status": "paid", "payment": pay.ID, "receipt": receipt.Number})
	if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = jsonb_set(data, '{payment_link}', COALESCE(data->'payment_link', '{}'::jsonb) || $1::jsonb),
		updated_at = CURRENT_TIMESTAMP WHERE doctype = 'SalesInvoice' AND id = $2 AND data->'payment_link'->>'id' = $3`, schema), string(b), invoiceID, linkID); err != nil {
		return "", err
	}
	return "receipt " + receipt.Number, nil
}
