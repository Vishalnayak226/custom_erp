package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
)

// 297.8: a large answer that rarely changes is sent once, then revalidated.
//
// writeJSONRevalidated marshals v, tags the body with a weak ETag (a hash of
// the body) and sends `Cache-Control: private, no-cache`. When the browser's
// If-None-Match already holds that tag, the answer is a 304 with no body, so
// an unchanged report catalog costs one round trip instead of 462 KB.
//
//   - no-cache: the browser asks every time, so the server still decides for
//     this user, tenant and role. A change to any of them, or a release,
//     changes the body and so the tag; nothing has to be invalidated.
//   - private, no max-age: a till PC is shared, and the next person must
//     never be shown the last one's list without the server being asked.
//   - weak: the compression layer changes the bytes on the wire.
//
// The handler's Cache-Control replaces apiMiddleware's no-store (49.4.5).
// compressResponses passes a 304 through untouched.
func writeJSONRevalidated(w http.ResponseWriter, r *http.Request, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Could not prepare the answer")
		return
	}
	sum := sha256.Sum256(body)
	tag := `W/"` + hex.EncodeToString(sum[:16]) + `"`
	h := w.Header()
	h.Set("ETag", tag)
	h.Set("Cache-Control", "private, no-cache")
	if etagMatches(r.Header.Get("If-None-Match"), tag) {
		h.Del("Content-Type")
		w.WriteHeader(http.StatusNotModified)
		return
	}
	h.Set("Content-Type", "application/json")
	_, _ = w.Write(body)
}

// etagMatches is If-None-Match's weak comparison (RFC 9110 13.1.2): a list of
// tags, each with or without W/, or *.
func etagMatches(header, tag string) bool {
	if header == "" {
		return false
	}
	want := strings.TrimPrefix(tag, "W/")
	for _, part := range strings.Split(header, ",") {
		p := strings.TrimSpace(part)
		if p == "*" || strings.TrimPrefix(p, "W/") == want {
			return true
		}
	}
	return false
}
