// Package releasever holds the release numbering rule (Stage 90, the user
// 2026-10-18: "it should be linked to the release ... update accordingly and
// show accordingly"). Semantic versioning, decided by the commits a release
// carries since the build that is live:
//
//   - any `feat:` commit (or `feat(scope):`)  -> MINOR, patch back to 0
//   - only fixes, docs, chores, refactors      -> PATCH
//   - MAJOR is never automatic: 1.0.0 is a go-live decision, passed to
//     deploy.ps1 as -Version.
//
// It lives outside engines so cmd/releasever builds in a second, not after a
// full engines compile (177.5: that compile was 130 s of every Release click).
// engines.NextReleaseVersion and engines.ValidReleaseVersion call it.
package releasever

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var featureCommit = regexp.MustCompile(`^feat(\([^)]*\))?!?:`)

// Next applies the rule. With no commits since the live build (a redeploy of
// the same commit) the version stays as it is.
func Next(previous string, subjects []string) (next, reason string, err error) {
	major, minor, patch, err := parse(previous)
	if err != nil {
		return "", "", err
	}
	if len(subjects) == 0 {
		return previous, "no commits since the live build", nil
	}
	features := 0
	for _, s := range subjects {
		if featureCommit.MatchString(strings.TrimSpace(s)) {
			features++
		}
	}
	if features > 0 {
		return fmt.Sprintf("%d.%d.0", major, minor+1), fmt.Sprintf("%d commits, %d feat", len(subjects), features), nil
	}
	return fmt.Sprintf("%d.%d.%d", major, minor, patch+1), fmt.Sprintf("%d commits, fixes only", len(subjects)), nil
}

// Valid reports whether v is MAJOR.MINOR.PATCH.
func Valid(v string) bool {
	_, _, _, err := parse(v)
	return err == nil
}

func parse(v string) (major, minor, patch int, err error) {
	parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(v), "v"), ".")
	if len(parts) != 3 {
		return 0, 0, 0, fmt.Errorf("version %q is not MAJOR.MINOR.PATCH", v)
	}
	nums := make([]int, 3)
	for i, p := range parts {
		n, convErr := strconv.Atoi(p)
		if convErr != nil || n < 0 {
			return 0, 0, 0, fmt.Errorf("version %q is not MAJOR.MINOR.PATCH", v)
		}
		nums[i] = n
	}
	return nums[0], nums[1], nums[2], nil
}
