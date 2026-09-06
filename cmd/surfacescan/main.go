// Command surfacescan regenerates the Stage 49.1.1 machine-readable
// attack-surface inventory, and (with -check) reports whether the committed
// copy is still current - the 49.1.6 baseline drift check.
//
// Usage:
//
//	go run ./cmd/surfacescan                  # rewrite docs/security/attack_surface.json
//	go run ./cmd/surfacescan -check           # exit 1 if it is stale, write nothing
//	go run ./cmd/surfacescan -out FILE        # write somewhere else
//
// On this project's Windows dev machine, Controlled Folder Access blocks a
// freshly built binary from writing under Documents\ and reports it as "the
// system cannot find the file specified" (see CLAUDE.md). Use -out to write
// into %TEMP% and copy the file in with PowerShell, the same way
// docs/brain/update-brain.ps1 does.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"custom_erp/internal/securityscan"
)

func main() {
	root := flag.String("root", ".", "repository root to scan")
	out := flag.String("out", filepath.Join("docs", "security", "attack_surface.json"), "file to write the inventory to")
	check := flag.Bool("check", false, "compare against the file at -out and exit 1 if it differs; write nothing")
	bypass := flag.Bool("bypass", false, "print the 49.1.4 no-bypass scan instead of the surface inventory")
	flag.Parse()

	if *bypass {
		findings, err := securityscan.ScanBypass(*root)
		if err != nil {
			fmt.Fprintf(os.Stderr, "surfacescan: %v\n", err)
			os.Exit(2)
		}
		if len(findings) == 0 {
			fmt.Println("surfacescan: no bypass-pattern hits")
			return
		}
		for _, f := range findings {
			fmt.Printf("%-24s %-8s %s:%d\n    %s\n", f.Category, f.Severity, f.File, f.Line, f.Excerpt)
		}
		fmt.Printf("\n%d hit(s). Each must be removed from the code or recorded in reviewedBypassFindings\n(internal/securityscan/bypass_test.go) with the reason it is safe.\n", len(findings))
		return
	}

	surface, err := securityscan.ScanSurface(*root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "surfacescan: %v\n", err)
		os.Exit(2)
	}
	encoded, err := securityscan.Encode(surface)
	if err != nil {
		fmt.Fprintf(os.Stderr, "surfacescan: %v\n", err)
		os.Exit(2)
	}

	// -out is resolved against -root unless it is already absolute, so the
	// %TEMP% workaround above can pass a full path.
	target := *out
	if !filepath.IsAbs(target) {
		target = filepath.Join(*root, target)
	}

	if *check {
		committed, err := os.ReadFile(target)
		if err != nil {
			fmt.Fprintf(os.Stderr, "surfacescan: no committed inventory at %s (%v) - run without -check to create it\n", *out, err)
			os.Exit(1)
		}
		if string(committed) != string(encoded) {
			var previous securityscan.Surface
			_ = json.Unmarshal(committed, &previous)
			fmt.Fprintf(os.Stderr, "surfacescan: %s is out of date - the attack surface changed.\n\n%s\n",
				*out, securityscan.DescribeDrift(&previous, surface))
			os.Exit(1)
		}
		fmt.Printf("surfacescan: %s is current (%d routes, %d background jobs, %d security-critical environment flags)\n",
			*out, surface.Totals["routes"], surface.Totals["background_jobs"], surface.Totals["environment_flags_security_critical"])
		return
	}

	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "surfacescan: %v\n", err)
		os.Exit(2)
	}
	if err := os.WriteFile(target, encoded, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "surfacescan: %v\n", err)
		os.Exit(2)
	}
	fmt.Printf("surfacescan: wrote %s (%d routes, %d background jobs, %d CLI commands, %d environment flags, %d outbound call sites)\n",
		target, surface.Totals["routes"], surface.Totals["background_jobs"], surface.Totals["cli_commands"],
		surface.Totals["environment_flags"], surface.Totals["outbound_call_sites"])
}
