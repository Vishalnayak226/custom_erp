package engines

import (
	"custom_erp/db"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// Stage 102.3.1: the store app's scan. One code - barcode, item code or GRN
// batch number - gives the till's price and MRP, the batches still holding
// stock, and stock only at stores the user may work at, working store first
// even at zero. Cost never leaves the server.
func TestStoreAppLookup(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schemaName := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schemaName)
	if _, err := ProvisionTenantSchema(tenantID, schemaName, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	dayAgo := func(n int) string { return time.Now().AddDate(0, 0, -n).Format("2006-01-02") }
	if _, err := db.DB.Exec(`INSERT INTO `+schemaName+`.documents (doctype, id, data, status, created_by) VALUES
		('Item', 'SA-TEE', '{"code":"SA-TEE","name":"Store app tee","barcode":"0200000102001","brand":"Minn","sale_price":499,"mrp":799,"cost_price":211}', 'Active', 'admin'),
		('Item', 'SA-CAP', '{"code":"SA-CAP","name":"Store app cap","mrp":299}', 'Active', 'admin'),
		('Location', 'ST-A', '{"name":"Andheri","type":"Store"}', 'Active', 'admin'),
		('Location', 'ST-B', '{"name":"Bandra","type":"Store"}', 'Active', 'admin'),
		('Location', 'ST-C', '{"name":"Colaba","type":"Store"}', 'Active', 'admin'),
		('Batch', 'BATCH-102-1', jsonb_build_object('batch_no','B-102-OLD','item','SA-TEE','received_qty',10,'received_on',$1::text,'grn','GRN-102-1'), 'Active', 'test'),
		('Batch', 'BATCH-102-2', jsonb_build_object('batch_no','B-102-NEW','item','SA-TEE','received_qty',4,'received_on',$2::text,'grn','GRN-102-2'), 'Active', 'test')`,
		dayAgo(100), dayAgo(5)); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := db.DB.Exec(`INSERT INTO ` + schemaName + `.inventory_availability (sku, location_code, on_hand, available) VALUES
		('SA-TEE', 'ST-A', 3, 3), ('SA-TEE', 'ST-B', 6, 6), ('SA-TEE', 'ST-C', 9, 9), ('SA-CAP', 'ST-B', 2, 2)`); err != nil {
		t.Fatalf("seed stock: %v", err)
	}
	// A cashier at Andheri who may also work at Bandra - not Colaba.
	access := combineStoreAccess("Cashier", "ST-A", StoreAccessSelected, []string{"ST-B"}, StoreAccessHome, nil)

	got, err := storeAppLookup(tenantID, access, "ST-A", "0200000102001")
	if err != nil {
		t.Fatalf("scan by barcode: %v", err)
	}
	if got.Code != "SA-TEE" || got.MatchedOn != "barcode" || got.Brand != "Minn" {
		t.Errorf("item = %s matched on %s brand %s", got.Code, got.MatchedOn, got.Brand)
	}
	if got.Price != 499 || got.PriceSource != PriceSourceItemMaster || got.MRP != 799 {
		t.Errorf("price %v (%s) mrp %v, want 499 from the item, mrp 799", got.Price, got.PriceSource, got.MRP)
	}
	if len(got.Stock) != 2 || got.Stock[0].Store != "ST-A" || !got.Stock[0].Working || got.Stock[0].Available != 3 ||
		got.Stock[1].Store != "ST-B" || got.Stock[1].Available != 6 || got.Stock[1].Name != "Bandra" {
		t.Errorf("stock = %+v, want Andheri (working) 3 then Bandra 6, and no Colaba", got.Stock)
	}
	if len(got.Batches) != 2 || got.Batches[0].BatchNo != "B-102-OLD" || got.Batch != nil {
		t.Errorf("batches = %+v (scanned %+v), want B-102-OLD first and no scanned batch", got.Batches, got.Batch)
	}
	raw, _ := json.Marshal(got)
	if strings.Contains(string(raw), "cost") || strings.Contains(string(raw), "211") {
		t.Errorf("the answer carries cost: %s", raw)
	}

	// A batch number from the sticker, typed in lower case.
	byBatch, err := storeAppLookup(tenantID, access, "ST-A", "b-102-old")
	if err != nil {
		t.Fatalf("scan by batch: %v", err)
	}
	if byBatch.MatchedOn != "batch" || byBatch.Code != "SA-TEE" || byBatch.Batch == nil ||
		byBatch.Batch.BatchNo != "B-102-OLD" || byBatch.Batch.AgeDays != 100 || len(byBatch.Batches) != 2 {
		t.Errorf("batch scan = matched %s item %s batch %+v batches %d", byBatch.MatchedOn, byBatch.Code, byBatch.Batch, len(byBatch.Batches))
	}

	// No stock here: the working store still shows, at zero, first; the price
	// falls back to the MRP when the item has no sale price.
	capItem, err := storeAppLookup(tenantID, access, "ST-A", "SA-CAP")
	if err != nil {
		t.Fatalf("scan by code: %v", err)
	}
	if len(capItem.Stock) != 2 || capItem.Stock[0].Store != "ST-A" || capItem.Stock[0].Available != 0 || capItem.Stock[1].Available != 2 {
		t.Errorf("stock = %+v, want Andheri 0 then Bandra 2", capItem.Stock)
	}
	if capItem.Price != 299 || capItem.PriceSource != PriceSourceItemMRP {
		t.Errorf("price %v (%s), want the MRP 299", capItem.Price, capItem.PriceSource)
	}

	if _, err := storeAppLookup(tenantID, access, "ST-A", "NO-SUCH-CODE"); !errors.Is(err, ErrItemNotFound) {
		t.Errorf("unknown code: err = %v, want ErrItemNotFound", err)
	}
}
