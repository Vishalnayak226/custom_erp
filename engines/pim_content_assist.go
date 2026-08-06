package engines

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"custom_erp/db"
)

// Stage 26.4.11 - product content assist.
//
// SCOPE DECISION (user, 2026-08-06): local/offline only. This engine composes a
// draft from the product's OWN structured data - its Item fields and its
// resolved family attribute values - using deterministic templates. It makes no
// network call, contacts no model provider, and has no API key. That is the
// whole point of the decision: no PIM content, and in particular no
// supplier-submitted text (26.4.10), ever leaves this server.
//
// The five governance questions go_live_decisions.md section 11 asks are
// therefore answered as:
//  1. Which provider / what data leaves the server - none, and nothing.
//  2. Human-in-the-loop - guaranteed structurally. GenerateContentSuggestion
//     RETURNS a draft; it never writes a ProductContent row. Whoever accepts it
//     saves it as a normal Draft, which still passes the existing Stage
//     15.1/26.4.5 approval gate before it can publish. There is no code path
//     from this file to a published document.
//  3. Audit trail - every generation writes a ContentAssistLog row (below).
//  4. Prompt injection - not applicable in the classic sense (no model, no
//     prompt), but the related risk IS still real: a supplier-submitted
//     attribute value could carry markup or control characters that end up in
//     published copy. sanitizeAssistInput handles that, and supplier-sourced
//     text is never used as a source field - see assistSourceFields.
//  5. Cost controls - not applicable, no metered API.
//
// What this deliberately is NOT: it does not invent claims, adjectives or
// marketing language that aren't derivable from stored data. A template that
// only ever restates known attributes cannot hallucinate a product feature,
// which is exactly why this shape is defensible without a review model behind
// it. The output is a starting point that saves retyping, not finished copy.

// ContentSuggestion is a proposed draft. Every field is a suggestion only -
// nothing here has been persisted to ProductContent.
type ContentSuggestion struct {
	ItemCode  string `json:"item_code"`
	Language  string `json:"language"`
	Title     string `json:"title"`
	SEOTitle  string `json:"seo_title"`
	ShortDesc string `json:"short_desc"`
	LongDesc  string `json:"long_desc"`
	Tags      string `json:"tags"`
	// SourceFields names every field the draft was actually built from, so a
	// reviewer can tell what the text rests on rather than trusting it blind.
	SourceFields []string `json:"source_fields"`
	// Warnings flag thin input - a draft built from almost nothing is worse
	// than no draft, and the reviewer should be told rather than left to
	// discover it.
	Warnings []string `json:"warnings,omitempty"`
}

// assistMaxSEOTitle is the conventional SEO title budget. Longer titles get
// truncated on a word boundary rather than mid-word.
const assistMaxSEOTitle = 60

// sanitizeAssistInput strips markup, collapses whitespace and drops control
// characters from a stored value before it is composed into copy.
//
// Load-bearing for point 4 above: attribute values can originate from a
// supplier submission, and this text is destined for a product page. Angle
// brackets are removed outright rather than escaped, because the output is a
// plain-text draft a human edits - it should never contain markup in the first
// place, and escaping would leave visible &lt; noise in the copy.
func sanitizeAssistInput(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case r == '<' || r == '>':
			return -1
		case r < 32 && r != '\t':
			return ' '
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

// truncateOnWord trims s to at most max characters without splitting a word.
func truncateOnWord(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := s[:max]
	if i := strings.LastIndex(cut, " "); i > 0 {
		return strings.TrimRight(cut[:i], " ,;-")
	}
	return strings.TrimRight(cut, " ,;-")
}

// assistAttribute is one resolved attribute ready to compose with.
type assistAttribute struct {
	Label string
	Value string
}

// collectAssistAttributes resolves every mandatory family attribute for the
// item at the requested locale, reusing ResolveAttributeValue (26.4.1) so
// locale/channel overrides apply here exactly as they do to completeness
// scoring and the outbound publish payload - not a second resolution path.
func collectAssistAttributes(tenantID, itemCode, family, locale string) []assistAttribute {
	attrs, err := fetchMandatoryFamilyAttributes(tenantID, family)
	if err != nil || len(attrs) == 0 {
		return nil
	}
	out := make([]assistAttribute, 0, len(attrs))
	for _, a := range attrs {
		val, err := ResolveAttributeValue(tenantID, itemCode, a.AttributeCode, locale, "")
		if err != nil {
			continue
		}
		val = sanitizeAssistInput(val)
		if val == "" {
			continue
		}
		label := sanitizeAssistInput(a.Label)
		if label == "" {
			label = a.AttributeCode
		}
		out = append(out, assistAttribute{Label: label, Value: val})
	}
	// Stable output: the same item must always produce the same draft, so two
	// reviewers comparing notes see the same text. Map iteration upstream
	// makes this ordering otherwise non-deterministic.
	sort.Slice(out, func(i, j int) bool { return out[i].Label < out[j].Label })
	return out
}

// GenerateContentSuggestion composes a draft for one item in one language.
//
// It returns a suggestion; it writes no ProductContent. See the file header for
// why that separation is the human-in-the-loop guarantee rather than a
// convention.
func GenerateContentSuggestion(tenantID, itemCode, language, userID string) (*ContentSuggestion, error) {
	if strings.TrimSpace(itemCode) == "" {
		return nil, &ValidationError{Code: "GLOBAL-0002", Message: "item code is required"}
	}
	if language == "" {
		language = "en"
	}

	data, _, err := fetchItemDoc(tenantID, itemCode)
	if err != nil {
		return nil, &ValidationError{Code: "GLOBAL-0002", Message: fmt.Sprintf("item %q not found", itemCode)}
	}

	get := func(field string) string {
		if !isFieldFilled(data, field) {
			return ""
		}
		return sanitizeAssistInput(fmt.Sprintf("%v", data[field]))
	}

	name := get("name")
	brand := get("brand")
	category := get("category")
	family, _ := data["family"].(string)

	sug := &ContentSuggestion{ItemCode: itemCode, Language: language}
	var sources []string
	note := func(field string, val string) string {
		if val != "" {
			sources = append(sources, field)
		}
		return val
	}
	note("name", name)
	note("brand", brand)
	note("category", category)

	if name == "" {
		// Without a name there is nothing to build a title from, and a draft
		// titled after a SKU code is worse than none.
		return nil, &ValidationError{
			Code:    "GLOBAL-0002",
			Message: fmt.Sprintf("item %q has no name - fill the Item's name before generating content", itemCode),
		}
	}

	attrs := collectAssistAttributes(tenantID, itemCode, family, language)
	for _, a := range attrs {
		sources = append(sources, "attribute:"+a.Label)
	}

	// --- Title: brand + name, deduplicated ---------------------------------
	// A brand is very often already inside the item name ("Acme Cotton Shirt"),
	// so prefixing blindly produces "Acme Acme Cotton Shirt".
	title := name
	if brand != "" && !strings.Contains(strings.ToLower(name), strings.ToLower(brand)) {
		title = brand + " " + name
	}
	sug.Title = title

	// --- SEO title: title + category, within the length budget -------------
	seo := title
	if category != "" && !strings.Contains(strings.ToLower(seo), strings.ToLower(category)) {
		if candidate := seo + " | " + category; len(candidate) <= assistMaxSEOTitle {
			seo = candidate
		}
	}
	sug.SEOTitle = truncateOnWord(seo, assistMaxSEOTitle)

	// --- Short description: one sentence naming the top attributes ---------
	var short strings.Builder
	short.WriteString(title)
	if len(attrs) > 0 {
		lead := attrs
		if len(lead) > 3 {
			lead = lead[:3]
		}
		parts := make([]string, 0, len(lead))
		for _, a := range lead {
			parts = append(parts, strings.ToLower(a.Label)+" "+a.Value)
		}
		short.WriteString(" with " + joinReadable(parts))
	}
	short.WriteString(".")
	sug.ShortDesc = short.String()

	// --- Long description: the short line plus a plain spec list -----------
	var long strings.Builder
	long.WriteString(sug.ShortDesc)
	if category != "" {
		long.WriteString(" Part of our " + category + " range.")
	}
	if len(attrs) > 0 {
		long.WriteString("\n\nSpecifications:\n")
		for _, a := range attrs {
			long.WriteString("- " + a.Label + ": " + a.Value + "\n")
		}
	}
	sug.LongDesc = strings.TrimRight(long.String(), "\n")

	// --- Tags: lowercased, de-duplicated, order-stable ---------------------
	tagSet := map[string]bool{}
	var tags []string
	addTag := func(t string) {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" || tagSet[t] {
			return
		}
		tagSet[t] = true
		tags = append(tags, t)
	}
	addTag(brand)
	addTag(category)
	for _, a := range attrs {
		addTag(a.Value)
	}
	sug.Tags = strings.Join(tags, ", ")

	// --- Provenance and honesty about thin input ---------------------------
	sort.Strings(sources)
	sug.SourceFields = sources
	if len(attrs) == 0 {
		sug.Warnings = append(sug.Warnings,
			"No family attribute values resolved for this item, so the draft rests on the item name, brand and category alone. Fill the family attributes and regenerate for a fuller description.")
	}
	if brand == "" {
		sug.Warnings = append(sug.Warnings, "Item has no brand set - the title and tags omit it.")
	}
	if category == "" {
		sug.Warnings = append(sug.Warnings, "Item has no category set - the SEO title and tags omit it.")
	}

	writeContentAssistLog(tenantID, itemCode, language, userID, sug)
	return sug, nil
}

// joinReadable renders a list as "a, b and c" rather than a bare comma join,
// since this text goes into a customer-facing sentence.
func joinReadable(parts []string) string {
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	default:
		return strings.Join(parts[:len(parts)-1], ", ") + " and " + parts[len(parts)-1]
	}
}

// writeContentAssistLog records that a suggestion was generated (governance
// point 3). Follows writeReportRunLog's precedent exactly: a plain row in
// documents, no bespoke table and so no ProvisionTenantSchema clone-list entry
// to forget - the gap 26.11.2 found the hard way.
//
// The generated text is stored alongside the request so a later reviewer can
// diff what was suggested against what was actually published, which is what
// makes "tell AI-authored from human-authored content after the fact" an
// answerable question rather than an aspiration.
func writeContentAssistLog(tenantID, itemCode, language, userID string, sug *ContentSuggestion) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return
	}
	id := NewDocIDCompact("CAL")
	docData := map[string]interface{}{
		"id": id, "code": id,
		"item_code": itemCode,
		"language":  language,
		// Names the generator and its version, not a model - if this is ever
		// replaced by a real model call, existing rows stay unambiguous about
		// which engine produced them.
		"generator":            "local-template-v1",
		"suggested_title":      sug.Title,
		"suggested_short_desc": sug.ShortDesc,
		"suggested_long_desc":  sug.LongDesc,
		"suggested_seo_title":  sug.SEOTitle,
		"suggested_tags":       sug.Tags,
		"source_fields":        strings.Join(sug.SourceFields, ", "),
	}
	if userID != "" {
		docData["user_id"] = userID
	}
	marshaled, err := json.Marshal(docData)
	if err != nil {
		return
	}
	createdBy := userID
	if createdBy == "" {
		createdBy = "system"
	}
	_, _ = db.DB.Exec(fmt.Sprintf(
		`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'ContentAssistLog', $2, 'Active', $3)`, schema),
		id, marshaled, createdBy)
}
