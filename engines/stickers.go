package engines

import (
	"custom_erp/db"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"
)

// StickerLabel is one item's data as printed on a sticker/label - Templates
// per MB 15.3 name barcode, item name, HSN, and price as the layout
// fields; MRP isn't tracked anywhere in this codebase's Item master today
// (no price-list module exists - the same gap flagged when the GST engine,
// Stage 13.10, needed a per-item rate field), so it's omitted rather than
// invented.
type StickerLabel struct {
	SKU     string `json:"sku"`
	Name    string `json:"name"`
	Barcode string `json:"barcode"`
	HSNCode string `json:"hsn_code"`
	// Stage 42.1.11: a real, scannable Code 128 rendering of Barcode, for the
	// browser @media print fallback sheet (the QZ Tray silent-print path
	// already gets a real barcode from the label printer's own native ZPL
	// ^BC command, engines/qz_payload.go, and needs nothing here). Blank
	// whenever Barcode contains a character Code Set B can't encode (rare -
	// SKU/barcode values are ordinarily plain ASCII) rather than failing the
	// whole print run over one unrenderable label; the plain-text fallback
	// in that case is exactly what every label looked like before this Stage.
	BarcodeSVG string `json:"barcode_svg,omitempty"`

	// Stage 52: additive fields for category-based templates and GRN/Transfer
	// Order-driven bulk printing. All omitempty, so a caller/test that only
	// ever inspected the five fields above sees identical JSON to before.
	Category      string `json:"category,omitempty"`
	BatchNo       string `json:"batch_no,omitempty"`
	ExpiryDate    string `json:"expiry_date,omitempty"`
	MfgDate       string `json:"mfg_date,omitempty"`
	Qty           int    `json:"qty,omitempty"`
	SourceDoctype string `json:"source_doctype,omitempty"`
	SourceDocID   string `json:"source_doc_id,omitempty"`
	// TemplateID/TemplateElements/LabelWidthMM/LabelHeightMM come from
	// ResolveStickerTemplate. Empty/zero means "no category template
	// resolved" - BuildStickerPayload and the browser print sheet both fall
	// back to today's hardcoded 3-line layout in that case, which is what
	// keeps this feature purely additive for a tenant that never configures
	// a StickerTemplate.
	TemplateID       string          `json:"template_id,omitempty"`
	TemplateName     string          `json:"template_name,omitempty"`
	TemplateElements json.RawMessage `json:"template_elements,omitempty"`
	LabelWidthMM     float64         `json:"label_width_mm,omitempty"`
	LabelHeightMM    float64         `json:"label_height_mm,omitempty"`
}

// StickerTemplate is a tenant-configured label layout, mapped to one or more
// free-text Item.category values. See db/migrations_stage52_sticker_templates.sql
// for the field definitions and the reasoning behind categories being plain
// comma-separated text rather than a Link (Item.category has no master
// behind it - db/migration.sql:345 - so there is nothing to link to).
type StickerTemplate struct {
	ID            string
	Code          string
	Name          string
	Categories    []string
	IsDefault     bool
	LabelWidthMM  float64
	LabelHeightMM float64
	Elements      json.RawMessage
}

// ResolveStickerTemplate finds the Active StickerTemplate whose categories
// list contains category (case-insensitive, trimmed). When nothing matches,
// it falls back to the single Active is_default template if one exists; when
// neither exists it returns (nil, nil) - the caller's cue to keep the
// pre-Stage-52 hardcoded label untouched rather than treat "unconfigured" as
// an error.
func ResolveStickerTemplate(tenantID, category string) (*StickerTemplate, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, COALESCE(data->>'code', ''), COALESCE(data->>'name', ''),
		       COALESCE(data->>'categories', ''), COALESCE(data->>'is_default', 'false'),
		       COALESCE(data->>'label_width_mm', '0'), COALESCE(data->>'label_height_mm', '0'),
		       COALESCE(data->>'elements', '[]')
		FROM %s.documents
		WHERE doctype = 'StickerTemplate' AND status != 'Cancelled'
		  AND COALESCE(data->>'status', 'Active') = 'Active'`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	needle := strings.ToLower(strings.TrimSpace(category))
	var fallback *StickerTemplate
	for rows.Next() {
		var t StickerTemplate
		var categoriesRaw, isDefaultRaw, widthRaw, heightRaw, elementsRaw string
		if err := rows.Scan(&t.ID, &t.Code, &t.Name, &categoriesRaw, &isDefaultRaw, &widthRaw, &heightRaw, &elementsRaw); err != nil {
			return nil, err
		}
		t.Elements = json.RawMessage(elementsRaw)
		t.IsDefault = isDefaultRaw == "true" || isDefaultRaw == "1"
		t.LabelWidthMM, _ = strconv.ParseFloat(widthRaw, 64)
		t.LabelHeightMM, _ = strconv.ParseFloat(heightRaw, 64)
		for _, c := range strings.Split(categoriesRaw, ",") {
			if c = strings.TrimSpace(c); c != "" {
				t.Categories = append(t.Categories, c)
			}
		}

		if needle != "" {
			for _, c := range t.Categories {
				if strings.ToLower(c) == needle {
					match := t
					return &match, nil
				}
			}
		}
		if t.IsDefault && fallback == nil {
			match := t
			fallback = &match
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return fallback, nil
}

// StickerElement is one positioned field on a StickerTemplate's label,
// authored by the drag-and-drop designer (public/app.js) and consumed by
// both BuildStickerPayload's ZPL renderer (engines/qz_payload.go) and the
// browser @media print fallback - one JSON shape, two renderers, so moving
// a box in the designer changes what both actually print.
//
// Field is one of: sku, name, barcode, hsn_code, category, batch_no,
// expiry_date, mfg_date, qty, source_doc, static (Text holds the literal
// string for a static element; every other field name is read off the
// StickerLabel being printed). Positions/sizes are in mm, matching the
// template's own LabelWidthMM/LabelHeightMM, so the designer canvas, the
// printed ZPL (converted to dots at the printer's DPI) and the browser
// print sheet (native CSS mm units) all agree on where things land.
type StickerElement struct {
	ID         string  `json:"id"`
	Field      string  `json:"field"`
	Text       string  `json:"text,omitempty"`
	XMM        float64 `json:"x_mm"`
	YMM        float64 `json:"y_mm"`
	WMM        float64 `json:"w_mm"`
	HMM        float64 `json:"h_mm"`
	FontSizeMM float64 `json:"font_size_mm,omitempty"`
	Bold       bool    `json:"bold,omitempty"`
	Align      string  `json:"align,omitempty"` // "left" (default) | "center" | "right"
}

// ParseStickerElements unmarshals a StickerTemplate/StickerLabel's raw
// elements JSON. A malformed or empty value returns an empty slice (not an
// error the caller must special-case) - BuildStickerPayload's contract is to
// fall back to the hardcoded default layout whenever there is nothing usable
// to render, same as "no template resolved at all".
func ParseStickerElements(raw json.RawMessage) []StickerElement {
	if len(raw) == 0 {
		return nil
	}
	var elements []StickerElement
	if err := json.Unmarshal(raw, &elements); err != nil {
		log.Printf("[STICKERS] malformed template elements JSON, falling back to default layout: %v", err)
		return nil
	}
	return elements
}

// StickerFieldText reads the value StickerElement.Field names off a
// resolved label - the single mapping both the ZPL renderer and (via the
// same-shaped JSON the browser fallback receives) the print-preview sheet's
// logic mirror.
func StickerFieldText(el StickerElement, label StickerLabel) string {
	switch el.Field {
	case "static":
		return el.Text
	case "sku":
		return label.SKU
	case "name":
		return label.Name
	case "hsn_code":
		return label.HSNCode
	case "category":
		return label.Category
	case "batch_no":
		return label.BatchNo
	case "expiry_date":
		return label.ExpiryDate
	case "mfg_date":
		return label.MfgDate
	case "qty":
		if label.Qty > 0 {
			return strconv.Itoa(label.Qty)
		}
		return ""
	case "source_doc":
		return label.SourceDocID
	case "barcode":
		return label.Barcode
	default:
		return ""
	}
}

// checkPrinterActive is the DEVICE-0298 (Stage 25.5) printer-existence gate,
// factored out so both PrintStickers and PrintStickersForDocument run the
// same check rather than one drifting from the other.
func checkPrinterActive(schema, printerCode string) error {
	var printerActive bool
	err := db.DB.QueryRow(fmt.Sprintf(`
		SELECT EXISTS(
			SELECT 1 FROM %s.documents
			WHERE doctype = 'Printer' AND (id = $1 OR data->>'code' = $1)
			AND status != 'Cancelled' AND COALESCE(data->>'status', 'Active') = 'Active'
		)`, schema), printerCode).Scan(&printerActive)
	if err != nil {
		return err
	}
	if !printerActive {
		return &ValidationError{Code: "DEVICE-0298", Message: fmt.Sprintf("printer %q is not configured or is inactive", printerCode)}
	}
	return nil
}

// resolveAndLogSticker resolves one SKU's Item data (and, if its category
// maps to one, a StickerTemplate), logs one sticker_print_log row, and
// returns the assembled label. Shared by PrintStickers (manual SKU-scan
// flow, sourceDoctype/sourceDocID blank) and PrintStickersForDocument (Stage
// 52, printing from a GRN/TransferOrder) so the SKU-resolution, barcode
// rendering, and audit-log write stay a single implementation.
func resolveAndLogSticker(tenantID, schema, sku string, copies int, printerCode, printedBy, reprintReason, sourceDoctype, sourceDocID, batchNo, expiryDate string) (StickerLabel, error) {
	// Stage 30.1.1: resolved through the shared ResolveItemBySKU (code ->
	// barcode -> id) rather than the internal id alone - a sticker run
	// keyed off item Codes used to print every label with a blank
	// name/HSN, which is exactly the label content that matters.
	resolved, err := ResolveItemBySKU(tenantID, sku)
	label := StickerLabel{SKU: sku, SourceDoctype: sourceDoctype, SourceDocID: sourceDocID, BatchNo: batchNo, ExpiryDate: expiryDate}
	if err != nil && !errors.Is(err, ErrItemNotFound) {
		// 24.18: read-only below (nil-map-safe), so this degrades the
		// same way an unregistered SKU already does by design (blank
		// name/HSN, barcode falls back to the SKU itself) - just logged
		// so a corrupt Item record doesn't go unnoticed.
		log.Printf("[STICKERS] could not resolve Item %s: %v", sku, err)
	}
	if err == nil {
		item := resolved.Data
		if v, ok := item["name"].(string); ok {
			label.Name = v
		}
		if v, ok := item["barcode"].(string); ok {
			label.Barcode = v
		}
		if v, ok := item["hsn_code"].(string); ok {
			label.HSNCode = v
		}
		if v, ok := item["category"].(string); ok {
			label.Category = v
		}
	}
	// A SKU not found as a real Item still gets printed (matches MB
	// 15.3's "Print by ... barcode range" case, where the barcode range
	// may not map 1:1 to registered Item records) - it just prints with
	// only the SKU/barcode known, name/HSN blank.
	if label.Barcode == "" {
		label.Barcode = sku
	}
	if svg, svgErr := RenderCode128SVG(label.Barcode); svgErr == nil {
		label.BarcodeSVG = svg
	}

	if tmpl, tErr := ResolveStickerTemplate(tenantID, label.Category); tErr != nil {
		log.Printf("[STICKERS] could not resolve template for category %q: %v", label.Category, tErr)
	} else if tmpl != nil {
		label.TemplateID = tmpl.ID
		label.TemplateName = tmpl.Name
		label.TemplateElements = tmpl.Elements
		label.LabelWidthMM = tmpl.LabelWidthMM
		label.LabelHeightMM = tmpl.LabelHeightMM
	}

	if _, err := db.DB.Exec(fmt.Sprintf(`
		INSERT INTO %s.sticker_print_log (sku, barcode, printer_code, printed_by, copies, reprint_reason, source_doctype, source_doc_id, template_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`, schema),
		sku, label.Barcode, printerCode, printedBy, copies, reprintReason,
		nullableString(sourceDoctype), nullableString(sourceDocID), nullableString(label.TemplateID)); err != nil {
		return label, fmt.Errorf("failed to log print for %s: %v", sku, err)
	}
	return label, nil
}

// nullableString turns an empty string into a real SQL NULL rather than an
// empty-string value, so source_doctype/source_doc_id/template_id (Stage 52)
// read back as NULL - not "" - for every pre-Stage-52 row and every
// manual-SKU-scan print that doesn't set them.
func nullableString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

// PrintStickers looks up each SKU's Item data, logs one sticker_print_log
// row per SKU (the audit trail MB 15.3's "Print History" asks for), and
// returns the label data for the caller to render into a printable sheet.
// Printing itself happens via the browser's print dialog against that
// rendered sheet - this function's job ends at "what should the label say
// and that it was printed," not device-level print-spooler integration.
func PrintStickers(tenantID string, skus []string, printerCode, printedBy, reprintReason string, copies int) ([]StickerLabel, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	if len(skus) == 0 {
		return nil, fmt.Errorf("at least one SKU is required")
	}
	if copies <= 0 {
		copies = 1
	}

	// DEVICE-0298 (Stage 25.5): "Printer not configured." printerCode was
	// previously never checked against the real Printer master at all - any
	// string was accepted and just stored as-is on every sticker_print_log
	// row, silently accepting a printer that doesn't exist or was
	// deactivated. Matches on either the Printer's id or its own "code"
	// field, since the frontend's printer picker sends whichever one it has.
	if err := checkPrinterActive(schema, printerCode); err != nil {
		return nil, err
	}

	var labels []StickerLabel
	for _, sku := range skus {
		label, err := resolveAndLogSticker(tenantID, schema, sku, copies, printerCode, printedBy, reprintReason, "", "", "", "")
		if err != nil {
			return nil, err
		}
		labels = append(labels, label)
	}
	return labels, nil
}

// PrintHistoryEntry is one row of the sticker print audit trail.
type PrintHistoryEntry struct {
	SKU           string    `json:"sku"`
	Barcode       string    `json:"barcode"`
	PrinterCode   string    `json:"printer_code"`
	PrintedBy     string    `json:"printed_by"`
	Copies        int       `json:"copies"`
	ReprintReason string    `json:"reprint_reason"`
	PrintedAt     time.Time `json:"printed_at"`
	SourceDoctype string    `json:"source_doctype,omitempty"`
	SourceDocID   string    `json:"source_doc_id,omitempty"`
	TemplateID    string    `json:"template_id,omitempty"`
}

// GetPrintHistory lists sticker print log entries, most recent first.
func GetPrintHistory(tenantID string) ([]PrintHistoryEntry, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT sku, COALESCE(barcode, ''), printer_code, printed_by, copies, COALESCE(reprint_reason, ''), printed_at,
		       COALESCE(source_doctype, ''), COALESCE(source_doc_id, ''), COALESCE(template_id, '')
		FROM %s.sticker_print_log ORDER BY printed_at DESC LIMIT 200`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []PrintHistoryEntry
	for rows.Next() {
		var e PrintHistoryEntry
		if err := rows.Scan(&e.SKU, &e.Barcode, &e.PrinterCode, &e.PrintedBy, &e.Copies, &e.ReprintReason, &e.PrintedAt,
			&e.SourceDoctype, &e.SourceDocID, &e.TemplateID); err != nil {
			return nil, err
		}
		results = append(results, e)
	}
	return results, nil
}
