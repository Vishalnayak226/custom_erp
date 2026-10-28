package engines

import (
	"custom_erp/db"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// Stage 49.5.6c: accepting a production order's BOM change is the action's
// alone - it needs a reason and a change to accept, and leaves an audit row
// with who, why and what changed; a save or an import can neither set nor
// clear it.
func TestStage4956cBOMVarianceAcknowledgement(t *testing.T) {
	db.InitDB(testConnStr())
	const tenantID = "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	const (
		bom    = "T4956C-BOM"
		order  = "T4956C-PRO"
		done   = "T4956C-PRO-DONE"
		reason = "T4956C cloth substituted, approved by planning"
	)
	clean := func() { db.DB.Exec(`DELETE FROM ` + schema + `.documents WHERE id LIKE 'T4956C-%'`) }
	clean()
	defer clean()
	insert := func(id, doctype, status string, data map[string]interface{}) {
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec(`INSERT INTO `+schema+`.documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'admin')`, id, doctype, raw, status); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	code := func(err error) string {
		var v *ValidationError
		if errors.As(err, &v) {
			return v.Code
		}
		return ""
	}
	for _, s := range []string{"T4956C-KIT", "T4956C-CLOTH", "T4956C-THREAD"} {
		insert(s, "Item", "Active", map[string]interface{}{"code": s, "name": s})
	}
	insert(bom, "BOM", "Active", map[string]interface{}{"parent_item": "T4956C-KIT", "components": `[{"sku":"T4956C-CLOTH","qty":2}]`})
	issued, err := explodeBOMComponents(tenantID, bom, 1.0, map[string]bool{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	snap, _ := json.Marshal(issued)
	orderData := func() map[string]interface{} {
		return map[string]interface{}{"code": order, "bom_id": bom, "quantity": 5, "location": "HO", "status": "Material Issued", "bom_snapshot": string(snap)}
	}
	insert(order, "ProductionOrder", "Material Issued", orderData())
	done2 := orderData()
	done2["code"], done2["status"] = done, "Completed"
	insert(done, "ProductionOrder", "Completed", done2)

	// A reason is required, and there must be a change to accept.
	if got := ackCode4956c(AcknowledgeBOMVariance(tenantID, order, "admin", "  ")); got != BOMVarianceReasonCode {
		t.Errorf("no reason: %q, want %s", got, BOMVarianceReasonCode)
	}
	if got := ackCode4956c(AcknowledgeBOMVariance(tenantID, order, "admin", reason)); got != BOMVarianceNoneCode {
		t.Errorf("BOM unchanged: %q, want %s", got, BOMVarianceNoneCode)
	}

	// The BOM changes: 2 -> 3 cloth, thread added.
	if _, err := db.DB.Exec(`UPDATE `+schema+`.documents SET data = jsonb_set(data, '{components}', to_jsonb($2::text)) WHERE id = $1`,
		bom, `[{"sku":"T4956C-CLOTH","qty":3},{"sku":"T4956C-THREAD","qty":1}]`); err != nil {
		t.Fatal(err)
	}
	if got := ackCode4956c(AcknowledgeBOMVariance(tenantID, done, "admin", reason)); got != BOMVarianceNoneCode {
		t.Errorf("a completed order: %q, want %s", got, BOMVarianceNoneCode)
	}
	ack, err := AcknowledgeBOMVariance(tenantID, order, "admin", reason)
	if err != nil {
		t.Fatalf("acknowledge: %v", err)
	}
	if ack.By != "admin" || ack.Reason != reason || ack.At == "" || len(ack.Variance) != 2 ||
		ack.Variance[0] != (BOMVarianceLine{Sku: "T4956C-CLOTH", Issued: 2, Now: 3}) || ack.Variance[1] != (BOMVarianceLine{Sku: "T4956C-THREAD", Issued: 0, Now: 1}) {
		t.Errorf("acknowledgement = %+v", ack)
	}
	stored, _, err := fetchDocData(tenantID, "ProductionOrder", order)
	if err != nil {
		t.Fatal(err)
	}
	rec, _ := stored["bom_variance_ack"].(map[string]interface{})
	if stored["bom_variance_acknowledged"] != true || rec == nil || rec["reason"] != reason || stored["quantity"] != float64(5) {
		t.Errorf("stored: acknowledged %v, record %v, quantity %v", stored["bom_variance_acknowledged"], rec, stored["quantity"])
	}
	var who, details string
	if err := db.DB.QueryRow(`SELECT user_id, details FROM `+schema+`.audit_logs WHERE action = 'PRODUCTION_ORDER_BOM_VARIANCE_ACK' AND details LIKE '%'||$1||'%' ORDER BY created_at DESC LIMIT 1`, order).Scan(&who, &details); err != nil {
		t.Fatalf("no audit row: %v", err)
	}
	if who != "admin" || !strings.Contains(details, reason) || !strings.Contains(details, "T4956C-CLOTH 2 -> 3") || !strings.Contains(details, "T4956C-THREAD added (1)") {
		t.Errorf("audit row by %q: %s", who, details)
	}

	// A save can send the keys back unchanged, but not set, change or clear them.
	if got := code(CheckActionOnlyFields(tenantID, "ProductionOrder", "", map[string]interface{}{"bom_variance_acknowledged": true})); got != ActionOnlyFieldCode {
		t.Errorf("a new order claiming an acknowledgement: %q", got)
	}
	if got := code(CheckActionOnlyFields(tenantID, "ProductionOrder", done, map[string]interface{}{"bom_variance_acknowledged": "true"})); got != ActionOnlyFieldCode {
		t.Errorf("setting it on a save: %q", got)
	}
	if got := code(CheckActionOnlyFields(tenantID, "ProductionOrder", order, map[string]interface{}{"bom_variance_acknowledged": false})); got != ActionOnlyFieldCode {
		t.Errorf("clearing it on a save: %q", got)
	}
	if got := code(CheckActionOnlyFields(tenantID, "ProductionOrder", order, map[string]interface{}{"bom_variance_ack": map[string]interface{}{"by": "someone else", "reason": "x"}})); got != ActionOnlyFieldCode {
		t.Errorf("rewriting who accepted it: %q", got)
	}
	resend := map[string]interface{}{"bom_variance_acknowledged": "true", "bom_variance_ack": stored["bom_variance_ack"], "quantity": 5}
	if err := CheckActionOnlyFields(tenantID, "ProductionOrder", order, resend); err != nil {
		t.Errorf("re-saving the record with the keys unchanged: %v", err)
	}
	if err := CheckActionOnlyFields(tenantID, "ProductionOrder", done, map[string]interface{}{"quantity": 5, "bom_variance_acknowledged": false}); err != nil {
		t.Errorf("an ordinary save of an order never acknowledged: %v", err)
	}

	// An import: a row may not set it, and a row that leaves it out keeps it.
	head := "id,code,bom_id,quantity,location,status,bom_variance_acknowledged\n"
	res, err := BulkImportCSV(tenantID, "ProductionOrder", strings.NewReader(head+done+","+done+","+bom+",5,HO,Completed,true\n"), "admin", "", true)
	if err != nil {
		t.Fatal(err)
	}
	if res.FailedRows != 1 || len(res.Errors) == 0 || !strings.Contains(res.Errors[0].Message, "bom_variance_acknowledged") {
		t.Errorf("an import row setting the acknowledgement: %d ok, %d failed %+v", res.SuccessRows, res.FailedRows, res.Errors)
	}
	res, err = BulkImportCSV(tenantID, "ProductionOrder", strings.NewReader("id,code,bom_id,quantity,location,status\n"+order+","+order+","+bom+",6,HO,Material Issued\n"), "admin", "", false)
	if err != nil {
		t.Fatal(err)
	}
	after, _, _ := fetchDocData(tenantID, "ProductionOrder", order)
	if res.SuccessRows != 1 || after["bom_variance_acknowledged"] != true || after["bom_variance_ack"] == nil {
		t.Errorf("an import update without the column: %d ok %+v; acknowledged %v, record %v", res.SuccessRows, res.Errors, after["bom_variance_acknowledged"], after["bom_variance_ack"])
	}
}

func ackCode4956c(_ *BOMVarianceAck, err error) string {
	var v *ValidationError
	if errors.As(err, &v) {
		return v.Code
	}
	return ""
}
