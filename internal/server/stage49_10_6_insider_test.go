package server

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"custom_erp/db"
	"custom_erp/engines"
)

// Stage 49.10.6 - insider and red-team scenarios. The map from each scenario
// to the tests that prove it is docs/security/insider-scenarios.md; these are
// the scenarios that had no test, and the check that keeps the map honest.

var insiderScenarios = []string{
	"Cashier payroll access", "Admin cross-tenant action", "DBA audit tamper",
	"Support impersonation", "Malicious import or attachment", "Price, refund and threshold fraud",
	"Owner crossover", "Connector SSRF and secret theft", "Stolen backup",
	"Compromised CI artifact", "Destructive operator",
}

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// Every scenario the item names has a section; every section names a test
// or says what is not covered; every test named exists, untagged.
func TestStage49106InsiderScenarioMapIsHonest(t *testing.T) {
	root := repoRoot(t)
	doc, err := os.ReadFile(filepath.Join(root, "docs", "security", "insider-scenarios.md"))
	if err != nil {
		t.Fatal(err)
	}
	doc = []byte(strings.ReplaceAll(string(doc), "\r\n", "\n")) // a Windows checkout writes CRLF
	sections := regexp.MustCompile(`(?m)^## \d+\. (.+)$`).FindAllStringSubmatchIndex(string(doc), -1)
	if len(sections) != len(insiderScenarios) {
		t.Fatalf("%d scenario sections, want %d", len(sections), len(insiderScenarios))
	}

	// Every func Test... in the repository's test files, and whether its file
	// is kept out of the ordinary run by a build tag.
	tests := map[string]bool{} // name -> tagged
	funcRe := regexp.MustCompile(`(?m)^func (Test\w+)\(`)
	for _, dir := range []string{"engines", "internal", "db", "cmd"} {
		filepath.WalkDir(filepath.Join(root, dir), func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, "_test.go") {
				return nil
			}
			src, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			tagged := strings.HasPrefix(string(src), "//go:build")
			for _, m := range funcRe.FindAllStringSubmatch(string(src), -1) {
				tests[m[1]] = tagged
			}
			return nil
		})
	}
	if len(tests) < 1000 {
		t.Fatalf("found only %d tests - the walk is wrong", len(tests))
	}

	cited := regexp.MustCompile("`(Test\\w+)`")
	for i, s := range sections {
		title := string(doc[s[2]:s[3]])
		if title != insiderScenarios[i] {
			t.Errorf("section %d is %q, want %q", i+1, title, insiderScenarios[i])
		}
		end := len(doc)
		if i+1 < len(sections) {
			end = sections[i+1][0]
		}
		body := string(doc[s[1]:end])
		names := cited.FindAllStringSubmatch(body, -1)
		if len(names) == 0 && !strings.Contains(body, "**Not covered:**") {
			t.Errorf("%s: names no test and says nothing is uncovered", title)
		}
		if !strings.Contains(body, "**Not covered:**") {
			t.Errorf("%s: has no **Not covered:** line (write \"none for the scenario as written\" when that is true)", title)
		}
		for _, n := range names {
			tagged, ok := tests[n[1]]
			switch {
			case !ok:
				t.Errorf("%s: cites %s, which does not exist", title, n[1])
			case tagged:
				t.Errorf("%s: cites %s, which a build tag keeps out of the ordinary run", title, n[1])
			}
		}
	}
}

// Support impersonation: opening a client as its support user leaves the
// client's own audit log naming the operator who did it, at issue and at
// sign-in.
func TestStage49106SupportSessionLeavesEvidence(t *testing.T) {
	db.InitDB(testConnStr())
	c := provisionHarnessClient(t, "insider_"+time.Now().Format("150405"))
	operator := "operator-qa-49106"
	code, err := engines.IssueSupportHandoff(c.tenant, operator)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if _, _, _, _, _, err := engines.RedeemSupportHandoff(c.tenant, code); err != nil {
		t.Fatalf("redeem: %v", err)
	}
	count := func(status, like string) int {
		var n int
		if err := db.DB.QueryRow(`SELECT count(*) FROM `+c.schema+`.audit_logs WHERE action = 'LOGIN' AND status = $1 AND details LIKE $2`,
			status, like).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if count("OPERATOR_OPEN_ISSUED", "%"+operator+"%") != 1 {
		t.Error("issuing the hand-off left no audit row naming the operator in the client's log")
	}
	if count("OPERATOR_SESSION", "%hand-off%") != 1 {
		t.Error("the support sign-in left no audit row in the client's log")
	}
	if _, _, _, _, _, err := engines.RedeemSupportHandoff(c.tenant, code); err == nil {
		t.Error("a hand-off code worked twice")
	}
}

// Stolen backup: what leaves the box is only ever the encrypted dump.
func TestStage49106BackupLeavesTheBoxEncrypted(t *testing.T) {
	script := readLF(t, filepath.Join(repoRoot(t), "deploy", "backup.sh"))
	for _, must := range []string{
		`set -euo pipefail`,                      // a failed encryption stops the run
		`: "${BACKUP_ENCRYPTION_KEY:?`,           // no key, no backup
		`openssl enc -aes-256-cbc -pbkdf2 -salt`, // encrypted at rest
	} {
		if !strings.Contains(script, must) {
			t.Errorf("deploy/backup.sh no longer contains %q", must)
		}
	}
	enc := strings.Index(script, `openssl enc -aes-256-cbc`)
	rm := strings.Index(script, `rm -f "$dump"`)
	if enc < 0 || rm < enc {
		t.Error("the plain dump is not removed right after it is encrypted")
	}
	// Every off-box copy is the encrypted dump, its checksum, or a media
	// file sealed by openssl just before (49.10.6a, "$sealed"); a new plain
	// copy of anything fails here.
	for _, m := range regexp.MustCompile(`offsite_put "([^"]+)"`).FindAllStringSubmatch(script, -1) {
		switch m[1] {
		case "$enc", "$enc.sha256", "$sealed":
		default:
			t.Errorf("offsite_put copies %q off the box - only encrypted files may leave", m[1])
		}
	}
	if !regexp.MustCompile(`openssl enc -aes-256-cbc -pbkdf2 -salt -in "\$dir/\$rel" -out "\$sealed" -pass env:BACKUP_ENCRYPTION_KEY \\\n\s+&& offsite_put "\$sealed"`).MatchString(script) {
		t.Error("media_sync no longer seals each file with the backup key right before it is uploaded")
	}
}

// Stolen backup, the media half (49.10.6a): runs the real deploy/backup.sh
// and deploy/restore_media.sh under bash, with pg_dump, psql and curl
// replaced by stubs - curl's "uploads" land in a folder standing in for the
// bucket - so what would leave the box can be read. Without the key nothing
// leaves; with it every object is sealed (no byte of a plain file in it), the
// files come back exactly with the key and not at all with a wrong one.
func TestStage49106aMediaCopyLeavesTheBoxEncrypted(t *testing.T) {
	bash := posixBash(t)
	root := repoRoot(t)
	backupScript := filepath.ToSlash(filepath.Join(root, "deploy", "backup.sh"))
	restoreScript := filepath.ToSlash(filepath.Join(root, "deploy", "restore_media.sh"))
	const key = "test-backup-key-49106a"
	const marker = "PLAINTEXT-MARKER-49106a"

	// The stubs: pg_dump writes a dump holding the marker, psql swallows the
	// run record, curl copies -T's file to <bucket>/<object key> and logs it.
	stubs := t.TempDir()
	for name, body := range map[string]string{
		"pg_dump": `out=""; while [ $# -gt 0 ]; do case "$1" in -f) out="$2"; shift 2;; *) shift;; esac; done
printf 'PGDMP dump %s\n' "` + marker + `" > "$out"`,
		"psql": `cat >/dev/null; exit 0`,
		"curl": `cat >/dev/null
src=""; url=""
while [ $# -gt 0 ]; do case "$1" in -T) src="$2"; shift 2;; -m|-K|-H|--retry|--retry-delay|--aws-sigv4) shift 2;; -*) shift;; *) url="$1"; shift;; esac; done
obj="${url#https://bucket.invalid/erp-backups/}"
mkdir -p "$FAKE_BUCKET/$(dirname "$obj")"
cp "$src" "$FAKE_BUCKET/$obj"
printf '%s\n' "$obj" >> "$FAKE_BUCKET.log"`,
	} {
		if err := os.WriteFile(filepath.Join(stubs, name), []byte("#!/usr/bin/env bash\n"+body+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// The media store: a receipt, a vendor invoice copy, an empty file.
	media := t.TempDir()
	originals := map[string][]byte{
		"receipts/2026/exp-001.pdf":  []byte("%PDF-1.4 expense receipt " + marker + "\x00\x01\x02"),
		"vendor-invoices/inv-7.jpg":  append([]byte{0xFF, 0xD8, 0xFF}, []byte("vendor invoice copy "+marker)...),
		"products/empty-placeholder": {},
	}
	for rel, b := range originals {
		p := filepath.Join(media, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// run runs a script under bash with the stubs first on PATH.
	run := func(env map[string]string, script string, args ...string) (string, error) {
		t.Helper()
		harness := `shim="$STUB_DIR"; if command -v cygpath >/dev/null 2>&1; then shim="$(cygpath -u "$STUB_DIR")"; fi
export PATH="$shim:$PATH"; exec "$BASH" "$@"`
		cmd := exec.Command(bash, append([]string{"-c", harness, "harness", script}, args...)...)
		cmd.Env = []string{}
		for _, kv := range os.Environ() {
			name, _, _ := strings.Cut(kv, "=")
			switch strings.ToUpper(name) {
			case "BACKUP_ENCRYPTION_KEY", "OPS_ALERT_WEBHOOK_URL", "TENANT_SCHEMA", "MEDIA_MANIFEST", "BACKUP_DIR", "MEDIA_DIR", "DATABASE_URL":
				continue
			}
			if strings.HasPrefix(strings.ToUpper(name), "OFFSITE_S3_") {
				continue
			}
			cmd.Env = append(cmd.Env, kv)
		}
		cmd.Env = append(cmd.Env, "STUB_DIR="+filepath.ToSlash(stubs))
		for k, v := range env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	backupEnv := func(bucket, backups string, withKey bool) map[string]string {
		env := map[string]string{
			"DATABASE_URL": "postgres://stub/none", "BACKUP_DIR": filepath.ToSlash(backups), "MEDIA_DIR": filepath.ToSlash(media),
			"OFFSITE_S3_ENDPOINT": "https://bucket.invalid", "OFFSITE_S3_BUCKET": "erp-backups", "OFFSITE_S3_ACCESS_KEY_ID": "id",
			"OFFSITE_S3_SECRET_ACCESS_KEY": "secret", "OFFSITE_S3_PREFIX": "testhost", "FAKE_BUCKET": filepath.ToSlash(bucket),
		}
		if withKey {
			env["BACKUP_ENCRYPTION_KEY"] = key
		}
		return env
	}
	objects := func(bucket string) map[string][]byte {
		t.Helper()
		out := map[string][]byte{}
		filepath.WalkDir(bucket, func(p string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				rel, _ := filepath.Rel(bucket, p)
				b, _ := os.ReadFile(p)
				out[filepath.ToSlash(rel)] = b
			}
			return nil
		})
		return out
	}

	// No key: refused before anything is dumped or sent.
	bucket := t.TempDir()
	if out, err := run(backupEnv(bucket, t.TempDir(), false), backupScript); err == nil || !strings.Contains(out, "BACKUP_ENCRYPTION_KEY") {
		t.Fatalf("backup.sh ran without its key (err %v):\n%s", err, out)
	}
	if got := objects(bucket); len(got) != 0 {
		t.Fatalf("without the key %d object(s) left the box", len(got))
	}

	// With the key: the dump and every media file leave sealed, nothing else.
	backups := t.TempDir()
	if out, err := run(backupEnv(bucket, backups, true), backupScript); err != nil {
		t.Fatalf("backup.sh: %v\n%s", err, out)
	}
	got := objects(bucket)
	var dumps int
	for obj, b := range got {
		switch {
		case strings.HasPrefix(obj, "testhost/database/custom_erp_") && strings.HasSuffix(obj, ".dump.enc.sha256"):
			continue
		case strings.HasPrefix(obj, "testhost/database/custom_erp_") && strings.HasSuffix(obj, ".dump.enc"):
			dumps++
		case strings.HasPrefix(obj, "testhost/media/") && strings.HasSuffix(obj, ".enc"):
			if _, ok := originals[strings.TrimSuffix(strings.TrimPrefix(obj, "testhost/media/"), ".enc")]; !ok {
				t.Errorf("unexpected media object %s", obj)
			}
		default:
			t.Errorf("object %s left the box - only sealed files and checksums may", obj)
			continue
		}
		if !bytes.HasPrefix(b, []byte("Salted__")) || bytes.Contains(b, []byte(marker)) {
			t.Errorf("object %s is not an openssl-sealed file, or carries plain text", obj)
		}
	}
	if dumps != 1 {
		t.Errorf("%d database dumps went up, want 1", dumps)
	}
	for rel := range originals {
		if _, ok := got["testhost/media/"+rel+".enc"]; !ok {
			t.Errorf("media file %s was not copied off the box", rel)
		}
	}
	entries, _ := os.ReadDir(backups)
	for _, e := range entries {
		if n := e.Name(); strings.HasSuffix(n, ".dump") {
			t.Errorf("the plain dump %s stayed on the box", n)
		}
	}

	// The manifest holds: a second night sends the dump, no media file again.
	os.Remove(bucket + ".log")
	if out, err := run(backupEnv(bucket, backups, true), backupScript); err != nil {
		t.Fatalf("second backup.sh: %v\n%s", err, out)
	}
	if log, _ := os.ReadFile(bucket + ".log"); strings.Contains(string(log), "/media/") {
		t.Errorf("unchanged media files were sent again:\n%s", log)
	}

	// Restore: the key brings every file back exactly; a wrong key or none
	// restores nothing.
	sealedMedia := filepath.ToSlash(filepath.Join(bucket, "testhost", "media"))
	wrong := t.TempDir()
	if out, err := run(map[string]string{"BACKUP_ENCRYPTION_KEY": "not-the-key"}, restoreScript, sealedMedia, filepath.ToSlash(wrong)); err == nil {
		t.Errorf("restore_media.sh succeeded with a wrong key:\n%s", out)
	}
	if out, err := run(nil, restoreScript, sealedMedia, filepath.ToSlash(wrong)); err == nil {
		t.Errorf("restore_media.sh ran without a key:\n%s", out)
	}
	for rel := range originals {
		if _, err := os.Stat(filepath.Join(wrong, filepath.FromSlash(rel))); err == nil {
			t.Errorf("a wrong key restored %s", rel)
		}
	}
	restored := t.TempDir()
	if out, err := run(map[string]string{"BACKUP_ENCRYPTION_KEY": key}, restoreScript, sealedMedia, filepath.ToSlash(restored)); err != nil {
		t.Fatalf("restore_media.sh: %v\n%s", err, out)
	}
	for rel, want := range originals {
		if b, err := os.ReadFile(filepath.Join(restored, filepath.FromSlash(rel))); err != nil || !bytes.Equal(b, want) {
			t.Errorf("%s did not come back as it was (err %v)", rel, err)
		}
	}

	// media_sync itself refuses without the key, whoever calls it.
	fn := regexp.MustCompile(`(?ms)^media_sync\(\) \{\n.*?^\}\n`).FindString(readLF(t, filepath.Join(root, "deploy", "backup.sh")))
	if fn == "" {
		t.Fatal("media_sync() not found in deploy/backup.sh")
	}
	calls := filepath.Join(t.TempDir(), "calls.log")
	stubbed := `offsite_put() { echo "PUT $*" >> "$CALLS"; }
record_run() { echo "RUN $*" >> "$CALLS"; }
alert_ops() { echo "ALERT $*" >> "$CALLS"; }
offsite_prefix() { printf testhost; }
RUN_SCOPE=""
` + fn + "media_sync\n"
	script := filepath.Join(t.TempDir(), "media_sync_alone.sh")
	if err := os.WriteFile(script, []byte(stubbed), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := run(map[string]string{"CALLS": filepath.ToSlash(calls), "MEDIA_DIR": filepath.ToSlash(media), "BACKUP_DIR": filepath.ToSlash(t.TempDir())}, filepath.ToSlash(script)); err == nil {
		t.Errorf("media_sync ran without the key:\n%s", out)
	}
	log, _ := os.ReadFile(calls)
	if strings.Contains(string(log), "PUT ") || !strings.Contains(string(log), "ALERT BACKUP_OFFSITE_FAILED") || !strings.Contains(string(log), "RUN Failed") {
		t.Errorf("media_sync without the key must send nothing, record a failed run and alert:\n%s", log)
	}
}

// posixBash is a bash that runs deploy/*.sh the way the server does: Git for
// Windows' bash on Windows (System32\bash.exe is WSL, which sees none of
// these paths), the bash on PATH elsewhere. The test is skipped when there is
// none, or it lacks the tools the scripts use.
func posixBash(t *testing.T) string {
	t.Helper()
	bash := ""
	if runtime.GOOS == "windows" {
		if git, err := exec.LookPath("git"); err == nil {
			for dir, i := filepath.Dir(git), 0; i < 3 && bash == ""; dir, i = filepath.Dir(dir), i+1 {
				if p := filepath.Join(dir, "bin", "bash.exe"); fileExistsLocal(p) {
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
	if out, err := exec.Command(bash, "-c", "for c in openssl find sort comm cut mktemp sha256sum; do command -v $c >/dev/null || echo missing $c; done").CombinedOutput(); err != nil || len(bytes.TrimSpace(out)) > 0 {
		t.Skipf("bash at %s cannot run deploy/backup.sh: %v %s", bash, err, out)
	}
	return bash
}

func fileExistsLocal(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}

// readLF reads a file with CRLF folded to LF.
func readLF(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(b), "\r\n", "\n")
}
