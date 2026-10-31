package engines

import (
	"context"
	"net"
	"strings"
)

// Stage 49.11.2: where a request came from and under which authorisation
// policy it ran, carried on the request context like its correlation id
// (correlation.go) so every security event the request writes through
// LogAuditEventCtx records them without each call site passing them on.
//
// Lawful and minimal: the address is the one the server's trusted-proxy rule
// reads (clientIP, middleware.go), the device is a few words from the user
// agent - never the raw string, which carries enough version detail to
// fingerprint a person - and both are cleared after their retention
// (auditSourceRetentionDays, the register's row). Neither is signed: a field
// that must be erased later cannot be part of the evidence.

type requestSourceKey struct{}
type requestPolicyKey struct{}

// RequestSource is the address and device summary of one request.
type RequestSource struct {
	Address string
	Device  string
}

// RequestPolicy is the capability one request needed and the fingerprint of
// the authorisation policy that decided it.
type RequestPolicy struct {
	Capability string
	Version    string
}

const (
	maxSourceAddressBytes = 45 // the longest textual IPv6 address
	maxDeviceBytes        = 48
	maxCapabilityBytes    = 80
	maxPolicyVersionBytes = 16
)

// WithRequestSource returns ctx carrying the request's client address and a
// short device summary of userAgent. An address that does not parse as an IP
// is not stored.
func WithRequestSource(ctx context.Context, address, userAgent string) context.Context {
	src := RequestSource{Device: DeviceSummary(userAgent)}
	if ip := net.ParseIP(strings.TrimSpace(address)); ip != nil {
		src.Address = ip.String()
	}
	if src.Address == "" && src.Device == "" {
		return ctx
	}
	return context.WithValue(ctx, requestSourceKey{}, src)
}

// RequestSourceFromContext is the source WithRequestSource stored, or zero.
func RequestSourceFromContext(ctx context.Context) RequestSource {
	if ctx == nil {
		return RequestSource{}
	}
	src, _ := ctx.Value(requestSourceKey{}).(RequestSource)
	return src
}

// WithRequestPolicy returns ctx carrying the route capability and policy
// fingerprint the request was authorised under.
func WithRequestPolicy(ctx context.Context, capability, version string) context.Context {
	p := RequestPolicy{Capability: boundPlain(capability, maxCapabilityBytes), Version: boundPlain(version, maxPolicyVersionBytes)}
	if p.Capability == "" && p.Version == "" {
		return ctx
	}
	return context.WithValue(ctx, requestPolicyKey{}, p)
}

// RequestPolicyFromContext is the policy WithRequestPolicy stored, or zero.
func RequestPolicyFromContext(ctx context.Context) RequestPolicy {
	if ctx == nil {
		return RequestPolicy{}
	}
	p, _ := ctx.Value(requestPolicyKey{}).(RequestPolicy)
	return p
}

// boundPlain keeps the characters a capability name or fingerprint uses and
// cuts it to max bytes.
func boundPlain(s string, max int) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(s) {
		if b.Len() >= max {
			break
		}
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_', r == ':':
			b.WriteRune(r)
		}
	}
	return b.String()
}

// DeviceSummary reduces a user agent to "<client> on <platform>" - a browser
// or tool family and an operating system family, no versions. "" for an
// empty user agent; "Other" when nothing is recognised.
func DeviceSummary(userAgent string) string {
	ua := strings.ToLower(strings.TrimSpace(userAgent))
	if ua == "" {
		return ""
	}
	first := func(pairs [][2]string) string {
		for _, p := range pairs {
			if strings.Contains(ua, p[0]) {
				return p[1]
			}
		}
		return ""
	}
	// Order matters: Edge and Opera also say "chrome", Chrome also says "safari".
	client := first([][2]string{
		{"edg/", "Edge"}, {"opr/", "Opera"}, {"samsungbrowser", "Samsung Internet"}, {"firefox/", "Firefox"},
		{"fxios", "Firefox"}, {"crios", "Chrome"}, {"chrome/", "Chrome"}, {"chromium", "Chrome"},
		{"safari/", "Safari"}, {"curl/", "curl"}, {"wget/", "Wget"}, {"postman", "Postman"},
		{"python", "Python client"}, {"go-http-client", "Go client"}, {"okhttp", "Android app"},
		{"java/", "Java client"}, {"node", "Node client"}, {"shopify", "Shopify"}, {"bigcommerce", "BigCommerce"},
		{"bot", "Bot"},
	})
	platform := first([][2]string{
		{"android", "Android"}, {"iphone", "iOS"}, {"ipad", "iOS"}, {"windows", "Windows"},
		{"cros", "ChromeOS"}, {"mac os", "macOS"}, {"macintosh", "macOS"}, {"linux", "Linux"},
	})
	switch {
	case client == "" && platform == "":
		return "Other"
	case client == "":
		return platform
	case platform == "":
		return client
	}
	out := client + " on " + platform
	if len(out) > maxDeviceBytes {
		out = out[:maxDeviceBytes]
	}
	return out
}
