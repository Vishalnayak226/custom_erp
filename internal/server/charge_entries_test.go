package server

import (
	"custom_erp/db"
	"custom_erp/engines"
	"encoding/json"
	"net/http"
	"testing"
)

// Stage 148.8: a charge entry is the receipt's once-only gate and the record of
// its postings. A deleted one would leave the postings behind and let a replay
// charge again, so not even a full-access role may type, change or delete one.
func TestStage1488EntriesAreNotWrittenByHand(t *testing.T) {
	db.InitDB(testConnStr())
	const id = "T1488-ENTRY"
	clean := func() { db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id = $1`, id) }
	clean()
	defer clean()
	if _, err := db.DB.Exec(`INSERT INTO tenant_default.documents (id, doctype, data, status, created_by)
		VALUES ($1, 'TradeChargeEntry', '{"code":"T1488-ENTRY","amount":100,"open_amount":100}', 'Estimated', 'admin')`, id); err != nil {
		t.Fatalf("seed: %v", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/doc/{doctype}", apiMiddleware(handleGenericDoc))
	mux.HandleFunc("/api/v1/doc/{doctype}/{id}", apiMiddleware(handleGenericDoc))
	admin := engines.SignToken("admin", "admin", engines.RoleSuperAdmin, "default", "", currentCredentialVersion("admin"))

	edit := map[string]interface{}{"code": id, "amount": 1, "open_amount": 0, "status": "Trued up"}
	for _, try := range []struct {
		name, method, path string
		body               interface{}
	}{
		{"typed", http.MethodPost, "/api/v1/doc/TradeChargeEntry", map[string]interface{}{"code": "T1488-TYPED", "amount": 5}},
		{"changed", http.MethodPost, "/api/v1/doc/TradeChargeEntry/" + id, edit},
		{"deleted", http.MethodDelete, "/api/v1/doc/TradeChargeEntry/" + id, nil},
	} {
		rec := doRequest(t, mux.ServeHTTP, try.method, try.path, admin, try.body)
		var got struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &got)
		if got.Code != "FIN-0894" {
			t.Errorf("%s by a Super Admin: %d %s, want FIN-0894", try.name, rec.Code, rec.Body.String())
		}
	}

	var amount, status string
	var live bool
	if err := db.DB.QueryRow(`SELECT data->>'amount', status, deleted_at IS NULL FROM tenant_default.documents WHERE id = $1`, id).Scan(&amount, &status, &live); err != nil {
		t.Fatalf("the entry is gone: %v", err)
	}
	if amount != "100" || status != "Estimated" || !live {
		t.Errorf("the entry changed: amount %s status %s live %v", amount, status, live)
	}
	if rec := doRequest(t, mux.ServeHTTP, http.MethodGet, "/api/v1/doc/TradeChargeEntry/"+id, admin, nil); rec.Code != http.StatusOK {
		t.Errorf("the entry cannot be opened: %d %s", rec.Code, rec.Body.String())
	}

	// /me/permissions sends this list, so no list or picker offers New.
	listed := false
	for _, doctype := range madeOnOwnScreen {
		listed = listed || doctype == "TradeChargeEntry"
	}
	if !listed {
		t.Error("TradeChargeEntry is not in madeOnOwnScreen, so the list still offers New")
	}
}
