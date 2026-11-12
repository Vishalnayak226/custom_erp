package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"custom_erp/db"
	"custom_erp/engines"
)

// Stage 49.10.3 - the two-hostile-tenant harness.
//
// Two clients, provisioned the way a real one is, hold records that collide
// on everything a person types: the same record id, the same SKU, the same
// item and customer name, the same attachment's document, the same job id.
// Only a marker differs. Every baseline role of client A, and A's integration
// (extension) token, then probes reads, searches, counts, reports, record
// links, attachments, record metadata (a cache), jobs, the audit log, writes
// and the "does it exist over there?" error oracle - through the real route
// table and middleware - and B's marker must never come back.
//
// TestCrossTenantIsolationAndTokenSecurity (server_test.go) keeps the token
// and header-spoofing cases; this extends it to colliding identifiers, every
// role and the side channels.

const (
	harnessSharedID   = "HARN-SHARED-0001" // the same record id in both clients
	harnessSharedSKU  = "HARN-SKU-COLLIDE"
	harnessSharedName = "Harness Collide Shirt"
	harnessMarkerA    = "MARKER-A-7f3e"
	harnessMarkerB    = "MARKER-B-9c1d"
	harnessOnlyInB    = "HARN-ONLY-IN-B-0002"
)

type harnessClient struct{ tenant, schema string }

func provisionHarnessClient(t *testing.T, tenant string) harnessClient {
	t.Helper()
	c := harnessClient{tenant: tenant, schema: "tenant_" + tenant}
	drop := func() {
		db.DB.Exec(`DROP SCHEMA IF EXISTS ` + c.schema + ` CASCADE`)
		db.DB.Exec(`DELETE FROM public.tenant_lifecycle_events WHERE tenant_id = $1`, c.tenant)
		db.DB.Exec(`DELETE FROM public.tenants WHERE tenant_id = $1`, c.tenant)
	}
	drop()
	t.Cleanup(drop)
	// Registered in public.tenants with its own schema, like a real client.
	if _, err := engines.ProvisionTenantSchema(c.tenant, c.schema, ""); err != nil {
		t.Fatalf("provision %s: %v", tenant, err)
	}
	mustExec := func(q string, args ...interface{}) {
		if _, err := db.DB.Exec(q, args...); err != nil {
			t.Fatalf("%s: %s: %v", tenant, q, err)
		}
	}
	mustExec(`UPDATE ` + c.schema + `.module_entitlements SET enabled = TRUE WHERE module_key = 'reports'`)
	return c
}

func seedHarnessRecords(t *testing.T, c harnessClient, marker string, extra bool) {
	t.Helper()
	ins := func(id, doctype string, data map[string]interface{}) {
		b, _ := json.Marshal(data)
		if _, err := db.DB.Exec(`INSERT INTO `+c.schema+`.documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, 'Active', 'system')`,
			id, doctype, string(b)); err != nil {
			t.Fatalf("seed %s %s: %v", c.tenant, id, err)
		}
	}
	// No HSN code on purpose: the item shows on the Item Master
	// Completeness report, which is the report probe.
	ins(harnessSharedID, "Item", map[string]interface{}{"item_code": harnessSharedSKU, "sku": harnessSharedSKU, "name": harnessSharedName + " " + marker, "description": marker})
	ins(harnessSharedID+"-C", "Customer", map[string]interface{}{"customer_name": harnessSharedName, "name": harnessSharedName, "phone": "9000000001", "address": marker})
	// Both clients attach a file to a goods receipt with the shared id
	// (GRN is one of the record types that take attachments).
	ins(harnessSharedID+"-G", "GRN", map[string]interface{}{"grn_no": harnessSharedID + "-G", "remarks": marker})
	if _, err := db.DB.Exec(`UPDATE `+c.schema+`.documents SET status = 'Pending' WHERE id = $1`, harnessSharedID+"-G"); err != nil {
		t.Fatalf("seed GRN: %v", err)
	}
	attachmentID := "7c1f0b5e-0000-4000-8000-000000004a01"
	if extra {
		attachmentID = "7c1f0b5e-0000-4000-8000-000000004903"
	}
	if _, err := db.DB.Exec(`INSERT INTO `+c.schema+`.document_attachments
		(id, doctype, document_id, file_name, content_type, size_bytes, sha256, storage_key, uploaded_by)
		VALUES ($3, 'GRN', $1, $2, 'application/pdf', 10, repeat('a', 64), 'harness/none', 'system')`,
		harnessSharedID+"-G", marker+".pdf", attachmentID); err != nil {
		t.Fatalf("seed attachment: %v", err)
	}
	if extra {
		ins(harnessOnlyInB, "Item", map[string]interface{}{"item_code": "HARN-SKU-B-ONLY", "name": "B only " + marker})
		// The client's own record-type field: cached record metadata must
		// not carry it to the other client.
		if _, err := db.DB.Exec(`INSERT INTO `+c.schema+`.doctype_fields (doctype_name, fieldname, label, fieldtype)
			VALUES ('Item', 'harness_field_b', $1, 'Data')`, marker); err != nil {
			t.Fatalf("seed field: %v", err)
		}
	}
	// The same job id in both clients; the job list shows the type, so the
	// marker rides there.
	if _, err := db.DB.Exec(`INSERT INTO `+c.schema+`.async_jobs (id, job_type, payload, status) VALUES ('HARN-JOB-0001', $2, $1, 'Succeeded')`,
		fmt.Sprintf(`{"note":%q}`, marker), "harness_probe "+marker); err != nil {
		t.Fatalf("seed job: %v", err)
	}
	engines.LogAuditEvent(c.tenant, "system", "HARNESS_PROBE", "SUCCESS", "harness audit row "+marker)
}

func TestStage49103TwoHostileTenantsHarness(t *testing.T) {
	db.InitDB(testConnStr())
	stamp := time.Now().Format("150405")
	a := provisionHarnessClient(t, "harn_a_"+stamp)
	b := provisionHarnessClient(t, "harn_b_"+stamp)
	seedHarnessRecords(t, a, harnessMarkerA, false)
	seedHarnessRecords(t, b, harnessMarkerB, true)

	previous := http.DefaultServeMux
	http.DefaultServeMux = http.NewServeMux()
	registerRoutes()
	mux := http.DefaultServeMux
	t.Cleanup(func() { http.DefaultServeMux = previous })

	// Every baseline role of client A, plus its integration identity.
	type identity struct{ name, token string }
	var ids []identity
	for _, role := range []string{engines.RoleSuperAdmin, engines.RoleAdmin, engines.RoleStoreManager, engines.RoleCashier, "Accountant", "Supplier"} {
		uid := "harn-" + strings.ReplaceAll(strings.ToLower(role), " ", "-")
		if _, err := db.DB.Exec(`INSERT INTO `+a.schema+`.users (id, username, password_hash, email, role, status, location_code)
			VALUES ($1, $1, 'x', $3, $2, 'Active', 'HO')`, uid, role, uid+"@harness.invalid"); err != nil {
			t.Fatalf("seed %s: %v", role, err)
		}
		ids = append(ids, identity{role, engines.SignToken(uid, uid, role, a.tenant, "HO", 1)})
	}
	ids = append(ids, identity{"extension token", engines.SignExtensionToken(a.tenant, "Item", time.Hour)})
	engines.ResetLiveUserStateCache()
	admin := ids[0].token

	seq := 0
	do := func(token, method, path string, body interface{}, spoofB bool) *httptest.ResponseRecorder {
		seq++
		var reader *bytes.Reader
		if body != nil {
			raw, _ := json.Marshal(body)
			reader = bytes.NewReader(raw)
		} else {
			reader = bytes.NewReader(nil)
		}
		req := httptest.NewRequest(method, path, reader)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		if spoofB {
			req.Header.Set("X-Tenant-ID", b.tenant) // a hostile header naming the other client
		}
		req.RemoteAddr = fmt.Sprintf("198.51.%d.%d:4000", seq/250, seq%250+1)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	// Positive controls: the probes really reach client A's data, so an
	// empty answer below is isolation, not a broken probe.
	if rec := do(admin, "GET", "/api/v1/doc/Item/"+harnessSharedID, nil, false); rec.Code != 200 || !strings.Contains(rec.Body.String(), harnessMarkerA) {
		t.Fatalf("A's admin cannot read A's own item: %d %s", rec.Code, rec.Body.String())
	}
	rec := do(admin, "GET", "/api/v1/doc/Item?count=true&q="+harnessSharedSKU, nil, false)
	if rec.Code != 200 || rec.Header().Get("X-Total-Count") != "1" || !strings.Contains(rec.Body.String(), harnessMarkerA) {
		t.Fatalf("A's search for the shared SKU: %d count=%q %s", rec.Code, rec.Header().Get("X-Total-Count"), rec.Body.String())
	}
	for _, path := range []string{
		"/api/v1/reports/run/item-master-completeness",
		"/api/v1/attachments/GRN/" + harnessSharedID + "-G",
		"/api/v1/jobs",
		"/api/v1/logs/audit",
		"/api/v1/doc/Customer/" + harnessSharedID + "-C",
	} {
		if rec := do(admin, "GET", path, nil, false); rec.Code != 200 || !strings.Contains(rec.Body.String(), harnessMarkerA) {
			t.Fatalf("A's admin does not see A's own row at %s: %d %.300s", path, rec.Code, rec.Body.String())
		}
	}

	// Every identity, every probe, with and without a header naming B.
	probes := []string{
		"/api/v1/doc/Item/" + harnessSharedID,
		"/api/v1/doc/Item/" + harnessOnlyInB,
		"/api/v1/doc/Item?q=" + harnessSharedSKU,
		"/api/v1/doc/Item?q=Harness",
		"/api/v1/doc/Item?count=true&q=" + harnessSharedSKU,
		"/api/v1/doc/Customer?q=" + strings.ReplaceAll(harnessSharedName, " ", "%20"),
		"/api/v1/doc/Customer/" + harnessSharedID + "-C",
		"/api/v1/doc/Item/meta",
		"/api/v1/attachments/GRN/" + harnessSharedID + "-G",
		"/api/v1/attachments/GRN",
		"/api/v1/attachments/file/7c1f0b5e-0000-4000-8000-000000004903",
		"/api/v1/reports/run/item-master-completeness",
		"/api/v1/jobs",
		"/api/v1/logs/audit",
	}
	// Warm any per-client cache with B's own view first.
	bAdminID := "harn-b-admin"
	db.DB.Exec(`INSERT INTO `+b.schema+`.users (id, username, password_hash, email, role, status, location_code)
		VALUES ($1, $1, 'x', 'harn-b-admin@harness.invalid', 'Super Admin', 'Active', 'HO')`, bAdminID)
	engines.ResetLiveUserStateCache()
	bAdmin := engines.SignToken(bAdminID, bAdminID, engines.RoleSuperAdmin, b.tenant, "HO", 1)
	if rec := do(bAdmin, "GET", "/api/v1/doc/Item/meta", nil, false); rec.Code != 200 || !strings.Contains(rec.Body.String(), "harness_field_b") {
		t.Fatalf("B's own metadata does not carry B's field: %d %s", rec.Code, rec.Body.String())
	}
	for _, id := range ids {
		for _, path := range probes {
			for _, spoof := range []bool{false, true} {
				rec := do(id.token, "GET", path, nil, spoof)
				if strings.Contains(rec.Body.String(), harnessMarkerB) || strings.Contains(rec.Body.String(), "harness_field_b") {
					t.Errorf("%s (spoofed header %v) GET %s leaked client B: %d %s", id.name, spoof, path, rec.Code, rec.Body.String())
				}
				if strings.Contains(path, "count=true") && rec.Code == 200 && rec.Header().Get("X-Total-Count") != "1" {
					t.Errorf("%s GET %s counted %q rows, want 1 (A's own)", id.name, path, rec.Header().Get("X-Total-Count"))
				}
			}
		}
	}

	// The existence oracle: a record only B has must answer exactly like a
	// record nobody has - same status, same body (apart from the request's
	// own reference).
	strip := func(rec *httptest.ResponseRecorder) string {
		var body map[string]interface{}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			return fmt.Sprintf("%d %s", rec.Code, rec.Body.String())
		}
		delete(body, "correlation_id")
		out, _ := json.Marshal(body)
		return fmt.Sprintf("%d %s", rec.Code, out)
	}
	for _, id := range ids {
		for _, pair := range [][2]string{
			{"/api/v1/doc/Item/" + harnessOnlyInB, "/api/v1/doc/Item/HARN-NOWHERE-0003"},
			{"/api/v1/attachments/file/7c1f0b5e-0000-4000-8000-000000004903", "/api/v1/attachments/file/7c1f0b5e-0000-4000-8000-0000000049ff"},
		} {
			inB, nowhere := strip(do(id.token, "GET", pair[0], nil, true)), strip(do(id.token, "GET", pair[1], nil, true))
			if inB != nowhere {
				t.Errorf("%s: a record that exists only in B answers differently from one that exists nowhere:\n  %s -> %s\n  %s -> %s", id.name, pair[0], inB, pair[1], nowhere)
			}
		}
	}

	// Writes: A's administrator, aiming at B's records by their ids with a
	// header naming B, changes nothing in B.
	do(admin, "PUT", "/api/v1/doc/Item/"+harnessSharedID, map[string]interface{}{"name": "overwritten by A", "item_code": harnessSharedSKU}, true)
	do(admin, "DELETE", "/api/v1/doc/Item/"+harnessOnlyInB, nil, true)
	do(admin, "DELETE", "/api/v1/attachments/file/7c1f0b5e-0000-4000-8000-000000004903", nil, true)
	var bName string
	var bDeleted, attDeleted bool
	db.DB.QueryRow(`SELECT data->>'name' FROM `+b.schema+`.documents WHERE id = $1`, harnessSharedID).Scan(&bName)
	db.DB.QueryRow(`SELECT deleted_at IS NOT NULL FROM `+b.schema+`.documents WHERE id = $1`, harnessOnlyInB).Scan(&bDeleted)
	db.DB.QueryRow(`SELECT deleted_at IS NOT NULL FROM ` + b.schema + `.document_attachments WHERE id = '7c1f0b5e-0000-4000-8000-000000004903'`).Scan(&attDeleted)
	if !strings.Contains(bName, harnessMarkerB) || bDeleted || attDeleted {
		t.Fatalf("A's writes reached B: name=%q deleted=%v attachmentDeleted=%v", bName, bDeleted, attDeleted)
	}
}
