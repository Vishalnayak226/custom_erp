package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
)

// Stage 73.1.10 (period close tasks, controlled reopen) and 73.2.16 (bank
// reconciliation workbench). Thin: every rule lives in the engine
// (engines/period_close.go, engines/bank_reconciliation_workbench.go).

func financeActor(r *http.Request) (tenantID, userID, username, role string) {
	return r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-User-ID"),
		r.Header.Get("Resolved-Username"), r.Header.Get("Resolved-Role")
}

func decodeFinanceBody(w http.ResponseWriter, r *http.Request, into interface{}) bool {
	if err := json.NewDecoder(r.Body).Decode(into); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "The request body is not valid JSON.")
		return false
	}
	return true
}

func handlePeriodCloseBoard(w http.ResponseWriter, r *http.Request) {
	tenantID, _, _, role := financeActor(r)
	board, err := engines.GetPeriodCloseBoard(tenantID, r.PathValue("id"), role)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(board)
}

func handlePeriodCloseTaskAction(w http.ResponseWriter, r *http.Request) {
	tenantID, _, username, role := financeActor(r)
	periodID, task, action := r.PathValue("id"), r.PathValue("task"), r.PathValue("action")
	var req struct {
		Owner       string `json:"owner"`
		Status      string `json:"status"`
		Evidence    string `json:"evidence"`
		EvidenceRef string `json:"evidence_ref"`
		Reason      string `json:"reason"`
	}
	if !decodeFinanceBody(w, r, &req) {
		return
	}
	var err error
	switch action {
	case "assign":
		err = engines.AssignPeriodCloseTask(tenantID, periodID, task, req.Owner, username, role)
	case "sign":
		err = engines.SignOffPeriodCloseTask(tenantID, periodID, task, req.Status, req.Evidence, req.EvidenceRef, username)
	case "undo":
		err = engines.UndoPeriodCloseTaskSignOff(tenantID, periodID, task, req.Reason, username, role)
	default:
		writeAPIErrorGeneric(w, r, http.StatusNotFound, "Unknown close task action.")
		return
	}
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func handlePeriodReopenRequest(w http.ResponseWriter, r *http.Request) {
	tenantID, userID, username, role := financeActor(r)
	var req struct {
		Reason string `json:"reason"`
	}
	if !decodeFinanceBody(w, r, &req) {
		return
	}
	id, err := engines.RequestPeriodReopen(tenantID, r.PathValue("id"), req.Reason, userID, username, role)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"id": id, "status": "Pending Approval"})
}

// handlePeriodReopenApply retries applying an already Approved request (the
// approval hook's own attempt failed). It cannot approve anything.
func handlePeriodReopenApply(w http.ResponseWriter, r *http.Request) {
	tenantID, _, _, role := financeActor(r)
	if !engines.CanManagePeriodClose(role) {
		writeAPIError(w, r, "FIN-0504", "")
		return
	}
	if err := engines.ApplyApprovedPeriodReopen(tenantID, r.PathValue("request"), ""); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "reopened"})
}

func handleBankReconWorkbench(w http.ResponseWriter, r *http.Request) {
	tenantID, _, _, _ := financeActor(r)
	q := r.URL.Query()
	wb, err := engines.GetBankReconciliationWorkbench(tenantID, q.Get("bank_account"), q.Get("from"), q.Get("to"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(wb)
}

func handleBankLineCandidates(w http.ResponseWriter, r *http.Request) {
	tenantID, _, _, _ := financeActor(r)
	cands, err := engines.BankLineCandidates(tenantID, r.PathValue("id"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"candidates": cands})
}

func handleBankLineAction(w http.ResponseWriter, r *http.Request) {
	tenantID, _, username, role := financeActor(r)
	lineID, action := r.PathValue("id"), r.PathValue("action")
	var req struct {
		PostingIDs      []string `json:"posting_ids"`
		Note            string   `json:"note"`
		Reason          string   `json:"reason"`
		Owner           string   `json:"owner"`
		BatchID         string   `json:"settlement_batch_id"`
		ClearingAccount string   `json:"clearing_account"`
		Fee             float64  `json:"fee"`
	}
	if !decodeFinanceBody(w, r, &req) {
		return
	}
	var err error
	switch action {
	case "match":
		err = engines.MatchBankLine(tenantID, lineID, req.PostingIDs, req.Note, username)
	case "unmatch":
		err = engines.UnmatchBankLine(tenantID, lineID, req.Reason, username)
	case "exception":
		err = engines.RaiseBankException(tenantID, lineID, req.Owner, req.Reason, username)
	case "explain":
		err = engines.ExplainBankException(tenantID, lineID, req.Note, username, role)
	case "match-marketplace":
		err = engines.MatchMarketplacePayout(tenantID, lineID, req.BatchID, username)
	case "match-pos-payout":
		err = engines.MatchPOSPayout(tenantID, lineID, req.ClearingAccount, req.Fee, username)
	default:
		writeAPIErrorGeneric(w, r, http.StatusNotFound, "Unknown statement line action.")
		return
	}
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func handleBankRuleMatch(w http.ResponseWriter, r *http.Request) {
	tenantID, _, username, _ := financeActor(r)
	var req struct {
		BankAccount string `json:"bank_account"`
		engines.BankMatchRule
	}
	if !decodeFinanceBody(w, r, &req) {
		return
	}
	res, err := engines.RuleMatchBankStatement(tenantID, req.BankAccount, req.BankMatchRule, username)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(res)
}
