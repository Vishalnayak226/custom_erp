package engines

import (
	"context"
	"custom_erp/db"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// Stage 135.11: Book Pickup sends no courier, so the courier service areas
// pick one by PIN code, and a booking stopped by missing credentials is
// finished by the next click instead of being reported as done.
func TestStage135ReversePickupPicksCourierByPincodeAndRetryFinishes(t *testing.T) {
	spInitDB()
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatal(err)
	}
	registerCourierAdapter(stage35CourierStub{})
	const returnID, areaID, pin = "TEST-RET-13511", "TEST-CSA-13511", "999111"
	cleanup := func() {
		_, _ = db.DB.Exec(`DELETE FROM `+schema+`.documents WHERE id IN ($1,$2) OR (doctype='LogisticsBooking' AND data->>'return_request_id'=$1)`, returnID, areaID)
		_, _ = db.DB.Exec(`DELETE FROM ` + schema + `.channel_credentials WHERE channel_code='courier:stage35stub'`)
	}
	cleanup()
	t.Cleanup(cleanup)

	area, _ := json.Marshal(map[string]interface{}{"code": areaID, "courier": "stage35stub", "pincode_prefix": "999", "priority": -100000})
	ret, _ := json.Marshal(map[string]interface{}{"code": returnID, "request_type": "Customer Return", "status": "Approved", "original_order_id": "TEST-ORDER-13511"})
	for _, doc := range []struct {
		id, doctype, status string
		data                []byte
	}{{areaID, "CourierServiceArea", "Active", area}, {returnID, "ReturnRequest", "Approved", ret}} {
		if _, err := db.DB.Exec(`INSERT INTO `+schema+`.documents (id,doctype,data,status,created_by) VALUES ($1,$2,$3,$4,'system')`, doc.id, doc.doctype, doc.data, doc.status); err != nil {
			t.Fatal(err)
		}
	}

	ctx := context.Background()
	at := time.Now().Add(24 * time.Hour)
	// No credentials yet: the booking is made with the picked courier, the AWB is refused.
	firstID, _, err := ScheduleReturnReversePickup(ctx, "default", returnID, "", pin, "1 Test Street", "Test Customer", at)
	if err == nil || !strings.Contains(err.Error(), "credentials pending") {
		t.Fatalf("without credentials: booking=%q err=%v", firstID, err)
	}
	if _, booking, _, err := fetchLogisticsBooking("default", firstID); err != nil || courierDataString(booking, "carrier") != "stage35stub" {
		t.Fatalf("carrier picked by PIN code: %v %v", booking["carrier"], err)
	}

	if err := SaveCourierCredential("default", "stage35stub", map[string]string{"api_key": "test-only"}); err != nil {
		t.Fatal(err)
	}
	secondID, awb, err := ScheduleReturnReversePickup(ctx, "default", returnID, "", pin, "1 Test Street", "Test Customer", at)
	if err != nil || secondID != firstID || awb != "REAL-AWB-355" {
		t.Fatalf("retry finishes the same booking: booking=%q (first %q) awb=%q err=%v", secondID, firstID, awb, err)
	}
	var status string
	if err := db.DB.QueryRow(`SELECT status FROM `+schema+`.documents WHERE id=$1`, returnID).Scan(&status); err != nil || status != "Pickup Scheduled" {
		t.Fatalf("return status=%q err=%v", status, err)
	}
}
