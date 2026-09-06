package server

import (
	"io/fs"
	"net/http"
	"path"
)

// Stage 49.1.2 - "directory listing ... absent or unreachable in production".
//
// routes.go serves public/ with a plain http.FileServer(http.Dir("./public")).
// Go's FileServer generates an HTML index for any directory that has no
// index.html, and two such directories ship in this repository:
// public/components and public/profiles. An unauthenticated GET /components/
// therefore returned a complete, linked listing of every front-end module
// file - a free map of the application's internal structure, handed to an
// attacker before they have a login, and the exact "unclassified surface"
// 49.1 exists to remove.
//
// Nothing in the product ever needs that listing: every asset the SPA loads,
// it loads by exact path. So the fix is to make a directory with no index.html
// simply not exist as far as the file server is concerned, rather than to add
// a rule to the reverse proxy - the Go process is reachable directly over the
// deployment's SSH tunnel as well as through Caddy, and a control that only
// one of those two paths enforces is not a control.
//
// noDirectoryListingFS wraps http.Dir rather than replacing http.FileServer so
// that everything else FileServer does correctly - Range requests, If-Modified-Since,
// Content-Type sniffing, %-decoding and path cleaning - keeps working untouched.
type noDirectoryListingFS struct {
	inner http.FileSystem
}

// noDirectoryListing returns fsys with directory indexes suppressed: a
// directory that contains an index.html still serves it, and any other
// directory reports "does not exist", which http.FileServer turns into a plain
// 404 identical to the one an unknown filename gets. Identical responses for
// "no such directory" and "a directory you may not enumerate" is deliberate -
// a different status or body would itself confirm the directory exists.
func noDirectoryListing(fsys http.FileSystem) http.FileSystem {
	return noDirectoryListingFS{inner: fsys}
}

func (n noDirectoryListingFS) Open(name string) (http.File, error) {
	f, err := n.inner.Open(name)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if !info.IsDir() {
		return f, nil
	}
	// http.FileServer opens the directory first and only then looks for its
	// index.html, so this is the one place that can tell the two cases apart
	// before the listing is generated.
	index, err := n.inner.Open(path.Join(name, "index.html"))
	if err != nil {
		f.Close()
		return nil, fs.ErrNotExist
	}
	index.Close()
	return f, nil
}
