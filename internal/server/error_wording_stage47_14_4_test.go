package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sort"
	"strings"
	"testing"

	"custom_erp/db"
)

// floorModule reports whether a catalog module is on the floor path: the
// till, returns, stock, transfers, receiving, the warehouse and its devices.
func floorModule(module string) bool {
	for _, m := range []string{"POS", "Sales / POS", "Sales Return", "Inventory", "Stock Transfer", "Goods Receipt", "WMS", "Mobile App / Device"} {
		if strings.Contains(module, m) {
			return true
		}
	}
	return false
}

var (
	slashAlternatives = regexp.MustCompile(`\b[A-Za-z]+/[A-Za-z]+\b`)
	systemWords       = regexp.MustCompile(`(?i)\b(doctype|payload|api|null|sql|json|http|uuid|stack trace|stage \d)`)
)

// Stage 47.14.4: every floor refusal says what to do next - specifically
// (not a sentence shared across unrelated errors), in plain words, with no
// slashed alternatives or system vocabulary, and with at most one value
// slot so it translates as whole sentences.
func TestStage47144FloorErrorsSayWhatToDo(t *testing.T) {
	sharedBy := map[string][]string{}
	floor := 0
	for code, e := range errorCatalog {
		if !floorModule(e.Module) || e.HTTPStatus < 400 {
			continue
		}
		floor++
		action := strings.TrimSpace(e.UserAction)
		if action == "" {
			t.Errorf("%s (%s): no next action", code, e.Module)
			continue
		}
		sharedBy[action] = append(sharedBy[action], code)
		if !strings.HasSuffix(action, ".") {
			t.Errorf("%s: the action is not a finished sentence: %q", code, action)
		}
		if len(action) > 260 {
			t.Errorf("%s: the action is %d characters - too long to read at a till: %q", code, len(action), action)
		}
		for _, text := range []string{e.UserMessage, action} {
			if m := slashAlternatives.FindString(text); m != "" {
				t.Errorf("%s: %q packs alternatives into %q - write them out", code, text, m)
			}
			if m := systemWords.FindString(text); m != "" {
				t.Errorf("%s: %q uses the system word %q", code, text, m)
			}
		}
		if n := len(placeholderRe.FindAllString(e.UserMessage, -1)); n > 1 {
			t.Errorf("%s: %d value slots in one message cannot be translated as a sentence", code, n)
		}
		if placeholderRe.MatchString(action) {
			t.Errorf("%s: the action has a value slot nothing fills: %q", code, action)
		}
	}
	if floor < 100 {
		t.Fatalf("only %d floor codes found - the module names changed", floor)
	}
	var shared []string
	for action, codes := range sharedBy {
		if len(codes) > 2 {
			sort.Strings(codes)
			shared = append(shared, strings.Join(codes, ", ")+" all say "+action)
		}
	}
	sort.Strings(shared)
	for _, s := range shared {
		t.Errorf("one action for unrelated errors is not an action: %s", s)
	}
}

// Stage 47.14.4: the envelope says whether a refused change saved anything,
// and never shows a raw value slot.
func TestStage47144EnvelopeSaysWhatWasSaved(t *testing.T) {
	db.InitDB(testConnStr())
	decode := func(rec *httptest.ResponseRecorder) map[string]interface{} {
		var body map[string]interface{}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("not JSON: %q", rec.Body.String())
		}
		return body
	}

	// A refused change: nothing was saved, and the reader is told so.
	rec := httptest.NewRecorder()
	writeAPIError(rec, httptest.NewRequest(http.MethodPost, "/api/v1/pos/checkout", nil), "SALESP-0122", "")
	body := decode(rec)
	if body["saved"] != "no" || body["saved_note"] != savedNotes["no"] {
		t.Fatalf("a refused POST: saved=%v note=%v", body["saved"], body["saved_note"])
	}
	if body["user_action"] != "Lower the price to the MRP or below." {
		t.Fatalf("SALESP-0122 action = %v - the floor wording overlay did not apply", body["user_action"])
	}

	// A failure part-way: the server does not claim it saved nothing.
	rec = httptest.NewRecorder()
	writeAPIErrorGeneric(rec, httptest.NewRequest(http.MethodPost, "/api/v1/pos/checkout", nil), http.StatusInternalServerError, "pq: connection reset")
	if body = decode(rec); body["saved"] != "unknown" || !strings.Contains(body["saved_note"].(string), "Check before you try again") {
		t.Fatalf("a 5xx POST: saved=%v note=%v", body["saved"], body["saved_note"])
	}

	// A read has nothing to save, and a success note is not a refusal.
	rec = httptest.NewRecorder()
	writeAPIError(rec, httptest.NewRequest(http.MethodGet, "/api/v1/doc/Item/nope", nil), "GLOBAL-0004", "")
	if _, present := decode(rec)["saved"]; present {
		t.Fatal("a refused read carries a saved marker")
	}
	rec = httptest.NewRecorder()
	writeAPIError(rec, httptest.NewRequest(http.MethodPost, "/api/v1/doc/GRN", nil), "GOODSR-0180", "")
	if _, present := decode(rec)["saved"]; present {
		t.Fatal("a 200 note carries a saved marker")
	}

	// GLOBAL-0002 with no field named (57 engine refusals) used to read
	// "Invalid format for {field name}."
	rec = httptest.NewRecorder()
	writeAPIErrorDetail(rec, httptest.NewRequest(http.MethodPost, "/api/v1/doc/Item", nil), "GLOBAL-0002", "", "a role-template migration must record who approved it")
	if headline := decode(rec)["error"].(string); strings.ContainsAny(headline, "{}") || !strings.Contains(headline, "this field") {
		t.Fatalf("headline = %q", headline)
	}
	// ...and a named field still fills the slot.
	if got := fillPlaceholders("Invalid format for {field name}. Please enter a valid value.", "GSTIN"); got != "Invalid format for GSTIN. Please enter a valid value." {
		t.Fatalf("fillPlaceholders = %q", got)
	}
	// No catalog row is left with a slot the fallback does not know.
	for code, e := range errorCatalog {
		for _, p := range placeholderRe.FindAllString(e.UserMessage, -1) {
			if _, ok := placeholderFallbacks[p]; !ok {
				t.Errorf("%s: value slot %s has no plain fallback in placeholderFallbacks", code, p)
			}
		}
	}
}
