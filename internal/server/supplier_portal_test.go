package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"custom_erp/db"

	"golang.org/x/crypto/bcrypt"
)

// Stage 26.4.10 regression tests for the supplier portal.
//
// The portal is a limited-role login rather than a separate application, so
// the security property that makes that shape acceptable is row-level
// scoping: a Supplier session must reach its OWN submissions and nothing
// else. Doctype-level RBAC does not provide that on its own - every supplier
// has read access to the same doctype - so these tests exist specifically to
// hold that line, at the handleGenericDoc choke point where it is enforced.

// seedVendor creates the Vendor master a supplier login points at. Required
// because SupplierSubmission.supplier_code is a Link field, and the Stage
// 13.13 link validator (META-0198) rejects a submission naming a vendor that
// does not exist - correctly, which is why the test seeds one rather than
// working around the check.
func seedVendor(t *testing.T, code string) func() {
	t.Helper()
	db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id = $1 AND doctype = 'Vendor'`, code)
	if _, err := db.DB.Exec(
		`INSERT INTO tenant_default.documents (id, doctype, data, status, created_by)
		 VALUES ($1, 'Vendor', $2, 'Active', 'system')`,
		code, `{"name":"Supplier Portal Test Vendor","status":"Active"}`); err != nil {
		t.Fatalf("failed to seed vendor %s: %v", code, err)
	}
	return func() { db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id = $1 AND doctype = 'Vendor'`, code) }
}

// seedSupplierUser creates a Supplier-role login tied to a vendor code and
// returns its password plus a cleanup.
func seedSupplierUser(t *testing.T, id, vendorCode string) (string, func()) {
	t.Helper()
	pw := randomTestPassword()
	hash, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("failed to hash test password: %v", err)
	}
	db.DB.Exec(`DELETE FROM tenant_default.users WHERE id = $1`, id)
	if _, err := db.DB.Exec(
		`INSERT INTO tenant_default.users (id, username, password_hash, email, role, status, supplier_code)
		 VALUES ($1, $1, $2, $3, 'Supplier', 'Active', $4)`, id, string(hash), id+"@supplier.test", vendorCode); err != nil {
		t.Fatalf("failed to seed supplier user: %v", err)
	}
	return pw, func() { db.DB.Exec(`DELETE FROM tenant_default.users WHERE id = $1`, id) }
}

// plainSessionToken logs in a non-MFA role (Supplier is not MFA-mandatory -
// engines.RequiresMFA covers HR/Admin only) and returns the session token.
func plainSessionToken(t *testing.T, user, pw, ip string) string {
	t.Helper()
	rec := doRequestFromIP(t, apiMiddleware(handleLogin), "POST", "/api/v1/login", "", map[string]string{
		"username": user, "password": pw,
	}, ip)
	if rec.Code != http.StatusOK {
		t.Fatalf("login failed: status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode login response: %v", err)
	}
	tok, _ := resp["token"].(string)
	if tok == "" {
		t.Fatalf("expected a session token for a non-MFA role, got: %s", rec.Body.String())
	}
	return tok
}

// docRequest drives handleGenericDoc with the path values its routing would
// normally supply.
func docRequest(t *testing.T, method, doctype, id, token string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	path := "/api/v1/doc/" + doctype
	if id != "" {
		path += "/" + id
	}
	req := newJSONRequest(t, method, path, token, body)
	req.SetPathValue("doctype", doctype)
	if id != "" {
		req.SetPathValue("id", id)
	}
	rec := httptest.NewRecorder()
	apiMiddleware(handleGenericDoc)(rec, req)
	return rec
}

func newJSONRequest(t *testing.T, method, path, token string, body interface{}) *http.Request {
	t.Helper()
	var req *http.Request
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("failed to marshal body: %v", err)
		}
		req = httptest.NewRequest(method, path, bytes.NewReader(b))
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return req
}

func TestSupplierPortalRowScoping(t *testing.T) {
	db.InitDB(testConnStr())

	const (
		supplierAUser = "__supplierportal_a__"
		supplierBUser = "__supplierportal_b__"
		vendorA       = "__SUPPLIERPORTAL_VENDOR_A"
		vendorB       = "__SUPPLIERPORTAL_VENDOR_B"
		itemCode      = "__SUPPLIERPORTAL_ITEM"
		subA          = "__SUPPLIERPORTAL_SUB_A"
	)

	defer seedVendor(t, vendorA)()
	defer seedVendor(t, vendorB)()

	pwA, cleanupA := seedSupplierUser(t, supplierAUser, vendorA)
	defer cleanupA()
	pwB, cleanupB := seedSupplierUser(t, supplierBUser, vendorB)
	defer cleanupB()

	// A real Item for the submission's product_id link to point at.
	db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id = ANY($1)`,
		"{"+itemCode+","+subA+","+itemCode+"::en}")
	db.DB.Exec(`INSERT INTO tenant_default.documents (id, doctype, data, status, created_by)
	            VALUES ($1, 'Item', $2, 'Active', 'system')`,
		itemCode, `{"name":"Supplier Portal Test Item","hsn_code":"6109","gst_rate":18}`)
	defer func() {
		db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id IN ($1, $2, $3)`, itemCode, subA, itemCode+"::en")
	}()

	tokenA := plainSessionToken(t, supplierAUser, pwA, mfaTestIP())
	tokenB := plainSessionToken(t, supplierBUser, pwB, mfaTestIP())

	// 1. Supplier A files a submission. Note it deliberately claims vendorB -
	// the server must overwrite that with A's own code rather than honour it.
	createRec := docRequest(t, http.MethodPost, "SupplierSubmission", "", tokenA, map[string]interface{}{
		"id":            subA,
		"code":          subA,
		"supplier_code": vendorB,
		"product_id":    itemCode,
		"language":      "en",
		"title":         "Supplier A's product copy",
		"short_desc":    "From supplier A",
		"status":        "Draft",
	})
	if createRec.Code != http.StatusOK && createRec.Code != http.StatusCreated {
		t.Fatalf("supplier A could not create a submission: status=%d body=%s", createRec.Code, createRec.Body.String())
	}

	var storedSupplier string
	if err := db.DB.QueryRow(
		`SELECT COALESCE(data->>'supplier_code', '') FROM tenant_default.documents WHERE id = $1`, subA).Scan(&storedSupplier); err != nil {
		t.Fatalf("could not read back the submission: %v", err)
	}
	if storedSupplier != vendorA {
		t.Fatalf("supplier_code was taken from the payload, not the session: got %q, want %q - a supplier can file under another supplier's name", storedSupplier, vendorA)
	}

	// 2. Supplier A sees its own submission in the list.
	listA := docRequest(t, http.MethodGet, "SupplierSubmission", "", tokenA, nil)
	if listA.Code != http.StatusOK {
		t.Fatalf("supplier A list failed: status=%d body=%s", listA.Code, listA.Body.String())
	}
	if !containsID(listA.Body.String(), subA) {
		t.Errorf("supplier A cannot see its own submission: %s", listA.Body.String())
	}

	// 3. Supplier B must NOT see it. This is the whole reason row scoping
	// exists - both suppliers hold identical doctype-level read permission.
	listB := docRequest(t, http.MethodGet, "SupplierSubmission", "", tokenB, nil)
	if listB.Code != http.StatusOK {
		t.Fatalf("supplier B list failed: status=%d body=%s", listB.Code, listB.Body.String())
	}
	if containsID(listB.Body.String(), subA) {
		t.Errorf("supplier B can list another supplier's submission: %s", listB.Body.String())
	}

	// 4. Nor read it directly by id, which is the obvious way around a list
	// filter.
	readB := docRequest(t, http.MethodGet, "SupplierSubmission", subA, tokenB, nil)
	if readB.Code == http.StatusOK {
		t.Errorf("supplier B can read another supplier's submission by id: %s", readB.Body.String())
	}

	// 5. Nor overwrite it by posting to its id.
	writeB := docRequest(t, http.MethodPost, "SupplierSubmission", subA, tokenB, map[string]interface{}{
		"code":       subA,
		"product_id": itemCode,
		"language":   "en",
		"title":      "Hijacked by supplier B",
		"status":     "Draft",
	})
	if writeB.Code == http.StatusOK || writeB.Code == http.StatusCreated {
		t.Errorf("supplier B can overwrite another supplier's submission: %s", writeB.Body.String())
	}
	var titleAfter string
	db.DB.QueryRow(`SELECT COALESCE(data->>'title', '') FROM tenant_default.documents WHERE id = $1`, subA).Scan(&titleAfter)
	if titleAfter != "Supplier A's product copy" {
		t.Errorf("supplier A's title was changed by supplier B: %q", titleAfter)
	}
}

// TestSupplierSubmissionApprovalCreatesDraftContent covers the QC half: an
// approved submission becomes ProductContent, and lands as a Draft so it
// still has to clear ProductContent's own approval gate before anything an
// outside party wrote can reach a live channel.
func TestSupplierSubmissionApprovalCreatesDraftContent(t *testing.T) {
	db.InitDB(testConnStr())

	const (
		supplierUser = "__supplierqc_supplier__"
		vendorCode   = "__SUPPLIERQC_VENDOR"
		itemCode     = "__SUPPLIERQC_ITEM"
		subID        = "__SUPPLIERQC_SUB"
		contentID    = itemCode + "::en"
	)

	defer seedVendor(t, vendorCode)()

	pw, cleanup := seedSupplierUser(t, supplierUser, vendorCode)
	defer cleanup()

	db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id IN ($1, $2, $3)`, itemCode, subID, contentID)
	db.DB.Exec(`INSERT INTO tenant_default.documents (id, doctype, data, status, created_by)
	            VALUES ($1, 'Item', $2, 'Active', 'system')`,
		itemCode, `{"name":"Supplier QC Test Item","hsn_code":"6109","gst_rate":18}`)
	defer func() {
		db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id IN ($1, $2, $3)`, itemCode, subID, contentID)
	}()

	supplierToken := plainSessionToken(t, supplierUser, pw, mfaTestIP())
	createRec := docRequest(t, http.MethodPost, "SupplierSubmission", "", supplierToken, map[string]interface{}{
		"id":         subID,
		"code":       subID,
		"product_id": itemCode,
		"language":   "en",
		"title":      "Vendor-written title",
		"short_desc": "Vendor-written short description",
		"tags":       "vendor,supplied",
		"status":     "Draft",
	})
	if createRec.Code != http.StatusOK && createRec.Code != http.StatusCreated {
		t.Fatalf("submission create failed: status=%d body=%s", createRec.Code, createRec.Body.String())
	}

	// The supplier submits it for QC; an HR/Admin decides. Both go through the
	// existing generic approval endpoints - the portal adds no approval engine
	// of its own.
	submitRec := doRequest(t, apiMiddleware(handleSubmitApproval), "POST", "/api/v1/approval/submit", supplierToken,
		map[string]string{"doctype": "SupplierSubmission", "document_id": subID})
	if submitRec.Code != http.StatusOK {
		t.Fatalf("submit for approval failed: status=%d body=%s", submitRec.Code, submitRec.Body.String())
	}

	const reviewer = "__supplierqc_reviewer__"
	reviewerPw, reviewerCleanup := seedMFATestUser(t, reviewer)
	defer reviewerCleanup()
	_, _, reviewerToken := enrollMFATestUser(t, reviewer, reviewerPw, mfaTestIP())

	decideRec := doRequest(t, apiMiddleware(handleDecideApproval), "POST", "/api/v1/approval/decide", reviewerToken,
		map[string]string{"doctype": "SupplierSubmission", "document_id": subID, "decision": "Approved", "comment": "Looks good"})
	if decideRec.Code != http.StatusOK {
		t.Fatalf("approval decision failed: status=%d body=%s", decideRec.Code, decideRec.Body.String())
	}

	// The approved submission must now exist as ProductContent...
	var contentStatus, contentTitle string
	err := db.DB.QueryRow(
		`SELECT status, COALESCE(data->>'title', '') FROM tenant_default.documents
		 WHERE id = $1 AND doctype = 'ProductContent' AND deleted_at IS NULL`, contentID).Scan(&contentStatus, &contentTitle)
	if err != nil {
		t.Fatalf("approved submission did not produce ProductContent %s: %v", contentID, err)
	}
	if contentTitle != "Vendor-written title" {
		t.Errorf("content title not carried across: got %q", contentTitle)
	}
	// ...as a Draft. An approved-on-arrival copy would let an outside party
	// publish to a live sales channel with no internal publish decision.
	if contentStatus != "Draft" {
		t.Errorf("supplier content landed as %q, expected Draft so it still needs internal approval", contentStatus)
	}
}

func containsID(body, id string) bool {
	return strings.Contains(body, id)
}
