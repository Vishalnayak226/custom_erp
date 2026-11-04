package engines

import (
	"custom_erp/db"
	"fmt"
	"strings"
)

// Stage 146.5.10: a task with an attribute checklist. The task (an ordinary
// PIMTask, so it keeps owner, due date, comments and the inbox) lists the
// attributes to fill for a product or a product group. Nothing is stored about
// progress: the checklist is read from the attribute values on every look, and
// the task completes itself on the look that finds every product filled.
// "Filled" means ResolveAttributeValue finds a non-blank value, the same test
// completeness uses (including the design fallback when that setting is on).

// maxChecklistProducts bounds the products one checklist reads.
const maxChecklistProducts = 200

// PIMChecklistRequest makes a checklist task.
type PIMChecklistRequest struct {
	Title      string   `json:"title"`
	ScopeType  string   `json:"scope_type"` // Product | Product Group
	ScopeRef   string   `json:"scope_ref"`
	Attributes []string `json:"attributes"`
	Assignee   string   `json:"assignee"`
	DueDate    string   `json:"due_date"`
	Priority   string   `json:"priority"`
}

// PIMChecklistRow is one product with what is still to fill.
type PIMChecklistRow struct {
	Item    string   `json:"item"`
	Name    string   `json:"name"`
	Missing []string `json:"missing"`
}

// PIMChecklist is the live state of a checklist task.
type PIMChecklist struct {
	TaskID     string            `json:"task_id"`
	Title      string            `json:"title"`
	Status     string            `json:"status"`
	ScopeType  string            `json:"scope_type"`
	ScopeRef   string            `json:"scope_ref"`
	Assignee   string            `json:"assignee"`
	DueDate    string            `json:"due_date"`
	Attributes []string          `json:"attributes"`
	Labels     map[string]string `json:"labels"`
	Products   int               `json:"products"`
	Complete   int               `json:"complete"`
	Truncated  bool              `json:"truncated,omitempty"`
	Rows       []PIMChecklistRow `json:"rows"`
}

// CreatePIMChecklistTask makes the task. The attributes must exist and the
// scope must be a product that exists or a product group.
func CreatePIMChecklistTask(tenantID, actor string, req PIMChecklistRequest) (string, error) {
	var attrs []string
	seen := map[string]bool{}
	for _, a := range req.Attributes {
		if a = strings.TrimSpace(a); a != "" && !seen[a] {
			seen[a] = true
			attrs = append(attrs, a)
		}
	}
	if len(attrs) == 0 {
		return "", templateErr("PIM-0972", "Attributes", "choose at least one attribute to fill")
	}
	for _, a := range attrs {
		if ok, _ := verifyDocumentExists(tenantID, "ProductAttributeDef", a); !ok {
			return "", templateErr("PIM-0972", "Attributes", fmt.Sprintf("attribute %q does not exist", a))
		}
	}
	scope := strings.TrimSpace(req.ScopeType)
	ref := strings.TrimSpace(req.ScopeRef)
	if scope != "Product" && scope != "Product Group" {
		return "", templateErr("PIM-0973", "Scope", "a checklist is for a product or a product group")
	}
	if ref == "" {
		return "", templateErr("PIM-0973", "Scope", "choose the product or product group")
	}
	r := PIMTaskRequest{
		Title: req.Title, TaskType: "Attributes", ScopeType: scope, ScopeRef: ref,
		Assignee: req.Assignee, DueDate: req.DueDate, Priority: req.Priority,
		Checklist: strings.Join(attrs, ","),
	}
	if scope == "Product" {
		r.ItemCode = ref
	} else if _, _, err := fetchPIMProductGroup(tenantID, ref); err != nil {
		return "", templateErr("PIM-0973", "Scope", err.Error())
	}
	return CreatePIMTask(tenantID, actor, r)
}

// PIMTaskChecklistFor reads the checklist of a task and, when every product
// is filled and the task is still open, completes the task.
func PIMTaskChecklistFor(tenantID, actor, taskID string) (*PIMChecklist, error) {
	id, data, err := pimTaskDocument(tenantID, taskID)
	if err != nil {
		return nil, err
	}
	attrs := tagList(pimString(data["checklist_attributes"]))
	if len(attrs) == 0 {
		return nil, templateErr("PIM-0972", "Task", "this task has no attribute checklist")
	}
	cl := &PIMChecklist{
		TaskID: id, Title: pimString(data["title"]), Status: pimString(data["status"]),
		ScopeType: pimString(data["scope_type"]), ScopeRef: pimString(data["scope_ref"]),
		Assignee: pimString(data["assignee"]), DueDate: pimString(data["due_date"]),
		Attributes: attrs, Labels: map[string]string{}, Rows: []PIMChecklistRow{},
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	for _, a := range attrs {
		var label string
		_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(data->>'label','') FROM %s.documents
			WHERE doctype = 'ProductAttributeDef' AND deleted_at IS NULL AND (id = $1 OR data->>'code' = $1) LIMIT 1`, schema), a).Scan(&label)
		cl.Labels[a] = firstNonEmpty(label, a)
	}

	var items []string
	if cl.ScopeType == "Product Group" {
		items, err = ResolvePIMProductGroupItemCodes(tenantID, cl.ScopeRef)
		if err != nil {
			return nil, err
		}
	} else if ref := firstNonEmpty(pimString(data["item_code"]), cl.ScopeRef); ref != "" {
		items = []string{ref}
	}
	if len(items) > maxChecklistProducts {
		items = items[:maxChecklistProducts]
		cl.Truncated = true
	}
	for _, it := range items {
		var name string
		_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(data->>'name','') FROM %s.documents WHERE doctype='Item' AND id=$1`, schema), it).Scan(&name)
		row := PIMChecklistRow{Item: it, Name: name, Missing: []string{}}
		for _, a := range attrs {
			if ok, _ := attributeValueFilled(tenantID, it, a, "", ""); !ok {
				row.Missing = append(row.Missing, a)
			}
		}
		if len(row.Missing) == 0 {
			cl.Complete++
		}
		cl.Rows = append(cl.Rows, row)
	}
	cl.Products = len(items)
	if cl.Products > 0 && cl.Complete == cl.Products && (cl.Status == "Open" || cl.Status == "In Progress") && !cl.Truncated {
		if err := SetPIMTaskStatus(tenantID, actor, id, "Done"); err == nil {
			cl.Status = "Done"
		}
	}
	return cl, nil
}

// ListPIMChecklistTasks lists the checklist tasks, newest first, each with its
// progress. Open ones are read live (which completes the finished ones);
// closed ones are listed as they ended.
func ListPIMChecklistTasks(tenantID, actor string) ([]PIMChecklist, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id FROM %s.documents WHERE doctype = 'PIMTask' AND deleted_at IS NULL
		AND COALESCE(data->>'checklist_attributes','') <> '' ORDER BY created_at DESC, id DESC LIMIT 100`, schema))
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	out := make([]PIMChecklist, 0, len(ids))
	for _, id := range ids {
		cl, cErr := PIMTaskChecklistFor(tenantID, actor, id)
		if cErr != nil {
			continue
		}
		cl.Rows = nil // the list carries counts; the detail carries the rows
		out = append(out, *cl)
	}
	return out, nil
}
