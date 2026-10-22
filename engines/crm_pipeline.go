package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Stage 101.6 - the business-buyer pipeline (the blueprint's M29, M30; D1:
// built last). Leads come from a source, go to an owner by the client's
// assignment rules (or wait in the unassigned queue) and convert once into a
// Customer and an Opportunity, however often "Convert" is pressed. An
// opportunity moves across stages that can require things before it enters
// (a value, a close date, a quotation); winning needs an approved Sales
// Quotation and raises exactly one Sales Order; losing needs a reason. Every
// change is a history row - nothing is overwritten. The forecast keeps the
// weighted pipeline, what is committed and what is booked apart, and a
// snapshot, once taken, never changes.

// CRMPipelineStage is one column of the board.
type CRMPipelineStage struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Position    int      `json:"position"`
	Probability int      `json:"probability"`
	Kind        string   `json:"kind"` // open, won, lost
	Requires    []string `json:"requires"`
	Committed   bool     `json:"committed"`
	Active      bool     `json:"active"`
}

var crmDefaultStages = []CRMPipelineStage{
	{Name: "New", Probability: 10, Kind: "open"},
	{Name: "Qualified", Probability: 25, Kind: "open", Requires: []string{"value"}},
	{Name: "Proposal", Probability: 50, Kind: "open", Requires: []string{"value", "expected_close"}},
	{Name: "Negotiation", Probability: 75, Kind: "open", Requires: []string{"value", "expected_close", "quotation"}, Committed: true},
	{Name: "Won", Probability: 100, Kind: "won"},
	{Name: "Lost", Probability: 0, Kind: "lost"},
}

// ListCRMPipelineStages returns the stages, creating the defaults once.
func ListCRMPipelineStages(tenantID string) ([]CRMPipelineStage, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	return crmStages(schema)
}

func crmStages(schema string) ([]CRMPipelineStage, error) {
	var n int
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.crm_pipeline_stages`, schema)).Scan(&n); err != nil {
		return nil, err
	}
	if n == 0 {
		for i, s := range crmDefaultStages {
			if s.Requires == nil {
				s.Requires = []string{}
			}
			_, _ = db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.crm_pipeline_stages (id, name, position, probability, kind, requires, committed) VALUES ($1, $2, $3, $4, $5, $6, $7)
				ON CONFLICT DO NOTHING`, schema), fmt.Sprintf("STG-%d", i+1), s.Name, i+1, s.Probability, s.Kind, crmJSON(s.Requires), s.Committed)
		}
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, name, position, probability, kind, requires, committed, active FROM %s.crm_pipeline_stages ORDER BY position`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CRMPipelineStage{}
	for rows.Next() {
		var s CRMPipelineStage
		var req []byte
		if err := rows.Scan(&s.ID, &s.Name, &s.Position, &s.Probability, &s.Kind, &req, &s.Committed, &s.Active); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(req, &s.Requires)
		if s.Requires == nil {
			s.Requires = []string{}
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// SaveCRMPipelineStages replaces the open stages' names, chances and rules
// (Won and Lost always exist; a stage with opportunities in it is kept).
func SaveCRMPipelineStages(tenantID string, stages []CRMPipelineStage) ([]CRMPipelineStage, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	if _, err := crmStages(schema); err != nil {
		return nil, err
	}
	allowed := map[string]bool{"value": true, "expected_close": true, "quotation": true, "owner": true}
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	for i, s := range stages {
		s.Name = strings.TrimSpace(s.Name)
		if s.Name == "" || s.Probability < 0 || s.Probability > 100 {
			return nil, crmErr(CRMCodePipeline, "each stage needs a name and a chance from 0 to 100")
		}
		var req []string
		for _, r := range s.Requires {
			if allowed[r] {
				req = append(req, r)
			}
		}
		if req == nil {
			req = []string{}
		}
		if s.ID == "" {
			s.ID = NewDocID("STG")
		}
		if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.crm_pipeline_stages (id, name, position, probability, kind, requires, committed, active) VALUES ($1, $2, $3, $4, 'open', $5, $6, $7)
			ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, position = EXCLUDED.position, probability = EXCLUDED.probability, requires = EXCLUDED.requires,
			committed = EXCLUDED.committed, active = EXCLUDED.active WHERE %s.crm_pipeline_stages.kind = 'open'`, schema, schema),
			s.ID, s.Name, i+1, s.Probability, crmJSON(req), s.Committed, s.Active || s.ID == ""); err != nil {
			return nil, err
		}
	}
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.crm_pipeline_stages SET position = 1000 + position WHERE kind <> 'open'`, schema)); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return crmStages(schema)
}

// --- leads -------------------------------------------------------------------------

// CRMLead is a business buyer not yet a customer.
type CRMLead struct {
	ID               string     `json:"id"`
	Name             string     `json:"name"`
	Company          string     `json:"company,omitempty"`
	Phone            string     `json:"phone,omitempty"`
	Email            string     `json:"email,omitempty"`
	Source           string     `json:"source"`
	Owner            string     `json:"owner,omitempty"`
	Status           string     `json:"status"`
	Notes            string     `json:"notes,omitempty"`
	CustomerID       string     `json:"customer_id,omitempty"`
	OpportunityID    string     `json:"opportunity_id,omitempty"`
	DisqualifyReason string     `json:"disqualify_reason,omitempty"`
	CreatedBy        string     `json:"created_by,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	ConvertedAt      *time.Time `json:"converted_at,omitempty"`
}

// CRMLeadSources are where leads come from.
var CRMLeadSources = []string{"Walk-in", "Phone", "Website", "Referral", "Exhibition", "Marketplace", "Social", "Other"}

// SaveCRMLead creates (id "") or edits a lead. A new lead with no owner goes
// to the owner the assignment rules give its source, else to the
// unassigned queue.
func SaveCRMLead(tenantID, id string, in CRMLead, actor string) (*CRMLead, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return nil, crmErr(CRMCodePipeline, "a lead needs a name")
	}
	if strings.TrimSpace(in.Phone) == "" && strings.TrimSpace(in.Email) == "" {
		return nil, crmErr(CRMCodePipeline, "a lead needs a phone number or an email")
	}
	if p := strings.TrimSpace(in.Phone); p != "" {
		ph := NormalizePhone(p, TenantPhoneCountry(tenantID))
		if !ph.Valid {
			return nil, crmErr(CRMCodePipeline, "the phone number is not valid: %s", ph.Reason)
		}
		in.Phone = ph.National
	}
	src := "Other"
	for _, s := range CRMLeadSources {
		if strings.EqualFold(s, strings.TrimSpace(in.Source)) {
			src = s
		}
	}
	if id == "" {
		if blocked, _ := CRMErasedContactBlocked(tenantID, in.Phone, in.Email); blocked {
			return nil, crmErr(CRMCodeErased, "this phone or email belonged to a customer who asked to be erased")
		}
		owner := strings.TrimSpace(in.Owner)
		if owner == "" {
			owner = crmAssignLeadOwner(schema, src)
		}
		id = NewDocID("LEAD")
		_, err = db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.crm_leads (id, name, company, phone, email, source, owner, notes, created_by)
			VALUES ($1, $2, NULLIF($3, ''), NULLIF($4, ''), NULLIF($5, ''), $6, NULLIF($7, ''), NULLIF($8, ''), $9)`, schema),
			id, in.Name, strings.TrimSpace(in.Company), in.Phone, strings.TrimSpace(in.Email), src, owner, strings.TrimSpace(in.Notes), actor)
	} else {
		var res sql.Result
		res, err = db.DB.Exec(fmt.Sprintf(`UPDATE %s.crm_leads SET name = $2, company = NULLIF($3, ''), phone = NULLIF($4, ''), email = NULLIF($5, ''), source = $6,
			owner = NULLIF($7, ''), notes = NULLIF($8, ''), updated_at = NOW() WHERE id = $1 AND status <> 'Converted'`, schema),
			id, in.Name, strings.TrimSpace(in.Company), in.Phone, strings.TrimSpace(in.Email), src, strings.TrimSpace(in.Owner), strings.TrimSpace(in.Notes))
		if err == nil {
			if n, _ := res.RowsAffected(); n == 0 {
				return nil, crmErr(CRMCodePipeline, "a converted lead cannot be changed - edit the customer or the opportunity")
			}
		}
	}
	if err != nil {
		return nil, err
	}
	return GetCRMLead(tenantID, id)
}

// crmAssignLeadOwner picks, among the rules for a source, the owner with the
// fewest open leads (ties by rule order); "" leaves it unassigned.
func crmAssignLeadOwner(schema, source string) string {
	var owner string
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT r.owner FROM %s.crm_lead_rules r
		WHERE r.source IN ($1, 'Any')
		ORDER BY (SELECT COUNT(*) FROM %s.crm_leads l WHERE l.owner = r.owner AND l.status IN ('New', 'Contacted', 'Qualified')), r.position LIMIT 1`, schema, schema), source).Scan(&owner)
	return owner
}

// CRMLeadRule sends a source's leads to an owner.
type CRMLeadRule struct {
	ID       int64  `json:"id"`
	Source   string `json:"source"`
	Owner    string `json:"owner"`
	Position int    `json:"position"`
}

// ListCRMLeadRules / SaveCRMLeadRules read and replace the assignment rules.
func ListCRMLeadRules(tenantID string) ([]CRMLeadRule, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, source, owner, position FROM %s.crm_lead_rules ORDER BY position, id`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CRMLeadRule{}
	for rows.Next() {
		var r CRMLeadRule
		if err := rows.Scan(&r.ID, &r.Source, &r.Owner, &r.Position); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func SaveCRMLeadRules(tenantID string, rules []CRMLeadRule) ([]CRMLeadRule, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(fmt.Sprintf(`DELETE FROM %s.crm_lead_rules`, schema)); err != nil {
		return nil, err
	}
	for i, r := range rules {
		if strings.TrimSpace(r.Owner) == "" {
			continue
		}
		if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.crm_lead_rules (source, owner, position) VALUES ($1, $2, $3)`, schema), firstNonEmpty(strings.TrimSpace(r.Source), "Any"), strings.TrimSpace(r.Owner), i+1); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return ListCRMLeadRules(tenantID)
}

const crmLeadSelect = `SELECT id, name, COALESCE(company, ''), COALESCE(phone, ''), COALESCE(email, ''), source, COALESCE(owner, ''), status, COALESCE(notes, ''),
	COALESCE(customer_id, ''), COALESCE(opportunity_id, ''), COALESCE(disqualify_reason, ''), COALESCE(created_by, ''), created_at, converted_at FROM %s.crm_leads`

func crmScanLead(r interface{ Scan(...interface{}) error }) (CRMLead, error) {
	var l CRMLead
	var conv sql.NullTime
	err := r.Scan(&l.ID, &l.Name, &l.Company, &l.Phone, &l.Email, &l.Source, &l.Owner, &l.Status, &l.Notes, &l.CustomerID, &l.OpportunityID, &l.DisqualifyReason, &l.CreatedBy, &l.CreatedAt, &conv)
	if conv.Valid {
		l.ConvertedAt = &conv.Time
	}
	return l, err
}

// GetCRMLead returns one lead.
func GetCRMLead(tenantID, id string) (*CRMLead, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	l, err := crmScanLead(db.DB.QueryRow(fmt.Sprintf(crmLeadSelect+` WHERE id = $1`, schema), id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, crmNotFound("lead", id)
	}
	return &l, err
}

// ListCRMLeads lists leads: "unassigned", an owner's, or by status.
func ListCRMLeads(tenantID string, f map[string]string) ([]CRMLead, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	where := []string{"TRUE"}
	args := []interface{}{}
	if f["unassigned"] == "1" {
		where = append(where, "owner IS NULL AND status IN ('New', 'Contacted', 'Qualified')")
	}
	if v := f["owner"]; v != "" {
		args = append(args, v)
		where = append(where, fmt.Sprintf("owner = $%d", len(args)))
	}
	switch v := f["status"]; v {
	case "", "open":
		where = append(where, "status IN ('New', 'Contacted', 'Qualified')")
	case "all":
	default:
		args = append(args, v)
		where = append(where, fmt.Sprintf("status = $%d", len(args)))
	}
	rows, err := db.DB.Query(fmt.Sprintf(crmLeadSelect+` WHERE `+strings.Join(where, " AND ")+` ORDER BY created_at DESC LIMIT 300`, schema), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CRMLead{}
	for rows.Next() {
		l, err := crmScanLead(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// SetCRMLeadStatus moves a lead to Contacted / Qualified, or Disqualified
// with a reason.
func SetCRMLeadStatus(tenantID, id, status, reason, actor string) (*CRMLead, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	if !map[string]bool{"New": true, "Contacted": true, "Qualified": true, "Disqualified": true}[status] {
		return nil, crmErr(CRMCodePipeline, "a lead is New, Contacted, Qualified or Disqualified (Convert makes it a customer)")
	}
	if status == "Disqualified" && strings.TrimSpace(reason) == "" {
		return nil, crmErr(CRMCodePipeline, "say why the lead is disqualified")
	}
	res, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.crm_leads SET status = $2, disqualify_reason = NULLIF($3, ''), updated_at = NOW() WHERE id = $1 AND status <> 'Converted'`, schema),
		id, status, strings.TrimSpace(reason))
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, crmErr(CRMCodePipeline, "a converted lead cannot change")
	}
	return GetCRMLead(tenantID, id)
}

// ConvertCRMLead makes the lead a Customer and an Opportunity - once: a
// second press (a double click, a retry) returns the same two.
func ConvertCRMLead(tenantID, id, title string, value float64, expectedClose, actor string) (*CRMLead, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var status, name, company, phone, email, owner string
	var cust sql.NullString
	err = tx.QueryRow(fmt.Sprintf(`SELECT status, name, COALESCE(company, ''), COALESCE(phone, ''), COALESCE(email, ''), COALESCE(owner, ''), customer_id FROM %s.crm_leads WHERE id = $1 FOR UPDATE`, schema), id).
		Scan(&status, &name, &company, &phone, &email, &owner, &cust)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, crmNotFound("lead", id)
	}
	if err != nil {
		return nil, err
	}
	if status == "Converted" {
		tx.Rollback()
		return GetCRMLead(tenantID, id)
	}
	if status == "Disqualified" {
		return nil, crmErr(CRMCodePipeline, "a disqualified lead cannot be converted - set it back to Qualified first")
	}
	customerID := cust.String
	if customerID == "" && phone != "" {
		if ref, found, _, err := LookupCustomerByPhone(tenantID, phone); err == nil && found {
			customerID = ref.ID
		}
	}
	if customerID == "" {
		data := map[string]interface{}{"name": firstNonEmpty(company, name), "phone": phone, "email": email, "status": "Active",
			"profile_sources": map[string]interface{}{"name": map[string]interface{}{"source": "lead", "by": actor, "at": time.Now().Format("2006-01-02 15:04")}}}
		if company != "" {
			data["contact_person"] = name
		}
		if err := PrepareMasterNumber(tenantID, "", "Customer", true, data); err != nil {
			return nil, err
		}
		customerID = firstNonEmpty(strField(data, "id"), strField(data, "code"))
		if customerID == "" {
			customerID = NewDocID("CUST")
			data["code"] = customerID
		}
		if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'Customer', $2, 'Active', $3)`, schema), customerID, crmJSON(data), actor); err != nil {
			return nil, err
		}
	}
	stages, err := crmStages(schema)
	if err != nil {
		return nil, err
	}
	first := stages[0]
	for _, s := range stages {
		if s.Kind == "open" && s.Active {
			first = s
			break
		}
	}
	oppID := NewDocID("OPP")
	var closeDate interface{}
	if strings.TrimSpace(expectedClose) != "" {
		if _, err := time.Parse("2006-01-02", expectedClose); err != nil {
			return nil, crmErr(CRMCodePipeline, "the expected close is a date (YYYY-MM-DD)")
		}
		closeDate = expectedClose
	}
	if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.crm_opportunities (id, title, customer_id, lead_id, owner, stage_id, value, probability, expected_close, created_by)
		VALUES ($1, $2, $3, $4, NULLIF($5, ''), $6, $7, $8, $9, $10)`, schema),
		oppID, firstNonEmpty(strings.TrimSpace(title), firstNonEmpty(company, name)+" - first order"), customerID, id, owner, first.ID, value, first.Probability, closeDate, actor); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.crm_opportunity_history (opportunity_id, field, from_value, to_value, changed_by) VALUES ($1, 'created', NULL, $2, $3)`, schema),
		oppID, "from lead "+id, actor); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.crm_leads SET status = 'Converted', customer_id = $2, opportunity_id = $3, converted_at = NOW(), updated_at = NOW() WHERE id = $1`, schema),
		id, customerID, oppID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return GetCRMLead(tenantID, id)
}

// --- opportunities ------------------------------------------------------------------

// CRMOpportunity is one deal.
type CRMOpportunity struct {
	ID              string              `json:"id"`
	Title           string              `json:"title"`
	CustomerID      string              `json:"customer_id"`
	CustomerName    string              `json:"customer_name"`
	LeadID          string              `json:"lead_id,omitempty"`
	Owner           string              `json:"owner,omitempty"`
	StageID         string              `json:"stage_id"`
	Stage           string              `json:"stage"`
	Value           float64             `json:"value"`
	Probability     int                 `json:"probability"`
	Weighted        float64             `json:"weighted"`
	ExpectedClose   string              `json:"expected_close,omitempty"`
	Status          string              `json:"status"`
	LostReason      string              `json:"lost_reason,omitempty"`
	QuotationID     string              `json:"quotation_id,omitempty"`
	QuotationStatus string              `json:"quotation_status,omitempty"`
	SalesOrderID    string              `json:"sales_order_id,omitempty"`
	CreatedAt       time.Time           `json:"created_at"`
	ClosedAt        *time.Time          `json:"closed_at,omitempty"`
	History         []CRMOppHistoryItem `json:"history,omitempty"`
}

// CRMOppHistoryItem is one recorded change.
type CRMOppHistoryItem struct {
	Field     string    `json:"field"`
	From      string    `json:"from,omitempty"`
	To        string    `json:"to,omitempty"`
	ChangedBy string    `json:"changed_by"`
	ChangedAt time.Time `json:"changed_at"`
}

func crmOppSelect(schema string) string {
	return fmt.Sprintf(`SELECT o.id, o.title, o.customer_id, COALESCE(c.data->>'name', ''), COALESCE(o.lead_id, ''), COALESCE(o.owner, ''), o.stage_id, COALESCE(s.name, ''),
		o.value, o.probability, COALESCE(to_char(o.expected_close, 'YYYY-MM-DD'), ''), o.status, COALESCE(o.lost_reason, ''), COALESCE(o.quotation_id, ''),
		COALESCE(q.status, ''), COALESCE(o.sales_order_id, ''), o.created_at, o.closed_at
		FROM %[1]s.crm_opportunities o LEFT JOIN %[1]s.documents c ON c.id = o.customer_id AND c.doctype = 'Customer'
		LEFT JOIN %[1]s.crm_pipeline_stages s ON s.id = o.stage_id LEFT JOIN %[1]s.documents q ON q.id = o.quotation_id AND q.doctype = 'SalesQuotation'`, schema)
}

func crmScanOpp(r interface{ Scan(...interface{}) error }) (CRMOpportunity, error) {
	var o CRMOpportunity
	var closed sql.NullTime
	err := r.Scan(&o.ID, &o.Title, &o.CustomerID, &o.CustomerName, &o.LeadID, &o.Owner, &o.StageID, &o.Stage, &o.Value, &o.Probability, &o.ExpectedClose, &o.Status,
		&o.LostReason, &o.QuotationID, &o.QuotationStatus, &o.SalesOrderID, &o.CreatedAt, &closed)
	if closed.Valid {
		o.ClosedAt = &closed.Time
	}
	o.Weighted = round2(o.Value * float64(o.Probability) / 100)
	return o, err
}

// GetCRMOpportunity returns one deal with its history.
func GetCRMOpportunity(tenantID, id string) (*CRMOpportunity, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	o, err := crmScanOpp(db.DB.QueryRow(crmOppSelect(schema)+` WHERE o.id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, crmNotFound("opportunity", id)
	}
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT field, COALESCE(from_value, ''), COALESCE(to_value, ''), COALESCE(changed_by, ''), changed_at FROM %s.crm_opportunity_history WHERE opportunity_id = $1 ORDER BY id`, schema), id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	o.History = []CRMOppHistoryItem{}
	for rows.Next() {
		var h CRMOppHistoryItem
		if err := rows.Scan(&h.Field, &h.From, &h.To, &h.ChangedBy, &h.ChangedAt); err != nil {
			return nil, err
		}
		o.History = append(o.History, h)
	}
	return &o, rows.Err()
}

// ListCRMOpportunities lists deals (open by default), for the board.
func ListCRMOpportunities(tenantID string, f map[string]string) ([]CRMOpportunity, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	where := []string{"TRUE"}
	args := []interface{}{}
	switch v := f["status"]; v {
	case "", "open":
		where = append(where, "o.status = 'Open'")
	case "all":
	default:
		args = append(args, v)
		where = append(where, fmt.Sprintf("o.status = $%d", len(args)))
	}
	for _, kv := range [][2]string{{"owner", "o.owner"}, {"customer_id", "o.customer_id"}, {"stage_id", "o.stage_id"}} {
		if v := f[kv[0]]; v != "" {
			args = append(args, v)
			where = append(where, fmt.Sprintf("%s = $%d", kv[1], len(args)))
		}
	}
	rows, err := db.DB.Query(crmOppSelect(schema)+` WHERE `+strings.Join(where, " AND ")+` ORDER BY o.expected_close NULLS LAST, o.updated_at DESC LIMIT 500`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CRMOpportunity{}
	for rows.Next() {
		o, err := crmScanOpp(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// CRMOpportunityInput edits a deal's details.
type CRMOpportunityInput struct {
	Title         string   `json:"title"`
	CustomerID    string   `json:"customer_id"`
	Owner         string   `json:"owner"`
	Value         *float64 `json:"value"`
	Probability   *int     `json:"probability"`
	ExpectedClose *string  `json:"expected_close"`
}

// SaveCRMOpportunity creates (id "") or edits a deal; each changed field is
// a history row.
func SaveCRMOpportunity(tenantID, id string, in CRMOpportunityInput, actor string) (*CRMOpportunity, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	stages, err := crmStages(schema)
	if err != nil {
		return nil, err
	}
	if id == "" {
		c, err := crmLoadCustomer(schema, in.CustomerID)
		if err != nil {
			return nil, crmNotFound("customer", in.CustomerID)
		}
		if strings.TrimSpace(in.Title) == "" {
			return nil, crmErr(CRMCodePipeline, "a deal needs a title")
		}
		var value float64
		if in.Value != nil {
			value = *in.Value
		}
		var closeDate interface{}
		if in.ExpectedClose != nil && *in.ExpectedClose != "" {
			closeDate = *in.ExpectedClose
		}
		id = NewDocID("OPP")
		if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.crm_opportunities (id, title, customer_id, owner, stage_id, value, probability, expected_close, created_by)
			VALUES ($1, $2, $3, NULLIF($4, ''), $5, $6, $7, $8, $9)`, schema), id, strings.TrimSpace(in.Title), c.ID, in.Owner, stages[0].ID, value, stages[0].Probability, closeDate, actor); err != nil {
			return nil, err
		}
		_, _ = db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.crm_opportunity_history (opportunity_id, field, to_value, changed_by) VALUES ($1, 'created', $2, $3)`, schema), id, stages[0].Name, actor)
		return GetCRMOpportunity(tenantID, id)
	}
	cur, err := GetCRMOpportunity(tenantID, id)
	if err != nil {
		return nil, err
	}
	if cur.Status != "Open" {
		return nil, crmErr(CRMCodePipeline, "a %s deal cannot change", strings.ToLower(cur.Status))
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	change := func(field, from, to, sqlSet string, val interface{}) error {
		if from == to {
			return nil
		}
		if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.crm_opportunities SET %s = $2, updated_at = NOW() WHERE id = $1`, schema, sqlSet), id, val); err != nil {
			return err
		}
		_, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.crm_opportunity_history (opportunity_id, field, from_value, to_value, changed_by) VALUES ($1, $2, $3, $4, $5)`, schema), id, field, from, to, actor)
		return err
	}
	if t := strings.TrimSpace(in.Title); t != "" {
		if err := change("title", cur.Title, t, "title", t); err != nil {
			return nil, err
		}
	}
	if err := change("owner", cur.Owner, strings.TrimSpace(in.Owner), "owner", nullIfEmpty(strings.TrimSpace(in.Owner))); err != nil {
		return nil, err
	}
	if in.Value != nil {
		if *in.Value < 0 {
			return nil, crmErr(CRMCodePipeline, "a value cannot be negative")
		}
		if err := change("value", fmt.Sprintf("%.2f", cur.Value), fmt.Sprintf("%.2f", *in.Value), "value", *in.Value); err != nil {
			return nil, err
		}
	}
	if in.Probability != nil {
		if *in.Probability < 0 || *in.Probability > 100 {
			return nil, crmErr(CRMCodePipeline, "a chance is 0 to 100")
		}
		if err := change("probability", fmt.Sprint(cur.Probability), fmt.Sprint(*in.Probability), "probability", *in.Probability); err != nil {
			return nil, err
		}
	}
	if in.ExpectedClose != nil {
		v := strings.TrimSpace(*in.ExpectedClose)
		if v != "" {
			if _, err := time.Parse("2006-01-02", v); err != nil {
				return nil, crmErr(CRMCodePipeline, "the expected close is a date (YYYY-MM-DD)")
			}
		}
		if err := change("expected_close", cur.ExpectedClose, v, "expected_close", nullIfEmpty(v)); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return GetCRMOpportunity(tenantID, id)
}

// MoveCRMOpportunity moves a deal to a stage. A stage's requirements must be
// met before it is entered; Won needs an approved quotation and raises one
// Sales Order; Lost needs a reason.
func MoveCRMOpportunity(tenantID, id, stageID, lostReason, actor string) (*CRMOpportunity, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	stages, err := crmStages(schema)
	if err != nil {
		return nil, err
	}
	var target *CRMPipelineStage
	for i := range stages {
		if stages[i].ID == stageID {
			target = &stages[i]
		}
	}
	if target == nil {
		return nil, crmNotFound("stage", stageID)
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var status, curStage, quotation, salesOrder, customerID string
	var value float64
	var closeDate sql.NullTime
	var owner sql.NullString
	err = tx.QueryRow(fmt.Sprintf(`SELECT status, stage_id, COALESCE(quotation_id, ''), COALESCE(sales_order_id, ''), customer_id, value, expected_close, owner
		FROM %s.crm_opportunities WHERE id = $1 FOR UPDATE`, schema), id).Scan(&status, &curStage, &quotation, &salesOrder, &customerID, &value, &closeDate, &owner)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, crmNotFound("opportunity", id)
	}
	if err != nil {
		return nil, err
	}
	if status == "Won" && target.Kind == "won" {
		tx.Rollback()
		return GetCRMOpportunity(tenantID, id) // pressed twice: the one order stands
	}
	if status != "Open" {
		return nil, crmErr(CRMCodePipeline, "the deal is %s", strings.ToLower(status))
	}
	if curStage == stageID {
		tx.Rollback()
		return GetCRMOpportunity(tenantID, id)
	}
	var qStatus string
	if quotation != "" {
		_ = tx.QueryRow(fmt.Sprintf(`SELECT status FROM %s.documents WHERE id = $1 AND doctype = 'SalesQuotation'`, schema), quotation).Scan(&qStatus)
	}
	for _, r := range target.Requires {
		switch r {
		case "value":
			if value <= 0 {
				return nil, crmErr(CRMCodePipeline, "%s needs the deal's value", target.Name)
			}
		case "expected_close":
			if !closeDate.Valid {
				return nil, crmErr(CRMCodePipeline, "%s needs an expected close date", target.Name)
			}
		case "quotation":
			if quotation == "" {
				return nil, crmErr(CRMCodePipeline, "%s needs a quotation", target.Name)
			}
		case "owner":
			if !owner.Valid || owner.String == "" {
				return nil, crmErr(CRMCodePipeline, "%s needs an owner", target.Name)
			}
		}
	}
	newStatus := "Open"
	switch target.Kind {
	case "won":
		if quotation == "" || qStatus != "Approved" && qStatus != "Converted" {
			return nil, crmErr(CRMCodePipeline, "a deal is won on an approved quotation - make one and have it approved first")
		}
		newStatus = "Won"
	case "lost":
		if strings.TrimSpace(lostReason) == "" {
			return nil, crmErr(CRMCodePipeline, "say why the deal was lost")
		}
		newStatus = "Lost"
	}
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.crm_opportunities SET stage_id = $2, probability = $3, status = $4::text, lost_reason = NULLIF($5, ''),
		closed_at = CASE WHEN $4::text = 'Open' THEN NULL ELSE NOW() END, updated_at = NOW() WHERE id = $1`, schema),
		id, target.ID, target.Probability, newStatus, strings.TrimSpace(lostReason)); err != nil {
		return nil, err
	}
	fromName := curStage
	for _, s := range stages {
		if s.ID == curStage {
			fromName = s.Name
		}
	}
	if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.crm_opportunity_history (opportunity_id, field, from_value, to_value, changed_by) VALUES ($1, 'stage', $2, $3, $4)`, schema),
		id, fromName, target.Name+map[bool]string{true: " - " + strings.TrimSpace(lostReason), false: ""}[newStatus == "Lost"], actor); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	if newStatus == "Won" && salesOrder == "" {
		if err := crmRaiseSalesOrder(tenantID, schema, id, quotation, customerID, actor); err != nil {
			return nil, crmErr(CRMCodePipeline, "the deal is won, but its Sales Order could not be raised: %v - open the deal and press Raise order", err)
		}
	}
	return GetCRMOpportunity(tenantID, id)
}

// crmRaiseSalesOrder raises the won deal's one Sales Order from its
// quotation. The order engine's own idempotency (channel B2B, the deal's id)
// makes a retry return the same order.
func crmRaiseSalesOrder(tenantID, schema, oppID, quotationID, customerID, actor string) error {
	var raw []byte
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents WHERE id = $1 AND doctype = 'SalesQuotation'`, schema), quotationID).Scan(&raw); err != nil {
		return err
	}
	var q map[string]interface{}
	_ = json.Unmarshal(raw, &q)
	lines := crmQuotationLines(q)
	if len(lines) == 0 {
		return fmt.Errorf("the quotation has no lines")
	}
	c, err := crmLoadCustomer(schema, customerID)
	if err != nil {
		return err
	}
	in := SalesOrderInput{Channel: "B2B", ChannelOrderID: oppID, CustomerName: c.Name(), CustomerRef: quotationID, PaymentStatus: "Pending",
		CustomerPhone: strField(c.Data, "phone"), ShippingAddress: strField(c.Data, "address"), ShippingLine2: strField(c.Data, "address_line2"),
		ShippingCity: strField(c.Data, "city"), ShippingState: strField(c.Data, "state"), ShippingPincode: strField(c.Data, "pincode"), SkipPending: true, AcceptedBy: actor}
	for _, l := range lines {
		in.Lines = append(in.Lines, SalesOrderLineInput{SKU: l.SKU, Qty: l.Qty, UnitPrice: l.Rate})
	}
	soID, err := CreateSalesOrder(tenantID, in)
	if err != nil {
		return err
	}
	if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.crm_opportunities SET sales_order_id = $2 WHERE id = $1 AND sales_order_id IS NULL`, schema), oppID, soID); err != nil {
		return err
	}
	_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET status = 'Converted', data = data || '{"status":"Converted"}'::jsonb || jsonb_build_object('sales_order_id', $2::text) WHERE id = $1`, schema), quotationID, soID)
	_, _ = db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.crm_opportunity_history (opportunity_id, field, to_value, changed_by) VALUES ($1, 'sales_order', $2, $3)`, schema), oppID, soID, actor)
	return nil
}

// RetryCRMSalesOrder raises a won deal's order if it is still missing.
func RetryCRMSalesOrder(tenantID, oppID, actor string) (*CRMOpportunity, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	o, err := GetCRMOpportunity(tenantID, oppID)
	if err != nil {
		return nil, err
	}
	if o.Status != "Won" || o.SalesOrderID != "" {
		return o, nil
	}
	if err := crmRaiseSalesOrder(tenantID, schema, oppID, o.QuotationID, o.CustomerID, actor); err != nil {
		return nil, crmErr(CRMCodePipeline, "%v", err)
	}
	return GetCRMOpportunity(tenantID, oppID)
}

// --- quotations -------------------------------------------------------------------

// CRMQuotationLine is one quoted item.
type CRMQuotationLine struct {
	SKU    string  `json:"sku"`
	Name   string  `json:"name,omitempty"`
	Qty    int     `json:"qty"`
	Rate   float64 `json:"rate"`
	Amount float64 `json:"amount"`
}

func crmQuotationLines(q map[string]interface{}) []CRMQuotationLine {
	var lines []CRMQuotationLine
	switch v := q["items"].(type) {
	case string:
		_ = json.Unmarshal([]byte(v), &lines)
	case []interface{}:
		b, _ := json.Marshal(v)
		_ = json.Unmarshal(b, &lines)
	}
	return lines
}

// SaveCRMQuotation makes or replaces a deal's draft quotation; a quotation
// already sent for approval or approved goes back to Draft when edited.
func SaveCRMQuotation(tenantID, oppID string, lines []CRMQuotationLine, validUntil, terms, actor string) (*CRMOpportunity, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	o, err := GetCRMOpportunity(tenantID, oppID)
	if err != nil {
		return nil, err
	}
	if o.Status != "Open" {
		return nil, crmErr(CRMCodeQuotation, "the deal is %s", strings.ToLower(o.Status))
	}
	if len(lines) == 0 {
		return nil, crmErr(CRMCodeQuotation, "a quotation needs at least one line")
	}
	total := 0.0
	for i := range lines {
		item, err := ResolveItemBySKU(tenantID, strings.TrimSpace(lines[i].SKU))
		if err != nil {
			return nil, crmErr(CRMCodeQuotation, "line %d: no item %q", i+1, lines[i].SKU)
		}
		if lines[i].Qty <= 0 || lines[i].Rate < 0 {
			return nil, crmErr(CRMCodeQuotation, "line %d: a quantity above 0 and a rate", i+1)
		}
		lines[i].SKU = strField(item.Data, "code")
		lines[i].Name = firstNonEmpty(strField(item.Data, "name"), strField(item.Data, "item_name"))
		lines[i].Amount = round2(float64(lines[i].Qty) * lines[i].Rate)
		total += lines[i].Amount
	}
	if validUntil != "" {
		if _, err := time.Parse("2006-01-02", validUntil); err != nil {
			return nil, crmErr(CRMCodeQuotation, "valid until is a date (YYYY-MM-DD)")
		}
	}
	itemsJSON, _ := json.Marshal(lines)
	data := map[string]interface{}{"customer": o.CustomerID, "opportunity_id": oppID, "valid_until": validUntil, "items": string(itemsJSON),
		"total_amount": round2(total), "terms": strings.TrimSpace(terms), "status": "Draft"}
	qid := o.QuotationID
	if qid != "" && (o.QuotationStatus == "Draft" || o.QuotationStatus == "Pending Approval" || o.QuotationStatus == "Approved" || o.QuotationStatus == "Rejected") {
		data["code"] = qid
		if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $2, status = 'Draft', updated_at = CURRENT_TIMESTAMP WHERE id = $1 AND doctype = 'SalesQuotation'`, schema), qid, crmJSON(data)); err != nil {
			return nil, err
		}
		if o.QuotationStatus != "Draft" {
			_ = logApprovalAction(tenantID, "SalesQuotation", qid, "Edited - needs approval again", actor, "", total, "")
		}
	} else {
		qid = NewDocID("QTN")
		data["code"] = qid
		if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'SalesQuotation', $2, 'Draft', $3)`, schema), qid, crmJSON(data), actor); err != nil {
			return nil, err
		}
		_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.crm_opportunities SET quotation_id = $2, updated_at = NOW() WHERE id = $1`, schema), oppID, qid)
		_, _ = db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.crm_opportunity_history (opportunity_id, field, to_value, changed_by) VALUES ($1, 'quotation', $2, $3)`, schema), oppID, qid, actor)
	}
	if o.Value <= 0 {
		v := round2(total)
		if _, err := SaveCRMOpportunity(tenantID, oppID, CRMOpportunityInput{Value: &v}, actor); err != nil {
			return nil, err
		}
	}
	return GetCRMOpportunity(tenantID, oppID)
}

// SubmitCRMQuotation sends a deal's quotation for approval.
func SubmitCRMQuotation(tenantID, oppID, actor, role string) (*CRMOpportunity, error) {
	o, err := GetCRMOpportunity(tenantID, oppID)
	if err != nil {
		return nil, err
	}
	if o.QuotationID == "" {
		return nil, crmErr(CRMCodeQuotation, "make the quotation first")
	}
	if o.QuotationStatus != "Draft" && o.QuotationStatus != "Rejected" {
		return nil, crmErr(CRMCodeQuotation, "the quotation is %s", strings.ToLower(o.QuotationStatus))
	}
	if o.QuotationStatus == "Rejected" {
		schema, _ := crmSchema(tenantID)
		_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET status = 'Draft' WHERE id = $1`, schema), o.QuotationID)
	}
	if err := SubmitForApproval(tenantID, "SalesQuotation", o.QuotationID, actor, role); err != nil {
		return nil, err
	}
	return GetCRMOpportunity(tenantID, oppID)
}

// GetCRMQuotation returns a quotation's lines and totals.
func GetCRMQuotation(tenantID, id string) (map[string]interface{}, []CRMQuotationLine, string, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, nil, "", err
	}
	var raw []byte
	var status string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT data, status FROM %s.documents WHERE id = $1 AND doctype = 'SalesQuotation'`, schema), id).Scan(&raw, &status); err != nil {
		return nil, nil, "", crmNotFound("quotation", id)
	}
	var d map[string]interface{}
	_ = json.Unmarshal(raw, &d)
	return d, crmQuotationLines(d), status, nil
}

// --- forecast (101.6.4) -------------------------------------------------------------

// CRMForecastRow is one month.
type CRMForecastRow struct {
	Month     string  `json:"month"`
	Open      int     `json:"open"`
	Pipeline  float64 `json:"pipeline"`
	Weighted  float64 `json:"weighted"`
	Committed float64 `json:"committed"`
	Booked    float64 `json:"booked"`
}

// GetCRMForecast is the forecast by expected-close month: the whole open
// pipeline, its weighted value, what is committed (deals in a committed
// stage) and what is booked (won deals' orders) - never added together.
func GetCRMForecast(tenantID string) ([]CRMForecastRow, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT COALESCE(to_char(COALESCE(o.expected_close, o.closed_at::date), 'YYYY-MM'), 'No date'),
		COUNT(*) FILTER (WHERE o.status = 'Open'),
		COALESCE(SUM(o.value) FILTER (WHERE o.status = 'Open'), 0),
		COALESCE(SUM(o.value * o.probability / 100.0) FILTER (WHERE o.status = 'Open'), 0),
		COALESCE(SUM(o.value) FILTER (WHERE o.status = 'Open' AND s.committed), 0),
		COALESCE(SUM(COALESCE(%s, o.value)) FILTER (WHERE o.status = 'Won'), 0)
		FROM %s.crm_opportunities o LEFT JOIN %s.crm_pipeline_stages s ON s.id = o.stage_id
		LEFT JOIN %s.documents so ON so.id = o.sales_order_id AND so.doctype = 'SalesOrder'
		WHERE o.status IN ('Open', 'Won') GROUP BY 1 ORDER BY 1`, crmNumSQL("so.data->>'total_amount'"), schema, schema, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CRMForecastRow{}
	for rows.Next() {
		var r CRMForecastRow
		if err := rows.Scan(&r.Month, &r.Open, &r.Pipeline, &r.Weighted, &r.Committed, &r.Booked); err != nil {
			return nil, err
		}
		r.Pipeline, r.Weighted, r.Committed, r.Booked = round2(r.Pipeline), round2(r.Weighted), round2(r.Committed), round2(r.Booked)
		out = append(out, r)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Month < out[j].Month })
	return out, rows.Err()
}

// CRMForecastSnapshot is a frozen forecast.
type CRMForecastSnapshot struct {
	ID      string           `json:"id"`
	Label   string           `json:"label"`
	TakenBy string           `json:"taken_by"`
	TakenAt time.Time        `json:"taken_at"`
	Rows    []CRMForecastRow `json:"rows"`
}

// TakeCRMForecastSnapshot freezes the forecast as it stands.
func TakeCRMForecastSnapshot(tenantID, label, actor string) (*CRMForecastSnapshot, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := GetCRMForecast(tenantID)
	if err != nil {
		return nil, err
	}
	label = strings.TrimSpace(label)
	if label == "" {
		label = time.Now().In(crmLocation(schema)).Format("Jan 2006")
	}
	s := &CRMForecastSnapshot{ID: NewDocID("FCS"), Label: label, TakenBy: actor, Rows: rows}
	err = db.DB.QueryRow(fmt.Sprintf(`INSERT INTO %s.crm_forecast_snapshots (id, label, taken_by, rows) VALUES ($1, $2, $3, $4) RETURNING taken_at`, schema), s.ID, label, actor, crmJSON(rows)).Scan(&s.TakenAt)
	return s, err
}

// ListCRMForecastSnapshots lists the frozen forecasts, newest first.
func ListCRMForecastSnapshots(tenantID string) ([]CRMForecastSnapshot, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, label, COALESCE(taken_by, ''), taken_at, rows FROM %s.crm_forecast_snapshots ORDER BY taken_at DESC LIMIT 60`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CRMForecastSnapshot{}
	for rows.Next() {
		var s CRMForecastSnapshot
		var raw []byte
		if err := rows.Scan(&s.ID, &s.Label, &s.TakenBy, &s.TakenAt, &raw); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(raw, &s.Rows)
		out = append(out, s)
	}
	return out, rows.Err()
}
