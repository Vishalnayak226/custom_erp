package engines

import (
	"custom_erp/db"
	"fmt"
)

// Stage 39.9 - "Was this helpful?" feedback. HelpArticleFeedback (seeded by
// db/migrations_stage39_9_help_feedback.sql) is an ordinary generic document,
// not a bespoke table - submission goes straight through the existing
// POST /api/v1/doc/{doctype} API with no new Go handler at all. This file is
// the read side: aggregating raw Yes/No rows into something a Knowledge
// Center author can act on, the same "register a function" shape
// engines/traceability_reports.go uses for the batch/serial reports.

// HelpFeedbackSummary is one article's aggregated feedback.
type HelpFeedbackSummary struct {
	Article         string `json:"article"`
	HelpfulCount    int    `json:"helpful_count"`
	NotHelpfulCount int    `json:"not_helpful_count"`
	TotalCount      int    `json:"total_count"`
	HelpfulPct      int    `json:"helpful_pct"`
	LastFeedbackAt  string `json:"last_feedback_at"`
}

// GetHelpFeedbackSummary groups every HelpArticleFeedback document by the
// article slug it names. Percentage is computed in Go rather than in SQL to
// keep the zero-responses case a plain zero instead of a divide-by-zero
// special case in the query itself.
func GetHelpFeedbackSummary(tenantID string) ([]HelpFeedbackSummary, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT
			data->>'article' AS article,
			COUNT(*) FILTER (WHERE data->>'helpful' = 'Yes') AS helpful_count,
			COUNT(*) FILTER (WHERE data->>'helpful' = 'No') AS not_helpful_count,
			COUNT(*) AS total_count,
			TO_CHAR(MAX(created_at), 'YYYY-MM-DD') AS last_feedback_at
		FROM %s.documents
		WHERE doctype = 'HelpArticleFeedback' AND deleted_at IS NULL
		GROUP BY data->>'article'
		ORDER BY total_count DESC, article ASC`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []HelpFeedbackSummary
	for rows.Next() {
		var s HelpFeedbackSummary
		if err := rows.Scan(&s.Article, &s.HelpfulCount, &s.NotHelpfulCount, &s.TotalCount, &s.LastFeedbackAt); err != nil {
			return nil, err
		}
		if s.TotalCount > 0 {
			s.HelpfulPct = s.HelpfulCount * 100 / s.TotalCount
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if out == nil {
		out = []HelpFeedbackSummary{}
	}
	return out, nil
}