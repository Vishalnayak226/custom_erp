package engines

import (
	"custom_erp/db"
	"encoding/json"
	"strings"
	"testing"
)

// Stage 53.2/53.12. Two rules worth pinning, both of which are easy to get
// backwards in a later refactor:
//
//   - the PERMISSIVE case. An unregistered location code must keep selling.
//     Stage 17.9 deliberately left location a free-text column on most
//     doctypes, so a tenant whose shops were never entered into the Location
//     master would be unable to sell at all if this rule defaulted to "no".
//     That is the failure a strict default would cause, and it would show up
//     as every till in the chain refusing every sale.
//   - the DERIVED case. 53.1 keeps `sellable` optional (making it mandatory
//     would reject a save of every pre-existing Location), which means a row
//     created afterwards can carry no value. The answer then comes from
//     `type`, and a Warehouse must not quietly become sellable.
func TestLocationIsSellableResolutionOrder(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}

	const (
		locYes     = "S53-LOC-YES"
		locNo      = "S53-LOC-NO"
		locBlankWh = "S53-LOC-BLANK-WH"
		locBlankSt = "S53-LOC-BLANK-STORE"
		locAbsent  = "S53-LOC-NOT-REGISTERED"
	)
	cleanup := func() {
		db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id LIKE 'S53-LOC-%'")
	}
	cleanup()
	defer cleanup()

	insert := func(id, locType, sellable string) {
		data := map[string]interface{}{"code": id, "name": id, "type": locType, "status": "Active"}
		if sellable != "" {
			data["sellable"] = sellable
		}
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Location', $2, 'Active', 'system')", id, raw); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	insert(locYes, "Warehouse", "Yes") // an explicit Yes beats the type
	insert(locNo, "Store", "No")       // and an explicit No does too
	insert(locBlankWh, "Warehouse", "")
	insert(locBlankSt, "Store", "")

	for _, tc := range []struct {
		name string
		code string
		want bool
	}{
		{"an explicit Yes sells, even at a warehouse", locYes, true},
		{"an explicit No refuses, even at a store", locNo, false},
		{"a blank flag on a Warehouse does not sell", locBlankWh, false},
		{"a blank flag on a Store sells", locBlankSt, true},
		{"an unregistered location code still sells", locAbsent, true},
		{"an empty location code is not a place to sell", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := LocationIsSellable(tenantID, tc.code)
			if err != nil {
				t.Fatalf("LocationIsSellable(%q): %v", tc.code, err)
			}
			if got != tc.want {
				t.Errorf("LocationIsSellable(%q) = %v, want %v", tc.code, got, tc.want)
			}
		})
	}

	t.Run("the guard returns the catalog code, not a bare error", func(t *testing.T) {
		err := ValidatePOSSellableLocation(tenantID, locBlankWh)
		if err == nil {
			t.Fatal("expected a warehouse to be refused")
		}
		verr, ok := err.(*ValidationError)
		if !ok {
			t.Fatalf("expected a *ValidationError so the handler can map it to a catalog entry, got %T", err)
		}
		if verr.Code != "POSOFF-0245" {
			t.Errorf("code = %q, want POSOFF-0245", verr.Code)
		}
		// The cashier has to be able to act on this standing at a counter.
		if !strings.Contains(verr.Message, locBlankWh) {
			t.Errorf("message %q does not name the location it refused", verr.Message)
		}
	})

	t.Run("a sellable location raises no error", func(t *testing.T) {
		if err := ValidatePOSSellableLocation(tenantID, locBlankSt); err != nil {
			t.Errorf("a Store should be accepted, got %v", err)
		}
	})
}

// OpenPOSSession is the other half of 53.2: the till must refuse to BIND to a
// non-selling place, not only refuse the sale at the end. Covered separately
// from the handler because the rule lives in the engine - a second POS entry
// point added later gets it without anyone remembering to add it.
func TestOpenPOSSessionRefusesANonSellableLocation(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}

	const locWarehouse = "S53-SESS-WH"
	cleanup := func() {
		db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id LIKE 'S53-SESS-%'")
		db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'POSSession' AND data->>'cashier' = 's53-cashier'")
	}
	cleanup()
	defer cleanup()

	data, _ := json.Marshal(map[string]interface{}{
		"code": locWarehouse, "name": "Back Warehouse", "type": "Warehouse",
		"status": "Active", "sellable": "No",
	})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Location', $2, 'Active', 'system')", locWarehouse, data); err != nil {
		t.Fatalf("insert location: %v", err)
	}

	_, err = OpenPOSSession(tenantID, "", locWarehouse, "s53-cashier", "s53-user", 0)
	if err == nil {
		t.Fatal("expected OpenPOSSession to refuse a non-sellable location")
	}
	verr, ok := err.(*ValidationError)
	if !ok || verr.Code != "POSOFF-0245" {
		t.Fatalf("expected the POSOFF-0245 refusal, got %T %v", err, err)
	}

	// And nothing was written: a refused session must not leave a row behind
	// that would then block the cashier from opening a real one elsewhere.
	var count int
	if err := db.DB.QueryRow("SELECT COUNT(*) FROM "+schema+".documents WHERE doctype = 'POSSession' AND data->>'cashier' = $1", "s53-cashier").Scan(&count); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if count != 0 {
		t.Errorf("a refused session left %d POSSession row(s) behind", count)
	}
}

// Stage 53.12: the receipt header. The point of the test is the forgiving
// half - a receipt must print even when the masters behind it are incomplete,
// because the alternative is a cashier with a paid sale and no paper.
func TestReceiptStoreHeaderResolvesStoreAndEntity(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}

	const (
		entity    = "S53-ENT"
		locFull   = "S53-RCPT-FULL"
		locBare   = "S53-RCPT-BARE"
		locNoName = "S53-RCPT-NONAME"
	)
	cleanup := func() {
		db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id LIKE 'S53-RCPT-%' OR id = '" + entity + "'")
	}
	cleanup()
	defer cleanup()

	ins := func(id, doctype string, data map[string]interface{}) {
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, 'Active', 'system')", id, doctype, raw); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	ins(entity, "LegalEntity", map[string]interface{}{
		"code": entity, "name": "Minn Retail Pvt Ltd", "gstin": "27AAAAA0000A1Z5", "state": "Maharashtra", "status": "Active",
	})
	ins(locFull, "Location", map[string]interface{}{
		"code": locFull, "name": "Bandra Flagship", "type": "Store", "legal_entity": entity, "status": "Active",
	})
	ins(locBare, "Location", map[string]interface{}{
		"code": locBare, "name": "Pop-up Counter", "type": "Store", "status": "Active",
	})
	ins(locNoName, "Location", map[string]interface{}{
		"code": locNoName, "name": locNoName, "type": "Store", "status": "Active",
	})

	t.Run("a fully configured store carries its entity onto the bill", func(t *testing.T) {
		h := ReceiptStoreHeader(tenantID, locFull)
		if h.Name != "Bandra Flagship" {
			t.Errorf("Name = %q, want the shop's name", h.Name)
		}
		if h.EntityName != "Minn Retail Pvt Ltd" || h.GSTIN != "27AAAAA0000A1Z5" || h.State != "Maharashtra" {
			t.Errorf("entity not resolved: %+v", h)
		}
		if h.Line() != "Bandra Flagship (S53-RCPT-FULL)" {
			t.Errorf("Line() = %q", h.Line())
		}
	})

	t.Run("a store with no legal entity still prints", func(t *testing.T) {
		h := ReceiptStoreHeader(tenantID, locBare)
		if h.Name != "Pop-up Counter" {
			t.Errorf("Name = %q", h.Name)
		}
		if h.EntityName != "" || h.GSTIN != "" {
			t.Errorf("expected no entity details, got %+v", h)
		}
	})

	t.Run("a location whose name is just its code does not print it twice", func(t *testing.T) {
		if got := ReceiptStoreHeader(tenantID, locNoName).Line(); got != locNoName {
			t.Errorf("Line() = %q, want the bare code", got)
		}
	})

	t.Run("an unregistered location falls back to its code", func(t *testing.T) {
		h := ReceiptStoreHeader(tenantID, "S53-RCPT-NOT-THERE")
		if h.Line() != "S53-RCPT-NOT-THERE" {
			t.Errorf("Line() = %q, want the code itself", h.Line())
		}
	})
}

// Stage 53.1's other half: the save-time stamp that keeps `sellable` real data
// on every row, which is what lets the POS location picker filter on
// sellable=Yes and get exactly the set the server would accept.
func TestLocationMasterRulesStampSellable(t *testing.T) {
	for _, tc := range []struct {
		name    string
		payload map[string]interface{}
		want    string
	}{
		{"a Store is stamped sellable", map[string]interface{}{"type": "Store"}, "Yes"},
		{"a Warehouse is not", map[string]interface{}{"type": "Warehouse"}, "No"},
		{"HO is not", map[string]interface{}{"type": "HO"}, "No"},
		{"an explicit No on a Store is left alone", map[string]interface{}{"type": "Store", "sellable": "No"}, "No"},
		{"an explicit Yes on a Warehouse is left alone", map[string]interface{}{"type": "Warehouse", "sellable": "Yes"}, "Yes"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateLocationMasterRules(tc.payload); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := tc.payload["sellable"]; got != tc.want {
				t.Errorf("sellable = %v, want %v", got, tc.want)
			}
		})
	}
}
