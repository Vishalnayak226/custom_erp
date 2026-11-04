package engines

import (
	"custom_erp/db"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Stage 146.5.7: typed attribute conditions for a Dynamic product group. A
// condition is {attribute, operator, value}; the operator must fit the
// attribute's value type, and every condition must hold (AND). Conditions are
// evaluated every time the group is resolved (nothing is stored), so feed runs
// (146.5.6), bulk set and tasks that read the group see the current members.
// A group with no conditions behaves exactly as before.

// maxConditionGroupMembers caps how many products a group with conditions
// returns; the resolution says Truncated when the cap cut it short.
var maxConditionGroupMembers = 2000

// ProductGroupCondition is one row of a group's filter_conditions.
type ProductGroupCondition struct {
	Attribute string `json:"attribute"`
	Operator  string `json:"operator"`
	Value     string `json:"value"`
}

// conditionOperators is the operator list per kind of value.
var conditionOperators = map[string][]string{
	"text":   {"equals", "not equals", "in", "contains", "starts with", "is empty", "is not empty"},
	"number": {"equals", "not equals", "in", "greater than", "less than", "between", "is empty", "is not empty"},
	"select": {"equals", "not equals", "in", "is empty", "is not empty"},
	"date":   {"equals", "before", "after", "between", "is empty", "is not empty"},
}

// conditionItemFields are the Item fields a condition may name besides an
// attribute; they behave as text.
var conditionItemFields = map[string]bool{"name": true, "family": true, "status": true}

// ConditionOperatorsFor lists the operators that fit an attribute value type.
func ConditionOperatorsFor(valueType string) []string {
	return conditionOperators[conditionKind(valueType)]
}

func conditionKind(valueType string) string {
	switch strings.ToLower(strings.TrimSpace(valueType)) {
	case "number", "decimal", "integer", "float", "currency":
		return "number"
	case "select":
		return "select"
	case "date":
		return "date"
	}
	return "text"
}

func conditionEmptyOp(op string) bool { return op == "is empty" || op == "is not empty" }

func productGroupConditions(payload map[string]interface{}) ([]ProductGroupCondition, error) {
	var rows []map[string]interface{}
	if err := decodeProductGroupJSON(payload["filter_conditions"], &rows); err != nil {
		return nil, fmt.Errorf("attribute conditions must be a JSON array of {attribute, operator, value} rows")
	}
	out := make([]ProductGroupCondition, 0, len(rows))
	for _, r := range rows {
		out = append(out, ProductGroupCondition{
			Attribute: strings.TrimSpace(pimString(r["attribute"])),
			Operator:  strings.ToLower(strings.TrimSpace(pimString(r["operator"]))),
			Value:     strings.TrimSpace(pimString(r["value"])),
		})
	}
	return out, nil
}

func conditionErr(msg string) error {
	return templateErr("PIM-0971", "Attribute Conditions", msg)
}

// conditionTarget returns the value kind and select options of the attribute
// or Item field a condition names.
func conditionTarget(tenantID, name string) (kind string, options []string, err error) {
	if db.DB == nil {
		return "text", nil, nil
	}
	schema, serr := db.GetTenantSchema(tenantID)
	if serr != nil {
		return "", nil, serr
	}
	var vt, opts string
	qerr := db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(data->>'value_type','Data'), COALESCE(data->>'value_options','')
		FROM %s.documents WHERE doctype = 'ProductAttributeDef' AND deleted_at IS NULL
		AND (id = $1 OR data->>'code' = $1) LIMIT 1`, schema), name).Scan(&vt, &opts)
	if qerr != nil {
		if conditionItemFields[name] {
			return "text", nil, nil
		}
		return "", nil, conditionErr(fmt.Sprintf("%q is not an attribute or a product field", name))
	}
	kind = conditionKind(vt)
	if kind == "select" {
		for _, o := range strings.Split(opts, ",") {
			if o = strings.TrimSpace(o); o != "" {
				options = append(options, o)
			}
		}
	}
	return kind, options, nil
}

func splitConditionList(v string) []string {
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func splitConditionRange(v string) (string, string, bool) {
	parts := strings.Split(v, "..")
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return "", "", false
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), true
}

func validConditionValue(kind, v string) bool {
	switch kind {
	case "number":
		_, err := strconv.ParseFloat(v, 64)
		return err == nil
	case "date":
		_, err := time.Parse("2006-01-02", v)
		return err == nil
	}
	return true
}

// validateProductGroupConditions refuses a condition whose attribute is
// unknown, whose operator does not fit the attribute's type, or whose value
// cannot be read as that type.
func validateProductGroupConditions(tenantID string, conds []ProductGroupCondition) error {
	for _, c := range conds {
		if c.Attribute == "" || c.Operator == "" {
			return conditionErr("every condition needs an attribute and an operator")
		}
		kind, options, err := conditionTarget(tenantID, c.Attribute)
		if err != nil {
			return err
		}
		if !containsString(conditionOperators[kind], c.Operator) {
			return conditionErr(fmt.Sprintf("%q cannot be used on %s (it takes: %s)", c.Operator, c.Attribute, strings.Join(conditionOperators[kind], ", ")))
		}
		if conditionEmptyOp(c.Operator) {
			continue
		}
		if c.Value == "" {
			return conditionErr(fmt.Sprintf("%s %s needs a value", c.Attribute, c.Operator))
		}
		var values []string
		switch c.Operator {
		case "in":
			values = splitConditionList(c.Value)
		case "between":
			lo, hi, ok := splitConditionRange(c.Value)
			if !ok {
				return conditionErr(fmt.Sprintf("%s between needs a range written as 10..20", c.Attribute))
			}
			values = []string{lo, hi}
		default:
			values = []string{c.Value}
		}
		for _, v := range values {
			if !validConditionValue(kind, v) {
				return conditionErr(fmt.Sprintf("%q is not a valid %s value for %s", v, kind, c.Attribute))
			}
			if kind == "select" && len(options) > 0 && !containsFold(options, v) {
				return conditionErr(fmt.Sprintf("%q is not one of the options of %s", v, c.Attribute))
			}
		}
	}
	return nil
}

// conditionHolds tests one stored value against one condition of a given kind.
func conditionHolds(kind string, c ProductGroupCondition, actual string) bool {
	actual = strings.TrimSpace(actual)
	switch c.Operator {
	case "is empty":
		return actual == ""
	case "is not empty":
		return actual != ""
	}
	if actual == "" {
		return c.Operator == "not equals"
	}
	cmp := func(a, b string) (int, bool) {
		switch kind {
		case "number":
			x, e1 := strconv.ParseFloat(a, 64)
			y, e2 := strconv.ParseFloat(b, 64)
			if e1 != nil || e2 != nil {
				return 0, false
			}
			switch {
			case x < y:
				return -1, true
			case x > y:
				return 1, true
			}
			return 0, true
		case "date":
			return strings.Compare(a, b), true
		}
		return strings.Compare(strings.ToLower(a), strings.ToLower(b)), true
	}
	switch c.Operator {
	case "equals":
		r, ok := cmp(actual, c.Value)
		return ok && r == 0
	case "not equals":
		r, ok := cmp(actual, c.Value)
		return !ok || r != 0
	case "in":
		for _, v := range splitConditionList(c.Value) {
			if r, ok := cmp(actual, v); ok && r == 0 {
				return true
			}
		}
		return false
	case "contains":
		return strings.Contains(strings.ToLower(actual), strings.ToLower(c.Value))
	case "starts with":
		return strings.HasPrefix(strings.ToLower(actual), strings.ToLower(c.Value))
	case "greater than", "after":
		r, ok := cmp(actual, c.Value)
		return ok && r > 0
	case "less than", "before":
		r, ok := cmp(actual, c.Value)
		return ok && r < 0
	case "between":
		lo, hi, ok := splitConditionRange(c.Value)
		if !ok {
			return false
		}
		a, ok1 := cmp(actual, lo)
		b, ok2 := cmp(actual, hi)
		return ok1 && ok2 && a >= 0 && b <= 0
	}
	return false
}

// groupConditionEvaluator caches each condition's kind so a resolution reads
// the attribute definitions once, not once per product.
type groupConditionEvaluator struct {
	tenantID string
	conds    []ProductGroupCondition
	kinds    []string
}

func newGroupConditionEvaluator(tenantID string, conds []ProductGroupCondition) (*groupConditionEvaluator, error) {
	e := &groupConditionEvaluator{tenantID: tenantID, conds: conds}
	for _, c := range conds {
		kind, _, err := conditionTarget(tenantID, c.Attribute)
		if err != nil {
			return nil, err
		}
		e.kinds = append(e.kinds, kind)
	}
	return e, nil
}

func (e *groupConditionEvaluator) matches(item itemRow) (bool, error) {
	for i, c := range e.conds {
		var actual string
		if v, handled := itemFieldValue(item, c.Attribute); handled {
			actual = v
		} else {
			v, err := ResolveAttributeValue(e.tenantID, item.ID, c.Attribute, "en", "")
			if err != nil {
				return false, err
			}
			actual = v
		}
		if !conditionHolds(e.kinds[i], c, actual) {
			return false, nil
		}
	}
	return true, nil
}

// itemFieldValue reads an Item field a candidate row already carries. An
// attribute that happens to share a field's name is read as an attribute only
// when it is not one of the carried fields.
func itemFieldValue(item itemRow, name string) (string, bool) {
	switch name {
	case "name":
		return item.Name, true
	case "family":
		return item.Family, true
	case "status":
		return item.Status, true
	}
	return "", false
}
