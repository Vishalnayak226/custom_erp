package engines

import (
	"custom_erp/db"
	"fmt"
	"testing"
	"time"
)

// TestDocumentEditWindow (Stage 30.7) covers the configurable PO/GRN edit
// window: off by default, enforced once set, and scoped to edits of doctypes
// that actually declare a window. Cleans the shared dev DB before and after
// (the documented shared-DB test-pollution gotcha) so it's idempotent.
func TestDocumentEditWindow(t *testing.T) {
	connStr := testConnStr()
	db.InitDB(connStr)

	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("get schema: %v", err)
	}

	const key = "procurement.po_edit_window_days"
	const docID = "PO-EDITWINDOW-TEST"

	clearSetting := func() {
		db.DB.Exec("DELETE FROM "+schema+".system_settings WHERE key = $1", key)
		settingsCacheMu.Lock()
		delete(settingsCache, settingsCacheKey(schema, key))
		settingsCacheMu.Unlock()
	}
	// Seed a PurchaseOrder created 10 days ago, so a window shorter than that
	// is breached and a longer one is not.
	seedDoc := func() {
		db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.documents WHERE id = $1`, schema), docID)
		if _, err := db.DB.Exec(fmt.Sprintf(
			`INSERT INTO %s.documents (id, doctype, status, data, created_at, created_by) VALUES ($1, 'PurchaseOrder', 'Draft', '{}'::jsonb, $2, 'system')`, schema),
			docID, time.Now().AddDate(0, 0, -10)); err != nil {
			t.Fatalf("seed document: %v", err)
		}
	}
	cleanup := func() {
		clearSetting()
		db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.documents WHERE id = $1`, schema), docID)
	}

	clearSetting()
	seedDoc()
	defer cleanup()

	priorData := map[string]interface{}{"items": "[]"}

	t.Run("default is no time limit", func(t *testing.T) {
		if err := validateDocumentEditWindow(tenantID, "PurchaseOrder", docID, priorData); err != nil {
			t.Fatalf("expected no window enforced at the default of 0, got %v", err)
		}
	})

	t.Run("edit inside the window is allowed", func(t *testing.T) {
		if err := SetSetting(tenantID, key, "30", "test"); err != nil {
			t.Fatalf("set: %v", err)
		}
		if err := validateDocumentEditWindow(tenantID, "PurchaseOrder", docID, priorData); err != nil {
			t.Fatalf("10-day-old PO with a 30-day window should be editable, got %v", err)
		}
	})

	t.Run("edit past the window is blocked", func(t *testing.T) {
		if err := SetSetting(tenantID, key, "3", "test"); err != nil {
			t.Fatalf("set: %v", err)
		}
		if err := validateDocumentEditWindow(tenantID, "PurchaseOrder", docID, priorData); err == nil {
			t.Fatalf("10-day-old PO with a 3-day window should be blocked")
		}
	})

	t.Run("a create is never blocked", func(t *testing.T) {
		// docID == "" is the create case, even with the window breached.
		if err := validateDocumentEditWindow(tenantID, "PurchaseOrder", "", priorData); err != nil {
			t.Fatalf("create should never be window-checked, got %v", err)
		}
		if err := validateDocumentEditWindow(tenantID, "PurchaseOrder", docID, nil); err != nil {
			t.Fatalf("nil priorData is a create, got %v", err)
		}
	})

	t.Run("a doctype with no declared window is unaffected", func(t *testing.T) {
		if err := validateDocumentEditWindow(tenantID, "Item", docID, priorData); err != nil {
			t.Fatalf("Item declares no edit window, got %v", err)
		}
	})
}
