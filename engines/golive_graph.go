package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Stage 47.12.2 - the structure graph: legal entity, tax registration,
// location (store or head office), warehouse and finance dimension (cost
// centre), read from the real masters and checked against the scope answers.
// A person can mark a node out of scope and bring it back; the decision is a
// record with who, when and why, and nothing is deleted or disabled.

// GraphNode is one box on the graph.
type GraphNode struct {
	ID      string   `json:"id"`
	Kind    string   `json:"kind"` // entity, registration, location, warehouse, dimension
	Label   string   `json:"label"`
	State   string   `json:"state"` // ok, issue, out, expected
	Issues  []string `json:"issues"`
	Example string   `json:"example,omitempty"`
	Fix     string   `json:"fix,omitempty"`
	OutBy   string   `json:"out_by,omitempty"`
	OutAt   string   `json:"out_at,omitempty"`
	Reason  string   `json:"reason,omitempty"`
	Doc     string   `json:"doc,omitempty"` // Doctype:id for a record node (can be set out of scope)
}

// GraphEdge joins two nodes: From owns To.
type GraphEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// GoLiveGraph is the whole graph.
type GoLiveGraph struct {
	Nodes  []GraphNode    `json:"nodes"`
	Edges  []GraphEdge    `json:"edges"`
	Counts map[string]int `json:"counts"`
}

var gstinFormat = regexp.MustCompile(`^[0-9]{2}[A-Z]{5}[0-9]{4}[A-Z][1-9A-Z]Z[0-9A-Z]$`)

type graphDoc struct {
	ID, Doctype, Status string
	Data                map[string]interface{}
}

func (d graphDoc) str(k string) string {
	if v, ok := d.Data[k]; ok && v != nil {
		return strings.TrimSpace(fmt.Sprint(v))
	}
	return ""
}

func loadGraphDocs(schema string) ([]graphDoc, error) {
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, doctype, COALESCE(status, ''), data::text FROM %s.documents
		WHERE doctype IN ('LegalEntity', 'Location', 'CostCenter') AND deleted_at IS NULL ORDER BY doctype, id LIMIT 2000`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []graphDoc
	for rows.Next() {
		var d graphDoc
		var raw string
		if err := rows.Scan(&d.ID, &d.Doctype, &d.Status, &raw); err != nil {
			return nil, err
		}
		d.Data = map[string]interface{}{}
		_ = json.Unmarshal([]byte(raw), &d.Data)
		out = append(out, d)
	}
	return out, rows.Err()
}

func graphInactive(d graphDoc) bool {
	s := d.Status
	if s == "" {
		s = d.str("status")
	}
	return s == "Inactive" || s == "Cancelled" || s == "Archived"
}

func graphNodeID(doctype, id string) string { return doctype + ":" + id }

// GetGoLiveGraph reads the masters and the scope answers into the graph.
func GetGoLiveGraph(tenantID string) (*GoLiveGraph, error) {
	c, err := newGoLiveCtx(tenantID)
	if err != nil {
		return nil, err
	}
	docs, err := loadGraphDocs(c.schema)
	if err != nil {
		return nil, err
	}
	g := &GoLiveGraph{Nodes: []GraphNode{}, Edges: []GraphEdge{}, Counts: map[string]int{}}
	add := func(n GraphNode) {
		if n.Issues == nil {
			n.Issues = []string{}
		}
		if n.Doc != "" {
			if r := c.latest("node:"+n.Doc, "NodeOut", "NodeIn"); r != nil && r.Kind == "NodeOut" {
				n.State, n.OutBy, n.OutAt, n.Reason = "out", r.By, r.At, r.Note
			}
		}
		if n.State == "" {
			n.State = "ok"
			if len(n.Issues) > 0 {
				n.State = "issue"
			}
		}
		g.Nodes = append(g.Nodes, n)
	}
	isOut := func(doc string) bool {
		r := c.latest("node:"+doc, "NodeOut", "NodeIn")
		return r != nil && r.Kind == "NodeOut"
	}
	scopeSaved := c.scope != nil && c.scope.Saved
	byID := map[string]graphDoc{}
	var entities, locations, dims []graphDoc
	for _, d := range docs {
		byID[d.Doctype+":"+d.ID] = d
		switch d.Doctype {
		case "LegalEntity":
			entities = append(entities, d)
		case "Location":
			locations = append(locations, d)
		case "CostCenter":
			dims = append(dims, d)
		}
	}
	wantReg := !scopeSaved || !c.scope.Answers.Has("registrations", "none")
	wantWarehouse := scopeSaved && c.scope.Answers.Has("locations", "warehouses")
	wantStore := scopeSaved && c.scope.Answers.Has("locations", "stores")
	oneEntity := scopeSaved && c.scope.Answers.Has("entities", "one")

	// legal entities and their registrations
	for _, e := range entities {
		id := graphNodeID("LegalEntity", e.ID)
		n := GraphNode{ID: id, Kind: "entity", Label: firstNonEmptyStr(e.str("name"), e.str("legal_name"), e.ID), Doc: "LegalEntity:" + e.ID,
			Fix: "doctype:LegalEntity", Example: "Head Office Pvt Ltd"}
		if graphInactive(e) {
			n.Issues = append(n.Issues, "Inactive")
		}
		add(n)
		gstin := strings.ToUpper(e.str("gstin"))
		switch {
		case gstin != "":
			rn := GraphNode{ID: "Registration:" + e.ID, Kind: "registration", Label: gstin, Fix: "doctype:LegalEntity", Example: "27ABCDE1234F1Z5"}
			if !gstinFormat.MatchString(gstin) {
				rn.Issues = append(rn.Issues, "Not a valid GSTIN")
			}
			if isOut(n.Doc) {
				rn.State = "out"
			}
			add(rn)
			g.Edges = append(g.Edges, GraphEdge{From: id, To: rn.ID})
		case wantReg && !isOut(n.Doc) && !graphInactive(e):
			add(GraphNode{ID: "Registration:" + e.ID, Kind: "registration", Label: "GSTIN missing", State: "expected", Fix: "doctype:LegalEntity", Example: "27ABCDE1234F1Z5",
				Issues: []string{"This entity has no tax registration"}})
			g.Edges = append(g.Edges, GraphEdge{From: id, To: "Registration:" + e.ID})
		}
	}
	if len(entities) == 0 {
		add(GraphNode{ID: "Expected:entity", Kind: "entity", Label: "Legal entity", State: "expected", Fix: "doctype:LegalEntity", Example: "Head Office Pvt Ltd",
			Issues: []string{"No legal entity yet"}})
	}
	if oneEntity && len(entities) > 1 {
		for i := range g.Nodes {
			if g.Nodes[i].Kind == "entity" && g.Nodes[i].State != "out" {
				g.Nodes[i].Issues = append(g.Nodes[i].Issues, "The scope says one entity")
				g.Nodes[i].State = "issue"
			}
		}
	}

	// finance dimensions
	for _, d := range dims {
		n := GraphNode{ID: graphNodeID("CostCenter", d.ID), Kind: "dimension", Label: firstNonEmptyStr(d.str("name"), d.ID), Doc: "CostCenter:" + d.ID,
			Fix: "doctype:CostCenter", Example: "Retail"}
		if graphInactive(d) {
			n.Issues = append(n.Issues, "Inactive")
		}
		add(n)
	}
	if len(dims) == 0 {
		add(GraphNode{ID: "Expected:dimension", Kind: "dimension", Label: "Cost centre", State: "expected", Fix: "doctype:CostCenter", Example: "Retail",
			Issues: []string{"No finance dimension yet"}})
	}

	// locations and warehouses
	stores, warehouses := 0, 0
	for _, l := range locations {
		kind := "location"
		if l.str("type") == "Warehouse" {
			kind = "warehouse"
			warehouses++
		} else {
			stores++
		}
		id := graphNodeID("Location", l.ID)
		n := GraphNode{ID: id, Kind: kind, Label: firstNonEmptyStr(l.str("name"), l.ID), Doc: "Location:" + l.ID, Fix: "doctype:Location", Example: "Main store"}
		if graphInactive(l) {
			n.Issues = append(n.Issues, "Inactive")
		}
		le := l.str("legal_entity")
		if le != "" {
			if _, ok := byID["LegalEntity:"+le]; ok {
				g.Edges = append(g.Edges, GraphEdge{From: graphNodeID("LegalEntity", le), To: id})
			} else {
				n.Issues = append(n.Issues, "Legal entity "+le+" not found")
			}
		} else if len(entities) > 0 {
			n.Issues = append(n.Issues, "Not under a legal entity")
		}
		if cc := l.str("cost_center"); cc != "" {
			if _, ok := byID["CostCenter:"+cc]; ok {
				g.Edges = append(g.Edges, GraphEdge{From: id, To: graphNodeID("CostCenter", cc)})
			} else {
				n.Issues = append(n.Issues, "Cost centre "+cc+" not found")
			}
		} else {
			n.Issues = append(n.Issues, "No finance dimension")
		}
		add(n)
	}
	if wantStore && stores == 0 {
		add(GraphNode{ID: "Expected:location", Kind: "location", Label: "Store", State: "expected", Fix: "doctype:Location", Example: "Main store",
			Issues: []string{"The scope has stores; none exist"}})
	}
	if wantWarehouse && warehouses == 0 {
		add(GraphNode{ID: "Expected:warehouse", Kind: "warehouse", Label: "Warehouse", State: "expected", Fix: "doctype:Location", Example: "Central warehouse",
			Issues: []string{"The scope has warehouses; none exist"}})
	}
	sort.SliceStable(g.Edges, func(i, j int) bool { return g.Edges[i].From+g.Edges[i].To < g.Edges[j].From+g.Edges[j].To })
	for _, n := range g.Nodes { // counted last: a later check can change a node's state
		g.Counts[n.State]++
	}
	return g, nil
}

func firstNonEmptyStr(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

// SetGoLiveNodeScope marks one master out of scope (with a reason) or brings it
// back. It only records the decision; the master itself is left as it is.
func SetGoLiveNodeScope(tenantID, doc string, out bool, reason, actor string) error {
	parts := strings.SplitN(doc, ":", 2)
	if len(parts) != 2 || (parts[0] != "LegalEntity" && parts[0] != "Location" && parts[0] != "CostCenter") {
		return goLiveErr("GLOBAL-9116", "That box cannot be set out of scope.")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var n int
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.documents WHERE doctype = $1 AND id = $2 AND deleted_at IS NULL`, schema), parts[0], parts[1]).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return goLiveErr("GLOBAL-9116", "That record does not exist.")
	}
	kind := "NodeIn"
	reason = strings.TrimSpace(reason)
	if out {
		kind = "NodeOut"
		if len(reason) < 5 || len(reason) > 1000 {
			return goLiveErr("GLOBAL-9116", "Give the reason, in a few words to 1000 characters.")
		}
	}
	return insertGoLiveRecord(schema, kind, "scope", "node:"+doc, reason, "", "", nil, actor)
}
