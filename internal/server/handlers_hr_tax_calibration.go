package server

import (
	"custom_erp/engines"
	"encoding/json"
	"fmt"
	"net/http"
)

// Ask 170 group F (170.22-170.25): appraisal calibration, the PF ECR file, the
// Form 24Q data and the salary-revision arrears list. Kept in a file of their
// own beside handlers_hr_payroll.go.

// GET /api/v1/hr/appraisal-cycles/{id}/rating-spread
func handleAppraisalRatingSpread(w http.ResponseWriter, r *http.Request) {
	rows, err := engines.AppraisalRatingSpread(r.Header.Get("Resolved-Tenant-ID"), r.PathValue("id"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"cycle_id": r.PathValue("id"), "spread": rows})
}

// POST /api/v1/hr/appraisals/{id}/calibrate  {rating, reason}
func handleCalibrateAppraisal(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Rating float64 `json:"rating"`
		Reason string  `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'rating' and 'reason' are required")
		return
	}
	if err := engines.CalibrateAppraisal(r.Header.Get("Resolved-Tenant-ID"), r.PathValue("id"), req.Rating, req.Reason,
		r.Header.Get("Resolved-User-ID"), r.Header.Get("Resolved-Role")); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "calibrated"})
}

// GET /api/v1/hr/pf-ecr?month=2026-04[&format=json]
func handlePFECR(w http.ResponseWriter, r *http.Request) {
	month := r.URL.Query().Get("month")
	rows, err := engines.PFECRRows(r.Header.Get("Resolved-Tenant-ID"), month)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	if r.URL.Query().Get("format") == "json" {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"month": month, "rows": rows})
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", attachmentDisposition(fmt.Sprintf("ECR-%s.txt", month)))
	_, _ = w.Write([]byte(engines.PFECRText(rows)))
}

// GET /api/v1/hr/form-24q?fin_year=2025-26&quarter=Q1[&annexure=I|II]
func handleForm24Q(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	csv, err := engines.Form24QCSV(r.Header.Get("Resolved-Tenant-ID"), q.Get("fin_year"), q.Get("quarter"), q.Get("annexure"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	ann := q.Get("annexure")
	if ann == "" {
		ann = "I"
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", attachmentDisposition(fmt.Sprintf("Form24Q-%s-%s-annexure-%s.csv", q.Get("fin_year"), q.Get("quarter"), ann)))
	_, _ = w.Write([]byte(csv))
}

// GET /api/v1/hr/salary-revisions/{id}/arrears
func handleSalaryRevisionArrears(w http.ResponseWriter, r *http.Request) {
	rows, total, err := engines.SalaryRevisionArrears(r.Header.Get("Resolved-Tenant-ID"), r.PathValue("id"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"revision_id": r.PathValue("id"), "rows": rows, "total_arrears": total})
}
