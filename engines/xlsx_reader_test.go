package engines

import "testing"

// Stage 128: what WriteXLSXBook writes, ReadXLSX reads back - sheet order,
// text, numbers as typed, and the Excel row numbers messages name.
func TestXLSXBookRoundTrip(t *testing.T) {
	file, err := WriteXLSXBook([]XLSXSheet{
		{Name: "Vendors", FreezeRows: 1, Rows: [][]XLSXCell{
			{{Text: "Vendor Name", Header: true}, {Text: "GST", Header: true}},
			{{Text: "# hint", Hint: true}},
			{{Text: `Shree "A" & Sons <Ltd>`}, {Num: 18, IsNum: true}},
			{},
			{{Text: "Second"}, {Num: 0.1, IsNum: true}},
		}, Validations: []XLSXListValidation{{Col: 1, FromRow: 2, ToRow: 10, Options: []string{"Yes", "No"}}}},
		{Name: "Settings", Rows: [][]XLSXCell{{{Text: "Key"}, {Text: "Value"}}, {{Text: "a"}, {Text: "Yes"}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	sheets, err := ReadXLSX(file)
	if err != nil {
		t.Fatal(err)
	}
	if len(sheets) != 2 || sheets[0].Name != "Vendors" || sheets[1].Name != "Settings" {
		t.Fatalf("sheets = %+v", sheets)
	}
	v := sheets[0]
	if got := v.Rows[2][0]; got != `Shree "A" & Sons <Ltd>` {
		t.Errorf("text = %q", got)
	}
	if got := v.Rows[2][1]; got != "18" {
		t.Errorf("number = %q, want 18", got)
	}
	last := len(v.Rows) - 1
	if v.Rows[last][0] != "Second" || v.Rows[last][1] != "0.1" || v.RowNumbers[last] != 5 {
		t.Errorf("last row = %q at %d, want Second / 0.1 at row 5", v.Rows[last], v.RowNumbers[last])
	}
	if _, err := ReadXLSX([]byte("not a workbook")); err == nil {
		t.Error("a non-xlsx file must be refused")
	}
}

// One sheet still writes exactly one sheet, as the Vendor PO export expects.
func TestWriteXLSXSingleSheetStillReads(t *testing.T) {
	file, err := WriteXLSX(XLSXSheet{Name: "PO/1: [x]", Rows: [][]XLSXCell{{{Text: "a"}}}})
	if err != nil {
		t.Fatal(err)
	}
	sheets, err := ReadXLSX(file)
	if err != nil {
		t.Fatal(err)
	}
	if len(sheets) != 1 || sheets[0].Name != "PO-1- -x-" || sheets[0].Rows[0][0] != "a" {
		t.Fatalf("sheets = %+v", sheets)
	}
}
