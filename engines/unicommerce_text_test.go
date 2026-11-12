package engines

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Stage 146.1.6 - the Unicommerce worker only logs "Would push" (the real
// connector is 146.4.1), so the screen and the admin guide must not say the
// ERP sends stock or orders.
func TestStage14616UnicommerceTextIsHonest(t *testing.T) {
	worker, err := os.ReadFile("unicommerce.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(worker), "Would push") {
		t.Skip("the worker now pushes for real - this text check no longer applies (146.4.1)")
	}
	screen, err := os.ReadFile(filepath.Join("..", "public", "view-admin.js"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(screen)
	i := strings.Index(s, "id: 'unicommerce'")
	if i < 0 {
		t.Fatal("Unicommerce entry not found in view-admin.js")
	}
	entry := s[i : i+400]
	if !strings.Contains(entry, "Not connected yet") || strings.Contains(entry, "order push and inventory sync") {
		t.Errorf("the Integrations entry must say Unicommerce is not connected yet: %s", entry)
	}
	guide, err := os.ReadFile(filepath.Join("..", "docs", "guides", "admin-guide.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(guide), "Unicommerce is not connected yet") {
		t.Error("ADMIN_GUIDE must say Unicommerce is not connected yet")
	}
}
