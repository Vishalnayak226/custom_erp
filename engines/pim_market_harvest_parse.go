package engines

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"custom_erp/db"
)

// Phase 2 of the Myntra harvest (34.4): read the pages kept on disk and write
// market-price observations. No network. A page that does not parse is
// recorded in the summary and skipped; it never stops the others.

// MyntraParseSummary says what a parse pass did.
type MyntraParseSummary struct {
	Files    int      `json:"files"`
	Products int      `json:"products"`
	Written  int      `json:"written"`
	BadFiles []string `json:"bad_files,omitempty"`
}

// myntraProduct is one search result, already cleaned.
type myntraProduct struct {
	ID, Name, Brand, URL         string
	Price, MRP, Discount, Rating float64
	Reviews                      int
}

// Myntra puts the search state in a script: window.__myx = {...}; the older
// name is the fallback. The value is read with a JSON decoder from that point,
// so a "};" inside a string cannot cut it short.
var myntraStateStart = []*regexp.Regexp{
	regexp.MustCompile(`window\.__myx\s*=\s*`),
	regexp.MustCompile(`window\.__m_initial_state\s*=\s*`),
}

func extractMyntraState(page []byte) (map[string]interface{}, error) {
	var lastErr error
	for _, re := range myntraStateStart {
		loc := re.FindIndex(page)
		if loc == nil {
			continue
		}
		var state map[string]interface{}
		if err := json.NewDecoder(bytes.NewReader(page[loc[1]:])).Decode(&state); err != nil {
			lastErr = fmt.Errorf("the embedded JSON is not valid: %v", err)
			continue
		}
		return state, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("no window.__myx or window.__m_initial_state in the page")
}

func parseMyntraPage(page []byte) ([]myntraProduct, error) {
	state, err := extractMyntraState(page)
	if err != nil {
		return nil, err
	}
	list, _ := myntraDig(state, "searchData", "results", "products").([]interface{})
	out := make([]myntraProduct, 0, len(list))
	for _, item := range list {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		p := myntraProduct{
			ID:     myntraText(m["productId"]),
			Name:   myntraFirstText(m, "productName", "product"),
			Brand:  myntraText(m["brand"]),
			Price:  myntraNum(m["price"]),
			MRP:    myntraNum(m["mrp"]),
			Rating: myntraNum(m["rating"]),
		}
		p.Reviews = int(myntraNum(m["ratingCount"]))
		if p.Name == "" || p.Price <= 0 {
			continue // no name or price is not an observation
		}
		if d, ok := m["discount"]; ok {
			p.Discount = myntraNum(d)
		}
		if p.Discount <= 0 && p.MRP > p.Price {
			p.Discount = math.Round((p.MRP-p.Price)*1000/p.MRP) / 10
		}
		p.URL = myntraProductURL(myntraText(m["landingPageUrl"]))
		out = append(out, p)
	}
	return out, nil
}

func myntraDig(m map[string]interface{}, keys ...string) interface{} {
	var cur interface{} = m
	for _, k := range keys {
		mm, ok := cur.(map[string]interface{})
		if !ok {
			return nil
		}
		cur = mm[k]
	}
	return cur
}

func myntraText(v interface{}) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	}
	return ""
}

func myntraFirstText(m map[string]interface{}, keys ...string) string {
	for _, k := range keys {
		if s := myntraText(m[k]); s != "" {
			return s
		}
	}
	return ""
}

func myntraNum(v interface{}) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case string:
		f, _ := strconv.ParseFloat(strings.TrimSpace(t), 64)
		return f
	}
	return 0
}

// myntraProductURL keeps a link only if it stays on www.myntra.com.
func myntraProductURL(raw string) string {
	raw = strings.TrimSpace(raw)
	switch {
	case raw == "":
		return ""
	case strings.HasPrefix(raw, "https://"+myntraHost+"/"):
		return raw
	case strings.Contains(raw, "://") || strings.HasPrefix(raw, "//"):
		return ""
	}
	return "https://" + myntraHost + "/" + strings.TrimLeft(raw, "/")
}

func myntraFmtNum(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

// parseMyntraDay parses every page kept for one day and upserts one
// CompetitorPrice per product. A row seen again keeps what a person has since
// added (our_item, status): only the harvested fields are refreshed.
func parseMyntraDay(tenantID, schema, root, date string) MyntraParseSummary {
	var sum MyntraParseSummary
	files, _ := filepath.Glob(filepath.Join(root, date, "*", "page_*.html"))
	sort.Strings(files)
	for _, f := range files {
		sum.Files++
		keyword := strings.ReplaceAll(filepath.Base(filepath.Dir(f)), "-", " ")
		page, err := os.ReadFile(f)
		if err != nil {
			sum.BadFiles = append(sum.BadFiles, filepath.Base(filepath.Dir(f))+"/"+filepath.Base(f)+": "+err.Error())
			continue
		}
		products, err := parseMyntraPage(page)
		if err != nil {
			sum.BadFiles = append(sum.BadFiles, filepath.Base(filepath.Dir(f))+"/"+filepath.Base(f)+": "+err.Error())
			continue
		}
		for _, p := range products {
			sum.Products++
			if err := upsertMyntraObservation(schema, date, keyword, p); err != nil {
				sum.BadFiles = append(sum.BadFiles, filepath.Base(f)+": "+err.Error())
				continue
			}
			sum.Written++
		}
	}
	return sum
}

func upsertMyntraObservation(schema, date, keyword string, p myntraProduct) error {
	id := harvestObservationID(date, p.ID, p.Name)
	data := map[string]interface{}{
		"code": id, "platform": "Myntra", "competitor_product": p.Name, "competitor_sku": p.ID,
		"competitor_price": myntraFmtNum(p.Price), "observed_at": date, "status": "Active",
		"brand": p.Brand, "keyword": keyword, "harvest_source": "Myntra harvest",
	}
	if p.MRP > 0 {
		data["mrp"] = myntraFmtNum(p.MRP)
	}
	if p.Discount > 0 {
		data["discount_pct"] = myntraFmtNum(p.Discount)
	}
	if p.Rating > 0 {
		data["rating"] = myntraFmtNum(p.Rating)
	}
	if p.Reviews > 0 {
		data["review_count"] = strconv.Itoa(p.Reviews)
	}
	if p.URL != "" {
		data["source_url"] = p.URL
	}
	b, _ := json.Marshal(data)
	_, err := db.DB.Exec(fmt.Sprintf(`
		INSERT INTO %[1]s.documents (id, doctype, data, status, created_by)
		VALUES ($1, 'CompetitorPrice', $2::jsonb, 'Active', 'system')
		ON CONFLICT (id) DO UPDATE
		   SET data = %[1]s.documents.data || ($2::jsonb - 'status'), updated_at = CURRENT_TIMESTAMP
		 WHERE %[1]s.documents.doctype = 'CompetitorPrice'`, schema), id, string(b))
	return err
}
