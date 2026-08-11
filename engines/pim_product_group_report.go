package engines

import "strings"

func init() {
	RegisterReport(ReportDefinition{
		ID: "pim-product-group-readiness", Label: "PIM Product Group Readiness", Category: "PIM",
		Columns: []ReportColumn{
			{Key: "item_code", Label: "Item"},
			{Key: "name", Label: "Name"},
			{Key: "family", Label: "Family"},
			{Key: "status", Label: "Status"},
			{Key: "completeness", Label: "Completeness %"},
			{Key: "missing_fields", Label: "Missing Fields"},
		},
		Params: []ReportParam{{Key: "group_id", Label: "Product Group ID or code", Type: "text", Required: true}},
		Run:    runPIMProductGroupReadinessReport,
	})
}

func runPIMProductGroupReadinessReport(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
	resolved, err := ResolvePIMProductGroup(tenantID, params["group_id"])
	if err != nil {
		return nil, err
	}
	rows := make([]map[string]interface{}, 0, len(resolved.Members))
	for _, member := range resolved.Members {
		rows = append(rows, map[string]interface{}{
			"item_code":      member.ItemCode,
			"name":           member.Name,
			"family":         member.Family,
			"status":         member.Status,
			"completeness":   member.Completeness,
			"missing_fields": strings.Join(member.Missing, ", "),
		})
	}
	return rows, nil
}
