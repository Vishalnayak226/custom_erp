package engines

import (
	"context"
	"crypto/tls"
	"custom_erp/db"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
)

// Stage 49.13.7 - graceful dependency failure, the part left after the audit
// writer: the DNS is down, the far end's TLS cannot be trusted, the disk is
// full. Each stops in an explicit, named state, sends nothing it should not,
// leaves nothing half-done, and is retried a bounded number of times - never
// silently skipped and never "succeeded". Local fakes only: a resolver that
// fails without the network, an httptest TLS server whose certificate no
// system trusts, a file whose writes run out of room part-way.

// downResolver is a DNS that answers nothing.
type downResolver struct{ asked atomic.Int32 }

func (r *downResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	r.asked.Add(1)
	return nil, &net.DNSError{Err: "server misbehaving", Name: host, IsTemporary: true}
}

// failureTestCalls counts the test job handler's runs.
var failureTestCalls atomic.Int32

const failureTestJobType = "test-49137-outbound"

func ensureFailureTestJob() {
	if _, ok := jobHandlers[failureTestJobType]; ok {
		return
	}
	// The real signed-POST mechanism every webhook delivery uses (the
	// sandbox and Outside-calls guards in front of it would simulate the
	// call in a test, which proves nothing about the dependency).
	RegisterJobHandler(failureTestJobType, func(ctx context.Context, schema string, job Job) (map[string]interface{}, error) {
		failureTestCalls.Add(1)
		u, _ := job.Payload["url"].(string)
		return sendWebhookHTTPContext(ctx, u, "secret-49137", "test.49137", map[string]interface{}{"n": 1}, job.ID)
	})
}

func TestStage49137DNSAndTLSFailuresAreExplicitAndBounded(t *testing.T) {
	oldResolver, oldPrivate := outboundResolver, outboundAllowPrivate
	t.Cleanup(func() { outboundResolver, outboundAllowPrivate = oldResolver, oldPrivate })
	if c := outboundTransport.TLSClientConfig; c != nil && c.InsecureSkipVerify {
		t.Fatal("the shared outbound transport skips certificate checks")
	}

	// The DNS is down: an explicit "does not resolve", no fallback address,
	// no success - and no network was needed to prove it.
	dns := &downResolver{}
	outboundResolver, outboundAllowPrivate = dns, false
	res, err := sendWebhookHTTPContext(context.Background(), "https://hooks.partner.example/erp", "s", "test.49137", nil, "job-dns")
	if err == nil || res != nil || !strings.Contains(err.Error(), `"hooks.partner.example" does not resolve`) || dns.asked.Load() == 0 {
		t.Fatalf("DNS down: result %v, error %v (resolver asked %d times) - want an explicit 'does not resolve'", res, err, dns.asked.Load())
	}

	// The far end's certificate is not trusted: the handshake fails and not a
	// byte of the signed body reaches it.
	var reached atomic.Int32
	untrusted := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached.Add(1)
		io.Copy(io.Discard, r.Body)
	}))
	defer untrusted.Close()
	outboundAllowPrivate = true // the fake binds to loopback, which the guard refuses
	res, err = sendWebhookHTTPContext(context.Background(), untrusted.URL+"/hook", "s", "test.49137", nil, "job-tls")
	var certErr *tls.CertificateVerificationError
	if err == nil || res != nil || !errors.As(err, &certErr) || reached.Load() != 0 {
		t.Fatalf("untrusted TLS: result %v, error %v, requests that reached it %d - want a certificate error and nothing sent", res, err, reached.Load())
	}

	// Through the job runner the same failures are retried with growing
	// waits, a bounded number of times, then dead-lettered with the reason -
	// never marked done, never retried forever.
	db.InitDB(testConnStr())
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatal(err)
	}
	ensureFailureTestJob()
	for _, c := range []struct {
		name, url, why string
		private        bool
	}{
		{"DNS down", "https://hooks.partner.example/erp", "does not resolve", false},
		{"untrusted TLS", untrusted.URL + "/hook", "certificate", true},
	} {
		outboundAllowPrivate = c.private
		failureTestCalls.Store(0)
		id, err := EnqueueJob("default", failureTestJobType, map[string]interface{}{"url": c.url}, "")
		if err != nil {
			t.Fatalf("%s: enqueue: %v", c.name, err)
		}
		t.Cleanup(func() { db.DB.Exec("DELETE FROM "+schema+".async_jobs WHERE id = $1", id) })
		if _, err := db.DB.Exec("UPDATE "+schema+".async_jobs SET max_attempts = 3 WHERE id = $1", id); err != nil {
			t.Fatal(err)
		}
		for attempt, wait := range []int{30, 60, 0} {
			runClaimedJobs(schema)
			var status, lastErr string
			var attempts int
			var waitSecs float64
			if err := db.DB.QueryRow(`SELECT status, attempts, last_error, COALESCE(EXTRACT(EPOCH FROM next_attempt_at - updated_at), 0)
				FROM `+schema+`.async_jobs WHERE id = $1`, id).Scan(&status, &attempts, &lastErr, &waitSecs); err != nil {
				t.Fatalf("%s: reload: %v", c.name, err)
			}
			if attempts != attempt+1 || !strings.Contains(lastErr, c.why) {
				t.Fatalf("%s attempt %d: attempts %d, last error %q - want %d and the reason %q", c.name, attempt+1, attempts, lastErr, attempt+1, c.why)
			}
			if wait > 0 && (status != "Pending" || int(waitSecs+0.5) != wait) {
				t.Fatalf("%s attempt %d: %s, next try in %.0fs - want Pending, retried in %ds", c.name, attempt+1, status, waitSecs, wait)
			}
			if wait == 0 && status != "DeadLettered" {
				t.Fatalf("%s: after the last attempt the job is %s, want DeadLettered", c.name, status)
			}
			db.DB.Exec("UPDATE "+schema+".async_jobs SET next_attempt_at = CURRENT_TIMESTAMP - INTERVAL '1 second' WHERE id = $1 AND status = 'Pending'", id)
		}
		runClaimedJobs(schema) // a dead-lettered job is not run again
		if n := failureTestCalls.Load(); n != 3 {
			t.Errorf("%s: the delivery ran %d times, want exactly 3", c.name, n)
		}
	}
	if reached.Load() != 0 {
		t.Errorf("%d requests reached the untrusted server through the runner", reached.Load())
	}
}

// fillingFile writes until its room runs out, then fails as a full disk does.
type fillingFile struct {
	f    *os.File
	room int
}

func (w *fillingFile) Write(b []byte) (int, error) {
	n := len(b)
	if n > w.room {
		n = w.room
	}
	w.f.Write(b[:n])
	w.room -= n
	if n < len(b) {
		return n, &os.PathError{Op: "write", Path: w.f.Name(), Err: syscall.ENOSPC}
	}
	return n, nil
}
func (w *fillingFile) Close() error { return w.f.Close() }

func TestStage49137FullDiskSavesNothingHalfWay(t *testing.T) {
	oldOpen, oldDir := openMediaFile, mediaStoreDir
	mediaStoreDir = t.TempDir()
	t.Cleanup(func() { openMediaFile, mediaStoreDir = oldOpen, oldDir })
	openMediaFile = func(name string, flag int, perm os.FileMode) (io.WriteCloser, error) {
		f, err := os.OpenFile(name, flag, perm)
		if err != nil {
			return nil, err
		}
		return &fillingFile{f: f, room: 100}, nil
	}

	// The store: an explicit "disk is full" (still matching ENOSPC), no path
	// or errno for the person uploading, and no truncated file left to serve.
	err := serverMedia.Write("receipts/r1.pdf", make([]byte, 5000), MediaWrite{})
	if err == nil || !errors.Is(err, syscall.ENOSPC) || !strings.Contains(err.Error(), "the server's disk is full - the file was not saved") ||
		strings.Contains(err.Error(), mediaStoreDir) {
		t.Fatalf("full disk: %v - want the plain 'disk is full' error", err)
	}
	if _, statErr := os.Stat(filepath.Join(mediaStoreDir, "receipts", "r1.pdf")); statErr == nil {
		t.Fatal("a half-written file was left in the media store")
	}
	if other := serverMedia.Write("../escape", []byte("x"), MediaWrite{}); other == nil || errors.Is(other, syscall.ENOSPC) {
		t.Fatalf("other errors must pass through as they were: %v", other)
	}

	// An upload on a full disk: refused in words, no record pointing at a
	// missing file, no file. With room again, the same upload goes through -
	// the retry is the person's, nothing loops on a full disk.
	db.InitDB(testConnStr())
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatal(err)
	}
	const item = "ST49137-ITEM"
	cleanup := func() {
		db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.documents WHERE doctype = 'ProductMedia' AND data->>'item' = $1`, schema), item)
	}
	cleanup()
	t.Cleanup(cleanup)
	img := tinyTestJPEG(t, 400, 400)
	records := func() int {
		var n int
		db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.documents WHERE doctype = 'ProductMedia' AND data->>'item' = $1`, schema), item).Scan(&n)
		return n
	}
	if _, err := SaveMediaFile("default", img, "st49137.jpg", item, "Gallery", "system", "", ""); err == nil || !strings.Contains(err.Error(), "disk is full") {
		t.Fatalf("an upload on a full disk: %v - want it refused as 'disk is full'", err)
	}
	if n := records(); n != 0 {
		t.Fatalf("an upload refused for a full disk left %d record(s)", n)
	}
	if entries, _ := os.ReadDir(mediaStoreDir); len(entries) > 1 { // receipts/ from above
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("an upload refused for a full disk left files: %v", names)
	}
	openMediaFile = oldOpen
	asset, err := SaveMediaFile("default", img, "st49137.jpg", item, "Gallery", "system", "", "")
	if err != nil || asset == nil || records() != 1 {
		t.Fatalf("with room again the upload must go through: %v", err)
	}
}
