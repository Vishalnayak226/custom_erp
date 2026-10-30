package engines

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Stage 47.13.2: the column dictionary that goes with a CSV import template.
// GET /api/v1/import/{doctype}/template?variant=dictionary returns it: one row
// per template column, in the template's own order, saying what the column
// is, its type and format, whether it is required, the values it accepts, an
// example, which records must exist first, whether it must be unique and what
// an update does to it - then the rules for the file as a whole.
//
// It is derived from the same doctype meta the template and the importer
// read (GetDocTypeMeta, importTemplateHeaders, ValidateDocument's rules,
// DetectFieldFormat), so it covers every doctype and cannot describe a column
// the template does not have. Every statement in it is what the importer in
// engines/import.go does today - in particular an update MERGES into the
// stored record (47.13.2a): a blank cell or a missing column keeps the stored
// value, and only ImportClearMarker clears a field.

// ImportTemplateVariantDictionary selects the dictionary on the template route.
const ImportTemplateVariantDictionary = "dictionary"

// ImportColumnSpec is one dictionary row.
type ImportColumnSpec struct {
	Order      int
	Column     string
	Label      string
	Type       string
	Required   string
	Definition string
	Format     string
	Allowed    string
	Example    string
	DependsOn  string
	Unique     string
	OnUpdate   string
}

var importDictionaryHeader = []string{"order", "column", "label", "type", "required", "definition", "format",
	"allowed_values", "example", "depends_on", "unique", "on_update"}

// ImportTemplateDictionary describes every column of doctype's template, in
// template order, followed by the file-wide rules (Order 0).
func ImportTemplateDictionary(tenantID, doctype string) ([]ImportColumnSpec, error) {
	headers, err := importTemplateHeaders(tenantID, doctype)
	if err != nil {
		return nil, err
	}
	fields, err := GetDocTypeMeta(tenantID, doctype)
	if err != nil {
		return nil, err
	}
	if len(fields) == 0 {
		return nil, &ValidationError{Code: "META-0196", Message: fmt.Sprintf("doctype %s metadata fields not found", doctype)}
	}
	byName := make(map[string]FieldMeta, len(fields))
	for _, f := range fields {
		byName[f.Fieldname] = f
	}
	isMaster, _ := IsMasterDoctype(tenantID, doctype)
	country := TenantPhoneCountry(tenantID)

	out := make([]ImportColumnSpec, 0, len(headers)+4)
	var deps []string
	seenDep := map[string]bool{}
	for i, h := range headers {
		var spec ImportColumnSpec
		if h == "id" {
			spec = ImportColumnSpec{Column: "id", Label: "ID", Type: "Text", Required: "No",
				Definition: "The record's id. Leave blank to create a new record (it is numbered for you); fill it with an existing id to update that record.",
				Example:    "leave blank for a new record", Unique: "Yes - once per file and once per record type",
				OnUpdate: "Selects the record to update; the id itself never changes"}
			if isMaster {
				spec.Definition += " For this master the id is its code."
			}
		} else {
			spec = describeImportColumn(doctype, byName[h], isMaster, country)
			if spec.DependsOn != "" && !seenDep[spec.DependsOn] {
				seenDep[spec.DependsOn] = true
				deps = append(deps, spec.DependsOn)
			}
		}
		spec.Order = i + 1
		out = append(out, spec)
	}

	maxRows := GetSettingInt(tenantID, "platform.max_import_rows")
	file := ImportColumnSpec{Column: "(file)", Type: "CSV",
		Definition: "First row: the column names as in the template (upper or lower case both work). One row per record. Every required column must be there; an optional one may be left out, but see (update).",
		Format:     "UTF-8 text, comma separated, double quotes around a cell that holds a comma, quote or line break."}
	if maxRows > 0 {
		file.Definition += fmt.Sprintf(" At most %d rows per file - split a larger one.", maxRows)
	}
	order := ImportColumnSpec{Column: "(order)", Type: "Dependencies",
		Definition: "Records a column links to must already exist (or come earlier in the same file).", DependsOn: strings.Join(deps, ", ")}
	if len(deps) == 0 {
		order.Definition = "This record type links to no other; it can be imported on its own."
	} else {
		order.Definition += " Import these first: " + strings.Join(deps, ", ") + "."
	}
	update := ImportColumnSpec{Column: "(update)", Type: "Behaviour",
		Definition: "A row with an existing id UPDATES that record: a filled cell replaces the stored value, a blank cell or a column left out of the file keeps it, and what the system keeps on the record itself (GST split, BOM snapshot, payments) stays. To empty a field, type " + ImportClearMarker + " in its cell. To change one field, fill the id and that column; the required columns must still be in the file, but their cells may stay blank.",
		Example:    ImportClearMarker + " empties the field; a blank cell leaves it as it is",
		OnUpdate:   "Never destructive: only " + ImportClearMarker + " clears a field, and a required field cannot be cleared. Nothing is deleted - records not in the file are untouched. Status is not in the file: new records are Active, an update keeps the record's status."}
	safety := ImportColumnSpec{Column: "(formulas)", Type: "Safety",
		Definition: "A cell starting with = + - or @ that is not a number is stored with a leading apostrophe, so a spreadsheet never runs it as a formula. Numbers (also negative ones) are kept as numbers.",
		Example:    "'=SUM(A1) is stored for =SUM(A1)"}
	locale := ImportColumnSpec{Column: "(locale)", Type: "Formats",
		Definition: "Whatever the computer's language settings, dates are YYYY-MM-DD and numbers use a dot for decimals with no thousands separators or currency sign.",
		Format:     "2026-10-28 · 1250.50"}
	out = append(out, file, order, update, safety, locale)
	return out, nil
}

// describeImportColumn is one field's dictionary row.
func describeImportColumn(doctype string, f FieldMeta, isMaster bool, country string) ImportColumnSpec {
	s := ImportColumnSpec{Column: f.Fieldname, Label: firstNonEmpty(f.Label, humanizeField(f.Fieldname)), Required: "No", Unique: "No",
		OnUpdate: "Replaced by this cell; a blank cell keeps the stored value; " + ImportClearMarker + " clears it"}
	if f.Mandatory {
		s.Required = "Yes"
	}
	switch f.Fieldtype {
	case "Number":
		s.Type, s.Format, s.Example = "Number", "Plain number, dot for decimals, no thousands separator or currency sign", "1250.50"
		s.Definition = s.Label + " (a number)."
	case "Date":
		s.Type, s.Format, s.Example = "Date", "YYYY-MM-DD", "2026-10-28"
		s.Definition = s.Label + " (a date)."
	case "Select":
		opts := splitOptions(f.Options)
		s.Type, s.Format = "Choice", "One of the allowed values, spelt exactly as listed"
		s.Allowed = strings.Join(opts, " | ")
		if len(opts) > 0 {
			s.Example = opts[0]
		}
		s.Definition = s.Label + " (one of a fixed list)."
	case "Link":
		s.Type = "Link"
		s.DependsOn = strings.TrimSpace(f.Options)
		s.Format = "The id of an existing " + s.DependsOn + " record (for a master, its code)"
		s.Example = "an existing " + s.DependsOn + " code"
		s.Definition = s.Label + " - which " + s.DependsOn + " this record refers to."
	case "Check":
		s.Type, s.Format, s.Allowed, s.Example = "Yes/No", "true or false (1/0 and yes/no are read too)", "true | false", "true"
		s.Definition = s.Label + " (yes or no)."
	case "JSONTable":
		s.Type, s.Format, s.Example = "Lines", "A JSON list of line objects", `[{"key":"value"}]`
		if cols := jsonTableColumnKeys(f.Options); cols != "" {
			s.Format += "; each line's keys: " + cols
		}
		s.Definition = s.Label + " (line items, as JSON)."
	case "JSONMap":
		s.Type, s.Format, s.Example = "Key/value", "A JSON object of key/value pairs", `{"key":"value"}`
		s.Definition = s.Label + " (key/value pairs, as JSON)."
	default:
		s.Type = "Text"
		s.Definition = s.Label + "."
		if spec, ok := DetectFieldFormat(f.Fieldname); ok {
			spec = formatForCountry(spec, country)
			s.Format, s.Example = spec.Hint, spec.Placeholder
		} else if isStateField(f) {
			s.Format, s.Example = "The state's full name as listed in the app; left blank, it is filled from the GSTIN", "Karnataka"
		}
	}
	if f.MaxLength != nil && *f.MaxLength > 0 {
		s.Format = strings.TrimPrefix(s.Format+"; at most "+strconv.Itoa(*f.MaxLength)+" characters", "; ")
	}
	switch {
	case f.AutoGenerated:
		// A numbered field (NumberSeries set) or one the server derives from
		// others (an Item's GST rate from its HSN). A mandatory one's column
		// must still be in the file (missingMandatoryColumns), left blank.
		s.Required = "No - the server fills it"
		if f.Mandatory {
			s.Required = "Yes, the column - leave the cell blank, the server fills it"
		}
		s.Example = "leave blank"
		s.OnUpdate = "Computed by the server from the record's other values"
		if f.NumberSeries != "" {
			s.Unique = "Yes - numbered by the server (" + f.NumberSeries + " series)"
			s.OnUpdate = "Kept as numbered; leave it as exported"
		}
	case f.Mirrored:
		s.Required = "No - copied from its partner field"
		s.OnUpdate = "Copied again from its partner field"
	case isMaster && f.Fieldname == "code":
		s.Unique = "Yes - the code is the record's id"
		if f.Mandatory && !IsTypedCodeMaster(doctype) {
			// Stage 76.5: the column must be there, a blank cell is numbered.
			s.Required = "Yes, the column - a blank cell is numbered for you"
		}
		s.OnUpdate = "Must match the id; changing a code is done on screen, not by import"
	}
	return s
}

func splitOptions(options string) []string {
	var out []string
	for _, o := range strings.Split(options, ",") {
		if o = strings.TrimSpace(o); o != "" {
			out = append(out, o)
		}
	}
	return out
}

// jsonTableColumnKeys lists a JSONTable field's column keys from its spec.
func jsonTableColumnKeys(options string) string {
	var cols []jsonTableColumn
	if err := json.Unmarshal([]byte(options), &cols); err != nil {
		return ""
	}
	keys := make([]string, 0, len(cols))
	for _, c := range cols {
		keys = append(keys, c.Key)
	}
	return strings.Join(keys, ", ")
}

// GenerateImportTemplateDictionaryCSV is the dictionary as a CSV, through the
// formula-safe writer like every CSV a person opens in a spreadsheet.
func GenerateImportTemplateDictionaryCSV(tenantID, doctype string) ([]byte, error) {
	specs, err := ImportTemplateDictionary(tenantID, doctype)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	w := newSafeCSVWriter(&buf)
	if err := w.Write(importDictionaryHeader); err != nil {
		return nil, err
	}
	for _, s := range specs {
		order := ""
		if s.Order > 0 {
			order = strconv.Itoa(s.Order)
		}
		if err := w.Write([]string{order, s.Column, s.Label, s.Type, s.Required, s.Definition, s.Format,
			s.Allowed, s.Example, s.DependsOn, s.Unique, s.OnUpdate}); err != nil {
			return nil, err
		}
	}
	w.Flush()
	return buf.Bytes(), w.Error()
}
