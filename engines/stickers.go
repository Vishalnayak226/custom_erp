package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
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

	var labels []StickerLabel
	for _, sku := range skus {
		var dataStr string
		err := db.DB.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents WHERE doctype = 'Item' AND id = $1`, schema), sku).Scan(&dataStr)
		label := StickerLabel{SKU: sku}
		if err == nil {
			var item map[string]interface{}
			_ = json.Unmarshal([]byte(dataStr), &item)
			if v, ok := item["name"].(string); ok {
				label.Name = v
			}
			if v, ok := item["barcode"].(string); ok {
				label.Barcode = v
			}
			if v, ok := item["hsn_code"].(string); ok {
				label.HSNCode = v
			}
		}
		// A SKU not found as a real Item still gets printed (matches MB
		// 15.3's "Print by ... barcode range" case, where the barcode range
		// may not map 1:1 to registered Item records) - it just prints with
		// only the SKU/barcode known, name/HSN blank.
		if label.Barcode == "" {
			label.Barcode = sku
		}
		labels = append(labels, label)

		if _, err := db.DB.Exec(fmt.Sprintf(`
			INSERT INTO %s.sticker_print_log (sku, barcode, printer_code, printed_by, copies, reprint_reason)
			VALUES ($1, $2, $3, $4, $5, $6)`, schema),
			sku, label.Barcode, printerCode, printedBy, copies, reprintReason); err != nil {
			return nil, fmt.Errorf("failed to log print for %s: %v", sku, err)
		}
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
}

// GetPrintHistory lists sticker print log entries, most recent first.
func GetPrintHistory(tenantID string) ([]PrintHistoryEntry, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT sku, COALESCE(barcode, ''), printer_code, printed_by, copies, COALESCE(reprint_reason, ''), printed_at
		FROM %s.sticker_print_log ORDER BY printed_at DESC LIMIT 200`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []PrintHistoryEntry
	for rows.Next() {
		var e PrintHistoryEntry
		if err := rows.Scan(&e.SKU, &e.Barcode, &e.PrinterCode, &e.PrintedBy, &e.Copies, &e.ReprintReason, &e.PrintedAt); err != nil {
			return nil, err
		}
		results = append(results, e)
	}
	return results, nil
}
