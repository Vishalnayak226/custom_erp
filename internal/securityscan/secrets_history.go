package securityscan

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// Item 49.6.6 (remainder) - secrets beyond the working tree.
//
//   - ScanHistory     the lines added by the last N commits on every ref;
//   - ScanBinary      the printable strings of a built binary;
//   - CheckBackupDir  every backup in a folder is encrypted, none left in clear.
//
// All three reuse the working-tree patterns (scanLine), so there is one list of
// secret shapes. A finding never carries the value (see SecretFinding).

// HistoryFinding is a hit in a past commit.
type HistoryFinding struct {
	SecretFinding
	Commit string // 12-character abbreviated hash
}

// maxHistoryLine skips an added line this long (a minified bundle or a data
// blob): a credential is short, and the working-tree scan skips such files too.
const maxHistoryLine = 4000

// ScanHistory scans the lines added by the newest maxCommits commits across
// all refs (maxCommits <= 0 means every commit). A secret that was committed
// and later deleted is still in every clone, so a hit here needs a rotation;
// rewriting history is not the fix. Needs git.
func ScanHistory(root string, maxCommits int) ([]HistoryFinding, error) {
	args := []string{"-C", root, "log", "--all", "--no-merges", "-p", "-U0", "--no-color", "--no-ext-diff", "--format=@@commit %H"}
	if maxCommits > 0 {
		args = append(args, fmt.Sprintf("--max-count=%d", maxCommits))
	}
	cmd := exec.Command("git", args...)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("git log: %w", err)
	}
	var findings []HistoryFinding
	commit, file := "", ""
	r := bufio.NewReaderSize(out, 1<<20)
	for {
		line, tooLong, rerr := readCappedLine(r, maxHistoryLine)
		if len(line) > 0 || tooLong {
			switch {
			case strings.HasPrefix(line, "@@commit "):
				commit = strings.TrimPrefix(line, "@@commit ")
				if len(commit) > 12 {
					commit = commit[:12]
				}
			case strings.HasPrefix(line, "+++ "):
				file = strings.TrimPrefix(strings.TrimPrefix(line, "+++ "), "b/")
			case strings.HasPrefix(line, "+") && !tooLong && !skippedSecretFiles[file]:
				for _, f := range scanLine(file, 0, line[1:]) {
					findings = append(findings, HistoryFinding{SecretFinding: f, Commit: commit})
				}
			}
		}
		if rerr != nil {
			break
		}
	}
	if err := cmd.Wait(); err != nil {
		return nil, fmt.Errorf("git log: %w", err)
	}
	sort.Slice(findings, func(i, j int) bool {
		a, b := findings[i], findings[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return a.Commit < b.Commit
	})
	return findings, nil
}

// readCappedLine reads one line; a line longer than max is consumed and
// reported as tooLong with only its first max bytes returned.
func readCappedLine(r *bufio.Reader, max int) (line string, tooLong bool, err error) {
	var buf []byte
	for {
		chunk, isPrefix, e := r.ReadLine()
		if len(buf) < max {
			buf = append(buf, chunk...)
		} else {
			tooLong = true
		}
		if e != nil {
			return string(buf), tooLong, e
		}
		if !isPrefix {
			if len(buf) > max {
				tooLong = true
			}
			return string(buf), tooLong, nil
		}
	}
}

// CountHistoryByFileAndKind collapses history findings to file -> kind -> the
// number of distinct commits that added a hit (one secret on several lines of
// the same commit counts once).
func CountHistoryByFileAndKind(findings []HistoryFinding) map[string]map[string]int {
	seen := map[string]bool{}
	out := map[string]map[string]int{}
	for _, f := range findings {
		key := f.File + "\x00" + f.Kind + "\x00" + f.Commit
		if seen[key] {
			continue
		}
		seen[key] = true
		if out[f.File] == nil {
			out[f.File] = map[string]int{}
		}
		out[f.File][f.Kind]++
	}
	return out
}

// maxBinaryScanBytes bounds a binary read into memory.
const maxBinaryScanBytes = 512 << 20

// ScanBinary scans the printable-ASCII runs (8 bytes or more) of a built
// binary or any other non-text file. Findings are attributed to the file's
// base name; Line holds the run's byte offset.
func ScanBinary(path string) ([]SecretFinding, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if fi.Size() > maxBinaryScanBytes {
		return nil, fmt.Errorf("%s is %d bytes, over the %d byte limit", path, fi.Size(), int64(maxBinaryScanBytes))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	name := filepath.Base(path)
	var findings []SecretFinding
	start := -1
	flush := func(end int) {
		if start >= 0 && end-start >= 8 && end-start <= maxHistoryLine {
			findings = append(findings, scanLine(name, start, string(data[start:end]))...)
		}
		start = -1
	}
	for i, c := range data {
		if (c >= 0x20 && c < 0x7f) || c == '\t' {
			if start < 0 {
				start = i
			}
			continue
		}
		flush(i)
	}
	flush(len(data))
	return findings, nil
}

// BackupProblem is one reason a backup folder is not safe.
type BackupProblem struct {
	File   string
	Reason string
}

// opensslSaltedMagic starts every file `openssl enc -salt` writes. deploy/backup.sh
// seals each dump this way; a file without it is not an encrypted backup.
var opensslSaltedMagic = []byte("Salted__")

// CheckBackupDir verifies a backup folder (deploy/backup.sh's BACKUP_DIR):
// every *.enc starts with the openssl salted header and is not readable by
// group or others (Unix only), every *.sha256 sidecar is allowed, and anything
// else - a leftover plaintext .dump, a .sql, a .csv, an archive - is a clear
// copy of the data and a problem.
func CheckBackupDir(dir string) ([]BackupProblem, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var problems []BackupProblem
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		switch {
		case strings.HasSuffix(name, ".sha256"):
			continue
		case strings.HasSuffix(name, ".enc"):
			f, err := os.Open(filepath.Join(dir, name))
			if err != nil {
				problems = append(problems, BackupProblem{name, "cannot be opened: " + err.Error()})
				continue
			}
			head := make([]byte, len(opensslSaltedMagic))
			_, rerr := io.ReadFull(f, head)
			f.Close()
			if rerr != nil || !bytes.Equal(head, opensslSaltedMagic) {
				problems = append(problems, BackupProblem{name, "named .enc but does not carry the openssl encrypted header - it may be a clear copy"})
			}
			if runtime.GOOS != "windows" {
				if info, err := e.Info(); err == nil && info.Mode().Perm()&0o044 != 0 {
					problems = append(problems, BackupProblem{name, fmt.Sprintf("mode %o is readable by others; backups are 0600", info.Mode().Perm())})
				}
			}
		default:
			problems = append(problems, BackupProblem{name, "not an encrypted backup: a clear copy of data must not sit in the backup folder"})
		}
	}
	return problems, nil
}
