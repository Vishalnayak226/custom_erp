package engines

import (
	"custom_erp/db"
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// Stage 108 (user, 2026-10-19, New Location form): "Short Code Suggested and
// Filled User Can Change. Std One ... Facility Code will be Sequence 1,2,3."
//
// The form fills both in (wireLocationForm, view-pickers.js, same rule); a
// new location that still arrives with either blank - cleared by hand, an
// API call - gets them here, before the single write. Edits are left alone:
// a Short Code is printed inside document numbers, so it never changes
// behind the user's back on an existing store.

// SuggestLocationShortCode is the one standard Short Code for a store name:
// a word written as an abbreviation (JP, MG - all capitals, up to 3 letters)
// is kept whole, every other word gives its first letter, the result is
// topped up to 3 from the last word and cut at 4. "JP Nagar" -> JPN,
// "Koramangala" -> KOR, "Main Warehouse" -> MWA.
func SuggestLocationShortCode(name string) string {
	words := strings.FieldsFunc(name, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	if len(words) == 0 {
		return ""
	}
	var code []rune
	used := 0 // letters of the last word already in the code
	for _, w := range words {
		r := []rune(w)
		if len(r) <= 3 && w == strings.ToUpper(w) {
			code, used = append(code, r...), len(r)
		} else {
			code, used = append(code, r[0]), 1
		}
	}
	last := []rune(words[len(words)-1])
	for i := used; len(code) < 3 && i < len(last); i++ {
		code = append(code, last[i])
	}
	if len(code) > 4 {
		code = code[:4]
	}
	return strings.ToUpper(string(code))
}

// PrepareNewLocationCodes fills a new location's blank Short Code (the
// standard suggestion, a digit added while another store has it as a short
// or store code) and blank Facility Code (the highest numeric one + 1).
func PrepareNewLocationCodes(tenantID string, payload map[string]interface{}) error {
	needShort := strings.TrimSpace(strField(payload, "short_code")) == ""
	needFacility := strings.TrimSpace(strField(payload, "facility_code")) == ""
	if !needShort && !needFacility {
		return nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	if needShort {
		if base := SuggestLocationShortCode(strField(payload, "name")); base != "" {
			taken := map[string]bool{}
			rows, err := db.DB.Query(fmt.Sprintf(`SELECT UPPER(TRIM(COALESCE(data->>'short_code', ''))), UPPER(COALESCE(data->>'code', id)), UPPER(id)
				FROM %s.documents WHERE doctype = 'Location'`, schema))
			if err != nil {
				return err
			}
			for rows.Next() {
				var s, c, i string
				if err := rows.Scan(&s, &c, &i); err != nil {
					rows.Close()
					return err
				}
				taken[s], taken[c], taken[i] = true, true, true
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				return err
			}
			code := base
			for n := 2; taken[code]; n++ {
				code = base + strconv.Itoa(n)
			}
			payload["short_code"] = code
		}
	}
	if needFacility {
		var next int
		if err := db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(MAX(TRIM(data->>'facility_code')::bigint), 0) + 1
			FROM %s.documents WHERE doctype = 'Location' AND TRIM(data->>'facility_code') ~ '^[0-9]{1,15}$'`, schema)).Scan(&next); err != nil {
			return err
		}
		payload["facility_code"] = strconv.Itoa(next)
	}
	return nil
}
