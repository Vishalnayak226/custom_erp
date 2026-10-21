package engines

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"path"
	"strconv"
	"strings"
)

// Stage 128: a minimal .xlsx reader, the read half of xlsx_writer.go - an
// .xlsx is a zip of XML parts, so archive/zip + encoding/xml cover it with no
// new dependency (lifted from cmd/genattrlib, which reads the attribute
// master the same way). It returns cell text only: shared and inline strings,
// numbers, booleans and formula results. Styles, dates and pictures are not
// read; the setup workbook never needs them.

// XLSXReadSheet is one worksheet as read: its name and rows of cell text.
// RowNumbers[i] is the 1-based Excel row of Rows[i], so a message can name
// the row the user sees even when Excel left blank rows out of the file.
type XLSXReadSheet struct {
	Name       string
	Rows       [][]string
	RowNumbers []int
}

// xlsxPartLimit caps one decompressed part, so a small upload cannot expand
// into gigabytes (a zip bomb). A setup workbook's largest part is a few MB.
const xlsxPartLimit = 64 << 20

// ReadXLSX returns every worksheet of an .xlsx file, in workbook order.
func ReadXLSX(data []byte) ([]XLSXReadSheet, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("this is not an Excel workbook (.xlsx): %v", err)
	}
	parts := map[string]*zip.File{}
	for _, f := range zr.File {
		parts[f.Name] = f
	}
	decode := func(name string, v interface{}) error {
		f, ok := parts[name]
		if !ok {
			return fmt.Errorf("the workbook has no %s - save it again from Excel as .xlsx", name)
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		defer rc.Close()
		lr := &io.LimitedReader{R: rc, N: xlsxPartLimit + 1}
		if err := xml.NewDecoder(lr).Decode(v); err != nil {
			return fmt.Errorf("could not read %s: %v", name, err)
		}
		if lr.N <= 0 {
			return fmt.Errorf("%s is too large", name)
		}
		return nil
	}

	var wb struct {
		Sheets []struct {
			Name string `xml:"name,attr"`
			RID  string `xml:"http://schemas.openxmlformats.org/officeDocument/2006/relationships id,attr"`
		} `xml:"sheets>sheet"`
	}
	if err := decode("xl/workbook.xml", &wb); err != nil {
		return nil, err
	}
	var rels struct {
		Rel []struct {
			ID     string `xml:"Id,attr"`
			Target string `xml:"Target,attr"`
		} `xml:"Relationship"`
	}
	if err := decode("xl/_rels/workbook.xml.rels", &rels); err != nil {
		return nil, err
	}
	target := map[string]string{}
	for _, r := range rels.Rel {
		target[r.ID] = path.Join("xl", strings.TrimPrefix(strings.TrimPrefix(r.Target, "/"), "xl/"))
	}

	type richText struct {
		T string `xml:"t"`
		R []struct {
			T string `xml:"t"`
		} `xml:"r"`
	}
	text := func(rt richText) string {
		s := rt.T
		for _, run := range rt.R {
			s += run.T
		}
		return s
	}
	var shared []string
	if _, ok := parts["xl/sharedStrings.xml"]; ok {
		var sst struct {
			SI []richText `xml:"si"`
		}
		if err := decode("xl/sharedStrings.xml", &sst); err != nil {
			return nil, err
		}
		for _, si := range sst.SI {
			shared = append(shared, text(si))
		}
	}

	var out []XLSXReadSheet
	for _, s := range wb.Sheets {
		var ws struct {
			Rows []struct {
				R     int `xml:"r,attr"`
				Cells []struct {
					Ref    string   `xml:"r,attr"`
					Type   string   `xml:"t,attr"`
					Value  string   `xml:"v"`
					Inline richText `xml:"is"`
				} `xml:"c"`
			} `xml:"sheetData>row"`
		}
		if err := decode(target[s.RID], &ws); err != nil {
			return nil, err
		}
		sheet := XLSXReadSheet{Name: s.Name}
		for i, row := range ws.Rows {
			var cells []string
			for j, c := range row.Cells {
				col := j
				if c.Ref != "" {
					col = xlsxColumnIndex(c.Ref)
				}
				if col < 0 || col > 16383 {
					continue
				}
				for len(cells) <= col {
					cells = append(cells, "")
				}
				v := c.Value
				switch c.Type {
				case "s":
					if n, err := strconv.Atoi(strings.TrimSpace(c.Value)); err == nil && n >= 0 && n < len(shared) {
						v = shared[n]
					}
				case "inlineStr":
					v = text(c.Inline)
				case "b":
					if strings.TrimSpace(c.Value) == "1" {
						v = "TRUE"
					} else {
						v = "FALSE"
					}
				case "str", "e":
				default:
					v = xlsxNumberText(v)
				}
				cells[col] = strings.TrimSpace(v)
			}
			n := row.R
			if n <= 0 {
				n = i + 1
			}
			sheet.Rows = append(sheet.Rows, cells)
			sheet.RowNumbers = append(sheet.RowNumbers, n)
		}
		out = append(out, sheet)
	}
	return out, nil
}

// xlsxNumberText prints a numeric cell the way it was typed: 18 not 18.0,
// and 0.1 not the 0.10000000000000001 a binary float can be stored as.
func xlsxNumberText(v string) string {
	f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil {
		return v
	}
	return strconv.FormatFloat(math.Round(f*1e9)/1e9, 'f', -1, 64)
}

// xlsxColumnIndex turns a cell reference such as "AB12" into a zero-based column.
func xlsxColumnIndex(ref string) int {
	n := 0
	for _, ch := range strings.ToUpper(ref) {
		if ch < 'A' || ch > 'Z' {
			break
		}
		n = n*26 + int(ch-'A'+1)
	}
	return n - 1
}
