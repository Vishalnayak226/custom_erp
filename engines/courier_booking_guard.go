package engines

import (
	"context"
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	neturl "net/url"
	"strings"
	"sync/atomic"
	"time"
)

// 47.11.8a: nothing is booked twice with a courier - the standard idempotent
// booking practice, on top of AllocateCourierAWB:
//
//  1. Before the call the attempt is saved: the claim, and the booking's
//     stable reference (courier_reference, its id), which every retry sends
//     again as the courier's order / reference id.
//  2. A call that never wrote its request (DNS, refused, TLS) or got a plain
//     answer releases the claim, so a retry is an ordinary retry. A call that
//     may have reached the courier (no answer after sending, a 5xx or 409, an
//     answer that cannot be read) keeps the claim as "uncertain".
//  3. The retry of an uncertain booking first asks the courier by that
//     reference (CourierBookingLookup): the AWB found is adopted; it books only
//     when the courier says there is none. A courier with no lookup refuses the
//     retry (CourierUnconfirmedCode) until someone resolves it from the
//     courier's portal: the AWB found there, or "not booked".
//  4. db/migrations_stage47_11_8a_courier_booking_guard.sql is the backstop:
//     one AWB on one live booking, one live AWB per parcel.

// CourierBookingLookup is optional on a CourierAdapter: an adapter whose API
// can find a shipment by the reference it was booked under implements it.
// found=false with a nil error means the courier confirms it has none; any
// doubt is an error, and the booking stays uncertain.
type CourierBookingLookup interface {
	FindBooking(ctx context.Context, cred map[string]string, req CourierShipmentRequest) (awb CourierAWB, found bool, err error)
}

// CourierUnconfirmedCode is a booking the courier may already have made that
// this server cannot check: 409, resolved from the courier's portal.
const CourierUnconfirmedCode = "INTEGR-4718"

const (
	courierStateUncertain = "uncertain" // may have reached the courier; claim kept
	courierStateChecking  = "checking"  // a retry is asking the courier / booking now
	// A claim with no outcome older than this belongs to a call whose process
	// stopped mid-flight (a booking call is bounded well under it), so the
	// courier may have the request: it is treated as uncertain.
	courierClaimStale = 2 * time.Minute
)

var errCourierNoAWB = errors.New("no AWB in the answer")

type courierBookingClaim struct {
	schema      string
	data        map[string]interface{}
	ref         string
	lookupFirst bool
}

// undo puts the booking back as it was when this call claimed it.
func (c courierBookingClaim) undo(bookingID string) {
	if c.lookupFirst {
		markCourierUncertain(c.schema, bookingID)
		return
	}
	releaseCourierClaim(c.schema, bookingID)
}

func courierClaimIsStale(claimedAt string) bool {
	t, err := time.Parse(time.RFC3339Nano, claimedAt)
	return err != nil || time.Since(t) > courierClaimStale
}

func lockLogisticsBooking(tx *sql.Tx, schema, bookingID string) (map[string]interface{}, string, error) {
	var raw []byte
	var status string
	err := tx.QueryRow(fmt.Sprintf(`SELECT data, status FROM %s.documents WHERE doctype = 'LogisticsBooking' AND id = $1 AND deleted_at IS NULL FOR UPDATE`, schema), bookingID).Scan(&raw, &status)
	if err != nil {
		return nil, "", fmt.Errorf("logistics booking %s not found: %v", bookingID, err)
	}
	var data map[string]interface{}
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, "", err
	}
	return data, status, nil
}

// claimCourierBooking saves the attempt under the booking's row lock, so two
// calls at once (a double click, two retries) cannot both reach the courier.
// It returns the booking's AWB instead when one is already recorded.
func claimCourierBooking(tenantID, provider, bookingID, remoteShipmentID string, canLookup bool) (courierBookingClaim, CourierAWB, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return courierBookingClaim{}, CourierAWB{}, err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return courierBookingClaim{}, CourierAWB{}, err
	}
	defer tx.Rollback()
	data, status, err := lockLogisticsBooking(tx, schema, bookingID)
	if err != nil {
		return courierBookingClaim{}, CourierAWB{}, err
	}
	if status != "AWB Assigned" {
		return courierBookingClaim{}, CourierAWB{}, fmt.Errorf("booking %s cannot allocate an AWB from status %q", bookingID, status)
	}
	if existing := courierDataString(data, "provider_awb"); existing != "" {
		return courierBookingClaim{}, CourierAWB{AWB: existing, TrackingNumber: courierDataString(data, "tracking_number"), RemoteShipmentID: courierDataString(data, "remote_shipment_id")}, nil
	}
	c := courierBookingClaim{schema: schema, data: data, ref: courierDataString(data, "courier_reference")}
	if c.ref == "" {
		c.ref = bookingID
	}
	claimedAt, state := courierDataString(data, "awb_allocation_claimed_at"), courierDataString(data, "awb_booking_state")
	switch {
	case claimedAt == "":
	case state == courierStateUncertain || courierClaimIsStale(claimedAt):
		c.lookupFirst = true
	default:
		return courierBookingClaim{}, CourierAWB{}, fmt.Errorf("booking %s AWB allocation is already in progress; retry shortly", bookingID)
	}
	if c.lookupFirst {
		if sentTo := courierDataString(data, "awb_booking_provider"); sentTo != "" && !strings.EqualFold(sentTo, provider) {
			return courierBookingClaim{}, CourierAWB{}, courierUnconfirmed(sentTo, c.ref, fmt.Sprintf("Booking %s may already be booked with %s.", bookingID, sentTo))
		}
		if !canLookup {
			if state != courierStateUncertain {
				if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || jsonb_build_object('awb_booking_state', $1::text) WHERE id = $2 AND doctype = 'LogisticsBooking'`, schema), courierStateUncertain, bookingID); err == nil {
					_ = tx.Commit()
				}
			}
			return courierBookingClaim{}, CourierAWB{}, courierUnconfirmed(provider, c.ref, fmt.Sprintf("%s may already have this shipment.", provider))
		}
	}
	if pkg := courierDataString(data, "shipping_package_id"); pkg != "" {
		if err := courierParcelFree(tx, schema, bookingID, pkg); err != nil {
			return courierBookingClaim{}, CourierAWB{}, err
		}
	}
	patch := map[string]interface{}{
		"awb_allocation_claimed_at": time.Now().UTC().Format(time.RFC3339Nano),
		"courier_reference":         c.ref,
		"awb_booking_provider":      provider,
	}
	if c.lookupFirst {
		patch["awb_booking_state"] = courierStateChecking
	}
	if courierDataString(data, "remote_shipment_id") == "" && strings.TrimSpace(remoteShipmentID) != "" {
		patch["remote_shipment_id"] = strings.TrimSpace(remoteShipmentID)
	}
	encoded, _ := json.Marshal(patch)
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = (data - 'awb_booking_state') || $1::jsonb WHERE id = $2 AND doctype = 'LogisticsBooking'`, schema), encoded, bookingID); err != nil {
		return courierBookingClaim{}, CourierAWB{}, err
	}
	if err := tx.Commit(); err != nil {
		return courierBookingClaim{}, CourierAWB{}, err
	}
	for k, v := range patch {
		c.data[k] = v
	}
	return c, CourierAWB{}, nil
}

// courierParcelFree refuses a courier booking for a parcel another live
// booking already holds an AWB for, or is booking right now. The advisory lock
// makes two bookings of one parcel take turns here.
func courierParcelFree(tx *sql.Tx, schema, bookingID, pkg string) error {
	if _, err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext($1))`, schema+":courier-parcel:"+pkg); err != nil {
		return err
	}
	var other, awb string
	err := tx.QueryRow(fmt.Sprintf(`SELECT id, COALESCE(data->>'provider_awb', '') FROM %s.documents
		WHERE doctype = 'LogisticsBooking' AND deleted_at IS NULL AND id <> $1 AND data->>'shipping_package_id' = $2
		  AND status NOT IN ('Cancelled', 'RTO') AND NOT (data ? 'courier_cancelled_at')
		  AND (COALESCE(data->>'provider_awb', '') <> '' OR COALESCE(data->>'awb_allocation_claimed_at', '') <> '')
		LIMIT 1`, schema), bookingID, pkg).Scan(&other, &awb)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	if awb != "" {
		return fmt.Errorf("parcel %s already has courier AWB %s on booking %s", pkg, awb, other)
	}
	return fmt.Errorf("parcel %s is already being booked with the courier on booking %s", pkg, other)
}

func markCourierUncertain(schema, bookingID string) {
	_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || jsonb_build_object('awb_booking_state', $1::text, 'awb_uncertain_at', $2::text), updated_at = CURRENT_TIMESTAMP
		WHERE id = $3 AND doctype = 'LogisticsBooking' AND COALESCE(data->>'provider_awb', '') = ''`, schema), courierStateUncertain, time.Now().UTC().Format(time.RFC3339), bookingID)
}

// releaseCourierClaim: nothing is outstanding at the courier. The reference
// stays, so the next attempt sends the same one.
func releaseCourierClaim(schema, bookingID string) {
	_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data - 'awb_allocation_claimed_at' - 'awb_booking_state' - 'awb_booking_provider' WHERE id = $1 AND doctype = 'LogisticsBooking'`, schema), bookingID)
}

// recordCourierAWB writes the courier's AWB once: never over another one.
func recordCourierAWB(tenantID, schema, provider, bookingID string, awb CourierAWB, action, actor string, extra map[string]interface{}) (CourierAWB, error) {
	awb.AWB = strings.TrimSpace(awb.AWB)
	if awb.TrackingNumber == "" {
		awb.TrackingNumber = awb.AWB
	}
	patch := map[string]interface{}{
		"carrier": provider, "awb_number": awb.AWB, "provider_awb": awb.AWB,
		"tracking_number": awb.TrackingNumber, "remote_shipment_id": awb.RemoteShipmentID,
		"provider_charge_paise": awb.ChargePaise, "awb_allocated_at": time.Now().UTC().Format(time.RFC3339),
	}
	for k, v := range extra {
		patch[k] = v
	}
	encoded, _ := json.Marshal(patch)
	res, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = (data - 'awb_allocation_claimed_at' - 'awb_booking_state' - 'awb_booking_provider') || $1::jsonb, updated_at = CURRENT_TIMESTAMP
		WHERE id = $2 AND doctype = 'LogisticsBooking' AND COALESCE(data->>'provider_awb', '') = ''`, schema), encoded, bookingID)
	if err != nil {
		// The courier holds this AWB but it cannot be kept here: the booking
		// stays uncertain (nothing books again) and the AWB is named.
		markCourierUncertain(schema, bookingID)
		LogAuditEvent(tenantID, actor, "COURIER_AWB_NOT_RECORDED", "FAILURE", fmt.Sprintf("booking=%s provider=%s awb=%s: %v", bookingID, provider, awb.AWB, err))
		return CourierAWB{}, courierAWBConflict(awb.AWB, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		LogAuditEvent(tenantID, actor, "COURIER_AWB_DUPLICATE", "FAILURE", fmt.Sprintf("booking=%s provider=%s awb=%s", bookingID, provider, awb.AWB))
		return CourierAWB{}, fmt.Errorf("booking %s already has an AWB; %s also gave %s - cancel that one at %s", bookingID, provider, awb.AWB, provider)
	}
	LogAuditEvent(tenantID, actor, action, "SUCCESS", fmt.Sprintf("booking=%s provider=%s awb=%s", bookingID, provider, awb.AWB))
	return awb, nil
}

func courierAWBConflict(awb string, err error) error {
	switch msg := err.Error(); {
	case strings.Contains(msg, "idx_logistics_booking_provider_awb_unique"):
		return fmt.Errorf("AWB %s is already on another booking", awb)
	case strings.Contains(msg, "idx_logistics_booking_package_live_awb"):
		return fmt.Errorf("this parcel already has a live courier AWB on another booking (AWB %s not saved)", awb)
	}
	return err
}

// courierTracedContext reports whether any request of the call was written to
// the courier: until then, nothing can have reached it.
func courierTracedContext(ctx context.Context) (context.Context, *atomic.Bool) {
	wrote := new(atomic.Bool)
	return httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{WroteHeaders: func() { wrote.Store(true) }}), wrote
}

func courierTransportError(err error) bool {
	var urlErr *neturl.Error
	var netErr net.Error
	if errors.As(err, &urlErr) || errors.As(err, &netErr) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
	msg := err.Error() // doConnectorRequest's own safety timeout, a panic, an oversized answer
	return strings.Contains(msg, "exceeded safety timeout") || strings.Contains(msg, "panicked") || strings.Contains(msg, "larger than")
}

func courierUnreadable(err error) bool {
	var syn *json.SyntaxError
	var typ *json.UnmarshalTypeError
	return errors.Is(err, errCourierNoAWB) || errors.As(err, &syn) || errors.As(err, &typ)
}

// courierAnswerUnclear: after the request was written, did the courier give
// no clear answer? A refusal made here (switched off, breaker) and a plain
// courier "no" (a 4xx other than 409, a rejected shipment) are clear.
func courierAnswerUnclear(err error) bool {
	var verr *ValidationError
	if errors.As(err, &verr) {
		return false
	}
	var httpErr *ConnectorHTTPError
	if errors.As(err, &httpErr) {
		return httpErr.Status >= 500 || httpErr.Status == http.StatusConflict
	}
	return courierUnreadable(err) || courierTransportError(err)
}

// settleFailedCourierBooking keeps or releases the claim after a failed
// booking call and words the answer.
func settleFailedCourierBooking(tenantID string, c courierBookingClaim, provider, bookingID string, canLookup, wrote bool, err error) error {
	if wrote && courierAnswerUnclear(err) {
		markCourierUncertain(c.schema, bookingID)
		LogAuditEvent(tenantID, "system", "COURIER_BOOKING_UNCERTAIN", "HELD", fmt.Sprintf("booking=%s provider=%s reference=%s", bookingID, provider, c.ref))
		return courierBookingUncertain(provider, c.ref, canLookup, err)
	}
	releaseCourierClaim(c.schema, bookingID)
	return courierClearFailure(provider, wrote, err)
}

// courierClearFailure words a failed call that left nothing outstanding at
// the courier (shared with the pickup / cancel guard, 47.11.8b).
func courierClearFailure(provider string, wrote bool, err error) error {
	if !wrote && courierTransportError(err) && !errors.Is(err, context.Canceled) {
		return &ValidationError{Code: CourierUnreachableCode, Message: fmt.Sprintf("%s could not be reached - nothing was sent and nothing was changed here. Try again shortly.", provider)}
	}
	return courierCallFailed(provider, err)
}

// courierUnclearAnswer names what the courier did after the request was sent.
func courierUnclearAnswer(provider string, err error) string {
	var httpErr *ConnectorHTTPError
	switch {
	case errors.As(err, &httpErr) && httpErr.Status == http.StatusConflict:
		return provider + " says this may already exist (HTTP 409)"
	case errors.As(err, &httpErr):
		return fmt.Sprintf("%s is not working right now (HTTP %d)", provider, httpErr.Status)
	case courierUnreadable(err):
		return provider + "'s answer could not be read"
	}
	return provider + " did not answer"
}

func courierBookingUncertain(provider, ref string, canLookup bool, err error) error {
	what := courierUnclearAnswer(provider, err)
	if canLookup {
		return &ValidationError{Code: CourierUnreachableCode, Message: what + " - no AWB was recorded here, but it may have received the request. Try again: the retry asks " + provider + " first, so it is not booked twice."}
	}
	return courierUnconfirmed(provider, ref, what+" - it may have booked this shipment.")
}

func courierUnconfirmed(provider, ref, why string) error {
	return &ValidationError{Code: CourierUnconfirmedCode, Message: fmt.Sprintf("%s Find reference %s on the %s portal, then enter its AWB here or mark it not booked.", why, ref, provider)}
}

func courierLookupFailed(provider, ref string, err error) error {
	if err == nil {
		return courierUnconfirmed(provider, ref, provider+" answered the check without an AWB.")
	}
	var verr *ValidationError
	var httpErr *ConnectorHTTPError
	if errors.As(err, &verr) || courierTransportError(err) || (errors.As(err, &httpErr) && httpErr.Status >= 500) {
		return &ValidationError{Code: CourierUnreachableCode, Message: fmt.Sprintf("%s could not be asked about reference %s - nothing was booked. Try again shortly.", provider, ref)}
	}
	return courierUnconfirmed(provider, ref, provider+" could not confirm whether it has this shipment.")
}

// ResolveUnconfirmedCourierBooking settles an uncertain booking from what the
// courier's portal shows: outcome "booked" records the AWB found there,
// "not_booked" releases the claim so the next attempt books. Audited.
func ResolveUnconfirmedCourierBooking(tenantID, provider, bookingID, outcome, awb, actor string) (string, error) {
	if _, err := courierAdapter(provider); err != nil {
		return "", err
	}
	outcome, awb = strings.ToLower(strings.TrimSpace(outcome)), strings.TrimSpace(awb)
	switch outcome {
	case "booked":
		if awb == "" || len(awb) > 64 || strings.ContainsAny(awb, " \t\r\n") {
			return "", errors.New("enter the AWB exactly as the courier's portal shows it")
		}
	case "not_booked":
		awb = ""
	default:
		return "", errors.New("outcome must be booked or not_booked")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	data, _, err := lockLogisticsBooking(tx, schema, bookingID)
	if err != nil {
		return "", err
	}
	if existing := courierDataString(data, "provider_awb"); existing != "" {
		return "", fmt.Errorf("booking %s already has AWB %s", bookingID, existing)
	}
	claimedAt, state := courierDataString(data, "awb_allocation_claimed_at"), courierDataString(data, "awb_booking_state")
	if claimedAt == "" {
		return "", fmt.Errorf("booking %s has no unconfirmed courier booking to resolve", bookingID)
	}
	if state != courierStateUncertain && !courierClaimIsStale(claimedAt) {
		return "", fmt.Errorf("booking %s is being checked with the courier right now; try again shortly", bookingID)
	}
	if sentTo := courierDataString(data, "awb_booking_provider"); sentTo != "" && !strings.EqualFold(sentTo, provider) {
		return "", fmt.Errorf("booking %s was sent to %s, not %s", bookingID, sentTo, provider)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	patch := map[string]interface{}{"awb_resolution": outcome, "awb_resolved_by": actor, "awb_resolved_at": now}
	if outcome == "booked" {
		for k, v := range map[string]interface{}{"carrier": provider, "awb_number": awb, "provider_awb": awb, "tracking_number": awb, "awb_allocated_at": now} {
			patch[k] = v
		}
	}
	encoded, _ := json.Marshal(patch)
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = (data - 'awb_allocation_claimed_at' - 'awb_booking_state' - 'awb_booking_provider') || $1::jsonb, updated_at = CURRENT_TIMESTAMP
		WHERE id = $2 AND doctype = 'LogisticsBooking'`, schema), encoded, bookingID); err != nil {
		return "", courierAWBConflict(awb, err)
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	LogAuditEvent(tenantID, actor, "COURIER_BOOKING_RESOLVED", "SUCCESS", fmt.Sprintf("booking=%s provider=%s reference=%s outcome=%s awb=%s", bookingID, provider, courierDataString(data, "courier_reference"), outcome, awb))
	return awb, nil
}
