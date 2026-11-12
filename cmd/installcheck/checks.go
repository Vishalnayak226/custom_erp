package main

import (
	"crypto/ed25519"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	_ "github.com/lib/pq"
)

// Stage 47.12.8: the pre-tenant install step. Each check answers one question
// a person installing the ERP on a fresh host would otherwise find out the
// hard way, and says what to do when the answer is no.

type Status string

const (
	Pass Status = "PASS"
	Warn Status = "WARN"
	Fail Status = "FAIL"
	Skip Status = "SKIP"
)

// Result is one row of the report.
type Result struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status Status `json:"status"`
	Detail string `json:"detail"`
	Next   string `json:"next,omitempty"`
}

// Config is everything a run needs; the fields after Env are seams a test
// replaces so no check needs the real host.
type Config struct {
	EnvFile    string
	Domain     string
	PublicIP   string
	DBURL      string
	AppPort    int
	BackupDir  string
	DeployDir  string
	Channel    string
	ReleaseBin string
	ReleaseSig string
	PubKey     string
	MinDiskGB  int
	MinMemMB   int
	MinPG      int // server_version_num / 10000, e.g. 14
	InitSecret bool

	freeBytes  func(path string) (uint64, error)
	totalMem   func() (uint64, error)
	ntpOffset  func() (time.Duration, error)
	lookupHost func(string) ([]string, error)
	localAddrs func() ([]net.IP, error)
	lookPath   func(string) (string, error)
	listen     func(addr string) error
	tlsRoots   *x509.CertPool
	tlsAddr    string
}

func (c *Config) defaults() {
	if c.AppPort == 0 {
		c.AppPort = 8080
	}
	if c.MinDiskGB == 0 {
		c.MinDiskGB = 10
	}
	if c.MinMemMB == 0 {
		c.MinMemMB = 1024
	}
	if c.MinPG == 0 {
		c.MinPG = 14
	}
	if c.freeBytes == nil {
		c.freeBytes = diskFree
	}
	if c.totalMem == nil {
		c.totalMem = memTotal
	}
	if c.ntpOffset == nil {
		c.ntpOffset = sntpOffset
	}
	if c.lookupHost == nil {
		c.lookupHost = net.LookupHost
	}
	if c.localAddrs == nil {
		c.localAddrs = localIPs
	}
	if c.lookPath == nil {
		c.lookPath = exec.LookPath
	}
	if c.listen == nil {
		c.listen = func(addr string) error {
			l, err := net.Listen("tcp", addr)
			if err != nil {
				return err
			}
			return l.Close()
		}
	}
}

// RunAll runs every check in a fixed order.
func RunAll(c *Config) []Result {
	c.defaults()
	out := []Result{
		checkOS(c),
		checkPostgres(c),
		checkDisk(c),
		checkMemory(c),
		checkPort(c, "port-app", c.AppPort, true),
		checkPort(c, "port-http", 80, false),
		checkPort(c, "port-https", 443, false),
		checkClock(c),
		checkTLSTools(c),
		checkDNS(c),
		checkCert(c),
		checkSecrets(c),
		checkChannel(c),
		checkBackup(c),
		checkRestoreDrill(c),
		checkSignature(c),
	}
	return out
}

// Failed reports whether any row is a FAIL (or a WARN when strict).
func Failed(rs []Result, strict bool) bool {
	for _, r := range rs {
		if r.Status == Fail || (strict && r.Status == Warn) {
			return true
		}
	}
	return false
}

func checkOS(c *Config) Result {
	r := Result{ID: "os", Name: "Operating system"}
	r.Detail = runtime.GOOS + "/" + runtime.GOARCH
	switch {
	case runtime.GOOS == "linux" && (runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64"):
		r.Status = Pass
	default:
		r.Status = Warn
		r.Next = "Production hosts run 64-bit Linux (Ubuntu LTS). This machine is fine for development only."
	}
	return r
}

func checkPostgres(c *Config) Result {
	r := Result{ID: "postgres", Name: "PostgreSQL"}
	dsn := c.DBURL
	if dsn == "" {
		r.Status, r.Detail = Fail, "no DATABASE_URL given"
		r.Next = "Put DATABASE_URL in the env file, or pass -db-url. Install PostgreSQL " + strconv.Itoa(c.MinPG) + "+ and create the erp role and database first."
		return r
	}
	if strings.Contains(dsn, "CHANGE_ME") {
		r.Status, r.Detail = Fail, "DATABASE_URL still holds the CHANGE_ME placeholder"
		r.Next = "Set the real database password in DATABASE_URL (the one you gave the erp role in PostgreSQL)."
		return r
	}
	d, err := sql.Open("postgres", withTimeout(dsn))
	if err != nil {
		r.Status, r.Detail = Fail, "DATABASE_URL does not parse: "+redact(err.Error(), dsn)
		r.Next = "Use postgres://user:password@host:5432/dbname?sslmode=disable"
		return r
	}
	defer d.Close()
	d.SetConnMaxLifetime(10 * time.Second)
	var num int
	if err := d.QueryRow(`SELECT current_setting('server_version_num')::int`).Scan(&num); err != nil {
		r.Status, r.Detail = Fail, "cannot connect: "+redact(err.Error(), dsn)
		r.Next = "Start PostgreSQL, check the host/port/password in DATABASE_URL, and that pg_hba.conf lets this host in."
		return r
	}
	major := num / 10000
	r.Detail = fmt.Sprintf("reachable, version %d", major)
	if major < c.MinPG {
		r.Status = Fail
		r.Next = fmt.Sprintf("Upgrade PostgreSQL to %d or newer.", c.MinPG)
		return r
	}
	r.Status = Pass
	return r
}

func withTimeout(dsn string) string {
	u, err := url.Parse(dsn)
	if err != nil || u.Scheme == "" {
		return dsn
	}
	q := u.Query()
	if q.Get("connect_timeout") == "" {
		q.Set("connect_timeout", "5")
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// redact keeps a password out of an error line.
func redact(msg, dsn string) string {
	if u, err := url.Parse(dsn); err == nil && u.User != nil {
		if pw, ok := u.User.Password(); ok && pw != "" {
			msg = strings.ReplaceAll(msg, pw, "***")
		}
	}
	return msg
}

func checkDisk(c *Config) Result {
	r := Result{ID: "disk", Name: "Disk space"}
	path := firstExisting(c.BackupDir, filepath.Dir(c.EnvFile), ".")
	free, err := c.freeBytes(path)
	if err != nil {
		r.Status, r.Detail = Warn, "cannot read free space of "+path+": "+err.Error()
		return r
	}
	gb := float64(free) / (1 << 30)
	r.Detail = fmt.Sprintf("%.1f GB free at %s", gb, path)
	if gb < float64(c.MinDiskGB) {
		r.Status = Fail
		r.Next = fmt.Sprintf("Free up space or attach a bigger disk: at least %d GB for the database, media and backups.", c.MinDiskGB)
		return r
	}
	r.Status = Pass
	return r
}

func firstExisting(paths ...string) string {
	for _, p := range paths {
		if p == "" {
			continue
		}
		for q := p; ; q = filepath.Dir(q) {
			if st, err := os.Stat(q); err == nil && st.IsDir() {
				return q
			}
			if filepath.Dir(q) == q {
				break
			}
		}
	}
	return "."
}

func checkMemory(c *Config) Result {
	r := Result{ID: "memory", Name: "Memory"}
	b, err := c.totalMem()
	if err != nil {
		r.Status, r.Detail = Skip, "not readable on this system: "+err.Error()
		return r
	}
	mb := int(b >> 20)
	r.Detail = fmt.Sprintf("%d MB", mb)
	if mb < c.MinMemMB {
		r.Status = Fail
		r.Next = fmt.Sprintf("Use a host with at least %d MB of memory (or add swap for a small trial).", c.MinMemMB)
		return r
	}
	r.Status = Pass
	return r
}

func checkPort(c *Config, id string, port int, mustBeFree bool) Result {
	r := Result{ID: id, Name: fmt.Sprintf("Port %d", port)}
	err := c.listen(fmt.Sprintf(":%d", port))
	switch {
	case err == nil:
		r.Status, r.Detail = Pass, "free"
	case isPermission(err):
		r.Status, r.Detail = Warn, "cannot test without root; run this as root to check it"
	case mustBeFree:
		r.Status, r.Detail = Fail, "already in use"
		r.Next = fmt.Sprintf("Stop whatever listens on %d, or set PORT in the env file to a free port.", port)
	default:
		r.Status, r.Detail = Warn, "in use"
		r.Next = "Fine if it is Caddy. If not, stop that program before running enable_tls.sh."
	}
	return r
}

func isPermission(err error) bool {
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "permission denied") || strings.Contains(s, "access permissions")
}

func checkClock(c *Config) Result {
	r := Result{ID: "clock", Name: "Clock"}
	off, err := c.ntpOffset()
	if err != nil {
		r.Status, r.Detail = Warn, "could not reach a time server: "+err.Error()
		r.Next = "Make sure the host keeps time (timedatectl set-ntp true). A clock minutes off breaks login codes and certificates."
		return r
	}
	abs := off
	if abs < 0 {
		abs = -abs
	}
	r.Detail = fmt.Sprintf("off by %s", abs.Round(time.Millisecond))
	switch {
	case abs > 2*time.Minute:
		r.Status = Fail
		r.Next = "Fix the clock: timedatectl set-ntp true. Login codes (2FA) and TLS certificates fail with a wrong clock."
	case abs > 5*time.Second:
		r.Status = Warn
		r.Next = "Enable time sync: timedatectl set-ntp true."
	default:
		r.Status = Pass
	}
	return r
}

func checkTLSTools(c *Config) Result {
	r := Result{ID: "tools", Name: "Caddy and openssl"}
	var missing []string
	for _, t := range []string{"caddy", "openssl"} {
		if _, err := c.lookPath(t); err != nil {
			missing = append(missing, t)
		}
	}
	if len(missing) == 0 {
		r.Status, r.Detail = Pass, "both found"
		return r
	}
	r.Status, r.Detail = Warn, "missing: "+strings.Join(missing, ", ")
	r.Next = "Install them (apt install caddy openssl). enable_tls.sh needs Caddy; the deploy signature check needs OpenSSL 3."
	return r
}

func checkDNS(c *Config) Result {
	r := Result{ID: "dns", Name: "Domain points here"}
	if c.Domain == "" {
		r.Status, r.Detail = Skip, "no -domain given"
		return r
	}
	addrs, err := c.lookupHost(c.Domain)
	if err != nil || len(addrs) == 0 {
		r.Status, r.Detail = Fail, c.Domain+" does not resolve"
		r.Next = "Create an A record for " + c.Domain + " pointing at this host's public address, then wait for DNS to update."
		return r
	}
	want := map[string]bool{}
	if c.PublicIP != "" {
		want[c.PublicIP] = true
	} else if ips, err := c.localAddrs(); err == nil {
		for _, ip := range ips {
			want[ip.String()] = true
		}
	}
	for _, a := range addrs {
		if ip := net.ParseIP(a); ip != nil && want[ip.String()] {
			r.Status, r.Detail = Pass, c.Domain+" -> "+a
			return r
		}
	}
	r.Status, r.Detail = Fail, c.Domain+" -> "+strings.Join(addrs, ", ")+", not an address of this host"
	r.Next = "Point the A record at this host. Behind NAT, a load balancer or Cloudflare, pass -public-ip <address> to say which one is yours."
	return r
}

func localIPs() ([]net.IP, error) {
	ifs, err := net.InterfaceAddrs()
	if err != nil {
		return nil, err
	}
	var out []net.IP
	for _, a := range ifs {
		if n, ok := a.(*net.IPNet); ok {
			out = append(out, n.IP)
		}
	}
	return out, nil
}

func checkCert(c *Config) Result {
	r := Result{ID: "cert", Name: "TLS certificate"}
	if c.Domain == "" && c.tlsAddr == "" {
		r.Status, r.Detail = Skip, "no -domain given"
		return r
	}
	addr, name := c.tlsAddr, c.Domain
	if addr == "" {
		addr = net.JoinHostPort(c.Domain, "443")
	}
	d := &net.Dialer{Timeout: 5 * time.Second}
	conn, err := tls.DialWithDialer(d, "tcp", addr, &tls.Config{ServerName: name, RootCAs: c.tlsRoots})
	if err != nil {
		if isRefused(err) || isTimeout(err) {
			r.Status, r.Detail = Warn, "nothing answers on "+addr+" yet"
			r.Next = "Expected before the first run of deploy/enable_tls.sh " + c.Domain + " <email>."
			return r
		}
		r.Status, r.Detail = Fail, "certificate not accepted: "+err.Error()
		r.Next = "Re-run deploy/enable_tls.sh, or check the DNS record and that ports 80 and 443 are open."
		return r
	}
	defer conn.Close()
	left := time.Until(conn.ConnectionState().PeerCertificates[0].NotAfter)
	days := int(left.Hours() / 24)
	r.Detail = fmt.Sprintf("valid, %d days left", days)
	if days < 14 {
		r.Status = Warn
		r.Next = "Renewal should be automatic with Caddy; check `journalctl -u caddy` if it is not."
		return r
	}
	r.Status = Pass
	return r
}

func isRefused(err error) bool {
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "refused") || strings.Contains(s, "no route") || strings.Contains(s, "unreachable")
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

func checkBackup(c *Config) Result {
	r := Result{ID: "backup-dir", Name: "Backup destination"}
	if c.BackupDir == "" {
		r.Status, r.Detail = Fail, "no -backup-dir given"
		r.Next = "Choose where backups go (default on the server: /opt/erp/backups) and pass -backup-dir. Prefer a different disk from the database."
		return r
	}
	if err := os.MkdirAll(c.BackupDir, 0o750); err != nil {
		r.Status, r.Detail = Fail, "cannot create "+c.BackupDir+": "+err.Error()
		r.Next = "Create it and give the erp user write access: sudo install -d -o erp -g erp -m 750 " + c.BackupDir
		return r
	}
	f, err := os.CreateTemp(c.BackupDir, ".installcheck-*")
	if err != nil {
		r.Status, r.Detail = Fail, c.BackupDir+" is not writable: "+err.Error()
		r.Next = "Give the erp user write access: sudo chown erp:erp " + c.BackupDir
		return r
	}
	name := f.Name()
	f.Close()
	os.Remove(name)
	r.Status, r.Detail = Pass, c.BackupDir+" is writable"
	return r
}

func checkRestoreDrill(c *Config) Result {
	r := Result{ID: "restore-drill", Name: "Restore drill"}
	dir := c.DeployDir
	if dir == "" {
		dir = "deploy"
	}
	p := filepath.Join(dir, "restore_drill.sh")
	if _, err := os.Stat(p); err != nil {
		r.Status, r.Detail = Fail, p+" not found"
		r.Next = "Install the release's deploy/ folder beside the app, or pass -deploy-dir."
		return r
	}
	r.Status = Pass
	r.Detail = p + " is present; run it once after the first backup. It keeps no pass record to read back."
	return r
}

func checkSignature(c *Config) Result {
	r := Result{ID: "signature", Name: "Release signature"}
	if c.ReleaseBin == "" {
		r.Status, r.Detail = Skip, "no -release-bin given"
		r.Next = "Pass -release-bin <file> -release-sig <file> to check a release before installing it."
		return r
	}
	pub := c.PubKey
	if pub == "" {
		for _, p := range []string{filepath.Join(c.DeployDir, "..", "release", "signing.pub"), "/etc/erp/signing.pub", filepath.Join("release", "signing.pub")} {
			if _, err := os.Stat(p); err == nil {
				pub = p
				break
			}
		}
	}
	if pub == "" || c.ReleaseSig == "" {
		r.Status, r.Detail = Fail, "need both a signature (-release-sig) and the public key (-pub)"
		return r
	}
	if err := verifyFile(pub, c.ReleaseBin, c.ReleaseSig); err != nil {
		r.Status, r.Detail = Fail, err.Error()
		r.Next = "Do not install this file. Download the release and its .sig again from the release machine."
		return r
	}
	r.Status, r.Detail = Pass, "signature matches "+filepath.Base(c.ReleaseBin)
	return r
}

// verifyFile checks a releasesign signature: the key is a PKIX PEM Ed25519
// key, the signature the 64 raw bytes over the whole file (the same format
// cmd/releasesign writes and deploy/remote_deploy.sh checks with openssl).
func verifyFile(pubPath, inPath, sigPath string) error {
	raw, err := os.ReadFile(pubPath)
	if err != nil {
		return err
	}
	blk, _ := pem.Decode(raw)
	if blk == nil {
		return fmt.Errorf("%s is not a PEM public key", pubPath)
	}
	k, err := x509.ParsePKIXPublicKey(blk.Bytes)
	if err != nil {
		return err
	}
	pub, ok := k.(ed25519.PublicKey)
	if !ok {
		return fmt.Errorf("%s is not an Ed25519 key", pubPath)
	}
	sig, err := os.ReadFile(sigPath)
	if err != nil {
		return err
	}
	if len(sig) != ed25519.SignatureSize {
		return fmt.Errorf("%s is %d bytes, not a %d-byte signature", sigPath, len(sig), ed25519.SignatureSize)
	}
	data, err := os.ReadFile(inPath)
	if err != nil {
		return err
	}
	if !ed25519.Verify(pub, data, sig) {
		return errors.New("the signature does not match this file and key")
	}
	return nil
}
