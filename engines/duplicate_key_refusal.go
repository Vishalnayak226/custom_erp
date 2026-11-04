package engines

import (
	"custom_erp/db"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/lib/pq"
)

// Stage 140.14.1: a Postgres unique violation (23505) from a record save is
// a refusal the user can act on, never a 500. The fields are read from the
// violation's own detail ("Key (upper((data ->> 'item'::text)), ...)=(...)
// already exists."), so every unique index, present or future, is covered
// without a hand list.

var (
	dupKeyFieldRe = regexp.MustCompile(`->>\s*'([A-Za-z0-9_]+)'`)
	dupKeyColRe   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

// duplicateKeyFields returns the field names a unique violation's detail
// names, in key order.
func duplicateKeyFields(detail string) []string {
	start := strings.Index(detail, "Key (")
	end := strings.Index(detail, ")=(")
	if start < 0 || end <= start+5 {
		return nil
	}
	key := detail[start+5 : end]
	var parts []string
	depth, from := 0, 0
	for i, c := range key {
		switch c {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, key[from:i])
				from = i + 1
			}
		}
	}
	parts = append(parts, key[from:])
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if m := dupKeyFieldRe.FindStringSubmatch(p); m != nil {
			out = append(out, m[1])
		} else if dupKeyColRe.MatchString(p) {
			out = append(out, p)
		}
	}
	return out
}

// DuplicateKeyRefusal turns a unique violation into a 409 MASTER-1321
// refusal that names the fields in words. It returns nil when err is not one.
// doctype may be "" when the caller does not know it.
func DuplicateKeyRefusal(tenantID, doctype string, err error) *ValidationError {
	var pe *pq.Error
	if err == nil || !errors.As(err, &pe) || pe.Code != "23505" {
		return nil
	}
	labels := map[string]string{}
	if doctype != "" {
		if schema, serr := db.GetTenantSchema(tenantID); serr == nil {
			if rows, qerr := db.DB.Query(fmt.Sprintf(`SELECT fieldname, label FROM %s.doctype_fields WHERE doctype_name = $1`, schema), doctype); qerr == nil {
				for rows.Next() {
					var f, l string
					if rows.Scan(&f, &l) == nil && l != "" {
						labels[f] = l
					}
				}
				rows.Close()
			}
		}
	}
	var names []string
	for _, f := range duplicateKeyFields(pe.Detail) {
		if f == "id" {
			continue
		}
		if l, ok := labels[f]; ok {
			names = append(names, l)
		} else {
			names = append(names, humanizeField(f))
		}
	}
	what := "A record"
	if doctype != "" {
		what = "A " + humanizeDoctype(doctype)
	}
	msg := what + " with these values already exists."
	if len(names) > 0 {
		msg = what + " with this " + joinWords(names) + " already exists."
	}
	return &ValidationError{Code: "MASTER-1321", Message: msg}
}

func joinWords(w []string) string {
	switch len(w) {
	case 0:
		return ""
	case 1:
		return w[0]
	}
	return strings.Join(w[:len(w)-1], ", ") + " and " + w[len(w)-1]
}
