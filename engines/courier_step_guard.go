package engines

import (
	"context"
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// 47.11.8b: pickup scheduling and cancel get the retry guard 47.11.8a gave
// booking (courier_booking_guard.go), under the booking's own row lock:
//
//  1. The attempt is saved before the call (<step>_claimed_at), so two calls
//     at once cannot both reach the courier, and a step already done
//     (pickup_reference, courier_cancelled_at) is answered from the record
//     without a second call.
//  2. A call that never wrote its request, or got a clear answer, releases
//     the claim. One that may have reached the courier keeps it as uncertain.
//  3. The retry of an uncertain step asks the courier first when its adapter
//     can (CourierPickupLookup / CourierCancelLookup); otherwise it is refused
//     with CourierStepUnconfirmedCode until someone resolves it from the
//     courier's portal (ResolveUnconfirmedCourierStep, the same Resolve route
//     as a booking, with step = pickup / cancel).
//
// CourierTaskBookedCode is the other half of 47.11.8b: OMS → Ship "Book"
// refuses a second local booking for a fulfillment task that already has a
// live one (CreateLogisticsBooking), where the parcel index only stopped two
// AWBs.

// CourierTaskBookedCode: a fulfillment task already has a live courier
// booking; a second one is refused (409).
const CourierTaskBookedCode = "INTEGR-4719"

// CourierStepUnconfirmedCode: a pickup or cancel the courier may already have
// done that this server cannot check (409); resolved from the courier's portal.
const CourierStepUnconfirmedCode = "INTEGR-4720"

// CourierPickupLookup is optional on a CourierAdapter: one whose API can say
// whether a pickup is already scheduled for these AWBs implements it.
type CourierPickupLookup interface {
	FindPickup(ctx context.Context, cred map[string]string, req CourierPickupRequest) (ref string, found bool, err error)
}

// CourierCancelLookup is optional on a CourierAdapter: one whose API can say
// whether a shipment is already cancelled implements it.
type CourierCancelLookup interface {
	ShipmentCancelled(ctx context.Context, cred map[string]string, awb string) (bool, error)
}

type courierStep struct {
	name     string
	claimKey string
	stateKey string
	doneKey  string // set once the step is done at the courier
	done     string // the resolve outcome that records it
	notDone  string // the resolve outcome that releases the claim
	pastVerb string
}

var (
	courierPickupStep = courierStep{name: "pickup", claimKey: "pickup_claimed_at", stateKey: "pickup_state", doneKey: "pickup_reference", done: "scheduled", notDone: "not_scheduled", pastVerb: "scheduled this pickup"}
	courierCancelStep = courierStep{name: "cancel", claimKey: "cancel_claimed_at", stateKey: "cancel_state", doneKey: "courier_cancelled_at", done: "cancelled", notDone: "not_cancelled", pastVerb: "cancelled this shipment"}
)

func courierStepByName(name string) (courierStep, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case courierPickupStep.name:
		return courierPickupStep, true
	case courierCancelStep.name:
		return courierCancelStep, true
	}
	return courierStep{}, false
}

type courierStepClaim struct {
	schema      string
	data        map[string]interface{}
	lookupFirst bool
}

// claimCourierStep saves the attempt under the booking's row lock after
// precheck passes on the locked row. done is the step's recorded result when
// it was already done: no claim is taken and no call is needed.
func claimCourierStep(tenantID, provider, bookingID string, step courierStep, canLookup bool, precheck func(data map[string]interface{}, status string) error) (c courierStepClaim, done string, err error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return c, "", err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return c, "", err
	}
	defer tx.Rollback()
	data, status, err := lockLogisticsBooking(tx, schema, bookingID)
	if err != nil {
		return c, "", err
	}
	c = courierStepClaim{schema: schema, data: data}
	if done = courierDataString(data, step.doneKey); done != "" {
		return c, done, nil
	}
	if err := precheck(data, status); err != nil {
		return c, "", err
	}
	if claimedAt := courierDataString(data, step.claimKey); claimedAt != "" {
		if courierDataString(data, step.stateKey) != courierStateUncertain && !courierClaimIsStale(claimedAt) {
			return c, "", fmt.Errorf("booking %s: the %s is being sent to the courier right now; try again shortly", bookingID, step.name)
		}
		if !canLookup {
			return c, "", courierStepUnconfirmed(step, provider, bookingID, provider+" may already have "+step.pastVerb+".")
		}
		c.lookupFirst = true
	}
	patch := map[string]interface{}{step.claimKey: time.Now().UTC().Format(time.RFC3339Nano)}
	if c.lookupFirst {
		patch[step.stateKey] = courierStateChecking
	}
	encoded, _ := json.Marshal(patch)
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = (data - $3::text) || $1::jsonb WHERE id = $2 AND doctype = 'LogisticsBooking'`, schema), encoded, bookingID, step.stateKey); err != nil {
		return c, "", err
	}
	if err := tx.Commit(); err != nil {
		return c, "", err
	}
	return c, "", nil
}

func markCourierStepUncertain(schema, bookingID string, step courierStep) {
	_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || jsonb_build_object($1::text, $2::text), updated_at = CURRENT_TIMESTAMP
		WHERE id = $3 AND doctype = 'LogisticsBooking' AND NOT (data ? $4::text)`, schema), step.stateKey, courierStateUncertain, bookingID, step.doneKey)
}

func releaseCourierStepClaim(schema, bookingID string, step courierStep) {
	_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data - $1::text - $2::text WHERE id = $3 AND doctype = 'LogisticsBooking'`, schema), step.claimKey, step.stateKey, bookingID)
}

// recordCourierStepDone writes the step's result and drops the claim.
func recordCourierStepDone(schema, bookingID string, step courierStep, patch map[string]interface{}) error {
	encoded, _ := json.Marshal(patch)
	_, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = (data - $3::text - $4::text) || $1::jsonb, updated_at = CURRENT_TIMESTAMP WHERE id = $2 AND doctype = 'LogisticsBooking'`, schema), encoded, bookingID, step.claimKey, step.stateKey)
	return err
}

// settleFailedCourierStep keeps or releases the claim after a failed pickup
// or cancel call and words the answer, as settleFailedCourierBooking does.
func settleFailedCourierStep(tenantID string, c courierStepClaim, step courierStep, provider, bookingID string, canLookup, wrote bool, err error) error {
	if wrote && courierAnswerUnclear(err) {
		markCourierStepUncertain(c.schema, bookingID, step)
		LogAuditEvent(tenantID, "system", "COURIER_"+strings.ToUpper(step.name)+"_UNCERTAIN", "HELD", fmt.Sprintf("booking=%s provider=%s", bookingID, provider))
		what := courierUnclearAnswer(provider, err)
		if canLookup {
			return &ValidationError{Code: CourierUnreachableCode, Message: what + " - nothing was recorded here, but it may have received the request. Try again: the retry asks " + provider + " first."}
		}
		return courierStepUnconfirmed(step, provider, bookingID, what+" - it may have "+step.pastVerb+".")
	}
	releaseCourierStepClaim(c.schema, bookingID, step)
	return courierClearFailure(provider, wrote, err)
}

func courierStepUnconfirmed(step courierStep, provider, bookingID, why string) error {
	return &ValidationError{Code: CourierStepUnconfirmedCode, Message: fmt.Sprintf("%s Check booking %s on the %s portal, then resolve it here as %s or %s.", why, bookingID, provider, step.done, step.notDone)}
}

// ResolveUnconfirmedCourierStep settles an uncertain pickup or cancel from
// what the courier's portal shows: the step's done outcome records it
// (reference optional for a pickup), its not-done outcome releases the claim
// so the next attempt sends it. Audited.
func ResolveUnconfirmedCourierStep(tenantID, provider, bookingID, stepName, outcome, reference, actor string) error {
	if _, err := courierAdapter(provider); err != nil {
		return err
	}
	step, ok := courierStepByName(stepName)
	if !ok {
		return errors.New("step must be pickup or cancel")
	}
	outcome, reference = strings.ToLower(strings.TrimSpace(outcome)), strings.TrimSpace(reference)
	if outcome != step.done && outcome != step.notDone {
		return fmt.Errorf("outcome must be %s or %s", step.done, step.notDone)
	}
	if len(reference) > 64 || strings.ContainsAny(reference, " \t\r\n") {
		return errors.New("enter the reference exactly as the courier's portal shows it")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	data, _, err := lockLogisticsBooking(tx, schema, bookingID)
	if err != nil {
		return err
	}
	if done := courierDataString(data, step.doneKey); done != "" {
		return fmt.Errorf("booking %s: the %s is already recorded (%s)", bookingID, step.name, done)
	}
	claimedAt := courierDataString(data, step.claimKey)
	if claimedAt == "" {
		return fmt.Errorf("booking %s has no unconfirmed %s to resolve", bookingID, step.name)
	}
	if courierDataString(data, step.stateKey) != courierStateUncertain && !courierClaimIsStale(claimedAt) {
		return fmt.Errorf("booking %s: the %s is being sent to the courier right now; try again shortly", bookingID, step.name)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	patch := map[string]interface{}{step.name + "_resolution": outcome, step.name + "_resolved_by": actor, step.name + "_resolved_at": now}
	if outcome == step.done {
		switch step.name {
		case courierPickupStep.name:
			if reference == "" {
				reference = "portal"
			}
			patch["pickup_reference"], patch["pickup_scheduled_at"] = reference, now
		case courierCancelStep.name:
			patch["courier_cancelled_at"] = now
		}
	}
	encoded, _ := json.Marshal(patch)
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = (data - $3::text - $4::text) || $1::jsonb, updated_at = CURRENT_TIMESTAMP WHERE id = $2 AND doctype = 'LogisticsBooking'`, schema), encoded, bookingID, step.claimKey, step.stateKey); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	LogAuditEvent(tenantID, actor, "COURIER_"+strings.ToUpper(step.name)+"_RESOLVED", "SUCCESS", fmt.Sprintf("booking=%s provider=%s outcome=%s reference=%s", bookingID, provider, outcome, reference))
	return nil
}

// liveBookingForTask (47.11.8b) names the live courier booking a fulfillment
// task already has, or "" - a booking that is Cancelled, RTO or cancelled at
// the courier no longer counts, as for the parcel index of 47.11.8a.
func liveBookingForTask(tx *sql.Tx, schema, taskID string) (string, error) {
	var id string
	err := tx.QueryRow(fmt.Sprintf(`SELECT id FROM %s.documents
		WHERE doctype = 'LogisticsBooking' AND deleted_at IS NULL AND data->>'fulfillment_task_id' = $1
		  AND status NOT IN ('Cancelled', 'RTO') AND NOT (data ? 'courier_cancelled_at')
		ORDER BY created_at, id LIMIT 1`, schema), taskID).Scan(&id)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return id, err
}
