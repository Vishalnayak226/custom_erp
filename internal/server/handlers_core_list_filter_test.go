package server

import (
	"custom_erp/db"
	"custom_erp/engines"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"
)

// 267.1: the Returns work queue asks the list for a status group
// (status=Requested&status=Approved...). A repeated status filter on the
// ReturnRequest list matches any of its values, so an open return past the
// first 200 rows is found and the count is the whole result; more than 20
// values, or any other repeated parameter, is still refused; and a cashier's
// list and count stay inside their store.
func TestReturnsQueueStatusGroupFilter(t *testing.T) {
	db.InitDB(testConnStr())
	tag := fmt.Sprintf("RQF%d", time.Now().UnixNano())
	cashierID := strings.ToLower(tag) + "_cashier"
	clean := func() {
		db.DB.Exec(`DELETE FROM tenant_default.documents WHERE doctype = 'ReturnRequest' AND id LIKE $1`, "%"+tag+"%")
		db.DB.Exec(`DELETE FROM tenant_default.users WHERE id = $1`, cashierID)
		engines.ResetLiveUserStateCache()
	}
	clean()
	defer clean()

	insert := func(id, status, store string) {
		t.Helper()
		raw, _ := json.Marshal(map[string]interface{}{"id": id, "code": id, "status": status, "return_location": store,
			"request_type": "Customer Return", "items": []interface{}{}})
		if _, err := db.DB.Exec(`INSERT INTO tenant_default.documents (id, doctype, data, status, created_by) VALUES ($1, 'ReturnRequest', $2, $3, 'admin')`, id, raw, status); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
	// 250 returns at HO in id order: all Closed but three open ones, every
	// one of them past row 200 - the old screen read 200 rows and never saw them.
	open := map[int]string{205: "Requested", 230: "Pickup Scheduled", 249: "QC Complete"}
	for i := 0; i < 250; i++ {
		status := "Closed"
		if s, ok := open[i]; ok {
			status = s
		}
		insert(fmt.Sprintf("RR-%s-%03d", tag, i), status, "HO")
	}
	// One open return at another store.
	otherStore := tag + "-B"
	insert("RR-"+tag+"-B", "Requested", otherStore)

	if _, err := db.DB.Exec(`INSERT INTO tenant_default.users (id, username, password_hash, role, status, location_code) VALUES ($1, $1, 'x', $2, 'Active', 'HO')`, cashierID, engines.RoleCashier); err != nil {
		t.Fatalf("seed cashier: %v", err)
	}
	engines.ResetLiveUserStateCache()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/doc/{doctype}", apiMiddleware(handleGenericDoc))
	admin := engines.SignToken("admin", "admin", engines.RoleSuperAdmin, "default", "HO", currentCredentialVersion("admin"))
	cashier := engines.SignToken(cashierID, cashierID, engines.RoleCashier, "default", "HO", currentCredentialVersion(cashierID))

	openGroup := []string{"Requested", "Approved", "Pickup Scheduled", "Received", "QC Complete"}
	query := func(statuses []string, extra string) string {
		v := url.Values{}
		for _, s := range statuses {
			v.Add("status", s)
		}
		v.Set("q", tag)
		v.Set("count", "true")
		v.Set("limit", "50")
		return "/api/v1/doc/ReturnRequest?" + v.Encode() + extra
	}
	list := func(token, path string) ([]string, string) {
		t.Helper()
		rec := doRequest(t, mux.ServeHTTP, http.MethodGet, path, token, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("list %s: %d %s", path, rec.Code, rec.Body.String())
		}
		var rows []map[string]interface{}
		if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
			t.Fatalf("list %s: %v", path, err)
		}
		ids := make([]string, len(rows))
		for i, r := range rows {
			ids[i] = fmt.Sprint(r["id"])
		}
		sort.Strings(ids)
		return ids, rec.Header().Get("X-Total-Count")
	}
	want := func(suffixes ...string) string {
		out := make([]string, len(suffixes))
		for i, s := range suffixes {
			out[i] = "RR-" + tag + "-" + s
		}
		sort.Strings(out)
		return strings.Join(out, ",")
	}

	// (1) The open group finds every open return, the three past row 200
	// and the other store's, and the count is the group's, not the page's.
	ids, total := list(admin, query(openGroup, ""))
	if got := strings.Join(ids, ","); got != want("205", "230", "249", "B") || total != "4" {
		t.Fatalf("open group (admin): rows %s total %s; want %s total 4", got, total, want("205", "230", "249", "B"))
	}

	// (2) One value is still plain equality; the count covers every page.
	ids, total = list(admin, query([]string{"Closed"}, ""))
	if len(ids) != 50 || total != "247" {
		t.Fatalf("Closed: %d rows total %s; want 50 rows total 247", len(ids), total)
	}

	// (3) Awaiting goods is two statuses.
	ids, total = list(admin, query([]string{"Approved", "Pickup Scheduled"}, ""))
	if got := strings.Join(ids, ","); got != want("230") || total != "1" {
		t.Fatalf("awaiting goods: rows %s total %s; want %s total 1", got, total, want("230"))
	}

	// (4) A cashier at HO sees HO's open returns only - the other store's
	// is in neither the rows nor the count.
	ids, total = list(cashier, query(openGroup, ""))
	if got := strings.Join(ids, ","); got != want("205", "230", "249") || total != "3" {
		t.Fatalf("open group (cashier at HO): rows %s total %s; want %s total 3", got, total, want("205", "230", "249"))
	}

	// (5) More than 20 values is refused, not turned into an open-ended list.
	many := make([]string, maxListFilterValues+1)
	for i := range many {
		many[i] = fmt.Sprintf("S%d", i)
	}
	if rec := doRequest(t, mux.ServeHTTP, http.MethodGet, query(many, ""), admin, nil); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("%d status values: %d %s; want 422", len(many), rec.Code, rec.Body.String())
	}

	// (6) Only status repeats, and only on this list: any other repeated
	// parameter stays ambiguous (49.4.4a).
	if rec := doRequest(t, mux.ServeHTTP, http.MethodGet, query(openGroup, "&return_location=HO&return_location="+url.QueryEscape(otherStore)), admin, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("repeated return_location: %d %s; want 400", rec.Code, rec.Body.String())
	}
	if rec := doRequest(t, mux.ServeHTTP, http.MethodGet, "/api/v1/doc/Customer?status=Active&status=Inactive", admin, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("repeated status on Customer: %d %s; want 400", rec.Code, rec.Body.String())
	}
}
