package engines

import (
	"encoding/json"
	"fmt"
)

// Stage 20 Track B.4 (20.35): a generic, reusable report framework
// replacing the previous shape (one hardcoded Go function + one hardcoded
// frontend render function per report, engines/reports.go). Deliberately
// NOT a full ad hoc SQL query builder - this ERP's scope doesn't need
// user-authored arbitrary SQL (an injection-risk feature this codebase's
// lightweight/solid principle argues against); instead every report is a
// fixed, developer-defined Go function registered once, with its
// parameters/columns described as data so ONE frontend screen can drive
// any of them - adding a new report from here on is "register a function,"
// not "write a new endpoint + a new render function."

// ReportColumn describes one output column - drives the generic frontend
// table and (via Sensitive) column-level masking (Stage 20.39).
type ReportColumn struct {
	Key       string `json:"key"`
	Label     string `json:"label"`
	Sensitive bool   `json:"sensitive"`
}

// ReportParam describes one input field, rendered generically by the
// frontend (date/text/select input) rather than each report needing its
// own bespoke filter form.
type ReportParam struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Type     string `json:"type"` // "date", "text", "select"
	Options  string `json:"options,omitempty"`
	Required bool   `json:"required"`
}

// ReportRunFunc executes a report and returns generic rows keyed by each
// ReportColumn's Key.
type ReportRunFunc func(tenantID string, params map[string]string) ([]map[string]interface{}, error)

// ReportDrillDownFunc returns the transaction-level rows behind one summary
// row (Stage 20.38) - optional per report.
type ReportDrillDownFunc func(tenantID string, rowKey string, params map[string]string) ([]map[string]interface{}, error)

// ReportDefinition is one catalog entry. Existing reports (engines/reports.go
// et al.) are wrapped here as-is rather than rewritten; every Stage 20.40
// catalog addition is registered the same way.
type ReportDefinition struct {
	ID           string              `json:"id"`
	Label        string              `json:"label"`
	Category     string              `json:"category"`
	Columns      []ReportColumn      `json:"columns"`
	Params       []ReportParam       `json:"params"`
	HasDrillDown bool                `json:"has_drill_down"`
	Run          ReportRunFunc       `json:"-"`
	DrillDown    ReportDrillDownFunc `json:"-"`
}

var reportRegistry = map[string]ReportDefinition{}
var reportRegistryOrder []string

// RegisterReport adds a definition to the catalog. Called only from this
// package's own init() functions (engines/report_definitions.go) - a
// duplicate ID is a build-time programmer error, so it panics rather than
// silently overwriting or returning a runtime error nothing would check.
func RegisterReport(def ReportDefinition) {
	if _, exists := reportRegistry[def.ID]; exists {
		panic(fmt.Sprintf("report %q already registered", def.ID))
	}
	def.HasDrillDown = def.DrillDown != nil
	reportRegistry[def.ID] = def
	reportRegistryOrder = append(reportRegistryOrder, def.ID)
}

// ListReportDefinitions returns the catalog in registration order, for the
// frontend's generic report picker.
func ListReportDefinitions() []ReportDefinition {
	out := make([]ReportDefinition, 0, len(reportRegistryOrder))
	for _, id := range reportRegistryOrder {
		out = append(out, reportRegistry[id])
	}
	return out
}

// reportFullVisibilityRoles see every column in full; any other role gets
// Sensitive-flagged columns redacted in place (Stage 20.39, matching the
// master plan's own Loophole Matrix control for "report export leaks
// customer or finance data"). Redacted in place, not omitted, so the
// column set the frontend renders stays identical regardless of who asked.
var reportFullVisibilityRoles = map[string]bool{
	"HR/Admin":      true,
	"Store Manager": true,
}

const redactedReportValue = "•••"

func maskSensitiveColumns(columns []ReportColumn, rows []map[string]interface{}, role string) []map[string]interface{} {
	if reportFullVisibilityRoles[role] {
		return rows
	}
	var sensitiveKeys []string
	for _, c := range columns {
		if c.Sensitive {
			sensitiveKeys = append(sensitiveKeys, c.Key)
		}
	}
	if len(sensitiveKeys) == 0 {
		return rows
	}
	for _, row := range rows {
		for _, k := range sensitiveKeys {
			if _, ok := row[k]; ok {
				row[k] = redactedReportValue
			}
		}
	}
	return rows
}

// RunReport looks up a registered report, validates required params, runs
// it, and applies column masking for the requesting role.
func RunReport(tenantID, reportID, role string, params map[string]string) (*ReportDefinition, []map[string]interface{}, error) {
	def, ok := reportRegistry[reportID]
	if !ok {
		return nil, nil, fmt.Errorf("unknown report %q", reportID)
	}
	for _, p := range def.Params {
		if p.Required && params[p.Key] == "" {
			return nil, nil, fmt.Errorf("param %q is required", p.Key)
		}
	}
	rows, err := def.Run(tenantID, params)
	if err != nil {
		return nil, nil, err
	}
	rows = maskSensitiveColumns(def.Columns, rows, role)
	return &def, rows, nil
}

// RunReportDrillDown runs a registered report's drill-down function (Stage
// 20.38), masking the same way RunReport does.
func RunReportDrillDown(tenantID, reportID, role, rowKey string, params map[string]string) ([]map[string]interface{}, error) {
	def, ok := reportRegistry[reportID]
	if !ok {
		return nil, fmt.Errorf("unknown report %q", reportID)
	}
	if def.DrillDown == nil {
		return nil, fmt.Errorf("report %q has no drill-down", reportID)
	}
	rows, err := def.DrillDown(tenantID, rowKey, params)
	if err != nil {
		return nil, err
	}
	return maskSensitiveColumns(def.Columns, rows, role), nil
}

// structsToRows converts a slice of any JSON-taggable struct (or a single
// struct, wrapped in a 1-element result) into generic rows keyed by its own
// json tags - one universal adapter so existing typed report functions
// (e.g. []SalesRegisterEntry, *GSTReturnSummary) register without a
// bespoke converter each.
func structsToRows(v interface{}) ([]map[string]interface{}, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	trimmed := len(b) > 0 && b[0] == '{'
	if trimmed {
		var row map[string]interface{}
		if err := json.Unmarshal(b, &row); err != nil {
			return nil, err
		}
		return []map[string]interface{}{row}, nil
	}
	var rows []map[string]interface{}
	if err := json.Unmarshal(b, &rows); err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []map[string]interface{}{}
	}
	return rows, nil
}
