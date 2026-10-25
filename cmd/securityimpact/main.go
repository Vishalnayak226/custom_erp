// Command securityimpact is the per-change security gate's evidence (Stage
// 49.17.1): it compares the attack-surface manifest of the last release with
// the one regenerated from the release set (cmd/surfacescan, gate G6) and
// prints what the release changes - routes and their authentication, public
// routes, background jobs, environment flags, outbound call sites and
// dependencies - marking the high-risk changes a security owner must read:
// a route anyone can reach, a route whose authentication changed, a new
// dependency, a new outbound call site, a new security-critical flag.
//
//	go run ./cmd/securityimpact -base old.json -new docs/security/attack_surface.json
//
// Exit 0 always, unless -strict is given and a high-risk change exists (2).
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

type route struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	Auth   string `json:"auth"`
}

type manifest struct {
	Routes         []route `json:"routes"`
	BackgroundJobs []struct {
		Starter string `json:"starter"`
	} `json:"background_jobs"`
	EnvironmentFlags []struct {
		Name             string `json:"name"`
		SecurityCritical bool   `json:"security_critical"`
	} `json:"environment_flags"`
	OutboundCallSites []struct {
		File string `json:"file"`
		Kind string `json:"kind"`
	} `json:"outbound_call_sites"`
	Dependencies []struct {
		Module  string `json:"module"`
		Version string `json:"version"`
	} `json:"dependencies"`
	CLICommands []struct {
		Name string `json:"name"`
	} `json:"cli_commands"`
	Migrations struct {
		IncrementalFiles int `json:"incremental_files"`
	} `json:"migrations"`
}

// impact is the comparison, ready to print.
type impact struct {
	High  []string
	Lines []string
}

func (im *impact) add(high bool, format string, args ...interface{}) {
	line := fmt.Sprintf(format, args...)
	if high {
		im.High = append(im.High, line)
		return
	}
	im.Lines = append(im.Lines, line)
}

// openAuth is an authentication level that does not need a signed-in user.
func openAuth(auth string) bool {
	return auth == "public" || auth == "none"
}

func compare(base, next manifest) impact {
	var im impact
	key := func(r route) string { return r.Method + " " + r.Path }
	baseRoutes := map[string]route{}
	for _, r := range base.Routes {
		baseRoutes[key(r)] = r
	}
	nextRoutes := map[string]route{}
	for _, r := range next.Routes {
		nextRoutes[key(r)] = r
	}
	for _, k := range sortedKeys(nextRoutes) {
		r := nextRoutes[k]
		old, existed := baseRoutes[k]
		switch {
		case !existed && openAuth(r.Auth):
			im.add(true, "new route anyone can reach: %s (%s)", k, r.Auth)
		case !existed:
			im.add(false, "new route: %s (%s)", k, r.Auth)
		case old.Auth != r.Auth:
			im.add(true, "route authentication changed: %s %s -> %s", k, old.Auth, r.Auth)
		}
	}
	for _, k := range sortedKeys(baseRoutes) {
		if _, ok := nextRoutes[k]; !ok {
			im.add(false, "route removed: %s", k)
		}
	}

	setDiff := func(label string, high bool, base, next []string) {
		b := map[string]bool{}
		for _, v := range base {
			b[v] = true
		}
		n := map[string]bool{}
		for _, v := range next {
			n[v] = true
			if !b[v] {
				im.add(high, "new %s: %s", label, v)
			}
		}
		for _, v := range base {
			if !n[v] {
				im.add(false, "%s removed: %s", label, v)
			}
		}
	}
	var bj, nj, bc, nc []string
	for _, j := range base.BackgroundJobs {
		bj = append(bj, j.Starter)
	}
	for _, j := range next.BackgroundJobs {
		nj = append(nj, j.Starter)
	}
	setDiff("background job", false, bj, nj)
	for _, c := range base.CLICommands {
		bc = append(bc, c.Name)
	}
	for _, c := range next.CLICommands {
		nc = append(nc, c.Name)
	}
	setDiff("command", false, bc, nc)

	baseFlags := map[string]bool{}
	for _, f := range base.EnvironmentFlags {
		baseFlags[f.Name] = true
	}
	for _, f := range next.EnvironmentFlags {
		if !baseFlags[f.Name] {
			im.add(f.SecurityCritical, "new environment flag: %s%s", f.Name, map[bool]string{true: " (security-critical)"}[f.SecurityCritical])
		}
	}

	var bo, no []string
	for _, o := range base.OutboundCallSites {
		bo = append(bo, o.File+" ("+o.Kind+")")
	}
	for _, o := range next.OutboundCallSites {
		no = append(no, o.File+" ("+o.Kind+")")
	}
	setDiff("outbound call site", true, bo, no)

	baseDeps := map[string]string{}
	for _, d := range base.Dependencies {
		baseDeps[d.Module] = d.Version
	}
	for _, d := range next.Dependencies {
		old, ok := baseDeps[d.Module]
		switch {
		case !ok:
			im.add(true, "new dependency: %s %s", d.Module, d.Version)
		case old != d.Version:
			im.add(false, "dependency version: %s %s -> %s", d.Module, old, d.Version)
		}
	}
	if delta := next.Migrations.IncrementalFiles - base.Migrations.IncrementalFiles; delta != 0 {
		im.add(false, "%+d migration file(s) - their data statements are G9's", delta)
	}
	return im
}

func sortedKeys(m map[string]route) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func load(path string) (manifest, error) {
	var m manifest
	b, err := os.ReadFile(path)
	if err != nil {
		return m, err
	}
	return m, json.Unmarshal(b, &m)
}

func render(w io.Writer, im impact) {
	if len(im.High) == 0 && len(im.Lines) == 0 {
		fmt.Fprintln(w, "No change to the attack surface.")
		return
	}
	if len(im.High) > 0 {
		fmt.Fprintf(w, "HIGH-RISK (%d) - a security owner reads each before go-live:\n", len(im.High))
		for _, l := range im.High {
			fmt.Fprintln(w, "  ! "+l)
		}
	}
	if len(im.Lines) > 0 {
		fmt.Fprintf(w, "Other changes (%d):\n", len(im.Lines))
		for _, l := range im.Lines {
			fmt.Fprintln(w, "  - "+l)
		}
	}
}

func main() {
	basePath := flag.String("base", "", "the last release's attack_surface.json")
	nextPath := flag.String("new", "docs/security/attack_surface.json", "the release set's attack_surface.json")
	strict := flag.Bool("strict", false, "exit 2 when a high-risk change exists")
	flag.Parse()
	if strings.TrimSpace(*basePath) == "" {
		fmt.Fprintln(os.Stderr, "-base is required")
		os.Exit(1)
	}
	base, err := load(*basePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read -base: %v\n", err)
		os.Exit(1)
	}
	next, err := load(*nextPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read -new: %v\n", err)
		os.Exit(1)
	}
	im := compare(base, next)
	render(os.Stdout, im)
	if *strict && len(im.High) > 0 {
		os.Exit(2)
	}
}
