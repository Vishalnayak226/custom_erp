package engines

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"custom_erp/db"
)

// Stage 47.13.4: stewardship of product attributes. One register entry per
// attribute (AttributeStewardship, a Master document) names its system of
// record, owner and steward, confidence, the date it takes effect, which
// source wins when two disagree, and the markets, channels and languages it
// covers. The register is changed only by a StewardshipChange request that
// goes through the maker-checker approval engine (a second person must
// approve) and is applied when approved - the same shape as 148.5's chart of
// accounts. Nothing is stewarded until an entry is approved, so with an empty
// register every write behaves exactly as before.
//
// Value provenance: ProductAttributeValue writes pass StewardshipGuard from
// ValidateProductAttributeValueDocument, the validation exit the generic
// document save, CSV and PIM imports and bulk edits already run through, and
// from the design-values writer. A stewarded attribute's value is stamped with
// its source and confidence; who and when are the row's own updated_by and
// updated_at. The one-wide-sheet import (146.5.4) writes the table directly
// and is not covered yet.
//
// Where a value came from is stamped by the door that took it (StampStewardshipSource),
// never read from the client: the document save, a bulk edit and the design-value
// writer are ERP with the user's id, a CSV import is Import.

const (
	StewardshipChangeDoctype = "StewardshipChange"
	StewardshipDoctype       = "AttributeStewardship"

	stewardInvalidCode  = "PIM-9150"
	stewardRefusedCode  = "PIM-9151"
	stewardSurvivorWins = "System of record wins"
	stewardLatestWins   = "Latest write wins"
)

var (
	stewardSources      = []string{"ERP", "Import", "Channel", "Person"}
	stewardConfidences  = []string{"High", "Medium", "Low"}
	stewardSurvivorship = []string{stewardLatestWins, stewardSurvivorWins}
)

// StewardEntry is one attribute's approved register entry.
type StewardEntry struct {
	Attribute      string `json:"attribute"`
	SystemOfRecord string `json:"system_of_record"`
	SORRef         string `json:"sor_ref"`
	DataOwner      string `json:"data_owner"`
	DataSteward    string `json:"data_steward"`
	Confidence     string `json:"confidence"`
	EffectiveFrom  string `json:"effective_from"`
	Survivorship   string `json:"survivorship"`
	Markets        string `json:"markets"`
	Channels       string `json:"channels"`
	Languages      string `json:"languages"`
	ChangeID       string `json:"change_id,omitempty"`
}

func stewardshipID(attribute string) string { return "STW::" + attribute }

func stewardEntryFrom(d map[string]interface{}) *StewardEntry {
	return &StewardEntry{
		Attribute: strField(d, "attribute"), SystemOfRecord: strField(d, "system_of_record"), SORRef: strField(d, "sor_ref"),
		DataOwner: strField(d, "data_owner"), DataSteward: strField(d, "data_steward"), Confidence: strField(d, "confidence"),
		EffectiveFrom: strField(d, "effective_from"), Survivorship: strField(d, "survivorship"),
		Markets: strField(d, "markets"), Channels: strField(d, "channels"), Languages: strField(d, "languages"),
		ChangeID: strField(d, "change_id"),
	}
}

// loadStewardship is an attribute's register entry, or nil when it has none.
func loadStewardship(tenantID, attribute string) *StewardEntry {
	e, _ := loadStewardshipEntry(tenantID, attribute)
	return e
}

// loadStewardshipEntry reads an entry: nil when the attribute has none, an
// error when the register could not be read.
func loadStewardshipEntry(tenantID, attribute string) (*StewardEntry, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	var raw []byte
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents WHERE id = $1 AND doctype = $2 AND deleted_at IS NULL`, schema),
		stewardshipID(attribute), StewardshipDoctype).Scan(&raw)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	d := map[string]interface{}{}
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, err
	}
	return stewardEntryFrom(d), nil
}

func stewardUserExists(schema, id string) bool {
	var n int
	return db.DB.QueryRow(fmt.Sprintf(`SELECT 1 FROM %s.users WHERE id = $1 AND COALESCE(status, 'Active') <> 'Inactive'`, schema), id).Scan(&n) == nil
}

func csvList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// validateStewardshipChange is the request's save check; apply runs it again.
func validateStewardshipChange(tenantID string, payload map[string]interface{}) error {
	bad := func(sub, msg string) error {
		return &ValidationError{Code: stewardInvalidCode, SubFor: sub, Message: msg}
	}
	action := firstNonEmpty(strField(payload, "action"), "Set")
	attribute := strings.TrimSpace(strField(payload, "attribute"))
	if action != "Set" && action != "Remove" {
		return bad("Action", "choose Set or Remove")
	}
	if attribute == "" {
		return bad("Attribute", "choose the attribute")
	}
	if _, _, err := fetchDocData(tenantID, "ProductAttributeDef", attribute); err != nil {
		return bad("Attribute", fmt.Sprintf("attribute %s was not found", attribute))
	}
	if action == "Remove" {
		if loadStewardship(tenantID, attribute) == nil {
			return bad("Attribute", fmt.Sprintf("%s has no stewardship entry to remove", attribute))
		}
		return nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	sor := strField(payload, "system_of_record")
	if !containsString(stewardSources, sor) {
		return bad("System of record", "choose ERP, Import, Channel or Person")
	}
	ref := strings.TrimSpace(strField(payload, "sor_ref"))
	switch sor {
	case "Channel":
		if ref == "" {
			return bad("Channel", "choose the channel that is the system of record")
		}
		if _, _, err := fetchDocData(tenantID, "Channel", ref); err != nil {
			return bad("Channel", fmt.Sprintf("channel %s was not found", ref))
		}
	case "Person":
		if ref == "" || !stewardUserExists(schema, ref) {
			return bad("Person", "choose a user as the system of record")
		}
	}
	for field, label := range map[string]string{"data_owner": "Owner", "data_steward": "Steward"} {
		if v := strings.TrimSpace(strField(payload, field)); v == "" || !stewardUserExists(schema, v) {
			return bad(label, fmt.Sprintf("choose a user as the %s", strings.ToLower(label)))
		}
	}
	if !containsString(stewardConfidences, strField(payload, "confidence")) {
		return bad("Confidence", "choose High, Medium or Low")
	}
	if !containsString(stewardSurvivorship, strField(payload, "survivorship")) {
		return bad("Survivorship", "choose Latest write wins or System of record wins")
	}
	if err := validateISODate("Effective from", strField(payload, "effective_from"), false); err != nil {
		return bad("Effective from", err.Error())
	}
	for _, ch := range csvList(strField(payload, "channels")) {
		if _, _, err := fetchDocData(tenantID, "Channel", ch); err != nil {
			return bad("Channels", fmt.Sprintf("channel %s was not found", ch))
		}
	}
	return nil
}

// validateNewStewardshipChange is the save check: one request per attribute
// may wait for approval at a time, or the second approval would quietly undo
// the first.
func validateNewStewardshipChange(tenantID string, payload map[string]interface{}) error {
	if err := validateStewardshipChange(tenantID, payload); err != nil {
		return err
	}
	return stewardshipWaiting(tenantID, strings.TrimSpace(strField(payload, "attribute")), "")
}

// stewardshipWaiting refuses a second request on an attribute while another is
// waiting for approval: two approved in either order would apply in the wrong one.
func stewardshipWaiting(tenantID, attribute, excludeID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var waiting string
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT id FROM %s.documents WHERE doctype = $1 AND deleted_at IS NULL AND status = 'Pending Approval' AND data->>'attribute' = $2 AND id <> $3 LIMIT 1`, schema),
		StewardshipChangeDoctype, attribute, excludeID).Scan(&waiting)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	return &ValidationError{Code: stewardInvalidCode, SubFor: "Attribute", Message: fmt.Sprintf("%s already has request %s waiting for approval", attribute, waiting)}
}

// StampStewardshipSource sets where a value came from. The door that took the
// write says so: a client's own claim is never believed. who is the writing
// user's id, the same id a Person system of record names.
func StampStewardshipSource(doctype string, payload map[string]interface{}, source, who string) {
	if doctype == "ProductAttributeValue" {
		payload["source"], payload["source_ref"] = source, who
	}
}

// CheckStewardshipDelete is the delete door. The register changes only by an
// approved request, and deleting a stewarded value is a write like any other.
func CheckStewardshipDelete(tenantID, doctype, id, userID string) error {
	switch doctype {
	case StewardshipDoctype:
		return stewardshipRegisterRefusal()
	case "ProductAttributeValue":
		d, _, err := fetchDocData(tenantID, doctype, id)
		if err != nil {
			return nil // nothing there to protect; the delete door answers for it
		}
		return StewardshipGuard(tenantID, map[string]interface{}{"item": strField(d, "item"), "attribute": strField(d, "attribute"),
			"locale": strField(d, "locale"), "channel": strField(d, "channel"), "value": "", "source": "ERP", "source_ref": userID})
	}
	return nil
}

// StewardshipEntryID and StewardshipValueID name the documents a door test seeds.
func StewardshipEntryID(attribute string) string { return stewardshipID(attribute) }
func StewardshipValueID(item, attribute string) string {
	return attributeValueID(item, attribute, "", "")
}

func stewardshipRegisterRefusal() error {
	return &ValidationError{Code: stewardInvalidCode, SubFor: "Register", Message: "the register changes through an approved request"}
}

// recordStewardshipApplyError keeps the reason an approved change did not apply
// on the request, so the register screen can show it.
func recordStewardshipApplyError(tenantID, docID string, applyErr error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return
	}
	if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || jsonb_build_object('apply_error', $3::text) WHERE id = $1 AND doctype = $2`, schema),
		docID, StewardshipChangeDoctype, redactAndBoundLogField(applyErr.Error(), 300)); err != nil {
		LogSystemError(tenantID, "", "ERROR", "stewardship_change", fmt.Sprintf("could not record the failed apply of %s: %v", docID, err), "")
	}
}

// applyStewardshipChange carries out an approved request once.
func applyStewardshipChange(tenantID, docID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var raw []byte
	var maker sql.NullString
	if err := tx.QueryRow(fmt.Sprintf(`SELECT data, created_by FROM %s.documents WHERE id = $1 AND doctype = $2 FOR UPDATE`, schema), docID, StewardshipChangeDoctype).Scan(&raw, &maker); err != nil {
		return err
	}
	d := map[string]interface{}{}
	if err := json.Unmarshal(raw, &d); err != nil {
		return err
	}
	if strField(d, "applied_at") != "" {
		return nil
	}
	if err := validateStewardshipChange(tenantID, d); err != nil {
		return err
	}
	attribute := strings.TrimSpace(strField(d, "attribute"))
	id := stewardshipID(attribute)
	if firstNonEmpty(strField(d, "action"), "Set") == "Remove" {
		if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET deleted_at = NOW(), updated_at = NOW() WHERE id = $1 AND doctype = $2`, schema), id, StewardshipDoctype); err != nil {
			return err
		}
	} else {
		entry := stewardEntryFrom(d)
		entry.Attribute, entry.ChangeID = attribute, docID
		body := map[string]interface{}{}
		b, _ := json.Marshal(entry)
		_ = json.Unmarshal(b, &body)
		body["code"], body["status"] = id, "Active"
		b, _ = json.Marshal(body)
		if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, 'Active', $4)
			ON CONFLICT (id) DO UPDATE SET data = EXCLUDED.data, status = 'Active', deleted_at = NULL, updated_at = NOW(), updated_by = EXCLUDED.created_by
			WHERE %s.documents.doctype = EXCLUDED.doctype`, schema, schema), id, StewardshipDoctype, b, maker); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || jsonb_build_object('applied_at', to_char(now() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')) WHERE id = $1`, schema), docID); err != nil {
		return err
	}
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data - 'apply_error' WHERE doctype = $1 AND data->>'attribute' = $2 AND data->>'apply_error' IS NOT NULL`, schema),
		StewardshipChangeDoctype, attribute); err != nil {
		return err
	}
	return tx.Commit()
}

func init() {
	// dualControl: the person who asked for a change never approves it.
	RegisterApprovalDecisionHook(StewardshipChangeDoctype, true, func(tenantID, docID, decision, actor string) {
		if decision != "Approved" {
			return
		}
		if err := applyStewardshipChange(tenantID, docID); err != nil {
			LogSystemError(tenantID, "", "ERROR", "stewardship_change", fmt.Sprintf("approved change %s not applied: %v", docID, err), "")
			recordStewardshipApplyError(tenantID, docID, err)
		}
	})
	// 47.13.4: two requests for one attribute submitted together queue here,
	// so the second reads the first as waiting and is refused.
	RegisterApprovalSubmitLock(StewardshipChangeDoctype, func(tenantID, docID string, data map[string]interface{}) string {
		return "stewardship:" + strings.TrimSpace(strField(data, "attribute"))
	})
	RegisterApprovalSubmitCheck(StewardshipChangeDoctype, func(tenantID, docID string, data map[string]interface{}) error {
		return stewardshipWaiting(tenantID, strings.TrimSpace(strField(data, "attribute")), docID)
	})
}

// stewardSourceMatches is whether a write's declared source is the entry's
// system of record: the same kind, and the same channel or person when the
// entry names one.
func stewardSourceMatches(e *StewardEntry, source, ref string) bool {
	if e.SystemOfRecord == "Person" { // the person is the source, whichever screen they use
		return strings.EqualFold(ref, e.SORRef)
	}
	if !strings.EqualFold(source, e.SystemOfRecord) {
		return false
	}
	return e.SORRef == "" || strings.EqualFold(ref, e.SORRef)
}

// StewardshipGuard runs on every ProductAttributeValue write. An attribute
// with no approved entry (or one not yet in force) is left alone. Otherwise
// the value is stamped with its source (default ERP) and the entry's
// confidence, and - when the entry says the system of record wins - a write
// from another source is refused unless it leaves the stored value as it is.
// The entry's channels and languages narrow the rule to the variants it covers.
func StewardshipGuard(tenantID string, payload map[string]interface{}) error {
	attribute := strField(payload, "attribute")
	if attribute == "" {
		return nil
	}
	e, err := loadStewardshipEntry(tenantID, attribute)
	if err != nil { // no write gets past a register that cannot be read
		return err
	}
	if e == nil || (e.EffectiveFrom != "" && BusinessToday() < e.EffectiveFrom) {
		return nil
	}
	source := firstNonEmpty(strField(payload, "source"), "ERP")
	if !containsString(stewardSources, source) {
		return &ValidationError{Code: stewardRefusedCode, SubFor: "Source", Message: "the source of a value is ERP, Import, Channel or Person"}
	}
	payload["source"] = source
	if strField(payload, "confidence") == "" {
		payload["confidence"] = e.Confidence
	}
	if e.Survivorship != stewardSurvivorWins {
		return nil
	}
	if ch := strField(payload, "channel"); ch != "" && len(csvList(e.Channels)) > 0 && !containsString(csvList(e.Channels), ch) {
		return nil
	}
	if lang := strField(payload, "locale"); lang != "" && len(csvList(e.Languages)) > 0 && !containsString(csvList(e.Languages), lang) {
		return nil
	}
	if stewardSourceMatches(e, source, strField(payload, "source_ref")) {
		return nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var stored string
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(data->>'value','') FROM %s.documents WHERE doctype = 'ProductAttributeValue' AND id = $1 AND deleted_at IS NULL`, schema),
		attributeValueID(strField(payload, "item"), attribute, strField(payload, "locale"), strField(payload, "channel"))).Scan(&stored)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if strings.TrimSpace(stored) == strings.TrimSpace(pimString(payload["value"])) {
		return nil
	}
	who := e.SystemOfRecord
	if e.SORRef != "" {
		who += " " + e.SORRef
	}
	return &ValidationError{Code: stewardRefusedCode, SubFor: "Source", Message: fmt.Sprintf("%s takes its value from %s - write it from there, or ask for the entry to change", attribute, who)}
}

// ---- the register screen ---------------------------------------------------

// StewardshipRow is one attribute on the register list.
type StewardshipRow struct {
	Attribute      string        `json:"attribute"`
	Label          string        `json:"label"`
	Entry          *StewardEntry `json:"entry,omitempty"`
	Pending        string        `json:"pending,omitempty"`
	ApplyError     string        `json:"apply_error,omitempty"` // an approved request that did not apply
	ItemsWithValue int           `json:"items_with_value"`
	Required       int           `json:"required"`
	Filled         int           `json:"filled"`
}

// StewardshipOverview lists every attribute with its entry, a request still
// waiting for approval, and the family completeness figures: how many items
// need the attribute (it is mandatory in their family) and how many have it.
func StewardshipOverview(tenantID string) ([]StewardshipRow, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, COALESCE(data->>'label', id) FROM %s.documents WHERE doctype = 'ProductAttributeDef' AND deleted_at IS NULL ORDER BY id`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StewardshipRow
	idx := map[string]int{}
	for rows.Next() {
		var r StewardshipRow
		if err := rows.Scan(&r.Attribute, &r.Label); err != nil {
			return nil, err
		}
		idx[r.Attribute] = len(out)
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	each := func(query string, fn func(attr string, a, b int)) error {
		rs, err := db.DB.Query(query)
		if err != nil {
			return err
		}
		defer rs.Close()
		for rs.Next() {
			var attr string
			var a, b int
			if err := rs.Scan(&attr, &a, &b); err != nil {
				return err
			}
			fn(attr, a, b)
		}
		return rs.Err()
	}
	if err := each(fmt.Sprintf(`SELECT data->>'attribute', COUNT(DISTINCT data->>'item')::int, 0 FROM %s.documents
		WHERE doctype = 'ProductAttributeValue' AND deleted_at IS NULL AND COALESCE(status, 'Active') = 'Active' AND BTRIM(COALESCE(data->>'value', '')) <> ''
		GROUP BY 1`, schema), func(attr string, n, _ int) {
		if i, ok := idx[attr]; ok {
			out[i].ItemsWithValue = n
		}
	}); err != nil {
		return nil, err
	}
	if err := each(fmt.Sprintf(`SELECT fa.data->>'attribute', COUNT(DISTINCT i.id)::int,
			COUNT(DISTINCT i.id) FILTER (WHERE EXISTS (SELECT 1 FROM %[1]s.documents v WHERE v.doctype = 'ProductAttributeValue' AND v.deleted_at IS NULL
				AND COALESCE(v.status, 'Active') = 'Active' AND v.data->>'item' = i.id AND v.data->>'attribute' = fa.data->>'attribute'
				AND BTRIM(COALESCE(v.data->>'value', '')) <> ''))::int
		FROM %[1]s.documents fa JOIN %[1]s.documents i ON i.doctype = 'Item' AND i.deleted_at IS NULL AND i.data->>'family' = fa.data->>'family'
		WHERE fa.doctype = 'ProductFamilyAttribute' AND fa.deleted_at IS NULL AND fa.data->>'mandatory' = 'Yes'
		GROUP BY 1`, schema), func(attr string, req, filled int) {
		if i, ok := idx[attr]; ok {
			out[i].Required, out[i].Filled = req, filled
		}
	}); err != nil {
		return nil, err
	}
	er, err := db.DB.Query(fmt.Sprintf(`SELECT data FROM %s.documents WHERE doctype = $1 AND deleted_at IS NULL`, schema), StewardshipDoctype)
	if err != nil {
		return nil, err
	}
	for er.Next() {
		var raw []byte
		if er.Scan(&raw) != nil {
			continue
		}
		d := map[string]interface{}{}
		if json.Unmarshal(raw, &d) == nil {
			if i, ok := idx[strField(d, "attribute")]; ok {
				out[i].Entry = stewardEntryFrom(d)
			}
		}
	}
	er.Close()
	ae, err := db.DB.Query(fmt.Sprintf(`SELECT data->>'attribute', data->>'apply_error' FROM %s.documents WHERE doctype = $1 AND deleted_at IS NULL AND data->>'apply_error' IS NOT NULL`, schema), StewardshipChangeDoctype)
	if err != nil {
		return nil, err
	}
	for ae.Next() {
		var attr, why string
		if ae.Scan(&attr, &why) == nil {
			if i, ok := idx[attr]; ok {
				out[i].ApplyError = why
			}
		}
	}
	ae.Close()
	pr, err := db.DB.Query(fmt.Sprintf(`SELECT data->>'attribute', id FROM %s.documents WHERE doctype = $1 AND deleted_at IS NULL AND status = 'Pending Approval'`, schema), StewardshipChangeDoctype)
	if err != nil {
		return nil, err
	}
	defer pr.Close()
	for pr.Next() {
		var attr, id string
		if pr.Scan(&attr, &id) == nil {
			if i, ok := idx[attr]; ok {
				out[i].Pending = id
			}
		}
	}
	return out, pr.Err()
}

// StewardConsumer is something that reads an attribute.
type StewardConsumer struct {
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Detail string `json:"detail,omitempty"`
	Items  int    `json:"items"`
}

// StewardWrite is one recent value of a stewarded attribute and where it came from.
type StewardWrite struct {
	Item       string `json:"item"`
	Value      string `json:"value"`
	Source     string `json:"source"`
	SourceRef  string `json:"source_ref,omitempty"`
	Confidence string `json:"confidence"`
	By         string `json:"by"`
	At         string `json:"at"`
}

// StewardDetail is the read-only panel for one attribute: what consumes it,
// how many items would change, and its latest writes.
type StewardDetail struct {
	Attribute      string            `json:"attribute"`
	ItemsWithValue int               `json:"items_with_value"`
	Consumers      []StewardConsumer `json:"consumers"`
	Writes         []StewardWrite    `json:"writes"`
}

// StewardshipDetail reads the downstream impact of an attribute. A consumer's
// item count is the number of items that carry a value for the attribute, the
// items a change to it reaches.
func StewardshipDetail(tenantID, attribute string) (*StewardDetail, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	d := &StewardDetail{Attribute: attribute, Consumers: []StewardConsumer{}, Writes: []StewardWrite{}}
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(DISTINCT data->>'item')::int FROM %s.documents WHERE doctype = 'ProductAttributeValue' AND deleted_at IS NULL
		AND COALESCE(status, 'Active') = 'Active' AND data->>'attribute' = $1 AND BTRIM(COALESCE(data->>'value', '')) <> ''`, schema), attribute).Scan(&d.ItemsWithValue)

	// Channel feeds and publish: field maps that read the attribute.
	targets := map[string][]string{} // channel -> target fields
	fm, err := db.DB.Query(fmt.Sprintf(`SELECT COALESCE(data->>'channel', ''), COALESCE(data->>'target_field', ''), COALESCE(data->>'mandatory', 'No') FROM %s.documents
		WHERE doctype = 'ChannelFieldMap' AND deleted_at IS NULL AND LOWER(data->>'source_field') = LOWER($1) ORDER BY 1, 2`, schema), attribute)
	if err != nil {
		return nil, err
	}
	for fm.Next() {
		var ch, target, mandatory string
		if fm.Scan(&ch, &target, &mandatory) != nil {
			continue
		}
		targets[ch] = append(targets[ch], target)
		detail := "feed column " + target
		if mandatory == "Yes" {
			detail += ", required to publish"
		}
		d.Consumers = append(d.Consumers, StewardConsumer{Kind: "Channel feed", Name: ch, Detail: detail, Items: d.ItemsWithValue})
	}
	fm.Close()

	// Export templates whose columns carry one of those feed fields.
	if len(targets) > 0 {
		tr, err := db.DB.Query(fmt.Sprintf(`SELECT id, COALESCE(data->>'channel', ''), data->'column_mappings' FROM %s.documents WHERE doctype = 'PIMExportTemplate' AND deleted_at IS NULL ORDER BY id`, schema))
		if err != nil {
			return nil, err
		}
		for tr.Next() {
			var id, ch string
			var cols []byte
			if tr.Scan(&id, &ch, &cols) != nil {
				continue
			}
			var parsed interface{}
			if json.Unmarshal(cols, &parsed) != nil {
				continue
			}
			list, _ := decodePIMExportColumns(parsed)
			for _, c := range list {
				if containsString(targets[ch], c.FieldKey) {
					d.Consumers = append(d.Consumers, StewardConsumer{Kind: "Export template", Name: id, Detail: "column " + c.FieldKey + " on " + ch, Items: d.ItemsWithValue})
					break
				}
			}
		}
		tr.Close()
	}

	// Family completeness: families that need the attribute.
	fr, err := db.DB.Query(fmt.Sprintf(`SELECT fa.data->>'family', COALESCE(fa.data->>'mandatory', 'No'),
			(SELECT COUNT(*)::int FROM %[1]s.documents i WHERE i.doctype = 'Item' AND i.deleted_at IS NULL AND i.data->>'family' = fa.data->>'family')
		FROM %[1]s.documents fa WHERE fa.doctype = 'ProductFamilyAttribute' AND fa.deleted_at IS NULL AND fa.data->>'attribute' = $1 ORDER BY 1`, schema), attribute)
	if err != nil {
		return nil, err
	}
	for fr.Next() {
		var fam, mandatory string
		var n int
		if fr.Scan(&fam, &mandatory, &n) != nil {
			continue
		}
		detail := "optional"
		if mandatory == "Yes" {
			detail = "required for completeness"
		}
		d.Consumers = append(d.Consumers, StewardConsumer{Kind: "Family", Name: fam, Detail: detail, Items: n})
	}
	fr.Close()

	// Stickers: label layouts that print the attribute.
	sr, err := db.DB.Query(fmt.Sprintf(`SELECT id FROM %s.documents WHERE doctype = 'StickerTemplate' AND deleted_at IS NULL
		AND POSITION(LOWER($1) IN LOWER(COALESCE(data->>'elements', ''))) > 0 ORDER BY id`, schema), attribute)
	if err == nil {
		for sr.Next() {
			var id string
			if sr.Scan(&id) == nil {
				d.Consumers = append(d.Consumers, StewardConsumer{Kind: "Sticker", Name: id, Detail: "label layout", Items: d.ItemsWithValue})
			}
		}
		sr.Close()
	}

	// Reports: the attribute quality report reads every value.
	var blank int
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*)::int FROM %s.documents WHERE doctype = 'ProductAttributeValue' AND deleted_at IS NULL AND data->>'attribute' = $1
		AND (BTRIM(COALESCE(data->>'value','')) = '' OR status <> 'Active')`, schema), attribute).Scan(&blank)
	d.Consumers = append(d.Consumers, StewardConsumer{Kind: "Report", Name: "Attribute quality", Detail: "values that are blank or inactive", Items: blank})

	wr, err := db.DB.Query(fmt.Sprintf(`SELECT COALESCE(data->>'item', ''), COALESCE(data->>'value', ''), COALESCE(data->>'source', ''), COALESCE(data->>'source_ref', ''),
			COALESCE(data->>'confidence', ''), COALESCE(updated_by, created_by, ''), to_char(COALESCE(updated_at, created_at), 'YYYY-MM-DD HH24:MI')
		FROM %s.documents WHERE doctype = 'ProductAttributeValue' AND deleted_at IS NULL AND data->>'attribute' = $1
		ORDER BY COALESCE(updated_at, created_at) DESC LIMIT 20`, schema), attribute)
	if err != nil {
		return nil, err
	}
	defer wr.Close()
	for wr.Next() {
		var w StewardWrite
		if wr.Scan(&w.Item, &w.Value, &w.Source, &w.SourceRef, &w.Confidence, &w.By, &w.At) == nil {
			d.Writes = append(d.Writes, w)
		}
	}
	return d, wr.Err()
}

// StewardLookups are the choices the register form offers: active users and channels.
type StewardLookups struct {
	Users    []map[string]string `json:"users"`
	Channels []string            `json:"channels"`
}

func StewardshipLookups(tenantID string) (*StewardLookups, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	out := &StewardLookups{Users: []map[string]string{}, Channels: []string{}}
	ur, err := db.DB.Query(fmt.Sprintf(`SELECT id, COALESCE(username, id) FROM %s.users WHERE COALESCE(status, 'Active') <> 'Inactive' ORDER BY 2`, schema))
	if err != nil {
		return nil, err
	}
	for ur.Next() {
		var id, name string
		if ur.Scan(&id, &name) == nil {
			out.Users = append(out.Users, map[string]string{"id": id, "name": name})
		}
	}
	ur.Close()
	cr, err := db.DB.Query(fmt.Sprintf(`SELECT id FROM %s.documents WHERE doctype = 'Channel' AND deleted_at IS NULL ORDER BY id`, schema))
	if err != nil {
		return nil, err
	}
	defer cr.Close()
	for cr.Next() {
		var id string
		if cr.Scan(&id) == nil {
			out.Channels = append(out.Channels, id)
		}
	}
	return out, cr.Err()
}
