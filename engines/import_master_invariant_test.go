package engines

import (
	"custom_erp/db"
	"encoding/json"
	"strings"
	"testing"
)

// TestBulkImportCSVRestoresTheMasterIDCodeInvariant is Stage 51.8.
//
// Stage 51.1 fixed the Master id = code invariant at handleGenericDoc and its
// comment claimed to cover "every caller (UI, CSV import, API)". Stage 51.9
// found that CSV import never reaches that handler - it goes through
// BulkImportCSV/importBatch, which drew its own id from GenerateSequence. For
// a Master that means id and code are two *different* draws from the same
// series, so every Link field pointing at the imported record fails "record
// does not exist" forever: 51.1's exact bug, on the one caller 51.1 believed
// it had already fixed.
//
// The assertion that matters is id == code, not the shape of either value.
func TestBulkImportCSVRestoresTheMasterIDCodeInvariant(t *testing.T) {
	db.InitDB(testConnStr())
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatalf("resolve default tenant schema: %v", err)
	}

	const vendorCode = "IMPORT-INVARIANT-VENDOR"
	cleanup := func() {
		_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'Vendor' AND (id = $1 OR data->>'code' = $1)", vendorCode)
	}
	cleanup()
	defer cleanup()

	csv := "code,name\n" + vendorCode + ",Import Invariant Vendor\n"
	res, err := BulkImportCSV("default", "Vendor", strings.NewReader(csv), "system", "HR/Admin", false)
	if err != nil {
		t.Fatalf("BulkImportCSV: %v", err)
	}
	if res.SuccessRows != 1 || res.FailedRows != 0 {
		t.Fatalf("expected 1 success / 0 failures, got %d/%d (%v)", res.SuccessRows, res.FailedRows, res.Errors)
	}

	var id, raw string
	if err := db.DB.QueryRow("SELECT id, data FROM "+schema+".documents WHERE doctype = 'Vendor' AND data->>'code' = $1", vendorCode).Scan(&id, &raw); err != nil {
		t.Fatalf("read imported vendor: %v", err)
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		t.Fatalf("unmarshal imported vendor: %v", err)
	}
	storedCode, _ := data["code"].(string)
	if id != storedCode {
		t.Errorf("imported Master id = %q, code = %q - they must be equal or every Link field pointing at this record fails", id, storedCode)
	}
	if id != vendorCode {
		t.Errorf("imported Master id = %q, want the supplied code %q", id, vendorCode)
	}

	// Negative control: a Transaction doctype must NOT get id = code. Its ids
	// come from the document-number series, and forcing id = code here would
	// change how every imported transaction is numbered.
	if isMaster, err := IsMasterDoctype("default", "PurchaseOrder"); err != nil {
		t.Fatalf("IsMasterDoctype(PurchaseOrder): %v", err)
	} else if isMaster {
		t.Error("PurchaseOrder reads as a Master doctype - the invariant would wrongly apply to transactions")
	}
	if isMaster, err := IsMasterDoctype("default", "Vendor"); err != nil {
		t.Fatalf("IsMasterDoctype(Vendor): %v", err)
	} else if !isMaster {
		t.Error("Vendor does not read as a Master doctype - the invariant would never apply")
	}
}

// TestApplyMasterIDCodeInvariantNoOpCases pins the cases where the shared
// helper must leave the payload completely alone. Pure unit test, no DB for
// the paths that return before the doctype lookup.
func TestApplyMasterIDCodeInvariantNoOpCases(t *testing.T) {
	db.InitDB(testConnStr())

	cases := []struct {
		name     string
		doctype  string
		isCreate bool
		payload  map[string]interface{}
	}{
		{"update never rewrites an existing id", "Vendor", false, map[string]interface{}{"code": "V-1"}},
		{"an explicit id wins", "Vendor", true, map[string]interface{}{"code": "V-1", "id": "EXPLICIT"}},
		{"no code to copy from", "Vendor", true, map[string]interface{}{"name": "No code"}},
		{"a blank code is not a code", "Vendor", true, map[string]interface{}{"code": "   "}},
		{"a transaction doctype is untouched", "PurchaseOrder", true, map[string]interface{}{"code": "PO-1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before, _ := tc.payload["id"]
			got, err := ApplyMasterIDCodeInvariant("default", tc.doctype, tc.isCreate, tc.payload)
			if err != nil {
				t.Fatalf("ApplyMasterIDCodeInvariant: %v", err)
			}
			if got != "" {
				t.Errorf("returned %q, want \"\" (no-op)", got)
			}
			if after, _ := tc.payload["id"]; after != before {
				t.Errorf("payload id changed from %v to %v", before, after)
			}
		})
	}

	// And the one case where it must act.
	payload := map[string]interface{}{"code": "ACTS-ON-THIS"}
	got, err := ApplyMasterIDCodeInvariant("default", "Vendor", true, payload)
	if err != nil {
		t.Fatalf("ApplyMasterIDCodeInvariant (acting case): %v", err)
	}
	if got != "ACTS-ON-THIS" || payload["id"] != "ACTS-ON-THIS" {
		t.Errorf("returned %q / payload id %v, want both to be the code", got, payload["id"])
	}
}

// TestBulkImportCSVGeneratesDesignBasedSKUForItemVariants is Stage 51.8,
// closing the second half of the gap Stage 51.9 found and named.
//
// PrepareItemVariantCode (Stage 51.5) had exactly one caller -
// handleGenericDoc - so Design ID / Combination ID generation was reachable
// only from the single-record form. A bulk-imported variant got a plain
// sequence number instead of a real SKU, which is why 51.9's own one-time
// client migration had to pre-compute all 34 item codes by hand in a
// transform script rather than letting the engine do it.
func TestBulkImportCSVGeneratesDesignBasedSKUForItemVariants(t *testing.T) {
	db.InitDB(testConnStr())
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatalf("resolve default tenant schema: %v", err)
	}

	const designCode = "IMPORT-SKU-DESIGN"
	cleanup := func() {
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id LIKE '" + designCode + "%' OR data->>'family' = '" + designCode + "'")
	}
	cleanup()
	defer cleanup()

	// The parent Design is an ordinary ProductFamily master.
	familyEncoded, _ := json.Marshal(map[string]interface{}{"code": designCode, "name": "Import SKU Design"})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'ProductFamily', $2, 'Active', 'system')", designCode, familyEncoded); err != nil {
		t.Fatalf("insert fixture design: %v", err)
	}

	// Two variants of that design, differing only in the attributes that are
	// supposed to shape the SKU. No code column at all - that is the whole
	// point: the engine has to build it.
	csv := "name,family,barcode,hsn_code,gst_rate,color,polish,size\n" +
		"Rose Small,IMPORT-SKU-DESIGN,8901234500071,7117,3,Rose,GoldPolish,S\n" +
		"Rose Large,IMPORT-SKU-DESIGN,8901234500088,7117,3,Rose,GoldPolish,L\n"

	res, err := BulkImportCSV("default", "Item", strings.NewReader(csv), "system", "HR/Admin", false)
	if err != nil {
		t.Fatalf("BulkImportCSV: %v", err)
	}
	if res.SuccessRows != 2 || res.FailedRows != 0 {
		t.Fatalf("expected 2 success / 0 failures, got %d/%d (%v)", res.SuccessRows, res.FailedRows, res.Errors)
	}

	rows, err := db.DB.Query("SELECT id, data FROM "+schema+".documents WHERE doctype = 'Item' AND data->>'family' = $1 ORDER BY id", designCode)
	if err != nil {
		t.Fatalf("read imported variants: %v", err)
	}
	defer rows.Close()

	found := map[string]string{}
	for rows.Next() {
		var id, raw string
		if err := rows.Scan(&id, &raw); err != nil {
			t.Fatalf("scan variant: %v", err)
		}
		var data map[string]interface{}
		if err := json.Unmarshal([]byte(raw), &data); err != nil {
			t.Fatalf("unmarshal variant: %v", err)
		}
		code, _ := data["code"].(string)
		name, _ := data["name"].(string)
		found[name] = code

		// The id = code invariant has to hold here too - an Item is a Master,
		// and these codes were generated rather than supplied.
		if id != code {
			t.Errorf("%s: id %q <> code %q", name, id, code)
		}
		// The generated SKU must be built from the parent design, not be a
		// plain Item sequence number.
		if !strings.HasPrefix(code, designCode+"-") {
			t.Errorf("%s: code %q is not a design-based SKU (want prefix %q)", name, code, designCode+"-")
		}
		// parent_product_code is the field sibling-variant uniqueness groups
		// by; bridging family -> parent_product_code is part of the same fix.
		if parent, _ := data["parent_product_code"].(string); parent != designCode {
			t.Errorf("%s: parent_product_code = %q, want %q", name, parent, designCode)
		}
	}
	if len(found) != 2 {
		t.Fatalf("expected 2 imported variants, found %d (%v)", len(found), found)
	}
	if found["Rose Small"] == found["Rose Large"] {
		t.Errorf("both variants got the same SKU %q - size is supposed to differentiate them", found["Rose Small"])
	}
	// Size is one of the six recognized variant-differentiating attributes,
	// so it must actually appear in the code.
	if !strings.HasSuffix(found["Rose Small"], "-S") || !strings.HasSuffix(found["Rose Large"], "-L") {
		t.Errorf("sizes not reflected in the SKUs: small=%q large=%q", found["Rose Small"], found["Rose Large"])
	}
}

// TestBulkImportCSVStillRequiresCodeWithoutAFamilyColumn is the negative
// control for the Stage 51.8 exception in missingMandatoryColumns: `code`
// stops being a required column *only* for an Item import that carries a
// family (where the server generates the SKU). Everything else must still be
// rejected up front rather than failing every row individually.
func TestBulkImportCSVStillRequiresCodeWithoutAFamilyColumn(t *testing.T) {
	db.InitDB(testConnStr())

	cases := []struct {
		name    string
		doctype string
		csv     string
	}{
		{
			name:    "an Item with no family column",
			doctype: "Item",
			csv:     "name,barcode,hsn_code,gst_rate\nNo Family,8901234500095,7117,3\n",
		},
		{
			name:    "a plain Master doctype",
			doctype: "Vendor",
			csv:     "name\nNo Code Vendor\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := BulkImportCSV("default", tc.doctype, strings.NewReader(tc.csv), "system", "HR/Admin", true)
			if err == nil {
				t.Fatal("expected the upload to be rejected for a missing code column, got no error")
			}
			if !strings.Contains(err.Error(), "code") {
				t.Errorf("rejection should name the missing code column, got: %v", err)
			}
		})
	}
}
