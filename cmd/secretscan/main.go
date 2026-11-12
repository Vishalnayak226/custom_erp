// Command secretscan looks for secrets beyond the working tree (item 49.6.6):
// git history, built binaries and a backup folder. The working tree itself is
// checked by internal/securityscan's TestNoUnreviewedCommittedSecret.
//
//	secretscan [-root .] [-history N] [-binary file]... [-backups dir]
//
// -history N scans the lines added by the last N commits (0 = every commit).
// It prints the kind, file and commit only, never the value, and exits 1 when
// anything is found. A real find needs the secret rotated; rewriting history
// does not un-leak it.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"custom_erp/internal/securityscan"
)

type listFlag []string

func (l *listFlag) String() string     { return strings.Join(*l, ",") }
func (l *listFlag) Set(v string) error { *l = append(*l, v); return nil }

func main() {
	root := flag.String("root", ".", "repository root")
	history := flag.Int("history", -1, "scan the last N commits of git history (0 = all; omit to skip)")
	var bins listFlag
	flag.Var(&bins, "binary", "a built binary to scan (repeatable)")
	backups := flag.String("backups", "", "a backup folder to check (every backup encrypted, none in clear)")
	flag.Parse()
	if *history < 0 && len(bins) == 0 && *backups == "" {
		fmt.Fprintln(os.Stderr, "usage: secretscan [-root .] [-history N] [-binary file]... [-backups dir]")
		os.Exit(2)
	}
	bad := 0
	if *history >= 0 {
		fs, err := securityscan.ScanHistory(*root, *history)
		if err != nil {
			fmt.Fprintln(os.Stderr, "history:", err)
			os.Exit(2)
		}
		for _, f := range fs {
			fmt.Printf("history  %-26s %s  commit %s  %s\n", f.Kind, f.File, f.Commit, f.Excerpt)
		}
		bad += len(fs)
	}
	for _, b := range bins {
		fs, err := securityscan.ScanBinary(b)
		if err != nil {
			fmt.Fprintln(os.Stderr, "binary:", err)
			os.Exit(2)
		}
		for _, f := range fs {
			fmt.Printf("binary   %-26s %s  offset %d\n", f.Kind, f.File, f.Line)
		}
		bad += len(fs)
	}
	if *backups != "" {
		ps, err := securityscan.CheckBackupDir(*backups)
		if err != nil {
			fmt.Fprintln(os.Stderr, "backups:", err)
			os.Exit(2)
		}
		for _, p := range ps {
			fmt.Printf("backup   %s  %s\n", p.File, p.Reason)
		}
		bad += len(ps)
	}
	if bad > 0 {
		fmt.Printf("%d finding(s). Rotate any real secret; do not paste values anywhere.\n", bad)
		os.Exit(1)
	}
	fmt.Println("clean")
}
