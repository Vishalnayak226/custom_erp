package server

import (
	"custom_erp/db"
	"custom_erp/engines"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"testing"
	"time"
)

// 47.10.7 (adversarial journey #8): a record past page one is found by
// search, the list's total is the number of records paging actually returns,
// and the total and the pages agree again once writes made mid-paging land.
func TestStage47SearchCountAndPagesAgree(t *testing.T) {
	db.InitDB(testConnStr())
	tag := fmt.Sprintf("SRCHCONS-%d", time.Now().UnixNano())
	clean := func() {
		db.DB.Exec(`DELETE FROM tenant_default.documents WHERE doctype = 'Department' AND data->>'name' LIKE $1`, tag+"%")
	}
	clean()
	defer clean()
	insert := func(n int) {
		t.Helper()
		raw, _ := json.Marshal(map[string]interface{}{"code": fmt.Sprintf("%s-%03d", tag, n), "name": fmt.Sprintf("%s %03d", tag, n), "status": "Active"})
		if _, err := db.DB.Exec(`INSERT INTO tenant_default.documents (id, doctype, data, status, created_by) VALUES ($1, 'Department', $2, 'Active', 'admin')`, fmt.Sprintf("%s-%03d", tag, n), raw); err != nil {
			t.Fatalf("seed %d: %v", n, err)
		}
	}
	for i := 1; i <= 60; i++ {
		insert(i)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/doc/{doctype}", apiMiddleware(handleGenericDoc))
	token := engines.SignToken("admin", "admin", engines.RoleSuperAdmin, "default", "HO", currentCredentialVersion("admin"))
	page := func(q string, offset int) ([]map[string]interface{}, int) {
		t.Helper()
		rec := doRequest(t, mux.ServeHTTP, http.MethodGet, fmt.Sprintf("/api/v1/doc/Department?q=%s&count=true&limit=25&offset=%d", q, offset), token, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("list offset %d: %d %s", offset, rec.Code, rec.Body.String())
		}
		var rows []map[string]interface{}
		json.Unmarshal(rec.Body.Bytes(), &rows)
		total, _ := strconv.Atoi(rec.Header().Get("X-Total-Count"))
		return rows, total
	}
	walk := func() (map[string]bool, int) {
		seen := map[string]bool{}
		_, total := page(tag, 0)
		for offset := 0; offset < total; offset += 25 {
			rows, _ := page(tag, offset)
			for _, r := range rows {
				seen[fmt.Sprint(r["id"])] = true
			}
		}
		return seen, total
	}

	seen, total := walk()
	if total != 60 || len(seen) != 60 {
		t.Fatalf("total %d, distinct records paged %d; both must be 60", total, len(seen))
	}
	// A record from the last page, whatever the list's order, is found by search.
	last, _ := page(tag, 50)
	if len(last) == 0 {
		t.Fatal("the last page is empty")
	}
	want := fmt.Sprint(last[len(last)-1]["id"])
	hit, n := page(want, 0)
	if n != 1 || len(hit) != 1 || fmt.Sprint(hit[0]["id"]) != want {
		t.Fatalf("search for %s from the last page: total %d, rows %v", want, n, hit)
	}

	// Writes land between two pages: once they have, total and pages agree again.
	page(tag, 0)
	for i := 61; i <= 65; i++ {
		insert(i)
	}
	seen, total = walk()
	if total != 65 || len(seen) != 65 {
		t.Fatalf("after five writes mid-paging: total %d, distinct records paged %d; both must be 65", total, len(seen))
	}
}
