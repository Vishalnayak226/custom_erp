package server

import (
	"custom_erp/db"
	"custom_erp/engines"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"
)

// Stage 49.3.8, second slice: the generated authorization cases beyond
// no-token / wrong-role (stage49_3_8_generated_authz_test.go) - export,
// owner / self / location, state - and the registry checks that fail CI on an
// unclassified role or an unlisted sensitive field.

// exportRoutePattern names the routes that hand data out of the system:
// exports, downloads, CSV feeds, prints and the forensic audit export.
var exportRoutePattern = regexp.MustCompile(`export|download|\.csv$|/print(/|$)|forensic`)

// TestStage4938ExportRoutesRefuseWrongTenantAndWrongRole: every export /
// download route, as a token of a client this server does not host and as a
// role the route does not allow, is refused at the middleware - before any
// handler could build a file.
func TestStage4938ExportRoutesRefuseWrongTenantAndWrongRole(t *testing.T) {
	db.InitDB(testConnStr())
	routes, err := RouteAccessTable("routes.go")
	if err != nil {
		t.Fatal(err)
	}
	reached := map[string]bool{}
	mux := http.NewServeMux()
	var exports []RouteAccess
	for _, rt := range routes {
		if !exportRoutePattern.MatchString(rt.Path) || rt.Level == "no session" {
			continue
		}
		exports = append(exports, rt)
		pattern := rt.Pattern
		mux.HandleFunc(pattern, apiMiddleware(func(w http.ResponseWriter, r *http.Request) {
			reached[pattern] = true
			w.WriteHeader(http.StatusOK)
		}))
	}
	if len(exports) < 20 {
		t.Fatalf("only %d export routes found - the pattern or the route reading has broken", len(exports))
	}
	users := seedGeneratedCaseUsers(t)
	ghostTenant := "purged-" + time.Now().Format("150405.000000")
	wrongTenant := engines.SignToken("authz-ghost", "authz-ghost", engines.RoleSuperAdmin, ghostTenant, "HO", 1)

	param := regexp.MustCompile(`\{[^}]*\}`)
	path := func(p string) string {
		return param.ReplaceAllStringFunc(p, func(m string) string {
			if strings.HasSuffix(m, "...}") {
				return "a/b"
			}
			return "x1"
		})
	}
	tenantCases, roleCases := 0, 0
	for _, rt := range exports {
		method := rt.Method
		if method == "ANY" {
			method = http.MethodGet
		}
		if !publicRoutes[path(rt.Path)] {
			tenantCases++
			rec := doRequestFromIP(t, mux.ServeHTTP, method, path(rt.Path), wrongTenant, nil, mfaTestIP())
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("%s as an unregistered client: %d %s, want 401", rt.Pattern, rec.Code, strings.TrimSpace(rec.Body.String()))
			}
		}
		if rt.Roles == nil {
			continue
		}
		allowed := map[string]bool{}
		for _, r := range rt.Roles {
			allowed[r] = true
		}
		for role, token := range users {
			if allowed[role] {
				continue
			}
			roleCases++
			rec := doRequestFromIP(t, mux.ServeHTTP, method, path(rt.Path), token, nil, mfaTestIP())
			if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "GLOBAL-0011") {
				t.Errorf("%s as %s (not allowed): %d %s, want 403 GLOBAL-0011", rt.Pattern, role, rec.Code, strings.TrimSpace(rec.Body.String()))
			}
		}
	}
	for pattern := range reached {
		t.Errorf("%s: a refused export call reached its handler", pattern)
	}
	t.Logf("export routes: %d; unregistered-client calls refused: %d; wrong-role calls refused: %d", len(exports), tenantCases, roleCases)
}

// seedGeneratedCaseUsers creates one Active user per role the generated
// cases call as, and returns role -> token.
func seedGeneratedCaseUsers(t *testing.T) map[string]string {
	t.Helper()
	users := map[string]string{}
	suffix := contractUniqueID()
	for _, role := range []string{engines.RoleCashier, engines.RoleStoreManager, engines.RoleAdmin} {
		id := "authz-cases-" + strings.ToLower(strings.ReplaceAll(role, " ", "-")) + "-" + suffix
		if _, err := db.DB.Exec(`INSERT INTO tenant_default.users (id, username, password_hash, email, role, status, location_code) VALUES ($1, $1, 'x', $2, $3, 'Active', 'HO')`, id, id+"@erp.invalid", role); err != nil {
			t.Fatalf("seed %s: %v", role, err)
		}
		t.Cleanup(func() { db.DB.Exec(`DELETE FROM tenant_default.users WHERE id = $1`, id) })
		users[role] = engines.SignToken(id, id, role, "default", "HO", 1)
	}
	return users
}

// TestStage4938OwnerSelfLocationCasesAnswerNotFound: for every doctype the
// scope registry confines (another store's, another employee's, another
// owner's record), a Cashier at HO with no Employee link and no owner is
// refused a read, an update, a delete and the attachments list of a record
// seeded out of their scope - and, where their role is otherwise permitted,
// refused as 404 GLOBAL-0004 (Stage 49.4.7's disclosure rule), never 200.
func TestStage4938OwnerSelfLocationCasesAnswerNotFound(t *testing.T) {
	db.InitDB(testConnStr())
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/doc/{doctype}", apiMiddleware(handleGenericDoc))
	mux.HandleFunc("/api/v1/doc/{doctype}/{id}", apiMiddleware(handleGenericDoc))
	mux.HandleFunc("GET /api/v1/attachments/{doctype}/{id}", apiMiddleware(handleListAttachments))
	users := seedGeneratedCaseUsers(t)
	suffix := contractUniqueID()
	ip := mfaTestIP()

	type outcome struct {
		status int
		code   string
	}
	call := func(method, path, token string, body interface{}) outcome {
		rec := doRequestFromIP(t, mux.ServeHTTP, method, path, token, body, ip)
		var env struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &env)
		return outcome{rec.Code, env.Code}
	}
	cases, skipped := 0, []string{}
	for _, doctype := range engines.ScopedDoctypes() {
		scope, _ := engines.ScopeForDoctype(doctype)
		if !scope.Scoped() {
			continue
		}
		var metaRows int
		db.DB.QueryRow(`SELECT COUNT(*) FROM tenant_default.doctype_meta WHERE name = $1`, doctype).Scan(&metaRows)
		if metaRows == 0 {
			skipped = append(skipped, doctype+" (no doctype_meta row)")
			continue
		}
		data := map[string]interface{}{"code": "AUTHZ-GEN-" + suffix, "name": "Out of scope " + doctype}
		for _, f := range scope.LocationFields {
			data[f] = "AUTHZ-ELSEWHERE"
		}
		if scope.SelfField != "" {
			data[scope.SelfField] = "AUTHZ-OTHER-EMP"
		}
		if scope.OwnerField != "" {
			data[scope.OwnerField] = "AUTHZ-OTHER-OWNER"
		}
		id := "AUTHZ-GEN-" + suffix + "-" + strings.ToUpper(doctype)
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec(`INSERT INTO tenant_default.documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, 'Draft', 'system')`, id, doctype, string(raw)); err != nil {
			t.Fatalf("seed %s: %v", doctype, err)
		}
		cleanupID := id
		t.Cleanup(func() { db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id = $1`, cleanupID) })

		// An Admin reaches the record: the doctype's module is on and the
		// record is readable, so a refusal below is the scope, nothing else.
		if adm := call(http.MethodGet, "/api/v1/doc/"+doctype+"/"+id, users[engines.RoleAdmin], nil); adm.status != http.StatusOK {
			skipped = append(skipped, fmt.Sprintf("%s (Admin read: %d %s)", doctype, adm.status, adm.code))
			continue
		}
		cashier := users[engines.RoleCashier]
		may := func(action string) bool {
			ok, _ := engines.RoleMayOnDoctype("default", engines.RoleCashier, doctype, action)
			return ok
		}
		check := func(what string, permitted bool, got outcome) {
			cases++
			if got.status >= 200 && got.status < 300 {
				t.Errorf("%s %s: another scope's record was %s (%d)", doctype, what, what, got.status)
				return
			}
			if permitted && (got.status != http.StatusNotFound || got.code != "GLOBAL-0004") {
				t.Errorf("%s %s: %d %s, want 404 GLOBAL-0004 (the role may %s this doctype, so only the scope refuses - and a refusal must read like a missing record)", doctype, what, got.status, got.code, what)
			}
		}
		check("read", may("read"), call(http.MethodGet, "/api/v1/doc/"+doctype+"/"+id, cashier, nil))
		check("update", may("update"), call(http.MethodPost, "/api/v1/doc/"+doctype+"/"+id, cashier, data))
		if engines.AttachmentsEnabled(doctype) {
			check("attachments", may("read"), call(http.MethodGet, "/api/v1/attachments/"+doctype+"/"+id, cashier, nil))
		}
		check("delete", may("delete"), call(http.MethodDelete, "/api/v1/doc/"+doctype+"/"+id, cashier, nil))
		var still int
		db.DB.QueryRow(`SELECT COUNT(*) FROM tenant_default.documents WHERE id = $1 AND deleted_at IS NULL AND data->>'name' = $2`, id, data["name"]).Scan(&still)
		if still != 1 {
			t.Errorf("%s: the out-of-scope record was changed or deleted", doctype)
		}
	}
	if cases < 60 {
		t.Fatalf("only %d cases ran (skipped: %v) - the scope registry reading has broken", cases, skipped)
	}
	t.Logf("owner / self / location cases: %d refused across %d doctypes; skipped: %v", cases, len(engines.ScopedDoctypes()), skipped)
}

// TestStage4938StateCasesRefuseGenericSave: a status that only the approval
// flow sets (every approval-gated doctype) or only the document's own action
// sets (actionOnlyStatuses), and a key only an action writes
// (ActionOnlyFieldDoctypes), is never reached by a generic save - on a create
// or on an update - by an Admin, the most permitted client role.
func TestStage4938StateCasesRefuseGenericSave(t *testing.T) {
	db.InitDB(testConnStr())
	mux := contractMux()
	users := seedGeneratedCaseUsers(t)
	admin := users[engines.RoleAdmin]
	suffix := contractUniqueID()
	ip := mfaTestIP()
	defer db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id LIKE $1`, "AUTHZ-STATE-"+suffix+"%")

	rows, err := db.DB.Query(`SELECT DISTINCT doctype FROM tenant_default.approval_rules ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	gated := map[string][]string{}
	for rows.Next() {
		var d string
		rows.Scan(&d)
		gated[d] = append([]string{}, approvalOnlyStatuses...)
	}
	rows.Close()
	for doctype, statuses := range actionOnlyStatuses {
		gated[doctype] = append(gated[doctype], statuses...)
	}
	doctypes := make([]string, 0, len(gated))
	for d := range gated {
		doctypes = append(doctypes, d)
	}
	sort.Strings(doctypes)

	// minimalPayload fills the doctype's mandatory fields with plain values
	// so the save reaches the status gate where the doctype's own validation
	// lets it; where it does not (a Link, a GL account, a JSON table), the
	// case still pins "never accepted" and the GLOBAL-0019 count below pins
	// that the gate itself is what answers for the rest.
	minimalPayload := func(doctype string) map[string]interface{} {
		fields, err := engines.GetDocTypeMeta("default", doctype)
		if err != nil {
			return map[string]interface{}{}
		}
		payload := map[string]interface{}{}
		for _, f := range fields {
			if !f.Mandatory || f.Fieldname == "id" || f.Fieldname == "status" {
				continue
			}
			switch f.Fieldtype {
			case "Number", "Currency":
				payload[f.Fieldname] = 1
			case "Date":
				payload[f.Fieldname] = "2030-01-01"
			case "Datetime":
				payload[f.Fieldname] = "2030-01-01T00:00:00Z"
			case "Select":
				opts := strings.FieldsFunc(f.Options, func(r rune) bool { return r == ',' || r == '\n' })
				if len(opts) > 0 {
					payload[f.Fieldname] = strings.TrimSpace(opts[0])
				}
			case "Check":
				payload[f.Fieldname] = true
			case "Link":
				payload[f.Fieldname] = "AUTHZ-NO-SUCH-LINK"
			default:
				payload[f.Fieldname] = "AUTHZ-STATE-" + suffix
			}
		}
		return payload
	}
	cases, exactCases := 0, 0
	for _, doctype := range doctypes {
		for _, status := range gated[doctype] {
			payload := minimalPayload(doctype)
			id := fmt.Sprintf("AUTHZ-STATE-%s-%s-%s", suffix, strings.ToUpper(doctype), strings.ReplaceAll(status, " ", ""))
			payload["id"], payload["status"] = id, status
			rec := doRequestFromIP(t, mux.ServeHTTP, http.MethodPost, "/api/v1/doc/"+doctype, admin, payload, ip)
			cases++
			var storedStatus string
			db.DB.QueryRow(`SELECT status FROM tenant_default.documents WHERE id = $1`, id).Scan(&storedStatus)
			if rec.Code < 300 || storedStatus == status {
				t.Errorf("%s saved straight into %q: %d %s (stored status %q)", doctype, status, rec.Code, strings.TrimSpace(rec.Body.String()), storedStatus)
			}
			if strings.Contains(rec.Body.String(), "GLOBAL-0019") {
				exactCases++
			}
		}
	}
	for doctype, fields := range engines.ActionOnlyFieldDoctypes() {
		for _, field := range fields {
			payload := minimalPayload(doctype)
			payload["id"], payload[field] = "AUTHZ-STATE-"+suffix+"-"+field, true
			rec := doRequestFromIP(t, mux.ServeHTTP, http.MethodPost, "/api/v1/doc/"+doctype, admin, payload, ip)
			cases++
			if rec.Code < 300 || !strings.Contains(rec.Body.String(), engines.ActionOnlyFieldCode) {
				t.Errorf("%s.%s written by a save: %d %s, want %s", doctype, field, rec.Code, strings.TrimSpace(rec.Body.String()), engines.ActionOnlyFieldCode)
			}
		}
	}
	if cases < 20 || exactCases < 10 {
		t.Fatalf("only %d state cases ran, %d answered by the status gate itself (GLOBAL-0019) - the gating tables are not being read", cases, exactCases)
	}
	t.Logf("state cases: %d statuses refused on a generic save across %d doctypes (%d pinned to GLOBAL-0019)", cases, len(doctypes), exactCases)
}

// TestStage4938EveryRoleAndSensitiveFieldIsRegistered fails CI on an
// unclassified role or an unlisted sensitive field: every role a policy table
// or the seeded permissions name is a role this product registers (roles.go
// constants and the role templates), and every field the sensitive-field
// policy names is a declared field of its doctype (or an explicitly listed
// JSON-only key) with a data classification.
func TestStage4938EveryRoleAndSensitiveFieldIsRegistered(t *testing.T) {
	db.InitDB(testConnStr())
	registered := map[string]bool{
		engines.RoleSuperAdmin: true, engines.RoleLegacySuperAdmin: true, engines.RoleAdmin: true,
		engines.RoleStoreManager: true, engines.RoleCashier: true, engines.RoleSetupOperator: true,
		supplierRole: true,
		"*":          true, // sensitive_fields.go's "any signed-in role may write" wildcard (anySensitiveWriter)
	}
	for _, tpl := range engines.RoleTemplates() {
		registered[tpl.Name] = true
	}
	unregistered := map[string][]string{}
	note := func(role, where string) {
		if !registered[role] {
			unregistered[role] = append(unregistered[role], where)
		}
	}
	for _, role := range engines.SensitiveFieldRoles() {
		note(role, "engines/sensitive_fields.go")
	}
	for _, role := range engines.ScopePolicyRoles() {
		note(role, "engines/scope_policy.go")
	}
	routes, err := RouteAccessTable("routes.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, rt := range routes {
		for _, role := range rt.Roles {
			note(role, rt.Pattern)
		}
	}
	rows, err := db.DB.Query(`SELECT DISTINCT role FROM tenant_default.role_permissions UNION SELECT DISTINCT role FROM tenant_default.users WHERE id NOT LIKE '\_\_%' AND id NOT LIKE 'authz-%' AND id NOT LIKE 'harn-%'`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var role string
		rows.Scan(&role)
		note(role, "seeded role_permissions / users")
	}
	rows.Close()
	for role, where := range unregistered {
		t.Errorf("role %q is not registered (roles.go / role_templates.go) but is named by %v", role, where)
	}

	// Sensitive fields: Item.cost_price is JSON-only by design (the file's own
	// comment); any other field must be a declared doctype_fields row.
	jsonOnly := map[string]bool{"Item.cost_price": true}
	fieldsChecked := 0
	for _, doctype := range engines.SensitiveFieldDoctypes() {
		for _, field := range engines.SensitiveFieldNames(doctype) {
			fieldsChecked++
			if _, ok := engines.ClassifyDoctypeField(doctype, field); !ok {
				t.Errorf("%s.%s has no data classification (engines/data_classification.go)", doctype, field)
			}
			var declared int
			db.DB.QueryRow(`SELECT COUNT(*) FROM tenant_default.doctype_fields WHERE doctype_name = $1 AND fieldname = $2`, doctype, field).Scan(&declared)
			if declared == 0 && !jsonOnly[doctype+"."+field] {
				t.Errorf("%s.%s is a sensitive field but not a declared field of %s - list it in jsonOnly here if that is by design", doctype, field, doctype)
			}
		}
	}
	if fieldsChecked < 20 || len(registered) < 10 {
		t.Fatalf("checked only %d fields and %d roles - a registry reading has broken", fieldsChecked, len(registered))
	}
	t.Logf("roles registered: %d; sensitive fields checked: %d", len(registered), fieldsChecked)
}
