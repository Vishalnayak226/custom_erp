package engines

import "strings"

// Stage 130.19 (55.21): connector setup guidance. For each platform: the
// credentials it reads, where on the platform each one is found, and what to
// set up there; CheckChannelSetup says whether a channel's saved credentials
// would connect (the connector's own check - no call to the platform) and how
// its last syncs went. It never returns a credential's value.

// ConnectorSetupField is one credential a platform needs.
type ConnectorSetupField struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Where string `json:"where"`
	Set   bool   `json:"set"`
}

type connectorSetupGuide struct {
	fields []ConnectorSetupField
	steps  []string
}

var partnerSetup = connectorSetupGuide{
	fields: []ConnectorSetupField{
		{Key: "base_url", Label: "API address", Where: "from the channel's partner integration team"},
		{Key: "access_token", Label: "Access token", Where: "issued by the channel under your seller contract"},
		{Key: "orders_path", Label: "Orders path", Where: "the contract's order endpoint"},
		{Key: "inventory_path", Label: "Inventory path", Where: "the contract's stock endpoint"},
		{Key: "catalogue_path", Label: "Catalogue path", Where: "the contract's listing endpoint"},
		{Key: "status_path", Label: "Status path", Where: "the contract's shipment status endpoint"},
	},
	steps: []string{"These channels connect only under a private seller contract - ask the channel's integration team for the API address, token and endpoint paths.",
		"Map the channel's SKUs to your items (SKU exceptions below), and set which store ships for it."},
}

var connectorSetupGuides = map[string]connectorSetupGuide{
	"Shopify": {fields: []ConnectorSetupField{
		{Key: "shop_domain", Label: "Shop domain", Where: "your-store.myshopify.com - the address of your Shopify admin"},
		{Key: "access_token", Label: "Admin API access token", Where: "Shopify admin → Settings → Apps and sales channels → Develop apps → your app → API credentials"},
	}, steps: []string{"Create a custom app in Shopify and give it read_orders, read_products, write_products, write_inventory and write_fulfillments.",
		"Install the app, copy the Admin API access token once, and save it here with the shop domain."}},
	"Amazon": {fields: []ConnectorSetupField{
		{Key: "seller_id", Label: "Seller (merchant) ID", Where: "Seller Central → Settings → Account Info → Merchant Token"},
		{Key: "marketplace_id", Label: "Marketplace ID", Where: "A21TJRUUN4KGV for amazon.in"},
		{Key: "refresh_token", Label: "Refresh token", Where: "Seller Central → Apps and Services → Develop Apps → authorise your app"},
		{Key: "lwa_client_id", Label: "LWA client ID", Where: "the app's Login with Amazon credentials"},
		{Key: "lwa_client_secret", Label: "LWA client secret", Where: "the app's Login with Amazon credentials"},
	}, steps: []string{"Register as an SP-API developer in Seller Central and create a private app with Orders, Listings and Feeds roles.",
		"Authorise the app for your seller account to get the refresh token; an access token alone works too but expires within the hour."}},
	"Flipkart": {fields: []ConnectorSetupField{
		{Key: "app_id", Label: "Application ID", Where: "Flipkart Seller Hub → Developer access → self-access application"},
		{Key: "app_secret", Label: "Application secret", Where: "shown with the application ID"},
	}, steps: []string{"Create a self-access application in Seller Hub's developer access, then save its ID and secret here.", "Set the store that ships Flipkart orders - Flipkart needs one."}},
	"WooCommerce": {fields: []ConnectorSetupField{
		{Key: "base_url", Label: "Store address", Where: "your site's address, e.g. https://shop.example.com"},
		{Key: "consumer_key", Label: "Consumer key", Where: "WooCommerce → Settings → Advanced → REST API → Add key (Read/Write)"},
		{Key: "consumer_secret", Label: "Consumer secret", Where: "shown once with the consumer key"},
	}, steps: []string{"Create a REST API key with Read/Write permission for a shop manager user, and save it here with the store address (https)."}},
	"Magento": {fields: []ConnectorSetupField{
		{Key: "base_url", Label: "Store address", Where: "your Magento store's address"},
		{Key: "access_token", Label: "Integration access token", Where: "Magento admin → System → Integrations → Add New Integration → Activate"},
	}, steps: []string{"Create an integration with access to Catalog and Sales, activate it, and copy the access token."}},
	"BigCommerce": {fields: []ConnectorSetupField{
		{Key: "store_hash", Label: "Store hash", Where: "the part after /stores/ in your API path"},
		{Key: "access_token", Label: "Access token", Where: "BigCommerce → Settings → API → Store-level API accounts"},
	}, steps: []string{"Create a store-level API account with Products and Orders modify scopes, and save its token with the store hash."}},
}

func init() {
	for _, code := range []string{"Myntra", "Meesho", "Ajio", "Nykaa", "Blinkit", "Zepto", "SwiggyInstamart"} {
		connectorSetupGuides[code] = partnerSetup
	}
}

// CheckChannelSetup reports whether a channel is ready to connect, and what
// to do if not.
func CheckChannelSetup(tenantID, channel string) (map[string]interface{}, error) {
	cfg, err := LoadChannelRuntimeConfig(tenantID, channel)
	if err != nil {
		return nil, err
	}
	out := map[string]interface{}{"channel": channel, "platform": cfg.Platform, "ready": false}
	guide, known := connectorSetupGuides[cfg.Platform]
	cred, _ := getChannelCredential(tenantID, channel)
	fields := make([]ConnectorSetupField, 0, len(guide.fields))
	var missing []string
	for _, f := range guide.fields {
		f.Set = strings.TrimSpace(cred[f.Key]) != ""
		if !f.Set {
			missing = append(missing, f.Label)
		}
		fields = append(fields, f)
	}
	out["fields"], out["steps"], out["missing"] = fields, guide.steps, missing
	connector, err := ResolveOmnichannelConnector(cfg.Platform)
	switch {
	case err != nil:
		out["problem"] = "this platform has no connector in this ERP yet"
	case len(cred) == 0:
		out["problem"] = "no credentials are saved for this channel"
	default:
		if verr := connector.ValidateCredentials(cred); verr != nil {
			out["problem"] = verr.Error()
		} else {
			out["ready"], out["missing"] = true, nil // alternatives (e.g. a token instead of LWA) are fine
		}
	}
	if !known && out["problem"] == nil {
		out["note"] = "no setup guide is written for this platform; its connector's own check passed"
	}
	if health, err := GetConnectorHealth(tenantID); err == nil {
		for _, h := range health {
			if h.Channel == channel {
				out["last_status"], out["last_error"], out["last_sync_at"], out["failures_24h"] = h.LastStatus, h.LastError, h.LastSyncAt, h.Failures24h
			}
		}
	}
	return out, nil
}
