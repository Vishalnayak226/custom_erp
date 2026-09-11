package engines

import (
	"strings"
	"testing"
)

func TestRedactForTelemetryMasksSecretShapedKeys(t *testing.T) {
	in := map[string]interface{}{
		"user_id":         "u-42",
		"bearer_token":    "eyJhbGciOi...real-jwt-here",
		"webhook_secret":  "whsec_abcdef123456",
		"session_cookie":  "sid=abc123",
		"mfa_code":        "123456",
		"password_hash":   "$2a$10$...",
		"api_key":         "sk_live_abcdef",
		"ordinary_amount": 199.5,
	}
	out := RedactForTelemetry("", in)

	for _, secretKey := range []string{"bearer_token", "webhook_secret", "session_cookie", "mfa_code", "password_hash", "api_key"} {
		if out[secretKey] != redactedMarker {
			t.Errorf("%s should be redacted, got %v", secretKey, out[secretKey])
		}
	}
	if out["user_id"] != "u-42" {
		t.Errorf("user_id should pass through unredacted, got %v", out["user_id"])
	}
	if out["ordinary_amount"] != 199.5 {
		t.Errorf("ordinary_amount should pass through unredacted, got %v", out["ordinary_amount"])
	}
}

func TestRedactForTelemetryMasksClassifiedFieldsByDoctype(t *testing.T) {
	in := map[string]interface{}{
		"gross_pay": 85000.0,
		"status":    "Approved",
	}
	out := RedactForTelemetry("Payslip", in)
	if out["gross_pay"] != redactedMarker {
		t.Errorf("Payslip.gross_pay should be masked via sensitive_fields.go classification, got %v", out["gross_pay"])
	}
	if out["status"] != "Approved" {
		t.Errorf("status should pass through unredacted, got %v", out["status"])
	}

	// Without a doctype, the same key carries no classification signal and
	// must pass through - this is the documented, deliberate behavior, not a
	// bug: the caller must supply the doctype for field-level masking.
	outNoDoctype := RedactForTelemetry("", in)
	if outNoDoctype["gross_pay"] != 85000.0 {
		t.Errorf("with no doctype, gross_pay is not key-shaped as a secret and should pass through, got %v", outNoDoctype["gross_pay"])
	}
}

func TestRedactForTelemetryWalksNestedPayloads(t *testing.T) {
	in := map[string]interface{}{
		"order_id": "SO-1",
		"customer": map[string]interface{}{
			"name":          "Jane Doe",
			"session_token": "abc.def.ghi",
		},
		"lines": []interface{}{
			map[string]interface{}{"sku": "ITEM-1", "api_key": "sk_test_1"},
			map[string]interface{}{"sku": "ITEM-2", "api_key": "sk_test_2"},
		},
	}
	out := RedactForTelemetry("", in)

	customer := out["customer"].(map[string]interface{})
	if customer["session_token"] != redactedMarker {
		t.Errorf("nested session_token should be redacted, got %v", customer["session_token"])
	}
	if customer["name"] != "Jane Doe" {
		t.Errorf("nested name should pass through, got %v", customer["name"])
	}

	lines := out["lines"].([]interface{})
	for i, l := range lines {
		line := l.(map[string]interface{})
		if line["api_key"] != redactedMarker {
			t.Errorf("lines[%d].api_key should be redacted, got %v", i, line["api_key"])
		}
	}
}

func TestMaskIdentifier(t *testing.T) {
	shortValues := []string{"", "ab", "abcd"}
	for _, in := range shortValues {
		want := redactedMarker
		if in == "" {
			want = ""
		}
		if got := MaskIdentifier(in); got != want {
			t.Errorf("MaskIdentifier(%q) = %q, want %q", in, got, want)
		}
	}

	longValues := []string{"abcde", "user@example.com", "9876543210"}
	for _, in := range longValues {
		got := MaskIdentifier(in)
		if !strings.HasPrefix(got, in[:2]) || !strings.HasSuffix(got, in[len(in)-2:]) {
			t.Errorf("MaskIdentifier(%q) = %q, want prefix %q and suffix %q preserved", in, got, in[:2], in[len(in)-2:])
		}
		if len(got) != len(in) {
			t.Errorf("MaskIdentifier(%q) changed length: got %q (len %d), want len %d", in, got, len(got), len(in))
		}
		if strings.Contains(got[2:len(got)-2], in[2:len(in)-2]) && len(in) > 4 {
			t.Errorf("MaskIdentifier(%q) = %q leaks the middle of the value", in, got)
		}
	}
}
