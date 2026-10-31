package main

import (
	"bytes"
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Stage 49.10.6b.3: the release binary is signed on the release machine and
// the server checks the signature before the binary runs.

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// runTool runs the command in-process and returns its exit code and output.
func runTool(args ...string) (int, string, string) {
	var out, errb bytes.Buffer
	code := run(args, &out, &errb)
	return code, out.String(), errb.String()
}

// signTo signs in with key and writes the raw signature to sig, the way
// deploy.ps1 does with the base64 line sign prints.
func signTo(t *testing.T, key, in, sig string) {
	t.Helper()
	code, out, errs := runTool("sign", "-key", key, "-in", in)
	if code != 0 {
		t.Fatalf("sign: exit %d: %s", code, errs)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(out))
	if err != nil {
		t.Fatalf("sign printed %q, not base64: %v", out, err)
	}
	if err := os.WriteFile(sig, raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

func writePub(t *testing.T, key, path string) {
	t.Helper()
	code, out, errs := runTool("pub", "-key", key)
	if code != 0 {
		t.Fatalf("pub: exit %d: %s", code, errs)
	}
	if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A key is made once and never replaced; what it signs verifies, and a
// changed file, another key, a cut or missing signature do not. The private
// key never appears in anything the tool prints.
func TestStage49106b3ReleaseSignatureSignsAndRefuses(t *testing.T) {
	dir := t.TempDir()
	key := filepath.Join(dir, "keys", "signing.key")
	code, out, errs := runTool("keygen", "-key", key)
	if code != 0 {
		t.Fatalf("keygen: exit %d: %s", code, errs)
	}
	keyBytes, err := os.ReadFile(key)
	if err != nil || !bytes.Contains(keyBytes, []byte("BEGIN PRIVATE KEY")) {
		t.Fatalf("keygen wrote no PKCS#8 key (%v)", err)
	}
	printed := out + errs
	if code, again, errs := runTool("keygen", "-key", key); code == 0 {
		t.Fatalf("a second keygen replaced the key: %s", again)
	} else {
		printed += again + errs
	}
	if after, _ := os.ReadFile(key); !bytes.Equal(after, keyBytes) {
		t.Fatal("a refused keygen still changed the key file")
	}

	pub := filepath.Join(dir, "signing.pub")
	writePub(t, key, pub)
	bin := filepath.Join(dir, "erp-server")
	if err := os.WriteFile(bin, bytes.Repeat([]byte("ELF linux binary "), 4096), 0o755); err != nil {
		t.Fatal(err)
	}
	sig := bin + ".sig"
	signTo(t, key, bin, sig)
	if code, out, errs := runTool("verify", "-pub", pub, "-in", bin, "-sig", sig); code != 0 {
		t.Fatalf("a good signature did not verify: %s %s", out, errs)
	} else {
		printed += out + errs
	}
	code, fp, _ := runTool("fingerprint", "-pub", pub)
	if code != 0 || len(strings.TrimSpace(fp)) != 16 || !strings.Contains(printed, strings.TrimSpace(fp)) {
		t.Errorf("fingerprint %q is not the 16-digit name keygen printed (%q)", fp, printed)
	}

	refused := func(name, pubPath, in, sigPath string) {
		t.Helper()
		code, out, errs := runTool("verify", "-pub", pubPath, "-in", in, "-sig", sigPath)
		if code == 0 {
			t.Errorf("%s: verify passed: %s", name, out)
		}
		printed += out + errs
	}
	changed := filepath.Join(dir, "changed")
	b, _ := os.ReadFile(bin)
	b[100] ^= 1
	os.WriteFile(changed, b, 0o755)
	refused("one byte changed", pub, changed, sig)

	otherKey := filepath.Join(dir, "other.key")
	if code, _, errs := runTool("keygen", "-key", otherKey); code != 0 {
		t.Fatal(errs)
	}
	otherPub := filepath.Join(dir, "other.pub")
	writePub(t, otherKey, otherPub)
	refused("checked against another key", otherPub, bin, sig)
	otherSig := filepath.Join(dir, "other.sig")
	signTo(t, otherKey, bin, otherSig)
	refused("signed by another key", pub, bin, otherSig)

	cut := filepath.Join(dir, "cut.sig")
	s, _ := os.ReadFile(sig)
	os.WriteFile(cut, s[:40], 0o644)
	refused("a cut signature", pub, bin, cut)
	refused("no signature file", pub, bin, filepath.Join(dir, "absent.sig"))
	refused("a private key passed as the public key", key, bin, sig)

	body := strings.TrimSpace(strings.Split(strings.Split(string(keyBytes), "-----\n")[1], "-----END")[0])
	for _, line := range strings.Split(body, "\n") {
		if line = strings.TrimSpace(line); len(line) > 8 && strings.Contains(printed, line) {
			t.Fatalf("the private key was printed: %q", line)
		}
	}
}

// The server checks the signature with its own openssl, not with a tool of
// ours: what releasesign signs must verify there, and nothing else must.
func TestStage49106b3OpenSSLAcceptsTheReleaseSignature(t *testing.T) {
	bash := posixBash(t)
	dir := t.TempDir()
	key := filepath.Join(dir, "signing.key")
	if code, _, errs := runTool("keygen", "-key", key); code != 0 {
		t.Fatal(errs)
	}
	pub := filepath.Join(dir, "signing.pub")
	writePub(t, key, pub)
	bin := filepath.Join(dir, "erp-server")
	os.WriteFile(bin, bytes.Repeat([]byte{0x7f, 'E', 'L', 'F', 1, 2, 3}, 300000), 0o755) // ~2 MB
	signTo(t, key, bin, bin+".sig")
	check := func(in string) error {
		cmd := exec.Command(bash, "-c", `openssl pkeyutl -verify -pubin -inkey "$1" -rawin -in "$2" -sigfile "$3"`, "x",
			filepath.ToSlash(pub), filepath.ToSlash(in), filepath.ToSlash(bin+".sig"))
		out, err := cmd.CombinedOutput()
		if err != nil {
			return &exec.ExitError{Stderr: out}
		}
		return nil
	}
	if err := check(bin); err != nil {
		t.Fatalf("openssl refused a releasesign signature: %s", err.(*exec.ExitError).Stderr)
	}
	b, _ := os.ReadFile(bin)
	b[len(b)-1] ^= 1
	changed := filepath.Join(dir, "changed")
	os.WriteFile(changed, b, 0o755)
	if check(changed) == nil {
		t.Fatal("openssl accepted the signature on a changed binary")
	}
	// The fingerprint the server prints is the one the release machine prints.
	out, err := exec.Command(bash, "-c", `openssl pkey -pubin -in "$1" -outform DER | sha256sum | cut -c1-16`, "x", filepath.ToSlash(pub)).Output()
	_, fp, _ := runTool("fingerprint", "-pub", pub)
	if err != nil || strings.TrimSpace(string(out)) != strings.TrimSpace(fp) {
		t.Errorf("server fingerprint %q, release machine %q (%v)", out, fp, err)
	}
}

// The real deploy/remote_deploy.sh, with systemd, sudo, curl and migrate.sh
// replaced by stubs: an unsigned, changed or foreign-key binary is refused
// before it runs (migrate never starts) and nothing is swapped; the first
// signed deploy pins its key and goes live; a later one with another key is
// refused even when it brings that key along.
func TestStage49106b3DeployRefusesABinaryTheReleaseMachineDidNotSign(t *testing.T) {
	bash := posixBash(t)
	script := filepath.Join(repoRoot(t), "deploy", "remote_deploy.sh")
	keys := t.TempDir()
	key, otherKey := filepath.Join(keys, "signing.key"), filepath.Join(keys, "other.key")
	for _, k := range []string{key, otherKey} {
		if code, _, errs := runTool("keygen", "-key", k); code != 0 {
			t.Fatal(errs)
		}
	}
	pin := filepath.Join(t.TempDir(), "release-signing.pub") // the box's /etc/erp/release-signing.pub

	stubs := map[string]string{
		"sudo":       `"$@"`,
		"sleep":      `exit 0`,
		"chown":      `exit 0`,
		"journalctl": `echo synthetic-journal`,
		// restart runs whatever erp-server is on disk now.
		"systemctl": `if [ "$1" = restart ]; then
  if grep -q NEW_BINARY "$REMOTE_DIR/erp-server"; then printf '%s' "$DEPLOY_COMMIT"; else printf old-commit; fi > "$REMOTE_DIR/.running"
fi
exit 0`,
		"curl": `running="$(cat "$REMOTE_DIR/.running" 2>/dev/null || echo old-commit)"
case "$*" in *"-o /dev/null"*) printf 200 ;; *) printf '{"status":"ok","git_commit":"%s"}' "$running" ;; esac`,
	}

	type box struct{ dir string }
	newBox := func(t *testing.T) box {
		t.Helper()
		d := t.TempDir()
		for _, sub := range []string{"public", "public.new", "deploy", "bin"} {
			os.MkdirAll(filepath.Join(d, sub), 0o755)
		}
		os.WriteFile(filepath.Join(d, "erp-server"), []byte("#!/usr/bin/env bash\n# OLD_BINARY\nexit 0\n"), 0o755)
		os.WriteFile(filepath.Join(d, "erp-server.new"), []byte("#!/usr/bin/env bash\n# NEW_BINARY\nexit 0\n"), 0o755)
		os.WriteFile(filepath.Join(d, "public", "app.js"), []byte("OLD_PUBLIC"), 0o644)
		os.WriteFile(filepath.Join(d, "public.new", "app.js"), []byte("NEW_PUBLIC"), 0o644)
		os.WriteFile(filepath.Join(d, ".running"), []byte("old-commit"), 0o644)
		os.WriteFile(filepath.Join(d, "deploy", "migrate.sh"), []byte("#!/usr/bin/env bash\ntouch \"$REMOTE_DIR/.migrated\"\n"), 0o755)
		for name, body := range stubs {
			os.WriteFile(filepath.Join(d, "bin", name), []byte("#!/usr/bin/env bash\n"+body+"\n"), 0o755)
		}
		raw, _ := os.ReadFile(script)
		os.WriteFile(filepath.Join(d, "deploy", "remote_deploy.sh"), bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n")), 0o755)
		return box{d}
	}
	deploy := func(t *testing.T, b box) (string, error) {
		t.Helper()
		harness := `REMOTE_DIR="$(cygpath -u "$REMOTE_DIR" 2>/dev/null || printf '%s' "$REMOTE_DIR")"; export REMOTE_DIR
export PATH="$REMOTE_DIR/bin:$PATH"; exec bash "$REMOTE_DIR/deploy/remote_deploy.sh"`
		cmd := exec.Command(bash, "-c", harness)
		cmd.Env = []string{}
		for _, kv := range os.Environ() {
			if n, _, _ := strings.Cut(kv, "="); strings.EqualFold(n, "OPS_ALERT_WEBHOOK_URL") {
				continue
			}
			cmd.Env = append(cmd.Env, kv)
		}
		cmd.Env = append(cmd.Env, "REMOTE_DIR="+filepath.ToSlash(b.dir), "DEPLOY_COMMIT=newcommit", "RELEASE_SIGNING_PIN="+filepath.ToSlash(pin))
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	read := func(b box, rel string) string {
		s, _ := os.ReadFile(filepath.Join(b.dir, rel))
		return string(s)
	}
	exists := func(b box, rel string) bool { _, err := os.Stat(filepath.Join(b.dir, rel)); return err == nil }
	refusedUntouched := func(t *testing.T, name string, b box, out string, err error, why string) {
		t.Helper()
		if err == nil || !strings.Contains(out, "DEPLOY-REFUSED") || strings.Contains(out, "DEPLOY-OK") {
			t.Fatalf("%s: not refused (err %v):\n%s", name, err, out)
		}
		if !strings.Contains(out, why) {
			t.Errorf("%s: the refusal does not say %q:\n%s", name, why, out)
		}
		if !strings.Contains(read(b, "erp-server"), "OLD_BINARY") || read(b, "public/app.js") != "OLD_PUBLIC" {
			t.Errorf("%s: the live build was changed", name)
		}
		if exists(b, ".migrated") || exists(b, "erp-server.new") || exists(b, "public.new") {
			t.Errorf("%s: the refused binary ran (migrate) or was left ready to run", name)
		}
		held, _ := filepath.Glob(filepath.Join(b.dir, "erp-server.failed-unsigned-*"))
		if len(held) != 1 || !strings.Contains(read(b, filepath.Base(held[0])), "NEW_BINARY") {
			t.Errorf("%s: the refused binary was not kept aside for inspection (%v)", name, held)
		}
	}
	ship := func(b box, signKey, shippedKey string) {
		signTo(t, signKey, filepath.Join(b.dir, "erp-server.new"), filepath.Join(b.dir, "erp-server.new.sig"))
		writePub(t, shippedKey, filepath.Join(b.dir, "deploy", "release-signing.pub"))
	}

	// No signature at all (a binary copied in by hand, or an old deploy.ps1).
	b := newBox(t)
	writePub(t, key, filepath.Join(b.dir, "deploy", "release-signing.pub"))
	out, err := deploy(t, b)
	refusedUntouched(t, "unsigned", b, out, err, "came without its signature")
	if _, err := os.Stat(pin); err == nil {
		t.Fatal("an unsigned deploy pinned a key")
	}

	// The first signed deploy: nothing pinned yet, so it pins its key and goes live.
	b = newBox(t)
	ship(b, key, key)
	out, err = deploy(t, b)
	if err != nil || !strings.Contains(out, "DEPLOY-OK") || !strings.Contains(out, "RELEASE-KEY-PINNED") || !strings.Contains(out, "RELEASE-SIGNATURE-OK") {
		t.Fatalf("first signed deploy did not pin and go live (err %v):\n%s", err, out)
	}
	if !strings.Contains(read(b, "erp-server"), "NEW_BINARY") || read(b, "public/app.js") != "NEW_PUBLIC" || !exists(b, ".migrated") {
		t.Fatal("first signed deploy: the new build is not live")
	}
	pinned, _ := os.ReadFile(pin)
	shipped, _ := os.ReadFile(filepath.Join(b.dir, "deploy", "release-signing.pub"))
	if !bytes.Equal(pinned, shipped) {
		t.Fatal("the pinned key is not the key the first signed deploy brought")
	}

	// Signed with the pinned key again: goes live, the pin stays as it was.
	b = newBox(t)
	ship(b, key, key)
	if out, err = deploy(t, b); err != nil || !strings.Contains(out, "DEPLOY-OK") || strings.Contains(out, "RELEASE-KEY-PINNED") {
		t.Fatalf("a later signed deploy (err %v):\n%s", err, out)
	}

	// Changed after signing.
	b = newBox(t)
	ship(b, key, key)
	os.WriteFile(filepath.Join(b.dir, "erp-server.new"), []byte("#!/usr/bin/env bash\n# NEW_BINARY tampered\nexit 0\n"), 0o755)
	out, err = deploy(t, b)
	refusedUntouched(t, "changed after signing", b, out, err, "does not verify")

	// Another key, bringing its own public key along.
	b = newBox(t)
	ship(b, otherKey, otherKey)
	out, err = deploy(t, b)
	refusedUntouched(t, "another key", b, out, err, "the release key changed")

	// Another key, but shipped with the pinned public key.
	b = newBox(t)
	ship(b, otherKey, key)
	out, err = deploy(t, b)
	refusedUntouched(t, "another key behind the pinned public key", b, out, err, "does not verify")

	if after, _ := os.ReadFile(pin); !bytes.Equal(after, pinned) {
		t.Fatal("a refused deploy changed the pinned key")
	}
}

// The release public key, once committed, is a public key - and no private
// key is ever committed beside the release or deploy scripts.
func TestStage49106b3NoReleasePrivateKeyInTheRepo(t *testing.T) {
	root := repoRoot(t)
	if _, err := os.Stat(filepath.Join(root, "release", "signing.pub")); err == nil {
		if _, err := loadPublicKey(filepath.Join(root, "release", "signing.pub")); err != nil {
			t.Errorf("release/signing.pub: %v", err)
		}
	}
	for _, dir := range []string{"release", "deploy"} {
		filepath.WalkDir(filepath.Join(root, dir), func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				if d != nil && d.IsDir() && (d.Name() == "reports" || d.Name() == "build") {
					return filepath.SkipDir // never committed (git-ignored)
				}
				return nil
			}
			if b, err := os.ReadFile(p); err == nil && bytes.Contains(b, []byte("PRIVATE KEY-----\n")) {
				t.Errorf("%s holds a private key - the release key lives only on the release machine", p)
			}
			return nil
		})
	}
}

// posixBash is a bash that runs deploy/*.sh as the server does: Git for
// Windows' bash on Windows (System32\bash.exe is WSL, which sees none of
// these paths), the bash on PATH elsewhere; skipped without one, or without
// an openssl that checks ed25519 (OpenSSL 3).
func posixBash(t *testing.T) string {
	t.Helper()
	bash := ""
	if runtime.GOOS == "windows" {
		if git, err := exec.LookPath("git"); err == nil {
			for dir, i := filepath.Dir(git), 0; i < 3 && bash == ""; dir, i = filepath.Dir(dir), i+1 {
				if p := filepath.Join(dir, "bin", "bash.exe"); fileExists(p) {
					bash = p
				}
			}
		}
	} else if p, err := exec.LookPath("bash"); err == nil {
		bash = p
	}
	if bash == "" {
		t.Skip("no bash to run deploy/*.sh with")
	}
	out, err := exec.Command(bash, "-c", `for c in openssl sha256sum install cut tr grep; do command -v $c >/dev/null || echo missing $c; done
openssl pkeyutl -help 2>&1 | grep -q -- -rawin || echo openssl without -rawin`).CombinedOutput()
	if err != nil || len(bytes.TrimSpace(out)) > 0 {
		t.Skipf("bash at %s cannot run the signature check: %v %s", bash, err, out)
	}
	return bash
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
