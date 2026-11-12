// Package repolayout has no runtime code. Its test checks the folder and
// file-naming rules in CONTRIBUTING.md (the project constitution,
// Articles 3 and 4). A file in the wrong place or with the wrong name then
// fails `go test ./...` in CI and in release gate G10, so nobody has to
// spot it in review.
//
// Files that were named before the constitution existed are listed in
// testdata/legacy-names.txt. That list may only shrink: rename a file, then
// delete its line. Never add a line to make a new file pass.
package repolayout
