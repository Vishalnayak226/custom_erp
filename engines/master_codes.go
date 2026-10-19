package engines

import (
	"custom_erp/db"
	"database/sql"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// Master codes (Stage 76.5).
//
// A master created with no series confirmed fell back to GenerateSequence's
// default shape - "Vendor/HQ/2026/000006", 21 characters for a supplier code.
// The user, 2026-10-17: "All code will be simple V00001 ... Ask me to set up
// the length for the first time ... in that vendor panel itself."
//
// So a master's code is a short prefix and a zero-padded number - no
// separator, no store, no year, never restarting. The first time a master
// type is created its New form asks for the prefix and length (the browser
// sends them as "_code_format" with that first record, so the save's own
// permission check covers setting them); the series is then confirmed and
// every later record just takes the next number. A create that arrives with
// no format and no confirmed series - a CSV import, an API caller - gets the
// suggested shape and confirms it, so no path can mint a long code again.
//
// Every master is numbered here, on the server, when it arrives without a
// code. The form used to fetch the number from the admin-only
// /api/v1/sequence endpoint, which refused a Store Manager creating a vendor.

// typedCodeMasters are masters whose code IS a natural identifier the user
// types - an ISO currency, a unit symbol, an HSN number. Never numbered.
var typedCodeMasters = map[string]bool{"Currency": true, "UOM": true, "HSNCode": true}

// IsTypedCodeMaster reports whether a master's code is typed, not numbered.
func IsTypedCodeMaster(doctype string) bool { return typedCodeMasters[doctype] }

// serverNumberedMasters are numbered whatever IsMasterDoctype says - they are
// created inline from the PO (Stage 62 / 65.3), sometimes before their
// doctype_meta row is in place in an older tenant.
var serverNumberedMasters = map[string]bool{
	"ProductFamily": true,
	"Category":      true,
	"ProductType":   true,
	"ItemName":      true,
}

// IsServerNumberedMaster reports whether a doctype is always numbered here.
func IsServerNumberedMaster(doctype string) bool { return serverNumberedMasters[doctype] }

// suggestedMasterPrefixes are the first suggestion where the doctype's own
// initials would read wrong to a user (a ProductFamily is called a Design).
var suggestedMasterPrefixes = map[string]string{
	"Vendor": "V", "Customer": "C", "Item": "I", "Employee": "E",
	"ProductFamily": "D", "Location": "S", "LegalEntity": "LE",
	"Category": "CAT", "ProductType": "PT", "ItemName": "IN", "Brand": "B", "ItemAttribute": "IA", "MRPRule": "MRP",
}

const (
	masterCodeDefaultLength = 5
	masterCodeMinLength     = 3
	masterCodeMaxLength     = 8
)

var masterPrefixPattern = regexp.MustCompile(`^[A-Z][A-Z0-9]{0,5}$`)

// MasterCodeSeries is what a master's New form needs to know.
type MasterCodeSeries struct {
	DocType    string `json:"doc_type"`
	Typed      bool   `json:"typed"`      // the user types the code
	Configured bool   `json:"configured"` // the format has been confirmed
	Prefix     string `json:"prefix"`     // confirmed, or the suggestion
	Length     int    `json:"length"`
	Next       string `json:"next"` // the number the next record gets
}

// masterCodeFormat is the "_code_format" a first record carries.
type masterCodeFormat struct {
	Prefix string
	Length int
}

// takeMasterCodeFormat reads and removes "_code_format" from a payload, so it
// is never stored as part of the record.
func takeMasterCodeFormat(payload map[string]interface{}) *masterCodeFormat {
	raw, ok := payload["_code_format"]
	delete(payload, "_code_format")
	m, isMap := raw.(map[string]interface{})
	if !ok || !isMap {
		return nil
	}
	f := &masterCodeFormat{Prefix: strings.ToUpper(strings.TrimSpace(fmt.Sprintf("%v", m["prefix"])))}
	switch v := m["length"].(type) {
	case float64:
		f.Length = int(v)
	case int:
		f.Length = v
	case string:
		fmt.Sscanf(v, "%d", &f.Length)
	}
	return f
}

// GetMasterCodeSeries reports a master's code format: confirmed, or the
// suggestion its New form should offer.
func GetMasterCodeSeries(tenantID, doctype string) (MasterCodeSeries, error) {
	out := MasterCodeSeries{DocType: doctype, Typed: typedCodeMasters[doctype]}
	if out.Typed {
		return out, nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return out, err
	}
	var prefix, sep, reset sql.NullString
	var padding sql.NullInt64
	var include sql.NullBool
	var confirmed bool
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT prefix, separator, padding_width, reset_frequency, include_store, confirmed_at IS NOT NULL
		FROM %s.prefix_configs WHERE doc_type = $1`, schema), doctype).Scan(&prefix, &sep, &padding, &reset, &include, &confirmed)
	if err != nil && err != sql.ErrNoRows {
		return out, err
	}
	if confirmed {
		out.Configured, out.Prefix, out.Length = true, prefix.String, int(padding.Int64)
	} else {
		taken, err := takenSeriesPrefixes(tenantID, doctype)
		if err != nil {
			return out, err
		}
		out.Prefix, out.Length = suggestMasterPrefix(doctype, taken), masterCodeDefaultLength
	}
	var current int64
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT current_val FROM %s.sequence_counters
		WHERE doc_type = $1 AND store_code = $2 AND financial_year = $2`, schema), doctype, counterWildcard).Scan(&current)
	out.Next = fmt.Sprintf("%s%0*d", out.Prefix, out.Length, current+1)
	return out, nil
}

// takenSeriesPrefixes is every prefix another series prints (upper-case).
func takenSeriesPrefixes(tenantID, except string) (map[string]bool, error) {
	all, err := ListNumberSeries(tenantID)
	if err != nil {
		return nil, err
	}
	taken := map[string]bool{}
	for _, s := range all {
		if s.DocType == except || counterSeries[s.DocType] {
			continue
		}
		for p := range seriesPrefixes(s.Prefix, s.StoreOverrides) {
			taken[p] = true
		}
	}
	return taken, nil
}

// suggestMasterPrefix offers the shortest free prefix: the curated letter,
// then the doctype's initials ("LegalEntity" -> LE), then its name's
// leading letters, then a digit.
func suggestMasterPrefix(doctype string, taken map[string]bool) string {
	var initials, letters strings.Builder
	for i, r := range doctype {
		if unicode.IsUpper(r) || i == 0 {
			initials.WriteRune(unicode.ToUpper(r))
		}
		if unicode.IsLetter(r) {
			letters.WriteRune(unicode.ToUpper(r))
		}
	}
	candidates := []string{}
	if p, ok := suggestedMasterPrefixes[doctype]; ok {
		candidates = append(candidates, p)
	}
	// Another master's curated prefix is kept for it, whichever is created
	// first - a Category made before any Customer must not take "C".
	reserved := map[string]bool{}
	for other, p := range suggestedMasterPrefixes {
		if other != doctype {
			reserved[p] = true
		}
	}
	ini := initials.String()
	for n := 1; n <= len(ini) && n <= 3; n++ {
		candidates = append(candidates, ini[:n])
	}
	l := letters.String()
	for n := 2; n <= len(l) && n <= 4; n++ {
		candidates = append(candidates, l[:n])
	}
	for _, c := range candidates {
		if masterPrefixPattern.MatchString(c) && !taken[c] && !reserved[c] {
			return c
		}
	}
	base := "M"
	if len(ini) > 0 {
		base = ini[:1]
	}
	for d := 1; d <= 9; d++ {
		if c := fmt.Sprintf("%s%d", base, d); !taken[c] {
			return c
		}
	}
	return base + "X"
}

// ensureMasterSeries confirms a master's code format the first time one is
// numbered: the format the New form sent, else the suggestion. A series that
// is already confirmed is left exactly as it is - changing it afterwards is
// an admin's job, on the Prefix Configurations screen.
func ensureMasterSeries(tenantID, doctype string, req *masterCodeFormat) error {
	current, err := GetMasterCodeSeries(tenantID, doctype)
	if err != nil || current.Configured {
		return err
	}
	f := masterCodeFormat{Prefix: current.Prefix, Length: current.Length}
	if req != nil {
		if req.Prefix != "" {
			f.Prefix = req.Prefix
		}
		if req.Length != 0 {
			f.Length = req.Length
		}
	}
	bad := func(msg string) error { return &ValidationError{Code: "ADMINC-0311", SubFor: doctype, Message: msg} }
	if !masterPrefixPattern.MatchString(f.Prefix) {
		return bad("the code prefix must be 1 to 6 letters or digits, starting with a letter - for example V")
	}
	if f.Length < masterCodeMinLength || f.Length > masterCodeMaxLength {
		return bad(fmt.Sprintf("the number length must be %d to %d digits", masterCodeMinLength, masterCodeMaxLength))
	}
	noStore := false
	if err := SaveNumberSeries(tenantID, NumberSeries{
		DocType: doctype, Prefix: f.Prefix, Separator: "", PaddingWidth: f.Length,
		ResetFrequency: "NEVER", ActiveStatus: true,
	}, &noStore, map[string]StoreSeriesOverride{}); err != nil {
		return err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	_, err = db.DB.Exec(fmt.Sprintf(`UPDATE %s.prefix_configs SET confirmed_at = NOW() WHERE doc_type = $1 AND confirmed_at IS NULL`, schema), doctype)
	return err
}

// PrepareMasterNumber numbers a new master that arrives with no code. A code
// the caller typed, or an explicit id (the API's upsert form), is kept; a
// typed-code master is never numbered. An Item under a design - a combination
// - takes the next Item number too (Stage 117; it used to be the design plus
// its values).
func PrepareMasterNumber(tenantID, location, doctype string, isCreate bool, payload map[string]interface{}) error {
	format := takeMasterCodeFormat(payload)
	if !isCreate || typedCodeMasters[doctype] ||
		strings.TrimSpace(strField(payload, "code")) != "" || strings.TrimSpace(strField(payload, "id")) != "" {
		return nil
	}
	if !serverNumberedMasters[doctype] {
		isMaster, err := IsMasterDoctype(tenantID, doctype)
		if err != nil || !isMaster {
			return err
		}
	}
	if err := ensureMasterSeries(tenantID, doctype, format); err != nil {
		return err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	// documents.id is unique across every record type, so a number someone
	// already used - typed by hand, imported, or printed by another series -
	// is skipped rather than overwriting that record on the upsert.
	for attempt := 0; attempt < 50; attempt++ {
		code, err := GenerateSequence(tenantID, doctype, "", "")
		if err != nil {
			if verr, ok := err.(*ValidationError); ok {
				return verr
			}
			return fmt.Errorf("could not number the new %s: %v", doctype, err)
		}
		var used bool
		if err := db.DB.QueryRow(fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM %s.documents WHERE id = $1)`, schema), code).Scan(&used); err != nil {
			return err
		}
		if !used {
			payload["code"] = code
			payload["id"] = code
			return nil
		}
	}
	return fmt.Errorf("could not find a free %s number: the next 50 are all in use", doctype)
}

// ReleaseMasterNumber gives back a number PrepareMasterNumber drew for a save
// that was then refused (a validation error, a conflict), so the next record
// takes it and codes stay V00001, V00002 ... without holes. Only the latest
// number can go back: if another record has drawn since, the gap stays -
// harmless for a master, which is never a tax document.
func ReleaseMasterNumber(tenantID, doctype, code string) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return
	}
	var prefix string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT prefix FROM %s.prefix_configs WHERE doc_type = $1`, schema), doctype).Scan(&prefix); err != nil {
		return
	}
	if !strings.HasPrefix(code, prefix) {
		return
	}
	n, err := strconv.ParseInt(code[len(prefix):], 10, 64)
	if err != nil || n <= 0 {
		return
	}
	_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.sequence_counters SET current_val = current_val - 1
		WHERE doc_type = $1 AND store_code = $2 AND financial_year = $2 AND current_val = $3`, schema), doctype, counterWildcard, n)
}
