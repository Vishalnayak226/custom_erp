package engines

import (
	"custom_erp/db"
	"strings"
	"testing"
)

func TestStage14634ExpandBinRange(t *testing.T) {
	cases := map[string]string{
		"A-C":       "A,B,C",
		"1-3":       "1,2,3",
		"01-03":     "01,02,03",
		"A,C":       "A,C",
		"R1,R2-R2":  "R1,R2-R2", // not a numeric / letter pair: kept literal
		"":          "",
		"3-1":       "",
		"A-B, 5-6":  "A,B,5,6",
		"09-11":     "09,10,11",
		" 2 - 3 ":   "2,3",
		"AA-AC":     "AA-AC",
		"a-c":       "a,b,c",
		"001-002,9": "001,002,9",
	}
	for in, want := range cases {
		got, err := expandBinRange(in)
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		if strings.Join(got, ",") != want {
			t.Errorf("expandBinRange(%q) = %q, want %q", in, strings.Join(got, ","), want)
		}
	}
	if _, err := expandBinRange("1-100000"); err == nil {
		t.Errorf("a huge range must be refused")
	}
}

func TestStage14634GenerateBins(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, _ := db.GetTenantSchema(tenantID)
	const loc = "T14634-LOC"
	cleanup := func() {
		db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'Bin' AND data->>'location' = $1", loc)
		db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id LIKE 'T14634-%'")
	}
	cleanup()
	defer cleanup()
	t14631Doc(t, schema, loc, "Location", "Active", map[string]interface{}{"code": loc, "name": "T14634", "status": "Active"})
	// An unknown location is refused.
	if _, err := GenerateBins(tenantID, BinGenInput{Location: "T14634-NOPE", Aisles: "A", Bins: "1", Preview: true}); err == nil {
		t.Fatalf("unknown location must be refused")
	}
	in := BinGenInput{Location: loc, Aisles: "A-B", Racks: "01-02", Shelves: "1-2", Bins: "1-3", UserID: "system", Preview: true}
	prev, err := GenerateBins(tenantID, in)
	if err != nil {
		t.Fatal(err)
	}
	if prev.Total != 24 || prev.Created != 0 || prev.Existing != 0 || prev.Rows[0].Code != "A-01-1-1" {
		t.Fatalf("preview = total %d created %d existing %d first %q", prev.Total, prev.Created, prev.Existing, prev.Rows[0].Code)
	}
	var n int
	db.DB.QueryRow("SELECT COUNT(*) FROM "+schema+".documents WHERE doctype = 'Bin' AND data->>'location' = $1", loc).Scan(&n)
	if n != 0 {
		t.Fatalf("a preview created %d bins", n)
	}
	in.Preview = false
	res, err := GenerateBins(tenantID, in)
	if err != nil || res.Created != 24 {
		t.Fatalf("generate: %+v %v", res, err)
	}
	var level, pos, aisle, status string
	db.DB.QueryRow("SELECT data->>'level', data->>'position', data->>'aisle', status FROM "+schema+".documents WHERE doctype = 'Bin' AND data->>'bin_code' = 'B-02-2-3'").Scan(&level, &pos, &aisle, &status)
	if level != "2" || pos != "3" || aisle != "B" || status != "Active" {
		t.Fatalf("B-02-2-3 = level %q position %q aisle %q status %q", level, pos, aisle, status)
	}
	// Re-running creates nothing twice; a wider range adds only the new ones.
	again, err := GenerateBins(tenantID, in)
	if err != nil || again.Created != 0 || again.Existing != 24 {
		t.Fatalf("rerun: %+v %v", again, err)
	}
	in.Bins = "1-4"
	wider, _ := GenerateBins(tenantID, in)
	if wider.Created != 8 || wider.Existing != 24 {
		t.Fatalf("wider: created %d existing %d, want 8 / 24", wider.Created, wider.Existing)
	}
	// A custom format.
	custom, err := GenerateBins(tenantID, BinGenInput{Location: loc, Format: "{aisle}{rack}.{bin}", Aisles: "C", Racks: "9", Bins: "1-2", UserID: "system"})
	if err != nil || custom.Created != 2 || custom.Rows[0].Code != "C9.1" {
		t.Fatalf("custom format: %+v %v", custom, err)
	}
	// A code already used at ANOTHER location is not recreated or stolen.
	t14631Doc(t, schema, "T14634-OTHER", "Bin", "Active", map[string]interface{}{"bin_code": "Z-1", "location": "T14634-ELSEWHERE", "status": "Active"})
	clash, err := GenerateBins(tenantID, BinGenInput{Location: loc, Aisles: "Z", Bins: "1", UserID: "system"})
	if err != nil || clash.Created != 0 || clash.Existing != 1 {
		t.Fatalf("clash: %+v %v", clash, err)
	}
}

func TestStage14634PickSequence(t *testing.T) {
	a := binWalk{Seq: 2, Zone: "Z1", Aisle: "A", Rack: "1", Bin: "B9"}
	b := binWalk{Seq: 1, Zone: "Z9", Aisle: "Z", Rack: "9", Bin: "B1"}
	if !walkBefore(b, a) || walkBefore(a, b) {
		t.Fatalf("a lower pick sequence must walk first, whatever the zone")
	}
	// Nothing set: exactly today's zone / aisle / rack / bin order.
	x := binWalk{Seq: noPickSeq, Zone: "Z1", Aisle: "B", Rack: "1", Bin: "B1"}
	y := binWalk{Seq: noPickSeq, Zone: "Z1", Aisle: "A", Rack: "9", Bin: "B2"}
	if !walkBefore(y, x) || walkBefore(x, y) {
		t.Fatalf("with no sequence the order is zone, aisle, rack, bin")
	}
	// A bin with a sequence walks before one without.
	if !walkBefore(a, x) {
		t.Fatalf("a sequenced bin walks before an unsequenced one")
	}

	// The reservation read carries the bin's sequence.
	db.InitDB(testConnStr())
	schema, _ := db.GetTenantSchema("default")
	cleanup := func() {
		db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id LIKE 'T146342-%'")
		db.DB.Exec("DELETE FROM " + schema + ".bin_allocation WHERE task_id = 'T146342-TASK'")
	}
	cleanup()
	defer cleanup()
	t14631Doc(t, schema, "T146342-B1", "Bin", "Active", map[string]interface{}{"bin_code": "T146342-B1", "location": "T146342-L", "pick_sequence": 7})
	t14631Doc(t, schema, "T146342-B2", "Bin", "Active", map[string]interface{}{"bin_code": "T146342-B2", "location": "T146342-L"})
	for _, bc := range []string{"T146342-B1", "T146342-B2"} {
		if _, err := db.DB.Exec("INSERT INTO "+schema+".bin_allocation (task_id, sku, location_code, bin_code, qty) VALUES ('T146342-TASK', 'T146342-S', 'T146342-L', $1, 2)", bc); err != nil {
			t.Fatal(err)
		}
	}
	lines, err := openTaskAllocations(schema, []string{"T146342-TASK"})
	if err != nil || len(lines) != 2 {
		t.Fatalf("allocations: %v %v", lines, err)
	}
	got := map[string]int{}
	for _, l := range lines {
		got[l.BinCode] = l.PickSeq
	}
	if got["T146342-B1"] != 7 || got["T146342-B2"] != noPickSeq {
		t.Fatalf("pick sequences = %v", got)
	}
}
