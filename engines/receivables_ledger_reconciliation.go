package engines

import (
	"custom_erp/db"
	"fmt"
	"math"
	"strings"
)

// Stage 49.5.5 - trusted totals: the receivables sub-ledger against the GL,
// invoice by invoice.
//
// The Receivables Ageing's control totals (control_totals.go) already say
// whether the open invoices add up to 1300 Accounts Receivable. When they do
// not, they cannot say where. This report does: for every posted customer
// invoice it sets what the invoice says is still owed (its book) beside what
// the ledger holds for it on 1300 - its own posting and receipts, and the
// credit notes raised against it - and names every invoice that differs and
// every 1300 posting no invoice accounts for (a marketplace sale, a journal,
// an invoice record that is gone). A difference is shown with what usually
// causes it; nothing is corrected here - the fix is a credit note, a receipt
// or a reversal, each of which leaves its own entry.

// ReceivableLedgerRow is one invoice, or one kind of 1300 posting no invoice
// accounts for.
type ReceivableLedgerRow struct {
	Source     string  `json:"source"`
	Document   string  `json:"document"`
	Customer   string  `json:"customer"`
	Status     string  `json:"status"`
	Book       float64 `json:"book_outstanding"`
	Ledger     float64 `json:"ledger_balance"`
	Difference float64 `json:"difference"`
	Postings   int     `json:"ledger_postings"`
	Owner      string  `json:"owner"`
	Hint       string  `json:"hint"`
}

// receivablesLedgerLimit bounds the report (Stage 47.10's bounded-queries
// rule): a reconciliation is the report that quietly becomes a scan of years.
const receivablesLedgerLimit = 5000

// receivablesOwner is the role a receivables difference belongs to.
const receivablesOwner = "Accountant"

// ReceivablesVsLedger compares every posted sales invoice's outstanding
// amount with its 1300 balance, and lists the 1300 postings no invoice
// accounts for. With mismatchesOnly, rows that agree are left out.
func ReceivablesVsLedger(tenantID string, mismatchesOnly bool) ([]ReceivableLedgerRow, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	out := []ReceivableLedgerRow{}

	// Each invoice: its book (what it says is owed) and its 1300 balance -
	// its own postings (the sale, receipts) plus its credit notes'.
	rows, err := db.DB.Query(fmt.Sprintf(`
		WITH links AS (
			SELECT g.document_id AS invoice_id, g.debit, g.credit FROM %[1]s.gl_postings g
			 WHERE g.account_code = '1300' AND g.document_type = 'SalesInvoice'
			UNION ALL
			SELECT cn.data->>'sales_invoice_id', g.debit, g.credit FROM %[1]s.gl_postings g
			  JOIN %[1]s.documents cn ON cn.doctype = 'CreditNote' AND cn.id = g.document_id
			 WHERE g.account_code = '1300' AND g.document_type = 'CreditNote' AND COALESCE(cn.data->>'sales_invoice_id', '') <> ''
		), gl AS (
			SELECT invoice_id, SUM(debit) - SUM(credit) AS net, COUNT(*) AS n FROM links GROUP BY invoice_id
		)
		SELECT COALESCE(NULLIF(i.data->>'invoice_number', ''), i.id), COALESCE(i.data->>'customer', ''),
		       COALESCE(i.status, ''), i.deleted_at IS NOT NULL, COALESCE(NULLIF(UPPER(i.data->>'currency'), ''), 'INR'),
		       (CASE WHEN i.deleted_at IS NOT NULL THEN 0
		             WHEN i.status = 'Approved' THEN COALESCE(NULLIF(i.data->>'outstanding_amount', '')::numeric, NULLIF(i.data->>'total_amount', '')::numeric, 0)
		             WHEN i.status = 'Paid' THEN COALESCE(NULLIF(i.data->>'outstanding_amount', '')::numeric, 0)
		             ELSE 0 END)::float8,
		       COALESCE(gl.net, 0), COALESCE(gl.n, 0)
		  FROM %[1]s.documents i
		  LEFT JOIN gl ON gl.invoice_id = i.id
		 WHERE i.doctype = 'SalesInvoice'
		   AND ((i.deleted_at IS NULL AND i.status IN ('Approved', 'Paid')) OR COALESCE(gl.n, 0) > 0)
		 ORDER BY i.created_at, i.id
		 LIMIT %[2]d`, schema, receivablesLedgerLimit))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var r ReceivableLedgerRow
		var deleted bool
		var currency string
		var netPaise int64
		if err := rows.Scan(&r.Document, &r.Customer, &r.Status, &deleted, &currency, &r.Book, &netPaise, &r.Postings); err != nil {
			return nil, err
		}
		r.Source, r.Owner = "Sales invoice", receivablesOwner
		r.Book = round2(r.Book)
		r.Ledger = PaiseToRupees(netPaise)
		r.Difference = round2(r.Book - r.Ledger)
		agrees := math.Abs(r.Difference) < controlTotalTolerance
		switch {
		case agrees:
			r.Hint = "agrees"
		case currency != "INR":
			r.Hint = fmt.Sprintf("a %s invoice: its book is in %s, the ledger in rupees at the booking rate - compare at that rate", currency, currency)
		case deleted:
			r.Hint = "the invoice record was deleted but the ledger still carries its receivable - reverse it with a credit note or journal"
		case r.Status != "Approved" && r.Status != "Paid":
			r.Hint = fmt.Sprintf("the invoice is %s but the ledger still carries its receivable - reverse it with a credit note", r.Status)
		case r.Postings == 0:
			r.Hint = "posted, but nothing reached the ledger - the posting failed or the status was set by hand"
		case r.Book > r.Ledger:
			r.Hint = "the invoice says more is owed than the ledger - a receipt or credit note reached the ledger but not the invoice"
		default:
			r.Hint = "the ledger says more is owed than the invoice - a receipt or credit note was marked on the invoice without its ledger entry"
		}
		if mismatchesOnly && agrees {
			continue
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()

	// The 1300 postings no invoice accounts for, by kind.
	rows, err = db.DB.Query(fmt.Sprintf(`
		SELECT g.document_type, COUNT(DISTINCT g.document_id), SUM(g.debit) - SUM(g.credit), COUNT(*),
		       array_to_string((array_agg(DISTINCT g.document_id))[1:5], ', ')
		  FROM %[1]s.gl_postings g
		 WHERE g.account_code = '1300'
		   AND NOT (g.document_type = 'SalesInvoice'
		            AND EXISTS (SELECT 1 FROM %[1]s.documents i WHERE i.doctype = 'SalesInvoice' AND i.id = g.document_id))
		   AND NOT (g.document_type = 'CreditNote'
		            AND EXISTS (SELECT 1 FROM %[1]s.documents cn JOIN %[1]s.documents i ON i.doctype = 'SalesInvoice' AND i.id = cn.data->>'sales_invoice_id'
		                         WHERE cn.doctype = 'CreditNote' AND cn.id = g.document_id))
		 GROUP BY g.document_type
		 ORDER BY g.document_type`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var docType, sample string
		var docs int
		var netPaise int64
		r := ReceivableLedgerRow{Owner: receivablesOwner, Status: "-"}
		if err := rows.Scan(&docType, &docs, &netPaise, &r.Postings, &sample); err != nil {
			return nil, err
		}
		r.Source = "Ledger only: " + docType
		r.Document = sample
		if docs > 5 {
			r.Document += fmt.Sprintf(" and %d more", docs-5)
		}
		r.Ledger = PaiseToRupees(netPaise)
		r.Difference = round2(-r.Ledger)
		agrees := math.Abs(r.Difference) < controlTotalTolerance
		switch {
		case agrees:
			r.Hint = "nets to nothing"
		case docType == "SalesInvoice":
			r.Hint = "postings for an invoice whose record no longer exists - find the invoice or reverse its receivable"
		case docType == "CreditNote":
			r.Hint = "a credit note not linked to any invoice - link it, or it stays outside the invoices' balances"
		case strings.HasPrefix(docType, "Marketplace") || docType == "POSCart":
			r.Hint = "marketplace sales owed by the marketplace and cleared by its settlements - not customer invoices, so the ageing does not show them"
		case docType == "JournalVoucher":
			r.Hint = "a journal posted straight to receivables - no invoice carries it"
		default:
			r.Hint = "posted to receivables by something that is not a customer invoice"
		}
		if mismatchesOnly && agrees {
			continue
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func init() {
	RegisterReport(ReportDefinition{
		ID: "receivables-vs-ledger", Label: "Receivables vs Ledger (by invoice)", Category: "Finance", StoreKey: "-",
		Columns: []ReportColumn{
			{Key: "source", Label: "Source"},
			{Key: "document", Label: "Document"},
			{Key: "customer", Label: "Customer"},
			{Key: "status", Label: "Status"},
			{Key: "book_outstanding", Label: "Owed per invoice", Sensitive: true},
			{Key: "ledger_balance", Label: "Owed per ledger (1300)", Sensitive: true},
			{Key: "difference", Label: "Difference", Sensitive: true},
			{Key: "ledger_postings", Label: "Ledger postings"},
			{Key: "owner", Label: "Owner"},
			{Key: "hint", Label: "What it suggests"},
		},
		Params: []ReportParam{
			{Key: "all", Label: "Include rows that agree", Type: "select", Options: "No,Yes"},
		},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			rows, err := ReceivablesVsLedger(tenantID, params["all"] != "Yes")
			if err != nil {
				return nil, err
			}
			return structsToRows(rows)
		},
	})
}
