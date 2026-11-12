package engines

import "testing"

// Stage 148.49: a posted receipt whose bill waits for its PO is not Ready to
// pay, and every tab's count is the length of the list it opens.
func TestStage14849ReadyToPayLeavesOutBillsWaitingForPO(t *testing.T) {
	tenantID, _, insert := poRulesTenant(t)
	grn := func(id string, extra map[string]interface{}) {
		d := map[string]interface{}{"flow": "steps", "grn_step": GRNStepPosted, "code": id, "vendor": "V-LOCAL", "location": "HO"}
		for k, v := range extra {
			d[k] = v
		}
		insert(id, "GRN", "Posted", d)
	}
	grn("G49-PLAIN", nil)
	grn("G49-NOPO", map[string]interface{}{"received_before_po": "Yes"})
	insert("PO49-DRAFT", "PurchaseOrder", "Pending Approval", map[string]interface{}{"received_before_po": "Yes"})
	grn("G49-RETRO-PENDING", map[string]interface{}{"received_before_po": "Yes", "retro_po_id": "PO49-DRAFT"})
	insert("PO49-OK", "PurchaseOrder", "Approved", map[string]interface{}{"received_before_po": "Yes"})
	grn("G49-RETRO-OK", map[string]interface{}{"received_before_po": "Yes", "retro_po_id": "PO49-OK"})

	rows, err := ListGRNInbox(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	tab := map[string]string{}
	for _, r := range rows {
		tab[r.ID] = r.Tab
	}
	want := map[string]string{"G49-PLAIN": "pay", "G49-NOPO": "po", "G49-RETRO-PENDING": "po", "G49-RETRO-OK": "pay"}
	for id, w := range want {
		if tab[id] != w {
			t.Errorf("%s on tab %q, want %q", id, tab[id], w)
		}
	}
}
