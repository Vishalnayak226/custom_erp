package engines

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// Catalog reports - Expense Management and HRMS (Stage 75.3.7, Wave C).
//
// An ExpenseClaim goes Submitted -> Verified (approved) -> Paid, or Rejected;
// it is paid by its ":PAY" ledger posting. The ERP has no expense policy
// engine, so a claim's eligible amount is what it claims, and the policy
// exception report flags the checks the record itself allows (no receipt,
// claimed long after the expense, a future date). HR: Employee, Attendance
// (date, store and status - no clock-in times are kept), Leave,
// ShiftAssignment, Payslip, EmployeeLoan, OnboardingChecklist, Appraisal,
// TrainingRecord and the users table. There is no recruitment record, so the
// Recruitment Pipeline waits for a module.

const expenseSLAHours = 72

func employeeOf(r *catRow, field string) map[string]interface{} {
	return r.master("Employee", r.hs(field))
}

func employeeCols(field string) map[string]catFn {
	return map[string]catFn{
		"Employee ID":   func(r *catRow) interface{} { return r.hs(field) },
		"Employee Name": func(r *catRow) interface{} { return firstNonEmpty(catStr(employeeOf(r, field)["name"]), r.hs(field)) },
		"Department": func(r *catRow) interface{} {
			return r.nameOf("Department", firstNonEmpty(r.hs("department"), catStr(employeeOf(r, field)["department"])))
		},
		"Manager Name": func(r *catRow) interface{} {
			return r.nameOf("Employee", catStr(employeeOf(r, field)["reporting_manager"]))
		},
	}
}

// --- expense claims -------------------------------------------------------------

func claimPaidOn(r *catRow) string {
	m, ok := r.lk.extra["claimpaid"].(map[string]string)
	if !ok {
		m = map[string]string{}
		rows, err := dbQuery(fmt.Sprintf(`SELECT document_id, to_char(MIN(created_at), 'YYYY-MM-DD') FROM %s.gl_postings
			WHERE document_type = 'ExpenseClaim' AND idempotency_key LIKE '%%:PAY%%' GROUP BY 1`, r.lk.schema))
		if err == nil {
			for rows.Next() {
				var id, day string
				if rows.Scan(&id, &day) == nil {
					m[id] = day
				}
			}
			rows.Close()
		}
		r.lk.extra["claimpaid"] = m
	}
	return m[r.ID]
}

func claimOpen(r *catRow) bool {
	return r.Status != "Paid" && r.Status != "Rejected" && r.Status != "Cancelled"
}

var claimCols = mergeCols(employeeCols("employee_id"), map[string]catFn{
	"Claim No":         func(r *catRow) interface{} { return firstNonEmpty(r.hs("code"), r.ID) },
	"Expense Date":     catHD("expense_date"),
	"Expense Category": catH("category"),
	"Claim Amount":     catHN("amount"),
	"Eligible Amount":  func(r *catRow) interface{} { return firstPositive(r.hn("approved_amount"), r.hn("amount")) },
	"Approval Status": func(r *catRow) interface{} {
		switch r.Status {
		case "Verified", "Approved", "Paid":
			return "Approved"
		case "Rejected", "Returned":
			return r.Status
		case "Draft":
			return "Not submitted"
		}
		return "Pending"
	},
	"Payment Status": func(r *catRow) interface{} {
		if r.Status == "Paid" {
			return "Paid"
		}
		return "Unpaid"
	},
	"SLA Age Hours": func(r *catRow) interface{} {
		if !claimOpen(r) {
			return nil
		}
		return math.Round(r.since(r.Created).Hours()*10) / 10
	},
	"SLA Due At":   func(r *catRow) interface{} { return catStamp(r.Created.Add(expenseSLAHours * time.Hour)) },
	"Payment Date": func(r *catRow) interface{} { return claimPaidOn(r) },
	"UTR No":       catNotHeld,
	"Reason":       func(r *catRow) interface{} { return firstNonEmpty(r.hs("rejection_reason"), r.hs("return_reason")) },
	"Remarks":      catH("purpose"),
	"User Name":    func(r *catRow) interface{} { return firstNonEmpty(r.approval()[0], r.CreatedBy) },
	"Headcount":    catH("employee_id"),
	"Policy Exception": func(r *catRow) interface{} {
		return strings.Join(claimExceptions(r), "; ")
	},
	"Advance No":              func(r *catRow) interface{} { return firstNonEmpty(r.hs("code"), r.ID) },
	"Advance Amount":          catHN("advance_adjusted"),
	"Advance Adjusted Amount": catHN("advance_adjusted"),
	"Advance Balance Amount":  func(r *catRow) interface{} { return r.hn("amount") - r.hn("advance_adjusted") },
	"Document Date":           catHD("expense_date"),
})

// claimExceptions: what the record shows against usual expense policy.
func claimExceptions(r *catRow) []string {
	var out []string
	if r.hs("receipt_number") == "" {
		out = append(out, "No receipt number")
	}
	if d := catDay(r.hv("expense_date")); d != "" {
		if t, err := time.Parse("2006-01-02", d); err == nil {
			if t.After(time.Now()) {
				out = append(out, "Expense dated in the future")
			} else if gap := r.Created.Sub(t).Hours() / 24; gap > 30 {
				out = append(out, fmt.Sprintf("Claimed %d days after the expense", int(gap)))
			}
		}
	}
	return out
}

var catClaims = &catSource{Label: "Expense claim", Doctype: "ExpenseClaim", DateField: "expense_date",
	Statuses: []string{"Draft", "Submitted", "Verified", "Paid", "Rejected"}, Cols: claimCols}

var catClaimPostings = &catSource{Label: "Expense posting", Rows: glRows("p.document_type = 'ExpenseClaim' AND p.debit > 0"), StoreOf: glDocStore,
	Cols: mergeCols(glCols, map[string]catFn{
		"Claim No":    func(r *catRow) interface{} { return catDocNumber(r, "ExpenseClaim", r.hs("doc_id")) },
		"Employee ID": func(r *catRow) interface{} { return r.master("ExpenseClaim", r.hs("doc_id"))["employee_id"] },
		"Employee Name": func(r *catRow) interface{} {
			return r.nameOf("Employee", catStr(r.master("ExpenseClaim", r.hs("doc_id"))["employee_id"]))
		},
		"Expense Category": func(r *catRow) interface{} { return r.master("ExpenseClaim", r.hs("doc_id"))["category"] },
		"Eligible Amount":  catHN("debit"),
		"GST Amount": func(r *catRow) interface{} {
			if r.hs("account") == "1500" { // input GST on the claim
				return r.hn("debit")
			}
			return 0.0
		},
		"Status": func(r *catRow) interface{} { return r.master("ExpenseClaim", r.hs("doc_id"))["_status"] },
	})}

// --- HR -----------------------------------------------------------------------------

var catEmployees = &catSource{Label: "Employee", Doctype: "Employee", Statuses: []string{"Active", "Inactive", "Exited", "On Notice"},
	Cols: mergeCols(employeeCols("id"), map[string]catFn{
		"Employee ID":   func(r *catRow) interface{} { return firstNonEmpty(r.hs("code"), r.ID) },
		"Employee Name": catH("name"),
		"Department":    func(r *catRow) interface{} { return r.nameOf("Department", r.hs("department")) },
		"Manager Name":  func(r *catRow) interface{} { return r.nameOf("Employee", r.hs("reporting_manager")) },
		"Headcount":     func(r *catRow) interface{} { return 1.0 },
		"Reason":        catNotHeld,
		"Document Date": catHD("date_of_exit"),
		"User ID":       catH("user_id"),
		"User Name": func(r *catRow) interface{} {
			if u := employeeUser(r); u != nil {
				return catStr(u["username"]) + " (" + catStr(u["role"]) + ")"
			}
			return nil
		},
		"Approved By": catNotHeld,
	})}

// employeeUser is the sign-in linked to an employee (users table).
func employeeUser(r *catRow) map[string]interface{} {
	m, ok := r.lk.extra["users"].(map[string]map[string]interface{})
	if !ok {
		m = map[string]map[string]interface{}{}
		rows, err := dbQuery(fmt.Sprintf(`SELECT id, username, COALESCE(role, ''), COALESCE(status, '') FROM %s.users`, r.lk.schema))
		if err == nil {
			for rows.Next() {
				var id, name, role, status string
				if rows.Scan(&id, &name, &role, &status) == nil {
					m[id] = map[string]interface{}{"username": name, "role": role, "status": status}
				}
			}
			rows.Close()
		}
		r.lk.extra["users"] = m
	}
	return m[r.hs("user_id")]
}

// shiftOn: the shift an employee was rostered for on a day.
func shiftOn(r *catRow, employee, day string) string {
	m, ok := r.lk.extra["shifts"].(map[string]string)
	if !ok {
		m = map[string]string{}
		rows, err := dbQuery(fmt.Sprintf(`SELECT data->>'employee_id', LEFT(data->>'date', 10), COALESCE(s.data->>'name', a.data->>'shift_id')
			FROM %[1]s.documents a LEFT JOIN %[1]s.documents s ON s.doctype = 'Shift' AND s.id = a.data->>'shift_id'
			WHERE a.doctype = 'ShiftAssignment' AND a.deleted_at IS NULL`, r.lk.schema))
		if err == nil {
			for rows.Next() {
				var e, d, name string
				if rows.Scan(&e, &d, &name) == nil {
					m[e+"/"+d] = name
				}
			}
			rows.Close()
		}
		r.lk.extra["shifts"] = m
	}
	return m[employee+"/"+day]
}

var catAttendance = &catSource{Label: "Attendance", Doctype: "Attendance", DateField: "date", Statuses: []string{"Present", "Absent", "Half Day", "Late", "On Leave"},
	Cols: mergeCols(employeeCols("employee_id"), map[string]catFn{
		"Attendance Date":   catHD("date"),
		"Shift":             func(r *catRow) interface{} { return shiftOn(r, r.hs("employee_id"), catDay(r.hv("date"))) },
		"Check In":          catNotHeld,
		"Check Out":         catNotHeld,
		"Attendance Status": func(r *catRow) interface{} { return firstNonEmpty(r.hs("status"), r.Status) },
		"Task Duration Min": catNotHeld,
		"Reason":            func(r *catRow) interface{} { return firstNonEmpty(r.hs("reason"), r.hs("remarks")) },
	})}

var catLeaves = &catSource{Label: "Leave", Doctype: "Leave", DateField: "from_date", StoreOf: func(r *catRow) string {
	return catStr(employeeOf(r, "employee_id")["location"])
},
	Statuses: []string{"Draft", "Pending Approval", "Approved", "Rejected", "Cancelled"},
	Cols: mergeCols(employeeCols("employee_id"), map[string]catFn{
		"Document Date":   func(r *catRow) interface{} { return catDay(r.Created) },
		"Leave Type":      catH("leave_type"),
		"Leave Days":      catHN("days"),
		"From Date":       catHD("from_date"),
		"To Date":         catHD("to_date"),
		"Approval Status": func(r *catRow) interface{} { return r.Status },
		"Reason":          catNotHeld,
	})}

var catRoster = &catSource{Label: "Shift", Doctype: "ShiftAssignment", DateField: "date",
	Cols: mergeCols(employeeCols("employee_id"), map[string]catFn{
		"Document Date": catHD("date"),
		"Shift":         func(r *catRow) interface{} { return r.nameOf("Shift", r.hs("shift_id")) },
	})}

func payslipDeductions(r *catRow) float64 {
	total := 0.0
	for _, f := range []string{"pf_deduction", "esi_deduction", "pt_deduction", "tds_deduction", "loan_deduction"} {
		total += r.hn(f)
	}
	return total
}

var payslipCols = mergeCols(employeeCols("employee_id"), map[string]catFn{
	"Payroll Period": func(r *catRow) interface{} {
		return catDay(r.hv("period_from")) + " to " + catDay(r.hv("period_to"))
	},
	"Gross Salary":   catHN("gross_pay"),
	"Deductions":     func(r *catRow) interface{} { return payslipDeductions(r) },
	"Net Pay":        catHN("net_pay"),
	"Payment Status": func(r *catRow) interface{} { return r.Status },
})

var catPayslips = &catSource{Label: "Payslip", Doctype: "Payslip", DateField: "period_from", StoreOf: func(r *catRow) string {
	return catStr(employeeOf(r, "employee_id")["location"])
}, Cols: payslipCols}

// payslipComponentRows: one row per payslip and pay component.
func payslipComponentRows(tenantID, schema string, p map[string]string) ([]*catRow, error) {
	slips, err := catPayslips.docRows(schema, p, false)
	if err != nil {
		return nil, err
	}
	comps := []struct{ field, name string }{{"gross_pay", "Gross pay"}, {"pf_deduction", "PF"}, {"esi_deduction", "ESI"}, {"pt_deduction", "Professional tax"},
		{"tds_deduction", "TDS"}, {"loan_deduction", "Loan recovery"}, {"net_pay", "Net pay"}}
	var out []*catRow
	for _, s := range slips {
		for _, c := range comps {
			v := catNum(s.H[c.field])
			if v == 0 {
				continue
			}
			r := *s
			r.L = map[string]interface{}{"component": c.name, "field": c.field, "amount": v}
			out = append(out, &r)
		}
	}
	return out, nil
}

var catPayslipComponents = &catSource{Label: "Pay component", Rows: payslipComponentRows, StoreOf: func(r *catRow) string {
	return catStr(employeeOf(r, "employee_id")["location"])
},
	Cols: mergeCols(payslipCols, map[string]catFn{
		"Document Type": catL("component"),
		"Gross Salary": func(r *catRow) interface{} {
			if r.ls("field") == "gross_pay" {
				return r.ln("amount")
			}
			return 0.0
		},
		"Deductions": func(r *catRow) interface{} {
			if strings.HasSuffix(r.ls("field"), "_deduction") {
				return r.ln("amount")
			}
			return 0.0
		},
		"Net Pay": func(r *catRow) interface{} {
			if r.ls("field") == "net_pay" {
				return r.ln("amount")
			}
			return 0.0
		},
		"Status": func(r *catRow) interface{} { return r.Status },
	})}

var catLoans = &catSource{Label: "Employee loan", Doctype: "EmployeeLoan", DateField: "start_date", StoreOf: func(r *catRow) string {
	return catStr(employeeOf(r, "employee_id")["location"])
},
	Cols: mergeCols(employeeCols("employee_id"), map[string]catFn{
		"Document Date":           catHD("start_date"),
		"Advance No":              func(r *catRow) interface{} { return firstNonEmpty(r.hs("code"), r.ID) },
		"Advance Amount":          catHN("principal_amount"),
		"Advance Adjusted Amount": func(r *catRow) interface{} { return r.hn("principal_amount") - r.hn("outstanding_balance") },
		"Advance Balance Amount":  catHN("outstanding_balance"),
	})}

var catOnboarding = &catSource{Label: "Onboarding", Rows: func(tenantID, schema string, p map[string]string) ([]*catRow, error) {
	docs, err := (&catSource{Doctype: "OnboardingChecklist", StoreField: "-"}).docRows(schema, p, false)
	if err != nil {
		return nil, err
	}
	var out []*catRow
	for _, d := range docs {
		items := catLineList(d.H["items"])
		if len(items) == 0 {
			r := *d
			r.L = map[string]interface{}{"task": d.H["checklist_type"], "status": d.Status}
			out = append(out, &r)
			continue
		}
		for i, it := range items {
			r := *d
			r.N = i + 1
			r.L = map[string]interface{}{"task": firstNonEmpty(catStr(it["task"]), catStr(it["name"]), catStr(it["title"]), catStr(it["item"])),
				"status": firstNonEmpty(catStr(it["status"]), map[bool]string{true: "Done", false: "Open"}[it["done"] == true]),
				"by":     firstNonEmpty(catStr(it["done_by"]), catStr(it["completed_by"])), "note": firstNonEmpty(catStr(it["note"]), catStr(it["remarks"]))}
			out = append(out, &r)
		}
	}
	return out, nil
}, StoreOf: func(r *catRow) string { return catStr(employeeOf(r, "employee_id")["location"]) },
	Cols: mergeCols(employeeCols("employee_id"), map[string]catFn{
		"Document Type": catL("task"),
		"Status":        catL("status"),
		"User Name":     catL("by"),
		"Approved By":   catNotHeld,
		"Remarks":       catL("note"),
	})}

var catAppraisals = &catSource{Label: "Appraisal", Doctype: "Appraisal", StoreOf: func(r *catRow) string {
	return catStr(employeeOf(r, "employee_id")["location"])
},
	Cols: mergeCols(employeeCols("employee_id"), map[string]catFn{
		"From Date": func(r *catRow) interface{} {
			return catDay(r.master("AppraisalCycle", r.hs("cycle_id"))["period_start"])
		},
		"To Date": func(r *catRow) interface{} { return catDay(r.master("AppraisalCycle", r.hs("cycle_id"))["period_end"]) },
		"Achievement %": func(r *catRow) interface{} {
			if v := r.hn("overall_rating"); v > 0 {
				return math.Round(v/5*1000) / 10 // a 1-5 rating as a share of the best
			}
			return nil
		},
		"Remarks": catH("comments"),
	})}

var catTraining = &catSource{Label: "Training", Doctype: "TrainingRecord", DateField: "completion_date", StoreOf: func(r *catRow) string {
	return catStr(employeeOf(r, "employee_id")["location"])
},
	Cols: mergeCols(employeeCols("employee_id"), map[string]catFn{
		"Document Date": catHD("completion_date"),
		"Document Type": func(r *catRow) interface{} {
			p := r.master("TrainingProgram", r.hs("program_id"))
			return firstNonEmpty(catStr(p["description"]), catStr(p["category"]), r.hs("program_id"))
		},
		"Remarks": func(r *catRow) interface{} {
			if s := r.hs("score"); s != "" {
				return "Score " + s
			}
			return nil
		},
	})}

var _ = catWire("cat-expense-claim-register", &catReport{Src: catClaims})
var _ = catWire("cat-claims-pending-approval", &catReport{Src: catClaims,
	Keep: func(r *catRow) bool { return r.Status == "Submitted" || r.Status == "Pending Approval" }})
var _ = catWire("cat-approved-pending-payment", &catReport{Src: catClaims,
	Keep: func(r *catRow) bool { return r.Status == "Verified" || r.Status == "Approved" }})
var _ = catWire("cat-paid-claims-register", &catReport{Src: catClaims, Keep: func(r *catRow) bool { return r.Status == "Paid" }})
var _ = catWire("cat-rejected-returned-claims", &catReport{Src: catClaims,
	Keep: func(r *catRow) bool { return r.Status == "Rejected" || r.Status == "Returned" }})
var _ = catWire("cat-expense-aging-sla-report", &catReport{Src: catClaims, Keep: claimOpen,
	Cols: map[string]catFn{"Status": func(r *catRow) interface{} {
		if r.since(r.Created) > expenseSLAHours*time.Hour {
			return "SLA breached"
		}
		return "Within SLA"
	}}})
var _ = catWire("cat-department-category-month-summary", &catReport{Src: catClaims,
	Group: []string{"From Date", "To Date", "Department", "Expense Category"}, Agg: map[string]string{"Headcount": "countd"}})
var _ = catWire("cat-employee-expense-summary", &catReport{Src: catClaims,
	Group: []string{"From Date", "To Date", "Employee ID", "Employee Name", "Department"},
	Agg:   map[string]string{"Payment Status": "min"}})
var _ = catWire("cat-policy-violation-exception-report", &catReport{Src: catClaims, Keep: func(r *catRow) bool { return len(claimExceptions(r)) > 0 }})
var _ = catWire("cat-expense-advance-settlement", &catReport{Src: catClaims, Keep: func(r *catRow) bool { return r.hn("advance_adjusted") > 0 }})
var _ = catWire("cat-accounting-export-report", &catReport{Src: catClaimPostings})

var _ = catWire("cat-employee-master-register", &catReport{Src: catEmployees})
var _ = catWire("cat-attendance-register", &catReport{Src: catAttendance})
var _ = catWire("cat-late-overtime-report", &catReport{Src: catAttendance,
	Keep: func(r *catRow) bool {
		s := strings.ToLower(firstNonEmpty(r.hs("status"), r.Status))
		return strings.Contains(s, "late") || strings.Contains(s, "half") || strings.Contains(s, "overtime") || r.hn("worked_minutes") > 0
	}})
var _ = catWire("cat-leave-register", &catReport{Src: catLeaves})
var _ = catWire("cat-shift-roster-report", &catReport{Src: catRoster})
var _ = catWire("cat-payroll-register", &catReport{Src: catPayslips})
var _ = catWire("cat-salary-component-detail", &catReport{Src: catPayslipComponents})
var _ = catWire("cat-statutory-deduction-summary", &catReport{Src: catPayslipComponents,
	Keep: func(r *catRow) bool {
		return strings.HasSuffix(r.ls("field"), "_deduction") && r.ls("field") != "loan_deduction"
	},
	Group: []string{"Payroll Period", "Department", "Document Type"}, Agg: map[string]string{"Status": "first"}})
var _ = catWire("cat-employee-loan-advance-register", &catReport{Src: catLoans})
var _ = catWire("cat-headcount-summary", &catReport{Src: catEmployees, NoDates: true,
	Keep:  func(r *catRow) bool { return r.Status == "Active" || r.Status == "On Notice" },
	Group: []string{"As Of Date", "Department", "Store Code", "Store Name"}, Agg: map[string]string{"Status": "first"}})
var _ = catWire("cat-attrition-report", &catReport{Src: catEmployees, NoDates: true,
	Keep: func(r *catRow) bool {
		return r.hs("date_of_exit") != "" || r.Status == "Exited" || r.Status == "Inactive"
	}})
var _ = catWire("cat-onboarding-checklist", &catReport{Src: catOnboarding})
var _ = catWire("cat-performance-review-summary", &catReport{Src: catAppraisals})
var _ = catWire("cat-training-certification-register", &catReport{Src: catTraining})
var _ = catWire("cat-employee-role-access-mapping", &catReport{Src: catEmployees, NoDates: true, Keep: func(r *catRow) bool { return r.hs("user_id") != "" }})
