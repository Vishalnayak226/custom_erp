package engines

// Ask 169: carrier claims (73.4.2.6) and freight invoice reconciliation
// (73.4.2.7), both on OMS -> Freight.
//
// A CarrierClaim is raised against a LogisticsBooking for a lost, damaged or
// short shipment and moves Open -> Filed (the carrier's reference) ->
// Settled (what the carrier paid), or Rejected with a note. One claim may be
// open per booking. Every move is here: the doctype is strict with no generic
// rule, so the document API cannot jump a claim to Settled.
//
// A FreightInvoiceLine is one AWB on a carrier's freight invoice, loaded with
// the standard CSV import. ReconcileFreightInvoices matches each line not yet
// matched to its booking by AWB (provider AWB, AWB number or tracking number)
// and sets the expected charge (the provider's quote, else the booking's
// shipping charge), the difference and a status: Matched within the
// tolerance, Overcharged, Undercharged, No booking, or Duplicate when the AWB
// is already billed on another line.

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"custom_erp/db"
)

const SettingFreightTolerance = "orders.freight_tolerance"

func init() {
	zero, most := 0.0, 100000.0
	RegisterSetting(SettingDefinition{Key: SettingFreightTolerance, Module: "Orders", Label: "Freight invoice tolerance",
		Type: SettingTypeInt, Default: "1", Unit: "rupees", Min: &zero, Max: &most,
		Description: "A freight invoice line within this many rupees of its booking's charge counts as matched."})
}

// bookingByAWB finds the newest booking carrying this AWB in any of its AWB
// fields: the courier's, the booking's own or the tracking number. A miss is
// sql.ErrNoRows.
func bookingByAWB(q interface {
	QueryRow(query string, args ...interface{}) *sql.Row
}, schema, awb string) (id string, data []byte, status string, err error) {
	err = q.QueryRow(fmt.Sprintf(`SELECT id, data, status FROM %s.documents WHERE doctype = 'LogisticsBooking' AND deleted_at IS NULL
		AND $1 <> '' AND $1 IN (COALESCE(data->>'provider_awb', ''), COALESCE(data->>'awb_number', ''), COALESCE(data->>'tracking_number', ''))
		ORDER BY created_at DESC LIMIT 1`, schema), strings.TrimSpace(awb)).Scan(&id, &data, &status)
	return id, data, status, err
}

func claimErr(format string, a ...interface{}) error {
	return &ValidationError{Code: "ORDERM-0471", SubFor: "CarrierClaim", Message: fmt.Sprintf(format, a...)}
}

// RaiseCarrierClaim opens a claim against a booking.
func RaiseCarrierClaim(tenantID, bookingID, claimType string, amount float64, notes, userID string) (string, error) {
	claimType = strings.TrimSpace(claimType)
	if claimType != "Lost" && claimType != "Damaged" && claimType != "Short" {
		return "", claimErr("claim type must be Lost, Damaged or Short")
	}
	amount = round2(amount)
	if amount <= 0 {
		return "", claimErr("claimed amount must be more than 0")
	}
	bookingID = strings.TrimSpace(bookingID)
	schema, booking, bookingStatus, err := fetchLogisticsBooking(tenantID, bookingID)
	if err != nil {
		// Not a booking id: take it as the AWB the user has in hand.
		if schema, err = db.GetTenantSchema(tenantID); err != nil {
			return "", err
		}
		var raw []byte
		bookingID, raw, bookingStatus, err = bookingByAWB(db.DB, schema, bookingID)
		if err == sql.ErrNoRows {
			return "", &ValidationError{Code: "GLOBAL-0004", Message: "no booking has this id or AWB"}
		} else if err != nil {
			return "", err
		}
		if err := json.Unmarshal(raw, &booking); err != nil {
			return "", err
		}
	}
	if bookingStatus == "Cancelled" {
		return "", claimErr("booking %s is cancelled", bookingID)
	}
	var open string
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT id FROM %s.documents WHERE doctype = 'CarrierClaim' AND deleted_at IS NULL
		AND data->>'booking_id' = $1 AND status IN ('Open', 'Filed') LIMIT 1`, schema), bookingID).Scan(&open)
	if err == nil {
		return "", claimErr("claim %s is already open on this booking", open)
	} else if err != sql.ErrNoRows {
		return "", err
	}
	id := NewDocID("CLM")
	data, _ := json.Marshal(map[string]interface{}{
		"code": id, "booking_id": bookingID, "order_id": strField(booking, "order_id"),
		"carrier": strField(booking, "carrier"), "awb": firstNonEmpty(strField(booking, "provider_awb"), strField(booking, "awb_number"), strField(booking, "tracking_number")),
		"claim_type": claimType, "amount_claimed": amount, "amount_settled": 0, "notes": strings.TrimSpace(notes),
		"status": "Open", "raised_by": userID,
	})
	if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by)
		VALUES ($1, 'CarrierClaim', $2, 'Open', 'system')`, schema), id, data); err != nil {
		return "", err
	}
	LogAuditEvent(tenantID, userID, "CARRIER_CLAIM_RAISED", "SUCCESS", fmt.Sprintf("%s on %s: %s %.2f", id, bookingID, claimType, amount))
	return id, nil
}

// CarrierClaimMove is one step on a claim.
type CarrierClaimMove struct {
	Action    string  `json:"action"` // file, settle, reject
	ClaimRef  string  `json:"claim_ref"`
	Amount    float64 `json:"amount"`
	Note      string  `json:"note"`
	SettledOn string  `json:"settled_on"`
}

// MoveCarrierClaim files, settles or rejects a claim.
func MoveCarrierClaim(tenantID, claimID string, mv CarrierClaimMove, userID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var raw []byte
	var status string
	err = tx.QueryRow(fmt.Sprintf(`SELECT data, status FROM %s.documents WHERE doctype = 'CarrierClaim' AND id = $1 AND deleted_at IS NULL FOR UPDATE`, schema), claimID).Scan(&raw, &status)
	if err == sql.ErrNoRows {
		return &ValidationError{Code: "GLOBAL-0004", Message: fmt.Sprintf("claim %s not found", claimID)}
	} else if err != nil {
		return err
	}
	var claim map[string]interface{}
	if err := json.Unmarshal(raw, &claim); err != nil {
		return err
	}
	next := ""
	switch strings.ToLower(strings.TrimSpace(mv.Action)) {
	case "file":
		if status != "Open" {
			return claimErr("only an Open claim can be filed (this one is %s)", status)
		}
		if strings.TrimSpace(mv.ClaimRef) == "" {
			return claimErr("give the carrier's claim reference")
		}
		next = "Filed"
		claim["claim_ref"] = strings.TrimSpace(mv.ClaimRef)
		claim["filed_at"] = time.Now().UTC().Format(time.RFC3339)
	case "settle":
		if status != "Filed" {
			return claimErr("only a Filed claim can be settled (this one is %s)", status)
		}
		claimed := round2(numFromInterface(claim["amount_claimed"]))
		amt := round2(mv.Amount)
		if amt <= 0 || amt > claimed {
			return claimErr("settled amount must be more than 0 and at most %.2f", claimed)
		}
		on := strings.TrimSpace(mv.SettledOn)
		if on == "" {
			on = BusinessToday()
		} else if _, err := time.Parse("2006-01-02", on); err != nil {
			return claimErr("settled on must be a date (YYYY-MM-DD)")
		}
		next = "Settled"
		claim["amount_settled"] = amt
		claim["settled_on"] = on
	case "reject":
		if status != "Open" && status != "Filed" {
			return claimErr("only an Open or Filed claim can be rejected (this one is %s)", status)
		}
		if strings.TrimSpace(mv.Note) == "" {
			return claimErr("say why the claim was rejected")
		}
		next = "Rejected"
	default:
		return claimErr("unknown claim step %q", mv.Action)
	}
	if n := strings.TrimSpace(mv.Note); n != "" {
		claim["notes"] = strings.TrimSpace(strField(claim, "notes") + "\n" + n)
	}
	claim["status"] = next
	out, _ := json.Marshal(claim)
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $1, status = $2, updated_at = CURRENT_TIMESTAMP
		WHERE doctype = 'CarrierClaim' AND id = $3`, schema), out, next, claimID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	LogAuditEvent(tenantID, userID, "CARRIER_CLAIM_"+strings.ToUpper(next), "SUCCESS", claimID)
	return nil
}

// FreightReconcileResult counts what one reconciliation run decided.
type FreightReconcileResult struct {
	Checked      int `json:"checked"`
	Matched      int `json:"matched"`
	Overcharged  int `json:"overcharged"`
	Undercharged int `json:"undercharged"`
	NoBooking    int `json:"no_booking"`
	Duplicate    int `json:"duplicate"`
}

// ReconcileFreightInvoices matches every freight invoice line not yet matched
// to its booking. Lines already decided keep their answer; a line with no
// booking is tried again, since the booking may have come in since.
func ReconcileFreightInvoices(tenantID, userID string) (FreightReconcileResult, error) {
	var res FreightReconcileResult
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return res, err
	}
	tolerance := float64(GetSettingInt(tenantID, SettingFreightTolerance))
	tx, err := db.DB.Begin()
	if err != nil {
		return res, err
	}
	defer tx.Rollback()
	// One run at a time per tenant, so two runs cannot both call the same
	// AWB the first bill.
	if _, err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext($1))`, schema+":freight-reconcile"); err != nil {
		return res, err
	}
	rows, err := tx.Query(fmt.Sprintf(`SELECT id, data FROM %s.documents WHERE doctype = 'FreightInvoiceLine' AND deleted_at IS NULL
		AND COALESCE(data->>'status', '') IN ('', 'Unmatched', 'No booking')
		ORDER BY created_at, id`, schema))
	if err != nil {
		return res, err
	}
	type line struct {
		id   string
		data map[string]interface{}
	}
	var lines []line
	for rows.Next() {
		var l line
		var raw []byte
		if err := rows.Scan(&l.id, &raw); err != nil {
			rows.Close()
			return res, err
		}
		if err := json.Unmarshal(raw, &l.data); err != nil {
			rows.Close()
			return res, err
		}
		lines = append(lines, l)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return res, err
	}
	for _, l := range lines {
		res.Checked++
		awb := strings.TrimSpace(strField(l.data, "awb"))
		charged := round2(numFromInterface(l.data["charged_amount"]))
		bookingID, bookingRaw, _, err := bookingByAWB(tx, schema, awb)
		status := ""
		var expected, diff float64
		switch {
		case err == sql.ErrNoRows:
			status = "No booking"
			res.NoBooking++
		case err != nil:
			return res, err
		default:
			var billed string
			dupErr := tx.QueryRow(fmt.Sprintf(`SELECT id FROM %s.documents WHERE doctype = 'FreightInvoiceLine' AND deleted_at IS NULL AND id <> $1
				AND data->>'booking_id' = $2 AND data->>'status' IN ('Matched', 'Overcharged', 'Undercharged') LIMIT 1`, schema), l.id, bookingID).Scan(&billed)
			var booking map[string]interface{}
			_ = json.Unmarshal(bookingRaw, &booking)
			expected = round2(numFromInterface(booking["provider_charge_paise"]) / 100)
			if expected == 0 {
				expected = round2(numFromInterface(booking["shipping_charge"]))
			}
			diff = round2(charged - expected)
			switch {
			case dupErr == nil:
				status = "Duplicate"
				l.data["duplicate_of"] = billed
				res.Duplicate++
			case dupErr != sql.ErrNoRows:
				return res, dupErr
			case diff > tolerance:
				status = "Overcharged"
				res.Overcharged++
			case diff < -tolerance:
				status = "Undercharged"
				res.Undercharged++
			default:
				status = "Matched"
				res.Matched++
			}
			l.data["booking_id"] = bookingID
			l.data["expected_amount"] = expected
			l.data["difference"] = diff
		}
		l.data["status"] = status
		l.data["reconciled_at"] = time.Now().UTC().Format(time.RFC3339)
		out, _ := json.Marshal(l.data)
		if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $1, status = $2, updated_at = CURRENT_TIMESTAMP
			WHERE doctype = 'FreightInvoiceLine' AND id = $3`, schema), out, status, l.id); err != nil {
			return res, err
		}
	}
	if err := tx.Commit(); err != nil {
		return res, err
	}
	if res.Checked > 0 {
		LogAuditEvent(tenantID, userID, "FREIGHT_INVOICE_RECONCILED", "SUCCESS",
			fmt.Sprintf("%d checked: %d matched, %d over, %d under, %d no booking, %d duplicate",
				res.Checked, res.Matched, res.Overcharged, res.Undercharged, res.NoBooking, res.Duplicate))
	}
	return res, nil
}
