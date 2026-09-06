package securityscan

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Stage 49.1.6 - "compares release manifest, routes, config ... to the
// approved profile; unexpected exposure blocks deployment".
//
// The approved profile is docs/security/attack_surface.json, reviewed and
// committed like any other source file. Drift is therefore an ordinary diff -
// but a raw JSON diff of a 500-route document is unreadable, so DescribeDrift
// turns it into the sentences a reviewer actually needs: what became
// reachable, what stopped being reachable, and what changed the gate it sits
// behind. That last category is the one that matters most and is the easiest
// to miss in a diff, because the route's line barely changes.

// Encode renders a Surface as the exact bytes the committed manifest holds:
// indented JSON with a trailing newline, so it reads in a diff and ends like
// every other text file in the repository.
func Encode(s *Surface) ([]byte, error) {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func routeKey(r Route) string { return r.Method + " " + r.Path }

// DescribeDrift returns a human-readable summary of what changed between an
// approved surface and the current one. The empty string means no change.
func DescribeDrift(approved, current *Surface) string {
	var b strings.Builder

	if approved.SchemaVersion != current.SchemaVersion {
		fmt.Fprintf(&b, "  * inventory schema version %d -> %d: regenerate, then re-review the whole file rather than the diff\n",
			approved.SchemaVersion, current.SchemaVersion)
	}

	before := map[string]Route{}
	for _, r := range approved.Routes {
		before[routeKey(r)] = r
	}
	after := map[string]Route{}
	for _, r := range current.Routes {
		after[routeKey(r)] = r
	}

	var added, removed, regated []string
	for key, r := range after {
		prev, existed := before[key]
		if !existed {
			added = append(added, fmt.Sprintf("  + %s  [auth: %s]  %s", key, r.Auth, r.Source))
			continue
		}
		if prev.Auth != r.Auth || prev.Scope != r.Scope {
			regated = append(regated, fmt.Sprintf("  ! %s  auth %s -> %s, scope %q -> %q",
				key, prev.Auth, r.Auth, prev.Scope, r.Scope))
		}
	}
	for key, r := range before {
		if _, still := after[key]; !still {
			removed = append(removed, fmt.Sprintf("  - %s  [was auth: %s]", key, r.Auth))
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	sort.Strings(regated)

	section := func(title string, lines []string) {
		if len(lines) == 0 {
			return
		}
		fmt.Fprintf(&b, "%s (%d):\n%s\n", title, len(lines), strings.Join(lines, "\n"))
	}
	section("Routes now reachable that the approved profile does not list", added)
	section("Routes in the approved profile that no longer exist", removed)
	section("Routes whose authentication class or scope changed", regated)

	section("Background jobs added", diffStrings(jobNames(approved), jobNames(current)))
	section("Background jobs removed", diffStrings(jobNames(current), jobNames(approved)))
	section("Environment flags added", diffStrings(flagNames(approved), flagNames(current)))
	section("Environment flags removed", diffStrings(flagNames(current), flagNames(approved)))
	section("Outbound call sites added", diffStrings(outboundNames(approved), outboundNames(current)))
	section("Outbound call sites removed", diffStrings(outboundNames(current), outboundNames(approved)))
	section("Dependencies added", diffStrings(dependencyNames(approved), dependencyNames(current)))
	section("Dependencies removed", diffStrings(dependencyNames(current), dependencyNames(approved)))

	var totals []string
	for key, now := range current.Totals {
		if was, ok := approved.Totals[key]; !ok || was != now {
			totals = append(totals, fmt.Sprintf("  %s: %d -> %d", key, approved.Totals[key], now))
		}
	}
	sort.Strings(totals)
	section("Totals", totals)

	if b.Len() == 0 {
		// Something outside the categories above moved - a route's source line,
		// a migration count, a static asset. Say so rather than reporting "no
		// drift" for a file that does not match.
		return "  The inventory differs from the approved profile in a field this summary does not itemise\n" +
			"  (a registration's file:line, the migration or static-asset counts, or a static root's\n" +
			"  contents). Regenerate and read the diff directly."
	}
	return b.String()
}

func diffStrings(from, to []string) []string {
	have := map[string]bool{}
	for _, v := range from {
		have[v] = true
	}
	var out []string
	for _, v := range to {
		if !have[v] {
			out = append(out, "  "+v)
		}
	}
	sort.Strings(out)
	return out
}

func jobNames(s *Surface) []string {
	out := make([]string, 0, len(s.BackgroundJobs))
	for _, j := range s.BackgroundJobs {
		out = append(out, j.Starter+" every "+j.Interval)
	}
	return out
}

func flagNames(s *Surface) []string {
	out := make([]string, 0, len(s.EnvironmentFlags))
	for _, f := range s.EnvironmentFlags {
		name := f.Name
		if f.SecurityCritical {
			name += " (security-critical)"
		}
		out = append(out, name)
	}
	return out
}

func outboundNames(s *Surface) []string {
	out := make([]string, 0, len(s.OutboundCallSites))
	for _, o := range s.OutboundCallSites {
		out = append(out, o.File+" ("+o.Kind+")")
	}
	return out
}

func dependencyNames(s *Surface) []string {
	out := make([]string, 0, len(s.Dependencies))
	for _, d := range s.Dependencies {
		out = append(out, d.Module+" "+d.Version)
	}
	return out
}
