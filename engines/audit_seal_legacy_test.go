package engines

import "testing"

// TestSealLegacyAuditRowsIsIdempotent: the admin button (47.7.4) may be pressed
// twice. The second press must find nothing new and write no empty checkpoint.
func TestSealLegacyAuditRowsIsIdempotent(t *testing.T) {
	tenantID := auditTestTenant(t)
	if _, err := SealLegacyAuditRows(tenantID); err != nil {
		t.Fatalf("first seal: %v", err)
	}
	again, err := SealLegacyAuditRows(tenantID)
	if err != nil {
		t.Fatalf("second seal: %v", err)
	}
	if again != nil {
		t.Errorf("second seal wrote checkpoint %+v with nothing new to seal", again)
	}
}
