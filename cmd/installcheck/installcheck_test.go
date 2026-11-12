package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func fakeConfig(t *testing.T) *Config {
	t.Helper()
	dir := t.TempDir()
	return &Config{
		EnvFile:   filepath.Join(dir, "erp.env"),
		BackupDir: filepath.Join(dir, "backups"),
		DeployDir: dir,
		freeBytes: func(string) (uint64, error) { return 50 << 30, nil },
		totalMem:  func() (uint64, error) { return 4 << 30, nil },
		ntpOffset: func() (time.Duration, error) { return 20 * time.Millisecond, nil },
		lookupHost: func(string) ([]string, error) {
			return []string{"203.0.113.9"}, nil
		},
		localAddrs: func() ([]net.IP, error) { return []net.IP{net.ParseIP("203.0.113.9")}, nil },
		lookPath:   func(string) (string, error) { return "/usr/bin/x", nil },
		listen:     func(string) error { return nil },
	}
}

func byID(rs []Result, id string) Result {
	for _, r := range rs {
		if r.ID == id {
			return r
		}
	}
	return Result{}
}

func TestGenerateSecretsNeverOverwritesAnExistingSecret(t *testing.T) {
	path := filepath.Join(t.TempDir(), "erp.env")
	original := "PORT=8080\nJWT_SECRET=my-own-real-secret-value\nDATABASE_URL=postgres://erp:pass@localhost/erp\n#BACKUP_ENCRYPTION_KEY=CHANGE_ME_openssl_rand_hex_32\nSTORED_VALUE_KEY=CHANGE_ME_x\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	written, kept, err := GenerateSecrets(path)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	text := string(got)
	if !strings.Contains(text, "JWT_SECRET=my-own-real-secret-value\n") {
		t.Fatalf("an existing secret was changed:\n%s", text)
	}
	if !strings.Contains(text, "DATABASE_URL=postgres://erp:pass@localhost/erp\n") || !strings.Contains(text, "PORT=8080\n") {
		t.Errorf("other lines were changed:\n%s", text)
	}
	if strings.Contains(text, "CHANGE_ME_x") {
		t.Error("a placeholder was left")
	}
	if len(kept) != 1 || kept[0] != "JWT_SECRET" || len(written) != len(requiredSecrets)-1 {
		t.Errorf("written=%v kept=%v", written, kept)
	}
	for _, s := range requiredSecrets {
		_, v, ok := envValue(strings.Split(text, "\n"), s.Key)
		if !ok || len(v) < 2 {
			t.Errorf("%s missing", s.Key)
		}
		if s.Key != "JWT_SECRET" && len(v) != s.Bytes*2 {
			t.Errorf("%s has %d chars, want %d", s.Key, len(v), s.Bytes*2)
		}
	}
	// Run it again: nothing is written, the file does not change.
	before := text
	written2, _, err := GenerateSecrets(path)
	after, _ := os.ReadFile(path)
	if err != nil || len(written2) != 0 || string(after) != before {
		t.Fatalf("second run changed the file: written=%v err=%v", written2, err)
	}
	if runtime.GOOS != "windows" {
		if st, _ := os.Stat(path); st.Mode().Perm()&0o077 != 0 {
			t.Errorf("mode %v lets others read it", st.Mode().Perm())
		}
	}
}

func TestGenerateSecretsCreatesAFileOnlyTheOwnerCanRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "erp.env")
	if _, _, err := GenerateSecrets(path); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		st, err := os.Stat(path)
		if err != nil || st.Mode().Perm() != 0o600 {
			t.Fatalf("mode %v err %v", st.Mode().Perm(), err)
		}
	}
	a, _ := os.ReadFile(path)
	if _, _, err := GenerateSecrets(path); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if !bytes.Equal(a, b) {
		t.Error("a complete file was rewritten")
	}
}

func TestSecretValuesAreNeverPrinted(t *testing.T) {
	c := fakeConfig(t)
	c.InitSecret = true
	rs := RunAll(c)
	raw, _ := json.Marshal(rs)
	body, _ := os.ReadFile(c.EnvFile)
	for _, l := range strings.Split(string(body), "\n") {
		if _, v, ok := strings.Cut(l, "="); ok && len(v) >= 32 && strings.Contains(string(raw), v) {
			t.Fatalf("a secret value appears in the report: %s", l[:12])
		}
	}
}

func TestSetEnvIfAbsentKeepsTheChosenChannel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "erp.env")
	os.WriteFile(path, []byte("UPGRADE_CHANNEL=hold\n"), 0o600)
	wrote, cur, err := SetEnvIfAbsent(path, "UPGRADE_CHANNEL", "stable")
	if err != nil || wrote || cur != "hold" {
		t.Fatalf("wrote=%v cur=%q err=%v", wrote, cur, err)
	}
	b, _ := os.ReadFile(path)
	if string(b) != "UPGRADE_CHANNEL=hold\n" {
		t.Errorf("file changed: %q", b)
	}
}

func TestEverythingPassesOnAGoodHost(t *testing.T) {
	c := fakeConfig(t)
	c.Domain = "app.example.com"
	c.InitSecret = true
	c.Channel = "stable"
	c.DBURL = "" // the DB check fails alone; the rest are what this test is about
	os.WriteFile(filepath.Join(c.DeployDir, "restore_drill.sh"), []byte("#!/bin/sh\n"), 0o755)
	rs := RunAll(c)
	for _, id := range []string{"disk", "memory", "port-app", "clock", "tools", "dns", "secrets", "channel", "backup-dir", "restore-drill"} {
		if r := byID(rs, id); r.Status != Pass {
			t.Errorf("%s: %s %s", id, r.Status, r.Detail)
		}
	}
	if r := byID(rs, "postgres"); r.Status != Fail || r.Next == "" {
		t.Errorf("a missing DATABASE_URL must FAIL with a next step: %+v", r)
	}
}

func TestFailuresSayWhatToDoAndSetTheExitCode(t *testing.T) {
	c := fakeConfig(t)
	c.Domain = "app.example.com"
	c.freeBytes = func(string) (uint64, error) { return 1 << 30, nil }
	c.totalMem = func() (uint64, error) { return 256 << 20, nil }
	c.ntpOffset = func() (time.Duration, error) { return -10 * time.Minute, nil }
	c.lookupHost = func(string) ([]string, error) { return []string{"198.51.100.1"}, nil }
	c.listen = func(string) error { return errors.New("bind: address already in use") }
	c.DBURL = "postgres://erp:CHANGE_ME_STRONG_DB_PASSWORD@localhost:5432/x"
	rs := RunAll(c)
	for _, id := range []string{"disk", "memory", "port-app", "clock", "dns", "secrets", "backup-dir", "restore-drill", "postgres"} {
		r := byID(rs, id)
		if id == "backup-dir" {
			continue // writable temp dir: passes
		}
		if r.Status != Fail || r.Next == "" {
			t.Errorf("%s should FAIL with a next step, got %s %q / %q", id, r.Status, r.Detail, r.Next)
		}
	}
	if !Failed(rs, false) {
		t.Error("Failed() = false")
	}
	if Failed([]Result{{Status: Warn}}, false) || !Failed([]Result{{Status: Warn}}, true) {
		t.Error("strict handling wrong")
	}
}

func TestRunExitCodeAndJSON(t *testing.T) {
	dir := t.TempDir()
	var out, errb bytes.Buffer
	code := run([]string{"-env", filepath.Join(dir, "missing.env"), "-json", "-db-url", "postgres://u:pass@127.0.0.1:1/x", "-deploy-dir", dir}, &out, &errb)
	if code != 1 {
		t.Fatalf("exit code %d, want 1 (secrets and database cannot pass)", code)
	}
	var rep struct {
		OK     bool     `json:"ok"`
		Checks []Result `json:"checks"`
	}
	if err := json.Unmarshal(out.Bytes(), &rep); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out.String())
	}
	if rep.OK || len(rep.Checks) < 10 {
		t.Errorf("ok=%v checks=%d", rep.OK, len(rep.Checks))
	}
	if strings.Contains(out.String(), ":p@") {
		t.Error("the database password is in the output")
	}
	if code := run([]string{"-nope"}, &out, &errb); code != 2 {
		t.Errorf("bad flag exit code %d", code)
	}
	// The table form names the next step.
	out.Reset()
	run([]string{"-env", filepath.Join(dir, "missing.env"), "-db-url", "postgres://u:pass@127.0.0.1:1/x", "-deploy-dir", dir}, &out, &errb)
	if !strings.Contains(out.String(), "Next steps:") || !strings.Contains(out.String(), "-init-secrets") {
		t.Errorf("no next steps:\n%s", out.String())
	}
}

func TestBackupDestinationMustBeWritable(t *testing.T) {
	c := fakeConfig(t)
	file := filepath.Join(t.TempDir(), "afile")
	os.WriteFile(file, []byte("x"), 0o600)
	c.BackupDir = filepath.Join(file, "inside") // a path under a file can never be a directory
	if r := checkBackup(c); r.Status != Fail || r.Next == "" {
		t.Errorf("%+v", r)
	}
	c.BackupDir = ""
	if r := checkBackup(c); r.Status != Fail {
		t.Errorf("empty backup dir: %+v", r)
	}
}

func TestSignatureCheck(t *testing.T) {
	dir := t.TempDir()
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	der, _ := x509.MarshalPKIXPublicKey(pub)
	pubPath := filepath.Join(dir, "signing.pub")
	os.WriteFile(pubPath, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}), 0o644)
	bin := filepath.Join(dir, "erp-server")
	os.WriteFile(bin, []byte("release bytes"), 0o755)
	sig := filepath.Join(dir, "erp-server.sig")
	os.WriteFile(sig, ed25519.Sign(priv, []byte("release bytes")), 0o644)

	c := fakeConfig(t)
	c.ReleaseBin, c.ReleaseSig, c.PubKey = bin, sig, pubPath
	if r := checkSignature(c); r.Status != Pass {
		t.Fatalf("good signature: %+v", r)
	}
	os.WriteFile(bin, []byte("release bytes, changed"), 0o755)
	if r := checkSignature(c); r.Status != Fail || !strings.Contains(r.Detail, "does not match") {
		t.Fatalf("a changed file passed: %+v", r)
	}
	// The repository's own public key parses.
	if _, err := os.Stat(filepath.Join("..", "..", "release", "signing.pub")); err == nil {
		c.PubKey = filepath.Join("..", "..", "release", "signing.pub")
		if r := checkSignature(c); r.Status != Fail || strings.Contains(r.Detail, "PEM") || strings.Contains(r.Detail, "Ed25519") {
			t.Errorf("repo key did not load: %+v", r)
		}
	}
}

func TestCertificateCheck(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	c := fakeConfig(t)
	c.Domain = "example.com"
	c.tlsAddr, c.tlsRoots = srv.Listener.Addr().String(), pool
	// httptest's certificate is for 127.0.0.1/example.com
	if r := checkCert(c); r.Status != Pass && r.Status != Fail {
		t.Fatalf("%+v", r)
	}
	c.Domain = "127.0.0.1"
	if r := checkCert(c); r.Status != Pass || !strings.Contains(r.Detail, "days left") {
		t.Errorf("valid certificate: %+v", r)
	}
	c.tlsRoots = x509.NewCertPool() // not trusted
	if r := checkCert(c); r.Status != Fail || r.Next == "" {
		t.Errorf("untrusted certificate: %+v", r)
	}
	srv.Close()
	c.tlsRoots = pool
	if r := checkCert(c); r.Status != Warn {
		t.Errorf("nothing listening should WARN, got %+v", r)
	}
}

func TestSNTPOffsetMath(t *testing.T) {
	now := time.Now()
	resp := make([]byte, 48)
	secs := uint32(now.Unix() + 2208988800 + 30)
	resp[40], resp[41], resp[42], resp[43] = byte(secs>>24), byte(secs>>16), byte(secs>>8), byte(secs)
	off := sntpFrom(resp, now, now)
	if off > -29*time.Second || off < -31*time.Second {
		t.Errorf("offset %v, want about -30s (local clock behind)", off)
	}
}
