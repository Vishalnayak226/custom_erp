package engines

import (
	"custom_erp/db"
	"strings"
	"testing"
)

// TestReversibleTerminalStatuses (Stage 29.8.5) covers the two judgement calls
// 29.8 flagged rather than guessed at: an Approved Leave and a Selected
// VendorQuote were both terminal, so neither decision could ever be reversed.
// The user's call was to allow both, each requiring a reason code.
//
// Asserts on behaviour through ValidateStatusTransition rather than on the
// presence of seed rows, so it fails if the rules exist but enforcement
// disagrees with them - which is the failure mode that actually matters.
// Read-only: this validator takes the prior status as an argument, so nothing
// needs to be seeded into the shared dev DB.
func TestReversibleTerminalStatuses(t *testing.T) {
	db.InitDB(testConnStr())

	const tenantID = "default"

	reversals := []struct {
		name    string
		doctype string
		from    string
		to      string
	}{
		{"revoke an approved leave", "Leave", "Approved", "Rejected"},
		{"send an approved leave back for re-decision", "Leave", "Approved", "Applied"},
		{"reopen a rejected leave on appeal", "Leave", "Rejected", "Applied"},
		{"unselect a selected vendor quote", "VendorQuote", "Selected", "Submitted"},
		{"reject a previously selected quote", "VendorQuote", "Selected", "Rejected"},
		{"reconsider a rejected quote", "VendorQuote", "Rejected", "Submitted"},
	}

	for _, c := range reversals {
		t.Run(c.name+" is refused without a reason code", func(t *testing.T) {
			err := ValidateStatusTransition(tenantID, c.doctype, c.from, c.to,
				map[string]interface{}{"status": c.to})
			if err == nil {
				t.Fatal("expected the reversal to be refused with no reason code")
			}
			verr, ok := err.(*ValidationError)
			if !ok {
				t.Fatalf("expected *ValidationError, got %T: %v", err, err)
			}
			if verr.Code != "GLOBAL-0019" {
				t.Errorf("expected GLOBAL-0019 (invalid status transition), got %q", verr.Code)
			}
			if !strings.Contains(verr.Message, "reason_code") {
				t.Errorf("the message should say what is missing, got %q", verr.Message)
			}
		})

		t.Run(c.name+" is allowed with one", func(t *testing.T) {
			if err := ValidateStatusTransition(tenantID, c.doctype, c.from, c.to,
				map[string]interface{}{"status": c.to, "reason_code": "REVOKED"}); err != nil {
				t.Fatalf("expected the reversal to be allowed with a reason code, got %v", err)
			}
		})
	}

	// The new rules must not have opened anything else up. A status outside the
	// doctype's own declared option set is still refused even with a reason.
	t.Run("undeclared destinations are still refused", func(t *testing.T) {
		err := ValidateStatusTransition(tenantID, "Leave", "Approved", "Cancelled",
			map[string]interface{}{"status": "Cancelled", "reason_code": "X"})
		if err == nil {
			t.Fatal("expected a status outside Leave's own option set to still be refused")
		}
	})

	// The forward path is untouched: Applied -> Approved still needs no reason.
	t.Run("the forward approval path is unchanged", func(t *testing.T) {
		if err := ValidateStatusTransition(tenantID, "Leave", "Applied", "Approved",
			map[string]interface{}{"status": "Approved"}); err != nil {
			t.Fatalf("approving a leave should not have started demanding a reason: %v", err)
		}
	})
}
