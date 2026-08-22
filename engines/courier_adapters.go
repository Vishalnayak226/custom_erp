package engines

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var (
	delhiveryBaseURL  = "https://track.delhivery.com"
	shiprocketBaseURL = "https://apiv2.shiprocket.in/v1/external"
)

func init() {
	registerCourierAdapter(delhiveryAdapter{})
	registerCourierAdapter(shiprocketAdapter{})
}

func courierJSONCall(ctx context.Context, provider, method, endpoint string, headers map[string]string, request interface{}, response interface{}) error {
	var body []byte
	var err error
	if request != nil {
		body, err = json.Marshal(request)
		if err != nil {
			return err
		}
	}
	if headers == nil {
		headers = map[string]string{}
	}
	if _, ok := headers["Content-Type"]; !ok {
		headers["Content-Type"] = "application/json"
	}
	status, responseBody, err := doConnectorRequest(ctx, 25*time.Second, method, endpoint, headers, body, "courier:"+provider)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("%s returned HTTP %d: %s", provider, status, strings.TrimSpace(string(responseBody)))
	}
	if response != nil && len(responseBody) > 0 {
		if err := json.Unmarshal(responseBody, response); err != nil {
			return fmt.Errorf("invalid %s response: %w", provider, err)
		}
	}
	return nil
}

type delhiveryAdapter struct{}

func (delhiveryAdapter) Code() string { return "delhivery" }

func delhiveryHeaders(cred map[string]string) (map[string]string, error) {
	token := strings.TrimSpace(cred["api_token"])
	if token == "" {
		return nil, fmt.Errorf("delhivery credential missing api_token")
	}
	return map[string]string{"Authorization": "Token " + token, "Accept": "application/json"}, nil
}

func (delhiveryAdapter) AllocateAWB(ctx context.Context, cred map[string]string, req CourierShipmentRequest) (CourierAWB, error) {
	headers, err := delhiveryHeaders(cred)
	if err != nil {
		return CourierAWB{}, err
	}
	pickupName := cred["pickup_name"]
	if pickupName == "" {
		return CourierAWB{}, fmt.Errorf("delhivery credential missing pickup_name")
	}
	payload := map[string]interface{}{"shipments": []map[string]interface{}{{
		"name": req.RecipientName, "add": req.RecipientAddress, "pin": req.DestinationPincode,
		"phone": req.RecipientPhone, "order": req.OrderID, "payment_mode": func() string {
			if req.CODAmount > 0 {
				return "COD"
			}
			return "Prepaid"
		}(),
		"cod_amount": req.CODAmount, "weight": req.WeightGrams,
	}}, "pickup_location": map[string]string{"name": pickupName}}
	encoded, _ := json.Marshal(payload)
	form := url.Values{"format": {"json"}, "data": {string(encoded)}}.Encode()
	headers["Content-Type"] = "application/x-www-form-urlencoded"
	status, body, err := doConnectorRequest(ctx, 25*time.Second, http.MethodPost, strings.TrimRight(delhiveryBaseURL, "/")+"/api/cmu/create.json", headers, []byte(form), "courier:delhivery")
	if err != nil {
		return CourierAWB{}, err
	}
	if status < 200 || status >= 300 {
		return CourierAWB{}, fmt.Errorf("delhivery returned HTTP %d: %s", status, body)
	}
	var response struct {
		Success  bool `json:"success"`
		Packages []struct {
			Waybill string   `json:"waybill"`
			Status  string   `json:"status"`
			Remarks []string `json:"remarks"`
		} `json:"packages"`
		RMK string `json:"rmk"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return CourierAWB{}, err
	}
	if !response.Success || len(response.Packages) == 0 || response.Packages[0].Waybill == "" {
		return CourierAWB{}, fmt.Errorf("delhivery rejected shipment: %s", response.RMK)
	}
	return CourierAWB{AWB: response.Packages[0].Waybill, TrackingNumber: response.Packages[0].Waybill}, nil
}

func (delhiveryAdapter) SchedulePickup(ctx context.Context, cred map[string]string, req CourierPickupRequest) (string, error) {
	headers, err := delhiveryHeaders(cred)
	if err != nil {
		return "", err
	}
	pickupName := req.PickupName
	if pickupName == "" {
		pickupName = cred["pickup_name"]
	}
	payload := map[string]interface{}{"pickup_time": req.PickupAt.Format("15:04:05"), "pickup_date": req.PickupAt.Format("2006-01-02"), "pickup_location": pickupName, "expected_package_count": len(req.AWBs)}
	var response map[string]interface{}
	if err := courierJSONCall(ctx, "delhivery", http.MethodPost, strings.TrimRight(delhiveryBaseURL, "/")+"/fm/request/new/", headers, payload, &response); err != nil {
		return "", err
	}
	ref := strings.TrimSpace(fmt.Sprint(response["pickup_id"]))
	if ref == "" {
		ref = strings.TrimSpace(fmt.Sprint(response["pr_exist"]))
	}
	if ref == "" {
		return "", fmt.Errorf("delhivery pickup response has no reference")
	}
	return ref, nil
}

func (delhiveryAdapter) CancelShipment(ctx context.Context, cred map[string]string, awb string) error {
	headers, err := delhiveryHeaders(cred)
	if err != nil {
		return err
	}
	return courierJSONCall(ctx, "delhivery", http.MethodPost, strings.TrimRight(delhiveryBaseURL, "/")+"/api/p/edit", headers, map[string]string{"waybill": awb, "cancellation": "true"}, nil)
}

func (delhiveryAdapter) Rates(ctx context.Context, cred map[string]string, req CourierRateRequest) ([]CourierRate, error) {
	headers, err := delhiveryHeaders(cred)
	if err != nil {
		return nil, err
	}
	q := url.Values{"md": {"S"}, "o_pin": {req.OriginPincode}, "d_pin": {req.DestinationPincode}, "cgm": {strconv.Itoa(req.WeightGrams)}, "ss": {"Delivered"}}
	if req.CODAmount > 0 {
		q.Set("pt", "COD")
		q.Set("cod", strconv.Itoa(req.CODAmount))
	} else {
		q.Set("pt", "Pre-paid")
	}
	status, body, err := doConnectorRequest(ctx, 20*time.Second, http.MethodGet, strings.TrimRight(delhiveryBaseURL, "/")+"/api/kinko/v1/invoice/charges/.json?"+q.Encode(), headers, nil, "courier:delhivery")
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("delhivery returned HTTP %d: %s", status, body)
	}
	var response []map[string]interface{}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, err
	}
	var rates []CourierRate
	for _, row := range response {
		charge, _ := strconv.ParseFloat(fmt.Sprint(row["total_amount"]), 64)
		if charge == 0 {
			charge, _ = strconv.ParseFloat(fmt.Sprint(row["gross_amount"]), 64)
		}
		rates = append(rates, CourierRate{Provider: "delhivery", Service: fmt.Sprint(row["service_type"]), ChargePaise: int(charge*100 + 0.5), Serviceable: charge > 0})
	}
	return rates, nil
}

func (delhiveryAdapter) ParseTrackingWebhook(body []byte) (CourierTrackingEvent, error) {
	var envelope struct {
		Shipment struct {
			AWB    string `json:"AWB"`
			Status struct {
				Status         string `json:"Status"`
				StatusType     string `json:"StatusType"`
				Instructions   string `json:"Instructions"`
				StatusDateTime string `json:"StatusDateTime"`
			} `json:"Status"`
		} `json:"Shipment"`
		Waybill string `json:"waybill"`
		Status  string `json:"status"`
		Reason  string `json:"reason"`
		EventID string `json:"event_id"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return CourierTrackingEvent{}, err
	}
	e := CourierTrackingEvent{AWB: envelope.Shipment.AWB, Status: envelope.Shipment.Status.Status, Reason: envelope.Shipment.Status.Instructions, EventTime: envelope.Shipment.Status.StatusDateTime, EventID: envelope.EventID}
	if e.AWB == "" {
		e.AWB = envelope.Waybill
	}
	if e.Status == "" {
		e.Status = envelope.Status
	}
	if e.Reason == "" {
		e.Reason = envelope.Reason
	}
	if e.EventID == "" {
		e.EventID = e.AWB + ":" + e.Status + ":" + e.EventTime
	}
	return e, nil
}

type shiprocketAdapter struct{}

func (shiprocketAdapter) Code() string { return "shiprocket" }

func shiprocketToken(ctx context.Context, cred map[string]string) (string, error) {
	if token := strings.TrimSpace(cred["access_token"]); token != "" {
		return token, nil
	}
	if cred["email"] == "" || cred["password"] == "" {
		return "", fmt.Errorf("shiprocket credential needs access_token or email/password")
	}
	var response struct {
		Token string `json:"token"`
	}
	if err := courierJSONCall(ctx, "shiprocket", http.MethodPost, strings.TrimRight(shiprocketBaseURL, "/")+"/auth/login", nil, map[string]string{"email": cred["email"], "password": cred["password"]}, &response); err != nil {
		return "", err
	}
	if response.Token == "" {
		return "", fmt.Errorf("shiprocket login returned no token")
	}
	return response.Token, nil
}
func shiprocketHeaders(token string) map[string]string {
	return map[string]string{"Authorization": "Bearer " + token, "Accept": "application/json"}
}

func (shiprocketAdapter) AllocateAWB(ctx context.Context, cred map[string]string, req CourierShipmentRequest) (CourierAWB, error) {
	if req.RemoteShipmentID == "" {
		return CourierAWB{}, fmt.Errorf("shiprocket AWB allocation requires remote_shipment_id from its order-creation response")
	}
	token, err := shiprocketToken(ctx, cred)
	if err != nil {
		return CourierAWB{}, err
	}
	payload := map[string]interface{}{"shipment_id": req.RemoteShipmentID}
	if id := cred["courier_id"]; id != "" {
		payload["courier_id"], _ = strconv.Atoi(id)
	}
	var response struct {
		AWBAssignStatus int `json:"awb_assign_status"`
		Response        struct {
			Data struct {
				AWBCode          string `json:"awb_code"`
				ShipmentID       int    `json:"shipment_id"`
				CourierCompanyID int    `json:"courier_company_id"`
			} `json:"data"`
		} `json:"response"`
	}
	if err := courierJSONCall(ctx, "shiprocket", http.MethodPost, strings.TrimRight(shiprocketBaseURL, "/")+"/courier/assign/awb", shiprocketHeaders(token), payload, &response); err != nil {
		return CourierAWB{}, err
	}
	if response.Response.Data.AWBCode == "" {
		return CourierAWB{}, fmt.Errorf("shiprocket rejected AWB allocation")
	}
	return CourierAWB{AWB: response.Response.Data.AWBCode, TrackingNumber: response.Response.Data.AWBCode, RemoteShipmentID: req.RemoteShipmentID}, nil
}

func (shiprocketAdapter) SchedulePickup(ctx context.Context, cred map[string]string, req CourierPickupRequest) (string, error) {
	token, err := shiprocketToken(ctx, cred)
	if err != nil {
		return "", err
	}
	var shipmentIDs []int
	for _, raw := range req.RemoteShipmentIDs {
		if id, convErr := strconv.Atoi(raw); convErr == nil && id > 0 {
			shipmentIDs = append(shipmentIDs, id)
		}
	}
	if len(shipmentIDs) == 0 {
		return "", fmt.Errorf("shiprocket pickup requires a numeric remote_shipment_id")
	}
	var response struct {
		PickupStatus int    `json:"pickup_status"`
		Response     string `json:"response"`
	}
	if err := courierJSONCall(ctx, "shiprocket", http.MethodPost, strings.TrimRight(shiprocketBaseURL, "/")+"/courier/generate/pickup", shiprocketHeaders(token), map[string]interface{}{"shipment_id": shipmentIDs}, &response); err != nil {
		return "", err
	}
	if response.PickupStatus == 0 {
		return "", fmt.Errorf("shiprocket rejected pickup: %s", response.Response)
	}
	return response.Response, nil
}

func (shiprocketAdapter) CancelShipment(ctx context.Context, cred map[string]string, awb string) error {
	token, err := shiprocketToken(ctx, cred)
	if err != nil {
		return err
	}
	return courierJSONCall(ctx, "shiprocket", http.MethodPost, strings.TrimRight(shiprocketBaseURL, "/")+"/orders/cancel/shipment/awbs", shiprocketHeaders(token), map[string]interface{}{"awbs": []string{awb}}, nil)
}

func (shiprocketAdapter) Rates(ctx context.Context, cred map[string]string, req CourierRateRequest) ([]CourierRate, error) {
	token, err := shiprocketToken(ctx, cred)
	if err != nil {
		return nil, err
	}
	q := url.Values{"pickup_postcode": {req.OriginPincode}, "delivery_postcode": {req.DestinationPincode}, "weight": {strconv.FormatFloat(float64(req.WeightGrams)/1000, 'f', 3, 64)}, "cod": {func() string {
		if req.CODAmount > 0 {
			return "1"
		}
		return "0"
	}()}}
	status, body, err := doConnectorRequest(ctx, 20*time.Second, http.MethodGet, strings.TrimRight(shiprocketBaseURL, "/")+"/courier/serviceability/?"+q.Encode(), shiprocketHeaders(token), nil, "courier:shiprocket")
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("shiprocket returned HTTP %d: %s", status, body)
	}
	var response struct {
		Data struct {
			AvailableCourierCompanies []struct {
				Name                  string  `json:"courier_name"`
				Rate                  float64 `json:"rate"`
				ETD                   string  `json:"etd"`
				EstimatedDeliveryDays string  `json:"estimated_delivery_days"`
			} `json:"available_courier_companies"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, err
	}
	var rates []CourierRate
	for _, row := range response.Data.AvailableCourierCompanies {
		days, _ := strconv.Atoi(row.EstimatedDeliveryDays)
		rates = append(rates, CourierRate{Provider: "shiprocket", Service: row.Name, ChargePaise: int(row.Rate*100 + .5), EstimatedDays: days, Serviceable: true})
	}
	return rates, nil
}

func (shiprocketAdapter) ParseTrackingWebhook(body []byte) (CourierTrackingEvent, error) {
	var payload struct {
		AWB             string `json:"awb"`
		CurrentStatus   string `json:"current_status"`
		CurrentStatusID int    `json:"current_status_id"`
		ShipmentStatus  string `json:"shipment_status"`
		Scans           []struct {
			Date     string `json:"date"`
			Activity string `json:"activity"`
			Status   string `json:"status"`
		} `json:"scans"`
		SRShipmentID interface{} `json:"sr_shipment_id"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return CourierTrackingEvent{}, err
	}
	e := CourierTrackingEvent{AWB: payload.AWB, Status: payload.CurrentStatus, EventID: fmt.Sprintf("%v:%d:%s", payload.SRShipmentID, payload.CurrentStatusID, payload.CurrentStatus)}
	if e.Status == "" {
		e.Status = payload.ShipmentStatus
	}
	if len(payload.Scans) > 0 {
		last := payload.Scans[len(payload.Scans)-1]
		e.EventTime = last.Date
		if e.Status == "" {
			e.Status = last.Status
		}
		e.Reason = last.Activity
	}
	return e, nil
}
