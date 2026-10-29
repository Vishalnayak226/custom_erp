package securityscan

import (
	"bytes"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Stage 49.6.6 - committed-secret scanning.
//
// bypass.go (49.1.4) already catches two credential shapes in code - a bcrypt
// hash or PEM private key, and a digit-bearing literal assigned to a
// credential-named symbol - but only in code files, never in tests, docs,
// JSON, HTML or anything else that ships in the repository. A token pasted
// into a Markdown runbook or a fixture is just as public as one in a .go file.
//
// ScanSecrets reads EVERY file git tracks (the repository is what leaks, so the
// file list is git's, not a directory walk: a developer's own untracked .env is
// not a finding, and a tracked one is), whatever its type, tests and docs
// included, and looks for the shapes a real credential has: a private key
// block, a provider token with a recognisable prefix (cloud, GitHub, Slack,
// Stripe, Google, Anthropic/OpenAI-style), a signed JWT, a connection string
// that carries a password, and a long high-entropy value assigned to a
// secret/token/password-named key. secrets_test.go holds the reviewed
// allowlist and fails the build on anything new.
//
// What this does NOT cover (49.6.6's remainder, said here so nobody assumes
// it): git HISTORY (a secret committed and later deleted is still in every
// clone - that needs a history scan and, for a real find, a rotation, not a
// rewrite), built binaries, and backup files. Those stay open on the tracker.

// SecretFinding is one hit. Excerpt never carries the matched value: it is
// printed in test output and CI logs.
type SecretFinding struct {
	Kind    string
	File    string
	Line    int
	Excerpt string
}

type secretPattern struct {
	kind    string
	pattern *regexp.Regexp
	// hints are plain substrings one of which a line must contain before the
	// regular expression is run at all - a long line is expensive to match,
	// and most lines contain none. fold runs both the hints and the pattern
	// against the ASCII-lower-cased line (same byte offsets), which is far
	// cheaper than a case-insensitive expression; the value is still read
	// from the original line.
	hints []string
	fold  bool
	// value, when set, is the submatch index holding the secret itself; the
	// match is only a finding when check(value) is true.
	value int
	check func(string) bool
}

var secretPatterns = []secretPattern{
	{kind: "private-key", hints: []string{"PRIVATE KEY"},
		pattern: regexp.MustCompile(`-----BEGIN (RSA |EC |DSA |OPENSSH |ENCRYPTED |PGP )?PRIVATE KEY( BLOCK)?-----`)},
	{kind: "aws-access-key", hints: []string{"AKIA", "ASIA"},
		pattern: regexp.MustCompile(`\b(AKIA|ASIA)[0-9A-Z]{16}\b`)},
	{kind: "github-token", hints: []string{"ghp_", "gho_", "ghu_", "ghs_", "ghr_", "github_pat_"},
		pattern: regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{36,}\b|\bgithub_pat_[A-Za-z0-9_]{60,}`)},
	{kind: "slack-token", hints: []string{"xox", "hooks.slack.com"},
		pattern: regexp.MustCompile(`\bxox[abposr]-[0-9]{6,}-[A-Za-z0-9-]{10,}|https://hooks\.slack\.com/services/T[A-Z0-9]{6,}/B[A-Z0-9]{6,}/[A-Za-z0-9]{16,}`)},
	{kind: "stripe-key", hints: []string{"_live_"},
		pattern: regexp.MustCompile(`\b(sk|rk)_live_[A-Za-z0-9]{20,}`)},
	{kind: "google-api-key", hints: []string{"AIza"},
		pattern: regexp.MustCompile(`\bAIza[0-9A-Za-z_\-]{35}\b`)},
	{kind: "llm-api-key", hints: []string{"sk-"},
		pattern: regexp.MustCompile(`\bsk-ant-[A-Za-z0-9_\-]{20,}|\bsk-(proj-)?[A-Za-z0-9]{40,}\b`)},
	{kind: "jwt", hints: []string{"eyJ"},
		pattern: regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{10,}\.eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{16,}`)},
	{
		// scheme://user:password@host - a connection string that carries its
		// own password. A placeholder password (the dev defaults, an env
		// reference, angle-bracket or ellipsis text) is not one.
		kind: "connection-string-password", fold: true,
		hints:   []string{"postgres", "mysql://", "mariadb://", "mongodb", "redis", "amqp", "smtp", "ftp://"},
		pattern: regexp.MustCompile(`\b(postgres|postgresql|mysql|mariadb|mongodb(?:\+srv)?|redis|rediss|amqps?|smtps?|ftp|sftp)://[^:/\s"'@<>]+:([^@\s"'/<>]+)@`),
		value:   2,
		check:   func(v string) bool { return !placeholderPassword(v) },
	},
	{
		// A long, high-entropy value assigned to a key whose name says it is a
		// secret. The entropy and mixed-character checks are what separate a
		// real secret from a setting key, an env-var name or a constant.
		kind: "high-entropy-assignment", fold: true,
		hints:   []string{"secret", "token", "passw", "api_key", "apikey", "private_key", "privatekey", "access_key", "accesskey"},
		pattern: regexp.MustCompile(`(secret|token|passw(?:or)?d|api_?key|private_?key|access_?key)[a-z0-9_.-]*["']?\s*(?:[:=]|:=)\s*["']([a-z0-9+/_\-=]{24,})["']`),
		value:   2,
		check:   highEntropySecret,
	},
}

func (p secretPattern) mayMatch(line, lower string) bool {
	s := line
	if p.fold {
		s = lower
	}
	for _, h := range p.hints {
		if strings.Contains(s, h) {
			return true
		}
	}
	return len(p.hints) == 0
}

// placeholderPassword reports whether a connection-string password is
// obviously not a real one.
func placeholderPassword(v string) bool {
	l := strings.ToLower(v)
	if strings.HasPrefix(l, "$") || strings.HasPrefix(l, "%") || strings.HasPrefix(l, "{") || strings.Contains(l, "...") || strings.Trim(l, "*x") == "" {
		return true
	}
	switch l {
	case "password", "pass", "passwd", "pwd", "postgres", "secret", "test", "user", "admin", "root":
		return true
	}
	return placeholderValue(v)
}

// placeholderWords mark a value its author wrote to be visibly fake - the
// AWS documentation's own example keys end in EXAMPLE, an env template says
// CHANGE_ME, a QA script's secret says not-for-deployment. A real credential
// is never spelled like that.
var placeholderWords = []string{
	"example", "changeme", "change_me", "change-me", "placeholder", "dummy", "fake",
	"synthetic", "not-for-", "not_for_", "your_", "your-", "redacted", "xxxx", "<", ">",
}

func placeholderValue(v string) bool {
	l := strings.ToLower(v)
	for _, w := range placeholderWords {
		if strings.Contains(l, w) {
			return true
		}
	}
	return false
}

// highEntropySecret is true for a value that looks generated rather than
// written: mixed letters and digits and at least 3.5 bits of entropy per
// character. A lower-case identifier ("integration_secret_key_name") or a
// repeated filler ("xxxxxxxx...") fails it.
func highEntropySecret(v string) bool {
	if strings.HasPrefix(v, "/") {
		return false // a path ("tokenUrl": "/api/public/v1/oauth/token")
	}
	var hasDigit, hasLetter bool
	for _, r := range v {
		switch {
		case r >= '0' && r <= '9':
			hasDigit = true
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z'):
			hasLetter = true
		}
	}
	return hasDigit && hasLetter && shannonEntropy(v) >= 3.5
}

func shannonEntropy(s string) float64 {
	if s == "" {
		return 0
	}
	counts := map[rune]int{}
	for _, r := range s {
		counts[r]++
	}
	n := float64(len([]rune(s)))
	var h float64
	for _, c := range counts {
		p := float64(c) / n
		h -= p * math.Log2(p)
	}
	return h
}

// maxSecretScanBytes skips a tracked file larger than this (a generated
// register or an image); every text file this repository holds is far smaller.
const maxSecretScanBytes = 8 << 20

// TrackedFiles lists the files git tracks under root, as slash paths relative
// to it.
func TrackedFiles(root string) ([]string, error) {
	cmd := exec.Command("git", "-C", root, "ls-files", "-z")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-files: %w", err)
	}
	var files []string
	for _, f := range bytes.Split(out, []byte{0}) {
		if len(f) > 0 {
			files = append(files, filepath.ToSlash(string(f)))
		}
	}
	return files, nil
}

// ScanSecrets reads each of files (slash paths relative to root) and returns
// every committed-secret hit, sorted by file then line. A file that cannot be
// read (deleted in the working tree), is binary or is over the size cap is
// skipped.
func ScanSecrets(root string, files []string) ([]SecretFinding, error) {
	var findings []SecretFinding
	for _, rel := range files {
		if skippedSecretFiles[rel] {
			continue
		}
		path := filepath.Join(root, filepath.FromSlash(rel))
		fi, err := os.Stat(path)
		if err != nil || fi.IsDir() || fi.Size() > maxSecretScanBytes {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		head := data
		if len(head) > 8000 {
			head = head[:8000]
		}
		if bytes.IndexByte(head, 0) >= 0 {
			continue // binary
		}
		lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
		for i, line := range lines {
			lower := asciiLower(line)
			for _, p := range secretPatterns {
				if !p.mayMatch(line, lower) {
					continue
				}
				subject := line
				if p.fold {
					subject = lower
				}
				for _, m := range p.pattern.FindAllStringSubmatchIndex(subject, -1) {
					v := line[m[2*p.value]:m[2*p.value+1]]
					if placeholderValue(v) || (p.check != nil && !p.check(v)) {
						continue
					}
					findings = append(findings, SecretFinding{
						Kind: p.kind, File: rel, Line: i + 1,
						Excerpt: redactSecretLine(line, m[0], m[1], p.kind),
					})
				}
			}
		}
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		if findings[i].Line != findings[j].Line {
			return findings[i].Line < findings[j].Line
		}
		return findings[i].Kind < findings[j].Kind
	})
	return findings, nil
}

// skippedSecretFiles is this scanner's own source: its patterns are written
// out in full here and would otherwise be its first findings.
var skippedSecretFiles = map[string]bool{
	"internal/securityscan/secrets.go": true,
}

// redactSecretLine replaces the matched span with a marker - and every other
// span on the line any pattern matches, so a second secret on the same line
// cannot ride out in the excerpt - then trims the line to a readable length.
func redactSecretLine(line string, start, end int, kind string) string {
	type span struct{ from, to int }
	spans := []span{{start, end}}
	lower := asciiLower(line)
	for _, p := range secretPatterns {
		if !p.mayMatch(line, lower) {
			continue
		}
		subject := line
		if p.fold {
			subject = lower
		}
		for _, m := range p.pattern.FindAllStringIndex(subject, -1) {
			spans = append(spans, span{m[0], m[1]})
		}
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].from < spans[j].from })
	var b strings.Builder
	at := 0
	for _, s := range spans {
		if s.to <= at {
			continue
		}
		if s.from > at {
			b.WriteString(line[at:s.from])
		}
		if s.from == start && s.to == end {
			b.WriteString("<redacted " + kind + ">")
		} else {
			b.WriteString("<redacted>")
		}
		at = s.to
	}
	b.WriteString(line[at:])
	out := strings.TrimSpace(b.String())
	if len(out) > 160 {
		out = out[:160] + "..."
	}
	return out
}

// asciiLower lower-cases A-Z only, so every byte offset in the result is the
// same byte offset in s (strings.ToLower can change a line's length).
func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}

// CountSecretsByFileAndKind collapses findings to the reviewed-allowlist
// shape: file -> kind -> hits.
func CountSecretsByFileAndKind(findings []SecretFinding) map[string]map[string]int {
	out := map[string]map[string]int{}
	for _, f := range findings {
		if out[f.File] == nil {
			out[f.File] = map[string]int{}
		}
		out[f.File][f.Kind]++
	}
	return out
}
