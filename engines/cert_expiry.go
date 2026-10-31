package engines

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"custom_erp/db"
	"errors"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// Stage 49.11.7: certificate expiry for the platform operator.
//
// What this server can see: the certificate each of its public addresses
// presents to a browser. TLS ends at Caddy (or at Cloudflare's edge, which
// then holds the certificate users see), not in this process, so there is no
// certificate file of its own to read. The check is a TLS handshake - nothing
// sent, nothing kept but the expiry - to every https origin in
// CORS_ALLOWED_ORIGINS and every client address <slug>.<TENANT_BASE_DOMAIN>,
// through the outbound guard's dial (an address that resolves inside the
// network is refused, as for any other outbound call). It runs only where
// outside calls run (production, or ERP_ENABLE_EXTERNAL_SIDE_EFFECTS=1) and
// not under the emergency switch.
//
// Every server checks for itself every interval and keeps the answer in
// memory (the Control Panel reads it); the scheduler leader alerts
// CERT_EXPIRING once a day per host while one has fewer than
// certExpiryAlertDays left - Caddy renews at 30 days, so that means renewal
// has been failing for over two weeks.

const (
	certExpiryAlertDays = 14
	certificateHostCap  = 200
	certCheckTimeout    = 8 * time.Second
	certCheckParallel   = 4
)

// CertificateStatus is one public address's certificate as a browser sees it.
type CertificateStatus struct {
	Host      string     `json:"host"`
	NotAfter  *time.Time `json:"not_after,omitempty"`
	DaysLeft  int        `json:"days_left"`
	Issuer    string     `json:"issuer,omitempty"`
	Expiring  bool       `json:"expiring"`
	Problem   string     `json:"problem,omitempty"`
	CheckedAt time.Time  `json:"checked_at"`
}

var certificateWatch struct {
	sync.Mutex
	statuses []CertificateStatus
	at       time.Time
	alerted  map[string]time.Time
}

// CertificateStatuses is this server's last check (nil before the first).
func CertificateStatuses() ([]CertificateStatus, time.Time) {
	certificateWatch.Lock()
	defer certificateWatch.Unlock()
	out := make([]CertificateStatus, len(certificateWatch.statuses))
	copy(out, certificateWatch.statuses)
	return out, certificateWatch.at
}

// certificateHosts lists the public hostnames to check: the https origins the
// server is told it serves and each client's own address, sorted, at most
// certificateHostCap.
func certificateHosts() []string {
	seen := map[string]bool{}
	for _, origin := range strings.Split(os.Getenv("CORS_ALLOWED_ORIGINS"), ",") {
		u, err := url.Parse(strings.TrimSpace(origin))
		if err != nil || u.Scheme != "https" || u.Hostname() == "" || net.ParseIP(u.Hostname()) != nil {
			continue
		}
		seen[strings.ToLower(u.Hostname())] = true
	}
	if base := strings.ToLower(strings.TrimSpace(os.Getenv("TENANT_BASE_DOMAIN"))); base != "" && db.DB != nil {
		rows, err := db.DB.Query(`SELECT host_slug FROM public.tenants
			WHERE COALESCE(host_slug, '') <> '' AND COALESCE(lifecycle_status, 'active') = 'active'`)
		if err == nil {
			for rows.Next() {
				var slug string
				if rows.Scan(&slug) == nil {
					seen[strings.ToLower(slug)+"."+base] = true
				}
			}
			rows.Close()
		}
	}
	out := make([]string, 0, len(seen))
	for h := range seen {
		out = append(out, h)
	}
	sort.Strings(out)
	if len(out) > certificateHostCap {
		out = out[:certificateHostCap]
	}
	return out
}

// CheckCertificates checks every public address now, keeps the answer for
// CertificateStatuses and returns it. Nothing is checked where outside calls
// are off.
func CheckCertificates(ctx context.Context) []CertificateStatus {
	if !ExternalSideEffectsEnabled() {
		return nil
	}
	hosts := certificateHosts()
	out := make([]CertificateStatus, len(hosts))
	sem := make(chan struct{}, certCheckParallel)
	var wg sync.WaitGroup
	for i, h := range hosts {
		wg.Add(1)
		go func(i int, h string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			out[i] = checkCertificate(ctx, h, outboundDial, nil, time.Now())
		}(i, h)
	}
	wg.Wait()
	certificateWatch.Lock()
	certificateWatch.statuses, certificateWatch.at = out, time.Now()
	certificateWatch.Unlock()
	return out
}

// checkCertificate handshakes with host:443 over dial and reads the leaf
// certificate - also when it fails verification (expired, wrong name,
// untrusted), so an expired certificate still shows its date. roots nil is
// the system pool.
func checkCertificate(ctx context.Context, host string, dial func(context.Context, string, string) (net.Conn, error), roots *x509.CertPool, now time.Time) CertificateStatus {
	st := CertificateStatus{Host: host, CheckedAt: now.UTC()}
	ctx, cancel := context.WithTimeout(ctx, certCheckTimeout)
	defer cancel()
	conn, err := dial(ctx, "tcp", net.JoinHostPort(host, "443"))
	if err != nil {
		st.Problem = "could not connect: " + err.Error()
		return st
	}
	defer conn.Close()
	tc := tls.Client(conn, &tls.Config{ServerName: host, RootCAs: roots, MinVersion: tls.VersionTLS12})
	err = tc.HandshakeContext(ctx)
	var leaf *x509.Certificate
	if err == nil {
		if certs := tc.ConnectionState().PeerCertificates; len(certs) > 0 {
			leaf = certs[0]
		}
	} else {
		var verr *tls.CertificateVerificationError
		if errors.As(err, &verr) && len(verr.UnverifiedCertificates) > 0 {
			leaf = verr.UnverifiedCertificates[0]
		}
		st.Problem = "not trusted: " + err.Error()
	}
	if leaf == nil {
		if st.Problem == "" {
			st.Problem = "no certificate presented"
		}
		return st
	}
	notAfter := leaf.NotAfter.UTC()
	st.NotAfter = &notAfter
	st.DaysLeft = int(notAfter.Sub(now).Hours() / 24)
	if notAfter.Before(now) {
		st.DaysLeft = -int(now.Sub(notAfter).Hours()/24) - 1
	}
	st.Issuer = leaf.Issuer.CommonName
	if st.Issuer == "" && len(leaf.Issuer.Organization) > 0 {
		st.Issuer = leaf.Issuer.Organization[0]
	}
	st.Expiring = notAfter.Before(now.Add(certExpiryAlertDays * 24 * time.Hour))
	return st
}

// StartCertificateExpiryWatch checks the public addresses shortly after start
// and then every interval; the scheduler leader alerts on an expiring one.
func StartCertificateExpiryWatch(ctx context.Context, interval time.Duration) {
	go func() {
		first := time.NewTimer(time.Minute)
		defer first.Stop()
		select {
		case <-ctx.Done():
			return
		case <-first.C:
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			statuses := CheckCertificates(ctx)
			if IsSchedulerLeader() {
				alertExpiringCertificates(statuses, time.Now())
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

// alertExpiringCertificates pages CERT_EXPIRING once a day per host while it
// has fewer than certExpiryAlertDays left.
func alertExpiringCertificates(statuses []CertificateStatus, now time.Time) {
	for _, st := range statuses {
		if !st.Expiring || st.NotAfter == nil {
			continue
		}
		certificateWatch.Lock()
		if certificateWatch.alerted == nil {
			certificateWatch.alerted = map[string]time.Time{}
		}
		last, seen := certificateWatch.alerted[st.Host]
		send := !seen || now.Sub(last) >= 24*time.Hour
		if send {
			certificateWatch.alerted[st.Host] = now
		}
		certificateWatch.Unlock()
		if send {
			msg := fmt.Sprintf("the certificate for %s expires %s (%d days) - automatic renewal is not working; check the TLS setup (deploy/enable_tls.sh)",
				st.Host, st.NotAfter.Format("2006-01-02"), st.DaysLeft)
			log.Printf("[CERT-WATCH] %s", msg)
			SendOpsAlert("CERT_EXPIRING", "cert-watch", msg)
		}
	}
}
