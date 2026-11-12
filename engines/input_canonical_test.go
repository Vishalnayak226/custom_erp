package engines

import (
	"custom_erp/db"
	"testing"
)

// Stage 49.4.6: a saved value may not carry an invisible control character or
// a text-direction override; tabs, line breaks and any language's letters are
// fine.
func TestStage49UnsafeCharactersAreRefusedAtSave(t *testing.T) {
	for s, bad := range map[string]bool{
		"Kurta Blue":            false,
		"Line one\nline two\t3": false,
		"साड़ी लाल":             false,
		"فستان":                 false,
		"INV\u202E-01":          true, // right-to-left override
		"A\u2066B":              true, // left-to-right isolate
		"NUL\x00":               true, // C0 control
		"DEL\x7f":               true,
		"C1\u0085":              true,
	} {
		if _, got := unsafeTextRune(s); got != bad {
			t.Errorf("%q: refused=%v, want %v", s, got, bad)
		}
	}

	db.InitDB(testConnStr())
	err := ValidateDocument("default", "Department", map[string]interface{}{"code": "D-49-4-6", "name": "Sales\u202Eselas", "status": "Active"})
	if verr, ok := err.(*ValidationError); !ok || verr.Code != "GLOBAL-0309" {
		t.Fatalf("a name with a direction override must be refused with GLOBAL-0309, got %v", err)
	}
	if err := ValidateDocument("default", "Department", map[string]interface{}{"code": "D-49-4-6", "name": "Sales North", "status": "Active"}); err != nil {
		t.Fatalf("a plain name must pass: %v", err)
	}
}
