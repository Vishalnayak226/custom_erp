package engines

import (
	"custom_erp/db"
	"errors"
	"fmt"
	"sync"
	"testing"
)

// Stage 49.3.5: the approval engine holds the line on threshold splitting.
// A piece under an approval limit that, with the same maker's same-kind
// pieces for the same party in the detection window (49.5.4's definition),
// reaches the limit is approved at the combined amount - and only when the
// client has switched the guard on.

func splitFixture(t *testing.T) (tenantID, schema string, insert stewardInsert) {
	t.Helper()
	tenantID, schema, insert = stage140Tenant(t)
	for _, q := range []string{
		`INSERT INTO %[1]s.doctype_meta (name, module, document_type) VALUES ('SplitDrill', 'Drill', 'Transaction')`,
		`INSERT INTO %[1]s.approval_rules (doctype, min_amount, max_amount, required_role) VALUES ('SplitDrill', 0, 999.99, 'Store Manager')`,
		`INSERT INTO %[1]s.approval_rules (doctype, min_amount, required_role) VALUES ('SplitDrill', 1000, 'HR/Admin')`,
	} {
		if _, err := db.DB.Exec(fmt.Sprintf(q, schema)); err != nil {
			t.Fatal(err)
		}
	}
	return
}

func splitPiece(insert stewardInsert, id, maker, vendor string, amount float64) {
	insert(id, "SplitDrill", "Draft", maker, map[string]interface{}{"vendor_id": vendor, "total_amount": amount, "status": "Draft"})
}

func splitRole(t *testing.T, tenantID, id string) string {
	t.Helper()
	data, _, _, err := fetchDocument(tenantID, "SplitDrill", id)
	if err != nil {
		t.Fatal(err)
	}
	r, err := requiredApproverRoleForDoc(tenantID, "SplitDrill", id, data, DocumentBaseAmount(data))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestStage4935SplitGuardIsOffByDefaultAndLookAlikesAreLeftAlone(t *testing.T) {
	tenantID, _, insert := splitFixture(t)
	splitPiece(insert, "SP-A", "maker1", "V1", 600)
	splitPiece(insert, "SP-B", "maker1", "V1", 600)
	if got := splitRole(t, tenantID, "SP-A"); got != "Store Manager" {
		t.Fatalf("guard off: %q, want Store Manager (each piece on its own)", got)
	}
	if err := SetSetting(tenantID, SettingApprovalSplitGuard, "true", "test"); err != nil {
		t.Fatal(err)
	}
	if got := splitRole(t, tenantID, "SP-A"); got != "HR/Admin" {
		t.Fatalf("guard on, two 600 pieces of one party: %q, want HR/Admin", got)
	}
	// Look-alikes stay on the lower slab.
	splitPiece(insert, "SP-C", "maker2", "V2", 600) // alone for its maker
	splitPiece(insert, "SP-D", "maker1", "V3", 600) // another party
	splitPiece(insert, "SP-E", "maker1", "V4", 900) // one piece, no sibling
	splitPiece(insert, "SP-F", "maker3", "V5", 600) // sibling below half the limit
	splitPiece(insert, "SP-G", "maker3", "V5", 300)
	splitPiece(insert, "SP-H", "maker4", "V6", 1200) // already over: its own slab
	for _, id := range []string{"SP-C", "SP-D", "SP-E", "SP-F", "SP-G"} {
		if got := splitRole(t, tenantID, id); got != "Store Manager" {
			t.Errorf("%s: %q, want Store Manager", id, got)
		}
	}
	if got := splitRole(t, tenantID, "SP-H"); got != "HR/Admin" {
		t.Errorf("SP-H: %q", got)
	}
}

func TestStage4935TwoHalvesSubmittedAtOnceBothNeedTheCombinedApprover(t *testing.T) {
	tenantID, _, insert := splitFixture(t)
	if err := SetSetting(tenantID, SettingApprovalSplitGuard, "true", "test"); err != nil {
		t.Fatal(err)
	}
	splitPiece(insert, "SH-1", "maker1", "V1", 700)
	splitPiece(insert, "SH-2", "maker1", "V1", 700)
	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make([]error, 2)
	for i, id := range []string{"SH-1", "SH-2"} {
		wg.Add(1)
		go func(i int, id string) {
			defer wg.Done()
			<-start
			errs[i] = SubmitForApproval(tenantID, "SplitDrill", id, "maker1", "Cashier")
		}(i, id)
	}
	close(start)
	wg.Wait()
	for i, e := range errs {
		if e != nil {
			t.Fatalf("submit %d: %v", i, e)
		}
	}
	for _, id := range []string{"SH-1", "SH-2"} {
		err := DecideApproval(tenantID, "SplitDrill", id, "mgr1", "Store Manager", "HO", "Approved", "ok")
		if !errors.Is(err, ErrApprovalRoleMismatch) {
			t.Fatalf("%s: a Store Manager's approval of half of a split: %v, want a role mismatch", id, err)
		}
		if err := DecideApproval(tenantID, "SplitDrill", id, "admin1", "HR/Admin", "HO", "Approved", "ok"); err != nil {
			t.Fatalf("%s: the combined amount's approver: %v", id, err)
		}
	}
}

// Stage 49.10.4: a submit that read the document while it was a Draft must not
// write Pending Approval over a document someone has decided in the meantime.
func TestStage49104StaleSubmitCannotReopenADecidedDocument(t *testing.T) {
	tenantID, _, insert := splitFixture(t)
	splitPiece(insert, "ST-1", "maker1", "V1", 100)
	data, _, _, err := fetchDocument(tenantID, "SplitDrill", "ST-1")
	if err != nil {
		t.Fatal(err)
	}
	// The decision lands between that read and the write.
	if err := setDocumentStatus(tenantID, "SplitDrill", "ST-1", "Approved", map[string]interface{}{"total_amount": 100.0}); err != nil {
		t.Fatal(err)
	}
	if err := setDocumentStatusFrom(tenantID, "SplitDrill", "ST-1", "Draft", "Pending Approval", data); err == nil {
		t.Fatal("a stale submit wrote over an Approved document")
	}
	if _, status, _, _ := fetchDocument(tenantID, "SplitDrill", "ST-1"); status != "Approved" {
		t.Fatalf("status %q, want Approved", status)
	}
}

// Stage 49.5.7: the edges of the splitting definition, for the approval guard.
func TestStage4957SplitBoundaries(t *testing.T) {
	tenantID, schema, insert := splitFixture(t)
	if err := SetSetting(tenantID, SettingApprovalSplitGuard, "true", "test"); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name    string
		amounts []float64
		want    string
	}{
		{"exactly the limit together", []float64{500, 500}, "HR/Admin"},
		{"one paisa under the limit together", []float64{500, 499.99}, "Store Manager"},
		{"three pieces reaching the limit", []float64{500, 500, 500}, "HR/Admin"},
		{"a piece just under half the limit is not a split piece", []float64{499.99, 600}, "Store Manager"},
		{"a piece exactly at half the limit is one", []float64{500, 600}, "HR/Admin"},
		{"a piece at the limit is already on the higher slab", []float64{1000, 100}, "HR/Admin"},
	}
	for i, c := range cases {
		maker, vendor := fmt.Sprintf("bm%d", i), fmt.Sprintf("BV%d", i)
		for j, a := range c.amounts {
			splitPiece(insert, fmt.Sprintf("BD-%d-%d", i, j), maker, vendor, a)
		}
		if got := splitRole(t, tenantID, fmt.Sprintf("BD-%d-0", i)); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
	// The window edge: a sibling just inside 24 hours counts, one just outside does not.
	for _, edge := range []struct {
		id, minutes, want string
	}{{"WE-IN", "1439", "HR/Admin"}, {"WE-OUT", "1441", "Store Manager"}} {
		maker, vendor := "bw-"+edge.id, "WV-"+edge.id
		splitPiece(insert, edge.id+"-a", maker, vendor, 600)
		splitPiece(insert, edge.id+"-b", maker, vendor, 600)
		if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET created_at = CURRENT_TIMESTAMP - ($1 || ' minutes')::interval WHERE id = $2`, schema), edge.minutes, edge.id+"-b"); err != nil {
			t.Fatal(err)
		}
		if got := splitRole(t, tenantID, edge.id+"-a"); got != edge.want {
			t.Errorf("window %s minutes: %q, want %q", edge.minutes, got, edge.want)
		}
	}
}
