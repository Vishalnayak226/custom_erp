package engines

import (
	"custom_erp/db"
	"fmt"
	"time"
)

// Stage 148.29 - quote expiry. With crm.quote_expiry on, a CRM quote past its
// valid_until expires and cannot convert to an order; its owner gets a
// reminder task 3 days before, once per quote. Off = exactly today.

const quoteReminderDays = 3

// QuoteExpiryOn reports whether quotes expire.
func QuoteExpiryOn(tenantID string) bool { return GetSettingBool(tenantID, "crm.quote_expiry") }

// crmQuoteExpiredErr refuses a convert on an expired quote. It reads the
// quote's own date, so it holds even before the daily sweep has run.
func crmQuoteExpiredErr(tenantID, schema, quotationID string) error {
	if quotationID == "" || !QuoteExpiryOn(tenantID) {
		return nil
	}
	var status, validUntil string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT status, COALESCE(data->>'valid_until', '') FROM %s.documents
		WHERE id = $1 AND doctype = 'SalesQuotation'`, schema), quotationID).Scan(&status, &validUntil); err != nil {
		return nil
	}
	if status == "Converted" {
		return nil
	}
	if status == "Expired" || (validUntil != "" && validUntil < BusinessToday()) {
		return crmErr(CRMCodeQuoteExpired, "the quotation expired on %s - revise it with a new valid-until date", validUntil)
	}
	return nil
}

// RunQuoteExpirySweep (daily) expires the open quotes past their date and
// reminds the owner of those that run out within 3 days. It returns how many
// of each. With the setting off it does nothing.
func RunQuoteExpirySweep(tenantID, schema string) (expired, reminded int, err error) {
	if !QuoteExpiryOn(tenantID) || !crmReady(schema) {
		return 0, 0, nil
	}
	today := BusinessToday()
	res, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET status = 'Expired', data = data || '{"status":"Expired"}'::jsonb, updated_at = CURRENT_TIMESTAMP
		WHERE doctype = 'SalesQuotation' AND deleted_at IS NULL AND status IN ('Draft', 'Approved')
		  AND COALESCE(data->>'valid_until', '') <> '' AND data->>'valid_until' < $1`, schema), today)
	if err != nil {
		return 0, 0, err
	}
	n, _ := res.RowsAffected()
	expired = int(n)
	limit := time.Now().In(businessLocation()).AddDate(0, 0, quoteReminderDays).Format("2006-01-02")
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT q.id, COALESCE(q.data->>'opportunity_id', ''), COALESCE(q.data->>'valid_until', ''), COALESCE(q.created_by, '')
		FROM %s.documents q WHERE q.doctype = 'SalesQuotation' AND q.deleted_at IS NULL AND q.status IN ('Draft', 'Pending Approval', 'Approved')
		  AND COALESCE(q.data->>'valid_until', '') <> '' AND q.data->>'valid_until' >= $1 AND q.data->>'valid_until' <= $2`, schema), today, limit)
	if err != nil {
		return expired, 0, err
	}
	type due struct{ id, opp, until, by string }
	var list []due
	for rows.Next() {
		var d due
		if rows.Scan(&d.id, &d.opp, &d.until, &d.by) == nil {
			list = append(list, d)
		}
	}
	rows.Close()
	for _, d := range list {
		var owner, customer, status string
		if d.opp != "" {
			_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(owner, ''), COALESCE(customer_id, ''), status FROM %s.crm_opportunities WHERE id = $1`, schema), d.opp).Scan(&owner, &customer, &status)
			if status != "" && status != "Open" {
				continue
			}
		}
		r, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %[1]s.crm_tasks (id, kind, title, customer_id, opportunity_id, owner, due_at, ref_doctype, ref_id, created_by)
			SELECT $1, 'follow_up', $2, NULLIF($3, ''), NULLIF($4, ''), NULLIF($5, ''), $6::date::timestamp, 'SalesQuotation', $7::text, 'quote_expiry'
			 WHERE NOT EXISTS (SELECT 1 FROM %[1]s.crm_tasks WHERE ref_doctype = 'SalesQuotation' AND ref_id = $7::text)`, schema),
			NewDocID("TSK"), truncateRunes("Quote runs out "+d.until, 300), customer, d.opp, firstNonEmpty(owner, d.by), d.until, d.id)
		if err != nil {
			return expired, reminded, err
		}
		if k, _ := r.RowsAffected(); k > 0 {
			reminded++
		}
	}
	return expired, reminded, nil
}
