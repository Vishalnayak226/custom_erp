package engines

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"strings"
)

// Stage 63.6: a minimal Office Open XML (.xlsx) writer - one worksheet,
// text and number cells, bold rows, column widths, and pictures anchored to
// cells. Hand-written on archive/zip + encoding/xml rather than a library, per
// the "no new dependency" rule; it covers exactly what the Vendor PO export
// needs and nothing more.

// XLSXCell is one cell: Text, or Number when IsNum.
type XLSXCell struct {
	Text  string
	Num   float64
	IsNum bool
	Bold  bool
}

// XLSXImage is a picture placed over one cell.
type XLSXImage struct {
	Row, Col int // zero-based
	Data     []byte
	Mime     string // image/jpeg or image/png
}

// XLSXSheet is the whole workbook's single sheet.
type XLSXSheet struct {
	Name       string
	ColWidths  []float64 // characters
	Rows       [][]XLSXCell
	RowHeights map[int]float64 // points, zero-based row
	Images     []XLSXImage
}

func xlsxColName(i int) string {
	name := ""
	for i++; i > 0; i = (i - 1) / 26 {
		name = string(rune('A'+(i-1)%26)) + name
	}
	return name
}

func xmlEscape(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

// WriteXLSX renders the sheet as .xlsx bytes.
func WriteXLSX(s XLSXSheet) ([]byte, error) {
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	put := func(name, body string) error {
		w, err := z.Create(name)
		if err != nil {
			return err
		}
		_, err = w.Write([]byte(body))
		return err
	}
	sheetName := s.Name
	if sheetName == "" {
		sheetName = "Sheet1"
	}
	if len(sheetName) > 31 {
		sheetName = sheetName[:31]
	}
	for _, bad := range []string{"/", "\\", "?", "*", "[", "]", ":"} {
		sheetName = strings.ReplaceAll(sheetName, bad, "-")
	}
	hasImages := len(s.Images) > 0

	ct := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
<Default Extension="xml" ContentType="application/xml"/>
<Default Extension="jpeg" ContentType="image/jpeg"/>
<Default Extension="png" ContentType="image/png"/>
<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>
<Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>
<Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>`
	if hasImages {
		ct += `
<Override PartName="/xl/drawings/drawing1.xml" ContentType="application/vnd.openxmlformats-officedocument.drawing+xml"/>`
	}
	ct += "\n</Types>"
	steps := []struct{ name, body string }{
		{"[Content_Types].xml", ct},
		{"_rels/.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`},
		{"xl/workbook.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="` + xmlEscape(sheetName) + `" sheetId="1" r:id="rId1"/></sheets></workbook>`},
		{"xl/_rels/workbook.xml.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/><Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/></Relationships>`},
		{"xl/styles.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><numFmts count="1"><numFmt numFmtId="164" formatCode="#,##0.00"/></numFmts><fonts count="2"><font><sz val="11"/><name val="Calibri"/></font><font><b/><sz val="11"/><name val="Calibri"/></font></fonts><fills count="2"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill></fills><borders count="1"><border><left/><right/><top/><bottom/><diagonal/></border></borders><cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs><cellXfs count="4"><xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0" applyAlignment="1"><alignment vertical="center" wrapText="1"/></xf><xf numFmtId="0" fontId="1" fillId="0" borderId="0" xfId="0" applyFont="1" applyAlignment="1"><alignment vertical="center" wrapText="1"/></xf><xf numFmtId="164" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1" applyAlignment="1"><alignment vertical="center"/></xf><xf numFmtId="164" fontId="1" fillId="0" borderId="0" xfId="0" applyNumberFormat="1" applyFont="1" applyAlignment="1"><alignment vertical="center"/></xf></cellXfs></styleSheet>`},
	}
	for _, st := range steps {
		if err := put(st.name, st.body); err != nil {
			return nil, err
		}
	}

	var sh strings.Builder
	sh.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">`)
	if len(s.ColWidths) > 0 {
		sh.WriteString("<cols>")
		for i, w := range s.ColWidths {
			fmt.Fprintf(&sh, `<col min="%d" max="%d" width="%.2f" customWidth="1"/>`, i+1, i+1, w)
		}
		sh.WriteString("</cols>")
	}
	sh.WriteString("<sheetData>")
	for r, row := range s.Rows {
		if h, ok := s.RowHeights[r]; ok {
			fmt.Fprintf(&sh, `<row r="%d" ht="%.1f" customHeight="1">`, r+1, h)
		} else {
			fmt.Fprintf(&sh, `<row r="%d">`, r+1)
		}
		for c, cell := range row {
			ref := fmt.Sprintf("%s%d", xlsxColName(c), r+1)
			style := 0
			if cell.Bold {
				style = 1
			}
			if cell.IsNum {
				fmt.Fprintf(&sh, `<c r="%s" s="%d"><v>%s</v></c>`, ref, style+2, strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.4f", cell.Num), "0"), "."))
			} else if cell.Text != "" {
				fmt.Fprintf(&sh, `<c r="%s" s="%d" t="inlineStr"><is><t xml:space="preserve">%s</t></is></c>`, ref, style, xmlEscape(cell.Text))
			}
		}
		sh.WriteString("</row>")
	}
	sh.WriteString("</sheetData>")
	if hasImages {
		sh.WriteString(`<drawing r:id="rId1"/>`)
	}
	sh.WriteString("</worksheet>")
	if err := put("xl/worksheets/sheet1.xml", sh.String()); err != nil {
		return nil, err
	}

	if hasImages {
		if err := put("xl/worksheets/_rels/sheet1.xml.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/drawing" Target="../drawings/drawing1.xml"/></Relationships>`); err != nil {
			return nil, err
		}
		var dr, rels strings.Builder
		dr.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<xdr:wsDr xmlns:xdr="http://schemas.openxmlformats.org/drawingml/2006/spreadsheetDrawing" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">`)
		rels.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`)
		for i, img := range s.Images {
			ext := "jpeg"
			if img.Mime == "image/png" {
				ext = "png"
			}
			media := fmt.Sprintf("image%d.%s", i+1, ext)
			w, err := z.Create("xl/media/" + media)
			if err != nil {
				return nil, err
			}
			if _, err := w.Write(img.Data); err != nil {
				return nil, err
			}
			fmt.Fprintf(&rels, `<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="../media/%s"/>`, i+1, media)
			// A 54 pt square, inset 3 pt into the cell (EMU: 1 pt = 12700);
			// the row is 60 pt high.
			fmt.Fprintf(&dr, `<xdr:oneCellAnchor><xdr:from><xdr:col>%d</xdr:col><xdr:colOff>38100</xdr:colOff><xdr:row>%d</xdr:row><xdr:rowOff>38100</xdr:rowOff></xdr:from><xdr:ext cx="685800" cy="685800"/><xdr:pic><xdr:nvPicPr><xdr:cNvPr id="%d" name="Picture %d"/><xdr:cNvPicPr><a:picLocks noChangeAspect="1"/></xdr:cNvPicPr></xdr:nvPicPr><xdr:blipFill><a:blip r:embed="rId%d"/><a:stretch><a:fillRect/></a:stretch></xdr:blipFill><xdr:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="685800" cy="685800"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></xdr:spPr></xdr:pic><xdr:clientData/></xdr:oneCellAnchor>`,
				img.Col, img.Row, i+2, i+1, i+1)
		}
		dr.WriteString("</xdr:wsDr>")
		rels.WriteString("</Relationships>")
		if err := put("xl/drawings/drawing1.xml", dr.String()); err != nil {
			return nil, err
		}
		if err := put("xl/drawings/_rels/drawing1.xml.rels", rels.String()); err != nil {
			return nil, err
		}
	}
	if err := z.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// BuildVendorPOWorkbook is the Vendor PO as an Excel sheet: the header
// (brand, PO number, date, buyer store, vendor), one row per line with the
// item's picture where it has one, and the GST split and totals.
func BuildVendorPOWorkbook(tenantID, poID, store string) ([]byte, string, error) {
	d, err := BuildPrintDocument(tenantID, PrintTxnVendorPO, poID, store, false)
	if err != nil {
		return nil, "", err
	}
	txt := func(s string) XLSXCell { return XLSXCell{Text: s} }
	bold := func(s string) XLSXCell { return XLSXCell{Text: s, Bold: true} }
	num := func(v float64) XLSXCell { return XLSXCell{Num: v, IsNum: true} }
	addr := func(p PrintStore) string {
		parts := []string{p.Address, p.City, p.Pincode, p.State}
		var keep []string
		for _, x := range parts {
			if strings.TrimSpace(x) != "" {
				keep = append(keep, x)
			}
		}
		s := strings.Join(keep, ", ")
		if p.GSTIN != "" {
			s += " | GSTIN " + p.GSTIN
		}
		return s
	}
	sheet := XLSXSheet{Name: "PO " + d.Number, RowHeights: map[int]float64{},
		ColWidths: []float64{5, 14, 20, 34, 11, 8, 12, 12, 8, 13, 13}}
	sheet.Rows = append(sheet.Rows,
		[]XLSXCell{bold(d.Brand["name"])},
		[]XLSXCell{bold("Purchase Order " + d.Number), {}, {}, txt("Date: " + d.Date)},
		[]XLSXCell{bold("Buyer"), {}, txt(d.Store.LegalName + " - " + d.Store.Name), txt(addr(d.Store))},
		[]XLSXCell{bold("Vendor"), {}, txt(d.Party.Name), txt(addr(d.Party))},
		[]XLSXCell{bold("Ship to"), {}, txt(d.ShipTo.Name), txt(addr(d.ShipTo))},
		[]XLSXCell{},
		[]XLSXCell{bold("#"), bold("Image"), bold("SKU"), bold("Item"), bold("HSN"), bold("Qty"), bold("Rate"), bold("MRP"), bold("GST %"), bold("Taxable"), bold("Amount")},
	)
	for _, l := range d.Lines {
		r := len(sheet.Rows)
		sku := strField(l, "sku")
		name := fmt.Sprintf("%v", l["item_name"])
		if name == "<nil>" {
			name = ""
		}
		sheet.Rows = append(sheet.Rows, []XLSXCell{num(numFromInterface(l["sr"])), {}, txt(sku), txt(name), txt(fmt.Sprintf("%v", l["hsn_code"])),
			num(numFromInterface(l["qty"])), num(numFromInterface(l["rate"])), num(numFromInterface(l["mrp"])),
			num(numFromInterface(l["gst_rate"])), num(numFromInterface(l["taxable"])), num(numFromInterface(l["amount"]))})
		if img, mime := ItemImageBytes(tenantID, sku); len(img) > 0 {
			sheet.Images = append(sheet.Images, XLSXImage{Row: r, Col: 1, Data: img, Mime: mime})
			sheet.RowHeights[r] = 60
		}
	}
	sheet.Rows = append(sheet.Rows, []XLSXCell{})
	tot := func(label string, v float64) {
		sheet.Rows = append(sheet.Rows, []XLSXCell{{}, {}, {}, {}, {}, {}, {}, {}, bold(label), {}, {Num: v, IsNum: true, Bold: true}})
	}
	tot("Taxable", d.Totals["taxable"])
	if d.Interstate {
		tot("IGST", d.Totals["igst"])
	} else {
		tot("CGST", d.Totals["cgst"])
		tot("SGST", d.Totals["sgst"])
	}
	if d.Totals["round_off"] != 0 {
		tot("Round off", d.Totals["round_off"])
	}
	tot("Grand total", d.Totals["grand_total"])
	sheet.Rows = append(sheet.Rows, []XLSXCell{{}, {}, txt(d.AmountInWords)})
	for k, row := range sheet.Rows {
		for i := range row {
			if row[i].Text == "<nil>" {
				sheet.Rows[k][i].Text = ""
			}
		}
	}
	out, err := WriteXLSX(sheet)
	name := "PO-" + strings.NewReplacer("/", "-", "\\", "-", " ", "_").Replace(d.Number) + ".xlsx"
	return out, name, err
}
