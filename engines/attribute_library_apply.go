package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Stage 81: the platform operator gives a client its product attributes from
// the industry attribute library (attribute_library.go). The client then owns
// them as ordinary ProductAttributeDef records: it can switch one off and trim
// its values, but an attribute that came from the library is never deleted
// (CheckAttributeDelete) and is never added a second time, so what the client
// changed stays changed - "it will stay forever for them".

// libraryIndustryByProfile maps the industry profile codes a client chooses
// (validIndustryCodes in the server) onto the library's industry codes.
var libraryIndustryByProfile = map[string]string{
	"JEWELRY": "JEW", "FOOD_BEV": "FNB", "AUTO": "AUT", "CLOTHING": "CLO", "PHARMA": "PHR",
	"METAL": "MET", "CONSTRUCTION": "CON", "MEDICAL": "MED", "SEMICONDUCTOR": "SEM", "AGRICULTURE": "AGR",
}

// LibraryIndustryForProfile returns the library code for a profile code, or "".
func LibraryIndustryForProfile(profile string) string {
	return libraryIndustryByProfile[strings.ToUpper(strings.TrimSpace(profile))]
}

// Used for values (ProductAttributeDef.used_for).
const (
	AttributeUsedForVariant = "Variant"
	AttributeUsedForProduct = "Product information"
)

// ClientAttribute is an attribute a client already has.
type ClientAttribute struct {
	Code      string `json:"code"`
	Label     string `json:"label"`
	Status    string `json:"status"`
	UsedFor   string `json:"used_for,omitempty"`
	LibraryID string `json:"library_id,omitempty"`
}

// LibraryOffer is one library attribute as the operator sees it for a client.
type LibraryOffer struct {
	LibraryAttribute
	Use       string           `json:"use"`
	CoveredBy string           `json:"covered_by,omitempty"`
	Values    []string         `json:"values,omitempty"`
	Client    *ClientAttribute `json:"client,omitempty"`  // the client already has this one
	Similar   *ClientAttribute `json:"similar,omitempty"` // a client attribute with a similar name
}

// AttributeLibraryView is the operator's page for one client and industry.
type AttributeLibraryView struct {
	Industries     []LibraryIndustry `json:"industries"`
	Industry       string            `json:"industry"`
	ClientIndustry string            `json:"client_industry,omitempty"`
	PIMEnabled     bool              `json:"pim_enabled"`
	Attributes     []LibraryOffer    `json:"attributes"`
	Others         []ClientAttribute `json:"others"` // the client's attributes that are not from this library
}

var nonLetters = regexp.MustCompile(`[^a-z]+`)

func similarNames(a, b string) bool {
	a, b = nonLetters.ReplaceAllString(strings.ToLower(a), ""), nonLetters.ReplaceAllString(strings.ToLower(b), "")
	if len(a) < 4 || len(b) < 4 {
		return a != "" && a == b
	}
	return a == b || strings.Contains(a, b) || strings.Contains(b, a)
}

type rowQuerier interface {
	Query(query string, args ...interface{}) (*sql.Rows, error)
}

// clientAttributes reads every live ProductAttributeDef of a schema.
func clientAttributes(q rowQuerier, schema string) ([]ClientAttribute, error) {
	rows, err := q.Query(fmt.Sprintf(`SELECT id, data FROM %s.documents WHERE doctype = 'ProductAttributeDef' AND deleted_at IS NULL ORDER BY id`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ClientAttribute
	for rows.Next() {
		var id, raw string
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, err
		}
		var d map[string]interface{}
		_ = json.Unmarshal([]byte(raw), &d)
		out = append(out, ClientAttribute{
			Code: firstNonEmpty(strField(d, "code"), id), Label: firstNonEmpty(strField(d, "label"), strField(d, "code"), id),
			Status: firstNonEmpty(strField(d, "status"), "Active"), UsedFor: strField(d, "used_for"), LibraryID: strField(d, "library_id"),
		})
	}
	return out, rows.Err()
}

// AttributeLibraryForTenant lists one industry's library attributes for a
// client, marking the ones it already has. industry "" uses the client's own
// industry profile (Jewellery when it has none).
func AttributeLibraryForTenant(tenantID, industry string) (AttributeLibraryView, error) {
	view := AttributeLibraryView{Industries: LibraryIndustries(), Attributes: []LibraryOffer{}, Others: []ClientAttribute{}}
	if lock, err := GetIndustryLock(tenantID); err == nil && lock != nil {
		view.ClientIndustry = lock.IndustryCode
	}
	industry = strings.ToUpper(strings.TrimSpace(industry))
	if industry == "" {
		industry = firstNonEmpty(LibraryIndustryForProfile(view.ClientIndustry), "JEW")
	}
	view.Industry = industry
	view.PIMEnabled, _ = IsModuleEnabled(tenantID, "pim")

	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return view, err
	}
	have, err := clientAttributes(db.DB, schema)
	if err != nil {
		return view, err
	}
	matched := map[string]bool{}
	for _, a := range LibraryAttributesFor(industry) {
		offer := LibraryOffer{LibraryAttribute: a, Use: LibraryAttributeUse(a), CoveredBy: LibraryCoveredBy(a.ID)}
		for _, v := range LibraryValues(a.ValueSet) {
			offer.Values = append(offer.Values, v.Label)
		}
		for i := range have {
			c := have[i]
			if c.LibraryID == a.ID || strings.EqualFold(c.Code, a.ID) {
				offer.Client = &c
				matched[c.Code] = true
				break
			}
		}
		if offer.Client == nil {
			for i := range have {
				if have[i].LibraryID == "" && similarNames(have[i].Label, a.Name) {
					c := have[i]
					offer.Similar = &c
					break
				}
			}
		}
		view.Attributes = append(view.Attributes, offer)
	}
	for _, c := range have {
		if !matched[c.Code] {
			view.Others = append(view.Others, c)
		}
	}
	return view, nil
}

// ErrUnknownLibraryAttribute: a pick names no attribute of the library.
var ErrUnknownLibraryAttribute = errors.New("no such attribute in the attribute library")

// AttributeLibraryPick is one attribute the operator ticked, and what for.
type AttributeLibraryPick struct {
	ID      string `json:"id"`
	UsedFor string `json:"used_for"`
}

// AttributeLibraryResult says what an apply did.
type AttributeLibraryResult struct {
	Added   []string `json:"added"`
	Skipped []string `json:"skipped"`
}

// ApplyAttributeLibrary adds the picked library attributes to a client, with
// their dropdown values and their library group, in one transaction. One the
// client already has (same library id or code) is skipped and left exactly as
// the client keeps it - switched off, values trimmed, whatever it is now.
func ApplyAttributeLibrary(tenantID, actor string, picks []AttributeLibraryPick) (AttributeLibraryResult, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return AttributeLibraryResult{}, err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return AttributeLibraryResult{}, err
	}
	defer tx.Rollback()
	res, err := applyAttributeLibraryTx(tx, schema, actor, picks)
	if err != nil {
		return res, err
	}
	if err := tx.Commit(); err != nil {
		return res, err
	}
	if len(res.Added) > 0 {
		LogAuditEvent(tenantID, actor, "ATTRIBUTE_LIBRARY_APPLIED", "SUCCESS",
			fmt.Sprintf("%d attribute(s) added from the attribute library: %s", len(res.Added), strings.Join(res.Added, ", ")))
	}
	return res, nil
}

var groupSlug = regexp.MustCompile(`[^A-Z0-9]+`)

func applyAttributeLibraryTx(tx *sql.Tx, schema, actor string, picks []AttributeLibraryPick) (AttributeLibraryResult, error) {
	res := AttributeLibraryResult{Added: []string{}, Skipped: []string{}}
	have, err := clientAttributes(tx, schema)
	if err != nil {
		return res, err
	}
	// created_by must be one of the client's own users, and the operator is
	// not: the 'system' user the industry seeds use, else its Super Admin.
	// Who actually did it is in added_by and in the audit log.
	var creator string
	if err := tx.QueryRow(fmt.Sprintf(`SELECT id FROM %s.users
		ORDER BY (id = 'system') DESC, (role IN ('Super Admin', 'HR/Admin')) DESC, created_at LIMIT 1`, schema)).Scan(&creator); err != nil {
		return res, fmt.Errorf("the client has no user to record as the creator: %w", err)
	}
	insert := func(id, doctype string, data map[string]interface{}) (bool, error) {
		body, _ := json.Marshal(data)
		var got string
		err := tx.QueryRow(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by)
			VALUES ($1, $2, $3::jsonb, 'Active', $4) ON CONFLICT DO NOTHING RETURNING id`, schema), id, doctype, string(body), creator).Scan(&got)
		if err == sql.ErrNoRows {
			return false, nil
		}
		return err == nil, err
	}
	groupOrder := map[string]int{}
	for _, pick := range picks {
		a, ok := LibraryAttributeByID(strings.TrimSpace(pick.ID))
		if !ok {
			return res, fmt.Errorf("%w: %q", ErrUnknownLibraryAttribute, pick.ID)
		}
		if use := LibraryAttributeUse(a); use == "covered" || use == "lot" {
			res.Skipped = append(res.Skipped, a.ID)
			continue
		}
		already := false
		for _, c := range have {
			if c.LibraryID == a.ID || strings.EqualFold(c.Code, a.ID) {
				already = true
				break
			}
		}
		if already {
			res.Skipped = append(res.Skipped, a.ID)
			continue
		}
		usedFor := strings.TrimSpace(pick.UsedFor)
		if usedFor != AttributeUsedForVariant && usedFor != AttributeUsedForProduct {
			usedFor = AttributeUsedForProduct
			if u := LibraryAttributeUse(a); u == "variant" || u == "variant-candidate" {
				usedFor = AttributeUsedForVariant
			}
		}

		groupID := ""
		if a.Group != "" {
			groupID = "LIB-" + a.Industry + "-" + strings.Trim(groupSlug.ReplaceAllString(strings.ToUpper(a.Group), "-"), "-")
			if _, seen := groupOrder[groupID]; !seen {
				groupOrder[groupID] = len(groupOrder) + 1
				if _, err := insert(groupID, "ProductAttributeGroup", map[string]interface{}{
					"code": groupID, "name": a.Group, "display_order": groupOrder[groupID], "status": "Active",
				}); err != nil {
					return res, err
				}
			}
		}

		valueType := LibraryValueType(a.Type)
		data := map[string]interface{}{
			"code": a.ID, "label": a.Name, "value_type": valueType, "status": "Active",
			"used_for": usedFor, "library_id": a.ID,
			"added_by": actor,
		}
		if groupID != "" {
			data["group"] = groupID
		}
		if a.UOM != "" {
			data["uom"] = a.UOM
		}
		if valueType == "Select" {
			var labels []string
			for _, v := range LibraryValues(a.ValueSet) {
				labels = append(labels, strings.ReplaceAll(v.Label, ",", " "))
			}
			data["value_options"] = strings.Join(labels, ",")
		}
		added, err := insert(a.ID, "ProductAttributeDef", data)
		if err != nil {
			return res, err
		}
		if added {
			res.Added = append(res.Added, a.ID)
		} else {
			res.Skipped = append(res.Skipped, a.ID)
		}
	}
	return res, nil
}

// TenantHasLibraryAttributes reports whether the operator has given this
// client attributes from the library. From then on its attribute list is the
// operator's to extend: transaction screens (a PO's design panel, the Item
// form) pick from it and never invent a new attribute.
func TenantHasLibraryAttributes(tenantID string) bool {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return false
	}
	var found bool
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT EXISTS (SELECT 1 FROM %s.documents
		WHERE doctype = 'ProductAttributeDef' AND deleted_at IS NULL AND COALESCE(data->>'library_id', '') <> '')`, schema)).Scan(&found)
	return found
}

// KeepLibraryAttributeIdentity keeps a library attribute's code and library
// id through an edit: the client changes its label, status and values, but
// clearing library_id would quietly lift the delete guard, and a new code
// would orphan the key every item and design stores its values under.
func KeepLibraryAttributeIdentity(tenantID, id string, payload map[string]interface{}) {
	if strings.TrimSpace(id) == "" {
		return
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return
	}
	var raw string
	if db.DB.QueryRow(fmt.Sprintf(`SELECT data::text FROM %s.documents
		WHERE id = $1 AND doctype = 'ProductAttributeDef' AND deleted_at IS NULL`, schema), id).Scan(&raw) != nil {
		return
	}
	var d map[string]interface{}
	if json.Unmarshal([]byte(raw), &d) != nil || strField(d, "library_id") == "" {
		return
	}
	payload["library_id"] = strField(d, "library_id")
	payload["code"] = firstNonEmpty(strField(d, "code"), id)
}

// CheckAttributeDelete refuses deleting an attribute that came from the
// library: the client switches it off instead (Status Inactive), so items and
// designs that use it keep a defined attribute behind their values.
func CheckAttributeDelete(tenantID, id string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var libraryID string
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(data->>'library_id', '') FROM %s.documents
		WHERE id = $1 AND doctype = 'ProductAttributeDef' AND deleted_at IS NULL`, schema), id).Scan(&libraryID)
	if err == sql.ErrNoRows || (err == nil && libraryID == "") {
		return nil
	}
	if err != nil {
		return err
	}
	return &ValidationError{Code: "PIM-0690", Message: "This attribute was set up for your business and cannot be deleted. Switch it off instead: open it and set Status to Inactive. Its values can be removed one by one."}
}
