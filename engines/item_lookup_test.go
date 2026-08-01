package engines

import (
	"custom_erp/db"
	"encoding/json"
	"errors"
	"testing"
)

// TestResolveItemBySKU is Stage 30.1.1's regression test. The defect it locks
// down: GetItemGSTInfo matched only the internal `id`, so an item created
// through the UI (which generates a UUID id) could not be sold or purchased by
// the Code or Barcode the user actually has - and the POS typeahead fills in
// the Code, making the failure certain rather than occasional.
func TestResolveItemBySKU(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}

	const (
		uuidishID = "7d2c4e3f-lookup-test-0001"
		itemCode  = "TEST-LOOKUP-CODE-01"
		barcode   = "8901234567890111"
		deletedID = "7d2c4e3f-lookup-test-0002"
		deadCode  = "TEST-LOOKUP-DELETED-01"
	)
	cleanup := func() {
		db.DB.Exec("DELETE FROM "+schema+".documents WHERE id IN ($1, $2)", uuidishID, deletedID)
	}
	cleanup()
	defer cleanup()

	insert := func(id string, data map[string]interface{}, deleted bool) {
		raw, _ := json.Marshal(data)
		del := "NULL"
		if deleted {
			del = "CURRENT_TIMESTAMP"
		}
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by, deleted_at) VALUES ($1, 'Item', $2, 'Active', 'system', "+del+")", id, raw); err != nil {
			t.Fatalf("insert item %s: %v", id, err)
		}
	}
	insert(uuidishID, map[string]interface{}{
		"name": "Lookup Test Tee", "code": itemCode, "barcode": barcode,
		"hsn_code": "6109", "gst_rate": 12.0,
	}, false)
	insert(deletedID, map[string]interface{}{
		"name": "Deleted Tee", "code": deadCode, "hsn_code": "6109", "gst_rate": 12.0,
	}, true)

	// The three identifiers a user can plausibly hold. Before 30.1.1 only the
	// last of these resolved.
	for _, tc := range []struct {
		name      string
		sku       string
		matchedOn string
	}{
		{"by item Code (what the POS typeahead fills in)", itemCode, "code"},
		{"by Barcode (what a scanner sends)", barcode, "barcode"},
		{"by internal id (the pre-30.1.1 only-working case)", uuidishID, "id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			item, err := ResolveItemBySKU(tenantID, tc.sku)
			if err != nil {
				t.Fatalf("expected %q to resolve, got error: %v", tc.sku, err)
			}
			if item.ID != uuidishID {
				t.Fatalf("resolved the wrong record: got id %q, want %q", item.ID, uuidishID)
			}
			if item.MatchedOn != tc.matchedOn {
				t.Fatalf("matched_on = %q, want %q", item.MatchedOn, tc.matchedOn)
			}
			if item.Status != "Active" {
				t.Fatalf("status = %q, want Active", item.Status)
			}
			if name, _ := item.Data["name"].(string); name != "Lookup Test Tee" {
				t.Fatalf("data not carried through: %+v", item.Data)
			}

			// The end-to-end point of the fix: the same three strings must
			// clear the checkout/PurchaseOrder GST gate, which is where the
			// defect actually bit.
			hsn, rate, gstErr := GetItemGSTInfo(tenantID, tc.sku)
			if gstErr != nil {
				t.Fatalf("GetItemGSTInfo(%q) failed: %v", tc.sku, gstErr)
			}
			if hsn != "6109" || rate != 12.0 {
				t.Fatalf("got hsn=%s rate=%v, want 6109/12", hsn, rate)
			}
		})
	}

	t.Run("an unknown SKU is a typed not-found, not a bare error string", func(t *testing.T) {
		_, err := ResolveItemBySKU(tenantID, "TEST-LOOKUP-NO-SUCH-SKU")
		if !errors.Is(err, ErrItemNotFound) {
			t.Fatalf("want ErrItemNotFound, got %v", err)
		}
	})

	t.Run("an empty SKU never matches an arbitrary row", func(t *testing.T) {
		if _, err := ResolveItemBySKU(tenantID, ""); !errors.Is(err, ErrItemNotFound) {
			t.Fatalf("want ErrItemNotFound for empty sku, got %v", err)
		}
	})

	t.Run("a soft-deleted item stays unresolvable", func(t *testing.T) {
		if _, err := ResolveItemBySKU(tenantID, deadCode); !errors.Is(err, ErrItemNotFound) {
			t.Fatalf("soft-deleted item resolved by code: %v", err)
		}
		if _, err := ResolveItemBySKU(tenantID, deletedID); !errors.Is(err, ErrItemNotFound) {
			t.Fatalf("soft-deleted item resolved by id: %v", err)
		}
	})

	t.Run("Code wins over another item's Barcode on a collision", func(t *testing.T) {
		// One item's `code` is another item's `barcode`. The user typed a
		// Code, so the Code match must win - otherwise a barcode collision
		// silently sells the wrong product at the wrong tax rate.
		const collisionValue = "TEST-LOOKUP-COLLIDE"
		const codeOwner = "7d2c4e3f-lookup-test-0003"
		const barcodeOwner = "7d2c4e3f-lookup-test-0004"
		defer db.DB.Exec("DELETE FROM "+schema+".documents WHERE id IN ($1, $2)", codeOwner, barcodeOwner)
		insert(barcodeOwner, map[string]interface{}{"name": "Barcode Owner", "code": "TEST-LOOKUP-BC-OWNER", "barcode": collisionValue, "hsn_code": "6109", "gst_rate": 5.0}, false)
		insert(codeOwner, map[string]interface{}{"name": "Code Owner", "code": collisionValue, "barcode": "8901234567890222", "hsn_code": "6109", "gst_rate": 18.0}, false)

		item, err := ResolveItemBySKU(tenantID, collisionValue)
		if err != nil {
			t.Fatalf("collision lookup failed: %v", err)
		}
		if item.ID != codeOwner || item.MatchedOn != "code" {
			t.Fatalf("code match did not win: got id=%s matched_on=%s", item.ID, item.MatchedOn)
		}
	})
}
