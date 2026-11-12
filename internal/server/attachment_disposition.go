package server

import "strings"

// Stage 49.4.8 - one builder for every download's Content-Disposition header.
// A file name built from an id, a title or a user-typed name must not be able
// to add quotes, separators, path parts or control characters to the header.
// Anything outside letters, digits, space and . _ - ( ) becomes "_"; the name
// is capped, and an empty result falls back to "download".
func attachmentDisposition(name string) string {
	var b strings.Builder
	for _, ch := range name {
		switch {
		case ch >= 'a' && ch <= 'z', ch >= 'A' && ch <= 'Z', ch >= '0' && ch <= '9',
			ch == ' ', ch == '.', ch == '_', ch == '-', ch == '(', ch == ')':
			b.WriteRune(ch)
		default:
			b.WriteRune('_')
		}
		if b.Len() >= 120 {
			break
		}
	}
	safe := strings.Trim(strings.TrimSpace(b.String()), ".")
	if safe == "" {
		safe = "download"
	}
	return `attachment; filename="` + safe + `"`
}
