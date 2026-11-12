package server

import (
	"custom_erp/db"
	"custom_erp/engines"
	"encoding/json"
	"net/http"
	"testing"
)

// Stage 49.4.7 (disclosure policy): asking for another store's record and
// asking for one that does not exist get the same answer, so a refusal never
// confirms that a record is there.
func TestStage49OutOfScopeAndMissingRecordsAnswerAlike(t *testing.T) {
	db.InitDB(testConnStr())
	mux := contractMux()
	suffix := contractUniqueID()
	otherID := "AUTHZ-DISC-OTHER-" + suffix
	defer db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id = $1`, otherID)
	if _, err := db.DB.Exec(`INSERT INTO tenant_default.documents (id, doctype, data, status, created_by) VALUES ($1, 'POSSession', $2, 'Open', 'system')`,
		otherID, `{"code":"`+otherID+`","location":"AUTHZ-ELSEWHERE"}`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	userID, cleanup := seedContractUser(t, engines.RoleCashier, "HO")
	defer cleanup()
	token := engines.SignToken(userID, userID, engines.RoleCashier, "default", "HO", 1)

	answer := func(id string) (int, string) {
		rec := contractRequest(mux, http.MethodGet, "/api/v1/doc/POSSession/"+id, token)
		var body map[string]interface{}
		json.Unmarshal(rec.Body.Bytes(), &body)
		code, _ := body["code"].(string)
		return rec.Code, code
	}
	otherStatus, otherCode := answer(otherID)
	missingStatus, missingCode := answer("AUTHZ-DISC-MISSING-" + suffix)
	if otherStatus == http.StatusOK {
		t.Fatal("another store's record was returned")
	}
	if otherStatus != missingStatus || otherCode != missingCode {
		t.Fatalf("another store's record answers %d %s, a missing one %d %s - the difference confirms the record exists", otherStatus, otherCode, missingStatus, missingCode)
	}
}
