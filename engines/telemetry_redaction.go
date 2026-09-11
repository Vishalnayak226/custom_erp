package engines

import "strings"

// Stage 49.6.7 - safe telemetry and support: a redaction choke point.
//
// No general structured-security-event pipeline exists in this codebase yet
// - the risk register's "Sensitive-read security event" and "manual-price
// detection" entries (docs/security/risk_register.md M-01/M-02) are forward
// references to Stage 49.11, not built code. This file is deliberately the
// REDACTION PRIMITIVE such a pipeline must call, not the pipeline itself -
// built now so 49.11 has a tested choke point to attach to on day one,
// following this codebase's own rule of attaching a cross-cutting control at
// the one shared point every caller runs through (CLAUDE.md's "first
// principle"), rather than every future call site inventing its own
// redaction.
//
// RedactForTelemetry takes an arbitrary key/value payload - the shape a
// structured security event, log line or support-bundle attachment would
// carry - and returns a copy safe to persist or transmit: any key that looks
// like it carries a bearer token, password, secret, MFA code, session/cookie
// value or connector credential is replaced with a fixed marker regardless
// of doctype, and any key matching a sensitive_fields.go-classified field
// name on the given doctype is masked even when its own key name gives no
// hint (e.g. "gross_pay").

const redactedMarker = "[REDACTED]"

// telemetrySecretKeyFragments: a key CONTAINING any of these (case
// insensitive) is treated as carrying a secret outright - the "never log a
// bearer/reset/MFA/session/cookie/connector secret" list 49.6.7 names
// explicitly, expressed as key shapes rather than value patterns because a
// telemetry payload's values are exactly what must never be inspected to
// decide this.
var telemetrySecretKeyFragments = []string{
	"password", "passwd", "secret", "token", "bearer", "authorization",
	"cookie", "session", "mfa", "otp", "api_key", "apikey", "access_key",
	"private_key", "signing_key", "credential",
}

// RedactForTelemetry returns a copy of fields safe to hand to a log line,
// structured security event or support-bundle attachment. doctype is
// optional ("" when the payload is not about one specific document) and
// enables the sensitive_fields.go lookup; nested maps and slices are walked
// so a document payload's line items or sub-objects are covered too.
func RedactForTelemetry(doctype string, fields map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(fields))
	for k, v := range fields {
		switch {
		case telemetryKeyLooksSecret(k):
			out[k] = redactedMarker
		case doctype != "" && sensitiveFieldNamed(doctype, k):
			out[k] = redactedMarker
		default:
			out[k] = redactTelemetryValue(doctype, v)
		}
	}
	return out
}

func redactTelemetryValue(doctype string, v interface{}) interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		return RedactForTelemetry(doctype, val)
	case []interface{}:
		out := make([]interface{}, len(val))
		for i, item := range val {
			out[i] = redactTelemetryValue(doctype, item)
		}
		return out
	default:
		return v
	}
}

func telemetryKeyLooksSecret(key string) bool {
	lower := strings.ToLower(key)
	for _, frag := range telemetrySecretKeyFragments {
		if strings.Contains(lower, frag) {
			return true
		}
	}
	return false
}

func sensitiveFieldNamed(doctype, field string) bool {
	_, ok := sensitiveFields[doctype][field]
	return ok
}

// MaskIdentifier shortens an identifier (email, phone, token prefix, user
// id) to a form still useful for correlating repeated events without
// disclosing the whole value - 49.6.7's "masked identifiers where enough."
// A value of 4 characters or fewer is masked entirely rather than
// partially, since a partial mask of a short value discloses most of it.
func MaskIdentifier(v string) string {
	if v == "" {
		return ""
	}
	if len(v) <= 4 {
		return redactedMarker
	}
	return v[:2] + strings.Repeat("*", len(v)-4) + v[len(v)-2:]
}
