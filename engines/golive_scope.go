package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Stage 47.12.1 - the scope questionnaire.
//
// Ten short questions (industry, countries, legal entities, tax registrations,
// channels, locations, roles, inventory tracking, accounting basis, devices).
// The answers are saved per tenant as a new version each time (golive_records,
// kind Scope) and read back with a recommendation: every choice is looked up
// in the capability maturity registry (capability_maturity.go), the one source
// of what is supported. A choice whose capabilities are Production reads
// Supported, Preview reads Preview (pilot use), and one that is Experimental,
// or has no register entry at all, reads Unsupported and is never recommended.
// Nothing here switches a module on: a person acts on the recommendation.

// Support words a choice carries.
const (
	ScopeSupported   = "Supported"
	ScopePreview     = "Preview"
	ScopeUnsupported = "Unsupported"
	ScopeOpen        = "Open" // asks for nothing from the register
)

// ScopeOption is one answer a question offers. Capabilities are keys of the
// maturity registry; Unsupported, when set, is the reason no register entry
// covers the choice.
type ScopeOption struct {
	Value        string   `json:"value"`
	Label        string   `json:"label"`
	Capabilities []string `json:"-"`
	Unsupported  string   `json:"-"`
}

// ScopeQuestion is one question with its options.
type ScopeQuestion struct {
	ID      string        `json:"id"`
	Label   string        `json:"label"`
	Multi   bool          `json:"multi"`
	Options []ScopeOption `json:"options"`
}

const (
	noEntry = "No register entry covers it"
)

// scopeQuestions is the questionnaire. Option values are stable; a label is
// free to change.
var scopeQuestions = []ScopeQuestion{
	{ID: "industry", Label: "Industry", Options: []ScopeOption{
		{Value: "retail", Label: "Retail", Capabilities: []string{"sales.online_till", "inventory.transfers_adjustments"}},
		{Value: "wholesale", Label: "Wholesale and distribution", Capabilities: []string{"wholesale.summary_dropship", "wms.rack_pick_pack"}},
		{Value: "manufacturing", Label: "Manufacturing", Capabilities: []string{"manufacturing.production"}},
		{Value: "services", Label: "Services", Capabilities: []string{"service.tickets"}},
		{Value: "other", Label: "Other", Unsupported: "Not a reference configuration"},
	}},
	{ID: "countries", Label: "Countries", Multi: true, Options: []ScopeOption{
		{Value: "india", Label: "India", Capabilities: []string{"core.masters"}},
		{Value: "other", Label: "Another country", Unsupported: "The reference configurations are India only"},
	}},
	{ID: "entities", Label: "Legal entities", Options: []ScopeOption{
		{Value: "one", Label: "One", Capabilities: []string{"core.masters"}},
		{Value: "several", Label: "More than one", Capabilities: []string{"core.legal_entities"}},
	}},
	{ID: "registrations", Label: "Tax registrations", Options: []ScopeOption{
		{Value: "none", Label: "Not registered", Capabilities: []string{"core.masters"}},
		{Value: "one", Label: "One GSTIN", Capabilities: []string{"finance.reconciling_reports"}},
		{Value: "many", Label: "More than one GSTIN", Capabilities: []string{"core.legal_entities"}},
	}},
	{ID: "channels", Label: "Sales channels", Multi: true, Options: []ScopeOption{
		{Value: "counter", Label: "Store counter", Capabilities: []string{"sales.online_till"}},
		{Value: "counter_offline", Label: "Counter that works offline", Capabilities: []string{"sales.offline_till"}},
		{Value: "website", Label: "Own website", Capabilities: []string{"oms.order_to_doorstep"}},
		{Value: "shopify", Label: "Shopify", Capabilities: []string{"integrations.shopify"}},
		{Value: "b2b", Label: "Wholesale orders", Capabilities: []string{"wholesale.summary_dropship"}},
		{Value: "courier_api", Label: "Courier API booking", Capabilities: []string{"integrations.courier_api"}},
	}},
	{ID: "locations", Label: "Locations", Multi: true, Options: []ScopeOption{
		{Value: "stores", Label: "Stores", Capabilities: []string{"core.masters"}},
		{Value: "warehouses", Label: "Warehouses", Capabilities: []string{"wms.rack_pick_pack"}},
	}},
	{ID: "roles", Label: "Roles", Options: []ScopeOption{
		{Value: "owner", Label: "Owner does everything", Capabilities: []string{"core.masters"}},
		{Value: "team", Label: "A small team with roles", Capabilities: []string{"core.masters"}},
		{Value: "segregated", Label: "Separate duties and approvals", Capabilities: []string{"core.masters"}},
	}},
	{ID: "inventory", Label: "Inventory tracking", Options: []ScopeOption{
		{Value: "none", Label: "None"},
		{Value: "quantity", Label: "By quantity", Capabilities: []string{"inventory.transfers_adjustments"}},
		{Value: "batch", Label: "By batch", Capabilities: []string{"inventory.batch_serial"}},
		{Value: "serial", Label: "By serial number", Capabilities: []string{"inventory.batch_serial"}},
	}},
	{ID: "accounting", Label: "Accounting basis", Options: []ScopeOption{
		{Value: "accrual", Label: "Accrual", Capabilities: []string{"finance.reconciling_reports"}},
		{Value: "cash", Label: "Cash", Unsupported: "Cash basis: " + noEntry},
	}},
	{ID: "devices", Label: "Devices", Multi: true, Options: []ScopeOption{
		{Value: "none", Label: "None"},
		{Value: "label_printers", Label: "Label printers", Capabilities: []string{"stickers.labels"}},
		{Value: "other", Label: "Other devices"},
	}},
}

// ScopeAnswers maps a question id to the option values chosen.
type ScopeAnswers map[string][]string

// Has reports whether question q has option v chosen.
func (a ScopeAnswers) Has(q, v string) bool {
	for _, x := range a[q] {
		if x == v {
			return true
		}
	}
	return false
}

// ScopeChoice is one chosen option, judged against the register.
type ScopeChoice struct {
	Question string `json:"question"`
	Value    string `json:"value"`
	Label    string `json:"label"`
	Support  string `json:"support"`
	Level    string `json:"level,omitempty"`
	Detail   string `json:"detail,omitempty"`
}

// ScopeModule is a module the answers call on, at the lowest level among the
// capabilities that name it.
type ScopeModule struct {
	Module string `json:"module"`
	Level  string `json:"level"`
	Titles string `json:"titles"`
}

// GoLiveScope is the saved answers with the recommendation read from them.
type GoLiveScope struct {
	Saved       bool            `json:"saved"`
	Unevaluated bool            `json:"unevaluated,omitempty"` // an answer's question or option was retired since
	Version     int             `json:"version"`
	SavedBy     string          `json:"saved_by,omitempty"`
	SavedAt     string          `json:"saved_at,omitempty"`
	Answers     ScopeAnswers    `json:"answers"`
	Questions   []ScopeQuestion `json:"questions"`
	Choices     []ScopeChoice   `json:"choices"`
	Modules     []ScopeModule   `json:"modules"`
	Unsupported []ScopeChoice   `json:"unsupported"`
	Recommended bool            `json:"recommended"` // true when nothing chosen is unsupported
	Versions    []ScopeVersion  `json:"versions"`
}

// ScopeVersion is one saved version in the list.
type ScopeVersion struct {
	Version int    `json:"version"`
	SavedBy string `json:"saved_by"`
	SavedAt string `json:"saved_at"`
}

func maturityByKey() map[string]CapabilityMaturity {
	out := map[string]CapabilityMaturity{}
	for _, c := range capabilityMaturity {
		out[c.Key] = c
	}
	return out
}

func scopeQuestionByID(id string) (ScopeQuestion, bool) {
	for _, q := range scopeQuestions {
		if q.ID == id {
			return q, true
		}
	}
	return ScopeQuestion{}, false
}

// judgeScopeOption reads one option against the registry.
func judgeScopeOption(q ScopeQuestion, o ScopeOption, reg map[string]CapabilityMaturity) ScopeChoice {
	c := ScopeChoice{Question: q.ID, Value: o.Value, Label: o.Label}
	if o.Unsupported != "" {
		c.Support, c.Detail = ScopeUnsupported, o.Unsupported
		return c
	}
	if len(o.Capabilities) == 0 {
		c.Support = ScopeOpen
		return c
	}
	lowest, weak := "", []string{}
	for _, key := range o.Capabilities {
		m, ok := reg[key]
		if !ok {
			c.Support, c.Detail = ScopeUnsupported, key+": "+noEntry
			return c
		}
		if lowest == "" || maturityRank(m.Level) < maturityRank(lowest) {
			lowest = m.Level
		}
		if m.Level == MaturityExperimental {
			weak = append(weak, m.Title)
		}
	}
	c.Level = lowest
	switch lowest {
	case MaturityExperimental:
		c.Support, c.Detail = ScopeUnsupported, "Experimental: "+strings.Join(weak, "; ")
	case MaturityPreview:
		c.Support = ScopePreview
	default:
		c.Support = ScopeSupported
	}
	return c
}

// EvaluateScope judges answers against the registry. It does not save.
func EvaluateScope(answers ScopeAnswers) (*GoLiveScope, error) {
	if err := validateScopeAnswers(answers); err != nil {
		return nil, err
	}
	reg := maturityByKey()
	out := &GoLiveScope{Answers: answers, Questions: scopeQuestions, Choices: []ScopeChoice{}, Modules: []ScopeModule{}, Unsupported: []ScopeChoice{}, Versions: []ScopeVersion{}}
	mods := map[string]*ScopeModule{}
	titles := map[string][]string{}
	for _, q := range scopeQuestions {
		for _, o := range q.Options {
			if !answers.Has(q.ID, o.Value) {
				continue
			}
			c := judgeScopeOption(q, o, reg)
			out.Choices = append(out.Choices, c)
			if c.Support == ScopeUnsupported {
				out.Unsupported = append(out.Unsupported, c)
				continue
			}
			for _, key := range o.Capabilities {
				m := reg[key]
				cur, ok := mods[m.Module]
				if !ok {
					mods[m.Module] = &ScopeModule{Module: m.Module, Level: m.Level}
					cur = mods[m.Module]
				} else if maturityRank(m.Level) < maturityRank(cur.Level) {
					cur.Level = m.Level
				}
				dup := false
				for _, t := range titles[m.Module] {
					dup = dup || t == m.Title
				}
				if !dup {
					titles[m.Module] = append(titles[m.Module], m.Title)
				}
			}
		}
	}
	for k, m := range mods {
		m.Titles = strings.Join(titles[k], "; ")
		out.Modules = append(out.Modules, *m)
	}
	sort.Slice(out.Modules, func(i, j int) bool { return out.Modules[i].Module < out.Modules[j].Module })
	out.Recommended = len(out.Unsupported) == 0
	return out, nil
}

func scopeErr(msg string) error { return &ValidationError{Code: "GLOBAL-9110", Message: msg} }

// validateScopeAnswers: every question answered, with options it offers, and
// one value only on a single-choice question.
func validateScopeAnswers(a ScopeAnswers) error {
	for id := range a {
		if _, ok := scopeQuestionByID(id); !ok {
			return scopeErr(fmt.Sprintf("%q is not a question", id))
		}
	}
	for _, q := range scopeQuestions {
		vals := a[q.ID]
		if len(vals) == 0 {
			return scopeErr("Answer: " + q.Label)
		}
		if !q.Multi && len(vals) > 1 {
			return scopeErr("Choose one: " + q.Label)
		}
		seen := map[string]bool{}
		for _, v := range vals {
			ok := false
			for _, o := range q.Options {
				ok = ok || o.Value == v
			}
			if !ok {
				return scopeErr(fmt.Sprintf("%q is not an answer to %s", v, q.Label))
			}
			if seen[v] {
				return scopeErr("Repeated answer: " + q.Label)
			}
			seen[v] = true
		}
		if q.Multi && seen["none"] && len(vals) > 1 {
			return scopeErr("None cannot go with another answer: " + q.Label)
		}
	}
	return nil
}

// SaveGoLiveScope saves the answers as the next version.
func SaveGoLiveScope(tenantID string, answers ScopeAnswers, actor string) (*GoLiveScope, error) {
	if _, err := EvaluateScope(answers); err != nil {
		return nil, err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(answers)
	if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.golive_records (kind, pack, line_id, data, created_by) VALUES ('Scope', 'scope', 'scope', $1::jsonb, $2)`, schema), string(raw), actor); err != nil {
		return nil, err
	}
	return GetGoLiveScope(tenantID)
}

// GetGoLiveScope returns the latest saved answers with their recommendation;
// Saved is false (and Answers empty) before the first save.
func GetGoLiveScope(tenantID string) (*GoLiveScope, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT data::text, created_by, to_char(created_at, 'YYYY-MM-DD HH24:MI'), row_number() OVER (ORDER BY id)
		FROM %s.golive_records WHERE kind = 'Scope' ORDER BY id DESC LIMIT 50`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var latest *GoLiveScope
	versions := []ScopeVersion{}
	for rows.Next() {
		var raw, by, at string
		var n int
		if err := rows.Scan(&raw, &by, &at, &n); err != nil {
			return nil, err
		}
		versions = append(versions, ScopeVersion{Version: n, SavedBy: by, SavedAt: at})
		if latest == nil {
			a := ScopeAnswers{}
			_ = json.Unmarshal([]byte(raw), &a)
			s, err := EvaluateScope(a)
			if err != nil { // a question was retired since: show the answers as they were
				s = &GoLiveScope{Unevaluated: true, Answers: a, Questions: scopeQuestions, Choices: []ScopeChoice{}, Modules: []ScopeModule{}, Unsupported: []ScopeChoice{}}
			}
			s.Saved, s.Version, s.SavedBy, s.SavedAt = true, n, by, at
			latest = s
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if latest == nil {
		return &GoLiveScope{Answers: ScopeAnswers{}, Questions: scopeQuestions, Choices: []ScopeChoice{}, Modules: []ScopeModule{}, Unsupported: []ScopeChoice{}, Versions: versions}, nil
	}
	latest.Versions = versions
	return latest, nil
}
