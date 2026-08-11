package server

import (
	"compress/gzip"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

// Response compression and static-asset caching (Stage 40.4).
//
// public/app.js is ~700KB of plain text and public/styles.css another ~60KB.
// Uncompressed, that is the single largest cost of a cold load - larger than
// every API call on the boot path put together. gzip takes the pair to well
// under a fifth of that.
//
// Caddy already sets `encode gzip` in front of the live deployment
// (deploy/Caddyfile), but that only covers the reverse-proxied path. The SSH
// tunnel straight to :8080 that this project actually uses day to day never
// touches Caddy, and neither does a local dev server - so the two paths
// behaved completely differently. Doing it here means one behaviour
// everywhere, and Caddy will not double-compress a response that already
// carries Content-Encoding.
//
// compress/gzip is stdlib. No new dependency, per this project's first
// principle - and no build step, bundler or asset pipeline is introduced
// either: the files on disk stay exactly what they are, they are just sent
// smaller.

// gzipWriterPool reuses the compressors. A gzip.Writer allocates a ~64KB
// window; minting one per request would trade bandwidth for garbage.
var gzipWriterPool = sync.Pool{
	New: func() interface{} { return gzip.NewWriter(io.Discard) },
}

// compressibleTypes are the content types worth compressing. Images, fonts
// and anything already compressed are deliberately absent: gzipping a PNG
// costs CPU and typically makes it fractionally larger.
func isCompressible(contentType string) bool {
	ct := strings.ToLower(strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0]))
	switch ct {
	case "text/html", "text/css", "text/plain", "text/csv",
		"application/javascript", "text/javascript",
		"application/json", "application/xml", "text/xml",
		"image/svg+xml":
		return true
	}
	return false
}

// minCompressSize is the floor below which compression is not worth it - the
// gzip header and trailer alone are ~20 bytes, and a small JSON API response
// can come out larger than it went in.
const minCompressSize = 1024

// gzipResponseWriter defers the decision to compress until the first Write,
// because that is the earliest point Content-Type is known (handlers set it
// explicitly or rely on http.DetectContentType, which only runs on write).
type gzipResponseWriter struct {
	http.ResponseWriter
	gz          *gzip.Writer
	wroteHeader bool
	compressing bool
	status      int
}

func (w *gzipResponseWriter) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	w.status = status

	h := w.Header()
	// Never compress a 304 (it has no body) or a response a handler has
	// already encoded itself.
	if status == http.StatusNotModified || status == http.StatusNoContent || h.Get("Content-Encoding") != "" {
		w.ResponseWriter.WriteHeader(status)
		return
	}
	if !isCompressible(h.Get("Content-Type")) {
		w.ResponseWriter.WriteHeader(status)
		return
	}
	// A declared length below the floor is not worth compressing. An
	// undeclared length (streamed JSON, which is most of this API) is
	// compressed - those are the responses that benefit most.
	if cl := h.Get("Content-Length"); cl != "" {
		if n, err := strconv.Atoi(cl); err == nil && n < minCompressSize {
			w.ResponseWriter.WriteHeader(status)
			return
		}
	}

	w.compressing = true
	// Content-Length no longer describes the bytes on the wire, and Vary tells
	// caches that the same URL has two representations - without it a shared
	// cache can hand a gzipped body to a client that did not ask for one.
	h.Del("Content-Length")
	h.Set("Content-Encoding", "gzip")
	appendVary(h, "Accept-Encoding")

	w.gz = gzipWriterPool.Get().(*gzip.Writer)
	w.gz.Reset(w.ResponseWriter)
	w.ResponseWriter.WriteHeader(status)
}

func (w *gzipResponseWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		// Mirror net/http: an implicit 200 on first write, but only after
		// giving DetectContentType a chance, so isCompressible above sees a
		// real type rather than "".
		if w.Header().Get("Content-Type") == "" {
			w.Header().Set("Content-Type", http.DetectContentType(b))
		}
		w.WriteHeader(http.StatusOK)
	}
	if w.compressing {
		return w.gz.Write(b)
	}
	return w.ResponseWriter.Write(b)
}

// Flush keeps streaming handlers working. Without it, a response written
// incrementally would sit in the gzip buffer until close.
func (w *gzipResponseWriter) Flush() {
	if w.compressing && w.gz != nil {
		_ = w.gz.Flush()
	}
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *gzipResponseWriter) close() {
	if w.compressing && w.gz != nil {
		_ = w.gz.Close()
		gzipWriterPool.Put(w.gz)
		w.gz = nil
	}
}

func appendVary(h http.Header, value string) {
	for _, existing := range h.Values("Vary") {
		if strings.EqualFold(strings.TrimSpace(existing), value) {
			return
		}
	}
	h.Add("Vary", value)
}

// compressResponses gzips eligible responses for clients that accept it.
func compressResponses(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}
		gw := &gzipResponseWriter{ResponseWriter: w}
		defer gw.close()
		next.ServeHTTP(gw, r)
	})
}

// staticAssetCache sets caching headers on everything served from public/.
//
// The frontend already cache-busts with an explicit query version
// (index.html's `app.js?v=23`), which is what makes a long max-age safe: a
// deploy changes the version, so the browser requests a different URL rather
// than being stuck with a stale file. Before this, every asset was revalidated
// on every navigation - a round trip each for app.js, styles.css, db.js,
// the typeahead component and qz-print.js, just to be told "not modified".
//
// index.html itself is deliberately excluded: it is the document that carries
// the version numbers, so it must always be re-fetched or a deploy would never
// reach anyone.
func staticAssetCache(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		switch {
		case path == "/" || strings.HasSuffix(path, "/") || strings.HasSuffix(path, ".html"):
			// Always revalidate the shell.
			w.Header().Set("Cache-Control", "no-cache")
		case r.URL.RawQuery != "" && strings.Contains(r.URL.RawQuery, "v="):
			// Version-stamped: safe to treat as immutable for a year.
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		case isStaticAssetPath(path):
			// Unversioned asset (an image, a font). An hour is long enough to
			// cover a session's navigations, short enough that a forgotten
			// version stamp is not a day-long stale file.
			w.Header().Set("Cache-Control", "public, max-age=3600")
		}
		next.ServeHTTP(w, r)
	})
}

func isStaticAssetPath(path string) bool {
	for _, ext := range []string{".js", ".css", ".png", ".jpg", ".jpeg", ".gif", ".svg", ".ico", ".woff", ".woff2", ".webp"} {
		if strings.HasSuffix(path, ext) {
			return true
		}
	}
	return false
}
