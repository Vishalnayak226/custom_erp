package server

import (
	"custom_erp/engines"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func handleCourierCredentials(w http.ResponseWriter, r *http.Request) {
	if !engines.HasFullAccess(r.Header.Get("Resolved-Role")) {
		writeAPIError(w, r, "GLOBAL-0011", "")
		return
	}
	provider := r.PathValue("provider")
	if r.Method == http.MethodGet {
		configured, err := engines.HasCourierCredential(r.Header.Get("Resolved-Tenant-ID"), provider)
		if err != nil {
			writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"provider": provider, "configured": configured})
		return
	}
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var fields map[string]string
	if err := json.NewDecoder(r.Body).Decode(&fields); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid credential payload")
		return
	}
	if err := engines.SaveCourierCredential(r.Header.Get("Resolved-Tenant-ID"), provider, fields); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "saved", "provider": provider})
}

func handleCourierAllocateAWB(w http.ResponseWriter, r *http.Request) {
	var req engines.CourierShipmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.BookingID == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "booking_id and a valid shipment payload are required")
		return
	}
	result, err := engines.AllocateCourierAWB(r.Context(), r.Header.Get("Resolved-Tenant-ID"), r.PathValue("provider"), req.BookingID, req)
	if err != nil {
		writeCourierError(w, r, err)
		return
	}
	_ = json.NewEncoder(w).Encode(result)
}

// writeCourierError (47.11.8): a courier that could not be reached or did not
// answer is reported as engines.CourierUnreachableCode - 503, retryable, with
// the engine's line on whether anything may have been sent. Every other
// failure keeps the 422 and message these handlers always gave. 47.11.8a: a
// booking the courier may already have, with no way to ask it, is
// engines.CourierUnconfirmedCode (409) with the reference to look up.
func writeCourierError(w http.ResponseWriter, r *http.Request, err error) {
	var verr *engines.ValidationError
	if errors.As(err, &verr) && (verr.Code == engines.CourierUnreachableCode || verr.Code == engines.CourierUnconfirmedCode) {
		writeAPIErrorDetail(w, r, verr.Code, "", verr.Message)
		return
	}
	writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
}

// handleCourierResolveBooking (47.11.8a) settles a booking the courier may or
// may not have made, from its portal: outcome "booked" with the AWB found
// there, or "not_booked" so the next attempt books. Booking's own route gates
// plus update on LogisticsBooking; audited by the engine.
func handleCourierResolveBooking(w http.ResponseWriter, r *http.Request) {
	var req struct {
		BookingID string `json:"booking_id"`
		Outcome   string `json:"outcome"`
		AWB       string `json:"awb"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.BookingID == "" || req.Outcome == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "booking_id and outcome are required")
		return
	}
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if ok, err := engines.RoleMayOnDoctype(tenantID, r.Header.Get("Resolved-Role"), "LogisticsBooking", "update"); err != nil || !ok {
		writeAPIError(w, r, "GLOBAL-0011", "")
		return
	}
	awb, err := engines.ResolveUnconfirmedCourierBooking(tenantID, r.PathValue("provider"), req.BookingID, req.Outcome, req.AWB, r.Header.Get("Resolved-User-ID"))
	if err != nil {
		writeCourierError(w, r, err)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "resolved", "booking_id": req.BookingID, "outcome": strings.ToLower(strings.TrimSpace(req.Outcome)), "awb": awb})
}

func handleCourierPickup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		BookingID  string `json:"booking_id"`
		PickupName string `json:"pickup_name"`
		PickupAt   string `json:"pickup_at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.BookingID == "" || req.PickupAt == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "booking_id and pickup_at are required")
		return
	}
	pickupAt, err := time.Parse(time.RFC3339, req.PickupAt)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "pickup_at must be RFC3339")
		return
	}
	ref, err := engines.ScheduleCourierPickup(r.Context(), r.Header.Get("Resolved-Tenant-ID"), r.PathValue("provider"), req.BookingID, req.PickupName, pickupAt)
	if err != nil {
		writeCourierError(w, r, err)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "scheduled", "pickup_reference": ref})
}

func handleCourierCancel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		BookingID string `json:"booking_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.BookingID == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "booking_id is required")
		return
	}
	if err := engines.CancelCourierShipment(r.Context(), r.Header.Get("Resolved-Tenant-ID"), r.PathValue("provider"), req.BookingID); err != nil {
		writeCourierError(w, r, err)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "cancelled", "booking_id": req.BookingID})
}

func handleCourierRates(w http.ResponseWriter, r *http.Request) {
	weight, err := strconv.Atoi(r.URL.Query().Get("weight_grams"))
	if err != nil || weight <= 0 {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "weight_grams must be positive")
		return
	}
	cod, _ := strconv.Atoi(r.URL.Query().Get("cod_amount"))
	providers := strings.Split(r.URL.Query().Get("providers"), ",")
	if len(providers) == 1 && providers[0] == "" {
		providers = []string{"delhivery", "shiprocket"}
	}
	rates, err := engines.ShopCourierRates(r.Context(), r.Header.Get("Resolved-Tenant-ID"), providers, engines.CourierRateRequest{OriginPincode: r.URL.Query().Get("origin_pincode"), DestinationPincode: r.URL.Query().Get("destination_pincode"), WeightGrams: weight, CODAmount: cod})
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if rates == nil {
		rates = []engines.CourierRate{}
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"rates": rates, "cheapest": func() interface{} {
		if len(rates) == 0 {
			return nil
		}
		return rates[0]
	}()})
}

func handleCourierTrackingWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, engines.InboundWebhookMaxBody))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Could not read webhook payload")
		return
	}
	tenantID, provider := r.Header.Get("Resolved-Tenant-ID"), r.PathValue("provider")
	if err := engines.VerifyCourierWebhook(tenantID, provider, r.Header.Get("X-Courier-Signature"), body); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnauthorized, "Invalid webhook signature") // 134.3: one answer, whatever failed
		return
	}
	source := "courier:" + provider
	if dup, err := engines.ClaimInboundWebhook(tenantID, source, "", body); err == nil && dup {
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "duplicate"})
		return
	}
	bookingID, err := engines.IngestCourierTrackingWebhook(tenantID, provider, body)
	if err != nil {
		engines.ReleaseInboundWebhook(tenantID, source, "", body)
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "processed", "booking_id": bookingID})
}

func handleNDRResolve(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Action      string `json:"action"`
		Note        string `json:"note"`
		ReattemptAt string `json:"reattempt_at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid NDR resolution payload")
		return
	}
	var at time.Time
	if req.ReattemptAt != "" {
		var err error
		at, err = time.Parse(time.RFC3339, req.ReattemptAt)
		if err != nil {
			writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "reattempt_at must be RFC3339")
			return
		}
	}
	if err := engines.ResolveNDR(r.Header.Get("Resolved-Tenant-ID"), r.PathValue("id"), req.Action, req.Note, r.Header.Get("Resolved-User-ID"), at); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "updated", "ndr_id": r.PathValue("id")})
}

func handleShippingLabelPDF(w http.ResponseWriter, r *http.Request) {
	bookingID := r.URL.Query().Get("booking_id")
	if bookingID == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "booking_id is required")
		return
	}
	pdf, err := engines.GenerateShippingLabelPDF(r.Header.Get("Resolved-Tenant-ID"), bookingID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=shipping-label-%s.pdf", bookingID))
	w.Header().Set("Content-Length", strconv.Itoa(len(pdf)))
	_, _ = w.Write(pdf)
}
