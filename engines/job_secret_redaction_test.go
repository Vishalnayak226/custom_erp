package engines

import (
	"custom_erp/db"
	"strings"
	"testing"
)

// Stage 49.8.6: a job row never stores a secret - at the top of the payload or
// inside an event's document - while ordinary fields pass through untouched.
func TestStage49JobPayloadNeverStoresASecret(t *testing.T) {
	db.InitDB(testConnStr())
	ensureTestJobHandler("test-4986-redact") // 49.8.6: the registry is the enqueue allowlist
	id, err := EnqueueJob("default", "test-4986-redact", map[string]interface{}{
		"url": "https://hooks.example.com/x", "api_key": "AK-4986", "session_id": "POSSESS-1",
		"event_payload": map[string]interface{}{"code": "V1", "client_secret": "CS-4986", "lines": []interface{}{map[string]interface{}{"password": "PW-4986", "qty": 2}}},
	}, "")
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	defer db.DB.Exec(`DELETE FROM tenant_default.async_jobs WHERE id = $1`, id)
	var stored string
	if err := db.DB.QueryRow(`SELECT payload::text FROM tenant_default.async_jobs WHERE id = $1`, id).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	for _, leaked := range []string{"AK-4986", "CS-4986", "PW-4986"} {
		if strings.Contains(stored, leaked) {
			t.Errorf("the stored job holds %s: %s", leaked, stored)
		}
	}
	for _, kept := range []string{"POSSESS-1", "hooks.example.com", `"qty": 2`, `"code": "V1"`} {
		if !strings.Contains(stored, kept) {
			t.Errorf("an ordinary field was lost (%s): %s", kept, stored)
		}
	}
}
