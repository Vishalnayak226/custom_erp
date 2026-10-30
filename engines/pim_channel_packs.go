package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Channel packs (140.12.22). Guide built the Shopify, Amazon and Myntra feeds
// by hand on app: a channel, ten field maps and three export templates. A
// pack ships that layout per marketplace - the marketplace's own column
// names, where each column comes from, the attributes it needs and their
// value lists - and ApplyChannelPack sets it up on a channel in one step:
// the field maps, any attribute the pack needs that the client does not have
// yet (with its value list), and an export template in the pack's column
// order. Applying again changes nothing that is already there.

// channelPackColumn is one marketplace column. Core is a value every channel
// payload has (item_code, title, description, image_url, channel_category);
// otherwise Field is an Item field, or Attribute names the attribute (by
// code or label) the value comes from.
type channelPackColumn struct {
	Header    string
	Core      string
	Field     string
	Attribute string
	Required  bool
	Values    []string // the marketplace's value list, for a new attribute
}

// ChannelPack is one marketplace's layout.
type ChannelPack struct {
	Platform string
	Columns  []channelPackColumn
}

var channelPacks = map[string]ChannelPack{
	"shopify": {Platform: "Shopify", Columns: []channelPackColumn{
		{Header: "Handle", Core: "item_code"},
		{Header: "Title", Core: "title", Required: true},
		{Header: "Body (HTML)", Core: "description"},
		{Header: "Vendor", Attribute: "brand"},
		{Header: "Product Category", Core: "channel_category"},
		{Header: "Variant SKU", Field: "code"},
		{Header: "Variant Price", Field: "sale_price", Required: true},
		{Header: "Variant Compare At Price", Field: "mrp"},
		{Header: "Variant Barcode", Field: "barcode"},
		{Header: "Option1 Value", Attribute: "size"},
		{Header: "Image Src", Core: "image_url"},
	}},
	"amazon": {Platform: "Amazon", Columns: []channelPackColumn{
		{Header: "item_sku", Core: "item_code"},
		{Header: "item_name", Core: "title", Required: true},
		{Header: "brand_name", Attribute: "brand", Required: true},
		{Header: "external_product_id", Field: "barcode"},
		{Header: "product_description", Core: "description"},
		{Header: "recommended_browse_nodes", Core: "channel_category", Required: true},
		{Header: "standard_price", Field: "sale_price", Required: true},
		{Header: "maximum_retail_price", Field: "mrp", Required: true},
		{Header: "color_name", Attribute: "color", Required: true},
		{Header: "size_name", Attribute: "size", Required: true},
		{Header: "fabric_type", Attribute: "fabric"},
		{Header: "department_name", Attribute: "department", Values: []string{"mens", "womens", "boys", "girls", "unisex-adult", "unisex-child"}},
		{Header: "country_of_origin", Field: "country_of_origin"},
		{Header: "main_image_url", Core: "image_url", Required: true},
	}},
	"myntra": {Platform: "Myntra", Columns: []channelPackColumn{
		{Header: "Vendor SKU Code", Core: "item_code"},
		{Header: "Article Type", Core: "channel_category", Required: true},
		{Header: "Brand", Attribute: "brand", Required: true},
		{Header: "Product Display Name", Core: "title", Required: true},
		{Header: "Gender", Attribute: "gender", Required: true, Values: []string{"Men", "Women", "Boys", "Girls", "Unisex"}},
		{Header: "Prominent Colour", Attribute: "color", Required: true},
		{Header: "Standard Size", Attribute: "size", Required: true},
		{Header: "Fabric", Attribute: "fabric"},
		{Header: "Sleeve Length", Attribute: "sleeve", Values: []string{"Short Sleeves", "Long Sleeves", "Three-Quarter Sleeves", "Sleeveless"}},
		{Header: "MRP", Field: "mrp", Required: true},
		{Header: "HSN", Field: "hsn_code", Required: true},
		{Header: "Product Details", Core: "description"},
		{Header: "Front Image", Core: "image_url", Required: true},
	}},
}

// ChannelPackFor is the pack for a channel's platform, if one ships.
func ChannelPackFor(platform string) (ChannelPack, bool) {
	p, ok := channelPacks[strings.ToLower(strings.TrimSpace(platform))]
	return p, ok
}

// ChannelPackPlatforms are the platforms a pack ships for.
func ChannelPackPlatforms() []string {
	out := []string{}
	for _, p := range channelPacks {
		out = append(out, p.Platform)
	}
	sort.Strings(out)
	return out
}

// ChannelPackResult is what applying a pack did.
type ChannelPackResult struct {
	Channel           string   `json:"channel"`
	Platform          string   `json:"platform"`
	Template          string   `json:"template"`
	FieldMaps         int      `json:"field_maps_added"`
	AttributesCreated []string `json:"attributes_created"`
	Columns           int      `json:"columns"`
}

var packIDChars = regexp.MustCompile(`[^A-Za-z0-9-]+`)

// ApplyChannelPack sets a channel up from its platform's pack.
func ApplyChannelPack(tenantID, channelID, user string) (*ChannelPackResult, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	var platform, name string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(data->>'platform', ''), COALESCE(data->>'name', id) FROM %s.documents
		WHERE doctype = 'Channel' AND id = $1 AND deleted_at IS NULL`, schema), channelID).Scan(&platform, &name); err != nil {
		return nil, &ValidationError{Code: "META-0198", SubFor: "Channel", Message: fmt.Sprintf("channel %s was not found", channelID)}
	}
	pack, ok := ChannelPackFor(platform)
	if !ok {
		return nil, &ValidationError{Code: "GLOBAL-0002", SubFor: "Platform", Message: fmt.Sprintf("no ready-made layout for %q - packs ship for %s", platform, strings.Join(ChannelPackPlatforms(), ", "))}
	}
	res := &ChannelPackResult{Channel: channelID, Platform: pack.Platform, AttributesCreated: []string{}}
	insert := func(id, doctype string, data map[string]interface{}) (bool, error) {
		raw, _ := json.Marshal(data)
		r, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, 'Active', $4)
			ON CONFLICT DO NOTHING`, schema), id, doctype, raw, user)
		if err != nil {
			return false, err
		}
		n, _ := r.RowsAffected()
		return n > 0, nil
	}
	var cols []PIMExportColumn
	for _, c := range pack.Columns {
		key := c.Core
		if key == "" {
			source := c.Field
			if c.Attribute != "" {
				attr, created, err := packAttribute(schema, c, insert)
				if err != nil {
					return nil, err
				}
				if created {
					res.AttributesCreated = append(res.AttributesCreated, attr)
				}
				source = attr
			}
			key = strings.ToLower(packIDChars.ReplaceAllString(c.Header, "_"))
			mapID := strings.ToUpper(packIDChars.ReplaceAllString(channelID+"-"+key, "-"))
			added, err := insert(mapID, "ChannelFieldMap", map[string]interface{}{"code": mapID, "channel": channelID, "source_field": source,
				"target_field": key, "mandatory": map[bool]string{true: "Yes", false: "No"}[c.Required], "status": "Active"})
			if err != nil {
				return nil, err
			}
			if added {
				res.FieldMaps++
			}
		}
		cols = append(cols, PIMExportColumn{FieldKey: key, ColumnHeader: c.Header})
	}
	tplID := strings.ToUpper(packIDChars.ReplaceAllString(channelID+"-FEED", "-"))
	colsJSON, _ := json.Marshal(cols)
	if _, err := insert(tplID, "PIMExportTemplate", map[string]interface{}{"code": tplID, "name": fmt.Sprintf("%s feed (%s)", pack.Platform, name),
		"channel": channelID, "column_mappings": string(colsJSON), "headerless": "No", "variant_mode": "All Rows", "status": "Active",
		"description": "Ready-made " + pack.Platform + " layout (channel pack)"}); err != nil {
		return nil, err
	}
	res.Template, res.Columns = tplID, len(cols)
	LogAuditEvent(tenantID, user, "PIM", "CHANNEL_PACK_APPLIED", fmt.Sprintf("%s pack on channel %s: %d field map(s), %d attribute(s) created, template %s",
		pack.Platform, channelID, res.FieldMaps, len(res.AttributesCreated), tplID))
	return res, nil
}

// packAttribute finds the client's attribute for a pack column by code or
// label, or creates it with the pack's value list.
func packAttribute(schema string, c channelPackColumn, insert func(string, string, map[string]interface{}) (bool, error)) (string, bool, error) {
	var id string
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT id FROM %s.documents WHERE doctype = 'ProductAttributeDef' AND deleted_at IS NULL
		AND (LOWER(id) = $1 OR LOWER(COALESCE(data->>'code', '')) = $1 OR LOWER(COALESCE(data->>'label', '')) = $1
		     OR LOWER(COALESCE(data->>'label', '')) = $2 OR LOWER(COALESCE(data->>'code', '')) = $2)
		ORDER BY (LOWER(COALESCE(data->>'code', '')) = $1) DESC LIMIT 1`, schema), c.Attribute, map[string]string{"color": "colour", "colour": "color"}[c.Attribute]).Scan(&id)
	if id != "" {
		return id, false, nil
	}
	id = strings.ToUpper(c.Attribute)
	data := map[string]interface{}{"code": id, "label": strings.Title(c.Attribute), "value_type": "Text", "status": "Active", "used_for": "Product information"}
	if len(c.Values) > 0 {
		data["value_type"], data["value_options"] = "Select", strings.Join(c.Values, ",")
	}
	created, err := insert(id, "ProductAttributeDef", data)
	return id, created, err
}
