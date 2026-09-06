// Package docgen provides deterministic, read-only comparison for documentation generators.
package docgen

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Path rejects output names that could escape a generator's explicit root.
func Path(root, name string) (string, error) {
	if !filepath.IsLocal(name) || strings.Contains(name, "\\") || strings.Contains(name, ":") {
		return "", fmt.Errorf("unsafe generated path %q", name)
	}
	return filepath.Join(root, filepath.FromSlash(name)), nil
}

// Diff never creates directories or touches timestamps, including on failure.
func Diff(root string, files map[string][]byte) []string {
	var differences []string
	for name, body := range files {
		path, err := Path(root, name)
		if err != nil {
			differences = append(differences, err.Error())
			continue
		}
		actual, err := os.ReadFile(path)
		switch {
		case os.IsNotExist(err):
			differences = append(differences, name+" (missing)")
		case err != nil:
			differences = append(differences, name+" (unreadable)")
		case !bytes.Equal(normalize(actual), normalize(body)):
			differences = append(differences, name+" (stale)")
		}
	}
	sort.Strings(differences)
	return differences
}

// Normalize only checkout line endings; dates and all authored bytes remain significant.
func normalize(body []byte) []byte { return bytes.ReplaceAll(body, []byte("\r\n"), []byte("\n")) }

func Write(root string, files map[string][]byte) error {
	// Validate every name before the first write.
	for name := range files {
		if _, err := Path(root, name); err != nil {
			return err
		}
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		path, _ := Path(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, files[name], 0o644); err != nil {
			return err
		}
	}
	return nil
}
