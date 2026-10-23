package engines

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"time"
)

// Stage 134.2 (49.8.4): one guard for every outbound call to an address a
// client typed - webhooks, extension hooks, notification webhooks, CRM
// senders, Web Push endpoints, marketplace connectors and image drives.
//
//   - Every address a host resolves to must be public (publicAddress): no
//     loopback, private, carrier-grade NAT, link-local (the cloud metadata
//     service), reserved or multicast address, nor an IPv6 wrapper of one
//     (IPv4-mapped, NAT64, 6to4).
//   - The check runs when each connection is made, and the connection goes to
//     the address that was checked - a DNS answer that changes between a
//     check and the dial (rebinding) cannot reach an internal address.
//   - No proxy from the environment: a proxy dials for us, unchecked.
//   - Redirects, where a caller follows them: at most three, never from https
//     down to http, each through the same dial.
//   - Response headers are capped here; bodies by readOutboundBody.
//
// OUTBOUND_ALLOW_PRIVATE=1 is an operator switch for an on-premise install
// whose hooks or storage live on its own network - set on the server, never
// by a client, and never on the SaaS.
var outboundAllowPrivate = os.Getenv("OUTBOUND_ALLOW_PRIVATE") == "1"

// blockedNets are the ranges no client-typed address may reach.
var blockedNets = func() []*net.IPNet {
	var out []*net.IPNet
	for _, cidr := range []string{
		"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16",
		"172.16.0.0/12", "192.0.0.0/24", "192.0.2.0/24", "192.88.99.0/24", "192.168.0.0/16",
		"198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4",
		"::/96", "64:ff9b:1::/48", "100::/64", "2001::/32", "2001:db8::/32",
		"fc00::/7", "fe80::/10", "fec0::/10", "ff00::/8",
	} {
		_, n, err := net.ParseCIDR(cidr)
		if err != nil {
			panic(err)
		}
		out = append(out, n)
	}
	return out
}()

var nat64Prefix = net.ParseIP("64:ff9b::")

// publicAddress reports whether ip is an ordinary public address.
func publicAddress(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if v4 := ip.To4(); v4 != nil {
		ip = v4 // an IPv4-mapped IPv6 address is judged as the IPv4 it carries
	} else if len(ip) == net.IPv6len {
		if ip[:12].Equal(nat64Prefix[:12]) {
			return publicAddress(net.IP(ip[12:16]))
		}
		if ip[0] == 0x20 && ip[1] == 0x02 && !publicAddress(net.IP(ip[2:6])) {
			return false // 6to4 wraps the IPv4 it would reach
		}
	}
	for _, n := range blockedNets {
		if n.Contains(ip) {
			return false
		}
	}
	return true
}

// outboundDial resolves, refuses any non-public answer, and connects to the
// address it checked.
func outboundDial(ctx context.Context, network, addr string) (net.Conn, error) {
	return guardedDial(ctx, network, addr, outboundAllowPrivate)
}

// guardedDial is outboundDial with the on-premise switch passed in (image
// drives keep their own, IMAGE_DRIVE_ALLOW_PRIVATE).
func guardedDial(ctx context.Context, network, addr string, allowPrivate bool) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	if allowPrivate {
		return dialer.DialContext(ctx, network, addr)
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil || len(ips) == 0 {
		return nil, fmt.Errorf("host %q does not resolve", host)
	}
	for _, a := range ips {
		if !publicAddress(a.IP) {
			return nil, fmt.Errorf("host %q resolves to a non-public address (%s) - refused", host, a.IP)
		}
	}
	var lastErr error
	for _, a := range ips {
		conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(a.IP.String(), port))
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

// outboundTransport is shared, so connections are reused across calls.
var outboundTransport = &http.Transport{
	Proxy:                  nil,
	DialContext:            outboundDial,
	ForceAttemptHTTP2:      true,
	TLSHandshakeTimeout:    10 * time.Second,
	ResponseHeaderTimeout:  30 * time.Second,
	MaxResponseHeaderBytes: 64 << 10,
	MaxIdleConns:           64,
	MaxIdleConnsPerHost:    4,
	IdleConnTimeout:        90 * time.Second,
	ExpectContinueTimeout:  time.Second,
}

// newOutboundClient is the client every call to a client-typed address uses.
// followRedirects false returns a 3xx as it is (a webhook's answer is its
// status, and a redirect must not carry the signed body elsewhere).
func newOutboundClient(timeout time.Duration, followRedirects bool) *http.Client {
	c := &http.Client{Timeout: timeout, Transport: outboundTransport}
	if followRedirects {
		c.CheckRedirect = outboundRedirect
	} else {
		c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	}
	return c
}

func outboundRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 3 {
		return fmt.Errorf("stopped after %d redirects", len(via))
	}
	if via[0].URL.Scheme == "https" && req.URL.Scheme != "https" {
		return fmt.Errorf("refused a redirect from https to %s", req.URL.Scheme)
	}
	return nil
}

// readOutboundBody reads at most max bytes of an answer; a longer one is an
// error rather than a silently cut document.
func readOutboundBody(r io.Reader, max int64) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, fmt.Errorf("the answer is larger than %d bytes - refused", max)
	}
	return b, nil
}
