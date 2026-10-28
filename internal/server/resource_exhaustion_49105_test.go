package server

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// 49.10.5: resource-exhaustion tests for what the envelope and limiter tests
// (request_envelope_test.go, rate_limit_bounds_test.go) do not already cover:
// slow clients, oversized header blocks, worst-case fragmented JSON, and
// high-cardinality login-abuse keys.

func TestServerConnectionBounds49105(t *testing.T) {
	s := newHTTPServer(":0", http.NotFoundHandler())
	if s.ReadHeaderTimeout != 10*time.Second || s.MaxHeaderBytes != 64<<10 {
		t.Fatalf("header bounds: ReadHeaderTimeout=%v MaxHeaderBytes=%d", s.ReadHeaderTimeout, s.MaxHeaderBytes)
	}
	// Unchanged, and the envelope's deadline still ends inside the write
	// timeout, so a handler that watches its context answers before the cut.
	if s.ReadTimeout != 30*time.Second || s.WriteTimeout != 60*time.Second || s.IdleTimeout != 120*time.Second || envelopeDeadline >= s.WriteTimeout {
		t.Fatalf("timeouts changed: read=%v write=%v idle=%v deadline=%v", s.ReadTimeout, s.WriteTimeout, s.IdleTimeout, envelopeDeadline)
	}
}

// serveBounded runs newHTTPServer on a loopback port; scale shortens its
// timeouts so a test does not wait the real ten seconds.
func serveBounded(t *testing.T, h http.Handler, scale func(*http.Server)) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := newHTTPServer("", h)
	if scale != nil {
		scale(s)
	}
	go func() { _ = s.Serve(l) }()
	t.Cleanup(func() { _ = s.Close() })
	return l.Addr().String()
}

// closedWithin reports whether the server closed conn within d.
func closedWithin(conn net.Conn, d time.Duration) bool {
	_ = conn.SetReadDeadline(time.Now().Add(d))
	_, err := io.Copy(io.Discard, conn)
	if ne, ok := err.(net.Error); ok && ne.Timeout() {
		return false
	}
	return true
}

func TestSlowClientsAreCut49105(t *testing.T) {
	var reached atomic.Int32
	var bodyErr atomic.Value
	addr := serveBounded(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached.Add(1)
		if _, err := io.ReadAll(r.Body); err != nil {
			bodyErr.Store(err.Error())
		}
	}), func(s *http.Server) {
		s.ReadHeaderTimeout = 300 * time.Millisecond
		s.ReadTimeout = 800 * time.Millisecond
	})

	// Slow headers: one header line every 100 ms never finishes the block.
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_, _ = conn.Write([]byte("GET /api/v1/health HTTP/1.1\r\nHost: x\r\n"))
	stop := make(chan struct{})
	go func() {
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			case <-time.After(100 * time.Millisecond):
				if _, err := fmt.Fprintf(conn, "X-Slow-%d: a\r\n", i); err != nil {
					return
				}
			}
		}
	}()
	start := time.Now()
	if !closedWithin(conn, 3*time.Second) {
		t.Fatal("a client trickling headers kept its connection past the header timeout")
	}
	close(stop)
	if elapsed := time.Since(start); elapsed > 2*time.Second || reached.Load() != 0 {
		t.Fatalf("slow headers: closed after %v, handler reached %d times", elapsed, reached.Load())
	}

	// Slow body: headers arrive, then the body trickles; the read fails at
	// ReadTimeout instead of holding the handler.
	conn2, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn2.Close()
	_, _ = conn2.Write([]byte("POST /api/v1/doc/Item HTTP/1.1\r\nHost: x\r\nContent-Type: application/json\r\nContent-Length: 100000\r\n\r\n{"))
	go func() {
		for i := 0; i < 100; i++ {
			time.Sleep(100 * time.Millisecond)
			if _, err := conn2.Write([]byte(" ")); err != nil {
				return
			}
		}
	}()
	if !closedWithin(conn2, 4*time.Second) {
		t.Fatal("a client trickling its body kept the connection past the read timeout")
	}
	if reached.Load() != 1 || bodyErr.Load() == nil {
		t.Fatalf("the handler's body read must fail, not wait: reached=%d err=%v", reached.Load(), bodyErr.Load())
	}
}

func TestOversizedHeaderBlockIsRefusedBeforeTheHandler49105(t *testing.T) {
	var reached atomic.Int32
	addr := serveBounded(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}), nil)
	send := func(headerBytes int) int {
		conn, err := net.Dial("tcp", addr)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		var b bytes.Buffer
		b.WriteString("GET /x HTTP/1.1\r\nHost: x\r\n")
		for i := 0; b.Len() < headerBytes; i++ {
			fmt.Fprintf(&b, "X-Pad-%d: %s\r\n", i, strings.Repeat("a", 1000))
		}
		b.WriteString("Connection: close\r\n\r\n")
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		// Written alongside the read: the server stops reading at its limit,
		// so a blocking write would never finish.
		go func() { _, _ = conn.Write(b.Bytes()) }()
		resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
		if err != nil {
			return 0
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	// A long bearer token and cookies fit.
	if got := send(40 << 10); got != http.StatusNoContent || reached.Load() != 1 {
		t.Fatalf("a 40 KiB header block reaches the handler: %d (reached %d)", got, reached.Load())
	}
	if got := send(200 << 10); got != http.StatusRequestHeaderFieldsTooLarge || reached.Load() != 1 {
		t.Fatalf("a 200 KiB header block is refused with 431 before the handler: %d (reached %d)", got, reached.Load())
	}
}

// The envelope validates a body token by token; the worst shapes a 2 MiB body
// can take stay linear: bounded time and memory, whatever the fragmentation.
// Measured 2026-10-27: nested lists cost 70.8 MB and 1.4 s before a list
// stopped getting a key map of its own, 23.6 MB and 0.4 s after; the other
// shapes 1-21 MB. The 48 MB bound holds that fix with room for a slower box.
func TestFragmentedJSONCostIsBounded49105(t *testing.T) {
	limit := int(envelopeMaxBody)
	fill := func(open, item, sep, close string) []byte {
		var b bytes.Buffer
		b.WriteString(open)
		for i := 0; b.Len()+len(item)+len(sep)+len(close)+12 < limit; i++ {
			if i > 0 {
				b.WriteString(sep)
			}
			b.WriteString(strings.ReplaceAll(item, "#", fmt.Sprint(i)))
		}
		b.WriteString(close)
		return b.Bytes()
	}
	nest := strings.Repeat("[", envelopeMaxJSONDepth-2) + strings.Repeat("]", envelopeMaxJSONDepth-2) // with {"a":[ that is the full depth
	for _, c := range []struct {
		name string
		body []byte
	}{
		{"one object, every key distinct", fill("{", `"#":0`, ",", "}")},
		{"many tiny objects", fill(`{"a":[`, `{"#":0}`, ",", "]}")},
		{"maximally nested siblings", fill(`{"a":[`, nest, ",", "]}")},
		{"many short strings", fill(`{"a":[`, `"#"`, ",", "]}")},
	} {
		runtime.GC()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		start := time.Now()
		_ = validateJSONEnvelope(c.body, envelopeMaxJSONString)
		elapsed := time.Since(start)
		runtime.ReadMemStats(&after)
		allocMB := float64(after.TotalAlloc-before.TotalAlloc) / (1 << 20)
		t.Logf("%s: %d bytes, %v, %.1f MB allocated", c.name, len(c.body), elapsed, allocMB)
		if elapsed > 3*time.Second || allocMB > 48 {
			t.Errorf("%s: %v and %.1f MB for one 2 MiB body", c.name, elapsed, allocMB)
		}
	}
}

// A failed sign-in keys its backoff on the name typed; a name far longer than
// any account's (users.username is VARCHAR(100)) must not be kept whole, and
// churning names keeps every map within its cap.
func TestLoginAbuseStateStaysBounded49105(t *testing.T) {
	loginAbuse.Lock()
	saved := loginAbuse.backoff
	savedAddr, savedName := loginAbuse.byAddr, loginAbuse.byName
	loginAbuse.backoff = map[string]*loginBackoffEntry{}
	loginAbuse.byAddr, loginAbuse.byName = map[string]*loginWatchEntry{}, map[string]*loginWatchEntry{}
	loginAbuse.Unlock()
	t.Cleanup(func() {
		loginAbuse.Lock()
		loginAbuse.backoff, loginAbuse.byAddr, loginAbuse.byName = saved, savedAddr, savedName
		loginAbuse.Unlock()
	})

	before := heapNow()
	const longNames = 1000
	for i := 0; i < longNames; i++ {
		name := fmt.Sprintf("%06d", i) + strings.Repeat("x", 64<<10) // 64 KiB each, distinct in the first 100 bytes
		loginFailed("default", name, fmt.Sprintf("198.51.100.%d", i%250))
	}
	if grown := int64(heapNow()) - int64(before); grown > 16<<20 {
		t.Fatalf("%d failed sign-ins with 64 KiB names kept %d MB (the names themselves are %d MB)", longNames, grown>>20, longNames*64>>10)
	}
	loginAbuse.Lock()
	for k := range loginAbuse.backoff {
		if len(k) > len("default")+1+loginNameMax+1+len("198.51.100.255") {
			loginAbuse.Unlock()
			t.Fatalf("a backoff key of %d bytes", len(k))
		}
	}
	loginAbuse.Unlock()

	// The same long name, however it continues past the width, is one name.
	long := strings.Repeat("y", loginNameMax)
	if loginNameKey("default", long+"a") != loginNameKey("default", long+"b") || loginNameKey("default", " Admin ") != loginNameKey("default", "admin") {
		t.Fatal("name keys: truncated at the column width, trimmed and case-folded")
	}

	for i := 0; i < 3*loginStateCap; i++ {
		loginFailed("default", fmt.Sprintf("churn-%d", i), fmt.Sprintf("203.0.%d.%d", (i/250)%250, i%250))
	}
	loginAbuse.Lock()
	n1, n2, n3 := len(loginAbuse.backoff), len(loginAbuse.byAddr), len(loginAbuse.byName)
	loginAbuse.Unlock()
	if n1 > loginStateCap+1 || n2 > loginStateCap+1 || n3 > loginStateCap+1 {
		t.Fatalf("login-abuse maps grew past the cap of %d: backoff=%d byAddr=%d byName=%d", loginStateCap, n1, n2, n3)
	}
}
