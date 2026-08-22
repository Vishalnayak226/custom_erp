package engines

import (
	"context"
	"custom_erp/db"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

type stage35CourierStub struct {
	rates []CourierRate
}

func (stage35CourierStub) Code() string { return "stage35stub" }
func (stage35CourierStub) AllocateAWB(_ context.Context, _ map[string]string, req CourierShipmentRequest) (CourierAWB, error) {
	return CourierAWB{AWB: "REAL-AWB-355", TrackingNumber: "REAL-AWB-355", RemoteShipmentID: req.RemoteShipmentID, ChargePaise: 12345}, nil
}
func (stage35CourierStub) SchedulePickup(context.Context, map[string]string, CourierPickupRequest) (string, error) {
	return "PICKUP-355", nil
}
func (stage35CourierStub) CancelShipment(context.Context, map[string]string, string) error {
	return nil
}
func (s stage35CourierStub) Rates(context.Context, map[string]string, CourierRateRequest) ([]CourierRate, error) {
	return s.rates, nil
}
func (stage35CourierStub) ParseTrackingWebhook(body []byte) (CourierTrackingEvent, error) {
	var event CourierTrackingEvent
	err := json.Unmarshal(body, &event)
	return event, err
}

func TestShippingLabelPDFHasVectorCode128(t *testing.T) {
	commands := code128PDFCommands("AWB355123", 24, 100, 200, 80)
	if !strings.Contains(commands, " re f") {
		t.Fatal("Code128 renderer emitted no vector bars")
	}
	pdf := buildSinglePagePDF(288, 432, commands)
	if !strings.HasPrefix(string(pdf), "%PDF-1.4") || !strings.Contains(string(pdf), "startxref") {
		t.Fatal("label is not a complete PDF")
	}
}

func TestCourierStatusNormalization(t *testing.T) {
	cases := map[string]string{"In Transit": "In-Transit", "Delivered": "Delivered", "Undelivered - consignee unavailable": "NDR", "RTO Initiated": "RTO"}
	for input, expected := range cases {
		if got := normalizeCourierStatus(input); got != expected {
			t.Errorf("%q normalized to %q, want %q", input, got, expected)
		}
	}
}

func TestCourierAWBRateAndNDRWorkflow(t *testing.T) {
	spInitDB()
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatal(err)
	}
	var ready bool
	if err := db.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM ` + schema + `.doctype_meta WHERE name='NDRCase')`).Scan(&ready); err != nil {
		t.Fatal(err)
	}
	if !ready {
		t.Skip("db/migrations_stage35_5_courier_integration.sql has not been applied")
	}

	registerCourierAdapter(stage35CourierStub{rates: []CourierRate{{Service: "Express", ChargePaise: 15000, EstimatedDays: 2, Serviceable: true}, {Service: "Ground", ChargePaise: 9000, EstimatedDays: 4, Serviceable: true}}})
	if err := SaveCourierCredential("default", "stage35stub", map[string]string{"api_key": "test-only", "webhook_secret": "secret"}); err != nil {
		t.Fatal(err)
	}
	bookingID := "TEST-COURIER-355"
	_, _ = db.DB.Exec(`DELETE FROM `+schema+`.documents WHERE id=$1 OR (doctype='NDRCase' AND data->>'booking_id'=$1) OR (doctype='CourierTrackingEvent' AND data->>'booking_id'=$1)`, bookingID)
	t.Cleanup(func() {
		_, _ = db.DB.Exec(`DELETE FROM `+schema+`.documents WHERE id=$1 OR (doctype='NDRCase' AND data->>'booking_id'=$1) OR (doctype='CourierTrackingEvent' AND data->>'booking_id'=$1)`, bookingID)
		_, _ = db.DB.Exec(`DELETE FROM ` + schema + `.channel_credentials WHERE channel_code='courier:stage35stub'`)
	})
	data, _ := json.Marshal(map[string]interface{}{"code": bookingID, "order_id": "TEST-ORDER-355", "destination_pincode": "560001", "awb_number": "LOCAL-AWB", "tracking_number": "LOCAL-AWB", "status": "AWB Assigned"})
	if _, err := db.DB.Exec(`INSERT INTO `+schema+`.documents (id,doctype,data,status,created_by) VALUES ($1,'LogisticsBooking',$2,'AWB Assigned','system')`, bookingID, data); err != nil {
		t.Fatal(err)
	}

	awb, err := AllocateCourierAWB(context.Background(), "default", "stage35stub", bookingID, CourierShipmentRequest{RemoteShipmentID: "REMOTE-355"})
	if err != nil || awb.AWB != "REAL-AWB-355" {
		t.Fatalf("allocate: %#v %v", awb, err)
	}
	second, err := AllocateCourierAWB(context.Background(), "default", "stage35stub", bookingID, CourierShipmentRequest{})
	if err != nil || second.AWB != awb.AWB {
		t.Fatalf("idempotent allocation: %#v %v", second, err)
	}
	rates, err := ShopCourierRates(context.Background(), "default", []string{"stage35stub"}, CourierRateRequest{WeightGrams: 500})
	if err != nil || len(rates) != 2 || rates[0].Service != "Ground" {
		t.Fatalf("sorted rates: %#v %v", rates, err)
	}

	if _, err := db.DB.Exec(`UPDATE `+schema+`.documents SET status='Handed Over', data=jsonb_set(data,'{status}','"Handed Over"') WHERE id=$1`, bookingID); err != nil {
		t.Fatal(err)
	}
	ndrID, err := RecordNDR("default", bookingID, "Customer unavailable", "courier:stage35stub")
	if err != nil {
		t.Fatal(err)
	}
	if err := ResolveNDR("default", ndrID, "reattempt", "Customer confirmed", "system", time.Now().Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	var ndrStatus string
	if err := db.DB.QueryRow(`SELECT status FROM `+schema+`.documents WHERE id=$1`, ndrID).Scan(&ndrStatus); err != nil || ndrStatus != "Reattempt Scheduled" {
		t.Fatalf("NDR status=%q err=%v", ndrStatus, err)
	}
}
