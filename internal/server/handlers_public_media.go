package server

import (
	"net/http"
	"strconv"
	"time"

	"custom_erp/engines"
)

// handlePublicMedia (Stage 146.5.9) serves one product image to a marketplace
// that holds its signed address, GET /media/{tenant}/{id}/{sig}.
//
// Registered without apiMiddleware on purpose, like /internal/tls-ask: the
// caller has no login and no headers to send, and the client is named by the
// signed address itself, never by a session, an X-Tenant-ID or a token - so an
// extension or session token sent along changes nothing. The handler is
// solely responsible for its own access control (engines.ReadPublicMedia) and
// for the same rate-limit bucket image reads use.
//
// Every refusal is the same plain 404, whatever the reason.
func handlePublicMedia(w http.ResponseWriter, r *http.Request) {
	category, limit := rateLimitCategory(r.URL.Path, http.MethodGet)
	key := clientIP(r) + ":" + category
	if !globalLimiter.Allow(key, limit, time.Minute) {
		if wait := globalLimiter.RetryAfter(key, time.Minute); wait > 0 {
			w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
		}
		w.Header().Set("Cache-Control", "no-store")
		http.Error(w, "Too many requests", http.StatusTooManyRequests)
		return
	}
	b, fileType, err := engines.ReadPublicMedia(r.PathValue("tenant"), r.PathValue("id"), r.PathValue("sig"))
	if err != nil { // whatever the reason, the caller learns nothing
		w.Header().Set("Cache-Control", "no-store")
		http.NotFound(w, r)
		return
	}
	h := w.Header()
	h.Set("Content-Type", fileType)
	h.Set("X-Content-Type-Options", "nosniff")
	// The media id carries the file's checksum (generateMediaID), so an address
	// always names the same bytes: a changed picture gets a new id and address.
	h.Set("Cache-Control", "public, max-age=31536000, immutable")
	h.Set("Cross-Origin-Resource-Policy", "cross-origin")
	h.Set("Content-Length", strconv.Itoa(len(b)))
	_, _ = w.Write(b)
}
