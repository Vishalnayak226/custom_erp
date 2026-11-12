package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
)

// Stage 26.8 (HR/Payroll Maturity Sprint) handlers. Kept in their own file,
// separate from handlers_procurement_pim2.go's existing handlePayrollExport,
// to avoid colliding with any concurrent session's in-flight edits to
// shared handler files - same precedent Stage 26.5 set with
// handlers_wms_enterprise.go.

func handleRunPayroll(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req struct {
		EmployeeID string `json:"employee_id"`
		PeriodFrom string `json:"period_from"`
		PeriodTo   string `json:"period_to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.EmployeeID == "" || req.PeriodFrom == "" || req.PeriodTo == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'employee_id', 'period_from', and 'period_to' are required")
		return
	}
	payslipID, err := engines.RunPayroll(tenantID, req.EmployeeID, req.PeriodFrom, req.PeriodTo, userID)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "payslip_generated", "payslip_id": payslipID})
}

func handlePostPayslip(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req struct {
		PayslipID string `json:"payslip_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.PayslipID == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'payslip_id' is required")
		return
	}
	if err := engines.PostPayslipToGL(tenantID, req.PayslipID, userID); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "payslip_posted"})
}

func handleDisburseEmployeeLoan(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req struct {
		LoanID string `json:"loan_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.LoanID == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'loan_id' is required")
		return
	}
	if err := engines.DisburseEmployeeLoan(tenantID, req.LoanID, userID); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "loan_disbursed"})
}

// handleSalaryComponentsPreview (26.8.2) is a read-only GET so a payroll
// screen can preview gross/PF/ESI/PT before actually running payroll.
func handleSalaryComponentsPreview(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	employeeID := r.URL.Query().Get("employee_id")
	if employeeID == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Query param 'employee_id' is required")
		return
	}
	sc, err := engines.CalculateSalaryComponents(tenantID, employeeID)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(sc)
}

// handleMyEmployeeRecord (26.8.5) resolves the logged-in user's own Employee
// record, so a self-service screen can prefill/scope Leave and
// ExpenseClaim submissions without the user needing to know their own
// employee code. Returns {"employee": null} if the user has no linked
// Employee (e.g. an admin login not itself an employee record).
func handleMyEmployeeRecord(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodGet {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	emp, err := engines.GetMyEmployeeRecord(tenantID, userID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"employee": emp})
}
