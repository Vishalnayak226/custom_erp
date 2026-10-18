// Command releasever prints the version the next release gets (Stage 90).
//
//	go run ./cmd/releasever -previous 0.1.0 -since 840909b
//
// -previous and -since are the live build's version and commit (deploy.ps1
// reads them from the box's /api/v1/version). The commits since then, up to
// HEAD, go through engines.NextReleaseVersion: a feat commit bumps MINOR,
// anything else PATCH. The version goes to stdout, the reason to stderr.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"custom_erp/engines"
)

func main() {
	previous := flag.String("previous", "", "the live build's version, e.g. 0.1.0")
	since := flag.String("since", "", "the live build's commit")
	repo := flag.String("repo", ".", "repository root")
	flag.Parse()
	if *previous == "" || *since == "" || *since == "dev" {
		fail("-previous and -since are the live build's version and commit; pass deploy.ps1 -Version to set one by hand")
	}
	if err := exec.Command("git", "-C", *repo, "merge-base", "--is-ancestor", *since, "HEAD").Run(); err != nil {
		fail(fmt.Sprintf("the live commit %s is not an ancestor of HEAD, so the commits in between are unknown; pass deploy.ps1 -Version", *since))
	}
	out, err := exec.Command("git", "-C", *repo, "log", "--format=%s", *since+"..HEAD").Output()
	if err != nil {
		fail("git log failed: " + err.Error())
	}
	var subjects []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.TrimSpace(line) != "" {
			subjects = append(subjects, line)
		}
	}
	next, reason, err := engines.NextReleaseVersion(*previous, subjects)
	if err != nil {
		fail(err.Error())
	}
	fmt.Fprintf(os.Stderr, "releasever: %s -> %s (%s since %s)\n", *previous, next, reason, *since)
	fmt.Println(next)
}

func fail(msg string) {
	fmt.Fprintln(os.Stderr, "releasever: "+msg)
	os.Exit(1)
}
