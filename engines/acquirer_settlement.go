package engines

import (
	"context"
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"
)

// Card / UPI clearing (140.12.12). Every till bill paid by card or UPI debits
// 1101 / 1102 and nothing emptied them unless someone matched each bank
// payout by hand. The acquirer's (or payment gateway's) daily settlement file
// is now imported as AcquirerSettlement rows through the standard CSV import -
// one row per payout: sales date, mode, gross, fee, net, settlement date, UTR
// - and MatchAcquirerSettlements pairs each row with the bank credit of the
// same net amount dated on or a few days after the settlement date, booking
// it through MatchPOSPayout (Dr bank + Dr 5230 fee / Cr clearing). What
// cannot be matched stays on screen as an exception, with the reason.

const AcquirerSettlementDoctype = "AcquirerSettlement"

// AcquirerMatchResult is one matching pass.
type AcquirerMatchResult struct {
	Matched    int                      `json:"matched"`
	Exceptions []map[string]interface{} `json:"exceptions"`
}

// MatchAcquirerSettlements matches every pending settlement row it can.
func MatchAcquirerSettlements(tenantID, actor string) (*AcquirerMatchResult, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, data FROM %s.documents WHERE doctype = $1 AND deleted_at IS NULL
		AND COALESCE(data->>'match_status', '') <> 'Matched' ORDER BY data->>'settlement_date', id`, schema), AcquirerSettlementDoctype)
	if err != nil {
		return nil, err
	}
	type pending struct {
		id   string
		data map[string]interface{}
	}
	var list []pending
	for rows.Next() {
		var p pending
		var raw []byte
		if rows.Scan(&p.id, &raw) == nil && json.Unmarshal(raw, &p.data) == nil {
			list = append(list, p)
		}
	}
	rows.Close()
	out := &AcquirerMatchResult{Exceptions: []map[string]interface{}{}}
	for _, p := range list {
		reason, lineID := matchOneAcquirerSettlement(tenantID, schema, p.data, actor)
		patch := map[string]interface{}{"match_status": "Matched", "bank_line": lineID, "exception": ""}
		if reason != "" {
			patch = map[string]interface{}{"match_status": "Exception", "exception": reason}
			ex := map[string]interface{}{"id": p.id, "reason": reason}
			for _, k := range []string{"settlement_date", "mode", "gross", "fee", "net", "utr"} {
				ex[k] = p.data[k]
			}
			out.Exceptions = append(out.Exceptions, ex)
		} else {
			out.Matched++
		}
		raw, _ := json.Marshal(patch)
		if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || $1::jsonb, status = $2, updated_at = CURRENT_TIMESTAMP WHERE id = $3`, schema),
			raw, patch["match_status"], p.id); err != nil {
			return out, err
		}
	}
	if out.Matched > 0 {
		LogAuditEvent(tenantID, actor, "FINANCE", "ACQUIRER_SETTLEMENTS_MATCHED", fmt.Sprintf("%d card / UPI payout(s) matched, %d exception(s)", out.Matched, len(out.Exceptions)))
	}
	return out, nil
}

// StartAcquirerSettlementMatcher retries every client's unmatched settlement
// rows each interval, so a payout whose bank credit arrives a day later (or
// whose statement is imported later) is matched without anyone asking.
func StartAcquirerSettlementMatcher(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if !IsSchedulerLeader() || db.DB == nil {
					continue
				}
				schemas, err := listTenantSchemas()
				if err != nil {
					continue
				}
				for _, schema := range schemas {
					var waiting bool
					if db.DB.QueryRow(fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM %s.documents WHERE doctype = $1 AND deleted_at IS NULL
						AND COALESCE(data->>'match_status', '') <> 'Matched')`, schema), AcquirerSettlementDoctype).Scan(&waiting) != nil || !waiting {
						continue
					}
					tenantID, err := tenantIDForSchema(schema)
					if err != nil {
						continue
					}
					if _, err := MatchAcquirerSettlements(tenantID, "system"); err != nil {
						log.Printf("[ACQUIRER] %s: %v", schema, err)
					}
				}
			}
		}
	}()
}

// matchOneAcquirerSettlement finds the bank credit for one row and books it.
// It returns why it could not, or the matched line.
func matchOneAcquirerSettlement(tenantID, schema string, d map[string]interface{}, actor string) (string, string) {
	clearing := ""
	switch strings.ToUpper(strings.TrimSpace(strField(d, "mode"))) {
	case "CARD":
		clearing = "1101"
	case "UPI":
		clearing = "1102"
	default:
		return "mode must be Card or UPI", ""
	}
	net, fee := numFromInterface(d["net"]), numFromInterface(d["fee"])
	if gross := numFromInterface(d["gross"]); net <= 0 && gross > 0 {
		net = round2(gross - fee)
	}
	if net <= 0 {
		return "no net amount", ""
	}
	day, err := time.Parse("2006-01-02", strings.TrimSpace(strField(d, "settlement_date")))
	if err != nil {
		return "settlement date must be YYYY-MM-DD", ""
	}
	var lineID string
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT id FROM %s.documents WHERE doctype = 'BankStatementLine' AND deleted_at IS NULL
		AND data->>'dr_cr' = 'Credit' AND COALESCE(data->>'match_status', 'Unmatched') NOT IN ('Matched', 'Explained')
		AND ROUND((data->>'amount')::numeric, 2) = ROUND($1::numeric, 2)
		AND (data->>'txn_date')::date BETWEEN $2::date - 1 AND $2::date + 3
		ORDER BY ABS((data->>'txn_date')::date - $2::date), id LIMIT 1`, schema), net, day.Format("2006-01-02")).Scan(&lineID)
	if lineID == "" {
		return fmt.Sprintf("no unmatched bank credit of %.2f dated %s to %s yet", net, day.AddDate(0, 0, -1).Format("2006-01-02"), day.AddDate(0, 0, 3).Format("2006-01-02")), ""
	}
	if err := MatchPOSPayout(tenantID, lineID, clearing, fee, actor); err != nil {
		return err.Error(), ""
	}
	return "", lineID
}

// RecordDeclaredTenders (140.12.12) stores what the cashier counted on the
// card terminal and the UPI app at close, beside what the till's bills say
// was taken, so a missed or double-keyed card / UPI payment shows at close
// and on the Z-report instead of surfacing weeks later in clearing.
func RecordDeclaredTenders(tenantID, sessionID string, card, upi *float64) error {
	if card == nil && upi == nil {
		return nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	system, err := TenderTotals(tenantID, sessionID, "", "", "")
	if err != nil {
		return err
	}
	patch := map[string]interface{}{}
	if card != nil {
		patch["declared_card"], patch["card_difference"] = round2(*card), round2(*card-system["Card"])
	}
	if upi != nil {
		patch["declared_upi"], patch["upi_difference"] = round2(*upi), round2(*upi-system["UPI"])
	}
	raw, _ := json.Marshal(patch)
	_, err = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || $1::jsonb, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'POSSession' AND id = $2`, schema), raw, sessionID)
	return err
}
