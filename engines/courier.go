package engines

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"custom_erp/db"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	neturl "net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

// CourierAdapter is the provider boundary for Stage 35.5. Provider-specific
// JSON and authentication stay behind this interface; shipment workflow code
// only deals in these stable request/response types.
type CourierAdapter interface {
	Code() string
	AllocateAWB(context.Context, map[string]string, CourierShipmentRequest) (CourierAWB, error)
	SchedulePickup(context.Context, map[string]string, CourierPickupRequest) (string, error)
	CancelShipment(context.Context, map[string]string, string) error
	Rates(context.Context, map[string]string, CourierRateRequest) ([]CourierRate, error)
	ParseTrackingWebhook([]byte) (CourierTrackingEvent, error)
}

type CourierShipmentRequest struct {
	BookingID          string `json:"booking_id"`
	OrderID            string `json:"order_id"`
	Reference          string `json:"reference,omitempty"` // 47.11.8a: set by AllocateCourierAWB, the same on every retry
	RemoteShipmentID   string `json:"remote_shipment_id,omitempty"`
	OriginPincode      string `json:"origin_pincode"`
	DestinationPincode string `json:"destination_pincode"`
	RecipientName      string `json:"recipient_name"`
	RecipientPhone     string `json:"recipient_phone"`
	RecipientAddress   string `json:"recipient_address"`
	WeightGrams        int    `json:"weight_grams"`
	CODAmount          int    `json:"cod_amount"`
}

type CourierAWB struct {
	AWB              string `json:"awb"`
	TrackingNumber   string `json:"tracking_number"`
	RemoteShipmentID string `json:"remote_shipment_id,omitempty"`
	ChargePaise      int    `json:"charge_paise"`
}

type CourierPickupRequest struct {
	AWBs              []string  `json:"awbs"`
	RemoteShipmentIDs []string  `json:"remote_shipment_ids,omitempty"`
	PickupName        string    `json:"pickup_name"`
	PickupAt          time.Time `json:"pickup_at"`
}

type CourierRateRequest struct {
	OriginPincode      string `json:"origin_pincode"`
	DestinationPincode string `json:"destination_pincode"`
	WeightGrams        int    `json:"weight_grams"`
	CODAmount          int    `json:"cod_amount"`
}

type CourierRate struct {
	Provider      string `json:"provider"`
	Service       string `json:"service"`
	ChargePaise   int    `json:"charge_paise"`
	EstimatedDays int    `json:"estimated_days"`
	Serviceable   bool   `json:"serviceable"`
}

type CourierTrackingEvent struct {
	AWB       string `json:"awb"`
	Status    string `json:"status"`
	Reason    string `json:"reason,omitempty"`
	EventID   string `json:"event_id,omitempty"`
	EventTime string `json:"event_time,omitempty"`
}

var (
	courierMu       sync.RWMutex
	courierAdapters = map[string]CourierAdapter{}
)

func registerCourierAdapter(adapter CourierAdapter) {
	courierMu.Lock()
	defer courierMu.Unlock()
	courierAdapters[strings.ToLower(adapter.Code())] = adapter
}

func courierAdapter(provider string) (CourierAdapter, error) {
	courierMu.RLock()
	defer courierMu.RUnlock()
	adapter, ok := courierAdapters[strings.ToLower(strings.TrimSpace(provider))]
	if !ok {
		return nil, fmt.Errorf("unsupported courier provider %q", provider)
	}
	return adapter, nil
}

func courierCredentialCode(provider string) string {
	return "courier:" + strings.ToLower(strings.TrimSpace(provider))
}

func courierDataString(data map[string]interface{}, key string) string {
	value, ok := data[key]
	if !ok || value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text)
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func SaveCourierCredential(tenantID, provider string, fields map[string]string) error {
	return SaveCourierCredentialBy(tenantID, provider, fields, "", nil)
}

// SaveCourierCredentialBy (49.8.5) records who saved the courier's
// credential and an optional expiry (SaveChannelCredentialBy).
func SaveCourierCredentialBy(tenantID, provider string, fields map[string]string, savedBy string, expiresAt *string) error {
	if _, err := courierAdapter(provider); err != nil {
		return err
	}
	if len(fields) == 0 {
		return errors.New("credential fields are required")
	}
	return SaveChannelCredentialBy(tenantID, courierCredentialCode(provider), fields, savedBy, expiresAt)
}

func HasCourierCredential(tenantID, provider string) (bool, error) {
	if _, err := courierAdapter(provider); err != nil {
		return false, err
	}
	return HasChannelCredential(tenantID, courierCredentialCode(provider))
}

func courierCredentials(tenantID, provider string) (map[string]string, error) {
	return getChannelCredential(tenantID, courierCredentialCode(provider))
}

// AllocateCourierAWB replaces the internal placeholder AWB only after the
// provider has accepted the shipment. 47.11.8a: nothing is booked twice. The
// attempt - a claim under the row lock (two calls at once cannot both reach
// the courier) and the booking's reference, sent as the courier's order id -
// is saved before the call; a call that may have reached the courier keeps
// the claim as uncertain, and the next attempt asks the courier by that
// reference before booking (courier_booking_guard.go).
func AllocateCourierAWB(ctx context.Context, tenantID, provider, bookingID string, req CourierShipmentRequest) (CourierAWB, error) {
	adapter, err := courierAdapter(provider)
	if err != nil {
		return CourierAWB{}, err
	}
	cred, err := courierCredentials(tenantID, provider)
	if err != nil {
		return CourierAWB{}, fmt.Errorf("%s credentials pending: %w", provider, err)
	}
	lookup, canLookup := adapter.(CourierBookingLookup)
	claim, existing, err := claimCourierBooking(tenantID, provider, bookingID, req.RemoteShipmentID, canLookup)
	if err != nil || existing.AWB != "" {
		return existing, err
	}
	data := claim.data
	req.BookingID, req.Reference = bookingID, claim.ref
	if req.OrderID == "" {
		req.OrderID = courierDataString(data, "order_id")
	}
	if req.DestinationPincode == "" {
		req.DestinationPincode = courierDataString(data, "destination_pincode")
	}
	if stored := courierDataString(data, "remote_shipment_id"); stored != "" {
		req.RemoteShipmentID = stored // the attempt's own, on every retry
	}
	if ctx.Err() != nil {
		claim.undo(bookingID)
		return CourierAWB{}, ctx.Err()
	}
	if claim.lookupFirst {
		found, ok, lerr := lookup.FindBooking(ctx, cred, req)
		if lerr != nil || (ok && strings.TrimSpace(found.AWB) == "") {
			markCourierUncertain(claim.schema, bookingID)
			return CourierAWB{}, courierLookupFailed(provider, claim.ref, lerr)
		}
		if ok {
			if found.RemoteShipmentID == "" {
				found.RemoteShipmentID = req.RemoteShipmentID
			}
			return recordCourierAWB(tenantID, claim.schema, provider, bookingID, found, "COURIER_AWB_ADOPTED", "system", nil)
		}
		// The courier confirms it has none: book it.
	}
	traced, wrote := courierTracedContext(ctx)
	awb, err := adapter.AllocateAWB(traced, cred, req)
	if err == nil && strings.TrimSpace(awb.AWB) == "" {
		err = fmt.Errorf("%s answered without an AWB: %w", provider, errCourierNoAWB)
	}
	if err != nil {
		return CourierAWB{}, settleFailedCourierBooking(tenantID, claim, provider, bookingID, canLookup, wrote.Load(), err)
	}
	return recordCourierAWB(tenantID, claim.schema, provider, bookingID, awb, "COURIER_AWB_ALLOCATED", "system", nil)
}

func ScheduleCourierPickup(ctx context.Context, tenantID, provider, bookingID, pickupName string, pickupAt time.Time) (string, error) {
	adapter, err := courierAdapter(provider)
	if err != nil {
		return "", err
	}
	cred, err := courierCredentials(tenantID, provider)
	if err != nil {
		return "", err
	}
	// 47.11.8b: claimed before the call, uncertain kept, retry asks first or
	// waits for Resolve (courier_step_guard.go). A pickup already scheduled
	// is answered from the record.
	lookup, canLookup := adapter.(CourierPickupLookup)
	claim, done, err := claimCourierStep(tenantID, provider, bookingID, courierPickupStep, canLookup, func(data map[string]interface{}, status string) error {
		if status == "Delivered" || status == "RTO" {
			return fmt.Errorf("booking %s is already %s", bookingID, status)
		}
		if courierDataString(data, "provider_awb") == "" {
			return fmt.Errorf("booking %s has no provider AWB", bookingID)
		}
		return nil
	})
	if err != nil || done != "" {
		return done, err
	}
	req := CourierPickupRequest{AWBs: []string{courierDataString(claim.data, "provider_awb")}, RemoteShipmentIDs: []string{courierDataString(claim.data, "remote_shipment_id")}, PickupName: pickupName, PickupAt: pickupAt}
	if ctx.Err() != nil {
		releaseCourierStepClaim(claim.schema, bookingID, courierPickupStep)
		return "", ctx.Err()
	}
	scheduled := map[string]interface{}{"pickup_scheduled_at": pickupAt.UTC().Format(time.RFC3339)}
	if claim.lookupFirst {
		ref, found, lerr := lookup.FindPickup(ctx, cred, req)
		if lerr != nil {
			markCourierStepUncertain(claim.schema, bookingID, courierPickupStep)
			return "", &ValidationError{Code: CourierUnreachableCode, Message: fmt.Sprintf("%s could not be asked about this pickup - nothing was sent. Try again shortly.", provider)}
		}
		if found {
			scheduled["pickup_reference"] = ref
			LogAuditEvent(tenantID, "system", "COURIER_PICKUP_ADOPTED", "SUCCESS", fmt.Sprintf("booking=%s provider=%s reference=%s", bookingID, provider, ref))
			return ref, recordCourierStepDone(claim.schema, bookingID, courierPickupStep, scheduled)
		}
	}
	traced, wrote := courierTracedContext(ctx)
	ref, err := adapter.SchedulePickup(traced, cred, req)
	if err != nil {
		return "", settleFailedCourierStep(tenantID, claim, courierPickupStep, provider, bookingID, canLookup, wrote.Load(), err)
	}
	scheduled["pickup_reference"] = ref
	return ref, recordCourierStepDone(claim.schema, bookingID, courierPickupStep, scheduled)
}

func CancelCourierShipment(ctx context.Context, tenantID, provider, bookingID string) error {
	adapter, err := courierAdapter(provider)
	if err != nil {
		return err
	}
	cred, err := courierCredentials(tenantID, provider)
	if err != nil {
		return err
	}
	// 47.11.8b: the same guard as pickup; a shipment already cancelled at
	// the courier is not cancelled again.
	lookup, canLookup := adapter.(CourierCancelLookup)
	claim, done, err := claimCourierStep(tenantID, provider, bookingID, courierCancelStep, canLookup, func(data map[string]interface{}, status string) error {
		if status != "AWB Assigned" {
			return fmt.Errorf("booking %s cannot be cancelled at courier from status %q", bookingID, status)
		}
		if courierDataString(data, "provider_awb") == "" {
			return fmt.Errorf("booking %s has no provider AWB", bookingID)
		}
		return nil
	})
	if err != nil || done != "" {
		return err
	}
	awb := courierDataString(claim.data, "provider_awb")
	if ctx.Err() != nil {
		releaseCourierStepClaim(claim.schema, bookingID, courierCancelStep)
		return ctx.Err()
	}
	cancelled := map[string]interface{}{"courier_cancelled_at": time.Now().UTC().Format(time.RFC3339)}
	if claim.lookupFirst {
		isCancelled, lerr := lookup.ShipmentCancelled(ctx, cred, awb)
		if lerr != nil {
			markCourierStepUncertain(claim.schema, bookingID, courierCancelStep)
			return &ValidationError{Code: CourierUnreachableCode, Message: fmt.Sprintf("%s could not be asked about this cancellation - nothing was sent. Try again shortly.", provider)}
		}
		if isCancelled {
			LogAuditEvent(tenantID, "system", "COURIER_CANCEL_ADOPTED", "SUCCESS", fmt.Sprintf("booking=%s provider=%s awb=%s", bookingID, provider, awb))
			return recordCourierStepDone(claim.schema, bookingID, courierCancelStep, cancelled)
		}
	}
	traced, wrote := courierTracedContext(ctx)
	if err := adapter.CancelShipment(traced, cred, awb); err != nil {
		return settleFailedCourierStep(tenantID, claim, courierCancelStep, provider, bookingID, canLookup, wrote.Load(), err)
	}
	return recordCourierStepDone(claim.schema, bookingID, courierCancelStep, cancelled)
}

// CourierUnreachableCode is the code a courier call that never got a usable
// answer is reported with: INTEGR-0167, 503 and retryable (47.11.8).
const CourierUnreachableCode = "INTEGR-0167"

// courierCallFailed (47.11.8) turns a courier that could not be reached, did
// not answer, or answered with its own server error into CourierUnreachableCode
// with the one line the operator needs: whether anything may have been sent,
// and what to do next. The raw transport error (an address, a socket message)
// is not shown. Every other failure - the courier refusing the shipment, a
// breaker or switched-off refusal, the caller leaving - is returned as it was.
func courierCallFailed(provider string, err error) error {
	if err == nil || errors.Is(err, context.Canceled) {
		return err
	}
	var httpErr *ConnectorHTTPError
	if errors.As(err, &httpErr) && httpErr.Status >= 500 {
		return &ValidationError{Code: CourierUnreachableCode, Message: fmt.Sprintf("%s is not working right now (HTTP %d) - nothing was recorded here. Try again shortly.", provider, httpErr.Status)}
	}
	var urlErr *neturl.Error
	if !errors.As(err, &urlErr) {
		return err
	}
	if _, notSent := crmClassifyHTTPError(urlErr).(*errCRMNotSent); notSent {
		return &ValidationError{Code: CourierUnreachableCode, Message: fmt.Sprintf("%s could not be reached - nothing was sent and nothing was changed here. Try again shortly.", provider)}
	}
	return &ValidationError{Code: CourierUnreachableCode, Message: fmt.Sprintf("%s did not answer - nothing was recorded here, but it may have received the request. Check %s for this booking before trying again.", provider, provider)}
}

// ShopCourierRates calls every requested configured adapter and returns only
// serviceable services ordered by landed charge, then ETA. Provider failures
// are isolated so one outage does not hide another courier's usable quote.
func ShopCourierRates(ctx context.Context, tenantID string, providers []string, req CourierRateRequest) ([]CourierRate, error) {
	var rates []CourierRate
	var failures []string
	for _, provider := range providers {
		adapter, err := courierAdapter(provider)
		if err != nil {
			failures = append(failures, err.Error())
			continue
		}
		cred, err := courierCredentials(tenantID, provider)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: credentials pending", provider))
			continue
		}
		quoted, err := adapter.Rates(ctx, cred, req)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", provider, err))
			continue
		}
		for i := range quoted {
			if quoted[i].Provider == "" {
				quoted[i].Provider = adapter.Code()
			}
			if quoted[i].Serviceable {
				rates = append(rates, quoted[i])
			}
		}
	}
	sort.Slice(rates, func(i, j int) bool {
		if rates[i].ChargePaise == rates[j].ChargePaise {
			return rates[i].EstimatedDays < rates[j].EstimatedDays
		}
		return rates[i].ChargePaise < rates[j].ChargePaise
	})
	if len(rates) == 0 && len(failures) > 0 {
		return nil, errors.New(strings.Join(failures, "; "))
	}
	return rates, nil
}

func VerifyCourierWebhook(tenantID, provider, signature string, body []byte) error {
	cred, err := courierCredentials(tenantID, provider)
	if err != nil {
		return err
	}
	secret := cred["webhook_secret"]
	if secret == "" {
		return fmt.Errorf("%s webhook secret is not configured", provider)
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	expected, err := hex.DecodeString(strings.TrimSpace(signature))
	if err != nil || !hmac.Equal(mac.Sum(nil), expected) {
		return errors.New("invalid courier webhook signature")
	}
	return nil
}

// IngestCourierTrackingWebhook is idempotent by provider event id. It maps
// courier vocabulary into the existing shipment state machine and diverts
// delivery failures into an actionable NDR case rather than prematurely RTOing.
func IngestCourierTrackingWebhook(tenantID, provider string, body []byte) (string, error) {
	adapter, err := courierAdapter(provider)
	if err != nil {
		return "", err
	}
	event, err := adapter.ParseTrackingWebhook(body)
	if err != nil {
		return "", err
	}
	if event.AWB == "" {
		return "", errors.New("tracking webhook has no AWB")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	var bookingID string
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT id FROM %s.documents WHERE doctype='LogisticsBooking' AND deleted_at IS NULL AND (data->>'provider_awb'=$1 OR data->>'awb_number'=$1) ORDER BY created_at DESC LIMIT 1`, schema), event.AWB).Scan(&bookingID)
	if err != nil {
		return "", fmt.Errorf("no booking found for AWB %s", event.AWB)
	}
	if event.EventID != "" {
		var seen bool
		_ = db.DB.QueryRow(fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM %s.documents WHERE doctype='CourierTrackingEvent' AND data->>'provider'=$1 AND data->>'event_id'=$2)`, schema), provider, event.EventID).Scan(&seen)
		if seen {
			return bookingID, nil
		}
	}
	normalized := normalizeCourierStatus(event.Status)
	switch normalized {
	case "In-Transit":
		_, _, current, _ := fetchLogisticsBooking(tenantID, bookingID)
		if current != "In-Transit" {
			err = RecordDeliveryEvent(tenantID, bookingID, normalized, "courier:"+provider)
		}
	case "Delivered":
		err = RecordDeliveryEvent(tenantID, bookingID, normalized, "courier:"+provider)
	case "NDR":
		_, err = RecordNDR(tenantID, bookingID, event.Reason, "courier:"+provider)
	case "RTO":
		if strings.TrimSpace(event.Reason) == "" {
			event.Reason = "Courier reported return to origin"
		}
		err = RecordRTO(tenantID, bookingID, event.Reason, "courier:"+provider)
	default:
		return "", fmt.Errorf("unsupported courier tracking status %q", event.Status)
	}
	if err != nil {
		return "", err
	}
	eventDoc, _ := json.Marshal(map[string]interface{}{"provider": provider, "event_id": event.EventID, "awb": event.AWB, "status": event.Status, "normalized_status": normalized, "booking_id": bookingID, "event_time": event.EventTime})
	_, err = db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id,doctype,data,status,created_by) VALUES ($1,'CourierTrackingEvent',$2,'Processed','system')`, schema), NewDocID("CTE"), eventDoc)
	return bookingID, err
}

func normalizeCourierStatus(status string) string {
	s := strings.ToLower(strings.TrimSpace(status))
	switch {
	case strings.Contains(s, "rto"), strings.Contains(s, "return to origin"):
		return "RTO"
	case strings.Contains(s, "deliver") && !strings.Contains(s, "undeliver"):
		return "Delivered"
	case strings.Contains(s, "ndr"), strings.Contains(s, "undeliver"), strings.Contains(s, "failed attempt"), strings.Contains(s, "consignee unavailable"):
		return "NDR"
	case strings.Contains(s, "transit"), strings.Contains(s, "picked"), strings.Contains(s, "dispatch"), strings.Contains(s, "manifest"):
		return "In-Transit"
	}
	return ""
}

func RecordNDR(tenantID, bookingID, reason, actor string) (string, error) {
	if strings.TrimSpace(reason) == "" {
		reason = "Delivery attempt failed"
	}
	schema, _, status, err := fetchLogisticsBooking(tenantID, bookingID)
	if err != nil {
		return "", err
	}
	if status == "Delivered" || status == "RTO" {
		return "", fmt.Errorf("booking %s cannot enter NDR from %s", bookingID, status)
	}
	var existing string
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT id FROM %s.documents WHERE doctype='NDRCase' AND data->>'booking_id'=$1 AND status IN ('Open','Reattempt Scheduled') AND deleted_at IS NULL ORDER BY created_at DESC LIMIT 1`, schema), bookingID).Scan(&existing)
	if err == nil {
		_, err = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data=data || jsonb_build_object('reason',$1,'last_reported_at',$2), updated_at=CURRENT_TIMESTAMP WHERE id=$3`, schema), reason, time.Now().UTC().Format(time.RFC3339), existing)
		return existing, err
	}
	id := NewDocID("NDR")
	data, _ := json.Marshal(map[string]interface{}{"code": id, "booking_id": bookingID, "reason": reason, "attempt_count": 1, "status": "Open", "reported_by": actor, "last_reported_at": time.Now().UTC().Format(time.RFC3339)})
	_, err = db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id,doctype,data,status,created_by) VALUES ($1,'NDRCase',$2,'Open','system')`, schema), id, data)
	return id, err
}

func ResolveNDR(tenantID, ndrID, action, note, actor string, reattemptAt time.Time) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var bookingID, status string
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT data->>'booking_id', status FROM %s.documents WHERE id=$1 AND doctype='NDRCase' AND deleted_at IS NULL`, schema), ndrID).Scan(&bookingID, &status)
	if err != nil {
		return fmt.Errorf("NDR case %s not found", ndrID)
	}
	if status == "Closed" || status == "RTO" {
		return fmt.Errorf("NDR case %s is already %s", ndrID, status)
	}
	var newStatus string
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "reattempt":
		if reattemptAt.IsZero() {
			return errors.New("reattempt_at is required for reattempt")
		}
		newStatus = "Reattempt Scheduled"
	case "rto":
		if strings.TrimSpace(note) == "" {
			note = "NDR marked return to origin"
		}
		if err := RecordRTO(tenantID, bookingID, note, actor); err != nil {
			return err
		}
		newStatus = "RTO"
	case "close":
		newStatus = "Closed"
	default:
		return errors.New("action must be reattempt, rto, or close")
	}
	_, err = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET status=$1::text, data=data || jsonb_build_object('status',$1::text,'resolution_note',$2::text,'reattempt_at',$3::text,'resolved_by',$4::text), updated_at=CURRENT_TIMESTAMP WHERE id=$5 AND doctype='NDRCase'`, schema), newStatus, note, func() interface{} {
		if reattemptAt.IsZero() {
			return nil
		}
		return reattemptAt.UTC().Format(time.RFC3339)
	}(), actor, ndrID)
	return err
}
