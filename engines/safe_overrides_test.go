package engines

import (
	"custom_erp/db"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// Stage 49.5.6: a stock line that arrives with "allow_negative": true - from
// a stored document's lines or a client's body - cannot lift the floor check.
// Only markNegativeStockAllowed's server mark (the product type's setting)
// can, and that mark still serialises as the same true.
func TestStage4956ClientAllowNegativeCannotLiftTheFloor(t *testing.T) {
	var lines []interface{}
	sku := "SKU4956-" + stage4911Suffix()
	if err := json.Unmarshal([]byte(`[{"sku":"`+sku+`","qty":-1,"allow_negative":true}]`), &lines); err != nil {
		t.Fatal(err)
	}
	line := lines[0].(map[string]interface{})
	if lineMarkedNegativeAllowed(line) {
		t.Fatal("a JSON-decoded allow_negative counted as the server's mark")
	}
	marked := map[string]interface{}{"sku": sku, "qty": -1.0, "allow_negative": negativeStockMark(true)}
	if !lineMarkedNegativeAllowed(marked) {
		t.Fatal("the server's own mark was not recognised")
	}
	if b, _ := json.Marshal(marked); !strings.Contains(string(b), `"allow_negative":true`) {
		t.Fatalf("the mark changed the stored shape: %s", b)
	}

	db.InitDB(testConnStr())
	_, err := PostInventoryLedger("default", "LOC4956-"+stage4911Suffix(), lines, false)
	var short *InsufficientStockError
	if !errors.As(err, &short) {
		t.Fatalf("a client-flagged line below zero posted (err %v); want InsufficientStockError", err)
	}
}
