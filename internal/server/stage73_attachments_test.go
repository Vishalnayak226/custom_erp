package server

import (
	"bytes"
	"custom_erp/db"
	"custom_erp/engines"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// Stage 55.15b: a receipt is exactly as private as its expense claim. Runs the
// real route table, apiMiddleware and request envelope, with sessions that
// differ only in what the generic document API lets them read.
func TestAttachmentsFollowTheClaimsReadRule(t *testing.T) {
	db.InitDB(testConnStr())
	engines.SetMediaStoreDirForTest(t.TempDir())
	t.Cleanup(func() { engines.SetMediaStoreDirForTest("media_store") })
	previous := http.DefaultServeMux
	http.DefaultServeMux = http.NewServeMux()
	registerRoutes()
	mux := http.DefaultServeMux
	t.Cleanup(func() { http.DefaultServeMux = previous })

	const locA, locB = "__ATT_LOC_A__", "__ATT_LOC_B__"
	const empCode, claimID, employeeDocID = "__ATT_EMP_A__", "__TEST_ATT/SRV/CLAIM__", "__TEST_ATT_SRV_EMP__"
	owner, c1 := seedStage47User(t, engines.RoleCashier, locA)
	otherCashier, c2 := seedStage47User(t, engines.RoleCashier, locA)
	managerA, c3 := seedStage47User(t, engines.RoleStoreManager, locA)
	managerB, c4 := seedStage47User(t, engines.RoleStoreManager, locB)
	admin, c5 := seedStage47User(t, engines.RoleSuperAdmin, locB)
	cleanup := func() {
		_, _ = db.DB.Exec(`DELETE FROM tenant_default.document_attachments WHERE document_id = $1`, claimID)
		_, _ = db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id IN ($1, $2)`, claimID, employeeDocID)
	}
	cleanup()
	t.Cleanup(func() {
		cleanup()
		c1()
		c2()
		c3()
		c4()
		c5()
	})
	if _, err := db.DB.Exec(`INSERT INTO tenant_default.documents (id, doctype, data, status, created_by) VALUES
		($1, 'Employee', jsonb_build_object('code', $3::text, 'user_id', $4::text, 'location', $5::text), 'Active', 'admin'),
		($2, 'ExpenseClaim', jsonb_build_object('employee_id', $3::text, 'location', $5::text, 'amount', 100), 'Draft', 'admin')`,
		employeeDocID, claimID, empCode, owner, locA); err != nil {
		t.Fatalf("seed: %v", err)
	}
	tokens := map[string]string{
		owner: stage47Token(owner, engines.RoleCashier, locA), otherCashier: stage47Token(otherCashier, engines.RoleCashier, locA),
		managerA: stage47Token(managerA, engines.RoleStoreManager, locA), managerB: stage47Token(managerB, engines.RoleStoreManager, locB),
		admin: stage47Token(admin, engines.RoleSuperAdmin, locB),
	}
	seq := 0
	call := func(user, method, path string, body io.Reader, contentType string) *httptest.ResponseRecorder {
		seq++
		req := httptest.NewRequest(method, path, body)
		req.Header.Set("Authorization", "Bearer "+tokens[user])
		req.Header.Set("X-Tenant-ID", "default")
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		req.RemoteAddr = fmt.Sprintf("198.19.%d.%d:12345", seq/250, seq%250+1)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}
	upload := func(user, name string, content []byte) *httptest.ResponseRecorder {
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		part, _ := mw.CreateFormFile("file", name)
		_, _ = part.Write(content)
		_ = mw.Close()
		return call(user, http.MethodPost, "/api/v1/attachments/ExpenseClaim/"+url.PathEscape(claimID), &buf, mw.FormDataContentType())
	}

	img := image.NewRGBA(image.Rect(0, 0, 6, 6))
	img.Set(1, 1, color.RGBA{G: 255, A: 255})
	var receipt bytes.Buffer
	_ = jpeg.Encode(&receipt, img, nil)

	if r := upload(owner, "fake.jpg", []byte("not a picture, just text")); r.Code != http.StatusUnprocessableEntity || !strings.Contains(r.Body.String(), "EXP-0541") {
		t.Fatalf("text renamed .jpg: %d %s", r.Code, r.Body.String())
	}
	r := upload(owner, "taxi.jpg", receipt.Bytes())
	if r.Code != http.StatusCreated {
		t.Fatalf("owner upload: %d %s", r.Code, r.Body.String())
	}
	var att engines.DocumentAttachment
	_ = json.Unmarshal(r.Body.Bytes(), &att)
	fileURL := "/api/v1/attachments/file/" + att.ID
	listURL := "/api/v1/attachments/ExpenseClaim/" + url.PathEscape(claimID)

	// Everyone the document API lets read the claim can read the receipt.
	for _, user := range []string{owner, managerA, admin} {
		if r := call(user, http.MethodGet, "/api/v1/doc/ExpenseClaim/"+url.PathEscape(claimID), nil, ""); r.Code != http.StatusOK {
			t.Fatalf("precondition: %s should read the claim, got %d", user, r.Code)
		}
		r := call(user, http.MethodGet, listURL, nil, "")
		var list struct {
			Attachments []struct {
				ID        string `json:"id"`
				CanRemove bool   `json:"can_remove"`
			}
			CanAdd bool `json:"can_add"`
		}
		_ = json.Unmarshal(r.Body.Bytes(), &list)
		if r.Code != http.StatusOK || len(list.Attachments) != 1 || list.Attachments[0].ID != att.ID {
			t.Fatalf("%s list: %d %s", user, r.Code, r.Body.String())
		}
		if wantRemove := user != managerA; list.Attachments[0].CanRemove != wantRemove {
			t.Errorf("%s can_remove = %v, want %v", user, list.Attachments[0].CanRemove, wantRemove)
		}
		f := call(user, http.MethodGet, fileURL, nil, "")
		if f.Code != http.StatusOK || !bytes.Equal(f.Body.Bytes(), receipt.Bytes()) {
			t.Fatalf("%s download: %d", user, f.Code)
		}
		if f.Header().Get("Content-Type") != "image/jpeg" || f.Header().Get("X-Content-Type-Options") != "nosniff" ||
			!strings.HasPrefix(f.Header().Get("Content-Disposition"), "inline;") || !strings.Contains(f.Header().Get("Content-Disposition"), "taxi.jpg") {
			t.Errorf("%s download headers: %v", user, f.Header())
		}
	}

	// Another store's manager and a colleague at the same store cannot read
	// the claim through the document API - and so get nothing here either.
	for _, user := range []string{managerB, otherCashier} {
		// Stage 49.4.7: refused as "not found", the same as a claim that does not exist.
		if r := call(user, http.MethodGet, "/api/v1/doc/ExpenseClaim/"+url.PathEscape(claimID), nil, ""); r.Code != http.StatusNotFound {
			t.Fatalf("precondition: %s should not read the claim, got %d", user, r.Code)
		}
		if r := call(user, http.MethodGet, listURL, nil, ""); r.Code != http.StatusForbidden {
			t.Errorf("%s list: %d %s", user, r.Code, r.Body.String())
		}
		if r := call(user, http.MethodGet, fileURL, nil, ""); r.Code != http.StatusForbidden || bytes.Contains(r.Body.Bytes(), receipt.Bytes()[:16]) {
			t.Errorf("%s download: %d", user, r.Code)
		}
		if r := call(user, http.MethodGet, "/api/v1/attachments/ExpenseClaim?ids="+url.QueryEscape(claimID), nil, ""); r.Code != http.StatusOK || strings.Contains(r.Body.String(), claimID) {
			t.Errorf("%s counts should leave the claim out: %d %s", user, r.Code, r.Body.String())
		}
		if r := upload(user, "other.png", receipt.Bytes()); r.Code != http.StatusForbidden {
			t.Errorf("%s upload: %d %s", user, r.Code, r.Body.String())
		}
		if r := call(user, http.MethodDelete, fileURL, nil, ""); r.Code != http.StatusForbidden {
			t.Errorf("%s remove: %d %s", user, r.Code, r.Body.String())
		}
	}

	// Reading is not removing: the approving manager cannot take the
	// claimant's receipt off; the claimant can, while it is Draft.
	if r := call(managerA, http.MethodDelete, fileURL, nil, ""); r.Code != http.StatusForbidden {
		t.Errorf("manager removing the claimant's receipt: %d %s", r.Code, r.Body.String())
	}
	if r := call(owner, http.MethodGet, "/api/v1/attachments/ExpenseClaim?ids="+url.QueryEscape(claimID), nil, ""); !strings.Contains(r.Body.String(), `"`+claimID+`":1`) {
		t.Errorf("owner counts: %s", r.Body.String())
	}
	if r := call(owner, http.MethodPost, fileURL+"/remove", nil, ""); r.Code != http.StatusOK {
		t.Fatalf("owner remove: %d %s", r.Code, r.Body.String())
	}
	if r := call(owner, http.MethodGet, fileURL, nil, ""); r.Code != http.StatusNotFound {
		t.Errorf("removed receipt still served: %d", r.Code)
	}
	var audited int
	_ = db.DB.QueryRow(`SELECT COUNT(*) FROM tenant_default.audit_logs WHERE details LIKE $1`, "%attachment "+att.ID+"%").Scan(&audited)
	if audited < 2 {
		t.Errorf("upload and removal should both be audit-logged, found %d", audited)
	}

	// A 3 MB scan gets past the shared request envelope's 2 MB default even
	// though the claim number holds "/" (sent as %2F), and a PDF comes back
	// as a download rather than inline.
	scan := append([]byte("%PDF-1.4\n"), bytes.Repeat([]byte("0"), 3<<20)...)
	r = upload(owner, "hotel bill.pdf", scan)
	if r.Code != http.StatusCreated {
		t.Fatalf("3 MB PDF: %d %s", r.Code, r.Body.String())
	}
	var pdf engines.DocumentAttachment
	_ = json.Unmarshal(r.Body.Bytes(), &pdf)
	f := call(admin, http.MethodGet, "/api/v1/attachments/file/"+pdf.ID, nil, "")
	if f.Code != http.StatusOK || f.Body.Len() != len(scan) || f.Header().Get("Content-Type") != "application/pdf" ||
		!strings.HasPrefix(f.Header().Get("Content-Disposition"), "attachment;") {
		t.Errorf("PDF download: %d %d bytes %v", f.Code, f.Body.Len(), f.Header())
	}
	if r := upload(owner, "huge.pdf", append([]byte("%PDF-1.4\n"), make([]byte, 5<<20)...)); r.Code != http.StatusRequestEntityTooLarge || !strings.Contains(r.Body.String(), "EXP-0542") {
		t.Errorf("over 5 MB: %d %s", r.Code, r.Body.String())
	}
}
