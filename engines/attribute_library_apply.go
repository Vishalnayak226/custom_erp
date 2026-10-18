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
// them as ordinary records: it can switch one off and trim its values, but an
// attribute that came from the library is never deleted (CheckAttributeDelete)
// and is never added a second time, so what the client changed stays changed -
// "it will stay forever for them".
//
// Stage 86.3: they land in two lists, because the ERP and PIM are separate
// apps a client can buy one without the other (user, 2026-10-18: "PIM will map
// it and have more attribute but ERP will have very basic attribute"). A
// variant attribute goes to the ERP's ItemAttribute list - and, for a client
// with PIM, also to PIM's list, mapped onto the ERP one; a product-information
// attribute goes to PIM's ProductAttributeDef list only.

// ItemAttributeDoctype is the ERP's own attribute list: the few attributes an
// item varies by - the Item's variant options, PO designs, prints.
const ItemAttributeDoctype = "ItemAttribute"

// itemAttributeID is the record id of the ERP attribute with this code when it
// is made for it (from the library, a PO, or copied from PIM's list by the
// Stage 86.3 migration): ids are unique across every record type, and PIM's
// attribute of the same code holds the bare code. The code - the key items and
// designs store values under - stays the bare code.
func itemAttributeID(code string) string { return "IA-" + code }

// Attribute lists a ClientAttribute belongs to.
const (
	AttributeListERP = "ERP"
	AttributeListPIM = "PIM"
)

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
	List      string `json:"list"` // AttributeListERP or AttributeListPIM
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

// clientAttributes reads every live attribute of a schema, ERP list first.
func clientAttributes(q rowQuerier, schema string) ([]ClientAttribute, error) {
	rows, err := q.Query(fmt.Sprintf(`SELECT doctype, id, data FROM %s.documents
		WHERE doctype IN ('ItemAttribute', 'ProductAttributeDef') AND deleted_at IS NULL
		ORDER BY doctype = 'ProductAttributeDef', id`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ClientAttribute
	for rows.Next() {
		var doctype, id, raw string
		if err := rows.Scan(&doctype, &id, &raw); err != nil {
			return nil, err
		}
		var d map[string]interface{}
		_ = json.Unmarshal([]byte(raw), &d)
		c := ClientAttribute{
			Code: firstNonEmpty(strField(d, "code"), id), Label: firstNonEmpty(strField(d, "label"), strField(d, "code"), id),
			Status: firstNonEmpty(strField(d, "status"), "Active"), UsedFor: strField(d, "used_for"), LibraryID: strField(d, "library_id"),
			List: AttributeListPIM,
		}
		if doctype == ItemAttributeDoctype {
			c.List, c.UsedFor = AttributeListERP, AttributeUsedForVariant
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// libraryAttributeIn finds the client's attribute for a library attribute in
// one list ("" = either).
func libraryAttributeIn(have []ClientAttribute, a LibraryAttribute, list string) *ClientAttribute {
	for i := range have {
		if (list == "" || have[i].List == list) && (have[i].LibraryID == a.ID || strings.EqualFold(have[i].Code, a.ID)) {
			return &have[i]
		}
	}
	return nil
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
	matched := map[string]bool{} // list + code
	for _, a := range LibraryAttributesFor(industry) {
		offer := LibraryOffer{LibraryAttribute: a, Use: LibraryAttributeUse(a), CoveredBy: LibraryCoveredBy(a.ID)}
		for _, v := range LibraryValues(a.ValueSet) {
			offer.Values = append(offer.Values, v.Label)
		}
		// A variant attribute "has it" in the ERP list, a product one in PIM's.
		target := AttributeListPIM
		if offer.Use == "variant" || offer.Use == "variant-candidate" {
			target = AttributeListERP
		}
		for _, list := range []string{target, AttributeListERP, AttributeListPIM} {
			if c := libraryAttributeIn(have, a, list); c != nil {
				copied := *c
				offer.Client = &copied
				break
			}
		}
		for _, list := range []string{AttributeListERP, AttributeListPIM} {
			if c := libraryAttributeIn(have, a, list); c != nil {
				matched[c.List+"|"+c.Code] = true
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
		if !matched[c.List+"|"+c.Code] {
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
	pimEnabled, _ := IsModuleEnabled(tenantID, "pim")
	res, err := applyAttributeLibraryTx(tx, schema, actor, picks, pimEnabled)
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

// applyAttributeLibraryTx: pimEnabled says whether the client has PIM - without
// it a product-information attribute has nowhere to go and is skipped, and a
// variant attribute goes to the ERP list alone.
func applyAttributeLibraryTx(tx *sql.Tx, schema, actor string, picks []AttributeLibraryPick, pimEnabled bool) (AttributeLibraryResult, error) {
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
		usedFor := strings.TrimSpace(pick.UsedFor)
		if usedFor != AttributeUsedForVariant && usedFor != AttributeUsedForProduct {
			usedFor = AttributeUsedForProduct
			if u := LibraryAttributeUse(a); u == "variant" || u == "variant-candidate" {
				usedFor = AttributeUsedForVariant
			}
		}
		// Which lists get it, minus the ones that already have it.
		toERP := usedFor == AttributeUsedForVariant && libraryAttributeIn(have, a, AttributeListERP) == nil
		toPIM := pimEnabled && libraryAttributeIn(have, a, AttributeListPIM) == nil
		if !toERP && !toPIM {
			res.Skipped = append(res.Skipped, a.ID)
			continue
		}

		valueType := LibraryValueType(a.Type)
		base := map[string]interface{}{
			"code": a.ID, "label": a.Name, "value_type": valueType, "status": "Active",
			"library_id": a.ID, "added_by": actor,
		}
		if a.UOM != "" {
			base["uom"] = a.UOM
		}
		if valueType == "Select" {
			var labels []string
			for _, v := range LibraryValues(a.ValueSet) {
				labels = append(labels, strings.ReplaceAll(v.Label, ",", " "))
			}
			base["value_options"] = strings.Join(labels, ",")
		}
		copyOf := func(extra map[string]interface{}) map[string]interface{} {
			out := map[string]interface{}{}
			for k, v := range base {
				out[k] = v
			}
			for k, v := range extra {
				out[k] = v
			}
			return out
		}
		addedAny := false
		if toERP {
			added, err := insert(itemAttributeID(a.ID), ItemAttributeDoctype, copyOf(nil))
			if err != nil {
				return res, err
			}
			addedAny = addedAny || added
		}
		if !toPIM {
			if addedAny {
				res.Added = append(res.Added, a.ID)
			} else {
				res.Skipped = append(res.Skipped, a.ID)
			}
			continue
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

		// PIM's own copy - mapped onto the ERP attribute when it is a variant.
		data := copyOf(map[string]interface{}{"used_for": usedFor})
		if groupID != "" {
			data["group"] = groupID
		}
		if usedFor == AttributeUsedForVariant {
			if toERP || libraryAttributeIn(have, a, AttributeListERP) != nil {
				data["erp_attribute"] = itemAttributeID(a.ID)
			}
		}
		added, err := insert(a.ID, "ProductAttributeDef", data)
		if err != nil {
			return res, err
		}
		if added || addedAny {
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
		WHERE doctype = 'ItemAttribute' AND deleted_at IS NULL AND COALESCE(data->>'library_id', '') <> '')`, schema)).Scan(&found)
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
		WHERE id = $1 AND doctype IN ('ItemAttribute', 'ProductAttributeDef') AND deleted_at IS NULL`, schema), id).Scan(&raw) != nil {
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
		WHERE id = $1 AND doctype IN ('ItemAttribute', 'ProductAttributeDef') AND deleted_at IS NULL`, schema), id).Scan(&libraryID)
	if err == sql.ErrNoRows || (err == nil && libraryID == "") {
		return nil
	}
	if err != nil {
		return err
	}
	return &ValidationError{Code: "PIM-0690", Message: "This attribute was set up for your business and cannot be deleted. Switch it off instead: open it and set Status to Inactive. Its values can be removed one by one."}
}
