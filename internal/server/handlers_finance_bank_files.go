package server

import (
	"encoding/json"
	"net/http"

	"custom_erp/engines"
)

// Stage 170.3 / 170.4: bank files. The browser reads the chosen file as text
// and sends it in the body, like the other CSV uploads.

func handleBankStatementImport(w http.ResponseWriter, r *http.Request) {
	tenantID, _, username, _ := financeActor(r)
	r.Body = http.MaxBytesReader(w, r.Body, 8<<20)
	var req struct {
		BankAccount string `json:"bank_account"`
		Format      string `json:"format"`
		Content     string `json:"content"`
	}
	if !decodeFinanceBody(w, r, &req) {
		return
	}
	res, err := engines.ImportBankStatement(tenantID, req.BankAccount, req.Format, req.Content, username)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(res)
}

func handlePaymentBankResponse(w http.ResponseWriter, r *http.Request) {
	tenantID, _, username, _ := financeActor(r)
	r.Body = http.MaxBytesReader(w, r.Body, 4<<20)
	var req struct {
		BankAccount string `json:"bank_account"`
		Content     string `json:"content"`
	}
	if !decodeFinanceBody(w, r, &req) {
		return
	}
	res, err := engines.ImportPaymentBankResponse(tenantID, r.PathValue("id"), req.BankAccount, req.Content, username)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(res)
}
