package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Stage 49.17.6: release/security-floor.txt is read by Deploy.cmd to say out
// loud when production runs a release with a known, fixed vulnerability. It
// must parse, name what was fixed, and never sit above the release number.
func TestSecurityFloorIsWellFormed(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "release", "security-floor.txt"))
	if err != nil {
		t.Fatal(err)
	}
	line := ""
	for _, l := range strings.Split(string(raw), "\n") {
		if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "#") {
			line = l
			break
		}
	}
	m := regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+) (.+)$`).FindStringSubmatch(line)
	if m == nil {
		t.Fatalf("floor line %q is not '<version> <what the next version fixed>'", line)
	}
	verRaw, err := os.ReadFile(filepath.Join("..", "..", "internal", "server", "VERSION"))
	if err != nil {
		t.Fatal(err)
	}
	v := regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)$`).FindStringSubmatch(strings.TrimSpace(string(verRaw)))
	if v == nil {
		t.Fatalf("internal/server/VERSION %q is not a release number", verRaw)
	}
	for i := 1; i <= 3; i++ {
		f, _ := strconv.Atoi(m[i])
		r, _ := strconv.Atoi(v[i])
		if f != r {
			if f > r {
				t.Errorf("security floor %s.%s.%s is above the release number %s", m[1], m[2], m[3], v[0])
			}
			break
		}
	}
}
