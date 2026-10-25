package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"strconv"
	"strings"
)

// Stage 137.2: the roll loaded in a printer decides how its stickers are laid
// out. A Printer record's sticker_template names the template for that roll;
// a label whose own template does not fit the roll prints with the roll's
// template instead, so a one-tag horizontal design can no longer be sent to a
// 3-across vertical roll and cross the tags. A template of the same roll
// shape keeps the rule's pick, so a Square and a Ring design of one roll
// still print as themselves.

// stickerRollShape is how a template's labels sit on the roll: one label's
// size, how many across and down one print, which way the design reads, and
// roll vs A4 sheet. Two templates of one shape fit the same roll.
type stickerRollShape struct {
	W, H            float64
	PerRow, PerCol  int
	Vertical, Sheet bool
}

func (a stickerRollShape) fits(b stickerRollShape) bool {
	return math.Abs(a.W-b.W) < 0.05 && math.Abs(a.H-b.H) < 0.05 &&
		a.PerRow == b.PerRow && a.PerCol == b.PerCol && a.Vertical == b.Vertical && a.Sheet == b.Sheet
}

// builtInStickerShape is the built-in standard label (public/sticker-engine.js
// DEFAULT_STICKER_TEMPLATE): 50 x 25 mm, one across, on a roll.
var builtInStickerShape = stickerRollShape{W: 50, H: 25, PerRow: 1, PerCol: 1}

// parseStickerRollShape reads a StickerTemplate's size and its design's
// settings the way the sticker engine's templateFromDoc / normaliseSettings
// do, defaults and limits included, so both sides agree on what fits.
func parseStickerRollShape(widthRaw, heightRaw, designRaw string) stickerRollShape {
	mm := func(raw string, def float64) float64 {
		if v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64); err == nil && v != 0 {
			return v
		}
		return def
	}
	s := stickerRollShape{W: mm(widthRaw, 50), H: mm(heightRaw, 25), PerRow: 1, PerCol: 1}
	var design struct {
		Settings struct {
			PerRow    interface{} `json:"per_row"`
			PerCol    interface{} `json:"per_col"`
			Direction string      `json:"direction"`
			Sheet     struct {
				Mode string `json:"mode"`
			} `json:"sheet"`
		} `json:"settings"`
	}
	if json.Unmarshal([]byte(designRaw), &design) != nil {
		return s
	}
	count := func(v interface{}) int {
		var n float64
		switch x := v.(type) {
		case float64:
			n = x
		case string:
			n, _ = strconv.ParseFloat(strings.TrimSpace(x), 64)
		}
		if r := int(math.Round(n)); r >= 1 {
			return min(r, 10)
		}
		return 1
	}
	s.PerRow, s.PerCol = count(design.Settings.PerRow), count(design.Settings.PerCol)
	s.Vertical = design.Settings.Direction == "vertical"
	s.Sheet = design.Settings.Sheet.Mode == "a4"
	return s
}

// stickerTemplateShape loads one template's roll shape; false when it is gone.
func stickerTemplateShape(schema, id string) (stickerRollShape, bool) {
	var w, h, design string
	err := db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(data->>'label_width_mm', ''), COALESCE(data->>'label_height_mm', ''),
		       COALESCE(data->>'design', '')
		FROM %s.documents WHERE doctype = 'StickerTemplate' AND id = $1 AND deleted_at IS NULL`, schema), id).Scan(&w, &h, &design)
	if err != nil {
		return stickerRollShape{}, false
	}
	return parseStickerRollShape(w, h, design), true
}

// printerRollTemplate is the template a printer's preset names for its roll,
// or "" for a printer with no preset.
func printerRollTemplate(schema, printerCode string) string {
	if strings.TrimSpace(printerCode) == "" {
		return ""
	}
	var id string
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(data->>'sticker_template', '') FROM %s.documents
		WHERE doctype = 'Printer' AND (id = $1 OR data->>'code' = $1) AND deleted_at IS NULL
		ORDER BY (id = $1) DESC LIMIT 1`, schema), printerCode).Scan(&id)
	return strings.TrimSpace(id)
}

// applyPrinterRoll is the printer's preset winning over the rules (decision
// 2026-10-25): tmpl when it fits the printer's roll (or the printer has no
// preset), else the roll's own template. A preset naming a template that is
// Inactive or gone is no preset, and the rules decide as before.
func applyPrinterRoll(tenantID, schema, printerCode string, tmpl *StickerTemplate) *StickerTemplate {
	rollID := printerRollTemplate(schema, printerCode)
	if rollID == "" || (tmpl != nil && tmpl.ID == rollID) {
		return tmpl
	}
	roll, err := ChosenStickerTemplate(tenantID, rollID)
	if err != nil || roll == nil {
		log.Printf("[STICKERS] printer %s names roll template %q, which is not an active template - ignored", printerCode, rollID)
		return tmpl
	}
	rollShape, ok := stickerTemplateShape(schema, roll.ID)
	if !ok {
		return tmpl
	}
	shape := builtInStickerShape
	if tmpl != nil {
		if shape, ok = stickerTemplateShape(schema, tmpl.ID); !ok {
			return roll
		}
	}
	if shape.fits(rollShape) {
		return tmpl
	}
	return roll
}
