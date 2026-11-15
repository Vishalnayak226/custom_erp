package engines

import (
	"custom_erp/db"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Ask 176 item 176.2: travel requests.
//
// A TravelRequest (who travels, where, when, the estimated cost) goes through
// the maker-checker approval like any approval-gated document (seeded rule:
// Store Manager approves). An employee advance (StaffAdvance) and an expense
// claim may then name it in travel_request, and only an Approved request of
// the same employee may be named. The "Travel requests" report sets each
// request's estimate against the advances paid and the claims filed on it.

const (
	TravelRequestDoctype     = "TravelRequest"
	travelRequestCodeInvalid = "EXP-1703"
	travelRequestCodeLink    = "EXP-1704"
)

func init() {
	RegisterReport(ReportDefinition{
		ID: "travel-requests", Label: "Travel requests", Category: "Expenses",
		Columns: []ReportColumn{
			{Key: "code", Label: "Request"}, {Key: "employee_id", Label: "Employee"},
			{Key: "destination", Label: "Destination"},
			{Key: "from_date", Label: "From"}, {Key: "to_date", Label: "To"},
			{Key: "status", Label: "Status"},
			{Key: "estimated", Label: "Estimated", Sensitive: true, FinanceSees: true, Total: true},
			{Key: "advance_paid", Label: "Advance paid", Sensitive: true, FinanceSees: true, Total: true},
			{Key: "claimed", Label: "Claimed", Sensitive: true, FinanceSees: true, Total: true},
			{Key: "left", Label: "Left of estimate", Sensitive: true, FinanceSees: true, Total: true},
		},
		Params: []ReportParam{{Key: "status", Label: "Status", Type: "select", Options: ",Draft,Pending Approval,Approved,Rejected"}},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			return TravelRequestSummary(tenantID, params["status"])
		},
	})
}

// validateTravelRequestSave checks a travel request on every record-form
// save. Approval itself is the approval engine's; an Approved request is
// what advances and claims rely on, so it is not edited afterwards.
func validateTravelRequestSave(tenantID, priorStatus string, payload map[string]interface{}) error {
	bad := func(msg string) error { return &ValidationError{Code: travelRequestCodeInvalid, Message: msg} }
	if priorStatus == "Approved" {
		return bad("An approved travel request cannot be changed.")
	}
	if _, _, err := fetchDocData(tenantID, "Employee", strField(payload, "employee_id")); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return bad("Pick the employee who travels.")
		}
		return err
	}
	if strField(payload, "purpose") == "" || strField(payload, "destination") == "" {
		return bad("Give the purpose and the destination.")
	}
	from, errFrom := time.Parse("2006-01-02", strField(payload, "from_date"))
	to, errTo := time.Parse("2006-01-02", strField(payload, "to_date"))
	if errFrom != nil || errTo != nil {
		return bad("Enter the From and To dates.")
	}
	if to.Before(from) {
		return bad("The To date cannot be before the From date.")
	}
	amount := numFromInterface(payload["amount"])
	if amount <= 0 {
		return bad("The estimated cost must be above 0.")
	}
	if adv := numFromInterface(payload["advance_needed"]); adv < 0 || adv > amount {
		return bad("The advance needed cannot be more than the estimated cost.")
	}
	return nil
}

// validateTravelRequestLink is the employee advance's and the expense
// claim's step: a travel request they name must be Approved and be for the
// same employee. Blank is allowed - most claims are not for a trip.
func validateTravelRequestLink(tenantID string, payload map[string]interface{}) error {
	ref := strField(payload, "travel_request")
	if ref == "" {
		return nil
	}
	bad := func(msg string) error { return &ValidationError{Code: travelRequestCodeLink, Message: msg} }
	req, status, err := fetchDocData(tenantID, TravelRequestDoctype, ref)
	if errors.Is(err, sql.ErrNoRows) {
		return bad("The travel request was not found.")
	}
	if err != nil {
		return err
	}
	if status != "Approved" {
		return bad("Only an approved travel request can be named.")
	}
	same, err := sameDocRef(tenantID, "Employee", strField(req, "employee_id"), strField(payload, "employee_id"))
	if err != nil {
		return err
	}
	if !same {
		return bad("The travel request is for another employee.")
	}
	return nil
}

// sameDocRef reports whether two references (each a document id or code)
// name the same document of doctype. A Link may hold either, so comparing
// the strings alone would call one employee two people.
func sameDocRef(tenantID, doctype, a, b string) (bool, error) {
	if a == "" || b == "" {
		return false, nil
	}
	if a == b {
		return true, nil
	}
	idA, err := docRefID(tenantID, doctype, a)
	if err != nil {
		return false, err
	}
	idB, err := docRefID(tenantID, doctype, b)
	if err != nil {
		return false, err
	}
	return idA != "" && idA == idB, nil
}

// docRefID is the id of the live document of doctype whose id or code is
// ref, "" when there is none.
func docRefID(tenantID, doctype, ref string) (string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	var id string
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT id FROM %s.documents
		 WHERE doctype = $1 AND deleted_at IS NULL AND (id = $2 OR data->>'code' = $2)
		 ORDER BY CASE WHEN id = $2 THEN 0 ELSE 1 END LIMIT 1`, schema), doctype, ref).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return id, err
}

// TravelRequestSummary is one row per travel request: the estimate, the
// advances paid against it (Paid or Settled) and the claims filed on it
// (amount plus GST, any status but Draft or Rejected). Read from the
// documents each time, never a stored counter.
func TravelRequestSummary(tenantID, status string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT COALESCE(NULLIF(r.data->>'code', ''), r.id), COALESCE(r.data->>'employee_id', ''),
		       COALESCE(r.data->>'destination', ''), COALESCE(r.data->>'from_date', ''),
		       COALESCE(r.data->>'to_date', ''), r.status,
		       COALESCE(NULLIF(r.data->>'amount', '')::numeric, 0)::float8,
		       COALESCE((SELECT SUM(COALESCE(NULLIF(a.data->>'amount', '')::numeric, 0)) FROM %[1]s.documents a
		                  WHERE a.doctype = 'StaffAdvance' AND a.deleted_at IS NULL AND a.status IN ('Paid', 'Settled')
		                    AND a.data->>'travel_request' IN (r.id, r.data->>'code')), 0)::float8,
		       COALESCE((SELECT SUM(COALESCE(NULLIF(c.data->>'amount', '')::numeric, 0)
		                          + COALESCE(NULLIF(c.data->>'gst_amount', '')::numeric, 0)) FROM %[1]s.documents c
		                  WHERE c.doctype = 'ExpenseClaim' AND c.deleted_at IS NULL AND c.status NOT IN ('Draft', 'Rejected')
		                    AND c.data->>'travel_request' IN (r.id, r.data->>'code')), 0)::float8
		  FROM %[1]s.documents r
		 WHERE r.doctype = 'TravelRequest' AND r.deleted_at IS NULL AND ($1 = '' OR r.status = $1)
		 ORDER BY r.data->>'from_date' DESC NULLS LAST, r.id`, schema), status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]interface{}{}
	for rows.Next() {
		var code, emp, dest, from, to, st string
		var est, adv, claimed float64
		if err := rows.Scan(&code, &emp, &dest, &from, &to, &st, &est, &adv, &claimed); err != nil {
			return nil, err
		}
		out = append(out, map[string]interface{}{
			"code": code, "employee_id": emp, "destination": dest, "from_date": from, "to_date": to, "status": st,
			"estimated": round2(est), "advance_paid": round2(adv), "claimed": round2(claimed), "left": round2(est - claimed),
		})
	}
	return out, rows.Err()
}
