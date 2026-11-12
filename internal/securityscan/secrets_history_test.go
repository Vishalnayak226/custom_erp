package securityscan

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// reviewedHistoryFindings is the history counterpart of reviewedSecretFindings:
// file -> kind -> why the hit is not a live credential. No counts: the window
// slides with every commit, so only the (file, kind) pair is reviewed. A real
// hit is rotated, not listed here.
var reviewedHistoryFindings = map[string]map[string]string{
	"cmd/installcheck/installcheck_test.go":                {"connection-string-password": "a fixture env file in a test, 'erp:pass@localhost'"},
	"cmd/journeylab/journeylab_test.go":                    {"connection-string-password": "a parser test with a throwaway local URL"},
	"cmd/journeylab/soak.go":                               {"high-entropy-assignment": "the soak lab's JWT secret for its own private scratch server; authenticates to nothing real (the literal is no longer in the tree)"},
	"engines/connector_credential_list_stage4985c_test.go": {"high-entropy-assignment": "the 49.8.5c test's synthetic secret under its pre-rename file name; see reviewedSecretFindings"},
	"internal/server/stage49_8_5_credentials_test.go":      {"high-entropy-assignment": "the 49.8.5 test's synthetic secret under its pre-rename file name; see reviewedSecretFindings"},
}

// isolateGit keeps a scratch repository's git calls off the caller's: a run
// with GIT_INDEX_FILE set (the repolayout / securityscan temp-index method) or
// GIT_DIR would otherwise point them at another repository's index or objects.
// The values come back when the test ends.
func isolateGit(t *testing.T) {
	t.Helper()
	for _, k := range []string{"GIT_INDEX_FILE", "GIT_DIR", "GIT_WORK_TREE", "GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_CEILING_DIRECTORIES"} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@example.invalid", "-c", "commit.gpgsign=false"}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// A secret committed and then deleted is gone from the tree and still in
// history: the scan finds it, names the commit, and never prints the value.
func TestScanHistoryFindsADeletedSecretAndHidesTheValue(t *testing.T) {
	isolateGit(t)
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q")
	planted := strings.Join([]string{"client_sec", `ret = "`, "q8F2kL0vX7mN3bR9tY6wZ1cH5jP4sD0a", `"`}, "")
	if err := os.WriteFile(filepath.Join(dir, "config.go"), []byte("package x\n"+planted+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "config.go")
	gitIn(t, dir, "commit", "-q", "-m", "add")
	if err := os.WriteFile(filepath.Join(dir, "config.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "commit", "-q", "-am", "remove it")

	if files, _ := TrackedFiles(dir); len(files) != 1 {
		t.Fatalf("setup: %v", files)
	}
	if now, _ := ScanSecrets(dir, []string{"config.go"}); len(now) != 0 {
		t.Fatalf("the working tree is clean: %+v", now)
	}
	got, err := ScanHistory(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Kind != "high-entropy-assignment" || got[0].File != "config.go" || len(got[0].Commit) != 12 {
		t.Fatalf("history findings = %+v", got)
	}
	if strings.Contains(got[0].Excerpt, "q8F2kL0vX7mN3bR9") {
		t.Fatalf("the excerpt carries the secret: %q", got[0].Excerpt)
	}
	// Bounded: the newest commit (the removal) added no secret.
	if got, err = ScanHistory(dir, 1); err != nil || len(got) != 0 {
		t.Fatalf("last commit only: %v %+v", err, got)
	}
}

func TestScanHistoryErrorsOutsideARepository(t *testing.T) {
	isolateGit(t)
	if _, err := ScanHistory(t.TempDir(), 5); err == nil {
		t.Fatal("a folder that is not a git repository must be an error, not a clean result")
	}
}

// Opt-in (a few seconds a commit - git renders every doc diff): the last N
// commits of this repository's history, N from ERP_SECRET_HISTORY, 0 = all.
// Release gate G10 may set it; the default test run does not.
func TestNoUnreviewedSecretInRecentHistory(t *testing.T) {
	v := os.Getenv("ERP_SECRET_HISTORY")
	if v == "" {
		t.Skip("set ERP_SECRET_HISTORY=<commits> (0 = all) to scan git history")
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		t.Fatalf("ERP_SECRET_HISTORY=%q is not a commit count", v)
	}
	findings, err := ScanHistory(repoRoot, n)
	if err != nil {
		t.Fatal(err)
	}
	var problems []string
	for file, kinds := range CountHistoryByFileAndKind(findings) {
		for kind, commits := range kinds {
			if _, ok := reviewedHistoryFindings[file][kind]; ok {
				continue
			}
			if _, ok := reviewedSecretFindings[file][kind]; ok {
				continue
			}
			problems = append(problems, fmt.Sprintf("  %s in %s (%d commit(s)) - ROTATE it if it was ever real; a deleted secret stays in every clone", kind, file, commits))
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		t.Fatalf("a secret-shaped value is in git history and not reviewed (49.6.6):\n%s", strings.Join(problems, "\n"))
	}
}

func TestScanBinaryFindsAnEmbeddedSecretAndHidesTheValue(t *testing.T) {
	planted := strings.Join([]string{"ghp", "_", "a1B2c3D4e5F6g7H8i9J0k1L2m3N4o5P6q7R8"}, "")
	blob := append([]byte{0, 1, 2, 3, 0xff, 0xfe}, []byte("token="+planted)...)
	blob = append(blob, 0, 0, 0)
	path := filepath.Join(t.TempDir(), "server.bin")
	if err := os.WriteFile(path, blob, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ScanBinary(path)
	if err != nil || len(got) == 0 {
		t.Fatalf("binary scan: %v %+v", err, got)
	}
	if got[0].Kind != "github-token" || strings.Contains(got[0].Excerpt, "a1B2c3D4e5F6g7H8") {
		t.Fatalf("finding = %+v", got[0])
	}
	clean := filepath.Join(filepath.Dir(path), "clean.bin")
	_ = os.WriteFile(clean, bytes.Repeat([]byte("plain text with nothing secret in it\x00"), 50), 0o644)
	if got, _ := ScanBinary(clean); len(got) != 0 {
		t.Fatalf("clean binary: %+v", got)
	}
}

// Opt-in: scan binaries named in ERP_SCAN_BINARIES (a path list), for example
// the release build. Nothing built is committed (repolayout), so the default
// run has nothing to read.
func TestBuiltBinariesCarryNoSecret(t *testing.T) {
	list := os.Getenv("ERP_SCAN_BINARIES")
	if list == "" {
		t.Skip("set ERP_SCAN_BINARIES=<path>[;<path>] to scan built binaries")
	}
	for _, p := range filepath.SplitList(list) {
		found, err := ScanBinary(p)
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		for _, f := range found {
			t.Errorf("%s: %s at offset %d: %s", f.File, f.Kind, f.Line, f.Excerpt)
		}
	}
}

func TestCheckBackupDir(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, data []byte, mode os.FileMode) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), data, mode); err != nil {
			t.Fatal(err)
		}
	}
	write("custom_erp_20261101T020000Z.dump.enc", append([]byte("Salted__"), bytes.Repeat([]byte{7}, 64)...), 0o600)
	write("custom_erp_20261101T020000Z.dump.enc.sha256", []byte("abc  x\n"), 0o600)
	if ps, err := CheckBackupDir(dir); err != nil || len(ps) != 0 {
		t.Fatalf("a sealed backup with its sidecar is clean: %v %+v", err, ps)
	}
	write("custom_erp_20261102T020000Z.dump", []byte("PGDMP plain"), 0o600)     // sealing never finished
	write("custom_erp_20261103T020000Z.dump.enc", []byte("PGDMP plain"), 0o600) // named .enc, is not
	write("export.sql", []byte("INSERT INTO x"), 0o600)
	ps, err := CheckBackupDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	reasons := map[string]string{}
	for _, p := range ps {
		reasons[p.File] = p.Reason
	}
	for _, name := range []string{"custom_erp_20261102T020000Z.dump", "custom_erp_20261103T020000Z.dump.enc", "export.sql"} {
		if reasons[name] == "" {
			t.Errorf("%s was not flagged: %+v", name, ps)
		}
	}
	if len(ps) != 3 {
		t.Errorf("expected exactly the three bad files, got %+v", ps)
	}
	if runtime.GOOS != "windows" {
		write("custom_erp_20261104T020000Z.dump.enc", append([]byte("Salted__"), 1, 2, 3), 0o644)
		_ = os.Chmod(filepath.Join(dir, "custom_erp_20261104T020000Z.dump.enc"), 0o644)
		ps, _ = CheckBackupDir(dir)
		if len(ps) != 4 {
			t.Errorf("a world-readable backup must be flagged: %+v", ps)
		}
	}
}

// deploy/backup.sh is the writer of every backup. These are the properties
// the secret scan relies on, pinned so an edit cannot quietly drop them: files
// are created 0600, the key reaches openssl through the environment (an
// argument shows in the process list), the clear dump is removed.
func TestBackupScriptKeepsSecretsOutOfClear(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repoRoot, "deploy", "backup.sh"))
	if err != nil {
		t.Fatal(err)
	}
	var code []string
	for _, l := range strings.Split(string(raw), "\n") {
		if t := strings.TrimSpace(l); t != "" && !strings.HasPrefix(t, "#") {
			code = append(code, t)
		}
	}
	text := strings.Join(code, "\n")
	if !strings.Contains(text, "\numask 077\n") {
		t.Error("backup.sh must set umask 077 before it writes the dump")
	}
	if strings.Contains(text, "pass:") {
		t.Error("backup.sh passes a key as a command-line argument (-pass pass:...); use -pass env:")
	}
	if !strings.Contains(text, `-in "$dump" -out "$enc" -pass env:BACKUP_ENCRYPTION_KEY`) {
		t.Error("the dump must be sealed with openssl -pass env:BACKUP_ENCRYPTION_KEY")
	}
	if !strings.Contains(text, `rm -f "$dump"`) || !strings.Contains(text, `rm -f -- "$dump"`) {
		t.Error("backup.sh must remove the clear dump after sealing and on a failed run")
	}

	// 163.2: the restore drill decrypts the newest backup; the same rule holds.
	drill, err := os.ReadFile(filepath.Join(repoRoot, "deploy", "restore_drill.sh"))
	if err != nil {
		t.Fatal(err)
	}
	var drillCode []string
	for _, l := range strings.Split(string(drill), "\n") {
		if t := strings.TrimSpace(l); t != "" && !strings.HasPrefix(t, "#") {
			drillCode = append(drillCode, t)
		}
	}
	if d := strings.Join(drillCode, "\n"); strings.Contains(d, "pass:") || !strings.Contains(d, "-pass env:BACKUP_ENCRYPTION_KEY") {
		t.Error("restore_drill.sh must pass the key to openssl as -pass env:BACKUP_ENCRYPTION_KEY, never as an argument")
	}
}
