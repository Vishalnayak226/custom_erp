package server

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Backup failures that used to go unseen: a bucket rule that silently ages
// out the media copy (49.10.6c.1), and a backup that dies on a full disk
// (49.13.7). Both run the real deploy/backup.sh under bash with pg_dump,
// psql, curl (and df / openssl where the case needs it) replaced by stubs.

// backupStubCurl answers the bucket: a PUT (-T) lands in $FAKE_BUCKET, the
// lifecycle GET answers $FAKE_LIFECYCLE_CODE with the file
// $FAKE_LIFECYCLE_BODY. Every call's arguments are logged to $FAKE_BUCKET.calls.
const backupStubCurl = `cat >/dev/null
printf '%s\n' "$*" >> "$FAKE_BUCKET.calls"
src=""; url=""; out=""
while [ $# -gt 0 ]; do case "$1" in
  -T) src="$2"; shift 2;; -o) out="$2"; shift 2;;
  -m|-K|-H|-w|-X|--retry|--retry-delay|--aws-sigv4) shift 2;;
  -*) shift;; *) url="$1"; shift;; esac; done
if [ -z "$src" ]; then
  [ -n "$out" ] && cat "${FAKE_LIFECYCLE_BODY:-/dev/null}" > "$out"
  printf '%s' "${FAKE_LIFECYCLE_CODE:-404}"
  exit 0
fi
obj="${url#https://bucket.invalid/erp-backups/}"
mkdir -p "$FAKE_BUCKET/$(dirname "$obj")"
cp "$src" "$FAKE_BUCKET/$obj"`

// runBackupScript runs script under bash with the stub programs first on
// PATH and a clean environment plus env.
func runBackupScript(t *testing.T, bash string, stubs map[string]string, env map[string]string, script string, args ...string) (string, error) {
	t.Helper()
	dir := t.TempDir()
	for name, body := range stubs {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/usr/bin/env bash\n"+body+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	harness := `shim="$STUB_DIR"; if command -v cygpath >/dev/null 2>&1; then shim="$(cygpath -u "$STUB_DIR")"; fi
export PATH="$shim:$PATH"; exec "$BASH" "$@"`
	cmd := exec.Command(bash, append([]string{"-c", harness, "harness", script}, args...)...)
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		switch u := strings.ToUpper(name); {
		case strings.HasPrefix(u, "OFFSITE_S3_"), u == "BACKUP_ENCRYPTION_KEY", u == "OPS_ALERT_WEBHOOK_URL", u == "TENANT_SCHEMA",
			u == "MEDIA_MANIFEST", u == "BACKUP_DIR", u == "MEDIA_DIR", u == "DATABASE_URL":
			continue
		}
		cmd.Env = append(cmd.Env, kv)
	}
	cmd.Env = append(cmd.Env, "STUB_DIR="+filepath.ToSlash(dir))
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func backupTestEnv(bucket, backups, media string) map[string]string {
	return map[string]string{
		"DATABASE_URL": "postgres://stub/none", "BACKUP_DIR": filepath.ToSlash(backups), "MEDIA_DIR": filepath.ToSlash(media),
		"BACKUP_ENCRYPTION_KEY": "test-backup-key", "OFFSITE_S3_ENDPOINT": "https://bucket.invalid", "OFFSITE_S3_BUCKET": "erp-backups",
		"OFFSITE_S3_ACCESS_KEY_ID": "id", "OFFSITE_S3_SECRET_ACCESS_KEY": "secret-49106c", "OFFSITE_S3_PREFIX": "testhost",
		"FAKE_BUCKET": filepath.ToSlash(bucket),
	}
}

// Stage 49.10.6c.1: the media copy is upload-only, so a lifecycle rule that
// expires media/ deletes the only off-box copy of every unchanged file. The
// check reads the bucket's rules (read-only) and says, for each shape of rule
// a provider writes, whether media would expire - and the nightly alerts it.
func TestStage49106c1MediaRetentionCheckFindsAnExpiringMediaCopy(t *testing.T) {
	bash := posixBash(t)
	script := filepath.ToSlash(filepath.Join(repoRoot(t), "deploy", "backup.sh"))
	stubs := map[string]string{
		"curl":    backupStubCurl,
		"psql":    `cat >/dev/null; exit 0`,
		"pg_dump": `out=""; while [ $# -gt 0 ]; do case "$1" in -f) out="$2"; shift 2;; *) shift;; esac; done; printf 'PGDMP dump\n' > "$out"`,
	}
	rule := func(inner string) string {
		return `<?xml version="1.0" encoding="UTF-8"?><LifecycleConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/">` + inner + `</LifecycleConfiguration>`
	}
	abort := `<Rule><ID>Default Multipart Abort Rule</ID><Filter><Prefix></Prefix></Filter><Status>Enabled</Status><AbortIncompleteMultipartUpload><DaysAfterInitiation>7</DaysAfterInitiation></AbortIncompleteMultipartUpload></Rule>`
	cases := []struct {
		name, code, body string
		exit             int
		says             string
	}{
		{"one 30-day rule on the whole bucket", "200", rule(abort + `<Rule><ID>expire-30</ID><Filter><Prefix></Prefix></Filter><Status>Enabled</Status><Expiration><Days>30</Days></Expiration></Rule>`), 1, "rule 'expire-30' (prefix '', after 30 days)"},
		{"no filter at all", "200", rule("<Rule>\n  <ID>all</ID>\n  <Filter/>\n  <Status>Enabled</Status>\n  <Expiration><Days>45</Days></Expiration>\n</Rule>"), 1, "after 45 days"},
		{"the host's whole prefix", "200", rule(`<Rule><ID>host</ID><Filter><Prefix>testhost/</Prefix></Filter><Status>Enabled</Status><Expiration><Days>30</Days></Expiration></Rule>`), 1, "prefix 'testhost/'"},
		{"the old rule-level prefix", "200", rule(`<Rule><ID>legacy</ID><Prefix></Prefix><Status>Enabled</Status><Expiration><Days>30</Days></Expiration></Rule>`), 1, "rule 'legacy'"},
		{"media itself, on a date", "200", rule(`<Rule><ID>dated</ID><Filter><And><Prefix>testhost/media/</Prefix><ObjectSizeGreaterThan>0</ObjectSizeGreaterThan></And></Filter><Status>Enabled</Status><Expiration><Date>2027-01-01T00:00:00Z</Date></Expiration></Rule>`), 1, "on a set date"},
		{"scoped to database/ and tenant/ (the documented rule)", "200", rule(abort + `<Rule><ID>db</ID><Filter><Prefix>testhost/database/</Prefix></Filter><Status>Enabled</Status><Expiration><Days>30</Days></Expiration></Rule><Rule><ID>tenant</ID><Filter><Prefix>testhost/tenant/</Prefix></Filter><Status>Enabled</Status><Expiration><Days>30</Days></Expiration></Rule>`), 0, "no expiry rule covers testhost/media/"},
		{"another host's prefix", "200", rule(`<Rule><ID>other</ID><Filter><Prefix>otherhost/</Prefix></Filter><Status>Enabled</Status><Expiration><Days>30</Days></Expiration></Rule>`), 0, "no expiry rule covers"},
		{"a disabled rule", "200", rule(`<Rule><ID>off</ID><Filter><Prefix></Prefix></Filter><Status>Disabled</Status><Expiration><Days>30</Days></Expiration></Rule>`), 0, "no expiry rule covers"},
		{"only the multipart clean-up", "200", rule(abort), 0, "no expiry rule covers"},
		{"a storage-class move, not a delete", "200", rule(`<Rule><ID>ia</ID><Filter><Prefix></Prefix></Filter><Status>Enabled</Status><Transition><Days>30</Days><StorageClass>STANDARD_IA</StorageClass></Transition></Rule>`), 0, "no expiry rule covers"},
		{"delete markers only", "200", rule(`<Rule><ID>markers</ID><Filter><Prefix></Prefix></Filter><Status>Enabled</Status><Expiration><ExpiredObjectDeleteMarker>true</ExpiredObjectDeleteMarker></Expiration></Rule>`), 0, "no expiry rule covers"},
		{"a tag filter (media carries no tags)", "200", rule(`<Rule><ID>tagged</ID><Filter><Tag><Key>tmp</Key><Value>1</Value></Tag></Filter><Status>Enabled</Status><Expiration><Days>1</Days></Expiration></Rule>`), 0, "no expiry rule covers"},
		{"no lifecycle rule", "404", `<Error><Code>NoSuchLifecycleConfiguration</Code></Error>`, 0, "has no lifecycle rule"},
		{"a missing bucket is not 'no rule'", "404", `<Error><Code>NoSuchBucket</Code></Error>`, 3, "HTTP 404, NoSuchBucket"},
		{"the token cannot read bucket settings", "403", `<Error><Code>AccessDenied</Code></Error>`, 3, "check by hand"},
		{"the bucket does not answer", "000", ``, 3, "could not read"},
	}
	for _, c := range cases {
		dir := t.TempDir()
		bucket := filepath.Join(dir, "bucket")
		body := filepath.Join(dir, "lifecycle.xml")
		os.WriteFile(body, []byte(c.body), 0o644)
		env := backupTestEnv(bucket, filepath.Join(dir, "backups"), filepath.Join(dir, "media"))
		env["FAKE_LIFECYCLE_CODE"], env["FAKE_LIFECYCLE_BODY"] = c.code, filepath.ToSlash(body)
		out, err := runBackupScript(t, bash, stubs, env, script, "--check-media-retention")
		got := 0
		if ee, ok := err.(*exec.ExitError); ok {
			got = ee.ExitCode()
		} else if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got != c.exit || !strings.Contains(out, c.says) {
			t.Errorf("%s: exit %d, want %d, and output must say %q:\n%s", c.name, got, c.exit, c.says, out)
		}
		if c.exit == 1 && !strings.Contains(out, "[BACKUP_MEDIA_EXPIRES]") {
			t.Errorf("%s: an expiring media copy was not alerted:\n%s", c.name, out)
		}
		calls, _ := os.ReadFile(bucket + ".calls")
		if strings.Count(string(calls), "\n") != 1 || !strings.Contains(string(calls), "erp-backups?lifecycle") ||
			strings.Contains(string(calls), "-T ") || strings.Contains(string(calls), "-X ") || strings.Contains(string(calls), "secret-49106c") {
			t.Errorf("%s: the check must make exactly one read-only GET of ?lifecycle, keys off the command line; curl got:\n%s", c.name, calls)
		}
		if entries, _ := os.ReadDir(filepath.Join(dir, "backups")); len(entries) > 0 {
			t.Errorf("%s: the check alone dumped or wrote something", c.name)
		}
	}

	// The nightly runs the check after the media copy: the backup still
	// succeeds, and the expiring rule is alerted.
	dir := t.TempDir()
	media := filepath.Join(dir, "media")
	os.MkdirAll(filepath.Join(media, "receipts"), 0o755)
	os.WriteFile(filepath.Join(media, "receipts", "r1.pdf"), []byte("%PDF receipt"), 0o644)
	body := filepath.Join(dir, "lifecycle.xml")
	os.WriteFile(body, []byte(cases[0].body), 0o644)
	env := backupTestEnv(filepath.Join(dir, "bucket"), filepath.Join(dir, "backups"), media)
	env["FAKE_LIFECYCLE_CODE"], env["FAKE_LIFECYCLE_BODY"] = "200", filepath.ToSlash(body)
	out, err := runBackupScript(t, bash, stubs, env, script)
	if err != nil || !strings.Contains(out, "backup complete:") || !strings.Contains(out, "[BACKUP_MEDIA_EXPIRES]") {
		t.Fatalf("nightly with an expiring rule: err %v - want a completed backup and BACKUP_MEDIA_EXPIRES:\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(dir, "bucket", "testhost", "media", "receipts", "r1.pdf.enc")); err != nil {
		t.Errorf("the media copy itself did not run: %v", err)
	}
}

// Stage 49.13.7 (disk full): a backup that runs out of disk while sealing the
// dump stops with an explicit failure - recorded, alerted, the full disk named
// - and leaves no plain dump (never unencrypted on disk, and never pruned) and
// no half-written .enc that would pass for a backup. The next nightly is the
// retry; nothing loops.
func TestStage49137BackupOnAFullDiskLeavesNothingHalfDone(t *testing.T) {
	bash := posixBash(t)
	script := filepath.ToSlash(filepath.Join(repoRoot(t), "deploy", "backup.sh"))
	dir := t.TempDir()
	backups := filepath.Join(dir, "backups")
	runs := filepath.Join(dir, "runs.log")
	stubs := map[string]string{
		"pg_dump": `out=""; while [ $# -gt 0 ]; do case "$1" in -f) out="$2"; shift 2;; *) shift;; esac; done; printf 'PGDMP plain dump PLAIN-49137\n' > "$out"`,
		// psql records each run row's outcome and detail.
		"psql": `cat >/dev/null; printf '%s\n' "$*" >> "$RUNS_LOG"; exit 0`,
		// openssl writes part of its output, then the disk is full.
		"openssl": `out=""; while [ $# -gt 0 ]; do case "$1" in -out) out="$2"; shift 2;; *) shift;; esac; done
printf 'Salted__partial' > "$out"; echo "write error: No space left on device" >&2; exit 1`,
		"df":   `printf 'Filesystem 1024-blocks Used Available Capacity Mounted on\n/dev/vda1 25000000 25000000 0 100%% /\n'`,
		"curl": backupStubCurl,
	}
	env := backupTestEnv(filepath.Join(dir, "bucket"), backups, filepath.Join(dir, "media"))
	env["RUNS_LOG"] = filepath.ToSlash(runs)
	out, err := runBackupScript(t, bash, stubs, env, script)
	if err == nil {
		t.Fatalf("a backup on a full disk reported success:\n%s", out)
	}
	if !strings.Contains(out, "[BACKUP_FAILED]") || !strings.Contains(out, "during 'openssl'") || !strings.Contains(out, "is full (0 KB free)") {
		t.Errorf("the failure must be alerted, name the step and the full disk:\n%s", out)
	}
	if entries, _ := os.ReadDir(backups); len(entries) > 0 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("a failed run left %v in the backup folder - no plain dump, no partial .enc may stay", names)
	}
	rows, _ := os.ReadFile(runs)
	if !strings.Contains(string(rows), "outcome=Failed") || !strings.Contains(string(rows), "is full") || strings.Contains(string(rows), "outcome=Success") {
		t.Errorf("the run must be recorded once, as Failed with the reason:\n%s", rows)
	}
	if calls, _ := os.ReadFile(filepath.Join(dir, "bucket") + ".calls"); len(calls) > 0 {
		t.Errorf("a failed backup still sent something off the box:\n%s", calls)
	}
}
