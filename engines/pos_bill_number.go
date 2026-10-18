package engines

import (
	"database/sql"
	"errors"
	"fmt"
	"hash/fnv"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Stage 85.D1 (76.10): the POS bill number comes from the server, per store,
// when the sale is finalised - never the browser's POS-<store>-<epoch> cart
// number, which is 20-odd characters and not in any series.
//
// India caps an invoice number at 16 characters (CGST Rule 46(b); the IRP
// refuses longer), consecutive per series. So the number is drawn inside the
// sale's own transaction (a sale that rolls back gives its number back - no
// gap) and built compact: prefix + store tag + financial year + 6 digits,
// e.g. BJAY2627000123 (14). The store tag is the store's Short Code (Stage
// 60) when it has one of up to 4 characters, otherwise a stable 4-character
// tag made from its code. The prefix is Prefix Configs' POSBILL row (default
// "B"). The cart number stays the sale's internal key.

var billTagPattern = regexp.MustCompile(`^[A-Z0-9]{1,4}$`)

// compactYearPattern splits a "26-27" financial year into its two halves.
var compactYearPattern = regexp.MustCompile(`^(\d{2})-(\d{2})$`)

func posBillStoreTag(tx *sql.Tx, schema, store string) string {
	var short string
	_ = tx.QueryRow(fmt.Sprintf(`SELECT UPPER(TRIM(COALESCE(data->>'short_code', ''))) FROM %s.documents WHERE doctype = 'Location' AND id = $1`, schema), store).Scan(&short)
	if billTagPattern.MatchString(short) {
		return short
	}
	if s := strings.ToUpper(strings.TrimSpace(store)); billTagPattern.MatchString(s) {
		return s
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(store))
	tag := strings.ToUpper(strconv.FormatUint(uint64(h.Sum32()), 36))
	for len(tag) < 4 {
		tag = "0" + tag
	}
	return tag[len(tag)-4:]
}

// issuePOSBillNumberTx draws the store's next bill number in the caller's
// transaction (the counter row stays locked until the sale commits).
func issuePOSBillNumberTx(tx *sql.Tx, schema, store string, now time.Time) (string, error) {
	return issueCompactNumberTx(tx, schema, "POSBILL", "B", store, now)
}

// issueCompactNumberTx is the till's 16-character numbering, shared by the
// bill (POSBILL, "B"), the advance receipt voucher (CA) and the advance
// refund voucher (AVR): prefix + store tag + year + 6 digits (5 after a
// longer prefix), per store, gapless because it is drawn in the caller's
// transaction.
func issueCompactNumberTx(tx *sql.Tx, schema, docType, defaultPrefix, store string, now time.Time) (string, error) {
	prefix := defaultPrefix
	var p string
	if err := tx.QueryRow(fmt.Sprintf(`SELECT COALESCE(prefix, '') FROM %s.prefix_configs WHERE doc_type = $1`, schema), docType).Scan(&p); err == nil {
		prefix = strings.ToUpper(strings.TrimSpace(p))
	}
	if len(prefix) > 3 {
		prefix = prefix[:3]
	}
	fy := documentFinancialYear(now)
	yy := fy
	if m := compactYearPattern.FindStringSubmatch(fy); m != nil {
		yy = m[1] + m[2]
	}
	var current int64
	err := tx.QueryRow(fmt.Sprintf(`SELECT current_val FROM %s.sequence_counters WHERE doc_type = $3 AND store_code = $1 AND financial_year = $2 FOR UPDATE`, schema), store, fy, docType).Scan(&current)
	if errors.Is(err, sql.ErrNoRows) {
		if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.sequence_counters (doc_type, store_code, financial_year, current_val) VALUES ($3, $1, $2, 0)`, schema), store, fy, docType); err != nil {
			return "", err
		}
		current = 0
	} else if err != nil {
		return "", err
	}
	next := current + 1
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.sequence_counters SET current_val = $1 WHERE doc_type = $4 AND store_code = $2 AND financial_year = $3`, schema), next, store, fy, docType); err != nil {
		return "", err
	}
	digits := 6
	if len(prefix) > 1 {
		digits = 5
	}
	number := fmt.Sprintf("%s%s%s%0*d", prefix, posBillStoreTag(tx, schema, store), yy, digits, next)
	if len(number) > 16 {
		return "", fmt.Errorf("bill number %s is over 16 characters", number)
	}
	return number, nil
}
