package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestSecurityImpactMarksHighRiskChanges(t *testing.T) {
	var base, next manifest
	base.Routes = []route{{"GET", "/api/v1/a", "session"}, {"POST", "/api/v1/b", "session"}, {"GET", "/api/v1/gone", "session"}}
	next.Routes = []route{{"GET", "/api/v1/a", "session"}, {"POST", "/api/v1/b", "public"},
		{"GET", "/api/v1/new", "session"}, {"POST", "/api/v1/hook", "public"}}
	next.Dependencies = append(next.Dependencies, struct {
		Module  string `json:"module"`
		Version string `json:"version"`
	}{"example.com/x", "v1.0.0"})
	next.Migrations.IncrementalFiles = 2

	im := compare(base, next)
	high := strings.Join(im.High, "\n")
	for _, want := range []string{"new route anyone can reach: POST /api/v1/hook", "route authentication changed: POST /api/v1/b session -> public", "new dependency: example.com/x"} {
		if !strings.Contains(high, want) {
			t.Errorf("high-risk list misses %q:\n%s", want, high)
		}
	}
	other := strings.Join(im.Lines, "\n")
	for _, want := range []string{"new route: GET /api/v1/new (session)", "route removed: GET /api/v1/gone", "+2 migration file(s)"} {
		if !strings.Contains(other, want) {
			t.Errorf("other changes miss %q:\n%s", want, other)
		}
	}
	if strings.Contains(high, "/api/v1/new") {
		t.Error("a new signed-in route is not high risk")
	}
	var out bytes.Buffer
	render(&out, compare(next, next))
	if !strings.Contains(out.String(), "No change") {
		t.Fatalf("an unchanged surface: %q", out.String())
	}
}
