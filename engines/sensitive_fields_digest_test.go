package engines

import (
	"custom_erp/db"
	"testing"
)

// 103.4.1: a stored token digest is hidden from every role, Super Admin
// included - the document list and form must never carry it.
func TestTokenDigestsAreHiddenFromSuperAdminToo(t *testing.T) {
	db.InitDB(testConnStr())
	const tenantID = "default"
	for _, c := range []struct{ doctype, field string }{
		{"PIMCatalog", "share_token_hash"},
		{"PIMImportSchedule", "hook_token_hash"},
	} {
		for _, role := range []string{RoleSuperAdmin, RoleLegacySuperAdmin, RoleStoreManager, RoleCashier} {
			perm, ok := sensitiveFieldPolicy(role, c.doctype)[c.field]
			if !ok || perm.AllowRead || perm.AllowWrite {
				t.Errorf("%s.%s: role %q must get neither read nor write (present=%v)", c.doctype, c.field, role, ok)
			}
		}
		data := map[string]interface{}{"code": "X", c.field: "deadbeef"}
		out, err := FilterFieldsForRole(tenantID, RoleSuperAdmin, c.doctype, data)
		if err != nil {
			t.Fatal(err)
		}
		if _, present := out[c.field]; present {
			t.Errorf("Super Admin still receives %s.%s over the document API", c.doctype, c.field)
		}
		if _, present := out["code"]; !present {
			t.Errorf("%s: ordinary fields must stay for Super Admin", c.doctype)
		}
		if err := RejectRestrictedFieldWrites(tenantID, RoleSuperAdmin, c.doctype, map[string]interface{}{c.field: "forged"}); err == nil {
			t.Errorf("Super Admin was allowed to write %s.%s", c.doctype, c.field)
		}
		for _, row := range ExplainSensitiveFields(RoleSuperAdmin, c.doctype) {
			if row.Field == c.field && (row.CanRead || row.CanWrite) {
				t.Errorf("%s.%s: explanation must say nobody reads it", c.doctype, row.Field)
			}
		}
	}
	// Other secrets stay readable to Super Admin (WebhookSubscription.secret is read by it).
	if _, restricted := sensitiveFieldPolicy(RoleSuperAdmin, "WebhookSubscription")["secret"]; restricted {
		t.Error("Super Admin must keep reading WebhookSubscription.secret")
	}
}
