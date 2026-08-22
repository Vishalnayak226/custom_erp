package engines

import (
	"context"
	"fmt"
	"time"
)

// legacyWebstoreConnector makes the pre-Stage-35 Shopify, BigCommerce and
// Magento catalogue connectors visible through the SDK without claiming
// operations they do not implement. Their established webhook/poll intake
// remains in place; new scheduled work is gated by these capability flags.
type legacyWebstoreConnector struct {
	code     string
	delegate ChannelConnector
}

func init() {
	registerOmnichannelConnector(legacyWebstoreConnector{code: "Shopify", delegate: shopifyConnector{}})
	registerOmnichannelConnector(legacyWebstoreConnector{code: "BigCommerce", delegate: bigCommerceConnector{}})
	registerOmnichannelConnector(legacyWebstoreConnector{code: "Magento", delegate: magentoConnector{}})
	registerOmnichannelConnector(legacyWebstoreConnector{code: "AdobeCommerce", delegate: magentoConnector{}})
}

func (c legacyWebstoreConnector) Descriptor() ConnectorDescriptor {
	return ConnectorDescriptor{Code: c.code, Kind: "webstore", PublishCatalogue: true}
}
func (c legacyWebstoreConnector) RateLimit() (int, time.Duration) {
	return c.delegate.RateLimit()
}
func (c legacyWebstoreConnector) PublishProduct(ctx context.Context, cred map[string]string, payload ChannelProductPayload) (string, error) {
	return c.delegate.PublishProduct(ctx, cred, payload)
}
func (c legacyWebstoreConnector) ValidateCredentials(cred map[string]string) error {
	required := []string{"access_token"}
	switch c.code {
	case "Shopify":
		required = append(required, "shop_domain")
	case "BigCommerce":
		required = append(required, "store_hash")
	default:
		required = append(required, "base_url")
	}
	for _, field := range required {
		if cred[field] == "" {
			return fmt.Errorf("%s credential missing %s", c.code, field)
		}
	}
	return nil
}
func (c legacyWebstoreConnector) PullOrders(context.Context, map[string]string, ConnectorPullRequest) (ConnectorOrderPage, error) {
	return ConnectorOrderPage{}, fmt.Errorf("%s order intake uses its existing webhook/poll adapter", c.code)
}
func (c legacyWebstoreConnector) PushInventory(context.Context, map[string]string, []ConnectorInventoryUpdate) error {
	return fmt.Errorf("%s inventory push is not implemented", c.code)
}
func (c legacyWebstoreConnector) PushOrderStatus(context.Context, map[string]string, ConnectorStatusUpdate) error {
	return fmt.Errorf("%s order-status push is not implemented", c.code)
}
func (c legacyWebstoreConnector) MapError(err error) string { return DefaultConnectorErrorCode(err) }
