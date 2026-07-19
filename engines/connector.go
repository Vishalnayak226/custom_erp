package engines

import (
	"context"
	"custom_erp/db"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Real Channel Connector Framework (Stage 16.1). A ChannelConnector
// implementation knows how to push one product to one specific e-commerce
// platform. engines/pim_publish.go's processPublishQueue resolves the
// right connector via resolveConnector(platform) and calls PublishProduct -
// everything about readiness-checking, idempotency, queueing, retry and
// audit logging stays exactly as Stage 15.2 built it; only the "actually
// call the platform" step becomes real instead of a stub.

type ChannelImage struct {
	Filename string
	MIMEType string
	Bytes    []byte
}

type ChannelProductPayload struct {
	ItemCode    string
	Title       string
	Description string
	Attributes  map[string]string // target_field -> value, per ChannelFieldMap
	Images      []ChannelImage
}

type ChannelConnector interface {
	// PublishProduct pushes one product to the platform and returns the
	// platform's own identifier for it (e.g. Shopify's product GID,
	// BigCommerce's numeric product ID, Magento's SKU) for storage in
	// pim_publish_log.external_id.
	PublishProduct(ctx context.Context, cred map[string]string, payload ChannelProductPayload) (externalID string, err error)
	// RateLimit declares this platform's outbound call budget, consulted by
	// engines/connector_http.go's allowConnectorCall before every publish
	// attempt - each connector knows its own platform's documented limit
	// (Shopify's own connector additionally self-corrects using the
	// GraphQL response's cost data; this is the static floor).
	RateLimit() (capacity int, window time.Duration)
}

var connectorRegistry = map[string]ChannelConnector{}

// registerConnector is called from each connector file's own init() -
// standard Go plugin-registration idiom.
func registerConnector(platform string, c ChannelConnector) {
	connectorRegistry[platform] = c
}

// resolveConnector falls back to the stub for "" / "Generic" / any
// unrecognized platform string - zero breaking change to every Stage 15.2
// Channel that has no platform set.
func resolveConnector(platform string) ChannelConnector {
	if c, ok := connectorRegistry[platform]; ok {
		return c
	}
	return stubConnector{}
}

type stubConnector struct{}

func (stubConnector) PublishProduct(ctx context.Context, cred map[string]string, payload ChannelProductPayload) (string, error) {
	return fmt.Sprintf("STUB-%s", payload.ItemCode), nil
}

// RateLimit: the stub makes no real outbound calls, so a generous
// effectively-unlimited budget is fine here.
func (stubConnector) RateLimit() (int, time.Duration) { return 1000, time.Second }

func init() {
	registerConnector("Generic", stubConnector{})
	registerConnector("", stubConnector{})
}

// fetchAttributeValueRaw returns a ProductAttributeValue's raw string value
// (unlike attributeValueFilled in engines/pim.go, which only reports
// whether it's non-empty) - needed here to actually populate a channel
// payload field, not just check completeness.
func fetchAttributeValueRaw(tenantID, itemCode, attributeCode string) (string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	var value string
	id := itemCode + "::" + attributeCode
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(data->>'value', '') FROM %s.documents WHERE doctype = 'ProductAttributeValue' AND id = $1`, schema), id).Scan(&value)
	if err != nil {
		return "", nil
	}
	return value, nil
}

// BuildChannelPayload assembles a ChannelProductPayload from existing PIM
// data - Item core fields, the channel's default-locale Approved
// ProductContent, ProductAttributeValue rows mapped through every
// ChannelFieldMap row for the channel (not just the mandatory ones - this
// builds the actual outbound payload, unlike engines/pim.go's
// fetchChannelMandatoryFields which only checks readiness), and raw media
// bytes for every Active ProductMedia (via the existing GetMediaFile,
// Stage 15.2). Pure translation layer over data that already exists - no
// new source of truth.
func BuildChannelPayload(tenantID, itemCode, channelCode string) (*ChannelProductPayload, error) {
	itemData, _, err := fetchItemDoc(tenantID, itemCode)
	if err != nil {
		return nil, fmt.Errorf("item not found: %v", err)
	}

	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}

	var defaultLocale string
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(data->>'default_locale', 'en') FROM %s.documents WHERE doctype = 'Channel' AND id = $1`, schema), channelCode).Scan(&defaultLocale)
	if defaultLocale == "" {
		defaultLocale = "en"
	}

	var title, shortDesc, longDesc string
	_ = db.DB.QueryRow(fmt.Sprintf(`
		SELECT COALESCE(data->>'title', ''), COALESCE(data->>'short_desc', ''), COALESCE(data->>'long_desc', '')
		FROM %s.documents WHERE doctype = 'ProductContent' AND data->>'product_id' = $1 AND data->>'language' = $2 AND status = 'Approved'
		ORDER BY updated_at DESC LIMIT 1`, schema), itemCode, defaultLocale).Scan(&title, &shortDesc, &longDesc)

	description := longDesc
	if description == "" {
		description = shortDesc
	}

	payload := &ChannelProductPayload{
		ItemCode:    itemCode,
		Title:       title,
		Description: description,
		Attributes:  map[string]string{},
	}

	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT COALESCE(data->>'source_field', ''), COALESCE(data->>'target_field', '') FROM %s.documents
		WHERE doctype = 'ChannelFieldMap' AND data->>'channel' = $1`, schema), channelCode)
	if err != nil {
		return nil, err
	}
	type mapping struct{ source, target string }
	var mappings []mapping
	for rows.Next() {
		var m mapping
		if err := rows.Scan(&m.source, &m.target); err != nil {
			rows.Close()
			return nil, err
		}
		if m.source != "" && m.target != "" {
			mappings = append(mappings, m)
		}
	}
	rows.Close()

	for _, m := range mappings {
		var val string
		if v, exists := itemData[m.source]; exists {
			val = fmt.Sprintf("%v", v)
		} else {
			val, _ = fetchAttributeValueRaw(tenantID, itemCode, m.source)
		}
		if val != "" {
			payload.Attributes[m.target] = val
		}
	}

	mediaList, err := ListMediaForItem(tenantID, itemCode)
	if err != nil {
		return nil, err
	}
	for _, m := range mediaList {
		path, fileType, errFile := GetMediaFile(tenantID, m.ID)
		if errFile != nil {
			continue // skip an unreadable asset rather than failing the whole payload
		}
		fileBytes, errRead := os.ReadFile(path)
		if errRead != nil {
			continue
		}
		payload.Images = append(payload.Images, ChannelImage{
			Filename: filepath.Base(path),
			MIMEType: fileType,
			Bytes:    fileBytes,
		})
	}

	return payload, nil
}
