package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"custom_erp/engines"
)

// Stage 99: the Shopify setup page and its actions (engines/shopify_sync.go,
// connector_shopify_sdk.go). Credentials, settings and webhook registration
// are an administrator's; the day-to-day actions (publish, send prices or
// stock, pull orders, payouts, the mismatch report) are open to the OMS. The
// webhook endpoint is public and proves itself with Shopify's HMAC over the
// body, signed with the channel's own webhook secret (fail closed).

func shopifyChannelParam(r *http.Request) string { return r.PathValue("channel") }

func shopifyCtx(r *http.Request) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), 4*time.Minute)
}

// handleShopifyOverview is the setup page's state (GET).
func handleShopifyOverview(w http.ResponseWriter, r *http.Request) {
	ov, err := engines.GetShopifyOverview(r.Header.Get("Resolved-Tenant-ID"), shopifyChannelParam(r))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	ov.Settings.WebhookURL = shopifyOr(ov.Settings.WebhookURL, shopifyCallbackURL(r, shopifyChannelParam(r)))
	_ = json.NewEncoder(w).Encode(ov)
}

func shopifyOr(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

// shopifyCallbackURL is where Shopify sends this client's webhooks: the
// address the administrator is using, so a client's own hostname routes to it.
func shopifyCallbackURL(r *http.Request, channel string) string {
	scheme := "https"
	if fp := r.Header.Get("X-Forwarded-Proto"); fp != "" {
		scheme = strings.Split(fp, ",")[0]
	} else if r.TLS == nil && (strings.HasPrefix(r.Host, "localhost") || strings.HasPrefix(r.Host, "127.")) {
		scheme = "http"
	}
	q := url.Values{"tenant_id": {r.Header.Get("Resolved-Tenant-ID")}, "channel": {channel}}
	return fmt.Sprintf("%s://%s/api/v1/integration/shopify/webhook?%s", scheme, r.Host, q.Encode())
}

// handleShopifyCredentials saves the shop address, token and webhook secret
// (admin). A field left blank keeps what was saved, so the token need not be
// typed again to change the secret.
func handleShopifyCredentials(w http.ResponseWriter, r *http.Request) {
	if !requireHRAdmin(w, r, r.Header.Get("Resolved-Role")) {
		return
	}
	tenantID, channel := r.Header.Get("Resolved-Tenant-ID"), shopifyChannelParam(r)
	if _, _, err := engines.LoadShopifyChannel(tenantID, channel); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	var req struct {
		ShopDomain    string `json:"shop_domain"`
		AccessToken   string `json:"access_token"`
		WebhookSecret string `json:"webhook_secret"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request payload")
		return
	}
	fields, _ := engines.ChannelCredentialFields(tenantID, channel)
	if fields == nil {
		fields = map[string]string{}
	}
	domain := strings.ToLower(strings.TrimSpace(req.ShopDomain))
	domain = strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(domain, "https://"), "http://"), "/")
	if domain != "" {
		if !strings.HasSuffix(domain, ".myshopify.com") || strings.ContainsAny(domain, "/ ?#") {
			writeAPIError(w, r, "CONN-0994", "the shop address is the store's myshopify.com address, e.g. yourstore.myshopify.com")
			return
		}
		fields["shop_domain"] = domain
	}
	for k, v := range map[string]string{"access_token": req.AccessToken, "webhook_secret": req.WebhookSecret} {
		if v = strings.TrimSpace(v); v != "" {
			fields[k] = v
		}
	}
	if fields["shop_domain"] == "" || fields["access_token"] == "" {
		writeAPIError(w, r, "CONN-0994", "both the shop address and the admin API access token are needed")
		return
	}
	if err := engines.SaveChannelCredential(tenantID, channel, fields); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to save the credentials")
		return
	}
	engines.LogAuditEvent(tenantID, r.Header.Get("Resolved-Username"), "INTEGRATION", "SHOPIFY_CREDENTIALS_SAVED", "channel "+channel+" shop "+fields["shop_domain"])
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "saved"})
}

// handleShopifySettings saves locations, price list and the sync switches (admin).
func handleShopifySettings(w http.ResponseWriter, r *http.Request) {
	if !requireHRAdmin(w, r, r.Header.Get("Resolved-Role")) {
		return
	}
	var s engines.ShopifySettings
	if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request payload")
		return
	}
	if err := engines.SaveShopifySettings(r.Header.Get("Resolved-Tenant-ID"), shopifyChannelParam(r), s, r.Header.Get("Resolved-Username")); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "saved"})
}

// handleShopifyAction runs one of the page's buttons: check, webhooks
// (admin), publish, link, prices, stock, pull, payouts.
func handleShopifyAction(w http.ResponseWriter, r *http.Request) {
	tenantID, channel, action := r.Header.Get("Resolved-Tenant-ID"), shopifyChannelParam(r), r.PathValue("action")
	user := r.Header.Get("Resolved-Username")
	var req struct {
		Codes []string `json:"codes"`
		SKUs  []string `json:"skus"`
		Full  bool     `json:"full"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	ctx, cancel := shopifyCtx(r)
	defer cancel()
	var out interface{}
	var err error
	switch action {
	case "check":
		var cred map[string]string
		if cred, err = engines.LoadConnectorCredential(tenantID, channel, "Shopify"); err == nil {
			var chk engines.ShopifyCheck
			if chk, err = engines.CheckShopifyChannel(ctx, tenantID, channel, cred); err == nil {
				out = chk
			}
		}
	case "webhooks":
		if !requireHRAdmin(w, r, r.Header.Get("Resolved-Role")) {
			return
		}
		var topics []string
		topics, err = engines.RegisterShopifyWebhooks(ctx, tenantID, channel, shopifyCallbackURL(r, channel), user)
		out = map[string]interface{}{"topics": topics, "callback": shopifyCallbackURL(r, channel)}
	case "publish":
		out, err = engines.PublishToShopify(ctx, tenantID, channel, req.Codes, user)
	case "link":
		var linked, unmatched int
		linked, unmatched, err = engines.LinkShopifyBySKU(ctx, tenantID, channel, user)
		out = map[string]int{"linked": linked, "unmatched": unmatched}
	case "prices":
		var n int
		n, err = engines.PushShopifyPrices(ctx, tenantID, channel, req.Full)
		out = map[string]int{"sent": n}
	case "stock":
		var n int
		n, err = engines.PushShopifyStock(ctx, tenantID, channel, req.Full, req.SKUs)
		out = map[string]int{"sent": n}
	case "pull":
		var p, f int
		p, f, err = engines.PullShopifyOrders(ctx, tenantID, channel)
		out = map[string]int{"processed": p, "failed": f}
	case "payouts":
		var n int
		n, err = engines.SyncShopifyPayouts(ctx, tenantID, channel, user)
		out = map[string]int{"lines": n}
	default:
		writeAPIErrorGeneric(w, r, http.StatusNotFound, "Unknown Shopify action")
		return
	}
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(out)
}

// handleShopifyLinks is the mapping list (GET).
func handleShopifyLinks(w http.ResponseWriter, r *http.Request) {
	links, err := engines.ListShopifyLinks(r.Header.Get("Resolved-Tenant-ID"), shopifyChannelParam(r))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to list the Shopify links")
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"links": links})
}

// handleShopifyMismatch is ERP vs Shopify stock per item and location (GET).
func handleShopifyMismatch(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := shopifyCtx(r)
	defer cancel()
	rows, err := engines.ShopifyStockMismatch(ctx, r.Header.Get("Resolved-Tenant-ID"), shopifyChannelParam(r))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"rows": rows})
}

// handleChannelChangeDone clears a Channel changes queue row (99.5.2).
func handleChannelChangeDone(w http.ResponseWriter, r *http.Request) {
	if err := engines.ClearChannelChange(r.Header.Get("Resolved-Tenant-ID"), r.PathValue("id"), r.Header.Get("Resolved-Username")); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "done"})
}

// handleShopifyWebhook takes Shopify's webhooks for one channel of the
// client the address names (public). Without the channel's webhook secret
// every call is refused; a forged or altered body fails the HMAC.
func handleShopifyWebhook(w http.ResponseWriter, r *http.Request) {
	tenantID, channel := r.Header.Get("Resolved-Tenant-ID"), r.URL.Query().Get("channel")
	body, err := io.ReadAll(io.LimitReader(r.Body, 5<<20))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusBadRequest, "Unreadable body")
		return
	}
	// 134.3: the current secret or, for a week after a change, the previous one.
	if !engines.VerifyWebhookHMACAny(body, r.Header.Get("X-Shopify-Hmac-Sha256"), "", "base64", engines.GetChannelWebhookSecrets(tenantID, channel)...) {
		writeAPIErrorGeneric(w, r, http.StatusUnauthorized, "Invalid webhook signature")
		return
	}
	// A repeat of a delivery already handled (same X-Shopify-Webhook-Id) is
	// answered OK and not processed again.
	source, eventID := "shopify:"+channel, r.Header.Get("X-Shopify-Webhook-Id")
	if dup, err := engines.ClaimInboundWebhook(tenantID, source, eventID, body); err == nil && dup {
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "result": "duplicate"})
		return
	}
	ctx, cancel := shopifyCtx(r)
	defer cancel()
	result, err := engines.HandleShopifyWebhook(ctx, tenantID, channel, r.Header.Get("X-Shopify-Topic"), body)
	if err != nil {
		engines.ReleaseInboundWebhook(tenantID, source, eventID, body) // Shopify's retry is processed
		// Shopify retries a failed delivery; the 15-minute pull is the backstop.
		engines.LogSystemError(tenantID, r.Header.Get("Resolved-Correlation-ID"), "WARN", r.URL.Path, fmt.Sprintf("shopify %s: %v", r.Header.Get("X-Shopify-Topic"), err), "")
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "result": result})
}
