package engines

// Stage 39.9 - registers the Knowledge Center Feedback report. See
// help_feedback.go for the query it runs.

func init() {
	RegisterReport(ReportDefinition{
		ID: "help-article-feedback", Label: "Knowledge Center Feedback", Category: "Admin",
		Columns: []ReportColumn{
			{Key: "article", Label: "Article Slug"},
			{Key: "helpful_count", Label: "Marked Helpful"},
			{Key: "not_helpful_count", Label: "Marked Not Helpful"},
			{Key: "total_count", Label: "Total Responses"},
			{Key: "helpful_pct", Label: "Helpful %"},
			{Key: "last_feedback_at", Label: "Last Feedback"},
		},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			rows, err := GetHelpFeedbackSummary(tenantID)
			if err != nil {
				return nil, err
			}
			return structsToRows(rows)
		},
	})
}