package engines

import (
	"custom_erp/db"
	"strings"
	"testing"
	"time"
)

func TestPreparePurchaseRequisition(t *testing.T) {
	db.InitDB("postgres://postgres@localhost:5435/custom_erp?sslmode=disable")
	const tenantID = "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	const description = "__test_pr_description_master__ A4 copier paper"
	const location = "__TEST_PR_LOCATION__"
	financialYear := purchaseRequisitionFinancialYear(testNow())

	var descriptionMetaExists bool
	if err := db.DB.QueryRow("SELECT EXISTS(SELECT 1 FROM " + schema + ".doctype_meta WHERE name = 'PurchaseRequisitionDescription')").Scan(&descriptionMetaExists); err != nil {
		t.Fatalf("check description master metadata: %v", err)
	}
	if !descriptionMetaExists {
		if _, err := db.DB.Exec("INSERT INTO " + schema + ".doctype_meta (name, module, document_type, module_key) VALUES ('PurchaseRequisitionDescription', 'Procurement', 'Master', 'procurement')"); err != nil {
			t.Fatalf("seed description master metadata: %v", err)
		}
	}

	cleanupData := func() {
		_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'PurchaseRequisitionDescription' AND data->>'description' ILIKE $1", description)
		_, _ = db.DB.Exec("DELETE FROM "+schema+".sequence_counters WHERE doc_type = 'PR' AND store_code = $1 AND financial_year = $2", location, financialYear)
	}
	cleanupData()
	defer func() {
		cleanupData()
		if !descriptionMetaExists {
			_, _ = db.DB.Exec("DELETE FROM " + schema + ".doctype_meta WHERE name = 'PurchaseRequisitionDescription'")
		}
	}()

	first := map[string]interface{}{"description": "  " + description + "  "}
	if err := PreparePurchaseRequisition(tenantID, location, true, first); err != nil {
		t.Fatalf("PreparePurchaseRequisition create: %v", err)
	}
	if err := EnsurePurchaseRequisitionDescription(tenantID, first["description"].(string)); err != nil {
		t.Fatalf("EnsurePurchaseRequisitionDescription: %v", err)
	}
	firstCode, _ := first["code"].(string)
	if firstCode == "" || !strings.Contains(firstCode, location) {
		t.Fatalf("expected a configured PR sequence for %q, got %q", location, firstCode)
	}
	if first["description"] != description {
		t.Fatalf("expected trimmed description, got %#v", first["description"])
	}

	var savedDescription string
	if err := db.DB.QueryRow("SELECT data->>'description' FROM "+schema+".documents WHERE doctype = 'PurchaseRequisitionDescription' AND data->>'description' = $1", description).Scan(&savedDescription); err != nil {
		t.Fatalf("description master was not created: %v", err)
	}

	second := map[string]interface{}{"description": strings.ToUpper(description)}
	if err := PreparePurchaseRequisition(tenantID, location, true, second); err != nil {
		t.Fatalf("PreparePurchaseRequisition second create: %v", err)
	}
	if err := EnsurePurchaseRequisitionDescription(tenantID, second["description"].(string)); err != nil {
		t.Fatalf("EnsurePurchaseRequisitionDescription second save: %v", err)
	}
	var count int
	if err := db.DB.QueryRow("SELECT COUNT(*) FROM "+schema+".documents WHERE doctype = 'PurchaseRequisitionDescription' AND LOWER(data->>'description') = LOWER($1)", description).Scan(&count); err != nil {
		t.Fatalf("count description masters: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected one canonical description master, got %d", count)
	}
}

func testNow() time.Time { return time.Now() }
