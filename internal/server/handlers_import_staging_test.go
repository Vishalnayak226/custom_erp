package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"custom_erp/db"
	"custom_erp/engines"
)

// Stage 47.13.1: the staged-import API. Refusals first: an extension token,
// a role with no right on the record type, a missing batch, an oversize file;
// then one full run over HTTP on a real record type.

func stagedUpload(t *testing.T, handler http.Handler, token, doctype, csv string, seq int, extra map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	_ = mw.WriteField("doctype", doctype)
	fw, _ := mw.CreateFormFile("file", "stg.csv")
	_, _ = fw.Write([]byte(csv))
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/import-batches", &body)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-Tenant-ID", "default")
	req.RemoteAddr = fmt.Sprintf("198.24.%d.%d:4000", seq/250, seq%250+1)
	for k, v := range extra {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func stagedCall(t *testing.T, handler http.Handler, token, method, path string, seq int) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "default")
	req.RemoteAddr = fmt.Sprintf("198.25.%d.%d:4000", seq/250, seq%250+1)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestStage471311StagedImportAPI(t *testing.T) {
	handler, admin := moduleHTTPFixture(t)
	suffix := stage47UniqueID()
	t.Cleanup(func() {
		db.DB.Exec(`DELETE FROM tenant_default.import_staging_rows WHERE batch_id IN (SELECT id FROM tenant_default.documents WHERE doctype = 'ImportBatch' AND data->>'file_name' = 'stg.csv')`)
		db.DB.Exec(`DELETE FROM tenant_default.documents WHERE doctype = 'ImportBatch' AND data->>'file_name' = 'stg.csv'`)
		db.DB.Exec(`DELETE FROM tenant_default.documents WHERE doctype = 'POSCostingGap' AND id LIKE $1`, "API-"+suffix+"%")
	})
	seq := 0
	next := func() int { seq++; return seq }

	// Refused: no token at all.
	if rec := stagedUpload(t, handler, "", "POSCostingGap", "id\nx\n", next(), nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token: %d %s", rec.Code, rec.Body.String())
	}
	// Refused: a role that cannot create or update the record type.
	cashierID, cleanup := seedStage47User(t, engines.RoleCashier, "")
	t.Cleanup(cleanup)
	cashier := stage47Token(cashierID, engines.RoleCashier, "")
	if rec := stagedUpload(t, handler, cashier, "POSCostingGap", "id\nx\n", next(), nil); rec.Code != http.StatusForbidden {
		t.Fatalf("cashier upload: %d %s", rec.Code, rec.Body.String())
	}
	// Refused: an extension token, even with the right role.
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/import-batches", nil)
	r.Header.Set("Resolved-Purpose", "extension")
	handleImportBatchList(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("extension token list: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodPost, "/api/v1/import-batches/x/commit", nil)
	r.SetPathValue("id", "x")
	r.Header.Set("Resolved-Purpose", "extension")
	handleImportBatchCommit(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("extension token commit: %d %s", w.Code, w.Body.String())
	}
	// Refused: a batch that does not exist is refused with DATAIM-0481, never a 500.
	if rec := stagedCall(t, handler, admin, http.MethodGet, "/api/v1/import-batches/NOPE-"+suffix, next()); rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "DATAIM-0481") {
		t.Fatalf("missing batch: %d %s", rec.Code, rec.Body.String())
	}
	// Refused: over the size cap.
	big := "id,cart_number,sku,qty,sale_value,status\n" + strings.Repeat("A-"+suffix+",,,,1,\n", 400000)
	if rec := stagedUpload(t, handler, admin, "POSCostingGap", big, next(), nil); rec.Code == http.StatusOK {
		t.Fatalf("oversize accepted: %d", rec.Code)
	}

	// A full run: stage, check, preview, approve, commit, reconcile.
	csv := fmt.Sprintf("id,cart_number,sku,qty,sale_value,status\nAPI-%[1]s-1,C1,S1,1,10,Open\nAPI-%[1]s-2,C2,S2,2,20,Open\n", suffix)
	rec := stagedUpload(t, handler, admin, "POSCostingGap", csv, next(), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("stage: %d %s", rec.Code, rec.Body.String())
	}
	var b engines.ImportBatchView
	_ = json.Unmarshal(rec.Body.Bytes(), &b)
	if b.ID == "" || b.Counts.Rows != 2 {
		t.Fatalf("staged batch: %s", rec.Body.String())
	}
	for _, step := range []string{"validate", "preview", "approve", "commit", "reconcile"} {
		rec = stagedCall(t, handler, admin, http.MethodPost, "/api/v1/import-batches/"+b.ID+"/"+step, next())
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", step, rec.Code, rec.Body.String())
		}
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &b)
	if b.Reconcile == nil || !b.Reconcile.Balanced || b.Counts.Committed != 2 {
		t.Fatalf("reconcile: %s", rec.Body.String())
	}
	var n int
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM tenant_default.documents WHERE doctype = 'POSCostingGap' AND id LIKE $1`, "API-"+suffix+"%").Scan(&n); err != nil || n != 2 {
		t.Fatalf("records after commit: %d %v", n, err)
	}
	// A second commit is refused, not repeated.
	if rec = stagedCall(t, handler, admin, http.MethodPost, "/api/v1/import-batches/"+b.ID+"/commit", next()); rec.Code == http.StatusOK && !strings.Contains(rec.Body.String(), "already_committed") {
		t.Fatalf("second commit repeated: %s", rec.Body.String())
	}
	// The list shows it.
	rec = stagedCall(t, handler, admin, http.MethodGet, "/api/v1/import-batches", next())
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), b.ID) {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}
}
