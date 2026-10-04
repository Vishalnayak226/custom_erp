package engines

import (
	"custom_erp/db"
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
		return nil, fmt.Errorf("%w: %q (supported: GRN, TransferOrder)", ErrUnsupportedStickerSource, sourceDoctype)
	}
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
type StickerLineSelection struct {
	SKU     string `json:"sku"`
	BatchNo string `json:"batch_no"`
	Copies  int    `json:"copies"`
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
func PrintStickersForDocument(tenantID, sourceDoctype, sourceDocID, printerCode, printedBy, reprintReason string, selections []StickerLineSelection) ([]StickerLabel, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
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
	for _, line := range lines {
		// filterStickerLines has already applied the selection's own copy count
		// to line.Qty, so a line reaching here always carries its final count.
		copies := line.Qty
		if copies <= 0 {
			copies = 1
		}
		label, err := resolveAndLogSticker(tenantID, schema, line.SKU, copies, printerCode, printedBy, reprintReason, sourceDoctype, sourceDocID, line.BatchNo, line.ExpiryDate)
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
	Qty          int    `json:"qty"`
	BatchNo      string `json:"batch_no,omitempty"`
	ExpiryDate   string `json:"expiry_date,omitempty"`
	TemplateName string `json:"template_name"`
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
		p := StickerPreviewLine{SKU: line.SKU, Qty: line.Qty, BatchNo: line.BatchNo, ExpiryDate: line.ExpiryDate, TemplateName: "Default layout"}
		if resolved, err := ResolveItemBySKU(tenantID, line.SKU); err == nil {
			if v, ok := resolved.Data["name"].(string); ok {
				p.Name = v
			}
			if v, ok := resolved.Data["category"].(string); ok {
				p.Category = v
			}
		}
		if tmpl, err := ResolveStickerTemplate(tenantID, p.Category); err == nil && tmpl != nil {
			p.TemplateName = tmpl.Name
		}
		preview = append(preview, p)
	}
	return preview, nil
}
