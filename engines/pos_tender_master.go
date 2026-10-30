package engines

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"custom_erp/db"
)

// Stage 85.E2 / 85.E3 / 85.E5 (77.5, 77.6, 77.10): the till's tenders and
// salesmen from the client's own masters (db/migrations_stage85_5_*), the
// tender-wise sales figures and the per-tender close.

// TenderDef is one way a store takes money.
type TenderDef struct {
	Name           string `json:"name"`
	Kind           string `json:"kind"`
	NeedsReference bool   `json:"needs_reference"`
	OpensDrawer    bool   `json:"opens_drawer"`
	Order          int    `json:"display_order"`
}

func storeListed(list, store string) bool {
	if strings.TrimSpace(list) == "" {
		return true
	}
	for _, s := range strings.Split(list, ",") {
		if strings.EqualFold(strings.TrimSpace(s), store) {
			return true
		}
	}
	return false
}

// StoreTenders is the active tenders a store takes, in till order. A client
// with none on file takes Cash, Card and UPI, as before.
func StoreTenders(tenantID, store string) ([]TenderDef, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT data FROM %s.documents WHERE doctype = 'POSTender' AND deleted_at IS NULL AND COALESCE(data->>'status', 'Active') = 'Active'`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TenderDef{}
	any := false
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		any = true
		var d map[string]interface{}
		_ = json.Unmarshal(raw, &d)
		t := TenderDef{Name: strings.TrimSpace(strField(d, "name")), Kind: strField(d, "kind"),
			NeedsReference: strField(d, "needs_reference") == "Yes", OpensDrawer: strField(d, "opens_drawer") == "Yes",
			Order: int(numFromInterface(d["display_order"]))}
		if t.Name == "" || !isPOSMoneyMode(t.Kind) || !storeListed(strField(d, "stores"), store) {
			continue
		}
		out = append(out, t)
	}
	if !any {
		out = []TenderDef{{Name: "Cash", Kind: "Cash", OpensDrawer: true, Order: 1}, {Name: "Card", Kind: "Card", NeedsReference: true, Order: 2}, {Name: "UPI", Kind: "UPI", NeedsReference: true, Order: 3}}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Order < out[j].Order })
	return out, rows.Err()
}

// ValidateStoreTender refuses a tender name the store does not take, or one
// sent with the wrong kind. A blank name is the kind itself (older tills).
func ValidateStoreTender(tenantID, store, name, kind string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	tenders, err := StoreTenders(tenantID, store)
	if err != nil {
		return err
	}
	for _, t := range tenders {
		if strings.EqualFold(t.Name, name) {
			if !strings.EqualFold(t.Kind, kind) {
				return &ValidationError{Code: "POSOFF-0875", Message: fmt.Sprintf("%s is a %s tender, not %s.", t.Name, t.Kind, kind)}
			}
			return nil
		}
	}
	return &ValidationError{Code: "POSOFF-0875", Message: fmt.Sprintf("%s is not a tender this store takes.", name)}
}

// --- salesmen -------------------------------------------------------------------

// SalesmanDef is one salesman of a store.
type SalesmanDef struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	IsDefault bool   `json:"is_default"`
}

// StoreSalesmen is the store's active salesmen (default first) and whether
// the till asks for one: Off, Optional or Mandatory (Location.salesman_mode).
func StoreSalesmen(tenantID, store string) ([]SalesmanDef, string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, "", err
	}
	mode := "Off"
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(NULLIF(data->>'salesman_mode', ''), 'Off') FROM %s.documents WHERE doctype = 'Location' AND id = $1`, schema), store).Scan(&mode)
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, COALESCE(data->>'name', id), COALESCE(data->>'is_default', 'No') FROM %s.documents
		WHERE doctype = 'Salesman' AND deleted_at IS NULL AND COALESCE(data->>'status', 'Active') = 'Active' AND data->>'store' = $1
		ORDER BY (data->>'is_default' = 'Yes') DESC, lower(data->>'name')`, schema), store)
	if err != nil {
		return nil, mode, err
	}
	defer rows.Close()
	out := []SalesmanDef{}
	for rows.Next() {
		var s SalesmanDef
		var def string
		if err := rows.Scan(&s.ID, &s.Name, &def); err != nil {
			return nil, mode, err
		}
		s.IsDefault = def == "Yes"
		out = append(out, s)
	}
	return out, mode, rows.Err()
}

// ValidateBillSalesman checks the salesman on a bill against the store's
// rule: Mandatory needs one, and any one named must be the store's, active.
func ValidateBillSalesman(tenantID, store, salesmanID string) error {
	list, mode, err := StoreSalesmen(tenantID, store)
	if err != nil {
		return err
	}
	salesmanID = strings.TrimSpace(salesmanID)
	if salesmanID == "" {
		if mode == "Mandatory" {
			return &ValidationError{Code: "POSOFF-0876", Message: "This store records the salesman on every bill - pick one."}
		}
		return nil
	}
	for _, s := range list {
		if s.ID == salesmanID {
			return nil
		}
	}
	return &ValidationError{Code: "POSOFF-0876", Message: fmt.Sprintf("%s is not an active salesman of this store.", salesmanID)}
}

// --- tender-wise figures ----------------------------------------------------------

// cartTenderTotals is what a paid bill took by tender name, from its own
// allocation (73.1.1 - money by kind) and the names it carries.
func cartTenderTotals(data map[string]interface{}) map[string]float64 {
	byKind := map[string]int64{}
	// The allocation is the bill's lines, each with its tenders (the money
	// kinds, Gift Card, Loyalty Points) - always adding up to the line.
	if lines, ok := data["allocation"].([]interface{}); ok {
		for _, l := range lines {
			if m, ok := l.(map[string]interface{}); ok {
				if tenders, ok := m["tenders"].(map[string]interface{}); ok {
					for k, v := range tenders {
						byKind[k] += int64(numFromInterface(v))
					}
				}
			}
		}
	}
	names := map[string]string{}
	if fixed, ok := data["tenders"].([]interface{}); ok {
		for _, f := range fixed {
			if m, ok := f.(map[string]interface{}); ok && strField(m, "name") != "" {
				names[strField(m, "mode")] = strField(m, "name")
			}
		}
	}
	if pt := strField(data, "payment_tender"); pt != "" {
		if _, taken := names[strField(data, "payment_mode")]; !taken {
			names[strField(data, "payment_mode")] = pt
		}
	}
	out := map[string]float64{}
	for kind, paise := range byKind {
		if paise == 0 {
			continue
		}
		if kind == TenderGiftCard {
			paise = splitStoredValueTenders(data, paise, out)
			if paise == 0 {
				continue
			}
		}
		label := kind
		if n, ok := names[kind]; ok {
			label = n
		}
		out[label] += PaiseToRupees(paise)
	}
	return out
}

// Tender lines the X-report shows for stored value that is not a gift card
// (140.12.2). The sale's allocation keeps them under Gift Card - a return
// gives them back through the same stored-value path - so only the summary
// splits them out, from the redemptions stored on the cart.
const (
	TenderAdvanceLabel     = "Advance"
	TenderStoreCreditLabel = "Store Credit"
)

// splitStoredValueTenders moves the advance and store-credit part of a
// cart's Gift Card total into their own lines and returns what is left.
func splitStoredValueTenders(data map[string]interface{}, giftPaise int64, out map[string]float64) int64 {
	list, _ := data["gift_card_tenders"].([]interface{})
	for _, t := range list {
		m, ok := t.(map[string]interface{})
		if !ok {
			continue
		}
		label := ""
		switch card := strField(m, "card"); {
		case strings.HasPrefix(card, "Advance "):
			label = TenderAdvanceLabel
		case card == "Store credit":
			label = TenderStoreCreditLabel
		}
		amount := int64(numFromInterface(m["amount_paise"]))
		if label == "" || amount <= 0 {
			continue
		}
		if amount > giftPaise {
			amount = giftPaise
		}
		out[label] += PaiseToRupees(amount)
		giftPaise -= amount
	}
	return giftPaise
}

// TenderTotals sums paid bills by tender: for one till session, or for a
// store between two dates (blank = all).
func TenderTotals(tenantID, sessionID, store, from, to string) (map[string]float64, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	q := fmt.Sprintf(`SELECT data FROM %s.documents WHERE doctype = 'POSCart' AND status = 'Paid' AND deleted_at IS NULL`, schema)
	args := []interface{}{}
	add := func(clause string, v string) {
		args = append(args, v)
		q += fmt.Sprintf(clause, len(args))
	}
	if sessionID != "" {
		add(" AND data->>'pos_session' = $%d", sessionID)
	}
	if store != "" {
		add(" AND data->>'location' = $%d", store)
	}
	if from != "" {
		add(" AND created_at >= public.erp_day_start($%d::date)", from)
	}
	if to != "" {
		add(" AND created_at < public.erp_day_start($%d::date + 1)", to)
	}
	rows, err := db.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]float64{}
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var d map[string]interface{}
		_ = json.Unmarshal(raw, &d)
		for k, v := range cartTenderTotals(d) {
			out[k] = round2(out[k] + v)
		}
	}
	return out, rows.Err()
}
