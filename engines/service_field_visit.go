package engines

import (
	"database/sql"
	"errors"
	"time"
)

// Ask 176 item 176.4: field-service visits.
//
// A ServiceVisit is one engineer's visit on a service ticket. Moving it to
// "Checked in" stamps checked_in_at with the business-time now, and "Done"
// stamps checked_out_at, unless a time was typed (a visit logged after the
// fact). Done also needs a check-in and the name of the customer who signed
// the work off; a Done visit is then the record and is not edited.

const (
	ServiceVisitDoctype     = "ServiceVisit"
	serviceVisitCodeInvalid = "CRMPLT-1132"
	serviceVisitTimeLayout  = "2006-01-02 15:04"
)

// serviceVisitTimeLayouts are the forms a typed check-in or check-out time
// is read in; it is stored back as serviceVisitTimeLayout.
var serviceVisitTimeLayouts = []string{serviceVisitTimeLayout, "2006-01-02T15:04", time.RFC3339}

func validateServiceVisitSave(tenantID, priorStatus string, priorData, payload map[string]interface{}) error {
	bad := func(msg string) error { return &ValidationError{Code: serviceVisitCodeInvalid, Message: msg} }
	if priorStatus == "Done" {
		return bad("A Done visit cannot be changed.")
	}
	ticketRef := strField(payload, "ticket")
	_, ticketStatus, err := fetchDocData(tenantID, "ServiceTicket", ticketRef)
	if errors.Is(err, sql.ErrNoRows) {
		return bad("Pick the service ticket.")
	}
	if err != nil {
		return err
	}
	// A new visit, or one moved to another ticket, needs an open ticket.
	if (priorData == nil || strField(priorData, "ticket") != ticketRef) && (ticketStatus == "Closed" || ticketStatus == "Cancelled") {
		return bad("That ticket is closed. Pick an open ticket.")
	}
	if _, _, err := fetchDocData(tenantID, "Employee", strField(payload, "engineer")); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return bad("Pick the engineer.")
		}
		return err
	}
	if _, err := time.Parse("2006-01-02", strField(payload, "scheduled_on")); err != nil {
		return bad("Enter the visit date.")
	}

	now := time.Now().In(BusinessLocation())
	in, err := serviceVisitTime(payload, "checked_in_at")
	if err != nil {
		return bad("The check-in time is not readable. Use 2026-11-14 10:30.")
	}
	out, err := serviceVisitTime(payload, "checked_out_at")
	if err != nil {
		return bad("The check-out time is not readable. Use 2026-11-14 17:45.")
	}
	switch strField(payload, "status") {
	case "Checked in":
		if in.IsZero() {
			in = now
		}
	case "Done":
		if in.IsZero() {
			return bad("Check in before marking the visit Done.")
		}
		if out.IsZero() {
			out = now
		}
		if strField(payload, "customer_signoff_name") == "" {
			return bad("Enter the name of the customer who signed the work off.")
		}
	}
	if !in.IsZero() && !out.IsZero() && out.Before(in) {
		return bad("Check-out cannot be before check-in.")
	}
	if !in.IsZero() {
		payload["checked_in_at"] = in.Format(serviceVisitTimeLayout)
	}
	if !out.IsZero() {
		payload["checked_out_at"] = out.Format(serviceVisitTimeLayout)
	}
	return nil
}

// serviceVisitTime reads a typed time in business time; zero when blank.
func serviceVisitTime(payload map[string]interface{}, key string) (time.Time, error) {
	s := strField(payload, key)
	if s == "" {
		return time.Time{}, nil
	}
	for _, layout := range serviceVisitTimeLayouts {
		if t, err := time.ParseInLocation(layout, s, BusinessLocation()); err == nil {
			return t.In(BusinessLocation()), nil
		}
	}
	return time.Time{}, errors.New("unreadable time")
}
