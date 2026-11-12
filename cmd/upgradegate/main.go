// Command upgradegate decides whether the Deploy click may put a build on a
// host, from the host's UPGRADE_CHANNEL (recorded by cmd/installcheck) and the
// build's own channel (release/channel.txt: "stable", or "candidate" for an
// early build). Item 162.8.
//
//	upgradegate -host-env <file|-> [-build stable|candidate]
//
// Exit 0 = go ahead, 1 = stop, 2 = usage. The reason is printed either way.
// It reads only the UPGRADE_CHANNEL line; no other value in the file is used.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

// Decision is the outcome for one host and one build.
type Decision struct {
	Allow  bool
	Host   string // the channel that was applied
	Reason string
}

// ParseChannel returns the UPGRADE_CHANNEL value in env-file text, lower-cased
// and trimmed of quotes; ok is false when the line is absent. The last line wins.
func ParseChannel(envText string) (string, bool) {
	val, ok := "", false
	sc := bufio.NewScanner(strings.NewReader(envText))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}
		if k, v, found := strings.Cut(line, "="); found && strings.TrimSpace(k) == "UPGRADE_CHANNEL" {
			val, ok = strings.ToLower(strings.Trim(strings.TrimSpace(v), `"'`)), true
		}
	}
	return val, ok
}

// Decide applies the rules: hold takes nothing; stable takes only stable
// builds; candidate takes both. A host with no channel recorded is treated as
// stable; an unknown value stops the deploy (fail safe).
func Decide(hostChannel, buildChannel string) Decision {
	host := strings.ToLower(strings.TrimSpace(hostChannel))
	build := strings.ToLower(strings.TrimSpace(buildChannel))
	if build == "" {
		build = "stable"
	}
	if build != "stable" && build != "candidate" {
		return Decision{false, host, fmt.Sprintf("the build's channel %q is not stable or candidate", build)}
	}
	switch host {
	case "":
		host = "stable"
		if build == "candidate" {
			return Decision{false, host, "no UPGRADE_CHANNEL on the host (treated as stable) and this is a candidate build"}
		}
		return Decision{true, host, "no UPGRADE_CHANNEL on the host; treated as stable, build is stable"}
	case "hold":
		return Decision{false, host, "the host is on hold (UPGRADE_CHANNEL=hold); change it by hand to deploy"}
	case "stable":
		if build == "candidate" {
			return Decision{false, host, "candidate build refused on a stable host"}
		}
		return Decision{true, host, "stable host, stable build"}
	case "candidate":
		return Decision{true, host, "candidate host takes " + build + " builds"}
	}
	return Decision{false, host, fmt.Sprintf("UPGRADE_CHANNEL=%q is not stable, candidate or hold", host)}
}

func main() { os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }

func run(args []string, in io.Reader, out, errw io.Writer) int {
	fs := flag.NewFlagSet("upgradegate", flag.ContinueOnError)
	fs.SetOutput(errw)
	hostEnv := fs.String("host-env", "", "host env file, or - for stdin (only UPGRADE_CHANNEL is read)")
	build := fs.String("build", "stable", "the build's channel: stable or candidate")
	if err := fs.Parse(args); err != nil || *hostEnv == "" {
		fmt.Fprintln(errw, "usage: upgradegate -host-env <file|-> [-build stable|candidate]")
		return 2
	}
	var b []byte
	var err error
	if *hostEnv == "-" {
		b, err = io.ReadAll(in)
	} else {
		b, err = os.ReadFile(*hostEnv)
	}
	if err != nil {
		fmt.Fprintln(errw, "upgradegate:", err)
		return 2
	}
	ch, _ := ParseChannel(string(b))
	d := Decide(ch, *build)
	verdict := "STOP"
	if d.Allow {
		verdict = "OK"
	}
	fmt.Fprintf(out, "%s: %s\n", verdict, d.Reason)
	if d.Allow {
		return 0
	}
	return 1
}
