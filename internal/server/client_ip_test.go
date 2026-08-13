package server

import (
	"net"
	"net/http"
	"testing"
)

// Stage 43.3 - forwarded-IP parsing.
//
// clientIP decides which rate-limit bucket a request lands in, so getting it
// wrong is not a cosmetic bug: the auth category allows 5 requests per minute
// per IP, and a caller who can choose their own IP has no limit at all.
func TestClientIP(t *testing.T) {
	// These are package-level and set from the environment at init; swap them
	// for the duration of the test rather than depending on how the test
	// process happens to be configured.
	origTrust, origNets := trustProxy, trustedProxyNets
	defer func() { trustProxy, trustedProxyNets = origTrust, origNets }()

	request := func(remoteAddr, xff string) *http.Request {
		r := &http.Request{RemoteAddr: remoteAddr, Header: http.Header{}}
		if xff != "" {
			r.Header.Set("X-Forwarded-For", xff)
		}
		return r
	}

	t.Run("without a trusted proxy the header is ignored entirely", func(t *testing.T) {
		trustProxy = false
		if got := clientIP(request("203.0.113.9:44321", "1.2.3.4")); got != "203.0.113.9" {
			t.Errorf("got %q, want the socket address 203.0.113.9", got)
		}
	})

	trustProxy = true
	trustedProxyNets = loadTrustedProxyNets()

	t.Run("a forged left-hand entry does not win", func(t *testing.T) {
		// The attack: client sends its own X-Forwarded-For, the proxy appends
		// the address it really saw. Left-most is the forgery; right-most
		// untrusted entry is the truth.
		got := clientIP(request("127.0.0.1:8080", "1.2.3.4, 203.0.113.9"))
		if got != "203.0.113.9" {
			t.Errorf("got %q, want 203.0.113.9 - a client must not be able to choose its own rate-limit bucket", got)
		}
	})

	t.Run("a single proxy-set entry is the client", func(t *testing.T) {
		if got := clientIP(request("127.0.0.1:8080", "203.0.113.9")); got != "203.0.113.9" {
			t.Errorf("got %q, want 203.0.113.9", got)
		}
	})

	t.Run("trusted hops are skipped from the right", func(t *testing.T) {
		// The shape Cloudflare activation produces once its CIDRs are trusted:
		// real client, then edge nodes that must not be mistaken for it.
		got := clientIP(request("127.0.0.1:8080", "203.0.113.9, 10.0.0.5, 192.168.1.7"))
		if got != "203.0.113.9" {
			t.Errorf("got %q, want 203.0.113.9", got)
		}
	})

	t.Run("garbage entries are skipped, not trusted", func(t *testing.T) {
		if got := clientIP(request("127.0.0.1:8080", "not-an-ip, 203.0.113.9")); got != "203.0.113.9" {
			t.Errorf("got %q, want 203.0.113.9", got)
		}
		// All-garbage must fall back to the socket, never to attacker text.
		if got := clientIP(request("127.0.0.1:8080", "garbage, more-garbage")); got != "127.0.0.1" {
			t.Errorf("got %q, want the socket address 127.0.0.1", got)
		}
	})

	t.Run("an all-internal chain falls back to the socket address", func(t *testing.T) {
		if got := clientIP(request("127.0.0.1:8080", "10.0.0.5, 192.168.1.7")); got != "127.0.0.1" {
			t.Errorf("got %q, want 127.0.0.1", got)
		}
	})

	t.Run("IPv6 clients and socket addresses are handled", func(t *testing.T) {
		if got := clientIP(request("[::1]:8080", "2001:db8::5")); got != "2001:db8::5" {
			t.Errorf("got %q, want 2001:db8::5", got)
		}
		if got := clientIP(request("[::1]:8080", "")); got != "::1" {
			t.Errorf("got %q, want ::1", got)
		}
	})

	t.Run("TRUSTED_PROXY_CIDRS extends the trust boundary", func(t *testing.T) {
		// This is the entire Cloudflare activation step: an env var, not a
		// code change. 198.51.100.0/24 stands in for a published edge range.
		t.Setenv("TRUSTED_PROXY_CIDRS", "198.51.100.0/24")
		trustedProxyNets = loadTrustedProxyNets()
		defer func() { trustedProxyNets = loadTrustedProxyNets() }()

		got := clientIP(request("127.0.0.1:8080", "203.0.113.9, 198.51.100.7"))
		if got != "203.0.113.9" {
			t.Errorf("got %q, want 203.0.113.9 - a configured edge CIDR must not be read as the client", got)
		}
	})

	t.Run("an unparseable CIDR is dropped, not silently trusted", func(t *testing.T) {
		t.Setenv("TRUSTED_PROXY_CIDRS", "not-a-cidr")
		nets := loadTrustedProxyNets()
		defer func() { trustedProxyNets = loadTrustedProxyNets() }()
		if len(nets) != len(defaultTrustedProxyCIDRs) {
			t.Errorf("expected only the %d defaults to survive, got %d", len(defaultTrustedProxyCIDRs), len(nets))
		}
	})

	t.Run("every default CIDR parses", func(t *testing.T) {
		for _, spec := range defaultTrustedProxyCIDRs {
			if _, _, err := net.ParseCIDR(spec); err != nil {
				t.Errorf("default trusted CIDR %q does not parse: %v", spec, err)
			}
		}
	})
}
