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

// 297.2: the resumable upload over HTTP, and an import staged from one. A
// start needs the right to import the record type; a piece that does not
// start where the server got to is a 409 whose Upload-Offset header says where
// to carry on; another user's upload is not found.
func uploadCall(t *testing.T, handler http.Handler, token, method, path, ctype, body string, headers map[string]string, seq int) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", ctype)
	req.Header.Set("X-Tenant-ID", "default")
	req.RemoteAddr = fmt.Sprintf("198.26.%d.%d:4000", seq/250, seq%250+1)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestImportUploadAPI(t *testing.T) {
	handler, admin := moduleHTTPFixture(t)
	engines.SetMediaStoreDirForTest(t.TempDir())
	t.Cleanup(func() { engines.SetMediaStoreDirForTest("media_store") })
	suffix := stage47UniqueID()
	name := "api-up-" + suffix + ".csv"
	t.Cleanup(func() {
		db.DB.Exec(`DELETE FROM tenant_default.async_jobs WHERE id::text IN (SELECT data->'work'->>'job_id' FROM tenant_default.documents WHERE doctype = 'ImportBatch' AND data->>'file_name' = $1)`, name)
		db.DB.Exec(`DELETE FROM tenant_default.import_staging_rows WHERE batch_id IN (SELECT id FROM tenant_default.documents WHERE doctype = 'ImportBatch' AND data->>'file_name' = $1)`, name)
		db.DB.Exec(`DELETE FROM tenant_default.documents WHERE doctype = 'ImportBatch' AND data->>'file_name' = $1`, name)
		db.DB.Exec(`DELETE FROM tenant_default.resumable_uploads WHERE file_name = $1`, name)
	})
	seq := 0
	next := func() int { seq++; return seq }
	const jsonType, binType = "application/json", "application/octet-stream"
	csv := fmt.Sprintf("id,cart_number,sku,qty,sale_value,status\nUP-%[1]s-1,C1,S1,1,10,Open\nUP-%[1]s-2,C2,S2,2,20,Open\n", suffix)
	start := fmt.Sprintf(`{"file_name":%q,"size":%d,"purpose":"import","doctype":"POSCostingGap"}`, name, len(csv))

	if rec := uploadCall(t, handler, "", http.MethodPost, "/api/v1/uploads", jsonType, start, nil, next()); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token: %d %s", rec.Code, rec.Body.String())
	}
	cashierID, cleanup := seedStage47User(t, engines.RoleCashier, "")
	t.Cleanup(cleanup)
	cashier := stage47Token(cashierID, engines.RoleCashier, "")
	if rec := uploadCall(t, handler, cashier, http.MethodPost, "/api/v1/uploads", jsonType, start, nil, next()); rec.Code != http.StatusForbidden {
		t.Fatalf("cashier start: %d %s", rec.Code, rec.Body.String())
	}
	other := strings.Replace(start, `"purpose":"import"`, `"purpose":"media"`, 1)
	if rec := uploadCall(t, handler, admin, http.MethodPost, "/api/v1/uploads", jsonType, other, nil, next()); rec.Code == http.StatusCreated || !strings.Contains(rec.Body.String(), "DATAIM-0489") {
		t.Fatalf("another purpose: %d %s", rec.Code, rec.Body.String())
	}

	rec := uploadCall(t, handler, admin, http.MethodPost, "/api/v1/uploads", jsonType, start, nil, next())
	if rec.Code != http.StatusCreated || rec.Header().Get("Upload-Offset") != "0" {
		t.Fatalf("start: %d offset %q %s", rec.Code, rec.Header().Get("Upload-Offset"), rec.Body.String())
	}
	var u engines.ResumableUpload
	_ = json.Unmarshal(rec.Body.Bytes(), &u)
	if u.ID == "" || u.Size != int64(len(csv)) {
		t.Fatalf("started upload: %s", rec.Body.String())
	}
	path := "/api/v1/uploads/" + u.ID
	if rec = uploadCall(t, handler, admin, http.MethodPut, path, binType, csv[:10], nil, next()); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "DATAIM-0488") {
		t.Fatalf("piece with no offset: %d %s", rec.Code, rec.Body.String())
	}
	if rec = uploadCall(t, handler, admin, http.MethodPut, path, binType, csv[:10], map[string]string{"Upload-Offset": "0"}, next()); rec.Code != http.StatusOK || rec.Header().Get("Upload-Offset") != "10" {
		t.Fatalf("first piece: %d offset %q %s", rec.Code, rec.Header().Get("Upload-Offset"), rec.Body.String())
	}
	if rec = uploadCall(t, handler, admin, http.MethodPut, path, binType, csv[20:], map[string]string{"Upload-Offset": "20"}, next()); rec.Code != http.StatusConflict || rec.Header().Get("Upload-Offset") != "10" || !strings.Contains(rec.Body.String(), "DATAIM-0488") {
		t.Fatalf("gap: %d offset %q %s; want 409 carrying on from 10", rec.Code, rec.Header().Get("Upload-Offset"), rec.Body.String())
	}
	if rec = uploadCall(t, handler, cashier, http.MethodGet, path, jsonType, "", nil, next()); rec.Code == http.StatusOK || !strings.Contains(rec.Body.String(), "DATAIM-0489") {
		t.Fatalf("another user's GET: %d %s", rec.Code, rec.Body.String())
	}
	if rec = uploadCall(t, handler, admin, http.MethodGet, path, jsonType, "", nil, next()); rec.Code != http.StatusOK || rec.Header().Get("Upload-Offset") != "10" {
		t.Fatalf("GET: %d offset %q %s", rec.Code, rec.Header().Get("Upload-Offset"), rec.Body.String())
	}
	if rec = uploadCall(t, handler, admin, http.MethodPut, path, binType, csv[10:], map[string]string{"Upload-Offset": "10"}, next()); rec.Code != http.StatusOK {
		t.Fatalf("last piece: %d %s", rec.Code, rec.Body.String())
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &u)
	if u.Received != int64(len(csv)) || u.SHA256 == "" {
		t.Fatalf("completed upload: %s", rec.Body.String())
	}

	// The import is staged from the upload, its rows read in the background.
	create := fmt.Sprintf(`{"upload_id":%q,"doctype":"POSCostingGap"}`, u.ID)
	if rec = uploadCall(t, handler, admin, http.MethodPost, "/api/v1/import-batches", jsonType, `{"doctype":"POSCostingGap"}`, nil, next()); rec.Code == http.StatusOK {
		t.Fatalf("create with no upload id: %d %s", rec.Code, rec.Body.String())
	}
	if rec = uploadCall(t, handler, cashier, http.MethodPost, "/api/v1/import-batches", jsonType, create, nil, next()); rec.Code != http.StatusForbidden {
		t.Fatalf("cashier create: %d %s", rec.Code, rec.Body.String())
	}
	rec = uploadCall(t, handler, admin, http.MethodPost, "/api/v1/import-batches", jsonType, create, nil, next())
	var b engines.ImportBatchView
	_ = json.Unmarshal(rec.Body.Bytes(), &b)
	if rec.Code != http.StatusOK || b.ID == "" || b.Work == nil || b.Work.UploadID != u.ID || b.FileName != name {
		t.Fatalf("create from upload: %d %s", rec.Code, rec.Body.String())
	}
	// The answer says the read is running, so the screen puts it in the Tasks bar.
	if !b.WorkLive {
		t.Fatalf("create from upload: work_live missing - the Tasks bar would not show the read: %s", rec.Body.String())
	}
	// Sent twice (the answer was lost): the same batch, not a second one.
	rec = uploadCall(t, handler, admin, http.MethodPost, "/api/v1/import-batches", jsonType, create, nil, next())
	var again engines.ImportBatchView
	_ = json.Unmarshal(rec.Body.Bytes(), &again)
	if rec.Code != http.StatusOK || again.ID != b.ID || !again.AlreadyStaged {
		t.Fatalf("create sent twice: %d %s; want batch %s, already staged", rec.Code, rec.Body.String(), b.ID)
	}
}

// 297.2: the import screens' lookups (the template, what to load first) do
// not spend the ten-a-minute budget the file itself needs; the file does.
func TestImportLookupsKeepTheFileBudget(t *testing.T) {
	for _, path := range []string{"/api/v1/import/Customer/load-order", "/api/v1/import/Customer/template"} {
		if c, _ := rateLimitCategory(path, http.MethodGet); c == "bulk-upload" {
			t.Fatalf("GET %s is in the bulk-upload bucket; a lookup must not spend the file budget", path)
		}
	}
	if c, n := rateLimitCategory("/api/v1/import/Customer", http.MethodPost); c != "bulk-upload" || n != 10 {
		t.Fatalf("POST /api/v1/import/Customer = %s/%d, want bulk-upload/10", c, n)
	}
}
