package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Stage 36.5: declarative data transformation rules.
//
// engines/connector.go's BuildChannelPayload already maps a source field to
// a target field per ChannelFieldMap row, but that mapping is a pure
// rename - the value crosses unchanged. PIMTransformRule is an ordered list
// of steps drawn from a CLOSED vocabulary (pimTransformFunctions below),
// the same shape Stage 36.2.3's workflow condition vocabulary uses and for
// the same reason: a rule is authored by a category manager in a form, and
// a form that accepts an expression or code is a remote-execution surface.
// ApplyPIMTransformRule is the one seam both the export path
// (ChannelFieldMap.transform_rule, engines/connector.go) and the import
// path (PIMImportTemplate's per-column rule, Stage 36.3) call, so the two
// can never disagree about what a given rule does to a value.

// PIMTransformStep is one row of a PIMTransformRule.steps JSONTable.
type PIMTransformStep struct {
	Sequence int    `json:"sequence"`
	Function string `json:"function"`
	Operand1 string `json:"operand1,omitempty"`
	Operand2 string `json:"operand2,omitempty"`
}

type pimTransformFunction struct {
	Description   string
	NeedsOperand1 bool
	Operand1Hint  string
	NeedsOperand2 bool
	Operand2Hint  string
	apply         func(value, operand1, operand2 string) (string, error)
}

// PIMTransformFunctionInfo is the form-facing description of one function,
// exported through ListPIMTransformFunctions so a rule's step editor can
// offer exactly what the engine implements.
type PIMTransformFunctionInfo struct {
	Key           string `json:"key"`
	Description   string `json:"description"`
	NeedsOperand1 bool   `json:"needs_operand1"`
	Operand1Hint  string `json:"operand1_hint,omitempty"`
	NeedsOperand2 bool   `json:"needs_operand2"`
	Operand2Hint  string `json:"operand2_hint,omitempty"`
}

var pimTransformFunctions = map[string]pimTransformFunction{
	"trim": {
		Description: "Remove leading and trailing whitespace.",
		apply: func(value, _, _ string) (string, error) {
			return strings.TrimSpace(value), nil
		},
	},
	"uppercase": {
		Description: "Convert to upper case.",
		apply: func(value, _, _ string) (string, error) {
			return strings.ToUpper(value), nil
		},
	},
	"lowercase": {
		Description: "Convert to lower case.",
		apply: func(value, _, _ string) (string, error) {
			return strings.ToLower(value), nil
		},
	},
	"prefix": {
		Description:   "Prepend a literal string.",
		NeedsOperand1: true, Operand1Hint: "the text to prepend",
		apply: func(value, operand1, _ string) (string, error) {
			return operand1 + value, nil
		},
	},
	"suffix": {
		Description:   "Append a literal string.",
		NeedsOperand1: true, Operand1Hint: "the text to append",
		apply: func(value, operand1, _ string) (string, error) {
			return value + operand1, nil
		},
	},
	"truncate": {
		Description:   "Cut to at most N characters.",
		NeedsOperand1: true, Operand1Hint: "a whole number of characters",
		apply: func(value, operand1, _ string) (string, error) {
			n, err := strconv.Atoi(strings.TrimSpace(operand1))
			if err != nil || n < 0 {
				return "", fmt.Errorf("truncate needs a non-negative whole number, got %q", operand1)
			}
			runes := []rune(value)
			if len(runes) <= n {
				return value, nil
			}
			return string(runes[:n]), nil
		},
	},
	"default_if_empty": {
		Description:   "Substitute a literal value when the source is blank.",
		NeedsOperand1: true, Operand1Hint: "the fallback value",
		apply: func(value, operand1, _ string) (string, error) {
			if strings.TrimSpace(value) == "" {
				return operand1, nil
			}
			return value, nil
		},
	},
	"find_replace_literal": {
		Description:   "Replace every literal occurrence of one string with another (not a regular expression).",
		NeedsOperand1: true, Operand1Hint: "the text to find",
		NeedsOperand2: true, Operand2Hint: "the text to replace it with",
		apply: func(value, operand1, operand2 string) (string, error) {
			if operand1 == "" {
				return "", fmt.Errorf("find_replace_literal needs a non-empty search text")
			}
			return strings.ReplaceAll(value, operand1, operand2), nil
		},
	},
	"number_format": {
		Description:   "Parse the value as a number and format it with a fixed number of decimal places.",
		NeedsOperand1: true, Operand1Hint: "a whole number of decimal places",
		apply: func(value, operand1, _ string) (string, error) {
			decimals, err := strconv.Atoi(strings.TrimSpace(operand1))
			if err != nil || decimals < 0 {
				return "", fmt.Errorf("number_format needs a non-negative whole number of decimals, got %q", operand1)
			}
			n, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
			if err != nil {
				return "", fmt.Errorf("number_format: %q is not a number", value)
			}
			return strconv.FormatFloat(n, 'f', decimals, 64), nil
		},
	},
	"date_format": {
		Description:   "Reparse a date from one layout and reformat it in another (Go reference-date layouts, e.g. 2006-01-02, 02/01/2006).",
		NeedsOperand1: true, Operand1Hint: "the source layout",
		NeedsOperand2: true, Operand2Hint: "the target layout",
		apply: func(value, operand1, operand2 string) (string, error) {
			if strings.TrimSpace(value) == "" {
				return "", nil
			}
			parsed, err := time.Parse(operand1, strings.TrimSpace(value))
			if err != nil {
				return "", fmt.Errorf("date_format: %q does not match layout %q", value, operand1)
			}
			return parsed.Format(operand2), nil
		},
	},
}

// ListPIMTransformFunctions returns the vocabulary in a stable order, for a
// rule's step-editing form.
func ListPIMTransformFunctions() []PIMTransformFunctionInfo {
	keys := make([]string, 0, len(pimTransformFunctions))
	for key := range pimTransformFunctions {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]PIMTransformFunctionInfo, 0, len(keys))
	for _, key := range keys {
		fn := pimTransformFunctions[key]
		out = append(out, PIMTransformFunctionInfo{
			Key: key, Description: fn.Description,
			NeedsOperand1: fn.NeedsOperand1, Operand1Hint: fn.Operand1Hint,
			NeedsOperand2: fn.NeedsOperand2, Operand2Hint: fn.Operand2Hint,
		})
	}
	return out
}

func decodePIMTransformSteps(raw interface{}) ([]PIMTransformStep, error) {
	var rows []map[string]interface{}
	if err := decodeProductGroupJSON(raw, &rows); err != nil {
		return nil, fmt.Errorf("steps must be a JSON array of step rows: %w", err)
	}
	steps := make([]PIMTransformStep, 0, len(rows))
	for _, row := range rows {
		sequence := 0
		if value := pimString(row["sequence"]); value != "" {
			parsed, err := strconv.ParseFloat(value, 64)
			if err != nil {
				return nil, fmt.Errorf("step has a non-numeric sequence %q", value)
			}
			sequence = int(parsed)
		}
		steps = append(steps, PIMTransformStep{
			Sequence: sequence, Function: pimString(row["function"]),
			Operand1: pimString(row["operand1"]), Operand2: pimString(row["operand2"]),
		})
	}
	return steps, nil
}

// ValidatePIMTransformRuleDocument runs at ValidateDocument's shared exit, so
// a rule saved through the generic form or a CSV import is checked the same
// way. Every check exists because the failure it prevents is silent: an
// unknown function or a missing operand saves happily and then either does
// nothing or breaks the very first import/export that uses it, far from
// where the rule was authored.
func ValidatePIMTransformRuleDocument(_ string, payload map[string]interface{}) error {
	steps, err := decodePIMTransformSteps(payload["steps"])
	if err != nil {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "Steps", Message: err.Error()}
	}
	if len(steps) == 0 {
		return &ValidationError{Code: "GLOBAL-0001", SubFor: "Steps",
			Message: "a transform rule needs at least one step"}
	}
	for i, step := range steps {
		name := strings.TrimSpace(step.Function)
		if name == "" {
			return &ValidationError{Code: "GLOBAL-0001", SubFor: "Steps",
				Message: fmt.Sprintf("step %d has no function", i+1)}
		}
		fn, known := pimTransformFunctions[name]
		if !known {
			return &ValidationError{Code: "GLOBAL-0002", SubFor: "Steps",
				Message: fmt.Sprintf("step %d names unknown function %q", i+1, name)}
		}
		if fn.NeedsOperand1 && strings.TrimSpace(step.Operand1) == "" {
			return &ValidationError{Code: "GLOBAL-0001", SubFor: "Steps",
				Message: fmt.Sprintf("step %d: function %q needs operand 1 (%s)", i+1, name, fn.Operand1Hint)}
		}
		if fn.NeedsOperand2 && strings.TrimSpace(step.Operand2) == "" {
			return &ValidationError{Code: "GLOBAL-0001", SubFor: "Steps",
				Message: fmt.Sprintf("step %d: function %q needs operand 2 (%s)", i+1, name, fn.Operand2Hint)}
		}
		// truncate/number_format's operand 1 is a whole number of
		// characters/decimals - checked directly here (not by running the
		// function, which needs a real value to validate against) so a typo
		// is caught at authoring time rather than the first import/publish
		// that hits this rule.
		if name == "truncate" || name == "number_format" {
			if n, numErr := strconv.Atoi(strings.TrimSpace(step.Operand1)); numErr != nil || n < 0 {
				return &ValidationError{Code: "GLOBAL-0002", SubFor: "Steps",
					Message: fmt.Sprintf("step %d: function %q needs a non-negative whole number for operand 1, got %q", i+1, name, step.Operand1)}
			}
		}
	}
	return nil
}

// PIMTransformRuleInfo is the list/picker-facing view of a rule.
type PIMTransformRuleInfo struct {
	ID          string             `json:"id"`
	Code        string             `json:"code"`
	Name        string             `json:"name"`
	Description string             `json:"description,omitempty"`
	Steps       []PIMTransformStep `json:"steps"`
}

// ListPIMTransformRules backs the rule picker on a Channel Field Map or
// Import Template's step editor - only Active rules, since one of those is
// choosing what to apply going forward, not auditing history.
func ListPIMTransformRules(tenantID string) ([]PIMTransformRuleInfo, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, data FROM %s.documents
		WHERE doctype = 'PIMTransformRule' AND deleted_at IS NULL AND status = 'Active'
		ORDER BY COALESCE(data->>'name', id)`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PIMTransformRuleInfo{}
	for rows.Next() {
		var id, raw string
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, err
		}
		var data map[string]interface{}
		if err := json.Unmarshal([]byte(raw), &data); err != nil {
			continue
		}
		steps, _ := decodePIMTransformSteps(data["steps"])
		sort.SliceStable(steps, func(i, j int) bool { return steps[i].Sequence < steps[j].Sequence })
		out = append(out, PIMTransformRuleInfo{
			ID: id, Code: pimString(data["code"]), Name: pimString(data["name"]),
			Description: pimString(data["description"]), Steps: steps,
		})
	}
	return out, rows.Err()
}

func fetchPIMTransformRule(tenantID, ruleID string) (string, []PIMTransformStep, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", nil, err
	}
	var canonicalID, raw, status string
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT id, data, status FROM %s.documents
		WHERE doctype = 'PIMTransformRule' AND (id = $1 OR UPPER(data->>'code') = UPPER($1)) AND deleted_at IS NULL
		ORDER BY CASE WHEN id = $1 THEN 0 ELSE 1 END, id LIMIT 1`, schema), ruleID).Scan(&canonicalID, &raw, &status)
	if err != nil {
		return "", nil, fmt.Errorf("transform rule %q not found", ruleID)
	}
	if status != "Active" {
		return "", nil, fmt.Errorf("transform rule %q is not active", ruleID)
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return "", nil, fmt.Errorf("transform rule %q has invalid stored data: %w", ruleID, err)
	}
	steps, err := decodePIMTransformSteps(data["steps"])
	if err != nil {
		return "", nil, fmt.Errorf("transform rule %q: %w", ruleID, err)
	}
	sort.SliceStable(steps, func(i, j int) bool { return steps[i].Sequence < steps[j].Sequence })
	return canonicalID, steps, nil
}

// ApplyPIMTransformRule runs value through every step of a rule in sequence
// order. The single seam both the export path (BuildChannelPayload, via
// ChannelFieldMap.transform_rule) and the import path (Stage 36.3's
// PIMImportTemplate) call, so the two can never disagree about what a rule
// does to a value.
func ApplyPIMTransformRule(tenantID, ruleID, value string) (string, error) {
	_, steps, err := fetchPIMTransformRule(tenantID, ruleID)
	if err != nil {
		return "", err
	}
	result := value
	for i, step := range steps {
		fn, known := pimTransformFunctions[strings.TrimSpace(step.Function)]
		if !known {
			// Unreachable through the generic API (ValidatePIMTransformRuleDocument
			// refuses this at save time) but guarded rather than panicking if a
			// row is ever written by a path that skips validation.
			return "", fmt.Errorf("transform rule %q step %d: unknown function %q", ruleID, i+1, step.Function)
		}
		result, err = fn.apply(result, step.Operand1, step.Operand2)
		if err != nil {
			return "", fmt.Errorf("transform rule %q step %d (%s): %w", ruleID, i+1, step.Function, err)
		}
	}
	return result, nil
}
