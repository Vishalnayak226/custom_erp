package engines

import "testing"

// Stage 36.5. Pure-function tests, deliberately no database: every function
// in pimTransformFunctions is a plain string transform with no tenant/DB
// dependency, the same reasoning currency_fx_test.go gives for keeping its
// sign-convention tests off a live rate table. The DB-backed seam
// (ApplyPIMTransformRule/fetchPIMTransformRule) and the two consumers
// (BuildChannelPayload, Stage 36.3's import path) are proven by the
// throwaway-server live HTTP pass, not here.

func TestPIMTransformFunctions(t *testing.T) {
	cases := []struct {
		name     string
		function string
		value    string
		operand1 string
		operand2 string
		want     string
		wantErr  bool
	}{
		{name: "trim", function: "trim", value: "  hello  ", want: "hello"},
		{name: "uppercase", function: "uppercase", value: "brand", want: "BRAND"},
		{name: "lowercase", function: "lowercase", value: "BRAND", want: "brand"},
		{name: "prefix", function: "prefix", value: "123", operand1: "SKU-", want: "SKU-123"},
		{name: "suffix", function: "suffix", value: "123", operand1: "-A", want: "123-A"},
		{name: "truncate shorter than value", function: "truncate", value: "hello world", operand1: "5", want: "hello"},
		{name: "truncate longer than value", function: "truncate", value: "hi", operand1: "5", want: "hi"},
		{name: "truncate bad operand", function: "truncate", value: "hi", operand1: "abc", wantErr: true},
		{name: "default_if_empty on blank", function: "default_if_empty", value: "", operand1: "N/A", want: "N/A"},
		{name: "default_if_empty on filled", function: "default_if_empty", value: "already set", operand1: "N/A", want: "already set"},
		{name: "find_replace_literal", function: "find_replace_literal", value: "a.b.c", operand1: ".", operand2: "-", want: "a-b-c"},
		{name: "find_replace_literal is not a regex", function: "find_replace_literal", value: "a.b.c", operand1: "[.]", operand2: "-", want: "a.b.c"},
		{name: "number_format rounds", function: "number_format", value: "12.3456", operand1: "2", want: "12.35"},
		{name: "number_format pads", function: "number_format", value: "12", operand1: "2", want: "12.00"},
		{name: "number_format non-numeric value", function: "number_format", value: "abc", operand1: "2", wantErr: true},
		{name: "date_format reformats", function: "date_format", value: "31/01/2026", operand1: "02/01/2006", operand2: "2006-01-02", want: "2026-01-31"},
		{name: "date_format blank passes through", function: "date_format", value: "", operand1: "02/01/2006", operand2: "2006-01-02", want: ""},
		{name: "date_format mismatched layout", function: "date_format", value: "2026-01-31", operand1: "02/01/2006", operand2: "2006-01-02", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fn, known := pimTransformFunctions[tc.function]
			if !known {
				t.Fatalf("function %q is not registered", tc.function)
			}
			got, err := fn.apply(tc.value, tc.operand1, tc.operand2)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got result %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestListPIMTransformFunctionsMatchesVocabulary(t *testing.T) {
	// Same guarantee 36.2.3 pinned for ListPIMWorkflowConditions: the
	// published list must be exactly what the engine implements, so a form
	// built from it can never offer a function that doesn't evaluate.
	info := ListPIMTransformFunctions()
	if len(info) != len(pimTransformFunctions) {
		t.Fatalf("ListPIMTransformFunctions returned %d entries, vocabulary has %d", len(info), len(pimTransformFunctions))
	}
	for _, entry := range info {
		if _, known := pimTransformFunctions[entry.Key]; !known {
			t.Fatalf("ListPIMTransformFunctions published unknown function %q", entry.Key)
		}
	}
}

func pimTransformStepsPayload(stepsJSON string) map[string]interface{} {
	return map[string]interface{}{"steps": stepsJSON}
}

func TestValidatePIMTransformRuleDocument(t *testing.T) {
	t.Run("refuses no steps", func(t *testing.T) {
		if err := ValidatePIMTransformRuleDocument("default", pimTransformStepsPayload(`[]`)); err == nil {
			t.Fatal("expected an error for an empty step list")
		}
	})
	t.Run("refuses an unknown function", func(t *testing.T) {
		payload := pimTransformStepsPayload(`[{"sequence":1,"function":"reverse_the_polarity"}]`)
		if err := ValidatePIMTransformRuleDocument("default", payload); err == nil {
			t.Fatal("expected an error for an unknown function")
		}
	})
	t.Run("refuses a missing required operand", func(t *testing.T) {
		payload := pimTransformStepsPayload(`[{"sequence":1,"function":"prefix"}]`)
		if err := ValidatePIMTransformRuleDocument("default", payload); err == nil {
			t.Fatal("expected an error for prefix with no operand1")
		}
	})
	t.Run("refuses a non-numeric truncate length", func(t *testing.T) {
		payload := pimTransformStepsPayload(`[{"sequence":1,"function":"truncate","operand1":"abc"}]`)
		if err := ValidatePIMTransformRuleDocument("default", payload); err == nil {
			t.Fatal("expected an error for a non-numeric truncate operand")
		}
	})
	t.Run("accepts a well-formed multi-step rule", func(t *testing.T) {
		payload := pimTransformStepsPayload(`[
			{"sequence":1,"function":"trim"},
			{"sequence":2,"function":"uppercase"},
			{"sequence":3,"function":"prefix","operand1":"SKU-"}]`)
		if err := ValidatePIMTransformRuleDocument("default", payload); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}
