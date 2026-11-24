package engines

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"custom_erp/db"
)

func printerHealState(t *testing.T, tenant string) HealState {
	t.Helper()
	list, err := ListHeals(tenant)
	if err != nil {
		t.Fatalf("ListHeals: %v", err)
	}
	for _, h := range list {
		if h.ID == healPrinterName {
			return h
		}
	}
	t.Fatal("the printer-name heal is not listed")
	return HealState{}
}

func printerDataOf(t *testing.T, schema, id string) map[string]interface{} {
	t.Helper()
	var raw string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT data::text FROM %s.documents WHERE id = $1`, schema), id).Scan(&raw); err != nil {
		t.Fatalf("read %s: %v", id, err)
	}
	var d map[string]interface{}
	_ = json.Unmarshal([]byte(raw), &d)
	return d
}

// TestPrinterJSONKeepsLegacyKey: a page loaded before ask 263 reads
// qz_printer_name, so a printer's JSON carries the name under both keys.
func TestPrinterJSONKeepsLegacyKey(t *testing.T) {
	raw, err := json.Marshal(AgentPrinter{ID: "P1", OSPrinterName: "Zebra ZD230"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"os_printer_name":"Zebra ZD230"`, `"qz_printer_name":"Zebra ZD230"`, `"id":"P1"`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("printer JSON %s lacks %s", raw, want)
		}
	}
}

func TestDataHealPrinterNameLifecycle(t *testing.T) {
	tenant, schema := healTestTenant(t)
	// Saved before ask 263: only the old key.
	healSeed(t, schema, "Printer", "HEAL-T-PRN1", map[string]interface{}{"code": "PRN1", "name": "Label", "status": "Active", "qz_printer_name": "Zebra ZD230"})
	// Both keys: the new one wins.
	healSeed(t, schema, "Printer", "HEAL-T-PRN2", map[string]interface{}{"code": "PRN2", "name": "Receipt", "status": "Active", "qz_printer_name": "Old TVS", "os_printer_name": "TVS RP3160"})
	// Already on the new key: not part of the heal.
	healSeed(t, schema, "Printer", "HEAL-T-PRN3", map[string]interface{}{"code": "PRN3", "name": "A4", "status": "Active", "os_printer_name": "Epson L3150"})

	st := printerHealState(t, tenant)
	if st.Status != HealStatusWaiting || st.Count != 2 || len(st.Doctypes) != 1 || st.Doctypes[0] != "Printer" {
		t.Fatalf("want the heal waiting on 2 printers and holding the Printer form, got %+v", st)
	}

	// Before the heal, printing still finds every printer's OS name.
	printers, err := ListAgentPrinters(tenant)
	if err != nil {
		t.Fatalf("ListAgentPrinters: %v", err)
	}
	got := map[string]string{}
	for _, p := range printers {
		got[p.ID] = p.OSPrinterName
	}
	if got["HEAL-T-PRN1"] != "Zebra ZD230" || got["HEAL-T-PRN2"] != "TVS RP3160" || got["HEAL-T-PRN3"] != "Epson L3150" {
		t.Fatalf("printers read before the heal: %v", got)
	}

	_, recs, err := HealRecords(tenant, healPrinterName)
	if err != nil {
		t.Fatalf("HealRecords: %v", err)
	}
	if len(recs) != 2 || recs[0].ID != "HEAL-T-PRN1" || recs[0].Current != "Zebra ZD230" || recs[0].Proposed != "Zebra ZD230" ||
		recs[1].Current != "Old TVS" || recs[1].Proposed != "TVS RP3160" {
		t.Fatalf("records: %+v", recs)
	}

	// A typed name that is too long refuses the run and changes nothing.
	if _, err := RunHeal(tenant, healPrinterName, "tester", map[string]string{"Printer/HEAL-T-PRN1": strings.Repeat("x", 256)}); err == nil {
		t.Fatal("a 256-character printer name was accepted")
	}
	if d := printerDataOf(t, schema, "HEAL-T-PRN1"); d["qz_printer_name"] != "Zebra ZD230" || d["os_printer_name"] != nil {
		t.Fatalf("a refused run changed the record: %v", d)
	}

	res, err := RunHeal(tenant, healPrinterName, "tester", map[string]string{"Printer/HEAL-T-PRN1": "Zebra ZD230 (USB)"})
	if err != nil {
		t.Fatalf("RunHeal: %v", err)
	}
	if res.Fixed != 2 || res.Typed != 1 || res.AlreadyDone {
		t.Fatalf("run: %+v", res)
	}
	for id, want := range map[string]string{"HEAL-T-PRN1": "Zebra ZD230 (USB)", "HEAL-T-PRN2": "TVS RP3160", "HEAL-T-PRN3": "Epson L3150"} {
		d := printerDataOf(t, schema, id)
		if d["os_printer_name"] != want {
			t.Errorf("%s os_printer_name = %v, want %q", id, d["os_printer_name"], want)
		}
		if _, left := d["qz_printer_name"]; left {
			t.Errorf("%s still holds qz_printer_name: %v", id, d)
		}
	}
	if st := printerHealState(t, tenant); st.Status != HealStatusDone || st.Fixed != 2 || st.Typed != 1 || st.DoneBy != "tester" {
		t.Fatalf("after the run: %+v", st)
	}
	again, err := RunHeal(tenant, healPrinterName, "tester", nil)
	if err != nil || !again.AlreadyDone {
		t.Fatalf("a second run: %+v %v", again, err)
	}
}
