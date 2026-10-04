package engines

import (
	"strings"
	"testing"
)

func TestRedactAndBoundLogField(t *testing.T) {
	got := redactAndBoundLogField(`failed Authorization: Bearer abc.def password="hunter2" api_key=key-123`, 4096)
	for _, secret := range []string{"abc.def", "hunter2", "key-123"} {
		if strings.Contains(got, secret) {
			t.Errorf("sensitive value %q leaked: %s", secret, got)
		}
	}
	if !strings.Contains(got, "[REDACTED]") {
		t.Fatalf("expected redaction markers, got %q", got)
	}

	long := redactAndBoundLogField(strings.Repeat("x", 1000), 100)
	if len(long) > 100 || !strings.HasSuffix(long, "…[truncated]") {
		t.Fatalf("bounded field = %d bytes, %q", len(long), long)
	}
	unicode := redactAndBoundLogField(strings.Repeat("界", 80), 100)
	if len(unicode) > 100 {
		t.Fatalf("multibyte field exceeded byte cap: %d", len(unicode))
	}
}
