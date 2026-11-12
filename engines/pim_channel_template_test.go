package engines

import (
	"custom_erp/db"
	"strings"
	"testing"
)

// Stage 146.5.2 (D14): a marketplace's own category file becomes a template
// per channel + category, with mandatory flags, accepted values, max lengths
// and conditional mandatory; coverage counts; a bad value skips that item
// with the column and the reason; clean variations get a parent row.

// s14652File is a synthetic category sheet shaped like the marketplaces' own:
// a definitions sheet (column, requirement, accepted values, length, rule) and
// a valid-values sheet with one column per attribute. No real marketplace file
// is bundled; this only proves the reader's tolerant header matching.
func s14652File(t *testing.T) []byte {
	t.Helper()
	cell := func(s string) XLSXCell { return XLSXCell{Text: s} }
	row := func(v ...string) []XLSXCell {
		out := make([]XLSXCell, len(v))
		for i, s := range v {
			out[i] = cell(s)
		}
		return out
	}
	defs := XLSXSheet{Name: "Data Definitions", Rows: [][]XLSXCell{
		row("Necklace category template - fill the Template sheet"),
		row(""),
		row("Field Name", "Required?", "Accepted Values", "Max Length", "Required If"),
		row("SKU", "Required", "", "20", ""),
		row("Title", "Required", "", "40", ""),
		row("Colour", "Required", "Red | Blue | Green", "", ""),
		row("Material", "Optional", "", "", ""),
		row("Hallmark No", "Optional", "", "", "Material=Gold"),
		row("Parentage", "Optional", "", "", ""),
		row("Parent SKU", "Optional", "", "", ""),
		row("Theme", "Optional", "", "", ""),
		row("Size", "Optional", "", "", ""),
	}}
	valid := XLSXSheet{Name: "Valid Values", Rows: [][]XLSXCell{
		row("Material", "Size"),
		row("Gold", "S"),
		row("Silver", "M"),
		row("", "L"),
	}}
	data, err := WriteXLSXBook([]XLSXSheet{defs, valid})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestStage14652ParseTemplateFile(t *testing.T) {
	parsed, err := ParseChannelTemplateXLSX(s14652File(t))
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]ChannelTemplateColumn{}
	for _, c := range parsed.Columns {
		by[c.Column] = c
	}
	if len(parsed.Columns) != 9 {
		t.Fatalf("columns = %d, want 9: %+v", len(parsed.Columns), parsed.Columns)
	}
	if !by["SKU"].Mandatory || by["SKU"].MaxLength != 20 || !by["Title"].Mandatory || by["Title"].MaxLength != 40 {
		t.Errorf("mandatory / length not read: %+v %+v", by["SKU"], by["Title"])
	}
	if by["Material"].Mandatory {
		t.Error("Optional was read as mandatory")
	}
	if got := strings.Join(by["Colour"].AllowedValues, ","); got != "Red,Blue,Green" {
		t.Errorf("Colour accepted values = %q", got)
	}
	// the Valid Values sheet fills a column whose own cell is blank
	if got := strings.Join(by["Material"].AllowedValues, ","); got != "Gold,Silver" {
		t.Errorf("Material accepted values from the Valid Values sheet = %q", got)
	}
	if by["Hallmark No"].RequiredIf != "Material=Gold" {
		t.Errorf("required-if = %q", by["Hallmark No"].RequiredIf)
	}
	if _, err := ParseChannelTemplateXLSX([]byte("not a workbook")); err == nil {
		t.Error("a non-workbook was accepted")
	}
}

type s14652Fixture struct {
	schema, channel, category, tplID, exportID string
}

// s14652Setup makes a channel, its category map, field maps for the template's
// columns, and the template loaded from the synthetic file.
func s14652Setup(t *testing.T) (s14652Fixture, func()) {
	t.Helper()
	schema, _, cleanup := pimTestFixture(t, "S14652")
	f := s14652Fixture{schema: schema, channel: "S14652-CH", category: "Necklace"}
	clean := func() {
		cleanup()
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id LIKE 'S14652%' OR id LIKE 'TPL-S14652%' OR id LIKE 'CCT-S14652%'")
	}
	pimInsertDoc(t, schema, f.channel, "Channel", "Active", map[string]interface{}{"code": f.channel, "name": "S14652 channel", "default_locale": "en"})
	pimInsertDoc(t, schema, "S14652-CAT", "ChannelCategoryMap", "Active", map[string]interface{}{"channel": f.channel, "erp_category": "NECK", "channel_category": f.category})
	pimInsertDoc(t, schema, "S14652-CAT2", "ChannelCategoryMap", "Active", map[string]interface{}{"channel": f.channel, "erp_category": "EAR", "channel_category": "Earring"})
	maps := [][2]string{{"code", "SKU"}, {"title_src", "Title"}, {"colour_src", "Colour"}, {"material_src", "Material"}, {"hallmark_src", "Hallmark No"}}
	for i, m := range maps {
		pimInsertDoc(t, schema, "S14652-FM"+string(rune('A'+i)), "ChannelFieldMap", "Active", map[string]interface{}{
			"channel": f.channel, "source_field": m[0], "target_field": m[1], "mandatory": "No"})
	}
	res, err := LoadChannelTemplate("default", "system", f.channel, f.category, s14652File(t), ChannelTemplateOptions{
		ParentRows: true, ParentageColumn: "Parentage", ParentSKUColumn: "Parent SKU", ThemeColumn: "Theme"})
	if err != nil {
		clean()
		t.Fatalf("load template: %v", err)
	}
	f.tplID, f.exportID = res.TemplateID, res.ExportTemplateID
	return f, clean
}

func s14652Item(t *testing.T, f s14652Fixture, id, category string, data map[string]interface{}) {
	t.Helper()
	d := map[string]interface{}{"code": id, "name": id, "category": category}
	for k, v := range data {
		d[k] = v
	}
	pimInsertDoc(t, f.schema, id, "Item", "Active", d)
}

func TestStage14652LoadCoverageAndExport(t *testing.T) {
	f, cleanup := s14652Setup(t)
	defer cleanup()

	// the load made the template and an export template that points at it
	var cols string
	if err := db.DB.QueryRow(`SELECT data->>'channel_template' FROM `+f.schema+`.documents WHERE id = $1`, f.exportID).Scan(&cols); err != nil || cols != f.tplID {
		t.Fatalf("export template %q does not point at %q: %q %v", f.exportID, f.tplID, cols, err)
	}

	// Items. A1 good; A2 colour outside the list; A3 title too long; A4 colour blank;
	// A5 gold without a hallmark (conditional mandatory); A6 an earring (other category).
	s14652Item(t, f, "S14652-A1", "NECK", map[string]interface{}{"title_src": "Gold chain", "colour_src": "Red", "material_src": "Silver"})
	s14652Item(t, f, "S14652-A2", "NECK", map[string]interface{}{"title_src": "Chain", "colour_src": "Purple"})
	s14652Item(t, f, "S14652-A3", "NECK", map[string]interface{}{"title_src": strings.Repeat("x", 41), "colour_src": "Blue"})
	s14652Item(t, f, "S14652-A4", "NECK", map[string]interface{}{"title_src": "Chain"})
	s14652Item(t, f, "S14652-A5", "NECK", map[string]interface{}{"title_src": "Chain", "colour_src": "Green", "material_src": "Gold"})
	s14652Item(t, f, "S14652-A6", "EAR", map[string]interface{}{"title_src": "Stud", "colour_src": "Red"})
	s14652Item(t, f, "S14652-A7", "NECK", map[string]interface{}{"title_src": "Gold chain 2", "colour_src": "Blue", "material_src": "Gold", "hallmark_src": "HM1"})

	out, err := RunPIMExport("default", f.exportID, PIMExportFilter{})
	if err != nil {
		t.Fatal(err)
	}
	skipped := map[string]PIMExportSkip{}
	for _, s := range out.Skipped {
		skipped[s.Item] = s
	}
	if out.Rows != 2 {
		t.Fatalf("exported rows = %d, want 2 (A1, A7); skipped: %+v", out.Rows, out.Skipped)
	}
	check := func(item, column, reasonPart string) {
		s, ok := skipped[item]
		if !ok {
			t.Errorf("%s was not skipped", item)
			return
		}
		if !strings.Contains(s.Column, column) || !strings.Contains(strings.ToLower(s.Reason), strings.ToLower(reasonPart)) {
			t.Errorf("%s skip = column %q reason %q, want column %q reason containing %q", item, s.Column, s.Reason, column, reasonPart)
		}
	}
	check("S14652-A2", "Colour", "Purple")
	check("S14652-A3", "Title", "40")
	check("S14652-A4", "Colour", "required")
	check("S14652-A5", "Hallmark No", "Material")
	if _, ok := skipped["S14652-A6"]; ok {
		t.Error("an item of another channel category was listed as skipped; it belongs to another template")
	}
	if !strings.Contains(string(out.Data), "S14652-A1") || strings.Contains(string(out.Data), "S14652-A2") {
		t.Errorf("file holds the wrong rows:\n%s", out.Data)
	}

	cov, err := ChannelTemplateCoverage("default", f.tplID)
	if err != nil {
		t.Fatal(err)
	}
	if cov.Items != 6 { // A1-A5 and A7; the earring is another category
		t.Errorf("coverage items = %d, want 6", cov.Items)
	}
	by := map[string]TemplateCoverageColumn{}
	for _, c := range cov.Columns {
		by[c.Column] = c
	}
	if c := by["Colour"]; !c.Mapped || c.Filled != 5 || !c.Mandatory {
		t.Errorf("Colour coverage = %+v, want mapped, filled 5, mandatory", c)
	}
	if c := by["Size"]; c.Mapped {
		t.Errorf("Size has no field map yet shows as mapped: %+v", c)
	}
	if cov.MandatoryTotal != 3 || cov.MandatoryMapped != 3 {
		t.Errorf("mandatory %d/%d, want 3/3", cov.MandatoryMapped, cov.MandatoryTotal)
	}
	// inactive columns do not count
	if _, err := db.DB.Exec(`UPDATE `+f.schema+`.documents SET data = jsonb_set(data, '{columns}', (
		SELECT jsonb_agg(CASE WHEN c->>'column' = 'Colour' THEN c || '{"active":"No"}'::jsonb ELSE c END) FROM jsonb_array_elements(data->'columns') c))
		WHERE id = $1`, f.tplID); err != nil {
		t.Fatal(err)
	}
	cov, err = ChannelTemplateCoverage("default", f.tplID)
	if err != nil {
		t.Fatal(err)
	}
	if cov.MandatoryTotal != 2 {
		t.Errorf("mandatory total with Colour inactive = %d, want 2", cov.MandatoryTotal)
	}
	// ...and an inactive column is not validated: A2 and A4 now export
	out, err = RunPIMExport("default", f.exportID, PIMExportFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if out.Rows != 4 { // A1 A2 A4 A7 (A3 title, A5 hallmark still fail)
		t.Errorf("rows with Colour inactive = %d, want 4; skipped %+v", out.Rows, out.Skipped)
	}
}

func TestStage14652ParentRowsOnlyWhenVariationIsClean(t *testing.T) {
	f, cleanup := s14652Setup(t)
	defer cleanup()
	// design D1: two combinations on Colour, clean. Design D2: the same colour twice, not clean.
	for _, c := range [][3]string{{"S14652-B1", "S14652-D1", "Colour:Red"}, {"S14652-B2", "S14652-D1", "Colour:Blue"},
		{"S14652-B3", "S14652-D2", "Colour:Red"}, {"S14652-B4", "S14652-D2", "Colour:Red"}} {
		s14652Item(t, f, c[0], "NECK", map[string]interface{}{"title_src": "Chain " + c[0], "colour_src": "Red", "parent_product_code": c[1], "variant_option_values": c[2]})
	}
	out, err := RunPIMExport("default", f.exportID, PIMExportFilter{})
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(out.Data)), "\n")
	header := strings.Split(strings.TrimSpace(lines[0]), ",")
	idx := map[string]int{}
	for i, h := range header {
		idx[h] = i
	}
	type rec struct{ parentage, parent, theme string }
	got := map[string]rec{}
	for _, l := range lines[1:] {
		c := strings.Split(strings.TrimSpace(l), ",")
		got[c[idx["SKU"]]] = rec{c[idx["Parentage"]], c[idx["Parent SKU"]], c[idx["Theme"]]}
	}
	if r := got["S14652-D1"]; r.parentage != "Parent" || r.theme != "Colour" {
		t.Errorf("clean design has no parent row: %+v (rows %v)", r, got)
	}
	for _, c := range []string{"S14652-B1", "S14652-B2"} {
		if r := got[c]; r.parentage != "Child" || r.parent != "S14652-D1" {
			t.Errorf("%s = %+v, want Child of D1", c, r)
		}
	}
	if _, ok := got["S14652-D2"]; ok {
		t.Error("a design whose combinations repeat got a parent row")
	}
	for _, c := range []string{"S14652-B3", "S14652-B4"} {
		if r := got[c]; r.parentage != "" || r.parent != "" {
			t.Errorf("%s = %+v, want a standalone row", c, r)
		}
	}
}

func TestStage14652ChannelWithoutTemplateIsUnchanged(t *testing.T) {
	schema, _, cleanup := pimTestFixture(t, "S14652N")
	defer cleanup()
	defer db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id LIKE 'S14652N%'")
	pimInsertDoc(t, schema, "S14652N-CH", "Channel", "Active", map[string]interface{}{"code": "S14652N-CH", "name": "n", "default_locale": "en"})
	pimInsertDoc(t, schema, "S14652N-FM", "ChannelFieldMap", "Active", map[string]interface{}{"channel": "S14652N-CH", "source_field": "colour_src", "target_field": "Colour", "mandatory": "No"})
	pimInsertDoc(t, schema, "S14652N-T", "PIMExportTemplate", "Active", map[string]interface{}{
		"code": "S14652N-T", "name": "plain", "channel": "S14652N-CH", "headerless": "No", "variant_mode": "All Rows",
		"column_mappings": []map[string]string{{"field_key": "item_code", "column_header": "SKU"}, {"field_key": "Colour", "column_header": "Colour"}}})
	pimInsertDoc(t, schema, "S14652N-X", "Item", "Active", map[string]interface{}{"code": "S14652N-X", "name": "x", "colour_src": "Purple"})
	out, err := RunPIMExport("default", "S14652N-T", PIMExportFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out.Data), "S14652N-X,Purple") || len(out.Skipped) != 0 {
		t.Errorf("a channel with no template changed: skipped %+v\n%s", out.Skipped, out.Data)
	}
}
