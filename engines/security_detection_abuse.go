package engines

import (
	"database/sql"
	"fmt"
	"time"

	"custom_erp/db"
	"github.com/lib/pq"
)

// Stage 49.5.4: business abuse and fraud patterns, as rules of the same
// detection engine (security_detection.go) - same severity, owner, threshold,
// window, dedup and first response, the same SECURITY_ALERT row, the same
// drill. The audit-row rules (stock adjustments, period reopening, approval
// rule removed, configuration bursts) are plain entries in the rule table;
// the ones here read what the ERP already records about the business event
// itself: the vendor master edit and the payment, the vendor bills and
// receipts, the approval tiers, the return requests, the till carts and the
// negative-stock records. No new table and no new write path.
//
// documents.created_at is session-local (column default), unlike the signed
// audit rows, so document windows compare the way detectAuditVerifyFailure
// does; audit rows compare with utcStamp.

// docSince is the SQL for "created after $n" on a session-local column.
func docSince(n int) string {
	return fmt.Sprintf("($%d::timestamptz AT TIME ZONE current_setting('TimeZone'))", n)
}

// scanHits reads (subject, count, correlations) rows into detection hits.
func scanHits(rows *sql.Rows, withCorrelations bool) ([]detectionHit, error) {
	defer rows.Close()
	var out []detectionHit
	for rows.Next() {
		var h detectionHit
		var err error
		if withCorrelations {
			err = rows.Scan(&h.Subject, &h.Count, pq.Array(&h.Correlations))
		} else {
			err = rows.Scan(&h.Subject, &h.Count)
		}
		if err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// detectVendorBankThenPaid: a vendor's bank account or IFSC changed (the
// signed EDIT_Vendor row the record save writes) and a payment to that same
// vendor followed inside the window - the payment-diversion pattern. The
// payment rows name the bill or receipt; the vendor is read from it.
func detectVendorBankThenPaid(schema string, now time.Time, rule SecurityDetectionRule) ([]detectionHit, error) {
	since := now.Add(-time.Duration(rule.WindowMinutes) * time.Minute)
	rows, err := db.DB.Query(fmt.Sprintf(`
		WITH changes AS (
			SELECT substring(details from '^Edited Vendor ([^:]+):') AS vendor_id, MIN(created_at) AS changed_at
			FROM %[1]s.audit_logs
			WHERE action = 'EDIT_Vendor' AND signature IS NOT NULL AND created_at > $1::timestamp
			  AND details ~ '\m(bank_account_number|bank_ifsc)\M'
			GROUP BY 1
		), pays AS (
			SELECT 'VendorInvoice'::text AS doctype, substring(details from 'vendor invoice ([^\s:;,()]+)') AS doc_id, created_at, correlation_id
			FROM %[1]s.audit_logs
			WHERE action = 'PAY_VENDOR_INVOICE' AND signature IS NOT NULL AND created_at > $1::timestamp
			UNION ALL
			SELECT 'GRN', substring(details from '^GRN ([^\s:]+):'), created_at, correlation_id
			FROM %[1]s.audit_logs
			WHERE action = 'GRN_VENDOR_PAYMENT' AND signature IS NOT NULL AND created_at > $1::timestamp
		)
		SELECT c.vendor_id, COUNT(*),
		       COALESCE((array_agg(p.correlation_id) FILTER (WHERE p.correlation_id IS NOT NULL))[1:3], '{}')
		FROM changes c
		JOIN pays p ON p.created_at > c.changed_at
		JOIN %[1]s.documents d ON d.doctype = p.doctype AND d.id = p.doc_id
		 AND COALESCE(NULLIF(d.data->>'vendor_id', ''), d.data->>'vendor') = c.vendor_id
		WHERE c.vendor_id IS NOT NULL
		GROUP BY c.vendor_id`, schema), utcStamp(since))
	if err != nil {
		return nil, err
	}
	return scanHits(rows, true)
}

// duplicateInvoiceLookbackDays bounds how far back a bill is compared.
const duplicateInvoiceLookbackDays = 400

// detectDuplicateVendorInvoice: one vendor, two live bills whose invoice
// numbers match once case, spaces and punctuation are dropped ("INV-001" and
// "inv 001"), at least one entered inside the window. Entry already refuses
// an exact repeat (checkDuplicateVendorInvoice); this catches the variant
// that slips past it. GRNs and stand-alone vendor bills are compared the way
// that check compares them - a bill raised from a GRN is the GRN's own.
func detectDuplicateVendorInvoice(schema string, now time.Time, rule SecurityDetectionRule) ([]detectionHit, error) {
	since := now.Add(-time.Duration(rule.WindowMinutes) * time.Minute)
	lookback := now.AddDate(0, 0, -duplicateInvoiceLookbackDays)
	rows, err := db.DB.Query(fmt.Sprintf(`
		WITH bills AS (
			SELECT data->>'vendor' AS vendor, data->>'vendor_invoice_no' AS num, created_at
			FROM %[1]s.documents
			WHERE doctype = 'GRN' AND deleted_at IS NULL AND status <> 'Cancelled' AND created_at > %[2]s
			UNION ALL
			SELECT data->>'vendor_id', data->>'invoice_number', created_at
			FROM %[1]s.documents
			WHERE doctype = 'VendorInvoice' AND deleted_at IS NULL AND status <> 'Cancelled' AND created_at > %[2]s
			  AND COALESCE(data->>'grn_id', '') = ''
		), keyed AS (
			SELECT vendor, regexp_replace(lower(COALESCE(num, '')), '[^a-z0-9]', '', 'g') AS num, created_at
			FROM bills WHERE COALESCE(vendor, '') <> ''
		)
		SELECT vendor || ' invoice ' || num, COUNT(*)
		FROM keyed WHERE num <> ''
		GROUP BY vendor, num
		HAVING COUNT(*) > 1 AND bool_or(created_at > %[3]s)`, schema, docSince(1), docSince(2)), lookback, since)
	if err != nil {
		return nil, err
	}
	return scanHits(rows, false)
}

// detectThresholdSplit: one maker, one counterparty, one document type, two
// or more documents in the window each between half of an approval tier's
// lower bound and that bound - so each passes the cheaper approval - whose
// total reaches the tier. The amount is the one the approval engine reads
// (extractAmount's key order); a tier measured in percent or points is not
// money and is left out.
func detectThresholdSplit(schema string, now time.Time, rule SecurityDetectionRule) ([]detectionHit, error) {
	since := now.Add(-time.Duration(rule.WindowMinutes) * time.Minute)
	rows, err := db.DB.Query(fmt.Sprintf(`
		WITH tiers AS (
			SELECT DISTINCT doctype, min_amount FROM %[1]s.approval_rules WHERE min_amount > 0
		), docs AS (
			SELECT d.doctype, d.created_by,
			       COALESCE(NULLIF(d.data->>'vendor_id', ''), NULLIF(d.data->>'vendor', ''),
			                NULLIF(d.data->>'customer_id', ''), NULLIF(d.data->>'customer', ''), '') AS party,
			       d.data, k.amount_key
			FROM %[1]s.documents d
			CROSS JOIN LATERAL (SELECT CASE
				WHEN jsonb_typeof(d.data->'total_amount') = 'number' THEN 'total_amount'
				WHEN jsonb_typeof(d.data->'amount') = 'number' THEN 'amount'
				WHEN jsonb_typeof(d.data->'discount_amount') = 'number' THEN 'discount_amount'
				WHEN jsonb_typeof(d.data->'variance_qty') = 'number' THEN 'variance_qty'
				WHEN jsonb_typeof(d.data->'invoice_amount') = 'number' THEN 'invoice_amount'
				WHEN jsonb_typeof(d.data->'points_value') = 'number' THEN 'points_value' END AS amount_key) k
			WHERE d.doctype IN (SELECT doctype FROM tiers) AND d.deleted_at IS NULL AND d.status <> 'Cancelled'
			  AND d.created_at > %[2]s
		), money AS (
			SELECT doctype, created_by, party, (data->>amount_key)::numeric AS amount
			FROM docs WHERE amount_key IN ('total_amount', 'amount', 'invoice_amount')
		)
		SELECT m.created_by || ' ' || m.doctype || CASE WHEN m.party <> '' THEN ' ' || m.party ELSE '' END
		       || ' under ' || t.min_amount::text, COUNT(*)
		FROM money m JOIN tiers t ON t.doctype = m.doctype
		WHERE m.amount >= t.min_amount / 2 AND m.amount < t.min_amount
		GROUP BY m.created_by, m.doctype, m.party, t.min_amount
		HAVING COUNT(*) >= 2 AND SUM(m.amount) >= t.min_amount`, schema, docSince(1)), since)
	if err != nil {
		return nil, err
	}
	return scanHits(rows, false)
}

// countRecentDocs counts one doctype's live documents created inside the
// rule's window, grouped by subjectSQL (a fixed column expression, never
// input). extraWhere, when set, is ANDed in; its arguments start at $2.
func countRecentDocs(schema string, now time.Time, rule SecurityDetectionRule, doctype, subjectSQL, extraWhere string, args ...interface{}) ([]detectionHit, error) {
	since := now.Add(-time.Duration(rule.WindowMinutes) * time.Minute)
	where := ""
	if extraWhere != "" {
		where = " AND " + extraWhere
	}
	params := append([]interface{}{since}, args...)
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT COALESCE(NULLIF(%[2]s, ''), '(unknown)'), COUNT(*)
		FROM %[1]s.documents
		WHERE doctype = '%[3]s' AND deleted_at IS NULL AND created_at > %[4]s%[5]s
		GROUP BY 1`, schema, subjectSQL, doctype, docSince(1), where), params...)
	if err != nil {
		return nil, err
	}
	return scanHits(rows, false)
}

// detectRefundBurst: many return requests raised by one user - the refund
// half of till fraud (cancels, voids and price overrides are till_reversal_burst).
func detectRefundBurst(schema string, now time.Time, rule SecurityDetectionRule) ([]detectionHit, error) {
	return countRecentDocs(schema, now, rule, "ReturnRequest", "data->>'requested_by'", "")
}

// detectManualDiscountBurst: many bills by one cashier carrying a manual
// discount above the line the client already set for "needs a reason"
// (pos.manual_discount.reason_above_pct) - the discount the server measured,
// stored on the cart, not the one the till claimed.
func detectManualDiscountBurst(schema string, now time.Time, rule SecurityDetectionRule) ([]detectionHit, error) {
	line := GetSettingFloatForSchema(schema, "pos.manual_discount.reason_above_pct")
	return countRecentDocs(schema, now, rule, "POSCart", "created_by",
		"jsonb_typeof(data->'manual_discount_pct') = 'number' AND (data->>'manual_discount_pct')::numeric > $2", line)
}

// detectOfflineSaleBurst: many bills one cashier sent as rung up offline.
// offline_synced is the till's own claim (20.13); since 49.5.6a each one is
// held for validation (pos_offline_sales) and posts only when a manager
// validates it, so every claim is counted here - validated, rejected or still
// waiting - by when it arrived (received_at is TIMESTAMPTZ, no clock shift).
func detectOfflineSaleBurst(schema string, now time.Time, rule SecurityDetectionRule) ([]detectionHit, error) {
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT COALESCE(NULLIF(cashier_user_id, ''), '(unknown)'), COUNT(*)
		FROM %s.pos_offline_sales WHERE received_at > $1
		GROUP BY 1`, schema), now.Add(-time.Duration(rule.WindowMinutes)*time.Minute))
	if err != nil {
		return nil, err
	}
	return scanHits(rows, false)
}

// detectNegativeStockBurst: many sales below zero stock at one store - each
// is already a POSOfflineSyncVariance record (20.13); a run of them is stock
// leaving that the books never had.
func detectNegativeStockBurst(schema string, now time.Time, rule SecurityDetectionRule) ([]detectionHit, error) {
	return countRecentDocs(schema, now, rule, "POSOfflineSyncVariance", "data->>'location'", "")
}
