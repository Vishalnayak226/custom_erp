package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	pimTransformMaxPad     = 200
	pimTransformMaxPattern = 200
)

var pimHTMLTagPattern = regexp.MustCompile(`(?s)<[^>]*>`)

// pimCompileTransformRegex compiles an operand as RE2 (linear time, no
// backtracking) and caps its length so a rule cannot carry a huge pattern.
func pimCompileTransformRegex(pattern string) (*regexp.Regexp, error) {
	if len(pattern) > pimTransformMaxPattern {
		return nil, fmt.Errorf("the pattern is longer than %d characters", pimTransformMaxPattern)
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("the pattern is not valid: %v", err)
	}
	return re, nil
}

func pimSplitList(list string) []string {
	out := []string{}
	for _, part := range strings.Split(list, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func pimTransformLookup(tc PIMTransformContext, attribute string) string {
	if tc.Lookup == nil {
		return ""
	}
	return tc.Lookup(attribute)
}

// lookupPIMValueMap finds the marketplace's name for our value. found is
// false when the channel has no map for it (the value then passes through).
func lookupPIMValueMap(tenantID, channel, attribute, ourValue string) (string, bool, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", false, err
	}
	var theirs string
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(data->>'their_value', '') FROM %s.documents
		WHERE doctype = 'PIMValueMap' AND deleted_at IS NULL AND status = 'Active'
		  AND UPPER(data->>'channel') = UPPER($1) AND UPPER(data->>'attribute') = UPPER($2)
		  AND UPPER(TRIM(data->>'our_value')) = UPPER(TRIM($3))
		ORDER BY id LIMIT 1`, schema), strings.TrimSpace(channel), strings.TrimSpace(attribute), ourValue).Scan(&theirs)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return theirs, true, nil
}

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
	// applyCtx is used instead of apply by a step that reads beyond its own
	// value (a value map, another attribute of the same product).
	applyCtx func(tc PIMTransformContext, value, operand1, operand2 string) (string, error)
	// RunsOnBlank (Stage 146.1.2): the step is meant for a blank value
	// (fill a default, build from other attributes). Every other step is
	// skipped on a blank value so it never emits junk (a prefix on nothing).
	RunsOnBlank bool
}

// PIMTransformContext is what a rule may read besides the value it is given.
// Channel is the marketplace being exported to (a value-map step reads that
// channel's names); Lookup returns another attribute of the same product or
// import row (join, first_non_empty). All are optional.
type PIMTransformContext struct {
	TenantID string
	Channel  string
	Lookup   func(attribute string) string
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
		RunsOnBlank: true,
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
	"constant": {
		Description:   "Replace the value with a fixed text.",
		NeedsOperand1: true, Operand1Hint: "the fixed text",
		RunsOnBlank: true,
		apply: func(_, operand1, _ string) (string, error) {
			return operand1, nil
		},
	},
	"join": {
		Description:   "Join this value with other attributes of the same product, skipping blanks.",
		NeedsOperand1: true, Operand1Hint: "attribute codes, comma separated",
		Operand2Hint: "separator, default a space",
		RunsOnBlank:  true,
		applyCtx: func(tc PIMTransformContext, value, operand1, operand2 string) (string, error) {
			sep := operand2
			if sep == "" {
				sep = " "
			}
			parts := []string{}
			if strings.TrimSpace(value) != "" {
				parts = append(parts, strings.TrimSpace(value))
			}
			for _, code := range pimSplitList(operand1) {
				if other := strings.TrimSpace(pimTransformLookup(tc, code)); other != "" {
					parts = append(parts, other)
				}
			}
			return strings.Join(parts, sep), nil
		},
	},
	"first_non_empty": {
		Description:   "Keep the value, or the first filled attribute of the list when it is blank.",
		NeedsOperand1: true, Operand1Hint: "attribute codes, comma separated",
		RunsOnBlank: true,
		applyCtx: func(tc PIMTransformContext, value, operand1, _ string) (string, error) {
			if strings.TrimSpace(value) != "" {
				return value, nil
			}
			for _, code := range pimSplitList(operand1) {
				if other := strings.TrimSpace(pimTransformLookup(tc, code)); other != "" {
					return other, nil
				}
			}
			return "", nil
		},
	},
	"pad": {
		Description:   "Pad to a width with a character, on the left by default.",
		NeedsOperand1: true, Operand1Hint: "the width, a whole number",
		Operand2Hint: "pad character, default 0; add :right to pad on the right",
		apply: func(value, operand1, operand2 string) (string, error) {
			width, err := strconv.Atoi(strings.TrimSpace(operand1))
			if err != nil || width < 0 || width > pimTransformMaxPad {
				return "", fmt.Errorf("pad needs a width from 0 to %d, got %q", pimTransformMaxPad, operand1)
			}
			char, right := "0", false
			if operand2 != "" {
				char = operand2
				if strings.HasSuffix(char, ":right") {
					char, right = strings.TrimSuffix(char, ":right"), true
				} else if strings.HasSuffix(char, ":left") {
					char = strings.TrimSuffix(char, ":left")
				}
				if utf8.RuneCountInString(char) != 1 {
					return "", fmt.Errorf("pad needs a single pad character, got %q", operand2)
				}
			}
			missing := width - utf8.RuneCountInString(value)
			if missing <= 0 {
				return value, nil
			}
			fill := strings.Repeat(char, missing)
			if right {
				return value + fill, nil
			}
			return fill + value, nil
		},
	},
	"regex_replace": {
		Description:   "Replace every match of a regular expression (RE2); $1 in the replacement is the first group.",
		NeedsOperand1: true, Operand1Hint: "the pattern",
		Operand2Hint: "the replacement, may be blank",
		apply: func(value, operand1, operand2 string) (string, error) {
			re, err := pimCompileTransformRegex(operand1)
			if err != nil {
				return "", fmt.Errorf("regex_replace: %w", err)
			}
			return re.ReplaceAllString(value, operand2), nil
		},
	},
	"regex_extract": {
		Description:   "Keep the first match of a regular expression (RE2), or its first group when it has one; blank when nothing matches.",
		NeedsOperand1: true, Operand1Hint: "the pattern",
		apply: func(value, operand1, _ string) (string, error) {
			re, err := pimCompileTransformRegex(operand1)
			if err != nil {
				return "", fmt.Errorf("regex_extract: %w", err)
			}
			m := re.FindStringSubmatch(value)
			switch {
			case m == nil:
				return "", nil
			case len(m) > 1:
				return m[1], nil
			}
			return m[0], nil
		},
	},
	"strip_html": {
		Description: "Remove HTML tags, decode entities and collapse spaces.",
		apply: func(value, _, _ string) (string, error) {
			text := pimHTMLTagPattern.ReplaceAllString(value, " ")
			return strings.Join(strings.Fields(html.UnescapeString(text)), " "), nil
		},
	},
	"split": {
		Description:   "Keep one part of the value split on a separator.",
		NeedsOperand1: true, Operand1Hint: "the separator",
		NeedsOperand2: true, Operand2Hint: "which part, counting from 1",
		apply: func(value, operand1, operand2 string) (string, error) {
			n, err := strconv.Atoi(strings.TrimSpace(operand2))
			if err != nil || n < 1 {
				return "", fmt.Errorf("split needs a part number from 1, got %q", operand2)
			}
			parts := strings.Split(value, operand1)
			if n > len(parts) {
				return "", nil
			}
			return strings.TrimSpace(parts[n-1]), nil
		},
	},
	"value_map": {
		Description:   "Swap our value for the marketplace's name from the Value Maps list.",
		NeedsOperand1: true, Operand1Hint: "the attribute the map is kept under",
		Operand2Hint: "channel code, default the channel being exported",
		applyCtx: func(tc PIMTransformContext, value, operand1, operand2 string) (string, error) {
			channel := strings.TrimSpace(operand2)
			if channel == "" {
				channel = tc.Channel
			}
			if channel == "" {
				return "", fmt.Errorf("value_map needs a channel: put the channel code in operand 2")
			}
			mapped, found, err := lookupPIMValueMap(tc.TenantID, channel, operand1, value)
			if err != nil {
				return "", err
			}
			if !found {
				return value, nil
			}
			return mapped, nil
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
		// Stage 146.5.3: a bad regex, pad width or split position is caught
		// here too, by running the step on a sample value.
		if name == "regex_replace" || name == "regex_extract" || name == "pad" || name == "split" {
			if _, err := fn.apply("sample", step.Operand1, step.Operand2); err != nil {
				return &ValidationError{Code: "GLOBAL-0002", SubFor: "Steps",
					Message: fmt.Sprintf("step %d: %v", i+1, err)}
			}
		}
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
	return ApplyPIMTransformRuleCtx(ruleID, value, PIMTransformContext{TenantID: tenantID})
}

// ApplyPIMTransformRuleCtx is ApplyPIMTransformRule with the channel and
// product context a value map or a join needs.
func ApplyPIMTransformRuleCtx(ruleID, value string, tc PIMTransformContext) (string, error) {
	_, steps, err := fetchPIMTransformRule(tc.TenantID, ruleID)
	if err != nil {
		return "", err
	}
	return runPIMTransformSteps(ruleID, steps, value, tc)
}

// runPIMTransformSteps: a blank value (Stage 146.1.2) still goes through the
// rule, but only steps marked RunsOnBlank act on it; the rest are skipped so a
// prefix, a format or a find-replace never turns nothing into junk.
func runPIMTransformSteps(ruleID string, steps []PIMTransformStep, value string, tc PIMTransformContext) (string, error) {
	result := value
	var err error
	for i, step := range steps {
		fn, known := pimTransformFunctions[strings.TrimSpace(step.Function)]
		if !known {
			// Unreachable through the generic API (ValidatePIMTransformRuleDocument
			// refuses this at save time) but guarded rather than panicking if a
			// row is ever written by a path that skips validation.
			return "", fmt.Errorf("transform rule %q step %d: unknown function %q", ruleID, i+1, step.Function)
		}
		if strings.TrimSpace(result) == "" && !fn.RunsOnBlank {
			result = ""
			continue
		}
		if fn.applyCtx != nil {
			result, err = fn.applyCtx(tc, result, step.Operand1, step.Operand2)
		} else {
			result, err = fn.apply(result, step.Operand1, step.Operand2)
		}
		if err != nil {
			return "", fmt.Errorf("transform rule %q step %d (%s): %w", ruleID, i+1, step.Function, err)
		}
	}
	return result, nil
}
