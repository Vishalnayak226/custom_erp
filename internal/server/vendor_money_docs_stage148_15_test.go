package server

import (
	"custom_erp/db"
	"custom_erp/engines"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// Stages 148.15 / 148.17: not even a full-access role may type, change or delete
// an advance or a rebate claim through the generic route or a CSV import. A typed
// advance carries money no bank posting backs, and it could then pay a bill.
func TestStage14815AdvancesAndClaimsAreNotTyped(t *testing.T) {
	db.InitDB(testConnStr())
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/doc/{doctype}", apiMiddleware(handleGenericDoc))
	mux.HandleFunc("/api/v1/doc/{doctype}/{id}", apiMiddleware(handleGenericDoc))
	admin := engines.SignToken("admin", "admin", engines.RoleSuperAdmin, "default", "", currentCredentialVersion("admin"))

	for _, tc := range []struct{ doctype, id, status, code string }{
		{"VendorAdvance", "T14815-ADV", "Open", "FIN-0926"},
		{"VendorRebateClaim", "T14817-CLAIM", "Calculated", "PURCHA-5025"},
	} {
		tc := tc
		t.Run(tc.doctype, func(t *testing.T) {
			clean := func() { db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id = $1`, tc.id) }
			clean()
			defer clean()
			if _, err := db.DB.Exec(`INSERT INTO tenant_default.documents (id, doctype, data, status, created_by)
				VALUES ($1, $2, '{"amount":100}', $3, 'admin')`, tc.id, tc.doctype, tc.status); err != nil {
				t.Fatalf("seed: %v", err)
			}
			edit := map[string]interface{}{"code": tc.id, "amount": 900, "status": tc.status}
			for _, try := range []struct {
				name, method, path string
				body               interface{}
			}{
				{"typed", http.MethodPost, "/api/v1/doc/" + tc.doctype, map[string]interface{}{"code": "T14815-TYPED", "amount": 5000, "status": tc.status}},
				{"changed", http.MethodPost, "/api/v1/doc/" + tc.doctype + "/" + tc.id, edit},
				{"deleted", http.MethodDelete, "/api/v1/doc/" + tc.doctype + "/" + tc.id, nil},
			} {
				rec := doRequest(t, mux.ServeHTTP, try.method, try.path, admin, try.body)
				var got struct {
					Code string `json:"code"`
				}
				_ = json.Unmarshal(rec.Body.Bytes(), &got)
				if got.Code != tc.code {
					t.Errorf("%s by a Super Admin: %d %s, want %s", try.name, rec.Code, rec.Body.String(), tc.code)
				}
			}
			var amount string
			var live bool
			if err := db.DB.QueryRow(`SELECT data->>'amount', deleted_at IS NULL FROM tenant_default.documents WHERE id = $1`, tc.id).Scan(&amount, &live); err != nil {
				t.Fatalf("the record is gone: %v", err)
			}
			if amount != "100" || !live {
				t.Errorf("the record changed: amount %s live %v", amount, live)
			}
			var typed int
			db.DB.QueryRow(`SELECT count(*) FROM tenant_default.documents WHERE doctype = $1 AND data->>'code' = 'T14815-TYPED'`, tc.doctype).Scan(&typed)
			if typed != 0 {
				t.Errorf("%d typed records were saved", typed)
			}
			if rec := doRequest(t, mux.ServeHTTP, http.MethodGet, "/api/v1/doc/"+tc.doctype+"/"+tc.id, admin, nil); rec.Code != http.StatusOK {
				t.Errorf("the record cannot be opened: %d %s", rec.Code, rec.Body.String())
			}

			if err := engines.ValidateDocument("default", tc.doctype, map[string]interface{}{"code": "T14815-CSV", "amount": 5}); err == nil {
				t.Error("ValidateDocument let a typed record through")
			} else if v, ok := err.(*engines.ValidationError); !ok || v.Code != tc.code {
				t.Errorf("ValidateDocument: %v, want %s", err, tc.code)
			}

			// A CSV import takes its own path to the same check.
			csv := "code,amount,status,vendor_id,period_from,period_to\nT14815-CSV,5," + tc.status + ",V1,2026-04-01,2026-06-30\n"
			res, err := engines.BulkImportCSV("default", tc.doctype, strings.NewReader(csv), "admin", engines.RoleSuperAdmin, true)
			if err != nil {
				t.Fatalf("import: %v", err)
			}
			if res.SuccessRows != 0 || len(res.Errors) != 1 || !strings.Contains(res.Errors[0].Message, "are made by") {
				t.Errorf("a CSV row was not refused by the own-screen check: %+v", res)
			}

			// /me/permissions sends this list, so no list or picker offers New.
			listed := false
			for _, doctype := range madeOnOwnScreen {
				listed = listed || doctype == tc.doctype
			}
			if !listed {
				t.Errorf("%s is not in madeOnOwnScreen, so the list still offers New", tc.doctype)
			}
		})
	}
}
