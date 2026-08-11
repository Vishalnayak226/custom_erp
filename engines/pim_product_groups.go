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

// ProductGroupFilterParams deliberately uses the same flat key/value shape as
// ReportFilterPreset.params. The stored fields are typed metadata fields (two
// Links, a Number and a Select), so the generic form stays usable and saved
// groups cannot smuggle SQL or grow a second ad-hoc filter language.
type ProductGroupFilterParams map[string]string

type ProductGroupMember struct {
	ItemCode     string   `json:"item_code"`
	Name         string   `json:"name"`
	Family       string   `json:"family"`
	Status       string   `json:"status"`
	Completeness float64  `json:"completeness"`
	Missing      []string `json:"missing_fields"`
}

type ProductGroupResolution struct {
	GroupID     string                   `json:"group_id"`
	Name        string                   `json:"name"`
	GroupType   string                   `json:"group_type"`
	Filters     ProductGroupFilterParams `json:"filters,omitempty"`
	Members     []ProductGroupMember     `json:"members"`
	MemberCount int                      `json:"member_count"`
	ResolvedAt  time.Time                `json:"resolved_at"`
}

type staticProductGroupRow struct {
	ItemCode string `json:"item_code"`
}

func decodeProductGroupJSON(raw interface{}, out interface{}) error {
	if raw == nil || strings.TrimSpace(fmt.Sprintf("%v", raw)) == "" {
		return nil
	}
	if s, ok := raw.(string); ok {
		return json.Unmarshal([]byte(s), out)
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

func productGroupMembers(payload map[string]interface{}) ([]staticProductGroupRow, error) {
	var rows []staticProductGroupRow
	if err := decodeProductGroupJSON(payload["members"], &rows); err != nil {
		return nil, fmt.Errorf("members must be a JSON array of Item links: %w", err)
	}
	return rows, nil
}

func productGroupFilters(payload map[string]interface{}) (ProductGroupFilterParams, error) {
	out := ProductGroupFilterParams{}
	fields := map[string]string{
		"family":             "filter_family",
		"completeness_below": "filter_completeness_below",
		"missing_attribute":  "filter_missing_attribute",
		"status":             "filter_status",
	}
	for param, field := range fields {
		value := strings.TrimSpace(fmt.Sprintf("%v", payload[field]))
		if payload[field] != nil && value != "" && value != "<nil>" {
			out[param] = value
		}
	}
	return out, nil
}

// ValidatePIMProductGroupDocument is called by ValidateDocument, which means
// the generic form, API writes, CSV imports and future callers all share the
// same group rules.
func ValidatePIMProductGroupDocument(tenantID string, payload map[string]interface{}) error {
	groupType := strings.TrimSpace(fmt.Sprintf("%v", payload["group_type"]))
	members, err := productGroupMembers(payload)
	if err != nil {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "Static Products", Message: err.Error()}
	}
	filters, err := productGroupFilters(payload)
	if err != nil {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "Dynamic Filters", Message: err.Error()}
	}

	switch groupType {
	case "Static":
		if len(filters) > 0 {
			return &ValidationError{Code: "GLOBAL-0002", SubFor: "Dynamic Filters", Message: "a Static product group cannot also carry dynamic filters"}
		}
		seen := map[string]bool{}
		for _, member := range members {
			itemCode := strings.TrimSpace(member.ItemCode)
			if itemCode == "" {
				return &ValidationError{Code: "GLOBAL-0001", SubFor: "Static Products", Message: "every static product-group line needs an Item"}
			}
			if seen[itemCode] {
				return &ValidationError{Code: "GLOBAL-0002", SubFor: "Static Products", Message: fmt.Sprintf("Item %q appears more than once in this group", itemCode)}
			}
			seen[itemCode] = true
			if db.DB != nil {
				exists, lookupErr := verifyDocumentExists(tenantID, "Item", itemCode)
				if lookupErr != nil {
					return lookupErr
				}
				if !exists {
					return &ValidationError{Code: "META-0198", SubFor: "Static Products", Message: fmt.Sprintf("linked Item record %q does not exist", itemCode)}
				}
			}
		}
	case "Dynamic":
		if len(members) > 0 {
			return &ValidationError{Code: "GLOBAL-0002", SubFor: "Static Products", Message: "a Dynamic product group cannot also carry static products"}
		}
		if len(filters) == 0 {
			return &ValidationError{Code: "GLOBAL-0001", SubFor: "Dynamic Filters", Message: "a Dynamic product group needs at least one filter"}
		}
	default:
		return &ValidationError{Code: "META-0199", SubFor: "Group Type", Message: "group_type must be Static or Dynamic"}
	}

	if threshold := filters["completeness_below"]; threshold != "" {
		value, parseErr := strconv.ParseFloat(threshold, 64)
		if parseErr != nil || value < 0 || value > 100 {
			return &ValidationError{Code: "GLOBAL-0002", SubFor: "Dynamic Filters", Message: "completeness_below must be a number from 0 to 100"}
		}
	}
	if status := filters["status"]; status != "" && status != "Active" && status != "Inactive" {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "Dynamic Filters", Message: "status filter must be Active or Inactive"}
	}
	return nil
}

func fetchPIMProductGroup(tenantID, groupID string) (string, map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", nil, err
	}
	var canonicalID, raw, status string
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT id, data, status FROM %s.documents
		WHERE doctype = 'PIMProductGroup' AND (id = $1 OR UPPER(data->>'code') = UPPER($1)) AND deleted_at IS NULL
		ORDER BY CASE WHEN id = $1 THEN 0 ELSE 1 END, id LIMIT 1`, schema), groupID).Scan(&canonicalID, &raw, &status)
	if err != nil {
		return "", nil, fmt.Errorf("product group %q not found", groupID)
	}
	if status != "Active" {
		return "", nil, fmt.Errorf("product group %q is not active", groupID)
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return "", nil, fmt.Errorf("product group %q has invalid stored data: %w", groupID, err)
	}
	return canonicalID, data, nil
}

func productGroupCandidateItems(tenantID string) ([]itemRow, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, COALESCE(data->>'name',''),
		COALESCE(data->>'family',''), status FROM %s.documents
		WHERE doctype = 'Item' AND deleted_at IS NULL AND status <> 'Cancelled'
		ORDER BY id`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []itemRow
	for rows.Next() {
		var item itemRow
		if err := rows.Scan(&item.ID, &item.Name, &item.Family, &item.Status); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func matchesDynamicProductGroup(tenantID string, item itemRow, completeness *CompletenessResult, filters ProductGroupFilterParams) (bool, error) {
	if family := filters["family"]; family != "" && item.Family != family {
		return false, nil
	}
	if status := filters["status"]; status != "" && item.Status != status {
		return false, nil
	}
	if threshold := filters["completeness_below"]; threshold != "" {
		value, _ := strconv.ParseFloat(threshold, 64) // validated on save/read
		if completeness.Score >= value {
			return false, nil
		}
	}
	if attribute := filters["missing_attribute"]; attribute != "" {
		value, err := ResolveAttributeValue(tenantID, item.ID, attribute, "en", "")
		if err != nil {
			return false, err
		}
		if strings.TrimSpace(value) != "" {
			return false, nil
		}
	}
	return true, nil
}

// ResolvePIMProductGroup re-evaluates a dynamic group on every call. Nothing
// is materialised, so a product enters/leaves as soon as its family,
// completeness, status or selected attribute changes.
func ResolvePIMProductGroup(tenantID, groupID string) (*ProductGroupResolution, error) {
	canonicalID, group, err := fetchPIMProductGroup(tenantID, groupID)
	if err != nil {
		return nil, err
	}
	if err := ValidatePIMProductGroupDocument(tenantID, group); err != nil {
		return nil, fmt.Errorf("product group %q is invalid: %w", groupID, err)
	}
	groupType := strings.TrimSpace(fmt.Sprintf("%v", group["group_type"]))
	filters, _ := productGroupFilters(group)
	staticRows, _ := productGroupMembers(group)
	staticSet := map[string]bool{}
	for _, row := range staticRows {
		staticSet[strings.TrimSpace(row.ItemCode)] = true
	}

	candidates, err := productGroupCandidateItems(tenantID)
	if err != nil {
		return nil, err
	}
	result := &ProductGroupResolution{
		GroupID: canonicalID, Name: strings.TrimSpace(fmt.Sprintf("%v", group["name"])),
		GroupType: groupType, Filters: filters, Members: []ProductGroupMember{}, ResolvedAt: time.Now().UTC(),
	}
	for _, item := range candidates {
		if groupType == "Static" && !staticSet[item.ID] {
			continue
		}
		completeness, scoreErr := CalculateCompleteness(tenantID, item.ID, "en", "")
		if scoreErr != nil {
			return nil, fmt.Errorf("score Item %q for product group %q: %w", item.ID, groupID, scoreErr)
		}
		if groupType == "Dynamic" {
			matches, matchErr := matchesDynamicProductGroup(tenantID, item, completeness, filters)
			if matchErr != nil {
				return nil, matchErr
			}
			if !matches {
				continue
			}
		}
		result.Members = append(result.Members, ProductGroupMember{
			ItemCode: item.ID, Name: item.Name, Family: item.Family, Status: item.Status,
			Completeness: completeness.Score, Missing: completeness.MissingFields,
		})
	}
	sort.Slice(result.Members, func(i, j int) bool { return result.Members[i].ItemCode < result.Members[j].ItemCode })
	result.MemberCount = len(result.Members)
	return result, nil
}

// ResolvePIMProductGroupItemCodes is the stable downstream seam for 36.1.3:
// bulk actions, tasks, readiness reporting and exports can all consume one
// resolver instead of reimplementing group semantics.
func ResolvePIMProductGroupItemCodes(tenantID, groupID string) ([]string, error) {
	resolved, err := ResolvePIMProductGroup(tenantID, groupID)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(resolved.Members))
	for _, member := range resolved.Members {
		ids = append(ids, member.ItemCode)
	}
	return ids, nil
}
