package engines

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/lib/pq"
)

// The printer-name heal (263.4): a Printer saved before ask 263 keeps the
// operating system's name for the printer under qz_printer_name; the field is
// os_printer_name now. Run heal moves each one across - the name typed on the
// screen where one was given, otherwise the name it already has - and drops
// the old key. Until it runs, the server reads either key
// (agentPrinterNameSQL), so printing never stops; the Printer form waits,
// because it shows only the new field and a save there could lose the name.

const (
	healPrinterName     = "printer-name"
	printerNameMaxLen   = 255 // print_job_log.os_printer_name is VARCHAR(255)
	printerNameLegacyIn = `doctype = 'Printer' AND deleted_at IS NULL AND data ? 'qz_printer_name'`
)

func printerNameHeal() healDef {
	return healDef{
		ID: healPrinterName, Item: "263.4", Title: "Printer names", FieldLabel: "OS Printer Name",
		Doctypes: []string{"Printer"}, MaxLen: printerNameMaxLen,
		count:   countPrinterNameHeal,
		records: printerNameHealRecords,
		check:   checkPrinterNameTyped,
		apply:   applyPrinterNameHeal,
	}
}

func countPrinterNameHeal(q healQuerier, schema string) (int, error) {
	var n int
	err := q.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.documents WHERE `+printerNameLegacyIn, schema)).Scan(&n)
	return n, err
}

func checkPrinterNameTyped(r HealRecord, typed string) error {
	if n := utf8.RuneCountInString(typed); n > printerNameMaxLen {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "OS Printer Name",
			Message: fmt.Sprintf("OS Printer Name for %s has %d characters; keep it to %d.", r.ID, n, printerNameMaxLen)}
	}
	return nil
}

// legacyPrinterRecords is every live Printer still holding qz_printer_name,
// in id order, at most limit (0: all).
func legacyPrinterRecords(q healQuerier, schema string, limit int) ([]HealRecord, error) {
	query := fmt.Sprintf(`SELECT id, data::text FROM %s.documents WHERE `+printerNameLegacyIn+` ORDER BY id`, schema)
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}
	rows, err := q.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []HealRecord
	for rows.Next() {
		var id, raw string
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, err
		}
		var d map[string]interface{}
		if json.Unmarshal([]byte(raw), &d) != nil {
			d = map[string]interface{}{}
		}
		old := strings.TrimSpace(strField(d, "qz_printer_name"))
		out = append(out, HealRecord{Doctype: "Printer", ID: id, Label: itemAliasHealLabel(d, id),
			Current: old, Proposed: firstNonEmpty(strings.TrimSpace(strField(d, "os_printer_name")), old)})
	}
	return out, rows.Err()
}

func printerNameHealRecords(q healQuerier, schema string, limit int) ([]HealRecord, error) {
	return legacyPrinterRecords(q, schema, limit)
}

func applyPrinterNameHeal(q healQuerier, schema string, typed map[string]string) (fixed, typedUsed int, err error) {
	recs, err := legacyPrinterRecords(q, schema, 0)
	if err != nil || len(recs) == 0 {
		return 0, 0, err
	}
	ids, names := make([]string, 0, len(recs)), make([]string, 0, len(recs))
	for _, r := range recs {
		name := typed["Printer/"+r.ID]
		if name != "" {
			typedUsed++
		} else {
			name = r.Proposed
		}
		ids, names = append(ids, r.ID), append(names, name)
	}
	res, err := q.Exec(fmt.Sprintf(`UPDATE %s.documents d
		SET data = (d.data - 'qz_printer_name') || jsonb_build_object('os_printer_name', u.name),
		    updated_at = CURRENT_TIMESTAMP, version = d.version + 1
		FROM unnest($1::text[], $2::text[]) AS u(id, name)
		WHERE d.id = u.id AND d.doctype = 'Printer' AND d.deleted_at IS NULL AND d.data ? 'qz_printer_name'`, schema),
		pq.Array(ids), pq.Array(names))
	if err != nil {
		return 0, 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), typedUsed, nil
}
