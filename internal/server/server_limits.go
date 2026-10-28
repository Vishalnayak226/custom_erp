package server

import (
	"net/http"
	"time"
)

// Stage 49.10.5: what one slow or oversized client can hold of this server,
// bounded before any handler or the request envelope runs.
//
//   - Headers must arrive within serverReadHeaderTimeout. Without it a
//     connection could trickle its headers for the whole ReadTimeout, holding
//     a socket and a goroutine each; 10 seconds is ample for a shop on a slow
//     mobile link, whose headers are one small packet.
//   - The request line and headers are capped at serverMaxHeaderBytes (Go's
//     default is 1 MiB). The envelope already refuses more than 32 KiB of
//     headers once they are parsed; 64 KiB leaves room for a long bearer
//     token and cookies while no longer reading a megabyte to say so.
//   - ReadTimeout, WriteTimeout and IdleTimeout are unchanged: a long export
//     and a streamed report depend on them, and the envelope's 55-second
//     deadline sits inside the 60-second WriteTimeout on purpose.
const (
	serverReadHeaderTimeout = 10 * time.Second
	serverMaxHeaderBytes    = 64 << 10
)

// newHTTPServer is the one place the HTTP server's connection bounds are set.
func newHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: serverReadHeaderTimeout,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    serverMaxHeaderBytes,
	}
}
