package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
)

// TestBulkSeedCatalogTranslations (Stage 36.7.5) exercises the three
// outcomes a real bulk run produces: a genuine seed (Approved EN content,
// no HI row yet), a skip because HI content already exists, and a skip
// because there is no Approved EN content to seed from - all in one batch,
// proving one bad/ineligible item never aborts the others.
func TestBulkSeedCatalogTranslations(t *testing.T) {
	db.InitDB(testConnStr())
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatalf("resolve default tenant schema: %v", err)
	}

	itemA := "PIM-XLT-A" // gets seeded
	itemB := "PIM-XLT-B" // already has HI content - skipped
	itemC := "PIM-XLT-C" // no approved EN content - skipped
	contentIDs := []string{itemA + "::en", itemA + "::hi", itemB + "::en", itemB + "::hi", itemC + "::en"}

	cleanup := func() {
		for _, id := range contentIDs {
			_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'ProductContent' AND id = $1", id)
		}
		for _, id := range []string{itemA, itemB, itemC} {
			_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'Item' AND id = $1", id)
		}
	}
	cleanup()
	defer cleanup()

	insertItem := func(id string) {
		t.Helper()
		encoded, _ := json.Marshal(map[string]interface{}{"code": id, "name": id})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Item', $2, 'Active', 'system')", id, encoded); err != nil {
			t.Fatalf("insert item %s: %v", id, err)
		}
	}
	insertContent := func(id, itemCode, language, status, title string) {
		t.Helper()
		encoded, _ := json.Marshal(map[string]interface{}{
			"code": id, "product_id": itemCode, "language": language,
			"title": title, "short_desc": title + " short", "long_desc": title + " long",
			"status": status,
		})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'ProductContent', $2, $3, 'system')", id, encoded, status); err != nil {
			t.Fatalf("insert content %s: %v", id, err)
		}
	}

	insertItem(itemA)
	insertContent(itemA+"::en", itemA, "en", "Approved", "Item A")

	insertItem(itemB)
	insertContent(itemB+"::en", itemB, "en", "Approved", "Item B")
	insertContent(itemB+"::hi", itemB, "hi", "Draft", "Item B (hi, pre-existing)")

	insertItem(itemC)
	insertContent(itemC+"::en", itemC, "en", "Draft", "Item C (not yet approved)")

	outcomes, err := BulkSeedCatalogTranslations("default", "", []string{itemA, itemB, itemC}, "en", "hi", "manager1")
	if err != nil {
		t.Fatalf("BulkSeedCatalogTranslations: %v", err)
	}
	if len(outcomes) != 3 {
		t.Fatalf("expected 3 outcomes, got %d: %#v", len(outcomes), outcomes)
	}
	byItem := map[string]TranslationSeedOutcome{}
	for _, o := range outcomes {
		byItem[o.ItemCode] = o
	}

	seeded := byItem[itemA]
	if seeded.Error != "" || seeded.ContentID != itemA+"::hi" {
		t.Errorf("item A should have been seeded, got %#v", seeded)
	}
	var title, status, owner string
	if err := db.DB.QueryRow("SELECT data->>'title', status, COALESCE(data->>'owner','') FROM "+schema+".documents WHERE id = $1", itemA+"::hi").Scan(&title, &status, &owner); err != nil {
		t.Fatalf("read seeded content: %v", err)
	}
	if title != "Item A" || status != "Draft" || owner != "manager1" {
		t.Errorf("seeded row = title=%q status=%q owner=%q, want title=Item A status=Draft owner=manager1", title, status, owner)
	}

	if byItem[itemB].Error == "" {
		t.Errorf("item B already has hi content and should have been skipped, got %#v", byItem[itemB])
	}
	if byItem[itemC].Error == "" {
		t.Errorf("item C has no approved en content and should have been skipped, got %#v", byItem[itemC])
	}

	// Same/blank language guards, checked without needing any fixture.
	if _, err := BulkSeedCatalogTranslations("default", "", []string{itemA}, "en", "en", "manager1"); err == nil {
		t.Error("expected source == target to be refused")
	}
	if _, err := BulkSeedCatalogTranslations("default", "", []string{itemA}, "en", "", "manager1"); err == nil {
		t.Error("expected a blank target_language to be refused")
	}
}
