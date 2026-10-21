package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Stage 52: printing a sticker run from an actual transaction (a GRN or a
// Transfer Order) instead of a clerk scanning SKUs by hand. The contract
// deliberately stops at "SKU + qty (+ batch/expiry when the source line
// carries one)" - everything else a label needs (name, category, HSN, which
// template applies) is resolved per-SKU by resolveAndLogSticker exactly as
// the manual-scan flow already does, so a document-driven print and a
// manual print produce identical label data for the same SKU.

// ErrUnsupportedStickerSource is returned by ResolveDocumentStickerLines for
// any sourceDoctype not in its switch. Adding a new source (PurchaseOrder,
// SalesOrder, ...) is one more case there, not a redesign.
var ErrUnsupportedStickerSource = errors.New("unsupported sticker source doctype")

// DocStickerLine is one line to print, after a source document's own line
// shape has been normalized. BatchNo/ExpiryDate are blank for a source
// (Transfer Order) or item that doesn't carry them.
type DocStickerLine struct {
	SKU        string
	Qty        int
	BatchNo    string
	ExpiryDate string
	TemplateID string // chosen for this print (Stage 70), "" = the rules decide
}

// ResolveDocumentStickerLines normalizes sourceDoctype's line-item shape into
// the SKU+qty (+batch) pairs a sticker run needs.
func ResolveDocumentStickerLines(tenantID, sourceDoctype, sourceDocID string) ([]DocStickerLine, error) {
	switch sourceDoctype {
	case "GRN":
		return resolveGRNStickerLines(tenantID, sourceDocID)
	case "TransferOrder":
		return resolveTransferOrderStickerLines(tenantID, sourceDocID)
	default:
		// Stage 127.5 (user, 2026-10-21: "any new taxn should load properly
		// in print"): any other transaction whose lines carry items.
		if DoctypeIsTransaction(tenantID, sourceDoctype) {
			return resolveGenericStickerLines(tenantID, sourceDoctype, sourceDocID)
		}
		return nil, fmt.Errorf("%w: %q is not a transaction", ErrUnsupportedStickerSource, sourceDoctype)
	}
}

// resolveGenericStickerLines reads any transaction's item lines - its line
// table, else items / lines / received_items / return_items - one sticker per
// unit, lots kept apart like a GRN's.
func resolveGenericStickerLines(tenantID, doctype, id string) ([]DocStickerLine, error) {
	data, status, err := fetchDocData(tenantID, doctype, id)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("no %s is numbered %q - pick one from the list under the box", humanizeDoctype(doctype), id)
	}
	if err != nil {
		return nil, fmt.Errorf("could not load %s %s: %v", doctype, id, err)
	}
	if status == "Cancelled" {
		return nil, fmt.Errorf("%s %s is cancelled", humanizeDoctype(doctype), id)
	}
	names := []string{}
	if fields, err := GetDocTypeMeta(tenantID, doctype); err == nil {
		for _, f := range fields {
			if f.Fieldtype == "JSONTable" {
				names = append(names, f.Fieldname)
			}
		}
	}
	names = append(names, "items", "lines", "received_items", "return_items")
	type key struct{ sku, batch string }
	byLine := map[key]*DocStickerLine{}
	var order []key
	for _, name := range names {
		for _, l := range genericLineList(data[name]) {
			sku := genericLineSKU(l)
			qty := int(numFromInterface(firstPresent(l, "accepted_qty", "qty", "quantity", "return_qty", "received_qty", "issued_qty")))
			if sku == "" || qty <= 0 {
				continue
			}
			k := key{sku, strField(l, "batch_no")}
			if existing, ok := byLine[k]; ok {
				existing.Qty += qty
				continue
			}
			byLine[k] = &DocStickerLine{SKU: sku, Qty: qty, BatchNo: k.batch, ExpiryDate: strField(l, "expiry_date")}
			order = append(order, k)
		}
		if len(order) > 0 {
			break
		}
	}
	lines := make([]DocStickerLine, 0, len(order))
	for _, k := range order {
		lines = append(lines, *byLine[k])
	}
	return lines, nil
}

// resolveGRNStickerLines prints against accepted_qty (qty minus the
// rejected/damaged split, or an explicit accepted_qty override) - the same
// derivation PostGRNReceiptWithQC uses to decide what actually lands in
// available stock (engines/transactional_validation.go's
// grnReceivedLine.derivedAcceptedQty). A rejected/damaged line contributes
// zero stickers: there's nothing sellable/shelvable to label.
//
// Two GRN lines for the same SKU are merged only when they share the same
// batch/lot (or neither has one) - a batch-tracked item received across two
// lots keeps its lots separate on the printed stickers rather than losing
// which units belong to which lot.
func resolveGRNStickerLines(tenantID, grnID string) ([]DocStickerLine, error) {
	data, status, err := fetchDocData(tenantID, "GRN", grnID)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("no goods receipt is numbered %q - pick one from the list under the box", grnID)
	}
	if err != nil {
		return nil, fmt.Errorf("could not load GRN %s: %v", grnID, err)
	}
	if status == "Cancelled" {
		return nil, fmt.Errorf("GRN %s is cancelled", grnID)
	}
	raw, _ := data["received_items"].(string)
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var received []grnReceivedLine
	if err := json.Unmarshal([]byte(raw), &received); err != nil {
		return nil, fmt.Errorf("could not parse GRN %s received_items: %v", grnID, err)
	}

	type key struct{ sku, batch string }
	byLine := map[key]*DocStickerLine{}
	var order []key
	for _, l := range received {
		accepted := int(l.derivedAcceptedQty())
		if accepted <= 0 {
			continue
		}
		k := key{sku: l.Sku, batch: l.BatchNo}
		if existing, ok := byLine[k]; ok {
			existing.Qty += accepted
			continue
		}
		byLine[k] = &DocStickerLine{SKU: l.Sku, Qty: accepted, BatchNo: l.BatchNo, ExpiryDate: l.ExpiryDate}
		order = append(order, k)
	}
	lines := make([]DocStickerLine, 0, len(order))
	for _, k := range order {
		lines = append(lines, *byLine[k])
	}
	return lines, nil
}

// resolveTransferOrderStickerLines uses the same parseTransferItems the
// Dispatch/Receive flows already validate against (engines/transfer_orders.go),
// so a sticker run can never see a shape those flows would have rejected.
func resolveTransferOrderStickerLines(tenantID, transferOrderID string) ([]DocStickerLine, error) {
	data, status, err := fetchDocData(tenantID, "TransferOrder", transferOrderID)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("no transfer order is numbered %q - pick one from the list under the box", transferOrderID)
	}
	if err != nil {
		return nil, fmt.Errorf("could not load Transfer Order %s: %v", transferOrderID, err)
	}
	if status == "Cancelled" {
		return nil, fmt.Errorf("Transfer Order %s is cancelled", transferOrderID)
	}
	raw, _ := data["items"].(string)
	items, err := parseTransferItems(raw)
	if err != nil {
		return nil, fmt.Errorf("could not parse Transfer Order %s items: %v", transferOrderID, err)
	}
	lines := make([]DocStickerLine, 0, len(items))
	for _, it := range items {
		lines = append(lines, DocStickerLine{SKU: it.Sku, Qty: it.Qty})
	}
	return lines, nil
}

// StickerLineSelection identifies one line of a document's sticker run.
//
// A document's lines are unique by SKU *and* batch (ResolveDocumentStickerLines
// merges two receipt lines only when they share a lot), so a selection keyed on
// SKU alone cannot address a SKU that arrived on two lots - picking either row
// would print both. BatchNo disambiguates: empty means "every line of this SKU"
// (which is what a SKU-only caller, e.g. the manual scan flow or an older API
// client, still gets), a value means that one lot's line only.
//
// Copies overrides the line's default copy count (its accepted/transfer qty);
// zero or less means "use the line's own quantity".
//
// TemplateID (Stage 70) prints the line with that template instead of the one
// the rules pick - a StickerTemplate id, or StickerTemplateStandard for the
// built-in label; blank leaves it to the rules.
type StickerLineSelection struct {
	SKU        string `json:"sku"`
	BatchNo    string `json:"batch_no"`
	Copies     int    `json:"copies"`
	TemplateID string `json:"template_id,omitempty"`
}

// SKUSelections adapts a plain SKU list to []StickerLineSelection, carrying
// the pre-Stage-52.8 per-SKU copy overrides. Both of the HTTP handlers accept
// the older `skus`/`copies_override` shape as well as the newer per-line one,
// so this is what keeps every existing caller behaving exactly as before.
func SKUSelections(skus []string, copyOverrides map[string]int) []StickerLineSelection {
	selections := make([]StickerLineSelection, 0, len(skus))
	for _, sku := range skus {
		selections = append(selections, StickerLineSelection{SKU: sku, Copies: copyOverrides[sku]})
	}
	return selections
}

// matchStickerLine reports whether sel addresses line, and how many copies it
// asks for. A selection with no BatchNo matches every lot of its SKU.
func matchStickerLine(sel StickerLineSelection, line DocStickerLine) bool {
	return sel.SKU == line.SKU && (sel.BatchNo == "" || sel.BatchNo == line.BatchNo)
}

// filterStickerLines keeps only the lines addressed by selections, and applies
// each selection's copy count to the line it matched. An empty selection means
// "every line" - this is what makes printing the whole document and printing
// one line the same code path, just a different selection size.
func filterStickerLines(lines []DocStickerLine, selections []StickerLineSelection) []DocStickerLine {
	if len(selections) == 0 {
		return lines
	}
	filtered := make([]DocStickerLine, 0, len(lines))
	for _, l := range lines {
		for _, sel := range selections {
			if !matchStickerLine(sel, l) {
				continue
			}
			if sel.Copies > 0 {
				l.Qty = sel.Copies
			}
			l.TemplateID = strings.TrimSpace(sel.TemplateID)
			filtered = append(filtered, l)
			break // one line prints once even if two selections name it
		}
	}
	return filtered
}

// PrintStickersForDocument resolves sourceDoctype/sourceDocID's line items,
// prints (and logs) one sticker per line via the same resolveAndLogSticker
// PrintStickers uses, and returns the labels sorted by resolved category so
// same-category labels come off the printer contiguously - each category's
// own StickerTemplate still renders its own label, this only orders the
// batch so a mixed-category GRN prints as separable stacks per category.
//
// selections narrows the run to just those lines (nil/empty = every line -
// "print the whole GRN"; one selection - "print this one line"), and carries
// each line's copy count, e.g. after the user edits the review table. See
// StickerLineSelection for why a line is addressed by SKU *and* batch.
func PrintStickersForDocument(tenantID, sourceDoctype, sourceDocID, printerCode, printedBy, reprintReason string, selections []StickerLineSelection, opts ...StickerPrintOptions) ([]StickerLabel, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	if reprintReason, err = normalizeReprintReason(tenantID, reprintReason); err != nil { // Stage 74.1.1
		return nil, err
	}
	if err := checkPrinterActive(schema, printerCode); err != nil {
		return nil, err
	}

	lines, err := ResolveDocumentStickerLines(tenantID, sourceDoctype, sourceDocID)
	if err != nil {
		return nil, err
	}
	lines = filterStickerLines(lines, selections)
	if len(lines) == 0 {
		return nil, fmt.Errorf("no stickerable lines found on %s %s", sourceDoctype, sourceDocID)
	}

	labels := make([]StickerLabel, 0, len(lines))
	for lineIndex, line := range lines {
		// filterStickerLines has already applied the selection's own copy count
		// to line.Qty, so a line reaching here always carries its final count.
		copies := line.Qty
		if copies <= 0 {
			copies = 1
		}
		label, err := resolveAndLogSticker(tenantID, schema, line.SKU, copies, printerCode, printedBy, reprintReason, sourceDoctype, sourceDocID, line.BatchNo, line.ExpiryDate, line.TemplateID, stickerRequestKey(opts), lineIndex)
		if err != nil {
			return nil, err
		}
		label.Qty = copies
		labels = append(labels, label)
	}

	sort.SliceStable(labels, func(i, j int) bool {
		return strings.ToLower(labels[i].Category) < strings.ToLower(labels[j].Category)
	})
	return labels, nil
}

// StickerPreviewLine is one line of the pre-print review table - read-only,
// no sticker_print_log write, so a user can reopen/recompute the preview
// (e.g. after editing copy counts) as many times as they like before
// actually committing to a print.
type StickerPreviewLine struct {
	SKU          string `json:"sku"`
	Name         string `json:"name"`
	Category     string `json:"category"`
	ProductType  string `json:"product_type,omitempty"` // Stage 119.5: the Print screen can keep a pick as this type's rule
	Qty          int    `json:"qty"`
	Barcode      string `json:"barcode,omitempty"` // Stage 111.2: a scan finds its line by this or the SKU
	BatchNo      string `json:"batch_no,omitempty"`
	ExpiryDate   string `json:"expiry_date,omitempty"`
	TemplateID   string `json:"template_id,omitempty"`
	TemplateName string `json:"template_name"`
	TemplateRule string `json:"template_rule"` // which rule picked it (Stage 70)
}

// PreviewDocumentStickerLines is ResolveDocumentStickerLines plus the Item
// and StickerTemplate lookups the review table needs to show a user, before
// they've committed to printing anything, which category/template each line
// will actually use.
func PreviewDocumentStickerLines(tenantID, sourceDoctype, sourceDocID string) ([]StickerPreviewLine, error) {
	lines, err := ResolveDocumentStickerLines(tenantID, sourceDoctype, sourceDocID)
	if err != nil {
		return nil, err
	}
	preview := make([]StickerPreviewLine, 0, len(lines))
	for _, line := range lines {
		p := StickerPreviewLine{SKU: line.SKU, Qty: line.Qty, BatchNo: line.BatchNo, ExpiryDate: line.ExpiryDate, TemplateName: "Standard label"}
		var item map[string]interface{}
		if resolved, err := ResolveItemBySKU(tenantID, line.SKU); err == nil {
			item = resolved.Data
			if v, ok := item["name"].(string); ok {
				p.Name = v
			}
			if v, ok := item["barcode"].(string); ok {
				p.Barcode = v
			}
			if v, ok := item["category"].(string); ok {
				p.Category = v
			}
			p.ProductType = pimString(item["product_type"])
		}
		tmpl, rule, err := ResolveStickerTemplateForItem(tenantID, item, p.Category)
		if err == nil && tmpl != nil {
			p.TemplateID, p.TemplateName = tmpl.ID, tmpl.Name
		}
		p.TemplateRule = rule
		preview = append(preview, p)
	}
	return preview, nil
}
