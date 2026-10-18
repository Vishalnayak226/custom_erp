package engines

import (
	"custom_erp/db"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// Stage 73.1.9: the reports that read gl_postings.posted_by/source_command.
//
// "gl-journal" is the General Journal - every posted GL line in a period with
// the user or service that posted it, filterable by poster, by attribution and
// by source document type. "gl-posting-review" is the accountable-posting
// review (55.10): one row per poster and command with the line count and
// totals, drilling to the exact lines behind it.
//
// Rows written before 73.1.9 have NULL attribution and are shown as
// glBeforeAttribution - honestly unknown, never guessed.

const glBeforeAttribution = "before attribution"

func glPostedByLabel(v sql.NullString) string {
	if !v.Valid || v.String == "" {
		return glBeforeAttribution
	}
	return v.String
}

// glAttributionFilter turns the report's poster / attribution params into a
// WHERE fragment. postedBy matches exactly ("before attribution" selects the
// NULL rows); attribution is "", "All", "Attributed" or "Before attribution".
func glAttributionFilter(postedBy, attribution string, nextArg int) (string, []interface{}) {
	postedBy = strings.TrimSpace(postedBy)
	var clause string
	var args []interface{}
	switch {
	case strings.EqualFold(postedBy, glBeforeAttribution):
		clause += " AND p.posted_by IS NULL"
	case postedBy != "":
		clause += fmt.Sprintf(" AND p.posted_by = $%d", nextArg)
		args = append(args, postedBy)
	}
	switch strings.ToLower(strings.TrimSpace(attribution)) {
	case "attributed":
		clause += " AND p.posted_by IS NOT NULL"
	case "before attribution":
		clause += " AND p.posted_by IS NULL"
	}
	return clause, args
}

// GetGLJournal lists every GL line in [startDate, endDate] with its poster.
func GetGLJournal(tenantID, startDate, endDate, postedBy, attribution, documentType string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	if startDate == "" || endDate == "" {
		return nil, fmt.Errorf("start and end are required")
	}
	args := []interface{}{startDate, endDate}
	where := "p.created_at >= $1::date AND p.created_at < ($2::date + 1)"
	attrClause, attrArgs := glAttributionFilter(postedBy, attribution, len(args)+1)
	where += attrClause
	args = append(args, attrArgs...)
	if dt := strings.TrimSpace(documentType); dt != "" {
		args = append(args, dt)
		where += fmt.Sprintf(" AND p.document_type = $%d", len(args))
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT p.created_at, p.document_type, p.document_id, p.account_code, a.account_name,
		       p.debit, p.credit, p.posted_by, p.source_command
		FROM %s.gl_postings p JOIN %s.gl_accounts a ON a.account_code = p.account_code
		WHERE %s
		ORDER BY p.created_at ASC, p.document_id, p.debit DESC`, schema, schema, where), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]interface{}{}
	for rows.Next() {
		var createdAt time.Time
		var docType, docID, accountCode, accountName string
		var debitPaise, creditPaise int64
		var by, command sql.NullString
		if err := rows.Scan(&createdAt, &docType, &docID, &accountCode, &accountName, &debitPaise, &creditPaise, &by, &command); err != nil {
			return nil, err
		}
		out = append(out, map[string]interface{}{
			"created_at": createdAt, "document_type": docType, "document_id": docID,
			"account_code": accountCode, "account_name": accountName,
			"debit": PaiseToRupees(debitPaise), "credit": PaiseToRupees(creditPaise),
			"posted_by": glPostedByLabel(by), "source_command": command.String,
		})
	}
	return out, rows.Err()
}

// GetGLPostingReview summarises a period's GL lines by poster and command.
func GetGLPostingReview(tenantID, startDate, endDate string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	if startDate == "" || endDate == "" {
		return nil, fmt.Errorf("start and end are required")
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT p.posted_by, COALESCE(string_agg(DISTINCT p.source_command, ', '), ''), COUNT(*),
		       COUNT(DISTINCT p.document_type || ':' || p.document_id),
		       COALESCE(SUM(p.debit), 0), COALESCE(SUM(p.credit), 0), MIN(p.created_at), MAX(p.created_at)
		FROM %s.gl_postings p
		WHERE p.created_at >= $1::date AND p.created_at < ($2::date + 1)
		GROUP BY p.posted_by
		ORDER BY p.posted_by NULLS LAST`, schema), startDate, endDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]interface{}{}
	for rows.Next() {
		var by sql.NullString
		var command string
		var lines, documents int
		var debitPaise, creditPaise int64
		var first, last time.Time
		if err := rows.Scan(&by, &command, &lines, &documents, &debitPaise, &creditPaise, &first, &last); err != nil {
			return nil, err
		}
		out = append(out, map[string]interface{}{
			"posted_by": glPostedByLabel(by), "source_command": command, "lines": lines, "documents": documents,
			"debit": PaiseToRupees(debitPaise), "credit": PaiseToRupees(creditPaise),
			"first_posted": first, "last_posted": last,
		})
	}
	return out, rows.Err()
}

func init() {
	attributionParams := []ReportParam{
		{Key: "posted_by", Label: "Posted by (username, service:job, or \"before attribution\")", Type: "text"},
		{Key: "attribution", Label: "Attribution", Type: "select", Options: "All,Attributed,Before attribution"},
	}
	RegisterReport(ReportDefinition{
		ID: "gl-journal", Label: "General Journal (who posted what)", Category: "Finance",
		Columns: []ReportColumn{
			{Key: "created_at", Label: "Posted at"}, {Key: "document_type", Label: "Document Type"},
			{Key: "document_id", Label: "Document"}, {Key: "account_code", Label: "Account"},
			{Key: "account_name", Label: "Account Name"},
			{Key: "debit", Label: "Debit", Sensitive: true}, {Key: "credit", Label: "Credit", Sensitive: true},
			{Key: "posted_by", Label: "Posted by"}, {Key: "source_command", Label: "Source command"},
		},
		Params: append([]ReportParam{
			{Key: "start", Label: "From", Type: "date", Required: true},
			{Key: "end", Label: "To", Type: "date", Required: true},
			{Key: "document_type", Label: "Document type (optional)", Type: "text"},
		}, attributionParams...),
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			return GetGLJournal(tenantID, params["start"], params["end"], params["posted_by"], params["attribution"], params["document_type"])
		},
	})

	RegisterReport(ReportDefinition{
		ID: "gl-posting-review", Label: "GL Posting Review (by poster)", Category: "Finance",
		Columns: []ReportColumn{
			{Key: "posted_by", Label: "Posted by"}, {Key: "source_command", Label: "Source commands"},
			{Key: "lines", Label: "GL lines"}, {Key: "documents", Label: "Documents"},
			{Key: "debit", Label: "Debit", Sensitive: true}, {Key: "credit", Label: "Credit", Sensitive: true},
			{Key: "first_posted", Label: "First posted"}, {Key: "last_posted", Label: "Last posted"},
		},
		Params: []ReportParam{
			{Key: "start", Label: "From", Type: "date", Required: true},
			{Key: "end", Label: "To", Type: "date", Required: true},
		},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			return GetGLPostingReview(tenantID, params["start"], params["end"])
		},
		// The drill key is the poster; the lines are that poster's, in the
		// same period - the same scoped set the summary row counted.
		DrillDown: func(tenantID, rowKey string, params map[string]string) ([]map[string]interface{}, error) {
			return GetGLJournal(tenantID, params["start"], params["end"], rowKey, "", "")
		},
	})
}
