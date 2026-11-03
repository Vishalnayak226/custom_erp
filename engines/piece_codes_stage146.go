package engines

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// Stage 146.3.2 - a code for every piece received.
//
// An item type that is marked "Code for every piece received" opts its items
// in; the setting inventory.piece_codes_stores (blank = off, "all", or store
// codes) switches a store on. At an opted-in store, posting a goods receipt
// gives every accepted piece its own serial code from the PieceCode number
// series. Codes typed or scanned on the line are kept and only the shortfall
// is minted, so manual and minted codes mix on one receipt.
//
// The minted codes are written back onto the GRN line (serial_numbers) in the
// receipt's own transaction. That one write is what makes everything else
// safe to repeat: a second post finds the line already full and mints nothing,
// and a sticker reprint reads the stored codes (resolveGRNStickerLines) and
// never calls the series.

// PieceCodeSeries is the number-series key the codes are drawn from.
const PieceCodeSeries = "PieceCode"

// pieceCodeStoreOn reports whether the store switch covers locationCode.
func pieceCodeStoreOn(tenantID, locationCode string) bool {
	v := strings.TrimSpace(GetSettingString(tenantID, "inventory.piece_codes_stores"))
	if v == "" {
		return false
	}
	if strings.EqualFold(v, "all") {
		return true
	}
	for _, s := range strings.Split(v, ",") {
		if strings.EqualFold(strings.TrimSpace(s), strings.TrimSpace(locationCode)) && strings.TrimSpace(s) != "" {
			return true
		}
	}
	return false
}

// PieceCodesOn reports whether receiving sku at locationCode gives every piece
// its own code: the store is switched on and the item's type opts in.
func PieceCodesOn(tenantID, sku, locationCode string) bool {
	if !pieceCodeStoreOn(tenantID, locationCode) {
		return false
	}
	resolved, err := ResolveItemBySKU(tenantID, sku)
	if err != nil || resolved == nil {
		return false
	}
	return ItemRules(tenantID, resolved.Data).PieceCodes
}

// MintPieceCodes draws n codes from the PieceCode series. Used by the receipt
// and by the handheld's "mint" button.
func MintPieceCodes(tenantID string, n int) ([]string, error) {
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		code, err := GenerateSequence(tenantID, PieceCodeSeries, "", "")
		if err != nil {
			return nil, err
		}
		out = append(out, code)
	}
	return out, nil
}

// ValidateReceiptPieceLine is ValidateReceiptSerialLine for a store and item
// that may give every piece a code. Where they do, fewer codes than pieces is
// fine (the rest are minted); a repeated code or more codes than pieces is
// still refused. Anywhere else it is exactly ValidateReceiptSerialLine.
func ValidateReceiptPieceLine(tenantID, locationCode, sku string, serialNumbers []string, acceptedQty float64) error {
	if !PieceCodesOn(tenantID, sku, locationCode) {
		return ValidateReceiptSerialLine(tenantID, sku, serialNumbers, acceptedQty)
	}
	want := int(acceptedQty + 0.5)
	seen := map[string]bool{}
	for _, s := range serialNumbers {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if seen[s] {
			return &ValidationError{Code: "INVENT-0115", Message: fmt.Sprintf(
				"code %s is listed twice on the same received line for item %s", s, sku)}
		}
		seen[s] = true
	}
	if len(seen) > want {
		return &ValidationError{Code: "INVENT-0115", Message: fmt.Sprintf(
			"item %s: %d piece(s) accepted but %d code(s) listed; a piece has one code", sku, want, len(seen))}
	}
	return nil
}

// planPieceCodesTx returns the codes for one accepted line: the typed ones,
// then the ones already stored on the GRN line, then freshly minted codes for
// whatever is still missing. The result is written back to the GRN line when
// it grew. A line that is not opted in comes back unchanged.
func planPieceCodesTx(tx *sql.Tx, tenantID, schema, locationCode, grnID string, lineIdx int, sku string, accepted float64, typed []string) ([]string, error) {
	if accepted <= 0 || !PieceCodesOn(tenantID, sku, locationCode) {
		return typed, nil
	}
	want := int(accepted + 0.5)
	codes := append([]string(nil), typed...)
	have := map[string]bool{}
	for _, c := range codes {
		have[c] = true
	}

	lines, asString, found := loadGRNLinesTx(tx, schema, grnID)
	var storedBefore int
	if found && lineIdx < len(lines) {
		if s, _ := lines[lineIdx]["sku"].(string); s == sku {
			if raw, ok := lines[lineIdx]["serial_numbers"].([]interface{}); ok {
				storedBefore = len(raw)
				for _, v := range raw {
					if c, ok := v.(string); ok && strings.TrimSpace(c) != "" && !have[strings.TrimSpace(c)] {
						codes = append(codes, strings.TrimSpace(c))
						have[strings.TrimSpace(c)] = true
					}
				}
			}
		} else {
			found = false
		}
	}
	if len(codes) > want {
		codes = codes[:want]
	}
	if short := want - len(codes); short > 0 {
		minted, err := MintPieceCodes(tenantID, short)
		if err != nil {
			return nil, err
		}
		codes = append(codes, minted...)
	}
	if found && len(codes) != storedBefore {
		list := make([]interface{}, len(codes))
		for i, c := range codes {
			list[i] = c
		}
		lines[lineIdx]["serial_numbers"] = list
		if err := saveGRNLinesTx(tx, schema, grnID, lines, asString); err != nil {
			return nil, err
		}
	}
	return codes, nil
}

// loadGRNLinesTx reads a GRN's received_items as generic maps, so a rewrite
// keeps every field the engine does not know about. asString says the column
// holds the JSON as a string (how the screens save it) rather than an array.
func loadGRNLinesTx(tx *sql.Tx, schema, grnID string) (lines []map[string]interface{}, asString, found bool) {
	if grnID == "" {
		return nil, false, false
	}
	var kind, raw sql.NullString
	err := tx.QueryRow(fmt.Sprintf(`SELECT jsonb_typeof(data->'received_items'), data->>'received_items'
		FROM %s.documents WHERE doctype = 'GRN' AND id = $1 FOR UPDATE`, schema), grnID).Scan(&kind, &raw)
	if err != nil || !raw.Valid || (kind.String != "string" && kind.String != "array") {
		return nil, false, false
	}
	if json.Unmarshal([]byte(raw.String), &lines) != nil {
		return nil, false, false
	}
	return lines, kind.String == "string", true
}

func saveGRNLinesTx(tx *sql.Tx, schema, grnID string, lines []map[string]interface{}, asString bool) error {
	b, err := json.Marshal(lines)
	if err != nil {
		return err
	}
	q := `UPDATE %s.documents SET data = jsonb_set(data, '{received_items}', $1::jsonb), updated_at = CURRENT_TIMESTAMP WHERE doctype = 'GRN' AND id = $2`
	arg := string(b)
	if asString {
		q = `UPDATE %s.documents SET data = jsonb_set(data, '{received_items}', to_jsonb($1::text)), updated_at = CURRENT_TIMESTAMP WHERE doctype = 'GRN' AND id = $2`
	}
	_, err = tx.Exec(fmt.Sprintf(q, schema), arg, grnID)
	return err
}

// PieceCodesSwitchedOn reports whether any store is switched on (the setting
// is not blank). The RF menu and the mint route use it.
func PieceCodesSwitchedOn(tenantID string) bool {
	return strings.TrimSpace(GetSettingString(tenantID, "inventory.piece_codes_stores")) != ""
}
