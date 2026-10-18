// Command genattrlib turns the industry product-attribute master workbook
// (ERP_Industry_Product_Attribute_Master.xlsx, maintained by the operator)
// into engines/attribute_library.json, the attribute library shipped inside
// the server binary (Stage 81). The workbook is the source of truth; rerun
// this after editing it:
//
//	go run ./cmd/genattrlib -xlsx "C:\...\ERP_Industry_Product_Attribute_Master.xlsx"
//
// It reads only the two canonical sheets the workbook itself names as the
// source of truth - 03_All_Attributes (definitions) and 04_Value_Set_Master
// (dropdown values) - plus 01_Industry_Index for industry names. The
// per-industry tabs are views of those and are ignored. Standard library only:
// an .xlsx is a zip of XML parts.
package main

import (
	"archive/zip"
	"encoding/json"
	"encoding/xml"
	"flag"
	"fmt"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
)

type industry struct {
	Code       string `json:"code"`
	Name       string `json:"name"`
	Categories string `json:"categories,omitempty"`
	Focus      string `json:"focus,omitempty"`
}

type attribute struct {
	ID         string `json:"id"`
	Industry   string `json:"industry"`
	Group      string `json:"group"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	ValueSet   string `json:"value_set,omitempty"`
	UOM        string `json:"uom,omitempty"`
	Mandatory  string `json:"mandatory,omitempty"`
	Variant    string `json:"variant"`
	Scope      string `json:"scope"`
	Categories string `json:"categories,omitempty"`
	Facet      string `json:"facet,omitempty"`
	Note       string `json:"note,omitempty"`
}

type value struct {
	Code  string `json:"code"`
	Label string `json:"label"`
}

type library struct {
	Source     string             `json:"source"`
	Industries []industry         `json:"industries"`
	Attributes []attribute        `json:"attributes"`
	ValueSets  map[string][]value `json:"value_sets"`
}

func main() {
	src := flag.String("xlsx", "", "path to ERP_Industry_Product_Attribute_Master.xlsx")
	out := flag.String("out", "engines/attribute_library.json", "output JSON path")
	flag.Parse()
	if *src == "" {
		fmt.Fprintln(os.Stderr, "genattrlib: -xlsx is required")
		os.Exit(2)
	}
	sheets, err := readWorkbook(*src)
	if err != nil {
		fmt.Fprintln(os.Stderr, "genattrlib:", err)
		os.Exit(1)
	}
	lib, err := build(sheets)
	if err != nil {
		fmt.Fprintln(os.Stderr, "genattrlib:", err)
		os.Exit(1)
	}
	lib.Source = path.Base(strings.ReplaceAll(*src, `\`, "/"))
	body, err := json.MarshalIndent(lib, "", " ")
	if err != nil {
		fmt.Fprintln(os.Stderr, "genattrlib:", err)
		os.Exit(1)
	}
	if err := os.WriteFile(*out, append(body, '\n'), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "genattrlib:", err)
		os.Exit(1)
	}
	values := 0
	for _, v := range lib.ValueSets {
		values += len(v)
	}
	fmt.Printf("genattrlib: %d industries, %d attributes, %d value sets (%d values) -> %s (%d bytes)\n",
		len(lib.Industries), len(lib.Attributes), len(lib.ValueSets), values, *out, len(body)+1)
}

// build reads the canonical sheets by header name, not column position, so a
// column the operator inserts or reorders in the draft does not shift fields.
func build(sheets map[string][][]string) (*library, error) {
	lib := &library{ValueSets: map[string][]value{}}

	index, err := table(sheets, "01_Industry_Index", "Industry Code")
	if err != nil {
		return nil, err
	}
	for _, r := range index {
		if r["Industry Code"] == "" {
			continue
		}
		lib.Industries = append(lib.Industries, industry{Code: r["Industry Code"], Name: r["Industry Name"],
			Categories: r["Applicable ERP Categories"], Focus: r["Specification Focus"]})
	}

	attrs, err := table(sheets, "03_All_Attributes", "Attribute ID")
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, r := range attrs {
		id := r["Attribute ID"]
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		lib.Attributes = append(lib.Attributes, attribute{
			ID: id, Industry: r["Industry Code"], Group: r["Attribute Group"], Name: r["Attribute Name"],
			Type: r["Data Type"], ValueSet: r["Value Set ID"], UOM: r["UOM"], Mandatory: r["Mandatory Policy"],
			Variant: r["Variant Driver"], Scope: r["Value Scope"], Categories: r["Applicable Categories"],
			Facet: r["Filterable / Facet"], Note: r["Validation / Implementation Notes"],
		})
	}

	vals, err := table(sheets, "04_Value_Set_Master", "Value Set ID")
	if err != nil {
		return nil, err
	}
	type ranked struct {
		value
		rank float64
	}
	sets := map[string][]ranked{}
	for _, r := range vals {
		set, label := r["Value Set ID"], r["Standard Display Value"]
		if set == "" || label == "" || (r["Status"] != "" && !strings.EqualFold(r["Status"], "Active")) {
			continue
		}
		rank, _ := strconv.ParseFloat(r["Sort Order"], 64)
		sets[set] = append(sets[set], ranked{value{Code: r["Value Code"], Label: label}, rank})
	}
	for set, list := range sets {
		sort.SliceStable(list, func(i, j int) bool { return list[i].rank < list[j].rank })
		for _, v := range list {
			lib.ValueSets[set] = append(lib.ValueSets[set], v.value)
		}
	}
	if len(lib.Attributes) == 0 || len(lib.ValueSets) == 0 {
		return nil, fmt.Errorf("no attributes or value sets found - is this the attribute master workbook?")
	}
	return lib, nil
}

// table returns a sheet's rows below the header row - the first row with a
// cell reading header - keyed by header text.
func table(sheets map[string][][]string, name, header string) ([]map[string]string, error) {
	rows, ok := sheets[name]
	if !ok {
		return nil, fmt.Errorf("sheet %q not found", name)
	}
	for i, row := range rows {
		isHeader := false
		for _, cell := range row {
			isHeader = isHeader || strings.TrimSpace(cell) == header
		}
		if !isHeader {
			continue
		}
		var out []map[string]string
		for _, r := range rows[i+1:] {
			m := map[string]string{}
			for c, h := range row {
				if c < len(r) {
					m[strings.TrimSpace(h)] = strings.TrimSpace(r[c])
				}
			}
			out = append(out, m)
		}
		return out, nil
	}
	return nil, fmt.Errorf("sheet %q has no %q header row", name, header)
}

// readWorkbook returns every sheet as rows of cell text.
func readWorkbook(file string) (map[string][][]string, error) {
	zr, err := zip.OpenReader(file)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	parts := map[string]*zip.File{}
	for _, f := range zr.File {
		parts[f.Name] = f
	}
	decode := func(name string, v interface{}) error {
		f, ok := parts[name]
		if !ok {
			return fmt.Errorf("%s missing from workbook", name)
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		defer rc.Close()
		return xml.NewDecoder(rc).Decode(v)
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
		target[r.ID] = path.Join("xl", strings.TrimPrefix(r.Target, "/xl/"))
	}

	var shared []string
	if _, ok := parts["xl/sharedStrings.xml"]; ok {
		var sst struct {
			SI []struct {
				T string `xml:"t"`
				R []struct {
					T string `xml:"t"`
				} `xml:"r"`
			} `xml:"si"`
		}
		if err := decode("xl/sharedStrings.xml", &sst); err != nil {
			return nil, err
		}
		for _, si := range sst.SI {
			text := si.T
			for _, run := range si.R {
				text += run.T
			}
			shared = append(shared, text)
		}
	}

	sheets := map[string][][]string{}
	for _, s := range wb.Sheets {
		var ws struct {
			Rows []struct {
				Cells []struct {
					Ref    string `xml:"r,attr"`
					Type   string `xml:"t,attr"`
					Value  string `xml:"v"`
					Inline string `xml:"is>t"`
				} `xml:"c"`
			} `xml:"sheetData>row"`
		}
		if err := decode(target[s.RID], &ws); err != nil {
			return nil, err
		}
		var rows [][]string
		for _, row := range ws.Rows {
			var cells []string
			for _, c := range row.Cells {
				col := columnIndex(c.Ref)
				for len(cells) <= col {
					cells = append(cells, "")
				}
				text := c.Value
				switch c.Type {
				case "s":
					if n, err := strconv.Atoi(c.Value); err == nil && n >= 0 && n < len(shared) {
						text = shared[n]
					}
				case "inlineStr":
					text = c.Inline
				}
				cells[col] = text
			}
			rows = append(rows, cells)
		}
		sheets[s.Name] = rows
	}
	return sheets, nil
}

// columnIndex turns a cell reference such as "AB12" into a zero-based column.
func columnIndex(ref string) int {
	n := 0
	for _, ch := range ref {
		if ch < 'A' || ch > 'Z' {
			break
		}
		n = n*26 + int(ch-'A'+1)
	}
	return n - 1
}
