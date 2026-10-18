package engines

import (
	"custom_erp/db"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Stage 73 / 55.10 (user decision 2026-10-17): Payables Ageing ages what we
// owe for goods received and not yet paid - the GRN Suspense (2100)
// sub-ledger - instead of approved purchase orders, so it ties to the ledger.
// The purchase-order view is kept as its own report, "po-commitments".
//
// The sub-ledger is built from the 2100 postings themselves, not from the
// documents, which is what makes detail = summary = ledger hold by
// construction: every 2100 line belongs to exactly one open item.
//
//   - A goods receipt credits 2100 at its taxable cost (GRN:<id>:RECEIPT_COST).
//   - Everything that later clears or adjusts it is attributed back to that
//     receipt through the document's grn_id: the vendor bill's payments (full,
//     part, override, TDS) and its FX revaluation, debit and vendor credit
//     notes, and purchase-return GST reversals. A debit note raised by a
//     purchase return carries no grn_id of its own, so the return it came from
//     (source_doctype / source_doc_id) supplies it.
//   - A 2100 line with no receipt behind it - a fixed asset bought on credit,
//     a journal, a debit note typed in without a GRN - is an open item of its
//     own, aged from its posting date. It is still payable, and leaving it out
//     is exactly how a sub-ledger stops agreeing with its control account.
//
// An item ages from the earliest due date on an unpaid vendor bill for that
// receipt when one exists, otherwise from the receipt (or posting) date -
// the same due-date-first rule Receivables Ageing uses.

// PayableOpenItem is one line of the received-not-paid sub-ledger.
type PayableOpenItem struct {
	Key         string
	GRNID       string
	SourceType  string
	SourceID    string
	Vendor      string
	Bills       []string
	ReceivedOn  time.Time
	DueDate     string
	AgeFrom     string // "due date" or "receipt" / "posting"
	AgeDays     int
	Received    int64 // paise credited to 2100
	Cleared     int64 // paise debited to 2100
	Outstanding int64 // Received - Cleared
	Bucket      string
}

// pgTextArray renders ids as a Postgres text[] literal for "= ANY($1::text[])".
func pgTextArray(ids []string) string {
	quoted := make([]string, len(ids))
	for i, id := range ids {
		quoted[i] = `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(id) + `"`
	}
	return "{" + strings.Join(quoted, ",") + "}"
}

// payablesOpenItems builds the sub-ledger as at cut (zero cut = now). Items
// that have cleared to exactly zero are dropped; an over-cleared receipt stays
// in, negative, because the ledger carries it.
func payablesOpenItems(q queryRower, schema string, cut time.Time) ([]PayableOpenItem, error) {
	var cutArg interface{}
	if !cut.IsZero() {
		cutArg = cut
	}
	rows, err := q.Query(fmt.Sprintf(`
		SELECT g.document_type, g.document_id,
		       CASE WHEN g.document_type = 'GRN' THEN g.document_id
		            ELSE COALESCE(NULLIF(d.data->>'grn_id', ''), NULLIF(src.data->>'grn_id', ''), '') END AS grn_ref,
		       COALESCE(SUM(g.credit), 0)::bigint, COALESCE(SUM(g.debit), 0)::bigint,
		       MIN(g.created_at) AT TIME ZONE current_setting('TimeZone'),
		       COALESCE(MAX(d.data->>'vendor_id'), MAX(d.data->>'vendor'), '')
		FROM %[1]s.gl_postings g
		LEFT JOIN %[1]s.documents d ON d.id = g.document_id AND d.doctype = g.document_type
		LEFT JOIN %[1]s.documents src ON src.id = d.data->>'source_doc_id' AND src.doctype = d.data->>'source_doctype'
		WHERE g.account_code = '2100' AND ($1::timestamp IS NULL OR g.created_at <= $1::timestamp)
		GROUP BY 1, 2, 3`, schema), cutArg)
	if err != nil {
		return nil, err
	}
	items := map[string]*PayableOpenItem{}
	var order []string
	for rows.Next() {
		var docType, docID, grnRef, vendor string
		var credit, debit int64
		var firstPosted time.Time
		if err := rows.Scan(&docType, &docID, &grnRef, &credit, &debit, &firstPosted, &vendor); err != nil {
			rows.Close()
			return nil, err
		}
		key := docType + ":" + docID
		if grnRef != "" {
			key = "GRN:" + grnRef
		}
		it, ok := items[key]
		if !ok {
			it = &PayableOpenItem{Key: key, GRNID: grnRef, SourceType: docType, SourceID: docID, ReceivedOn: firstPosted}
			if grnRef != "" {
				it.SourceType, it.SourceID = "GRN", grnRef
			}
			items[key] = it
			order = append(order, key)
		}
		it.Received += credit
		it.Cleared += debit
		if firstPosted.Before(it.ReceivedOn) {
			it.ReceivedOn = firstPosted
		}
		if it.Vendor == "" {
			it.Vendor = vendor
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var grnIDs []string
	for _, key := range order {
		if it := items[key]; it.GRNID != "" {
			grnIDs = append(grnIDs, it.GRNID)
		}
	}
	if len(grnIDs) > 0 {
		// The receipt's own date and vendor, where the GRN document exists.
		grnRows, err := q.Query(fmt.Sprintf(`
			SELECT id, created_at AT TIME ZONE current_setting('TimeZone'), COALESCE(data->>'vendor', data->>'vendor_id', '')
			FROM %s.documents WHERE doctype = 'GRN' AND id = ANY($1::text[])`, schema), pgTextArray(grnIDs))
		if err != nil {
			return nil, err
		}
		for grnRows.Next() {
			var id, vendor string
			var createdAt time.Time
			if err := grnRows.Scan(&id, &createdAt, &vendor); err != nil {
				grnRows.Close()
				return nil, err
			}
			if it := items["GRN:"+id]; it != nil {
				it.ReceivedOn = createdAt
				if vendor != "" {
					it.Vendor = vendor
				}
			}
		}
		grnRows.Close()
		if err := grnRows.Err(); err != nil {
			return nil, err
		}

		// The vendor bills for each receipt, and the earliest due date on one
		// not yet paid.
		billRows, err := q.Query(fmt.Sprintf(`
			SELECT id, data->>'grn_id', COALESCE(NULLIF(data->>'invoice_number', ''), id), COALESCE(data->>'due_date', ''), status
			FROM %s.documents
			WHERE doctype = 'VendorInvoice' AND deleted_at IS NULL AND status <> 'Cancelled'
			  AND data->>'grn_id' = ANY($1::text[])
			ORDER BY id`, schema), pgTextArray(grnIDs))
		if err != nil {
			return nil, err
		}
		for billRows.Next() {
			var id, grnID, number, due, status string
			if err := billRows.Scan(&id, &grnID, &number, &due, &status); err != nil {
				billRows.Close()
				return nil, err
			}
			it := items["GRN:"+grnID]
			if it == nil {
				continue
			}
			it.Bills = append(it.Bills, number)
			if status != "Paid" && due != "" && (it.DueDate == "" || due < it.DueDate) {
				it.DueDate = due
			}
		}
		billRows.Close()
		if err := billRows.Err(); err != nil {
			return nil, err
		}
	}

	now := time.Now()
	out := make([]PayableOpenItem, 0, len(order))
	for _, key := range order {
		it := items[key]
		it.Outstanding = it.Received - it.Cleared
		if it.Outstanding == 0 {
			continue
		}
		ageFrom := it.ReceivedOn
		it.AgeFrom = "receipt"
		if it.GRNID == "" {
			it.AgeFrom = "posting"
		}
		if it.DueDate != "" {
			if parsed, perr := time.ParseInLocation("2006-01-02", it.DueDate, time.Local); perr == nil {
				ageFrom = parsed
				it.AgeFrom = "due date"
			}
		}
		it.AgeDays = int(now.Sub(ageFrom).Hours() / 24)
		if it.AgeDays < 0 {
			it.AgeDays = 0 // a bill not yet due sits in the freshest bucket
		}
		it.Bucket = ageBucketLabel(it.AgeDays)
		out = append(out, *it)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ReceivedOn.Before(out[j].ReceivedOn) })
	return out, nil
}

// GetPayablesAgeingReport buckets the received-not-paid sub-ledger by age.
// Amount is what is still owed (2100's credit balance for those items), so
// the four buckets sum to GL 2100.
func GetPayablesAgeingReport(tenantID string) ([]PayablesAgeingBucket, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	items, err := payablesOpenItems(db.DB, schema, time.Time{})
	if err != nil {
		return nil, err
	}
	return bucketPayableItems(items), nil
}

func bucketPayableItems(items []PayableOpenItem) []PayablesAgeingBucket {
	labels := []string{"0-30 days", "31-60 days", "61-90 days", "90+ days"}
	paise := map[string]int64{}
	count := map[string]int{}
	for _, it := range items {
		paise[it.Bucket] += it.Outstanding
		count[it.Bucket]++
	}
	out := make([]PayablesAgeingBucket, 0, len(labels))
	for _, label := range labels {
		out = append(out, PayablesAgeingBucket{Bucket: label, Count: count[label], Amount: PaiseToRupees(paise[label])})
	}
	return out
}

// payablesAgeingDrillDown lists exactly the open items one bucket was summed
// from.
func payablesAgeingDrillDown(tenantID, bucketLabel string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	items, err := payablesOpenItems(db.DB, schema, time.Time{})
	if err != nil {
		return nil, err
	}
	results := []map[string]interface{}{}
	for _, it := range items {
		if it.Bucket != bucketLabel {
			continue
		}
		results = append(results, map[string]interface{}{
			"id": it.SourceID, "source": it.SourceType, "vendor": it.Vendor,
			"bills": strings.Join(it.Bills, ", "), "received_on": it.ReceivedOn.Format("2006-01-02"),
			"due_date": it.DueDate, "aged_from": it.AgeFrom, "age_days": it.AgeDays,
			"received": PaiseToRupees(it.Received), "cleared": PaiseToRupees(it.Cleared),
			"outstanding_amount": PaiseToRupees(it.Outstanding),
		})
	}
	return results, nil
}
