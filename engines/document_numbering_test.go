package engines

import (
	"custom_erp/db"
	"strings"
	"testing"
	"time"
)

// TestSequencePeriod pins the reset_frequency -> (counter bucket, visible
// segment) mapping. The two are returned together deliberately: a series that
// resets on a period it does not display re-issues the same number when that
// period rolls over, and a generated number is also the document id, so that
// is a rejected save rather than a cosmetic problem.
func TestSequencePeriod(t *testing.T) {
	// 2026-07-31 falls in the 26-27 Indian financial year (April-March).
	now := time.Date(2026, time.July, 31, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name        string
		resetFreq   string
		wantBucket  string
		wantSegment string
	}{
		{"annual", "ANNUAL", "26-27", "26-27"},
		{"monthly", "MONTHLY", "26-27-07", "26-27-07"},
		{"never has no visible period", "NEVER", counterWildcard, ""},
		{"lowercase is accepted", "annual", "26-27", "26-27"},
		{"unknown value falls back to annual", "QUARTERLY", "26-27", "26-27"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bucket, segment := sequencePeriod(tc.resetFreq, "26-27", now)
			if bucket != tc.wantBucket {
				t.Errorf("bucket: want %q, got %q", tc.wantBucket, bucket)
			}
			if segment != tc.wantSegment {
				t.Errorf("segment: want %q, got %q", tc.wantSegment, segment)
			}
		})
	}
}

func TestDocumentFinancialYear(t *testing.T) {
	cases := []struct {
		date time.Time
		want string
	}{
		// April starts a new financial year.
		{time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC), "26-27"},
		{time.Date(2026, time.July, 31, 0, 0, 0, 0, time.UTC), "26-27"},
		{time.Date(2027, time.March, 31, 0, 0, 0, 0, time.UTC), "26-27"},
		// March 31 -> April 1 rolls the year over.
		{time.Date(2027, time.April, 1, 0, 0, 0, 0, time.UTC), "27-28"},
		// January is still the previous April's year.
		{time.Date(2027, time.January, 15, 0, 0, 0, 0, time.UTC), "26-27"},
	}
	for _, tc := range cases {
		if got := documentFinancialYear(tc.date); got != tc.want {
			t.Errorf("%s: want %q, got %q", tc.date.Format("2006-01-02"), tc.want, got)
		}
	}
}

// TestGenerateSequenceSegments covers the two segment-dropping paths that did
// not exist before Stage 30.6, including the collision each one has to avoid.
func TestGenerateSequenceSegments(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("Failed to get tenant schema: %v", err)
	}

	// Fixtures are cleared before asserting rather than after, matching this
	// package's existing convention - the dev database is shared, so debris
	// from an earlier interrupted run must not be inherited.
	setup := func(t *testing.T, docType, resetFreq string, includeStore bool) {
		t.Helper()
		db.DB.Exec("DELETE FROM "+schema+".prefix_configs WHERE doc_type = $1", docType)
		db.DB.Exec("DELETE FROM "+schema+".sequence_counters WHERE doc_type = $1", docType)
		if _, err := db.DB.Exec(`
			INSERT INTO `+schema+`.prefix_configs (doc_type, prefix, separator, padding_width, reset_frequency, include_store)
			VALUES ($1, $2, '/', 4, $3, $4)`, docType, docType, resetFreq, includeStore); err != nil {
			t.Fatalf("Failed to insert prefix config: %v", err)
		}
		t.Cleanup(func() {
			db.DB.Exec("DELETE FROM "+schema+".prefix_configs WHERE doc_type = $1", docType)
			db.DB.Exec("DELETE FROM "+schema+".sequence_counters WHERE doc_type = $1", docType)
		})
	}

	t.Run("include_store false shares one counter across stores", func(t *testing.T) {
		docType := "TEST_NOSTORE"
		setup(t, docType, "ANNUAL", false)

		// Two different stores. With the store segment dropped from the
		// format, they must also share a counter - counting per-store while
		// displaying no store would hand both of them "TEST_NOSTORE/26-27/0001".
		first, err := GenerateSequence(tenantID, docType, "HQ", "26-27")
		if err != nil {
			t.Fatalf("first: %v", err)
		}
		second, err := GenerateSequence(tenantID, docType, "STORE2", "26-27")
		if err != nil {
			t.Fatalf("second: %v", err)
		}

		if want := docType + "/26-27/0001"; first != want {
			t.Errorf("first: want %q, got %q", want, first)
		}
		if want := docType + "/26-27/0002"; second != want {
			t.Errorf("second: want %q, got %q - a second store must not restart the counter when the store segment is hidden", want, second)
		}
	})

	t.Run("reset NEVER omits the period and does not reset across years", func(t *testing.T) {
		docType := "TEST_NEVER"
		setup(t, docType, "NEVER", true)

		first, err := GenerateSequence(tenantID, docType, "HQ", "26-27")
		if err != nil {
			t.Fatalf("first: %v", err)
		}
		// A later financial year must continue the same series, not restart it.
		second, err := GenerateSequence(tenantID, docType, "HQ", "27-28")
		if err != nil {
			t.Fatalf("second: %v", err)
		}

		if want := docType + "/HQ/0001"; first != want {
			t.Errorf("first: want %q, got %q", want, first)
		}
		if want := docType + "/HQ/0002"; second != want {
			t.Errorf("second: want %q, got %q - NEVER must not reset on a year change", want, second)
		}
	})
}

// TestPrepareDocumentNumber covers the contract the create choke point relies
// on: the number is always server-issued, always lands on every field that
// doctype declares mandatory, and never touches an update.
func TestPrepareDocumentNumber(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"

	t.Run("fills id and every declared number field", func(t *testing.T) {
		payload := map[string]interface{}{"vendor": "V1"}
		if err := PrepareDocumentNumber(tenantID, "HQ", "PurchaseOrder", true, payload); err != nil {
			t.Fatalf("PrepareDocumentNumber: %v", err)
		}
		id, _ := payload["id"].(string)
		if id == "" {
			t.Fatal("id was not set")
		}
		if !strings.HasPrefix(id, "PO/") {
			t.Errorf("want a PO-series number, got %q", id)
		}
		// PurchaseOrder carries two overlapping mandatory number fields from
		// this project's history; both must be filled or ValidateDocument
		// rejects the save.
		if payload["po_number"] != id {
			t.Errorf("po_number: want %q, got %v", id, payload["po_number"])
		}
		if payload["code"] != id {
			t.Errorf("code: want %q, got %v", id, payload["code"])
		}
	})

	t.Run("leaves an explicit id alone - that is this API's upsert form", func(t *testing.T) {
		// Posting an id to the collection route addresses an existing
		// document (handleGenericDoc resolves docID from payload["id"] when
		// the path has none). Numbering it would turn an update into a second
		// document. No create screen sends an id any more, so this path is
		// reachable only by a deliberate API caller.
		payload := map[string]interface{}{"id": "PO/EXISTING-DOC", "code": "PO/EXISTING-DOC"}
		if err := PrepareDocumentNumber(tenantID, "HQ", "PurchaseOrder", true, payload); err != nil {
			t.Fatalf("PrepareDocumentNumber: %v", err)
		}
		if payload["id"] != "PO/EXISTING-DOC" {
			t.Errorf("an explicitly addressed document must keep its id, got %v", payload["id"])
		}
	})

	t.Run("an empty id is still a create", func(t *testing.T) {
		// A form that sends "id": "" must not be mistaken for an upsert.
		payload := map[string]interface{}{"id": "  ", "vendor": "V1"}
		if err := PrepareDocumentNumber(tenantID, "HQ", "PurchaseOrder", true, payload); err != nil {
			t.Fatalf("PrepareDocumentNumber: %v", err)
		}
		id, _ := payload["id"].(string)
		if !strings.HasPrefix(id, "PO/") {
			t.Errorf("a blank id must be replaced by a series number, got %q", id)
		}
	})

	t.Run("leaves ASN's po_number reference alone", func(t *testing.T) {
		// ASN's po_number holds the *referenced PO's* number, not the ASN's
		// own - overwriting it would silently detach every ASN from its order.
		payload := map[string]interface{}{"po_number": "PO/HQ/26-27/000123", "po_id": "PO/HQ/26-27/000123"}
		if err := PrepareDocumentNumber(tenantID, "HQ", "ASN", true, payload); err != nil {
			t.Fatalf("PrepareDocumentNumber: %v", err)
		}
		if payload["po_number"] != "PO/HQ/26-27/000123" {
			t.Errorf("po_number must stay the PO reference, got %v", payload["po_number"])
		}
		asnNumber, _ := payload["asn_number"].(string)
		if !strings.HasPrefix(asnNumber, "ASN/") {
			t.Errorf("asn_number: want an ASN-series number, got %q", asnNumber)
		}
	})

	t.Run("no-op on update", func(t *testing.T) {
		payload := map[string]interface{}{"code": "PO/HQ/26-27/000001"}
		if err := PrepareDocumentNumber(tenantID, "HQ", "PurchaseOrder", false, payload); err != nil {
			t.Fatalf("PrepareDocumentNumber: %v", err)
		}
		if _, exists := payload["id"]; exists {
			t.Error("an update must not be assigned a new number")
		}
		if payload["code"] != "PO/HQ/26-27/000001" {
			t.Errorf("an update must keep its existing number, got %v", payload["code"])
		}
	})

	t.Run("no-op for a doctype with no series", func(t *testing.T) {
		payload := map[string]interface{}{"code": "ITEM-1"}
		if err := PrepareDocumentNumber(tenantID, "HQ", "Item", true, payload); err != nil {
			t.Fatalf("PrepareDocumentNumber: %v", err)
		}
		if _, exists := payload["id"]; exists {
			t.Error("Item has no registered series and must be left untouched")
		}
	})
}

// TestDocumentNumberSeriesKeysAreDistinct guards the invariant that makes a
// generated number safe to use as the document id: documents.id is unique
// across every doctype, not per-doctype, so two series sharing a prefix would
// eventually collide and turn one doctype's create into an overwrite of
// another's.
func TestDocumentNumberSeriesKeysAreDistinct(t *testing.T) {
	seen := map[string]string{}
	for doctype, series := range documentNumberSeriesByDoctype {
		if other, dup := seen[series.SeriesKey]; dup {
			t.Errorf("series key %q is used by both %s and %s", series.SeriesKey, other, doctype)
		}
		seen[series.SeriesKey] = doctype

		if len(series.NumberFields) == 0 {
			t.Errorf("%s declares no number field, so its number would never be stored", doctype)
		}
	}
}
