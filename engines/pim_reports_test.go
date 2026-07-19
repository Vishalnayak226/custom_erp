package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
)

func TestGetPIMDashboard(t *testing.T) {
	db.InitDB("postgres://postgres@localhost:5435/custom_erp?sslmode=disable")
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatalf("resolve default tenant schema: %v", err)
	}

	const readyItem = "PIM-DASHBOARD-READY"
	const publishedItem = "PIM-DASHBOARD-PUBLISHED"
	const readyProfile = readyItem + "::profile"
	const publishedProfile = publishedItem + "::profile"
	const contentID = readyItem + "::dashboard-content"
	const mediaID = readyItem + "::dashboard-main-image"

	cleanup := func() {
		_, _ = db.DB.Exec("DELETE FROM "+schema+".pim_publish_queue WHERE item_code IN ($1, $2)", readyItem, publishedItem)
		_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE id IN ($1, $2, $3, $4, $5, $6)", readyItem, publishedItem, readyProfile, publishedProfile, contentID, mediaID)
	}
	cleanup()
	defer cleanup()

	baseline, err := GetPIMDashboard("default")
	if err != nil {
		t.Fatalf("read dashboard baseline: %v", err)
	}

	insertDoc := func(id, doctype, status string, data map[string]interface{}) {
		t.Helper()
		encoded, err := json.Marshal(data)
		if err != nil {
			t.Fatalf("marshal %s: %v", id, err)
		}
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system')", id, doctype, encoded, status); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}

	insertDoc(readyItem, "Item", "Active", map[string]interface{}{"code": readyItem, "name": "Dashboard ready fixture"})
	insertDoc(publishedItem, "Item", "Active", map[string]interface{}{"code": publishedItem, "name": "Dashboard published fixture"})
	insertDoc(readyProfile, "PIMProductProfile", "Ready to Publish", map[string]interface{}{
		"product_id": readyItem, "enrichment_status": "Ready to Publish", "completeness_score": 50,
	})
	insertDoc(publishedProfile, "PIMProductProfile", "Published", map[string]interface{}{
		"product_id": publishedItem, "enrichment_status": "Published", "completeness_score": 100,
	})
	insertDoc(contentID, "ProductContent", "Pending Approval", map[string]interface{}{
		"product_id": readyItem, "language": "en", "title": "Pending dashboard content",
	})
	insertDoc(mediaID, "ProductMedia", "Active", map[string]interface{}{
		"item": readyItem, "media_role": "Main Image", "status": "Active",
	})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".pim_publish_queue (item_code, channel_code, payload_hash, status) VALUES ($1, 'DASHBOARD', 'queued-dashboard-fixture', 'Queued'), ($2, 'DASHBOARD', 'failed-dashboard-fixture', 'Failed')", readyItem, publishedItem); err != nil {
		t.Fatalf("insert dashboard publish fixtures: %v", err)
	}

	dashboard, err := GetPIMDashboard("default")
	if err != nil {
		t.Fatalf("read dashboard: %v", err)
	}
	checks := []struct {
		name string
		got  int
		want int
	}{
		{"total products", dashboard.TotalProducts, baseline.TotalProducts + 2},
		{"incomplete products", dashboard.IncompleteProducts, baseline.IncompleteProducts + 1},
		{"pending content approvals", dashboard.PendingContentApprovals, baseline.PendingContentApprovals + 1},
		{"ready to publish", dashboard.ReadyToPublish, baseline.ReadyToPublish + 1},
		{"published products", dashboard.PublishedProducts, baseline.PublishedProducts + 1},
		{"missing main images", dashboard.MissingMainImages, baseline.MissingMainImages + 1},
		{"queued publish jobs", dashboard.QueuedPublishJobs, baseline.QueuedPublishJobs + 1},
		{"failed publish jobs", dashboard.FailedPublishJobs, baseline.FailedPublishJobs + 1},
	}
	for _, check := range checks {
		if check.got != check.want {
			t.Errorf("%s: got %d, want %d", check.name, check.got, check.want)
		}
	}
}
