// Command genkb builds the Knowledge Center: Markdown in docs/kb/ becomes
// inert HTML fragments, a navigation index and a prebuilt search index under
// internal/kb/content/, which is embedded into the server binary.
//
//	go run ./cmd/genkb                 # build and write
//	go run ./cmd/genkb -check          # fail if the committed output is stale
//	go run ./cmd/genkb -out <dir>      # write somewhere else
//
// -check is what the build gate runs: it rebuilds in memory and compares,
// exiting non-zero when the committed output no longer matches its source.
//
// Windows note: Controlled Folder Access refuses writes under Documents\ from
// an unrecognised binary and reports it as "the system cannot find the file
// specified". Same failure and workaround as cmd/gendocs and cmd/brainmap -
// generate into %TEMP% with -out and copy in with PowerShell, which is exactly
// what docs/kb/update-kb.ps1 does.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"custom_erp/internal/kb"
)

func main() {
	source := flag.String("source", filepath.Join("docs", "kb"), "directory of Markdown articles")
	out := flag.String("out", filepath.Join("internal", "kb", "content"), "directory to write the generated Knowledge Center into")
	check := flag.Bool("check", false, "do not write; exit non-zero if the generated output is stale or orphaned")
	quiet := flag.Bool("quiet", false, "suppress per-article warnings")
	flag.Parse()

	result, err := kb.Build(*source)
	if err != nil {
		fmt.Printf("  [fail] knowledge center build: %v\n", err)
		os.Exit(1)
	}
	if !*quiet {
		for _, warning := range result.Warnings {
			fmt.Printf("  [warn] %s\n", warning)
		}
	}

	if *check {
		differences := kb.Diff(*out, result)
		if len(differences) == 0 {
			fmt.Printf("  [ok]   knowledge center is current (%d articles)\n", result.Index.ArticleCount)
			return
		}
		fmt.Printf("  [fail] knowledge center output is out of date (%d difference(s)):\n", len(differences))
		for _, difference := range differences {
			fmt.Printf("         %s\n", difference)
		}
		fmt.Println("         Run `go run ./cmd/genkb` (or docs/kb/update-kb.ps1) and commit the result.")
		os.Exit(1)
	}

	if err := kb.WriteTo(*out, result); err != nil {
		fmt.Printf("  [fail] write %s: %v\n", *out, err)
		if os.IsNotExist(err) || filepath.IsAbs(*out) {
			fmt.Println("         On Windows this is usually Controlled Folder Access blocking an")
			fmt.Println("         unrecognised binary from writing under Documents\\. Generate into a")
			fmt.Println("         TEMP directory with -out and copy in with PowerShell, as")
			fmt.Println("         docs/kb/update-kb.ps1 does.")
		}
		os.Exit(1)
	}
	fmt.Printf("  [ok]   %s (%d articles, %d files)\n", *out, result.Index.ArticleCount, len(result.Files))
}
