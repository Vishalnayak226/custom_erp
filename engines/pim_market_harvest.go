package engines

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"custom_erp/db"
)

// Market-price harvester for Myntra (34.4). The owner accepted the terms risk
// on 2026-11-13 on these conditions, each one enforced here and tested:
//
//   - off by default: platform.market_harvest_myntra, an admin-only setting;
//   - robots.txt is fetched once per run and every page path is checked
//     against it (longest match, Allow wins a tie); a disallowed path is
//     never fetched;
//   - one request at a time, at least platform.market_harvest_interval_seconds
//     (never under 5) apart, a hard cap on pages per run, and a stop on the
//     first 429 / 403 / captcha-looking answer or redirect;
//   - one fixed host (www.myntra.com over https), redirects not followed,
//     a generic product User-Agent;
//   - raw pages are kept per client under MEDIA_STORE_DIR/<schema>/, the
//     parsed rows are the client's own CompetitorPrice documents.
//
// Two phases (the retired project's shape): phase 1 fetches raw pages to disk
// and skips any page already on disk for the day; phase 2 parses the files
// offline, so a parser fix costs no request (the Re-parse action). Phase 1 is
// a pool of one: the owner's condition is one request at a time.

const (
	SettingMyntraHarvest         = "platform.market_harvest_myntra"
	SettingMyntraHarvestMaxPages = "platform.market_harvest_max_pages"
	SettingMyntraHarvestInterval = "platform.market_harvest_interval_seconds"

	myntraHarvestJobType  = "market_harvest_myntra"
	myntraHost            = "www.myntra.com"
	myntraUserAgent       = "WholeOps-MarketPrice/1.0"
	myntraAgentToken      = "wholeops-marketprice"
	myntraMinInterval     = 5 * time.Second
	myntraTrivialBytes    = 100
	myntraMaxBodyBytes    = 8 << 20
	myntraMaxKeywords     = 10
	myntraMaxKeywordRunes = 60

	harvestOffCode     = "PIM-9340"
	harvestInvalidCode = "PIM-9341"
	harvestBusyCode    = "PIM-9342"
	harvestNoPagesCode = "PIM-9343"
)

var istZone = time.FixedZone("IST", 5*3600+1800)

// MyntraHarvestRequest is what an admin asks for.
type MyntraHarvestRequest struct {
	Keywords []string `json:"keywords"`
	Category string   `json:"category"`
	MaxPages int      `json:"max_pages"`
}

// MyntraPageLog is one line of a run's log.
type MyntraPageLog struct {
	Keyword string `json:"keyword"`
	Page    int    `json:"page"`
	Outcome string `json:"outcome"` // fetched, cached, refused by robots.txt, stopped, error
	Note    string `json:"note,omitempty"`
}

// MyntraHarvestResult is the job's result: the run log.
type MyntraHarvestResult struct {
	Date         string             `json:"date"`
	Fetched      int                `json:"fetched"`
	Cached       int                `json:"cached"`
	Refused      int                `json:"refused_by_robots"`
	RefusedPaths []string           `json:"refused_paths,omitempty"`
	StopReason   string             `json:"stop_reason"`
	Pages        []MyntraPageLog    `json:"pages"`
	Parse        MyntraParseSummary `json:"parse"`
}

func (r *MyntraHarvestResult) toMap() map[string]interface{} {
	b, _ := json.Marshal(r)
	out := map[string]interface{}{}
	_ = json.Unmarshal(b, &out)
	return out
}

// ---------------------------------------------------------------------------
// robots.txt
// ---------------------------------------------------------------------------

type robotsRule struct {
	allow bool
	size  int
	re    *regexp.Regexp
}

type robotsRules struct{ rules []robotsRule }

// parseRobots keeps the groups that name "*" or this product's agent token and
// applies both (the stricter reading: a specific group never loosens "*").
func parseRobots(body string) *robotsRules {
	rr := &robotsRules{}
	var agents []string
	inRules := false
	applies := func() bool {
		for _, a := range agents {
			if a == "*" || (a != "" && strings.Contains(myntraAgentToken, a)) {
				return true
			}
		}
		return false
	}
	for _, line := range strings.Split(body, "\n") {
		if i := strings.Index(line, "#"); i >= 0 {
			line = line[:i]
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		k = strings.ToLower(strings.TrimSpace(k))
		v = strings.TrimSpace(v)
		switch k {
		case "user-agent":
			if inRules {
				agents, inRules = nil, false
			}
			agents = append(agents, strings.ToLower(v))
		case "allow", "disallow":
			inRules = true
			if v == "" || !applies() {
				continue
			}
			pat := regexp.QuoteMeta(v)
			pat = strings.ReplaceAll(pat, `\*`, ".*")
			if strings.HasSuffix(pat, `\$`) {
				pat = strings.TrimSuffix(pat, `\$`) + "$"
			}
			re, err := regexp.Compile("^" + pat)
			if err != nil {
				continue
			}
			rr.rules = append(rr.rules, robotsRule{allow: k == "allow", size: len(v), re: re})
		}
	}
	return rr
}

// allowed applies the longest matching rule; Allow wins a tie; no match = allowed.
func (rr *robotsRules) allowed(requestURI string) bool {
	best, verdict := -1, true
	for _, r := range rr.rules {
		if !r.re.MatchString(requestURI) {
			continue
		}
		if r.size > best || (r.size == best && r.allow) {
			best, verdict = r.size, r.allow
		}
	}
	return verdict
}

// ---------------------------------------------------------------------------
// The harvester: fixed host, one request at a time, spaced
// ---------------------------------------------------------------------------

type myntraHarvester struct {
	schema   string
	base     *url.URL
	client   *http.Client
	now      func() time.Time
	sleep    func(ctx context.Context, d time.Duration) error
	interval time.Duration
	root     string
	last     time.Time
	robots   *robotsRules
}

func contextSleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// newMyntraHarvester is the production one: https://www.myntra.com only, the
// guarded outbound client, redirects not followed.
func newMyntraHarvester(schema string, interval time.Duration) *myntraHarvester {
	return &myntraHarvester{
		schema:   schema,
		base:     &url.URL{Scheme: "https", Host: myntraHost},
		client:   newOutboundClient(30*time.Second, false),
		now:      time.Now,
		sleep:    contextSleep,
		interval: interval,
		root:     marketHarvestRoot(schema),
	}
}

// marketHarvestRoot is the client's own folder, beside its other server-side files.
func marketHarvestRoot(schema string) string {
	return filepath.Join(mediaStoreDir, schema, "market_harvest", "myntra")
}

func (h *myntraHarvester) spacing() time.Duration {
	if h.interval < myntraMinInterval {
		return myntraMinInterval
	}
	return h.interval
}

// testBase is true only for a harvester built over a non-Myntra base (tests).
func (h *myntraHarvester) testBase() bool { return h.base.Host != myntraHost }

type myntraOutsideRefusal struct{ reason string }

func (e *myntraOutsideRefusal) Error() string { return e.reason }

// get is the only network call: it spaces requests, refuses another host,
// honours the client's Outside calls switches, and never follows a redirect.
func (h *myntraHarvester) get(ctx context.Context, requestURI string) (int, []byte, error) {
	ref, err := url.Parse(requestURI)
	if err != nil {
		return 0, nil, err
	}
	if ref.Host != "" || ref.Scheme != "" {
		return 0, nil, fmt.Errorf("only paths on %s are used", myntraHost)
	}
	u := *h.base
	u.Path, u.RawQuery = ref.Path, ref.RawQuery
	if !h.testBase() && (u.Scheme != "https" || u.Host != myntraHost) {
		return 0, nil, fmt.Errorf("only https://%s is used", myntraHost)
	}
	if reason := outsideCallRefusal(h.schema, http.MethodGet, u.String()); reason != "" {
		return 0, nil, &myntraOutsideRefusal{reason}
	}
	if !h.last.IsZero() {
		if wait := h.spacing() - h.now().Sub(h.last); wait > 0 {
			if err := h.sleep(ctx, wait); err != nil {
				return 0, nil, err
			}
		}
	}
	h.last = h.now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("User-Agent", myntraUserAgent)
	req.Header.Set("Accept", "text/html,text/plain;q=0.8")
	req.Header.Set("Accept-Language", "en-IN,en;q=0.8")
	resp, err := h.client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, err := readOutboundBody(resp.Body, myntraMaxBodyBytes)
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, body, nil
}

var myntraCaptchaLook = regexp.MustCompile(`(?i)captcha|access denied|are you a (human|robot)|unusual traffic`)

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

func harvestSlug(s string) string {
	return strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(s), "-"), "-")
}

// loadRobots fetches robots.txt once per run. Only 200 (rules) and 404/410
// (no rules) let the run go on; anything else is a reason to stop.
func (h *myntraHarvester) loadRobots(ctx context.Context) (string, error) {
	status, body, err := h.get(ctx, "/robots.txt")
	if err != nil {
		return "", err
	}
	switch {
	case status == http.StatusOK:
		h.robots = parseRobots(string(body))
	case status == http.StatusNotFound || status == http.StatusGone:
		h.robots = &robotsRules{}
	default:
		return fmt.Sprintf("robots.txt answered %d - no page was fetched", status), nil
	}
	return "", nil
}

func harvestPagePath(slug string, page int) string {
	q := url.Values{}
	q.Set("rawQuery", strings.ReplaceAll(slug, "-", " "))
	if page > 1 {
		q.Set("p", strconv.Itoa(page))
	}
	return "/" + slug + "?" + q.Encode()
}

// run is phase 1 then phase 2.
func (h *myntraHarvester) run(ctx context.Context, tenantID string, req MyntraHarvestRequest, maxPages int) *MyntraHarvestResult {
	res := &MyntraHarvestResult{Date: h.now().In(istZone).Format("2006-01-02"), Pages: []MyntraPageLog{}}
	h.fetchAll(ctx, res, req, maxPages)
	if res.StopReason == "" {
		res.StopReason = "finished"
	}
	// Phase 2 runs on whatever is on disk, even after a stop.
	res.Parse = parseMyntraDay(tenantID, h.schema, h.root, res.Date)
	return res
}

func (h *myntraHarvester) fetchAll(ctx context.Context, res *MyntraHarvestResult, req MyntraHarvestRequest, maxPages int) {
	perKeyword := maxPages / len(req.Keywords)
	if perKeyword < 1 {
		perKeyword = 1
	}
	for _, kw := range req.Keywords {
		slug := harvestSlug(kw)
		if req.Category != "" {
			slug = harvestSlug(req.Category)
		}
		kwDir := filepath.Join(h.root, res.Date, harvestSlug(kw))
		for page := 1; page <= perKeyword; page++ {
			file := filepath.Join(kwDir, fmt.Sprintf("page_%d.html", page))
			if st, err := os.Stat(file); err == nil && st.Size() > myntraTrivialBytes {
				res.Cached++
				res.Pages = append(res.Pages, MyntraPageLog{kw, page, "cached", ""})
				if cachedPage, rerr := os.ReadFile(file); rerr == nil {
					if products, _ := parseMyntraPage(cachedPage); len(products) == 0 {
						break // the empty page that ended this keyword before still ends it
					}
				}
				continue
			}
			if res.Fetched >= maxPages {
				res.StopReason = fmt.Sprintf("page limit of %d reached", maxPages)
				return
			}
			path := harvestPagePath(slug, page)
			if h.robots == nil {
				reason, err := h.loadRobots(ctx)
				if err != nil || reason != "" {
					res.StopReason = stopFrom(reason, err, "robots.txt")
					return
				}
			}
			if !h.robots.allowed(path) {
				res.Refused++
				res.RefusedPaths = append(res.RefusedPaths, strings.SplitN(path, "?", 2)[0])
				res.Pages = append(res.Pages, MyntraPageLog{kw, page, "refused by robots.txt", path})
				break
			}
			status, body, err := h.get(ctx, path)
			if err != nil {
				res.StopReason = stopFrom("", err, "the request")
				res.Pages = append(res.Pages, MyntraPageLog{kw, page, "stopped", err.Error()})
				return
			}
			if why := blockedAnswer(status, body); why != "" {
				res.StopReason = why
				res.Pages = append(res.Pages, MyntraPageLog{kw, page, "stopped", why})
				return
			}
			if status != http.StatusOK || len(body) <= myntraTrivialBytes {
				res.Pages = append(res.Pages, MyntraPageLog{kw, page, "error", fmt.Sprintf("answered %d, %d bytes - not kept", status, len(body))})
				break
			}
			if err := os.MkdirAll(kwDir, 0o755); err != nil {
				res.StopReason = "cannot write raw pages: " + err.Error()
				return
			}
			if err := os.WriteFile(file, body, 0o644); err != nil {
				res.StopReason = "cannot write raw pages: " + err.Error()
				return
			}
			res.Fetched++
			res.Pages = append(res.Pages, MyntraPageLog{kw, page, "fetched", ""})
			if products, _ := parseMyntraPage(body); len(products) == 0 {
				break // an empty page ends this keyword
			}
		}
	}
}

func stopFrom(reason string, err error, what string) string {
	if reason != "" {
		return reason
	}
	if e, ok := err.(*myntraOutsideRefusal); ok {
		return "outside calls are not allowed for this client: " + e.reason
	}
	return what + " failed: " + err.Error()
}

// blockedAnswer names a block signal; "" means carry on.
func blockedAnswer(status int, body []byte) string {
	switch {
	case status == http.StatusTooManyRequests:
		return "stopped: Myntra answered 429 (too many requests)"
	case status == http.StatusForbidden || status == http.StatusUnauthorized:
		return fmt.Sprintf("stopped: Myntra answered %d (blocked)", status)
	case status >= 300 && status < 400:
		return fmt.Sprintf("stopped: Myntra answered a redirect (%d), not followed", status)
	case status >= 500:
		return fmt.Sprintf("stopped: Myntra answered %d", status)
	}
	if !bytes.Contains(body, []byte("__myx")) && !bytes.Contains(body, []byte("__m_initial_state")) && myntraCaptchaLook.Match(body) {
		return "stopped: the answer looks like a captcha or block page"
	}
	return ""
}

// ---------------------------------------------------------------------------
// Entry points: enqueue, re-parse, list
// ---------------------------------------------------------------------------

func validateHarvestRequest(req *MyntraHarvestRequest) error {
	var kws []string
	seen := map[string]bool{}
	for _, k := range req.Keywords {
		k = strings.Join(strings.Fields(k), " ")
		if k == "" || seen[strings.ToLower(k)] {
			continue
		}
		if len([]rune(k)) > myntraMaxKeywordRunes || harvestSlug(k) == "" {
			return &ValidationError{Code: harvestInvalidCode, Message: "A keyword must be letters or numbers, up to 60 characters."}
		}
		seen[strings.ToLower(k)] = true
		kws = append(kws, k)
	}
	if len(kws) == 0 {
		return &ValidationError{Code: harvestInvalidCode, Message: "Enter at least one keyword."}
	}
	if len(kws) > myntraMaxKeywords {
		return &ValidationError{Code: harvestInvalidCode, Message: fmt.Sprintf("At most %d keywords per run.", myntraMaxKeywords)}
	}
	req.Keywords = kws
	req.Category = strings.Join(strings.Fields(req.Category), " ")
	if req.Category != "" && (len([]rune(req.Category)) > myntraMaxKeywordRunes || harvestSlug(req.Category) == "") {
		return &ValidationError{Code: harvestInvalidCode, Message: "The category must be letters or numbers, up to 60 characters."}
	}
	if req.MaxPages < 0 {
		return &ValidationError{Code: harvestInvalidCode, Message: "Max pages must be a positive number."}
	}
	return nil
}

func harvestMaxPages(schema string, asked int) int {
	limit := GetSettingIntForSchema(schema, SettingMyntraHarvestMaxPages)
	if limit < 1 {
		limit = 20
	}
	if asked > 0 && asked < limit {
		return asked
	}
	return limit
}

// EnqueueMyntraHarvest queues a run as a background job. Refused while the
// client's setting is off, or while another run is queued or running.
func EnqueueMyntraHarvest(tenantID, actor string, req MyntraHarvestRequest) (string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	if !GetSettingBoolForSchema(schema, SettingMyntraHarvest) {
		return "", &ValidationError{Code: harvestOffCode, Message: "Market-price collection from Myntra is switched off."}
	}
	if err := validateHarvestRequest(&req); err != nil {
		return "", err
	}
	var open int
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT COUNT(*) FROM %s.async_jobs WHERE job_type = $1 AND status IN ('Pending','Leased')`, schema), myntraHarvestJobType).Scan(&open); err != nil {
		return "", err
	}
	if open > 0 {
		return "", &ValidationError{Code: harvestBusyCode, Message: "A Myntra run is already waiting or running."}
	}
	kws := make([]interface{}, len(req.Keywords))
	for i, k := range req.Keywords {
		kws[i] = k
	}
	ctx := WithJobActor(context.Background(), actor)
	return EnqueueJobContext(ctx, tenantID, myntraHarvestJobType, map[string]interface{}{
		"tenant_id": tenantID, "keywords": kws, "category": req.Category, "max_pages": req.MaxPages,
	}, "")
}

// ReparseMyntraHarvest is phase 2 alone, on the pages kept for a day
// (YYYY-MM-DD; empty = today) - no request leaves the server.
func ReparseMyntraHarvest(tenantID, date string) (MyntraParseSummary, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return MyntraParseSummary{}, err
	}
	if date == "" {
		date = time.Now().In(istZone).Format("2006-01-02")
	}
	if _, err := time.Parse("2006-01-02", date); err != nil {
		return MyntraParseSummary{}, &ValidationError{Code: harvestInvalidCode, Message: "The day must look like 2026-11-13."}
	}
	sum := parseMyntraDay(tenantID, schema, marketHarvestRoot(schema), date)
	if sum.Files == 0 {
		return sum, &ValidationError{Code: harvestNoPagesCode, Message: "No saved Myntra pages for " + date + "."}
	}
	return sum, nil
}

// MyntraHarvestRuns lists the latest runs with their logs, newest first.
func MyntraHarvestRuns(tenantID string, limit int) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	rows, err := db.DB.Query(fmt.Sprintf(
		`SELECT id, status, created_at, actor, last_error, result FROM %s.async_jobs WHERE job_type = $1 ORDER BY created_at DESC LIMIT $2`, schema),
		myntraHarvestJobType, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]interface{}{}
	for rows.Next() {
		var id, status, actor, lastErr string
		var created time.Time
		var result []byte
		if err := rows.Scan(&id, &status, &created, &actor, &lastErr, &result); err != nil {
			return nil, err
		}
		var parsed interface{}
		if len(result) > 0 {
			_ = json.Unmarshal(result, &parsed)
		}
		out = append(out, map[string]interface{}{"id": id, "status": status, "created_at": created, "actor": actor, "last_error": lastErr, "result": parsed})
	}
	return out, rows.Err()
}

// MyntraHarvestStatus is what the screen needs before it draws.
func MyntraHarvestStatus(tenantID string) map[string]interface{} {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return map[string]interface{}{"enabled": false}
	}
	interval := time.Duration(GetSettingIntForSchema(schema, SettingMyntraHarvestInterval)) * time.Second
	return map[string]interface{}{
		"enabled":          GetSettingBoolForSchema(schema, SettingMyntraHarvest),
		"max_pages":        harvestMaxPages(schema, 0),
		"interval_seconds": int((&myntraHarvester{interval: interval}).spacing() / time.Second),
	}
}

func init() {
	RegisterJobHandler(myntraHarvestJobType, func(ctx context.Context, schema string, job Job) (map[string]interface{}, error) {
		tenantID, _ := job.Payload["tenant_id"].(string)
		stopped := func(why string) map[string]interface{} {
			return (&MyntraHarvestResult{Pages: []MyntraPageLog{}, StopReason: why}).toMap()
		}
		if !GetSettingBoolForSchema(schema, SettingMyntraHarvest) {
			return stopped("Market-price collection from Myntra is switched off."), nil
		}
		var req MyntraHarvestRequest
		if ks, ok := job.Payload["keywords"].([]interface{}); ok {
			for _, k := range ks {
				if s, ok := k.(string); ok {
					req.Keywords = append(req.Keywords, s)
				}
			}
		}
		req.Category, _ = job.Payload["category"].(string)
		if f, ok := job.Payload["max_pages"].(float64); ok {
			req.MaxPages = int(f)
		}
		if err := validateHarvestRequest(&req); err != nil {
			return stopped(err.Error()), nil
		}
		h := newMyntraHarvester(schema, time.Duration(GetSettingIntForSchema(schema, SettingMyntraHarvestInterval))*time.Second)
		// Never an error: a retry would ask Myntra again. The log is the result.
		return h.run(ctx, tenantID, req, harvestMaxPages(schema, req.MaxPages)).toMap(), nil
	})
	SetJobMaxRuntime(myntraHarvestJobType, 90*time.Minute)
}

// harvestObservationID is stable per day and product, so a re-parse updates
// the row instead of adding a second one.
func harvestObservationID(date, productID, name string) string {
	if productID == "" {
		sum := sha1.Sum([]byte(name))
		productID = hex.EncodeToString(sum[:6])
	}
	return "MYN-" + strings.ReplaceAll(date, "-", "") + "-" + productID
}
