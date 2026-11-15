package engines

import (
	"custom_erp/db"
	"database/sql"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Stage 76.10 (decided as recommended, ask 179): an offline till numbers its
// own bills from a series of its own.
//
// The store's bill number (pos_bill_number.go) is drawn on the server when
// the sale posts, so a sale rung up offline had no number until a manager
// validated it (49.5.6a) and its receipt went out without one. While it is
// online, a till now registers a series: the next free till number (01-99)
// at its store, and a base of prefix + till + store tag, e.g. T01JAY. The
// till comes before the store tag so the GSTR-1 documents-issued table, which
// groups by the number without its running digits, keeps each till's series
// on its own row. Offline, the till numbers each sale base + financial year
// + running number, padded to 16 characters (CGST Rule 46(b)):
// T01JAY2627000001. When the sale posts, checkout takes that number as the
// bill number once offlineTillBillNumberTx has checked it is one of this
// store's till series and on no other bill. A till that has no series yet
// queues as before, and its sale gets the store's next number when it posts.
//
// The prefix is Prefix Configs' POSTILL row (default "T"), one character, so
// a till always has at least five running digits.

// POSTillSeries is one till's registered series.
type POSTillSeries struct {
	Store  string `json:"store"`
	Till   string `json:"till"`
	Base   string `json:"base"`
	Digits int    `json:"digits"` // running digits after base + 4-digit year
	Label  string `json:"label,omitempty"`
}

const posTillNumberLen = 16

var tillNumberPattern = regexp.MustCompile(`^[A-Z0-9]{1,16}$`)

func posTillError(format string, args ...interface{}) error {
	return &ValidationError{Code: "POSOFF-1371", Message: fmt.Sprintf(format, args...)}
}

// RegisterPOSTill gives a till at the store its own bill series.
func RegisterPOSTill(tenantID, store, actor, label string) (*POSTillSeries, error) {
	store = strings.TrimSpace(store)
	label = strings.TrimSpace(label)
	if len(label) > 60 {
		label = label[:60]
	}
	if store == "" {
		return nil, posTillError("Choose the store this till sells for.")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var found int
	if err := tx.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.documents WHERE doctype = 'Location' AND id = $1 AND deleted_at IS NULL`, schema), store).Scan(&found); err != nil {
		return nil, err
	}
	if found == 0 {
		return nil, posTillError("%s is not a store.", store)
	}
	prefix := "T"
	var p string
	if err := tx.QueryRow(fmt.Sprintf(`SELECT UPPER(TRIM(COALESCE(prefix, ''))) FROM %s.prefix_configs WHERE doc_type = 'POSTILL'`, schema)).Scan(&p); err == nil && p != "" {
		prefix = p[:1]
	}
	tag := posBillStoreTag(tx, schema, store)
	// One statement per candidate: a till number taken by another till of
	// this store, or a base another store already uses, is skipped.
	for n := 1; n <= 99; n++ {
		till := fmt.Sprintf("%02d", n)
		base := prefix + till + tag
		var got string
		err := tx.QueryRow(fmt.Sprintf(`INSERT INTO %s.pos_till_series (store, till, base, label, created_by) VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT DO NOTHING RETURNING till`, schema), store, till, base, label, actor).Scan(&got)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		LogAuditEvent(tenantID, actor, "POS_TILL_SERIES_REGISTERED", "SUCCESS", fmt.Sprintf("Till %s at %s numbers offline bills %s...", till, store, base))
		return &POSTillSeries{Store: store, Till: till, Base: base, Digits: posTillNumberLen - len(base) - 4, Label: label}, nil
	}
	return nil, posTillError("Every till number at %s (01-99) already has a series.", store)
}

// offlineTillBillNumberTx checks the number an offline till gave a sale and
// returns it, upper-cased, for the checkout to post the bill under. It holds
// a lock on the number until the sale commits, so the same number synced
// twice at once posts once.
func offlineTillBillNumberTx(tx *sql.Tx, schema, store, number string) (string, error) {
	number = strings.ToUpper(strings.TrimSpace(number))
	if len(number) != posTillNumberLen || !tillNumberPattern.MatchString(number) {
		return "", posTillError("Offline bill number %q is not a till series number.", number)
	}
	var base string
	err := tx.QueryRow(fmt.Sprintf(`SELECT base FROM %s.pos_till_series WHERE store = $1 AND $2 LIKE base || '%%'
		ORDER BY length(base) DESC LIMIT 1`, schema), store, number).Scan(&base)
	if err == sql.ErrNoRows {
		return "", posTillError("Offline bill number %s is not from a till series of %s.", number, store)
	}
	if err != nil {
		return "", err
	}
	rest := number[len(base):]
	year, run := rest[:4], rest[4:]
	from, e1 := strconv.Atoi(year[:2])
	to, e2 := strconv.Atoi(year[2:])
	seq, e3 := strconv.ParseInt(run, 10, 64)
	if e1 != nil || e2 != nil || e3 != nil || to != (from+1)%100 || seq <= 0 {
		return "", posTillError("Offline bill number %s does not end in a financial year and a running number.", number)
	}
	if _, err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext($1))`, schema+"/till-bill/"+number); err != nil {
		return "", err
	}
	var taken int
	if err := tx.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.documents WHERE doctype = 'POSCart' AND data->>'bill_number' = $1`, schema), number).Scan(&taken); err != nil {
		return "", err
	}
	if taken > 0 {
		return "", posTillError("Offline bill number %s is already on another bill.", number)
	}
	return number, nil
}
