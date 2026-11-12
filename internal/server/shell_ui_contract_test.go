package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"custom_erp/db"
	"custom_erp/engines"
)

// Stage 47.14.5 over HTTP: the Roles screen's landing door, and the nav block
// the shell reads from /me/modules.
func TestStage47145RoleLandingDoorAndNavPayload(t *testing.T) {
	db.InitDB(testConnStr())
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/admin/roles/landing", apiMiddleware(handleRoleLanding))
	mux.HandleFunc("POST /api/v1/admin/roles/landing", apiMiddleware(handleRoleLanding))
	mux.HandleFunc("GET /api/v1/me/modules", apiMiddleware(handleMyModules))
	adminID, cleanupAdmin := seedStage47User(t, engines.RoleSuperAdmin, "")
	t.Cleanup(cleanupAdmin)
	cashierID, cleanupCashier := seedStage47User(t, engines.RoleCashier, "")
	t.Cleanup(cleanupCashier)
	admin := stage47Token(adminID, engines.RoleSuperAdmin, "")
	cashier := stage47Token(cashierID, engines.RoleCashier, "")
	const role = "Store Manager"
	t.Cleanup(func() { db.DB.Exec(`DELETE FROM tenant_default.role_landing WHERE role = $1`, role) })
	ip := mfaTestIP()

	if rec := doRequestFromIP(t, mux.ServeHTTP, "GET", "/api/v1/admin/roles/landing", cashier, nil, ip); rec.Code != http.StatusForbidden {
		t.Fatalf("a cashier must not read the landing settings: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doRequestFromIP(t, mux.ServeHTTP, "POST", "/api/v1/admin/roles/landing", cashier, map[string]string{"role": role, "view": "pos"}, ip); rec.Code != http.StatusForbidden {
		t.Fatalf("a cashier must not set a landing: %d", rec.Code)
	}
	if rec := doRequestFromIP(t, mux.ServeHTTP, "POST", "/api/v1/admin/roles/landing", admin, map[string]string{"role": role, "view": "nowhere"}, ip); rec.Code == http.StatusOK {
		t.Fatalf("an unknown screen was accepted")
	}
	rec := doRequestFromIP(t, mux.ServeHTTP, "POST", "/api/v1/admin/roles/landing", admin, map[string]string{"role": role, "view": "finance"}, ip)
	if rec.Code != http.StatusOK {
		t.Fatalf("set: %d %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Options []engines.LandingChoice `json:"options"`
		Roles   map[string]struct {
			View     string `json:"view"`
			Template bool   `json:"template"`
		} `json:"roles"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || len(got.Options) == 0 || got.Roles[role].View != "finance" {
		t.Fatalf("body %s (%v)", rec.Body.String(), err)
	}

	// The cashier's own payload carries its template landing and the labels.
	rec = doRequestFromIP(t, mux.ServeHTTP, "GET", "/api/v1/me/modules", cashier, nil, ip)
	var me struct {
		Nav engines.NavInfo `json:"nav"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &me); err != nil || me.Nav.Landing != "pos" || len(me.Nav.Modules)+len(me.Nav.Menus) == 0 {
		t.Fatalf("me/modules nav: %d %s (%v)", rec.Code, rec.Body.String(), err)
	}
}

// Stage 47.14.6: empty, error and permission states are three different looks.
// The browser twin is docs/qa/state-contract.cjs (computed styles); this reads
// the sources so the classes and colours cannot drift into one another.
func TestStage47146StatesAreDistinctInSource(t *testing.T) {
	css := readPublicAsset(t, "shell-steady.css")
	app := readPublicAsset(t, "app.js")
	nav := readPublicAsset(t, "shell-nav.js")
	rule := func(sel string) string {
		out := ""
		for _, m := range regexp.MustCompile(`(?m)^([^{}\n]+)\{([^}]*)\}`).FindAllStringSubmatch(css, -1) {
			if strings.Contains(m[1], sel) {
				out += m[2]
			}
		}
		if out == "" {
			t.Fatalf("shell-steady.css has no %s rule", sel)
		}
		return out
	}
	empty, errState, denied := rule(".state-empty"), rule(".state-error"), rule(".state-denied")
	if !strings.Contains(empty, "dashed") {
		t.Error("the empty state must be dashed")
	}
	if !strings.Contains(errState, "--danger") || !strings.Contains(denied, "--warning") {
		t.Error("error must use the danger tokens and permission the warning tokens")
	}
	if strings.Contains(errState, "--warning") || strings.Contains(denied, "--danger") || strings.Contains(empty, "--danger") || strings.Contains(empty, "--warning") {
		t.Error("the three states share a colour token")
	}
	if !strings.Contains(app, "state-' + ['denied', 'missing', 'error'][kind]") {
		t.Error("renderErrorPanel must set state-denied / state-missing / state-error from its kind")
	}
	if !strings.Contains(nav, "state-empty") {
		t.Error("shell-nav.js must tag the empty rows")
	}
	for _, s := range []string{empty, errState, denied} {
		if strings.Contains(strings.ToLower(s), "content: \"click") || strings.Contains(strings.ToLower(s), "how to") {
			t.Error("a state carries how-to text")
		}
	}
}

// Stage 47.10.4: tables over the gate render in chunks and keep their row
// numbers for screen readers; print and Ctrl+F get every row.
func TestStage47104BigTablesAreChunkedAccessibly(t *testing.T) {
	nav := readPublicAsset(t, "shell-nav.js")
	for _, want := range []string{"IntersectionObserver", "aria-rowcount", "aria-rowindex", "beforeprint", "e.key.toLowerCase() === 'f'", "CHUNK_MIN", "}, true);", "Show all "} {
		if !strings.Contains(nav, want) {
			t.Errorf("shell-nav.js lacks %s", want)
		}
	}
	// The behaviour itself (filter, sort and keyboard over a 5,000-row table) is
	// checked in a real browser by docs/qa/big-table-rows.cjs; this only keeps it present.
	if _, err := os.Stat(filepath.Join("..", "..", "docs", "qa", "big-table-rows.cjs")); err != nil {
		t.Errorf("the browser check for big tables is missing: %v", err)
	}
	if !strings.Contains(readPublicAsset(t, "app.js"), "applyShellNav") {
		t.Error("app.js never loads the shell-nav module")
	}
}

// 163.3: every PIM menu entry that borrows the module rule (data-like="menu-pim") also names the
// records its page reads, so a role that cannot read them never sees a screen that opens on an error.
func TestMenuPIMEntriesGateOnTheirOwnRecords(t *testing.T) {
	html := readPublicAsset(t, "index.html")
	app := readPublicAsset(t, "app.js")
	ids := regexp.MustCompile(`id="(menu-pim-[a-z-]+)"[^>]*data-like="menu-pim"`).FindAllStringSubmatch(html, -1)
	if len(ids) == 0 {
		t.Fatal("no PIM menu entries found")
	}
	open := map[string]bool{"menu-pim-dashboard": true, "menu-pim-media": true, "menu-pim-checklists": true}
	for _, m := range ids {
		if open[m[1]] {
			continue
		}
		if !strings.Contains(app, "'"+m[1]+"': { doctypes: [") {
			t.Errorf("%s borrows the PIM module rule and has no rule of its own in MENU_PERMISSION_MAP", m[1])
		}
	}
}
