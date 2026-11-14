package server

import (
	"net/http"
	"testing"

	"custom_erp/db"
)

// A method the generic document endpoint does not offer (PUT, PATCH) is a
// refusal for every role - 403 without the update right, otherwise the 404 /
// 405 a missing record or method gets. A non-administrator used to get a 500,
// because the permission check was asked about an empty action.
// (TestLockedRecordSaysWhyOnGenericPutAndEdit keeps 162.3's 422 for an editor.)
func TestGenericDocUnsupportedMethodIsNotAnError500(t *testing.T) {
	db.InitDB(testConnStr())
	const user, vendor = "__docmethod_supplier__", "__DOCMETHOD_VENDOR"
	defer seedVendor(t, vendor)()
	pw, cleanup := seedSupplierUser(t, user, vendor)
	defer cleanup()
	token := plainSessionToken(t, user, pw, mfaTestIP())

	for _, doctype := range []string{"SupplierSubmission", "Vendor"} {
		for _, method := range []string{http.MethodPut, http.MethodPatch} {
			rec := docRequest(t, method, doctype, "SOME-ID", token, map[string]interface{}{"title": "x"})
			switch rec.Code {
			case http.StatusForbidden, http.StatusNotFound, http.StatusMethodNotAllowed:
			default:
				t.Errorf("%s %s as a Supplier: status %d, want 403, 404 or 405 (body %s)", method, doctype, rec.Code, rec.Body.String())
			}
		}
	}
}
