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
	"time"

	"custom_erp/internal/docgen"
	"custom_erp/internal/kb"
)

func main() {
	source := flag.String("source", filepath.Join("docs", "kb"), "directory of Markdown articles")
	out := flag.String("out", filepath.Join("internal", "kb", "content"), "directory to write the generated Knowledge Center into")
	check := flag.Bool("check", false, "do not write; exit non-zero if the generated output is stale or orphaned")
	quiet := flag.Bool("quiet", false, "suppress per-article warnings")
	manuals := flag.String("manuals", "", "optional curated manual selection JSON; builds manuals instead of embedded KB")
	release := flag.String("release", "", "source release label required for manual projections")
	appJS := flag.String("app-js", filepath.Join("public", "app.js"), "path to the frontend router, for the 39.8 screen-id drift guard")
	errorCatalog := flag.String("error-catalog", filepath.Join("internal", "server", "error_catalog_generated.go"), "path to the error catalog, for the 39.8 error-code drift guard")
	flag.Parse()

	result, err := kb.Build(*source)
	if err != nil {
		fmt.Printf("  [fail] knowledge center build: %v\n", err)
		os.Exit(1)
	}
	if *manuals != "" {
		if *release == "" {
			fmt.Fprintln(os.Stderr, "manuals require -release and an explicit -out directory")
			os.Exit(1)
		}
		explicitOut := false
		flag.Visit(func(f *flag.Flag) {
			if f.Name == "out" {
				explicitOut = true
			}
		})
		if !explicitOut {
			fmt.Fprintln(os.Stderr, "manuals require an explicit -out directory")
			os.Exit(1)
		}
		files, err := kb.BuildManuals(result, *source, *manuals, *release)
		if err == nil && *check {
			if diffs := docgen.Diff(*out, files); len(diffs) != 0 {
				err = fmt.Errorf("manual drift: %v", diffs)
			}
		} else if err == nil {
			err = docgen.Write(*out, files)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Printf("  [ok]   %d curated manuals verified\n", len(files))
		return
	}
	if !*quiet {
		for _, warning := range result.Warnings {
			fmt.Printf("  [warn] %s\n", warning)
		}
		driftSources := kb.DriftSources{
			AppJSPath:        *appJS,
			ErrorCatalogPath: *errorCatalog,
			RouteFiles: []string{
				filepath.Join("internal", "server", "routes.go"),
				filepath.Join("internal", "server", "routes_public_api_v1.go"),
			},
		}
		for _, warning := range kb.DriftGuards(result.Articles, driftSources, time.Now()) {
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
