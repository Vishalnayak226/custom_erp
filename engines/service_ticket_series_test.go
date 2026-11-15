package engines

import (
	"os"
	"strings"
	"testing"

	"custom_erp/db"
)

// 57.26(b) / 178.2: a Service Ticket takes its number from the TKT series,
// whether it is raised on the record form or by the service desk engine, and
// its id stays internal so the ticket's action routes keep working.
func TestServiceTicketNumberComesFromItsSeries(t *testing.T) {
	db.InitDB(testConnStr())
	sqlText, err := os.ReadFile("../db/migrations/migrations_stage178_2_service_ticket_series.sql")
	if err != nil {
		t.Fatal(err)
	}
	for run := 0; run < 2; run++ { // re-running changes nothing
		if _, err := db.DB.Exec(string(sqlText)); err != nil {
			t.Fatalf("migration run %d: %v", run+1, err)
		}
	}
	var rows int
	db.DB.QueryRow(`SELECT COUNT(*) FROM tenant_default.prefix_configs WHERE doc_type = 'TKT'`).Scan(&rows)
	if rows != 1 {
		t.Fatalf("TKT series rows = %d, want 1", rows)
	}

	// Record form: the number is filled, the id is left to the create.
	payload := map[string]interface{}{"customer": "C-1", "description": "noisy compressor"}
	if err := PrepareDocumentNumber("default", "HO", "ServiceTicket", true, payload); err != nil {
		t.Fatal(err)
	}
	if code, _ := payload["code"].(string); !strings.HasPrefix(code, "TKT/") {
		t.Errorf("form code = %q, want a TKT/ number", payload["code"])
	}
	if _, has := payload["id"]; has {
		t.Errorf("form create was given id %v; a ticket's id stays internal", payload["id"])
	}

	// Engine create: same series, internal id.
	id, err := CreateServiceTicket("default", "C-1", "noisy compressor", "Medium", "", "", "", "", "admin")
	if err != nil {
		t.Fatal(err)
	}
	defer db.DB.Exec(`DELETE FROM tenant_default.documents WHERE doctype = 'ServiceTicket' AND id = $1`, id)
	if !strings.HasPrefix(id, "SVC-") {
		t.Errorf("ticket id = %q, want the internal SVC- id", id)
	}
	if n := DocumentNumber("default", "ServiceTicket", id); !strings.HasPrefix(n, "TKT/") {
		t.Errorf("ticket number = %q, want a TKT/ number", n)
	}
}

// 57.26(d): a number drawn for a save that is then refused goes back, so the
// next document takes it; only the latest number of its bucket can go back.
func TestRefusedSaveGivesItsNumberBack(t *testing.T) {
	db.InitDB(testConnStr())
	draw := func() (string, *NumberDraw) {
		t.Helper()
		p := map[string]interface{}{"customer": "C-1", "description": "x"}
		d, err := DrawDocumentNumber("default", "HO", "ServiceTicket", true, p)
		if err != nil || d == nil {
			t.Fatalf("draw: %v %v", d, err)
		}
		return p["code"].(string), d
	}
	first, d1 := draw()
	d1.Release()
	again, d2 := draw()
	if again != first {
		t.Errorf("after a release the next number is %q, want %q back", again, first)
	}
	// An older draw cannot go back once a newer one exists: the gap stays
	// rather than two documents sharing a number.
	newer, d3 := draw()
	d2.Release()
	d3.Release()
	if next, d4 := draw(); next != newer {
		t.Errorf("next number %q, want %q (only the latest goes back)", next, newer)
	} else {
		d4.Release()
	}
	var nilDraw *NumberDraw
	nilDraw.Release() // a doctype with no series draws nothing; releasing it is a no-op
}
