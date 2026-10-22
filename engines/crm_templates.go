package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// Stage 101.3.4 - message templates (the blueprint's M11). A template is
// versioned per channel and language: saving a changed text makes a new
// version, and a campaign or journey keeps pointing at the exact version it
// was approved with. Placeholders are {{name}} or {{name|fallback}}; a
// placeholder with no fallback is required, and a message whose required
// value is blank is suppressed as TEMPLATE_DATA_MISSING rather than sent with
// a gap ("Dear , your  is ready").

// CRMTemplateVariables are the placeholders the CRM can fill, with a sample
// for the preview.
var CRMTemplateVariables = []struct{ Name, Label, Sample string }{
	{"customer.first_name", "Customer's first name", "Asha"},
	{"customer.name", "Customer's full name", "Asha Rao"},
	{"customer.points", "Points they can use", "320"},
	{"customer.tier", "Loyalty tier", "Gold"},
	{"business.name", "Your business name", "Seznik"},
	{"store.name", "Store", "Seznik Indiranagar"},
	{"bill.number", "Bill number", "HO/26-27/00123"},
	{"bill.amount", "Bill amount", "2,499.00"},
	{"bill.date", "Bill date", "22 Oct"},
	{"item.name", "Item", "Linen shirt - Blue / M"},
	{"item.price", "Item price", "1,299.00"},
	{"item.link", "Item link", "https://example.com/i/123"},
	{"voucher.code", "Voucher code", "SZ7KQ2M9XP"},
	{"voucher.value", "Voucher value", "10% off"},
	{"voucher.expiry", "Voucher expiry", "30 Nov"},
	{"points.expiring", "Points about to expire", "150"},
	{"points.expiry_date", "When they expire", "15 Nov"},
	{"appointment.time", "Appointment time", "Sat 25 Oct, 4:30 pm"},
	{"appointment.store", "Appointment store", "Seznik Indiranagar"},
	{"referral.code", "Their referral code", "RFAS7K2Q"},
	{"link.preferences", "Preference / unsubscribe link", "https://minn.wholeops.in/p/?t=..."},
	{"link.feedback", "Feedback link", "https://minn.wholeops.in/f/?t=..."},
	{"message.text", "Free text typed by staff", "Your alteration is ready."},
}

var crmPlaceholderRe = regexp.MustCompile(`\{\{\s*([a-z][a-z0-9_]*(?:\.[a-z0-9_]+)?)\s*(?:\|([^}]*))?\}\}`)

// CRMTemplateVar is one placeholder a version uses.
type CRMTemplateVar struct {
	Name     string `json:"name"`
	Required bool   `json:"required"`
	Fallback string `json:"fallback,omitempty"`
}

// CRMTemplateVersion is one frozen text.
type CRMTemplateVersion struct {
	Version        int              `json:"version"`
	Subject        string           `json:"subject,omitempty"`
	Body           string           `json:"body"`
	Variables      []CRMTemplateVar `json:"variables"`
	DLTTemplateID  string           `json:"dlt_template_id,omitempty"`
	WATemplateName string           `json:"wa_template_name,omitempty"`
	WALanguage     string           `json:"wa_language,omitempty"`
	WAStatus       string           `json:"wa_status,omitempty"`
	CreatedBy      string           `json:"created_by,omitempty"`
	CreatedAt      time.Time        `json:"created_at"`
	SMSParts       int              `json:"sms_parts,omitempty"`
	SMSUnicode     bool             `json:"sms_unicode,omitempty"`
}

// CRMTemplate is a template and its current version.
type CRMTemplate struct {
	ID             string              `json:"id"`
	Name           string              `json:"name"`
	Channel        string              `json:"channel"`
	Language       string              `json:"language"`
	Purpose        string              `json:"purpose"`
	Status         string              `json:"status"`
	CurrentVersion int                 `json:"current_version"`
	Current        *CRMTemplateVersion `json:"current,omitempty"`
	UpdatedAt      time.Time           `json:"updated_at"`
	Ready          bool                `json:"ready"`
	NotReady       string              `json:"not_ready,omitempty"`
}

// CRMTemplateInput is a save from the screen.
type CRMTemplateInput struct {
	Name           string `json:"name"`
	Channel        string `json:"channel"`
	Language       string `json:"language"`
	Purpose        string `json:"purpose"`
	Subject        string `json:"subject"`
	Body           string `json:"body"`
	DLTTemplateID  string `json:"dlt_template_id"`
	WATemplateName string `json:"wa_template_name"`
	WALanguage     string `json:"wa_language"`
	Status         string `json:"status"`
}

func crmKnownVariable(name string) bool {
	for _, v := range CRMTemplateVariables {
		if v.Name == name {
			return true
		}
	}
	return false
}

// crmTemplateVars lists a text's placeholders, first appearance first.
func crmTemplateVars(texts ...string) ([]CRMTemplateVar, error) {
	seen := map[string]bool{}
	var out []CRMTemplateVar
	for _, t := range texts {
		for _, m := range crmPlaceholderRe.FindAllStringSubmatch(t, -1) {
			name := m[1]
			if !crmKnownVariable(name) {
				return nil, crmErr(CRMCodeTemplate, "{{%s}} is not a value the CRM can fill - pick one from the list", name)
			}
			if seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, CRMTemplateVar{Name: name, Required: m[2] == "" && !strings.Contains(m[0], "|"), Fallback: strings.TrimSpace(m[2])})
		}
	}
	if strings.Count(strings.Join(texts, ""), "{{") != strings.Count(strings.Join(texts, ""), "}}") {
		return nil, crmErr(CRMCodeTemplate, "a {{ placeholder }} is not closed")
	}
	return out, nil
}

// crmSMSParts counts billable SMS parts: 160 characters (153 per part when
// split) in the GSM alphabet, 70 (67) once any other character appears.
func crmSMSParts(text string) (parts int, unicode bool) {
	const gsm = "@£$¥èéùìòÇ\nØø\rÅåΔ_ΦΓΛΩΠΨΣΘΞÆæßÉ !\"#¤%&'()*+,-./0123456789:;<=>?¡ABCDEFGHIJKLMNOPQRSTUVWXYZÄÖÑÜ§¿abcdefghijklmnopqrstuvwxyzäöñüà"
	const gsmExt = "^{}\\[~]|€"
	units := 0
	for _, r := range text {
		switch {
		case strings.ContainsRune(gsm, r):
			units++
		case strings.ContainsRune(gsmExt, r):
			units += 2
		default:
			unicode = true
		}
	}
	if unicode {
		n := utf8.RuneCountInString(text)
		if n <= 70 {
			return 1, true
		}
		return (n + 66) / 67, true
	}
	if units <= 160 {
		return 1, false
	}
	return (units + 152) / 153, false
}

// SaveCRMTemplate creates a template (id "") or saves a new version of one.
// A save that changes no text keeps the current version (only the name,
// status or approval fields change).
func SaveCRMTemplate(tenantID, id string, in CRMTemplateInput, actor string) (*CRMTemplate, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	in.Name, in.Body, in.Subject = strings.TrimSpace(in.Name), strings.TrimSpace(in.Body), strings.TrimSpace(in.Subject)
	channel := crmChannel(in.Channel)
	purpose := crmPurpose(in.Purpose)
	if in.Name == "" || channel == "" || purpose == "" {
		return nil, crmErr(CRMCodeTemplate, "a template needs a name, a channel (SMS, WhatsApp or Email) and a purpose")
	}
	if in.Body == "" {
		return nil, crmErr(CRMCodeTemplate, "write the message")
	}
	if channel == CRMChannelEmail && in.Subject == "" {
		return nil, crmErr(CRMCodeTemplate, "an email needs a subject")
	}
	if channel != CRMChannelEmail {
		in.Subject = ""
	}
	if len([]rune(in.Body)) > 4000 {
		return nil, crmErr(CRMCodeTemplate, "the message is longer than 4,000 characters")
	}
	vars, err := crmTemplateVars(in.Subject, in.Body)
	if err != nil {
		return nil, err
	}
	lang := strings.TrimSpace(in.Language)
	if lang == "" {
		lang = "en"
	}
	status := "Active"
	if strings.EqualFold(in.Status, "Inactive") {
		status = "Inactive"
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if id == "" {
		id = NewDocID("TPL")
		if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.crm_templates (id, name, channel, language, purpose, status, created_by) VALUES ($1, $2, $3, $4, $5, $6, $7)`, schema),
			id, in.Name, channel, lang, purpose, status, actor); err != nil {
			return nil, err
		}
	} else {
		var curChannel string
		err := tx.QueryRow(fmt.Sprintf(`SELECT channel FROM %s.crm_templates WHERE id = $1 FOR UPDATE`, schema), id).Scan(&curChannel)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, crmNotFound("template", id)
		}
		if err != nil {
			return nil, err
		}
		if curChannel != channel {
			return nil, crmErr(CRMCodeTemplate, "a template's channel cannot change - make a new template for %s", channel)
		}
		if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.crm_templates SET name = $2, language = $3, purpose = $4, status = $5, updated_at = NOW() WHERE id = $1`, schema),
			id, in.Name, lang, purpose, status); err != nil {
			return nil, err
		}
	}
	var cur CRMTemplateVersion
	var curVersion int
	err = tx.QueryRow(fmt.Sprintf(`SELECT t.current_version, COALESCE(v.subject, ''), COALESCE(v.body, ''), COALESCE(v.dlt_template_id, ''), COALESCE(v.wa_template_name, ''), COALESCE(v.wa_language, '')
		FROM %s.crm_templates t LEFT JOIN %s.crm_template_versions v ON v.template_id = t.id AND v.version = t.current_version WHERE t.id = $1`, schema, schema), id).
		Scan(&curVersion, &cur.Subject, &cur.Body, &cur.DLTTemplateID, &cur.WATemplateName, &cur.WALanguage)
	if err != nil {
		return nil, err
	}
	changed := curVersion == 0 || cur.Subject != in.Subject || cur.Body != in.Body || cur.DLTTemplateID != strings.TrimSpace(in.DLTTemplateID) ||
		cur.WATemplateName != strings.TrimSpace(in.WATemplateName) || cur.WALanguage != strings.TrimSpace(in.WALanguage)
	if changed {
		next := curVersion + 1
		waStatus := ""
		if channel == CRMChannelWhatsApp && strings.TrimSpace(in.WATemplateName) != "" {
			waStatus = "Not synced"
		}
		if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.crm_template_versions (template_id, version, subject, body, variables, dlt_template_id, wa_template_name, wa_language, wa_status, created_by)
			VALUES ($1, $2, NULLIF($3, ''), $4, $5, NULLIF($6, ''), NULLIF($7, ''), NULLIF($8, ''), NULLIF($9, ''), $10)`, schema),
			id, next, in.Subject, in.Body, crmJSON(vars), strings.TrimSpace(in.DLTTemplateID), strings.TrimSpace(in.WATemplateName), strings.TrimSpace(in.WALanguage), waStatus, actor); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.crm_templates SET current_version = $2, updated_at = NOW() WHERE id = $1`, schema), id, next); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return GetCRMTemplate(tenantID, id)
}

func crmTemplateReady(t *CRMTemplate) (bool, string) {
	if t.Status != "Active" {
		return false, "switched off"
	}
	if t.Current == nil {
		return false, "no text yet"
	}
	switch t.Channel {
	case CRMChannelSMS:
		if t.Current.DLTTemplateID == "" {
			return false, "needs its DLT template ID before it can go to Indian numbers"
		}
	case CRMChannelWhatsApp:
		if t.Current.WATemplateName == "" {
			return t.Purpose == CRMPurposeService, "a WhatsApp template name is needed outside a customer's 24-hour reply window"
		}
		if !strings.EqualFold(t.Current.WAStatus, "APPROVED") {
			return false, "WhatsApp has not approved this template yet (" + firstNonEmpty(t.Current.WAStatus, "not synced") + ")"
		}
	}
	return true, ""
}

// GetCRMTemplate returns one template with its current version.
func GetCRMTemplate(tenantID, id string) (*CRMTemplate, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	return crmGetTemplate(schema, id)
}

func crmGetTemplate(schema, id string) (*CRMTemplate, error) {
	var t CRMTemplate
	err := db.DB.QueryRow(fmt.Sprintf(`SELECT id, name, channel, language, purpose, status, current_version, updated_at FROM %s.crm_templates WHERE id = $1`, schema), id).
		Scan(&t.ID, &t.Name, &t.Channel, &t.Language, &t.Purpose, &t.Status, &t.CurrentVersion, &t.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, crmNotFound("template", id)
	}
	if err != nil {
		return nil, err
	}
	if t.CurrentVersion > 0 {
		v, err := crmTemplateVersion(schema, id, t.CurrentVersion)
		if err != nil {
			return nil, err
		}
		t.Current = v
	}
	t.Ready, t.NotReady = crmTemplateReady(&t)
	return &t, nil
}

func crmTemplateVersion(schema, id string, version int) (*CRMTemplateVersion, error) {
	var v CRMTemplateVersion
	var vars []byte
	var waAt sql.NullTime
	err := db.DB.QueryRow(fmt.Sprintf(`SELECT version, COALESCE(subject, ''), body, variables, COALESCE(dlt_template_id, ''), COALESCE(wa_template_name, ''),
		COALESCE(wa_language, ''), COALESCE(wa_status, ''), wa_status_at, COALESCE(created_by, ''), created_at
		FROM %s.crm_template_versions WHERE template_id = $1 AND version = $2`, schema), id, version).
		Scan(&v.Version, &v.Subject, &v.Body, &vars, &v.DLTTemplateID, &v.WATemplateName, &v.WALanguage, &v.WAStatus, &waAt, &v.CreatedBy, &v.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, crmNotFound("template version", fmt.Sprintf("%s v%d", id, version))
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(vars, &v.Variables)
	v.SMSParts, v.SMSUnicode = crmSMSParts(v.Body)
	return &v, nil
}

// ListCRMTemplates lists templates, optionally of one channel.
func ListCRMTemplates(tenantID, channel string) ([]CRMTemplate, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id FROM %s.crm_templates WHERE ($1 = '' OR channel = $1) ORDER BY status, name`, schema), crmChannel(channel))
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	out := []CRMTemplate{}
	for _, id := range ids {
		t, err := crmGetTemplate(schema, id)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, nil
}

// ListCRMTemplateVersions lists every version of a template, newest first.
func ListCRMTemplateVersions(tenantID, id string) ([]CRMTemplateVersion, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT version FROM %s.crm_template_versions WHERE template_id = $1 ORDER BY version DESC`, schema), id)
	if err != nil {
		return nil, err
	}
	var versions []int
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err == nil {
			versions = append(versions, v)
		}
	}
	rows.Close()
	out := []CRMTemplateVersion{}
	for _, n := range versions {
		v, err := crmTemplateVersion(schema, id, n)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, nil
}

// SetCRMTemplateWAStatus records WhatsApp's approval of one version (from a
// sync or the provider's template-status callback).
func setCRMTemplateWAStatus(schema, id string, version int, status string) error {
	_, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.crm_template_versions SET wa_status = $3, wa_status_at = NOW() WHERE template_id = $1 AND version = $2`, schema),
		id, version, strings.ToUpper(strings.TrimSpace(status)))
	return err
}

// crmRendered is a message after its placeholders were filled.
type crmRendered struct {
	Subject string
	Body    string
	// Params are the filled values in order of first appearance - a
	// WhatsApp template's {{1}}, {{2}} ...
	Params  []string
	Missing []string
	Parts   int
	Unicode bool
}

// crmRender fills a version's placeholders from vals. A required value that
// is blank is listed in Missing; the caller suppresses the message.
func crmRender(v *CRMTemplateVersion, vals map[string]string) crmRendered {
	r := crmRendered{}
	params := map[string]string{}
	fill := func(text string) string {
		return crmPlaceholderRe.ReplaceAllStringFunc(text, func(m string) string {
			sub := crmPlaceholderRe.FindStringSubmatch(m)
			name, fallback := sub[1], strings.TrimSpace(sub[2])
			val := strings.TrimSpace(vals[name])
			if val == "" {
				val = fallback
			}
			if val == "" && !strings.Contains(m, "|") {
				r.Missing = append(r.Missing, name)
			}
			if _, ok := params[name]; !ok {
				params[name] = val
			}
			return val
		})
	}
	r.Subject = fill(v.Subject)
	r.Body = fill(v.Body)
	for _, tv := range v.Variables {
		r.Params = append(r.Params, params[tv.Name])
	}
	sort.Strings(r.Missing)
	r.Missing = crmUnique(r.Missing)
	r.Parts, r.Unicode = crmSMSParts(r.Body)
	return r
}

func crmUnique(in []string) []string {
	out := in[:0]
	var last string
	for i, s := range in {
		if i == 0 || s != last {
			out = append(out, s)
		}
		last = s
	}
	return out
}

// CRMTemplatePreview is a template rendered for one customer, or with the
// sample values when none is chosen.
type CRMTemplatePreview struct {
	Subject string   `json:"subject,omitempty"`
	Body    string   `json:"body"`
	Missing []string `json:"missing"`
	Parts   int      `json:"sms_parts"`
	Unicode bool     `json:"sms_unicode"`
	Sample  bool     `json:"sample"`
}

// PreviewCRMTemplate renders a template's version (0 = current) for a
// customer, or with samples.
func PreviewCRMTemplate(tenantID, id string, version int, customerID string) (*CRMTemplatePreview, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	t, err := crmGetTemplate(schema, id)
	if err != nil {
		return nil, err
	}
	if version <= 0 {
		version = t.CurrentVersion
	}
	v, err := crmTemplateVersion(schema, id, version)
	if err != nil {
		return nil, err
	}
	vals := map[string]string{}
	sample := strings.TrimSpace(customerID) == ""
	if sample {
		for _, sv := range CRMTemplateVariables {
			vals[sv.Name] = sv.Sample
		}
	} else {
		c, err := crmLoadCustomer(schema, customerID)
		if err != nil {
			return nil, crmNotFound("customer", customerID)
		}
		vals = crmCustomerVars(schema, c)
		vals["link.preferences"] = crmPublicLink(schema, "/p/", "PREVIEW")
	}
	r := crmRender(v, vals)
	return &CRMTemplatePreview{Subject: r.Subject, Body: r.Body, Missing: append([]string{}, r.Missing...), Parts: r.Parts, Unicode: r.Unicode, Sample: sample}, nil
}

// crmCustomerVars are the customer.* and business values for a customer.
func crmCustomerVars(schema string, c crmCustomer) map[string]string {
	vals := map[string]string{
		"customer.name": c.Name(),
		"customer.tier": strField(c.Data, "loyalty_tier"),
		"business.name": crmBusinessName(schema),
		"referral.code": "",
	}
	if f := strings.Fields(c.Name()); len(f) > 0 {
		vals["customer.first_name"] = f[0]
	}
	var bal int
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(SUM(CASE WHEN transaction_type = 'Earn' THEN points ELSE -points END), 0) FROM %s.loyalty_point_ledger
		WHERE customer_id = $1 AND `+crmUsablePointsSQL, schema), c.ID).Scan(&bal); err == nil {
		vals["customer.points"] = fmt.Sprintf("%d", bal)
	}
	store := firstNonEmpty(strField(c.Data, "preferred_store"))
	if store == "" {
		_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(data->>'location', '') FROM %s.documents WHERE doctype = 'POSCart' AND status = 'Paid' AND data->>'customer_id' = $1 ORDER BY created_at DESC LIMIT 1`, schema), c.ID).Scan(&store)
	}
	if store != "" {
		vals["store.name"] = crmStoreName(schema, store)
	}
	var code string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT code FROM %s.crm_referral_codes WHERE customer_id = $1`, schema), c.ID).Scan(&code); err == nil {
		vals["referral.code"] = code
	}
	return vals
}

// crmStoreName is a store's name (Location), else its code.
func crmStoreName(schema, code string) string {
	var name string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(NULLIF(data->>'name', ''), NULLIF(data->>'location_name', ''), $1) FROM %s.documents
		WHERE doctype = 'Location' AND deleted_at IS NULL AND (id = $1 OR data->>'code' = $1) LIMIT 1`, schema), code).Scan(&name); err != nil {
		return code
	}
	return name
}
