package engines

import (
	"custom_erp/db"
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// HSN suggestions (Stage 66).
//
// The New Item HSN box (Stage 57.3) searched only the client's own HSN list,
// which starts empty - so the person creating "Zircon Haar" was asked for an
// HSN number with nothing to choose from. The user asked for it to be smart:
// load the codes for the client's industry, read the item name ("silver" is
// not "zircon", cloth is not fabric), let them pick, and still type and add
// a code nobody suggested. The GST rate follows the code.
//
// SuggestHSN ranks codes from four sources, strongest first:
//
//   - the client's own items and item names: a code already used on items
//     whose names share words with this one ("Zircon Haar Green" -> 711719);
//   - the shipped master list (hsn_master.json): keywords decide WHICH code
//     (the material, the exact product), related words say what KIND of
//     thing it is, and the client's industry breaks ties;
//   - the client's HSN Code list (description words, and its rate, which
//     outranks the master's because an administrator set it);
//   - with nothing in the name to go on, the industry's common codes.
//
// It is a suggestion list, never a constraint - the same footing as the
// catalogue it feeds (hsn_catalog.go). Nothing is rejected for not being in
// it, and a code typed by hand is still created inline and learned on save.

//go:embed hsn_master.json
var hsnMasterJSON []byte

// HSNRate is one GST rate a code carries. Note says when it applies, for a
// code with more than one (garments: up to / above Rs 2,500 a piece).
type HSNRate struct {
	Rate float64 `json:"rate"`
	Note string  `json:"note,omitempty"`
}

type hsnMasterEntry struct {
	HSN         string    `json:"hsn"`
	Description string    `json:"description"`
	Rate        *float64  `json:"rate"`
	Rates       []HSNRate `json:"rates"`
	RateSet     string    `json:"rate_set"`
	Industries  []string  `json:"industries"`
	Keywords    []string  `json:"keywords"`
	Related     []string  `json:"related"`
	Exclude     []string  `json:"exclude"`
}

func (e *hsnMasterEntry) inIndustry(code string) bool {
	for _, i := range e.Industries {
		if i == code {
			return true
		}
	}
	return false
}

type hsnMasterFile struct {
	RateSets  map[string][]HSNRate `json:"rate_sets"`
	WordLists map[string][]string  `json:"word_lists"`
	Entries   []hsnMasterEntry     `json:"entries"`
}

var (
	hsnMasterOnce  sync.Once
	hsnMasterList  []hsnMasterEntry
	hsnMasterIndex map[string]int
	hsnMasterErr   error
)

// hsnMaster is the parsed master list, read once from the embedded file.
func hsnMaster() ([]hsnMasterEntry, map[string]int, error) {
	hsnMasterOnce.Do(func() {
		hsnMasterList, hsnMasterIndex, hsnMasterErr = parseHSNMaster(hsnMasterJSON)
	})
	return hsnMasterList, hsnMasterIndex, hsnMasterErr
}

// parseHSNMaster resolves rate sets and @word lists and normalizes every
// word, so matching never has to. A malformed entry fails the whole file:
// every code in it must be one an Item accepts (validHSN), and appear once.
func parseHSNMaster(raw []byte) ([]hsnMasterEntry, map[string]int, error) {
	var f hsnMasterFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, nil, fmt.Errorf("hsn master: %w", err)
	}
	expand := func(words []string) ([]string, error) {
		var out []string
		for _, w := range words {
			if strings.HasPrefix(w, "@") {
				list, ok := f.WordLists[w[1:]]
				if !ok {
					return nil, fmt.Errorf("hsn master: unknown word list %s", w)
				}
				for _, l := range list {
					out = append(out, hsnText(l))
				}
				continue
			}
			out = append(out, hsnText(w))
		}
		return out, nil
	}
	index := make(map[string]int, len(f.Entries))
	for i := range f.Entries {
		e := &f.Entries[i]
		e.HSN = NormalizeHSN(e.HSN)
		if !validHSN(e.HSN) {
			return nil, nil, fmt.Errorf("hsn master: %q is not a 4, 6 or 8 digit code", e.HSN)
		}
		if _, dup := index[e.HSN]; dup {
			return nil, nil, fmt.Errorf("hsn master: %s appears twice", e.HSN)
		}
		index[e.HSN] = i
		switch {
		case e.RateSet != "":
			rs, ok := f.RateSets[e.RateSet]
			if !ok {
				return nil, nil, fmt.Errorf("hsn master: %s names unknown rate set %q", e.HSN, e.RateSet)
			}
			e.Rates = rs
		case len(e.Rates) > 0:
		case e.Rate != nil:
			e.Rates = []HSNRate{{Rate: *e.Rate}}
		}
		var err error
		if e.Keywords, err = expand(e.Keywords); err != nil {
			return nil, nil, err
		}
		if e.Related, err = expand(e.Related); err != nil {
			return nil, nil, err
		}
		if e.Exclude, err = expand(e.Exclude); err != nil {
			return nil, nil, err
		}
	}
	return f.Entries, index, nil
}

// hsnMasterDescription is the master list's description for a code, "" for
// a code it does not have.
func hsnMasterDescription(hsn string) string {
	list, index, err := hsnMaster()
	if err != nil {
		return ""
	}
	if i, ok := index[hsn]; ok {
		return list[i].Description
	}
	return ""
}

// hsnSpelling folds the spellings item names actually use onto the one the
// master list is written in.
var hsnSpelling = map[string]string{
	"aluminum": "aluminium", "colour": "color", "coloured": "color",
	"jewelry": "jewellery", "jewelery": "jewellery", "jwellery": "jewellery",
	"oxidized": "oxidised", "galvanized": "galvanised",
	"tire": "tyre", "tires": "tyres", "tshirt": "t shirt", "tshirts": "t shirts",
	"moisturizer": "moisturiser", "nebulizer": "nebuliser",
	"fertilizer": "fertiliser", "fertilizers": "fertilisers",
	"orthopedic": "orthopaedic", "yogurt": "yoghurt", "chili": "chilli",
	"daal": "dal", "pajama": "pyjama", "pyjamas": "pyjama",
	"flavored": "flavoured",
}

// hsnText lower-cases text, turns everything but letters and digits into
// single spaces and folds spelling variants, so "T-Shirt" matches "t shirt"
// and "Aluminum" matches "aluminium".
func hsnText(s string) string {
	var b strings.Builder
	space := true
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			space = false
		} else if !space {
			b.WriteByte(' ')
			space = true
		}
	}
	words := strings.Fields(b.String())
	for i, w := range words {
		if c, ok := hsnSpelling[w]; ok {
			words[i] = c
		}
	}
	return strings.Join(words, " ")
}

// hsnWords is a normalized text, ready to be asked whether it contains a
// word or phrase.
type hsnWords struct {
	padded string
	tokens map[string]bool
}

func newHSNWords(text string) hsnWords {
	t := hsnText(text)
	w := hsnWords{padded: " " + t + " ", tokens: map[string]bool{}}
	for _, tok := range strings.Fields(t) {
		w.tokens[tok] = true
	}
	return w
}

// has reports whether the text contains term as a whole word or phrase,
// singular or plural ("bangle" matches "Bangles", "panty" matches "panties").
func (w hsnWords) has(term string) bool {
	if term == "" {
		return false
	}
	if strings.Contains(term, " ") {
		return strings.Contains(w.padded, " "+term+" ") || strings.Contains(w.padded, " "+term+"s ") ||
			strings.Contains(w.padded, " "+term+"es ")
	}
	if w.tokens[term] || w.tokens[term+"s"] || w.tokens[term+"es"] {
		return true
	}
	return strings.HasSuffix(term, "y") && w.tokens[term[:len(term)-1]+"ies"]
}

// scoreHSNEntry: 3 per keyword found, 1 per related word; 0 when an
// exclusion is present ("german silver" is not silver).
func scoreHSNEntry(e *hsnMasterEntry, w hsnWords) (int, []string) {
	for _, t := range e.Exclude {
		if w.has(t) {
			return 0, nil
		}
	}
	score := 0
	var hits []string
	for _, t := range e.Keywords {
		if w.has(t) {
			score += 3
			hits = append(hits, t)
		}
	}
	for _, t := range e.Related {
		if w.has(t) {
			score++
			hits = append(hits, t)
		}
	}
	return score, hits
}

// hsnStopWords never count as shared words between two item names.
var hsnStopWords = map[string]bool{
	"the": true, "and": true, "for": true, "with": true, "new": true, "pcs": true,
	"piece": true, "pieces": true, "size": true, "free": true, "pack": true, "set": true,
	"item": true, "items": true, "type": true, "design": true, "color": true, "small": true,
	"medium": true, "large": true, "big": true, "mini": true,
}

// hsnNameTokens are the words of a name worth matching against other names:
// three characters or more, not a stop word.
func hsnNameTokens(name string) []string {
	var out []string
	seen := map[string]bool{}
	for _, t := range strings.Fields(hsnText(name)) {
		if len(t) < 3 || hsnStopWords[t] || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return out
}

// HSNSuggestion is one code offered to the user. GSTRate is the rate to fill
// in (nil when it depends on something only the user knows, such as the
// sale price); Rates lists every rate the code can carry.
type HSNSuggestion struct {
	HSN         string    `json:"hsn"`
	Description string    `json:"description,omitempty"`
	GSTRate     *float64  `json:"gst_rate,omitempty"`
	Rates       []HSNRate `json:"rates,omitempty"`
	Group       string    `json:"group"`
	Reason      string    `json:"reason,omitempty"`
	InCatalogue bool      `json:"in_catalogue"`
}

// HSNSuggestQuery is what the HSN box knows: the item's name (and whatever
// else on the form describes it), what has been typed into the box, or one
// exact code whose rate is wanted.
type HSNSuggestQuery struct {
	Name  string
	Query string
	HSN   string
	Limit int
	// HistoryDoctypes are the record types whose existing HSNs may be shown
	// (the caller passes only those the user can read): "Item", "ItemName".
	HistoryDoctypes []string
}

// HSNSuggestResult is the ranked list, plus the industry it was ranked for.
type HSNSuggestResult struct {
	Industry     string          `json:"industry,omitempty"`
	IndustryName string          `json:"industry_name,omitempty"`
	Suggestions  []HSNSuggestion `json:"suggestions"`
}

// hsnIndustryNames label the "Common in ..." group; the codes are the ones
// handleGetIndustries offers and industry_lock stores.
var hsnIndustryNames = map[string]string{
	"JEWELRY": "Jewellery", "FOOD_BEV": "Food & Beverage", "AUTO": "Automobile",
	"CLOTHING": "Clothing & Apparel", "PHARMA": "Pharma", "METAL": "Metal & Steel",
	"CONSTRUCTION": "Construction", "MEDICAL": "Medical Devices",
	"SEMICONDUCTOR": "Electronics & Semiconductors", "AGRICULTURE": "Agriculture",
}

type hsnCandidate struct {
	HSNSuggestion
	score       int
	industry    bool
	masterOrder int
	master      *hsnMasterEntry
	catRate     *float64
	catDesc     string
	histRate    *float64
	histCount   int
	histExample string
	reasons     []string
	sources     int
}

// hsnHistoryDoctypes keeps the history query to the two record types that
// carry an item's HSN, whatever a caller passes.
var hsnHistoryDoctypes = map[string]bool{"Item": true, "ItemName": true}

// SuggestHSN ranks HSN/SAC codes for an item - see the comment at the top of
// this file.
func SuggestHSN(tenantID string, q HSNSuggestQuery) (*HSNSuggestResult, error) {
	master, index, err := hsnMaster()
	if err != nil {
		return nil, err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	res := &HSNSuggestResult{Suggestions: []HSNSuggestion{}}
	if lock, err := GetIndustryLock(tenantID); err == nil && lock.Locked {
		res.Industry = strings.ToUpper(lock.IndustryCode)
		res.IndustryName = hsnIndustryNames[res.Industry]
		if res.IndustryName == "" {
			res.IndustryName = res.Industry
		}
	}
	limit := q.Limit
	if limit <= 0 || limit > 60 {
		limit = 40
	}
	var doctypes []string
	for _, d := range q.HistoryDoctypes {
		if hsnHistoryDoctypes[d] {
			doctypes = append(doctypes, d)
		}
	}

	cands := map[string]*hsnCandidate{}
	get := func(hsn string) *hsnCandidate {
		c, ok := cands[hsn]
		if !ok {
			c = &hsnCandidate{HSNSuggestion: HSNSuggestion{HSN: hsn}, masterOrder: -1}
			if i, ok := index[hsn]; ok {
				c.master = &master[i]
				c.masterOrder = i
				c.industry = res.Industry != "" && c.master.inIndustry(res.Industry)
			}
			cands[hsn] = c
		}
		return c
	}

	exact := NormalizeHSN(q.HSN)
	if exact != "" {
		get(exact)
	}

	// The client's HSN list.
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT data::text FROM %s.documents
		WHERE doctype = 'HSNCode' AND deleted_at IS NULL LIMIT 3000`, schema))
	if err != nil {
		return nil, err
	}
	nameWords := newHSNWords(q.Name)
	nameTokens := hsnNameTokens(q.Name)
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			rows.Close()
			return nil, err
		}
		var d map[string]interface{}
		if json.Unmarshal([]byte(raw), &d) != nil || strField(d, "status") == "Inactive" {
			continue
		}
		hsn := NormalizeHSN(strField(d, "hsn"))
		if !validHSN(hsn) || (exact != "" && hsn != exact) {
			continue
		}
		c := get(hsn)
		c.InCatalogue = true
		c.catDesc = strings.TrimSpace(strField(d, "description"))
		if r, ok := hsnRateValue(d["gst_rate"]); ok {
			c.catRate = &r
		}
		if c.catDesc != "" && len(nameTokens) > 0 {
			desc := newHSNWords(c.catDesc)
			shared := 0
			for _, t := range nameTokens {
				if desc.has(t) {
					shared++
				}
			}
			if shared > 0 {
				c.bump(2*shared, "in your HSN list")
			}
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Codes the client's own similar items already carry.
	if len(doctypes) > 0 {
		if err := hsnHistory(schema, doctypes, exact, nameTokens, get); err != nil {
			return nil, err
		}
	}

	// The master list.
	if exact == "" {
		for i := range master {
			e := &master[i]
			score, hits := scoreHSNEntry(e, nameWords)
			c, seen := cands[e.HSN]
			if score == 0 && !seen && !(res.Industry != "" && e.inIndustry(res.Industry)) && q.Query == "" {
				continue
			}
			if c == nil {
				c = get(e.HSN)
			}
			if score > 0 {
				if c.industry {
					score++
				}
				c.bump(score, "matches "+hsnQuoteList(hits))
			}
		}
	}

	list := make([]*hsnCandidate, 0, len(cands))
	for _, c := range cands {
		c.finish()
		if q.Query != "" && !c.matchesQuery(q.Query) {
			continue
		}
		list = append(list, c)
	}
	top := 0
	for _, c := range list {
		if c.score > top {
			top = c.score
		}
	}
	// A weak match next to a strong one is noise: "cotton saree" should not
	// list every cotton garment under the sarees.
	cut := (top + 2) / 3
	if cut < 1 {
		cut = 1
	}
	label := hsnGroupLabel(q.Name)
	other := "Other codes"
	if q.Query != "" {
		other = "Matching " + hsnQuoteList([]string{q.Query})
	}
	for _, c := range list {
		switch {
		case exact != "":
			c.Group = ""
		case c.score >= cut && c.score > 0:
			c.Group = label
		case c.InCatalogue:
			c.Group = "Your HSN list"
		case c.industry:
			c.Group = "Common in " + res.IndustryName
		default:
			c.Group = other
		}
	}
	rank := func(c *hsnCandidate) int {
		switch {
		case c.Group == label:
			return 0
		case c.InCatalogue:
			return 1
		case c.industry:
			return 2
		}
		return 3
	}
	sort.SliceStable(list, func(i, j int) bool {
		a, b := list[i], list[j]
		if ra, rb := rank(a), rank(b); ra != rb {
			return ra < rb
		}
		if a.Group == label && a.score != b.score {
			return a.score > b.score
		}
		if a.industry != b.industry {
			return a.industry
		}
		if (a.masterOrder >= 0) != (b.masterOrder >= 0) {
			return a.masterOrder >= 0
		}
		if a.masterOrder != b.masterOrder {
			return a.masterOrder < b.masterOrder
		}
		return a.HSN < b.HSN
	})
	// Typed into the box, the rest are search results worth showing; without
	// a query they are weak matches from other trades, which are not.
	for _, c := range list {
		if len(res.Suggestions) >= limit {
			break
		}
		if c.Group == other && q.Query == "" {
			continue
		}
		res.Suggestions = append(res.Suggestions, c.HSNSuggestion)
	}
	return res, nil
}

// bump records one source's opinion of this code: the best score wins, and
// every further source that agrees adds one, because agreement is evidence.
func (c *hsnCandidate) bump(score int, reason string) {
	if score <= 0 {
		return
	}
	if c.sources > 0 {
		if score > c.score {
			c.score = score + 1
		} else {
			c.score++
		}
	} else {
		c.score = score
	}
	c.sources++
	c.reasons = append(c.reasons, reason)
}

// finish settles what the user is shown: the client's own description and
// rate over the master's, and the master's rates (with their notes) as the
// choices for the GST box.
func (c *hsnCandidate) finish() {
	c.Description = c.catDesc
	if c.master != nil {
		if c.Description == "" {
			c.Description = c.master.Description
		}
		c.Rates = c.master.Rates
	}
	switch {
	case c.catRate != nil:
		c.GSTRate = c.catRate
	case c.histRate != nil:
		c.GSTRate = c.histRate
	case len(c.Rates) == 1:
		r := c.Rates[0].Rate
		c.GSTRate = &r
	}
	// The rate that will be filled is always one of the choices shown: a
	// rate this client set (or uses) that the master list does not carry -
	// a pre-GST-2.0 12%, say - is listed first and labelled as theirs.
	if c.GSTRate != nil {
		listed := false
		for _, r := range c.Rates {
			if r.Rate == *c.GSTRate {
				listed = true
			}
		}
		if !listed {
			note := ""
			if len(c.Rates) > 0 {
				note = "Used on your items"
				if c.catRate != nil {
					note = "Set on your HSN list"
				}
			}
			c.Rates = append([]HSNRate{{Rate: *c.GSTRate, Note: note}}, c.Rates...)
		}
	}
	if c.Description == "" && c.histExample != "" {
		c.Description = "as on " + c.histExample
	}
	c.Reason = strings.Join(c.reasons, "; ")
}

// matchesQuery: digits narrow by code prefix ("7113" finds 711311), words by
// description and match words - every typed word must start one of them.
func (c *hsnCandidate) matchesQuery(query string) bool {
	qd := NormalizeHSN(query)
	if qd != "" && strings.Trim(qd, "0123456789") == "" {
		return strings.HasPrefix(c.HSN, qd) || strings.HasPrefix(qd, c.HSN)
	}
	hay := c.Description + " " + c.histExample
	if c.master != nil {
		hay += " " + c.master.Description + " " + strings.Join(c.master.Keywords, " ") + " " + strings.Join(c.master.Related, " ")
	}
	words := strings.Fields(hsnText(hay))
	for _, qw := range strings.Fields(hsnText(query)) {
		found := false
		for _, w := range words {
			if strings.HasPrefix(w, qw) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// hsnHistory reads the HSN (and rate) of existing items. For a name, the
// items sharing words with it; for an exact code, every item carrying it -
// its most common rate is that code's rate in this business.
func hsnHistory(schema string, doctypes []string, exact string, nameTokens []string, get func(string) *hsnCandidate) error {
	if exact == "" && len(nameTokens) == 0 {
		return nil
	}
	placeholders := make([]string, len(doctypes))
	args := make([]interface{}, 0, len(doctypes)+1)
	for i, d := range doctypes {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args = append(args, d)
	}
	query := fmt.Sprintf(`SELECT COALESCE(data->>'name', ''), COALESCE(data->>'hsn_code', ''), COALESCE(data->>'gst_rate', '')
		FROM %s.documents
		WHERE doctype IN (%s) AND deleted_at IS NULL AND COALESCE(data->>'hsn_code', '') <> ''`,
		schema, strings.Join(placeholders, ", "))
	if exact != "" {
		query += fmt.Sprintf(` AND regexp_replace(data->>'hsn_code', '[[:space:].]', '', 'g') = $%d`, len(args)+1)
		args = append(args, exact)
	} else {
		// Tokens are [a-z0-9] only (hsnText), so they are safe inside the
		// pattern; \m anchors each at the start of a word.
		query += fmt.Sprintf(` AND lower(COALESCE(data->>'name', '')) ~ $%d`, len(args)+1)
		args = append(args, `\m(`+strings.Join(nameTokens, "|")+`)`)
	}
	query += " LIMIT 500"
	rows, err := db.DB.Query(query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	type agg struct {
		best    int
		count   int
		example string
		rates   map[float64]int
	}
	byCode := map[string]*agg{}
	var order []string
	for rows.Next() {
		var name, rawHSN, rawRate string
		if err := rows.Scan(&name, &rawHSN, &rawRate); err != nil {
			return err
		}
		hsn := NormalizeHSN(rawHSN)
		if !validHSN(hsn) {
			continue
		}
		shared := 0
		if exact == "" {
			words := newHSNWords(name)
			for _, t := range nameTokens {
				if words.has(t) {
					shared++
				}
			}
			if shared == 0 {
				continue
			}
		}
		a, ok := byCode[hsn]
		if !ok {
			a = &agg{rates: map[float64]int{}}
			byCode[hsn] = a
			order = append(order, hsn)
		}
		a.count++
		if a.example == "" || shared > a.best {
			a.best = shared
			a.example = strings.TrimSpace(name)
		}
		if r, err := strconv.ParseFloat(strings.TrimSpace(rawRate), 64); err == nil {
			a.rates[r]++
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, hsn := range order {
		a := byCode[hsn]
		c := get(hsn)
		c.histCount = a.count
		c.histExample = a.example
		bestRate, bestN := 0.0, 0
		for r, n := range a.rates {
			if n > bestN || (n == bestN && r < bestRate) {
				bestRate, bestN = r, n
			}
		}
		if bestN > 0 {
			c.histRate = &bestRate
		}
		if exact == "" {
			extra := a.count - 1
			if extra > 2 {
				extra = 2
			}
			reason := fmt.Sprintf("used on %d of your items, e.g. %q", a.count, a.example)
			if a.count == 1 {
				reason = fmt.Sprintf("used on your item %q", a.example)
			}
			c.bump(4*a.best+extra, reason)
		}
	}
	return nil
}

// hsnRateValue reads a stored rate, which JSON may hold as a number or a
// string; ok is false when none is set.
func hsnRateValue(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case string:
		if strings.TrimSpace(n) == "" {
			return 0, false
		}
		f, err := strconv.ParseFloat(strings.TrimSpace(n), 64)
		return f, err == nil
	}
	return 0, false
}

func hsnQuoteList(words []string) string {
	q := make([]string, len(words))
	for i, w := range words {
		q[i] = "“" + w + "”"
	}
	return strings.Join(q, ", ")
}

func hsnGroupLabel(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "Suggested"
	}
	if r := []rune(name); len(r) > 40 {
		name = string(r[:40]) + "…"
	}
	return "Suggested for “" + name + "”"
}

// seedIndustryHSNCodes adds the industry's master codes to the client's HSN
// list when it chooses that industry ("once industry selected, load those
// HSN"). Runs inside SwitchIndustryProfile's transaction, whose search_path
// is the tenant schema. A code already on the list is left exactly as it is,
// so a description or rate an administrator set is never overwritten; a
// code with more than one rate goes in without a default rate, since the
// right one depends on the item.
func seedIndustryHSNCodes(tx *sql.Tx, industryCode string) (int, error) {
	master, _, err := hsnMaster()
	if err != nil {
		return 0, err
	}
	code := strings.ToUpper(strings.TrimSpace(industryCode))
	added := 0
	for i := range master {
		e := &master[i]
		if !e.inIndustry(code) {
			continue
		}
		data := map[string]interface{}{
			"code": HSNCatalogID(e.HSN), "hsn": e.HSN, "description": e.Description, "status": "Active",
		}
		if len(e.Rates) == 1 {
			data["gst_rate"] = e.Rates[0].Rate
		}
		payload, err := json.Marshal(data)
		if err != nil {
			return added, err
		}
		r, err := tx.Exec(`INSERT INTO documents (id, doctype, data, status, created_by)
			VALUES ($1, 'HSNCode', $2, 'Active', 'system')
			ON CONFLICT (id) DO NOTHING`, HSNCatalogID(e.HSN), payload)
		if err != nil {
			return added, fmt.Errorf("failed to add HSN %s: %w", e.HSN, err)
		}
		if n, _ := r.RowsAffected(); n > 0 {
			added++
		}
	}
	return added, nil
}
