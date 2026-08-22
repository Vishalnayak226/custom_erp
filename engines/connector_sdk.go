package engines

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ConnectorDescriptor is the stable, documented surface a channel file must
// declare. Operational code uses capabilities, not platform-name switches.
type ConnectorDescriptor struct {
	Code             string `json:"code"`
	Kind             string `json:"kind"` // marketplace, quick_commerce, webstore
	PullOrders       bool   `json:"pull_orders"`
	PushInventory    bool   `json:"push_inventory"`
	PublishCatalogue bool   `json:"publish_catalogue"`
	PushStatus       bool   `json:"push_status"`
	RequiresLocation bool   `json:"requires_location"`
	NoSplit          bool   `json:"no_split"`
	PrivateContract  bool   `json:"private_contract"`
}

type ConnectorOrderLine struct {
	ChannelSKU string  `json:"channel_sku"`
	Quantity   int     `json:"quantity"`
	UnitPrice  float64 `json:"unit_price"`
}

type ConnectorOrder struct {
	ChannelOrderID  string               `json:"channel_order_id"`
	CustomerName    string               `json:"customer_name"`
	CustomerPhone   string               `json:"customer_phone"`
	ShippingAddress string               `json:"shipping_address"`
	ShippingState   string               `json:"shipping_state"`
	PaymentStatus   string               `json:"payment_status"`
	LocationCode    string               `json:"location_code"`
	Lines           []ConnectorOrderLine `json:"lines"`
}

type ConnectorPullRequest struct {
	UpdatedAfter time.Time `json:"updated_after"`
	Cursor       string    `json:"cursor"`
	Limit        int       `json:"limit"`
}

type ConnectorOrderPage struct {
	Orders     []ConnectorOrder `json:"orders"`
	NextCursor string           `json:"next_cursor"`
}

type ConnectorInventoryUpdate struct {
	ChannelSKU   string `json:"channel_sku"`
	Quantity     int    `json:"quantity"`
	LocationCode string `json:"location_code"`
	ProductID    string `json:"product_id,omitempty"`
}

type ConnectorStatusUpdate struct {
	ChannelOrderID string                `json:"channel_order_id"`
	Status         string                `json:"status"`
	TrackingNumber string                `json:"tracking_number,omitempty"`
	Carrier        string                `json:"carrier,omitempty"`
	Items          []ConnectorStatusLine `json:"items,omitempty"`
}

type ConnectorStatusLine struct {
	OrderItemID string `json:"order_item_id"`
	Quantity    int    `json:"quantity"`
}

// OmnichannelConnector is Stage 35.6's full connector SDK. It deliberately
// embeds the existing ChannelConnector, preserving Stage 16's catalogue queue
// while adding the four missing OMS operations.
type OmnichannelConnector interface {
	ChannelConnector
	Descriptor() ConnectorDescriptor
	ValidateCredentials(map[string]string) error
	PullOrders(context.Context, map[string]string, ConnectorPullRequest) (ConnectorOrderPage, error)
	PushInventory(context.Context, map[string]string, []ConnectorInventoryUpdate) error
	PushOrderStatus(context.Context, map[string]string, ConnectorStatusUpdate) error
	MapError(error) string
}

var (
	omnichannelConnectorMu sync.RWMutex
	omnichannelConnectors  = map[string]OmnichannelConnector{}
)

func normalizeConnectorCode(code string) string { return strings.ToLower(strings.TrimSpace(code)) }

func registerOmnichannelConnector(c OmnichannelConnector) {
	d := c.Descriptor()
	omnichannelConnectorMu.Lock()
	omnichannelConnectors[normalizeConnectorCode(d.Code)] = c
	omnichannelConnectorMu.Unlock()
	// The pre-existing PIM publisher resolves by the Channel.platform spelling.
	registerConnector(d.Code, c)
}

func ResolveOmnichannelConnector(code string) (OmnichannelConnector, error) {
	omnichannelConnectorMu.RLock()
	defer omnichannelConnectorMu.RUnlock()
	c, ok := omnichannelConnectors[normalizeConnectorCode(code)]
	if !ok {
		return nil, fmt.Errorf("channel connector %q is not registered", code)
	}
	return c, nil
}

func ListConnectorDescriptors() []ConnectorDescriptor {
	omnichannelConnectorMu.RLock()
	defer omnichannelConnectorMu.RUnlock()
	out := make([]ConnectorDescriptor, 0, len(omnichannelConnectors))
	for _, c := range omnichannelConnectors {
		out = append(out, c.Descriptor())
	}
	return out
}

func LoadConnectorCredential(tenantID, channelCode, platform string) (map[string]string, error) {
	cred, err := getChannelCredential(tenantID, channelCode)
	if err != nil && !strings.EqualFold(channelCode, platform) {
		cred, err = getChannelCredential(tenantID, platform)
	}
	if err != nil {
		return nil, err
	}
	c, err := ResolveOmnichannelConnector(platform)
	if err != nil {
		return nil, err
	}
	if err := c.ValidateCredentials(cred); err != nil {
		return nil, err
	}
	return cred, nil
}

func connectorJSONRequest(ctx context.Context, platform, method, endpoint string, headers map[string]string, input, output interface{}) error {
	var body []byte
	var err error
	if input != nil {
		body, err = json.Marshal(input)
		if err != nil {
			return err
		}
	}
	if headers == nil {
		headers = map[string]string{}
	}
	if _, ok := headers["Content-Type"]; !ok {
		headers["Content-Type"] = "application/json"
	}
	status, response, err := doConnectorRequest(ctx, 25*time.Second, method, endpoint, headers, body, "channel:"+normalizeConnectorCode(platform))
	if err != nil {
		return err
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return &ConnectorHTTPError{Platform: platform, Status: status, Body: strings.TrimSpace(string(response))}
	}
	if output != nil && len(response) > 0 {
		if err := json.Unmarshal(response, output); err != nil {
			return fmt.Errorf("%s returned invalid JSON: %w", platform, err)
		}
	}
	return nil
}

type ConnectorHTTPError struct {
	Platform string
	Status   int
	Body     string
}

func (e *ConnectorHTTPError) Error() string {
	return fmt.Sprintf("%s returned HTTP %d: %s", e.Platform, e.Status, e.Body)
}

func DefaultConnectorErrorCode(err error) string {
	if err == nil {
		return ""
	}
	var httpErr *ConnectorHTTPError
	if errors.As(err, &httpErr) {
		switch httpErr.Status {
		case 401, 403:
			return "CONN-0224"
		case 429:
			return "CONN-0225"
		}
	}
	return classifyConnectorError(err.Error())
}
