package engines

import (
	"custom_erp/db"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Release numbering (Stage 90, the user 2026-10-18: "it should be linked to
// the release ... update accordingly and show accordingly"). Semantic
// versioning, decided by the commits a release carries since the build that
// is live:
//
//   - any `feat:` commit (or `feat(scope):`)  -> MINOR, patch back to 0
//   - only fixes, docs, chores, refactors      -> PATCH
//   - MAJOR is never automatic: 1.0.0 is a go-live decision, passed to
//     deploy.ps1 as -Version.
//
// deploy.ps1 asks the live box for its version and commit, cmd/releasever
// applies this rule to the commits in between, the number is stamped into the
// binary, and -record-release writes it to public.deployments once the deploy
// is healthy - the ledger the Control Panel's Releases table reads.

var featureCommit = regexp.MustCompile(`^feat(\([^)]*\))?!?:`)

// NextReleaseVersion applies the rule. With no commits since the live build
// (a redeploy of the same commit) the version stays as it is.
func NextReleaseVersion(previous string, subjects []string) (next, reason string, err error) {
	major, minor, patch, err := parseReleaseVersion(previous)
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

// ValidReleaseVersion reports whether v is MAJOR.MINOR.PATCH.
func ValidReleaseVersion(v string) bool {
	_, _, _, err := parseReleaseVersion(v)
	return err == nil
}

func parseReleaseVersion(v string) (major, minor, patch int, err error) {
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

// ReleaseRecord is one row of the release ledger.
type ReleaseRecord struct {
	Environment string
	Version     string
	Commit      string
	By          string
	Notes       string
	At          time.Time // zero = now
	// StampClients sets every unpinned client's app_version to Version: one
	// running binary serves every client, so they are all on this release.
	// Off for a backfilled historical row.
	StampClients bool
}

// RecordRelease writes the ledger row and, when asked, stamps the clients, in
// one transaction. Recording the same environment, commit and version again
// adds no second row, so a re-run is harmless. It returns how many clients
// were stamped.
func RecordRelease(r ReleaseRecord) (stamped int64, err error) {
	if !ValidReleaseVersion(r.Version) {
		return 0, fmt.Errorf("version %q is not MAJOR.MINOR.PATCH", r.Version)
	}
	if strings.TrimSpace(r.Commit) == "" || r.Commit == "dev" {
		return 0, fmt.Errorf("an unstamped development build is not a release")
	}
	if r.Environment == "" {
		r.Environment = "production"
	}
	if r.At.IsZero() {
		r.At = time.Now()
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var exists bool
	if err := tx.QueryRow(`SELECT EXISTS (SELECT 1 FROM public.deployments
		WHERE environment = $1 AND git_commit = $2 AND app_version = $3)`, r.Environment, r.Commit, r.Version).Scan(&exists); err != nil {
		return 0, err
	}
	if !exists {
		if _, err := tx.Exec(`INSERT INTO public.deployments
			(environment, tenant_scope, git_commit, app_version, promoted_by, promoted_at, build_status, notes)
			VALUES ($1, 'ALL', $2, $3, $4, $5, 'passed', $6)`,
			r.Environment, r.Commit, r.Version, r.By, r.At.UTC(), r.Notes); err != nil {
			return 0, err
		}
	}
	if r.StampClients {
		res, err := tx.Exec(`UPDATE public.tenants SET app_version = $1
			WHERE COALESCE(pinned_version, '') = '' AND COALESCE(app_version, '') <> $1`, r.Version)
		if err != nil {
			return 0, err
		}
		stamped, _ = res.RowsAffected()
	}
	return stamped, tx.Commit()
}
