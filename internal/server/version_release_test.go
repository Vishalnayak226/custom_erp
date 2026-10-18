package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestReleaseDateDerivesFromBuildStamp covers the release-date half of the
// sidebar version line (public/index.html #app-version-line). releaseDate()
// parses the ldflags-injected buildTime, so the three cases that matter are
// a real stamped release build, the "unknown" default a bare `go build`
// leaves, and anything unparseable - the last two must degrade to "" rather
// than to a wrong or zero-value date, because the frontend keys off the
// empty string to render "development build" instead of a date.
func TestReleaseDateDerivesFromBuildStamp(t *testing.T) {
	original := buildTime
	defer func() { buildTime = original }()

	for _, tc := range []struct{ build, want string }{
		{"2026-10-04T09:12:00Z", "2026-10-04"},
		{"unknown", ""},
		{"", ""},
		{"not-a-timestamp", ""},
	} {
		buildTime = tc.build
		if got := releaseDate(); got != tc.want {
			t.Errorf("buildTime=%q: releaseDate()=%q, want %q", tc.build, got, tc.want)
		}
	}
}

// TestVersionPayloadShape pins the contract the sidebar version line reads.
// Deliberately DB-free (unlike TestVersionEndpointIsPublicAndTenantStamping
// Works, which also exercises tenant stamping): handleVersion touches no
// database, and this is the test that should still run when one isn't up.
// The flat map[string]string decode is the part worth guarding - adding a
// non-string field here would break every existing caller that decodes this
// response into a string map.
func TestVersionPayloadShape(t *testing.T) {
	origBuild, origCommit := buildTime, gitCommit
	buildTime, gitCommit = "2026-10-04T09:12:00Z", "abc123def456"
	defer func() { buildTime, gitCommit = origBuild, origCommit }()

	rec := httptest.NewRecorder()
	handleVersion(rec, httptest.NewRequest(http.MethodGet, "/api/v1/version", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("version payload must decode as a flat string map: %v", err)
	}
	for field, want := range map[string]string{
		"version":      currentAppVersion(),
		"git_commit":   "abc123def456",
		"build_time":   "2026-10-04T09:12:00Z",
		"release_date": "2026-10-04",
	} {
		if resp[field] != want {
			t.Errorf("%s = %q, want %q", field, resp[field], want)
		}
	}
}

// Stage 90: a production build carries the release number deploy.ps1 stamped;
// any other build reports the VERSION file.
func TestStampedReleaseVersionWins(t *testing.T) {
	original := releaseVersion
	defer func() { releaseVersion = original }()
	releaseVersion = ""
	file := currentAppVersion()
	if file == "" {
		t.Fatal("the VERSION file is empty")
	}
	releaseVersion = "0.9.1"
	if got := currentAppVersion(); got != "0.9.1" {
		t.Fatalf("currentAppVersion() = %q with a stamped release, want 0.9.1", got)
	}
	if v, _, _ := BuildIdentity(); v != "0.9.1" {
		t.Fatalf("BuildIdentity version = %q, want 0.9.1", v)
	}
	releaseVersion = "  "
	if got := currentAppVersion(); got != file {
		t.Fatalf("a blank stamp gave %q, want the VERSION file's %q", got, file)
	}
}
